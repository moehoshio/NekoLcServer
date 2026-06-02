package server

import (
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/store"
)

// Preferences represents optional preference information that can accompany requests.
type Preferences struct {
	Language string `json:"language"`
}

// ClientInfo captures metadata about the requesting launcher.
type ClientInfo struct {
	App      *AppInfo               `json:"app"`
	System   *SystemInfo            `json:"system"`
	Extra    map[string]interface{} `json:"extra"`
	DeviceID string                 `json:"deviceId"`
}

// AppInfo contains version details for the launcher core/resources.
type AppInfo struct {
	AppName         string `json:"appName"`
	CoreVersion     string `json:"coreVersion"`
	ResourceVersion string `json:"resourceVersion"`
	BuildID         string `json:"buildId"`
}

// SystemInfo reflects the requesting operating system.
type SystemInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	OSVersion string `json:"osVersion"`
}

// LoginPayload models the login request body.
type LoginPayload struct {
	LoginRequest LoginRequest `json:"loginRequest"`
	Preferences  *Preferences `json:"preferences"`
}

// LoginRequest supports both username/password and identifier signature flows.
type LoginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	Identifier string `json:"identifier"`
	Timestamp  int64  `json:"timestamp"`
	Signature  string `json:"signature"`
}

// RefreshPayload models the refresh token request body.
type RefreshPayload struct {
	RefreshRequest struct {
		RefreshToken string `json:"refreshToken"`
	} `json:"refreshRequest"`
	Preferences *Preferences `json:"preferences"`
}

// ValidatePayload models the validate request body.
type ValidatePayload struct {
	ValidateRequest struct {
		AccessToken string `json:"accessToken"`
	} `json:"validateRequest"`
	Preferences *Preferences `json:"preferences"`
}

// LogoutPayload models the logout request body.
type LogoutPayload struct {
	LogoutRequest struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	} `json:"logoutRequest"`
	Preferences *Preferences `json:"preferences"`
}

// LauncherConfigPayload models the launcher config request.
type LauncherConfigPayload struct {
	LauncherConfigRequest struct {
		ClientInfo *ClientInfo `json:"clientInfo"`
		Timestamp  int64       `json:"timestamp"`
	} `json:"launcherConfigRequest"`
	Preferences *Preferences `json:"preferences"`
}

// MaintenancePayload models the maintenance request body.
type MaintenancePayload struct {
	MaintenanceRequest struct {
		ClientInfo *ClientInfo `json:"clientInfo"`
		Timestamp  int64       `json:"timestamp"`
	} `json:"maintenanceRequest"`
	Preferences *Preferences `json:"preferences"`
}

// UpdatePayload models the update check request body.
type UpdatePayload struct {
	UpdateRequest struct {
		ClientInfo *ClientInfo `json:"clientInfo"`
		Timestamp  int64       `json:"timestamp"`
	} `json:"updateRequest"`
	Preferences *Preferences `json:"preferences"`
}

// NewsPayload models the news request body.
type NewsPayload struct {
	NewsRequest struct {
		ClientInfo *ClientInfo `json:"clientInfo"`
		Timestamp  int64       `json:"timestamp"`
		Limit      int         `json:"limit"`
		Categories []string    `json:"categories"`
		LastID     string      `json:"lastId"`
	} `json:"newsRequest"`
	Preferences *Preferences `json:"preferences"`
}

// FeedbackLogPayload models the feedback log submission.
type FeedbackLogPayload struct {
	FeedbackLogRequest struct {
		ClientInfo *ClientInfo `json:"clientInfo"`
		Timestamp  int64       `json:"timestamp"`
		Content    string      `json:"content"`
	} `json:"feedbackLogRequest"`
	Preferences *Preferences `json:"preferences"`
}

// FeedbackLogsResponseBody lists feedback entries (admin only).
type FeedbackLogsResponseBody struct {
	FeedbackLogs []FeedbackLogItem `json:"feedbackLogs"`
	Count        int               `json:"count"`
	Meta         Meta              `json:"meta"`
}

// FeedbackLogItem represents a single feedback log entry.
type FeedbackLogItem struct {
	ID              int64       `json:"id"`
	UserID          int64       `json:"userId,omitempty"`
	DeviceID        string      `json:"deviceId,omitempty"`
	Lang            string      `json:"lang,omitempty"`
	ClientInfo      interface{} `json:"clientInfo,omitempty"`
	Content         string      `json:"content"`
	ReceivedAt      string      `json:"receivedAt"`
	Timestamp       int64       `json:"timestamp"`
	CoreVersion     string      `json:"coreVersion,omitempty"`
	ResourceVersion string      `json:"resourceVersion,omitempty"`
	BuildID         string      `json:"buildId,omitempty"`
	Platform        string      `json:"platform,omitempty"`
	Arch            string      `json:"arch,omitempty"`
	Region          string      `json:"region,omitempty"`
}

// FeedbackFilterOptionsResponse returns available filter options.
type FeedbackFilterOptionsResponse struct {
	Options store.FeedbackFilterOptions `json:"options"`
	Meta    Meta                        `json:"meta"`
}

// Meta defines the meta object returned with each response.
type Meta struct {
	APIVersion        string `json:"apiVersion"`
	MinAPIVersion     string `json:"minApiVersion"`
	BuildVersion      string `json:"buildVersion"`
	Timestamp         int64  `json:"timestamp"`
	ReleaseDate       string `json:"releaseDate"`
	IsDeprecated      bool   `json:"isDeprecated"`
	DeprecatedMessage string `json:"deprecatedMessage"`
}

// APIError follows the documented error shape.
type APIError struct {
	Error        string `json:"error"`
	ErrorType    string `json:"errorType"`
	ErrorMessage string `json:"errorMessage"`
}

// ErrorResponse is the envelope for non-successful responses.
type ErrorResponse struct {
	Errors []APIError `json:"errors"`
	Meta   Meta       `json:"meta"`
}

// LoginResponseBody wraps login response payloads with meta.
type LoginResponseBody struct {
	LoginResponse struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		UserID       int64  `json:"userId,omitempty"`
		Username     string `json:"username,omitempty"`
		Role         string `json:"role,omitempty"`
	} `json:"loginResponse"`
	Meta Meta `json:"meta"`
}

// RefreshResponseBody wraps refresh responses.
type RefreshResponseBody struct {
	RefreshResponse struct {
		AccessToken string `json:"accessToken"`
	} `json:"refreshResponse"`
	Meta Meta `json:"meta"`
}

// LauncherConfigResponseBody mirrors launcher config outputs.
type LauncherConfigResponseBody struct {
	LauncherConfigResponse config.LauncherConfig `json:"launcherConfigResponse"`
	Meta                   Meta                  `json:"meta"`
}

// MaintenanceResponseBody contains maintenance info when available.
type MaintenanceResponseBody struct {
	MaintenanceResponse config.MaintenanceInfo `json:"maintenanceResponse"`
	Meta                Meta                   `json:"meta"`
}

// UpdateFileResponse describes individual update files.
type UpdateFileResponse struct {
	URL          string       `json:"url"`
	FileName     string       `json:"fileName"`
	Checksum     string       `json:"checksum"`
	Size         int64        `json:"size,omitempty"`
	DownloadMeta DownloadMeta `json:"downloadMeta"`
}

// DownloadMeta follows the documented structure.
type DownloadMeta struct {
	HashAlgorithm      string `json:"hashAlgorithm"`
	SuggestMultiThread bool   `json:"suggestMultiThread"`
	IsCoreFile         bool   `json:"isCoreFile"`
	IsAbsoluteURL      bool   `json:"isAbsoluteUrl"`
}

// UpdateResponsePayload contains update metadata returned to clients.
type UpdateResponsePayload struct {
	Title           string               `json:"title"`
	Description     string               `json:"description"`
	PosterURL       string               `json:"posterUrl"`
	PublishTime     string               `json:"publishTime"`
	ResourceVersion string               `json:"resourceVersion,omitempty"`
	IsMandatory     bool                 `json:"isMandatory"`
	Files           []UpdateFileResponse `json:"files"`
}

// UpdateResponseBody is the envelope for update responses.
type UpdateResponseBody struct {
	UpdateResponse UpdateResponsePayload `json:"updateResponse"`
	Meta           Meta                  `json:"meta"`
}

// NewsResponsePayload contains paginated news items.
type NewsResponsePayload struct {
	Items   []config.NewsItem `json:"items"`
	HasMore bool              `json:"hasMore"`
}

// NewsResponseBody wraps news responses.
type NewsResponseBody struct {
	NewsResponse NewsResponsePayload `json:"newsResponse"`
	Meta         Meta                `json:"meta"`
}

// AdminMaintenanceResponse returns maintenance configuration for admin UI.
type AdminMaintenanceResponse struct {
	Maintenance config.MaintenanceConfig `json:"maintenance"`
	Meta        Meta                     `json:"meta"`
}

// AdminMaintenanceUpdatePayload is the request body for updating maintenance config.
type AdminMaintenanceUpdatePayload struct {
	Maintenance config.MaintenanceConfig `json:"maintenance"`
}

// AdminUpdatesResponse returns updates configuration for admin UI.
type AdminUpdatesResponse struct {
	Updates config.UpdateConfig `json:"updates"`
	Meta    Meta                `json:"meta"`
}

// AdminUpdatesUpdatePayload is the request body for updating updates config.
type AdminUpdatesUpdatePayload struct {
	Updates config.UpdateConfig `json:"updates"`
}

// AdminNewsResponse returns news configuration for admin UI.
type AdminNewsResponse struct {
	News config.NewsConfig `json:"news"`
	Meta Meta              `json:"meta"`
}

// AdminNewsUpdatePayload is the request body for updating news config.
type AdminNewsUpdatePayload struct {
	News config.NewsConfig `json:"news"`
}

// AdminLauncherResponse returns launcher configuration for admin UI.
type AdminLauncherResponse struct {
	Launcher config.LauncherConfig `json:"launcher"`
	Meta     Meta                  `json:"meta"`
}

// AdminLauncherUpdatePayload is the request body for updating launcher config.
type AdminLauncherUpdatePayload struct {
	Launcher config.LauncherConfig `json:"launcher"`
}

// AdminMessageResponse is a simple message response for admin operations.
type AdminMessageResponse struct {
	Message string `json:"message"`
	Meta    Meta   `json:"meta"`
}

// RegisterPayload models the registration request body.
type RegisterPayload struct {
	RegisterRequest struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	} `json:"registerRequest"`
	Preferences *Preferences `json:"preferences"`
}

// RegisterResponseBody wraps registration response payloads with meta.
type RegisterResponseBody struct {
	RegisterResponse struct {
		UserID   int64  `json:"userId"`
		Username string `json:"username"`
	} `json:"registerResponse"`
	Meta Meta `json:"meta"`
}

// AdminScanPathPayload is the request body for scanning a directory to generate update config.
type AdminScanPathPayload struct {
	Path         string `json:"path"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	BaseURL      string `json:"baseUrl"`
	IsCore       bool   `json:"isCore"`
}

// AdminScanPathResponse returns the generated update files from scanning.
type AdminScanPathResponse struct {
	Files []UpdateFileResponse `json:"files"`
	Count int                  `json:"count"`
	Meta  Meta                 `json:"meta"`
}

// AdminUploadResponse describes a file that was uploaded and is now served by the server.
type AdminUploadResponse struct {
	URL           string `json:"url"`
	FileName      string `json:"fileName"`
	RelativePath  string `json:"relativePath"`
	Size          int64  `json:"size"`
	Checksum      string `json:"checksum"`
	HashAlgorithm string `json:"hashAlgorithm"`
	Meta          Meta   `json:"meta"`
}

// AdminBrowseEntry describes a single entry returned by the directory browser.
type AdminBrowseEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size,omitempty"`
}

// AdminBrowseResponse is the response for browsing the update assets directory.
type AdminBrowseResponse struct {
	Path    string             `json:"path"`
	Parent  string             `json:"parent"`
	Entries []AdminBrowseEntry `json:"entries"`
	Meta    Meta               `json:"meta"`
}
