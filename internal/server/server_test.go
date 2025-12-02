package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moehoshio/NekoLcServer/internal/auth"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/localization"
)

func newTestServer(t *testing.T, mutate func(*config.AppConfig)) *Server {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	appCfgPath := filepath.Join(root, "configs", "app.json")
	appCfg, err := config.LoadAppConfig(appCfgPath)
	if err != nil {
		t.Fatalf("load app config: %v", err)
	}
	if mutate != nil {
		mutate(appCfg)
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
	updatePath := resolve(appCfg.Update.ConfigPath)
	updateCfg, err := config.LoadUpdates(updatePath)
	if err != nil {
		t.Fatalf("load updates: %v", err)
	}
	localizer := localization.New(appCfg.Language.Default, langBundle)
	authService := auth.NewService(appCfg)
	feedbackPath := filepath.Join(t.TempDir(), "feedback.log")
	updateDir := filepath.Dir(updatePath)
	srv, err := New(appCfg, launcherCfg, maintenanceCfg, updateCfg, updateDir, localizer, authService, feedbackPath)
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
	signature := makeSignature(identifier, timestamp, srv.appConfig.Authentication.JWTSecret)
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

func makeSignature(identifier string, timestamp int64, secret string) string {
	payload := fmt.Sprintf("%s:%d:%s", identifier, timestamp, secret)
	sum := sha256.Sum256([]byte(payload))
	return base64.StdEncoding.EncodeToString(sum[:])
}
