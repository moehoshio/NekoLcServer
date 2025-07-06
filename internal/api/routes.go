package api

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/moehoshio/NekoLcServer/internal/auth"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/handlers"
	"github.com/moehoshio/NekoLcServer/internal/middleware"
	"github.com/moehoshio/NekoLcServer/internal/storage"
)

func SetupRoutes(cfg *config.Config) http.Handler {
	// Initialize database
	if err := os.MkdirAll("./data", 0755); err != nil {
		log.Printf("Warning: Failed to create data directory: %v", err)
	}
	
	db, err := storage.NewDatabase(cfg.App.Database.Path)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	
	// Initialize JWT authentication
	jwtAuth := auth.NewJWTAuth(cfg.App.Authentication.JWTSecret)
	
	mux := http.NewServeMux()
	
	// Create handlers with dependencies
	testingHandler := handlers.NewTestingHandler(cfg)
	authHandler := handlers.NewAuthHandler(cfg, db, jwtAuth)
	launcherHandler := handlers.NewLauncherHandler(cfg, db)
	
	// Testing endpoints
	mux.Handle("/v0/testing/ping", applyMiddleware(
		http.HandlerFunc(testingHandler.Ping),
		middleware.CommonMiddleware(cfg),
	))
	
	mux.Handle("/v0/testing/echo", applyMiddleware(
		http.HandlerFunc(testingHandler.Echo),
		middleware.CommonMiddleware(cfg),
		middleware.DebugOnlyMiddleware(cfg),
		middleware.AuthMiddleware(cfg, db, jwtAuth, false), // Optional auth for echo
	))
	
	// Authentication endpoints (optional)
	mux.Handle("/v0/api/auth/login", applyMiddleware(
		http.HandlerFunc(authHandler.Login),
		middleware.CommonMiddleware(cfg),
		methodFilter("POST"),
	))
	
	mux.Handle("/v0/api/auth/refresh", applyMiddleware(
		http.HandlerFunc(authHandler.Refresh),
		middleware.CommonMiddleware(cfg),
		methodFilter("POST"),
	))
	
	mux.Handle("/v0/api/auth/validate", applyMiddleware(
		http.HandlerFunc(authHandler.Validate),
		middleware.CommonMiddleware(cfg),
		methodFilter("POST"),
	))
	
	mux.Handle("/v0/api/auth/logout", applyMiddleware(
		http.HandlerFunc(authHandler.Logout),
		middleware.CommonMiddleware(cfg),
		methodFilter("POST"),
	))
	
	// Launcher endpoints
	mux.Handle("/v0/api/launcherConfig", applyMiddleware(
		http.HandlerFunc(launcherHandler.LauncherConfig),
		middleware.CommonMiddleware(cfg),
		methodFilter("POST"),
		middleware.AuthMiddleware(cfg, db, jwtAuth, false), // Optional auth
	))
	
	mux.Handle("/v0/api/maintenance", applyMiddleware(
		http.HandlerFunc(launcherHandler.Maintenance),
		middleware.CommonMiddleware(cfg),
		methodFilter("POST"),
		middleware.AuthMiddleware(cfg, db, jwtAuth, false), // Optional auth
	))
	
	mux.Handle("/v0/api/checkUpdates", applyMiddleware(
		http.HandlerFunc(launcherHandler.CheckUpdates),
		middleware.CommonMiddleware(cfg),
		methodFilter("POST"),
		middleware.AuthMiddleware(cfg, db, jwtAuth, false), // Optional auth
	))
	
	mux.Handle("/v0/api/feedbackLog", applyMiddleware(
		http.HandlerFunc(launcherHandler.FeedbackLog),
		middleware.CommonMiddleware(cfg),
		methodFilter("POST"),
		middleware.AuthMiddleware(cfg, db, jwtAuth, false), // Optional auth
	))
	
	// Log configuration status
	log.Printf("Authentication enabled: %v", cfg.App.Authentication.Enabled)
	log.Printf("Debug mode enabled: %v", cfg.App.Debug.Enabled)
	log.Printf("Database path: %s", cfg.App.Database.Path)
	
	return &serverWrapper{
		handler: mux,
		db:      db,
	}
}

// serverWrapper wraps the HTTP handler and holds the database reference for cleanup
type serverWrapper struct {
	handler http.Handler
	db      *storage.Database
}

func (sw *serverWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sw.handler.ServeHTTP(w, r)
}

// Close closes the database connection (call this on server shutdown)
func (sw *serverWrapper) Close() error {
	if sw.db != nil {
		return sw.db.Close()
	}
	return nil
}

// applyMiddleware applies multiple middleware functions to a handler
func applyMiddleware(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// methodFilter creates middleware that only allows specific HTTP methods
func methodFilter(allowedMethod string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != allowedMethod {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusMethodNotAllowed)
				fmt.Fprintf(w, `{"errors":[{"error":"ForClientError","errorType":"MethodNotAllowed","errorMessage":"Method %s not allowed"}]}`, r.Method)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}