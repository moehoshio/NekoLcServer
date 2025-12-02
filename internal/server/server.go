package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/moehoshio/NekoLcServer/internal/auth"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/localization"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Server wires configuration, localization, and handlers together.
type Server struct {
	appConfig         *config.AppConfig
	router            chi.Router
	launcherConfig    *config.LauncherConfig
	maintenanceConfig *config.MaintenanceConfig
	updateConfig      *config.UpdateConfig
	updateAssetsDir   string
	localizer         *localization.Localizer
	authService       *auth.Service
	feedbackLogPath   string
	debug             bool
}

// New constructs a Server and prepares its router.
func New(
	appCfg *config.AppConfig,
	launcherCfg *config.LauncherConfig,
	maintenanceCfg *config.MaintenanceConfig,
	updateCfg *config.UpdateConfig,
	updateAssetsDir string,
	localizer *localization.Localizer,
	authSvc *auth.Service,
	feedbackPath string,
) (*Server, error) {
	if appCfg == nil {
		return nil, errors.New("app config is required")
	}
	if err := os.MkdirAll(filepath.Dir(feedbackPath), 0o755); err != nil {
		return nil, fmt.Errorf("create feedback directory: %w", err)
	}
	srv := &Server{
		appConfig:         appCfg,
		launcherConfig:    launcherCfg,
		maintenanceConfig: maintenanceCfg,
		updateConfig:      updateCfg,
		updateAssetsDir:   updateAssetsDir,
		localizer:         localizer,
		authService:       authSvc,
		feedbackLogPath:   feedbackPath,
		debug:             appCfg.Debug.Enabled,
	}
	srv.router = srv.buildRouter()
	return srv, nil
}

// Router returns the configured HTTP handler.
func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/v0/testing/ping", s.handlePing)
	r.Post("/v0/testing/echo", s.handleEcho)

	r.Route("/v0/api", func(r chi.Router) {
		r.Route("/auth", func(authRouter chi.Router) {
			authRouter.Post("/login", s.handleLogin)
			authRouter.Post("/refresh", s.handleRefresh)
			authRouter.Post("/validate", s.handleValidate)
			authRouter.Post("/logout", s.handleLogout)
		})
		r.Post("/launcherConfig", s.handleLauncherConfig)
		r.Post("/maintenance", s.handleMaintenance)
		r.Post("/checkUpdates", s.handleCheckUpdates)
		r.Post("/feedbackLog", s.handleFeedbackLog)
	})

	return r
}

func (s *Server) decode(r *http.Request, dest interface{}) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	defer r.Body.Close()
	reader := io.LimitReader(r.Body, maxBodyBytes)
	dec := json.NewDecoder(reader)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain a single JSON object")
		}
		return err
	}
	return nil
}

func (s *Server) languageFromPreferences(pref *Preferences) string {
	if pref == nil {
		if s.localizer != nil {
			return s.localizer.ResolveCode(s.appConfig.Language.Default)
		}
		return s.appConfig.Language.Default
	}
	language := strings.TrimSpace(pref.Language)
	if language == "" {
		language = s.appConfig.Language.Default
	}
	if s.localizer != nil {
		return s.localizer.ResolveCode(language)
	}
	return language
}

func (s *Server) authMethod() string {
	method := strings.ToLower(strings.TrimSpace(s.appConfig.Authentication.Method))
	if method == "" {
		return "jwt"
	}
	return method
}

func (s *Server) meta() Meta {
	return Meta{
		APIVersion:    s.appConfig.Server.APIVersion,
		MinAPIVersion: s.appConfig.Server.MinAPIVer,
		BuildVersion:  s.appConfig.Server.BuildVersion,
		Timestamp:     time.Now().UTC().Unix(),
		ReleaseDate:   s.appConfig.Server.ReleaseDate,
		IsDeprecated:  false,
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// Logging is best-effort here; avoid panics while responding
		fmt.Fprintf(os.Stderr, "failed to encode response: %v\n", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, lang, errorType, fallback string) {
	errorCategory := "ForClientError"
	if status >= http.StatusInternalServerError {
		errorCategory = "ForServerError"
	}
	message := fallback
	if s.localizer != nil {
		localized := s.localizer.Error(lang, errorType)
		if localized != "" && localized != errorType {
			message = localized
		}
	}
	if message == "" {
		message = errorType
	}
	resp := ErrorResponse{
		Errors: []APIError{{
			Error:        errorCategory,
			ErrorType:    errorType,
			ErrorMessage: message,
		}},
		Meta: s.meta(),
	}
	s.writeJSON(w, status, resp)
}
func (s *Server) maintenanceForClient(client *ClientInfo) (config.MaintenanceInfo, bool) {
	if s.maintenanceConfig != nil && s.maintenanceConfig.MaintenanceActive {
		return s.maintenanceConfig.MaintenanceInfo, true
	}
	key := platformKey(client)
	if key == "" || s.maintenanceConfig == nil {
		return config.MaintenanceInfo{}, false
	}
	if platform, ok := s.maintenanceConfig.PlatformSpecific[key]; ok && platform.MaintenanceActive {
		return platform.MaintenanceInfo, true
	}
	return config.MaintenanceInfo{}, false
}

func platformKey(client *ClientInfo) string {
	if client == nil {
		return ""
	}
	return systemKey(client.System)
}

func systemKey(system *SystemInfo) string {
	if system == nil {
		return ""
	}
	return fmt.Sprintf("%s-%s", strings.ToLower(system.OS), strings.ToLower(system.Arch))
}

func (s *Server) logFeedback(entry interface{}) error {
	file, err := os.OpenFile(s.feedbackLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	return enc.Encode(entry)
}

func (s *Server) diffFilesFromPath(path string, isCore bool) []UpdateFileResponse {
	if path == "" {
		return nil
	}
	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return []UpdateFileResponse{s.fileFromPath(path, isCore)}
	}
	resolved := s.resolveUpdateAssetPath(path)
	data, err := os.ReadFile(resolved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read diff file %s: %v\n", resolved, err)
		return nil
	}
	var urls []string
	if err := json.Unmarshal(data, &urls); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse diff file %s: %v\n", resolved, err)
		return nil
	}
	files := make([]UpdateFileResponse, 0, len(urls))
	for _, url := range urls {
		trimmed := strings.TrimSpace(url)
		if trimmed == "" {
			continue
		}
		files = append(files, s.fileFromPath(trimmed, isCore))
	}
	return files
}

func (s *Server) resolveUpdateAssetPath(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if s.updateAssetsDir == "" {
		return filepath.Clean(path)
	}
	return filepath.Join(s.updateAssetsDir, path)
}
