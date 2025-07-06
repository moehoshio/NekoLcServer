package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/middleware"
	"github.com/moehoshio/NekoLcServer/internal/models"
)

type AuthHandler struct {
	Config *config.Config
}

func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{Config: cfg}
}

// Login handles POST /v0/api/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	// If authentication is not implemented, return 501
	if !h.Config.EnableAuthentication {
		rw.WriteError(http.StatusNotImplemented, "NotImplemented", "Authentication system not implemented")
		return
	}
	
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Validate authentication data
	isValid := false
	if req.Auth.Username != "" && req.Auth.Password != "" {
		// Username/password authentication
		// In a real implementation, you would validate against a database
		isValid = req.Auth.Username == "admin" && req.Auth.Password == "password"
	} else if req.Auth.Identifier != "" && req.Auth.Signature != "" {
		// Identifier/signature authentication
		// In a real implementation, you would validate the signature
		isValid = req.Auth.Identifier != "" && req.Auth.Signature != ""
	}
	
	if !isValid {
		rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Invalid credentials")
		return
	}
	
	// Generate tokens (simplified for demo)
	response := models.LoginResponse{
		AccessToken:  "token-" + req.Auth.Username + "-" + h.Config.BuildVersion,
		RefreshToken: "refresh-" + req.Auth.Username + "-" + h.Config.BuildVersion,
		Meta:         models.NewMeta(h.Config.APIVersion, h.Config.MinAPIVersion, h.Config.BuildVersion, h.Config.ReleaseDate),
	}
	
	rw.WriteJSON(http.StatusOK, response)
}

// Refresh handles POST /v0/api/auth/refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	if !h.Config.EnableAuthentication {
		rw.WriteError(http.StatusNotImplemented, "NotImplemented", "Authentication system not implemented")
		return
	}
	
	var req models.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Validate refresh token (simplified)
	if req.RefreshToken == "" || !isValidRefreshToken(req.RefreshToken) {
		rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Invalid or expired refresh token")
		return
	}
	
	response := models.RefreshResponse{
		AccessToken: "token-refreshed-" + h.Config.BuildVersion,
		Meta:        models.NewMeta(h.Config.APIVersion, h.Config.MinAPIVersion, h.Config.BuildVersion, h.Config.ReleaseDate),
	}
	
	rw.WriteJSON(http.StatusOK, response)
}

// Validate handles POST /v0/api/auth/validate
func (h *AuthHandler) Validate(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	if !h.Config.EnableAuthentication {
		rw.WriteError(http.StatusNotImplemented, "NotImplemented", "Authentication system not implemented")
		return
	}
	
	var req models.ValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Validate access token (simplified)
	if req.AccessToken == "" || !isValidAccessToken(req.AccessToken) {
		rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Invalid or expired access token")
		return
	}
	
	// Return 204 No Content for valid token
	rw.WriteNoContent()
}

// Logout handles POST /v0/api/auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	if !h.Config.EnableAuthentication {
		rw.WriteError(http.StatusNotImplemented, "NotImplemented", "Authentication system not implemented")
		return
	}
	
	var req models.LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// In a real implementation, you would invalidate the tokens
	// For now, just return success
	rw.WriteNoContent()
}

// Helper functions for token validation (simplified)
func isValidAccessToken(token string) bool {
	// In a real implementation, you would validate against a token store
	return token != "" && len(token) > 10
}

func isValidRefreshToken(token string) bool {
	// In a real implementation, you would validate against a token store
	return token != "" && len(token) > 10
}