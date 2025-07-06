package config

import (
	"os"
)

type Config struct {
	Port                    string
	APIVersion             string
	MinAPIVersion          string
	BuildVersion           string
	ReleaseDate            string
	EnableAuthentication   bool
	EnableDebugMode        bool
	TokenExpirationSec     int
	RefreshTokenExpirationDays int
}

func Load() *Config {
	return &Config{
		Port:                    getEnvOrDefault("PORT", "8080"),
		APIVersion:             getEnvOrDefault("API_VERSION", "1.0.0"),
		MinAPIVersion:          getEnvOrDefault("MIN_API_VERSION", "1.0.0"),
		BuildVersion:           getEnvOrDefault("BUILD_VERSION", "20240601"),
		ReleaseDate:            getEnvOrDefault("RELEASE_DATE", "2024-06-01T12:00:00Z"),
		EnableAuthentication:   getEnvOrDefault("ENABLE_AUTH", "false") == "true",
		EnableDebugMode:        getEnvOrDefault("DEBUG_MODE", "false") == "true",
		TokenExpirationSec:     3600,
		RefreshTokenExpirationDays: 30,
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}