package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/moehoshio/NekoLcServer/internal/auth"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/middleware"
	"github.com/moehoshio/NekoLcServer/internal/models"
	"github.com/moehoshio/NekoLcServer/internal/storage"
)

type AuthHandler struct {
	Config  *config.Config
	DB      storage.Storage
	JWTAuth *auth.JWTAuth
}

func NewAuthHandler(cfg *config.Config, db storage.Storage, jwtAuth *auth.JWTAuth) *AuthHandler {
	return &AuthHandler{
		Config:  cfg,
		DB:      db,
		JWTAuth: jwtAuth,
	}
}

// Login handles POST /v0/api/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	// If authentication is not implemented, return 501
	if !h.Config.App.Authentication.Enabled {
		rw.WriteError(http.StatusNotImplemented, "NotImplemented", "Authentication system not implemented")
		return
	}
	
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Get preferred language for error messages
	language := "en"
	if req.Preferences.Language != "" {
		language = req.Preferences.Language
	}
	
	var accessToken, refreshToken string
	var err error
	
	// Check authentication method
	if req.Auth.Username != "" && req.Auth.Password != "" {
		// Username/password authentication
		accessToken, refreshToken, err = h.JWTAuth.GenerateTokensFromCredentials(req.Auth.Username, req.Auth.Password)
	} else if req.Auth.Identifier != "" && req.Auth.Signature != "" {
		// Identifier/signature authentication (JWT specification requirement)
		accessToken, refreshToken, err = h.JWTAuth.GenerateTokensFromSignature(req.Auth.Identifier, req.Auth.Timestamp, req.Auth.Signature)
	} else {
		rw.WriteErrorWithLanguage(http.StatusBadRequest, "InvalidRequest", "Username/password or identifier/signature required", language)
		return
	}
	
	if err != nil {
		rw.WriteErrorWithLanguage(http.StatusUnauthorized, "Unauthorized", "Invalid credentials", language)
		return
	}
	
	// Store tokens in database for revocation tracking
	userID := req.Auth.Username
	if userID == "" {
		userID = req.Auth.Identifier
	}
	
	accessTokenHash := h.JWTAuth.GetTokenHash(accessToken)
	refreshTokenHash := h.JWTAuth.GetTokenHash(refreshToken)
	
	// Store access token
	accessTokenRecord := &storage.AuthToken{
		TokenHash: accessTokenHash,
		TokenType: "access",
		UserID:    userID,
		ExpiresAt: time.Now().Add(time.Duration(h.Config.App.Authentication.TokenExpirationSec) * time.Second),
	}
	if err := h.DB.StoreAuthToken(accessTokenRecord); err != nil {
		rw.WriteErrorWithLanguage(http.StatusInternalServerError, "InternalError", "Failed to store access token", language)
		return
	}
	
	// Store refresh token
	refreshTokenRecord := &storage.AuthToken{
		TokenHash: refreshTokenHash,
		TokenType: "refresh",
		UserID:    userID,
		ExpiresAt: time.Now().Add(time.Duration(h.Config.App.Authentication.RefreshTokenExpirationDays) * 24 * time.Hour),
	}
	if err := h.DB.StoreAuthToken(refreshTokenRecord); err != nil {
		rw.WriteErrorWithLanguage(http.StatusInternalServerError, "InternalError", "Failed to store refresh token", language)
		return
	}
	
	response := models.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Meta:         models.NewMeta(h.Config.App.Server.APIVersion, h.Config.App.Server.MinAPIVersion, h.Config.App.Server.BuildVersion, h.Config.App.Server.ReleaseDate),
	}
	
	rw.WriteJSON(http.StatusOK, response)
}

// Refresh handles POST /v0/api/auth/refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	if !h.Config.App.Authentication.Enabled {
		rw.WriteError(http.StatusNotImplemented, "NotImplemented", "Authentication system not implemented")
		return
	}
	
	var req models.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Validate refresh token
	refreshTokenHash := h.JWTAuth.GetTokenHash(req.RefreshToken)
	storedToken, err := h.DB.GetAuthToken(refreshTokenHash)
	if err != nil || storedToken == nil || storedToken.IsRevoked || storedToken.TokenType != "refresh" {
		rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Invalid or expired refresh token")
		return
	}
	
	// Check if token is expired
	if time.Now().After(storedToken.ExpiresAt) {
		rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Refresh token has expired")
		return
	}
	
	// Generate new access token
	newAccessToken, err := h.JWTAuth.RefreshAccessToken(req.RefreshToken)
	if err != nil {
		rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Failed to refresh token")
		return
	}
	
	// Store new access token
	newAccessTokenHash := h.JWTAuth.GetTokenHash(newAccessToken)
	accessTokenRecord := &storage.AuthToken{
		TokenHash: newAccessTokenHash,
		TokenType: "access",
		UserID:    storedToken.UserID,
		ExpiresAt: time.Now().Add(time.Duration(h.Config.App.Authentication.TokenExpirationSec) * time.Second),
	}
	if err := h.DB.StoreAuthToken(accessTokenRecord); err != nil {
		rw.WriteError(http.StatusInternalServerError, "InternalError", "Failed to store new access token")
		return
	}
	
	response := models.RefreshResponse{
		AccessToken: newAccessToken,
		Meta:        models.NewMeta(h.Config.App.Server.APIVersion, h.Config.App.Server.MinAPIVersion, h.Config.App.Server.BuildVersion, h.Config.App.Server.ReleaseDate),
	}
	
	rw.WriteJSON(http.StatusOK, response)
}

// Validate handles POST /v0/api/auth/validate
func (h *AuthHandler) Validate(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	if !h.Config.App.Authentication.Enabled {
		rw.WriteError(http.StatusNotImplemented, "NotImplemented", "Authentication system not implemented")
		return
	}
	
	var req models.ValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Validate JWT token
	_, err := h.JWTAuth.ValidateToken(req.AccessToken)
	if err != nil {
		rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Invalid or expired access token")
		return
	}
	
	// Check if token is revoked in database
	tokenHash := h.JWTAuth.GetTokenHash(req.AccessToken)
	storedToken, err := h.DB.GetAuthToken(tokenHash)
	if err != nil || storedToken == nil || storedToken.IsRevoked {
		rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Token has been revoked")
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
	
	if !h.Config.App.Authentication.Enabled {
		rw.WriteError(http.StatusNotImplemented, "NotImplemented", "Authentication system not implemented")
		return
	}
	
	var req models.LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Revoke both tokens
	if req.Logout.AccessToken != "" {
		accessTokenHash := h.JWTAuth.GetTokenHash(req.Logout.AccessToken)
		if err := h.DB.RevokeAuthToken(accessTokenHash); err != nil {
			rw.WriteError(http.StatusInternalServerError, "InternalError", "Failed to revoke access token")
			return
		}
	}
	
	if req.Logout.RefreshToken != "" {
		refreshTokenHash := h.JWTAuth.GetTokenHash(req.Logout.RefreshToken)
		if err := h.DB.RevokeAuthToken(refreshTokenHash); err != nil {
			rw.WriteError(http.StatusInternalServerError, "InternalError", "Failed to revoke refresh token")
			return
		}
	}
	
	rw.WriteNoContent()
}