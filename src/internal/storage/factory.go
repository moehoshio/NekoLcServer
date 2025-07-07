package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/moehoshio/NekoLcServer/internal/config"
)

// NewStorage creates a new storage implementation based on configuration
func NewStorage(cfg *config.Config) (Storage, error) {
	switch cfg.App.Database.Type {
	case "sqlite":
		return NewDatabase(cfg.App.Database.Path)
	case "file":
		basePath := cfg.App.Database.Path
		if basePath == "" {
			basePath = cfg.App.Storage.BasePath
		}
		return NewFileStorage(basePath)
	case "mysql":
		// TODO: Implement MySQL storage
		return nil, fmt.Errorf("MySQL storage not yet implemented")
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.App.Database.Type)
	}
}

// EnsureDataDirectory creates the data directory if it doesn't exist
func EnsureDataDirectory(cfg *config.Config) error {
	var dirPath string
	
	switch cfg.App.Database.Type {
	case "sqlite":
		dirPath = filepath.Dir(cfg.App.Database.Path)
	case "file":
		dirPath = cfg.App.Database.Path
		if dirPath == "" {
			dirPath = cfg.App.Storage.BasePath
		}
	default:
		return nil // No directory needed for other types
	}
	
	if dirPath != "" && dirPath != "." {
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("failed to create data directory: %w", err)
		}
	}
	
	return nil
}