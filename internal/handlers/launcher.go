package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/middleware"
	"github.com/moehoshio/NekoLcServer/internal/models"
	"github.com/moehoshio/NekoLcServer/internal/storage"
)

type LauncherHandler struct {
	Config *config.Config
	DB     *storage.Database
}

func NewLauncherHandler(cfg *config.Config, db *storage.Database) *LauncherHandler {
	return &LauncherHandler{
		Config: cfg,
		DB:     db,
	}
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
	
	// Build launcher configuration from config files
	launcherConfig := models.LauncherConfig{
		Host:             h.Config.Launcher.Host,
		RetryIntervalSec: h.Config.Launcher.RetryIntervalSec,
		MaxRetryCount:    h.Config.Launcher.MaxRetryCount,
		WebSocket: models.WebSocket{
			Enable:                h.Config.Launcher.WebSocket.Enable,
			SocketHost:           h.Config.Launcher.WebSocket.SocketHost,
			HeartbeatIntervalSec: h.Config.Launcher.WebSocket.HeartbeatIntervalSec,
		},
		Security: models.Security{
			EnableAuthentication:        h.Config.Launcher.Security.EnableAuthentication,
			TokenExpirationSec:         h.Config.Launcher.Security.TokenExpirationSec,
			RefreshTokenExpirationDays: h.Config.Launcher.Security.RefreshTokenExpirationDays,
			LoginUrl:                   h.Config.Launcher.Security.LoginUrl,
			LogoutUrl:                  h.Config.Launcher.Security.LogoutUrl,
			RefreshUrl:                 h.Config.Launcher.Security.RefreshUrl,
		},
		FeaturesFlags: h.Config.Launcher.FeaturesFlags,
	}
	
	response := models.LauncherConfigResponse{
		LauncherConfig: launcherConfig,
		Meta:           models.NewMeta(h.Config.App.Server.APIVersion, h.Config.App.Server.MinAPIVersion, h.Config.App.Server.BuildVersion, h.Config.App.Server.ReleaseDate),
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
	
	// Get preferred language for localized messages
	language := "en"
	if req.Preferences.Language != "" {
		language = req.Preferences.Language
	}
	
	// Check if maintenance is active from config
	if !h.Config.Maintenance.MaintenanceActive {
		// Return 204 No Content if not in maintenance
		rw.WriteNoContent()
		return
	}
	
	// Get localized maintenance message
	localizedMessage := h.Config.GetLocalizedString(language, "maintenance", h.Config.Maintenance.MaintenanceInfo.Status)
	if localizedMessage == h.Config.Maintenance.MaintenanceInfo.Status {
		// Fallback to config message if no localization found
		localizedMessage = h.Config.Maintenance.MaintenanceInfo.Message
	}
	
	// Return maintenance information from config
	response := models.MaintenanceResponse{
		MaintenanceInformation: models.MaintenanceInformation{
			Status:    h.Config.Maintenance.MaintenanceInfo.Status,
			Message:   localizedMessage,
			StartTime: h.Config.Maintenance.MaintenanceInfo.StartTime,
			ExEndTime: h.Config.Maintenance.MaintenanceInfo.ExEndTime,
			PosterUrl: h.Config.Maintenance.MaintenanceInfo.PosterUrl,
			Link:      h.Config.Maintenance.MaintenanceInfo.Link,
		},
		Meta: models.NewMeta(h.Config.App.Server.APIVersion, h.Config.App.Server.MinAPIVersion, h.Config.App.Server.BuildVersion, h.Config.App.Server.ReleaseDate),
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
	
	// Get preferred language for localized messages
	language := "en"
	if req.Preferences.Language != "" {
		language = req.Preferences.Language
	}
	
	// Check for updates (simplified - always no updates for this example)
	// In a real implementation, you would check version comparisons and update availability
	hasUpdates := false
	
	if !hasUpdates {
		// Return 204 No Content if no updates
		rw.WriteNoContent()
		return
	}
	
	// Get localized update messages
	localizedTitle := h.Config.GetLocalizedString(language, "updates", "available")
	localizedDescription := h.Config.GetLocalizedString(language, "updates", "description")
	
	// If updates available, return update information
	response := models.UpdateResponse{
		UpdateInformation: models.UpdateInformation{
			Title:           localizedTitle,
			Description:     localizedDescription,
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
		Meta: models.NewMeta(h.Config.App.Server.APIVersion, h.Config.App.Server.MinAPIVersion, h.Config.App.Server.BuildVersion, h.Config.App.Server.ReleaseDate),
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
	
	// Store feedback log in database
	feedbackLog := &storage.FeedbackLog{
		OS:              req.FeedbackLog.OS,
		Arch:            req.FeedbackLog.Arch,
		CoreVersion:     req.FeedbackLog.CoreVersion,
		ResourceVersion: req.FeedbackLog.ResourceVersion,
		Timestamp:       req.FeedbackLog.Timestamp,
		Content:         req.FeedbackLog.Content,
	}
	
	if err := h.DB.StoreFeedbackLog(feedbackLog); err != nil {
		rw.WriteError(http.StatusInternalServerError, "InternalError", "Failed to store feedback log")
		return
	}
	
	// Return 204 No Content for success
	rw.WriteNoContent()
}