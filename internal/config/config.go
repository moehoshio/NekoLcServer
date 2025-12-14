package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// AppConfig represents the top-level server configuration loaded from app.json.
type AppConfig struct {
	Server struct {
		Port         string `json:"port"`
		APIVersion   string `json:"apiVersion"`
		MinAPIVer    string `json:"minApiVersion"`
		BuildVersion string `json:"buildVersion"`
		ReleaseDate  string `json:"releaseDate"`
		BasePath     string `json:"basePath"`
	} `json:"server"`
	Authentication struct {
		Enabled                    bool   `json:"enabled"`
		Method                     string `json:"method"`
		JWTSecret                  string `json:"jwtSecret"`
		IgnoreTokenExpiration      bool   `json:"ignoreTokenExpiration"`
		TokenExpirationSec         int    `json:"tokenExpirationSec"`
		RefreshTokenExpirationDays int    `json:"refreshTokenExpirationDays"`
	} `json:"authentication"`
	Debug struct {
		Enabled bool `json:"enabled"`
	} `json:"debug"`
	Language struct {
		Default    string `json:"default"`
		ConfigPath string `json:"configPath"`
	} `json:"language"`
	Launcher struct {
		ConfigPath string `json:"configPath"`
	} `json:"launcher"`
	Maintenance struct {
		ConfigPath string `json:"configPath"`
	} `json:"maintenance"`
	Update struct {
		ConfigPath string `json:"configPath"`
	} `json:"update"`
}

// LanguageBundle is the strongly typed representation of languages.json.
type LanguageBundle map[string]LanguagePack

// LanguagePack keeps localized strings grouped by domain.
type LanguagePack struct {
	Errors      map[string]string `json:"errors"`
	Maintenance map[string]string `json:"maintenance"`
	Updates     map[string]string `json:"updates"`
}

// LauncherConfig defines launcher.json structure.
type LauncherConfig struct {
	Host             []string               `json:"host"`
	RetryIntervalSec int                    `json:"retryIntervalSec"`
	MaxRetryCount    int                    `json:"maxRetryCount"`
	WebSocket        WebSocketConfig        `json:"webSocket"`
	Security         SecurityConfig         `json:"security"`
	FeaturesFlags    map[string]interface{} `json:"featuresFlags"`
}

// WebSocketConfig describes the websocket options returned to clients.
type WebSocketConfig struct {
	Enable               bool   `json:"enable"`
	SocketHost           string `json:"socketHost"`
	HeartbeatIntervalSec int    `json:"heartbeatIntervalSec"`
}

// SecurityConfig mirrors the security section of launcher.json.
type SecurityConfig struct {
	EnableAuthentication       bool   `json:"enableAuthentication"`
	TokenExpirationSec         int    `json:"tokenExpirationSec"`
	RefreshTokenExpirationDays int    `json:"refreshTokenExpirationDays"`
	LoginURL                   string `json:"loginUrl"`
	LogoutURL                  string `json:"logoutUrl"`
	RefreshURL                 string `json:"refreshUrl"`
}

// MaintenanceConfig reflects maintenance.json.
type MaintenanceConfig struct {
	MaintenanceActive bool                           `json:"maintenanceActive"`
	MaintenanceInfo   MaintenanceInfo                `json:"maintenanceInfo"`
	PlatformSpecific  map[string]PlatformMaintenance `json:"platformSpecific"`
}

// PlatformMaintenance contains overrides per platform tuple.
type PlatformMaintenance struct {
	MaintenanceActive bool            `json:"maintenanceActive"`
	MaintenanceInfo   MaintenanceInfo `json:"maintenanceInfo"`
}

// MaintenanceInfo follows the public contract for maintenance API.
type MaintenanceInfo struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Start   string `json:"startTime"`
	End     string `json:"exEndTime"`
	Poster  string `json:"posterUrl"`
	Link    string `json:"link"`
}

// UpdateConfig mirrors updates.json and organizes updates by platform and architecture.
// Each architecture declares the latest package plus optional diffs from a specific
// client version to that latest package.
type UpdateConfig struct {
	Platforms map[string]PlatformUpdates `json:"platforms"`
}

// PlatformUpdates groups architectures for an OS.
type PlatformUpdates struct {
	Architectures map[string]ArchUpdates `json:"architectures"`
}

// ArchUpdates holds the latest package for an architecture and the diff list.
type ArchUpdates struct {
	Latest FullPackage `json:"latest"`
	Diffs  []DiffFile  `json:"diffs"`
}

// FullPackage contains the latest package metadata per platform/architecture.
type FullPackage struct {
	CoreVersion     string          `json:"coreVersion"`
	ResourceVersion string          `json:"resourceVersion"`
	Core            []DownloadEntry `json:"core"`
	Resource        []DownloadEntry `json:"resource"`
}

// DiffFile describes incremental update metadata from a specific version to latest.
type DiffFile struct {
	FromCoreVersion     string          `json:"fromCoreVersion"`
	Core                []DownloadEntry `json:"core"`
	FromResourceVersion string          `json:"fromResourceVersion"`
	Resource            []DownloadEntry `json:"resource"`
}

// DownloadEntry captures a single downloadable artifact or a local path that expands to many files.
type DownloadEntry struct {
	URL          string       `json:"url,omitempty"`
	Path         string       `json:"path,omitempty"`
	BaseURL      string       `json:"baseUrl,omitempty"`
	FileName     string       `json:"fileName,omitempty"`
	Checksum     string       `json:"checksum,omitempty"`
	Size         int64        `json:"size,omitempty"`
	DownloadMeta DownloadMeta `json:"downloadMeta"`
}

// DownloadMeta configures download hints per artifact.
type DownloadMeta struct {
	HashAlgorithm      string `json:"hashAlgorithm"`
	SuggestMultiThread bool   `json:"suggestMultiThread"`
}

// LoadAppConfig reads the primary configuration file.
func LoadAppConfig(path string) (*AppConfig, error) {
	var cfg AppConfig
	if err := loadJSON(path, &cfg); err != nil {
		return nil, fmt.Errorf("load app config: %w", err)
	}
	if cfg.Server.Port == "" {
		return nil, errors.New("server.port must be specified")
	}
	return &cfg, nil
}

// LoadLanguages loads the localization bundle.
func LoadLanguages(path string) (LanguageBundle, error) {
	bundle := LanguageBundle{}
	if err := loadJSON(path, &bundle); err != nil {
		return nil, fmt.Errorf("load languages: %w", err)
	}
	return bundle, nil
}

// LoadLauncher loads launcher configuration.
func LoadLauncher(path string) (*LauncherConfig, error) {
	var cfg LauncherConfig
	if err := loadJSON(path, &cfg); err != nil {
		return nil, fmt.Errorf("load launcher config: %w", err)
	}
	return &cfg, nil
}

// LoadMaintenance loads maintenance configuration.
func LoadMaintenance(path string) (*MaintenanceConfig, error) {
	var cfg MaintenanceConfig
	if err := loadJSON(path, &cfg); err != nil {
		return nil, fmt.Errorf("load maintenance config: %w", err)
	}
	if cfg.PlatformSpecific == nil {
		cfg.PlatformSpecific = map[string]PlatformMaintenance{}
	}
	return &cfg, nil
}

// LoadUpdates loads update configuration.
func LoadUpdates(path string) (*UpdateConfig, error) {
	var cfg UpdateConfig
	if err := loadJSONAllowComments(path, &cfg); err != nil {
		return nil, fmt.Errorf("load update config: %w", err)
	}
	if cfg.Platforms == nil {
		cfg.Platforms = map[string]PlatformUpdates{}
	}
	for k, platform := range cfg.Platforms {
		if platform.Architectures == nil {
			platform.Architectures = map[string]ArchUpdates{}
		}
		for archKey, arch := range platform.Architectures {
			ensureDownloadMetaSlice(arch.Latest.Core)
			ensureDownloadMetaSlice(arch.Latest.Resource)
			for i := range arch.Diffs {
				ensureDownloadMetaSlice(arch.Diffs[i].Core)
				ensureDownloadMetaSlice(arch.Diffs[i].Resource)
			}
			platform.Architectures[archKey] = arch
		}
		cfg.Platforms[k] = platform
	}
	return &cfg, nil
}

func ensureDownloadMetaSlice(entries []DownloadEntry) {
	for i := range entries {
		if entries[i].DownloadMeta.HashAlgorithm == "" {
			entries[i].DownloadMeta.HashAlgorithm = "sha256"
		}
	}
}

func loadJSON(path string, target interface{}) error {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// loadJSONAllowComments behaves like loadJSON but permits // and /* */ style comments by stripping them before decoding.
func loadJSONAllowComments(path string, target interface{}) error {
	file, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	clean := stripJSONComments(file)
	decoder := json.NewDecoder(bytes.NewReader(clean))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func stripJSONComments(src []byte) []byte {
	var out bytes.Buffer
	inString := false
	inLineComment := false
	inBlockComment := false
	escaped := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inLineComment {
			if c == '\n' {
				inLineComment = false
				out.WriteByte(c)
			}
			continue
		}
		if inBlockComment {
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if escaped {
			out.WriteByte(c)
			escaped = false
			continue
		}
		if inString {
			if c == '\\' {
				escaped = true
				out.WriteByte(c)
				continue
			}
			if c == '"' {
				inString = false
			}
			out.WriteByte(c)
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(src) {
			next := src[i+1]
			if next == '/' {
				inLineComment = true
				i++
				continue
			}
			if next == '*' {
				inBlockComment = true
				i++
				continue
			}
		}
		out.WriteByte(c)
	}
	return out.Bytes()
}
