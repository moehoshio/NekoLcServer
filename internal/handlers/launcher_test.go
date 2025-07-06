package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/models"
)

func createTestLauncherConfig() *config.Config {
	cfg := &config.Config{
		App: &config.AppConfig{},
		Launcher: &config.LauncherConfigData{
			Host:             []string{"localhost:8080"},
			RetryIntervalSec: 5,
			MaxRetryCount:    3,
			WebSocket: config.WebSocketConfig{
				Enable:               false,
				SocketHost:          "",
				HeartbeatIntervalSec: 30,
			},
			Security: config.SecurityConfig{
				EnableAuthentication:        false,
				TokenExpirationSec:         3600,
				RefreshTokenExpirationDays: 30,
				LoginUrl:                   "/v0/api/auth/login",
				LogoutUrl:                  "/v0/api/auth/logout",
				RefreshUrl:                 "/v0/api/auth/refresh",
			},
			FeaturesFlags: map[string]interface{}{
				"enableFeatureA": true,
				"enableFeatureB": false,
				"ui": map[string]interface{}{
					"enableDevHint": false,
				},
			},
		},
		Maintenance: &config.MaintenanceConfigData{
			MaintenanceActive: false,
			MaintenanceInfo: config.MaintenanceInfoConfig{
				Status:    "scheduled",
				Message:   "Scheduled maintenance",
				StartTime: "2024-06-01T12:00:00Z",
				ExEndTime: "2024-06-01T14:00:00Z",
				PosterUrl: "https://example.com/maintenance-poster.jpg",
				Link:      "https://example.com/maintenance-announcement",
			},
		},
		Languages: make(config.LanguageConfig),
	}
	cfg.App.Server.APIVersion = "1.0.0"
	cfg.App.Server.MinAPIVersion = "1.0.0"
	cfg.App.Server.BuildVersion = "test"
	cfg.App.Server.ReleaseDate = "2024-01-01T00:00:00Z"
	
	// Add basic English language support
	cfg.Languages["en"] = config.LanguageStrings{
		Errors: map[string]string{
			"InvalidRequest": "The request is invalid.",
			"NotFound":      "Resource not found.",
		},
		Maintenance: map[string]string{
			"scheduled": "Scheduled maintenance",
			"progress":  "Maintenance in progress",
		},
		Updates: map[string]string{
			"available":   "New version available",
			"description": "Bug fixes and improvements",
		},
	}
	
	return cfg
}

func TestLauncherHandler_LauncherConfig_Success(t *testing.T) {
	cfg := createTestLauncherConfig()
	db, cleanup := createTestDatabase()
	defer cleanup()
	
	handler := NewLauncherHandler(cfg, db)
	
	req := models.LauncherConfigRequest{
		LauncherConfigRequest: models.LauncherConfigRequestInfo{
			OS:   "windows",
			Arch: "x64",
		},
		Preferences: models.Preferences{
			Language: "en",
		},
	}
	
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/launcherConfig", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	handler.LauncherConfig(w, httpReq)
	
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
	
	var response models.LauncherConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to parse response: %v", err)
	}
	
	if len(response.LauncherConfig.Host) == 0 {
		t.Error("Expected host configuration")
	}
}

func TestLauncherHandler_LauncherConfig_MissingFields(t *testing.T) {
	cfg := createTestLauncherConfig()
	db, cleanup := createTestDatabase()
	defer cleanup()
	
	handler := NewLauncherHandler(cfg, db)
	
	req := models.LauncherConfigRequest{
		LauncherConfigRequest: models.LauncherConfigRequestInfo{
			// Missing OS and Arch
		},
	}
	
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/launcherConfig", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	handler.LauncherConfig(w, httpReq)
	
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestLauncherHandler_Maintenance_NotActive(t *testing.T) {
	cfg := createTestLauncherConfig()
	db, cleanup := createTestDatabase()
	defer cleanup()
	
	handler := NewLauncherHandler(cfg, db)
	
	req := models.MaintenanceRequest{
		CheckMaintenance: models.CheckMaintenanceInfo{
			OS:   "windows",
			Arch: "x64",
		},
	}
	
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/maintenance", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	handler.Maintenance(w, httpReq)
	
	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d", http.StatusNoContent, w.Code)
	}
}

func TestLauncherHandler_Maintenance_Active(t *testing.T) {
	cfg := createTestLauncherConfig()
	cfg.Maintenance.MaintenanceActive = true // Set maintenance as active
	db, cleanup := createTestDatabase()
	defer cleanup()
	
	handler := NewLauncherHandler(cfg, db)
	
	req := models.MaintenanceRequest{
		CheckMaintenance: models.CheckMaintenanceInfo{
			OS:   "windows",
			Arch: "x64",
		},
	}
	
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/maintenance", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	handler.Maintenance(w, httpReq)
	
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
	
	var response models.MaintenanceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Errorf("Failed to parse response: %v", err)
	}
	
	if response.MaintenanceInformation.Status == "" {
		t.Error("Expected maintenance status")
	}
}

func TestLauncherHandler_CheckUpdates_NoUpdates(t *testing.T) {
	cfg := createTestLauncherConfig()
	db, cleanup := createTestDatabase()
	defer cleanup()
	
	handler := NewLauncherHandler(cfg, db)
	
	req := models.CheckUpdateRequest{
		CheckUpdate: models.CheckUpdateInfo{
			OS:              "windows",
			Arch:            "x64",
			CoreVersion:     "1.0.0",
			ResourceVersion: "2.0.0",
		},
	}
	
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/checkUpdates", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	handler.CheckUpdates(w, httpReq)
	
	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d", http.StatusNoContent, w.Code)
	}
}

func TestLauncherHandler_CheckUpdates_MissingFields(t *testing.T) {
	cfg := createTestLauncherConfig()
	db, cleanup := createTestDatabase()
	defer cleanup()
	
	handler := NewLauncherHandler(cfg, db)
	
	req := models.CheckUpdateRequest{
		CheckUpdate: models.CheckUpdateInfo{
			OS:   "windows",
			Arch: "x64",
			// Missing CoreVersion and ResourceVersion
		},
	}
	
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/checkUpdates", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	handler.CheckUpdates(w, httpReq)
	
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestLauncherHandler_FeedbackLog_Success(t *testing.T) {
	cfg := createTestLauncherConfig()
	db, cleanup := createTestDatabase()
	defer cleanup()
	
	handler := NewLauncherHandler(cfg, db)
	
	req := models.FeedbackLogRequest{
		FeedbackLog: models.FeedbackLogInfo{
			OS:              "windows",
			Arch:            "x64",
			CoreVersion:     "1.0.0",
			ResourceVersion: "2.0.0",
			Timestamp:       1685625600,
			Content:         "Test feedback log",
		},
	}
	
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/feedbackLog", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	handler.FeedbackLog(w, httpReq)
	
	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d", http.StatusNoContent, w.Code)
	}
}

func TestLauncherHandler_FeedbackLog_MissingFields(t *testing.T) {
	cfg := createTestLauncherConfig()
	db, cleanup := createTestDatabase()
	defer cleanup()
	
	handler := NewLauncherHandler(cfg, db)
	
	req := models.FeedbackLogRequest{
		FeedbackLog: models.FeedbackLogInfo{
			OS:   "windows",
			Arch: "x64",
			// Missing other required fields
		},
	}
	
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/feedbackLog", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	
	handler.FeedbackLog(w, httpReq)
	
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}