package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/middleware"
	"github.com/moehoshio/NekoLcServer/internal/models"
)

type LauncherHandler struct {
	Config *config.Config
}

func NewLauncherHandler(cfg *config.Config) *LauncherHandler {
	return &LauncherHandler{Config: cfg}
}

// LauncherConfig handles POST /v0/api/launcherConfig
func (h *LauncherHandler) LauncherConfig(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	var req models.LauncherConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Validate required fields
	if req.LauncherConfigRequest.OS == "" || req.LauncherConfigRequest.Arch == "" {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "OS and architecture are required")
		return
	}
	
	// Build launcher configuration
	launcherConfig := models.LauncherConfig{
		Host:             []string{"localhost:8080"},
		RetryIntervalSec: 5,
		MaxRetryCount:    3,
		WebSocket: models.WebSocket{
			Enable:                false, // Disabled by default for simplicity
			SocketHost:           "",
			HeartbeatIntervalSec: 30,
		},
		Security: models.Security{
			EnableAuthentication:        h.Config.EnableAuthentication,
			TokenExpirationSec:         h.Config.TokenExpirationSec,
			RefreshTokenExpirationDays: h.Config.RefreshTokenExpirationDays,
			LoginUrl:                   "/v0/api/auth/login",
			LogoutUrl:                  "/v0/api/auth/logout",
			RefreshUrl:                 "/v0/api/auth/refresh",
		},
		FeaturesFlags: map[string]interface{}{
			"ui": map[string]interface{}{
				"enableDevHint": h.Config.EnableDebugMode,
			},
			"enableFeatureA": true,
			"enableFeatureB": false,
		},
	}
	
	response := models.LauncherConfigResponse{
		LauncherConfig: launcherConfig,
		Meta:           models.NewMeta(h.Config.APIVersion, h.Config.MinAPIVersion, h.Config.BuildVersion, h.Config.ReleaseDate),
	}
	
	rw.WriteJSON(http.StatusOK, response)
}

// Maintenance handles POST /v0/api/maintenance
func (h *LauncherHandler) Maintenance(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	var req models.MaintenanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Check if maintenance is active (simplified - always not in maintenance for demo)
	inMaintenance := false
	
	if !inMaintenance {
		// Return 204 No Content if not in maintenance
		rw.WriteNoContent()
		return
	}
	
	// If in maintenance, return maintenance information
	response := models.MaintenanceResponse{
		MaintenanceInformation: models.MaintenanceInformation{
			Status:    "scheduled",
			Message:   "Scheduled maintenance",
			StartTime: "2024-06-01T12:00:00Z",
			ExEndTime: "2024-06-01T14:00:00Z",
			PosterUrl: "https://example.com/maintenance-poster.jpg",
			Link:      "https://example.com/maintenance-announcement",
		},
		Meta: models.NewMeta(h.Config.APIVersion, h.Config.MinAPIVersion, h.Config.BuildVersion, h.Config.ReleaseDate),
	}
	
	rw.WriteJSON(http.StatusOK, response)
}

// CheckUpdates handles POST /v0/api/checkUpdates
func (h *LauncherHandler) CheckUpdates(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	var req models.CheckUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Validate required fields
	if req.CheckUpdate.OS == "" || req.CheckUpdate.Arch == "" || 
	   req.CheckUpdate.CoreVersion == "" || req.CheckUpdate.ResourceVersion == "" {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "OS, architecture, coreVersion, and resourceVersion are required")
		return
	}
	
	// Check for updates (simplified - always no updates for demo)
	hasUpdates := false
	
	if !hasUpdates {
		// Return 204 No Content if no updates
		rw.WriteNoContent()
		return
	}
	
	// If updates available, return update information
	response := models.UpdateResponse{
		UpdateInformation: models.UpdateInformation{
			Title:           "New Version Available",
			Description:     "Bug fixes and improvements",
			PosterUrl:       "https://example.com/update-poster.jpg",
			PublishTime:     "2024-06-01T12:00:00Z",
			ResourceVersion: "2.0.1",
			IsMandatory:     false,
			Files: []models.FileInfo{
				{
					URL:      "https://example.com/download/main.exe",
					FileName: "main.exe",
					Checksum: "abcdef1234567890",
					DownloadMeta: models.DownloadMeta{
						HashAlgorithm:      "sha256",
						SuggestMultiThread: false,
						IsCoreFile:         true,
						IsAbsoluteUrl:      true,
					},
				},
			},
		},
		Meta: models.NewMeta(h.Config.APIVersion, h.Config.MinAPIVersion, h.Config.BuildVersion, h.Config.ReleaseDate),
	}
	
	rw.WriteJSON(http.StatusOK, response)
}

// FeedbackLog handles POST /v0/api/feedbackLog
func (h *LauncherHandler) FeedbackLog(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	var req models.FeedbackLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Validate required fields
	if req.FeedbackLog.OS == "" || req.FeedbackLog.Arch == "" || 
	   req.FeedbackLog.CoreVersion == "" || req.FeedbackLog.ResourceVersion == "" ||
	   req.FeedbackLog.Content == "" {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "All feedback log fields are required")
		return
	}
	
	// Validate versions exist (simplified - always valid for demo)
	versionsValid := true
	if !versionsValid {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid core or resource version")
		return
	}
	
	// Process feedback log (in a real implementation, you would store it)
	// For now, just return success
	rw.WriteNoContent()
}