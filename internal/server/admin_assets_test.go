package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/store"
)

func adminServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	token := loginAsAdmin(t, srv)
	return srv, token
}

func uploadRequest(t *testing.T, srv *Server, token, fileName, subdir string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if subdir != "" {
		if err := mw.WriteField("subdir", subdir); err != nil {
			t.Fatalf("write subdir field: %v", err)
		}
	}
	part, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write content: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v0/api/admin/uploadFile", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func TestAdminUploadFileAndServe(t *testing.T) {
	srv, token := adminServer(t)
	content := []byte("hello nekolc update payload")
	rec := uploadRequest(t, srv, token, "patch.zip", "windows-x64", content)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AdminUploadResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.FileName != "patch.zip" {
		t.Fatalf("unexpected file name %q", resp.FileName)
	}
	if resp.RelativePath != "windows-x64/patch.zip" {
		t.Fatalf("unexpected relative path %q", resp.RelativePath)
	}
	if resp.Size != int64(len(content)) {
		t.Fatalf("unexpected size %d", resp.Size)
	}
	if resp.HashAlgorithm != "sha256" || resp.Checksum == "" {
		t.Fatalf("expected sha256 checksum, got %q %q", resp.HashAlgorithm, resp.Checksum)
	}
	// The full URL must be derived from the request host and the /files/ base.
	wantURL := "http://example.com/files/windows-x64/patch.zip"
	if resp.URL != wantURL {
		t.Fatalf("unexpected url %q want %q", resp.URL, wantURL)
	}
	// File must exist on disk under the assets directory.
	stored := filepath.Join(srv.updateAssetsDir, "windows-x64", "patch.zip")
	if data, err := os.ReadFile(stored); err != nil || !bytes.Equal(data, content) {
		t.Fatalf("stored file mismatch: err=%v", err)
	}
	// And it must be downloadable via the static file endpoint.
	getRec := doRequest(t, srv, http.MethodGet, "/files/windows-x64/patch.zip", nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 serving file got %d", getRec.Code)
	}
	if !bytes.Equal(getRec.Body.Bytes(), content) {
		t.Fatalf("served content mismatch")
	}
}

func TestAdminUploadFileRequiresAdmin(t *testing.T) {
	srv, _ := adminServer(t)
	rec := uploadRequest(t, srv, "", "patch.zip", "", []byte("data"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", rec.Code)
	}
}

func TestServeFileRejectsTraversal(t *testing.T) {
	srv, _ := adminServer(t)
	rec := doRequest(t, srv, http.MethodGet, "/files/..%2f..%2fconfig.json", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for traversal got %d", rec.Code)
	}
}

func TestAdminUploadRejectsTraversalSubdir(t *testing.T) {
	srv, token := adminServer(t)
	// A subdirectory escaping the assets directory must be rejected and must not
	// create any file outside the assets directory.
	rec := uploadRequest(t, srv, token, "evil.txt", "../../escape", []byte("x"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal subdir got %d: %s", rec.Code, rec.Body.String())
	}
	parent := filepath.Dir(filepath.Dir(srv.updateAssetsDir))
	if _, err := os.Stat(filepath.Join(parent, "escape", "evil.txt")); err == nil {
		t.Fatalf("traversal upload escaped the assets directory")
	}
}

func TestAdminBrowseRejectsTraversal(t *testing.T) {
	srv, token := adminServer(t)
	rec := doAuthRequest(t, srv, http.MethodGet, "/v0/api/admin/browseDir?path=../..", nil, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal path got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminBrowseDir(t *testing.T) {
	srv, token := adminServer(t)
	// Seed a subdirectory and a file.
	sub := filepath.Join(srv.updateAssetsDir, "linux-x64")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "core.tar.gz"), []byte("abc"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	rec := doAuthRequest(t, srv, http.MethodGet, "/v0/api/admin/browseDir", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AdminBrowseResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var foundDir bool
	for _, e := range resp.Entries {
		if e.Name == "linux-x64" && e.IsDir {
			foundDir = true
		}
	}
	if !foundDir {
		t.Fatalf("expected linux-x64 directory in browse result: %+v", resp.Entries)
	}
	// Browsing into the subdirectory should list the file and expose a parent.
	rec = doAuthRequest(t, srv, http.MethodGet, "/v0/api/admin/browseDir?path=linux-x64", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	resp = AdminBrowseResponse{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 1 || resp.Entries[0].Name != "core.tar.gz" || resp.Entries[0].IsDir {
		t.Fatalf("unexpected entries: %+v", resp.Entries)
	}
}

func TestAdminDeleteFeedback(t *testing.T) {
	srv, token := adminServer(t)
	if err := srv.store.SaveFeedback(context.Background(), store.FeedbackLog{
		DeviceID:   "dev-1",
		Content:    "to be removed",
		ReceivedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save feedback: %v", err)
	}
	rec := doAuthRequest(t, srv, http.MethodDelete, "/v0/api/admin/feedbackLogs/1", nil, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 got %d: %s", rec.Code, rec.Body.String())
	}
	// Deleting again should report not found.
	rec = doAuthRequest(t, srv, http.MethodDelete, "/v0/api/admin/feedbackLogs/1", nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d", rec.Code)
	}
	// Invalid id is a bad request.
	rec = doAuthRequest(t, srv, http.MethodDelete, "/v0/api/admin/feedbackLogs/abc", nil, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", rec.Code)
	}
}
