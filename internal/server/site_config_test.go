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

// TestSiteConfigShowToggleDefaults verifies the custom unmarshaler defaults the
// ShowNews and ShowMaintenance dashboard toggles to true when absent, while still
// honoring an explicit false.
func TestSiteConfigShowToggleDefaults(t *testing.T) {
	var cfg config.SiteConfig
	if err := json.Unmarshal([]byte(`{"siteName":"x"}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !cfg.ShowNews || !cfg.ShowMaintenance {
		t.Fatalf("expected ShowNews/ShowMaintenance to default to true, got %+v", cfg)
	}
	if err := json.Unmarshal([]byte(`{"showNews":false,"showMaintenance":false}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.ShowNews || cfg.ShowMaintenance {
		t.Fatalf("expected explicit false to be honored, got %+v", cfg)
	}
}

// TestDashboardSectionToggles verifies the home-content API omits news and the
// maintenance notice when the corresponding site config toggles are disabled.
func TestDashboardSectionToggles(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	srv.setNewsItems([]config.NewsItem{{ID: "n1", Title: "Hello"}})
	srv.setMaintenanceConfig(&config.MaintenanceConfig{
		MaintenanceActive: true,
		MaintenanceInfo:   config.MaintenanceInfo{Status: "progress", Message: "down"},
	})

	// Disabled: neither section should be present.
	srv.setSiteConfig(&config.SiteConfig{ShowNews: false, ShowMaintenance: false})
	rec := doRequest(t, srv, http.MethodGet, "/app/api/home-content", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("home-content expected 200 got %d", rec.Code)
	}
	var resp HomeContentResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.News) != 0 {
		t.Fatalf("expected no news when showNews is off, got %d", len(resp.News))
	}
	if resp.Maintenance != nil {
		t.Fatalf("expected no maintenance when showMaintenance is off")
	}

	// Enabled: both sections should be present.
	srv.setSiteConfig(&config.SiteConfig{ShowNews: true, ShowMaintenance: true})
	rec = doRequest(t, srv, http.MethodGet, "/app/api/home-content", nil)
	resp = HomeContentResponse{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.News) != 1 {
		t.Fatalf("expected 1 news item when showNews is on, got %d", len(resp.News))
	}
	if resp.Maintenance == nil || !resp.Maintenance.Active {
		t.Fatalf("expected active maintenance notice when showMaintenance is on")
	}
}

// TestNewsPermalinkPage verifies the public per-news page renders an existing
// item and returns 404 for an unknown id.
func TestNewsPermalinkPage(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {})
	srv.setNewsItems([]config.NewsItem{{ID: "n1", Title: "Patch Notes", Content: "Body text"}})

	rec := doRequest(t, srv, http.MethodGet, "/app/news?id=n1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("news page expected 200 got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Patch Notes") || !strings.Contains(body, "Body text") {
		t.Fatalf("news page missing title/content: %s", body)
	}

	rec = doRequest(t, srv, http.MethodGet, "/app/news?id=missing", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown news id expected 404 got %d", rec.Code)
	}
}

// TestHomePageHasNoAdminLink verifies the public home page no longer exposes a
// default admin dashboard entry (admin access is gated by role at login).
func TestHomePageHasNoAdminLink(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {})
	rec := doRequest(t, srv, http.MethodGet, "/app", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("home expected 200 got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `id="link-admin"`) || strings.Contains(body, "/app/admin") {
		t.Fatalf("home page should not contain the admin dashboard entry")
	}
}

// TestUserDashboardAdminLinkRoleGated verifies the user dashboard ships a hidden
// admin entry that is revealed by JS only for admin-role users.
func TestUserDashboardAdminLinkRoleGated(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	rec := doRequest(t, srv, http.MethodGet, "/app/dashboard", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard expected 200 got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="btn-secondary hidden" id="link-admin"`) {
		t.Fatalf("dashboard admin link should be present but hidden by default")
	}
	if !strings.Contains(body, "/app/admin") {
		t.Fatalf("dashboard should link to the admin panel for admins")
	}
}

// TestAdminDashboardStructure verifies the admin page reflects the reorganized
// sections, theme switcher and removal of the stats auto-refresh control.
func TestAdminDashboardStructure(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {})
	rec := doRequest(t, srv, http.MethodGet, "/app/admin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin page expected 200 got %d", rec.Code)
	}
	body := rec.Body.String()

	mustContain := []string{
		`id="themeSelect"`,       // theme switcher present
		`changeTheme()`,          // theme JS wired
		`data-theme`,             // theme application
		`prefers-color-scheme`,   // auto system theme
		`id="nav-site"`,          // site config nav
		`id="acct-policy-title"`, // account policy moved into Users section
		`id="maint-title"`,       // maintenance i18n wired
		`id="updates-config-title"`,
		`id="upload-title"`,
	}
	for _, s := range mustContain {
		if !strings.Contains(body, s) {
			t.Fatalf("admin page missing %q", s)
		}
	}

	mustNotContain := []string{
		`id="stats-autorefresh"`, // auto-refresh control removed
		`Email &amp; Home`,       // nav renamed to just Email
	}
	for _, s := range mustNotContain {
		if strings.Contains(body, s) {
			t.Fatalf("admin page should not contain %q", s)
		}
	}

	// The home-content editor must now live inside the Site Config section,
	// not the Email section.
	siteIdx := strings.Index(body, `id="section-site"`)
	settingsIdx := strings.Index(body, `id="section-settings"`)
	homeIdx := strings.Index(body, `id="home-content"`)
	if siteIdx < 0 || settingsIdx < 0 || homeIdx < 0 {
		t.Fatalf("expected site/settings/home markers present")
	}
	if !(homeIdx > siteIdx && homeIdx < settingsIdx) {
		t.Fatalf("home-content editor is not inside the Site Config section")
	}
}
