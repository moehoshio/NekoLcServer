package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/middleware"
	"github.com/moehoshio/NekoLcServer/internal/models"
	"github.com/moehoshio/NekoLcServer/internal/storage"
)

type LauncherHandler struct {
	Config *config.Config
	DB     storage.Storage
}

func NewLauncherHandler(cfg *config.Config, db storage.Storage) *LauncherHandler {
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
	
	// Check for platform-specific maintenance first if OS and Arch are provided
	var maintenanceActive bool
	var maintenanceInfo config.MaintenanceInfoConfig
	
	// Try to get OS and Arch from the request (you might need to add these fields to MaintenanceRequest)
	// For now, let's use a platform key if available in the config
	// platformKey := fmt.Sprintf("%s-%s", os, arch) // Would need OS/Arch in request
	
	// Check platform-specific maintenance if available
	platformSpecificChecked := false
	for _, platformMaintenance := range h.Config.Maintenance.PlatformSpecific {
		// For demonstration, we'll check all platform-specific configs
		// In practice, you'd want to match against the client's OS/Arch
		if platformMaintenance.MaintenanceActive {
			maintenanceActive = true
			maintenanceInfo = platformMaintenance.MaintenanceInfo
			platformSpecificChecked = true
			break
		}
	}
	
	// Fall back to global maintenance if no platform-specific maintenance
	if !platformSpecificChecked {
		maintenanceActive = h.Config.Maintenance.MaintenanceActive
		maintenanceInfo = h.Config.Maintenance.MaintenanceInfo
	}
	
	// Return 204 No Content if not in maintenance
	if !maintenanceActive {
		rw.WriteNoContent()
		return
	}
	
	// Get localized maintenance message
	localizedMessage := h.Config.GetLocalizedString(language, "maintenance", maintenanceInfo.Status)
	if localizedMessage == maintenanceInfo.Status {
		// Fallback to config message if no localization found
		localizedMessage = maintenanceInfo.Message
	}
	
	// Return maintenance information from config
	response := models.MaintenanceResponse{
		MaintenanceInformation: models.MaintenanceInformation{
			Status:    maintenanceInfo.Status,
			Message:   localizedMessage,
			StartTime: maintenanceInfo.StartTime,
			ExEndTime: maintenanceInfo.ExEndTime,
			PosterUrl: maintenanceInfo.PosterUrl,
			Link:      maintenanceInfo.Link,
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
	
	// Check if either core version or resource version is outdated
	coreOutdated := req.CheckUpdate.CoreVersion != h.Config.Updates.LatestCoreVersion
	resourceOutdated := req.CheckUpdate.ResourceVersion != h.Config.Updates.LatestResourceVersion
	
	if !coreOutdated && !resourceOutdated {
		// Return 204 No Content if no updates needed
		rw.WriteNoContent()
		return
	}
	
	// Create platform key for OS-arch specific lookup
	platformKey := fmt.Sprintf("%s-%s", req.CheckUpdate.OS, req.CheckUpdate.Arch)
	
	// Look for incremental update path
	var updateFiles []models.FileInfo
	var incrementalUpdate *config.UpdateFileInfo
	
	// Check for incremental update for the core version
	if coreOutdated {
		for _, file := range h.Config.Updates.Files {
			if file.OS == req.CheckUpdate.OS && 
			   file.Arch == req.CheckUpdate.Arch && 
			   file.CoreVersion == req.CheckUpdate.CoreVersion {
				incrementalUpdate = &file
				break
			}
		}
	}
	
	// If incremental update available, use it
	if incrementalUpdate != nil {
		updateFiles = []models.FileInfo{
			{
				URL:      fmt.Sprintf("https://example.com/updates/%s", incrementalUpdate.CoreVersionPath),
				FileName: "update.json",
				Checksum: "incremental-update-checksum",
				DownloadMeta: models.DownloadMeta{
					HashAlgorithm:      "sha256",
					SuggestMultiThread: false,
					IsCoreFile:         true,
					IsAbsoluteUrl:      true,
				},
			},
		}
	} else {
		// No incremental update available, check for full package
		if fullPackage, exists := h.Config.Updates.FullPackages[platformKey]; exists {
			updateFiles = []models.FileInfo{
				{
					URL:      fullPackage.DownloadUrl,
					FileName: fmt.Sprintf("%s-full-update.zip", platformKey),
					Checksum: fullPackage.Checksum,
					DownloadMeta: models.DownloadMeta{
						HashAlgorithm:      "sha256",
						SuggestMultiThread: true,
						IsCoreFile:         true,
						IsAbsoluteUrl:      true,
					},
				},
			}
		} else {
			// No update available for this platform
			rw.WriteNoContent()
			return
		}
	}
	
	// Get localized update messages
	localizedTitle := h.Config.GetLocalizedString(language, "updates", "available")
	localizedDescription := h.Config.GetLocalizedString(language, "updates", "description")
	
	// Return update information
	response := models.UpdateResponse{
		UpdateInformation: models.UpdateInformation{
			Title:           localizedTitle,
			Description:     localizedDescription,
			PosterUrl:       "https://example.com/update-poster.jpg",
			PublishTime:     "2024-06-01T12:00:00Z",
			ResourceVersion: h.Config.Updates.LatestResourceVersion,
			IsMandatory:     false,
			Files:          updateFiles,
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