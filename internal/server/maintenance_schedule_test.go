package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/moehoshio/NekoLcServer/internal/config"
)

func rfc(tm time.Time) string { return tm.UTC().Format(time.RFC3339) }

// TestScheduleStatusAt verifies the time-driven derivation of a window's status.
func TestScheduleStatusAt(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		start, end time.Time
		lead       int
		want       string
	}{
		{"in progress", now.Add(-30 * time.Minute), now.Add(30 * time.Minute), 60, config.MaintenanceStatusProgress},
		{"scheduled within lead", now.Add(30 * time.Minute), now.Add(90 * time.Minute), 60, config.MaintenanceStatusScheduled},
		{"too far out", now.Add(3 * time.Hour), now.Add(4 * time.Hour), 60, ""},
		{"finished", now.Add(-2 * time.Hour), now.Add(-1 * time.Hour), 60, ""},
		{"no end is still in progress", now.Add(-1 * time.Hour), time.Time{}, 60, config.MaintenanceStatusProgress},
	}
	for _, c := range cases {
		sc := config.MaintenanceSchedule{StartTime: rfc(c.start)}
		if !c.end.IsZero() {
			sc.EndTime = rfc(c.end)
		}
		if got := scheduleStatusAt(sc, c.lead, now); got != c.want {
			t.Fatalf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// TestActiveMaintenancePrioritisesProgress checks that an in-progress window is
// reported ahead of an upcoming one, and that the most-relevant helper agrees.
func TestActiveMaintenancePrioritisesProgress(t *testing.T) {
	srv := newTestServer(t, nil)
	now := time.Now().UTC()
	srv.setMaintenanceConfig(&config.MaintenanceConfig{
		ScheduledLeadMinutes: 60,
		Schedules: []config.MaintenanceSchedule{
			{ID: "up", StartTime: rfc(now.Add(30 * time.Minute)), EndTime: rfc(now.Add(90 * time.Minute)), Message: "upcoming"},
			{ID: "now", StartTime: rfc(now.Add(-10 * time.Minute)), EndTime: rfc(now.Add(50 * time.Minute)), Message: "ongoing"},
		},
	})
	list := srv.activeMaintenance(nil, now)
	if len(list) != 2 {
		t.Fatalf("expected 2 active windows, got %d", len(list))
	}
	if list[0].Status != config.MaintenanceStatusProgress || list[0].Message != "ongoing" {
		t.Fatalf("expected in-progress window first, got %+v", list[0])
	}
	if list[1].Status != config.MaintenanceStatusScheduled {
		t.Fatalf("expected scheduled window second, got %+v", list[1])
	}
	info, ok := srv.maintenanceForClient(nil)
	if !ok || info.Status != config.MaintenanceStatusProgress {
		t.Fatalf("expected most-relevant window to be in-progress, got %+v ok=%v", info, ok)
	}
}

// TestPlatformScopedScheduleFiltering verifies windows restricted to a platform
// only apply to that platform tuple.
func TestPlatformScopedScheduleFiltering(t *testing.T) {
	srv := newTestServer(t, nil)
	now := time.Now().UTC()
	srv.setMaintenanceConfig(&config.MaintenanceConfig{
		ScheduledLeadMinutes: 60,
		Schedules: []config.MaintenanceSchedule{
			{ID: "win", StartTime: rfc(now.Add(-10 * time.Minute)), EndTime: rfc(now.Add(50 * time.Minute)), Message: "win only", Platforms: []string{"windows-x64"}},
		},
	})
	winClient := &ClientInfo{System: &SystemInfo{OS: "windows", Arch: "x64"}}
	if list := srv.activeMaintenance(winClient, now); len(list) != 1 {
		t.Fatalf("expected windows client to see the window, got %d", len(list))
	}
	linuxClient := &ClientInfo{System: &SystemInfo{OS: "linux", Arch: "x64"}}
	if list := srv.activeMaintenance(linuxClient, now); len(list) != 0 {
		t.Fatalf("expected linux client to see no windows, got %d", len(list))
	}
	// An unknown platform should not match platform-scoped windows.
	if list := srv.activeMaintenance(nil, now); len(list) != 0 {
		t.Fatalf("expected unknown platform to see no scoped windows, got %d", len(list))
	}
}

// TestScheduledWindowReturnedButDoesNotBlockUpdates confirms an upcoming window is
// surfaced by the maintenance endpoint yet does not stop update delivery.
func TestScheduledWindowReturnedButDoesNotBlockUpdates(t *testing.T) {
	srv := newTestServer(t, nil)
	now := time.Now().UTC()
	srv.setMaintenanceConfig(&config.MaintenanceConfig{
		ScheduledLeadMinutes: 120,
		Schedules: []config.MaintenanceSchedule{
			{ID: "soon", StartTime: rfc(now.Add(30 * time.Minute)), EndTime: rfc(now.Add(90 * time.Minute)), Message: "soon"},
		},
	})

	// The maintenance endpoint reports the upcoming window as "scheduled".
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/maintenance", map[string]interface{}{
		"maintenanceRequest": map[string]interface{}{"timestamp": 1},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("maintenance endpoint expected 200, got %d", rec.Code)
	}
	var body MaintenanceResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode maintenance: %v", err)
	}
	if body.MaintenanceResponse.Status != config.MaintenanceStatusScheduled {
		t.Fatalf("expected scheduled status, got %q", body.MaintenanceResponse.Status)
	}

	// An upcoming window must not block the update check with a 503.
	rec = doRequest(t, srv, http.MethodPost, "/v0/api/checkUpdates", map[string]interface{}{
		"updateRequest": map[string]interface{}{
			"timestamp": 1,
			"clientInfo": map[string]interface{}{
				"app":    map[string]interface{}{"coreVersion": "0.0.1", "resourceVersion": "0.0.1"},
				"system": map[string]interface{}{"os": "windows", "arch": "x64"},
			},
		},
	})
	if rec.Code == http.StatusServiceUnavailable {
		t.Fatalf("scheduled (upcoming) window must not block updates, got 503")
	}
}

// TestInProgressWindowBlocksUpdates confirms an active window blocks the update
// check with a 503 maintenance response.
func TestInProgressWindowBlocksUpdates(t *testing.T) {
	srv := newTestServer(t, nil)
	now := time.Now().UTC()
	srv.setMaintenanceConfig(&config.MaintenanceConfig{
		ScheduledLeadMinutes: 60,
		Schedules: []config.MaintenanceSchedule{
			{ID: "now", StartTime: rfc(now.Add(-10 * time.Minute)), EndTime: rfc(now.Add(50 * time.Minute)), Message: "ongoing"},
		},
	})
	rec := doRequest(t, srv, http.MethodPost, "/v0/api/checkUpdates", map[string]interface{}{
		"updateRequest": map[string]interface{}{
			"timestamp": 1,
			"clientInfo": map[string]interface{}{
				"app":    map[string]interface{}{"coreVersion": "0.0.1", "resourceVersion": "0.0.1"},
				"system": map[string]interface{}{"os": "windows", "arch": "x64"},
			},
		},
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("in-progress window should block updates with 503, got %d", rec.Code)
	}
}

// TestScheduledLeadDefaultApplied verifies the default lead time is used when the
// configured value is not positive.
func TestScheduledLeadDefaultApplied(t *testing.T) {
	srv := newTestServer(t, nil)
	now := time.Now().UTC()
	srv.setMaintenanceConfig(&config.MaintenanceConfig{
		// ScheduledLeadMinutes left as 0 -> DefaultScheduledLeadMinutes (60).
		Schedules: []config.MaintenanceSchedule{
			{ID: "soon", StartTime: rfc(now.Add(45 * time.Minute)), EndTime: rfc(now.Add(90 * time.Minute)), Message: "soon"},
		},
	})
	list := srv.activeMaintenance(nil, now)
	if len(list) != 1 || list[0].Status != config.MaintenanceStatusScheduled {
		t.Fatalf("expected the window to be scheduled under the default lead, got %+v", list)
	}
}
