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

func TestLauncherHandler_LauncherConfig(t *testing.T) {
	cfg := &config.Config{
		APIVersion:           "1.0.0",
		MinAPIVersion:        "1.0.0",
		BuildVersion:         "test",
		ReleaseDate:          "2024-01-01T00:00:00Z",
		EnableAuthentication: false,
		TokenExpirationSec:   3600,
		RefreshTokenExpirationDays: 30,
	}
	
	handler := NewLauncherHandler(cfg)
	
	req := models.LauncherConfigRequest{
		LauncherConfigRequest: models.LauncherConfigRequestInfo{
			OS:   "windows",
			Arch: "x64",
		},
		Preferences: models.Preferences{
			Language: "en",
		},
	}
	
	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/launcherConfig", bytes.NewBuffer(jsonData))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.LauncherConfig(rr, httpReq)
	
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}
	
	var response models.LauncherConfigResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	
	if response.LauncherConfig.RetryIntervalSec != 5 {
		t.Errorf("Expected retry interval 5, got %d", response.LauncherConfig.RetryIntervalSec)
	}
	
	if response.LauncherConfig.Security.EnableAuthentication != false {
		t.Errorf("Expected authentication disabled, got %v", response.LauncherConfig.Security.EnableAuthentication)
	}
}

func TestLauncherHandler_LauncherConfig_MissingOS(t *testing.T) {
	cfg := &config.Config{
		APIVersion:    "1.0.0",
		MinAPIVersion: "1.0.0",
		BuildVersion:  "test",
		ReleaseDate:   "2024-01-01T00:00:00Z",
	}
	
	handler := NewLauncherHandler(cfg)
	
	req := models.LauncherConfigRequest{
		LauncherConfigRequest: models.LauncherConfigRequestInfo{
			Arch: "x64", // Missing OS
		},
	}
	
	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/launcherConfig", bytes.NewBuffer(jsonData))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.LauncherConfig(rr, httpReq)
	
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestLauncherHandler_Maintenance_NotInMaintenance(t *testing.T) {
	cfg := &config.Config{
		APIVersion:    "1.0.0",
		MinAPIVersion: "1.0.0",
		BuildVersion:  "test",
		ReleaseDate:   "2024-01-01T00:00:00Z",
	}
	
	handler := NewLauncherHandler(cfg)
	
	req := models.MaintenanceRequest{
		CheckMaintenance: models.CheckMaintenanceInfo{
			OS:   "windows",
			Arch: "x64",
		},
	}
	
	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/maintenance", bytes.NewBuffer(jsonData))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.Maintenance(rr, httpReq)
	
	if rr.Code != http.StatusNoContent {
		t.Errorf("Expected status code %d, got %d", http.StatusNoContent, rr.Code)
	}
}

func TestLauncherHandler_CheckUpdates_NoUpdates(t *testing.T) {
	cfg := &config.Config{
		APIVersion:    "1.0.0",
		MinAPIVersion: "1.0.0",
		BuildVersion:  "test",
		ReleaseDate:   "2024-01-01T00:00:00Z",
	}
	
	handler := NewLauncherHandler(cfg)
	
	req := models.CheckUpdateRequest{
		CheckUpdate: models.CheckUpdateInfo{
			OS:              "windows",
			Arch:            "x64",
			CoreVersion:     "1.0.0",
			ResourceVersion: "2.0.0",
		},
	}
	
	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/checkUpdates", bytes.NewBuffer(jsonData))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.CheckUpdates(rr, httpReq)
	
	if rr.Code != http.StatusNoContent {
		t.Errorf("Expected status code %d, got %d", http.StatusNoContent, rr.Code)
	}
}

func TestLauncherHandler_FeedbackLog_Success(t *testing.T) {
	cfg := &config.Config{
		APIVersion:    "1.0.0",
		MinAPIVersion: "1.0.0",
		BuildVersion:  "test",
		ReleaseDate:   "2024-01-01T00:00:00Z",
	}
	
	handler := NewLauncherHandler(cfg)
	
	req := models.FeedbackLogRequest{
		FeedbackLog: models.FeedbackLogInfo{
			OS:              "windows",
			Arch:            "x64",
			CoreVersion:     "1.0.0",
			ResourceVersion: "2.0.0",
			Timestamp:       1685625600,
			Content:         "Test feedback",
		},
	}
	
	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/feedbackLog", bytes.NewBuffer(jsonData))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.FeedbackLog(rr, httpReq)
	
	if rr.Code != http.StatusNoContent {
		t.Errorf("Expected status code %d, got %d", http.StatusNoContent, rr.Code)
	}
}