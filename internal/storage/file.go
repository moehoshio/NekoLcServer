package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileStorage implements storage using local files
type FileStorage struct {
	basePath string
}

// NewFileStorage creates a new file-based storage
func NewFileStorage(basePath string) (*FileStorage, error) {
	// Ensure the base directory exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}
	
	return &FileStorage{
		basePath: basePath,
	}, nil
}

func (f *FileStorage) StoreFeedbackLog(log *FeedbackLog) error {
	feedbackDir := filepath.Join(f.basePath, "feedback")
	if err := os.MkdirAll(feedbackDir, 0755); err != nil {
		return fmt.Errorf("failed to create feedback directory: %w", err)
	}
	
	// Create filename with timestamp
	filename := fmt.Sprintf("feedback_%d_%s_%s.json", 
		time.Now().Unix(), log.OS, log.Arch)
	filePath := filepath.Join(feedbackDir, filename)
	
	data, err := json.Marshal(log)
	if err != nil {
		return fmt.Errorf("failed to marshal feedback log: %w", err)
	}
	
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write feedback log: %w", err)
	}
	
	return nil
}

func (f *FileStorage) StoreAuthToken(token *AuthToken) error {
	tokenDir := filepath.Join(f.basePath, "tokens")
	if err := os.MkdirAll(tokenDir, 0755); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}
	
	// Create filename with token hash
	filename := fmt.Sprintf("token_%s.json", token.TokenHash)
	filePath := filepath.Join(tokenDir, filename)
	
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to marshal auth token: %w", err)
	}
	
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write auth token: %w", err)
	}
	
	return nil
}

func (f *FileStorage) GetAuthToken(tokenHash string) (*AuthToken, error) {
	filename := fmt.Sprintf("token_%s.json", tokenHash)
	filePath := filepath.Join(f.basePath, "tokens", filename)
	
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Token not found
		}
		return nil, fmt.Errorf("failed to read auth token: %w", err)
	}
	
	var token AuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal auth token: %w", err)
	}
	
	// Check if token is revoked or expired
	if token.IsRevoked || time.Now().After(token.ExpiresAt) {
		return nil, nil
	}
	
	return &token, nil
}

func (f *FileStorage) RevokeAuthToken(tokenHash string) error {
	token, err := f.GetAuthToken(tokenHash)
	if err != nil {
		return err
	}
	if token == nil {
		return nil // Token doesn't exist
	}
	
	token.IsRevoked = true
	return f.StoreAuthToken(token)
}

func (f *FileStorage) RevokeAllUserTokens(userID string) error {
	tokenDir := filepath.Join(f.basePath, "tokens")
	files, err := os.ReadDir(tokenDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No tokens directory
		}
		return fmt.Errorf("failed to read tokens directory: %w", err)
	}
	
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		
		filePath := filepath.Join(tokenDir, file.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		
		var token AuthToken
		if err := json.Unmarshal(data, &token); err != nil {
			continue
		}
		
		if token.UserID == userID && !token.IsRevoked {
			token.IsRevoked = true
			tokenData, err := json.Marshal(&token)
			if err != nil {
				continue
			}
			os.WriteFile(filePath, tokenData, 0644)
		}
	}
	
	return nil
}

func (f *FileStorage) Close() error {
	// No cleanup needed for file storage
	return nil
}