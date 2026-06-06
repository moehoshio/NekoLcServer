package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/moehoshio/NekoLcServer/internal/config"
)

// TestAdminHasUserDashboardPreview verifies the admin back-office exposes a
// "User Dashboard" section that embeds the live user dashboard for preview.
func TestAdminHasUserDashboardPreview(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {})
	rec := doRequest(t, srv, http.MethodGet, "/app/admin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin page expected 200 got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="nav-dashboard"`,
		`id="section-dashboard"`,
		`id="dashboard-preview-frame"`,
		`refreshDashboardPreview()`,
		`/app/dashboard`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin page missing %q", want)
		}
	}
}

// TestAdminMaintenanceUsesSchedules verifies the maintenance section ships the
// multi-window scheduler and lead-time control, and no longer offers a manual
// status dropdown.
func TestAdminMaintenanceUsesSchedules(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {})
	rec := doRequest(t, srv, http.MethodGet, "/app/admin", nil)
	body := rec.Body.String()
	for _, want := range []string{
		`id="schedules-list"`,
		`id="btn-add-schedule"`,
		`id="maint-lead"`,
		`addSchedule()`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin maintenance section missing %q", want)
		}
	}
	if strings.Contains(body, `id="maint-status"`) {
		t.Fatalf("admin maintenance section should no longer have a manual status dropdown")
	}
}

// TestHomePageHasThemeSwitcher verifies the public home page offers a theme
// switcher with icons and language icons.
func TestHomePageHasThemeSwitcher(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {})
	rec := doRequest(t, srv, http.MethodGet, "/app", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("home expected 200 got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="themeSelect"`,
		`changeTheme()`,
		`data-theme`,
		`id="opt-theme-auto"`,
		`class="ctrl-icon"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("home page missing %q", want)
		}
	}
}

// TestMaintenanceConfigRoundTripWithSchedules verifies the admin update endpoint
// accepts and persists schedules and the lead-time setting.
func TestMaintenanceConfigRoundTripWithSchedules(t *testing.T) {
	srv := newTestServer(t, func(cfg *config.AppConfig) {
		cfg.Authentication.Enabled = true
		cfg.Authentication.Method = "mysql"
	})
	token := loginAsAdmin(t, srv)

	rec := doAuthRequest(t, srv, http.MethodPut, "/v0/api/admin/maintenance", map[string]interface{}{
		"maintenance": map[string]interface{}{
			"maintenanceActive":    false,
			"scheduledLeadMinutes": 90,
			"schedules": []map[string]interface{}{
				{
					"id":        "sch-1",
					"startTime": "2026-06-06T12:00:00Z",
					"exEndTime": "2026-06-06T14:00:00Z",
					"message":   "Planned upgrade",
					"platforms": []string{"windows-x64"},
				},
			},
		},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("update maintenance expected 200 got %d: %s", rec.Code, rec.Body.String())
	}

	cfg := srv.currentMaintenanceConfig()
	if cfg.ScheduledLeadMinutes != 90 {
		t.Fatalf("expected lead 90, got %d", cfg.ScheduledLeadMinutes)
	}
	if len(cfg.Schedules) != 1 || cfg.Schedules[0].Message != "Planned upgrade" {
		t.Fatalf("expected one persisted schedule, got %+v", cfg.Schedules)
	}
	if len(cfg.Schedules[0].Platforms) != 1 || cfg.Schedules[0].Platforms[0] != "windows-x64" {
		t.Fatalf("expected platform scoping persisted, got %+v", cfg.Schedules[0].Platforms)
	}
}
