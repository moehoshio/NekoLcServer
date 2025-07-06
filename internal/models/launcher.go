package models

// Launcher models

type LauncherConfigRequest struct {
	LauncherConfigRequest LauncherConfigRequestInfo `json:"launcherConfigRequest"`
	Preferences           Preferences               `json:"preferences,omitempty"`
}

type LauncherConfigRequestInfo struct {
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	CoreVersion     string `json:"coreVersion,omitempty"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

type LauncherConfigResponse struct {
	LauncherConfig LauncherConfig `json:"launcherConfig"`
	Meta           Meta           `json:"meta"`
}

type LauncherConfig struct {
	Host               []string     `json:"host"`
	WebSocket          WebSocket    `json:"webSocket"`
	RetryIntervalSec   int          `json:"retryIntervalSec"`
	MaxRetryCount      int          `json:"maxRetryCount"`
	Security           Security     `json:"security"`
	FeaturesFlags      interface{}  `json:"featuresFlags"`
}

type WebSocket struct {
	Enable                bool   `json:"enable"`
	SocketHost           string `json:"socketHost,omitempty"`
	HeartbeatIntervalSec int    `json:"heartbeatIntervalSec,omitempty"`
}

type Security struct {
	EnableAuthentication         bool   `json:"enableAuthentication"`
	TokenExpirationSec          int    `json:"tokenExpirationSec"`
	RefreshTokenExpirationDays  int    `json:"refreshTokenExpirationDays"`
	LoginUrl                    string `json:"loginUrl,omitempty"`
	LogoutUrl                   string `json:"logoutUrl,omitempty"`
	RefreshUrl                  string `json:"refreshUrl,omitempty"`
}

// Maintenance models

type MaintenanceRequest struct {
	CheckMaintenance CheckMaintenanceInfo `json:"checkMaintenance"`
	Preferences      Preferences          `json:"preferences,omitempty"`
}

type CheckMaintenanceInfo struct {
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	CoreVersion     string `json:"coreVersion,omitempty"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

type MaintenanceResponse struct {
	MaintenanceInformation MaintenanceInformation `json:"maintenanceInformation"`
	Meta                   Meta                   `json:"meta"`
}

type MaintenanceInformation struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	StartTime string `json:"startTime"`
	ExEndTime string `json:"exEndTime"`
	PosterUrl string `json:"posterUrl,omitempty"`
	Link      string `json:"link,omitempty"`
}

// Update models

type CheckUpdateRequest struct {
	CheckUpdate CheckUpdateInfo `json:"checkUpdate"`
	Preferences Preferences     `json:"preferences,omitempty"`
}

type CheckUpdateInfo struct {
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	CoreVersion     string `json:"coreVersion"`
	ResourceVersion string `json:"resourceVersion"`
}

type UpdateResponse struct {
	UpdateInformation UpdateInformation `json:"updateInformation"`
	Meta              Meta              `json:"meta"`
}

type UpdateInformation struct {
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	PosterUrl       string     `json:"posterUrl,omitempty"`
	PublishTime     string     `json:"publishTime"`
	ResourceVersion string     `json:"resourceVersion,omitempty"`
	IsMandatory     bool       `json:"isMandatory"`
	Files           []FileInfo `json:"files"`
}

type FileInfo struct {
	URL          string       `json:"url"`
	FileName     string       `json:"fileName"`
	Checksum     string       `json:"checksum"`
	DownloadMeta DownloadMeta `json:"downloadMeta"`
}

type DownloadMeta struct {
	HashAlgorithm       string `json:"hashAlgorithm"`
	SuggestMultiThread  bool   `json:"suggestMultiThread"`
	IsCoreFile          bool   `json:"isCoreFile"`
	IsAbsoluteUrl       bool   `json:"isAbsoluteUrl"`
}

// Feedback models

type FeedbackLogRequest struct {
	FeedbackLog FeedbackLogInfo `json:"feedbackLog"`
	Preferences Preferences     `json:"preferences,omitempty"`
}

type FeedbackLogInfo struct {
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	CoreVersion     string `json:"coreVersion"`
	ResourceVersion string `json:"resourceVersion"`
	Timestamp       int64  `json:"timestamp"`
	Content         string `json:"content"`
}