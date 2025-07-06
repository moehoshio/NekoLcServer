package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/models"
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

// WriteError writes a standardized error response
func (rw *ResponseWriter) WriteError(statusCode int, errorType, errorMessage string) error {
	meta := models.NewMeta(rw.Config.APIVersion, rw.Config.MinAPIVersion, rw.Config.BuildVersion, rw.Config.ReleaseDate)
	errorResp := models.NewErrorResponse(meta, errorType, errorMessage)
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

// AuthMiddleware checks for valid authentication (optional)
func AuthMiddleware(cfg *config.Config, required bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &ResponseWriter{
				ResponseWriter: w,
				Config:         cfg,
			}
			
			// If authentication is not enabled, proceed
			if !cfg.EnableAuthentication {
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
			
			// Basic token validation (simplified for this implementation)
			if !strings.HasPrefix(authHeader, "Bearer ") {
				if required {
					rw.WriteError(http.StatusUnauthorized, "Unauthorized", "Invalid authorization format")
					return
				}
			}
			
			// In a real implementation, you would validate the token here
			// For now, we just check if it has the proper format
			
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
			
			if !cfg.EnableDebugMode {
				rw.WriteError(http.StatusNotFound, "NotFound", "Endpoint not available in production")
				return
			}
			
			next.ServeHTTP(w, r)
		})
	}
}