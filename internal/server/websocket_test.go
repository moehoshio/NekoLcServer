package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moehoshio/NekoLcServer/internal/auth"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/localization"
	"github.com/moehoshio/NekoLcServer/internal/store"
)

func newWSTestServer(t *testing.T) *Server {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	appCfgPath := filepath.Join(root, "config.json")
	appCfg, err := config.LoadAppConfig(appCfgPath)
	if err != nil {
		t.Fatalf("load app config: %v", err)
	}
	// Ensure authentication is enabled for WS tests
	appCfg.Authentication.Enabled = true
	appCfg.Authentication.Method = "jwt"
	if appCfg.Authentication.JWT.JWTSecret == "" {
		appCfg.Authentication.JWT.JWTSecret = "test-ws-secret"
	}
	appCfg.Authentication.IgnoreTokenExpiration = true

	launcherCfg := &config.LauncherConfig{
		Host:             []string{"localhost:8080"},
		RetryIntervalSec: 5,
		MaxRetryCount:    3,
		WebSocket: config.WebSocketConfig{
			Enable:               true,
			SocketHost:           "ws://localhost:8080/v0/ws",
			HeartbeatIntervalSec: 30,
		},
		Security: config.SecurityConfig{
			EnableAuthentication:       true,
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
		},
	}

	langBundle := config.LanguageBundle{
		"en": config.LanguagePack{
			Errors: map[string]string{
				"InvalidRequest": "The request is invalid.",
				"Unauthorized":   "Authentication required.",
			},
		},
	}

	localizer := localization.New(appCfg.Language.Default, langBundle)
	memStore := store.NewMemory()
	authService := auth.NewService(appCfg, memStore)
	feedbackPath := filepath.Join(t.TempDir(), "feedback.log")
	updateDir := t.TempDir()
	testMaintPath := filepath.Join(t.TempDir(), "maintenance.json")
	testNewsPath := filepath.Join(t.TempDir(), "news.json")
	updatePath := filepath.Join(t.TempDir(), "updates.json")

	srv, err := New(appCfg, launcherCfg, maintenanceCfg, testMaintPath, nil, testNewsPath, nil, updatePath, updateDir, localizer, authService, memStore, feedbackPath)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv
}

func wsURL(ts *httptest.Server) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/v0/ws"
}

func TestWebSocketPingPong(t *testing.T) {
	srv := newWSTestServer(t)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	// Send ping
	msg := WSClientMessage{Action: "ping", Timestamp: time.Now().UTC().Unix()}
	if err := ws.WriteJSON(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read pong
	var resp WSServerMessage
	if err := ws.ReadJSON(&resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Action != "pong" {
		t.Errorf("expected pong, got %s", resp.Action)
	}
	if resp.Meta.APIVersion == "" {
		t.Error("expected meta.apiVersion in response")
	}
}

func TestWebSocketQuery(t *testing.T) {
	srv := newWSTestServer(t)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	// Send query
	msg := WSClientMessage{Action: "query", Timestamp: time.Now().UTC().Unix()}
	if err := ws.WriteJSON(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Should get pong (no maintenance active)
	var resp WSServerMessage
	if err := ws.ReadJSON(&resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Action != "pong" {
		t.Errorf("expected pong (no maintenance), got %s", resp.Action)
	}
}

func TestWebSocketBroadcast(t *testing.T) {
	srv := newWSTestServer(t)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	// Give the hub time to register client
	time.Sleep(50 * time.Millisecond)

	// Broadcast a notification
	srv.wsHub.Broadcast(&WSNotification{
		Type:    "update",
		Message: "New version available",
	})

	// Read the broadcast
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	var resp WSServerMessage
	if err := ws.ReadJSON(&resp); err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	if resp.Action != "notify" {
		t.Errorf("expected notify, got %s", resp.Action)
	}
	if resp.NotifyChanged == nil {
		t.Fatal("expected notifyChanged in response")
	}
	if resp.NotifyChanged.Type != "update" {
		t.Errorf("expected type update, got %s", resp.NotifyChanged.Type)
	}
	if resp.NotifyChanged.Message != "New version available" {
		t.Errorf("expected message, got %s", resp.NotifyChanged.Message)
	}
	if resp.MessageID == "" {
		t.Error("expected messageId in broadcast")
	}
}

func TestWebSocketAdminBroadcastEndpoint(t *testing.T) {
	srv := newWSTestServer(t)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Create admin user and get token
	_, err := srv.store.CreateUser(nil, "admin", "$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012", "admin@test.com", "admin")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	access, _, err := srv.authService.IssueTokens("user:1", "admin")
	if err != nil {
		t.Fatalf("issue tokens: %v", err)
	}

	// Connect WebSocket client
	ws, _, err := websocket.DefaultDialer.Dial(wsURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()
	time.Sleep(50 * time.Millisecond)

	// Call admin broadcast endpoint
	payload := `{"type":"maintenance","message":"Server going down"}`
	req, _ := http.NewRequest("POST", ts.URL+"/v0/api/admin/broadcast", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("broadcast request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Check that the WS client received the broadcast
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	var wsResp WSServerMessage
	if err := ws.ReadJSON(&wsResp); err != nil {
		t.Fatalf("read ws: %v", err)
	}
	if wsResp.Action != "notify" {
		t.Errorf("expected notify, got %s", wsResp.Action)
	}
	if wsResp.NotifyChanged.Type != "maintenance" {
		t.Errorf("expected maintenance, got %s", wsResp.NotifyChanged.Type)
	}
}

func TestWebSocketInvalidAction(t *testing.T) {
	srv := newWSTestServer(t)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL(ts), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	msg := WSClientMessage{Action: "invalid_action"}
	if err := ws.WriteJSON(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var resp WSServerMessage
	json.Unmarshal(raw, &resp)
	if len(resp.Errors) == 0 {
		t.Error("expected error response for invalid action")
	}
}

func TestWebSocketDisabledReturns404(t *testing.T) {
	// Use the standard test server which has WS disabled
	srv := newTestServer(t, nil)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Attempt WebSocket upgrade should fail
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(ts), nil)
	if err == nil {
		t.Fatal("expected error when WS disabled")
	}
	if resp != nil && resp.StatusCode != http.StatusNotFound {
		t.Logf("status: %d (expected failure)", resp.StatusCode)
	}
}
