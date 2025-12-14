package server

import "github.com/moehoshio/NekoLcServer/internal/config"

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

// FeedbackLogPayload models the feedback log submission.
type FeedbackLogPayload struct {
	FeedbackLogRequest struct {
		ClientInfo *ClientInfo `json:"clientInfo"`
		Timestamp  int64       `json:"timestamp"`
		Content    string      `json:"content"`
	} `json:"feedbackLogRequest"`
	Preferences *Preferences `json:"preferences"`
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
