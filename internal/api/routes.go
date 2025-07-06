package api

import (
	"net/http"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/handlers"
	"github.com/moehoshio/NekoLcServer/internal/middleware"
)

func SetupRoutes(cfg *config.Config) http.Handler {
	mux := http.NewServeMux()
	
	// Create handlers
	testingHandler := handlers.NewTestingHandler(cfg)
	authHandler := handlers.NewAuthHandler(cfg)
	launcherHandler := handlers.NewLauncherHandler(cfg)
	
	// Testing endpoints
	mux.Handle("/v0/testing/ping", applyMiddleware(
		http.HandlerFunc(testingHandler.Ping),
		middleware.CommonMiddleware(cfg),
	))
	
	mux.Handle("/v0/testing/echo", applyMiddleware(
		http.HandlerFunc(testingHandler.Echo),
		middleware.CommonMiddleware(cfg),
		middleware.DebugOnlyMiddleware(cfg),
		middleware.AuthMiddleware(cfg, false), // Optional auth for echo
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
		middleware.AuthMiddleware(cfg, false), // Optional auth
	))
	
	mux.Handle("/v0/api/maintenance", applyMiddleware(
		http.HandlerFunc(launcherHandler.Maintenance),
		middleware.CommonMiddleware(cfg),
		methodFilter("POST"),
		middleware.AuthMiddleware(cfg, false), // Optional auth
	))
	
	mux.Handle("/v0/api/checkUpdates", applyMiddleware(
		http.HandlerFunc(launcherHandler.CheckUpdates),
		middleware.CommonMiddleware(cfg),
		methodFilter("POST"),
		middleware.AuthMiddleware(cfg, false), // Optional auth
	))
	
	mux.Handle("/v0/api/feedbackLog", applyMiddleware(
		http.HandlerFunc(launcherHandler.FeedbackLog),
		middleware.CommonMiddleware(cfg),
		methodFilter("POST"),
		middleware.AuthMiddleware(cfg, false), // Optional auth
	))
	
	return mux
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
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}