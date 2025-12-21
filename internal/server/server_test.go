package server

import (
	"bytes"
	"context"
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

	"golang.org/x/crypto/bcrypt"

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
	appCfgPath := filepath.Join(root, "config.json")
	appCfg, err := config.LoadAppConfig(appCfgPath)
	if err != nil {
		t.Fatalf("load app config: %v", err)
	}
	if mutateApp != nil {
		mutateApp(appCfg)
	}

	// Use default in-memory configurations since configs are now stored in database
	langBundle := config.LanguageBundle{
		"en": config.LanguagePack{
			Errors: map[string]string{
				"InvalidRequest":     "The request is invalid.",
				"NotFound":           "Resource not found.",
				"Unauthorized":       "Authentication required.",
				"InternalError":      "Internal server error.",
				"NotImplemented":     "Feature not implemented.",
				"ServiceUnavailable": "Service is currently unavailable.",
			},
			Maintenance: map[string]string{
				"scheduled": "Scheduled maintenance",
				"progress":  "Maintenance in progress",
			},
			Updates: map[string]string{
				"available":   "New version available",
				"description": "Bug fixes and improvements",
			},
		},
	}

	launcherCfg := &config.LauncherConfig{
		Host:             []string{"localhost:8080", "api.example.com"},
		RetryIntervalSec: 5,
		MaxRetryCount:    3,
		WebSocket: config.WebSocketConfig{
			Enable:               false,
			SocketHost:           "",
			HeartbeatIntervalSec: 30,
		},
		Security: config.SecurityConfig{
			EnableAuthentication:       false,
			TokenExpirationSec:         3600,
			RefreshTokenExpirationDays: 30,
			LoginURL:                   "/v0/api/auth/login",
			LogoutURL:                  "/v0/api/auth/logout",
			RefreshURL:                 "/v0/api/auth/refresh",
			RegisterURL:                "/v0/api/auth/register",
		},
		FeaturesFlags: map[string]interface{}{},
	}

	maintenanceCfg := &config.MaintenanceConfig{
		MaintenanceActive: false,
		MaintenanceInfo: config.MaintenanceInfo{
			Status:  "scheduled",
			Message: "Scheduled maintenance",
			Start:   "2024-06-01T12:00:00Z",
			End:     "2024-06-01T14:00:00Z",
			Poster:  "https://example.com/maintenance-poster.jpg",
			Link:    "https://example.com/maintenance-announcement",
		},
		PlatformSpecific: map[string]config.PlatformMaintenance{
			"linux-x64": {
				MaintenanceActive: true,
				MaintenanceInfo: config.MaintenanceInfo{
					Status:  "progress",
					Message: "Linux x64 servers undergoing maintenance",
				},
			},
			"windows-x64": {
				MaintenanceActive: false,
				MaintenanceInfo: config.MaintenanceInfo{
					Status:  "",
					Message: "No scheduled maintenance for Windows x64",
				},
			},
		},
	}

	newsCfg := &config.NewsConfig{
		Items: []config.NewsItem{
			{
				ID:          "news-001",
				Title:       "Welcome to NekoLc",
				Summary:     "A brief introduction to NekoLc",
				Content:     "Welcome! NekoLc is a launcher server for managing updates.",
				PosterURL:   "https://example.com/welcome-poster.jpg",
				Link:        "https://example.com/news/welcome",
				PublishTime: "2024-01-15T10:00:00Z",
				Category:    "announcement",
				Tags:        []string{"welcome", "introduction"},
				Priority:    10,
			},
		},
	}

	updateCfg := &config.UpdateConfig{
		Platforms: map[string]config.PlatformUpdates{
			"linux": {
				Architectures: map[string]config.ArchUpdates{
					"x64": {
						Latest: config.FullPackage{
							CoreVersion:     "1.2.0",
							ResourceVersion: "1.0.5",
							Core: []config.DownloadEntry{
								{
									URL:      "https://cdn.example.com/linux-x64/core-1.2.0.tar.gz",
									FileName: "core-1.2.0.tar.gz",
									Checksum: "sha256:abc123",
									Size:     15000000,
									DownloadMeta: config.DownloadMeta{
										HashAlgorithm:      "sha256",
										SuggestMultiThread: true,
									},
								},
							},
							Resource: []config.DownloadEntry{
								{
									Path:    "/path/to/one",
									BaseURL: "https://cdn.example.com/linux-x64/",
									DownloadMeta: config.DownloadMeta{
										HashAlgorithm:      "sha256",
										SuggestMultiThread: false,
									},
								},
							},
						},
						Diffs: []config.DiffFile{},
					},
				},
			},
			"windows": {
				Architectures: map[string]config.ArchUpdates{
					"x64": {
						Latest: config.FullPackage{
							CoreVersion:     "1.2.0",
							ResourceVersion: "1.0.5",
							Core:            []config.DownloadEntry{},
							Resource:        []config.DownloadEntry{},
						},
						Diffs: []config.DiffFile{},
					},
				},
			},
		},
	}
	if mutateUpdate != nil {
		mutateUpdate(updateCfg)
	}

	localizer := localization.New(appCfg.Language.Default, langBundle)
	memStore := store.NewMemory()
	authService := auth.NewService(appCfg, memStore)
	feedbackPath := filepath.Join(t.TempDir(), "feedback.log")
	updateDir := t.TempDir()
	// Use temp paths for maintenance and news in tests to avoid modifying real config files
	testMaintPath := filepath.Join(t.TempDir(), "maintenance.json")
	testNewsPath := filepath.Join(t.TempDir(), "news.json")
	updatePath := filepath.Join(t.TempDir(), "updates.json")
	srv, err := New(appCfg, launcherCfg, maintenanceCfg, testMaintPath, newsCfg, testNewsPath, updateCfg, updatePath, updateDir, localizer, authService, memStore, feedbackPath)
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

func TestJWTLoginWithUsernamePassword(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "jwt"
	})
	// With a store available, username/password login should work
	// But with non-existent user, it should return 401
	payload := map[string]interface{}{
		"loginRequest": map[string]interface{}{
			"username": "nonexistent",
			"password": "pass",
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/auth/login", payload)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", rec.Code)
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

func doAuthRequest(t *testing.T, srv *Server, method, path string, payload interface{}, token string) *httptest.ResponseRecorder {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func loginAsAdmin(t *testing.T, srv *Server) string {
	t.Helper()
	// Create an admin user in memory store
	memStore := srv.store
	if memStore == nil {
		t.Fatalf("store is nil")
	}
	// Use bcrypt to hash password
	password := "adminpass123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	_, err = memStore.CreateUser(context.Background(), "testadmin", string(hash), "admin")
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	// Login to get token
	payload := map[string]interface{}{
		"loginRequest": map[string]interface{}{
			"username": "testadmin",
			"password": password,
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/auth/login", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("login expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var loginResp LoginResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return loginResp.LoginResponse.AccessToken
}

func TestAdminGetMaintenanceUnauthorized(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	rec := doAuthRequest(t, srv, http.MethodGet, "/v0/api/admin/maintenance", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", rec.Code)
	}
}

func TestAdminGetMaintenance(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	token := loginAsAdmin(t, srv)
	rec := doAuthRequest(t, srv, http.MethodGet, "/v0/api/admin/maintenance", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AdminMaintenanceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Verify we got a response with maintenance data
	if resp.Meta.APIVersion == "" {
		t.Fatalf("expected meta.apiVersion in response")
	}
}

func TestAdminUpdateMaintenance(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	token := loginAsAdmin(t, srv)
	payload := map[string]interface{}{
		"maintenance": map[string]interface{}{
			"maintenanceActive": true,
			"maintenanceInfo": map[string]interface{}{
				"status":    "progress",
				"message":   "Test maintenance",
				"startTime": "2024-01-01T00:00:00Z",
				"exEndTime": "2024-01-01T02:00:00Z",
				"posterUrl": "",
				"link":      "",
			},
			"platformSpecific": map[string]interface{}{},
		},
	}
	rec := doAuthRequest(t, srv, http.MethodPut, "/v0/api/admin/maintenance", payload, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AdminMessageResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Message == "" {
		t.Fatalf("expected success message")
	}
}

func TestAdminGetUpdates(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	token := loginAsAdmin(t, srv)
	rec := doAuthRequest(t, srv, http.MethodGet, "/v0/api/admin/updates", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AdminUpdatesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Updates.Platforms == nil {
		t.Fatalf("expected platforms in response")
	}
}

func TestAdminGetNews(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	token := loginAsAdmin(t, srv)
	rec := doAuthRequest(t, srv, http.MethodGet, "/v0/api/admin/news", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AdminNewsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.News.Items == nil {
		t.Fatalf("expected items in response")
	}
}

func TestAdminUpdateNews(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	token := loginAsAdmin(t, srv)
	payload := map[string]interface{}{
		"news": map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":          "test-001",
					"title":       "Test News",
					"summary":     "Test summary",
					"content":     "Test content",
					"posterUrl":   "",
					"link":        "",
					"publishTime": "2024-01-01T00:00:00Z",
					"category":    "general",
					"tags":        []string{},
					"priority":    5,
				},
			},
		},
	}
	rec := doAuthRequest(t, srv, http.MethodPut, "/v0/api/admin/news", payload, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AdminMessageResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Message == "" {
		t.Fatalf("expected success message")
	}
}

func TestAppAdminPage(t *testing.T) {
	srv := newTestServer(t, nil)
	rec := doRequest(t, srv, http.MethodGet, "/app/admin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "NekoLc Admin Dashboard") {
		t.Fatalf("expected admin dashboard page content")
	}
}

func TestAdminGetLauncher(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	token := loginAsAdmin(t, srv)
	rec := doAuthRequest(t, srv, http.MethodGet, "/v0/api/admin/launcher", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AdminLauncherResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Meta.APIVersion == "" {
		t.Fatalf("expected meta.apiVersion in response")
	}
}

func TestAdminUpdateLauncher(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	token := loginAsAdmin(t, srv)
	payload := map[string]interface{}{
		"launcher": map[string]interface{}{
			"host":             []string{"https://api.example.com"},
			"retryIntervalSec": 10,
			"maxRetryCount":    5,
			"webSocket": map[string]interface{}{
				"enable":               true,
				"socketHost":           "wss://ws.example.com",
				"heartbeatIntervalSec": 30,
			},
			"security": map[string]interface{}{
				"enableAuthentication":       true,
				"tokenExpirationSec":         7200,
				"refreshTokenExpirationDays": 14,
				"loginUrl":                   "/v0/api/auth/login",
				"logoutUrl":                  "/v0/api/auth/logout",
			},
			"featuresFlags": map[string]interface{}{},
		},
	}
	rec := doAuthRequest(t, srv, http.MethodPut, "/v0/api/admin/launcher", payload, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AdminMessageResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Message == "" {
		t.Fatalf("expected success message")
	}
}

func TestAppRegisterPage(t *testing.T) {
	srv := newTestServer(t, nil)
	rec := doRequest(t, srv, http.MethodGet, "/app/register", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Create Account") {
		t.Fatalf("expected register page content")
	}
}

func TestAppHomePage(t *testing.T) {
	srv := newTestServer(t, nil)
	// Test /app
	rec := doRequest(t, srv, http.MethodGet, "/app", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "NekoLcServer") {
		t.Fatalf("expected home page content")
	}
	// Test /app/
	rec = doRequest(t, srv, http.MethodGet, "/app/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d for /app/", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "NekoLcServer") {
		t.Fatalf("expected home page content for /app/")
	}
}

func TestRegisterSuccess(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	payload := map[string]interface{}{
		"registerRequest": map[string]interface{}{
			"username": "newuser",
			"password": "password123",
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/app/register", payload)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp RegisterResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.RegisterResponse.Username != "newuser" {
		t.Fatalf("expected username 'newuser' got '%s'", resp.RegisterResponse.Username)
	}
}

func TestRegisterDuplicateUsername(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	// First registration
	payload := map[string]interface{}{
		"registerRequest": map[string]interface{}{
			"username": "dupuser",
			"password": "password123",
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/app/register", payload)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d: %s", rec.Code, rec.Body.String())
	}
	// Second registration with same username
	rec = doRequest(t, srv, http.MethodPost, "/app/register", payload)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRegisterValidation(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	// Short username
	payload := map[string]interface{}{
		"registerRequest": map[string]interface{}{
			"username": "ab",
			"password": "password123",
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/app/register", payload)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d: %s", rec.Code, rec.Body.String())
	}
	// Short password
	payload = map[string]interface{}{
		"registerRequest": map[string]interface{}{
			"username": "validuser",
			"password": "12345",
		},
	}
	rec = doRequest(t, srv, http.MethodPost, "/app/register", payload)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRegisterThenLogin(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	// Register a new user
	registerPayload := map[string]interface{}{
		"registerRequest": map[string]interface{}{
			"username": "loginuser",
			"password": "mypassword123",
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/app/register", registerPayload)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register expected 201 got %d: %s", rec.Code, rec.Body.String())
	}

	// Now try to login with the same credentials
	loginPayload := map[string]interface{}{
		"loginRequest": map[string]interface{}{
			"username": "loginuser",
			"password": "mypassword123",
		},
	}
	rec = doRequest(t, srv, http.MethodPost, "/v0/api/auth/login", loginPayload)
	if rec.Code != http.StatusOK {
		t.Fatalf("login expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp LoginResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if resp.LoginResponse.AccessToken == "" {
		t.Fatal("expected access token")
	}
	if resp.LoginResponse.Username != "loginuser" {
		t.Fatalf("expected username 'loginuser' got '%s'", resp.LoginResponse.Username)
	}
	if resp.LoginResponse.Role != "user" {
		t.Fatalf("expected role 'user' got '%s'", resp.LoginResponse.Role)
	}
}

// TestAppAPILogin tests the app-specific login endpoint (/app/api/login)
func TestAppAPILogin(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})

	// First register a user via /app/register
	registerPayload := map[string]interface{}{
		"registerRequest": map[string]interface{}{
			"username": "appuser",
			"password": "apppassword123",
		},
	}
	rec := doRequest(t, srv, http.MethodPost, "/app/register", registerPayload)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register expected 201 got %d: %s", rec.Code, rec.Body.String())
	}

	// Now try to login with the app-specific endpoint
	loginPayload := map[string]interface{}{
		"username": "appuser",
		"password": "apppassword123",
	}
	rec = doRequest(t, srv, http.MethodPost, "/app/api/login", loginPayload)
	if rec.Code != http.StatusOK {
		t.Fatalf("app api login expected 200 got %d: %s", rec.Code, rec.Body.String())
	}

	var resp AppLoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode app login response: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("expected access token")
	}
	if resp.Username != "appuser" {
		t.Fatalf("expected username 'appuser' got '%s'", resp.Username)
	}
	if resp.Role != "user" {
		t.Fatalf("expected role 'user' got '%s'", resp.Role)
	}
}

// TestGetRegister tests the GET /v0/api/auth/register endpoint
// Per API spec: returns 200 with registerResponse.registerUrl
func TestGetRegister(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})

	rec := doRequest(t, srv, http.MethodGet, "/v0/api/auth/register", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", rec.Code, rec.Body.String())
	}

	var resp RegisterGetResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode register get response: %v", err)
	}
	if resp.RegisterResponse.RegisterURL == "" {
		t.Fatal("expected registerUrl in response")
	}
	// Should contain /app/register path
	if !strings.Contains(resp.RegisterResponse.RegisterURL, "/app/register") {
		t.Fatalf("expected registerUrl to contain '/app/register' got '%s'", resp.RegisterResponse.RegisterURL)
	}
}
