package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/models"
)

func TestAuthHandler_Login_NotImplemented(t *testing.T) {
	cfg := &config.Config{
		APIVersion:           "1.0.0",
		MinAPIVersion:        "1.0.0",
		BuildVersion:         "test",
		ReleaseDate:          "2024-01-01T00:00:00Z",
		EnableAuthentication: false,
	}
	
	handler := NewAuthHandler(cfg)
	
	req := models.LoginRequest{
		Auth: models.AuthInfo{
			Username: "admin",
			Password: "password",
		},
	}
	
	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/auth/login", bytes.NewBuffer(jsonData))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.Login(rr, httpReq)
	
	if rr.Code != http.StatusNotImplemented {
		t.Errorf("Expected status code %d, got %d", http.StatusNotImplemented, rr.Code)
	}
}

func TestAuthHandler_Login_Success(t *testing.T) {
	cfg := &config.Config{
		APIVersion:           "1.0.0",
		MinAPIVersion:        "1.0.0",
		BuildVersion:         "test",
		ReleaseDate:          "2024-01-01T00:00:00Z",
		EnableAuthentication: true,
	}
	
	handler := NewAuthHandler(cfg)
	
	req := models.LoginRequest{
		Auth: models.AuthInfo{
			Username: "admin",
			Password: "password",
		},
	}
	
	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/auth/login", bytes.NewBuffer(jsonData))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.Login(rr, httpReq)
	
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}
	
	var response models.LoginResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	
	if response.AccessToken == "" {
		t.Errorf("Expected access token to be non-empty")
	}
	
	if response.RefreshToken == "" {
		t.Errorf("Expected refresh token to be non-empty")
	}
}

func TestAuthHandler_Login_InvalidCredentials(t *testing.T) {
	cfg := &config.Config{
		APIVersion:           "1.0.0",
		MinAPIVersion:        "1.0.0",
		BuildVersion:         "test",
		ReleaseDate:          "2024-01-01T00:00:00Z",
		EnableAuthentication: true,
	}
	
	handler := NewAuthHandler(cfg)
	
	req := models.LoginRequest{
		Auth: models.AuthInfo{
			Username: "wrong",
			Password: "wrong",
		},
	}
	
	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/auth/login", bytes.NewBuffer(jsonData))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.Login(rr, httpReq)
	
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status code %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestAuthHandler_Validate_Success(t *testing.T) {
	cfg := &config.Config{
		APIVersion:           "1.0.0",
		MinAPIVersion:        "1.0.0",
		BuildVersion:         "test",
		ReleaseDate:          "2024-01-01T00:00:00Z",
		EnableAuthentication: true,
	}
	
	handler := NewAuthHandler(cfg)
	
	req := models.ValidateRequest{
		AccessToken: "valid-token-12345",
	}
	
	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/auth/validate", bytes.NewBuffer(jsonData))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.Validate(rr, httpReq)
	
	if rr.Code != http.StatusNoContent {
		t.Errorf("Expected status code %d, got %d", http.StatusNoContent, rr.Code)
	}
}

func TestAuthHandler_Validate_InvalidToken(t *testing.T) {
	cfg := &config.Config{
		APIVersion:           "1.0.0",
		MinAPIVersion:        "1.0.0",
		BuildVersion:         "test",
		ReleaseDate:          "2024-01-01T00:00:00Z",
		EnableAuthentication: true,
	}
	
	handler := NewAuthHandler(cfg)
	
	req := models.ValidateRequest{
		AccessToken: "short", // Too short to be valid
	}
	
	jsonData, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/v0/api/auth/validate", bytes.NewBuffer(jsonData))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.Validate(rr, httpReq)
	
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status code %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}