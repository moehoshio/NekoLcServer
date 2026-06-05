package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/moehoshio/NekoLcServer/internal/config"
)

// TestRegisterBlockedWhenRegistrationDisabled verifies that registration is
// rejected when account.allowRegistration is false.
func TestRegisterBlockedWhenRegistrationDisabled(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	srv.setAccountConfig(&config.AccountConfig{AllowRegistration: false})

	rec := doRequest(t, srv, http.MethodPost, "/app/register", map[string]interface{}{
		"registerRequest": map[string]interface{}{"username": "blocked", "password": "secret123"},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when registration disabled, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAccountConfigDefaultsAllowRegistration verifies the custom unmarshaler
// defaults allowRegistration to true when the field is absent.
func TestAccountConfigDefaultsAllowRegistration(t *testing.T) {
	var cfg config.AccountConfig
	if err := json.Unmarshal([]byte(`{"requireEmail":true}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !cfg.AllowRegistration {
		t.Fatalf("expected AllowRegistration to default to true when absent")
	}
	if err := json.Unmarshal([]byte(`{"allowRegistration":false}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.AllowRegistration {
		t.Fatalf("expected AllowRegistration to be false when explicitly set")
	}
}

// TestUserPagesDisabledWhenAccountModeOff verifies the user-facing pages return
// the "feature not enabled" notice when authentication is disabled.
func TestUserPagesDisabledWhenAccountModeOff(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = false
	})
	for _, path := range []string{"/app/login", "/app/register", "/app/dashboard"} {
		rec := doRequest(t, srv, http.MethodGet, path, nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: expected 503 when account mode off, got %d", path, rec.Code)
		}
		if body := rec.Body.String(); !strings.Contains(body, "Account feature disabled") {
			t.Fatalf("%s: expected disabled notice page, got: %s", path, body)
		}
	}
}

// TestAdminSiteConfigRoundTrip verifies the admin site config can be saved and
// the public endpoint reflects it.
func TestAdminSiteConfigRoundTrip(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	token := loginAsAdmin(t, srv)

	rec := doAuthRequest(t, srv, http.MethodPut, "/v0/api/admin/site", map[string]interface{}{
		"site": map[string]interface{}{
			"siteName":       "My Site",
			"seoDescription": "desc here",
			"announcement":   "hello world",
		},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("save site expected 200 got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodGet, "/app/api/site-config", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("public site-config expected 200 got %d", rec.Code)
	}
	var resp AppSiteConfigResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SiteName != "My Site" || resp.Announcement != "hello world" || resp.SEODescription != "desc here" {
		t.Fatalf("unexpected site config: %+v", resp)
	}

	// The public home page should render the site name and announcement.
	rec = doRequest(t, srv, http.MethodGet, "/app", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("home expected 200 got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "My Site") || !strings.Contains(body, "hello world") {
		t.Fatalf("home page missing site name/announcement")
	}
}
