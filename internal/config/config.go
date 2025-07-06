package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AppConfig represents the main application configuration
type AppConfig struct {
	Server struct {
		Port           string `json:"port"`
		APIVersion     string `json:"apiVersion"`
		MinAPIVersion  string `json:"minApiVersion"`
		BuildVersion   string `json:"buildVersion"`
		ReleaseDate    string `json:"releaseDate"`
	} `json:"server"`
	Authentication struct {
		Enabled                   bool   `json:"enabled"`
		JWTSecret                string `json:"jwtSecret"`
		TokenExpirationSec       int    `json:"tokenExpirationSec"`
		RefreshTokenExpirationDays int   `json:"refreshTokenExpirationDays"`
	} `json:"authentication"`
	Debug struct {
		Enabled bool `json:"enabled"`
	} `json:"debug"`
	Database struct {
		Type string `json:"type"`
		Path string `json:"path"`
	} `json:"database"`
}

// LauncherConfig represents launcher configuration
type LauncherConfigData struct {
	Host             []string               `json:"host"`
	RetryIntervalSec int                   `json:"retryIntervalSec"`
	MaxRetryCount    int                   `json:"maxRetryCount"`
	WebSocket        WebSocketConfig       `json:"webSocket"`
	Security         SecurityConfig        `json:"security"`
	FeaturesFlags    map[string]interface{} `json:"featuresFlags"`
}

type WebSocketConfig struct {
	Enable               bool   `json:"enable"`
	SocketHost          string `json:"socketHost"`
	HeartbeatIntervalSec int    `json:"heartbeatIntervalSec"`
}

type SecurityConfig struct {
	EnableAuthentication        bool   `json:"enableAuthentication"`
	TokenExpirationSec         int    `json:"tokenExpirationSec"`
	RefreshTokenExpirationDays int    `json:"refreshTokenExpirationDays"`
	LoginUrl                   string `json:"loginUrl"`
	LogoutUrl                  string `json:"logoutUrl"`
	RefreshUrl                 string `json:"refreshUrl"`
}

// MaintenanceConfig represents maintenance configuration
type MaintenanceConfigData struct {
	MaintenanceActive bool                     `json:"maintenanceActive"`
	MaintenanceInfo   MaintenanceInfoConfig   `json:"maintenanceInfo"`
}

type MaintenanceInfoConfig struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	StartTime string `json:"startTime"`
	ExEndTime string `json:"exEndTime"`
	PosterUrl string `json:"posterUrl"`
	Link      string `json:"link"`
}

// LanguageConfig represents language configuration
type LanguageConfig map[string]LanguageStrings

type LanguageStrings struct {
	Errors      map[string]string `json:"errors"`
	Maintenance map[string]string `json:"maintenance"`
	Updates     map[string]string `json:"updates"`
}

// Config holds all configuration data
type Config struct {
	App         *AppConfig
	Launcher    *LauncherConfigData
	Maintenance *MaintenanceConfigData
	Languages   LanguageConfig
	ConfigPath  string
}

func Load() *Config {
	configPath := getEnvOrDefault("CONFIG_PATH", "./configs")
	
	config := &Config{
		ConfigPath: configPath,
	}
	
	// Load all configuration files
	config.loadAppConfig()
	config.loadLauncherConfig()
	config.loadMaintenanceConfig()
	config.loadLanguageConfig()
	
	// Override with environment variables
	config.overrideWithEnv()
	
	return config
}

func (c *Config) loadAppConfig() {
	appConfigPath := filepath.Join(c.ConfigPath, "app.json")
	data, err := os.ReadFile(appConfigPath)
	if err != nil {
		// Fall back to defaults if config file doesn't exist
		c.App = &AppConfig{}
		c.App.Server.Port = "8080"
		c.App.Server.APIVersion = "1.0.0"
		c.App.Server.MinAPIVersion = "1.0.0"
		c.App.Server.BuildVersion = "20240601"
		c.App.Server.ReleaseDate = "2024-06-01T12:00:00Z"
		c.App.Authentication.Enabled = false
		c.App.Authentication.JWTSecret = "default-secret-change-this"
		c.App.Authentication.TokenExpirationSec = 3600
		c.App.Authentication.RefreshTokenExpirationDays = 30
		c.App.Debug.Enabled = false
		c.App.Database.Type = "sqlite"
		c.App.Database.Path = "./data/nekolc.db"
		return
	}
	
	c.App = &AppConfig{}
	if err := json.Unmarshal(data, c.App); err != nil {
		fmt.Printf("Error loading app config: %v\n", err)
		// Use defaults on error
		c.loadAppConfig() // Recursive call to set defaults
	}
}

func (c *Config) loadLauncherConfig() {
	launcherConfigPath := filepath.Join(c.ConfigPath, "launcher.json")
	data, err := os.ReadFile(launcherConfigPath)
	if err != nil {
		// Fall back to defaults
		c.Launcher = &LauncherConfigData{
			Host:             []string{"localhost:8080"},
			RetryIntervalSec: 5,
			MaxRetryCount:    3,
			WebSocket: WebSocketConfig{
				Enable:               false,
				SocketHost:          "",
				HeartbeatIntervalSec: 30,
			},
			Security: SecurityConfig{
				EnableAuthentication:        false,
				TokenExpirationSec:         3600,
				RefreshTokenExpirationDays: 30,
				LoginUrl:                   "/v0/api/auth/login",
				LogoutUrl:                  "/v0/api/auth/logout",
				RefreshUrl:                 "/v0/api/auth/refresh",
			},
			FeaturesFlags: map[string]interface{}{
				"ui": map[string]interface{}{
					"enableDevHint": false,
				},
				"enableFeatureA": true,
				"enableFeatureB": false,
			},
		}
		return
	}
	
	c.Launcher = &LauncherConfigData{}
	if err := json.Unmarshal(data, c.Launcher); err != nil {
		fmt.Printf("Error loading launcher config: %v\n", err)
		c.loadLauncherConfig() // Use defaults on error
	}
}

func (c *Config) loadMaintenanceConfig() {
	maintenanceConfigPath := filepath.Join(c.ConfigPath, "maintenance.json")
	data, err := os.ReadFile(maintenanceConfigPath)
	if err != nil {
		// Fall back to defaults
		c.Maintenance = &MaintenanceConfigData{
			MaintenanceActive: false,
			MaintenanceInfo: MaintenanceInfoConfig{
				Status:    "scheduled",
				Message:   "Scheduled maintenance",
				StartTime: "2024-06-01T12:00:00Z",
				ExEndTime: "2024-06-01T14:00:00Z",
				PosterUrl: "https://example.com/maintenance-poster.jpg",
				Link:      "https://example.com/maintenance-announcement",
			},
		}
		return
	}
	
	c.Maintenance = &MaintenanceConfigData{}
	if err := json.Unmarshal(data, c.Maintenance); err != nil {
		fmt.Printf("Error loading maintenance config: %v\n", err)
		c.loadMaintenanceConfig() // Use defaults on error
	}
}

func (c *Config) loadLanguageConfig() {
	languageConfigPath := filepath.Join(c.ConfigPath, "languages.json")
	data, err := os.ReadFile(languageConfigPath)
	if err != nil {
		// Fall back to minimal English defaults
		c.Languages = LanguageConfig{
			"en": LanguageStrings{
				Errors: map[string]string{
					"InvalidRequest":     "The request is invalid.",
					"NotFound":          "Resource not found.",
					"Unauthorized":      "Authentication required.",
					"InternalError":     "Internal server error.",
					"NotImplemented":    "Feature not implemented.",
					"ServiceUnavailable": "Service is currently unavailable.",
				},
				Maintenance: map[string]string{
					"scheduled": "Scheduled maintenance",
					"progress":  "Maintenance in progress",
				},
				Updates: map[string]string{
					"available":   "New version available",
					"description": "Bug fixes and improvements",
				},
			},
		}
		return
	}
	
	c.Languages = make(LanguageConfig)
	if err := json.Unmarshal(data, &c.Languages); err != nil {
		fmt.Printf("Error loading language config: %v\n", err)
		c.loadLanguageConfig() // Use defaults on error
	}
}

func (c *Config) overrideWithEnv() {
	// Override with environment variables for deployment flexibility
	if port := os.Getenv("PORT"); port != "" {
		c.App.Server.Port = port
	}
	if apiVersion := os.Getenv("API_VERSION"); apiVersion != "" {
		c.App.Server.APIVersion = apiVersion
	}
	if buildVersion := os.Getenv("BUILD_VERSION"); buildVersion != "" {
		c.App.Server.BuildVersion = buildVersion
	}
	if enableAuth := os.Getenv("ENABLE_AUTH"); enableAuth == "true" {
		c.App.Authentication.Enabled = true
		c.Launcher.Security.EnableAuthentication = true
	}
	if debugMode := os.Getenv("DEBUG_MODE"); debugMode == "true" {
		c.App.Debug.Enabled = true
		if ui, ok := c.Launcher.FeaturesFlags["ui"].(map[string]interface{}); ok {
			ui["enableDevHint"] = true
		}
	}
	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		c.App.Authentication.JWTSecret = jwtSecret
	}
}

// GetLocalizedString returns a localized string for the given language
func (c *Config) GetLocalizedString(language, category, key string) string {
	if lang, exists := c.Languages[language]; exists {
		switch category {
		case "errors":
			if msg, ok := lang.Errors[key]; ok {
				return msg
			}
		case "maintenance":
			if msg, ok := lang.Maintenance[key]; ok {
				return msg
			}
		case "updates":
			if msg, ok := lang.Updates[key]; ok {
				return msg
			}
		}
	}
	
	// Fall back to English
	if lang, exists := c.Languages["en"]; exists {
		switch category {
		case "errors":
			if msg, ok := lang.Errors[key]; ok {
				return msg
			}
		case "maintenance":
			if msg, ok := lang.Maintenance[key]; ok {
				return msg
			}
		case "updates":
			if msg, ok := lang.Updates[key]; ok {
				return msg
			}
		}
	}
	
	// Final fallback
	return key
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}