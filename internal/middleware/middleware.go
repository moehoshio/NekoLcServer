package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/moehoshio/NekoLcServer/internal/auth"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/models"
	"github.com/moehoshio/NekoLcServer/internal/storage"
)

// ResponseWriter wraps http.ResponseWriter to provide utility methods
type ResponseWriter struct {
	http.ResponseWriter
	Config *config.Config
}

// WriteJSON writes a JSON response with the given status code
func (rw *ResponseWriter) WriteJSON(statusCode int, data interface{}) error {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(statusCode)
	return json.NewEncoder(rw.ResponseWriter).Encode(data)
}

// WriteError writes a standardized error response with localization support
func (rw *ResponseWriter) WriteError(statusCode int, errorType, errorMessage string) error {
	return rw.WriteErrorWithLanguage(statusCode, errorType, errorMessage, "en")
}

// WriteErrorWithLanguage writes a localized error response
func (rw *ResponseWriter) WriteErrorWithLanguage(statusCode int, errorType, fallbackMessage, language string) error {
	// Try to get localized error message
	localizedMessage := rw.Config.GetLocalizedString(language, "errors", errorType)
	if localizedMessage == errorType {
		// Fallback to provided message if no localization found
		localizedMessage = fallbackMessage
	}
	
	meta := models.NewMeta(rw.Config.App.Server.APIVersion, rw.Config.App.Server.MinAPIVersion, 
		rw.Config.App.Server.BuildVersion, rw.Config.App.Server.ReleaseDate)
	errorResp := models.NewErrorResponse(meta, errorType, localizedMessage)
	return rw.WriteJSON(statusCode, errorResp)
}

// WriteNoContent writes a 204 No Content response
func (rw *ResponseWriter) WriteNoContent() {
	rw.WriteHeader(http.StatusNoContent)
}

// CommonMiddleware provides common functionality for all endpoints
func CommonMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Wrap the response writer
			rw := &ResponseWriter{
				ResponseWriter: w,
				Config:         cfg,
			}
			
			// Set common headers
			w.Header().Set("Content-Type", "application/json")
			
			// Check Content-Type for POST requests
			if r.Method == "POST" && !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
				rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Content-Type must be application/json")
				return
			}
			
			// Continue with the request
			next.ServeHTTP(rw, r)
		})
	}
}

// AuthMiddleware checks for valid JWT authentication
func AuthMiddleware(cfg *config.Config, db *storage.Database, jwtAuth *auth.JWTAuth, required bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &ResponseWriter{
				ResponseWriter: w,
				Config:         cfg,
			}
			
			// If authentication is not enabled, proceed
			if !cfg.App.Authentication.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			
			// Check for Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				if required {
					rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Authorization header required")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			
			// Check Bearer token format
			if !strings.HasPrefix(authHeader, "Bearer ") {
				if required {
					rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Invalid authorization format")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			
			// Extract token
			token := strings.TrimPrefix(authHeader, "Bearer ")
			
			// Validate JWT token
			_, err := jwtAuth.ValidateToken(token)
			if err != nil {
				if required {
					rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Invalid or expired token")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			
			// Check if token is revoked in database
			tokenHash := jwtAuth.GetTokenHash(token)
			storedToken, err := db.GetAuthToken(tokenHash)
			if err != nil || storedToken == nil || storedToken.IsRevoked {
				if required {
					rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Token has been revoked")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			
			// Add user info to request context if needed
			// context.WithValue(r.Context(), "userID", claims.UserID)
			
			next.ServeHTTP(w, r)
		})
	}
}

// DebugOnlyMiddleware restricts access to debug-only endpoints
func DebugOnlyMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &ResponseWriter{
				ResponseWriter: w,
				Config:         cfg,
			}
			
			if !cfg.App.Debug.Enabled {
				rw.WriteError(http.StatusNotFound, "NotFound", "Endpoint not available in production")
				return
			}
			
			next.ServeHTTP(w, r)
		})
	}
}