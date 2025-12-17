package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moehoshio/NekoLcServer/internal/auth"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/localization"
	"github.com/moehoshio/NekoLcServer/internal/store"
)

func newTestServer(t *testing.T, mutate func(*config.AppConfig)) *Server {
	return newTestServerWithConfig(t, mutate, nil)
}

func newTestServerWithConfig(t *testing.T, mutateApp func(*config.AppConfig), mutateUpdate func(*config.UpdateConfig)) *Server {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	appCfgPath := filepath.Join(root, "configs", "app.json")
	appCfg, err := config.LoadAppConfig(appCfgPath)
	if err != nil {
		t.Fatalf("load app config: %v", err)
	}
	if mutateApp != nil {
		mutateApp(appCfg)
	}
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(root, filepath.Clean(strings.TrimPrefix(p, "./")))
	}
	langBundle, err := config.LoadLanguages(resolve(appCfg.Language.ConfigPath))
	if err != nil {
		t.Fatalf("load languages: %v", err)
	}
	launcherCfg, err := config.LoadLauncher(resolve(appCfg.Launcher.ConfigPath))
	if err != nil {
		t.Fatalf("load launcher: %v", err)
	}
	maintenanceCfg, err := config.LoadMaintenance(resolve(appCfg.Maintenance.ConfigPath))
	if err != nil {
		t.Fatalf("load maintenance: %v", err)
	}
	newsCfg, err := config.LoadNews(resolve(appCfg.News.ConfigPath))
	if err != nil {
		t.Fatalf("load news: %v", err)
	}
	updatePath := resolve(appCfg.Update.ConfigPath)
	updateCfg, err := config.LoadUpdates(updatePath)
	if err != nil {
		t.Fatalf("load updates: %v", err)
	}
	if mutateUpdate != nil {
		mutateUpdate(updateCfg)
	}
	localizer := localization.New(appCfg.Language.Default, langBundle)
	memStore := store.NewMemory()
	authService := auth.NewService(appCfg, memStore)
	feedbackPath := filepath.Join(t.TempDir(), "feedback.log")
	updateDir := filepath.Dir(updatePath)
	srv, err := New(appCfg, launcherCfg, maintenanceCfg, newsCfg, updateCfg, updatePath, updateDir, localizer, authService, memStore, feedbackPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv
}

func doRequest(t *testing.T, srv *Server, method, path string, payload interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if payload != nil {
		body = bytes.NewReader(mustJSON(t, payload))
	} else {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	buf, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return buf
}

func TestPing(t *testing.T) {
	srv := newTestServer(t, nil)
	rec := doRequest(t, srv, http.MethodGet, "/v0/testing/ping", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
}

func TestLauncherConfig(t *testing.T) {
	srv := newTestServer(t, nil)
	payload := map[string]interface{}{
		"launcherConfigRequest": map[string]interface{}{
			"timestamp": 123,
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/launcherConfig", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
}

func TestMaintenancePlatformSpecific(t *testing.T) {
	srv := newTestServer(t, nil)
	payload := map[string]interface{}{
		"maintenanceRequest": map[string]interface{}{
			"timestamp": 123,
			"clientInfo": map[string]interface{}{
				"system": map[string]interface{}{
					"os":   "linux",
					"arch": "x64",
				},
			},
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/maintenance", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
}

func TestUpdateCheckReturnsFiles(t *testing.T) {
	srv := newTestServer(t, nil)
	payload := map[string]interface{}{
		"updateRequest": map[string]interface{}{
			"timestamp": 123,
			"clientInfo": map[string]interface{}{
				"app": map[string]interface{}{
					"coreVersion":     "1.0.0",
					"resourceVersion": "1.0.0",
				},
				"system": map[string]interface{}{
					"os":   "windows",
					"arch": "x64",
				},
			},
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/checkUpdates", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
}

func TestUpdateDirectoryPathProducesFiles(t *testing.T) {
	tmp := t.TempDir()
	nested := filepath.Join(tmp, "img", "img.png")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := []byte("hello update")
	if err := os.WriteFile(nested, original, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	originalHash := fmt.Sprintf("%x", sha256.Sum256(original))

	srv := newTestServerWithConfig(t, nil, func(cfg *config.UpdateConfig) {
		win := cfg.Platforms["windows"]
		arch := win.Architectures["x64"]
		arch.Latest.CoreVersion = "9.9.9"
		arch.Latest.Core = []config.DownloadEntry{{
			Path: tmp,
			DownloadMeta: config.DownloadMeta{
				HashAlgorithm: "sha256",
			},
		}}
		arch.Latest.Resource = nil
		arch.Diffs = nil
		win.Architectures["x64"] = arch
		cfg.Platforms["windows"] = win
	})

	payload := map[string]interface{}{
		"updateRequest": map[string]interface{}{
			"timestamp": 123,
			"clientInfo": map[string]interface{}{
				"app": map[string]interface{}{
					"coreVersion":     "1.0.0",
					"resourceVersion": "1.0.0",
				},
				"system": map[string]interface{}{
					"os":   "windows",
					"arch": "x64",
				},
			},
		},
	}

	rec := doRequest(t, srv, http.MethodPost, "/v0/api/checkUpdates", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	var resp UpdateResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.UpdateResponse.Files) != 1 {
		t.Fatalf("expected 1 file got %d", len(resp.UpdateResponse.Files))
	}
	file := resp.UpdateResponse.Files[0]
	if file.URL != "img/img.png" {
		t.Fatalf("unexpected url %s", file.URL)
	}
	if file.Checksum != originalHash {
		t.Fatalf("checksum mismatch got %s want %s", file.Checksum, originalHash)
	}
	if file.Size != int64(len(original)) {
		t.Fatalf("size mismatch got %d want %d", file.Size, len(original))
	}
	if !file.DownloadMeta.IsCoreFile {
		t.Fatalf("expected IsCoreFile true")
	}

	modified := []byte("changed update")
	if err := os.WriteFile(nested, modified, 0o644); err != nil {
		t.Fatalf("rewrite file: %v", err)
	}
	updatedHash := fmt.Sprintf("%x", sha256.Sum256(modified))

	rec = doRequest(t, srv, http.MethodPost, "/v0/api/checkUpdates", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	var resp2 UpdateResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode response2: %v", err)
	}
	if len(resp2.UpdateResponse.Files) != 1 {
		t.Fatalf("expected 1 file got %d", len(resp2.UpdateResponse.Files))
	}
	if resp2.UpdateResponse.Files[0].Checksum != updatedHash {
		t.Fatalf("checksum did not refresh, got %s want %s", resp2.UpdateResponse.Files[0].Checksum, updatedHash)
	}
}

func TestUpdateDirectoryPathWithBaseURL(t *testing.T) {
	tmp := t.TempDir()
	img := filepath.Join(tmp, "img.png")
	logFile := filepath.Join(tmp, "logs", "log.txt")
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	imgBytes := []byte("image-data")
	logBytes := []byte("log-data")
	if err := os.WriteFile(img, imgBytes, 0o644); err != nil {
		t.Fatalf("write img: %v", err)
	}
	if err := os.WriteFile(logFile, logBytes, 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	imgHash := fmt.Sprintf("%x", sha256.Sum256(imgBytes))
	logHash := fmt.Sprintf("%x", sha256.Sum256(logBytes))
	base := "https://example.com/updates/windows-x64-0.0.1-files/"

	srv := newTestServerWithConfig(t, nil, func(cfg *config.UpdateConfig) {
		win := cfg.Platforms["windows"]
		arch := win.Architectures["x64"]
		arch.Latest.CoreVersion = "9.9.9"
		arch.Latest.Core = []config.DownloadEntry{{
			Path:    tmp,
			BaseURL: base,
			DownloadMeta: config.DownloadMeta{
				HashAlgorithm: "sha256",
			},
		}}
		arch.Latest.Resource = nil
		arch.Diffs = nil
		win.Architectures["x64"] = arch
		cfg.Platforms["windows"] = win
	})

	payload := map[string]interface{}{
		"updateRequest": map[string]interface{}{
			"timestamp": 123,
			"clientInfo": map[string]interface{}{
				"app": map[string]interface{}{
					"coreVersion":     "1.0.0",
					"resourceVersion": "1.0.0",
				},
				"system": map[string]interface{}{
					"os":   "windows",
					"arch": "x64",
				},
			},
		},
	}

	rec := doRequest(t, srv, http.MethodPost, "/v0/api/checkUpdates", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	var resp UpdateResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.UpdateResponse.Files) != 2 {
		t.Fatalf("expected 2 files got %d", len(resp.UpdateResponse.Files))
	}
	files := map[string]UpdateFileResponse{}
	for _, f := range resp.UpdateResponse.Files {
		files[f.URL] = f
	}
	expectedImgURL := base + "img.png"
	expectedLogURL := base + "logs/log.txt"
	imgFile, ok := files[expectedImgURL]
	if !ok {
		t.Fatalf("missing img file url %s", expectedImgURL)
	}
	logEntry, ok := files[expectedLogURL]
	if !ok {
		t.Fatalf("missing log file url %s", expectedLogURL)
	}
	if imgFile.Checksum != imgHash || logEntry.Checksum != logHash {
		t.Fatalf("checksum mismatch img %s log %s", imgFile.Checksum, logEntry.Checksum)
	}
	if imgFile.Size != int64(len(imgBytes)) || logEntry.Size != int64(len(logBytes)) {
		t.Fatalf("size mismatch img %d log %d", imgFile.Size, logEntry.Size)
	}
	if !imgFile.DownloadMeta.IsAbsoluteURL || !logEntry.DownloadMeta.IsAbsoluteURL {
		t.Fatalf("expected absolute URLs with baseUrl")
	}
	if !imgFile.DownloadMeta.IsCoreFile || !logEntry.DownloadMeta.IsCoreFile {
		t.Fatalf("expected IsCoreFile true")
	}
}

func TestNewsReturnsItems(t *testing.T) {
	srv := newTestServer(t, nil)
	payload := map[string]interface{}{
		"newsRequest": map[string]interface{}{
			"timestamp": 123,
			"limit":     1,
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/news", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	var resp NewsResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.NewsResponse.Items) != 1 {
		t.Fatalf("expected 1 item got %d", len(resp.NewsResponse.Items))
	}
	if !resp.NewsResponse.HasMore {
		t.Fatalf("expected hasMore true")
	}
}

func TestNewsInvalidLastID(t *testing.T) {
	srv := newTestServer(t, nil)
	payload := map[string]interface{}{
		"newsRequest": map[string]interface{}{
			"timestamp": 123,
			"lastId":    "does-not-exist",
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/news", payload)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", rec.Code)
	}
}

func TestAuthDisabledLogin(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = false
	})
	payload := map[string]interface{}{
		"loginRequest": map[string]interface{}{
			"identifier": "device",
			"timestamp":  123,
			"signature":  "invalid",
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/auth/login", payload)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 got %d", rec.Code)
	}
}

func TestJWTLoginRejectsUsernamePassword(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "jwt"
	})
	payload := map[string]interface{}{
		"loginRequest": map[string]interface{}{
			"username": "user",
			"password": "pass",
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/auth/login", payload)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", rec.Code)
	}
}

func TestJWTLoginSuccess(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "jwt"
	})
	identifier := "device-uuid"
	timestamp := time.Now().UTC().Unix()
	secret := srv.appConfig.Authentication.JWT.JWTSecret
	if secret == "" {
		secret = srv.appConfig.Authentication.JWTSecret
	}
	signature := makeSignature(identifier, timestamp, secret)
	payload := map[string]interface{}{
		"loginRequest": map[string]interface{}{
			"identifier": identifier,
			"timestamp":  timestamp,
			"signature":  signature,
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/auth/login", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
}

func TestAccountModeNotImplemented(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Method = "account"
	})
	payload := map[string]interface{}{
		"loginRequest": map[string]interface{}{
			"identifier": "device",
			"timestamp":  123,
			"signature":  "invalid",
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/auth/login", payload)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 got %d", rec.Code)
	}
}

func TestFeedbackViewerJSON(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Debug.Enabled = true
	})

	feedbackPayload := map[string]interface{}{
		"feedbackLogRequest": map[string]interface{}{
			"timestamp": time.Now().UTC().Unix(),
			"content":   "test feedback",
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/feedbackLog", feedbackPayload)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 got %d", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodGet, "/debug/feedback.json", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	count, ok := body["count"].(float64)
	if !ok || count < 1 {
		t.Fatalf("expected entries in feedback json, got %v", body)
	}
}

func makeSignature(identifier string, timestamp int64, secret string) string {
	payload := fmt.Sprintf("%s:%d:%s", identifier, timestamp, secret)
	sum := sha256.Sum256([]byte(payload))
	return base64.StdEncoding.EncodeToString(sum[:])
}
