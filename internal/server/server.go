package server

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/bcrypt"

	"github.com/moehoshio/NekoLcServer/internal/auth"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/localization"
	"github.com/moehoshio/NekoLcServer/internal/store"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Server wires configuration, localization, and handlers together.
type Server struct {
	appConfig             *config.AppConfig
	router                chi.Router
	launcherConfig        *config.LauncherConfig
	maintenanceConfig     *config.MaintenanceConfig
	maintenanceConfigPath string
	newsItems             []config.NewsItem
	newsConfigPath        string
	updateConfig          *config.UpdateConfig
	updateConfigPath      string
	updateAssetsDir       string
	localizer             *localization.Localizer
	authService           *auth.Service
	store                 store.Store
	feedbackLogPath       string
	debug                 bool
	basePath              string
	configMu              sync.RWMutex
	dirCacheMu            sync.RWMutex
	dirCache              map[string]dirCacheEntry
	updateConfigMu        sync.RWMutex
	loginLimiter          *rateLimiter
}

type dirCacheEntry struct {
	files         []UpdateFileResponse
	lastModified  time.Time
	hashAlgorithm string
	baseURL       string
	isCore        bool
}

// New constructs a Server and prepares its router.
func New(
	appCfg *config.AppConfig,
	launcherCfg *config.LauncherConfig,
	maintenanceCfg *config.MaintenanceConfig,
	maintenanceCfgPath string,
	newsCfg *config.NewsConfig,
	newsCfgPath string,
	updateCfg *config.UpdateConfig,
	updateCfgPath string,
	updateAssetsDir string,
	localizer *localization.Localizer,
	authSvc *auth.Service,
	store store.Store,
	feedbackPath string,
) (*Server, error) {
	if appCfg == nil {
		return nil, errors.New("app config is required")
	}
	if err := os.MkdirAll(filepath.Dir(feedbackPath), 0o755); err != nil {
		return nil, fmt.Errorf("create feedback directory: %w", err)
	}
	srv := &Server{
		appConfig:             appCfg,
		launcherConfig:        launcherCfg,
		maintenanceConfig:     maintenanceCfg,
		maintenanceConfigPath: maintenanceCfgPath,
		newsItems:             normalizeNewsItems(newsCfg),
		newsConfigPath:        newsCfgPath,
		updateConfig:          updateCfg,
		updateConfigPath:      updateCfgPath,
		updateAssetsDir:       updateAssetsDir,
		localizer:             localizer,
		authService:           authSvc,
		store:                 store,
		feedbackLogPath:       feedbackPath,
		debug:                 appCfg.Debug.Enabled,
		basePath:              normalizeBasePath(appCfg.Server.BasePath),
		dirCache:              map[string]dirCacheEntry{},
		loginLimiter:          newRateLimiter(loginRateLimitMax, loginRateLimitWindow),
	}
	srv.router = srv.buildRouter()
	srv.startUpdateReloader()
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
	r.Use(s.apiTrackingMiddleware)

	mount := func(router chi.Router) {
		router.Get("/v0/testing/ping", s.handlePing)
		router.Post("/v0/testing/echo", s.handleEcho)

		router.Route("/v0/api", func(r chi.Router) {
			r.Route("/auth", func(authRouter chi.Router) {
				authRouter.Post("/login", s.handleLogin)
				authRouter.Post("/refresh", s.handleRefresh)
				authRouter.Post("/validate", s.handleValidate)
				authRouter.Post("/logout", s.handleLogout)
				authRouter.Get("/register", s.handleRegisterInfo)
				authRouter.Post("/register", s.handleAppRegisterSubmit)
			})
			r.Post("/launcherConfig", s.handleLauncherConfig)
			r.Post("/maintenance", s.handleMaintenance)
			r.Post("/checkUpdates", s.handleCheckUpdates)
			r.Post("/news", s.handleNews)
			r.Post("/feedbackLog", s.handleFeedbackLog)
			r.Get("/feedbackLogs", s.handleFeedbackLogs)

			// Admin API routes for configuration management
			r.Route("/admin", func(adminRouter chi.Router) {
				adminRouter.Get("/launcher", s.handleAdminGetLauncher)
				adminRouter.Put("/launcher", s.handleAdminUpdateLauncher)
				adminRouter.Get("/maintenance", s.handleAdminGetMaintenance)
				adminRouter.Put("/maintenance", s.handleAdminUpdateMaintenance)
				adminRouter.Get("/updates", s.handleAdminGetUpdates)
				adminRouter.Put("/updates", s.handleAdminUpdateUpdates)
				adminRouter.Get("/news", s.handleAdminGetNews)
				adminRouter.Put("/news", s.handleAdminUpdateNews)
				adminRouter.Post("/scanPath", s.handleAdminScanPath)
				adminRouter.Post("/generateUpdates", s.handleAdminGenerateUpdates)
				adminRouter.Post("/uploadFile", s.handleAdminUploadFile)
				adminRouter.Get("/browseDir", s.handleAdminBrowseDir)
				// User management
				adminRouter.Get("/users", s.handleAdminListUsers)
				adminRouter.Post("/users", s.handleAdminCreateUser)
				adminRouter.Put("/users/{id}", s.handleAdminUpdateUser)
				adminRouter.Delete("/users/{id}", s.handleAdminDeleteUser)
				// Statistics
				adminRouter.Get("/stats", s.handleAdminGetStats)
				// Feedback filtering
				adminRouter.Get("/feedbackLogs", s.handleFeedbackLogs)
				adminRouter.Delete("/feedbackLogs/{id}", s.handleAdminDeleteFeedback)
				adminRouter.Get("/feedbackFilterOptions", s.handleFeedbackFilterOptions)
			})
		})

		if s.debug {
			router.Get("/debug/feedback", s.handleFeedbackView)
			router.Get("/debug/feedback.json", s.handleFeedbackJSON)
		}

		router.Get("/app", s.handleAppHome)
		router.Get("/app/", s.handleAppHome)
		router.Get("/app/login", s.handleAppLogin)

		// Static download endpoint for uploaded update assets.
		router.Get("/files/*", s.handleServeFile)
		router.Get("/app/register", s.handleAppRegister)
		router.Post("/app/register", s.handleAppRegisterSubmit)
		router.Get("/app/feedback", s.handleAppFeedback)
		router.Get("/app/admin", s.handleAppAdmin)
		router.Get("/app/dashboard", s.handleAppUserDashboard)

		// App-specific API endpoints (for NekoLcServer UI, separate from NekoLcApi)
		router.Route("/app/api", func(appApiRouter chi.Router) {
			appApiRouter.Post("/login", s.handleAppAPILogin)
			appApiRouter.Post("/logout", s.handleAppAPILogout)
			appApiRouter.Post("/register", s.handleAppRegisterSubmit)
		})
	}

	if s.basePath == "" {
		mount(r)
	} else {
		r.Route(s.basePath, func(sub chi.Router) {
			mount(sub)
		})
	}
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

// prependBasePath adds basePath prefix to a URL if it's a relative path (starts with /).
// Absolute URLs (starting with http:// or https://) are returned unchanged.
func (s *Server) prependBasePath(url string) string {
	trimmed := strings.TrimSpace(url)
	if trimmed == "" {
		return trimmed
	}
	lower := strings.ToLower(trimmed)
	// Absolute URLs don't need basePath
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return trimmed
	}
	// If the URL already starts with basePath, don't double-prepend
	if s.basePath != "" && strings.HasPrefix(trimmed, s.basePath) {
		return trimmed
	}
	// Prepend basePath to relative paths
	if strings.HasPrefix(trimmed, "/") {
		return s.basePath + trimmed
	}
	return s.basePath + "/" + trimmed
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
	cfg := s.currentMaintenanceConfig()
	if cfg != nil && cfg.MaintenanceActive {
		return cfg.MaintenanceInfo, true
	}
	key := platformKey(client)
	if key == "" || cfg == nil {
		return config.MaintenanceInfo{}, false
	}
	if platform, ok := cfg.PlatformSpecific[key]; ok && platform.MaintenanceActive {
		return platform.MaintenanceInfo, true
	}
	return config.MaintenanceInfo{}, false
}

// localizeMaintenanceInfo returns a copy of the maintenance info with localized messages applied.
func (s *Server) localizeMaintenanceInfo(info config.MaintenanceInfo, lang string) config.MaintenanceInfo {
	result := info
	// Apply localized message if available
	if len(info.LocalizedMessages.Langs) > 0 {
		langLower := strings.ToLower(lang)
		if localizedMsg, ok := info.LocalizedMessages.Langs[langLower]; ok && localizedMsg != "" {
			result.Message = localizedMsg
		} else if info.LocalizedMessages.Default != "" {
			result.Message = info.LocalizedMessages.Default
		}
	}
	return result
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

func (s *Server) lookupUser(username string) (*store.User, error) {
	if s.store == nil {
		return nil, errors.New("store not configured")
	}
	return s.store.GetUserByUsername(context.Background(), username)
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

func (s *Server) currentUpdateConfig() *config.UpdateConfig {
	s.updateConfigMu.RLock()
	cfg := s.updateConfig
	s.updateConfigMu.RUnlock()
	return cfg
}

// currentLauncherConfig returns the active launcher configuration. The returned
// pointer must be treated as read-only; updates replace the whole pointer under
// configMu so existing readers keep observing a consistent snapshot.
func (s *Server) currentLauncherConfig() *config.LauncherConfig {
	s.configMu.RLock()
	cfg := s.launcherConfig
	s.configMu.RUnlock()
	return cfg
}

func (s *Server) setLauncherConfig(cfg *config.LauncherConfig) {
	s.configMu.Lock()
	s.launcherConfig = cfg
	s.configMu.Unlock()
}

// currentMaintenanceConfig returns the active maintenance configuration. The
// returned pointer must be treated as read-only.
func (s *Server) currentMaintenanceConfig() *config.MaintenanceConfig {
	s.configMu.RLock()
	cfg := s.maintenanceConfig
	s.configMu.RUnlock()
	return cfg
}

func (s *Server) setMaintenanceConfig(cfg *config.MaintenanceConfig) {
	s.configMu.Lock()
	s.maintenanceConfig = cfg
	s.configMu.Unlock()
}

// currentNewsItems returns the active news items. The returned slice must be
// treated as read-only.
func (s *Server) currentNewsItems() []config.NewsItem {
	s.configMu.RLock()
	items := s.newsItems
	s.configMu.RUnlock()
	return items
}

func (s *Server) setNewsItems(items []config.NewsItem) {
	s.configMu.Lock()
	s.newsItems = items
	s.configMu.Unlock()
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (*authClaims, error) {
	claims, err := s.authenticate(r)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, s.appConfig.Language.Default, "Unauthorized", err.Error())
		return nil, err
	}
	if !strings.EqualFold(claims.Role, "admin") {
		s.writeError(w, http.StatusForbidden, s.appConfig.Language.Default, "Unauthorized", "admin required")
		return nil, errors.New("forbidden")
	}
	return claims, nil
}

type authClaims struct {
	Subject string
	Role    string
}

func (s *Server) authenticate(r *http.Request) (*authClaims, error) {
	bearer := strings.TrimSpace(r.Header.Get("Authorization"))
	if bearer == "" || !strings.HasPrefix(strings.ToLower(bearer), "bearer ") {
		return nil, errors.New("missing bearer token")
	}
	token := strings.TrimSpace(bearer[len("bearer "):])
	parsed, err := s.authService.ParseAccess(token)
	if err != nil {
		return nil, err
	}
	return &authClaims{Subject: parsed.Subject, Role: parsed.Role}, nil
}

func parseLimitOffset(r *http.Request) (int, int) {
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func nullUserID(v sql.NullInt64) int64 {
	if v.Valid {
		return v.Int64
	}
	return 0
}

func jsonRaw(raw []byte) interface{} {
	if len(raw) == 0 {
		return nil
	}
	var obj interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return string(raw)
	}
	return obj
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// apiTrackingMiddleware logs API events for statistics.
func (s *Server) apiTrackingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip tracking for static assets and certain paths
		path := r.URL.Path
		if strings.HasPrefix(path, "/app") && !strings.HasPrefix(path, "/app/api") {
			next.ServeHTTP(w, r)
			return
		}

		// Wrap the response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		// Track API events asynchronously if store is available
		if s.store != nil && strings.HasPrefix(path, "/v0/") {
			go func(endpoint, method string, statusCode int) {
				event := store.APIEvent{
					Endpoint:   endpoint,
					Method:     method,
					StatusCode: statusCode,
					CreatedAt:  time.Now().UTC(),
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.store.SaveAPIEvent(ctx, event); err != nil {
					fmt.Fprintf(os.Stderr, "failed to save API event: %v\n", err)
				}
			}(path, r.Method, wrapped.statusCode)
		}
	})
}

var appHomeTemplate = template.Must(template.New("appHome").Parse(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>NekoLcServer</title>
	<style>
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body { font-family: "Segoe UI", sans-serif; background: linear-gradient(135deg, #0f172a 0%, #1e1b4b 50%, #0f172a 100%); color: #e2e8f0; min-height: 100vh; display: flex; flex-direction: column; align-items: center; justify-content: center; }
		.container { text-align: center; padding: 40px; }
		.logo { font-size: 80px; margin-bottom: 20px; animation: float 3s ease-in-out infinite; }
		@keyframes float { 0%, 100% { transform: translateY(0); } 50% { transform: translateY(-10px); } }
		h1 { font-size: 48px; font-weight: 700; margin-bottom: 16px; background: linear-gradient(120deg, #22d3ee, #818cf8, #f472b6); -webkit-background-clip: text; -webkit-text-fill-color: transparent; background-clip: text; }
		.subtitle { font-size: 20px; color: #94a3b8; margin-bottom: 40px; }
		.links { display: flex; gap: 20px; justify-content: center; flex-wrap: wrap; }
		.btn { padding: 14px 32px; border-radius: 12px; text-decoration: none; font-weight: 600; font-size: 16px; transition: all 0.3s ease; display: inline-flex; align-items: center; gap: 8px; }
		.btn-primary { background: linear-gradient(120deg, #22d3ee, #818cf8); color: #0b1220; }
		.btn-primary:hover { filter: brightness(1.1); transform: translateY(-2px); box-shadow: 0 10px 40px rgba(34, 211, 238, 0.3); }
		.btn-secondary { background: rgba(255, 255, 255, 0.1); color: #e2e8f0; border: 1px solid rgba(255, 255, 255, 0.2); }
		.btn-secondary:hover { background: rgba(255, 255, 255, 0.2); transform: translateY(-2px); }
		.features { margin-top: 60px; display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); gap: 24px; max-width: 900px; }
		.feature { background: rgba(255, 255, 255, 0.05); padding: 24px; border-radius: 16px; border: 1px solid rgba(255, 255, 255, 0.1); }
		.feature h3 { font-size: 18px; margin-bottom: 8px; color: #f8fafc; }
		.feature p { font-size: 14px; color: #94a3b8; line-height: 1.6; }
		.footer { margin-top: 60px; color: #64748b; font-size: 14px; }
		.footer a { color: #818cf8; text-decoration: none; }
		.footer a:hover { text-decoration: underline; }
		.lang-switch { position: absolute; top: 16px; right: 16px; }
		.lang-switch select { padding: 6px 10px; border-radius: 6px; border: 1px solid rgba(255,255,255,0.2); background: rgba(0,0,0,0.3); color: #e2e8f0; cursor: pointer; }
	</style>
</head>
<body>
	<div class="lang-switch">
		<select id="langSelect" onchange="changeLang()">
			<option value="en">English</option>
			<option value="zh-hans">简体中文</option>
			<option value="zh-hant">繁體中文</option>
		</select>
	</div>
	<div class="container">
		<div class="logo">🐱</div>
		<h1>NekoLcServer</h1>
		<p class="subtitle" id="subtitle">A modern launcher server for managing updates, news, and user authentication</p>
		<div class="links">
			<a href="{{.BasePath}}/app/login" class="btn btn-primary" id="link-signin">🔑 Sign In</a>
			<a href="{{.BasePath}}/app/register" class="btn btn-secondary" id="link-register">📝 Register</a>
			<a href="{{.BasePath}}/app/admin" class="btn btn-secondary" id="link-admin">⚙️ Admin Dashboard</a>
		</div>
		<div class="features">
			<div class="feature">
				<h3 id="f1-title">🚀 Update Management</h3>
				<p id="f1-desc">Configure and distribute updates for multiple platforms and architectures with automatic checksum verification.</p>
			</div>
			<div class="feature">
				<h3 id="f2-title">🔧 Maintenance Control</h3>
				<p id="f2-desc">Schedule and manage maintenance windows with platform-specific settings and customizable messages.</p>
			</div>
			<div class="feature">
				<h3 id="f3-title">📰 News System</h3>
				<p id="f3-desc">Publish and manage news items with categories, priorities, and rich content support.</p>
			</div>
		</div>
		<div class="footer">
			<p id="footer">Powered by <a href="https://github.com/moehoshio/NekoLcServer" target="_blank">NekoLcServer</a></p>
		</div>
	</div>
	<script>
		const i18n = {
			'en': { subtitle: 'A modern launcher server for managing updates, news, and user authentication', signin: '🔑 Sign In', register: '📝 Register', admin: '⚙️ Admin Dashboard', f1t: '🚀 Update Management', f1d: 'Configure and distribute updates for multiple platforms and architectures with automatic checksum verification.', f2t: '🔧 Maintenance Control', f2d: 'Schedule and manage maintenance windows with platform-specific settings and customizable messages.', f3t: '📰 News System', f3d: 'Publish and manage news items with categories, priorities, and rich content support.' },
			'zh-hans': { subtitle: '一个现代化的启动器服务器，用于管理更新、新闻和用户认证', signin: '🔑 登录', register: '📝 注册', admin: '⚙️ 管理面板', f1t: '🚀 更新管理', f1d: '配置和分发多平台多架构的更新，支持自动校验。', f2t: '🔧 维护控制', f2d: '安排和管理维护窗口，支持按平台设置和自定义消息。', f3t: '📰 新闻系统', f3d: '发布和管理新闻，支持分类、优先级和富文本内容。' },
			'zh-hant': { subtitle: '一個現代化的啟動器伺服器，用於管理更新、新聞和使用者認證', signin: '🔑 登入', register: '📝 註冊', admin: '⚙️ 管理面板', f1t: '🚀 更新管理', f1d: '配置和分發多平台多架構的更新，支援自動校驗。', f2t: '🔧 維護控制', f2d: '安排和管理維護視窗，支援按平台設定和自訂訊息。', f3t: '📰 新聞系統', f3d: '發佈和管理新聞，支援分類、優先順序和富文本內容。' }
		};
		function getLang() { return localStorage.getItem('lang') || 'en'; }
		function setLang(lang) { localStorage.setItem('lang', lang); applyLang(); }
		function changeLang() { setLang(document.getElementById('langSelect').value); }
		function applyLang() {
			const lang = getLang();
			document.getElementById('langSelect').value = lang;
			const t = i18n[lang] || i18n['en'];
			document.getElementById('subtitle').innerText = t.subtitle;
			document.getElementById('link-signin').innerText = t.signin;
			document.getElementById('link-register').innerText = t.register;
			document.getElementById('link-admin').innerText = t.admin;
			document.getElementById('f1-title').innerText = t.f1t;
			document.getElementById('f1-desc').innerText = t.f1d;
			document.getElementById('f2-title').innerText = t.f2t;
			document.getElementById('f2-desc').innerText = t.f2d;
			document.getElementById('f3-title').innerText = t.f3t;
			document.getElementById('f3-desc').innerText = t.f3d;
		}
		applyLang();
	</script>
</body>
</html>`))

func (s *Server) handleAppHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	appHomeTemplate.Execute(w, map[string]interface{}{"BasePath": s.basePath})
}

var appLoginTemplate = template.Must(template.New("appLogin").Parse(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>NekoLc Login</title>
	<style>
		body { font-family: "Segoe UI", sans-serif; background: #0f172a; color: #e2e8f0; margin: 0; padding: 0; display: flex; align-items: center; justify-content: center; height: 100vh; }
		.card { background: #111827; padding: 32px; border-radius: 12px; box-shadow: 0 10px 30px rgba(0,0,0,0.35); width: 360px; }
		h1 { margin: 0 0 12px 0; font-size: 22px; }
		label { display: block; margin-top: 12px; color: #cbd5e1; }
		input[type="text"], input[type="password"] { width: 100%; padding: 10px; margin-top: 6px; border-radius: 8px; border: 1px solid #1f2937; background: #0b1220; color: #e2e8f0; box-sizing: border-box; }
		button { width: 100%; margin-top: 18px; padding: 12px; border: none; border-radius: 10px; background: linear-gradient(120deg,#22d3ee,#818cf8); color: #0b1220; font-weight: 700; cursor: pointer; }
		button:hover { filter: brightness(1.05); }
		.error { color: #f87171; margin-top: 10px; min-height: 20px; }
		.link { text-align: center; margin-top: 16px; }
		.link a { color: #22d3ee; text-decoration: none; }
		.link a:hover { text-decoration: underline; }
		.lang-switch { position: absolute; top: 16px; right: 16px; }
		.lang-switch select { padding: 6px 10px; border-radius: 6px; border: 1px solid #1f2937; background: #111827; color: #e2e8f0; cursor: pointer; }
		.remember-me { display: flex; align-items: center; gap: 8px; margin-top: 12px; }
		.remember-me input[type="checkbox"] { width: 18px; height: 18px; cursor: pointer; }
		.remember-me label { margin-top: 0; cursor: pointer; }
	</style>
</head>
<body>
	<div class="lang-switch">
		<select id="langSelect" onchange="changeLang()">
			<option value="en">English</option>
			<option value="zh-hans">简体中文</option>
			<option value="zh-hant">繁體中文</option>
		</select>
	</div>
	<div class="card">
		<h1 id="title">Sign in</h1>
		<label id="lbl-username">Username</label>
		<input id="username" type="text" autocomplete="username" />
		<label id="lbl-password">Password</label>
		<input id="password" type="password" autocomplete="current-password" />
		<div class="remember-me">
			<input type="checkbox" id="remember-me" checked />
			<label for="remember-me" id="lbl-remember">Remember me</label>
		</div>
		<button onclick="login()" id="btn-login">Login</button>
		<div class="error" id="error"></div>
		<div class="link"><span id="no-account">Don't have an account?</span> <a href="{{.BasePath}}/app/register" id="link-register">Create one</a></div>
	</div>
	<script>
		const basePath = '{{.BasePath}}';
		const i18n = {
			'en': { title: 'Sign in', username: 'Username', password: 'Password', login: 'Login', noAccount: "Don't have an account?", createOne: 'Create one', loginFailed: 'Login failed', remember: 'Remember me' },
			'zh-hans': { title: '登录', username: '用户名', password: '密码', login: '登录', noAccount: '没有账号？', createOne: '创建账号', loginFailed: '登录失败', remember: '记住我' },
			'zh-hant': { title: '登入', username: '使用者名稱', password: '密碼', login: '登入', noAccount: '沒有帳號？', createOne: '建立帳號', loginFailed: '登入失敗', remember: '記住我' }
		};
		function getLang() { return localStorage.getItem('lang') || 'en'; }
		function setLang(lang) { localStorage.setItem('lang', lang); applyLang(); }
		function changeLang() { setLang(document.getElementById('langSelect').value); }
		function getStorage() { return document.getElementById('remember-me').checked ? localStorage : sessionStorage; }
		function applyLang() {
			const lang = getLang();
			document.getElementById('langSelect').value = lang;
			const t = i18n[lang] || i18n['en'];
			document.getElementById('title').innerText = t.title;
			document.getElementById('lbl-username').innerText = t.username;
			document.getElementById('lbl-password').innerText = t.password;
			document.getElementById('btn-login').innerText = t.login;
			document.getElementById('no-account').innerText = t.noAccount;
			document.getElementById('link-register').innerText = t.createOne;
			document.getElementById('lbl-remember').innerText = t.remember;
		}
		applyLang();
		async function login() {
			const username = document.getElementById('username').value.trim();
			const password = document.getElementById('password').value;
			const rememberMe = document.getElementById('remember-me').checked;
			const lang = getLang();
			const t = i18n[lang] || i18n['en'];
			const res = await fetch(basePath + '/app/api/login', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ username, password })
			});
			if (!res.ok) {
				const data = await res.json().catch(()=>({}));
				document.getElementById('error').innerText = (data.errors && data.errors[0] && data.errors[0].errorMessage) || t.loginFailed;
				return;
			}
			const data = await res.json();
			const storage = rememberMe ? localStorage : sessionStorage;
			storage.setItem('accessToken', data.accessToken);
			storage.setItem('refreshToken', data.refreshToken);
			storage.setItem('userRole', data.role || 'user');
			storage.setItem('username', data.username || username);
			storage.setItem('rememberMe', rememberMe ? 'true' : 'false');
			// Redirect based on role
			if (data.role === 'admin') {
				window.location.href = basePath + '/app/admin';
			} else {
				window.location.href = basePath + '/app/dashboard';
			}
		}
	</script>
</body>
</html>`))

var appRegisterTemplate = template.Must(template.New("appRegister").Parse(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>NekoLc Register</title>
	<style>
		body { font-family: "Segoe UI", sans-serif; background: #0f172a; color: #e2e8f0; margin: 0; padding: 0; display: flex; align-items: center; justify-content: center; height: 100vh; }
		.card { background: #111827; padding: 32px; border-radius: 12px; box-shadow: 0 10px 30px rgba(0,0,0,0.35); width: 360px; }
		h1 { margin: 0 0 12px 0; font-size: 22px; }
		label { display: block; margin-top: 12px; color: #cbd5e1; }
		input { width: 100%; padding: 10px; margin-top: 6px; border-radius: 8px; border: 1px solid #1f2937; background: #0b1220; color: #e2e8f0; box-sizing: border-box; }
		button { width: 100%; margin-top: 18px; padding: 12px; border: none; border-radius: 10px; background: linear-gradient(120deg,#22d3ee,#818cf8); color: #0b1220; font-weight: 700; cursor: pointer; }
		button:hover { filter: brightness(1.05); }
		.error { color: #f87171; margin-top: 10px; min-height: 20px; }
		.success { color: #34d399; margin-top: 10px; min-height: 20px; }
		.link { text-align: center; margin-top: 16px; }
		.link a { color: #22d3ee; text-decoration: none; }
		.link a:hover { text-decoration: underline; }
		.lang-switch { position: absolute; top: 16px; right: 16px; }
		.lang-switch select { padding: 6px 10px; border-radius: 6px; border: 1px solid #1f2937; background: #111827; color: #e2e8f0; cursor: pointer; }
	</style>
</head>
<body>
	<div class="lang-switch">
		<select id="langSelect" onchange="changeLang()">
			<option value="en">English</option>
			<option value="zh-hans">简体中文</option>
			<option value="zh-hant">繁體中文</option>
		</select>
	</div>
	<div class="card">
		<h1 id="title">Create Account</h1>
		<label id="lbl-username">Username</label>
		<input id="username" autocomplete="username" />
		<label id="lbl-password">Password</label>
		<input id="password" type="password" autocomplete="new-password" />
		<label id="lbl-confirm">Confirm Password</label>
		<input id="confirmPassword" type="password" autocomplete="new-password" />
		<button onclick="register()" id="btn-register">Register</button>
		<div class="error" id="error"></div>
		<div class="success" id="success"></div>
		<div class="link"><span id="have-account">Already have an account?</span> <a href="{{.BasePath}}/app/login" id="link-signin">Sign in</a></div>
	</div>
	<script>
		const basePath = '{{.BasePath}}';
		const i18n = {
			'en': { title: 'Create Account', username: 'Username', password: 'Password', confirm: 'Confirm Password', register: 'Register', haveAccount: 'Already have an account?', signIn: 'Sign in', passwordMismatch: 'Passwords do not match', regFailed: 'Registration failed', created: 'Account created! Redirecting to login...' },
			'zh-hans': { title: '创建账号', username: '用户名', password: '密码', confirm: '确认密码', register: '注册', haveAccount: '已有账号？', signIn: '登录', passwordMismatch: '两次密码不一致', regFailed: '注册失败', created: '账号创建成功！正在跳转到登录页面...' },
			'zh-hant': { title: '建立帳號', username: '使用者名稱', password: '密碼', confirm: '確認密碼', register: '註冊', haveAccount: '已有帳號？', signIn: '登入', passwordMismatch: '兩次密碼不一致', regFailed: '註冊失敗', created: '帳號建立成功！正在跳轉到登入頁面...' }
		};
		function getLang() { return localStorage.getItem('lang') || 'en'; }
		function setLang(lang) { localStorage.setItem('lang', lang); applyLang(); }
		function changeLang() { setLang(document.getElementById('langSelect').value); }
		function applyLang() {
			const lang = getLang();
			document.getElementById('langSelect').value = lang;
			const t = i18n[lang] || i18n['en'];
			document.getElementById('title').innerText = t.title;
			document.getElementById('lbl-username').innerText = t.username;
			document.getElementById('lbl-password').innerText = t.password;
			document.getElementById('lbl-confirm').innerText = t.confirm;
			document.getElementById('btn-register').innerText = t.register;
			document.getElementById('have-account').innerText = t.haveAccount;
			document.getElementById('link-signin').innerText = t.signIn;
		}
		applyLang();
		async function register() {
			const username = document.getElementById('username').value.trim();
			const password = document.getElementById('password').value;
			const confirmPassword = document.getElementById('confirmPassword').value;
			const lang = getLang();
			const t = i18n[lang] || i18n['en'];
			document.getElementById('error').innerText = '';
			document.getElementById('success').innerText = '';
			if (password !== confirmPassword) {
				document.getElementById('error').innerText = t.passwordMismatch;
				return;
			}
			const res = await fetch(basePath + '/app/register', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ registerRequest: { username, password } })
			});
			if (!res.ok) {
				const data = await res.json().catch(()=>({}));
				document.getElementById('error').innerText = (data.errors && data.errors[0] && data.errors[0].errorMessage) || t.regFailed;
				return;
			}
			document.getElementById('success').innerText = t.created;
			setTimeout(() => { window.location.href = basePath + '/app/login'; }, 1500);
		}
	</script>
</body>
</html>`))

var appFeedbackTemplate = template.Must(template.New("appFeedback").Parse(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>Feedback Logs</title>
	<style>
		body { font-family: "Segoe UI", sans-serif; background: #0f172a; color: #e2e8f0; margin: 16px; }
		h1 { margin: 0 0 12px 0; }
		table { width: 100%; border-collapse: collapse; }
		th, td { padding: 10px; border-bottom: 1px solid #1f2937; vertical-align: top; }
		th { text-align: left; color: #cbd5e1; }
		.content { white-space: pre-wrap; color: #f8fafc; }
		.code { background: #111827; padding: 8px; border-radius: 6px; font-family: "Cascadia Code", Consolas, monospace; color: #cbd5e1; white-space: pre-wrap; }
		.error { color: #f87171; margin: 8px 0; }
	</style>
</head>
<body>
	<h1>Feedback Logs</h1>
	<div class="error" id="error"></div>
	<table id="table"><thead><tr><th>When</th><th>Content</th><th>Client</th></tr></thead><tbody></tbody></table>
	<script>
		const basePath = '{{.BasePath}}';
		function escapeHtml(str) {
			if (!str) return '';
			return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#039;');
		}
		async function loadLogs() {
			const token = localStorage.getItem('accessToken');
			if (!token) { window.location.href = basePath + '/app/login'; return; }
			const res = await fetch(basePath + '/v0/api/feedbackLogs?limit=50', { headers: { 'Authorization': 'Bearer ' + token } });
			if (res.status === 401 || res.status === 403) { localStorage.removeItem('accessToken'); window.location.href = basePath + '/app/login'; return; }
			if (!res.ok) { document.getElementById('error').innerText = 'Failed to load logs'; return; }
			const data = await res.json();
			const tbody = document.querySelector('#table tbody');
			tbody.innerHTML = '';
			for (const item of (data.feedbackLogs || [])) {
				const tr = document.createElement('tr');
				const when = document.createElement('td'); when.innerText = item.receivedAt || ''; tr.appendChild(when);
				const content = document.createElement('td'); content.className='content'; content.innerText = item.content || ''; tr.appendChild(content);
				const info = document.createElement('td');
				const codeDiv = document.createElement('div');
				codeDiv.className = 'code';
				codeDiv.textContent = JSON.stringify(item.clientInfo || {}, null, 2);
				info.appendChild(codeDiv);
				tr.appendChild(info);
				tbody.appendChild(tr);
			}
		}
		loadLogs();
	</script>
</body>
</html>`))

func (s *Server) handleAppLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	appLoginTemplate.Execute(w, map[string]interface{}{"BasePath": s.basePath})
}

func (s *Server) handleAppRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	appRegisterTemplate.Execute(w, map[string]interface{}{"BasePath": s.basePath})
}

func (s *Server) handleAppRegisterSubmit(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil || !s.authService.Enabled() {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Authentication is disabled")
		return
	}
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Account store not configured")
		return
	}
	var payload RegisterPayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.languageFromPreferences(payload.Preferences), "InvalidRequest", err.Error())
		return
	}
	lang := s.languageFromPreferences(payload.Preferences)
	username := strings.TrimSpace(payload.RegisterRequest.Username)
	password := strings.TrimSpace(payload.RegisterRequest.Password)
	if username == "" || password == "" {
		s.writeError(w, http.StatusBadRequest, lang, "InvalidRequest", "username and password are required")
		return
	}
	if len(username) < 3 || len(username) > 50 {
		s.writeError(w, http.StatusBadRequest, lang, "InvalidRequest", "username must be 3-50 characters")
		return
	}
	if len(password) < 6 {
		s.writeError(w, http.StatusBadRequest, lang, "InvalidRequest", "password must be at least 6 characters")
		return
	}
	// Check if username already exists
	_, err := s.store.GetUserByUsername(r.Context(), username)
	if err == nil {
		s.writeError(w, http.StatusConflict, lang, "Conflict", "username already exists")
		return
	}
	if err != store.ErrNotFound {
		s.writeError(w, http.StatusInternalServerError, lang, "InternalError", err.Error())
		return
	}
	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, lang, "InternalError", err.Error())
		return
	}
	// Create user with "user" role (not admin)
	userID, err := s.store.CreateUser(r.Context(), username, string(hash), "user")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, lang, "InternalError", err.Error())
		return
	}
	resp := RegisterResponseBody{Meta: s.meta()}
	resp.RegisterResponse.UserID = userID
	resp.RegisterResponse.Username = username
	s.writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleAppFeedback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	appFeedbackTemplate.Execute(w, map[string]interface{}{"BasePath": s.basePath})
}

func (s *Server) diffFilesFromPath(path string, isCore bool) []UpdateFileResponse {
	if path == "" {
		return nil
	}
	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		entry := config.DownloadEntry{URL: path, DownloadMeta: config.DownloadMeta{HashAlgorithm: "sha256"}}
		return s.filesFromEntry(entry, isCore)
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
		entry := config.DownloadEntry{URL: trimmed, DownloadMeta: config.DownloadMeta{HashAlgorithm: "sha256"}}
		files = append(files, s.filesFromEntry(entry, isCore)...)
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

func normalizeBasePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "/" {
		return ""
	}
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return ""
	}
	return "/" + trimmed
}

func normalizeNewsItems(cfg *config.NewsConfig) []config.NewsItem {
	if cfg == nil || len(cfg.Items) == 0 {
		return nil
	}
	items := make([]config.NewsItem, len(cfg.Items))
	copy(items, cfg.Items)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority == items[j].Priority {
			return strings.Compare(items[i].PublishTime, items[j].PublishTime) > 0
		}
		return items[i].Priority > items[j].Priority
	})
	return items
}

type feedbackEntry struct {
	ReceivedAt string                 `json:"receivedAt"`
	ClientInfo map[string]interface{} `json:"clientInfo"`
	Timestamp  json.Number            `json:"timestamp"`
	Content    string                 `json:"content"`
}

func (s *Server) loadFeedbackEntries(limit int) ([]feedbackEntry, error) {
	if s.store != nil {
		items, err := s.store.ListFeedback(context.Background(), limit, 0)
		if err != nil {
			return nil, err
		}
		entries := make([]feedbackEntry, 0, len(items))
		for _, e := range items {
			var client map[string]interface{}
			if v := jsonRaw(e.ClientInfo); v != nil {
				if m, ok := v.(map[string]interface{}); ok {
					client = m
				} else {
					client = map[string]interface{}{"value": v}
				}
			}
			entries = append(entries, feedbackEntry{
				ReceivedAt: e.ReceivedAt.Format(time.RFC3339),
				ClientInfo: client,
				Timestamp:  json.Number(fmt.Sprintf("%d", e.Timestamp)),
				Content:    e.Content,
			})
		}
		return entries, nil
	}

	file, err := os.Open(s.feedbackLogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	entries := []feedbackEntry{}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var entry feedbackEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

var feedbackTemplate = template.Must(template.New("feedback").Funcs(template.FuncMap{
	"formatTime": func(raw string) string {
		if raw == "" {
			return ""
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t.Local().Format("2006-01-02 15:04:05")
		}
		return raw
	},
	"jsonPretty": func(v interface{}) string {
		if v == nil {
			return "{}"
		}
		buf, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return "{}"
		}
		return string(buf)
	},
}).Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Feedback Log</title>
  <style>
    body { font-family: "Segoe UI", sans-serif; margin: 20px; background: #0f172a; color: #e2e8f0; }
    h1 { margin-bottom: 8px; }
    .meta { color: #94a3b8; margin-bottom: 16px; }
    table { width: 100%; border-collapse: collapse; }
    th, td { padding: 10px; border-bottom: 1px solid #1e293b; vertical-align: top; }
    th { text-align: left; color: #cbd5e1; }
    .content { white-space: pre-wrap; color: #f8fafc; }
    .code { background: #1e293b; padding: 8px; border-radius: 6px; font-family: "Cascadia Code", Consolas, monospace; color: #cbd5e1; white-space: pre-wrap; }
    .timestamp { color: #a5b4fc; }
  </style>
</head>
<body>
  <h1>Feedback Log</h1>
  <div class="meta">Showing newest first. Source: {{.File}}</div>
  {{if not .Entries}}
    <div>No feedback entries found.</div>
  {{else}}
    <table>
      <thead>
        <tr><th>When</th><th>Content</th><th>Client Info</th></tr>
      </thead>
      <tbody>
      {{range .Entries}}
        <tr>
          <td class="timestamp">{{formatTime .ReceivedAt}}<br/><small>ts={{.Timestamp}}</small></td>
          <td class="content">{{.Content}}</td>
          <td><div class="code">{{jsonPretty .ClientInfo}}</div></td>
        </tr>
      {{end}}
      </tbody>
    </table>
  {{end}}
</body>
</html>`))

func (s *Server) handleFeedbackView(w http.ResponseWriter, r *http.Request) {
	entries, err := s.loadFeedbackEntries(200)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := feedbackTemplate.Execute(w, map[string]interface{}{
		"Entries": entries,
		"File":    s.feedbackLogPath,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "render feedback view: %v\n", err)
	}
}

func (s *Server) handleFeedbackJSON(w http.ResponseWriter, r *http.Request) {
	entries, err := s.loadFeedbackEntries(200)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
		"count":   len(entries),
	})
}

func (s *Server) startUpdateReloader() {
	if strings.TrimSpace(s.updateConfigPath) == "" {
		return
	}
	const interval = 10 * time.Second
	go func() {
		lastMod := modTimeSafe(s.updateConfigPath)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			current := modTimeSafe(s.updateConfigPath)
			if current.IsZero() || !current.After(lastMod) {
				continue
			}
			cfg, err := config.LoadUpdates(s.updateConfigPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "reload updates failed: %v\n", err)
				continue
			}
			s.updateConfigMu.Lock()
			s.updateConfig = cfg
			s.dirCacheMu.Lock()
			s.dirCache = map[string]dirCacheEntry{}
			s.dirCacheMu.Unlock()
			s.updateConfigMu.Unlock()
			lastMod = current
			fmt.Fprintf(os.Stderr, "reloaded updates config at %s\n", time.Now().Format(time.RFC3339))
		}
	}()
}

func modTimeSafe(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// saveLauncherConfig writes the current launcher configuration to database.
func (s *Server) saveLauncherConfig() error {
	cfg := s.currentLauncherConfig()
	// Try to save to database first
	if s.store != nil {
		data, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		if err := s.store.SetConfig(context.Background(), store.ConfigKeyLauncher, data); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save launcher config to database: %v\n", err)
		}
	}
	return nil
}

// saveMaintenanceConfig writes the current maintenance configuration to database and/or file.
func (s *Server) saveMaintenanceConfig() error {
	cfg := s.currentMaintenanceConfig()
	// Try to save to database first
	if s.store != nil {
		data, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		if err := s.store.SetConfig(context.Background(), store.ConfigKeyMaintenance, data); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save maintenance config to database: %v\n", err)
		}
	}
	// Also save to file if path is configured
	if s.maintenanceConfigPath != "" {
		return saveJSONFile(s.maintenanceConfigPath, cfg)
	}
	return nil
}

// saveUpdatesConfig writes the current updates configuration to database and/or file.
func (s *Server) saveUpdatesConfig() error {
	s.updateConfigMu.RLock()
	cfg := s.updateConfig
	s.updateConfigMu.RUnlock()

	// Try to save to database first
	if s.store != nil {
		data, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		if err := s.store.SetConfig(context.Background(), store.ConfigKeyUpdates, data); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save updates config to database: %v\n", err)
		}
	}
	// Also save to file if path is configured
	if s.updateConfigPath != "" {
		return saveJSONFile(s.updateConfigPath, cfg)
	}
	return nil
}

// saveNewsConfig writes the news configuration to database and/or file.
func (s *Server) saveNewsConfig(cfg *config.NewsConfig) error {
	// Try to save to database first
	if s.store != nil {
		data, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		if err := s.store.SetConfig(context.Background(), store.ConfigKeyNews, data); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save news config to database: %v\n", err)
		}
	}
	// Also save to file if path is configured
	if s.newsConfigPath != "" {
		return saveJSONFile(s.newsConfigPath, cfg)
	}
	return nil
}

func saveJSONFile(path string, data interface{}) error {
	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

var appAdminTemplate = template.Must(template.New("appAdmin").Parse(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>NekoLc Admin Dashboard</title>
	<style>
		* { box-sizing: border-box; }
		body { font-family: "Segoe UI", sans-serif; background: #0f172a; color: #e2e8f0; margin: 0; padding: 0; min-height: 100vh; }
		.header { background: #111827; padding: 16px 24px; display: flex; align-items: center; justify-content: space-between; border-bottom: 1px solid #1f2937; }
		.header h1 { margin: 0; font-size: 20px; }
		.header .user { display: flex; align-items: center; gap: 12px; }
		.header button { background: #374151; border: none; padding: 8px 16px; border-radius: 6px; color: #e2e8f0; cursor: pointer; }
		.header button:hover { background: #4b5563; }
		.container { display: flex; min-height: calc(100vh - 60px); }
		.sidebar { width: 220px; background: #111827; padding: 16px; border-right: 1px solid #1f2937; }
		.sidebar button { width: 100%; text-align: left; padding: 12px 16px; margin-bottom: 8px; background: transparent; border: none; border-radius: 8px; color: #cbd5e1; cursor: pointer; font-size: 14px; }
		.sidebar button:hover, .sidebar button.active { background: #1f2937; color: #f8fafc; }
		.main { flex: 1; padding: 24px; overflow-y: auto; }
		.card { background: #111827; border-radius: 12px; padding: 24px; margin-bottom: 24px; border: 1px solid #1f2937; }
		.card h2 { margin: 0 0 16px 0; font-size: 18px; color: #f8fafc; }
		.form-group { margin-bottom: 16px; }
		.form-group label { display: block; margin-bottom: 6px; color: #94a3b8; font-size: 14px; }
		.form-group input, .form-group textarea, .form-group select { width: 100%; padding: 10px 12px; border-radius: 8px; border: 1px solid #374151; background: #0b1220; color: #e2e8f0; font-size: 14px; }
		.form-group input:focus, .form-group textarea:focus { outline: none; border-color: #818cf8; }
		.form-group textarea { resize: vertical; min-height: 80px; font-family: "Cascadia Code", Consolas, monospace; }
		.form-row { display: flex; gap: 16px; }
		.form-row .form-group { flex: 1; }
		.toggle { display: flex; align-items: center; gap: 12px; }
		.toggle input[type="checkbox"] { width: 44px; height: 24px; appearance: none; background: #374151; border-radius: 12px; position: relative; cursor: pointer; transition: background 0.2s; }
		.toggle input[type="checkbox"]:checked { background: #22d3ee; }
		.toggle input[type="checkbox"]::before { content: ''; position: absolute; top: 2px; left: 2px; width: 20px; height: 20px; background: #f8fafc; border-radius: 50%; transition: transform 0.2s; }
		.toggle input[type="checkbox"]:checked::before { transform: translateX(20px); }
		.btn { padding: 10px 20px; border: none; border-radius: 8px; font-weight: 600; cursor: pointer; font-size: 14px; transition: all 0.2s; }
		.btn-primary { background: linear-gradient(120deg, #22d3ee, #818cf8); color: #0b1220; }
		.btn-primary:hover { filter: brightness(1.1); }
		.btn-secondary { background: #374151; color: #e2e8f0; }
		.btn-secondary:hover { background: #4b5563; }
		.btn-danger { background: #dc2626; color: #fff; }
		.btn-danger:hover { background: #ef4444; }
		.actions { display: flex; gap: 12px; margin-top: 16px; }
		.message { padding: 12px 16px; border-radius: 8px; margin-bottom: 16px; }
		.message.success { background: #065f46; color: #a7f3d0; }
		.message.error { background: #7f1d1d; color: #fca5a5; }
		.hidden { display: none; }
		.news-item { background: #1e293b; padding: 16px; border-radius: 8px; margin-bottom: 12px; }
		.news-item .header-row { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
		.news-item h3 { margin: 0; font-size: 16px; }
		.platform-section { background: #1e293b; padding: 16px; border-radius: 8px; margin-bottom: 12px; }
		.platform-section h3 { margin: 0 0 12px 0; font-size: 16px; }
		.arch-item { background: #0f172a; padding: 12px; border-radius: 6px; margin-bottom: 8px; }
		.arch-item h4 { margin: 0 0 8px 0; font-size: 14px; color: #94a3b8; }
		.loading { display: flex; align-items: center; justify-content: center; padding: 40px; color: #94a3b8; }
		.tabs { display: flex; gap: 8px; margin-bottom: 16px; }
		.tabs button { padding: 8px 16px; background: transparent; border: 1px solid #374151; border-radius: 6px; color: #cbd5e1; cursor: pointer; }
		.tabs button.active { background: #374151; color: #f8fafc; }
		.fb-content { white-space: pre-wrap; word-break: break-word; overflow-wrap: anywhere; max-width: 520px; }
		.fb-content.collapsed { display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; max-height: 4.5em; }
		.fb-toggle { background: none; border: none; color: #38bdf8; cursor: pointer; padding: 0; margin-top: 4px; font-size: 12px; }
		.client-info-pre { white-space: pre-wrap; word-break: break-word; overflow-wrap: anywhere; max-width: 520px; background:#0b1220; padding:8px; border-radius:6px; font-size:12px; margin:0; }
	</style>
</head>
<body>
	<div class="header">
		<h1 id="admin-title">🐱 NekoLc Admin Dashboard</h1>
		<div class="user">
			<div class="lang-switch" style="margin-right:12px;">
				<select id="langSelect" onchange="changeLang()" style="padding:6px 10px;border-radius:6px;border:1px solid #374151;background:#1f2937;color:#e2e8f0;cursor:pointer;">
					<option value="en">English</option>
					<option value="zh-hans">简体中文</option>
					<option value="zh-hant">繁體中文</option>
				</select>
			</div>
			<span id="username">Admin</span>
			<button onclick="logout()" id="btn-logout">Logout</button>
		</div>
	</div>
	<div class="container">
		<div class="sidebar">
			<button class="active" onclick="showSection('stats')" id="nav-stats">📊 Statistics</button>
			<button onclick="showSection('launcher')" id="nav-launcher">🚀 Launcher</button>
			<button onclick="showSection('maintenance')" id="nav-maintenance">🔧 Maintenance</button>
			<button onclick="showSection('updates')" id="nav-updates">📦 Updates</button>
			<button onclick="showSection('news')" id="nav-news">📰 News</button>
			<button onclick="showSection('users')" id="nav-users">👥 Users</button>
			<button onclick="showSection('feedback')" id="nav-feedback">💬 Feedback</button>
		</div>
		<div class="main">
			<div id="message" class="message hidden"></div>
			
			<!-- Statistics Section -->
			<div id="section-stats" class="section">
				<div class="card">
					<h2 id="stats-title">📊 Statistics Overview</h2>
					<p style="color: #94a3b8; margin-bottom: 16px;" id="stats-desc">View server usage statistics and analytics.</p>
					<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; margin-bottom: 24px;">
						<div style="background: linear-gradient(135deg, #0ea5e9 0%, #2563eb 100%); padding: 24px; border-radius: 12px;">
							<div style="font-size: 14px; opacity: 0.9;" id="lbl-total-requests">Total Requests</div>
							<div style="font-size: 32px; font-weight: 700;" id="stat-total-requests">-</div>
						</div>
						<div style="background: linear-gradient(135deg, #22c55e 0%, #16a34a 100%); padding: 24px; border-radius: 12px;">
							<div style="font-size: 14px; opacity: 0.9;" id="lbl-today-requests">Today's Requests</div>
							<div style="font-size: 32px; font-weight: 700;" id="stat-today-requests">-</div>
						</div>
						<div style="background: linear-gradient(135deg, #a855f7 0%, #7c3aed 100%); padding: 24px; border-radius: 12px;">
							<div style="font-size: 14px; opacity: 0.9;" id="lbl-total-users">Total Users</div>
							<div style="font-size: 32px; font-weight: 700;" id="stat-total-users">-</div>
						</div>
						<div style="background: linear-gradient(135deg, #f59e0b 0%, #d97706 100%); padding: 24px; border-radius: 12px;">
							<div style="font-size: 14px; opacity: 0.9;" id="lbl-total-feedback">Total Feedback</div>
							<div style="font-size: 32px; font-weight: 700;" id="stat-total-feedback">-</div>
						</div>
					</div>
					<div class="actions" style="margin-bottom: 24px;">
						<button class="btn btn-secondary" onclick="loadStats()">🔄 Refresh</button>
						<select id="stats-days" onchange="loadStats()" style="padding: 8px 12px; border-radius: 6px; border: 1px solid #374151; background: #1f2937; color: #e2e8f0;">
							<option value="7">Last 7 days</option>
							<option value="14">Last 14 days</option>
							<option value="30">Last 30 days</option>
						</select>
						<label style="display:flex;align-items:center;gap:8px;color:#94a3b8;font-size:14px;">
							<input type="checkbox" id="stats-autorefresh" onchange="toggleStatsAutoRefresh()" /> <span id="lbl-autorefresh">Auto-refresh (live)</span>
						</label>
					</div>
				</div>
				<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(400px, 1fr)); gap: 20px;">
					<div class="card">
						<h3 style="margin-bottom: 16px;">📈 Daily Requests</h3>
						<div id="daily-chart" style="height: 200px; background: #0f172a; border-radius: 8px; padding: 16px; display: flex; align-items: flex-end; gap: 8px; justify-content: space-around;">
							<div style="color: #94a3b8; text-align: center;">Loading...</div>
						</div>
					</div>
					<div class="card">
						<h3 style="margin-bottom: 16px;">🔗 Top Endpoints</h3>
						<div id="endpoints-list" style="max-height: 200px; overflow-y: auto;">
							<div style="color: #94a3b8;">Loading...</div>
						</div>
					</div>
				</div>
				<div class="card" style="margin-top: 20px;">
					<h3 style="margin-bottom: 16px;">💻 Platform Distribution</h3>
					<div id="platforms-list" style="display: flex; flex-wrap: wrap; gap: 12px;">
						<div style="color: #94a3b8;">Loading...</div>
					</div>
				</div>
			</div>
			
			<!-- Launcher Section -->
			<div id="section-launcher" class="section hidden">
				<div class="card">
					<h2 id="launcher-title">Launcher Configuration</h2>
					<p style="color: #94a3b8; margin-bottom: 16px;" id="launcher-desc">Configure launcher settings that clients receive.</p>
					<div class="form-group">
						<label for="launcher-hosts" id="lbl-hosts">Hosts (one per line)</label>
						<textarea id="launcher-hosts" rows="3" placeholder="https://api.example.com"></textarea>
					</div>
					<div class="form-row">
						<div class="form-group">
							<label for="launcher-retry-interval" id="lbl-retry-interval">Retry Interval (seconds)</label>
							<input type="number" id="launcher-retry-interval" min="1" />
						</div>
						<div class="form-group">
							<label for="launcher-max-retry" id="lbl-max-retry">Max Retry Count</label>
							<input type="number" id="launcher-max-retry" min="0" />
						</div>
					</div>
					<h3 style="margin-top: 24px; margin-bottom: 16px; font-size: 16px;" id="ws-title">WebSocket</h3>
					<div class="form-group toggle">
						<input type="checkbox" id="launcher-ws-enable" />
						<label for="launcher-ws-enable" id="lbl-ws-enable">Enable WebSocket</label>
					</div>
					<div class="form-row">
						<div class="form-group">
							<label for="launcher-ws-host" id="lbl-ws-host">WebSocket Host</label>
							<input type="text" id="launcher-ws-host" placeholder="wss://ws.example.com" />
						</div>
						<div class="form-group">
							<label for="launcher-ws-heartbeat" id="lbl-ws-heartbeat">Heartbeat Interval (seconds)</label>
							<input type="number" id="launcher-ws-heartbeat" min="1" />
						</div>
					</div>
					<h3 style="margin-top: 24px; margin-bottom: 16px; font-size: 16px;" id="security-title">Security</h3>
					<div class="form-group toggle">
						<input type="checkbox" id="launcher-auth-enable" />
						<label for="launcher-auth-enable" id="lbl-auth-enable">Enable Authentication</label>
					</div>
					<div class="form-row">
						<div class="form-group">
							<label for="launcher-token-exp" id="lbl-token-exp">Token Expiration (seconds)</label>
							<input type="number" id="launcher-token-exp" min="1" />
						</div>
						<div class="form-group">
							<label for="launcher-refresh-exp" id="lbl-refresh-exp">Refresh Token Expiration (days)</label>
							<input type="number" id="launcher-refresh-exp" min="1" />
						</div>
					</div>
					<div class="form-row">
						<div class="form-group">
							<label for="launcher-login-url" id="lbl-login-url">Login URL</label>
							<input type="text" id="launcher-login-url" placeholder="/v0/api/auth/login" />
						</div>
						<div class="form-group">
							<label for="launcher-logout-url" id="lbl-logout-url">Logout URL</label>
							<input type="text" id="launcher-logout-url" placeholder="/v0/api/auth/logout" />
						</div>
					</div>
					<div class="form-row">
						<div class="form-group">
							<label for="launcher-refresh-url" id="lbl-refresh-url">Refresh URL</label>
							<input type="text" id="launcher-refresh-url" placeholder="/v0/api/auth/refresh" />
						</div>
						<div class="form-group">
							<label for="launcher-register-url" id="lbl-register-url">Register URL (API)</label>
							<input type="text" id="launcher-register-url" placeholder="/v0/api/auth/register" />
						</div>
					</div>
					<div class="form-row">
						<div class="form-group">
							<label for="launcher-register-ui-url" id="lbl-register-ui-url">Register URL (UI)</label>
							<input type="text" id="launcher-register-ui-url" placeholder="/app/register" />
						</div>
						<div class="form-group"></div>
					</div>
					<div class="actions">
						<button class="btn btn-primary" onclick="saveLauncher()" id="btn-save-launcher">Save Changes</button>
						<button class="btn btn-secondary" onclick="loadLauncher()" id="btn-reload-launcher">Reload</button>
					</div>
				</div>
			</div>
			
			<!-- Maintenance Section -->
			<div id="section-maintenance" class="section hidden">
				<div class="card">
					<h2>Maintenance Configuration</h2>
					<div class="form-group toggle">
						<input type="checkbox" id="maint-active" />
						<label for="maint-active">Maintenance Active</label>
					</div>
					<div class="form-row">
						<div class="form-group">
							<label for="maint-status">Status</label>
							<select id="maint-status">
								<option value="none">None</option>
								<option value="scheduled">Scheduled</option>
								<option value="progress">In Progress</option>
							</select>
						</div>
						<div class="form-group">
							<label for="maint-poster">Poster URL</label>
							<input type="text" id="maint-poster" placeholder="https://..." />
						</div>
					</div>
					<div class="form-group">
						<label for="maint-message">Message (Default)</label>
						<textarea id="maint-message" rows="2"></textarea>
					</div>
					<div class="form-group">
						<label>Localized Messages</label>
						<p style="color: #94a3b8; margin-bottom: 8px; font-size: 13px;">Add messages for different languages. The default message above will be used if no localized message is available.</p>
						<div id="localized-messages-list"></div>
						<button type="button" class="btn btn-secondary" style="margin-top: 8px;" onclick="addLocalizedMessage()">+ Add Language</button>
					</div>
					<div class="form-row">
						<div class="form-group">
							<label for="maint-start">Start Time</label>
							<input type="datetime-local" id="maint-start" />
						</div>
						<div class="form-group">
							<label for="maint-end">Expected End Time</label>
							<input type="datetime-local" id="maint-end" />
						</div>
					</div>
					<div class="form-group">
						<label for="maint-link">Announcement Link</label>
						<input type="text" id="maint-link" placeholder="https://..." />
					</div>
					<div class="actions">
						<button class="btn btn-primary" onclick="saveMaintenance()">Save Changes</button>
						<button class="btn btn-secondary" onclick="loadMaintenance()">Reload</button>
					</div>
				</div>
				<div class="card">
					<h2>Platform-Specific Maintenance</h2>
					<p style="color: #94a3b8; margin-bottom: 16px;">Configure maintenance settings per platform (e.g., windows-x64, linux-arm64).</p>
					<div id="platform-maintenance-list"></div>
					<button class="btn btn-secondary" style="margin-top: 16px;" onclick="addPlatformMaintenance()">+ Add Platform</button>
				</div>
			</div>
			
			<!-- Updates Section -->
			<div id="section-updates" class="section hidden">
				<div class="card">
					<h2>Auto-Generate from Directory</h2>
					<p style="color: #94a3b8; margin-bottom: 16px;">Scan a directory to automatically generate update files with checksums.</p>
					<div class="form-row">
						<div class="form-group">
							<label for="scan-path">Directory Path</label>
							<div style="display:flex;gap:8px;">
								<input type="text" id="scan-path" placeholder="./updates/windows-x64" style="flex:1;" />
								<button type="button" class="btn btn-secondary" onclick="openDirBrowser()">📁 Browse</button>
							</div>
						</div>
						<div class="form-group">
							<label for="scan-baseurl">Base URL</label>
							<input type="text" id="scan-baseurl" placeholder="https://example.com/updates/" />
						</div>
					</div>
					<div class="form-row">
						<div class="form-group">
							<label for="scan-platform">Platform</label>
							<select id="scan-platform">
								<option value="windows">Windows</option>
								<option value="linux">Linux</option>
								<option value="macos">macOS</option>
							</select>
						</div>
						<div class="form-group">
							<label for="scan-arch">Architecture</label>
							<select id="scan-arch">
								<option value="x64">x64</option>
								<option value="arm64">ARM64</option>
								<option value="x86">x86</option>
							</select>
						</div>
						<div class="form-group">
							<label for="scan-type">Type</label>
							<select id="scan-type">
								<option value="core">Core</option>
								<option value="resource">Resource</option>
							</select>
						</div>
					</div>
					<div class="actions">
						<button class="btn btn-primary" onclick="generateUpdates()">Generate & Save</button>
						<button class="btn btn-secondary" onclick="scanPath()">Scan Only</button>
					</div>
					<div id="scan-results" style="margin-top: 16px;"></div>
				</div>
				<div class="card">
					<h2>Upload Update File</h2>
					<p style="color: #94a3b8; margin-bottom: 16px;">Upload a file to be hosted by this server. The download URL is generated automatically from the current site URL.</p>
					<div class="form-row">
						<div class="form-group">
							<label for="upload-file">File</label>
							<input type="file" id="upload-file" />
						</div>
						<div class="form-group">
							<label for="upload-subdir">Sub-directory (optional)</label>
							<input type="text" id="upload-subdir" placeholder="windows-x64" />
						</div>
					</div>
					<div class="actions">
						<button class="btn btn-primary" onclick="uploadFile()">Upload</button>
					</div>
					<div id="upload-results" style="margin-top: 16px;"></div>
				</div>
			</div>

			<!-- Directory browser modal -->
			<div id="dir-browser" class="hidden" style="position:fixed;inset:0;background:rgba(0,0,0,0.6);z-index:50;display:flex;align-items:center;justify-content:center;">
				<div style="background:#111827;border:1px solid #1f2937;border-radius:12px;width:min(640px,92vw);max-height:80vh;display:flex;flex-direction:column;padding:20px;">
					<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;">
						<h3 style="margin:0;">📁 Select Directory</h3>
						<button class="btn btn-secondary" onclick="closeDirBrowser()">Close</button>
					</div>
					<div id="dir-browser-current" style="color:#94a3b8;font-size:13px;margin-bottom:8px;font-family:monospace;">/</div>
					<div id="dir-browser-list" style="flex:1;overflow-y:auto;background:#0f172a;border-radius:8px;padding:8px;min-height:200px;"></div>
					<div class="actions">
						<button class="btn btn-primary" onclick="chooseCurrentDir()">Use This Directory</button>
					</div>
				</div>
			</div>
			
			<!-- News Section -->
			<div id="section-news" class="section hidden">
				<div class="card">
					<h2>News Items</h2>
					<div class="form-row" style="margin-bottom:12px;flex-wrap:wrap;">
						<div class="form-group" style="flex:2;min-width:180px;">
							<label id="lbl-news-search">Search</label>
							<input type="text" id="news-search" placeholder="Search title, summary, category..." oninput="renderNews()" />
						</div>
						<div class="form-group" style="flex:1;min-width:150px;">
							<label id="lbl-news-sort">Sort by</label>
							<select id="news-sort" onchange="renderNews()">
								<option value="priority-desc">Priority (high→low)</option>
								<option value="priority-asc">Priority (low→high)</option>
								<option value="title-asc">Title (A→Z)</option>
								<option value="title-desc">Title (Z→A)</option>
								<option value="publish-desc">Publish time (newest)</option>
								<option value="publish-asc">Publish time (oldest)</option>
							</select>
						</div>
					</div>
					<div id="news-list"></div>
					<button class="btn btn-secondary" style="margin-top: 16px;" onclick="addNewsItem()">+ Add News Item</button>
					<div class="actions">
						<button class="btn btn-primary" onclick="saveNews()">Save Changes</button>
						<button class="btn btn-secondary" onclick="loadNews()">Reload</button>
					</div>
				</div>
			</div>
			
			<!-- Users Section -->
			<div id="section-users" class="section hidden">
				<div class="card">
					<h2>User Management</h2>
					<p style="color: #94a3b8; margin-bottom: 16px;">Manage user accounts and permissions.</p>
					<div class="actions" style="margin-bottom: 16px;">
						<button class="btn btn-primary" onclick="showAddUserForm()">+ Add User</button>
						<button class="btn btn-secondary" onclick="loadUsers()">Reload</button>
					</div>
					<div id="add-user-form" style="display:none; margin-bottom: 16px; padding: 16px; background: #0b1220; border-radius: 8px;">
						<h3 style="margin-bottom: 12px;">New User</h3>
						<div class="form-row">
							<div class="form-group">
								<label>Username</label>
								<input type="text" id="new-user-username" placeholder="Username" />
							</div>
							<div class="form-group">
								<label>Password</label>
								<input type="password" id="new-user-password" placeholder="Password" />
							</div>
						</div>
						<div class="form-group">
							<label>Role</label>
							<select id="new-user-role">
								<option value="user">User</option>
								<option value="admin">Admin</option>
							</select>
						</div>
						<div class="actions">
							<button class="btn btn-primary" onclick="createUser()">Create User</button>
							<button class="btn btn-secondary" onclick="hideAddUserForm()">Cancel</button>
						</div>
					</div>
					<table class="data-table" style="width: 100%;">
						<thead>
							<tr>
								<th style="text-align: left; padding: 10px; border-bottom: 1px solid #374151;">ID</th>
								<th style="text-align: left; padding: 10px; border-bottom: 1px solid #374151;">Username</th>
								<th style="text-align: left; padding: 10px; border-bottom: 1px solid #374151;">Role</th>
								<th style="text-align: left; padding: 10px; border-bottom: 1px solid #374151;">Created</th>
								<th style="text-align: left; padding: 10px; border-bottom: 1px solid #374151;">Actions</th>
							</tr>
						</thead>
						<tbody id="users-table-body">
							<tr><td colspan="5" style="padding: 20px; text-align: center; color: #94a3b8;">Loading users...</td></tr>
						</tbody>
					</table>
				</div>
			</div>
			
			<!-- Feedback Section -->
			<div id="section-feedback" class="section hidden">
				<div class="card">
					<h2>Feedback Logs</h2>
					<div class="form-row" style="margin-bottom: 16px; flex-wrap: wrap;">
						<div class="form-group" style="flex: 1; min-width: 150px;">
							<label for="filter-coreVersion">Core Version</label>
							<select id="filter-coreVersion" onchange="applyFeedbackFilters()">
								<option value="">All</option>
							</select>
						</div>
						<div class="form-group" style="flex: 1; min-width: 150px;">
							<label for="filter-resourceVersion">Resource Version</label>
							<select id="filter-resourceVersion" onchange="applyFeedbackFilters()">
								<option value="">All</option>
							</select>
						</div>
						<div class="form-group" style="flex: 1; min-width: 150px;">
							<label for="filter-platform">Platform</label>
							<select id="filter-platform" onchange="applyFeedbackFilters()">
								<option value="">All</option>
							</select>
						</div>
						<div class="form-group" style="flex: 1; min-width: 150px;">
							<label for="filter-buildId">Build ID</label>
							<select id="filter-buildId" onchange="applyFeedbackFilters()">
								<option value="">All</option>
							</select>
						</div>
					</div>
					<div class="form-row" style="margin-bottom: 16px; flex-wrap: wrap;">
						<div class="form-group" style="flex: 1; min-width: 150px;">
							<label for="filter-region">Region</label>
							<select id="filter-region" onchange="applyFeedbackFilters()">
								<option value="">All</option>
							</select>
						</div>
						<div class="form-group" style="flex: 1; min-width: 150px;">
							<label for="filter-lang">Language</label>
							<select id="filter-lang" onchange="applyFeedbackFilters()">
								<option value="">All</option>
							</select>
						</div>
						<div class="form-group" style="flex: 1; min-width: 150px;">
							<label for="filter-startTime">Start Time</label>
							<input type="datetime-local" id="filter-startTime" onchange="applyFeedbackFilters()" />
						</div>
						<div class="form-group" style="flex: 1; min-width: 150px;">
							<label for="filter-endTime">End Time</label>
							<input type="datetime-local" id="filter-endTime" onchange="applyFeedbackFilters()" />
						</div>
					</div>
					<div style="margin-bottom: 16px;">
						<button class="btn btn-secondary" onclick="clearFeedbackFilters()">Clear Filters</button>
						<button class="btn btn-secondary" onclick="loadFeedback()" style="margin-left: 8px;">Reload</button>
					</div>
					<div class="form-row" style="margin-bottom:16px;flex-wrap:wrap;">
						<div class="form-group" style="flex:2;min-width:180px;">
							<label id="lbl-feedback-search">Search</label>
							<input type="text" id="feedback-search" placeholder="Search content, device, platform..." oninput="renderFeedback()" />
						</div>
						<div class="form-group" style="flex:1;min-width:150px;">
							<label id="lbl-feedback-sort">Sort by</label>
							<select id="feedback-sort" onchange="renderFeedback()">
								<option value="time-desc">Time (newest)</option>
								<option value="time-asc">Time (oldest)</option>
								<option value="platform-asc">Platform (A→Z)</option>
								<option value="coreVersion-asc">Core version (A→Z)</option>
							</select>
						</div>
					</div>
					<div id="feedback-list">
						<div class="loading">Loading feedback...</div>
					</div>
				</div>
			</div>
		</div>
	</div>
	
	<script>
		const basePath = '{{.BasePath}}';
		let launcherData = null;
		let maintenanceData = null;
		let updatesData = null;
		let newsData = null;
		let usersData = null;
		let statsData = null;
		let feedbackData = [];
		let statsAutoRefreshTimer = null;
		let dirBrowserPath = '';
		
		// i18n translations
		const i18n = {
			'en': {
				adminTitle: '🐱 NekoLc Admin Dashboard',
				logout: 'Logout',
				navStats: '📊 Statistics',
				navLauncher: '🚀 Launcher',
				navMaintenance: '🔧 Maintenance',
				navUpdates: '📦 Updates',
				navNews: '📰 News',
				navUsers: '👥 Users',
				navFeedback: '💬 Feedback',
				statsTitle: '📊 Statistics Overview',
				statsDesc: 'View server usage statistics and analytics.',
				totalRequests: 'Total Requests',
				todayRequests: "Today's Requests",
				totalUsers: 'Total Users',
				totalFeedback: 'Total Feedback',
				dailyRequests: 'Daily Requests',
				topEndpoints: 'Top Endpoints',
				platformDistribution: 'Platform Distribution',
				launcherTitle: 'Launcher Configuration',
				launcherDesc: 'Configure launcher settings that clients receive.',
				hosts: 'Hosts (one per line)',
				retryInterval: 'Retry Interval (seconds)',
				maxRetry: 'Max Retry Count',
				wsTitle: 'WebSocket',
				wsEnable: 'Enable WebSocket',
				wsHost: 'WebSocket Host',
				wsHeartbeat: 'Heartbeat Interval (seconds)',
				securityTitle: 'Security',
				authEnable: 'Enable Authentication',
				tokenExp: 'Token Expiration (seconds)',
				refreshExp: 'Refresh Token Expiration (days)',
				loginUrl: 'Login URL',
				logoutUrl: 'Logout URL',
				refreshUrl: 'Refresh URL',
				registerUrl: 'Register URL (API)',
				registerUiUrl: 'Register URL (UI)',
				saveChanges: 'Save Changes',
				reload: 'Reload',
				savedSuccess: 'Saved successfully',
				saveFailed: 'Failed to save',
				maintTitle: 'Maintenance Configuration',
				maintActive: 'Maintenance Active',
				status: 'Status',
				statusNone: 'None',
				statusScheduled: 'Scheduled',
				statusProgress: 'In Progress',
				posterUrl: 'Poster URL',
				message: 'Message',
				startTime: 'Start Time',
				endTime: 'Expected End Time',
				announcementLink: 'Announcement Link',
				platformMaintTitle: 'Platform-Specific Maintenance',
				platformMaintDesc: 'Configure maintenance settings per platform (e.g., windows-x64, linux-arm64).',
				addPlatform: '+ Add Platform',
				remove: 'Remove',
				updatesAutoGen: 'Auto-Generate from Directory',
				updatesAutoGenDesc: 'Scan a directory to automatically generate update files with checksums.',
				dirPath: 'Directory Path',
				baseUrl: 'Base URL',
				platform: 'Platform',
				architecture: 'Architecture',
				hashAlg: 'Hash Algorithm',
				generate: 'Generate',
				updatesConfig: 'Updates Configuration',
				updatesConfigDesc: 'Configure update settings for different platforms and architectures.',
				isCore: 'Is Core',
				addFile: 'Add File',
				newsTitle: 'News Management',
				newsDesc: 'Create and manage news items.',
				addNewsItem: '+ Add News Item',
				title: 'Title',
				content: 'Content',
				category: 'Category',
				priority: 'Priority',
				publishTime: 'Publish Time',
				usersTitle: 'User Management',
				usersDesc: 'View and manage user accounts.',
				createUser: '+ Create User',
				id: 'ID',
				username: 'Username',
				role: 'Role',
				created: 'Created',
				actions: 'Actions',
				edit: 'Edit',
				delete: 'Delete',
				feedbackTitle: 'Feedback Logs',
				time: 'Time',
				loading: 'Loading...',
				noData: 'No data available.',
				confirmDelete: 'Are you sure you want to delete',
				deleteSuccess: 'Deleted successfully',
				deleteFailed: 'Failed to delete',
				password: 'Password',
				save: 'Save',
				cancel: 'Cancel'
			},
			'zh-hans': {
				adminTitle: '🐱 NekoLc 管理面板',
				logout: '登出',
				navStats: '📊 统计',
				navLauncher: '🚀 启动器',
				navMaintenance: '🔧 维护',
				navUpdates: '📦 更新',
				navNews: '📰 新闻',
				navUsers: '👥 用户',
				navFeedback: '💬 反馈',
				statsTitle: '📊 统计概览',
				statsDesc: '查看服务器使用统计和分析。',
				totalRequests: '总请求数',
				todayRequests: '今日请求',
				totalUsers: '总用户数',
				totalFeedback: '总反馈数',
				dailyRequests: '每日请求',
				topEndpoints: '热门端点',
				platformDistribution: '平台分布',
				launcherTitle: '启动器配置',
				launcherDesc: '配置客户端接收的启动器设置。',
				hosts: '主机地址（每行一个）',
				retryInterval: '重试间隔（秒）',
				maxRetry: '最大重试次数',
				wsTitle: 'WebSocket',
				wsEnable: '启用 WebSocket',
				wsHost: 'WebSocket 主机',
				wsHeartbeat: '心跳间隔（秒）',
				securityTitle: '安全',
				authEnable: '启用身份验证',
				tokenExp: '令牌过期时间（秒）',
				refreshExp: '刷新令牌过期时间（天）',
				loginUrl: '登录 URL',
				logoutUrl: '登出 URL',
				refreshUrl: '刷新 URL',
				registerUrl: '注册 URL (API)',
				registerUiUrl: '注册 URL (UI)',
				saveChanges: '保存更改',
				reload: '重新加载',
				savedSuccess: '保存成功',
				saveFailed: '保存失败',
				maintTitle: '维护配置',
				maintActive: '维护中',
				status: '状态',
				statusNone: '无',
				statusScheduled: '已计划',
				statusProgress: '进行中',
				posterUrl: '海报 URL',
				message: '消息',
				startTime: '开始时间',
				endTime: '预计结束时间',
				announcementLink: '公告链接',
				platformMaintTitle: '平台特定维护',
				platformMaintDesc: '为不同平台配置维护设置（例如：windows-x64、linux-arm64）。',
				addPlatform: '+ 添加平台',
				remove: '移除',
				updatesAutoGen: '从目录自动生成',
				updatesAutoGenDesc: '扫描目录以自动生成带校验和的更新文件。',
				dirPath: '目录路径',
				baseUrl: '基础 URL',
				platform: '平台',
				architecture: '架构',
				hashAlg: '哈希算法',
				generate: '生成',
				updatesConfig: '更新配置',
				updatesConfigDesc: '为不同平台和架构配置更新设置。',
				isCore: '核心文件',
				addFile: '添加文件',
				newsTitle: '新闻管理',
				newsDesc: '创建和管理新闻项目。',
				addNewsItem: '+ 添加新闻',
				title: '标题',
				content: '内容',
				category: '分类',
				priority: '优先级',
				publishTime: '发布时间',
				usersTitle: '用户管理',
				usersDesc: '查看和管理用户账户。',
				createUser: '+ 创建用户',
				id: 'ID',
				username: '用户名',
				role: '角色',
				created: '创建时间',
				actions: '操作',
				edit: '编辑',
				delete: '删除',
				feedbackTitle: '反馈日志',
				time: '时间',
				loading: '加载中...',
				noData: '暂无数据。',
				confirmDelete: '确定要删除',
				deleteSuccess: '删除成功',
				deleteFailed: '删除失败',
				password: '密码',
				save: '保存',
				cancel: '取消'
			},
			'zh-hant': {
				adminTitle: '🐱 NekoLc 管理面板',
				logout: '登出',
				navStats: '📊 統計',
				navLauncher: '🚀 啟動器',
				navMaintenance: '🔧 維護',
				navUpdates: '📦 更新',
				navNews: '📰 新聞',
				navUsers: '👥 使用者',
				navFeedback: '💬 意見回饋',
				statsTitle: '📊 統計概覽',
				statsDesc: '檢視伺服器使用統計和分析。',
				totalRequests: '總請求數',
				todayRequests: '今日請求',
				totalUsers: '總使用者數',
				totalFeedback: '總意見回饋數',
				dailyRequests: '每日請求',
				topEndpoints: '熱門端點',
				platformDistribution: '平台分佈',
				launcherTitle: '啟動器設定',
				launcherDesc: '設定客戶端接收的啟動器設定。',
				hosts: '主機位址（每行一個）',
				retryInterval: '重試間隔（秒）',
				maxRetry: '最大重試次數',
				wsTitle: 'WebSocket',
				wsEnable: '啟用 WebSocket',
				wsHost: 'WebSocket 主機',
				wsHeartbeat: '心跳間隔（秒）',
				securityTitle: '安全性',
				authEnable: '啟用身份驗證',
				tokenExp: '令牌過期時間（秒）',
				refreshExp: '重新整理令牌過期時間（天）',
				loginUrl: '登入 URL',
				logoutUrl: '登出 URL',
				refreshUrl: '重新整理 URL',
				registerUrl: '註冊 URL (API)',
				registerUiUrl: '註冊 URL (UI)',
				saveChanges: '儲存變更',
				reload: '重新載入',
				savedSuccess: '儲存成功',
				saveFailed: '儲存失敗',
				maintTitle: '維護設定',
				maintActive: '維護中',
				status: '狀態',
				statusNone: '無',
				statusScheduled: '已排程',
				statusProgress: '進行中',
				posterUrl: '海報 URL',
				message: '訊息',
				startTime: '開始時間',
				endTime: '預計結束時間',
				announcementLink: '公告連結',
				platformMaintTitle: '平台特定維護',
				platformMaintDesc: '為不同平台設定維護設定（例如：windows-x64、linux-arm64）。',
				addPlatform: '+ 新增平台',
				remove: '移除',
				updatesAutoGen: '從目錄自動產生',
				updatesAutoGenDesc: '掃描目錄以自動產生帶校驗和的更新檔案。',
				dirPath: '目錄路徑',
				baseUrl: '基礎 URL',
				platform: '平台',
				architecture: '架構',
				hashAlg: '雜湊演算法',
				generate: '產生',
				updatesConfig: '更新設定',
				updatesConfigDesc: '為不同平台和架構設定更新設定。',
				isCore: '核心檔案',
				addFile: '新增檔案',
				newsTitle: '新聞管理',
				newsDesc: '建立和管理新聞項目。',
				addNewsItem: '+ 新增新聞',
				title: '標題',
				content: '內容',
				category: '分類',
				priority: '優先順序',
				publishTime: '發布時間',
				usersTitle: '使用者管理',
				usersDesc: '檢視和管理使用者帳戶。',
				createUser: '+ 建立使用者',
				id: 'ID',
				username: '使用者名稱',
				role: '角色',
				created: '建立時間',
				actions: '操作',
				edit: '編輯',
				delete: '刪除',
				feedbackTitle: '意見回饋記錄',
				time: '時間',
				loading: '載入中...',
				noData: '暫無資料。',
				confirmDelete: '確定要刪除',
				deleteSuccess: '刪除成功',
				deleteFailed: '刪除失敗',
				password: '密碼',
				save: '儲存',
				cancel: '取消'
			}
		};
		
		function getLang() { return localStorage.getItem('lang') || 'en'; }
		function setLang(lang) { localStorage.setItem('lang', lang); applyLang(); }
		function changeLang() { setLang(document.getElementById('langSelect').value); }
		function t(key) { const lang = getLang(); return (i18n[lang] && i18n[lang][key]) || i18n['en'][key] || key; }
		
		function applyLang() {
			const lang = getLang();
			document.getElementById('langSelect').value = lang;
			// Header
			document.getElementById('admin-title').innerText = t('adminTitle');
			document.getElementById('btn-logout').innerText = t('logout');
			// Navigation
			document.getElementById('nav-stats').innerText = t('navStats');
			document.getElementById('nav-launcher').innerText = t('navLauncher');
			document.getElementById('nav-maintenance').innerText = t('navMaintenance');
			document.getElementById('nav-updates').innerText = t('navUpdates');
			document.getElementById('nav-news').innerText = t('navNews');
			document.getElementById('nav-users').innerText = t('navUsers');
			document.getElementById('nav-feedback').innerText = t('navFeedback');
			// Statistics section
			document.getElementById('stats-title').innerText = t('statsTitle');
			document.getElementById('stats-desc').innerText = t('statsDesc');
			document.getElementById('lbl-total-requests').innerText = t('totalRequests');
			document.getElementById('lbl-today-requests').innerText = t('todayRequests');
			document.getElementById('lbl-total-users').innerText = t('totalUsers');
			document.getElementById('lbl-total-feedback').innerText = t('totalFeedback');
			// Launcher section
			document.getElementById('launcher-title').innerText = t('launcherTitle');
			document.getElementById('launcher-desc').innerText = t('launcherDesc');
			document.getElementById('lbl-hosts').innerText = t('hosts');
			document.getElementById('lbl-retry-interval').innerText = t('retryInterval');
			document.getElementById('lbl-max-retry').innerText = t('maxRetry');
			document.getElementById('ws-title').innerText = t('wsTitle');
			document.getElementById('lbl-ws-enable').innerText = t('wsEnable');
			document.getElementById('lbl-ws-host').innerText = t('wsHost');
			document.getElementById('lbl-ws-heartbeat').innerText = t('wsHeartbeat');
			document.getElementById('security-title').innerText = t('securityTitle');
			document.getElementById('lbl-auth-enable').innerText = t('authEnable');
			document.getElementById('lbl-token-exp').innerText = t('tokenExp');
			document.getElementById('lbl-refresh-exp').innerText = t('refreshExp');
			document.getElementById('lbl-login-url').innerText = t('loginUrl');
			document.getElementById('lbl-logout-url').innerText = t('logoutUrl');
			document.getElementById('lbl-refresh-url').innerText = t('refreshUrl');
			document.getElementById('lbl-register-url').innerText = t('registerUrl');
			document.getElementById('lbl-register-ui-url').innerText = t('registerUiUrl');
			document.getElementById('btn-save-launcher').innerText = t('saveChanges');
			document.getElementById('btn-reload-launcher').innerText = t('reload');
		}
		
		function getActiveStorage() {
			// Check localStorage first (persistent), then sessionStorage (session-only)
			if (localStorage.getItem('accessToken')) return localStorage;
			if (sessionStorage.getItem('accessToken')) return sessionStorage;
			return localStorage;
		}
		
		function getToken() {
			const storage = getActiveStorage();
			return storage.getItem('accessToken');
		}
		
		function getRefreshToken() {
			const storage = getActiveStorage();
			return storage.getItem('refreshToken');
		}
		
		async function tryRefreshToken() {
			const refreshToken = getRefreshToken();
			if (!refreshToken) return false;
			try {
				const res = await fetch(basePath + '/v0/api/auth/refresh', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ refreshRequest: { refreshToken: refreshToken } })
				});
				if (!res.ok) return false;
				const data = await res.json();
				if (data.refreshResponse && data.refreshResponse.accessToken) {
					const storage = getActiveStorage();
					storage.setItem('accessToken', data.refreshResponse.accessToken);
					return true;
				}
			} catch (e) {
				console.error('Token refresh failed:', e);
			}
			return false;
		}
		
		async function checkAuth() {
			if (!getToken()) {
				// Try to refresh token
				const refreshed = await tryRefreshToken();
				if (!refreshed) {
					window.location.href = basePath + '/app/login';
				}
			}
		}
		
		function logout() {
			localStorage.removeItem('accessToken');
			localStorage.removeItem('refreshToken');
			localStorage.removeItem('userRole');
			localStorage.removeItem('username');
			localStorage.removeItem('rememberMe');
			sessionStorage.removeItem('accessToken');
			sessionStorage.removeItem('refreshToken');
			sessionStorage.removeItem('userRole');
			sessionStorage.removeItem('username');
			sessionStorage.removeItem('rememberMe');
			window.location.href = basePath + '/app/login';
		}
		
		function showMessage(text, isError = false) {
			const msg = document.getElementById('message');
			msg.textContent = text;
			msg.className = 'message ' + (isError ? 'error' : 'success');
			setTimeout(() => { msg.className = 'message hidden'; }, 4000);
		}
		
		function showSection(name) {
			document.querySelectorAll('.section').forEach(s => s.classList.add('hidden'));
			document.querySelectorAll('.sidebar button').forEach(b => b.classList.remove('active'));
			document.getElementById('section-' + name).classList.remove('hidden');
			document.querySelector('.sidebar button[onclick*="' + name + '"]').classList.add('active');
			
			if (name === 'stats' && !statsData) loadStats();
			if (name === 'launcher' && !launcherData) loadLauncher();
			if (name === 'maintenance' && !maintenanceData) loadMaintenance();
			if (name === 'updates' && !updatesData) loadUpdates();
			if (name === 'news' && !newsData) loadNews();
			if (name === 'users' && !usersData) loadUsers();
			if (name === 'feedback') loadFeedback();
		}
		
		async function apiRequest(method, path, body = null) {
			const opts = {
				method,
				headers: { 'Authorization': 'Bearer ' + getToken() }
			};
			if (body) {
				opts.headers['Content-Type'] = 'application/json';
				opts.body = JSON.stringify(body);
			}
			const res = await fetch(basePath + path, opts);
			if (res.status === 401 || res.status === 403) {
				logout();
				return null;
			}
			return res;
		}
		
		function escapeHtml(str) {
			if (!str) return '';
			return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#039;');
		}
		
		// Statistics functions
		async function loadStats() {
			const days = document.getElementById('stats-days')?.value || 7;
			const res = await apiRequest('GET', '/v0/api/admin/stats?days=' + days);
			if (!res) return;
			const data = await res.json();
			statsData = data.stats;
			
			// Update summary cards
			document.getElementById('stat-total-requests').innerText = statsData.totalRequests?.toLocaleString() || '0';
			document.getElementById('stat-today-requests').innerText = statsData.todayRequests?.toLocaleString() || '0';
			document.getElementById('stat-total-users').innerText = statsData.totalUsers?.toLocaleString() || '0';
			document.getElementById('stat-total-feedback').innerText = statsData.totalFeedback?.toLocaleString() || '0';
			
			// Render daily chart
			renderDailyChart(statsData.dailyStats || []);
			
			// Render endpoints list
			renderEndpointsList(statsData.recentEndpoints || []);
			
			// Render platforms
			renderPlatformsList(statsData.platformCounts || {});
		}

		function toggleStatsAutoRefresh() {
			const enabled = document.getElementById('stats-autorefresh')?.checked;
			if (statsAutoRefreshTimer) {
				clearInterval(statsAutoRefreshTimer);
				statsAutoRefreshTimer = null;
			}
			if (enabled) {
				statsAutoRefreshTimer = setInterval(loadStats, 5000);
				loadStats();
			}
		}
		
		function renderDailyChart(dailyStats) {
			const container = document.getElementById('daily-chart');
			if (!dailyStats || dailyStats.length === 0) {
				container.innerHTML = '<div style="color: #94a3b8; text-align: center; width: 100%;">No data available</div>';
				return;
			}
			
			const maxCount = Math.max(...dailyStats.map(d => d.count), 1);
			let html = '';
			dailyStats.forEach(stat => {
				const height = Math.max((stat.count / maxCount) * 150, 4);
				const date = stat.date.split('-').slice(1).join('/');
				html += '<div style="display: flex; flex-direction: column; align-items: center; flex: 1; min-width: 40px;">';
				html += '<div style="background: linear-gradient(180deg, #22d3ee 0%, #818cf8 100%); width: 80%; height: ' + height + 'px; border-radius: 4px 4px 0 0; transition: height 0.3s;"></div>';
				html += '<div style="font-size: 11px; color: #94a3b8; margin-top: 4px;">' + date + '</div>';
				html += '<div style="font-size: 10px; color: #64748b;">' + stat.count + '</div>';
				html += '</div>';
			});
			container.innerHTML = html;
		}
		
		function renderEndpointsList(endpoints) {
			const container = document.getElementById('endpoints-list');
			if (!endpoints || endpoints.length === 0) {
				container.innerHTML = '<div style="color: #94a3b8;">No endpoint data available</div>';
				return;
			}
			
			let html = '';
			endpoints.slice(0, 10).forEach((ep, idx) => {
				const percent = endpoints[0]?.count > 0 ? (ep.count / endpoints[0].count * 100) : 0;
				html += '<div style="display: flex; align-items: center; gap: 12px; padding: 8px 0; border-bottom: 1px solid #1f2937;">';
				html += '<span style="color: #64748b; width: 20px;">' + (idx + 1) + '</span>';
				html += '<div style="flex: 1;">';
				html += '<div style="font-size: 13px; margin-bottom: 4px;">' + escapeHtml(ep.endpoint) + '</div>';
				html += '<div style="background: #1f2937; height: 4px; border-radius: 2px; overflow: hidden;">';
				html += '<div style="background: linear-gradient(90deg, #22d3ee, #818cf8); height: 100%; width: ' + percent + '%;"></div>';
				html += '</div></div>';
				html += '<span style="color: #94a3b8; font-size: 13px;">' + ep.count.toLocaleString() + '</span>';
				html += '</div>';
			});
			container.innerHTML = html;
		}
		
		function renderPlatformsList(platformCounts) {
			const container = document.getElementById('platforms-list');
			const platforms = Object.entries(platformCounts);
			if (!platforms || platforms.length === 0) {
				container.innerHTML = '<div style="color: #94a3b8;">No platform data available</div>';
				return;
			}
			
			const colors = ['#22d3ee', '#a855f7', '#22c55e', '#f59e0b', '#ef4444', '#6366f1'];
			let html = '';
			platforms.forEach(([platform, count], idx) => {
				const color = colors[idx % colors.length];
				html += '<div style="background: ' + color + '20; border: 1px solid ' + color + '40; padding: 12px 20px; border-radius: 8px; display: flex; flex-direction: column; align-items: center;">';
				html += '<div style="font-size: 24px; font-weight: 700; color: ' + color + ';">' + count.toLocaleString() + '</div>';
				html += '<div style="font-size: 13px; color: #94a3b8;">' + escapeHtml(platform) + '</div>';
				html += '</div>';
			});
			container.innerHTML = html;
		}
		
		// Launcher functions
		async function loadLauncher() {
			const res = await apiRequest('GET', '/v0/api/admin/launcher');
			if (!res) return;
			const data = await res.json();
			launcherData = data.launcher;
			
			document.getElementById('launcher-hosts').value = (launcherData.host || []).join('\n');
			document.getElementById('launcher-retry-interval').value = launcherData.retryIntervalSec || 5;
			document.getElementById('launcher-max-retry').value = launcherData.maxRetryCount || 3;
			
			const ws = launcherData.webSocket || {};
			document.getElementById('launcher-ws-enable').checked = ws.enable || false;
			document.getElementById('launcher-ws-host').value = ws.socketHost || '';
			document.getElementById('launcher-ws-heartbeat').value = ws.heartbeatIntervalSec || 30;
			
			const sec = launcherData.security || {};
			document.getElementById('launcher-auth-enable').checked = sec.enableAuthentication || false;
			document.getElementById('launcher-token-exp').value = sec.tokenExpirationSec || 3600;
			document.getElementById('launcher-refresh-exp').value = sec.refreshTokenExpirationDays || 30;
			document.getElementById('launcher-login-url').value = sec.loginUrl || '';
			document.getElementById('launcher-logout-url').value = sec.logoutUrl || '';
			document.getElementById('launcher-refresh-url').value = sec.refreshUrl || '';
			document.getElementById('launcher-register-url').value = sec.registerUrl || '/v0/api/auth/register';
			document.getElementById('launcher-register-ui-url').value = (sec.ui && sec.ui.registerUrl) || '/app/register';
		}
		
		async function saveLauncher() {
			const hosts = document.getElementById('launcher-hosts').value.trim().split('\n').filter(h => h.trim());
			const payload = {
				launcher: {
					host: hosts,
					retryIntervalSec: parseInt(document.getElementById('launcher-retry-interval').value) || 5,
					maxRetryCount: parseInt(document.getElementById('launcher-max-retry').value) || 3,
					webSocket: {
						enable: document.getElementById('launcher-ws-enable').checked,
						socketHost: document.getElementById('launcher-ws-host').value,
						heartbeatIntervalSec: parseInt(document.getElementById('launcher-ws-heartbeat').value) || 30
					},
					security: {
						enableAuthentication: document.getElementById('launcher-auth-enable').checked,
						tokenExpirationSec: parseInt(document.getElementById('launcher-token-exp').value) || 3600,
						refreshTokenExpirationDays: parseInt(document.getElementById('launcher-refresh-exp').value) || 30,
						loginUrl: document.getElementById('launcher-login-url').value,
						logoutUrl: document.getElementById('launcher-logout-url').value,
						refreshUrl: document.getElementById('launcher-refresh-url').value,
						registerUrl: document.getElementById('launcher-register-url').value,
						ui: {
							registerUrl: document.getElementById('launcher-register-ui-url').value
						}
					},
					featuresFlags: launcherData?.featuresFlags || {}
				}
			};
			const res = await apiRequest('PUT', '/v0/api/admin/launcher', payload);
			if (res && res.ok) {
				showMessage('Launcher configuration saved successfully');
			} else {
				showMessage('Failed to save launcher configuration', true);
			}
		}
		
		// Maintenance functions
		async function loadMaintenance() {
			const res = await apiRequest('GET', '/v0/api/admin/maintenance');
			if (!res) return;
			const data = await res.json();
			maintenanceData = data.maintenance;
			
			document.getElementById('maint-active').checked = maintenanceData.maintenanceActive || false;
			const info = maintenanceData.maintenanceInfo || {};
			document.getElementById('maint-status').value = info.status || 'none';
			document.getElementById('maint-message').value = info.message || '';
			document.getElementById('maint-poster').value = info.posterUrl || '';
			document.getElementById('maint-link').value = info.link || '';
			if (info.startTime) {
				document.getElementById('maint-start').value = info.startTime.slice(0, 16);
			}
			if (info.exEndTime) {
				document.getElementById('maint-end').value = info.exEndTime.slice(0, 16);
			}
			renderLocalizedMessages();
			renderPlatformMaintenance();
		}
		
		function renderLocalizedMessages() {
			const container = document.getElementById('localized-messages-list');
			if (!maintenanceData || !maintenanceData.maintenanceInfo) {
				container.innerHTML = '';
				return;
			}
			const info = maintenanceData.maintenanceInfo;
			const localized = info.localizedMessages || { default: '', langs: {} };
			const langs = localized.langs || {};
			const langKeys = Object.keys(langs);
			
			let html = '';
			langKeys.forEach((langCode, idx) => {
				const safeLang = escapeHtml(langCode);
				const safeValue = escapeHtml(langs[langCode] || '');
				html += '<div class="form-row" id="localized-msg-' + idx + '" style="margin-bottom: 8px;">';
				html += '<div class="form-group" style="flex: 0 0 120px;">';
				html += '<input type="text" value="' + safeLang + '" placeholder="e.g., zh-hant" onchange="updateLocalizedMsgLang(' + idx + ', this.value)" style="width: 100%;" />';
				html += '</div>';
				html += '<div class="form-group" style="flex: 1;">';
				html += '<input type="text" value="' + safeValue + '" placeholder="Localized message" onchange="updateLocalizedMsgText(\'' + safeLang + '\', this.value)" style="width: 100%;" />';
				html += '</div>';
				html += '<button type="button" class="btn btn-danger" style="padding: 8px 12px; flex: 0 0 auto;" onclick="removeLocalizedMessage(\'' + safeLang + '\')">×</button>';
				html += '</div>';
			});
			container.innerHTML = html;
		}
		
		function addLocalizedMessage() {
			if (!maintenanceData) maintenanceData = { maintenanceActive: false, maintenanceInfo: {} };
			if (!maintenanceData.maintenanceInfo) maintenanceData.maintenanceInfo = {};
			if (!maintenanceData.maintenanceInfo.localizedMessages) {
				maintenanceData.maintenanceInfo.localizedMessages = { default: '', langs: {} };
			}
			if (!maintenanceData.maintenanceInfo.localizedMessages.langs) {
				maintenanceData.maintenanceInfo.localizedMessages.langs = {};
			}
			const existing = Object.keys(maintenanceData.maintenanceInfo.localizedMessages.langs);
			let newLang = 'zh-hant';
			const commonLangs = ['zh-hant', 'zh-hans', 'ja', 'ko', 'en', 'fr', 'de', 'es'];
			for (const lang of commonLangs) {
				if (!existing.includes(lang)) {
					newLang = lang;
					break;
				}
			}
			maintenanceData.maintenanceInfo.localizedMessages.langs[newLang] = '';
			renderLocalizedMessages();
		}
		
		function updateLocalizedMsgLang(idx, newLang) {
			if (!maintenanceData?.maintenanceInfo?.localizedMessages?.langs) return;
			const langs = maintenanceData.maintenanceInfo.localizedMessages.langs;
			const keys = Object.keys(langs);
			if (idx >= keys.length) return;
			const oldLang = keys[idx];
			const value = langs[oldLang];
			delete langs[oldLang];
			langs[newLang.toLowerCase()] = value;
			renderLocalizedMessages();
		}
		
		function updateLocalizedMsgText(langCode, text) {
			if (!maintenanceData?.maintenanceInfo?.localizedMessages?.langs) return;
			maintenanceData.maintenanceInfo.localizedMessages.langs[langCode] = text;
		}
		
		function removeLocalizedMessage(langCode) {
			if (!maintenanceData?.maintenanceInfo?.localizedMessages?.langs) return;
			delete maintenanceData.maintenanceInfo.localizedMessages.langs[langCode];
			renderLocalizedMessages();
		}
		
		function renderPlatformMaintenance() {
			const container = document.getElementById('platform-maintenance-list');
			if (!maintenanceData || !maintenanceData.platformSpecific) {
				container.innerHTML = '<p style="color:#94a3b8;">No platform-specific maintenance configured.</p>';
				return;
			}
			const platforms = Object.entries(maintenanceData.platformSpecific);
			if (platforms.length === 0) {
				container.innerHTML = '<p style="color:#94a3b8;">No platform-specific maintenance configured.</p>';
				return;
			}
			let html = '';
			platforms.forEach(([key, pdata], idx) => {
				const info = pdata.maintenanceInfo || {};
				const safeKey = escapeHtml(key);
				html += '<div class="platform-section" id="plat-maint-' + idx + '">';
				html += '<div class="header-row" style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;">';
				html += '<h3 style="margin:0;">' + safeKey + '</h3>';
				html += '<button class="btn btn-danger" onclick="removePlatformMaintenance(\'' + safeKey + '\')">Remove</button>';
				html += '</div>';
				html += '<div class="form-group toggle"><input type="checkbox" id="plat-maint-active-' + idx + '" ' + (pdata.maintenanceActive ? 'checked' : '') + ' onchange="updatePlatformMaint(\'' + safeKey + '\', \'active\', this.checked)" />';
				html += '<label for="plat-maint-active-' + idx + '">Maintenance Active</label></div>';
				html += '<div class="form-row">';
				html += '<div class="form-group"><label>Status</label><select id="plat-maint-status-' + idx + '" onchange="updatePlatformMaint(\'' + safeKey + '\', \'status\', this.value)">';
				html += '<option value="none"' + (info.status === 'none' ? ' selected' : '') + '>None</option>';
				html += '<option value="scheduled"' + (info.status === 'scheduled' ? ' selected' : '') + '>Scheduled</option>';
				html += '<option value="progress"' + (info.status === 'progress' ? ' selected' : '') + '>In Progress</option>';
				html += '</select></div>';
				html += '<div class="form-group"><label>Message</label><input type="text" value="' + escapeHtml(info.message || '') + '" onchange="updatePlatformMaint(\'' + safeKey + '\', \'message\', this.value)" /></div>';
				html += '</div>';
				html += '</div>';
			});
			container.innerHTML = html;
		}
		
		function addPlatformMaintenance() {
			const key = prompt('Enter platform key (e.g., windows-x64, linux-arm64):');
			if (!key || !key.trim()) return;
			const platformKey = key.trim().toLowerCase();
			if (!maintenanceData) maintenanceData = { maintenanceActive: false, maintenanceInfo: {}, platformSpecific: {} };
			if (!maintenanceData.platformSpecific) maintenanceData.platformSpecific = {};
			if (maintenanceData.platformSpecific[platformKey]) {
				showMessage('Platform already exists', true);
				return;
			}
			maintenanceData.platformSpecific[platformKey] = {
				maintenanceActive: false,
				maintenanceInfo: { status: 'none', message: '', startTime: '', exEndTime: '', posterUrl: '', link: '' }
			};
			renderPlatformMaintenance();
			showMessage('Platform added. Remember to save changes.');
		}
		
		function removePlatformMaintenance(key) {
			if (!confirm('Remove maintenance settings for ' + key + '?')) return;
			if (maintenanceData && maintenanceData.platformSpecific) {
				delete maintenanceData.platformSpecific[key];
				renderPlatformMaintenance();
				showMessage('Platform removed. Remember to save changes.');
			}
		}
		
		function updatePlatformMaint(key, field, value) {
			if (!maintenanceData || !maintenanceData.platformSpecific || !maintenanceData.platformSpecific[key]) return;
			const p = maintenanceData.platformSpecific[key];
			if (field === 'active') {
				p.maintenanceActive = value;
			} else {
				if (!p.maintenanceInfo) p.maintenanceInfo = {};
				p.maintenanceInfo[field] = value;
			}
		}
		
		async function saveMaintenance() {
			const localizedMessages = maintenanceData?.maintenanceInfo?.localizedMessages || { default: '', langs: {} };
			const payload = {
				maintenance: {
					maintenanceActive: document.getElementById('maint-active').checked,
					maintenanceInfo: {
						status: document.getElementById('maint-status').value,
						message: document.getElementById('maint-message').value,
						startTime: document.getElementById('maint-start').value ? new Date(document.getElementById('maint-start').value).toISOString() : '',
						exEndTime: document.getElementById('maint-end').value ? new Date(document.getElementById('maint-end').value).toISOString() : '',
						posterUrl: document.getElementById('maint-poster').value,
						link: document.getElementById('maint-link').value,
						localizedMessages: localizedMessages
					},
					platformSpecific: maintenanceData?.platformSpecific || {}
				}
			};
			const res = await apiRequest('PUT', '/v0/api/admin/maintenance', payload);
			if (res && res.ok) {
				showMessage('Maintenance configuration saved successfully');
			} else {
				showMessage('Failed to save maintenance configuration', true);
			}
		}
		
		// Updates functions
		async function loadUpdates() {
			const res = await apiRequest('GET', '/v0/api/admin/updates');
			if (!res) return;
			const data = await res.json();
			updatesData = data.updates;
			renderUpdates();
		}
		
		function renderUpdates() {
			const container = document.getElementById('updates-content');
			if (!updatesData || !updatesData.platforms) {
				container.innerHTML = '<p style="color:#94a3b8;">No platforms configured.</p>';
				return;
			}
			let html = '';
			for (const [platform, pdata] of Object.entries(updatesData.platforms)) {
				html += '<div class="platform-section">';
				html += '<h3>' + escapeHtml(platform.charAt(0).toUpperCase() + platform.slice(1)) + '</h3>';
				if (pdata.architectures) {
					for (const [arch, adata] of Object.entries(pdata.architectures)) {
						html += '<div class="arch-item">';
						html += '<h4>' + escapeHtml(arch) + '</h4>';
						html += '<div class="form-row">';
						html += '<div class="form-group"><label>Core Version</label>';
						html += '<input type="text" data-platform="' + escapeHtml(platform) + '" data-arch="' + escapeHtml(arch) + '" data-type="core" value="' + escapeHtml(adata.latest?.coreVersion || '') + '" /></div>';
						html += '<div class="form-group"><label>Resource Version</label>';
						html += '<input type="text" data-platform="' + escapeHtml(platform) + '" data-arch="' + escapeHtml(arch) + '" data-type="res" value="' + escapeHtml(adata.latest?.resourceVersion || '') + '" /></div>';
						html += '</div>';
						html += '</div>';
					}
				}
				html += '</div>';
			}
			container.innerHTML = html || '<p style="color:#94a3b8;">No platforms configured.</p>';
		}
		
		async function saveUpdates() {
			// Collect updated versions from inputs using data attributes
			if (updatesData && updatesData.platforms) {
				const inputs = document.querySelectorAll('#updates-content input[data-platform]');
				inputs.forEach(input => {
					const platform = input.dataset.platform;
					const arch = input.dataset.arch;
					const type = input.dataset.type;
					if (updatesData.platforms[platform] && 
						updatesData.platforms[platform].architectures && 
						updatesData.platforms[platform].architectures[arch] &&
						updatesData.platforms[platform].architectures[arch].latest) {
						if (type === 'core') {
							updatesData.platforms[platform].architectures[arch].latest.coreVersion = input.value;
						} else if (type === 'res') {
							updatesData.platforms[platform].architectures[arch].latest.resourceVersion = input.value;
						}
					}
				});
			}
			const res = await apiRequest('PUT', '/v0/api/admin/updates', { updates: updatesData });
			if (res && res.ok) {
				showMessage('Updates configuration saved successfully');
			} else {
				showMessage('Failed to save updates configuration', true);
			}
		}
		
		async function scanPath() {
			const path = document.getElementById('scan-path').value.trim();
			const baseUrl = document.getElementById('scan-baseurl').value.trim();
			const isCore = document.getElementById('scan-type').value === 'core';
			if (!path) {
				showMessage('Please enter a directory path', true);
				return;
			}
			const res = await apiRequest('POST', '/v0/api/admin/scanPath', { path, baseUrl, isCore });
			if (!res) return;
			if (!res.ok) {
				const data = await res.json().catch(() => ({}));
				showMessage((data.errors && data.errors[0] && data.errors[0].errorMessage) || 'Scan failed', true);
				return;
			}
			const data = await res.json();
			const container = document.getElementById('scan-results');
			if (!data.files || data.files.length === 0) {
				container.innerHTML = '<p style="color:#94a3b8;">No files found in directory.</p>';
				return;
			}
			let html = '<p style="color:#22d3ee;">Found ' + data.count + ' file(s):</p>';
			html += '<div style="max-height:200px;overflow-y:auto;background:#0f172a;padding:12px;border-radius:8px;font-family:monospace;font-size:12px;">';
			data.files.forEach(f => {
				html += '<div style="margin-bottom:4px;">' + escapeHtml(f.fileName) + ' <span style="color:#94a3b8;">(' + escapeHtml(f.checksum.substring(0,16)) + '...)</span></div>';
			});
			html += '</div>';
			container.innerHTML = html;
			showMessage('Scan complete: ' + data.count + ' files found');
		}
		
		async function generateUpdates() {
			const path = document.getElementById('scan-path').value.trim();
			const baseUrl = document.getElementById('scan-baseurl').value.trim();
			const platform = document.getElementById('scan-platform').value;
			const architecture = document.getElementById('scan-arch').value;
			const isCore = document.getElementById('scan-type').value === 'core';
			if (!path) {
				showMessage('Please enter a directory path', true);
				return;
			}
			const res = await apiRequest('POST', '/v0/api/admin/generateUpdates', { path, baseUrl, platform, architecture, isCore });
			if (!res) return;
			if (!res.ok) {
				const data = await res.json().catch(() => ({}));
				showMessage((data.errors && data.errors[0] && data.errors[0].errorMessage) || 'Generate failed', true);
				return;
			}
			const data = await res.json();
			showMessage('Generated update config with ' + data.count + ' files for ' + platform + '/' + architecture);
			// Reload updates to show new config
			updatesData = null;
			loadUpdates();
		}

		async function uploadFile() {
			const input = document.getElementById('upload-file');
			if (!input || !input.files || input.files.length === 0) {
				showMessage('Please choose a file to upload', true);
				return;
			}
			const subdir = document.getElementById('upload-subdir').value.trim();
			const form = new FormData();
			form.append('file', input.files[0]);
			if (subdir) form.append('subdir', subdir);
			const res = await fetch(basePath + '/v0/api/admin/uploadFile', {
				method: 'POST',
				headers: { 'Authorization': 'Bearer ' + getToken() },
				body: form
			});
			if (res.status === 401 || res.status === 403) { logout(); return; }
			const data = await res.json().catch(() => ({}));
			if (!res.ok) {
				showMessage((data.errors && data.errors[0] && data.errors[0].errorMessage) || 'Upload failed', true);
				return;
			}
			const container = document.getElementById('upload-results');
			let html = '<div style="background:#0f172a;padding:12px;border-radius:8px;">';
			html += '<div style="color:#22d3ee;margin-bottom:8px;">Uploaded ' + escapeHtml(data.fileName) + ' (' + data.size + ' bytes)</div>';
			html += '<div style="display:flex;gap:8px;align-items:center;">';
			html += '<input type="text" readonly value="' + escapeHtml(data.url) + '" style="flex:1;font-family:monospace;font-size:12px;" id="upload-url" />';
			html += '<button class="btn btn-secondary" onclick="copyUploadUrl()">Copy</button>';
			html += '<button class="btn btn-secondary" onclick="useUploadUrl()">Use as Base URL</button>';
			html += '</div>';
			html += '<div style="color:#94a3b8;font-size:12px;margin-top:8px;">sha256: ' + escapeHtml(data.checksum) + '</div>';
			html += '</div>';
			container.innerHTML = html;
			showMessage('File uploaded successfully');
		}

		function copyUploadUrl() {
			const el = document.getElementById('upload-url');
			if (!el) return;
			el.select();
			if (navigator.clipboard) { navigator.clipboard.writeText(el.value); }
			else { document.execCommand('copy'); }
			showMessage('URL copied to clipboard');
		}

		function useUploadUrl() {
			const el = document.getElementById('upload-url');
			if (!el) return;
			document.getElementById('scan-baseurl').value = el.value;
			showMessage('Base URL set from uploaded file');
		}

		// Directory browser for visual scan-path selection
		function openDirBrowser() {
			document.getElementById('dir-browser').classList.remove('hidden');
			browseDir('');
		}

		function closeDirBrowser() {
			document.getElementById('dir-browser').classList.add('hidden');
		}

		async function browseDir(path) {
			const res = await apiRequest('GET', '/v0/api/admin/browseDir?path=' + encodeURIComponent(path || ''));
			if (!res) return;
			const data = await res.json().catch(() => ({}));
			if (!res.ok) {
				showMessage((data.errors && data.errors[0] && data.errors[0].errorMessage) || 'Browse failed', true);
				return;
			}
			dirBrowserPath = data.path || '';
			document.getElementById('dir-browser-current').innerText = '/' + dirBrowserPath;
			let html = '';
			if (data.path) {
				html += '<div class="dir-row" style="padding:8px;cursor:pointer;" onclick="browseDir(\'' + encodeURIComponent(data.parent || '').replace(/'/g, "\\'") + '\')">📁 ..</div>';
			}
			(data.entries || []).forEach(e => {
				if (e.isDir) {
					html += '<div class="dir-row" style="padding:8px;cursor:pointer;color:#e2e8f0;" onclick="browseDir(\'' + e.path.replace(/'/g, "\\'") + '\')">📁 ' + escapeHtml(e.name) + '</div>';
				} else {
					html += '<div style="padding:8px;color:#64748b;">📄 ' + escapeHtml(e.name) + '</div>';
				}
			});
			document.getElementById('dir-browser-list').innerHTML = html || '<div style="color:#94a3b8;padding:8px;">Empty directory</div>';
		}

		function chooseCurrentDir() {
			document.getElementById('scan-path').value = dirBrowserPath || '.';
			closeDirBrowser();
			showMessage('Directory selected: ' + (dirBrowserPath || '.'));
		}
		
		// News functions
		async function loadNews() {
			const res = await apiRequest('GET', '/v0/api/admin/news');
			if (!res) return;
			const data = await res.json();
			newsData = data.news;
			renderNews();
		}
		
		function renderNews() {
			const container = document.getElementById('news-list');
			if (!newsData || !newsData.items || newsData.items.length === 0) {
				container.innerHTML = '<p style="color:#94a3b8;">No news items.</p>';
				return;
			}
			// Pair each item with its original index so edits map to the real array.
			let view = newsData.items.map((item, idx) => ({ item, idx }));
			const term = (document.getElementById('news-search')?.value || '').trim().toLowerCase();
			if (term) {
				view = view.filter(({ item }) =>
					(item.title || '').toLowerCase().includes(term) ||
					(item.summary || '').toLowerCase().includes(term) ||
					(item.category || '').toLowerCase().includes(term) ||
					(item.id || '').toLowerCase().includes(term));
			}
			const sort = document.getElementById('news-sort')?.value || 'priority-desc';
			view.sort((a, b) => {
				switch (sort) {
					case 'priority-asc': return (a.item.priority || 0) - (b.item.priority || 0);
					case 'title-asc': return (a.item.title || '').localeCompare(b.item.title || '');
					case 'title-desc': return (b.item.title || '').localeCompare(a.item.title || '');
					case 'publish-asc': return String(a.item.publishTime || '').localeCompare(String(b.item.publishTime || ''));
					case 'publish-desc': return String(b.item.publishTime || '').localeCompare(String(a.item.publishTime || ''));
					case 'priority-desc':
					default: return (b.item.priority || 0) - (a.item.priority || 0);
				}
			});
			if (view.length === 0) {
				container.innerHTML = '<p style="color:#94a3b8;">No news items match your search.</p>';
				return;
			}
			let html = '';
			view.forEach(({ item, idx }) => {
				html += '<div class="news-item" id="news-item-' + idx + '">';
				html += '<div class="header-row"><h3>' + escapeHtml(item.title || 'Untitled') + '</h3>';
				html += '<button class="btn btn-danger" onclick="removeNewsItem(' + idx + ')">Remove</button></div>';
				html += '<div class="form-row">';
				html += '<div class="form-group"><label>ID</label><input type="text" value="' + escapeHtml(item.id || '') + '" onchange="updateNewsField(' + idx + ', \'id\', this.value)" /></div>';
				html += '<div class="form-group"><label>Title</label><input type="text" value="' + escapeHtml(item.title || '') + '" onchange="updateNewsField(' + idx + ', \'title\', this.value)" /></div>';
				html += '</div>';
				html += '<div class="form-group"><label>Summary</label><textarea onchange="updateNewsField(' + idx + ', \'summary\', this.value)">' + escapeHtml(item.summary || '') + '</textarea></div>';
				html += '<div class="form-row">';
				html += '<div class="form-group"><label>Category</label><input type="text" value="' + escapeHtml(item.category || '') + '" onchange="updateNewsField(' + idx + ', \'category\', this.value)" /></div>';
				html += '<div class="form-group"><label>Priority</label><input type="number" value="' + (item.priority || 0) + '" onchange="updateNewsField(' + idx + ', \'priority\', parseInt(this.value))" /></div>';
				html += '</div>';
				html += '<div class="form-group"><label>Link</label><input type="text" value="' + escapeHtml(item.link || '') + '" onchange="updateNewsField(' + idx + ', \'link\', this.value)" /></div>';
				html += '</div>';
			});
			container.innerHTML = html;
		}
		
		function updateNewsField(idx, field, value) {
			if (newsData && newsData.items && newsData.items[idx]) {
				newsData.items[idx][field] = value;
			}
		}
		
		function addNewsItem() {
			if (!newsData) newsData = { items: [] };
			if (!newsData.items) newsData.items = [];
			newsData.items.push({
				id: 'news-' + Date.now(),
				title: 'New Item',
				summary: '',
				content: '',
				posterUrl: '',
				link: '',
				publishTime: new Date().toISOString(),
				category: 'general',
				tags: [],
				priority: 0
			});
			renderNews();
		}
		
		function removeNewsItem(idx) {
			if (newsData && newsData.items) {
				newsData.items.splice(idx, 1);
				renderNews();
			}
		}
		
		async function saveNews() {
			const res = await apiRequest('PUT', '/v0/api/admin/news', { news: newsData });
			if (res && res.ok) {
				showMessage('News configuration saved successfully');
			} else {
				showMessage('Failed to save news configuration', true);
			}
		}
		
		// User management functions
		async function loadUsers() {
			const res = await apiRequest('GET', '/v0/api/admin/users');
			if (!res) return;
			const data = await res.json();
			usersData = data.users || [];
			renderUsersTable();
		}
		
		function renderUsersTable() {
			const tbody = document.getElementById('users-table-body');
			if (!usersData || usersData.length === 0) {
				tbody.innerHTML = '<tr><td colspan="5" style="padding: 20px; text-align: center; color: #94a3b8;">No users found.</td></tr>';
				return;
			}
			let html = '';
			usersData.forEach(user => {
				html += '<tr style="border-bottom: 1px solid #1f2937;">';
				html += '<td style="padding: 10px;">' + user.id + '</td>';
				html += '<td style="padding: 10px;">' + escapeHtml(user.username) + '</td>';
				html += '<td style="padding: 10px;"><span style="padding: 4px 8px; border-radius: 4px; background: ' + (user.role === 'admin' ? '#7c3aed' : '#374151') + '; font-size: 12px;">' + escapeHtml(user.role) + '</span></td>';
				html += '<td style="padding: 10px; color: #94a3b8;">' + escapeHtml(user.createdAt) + '</td>';
				html += '<td style="padding: 10px;">';
				html += '<button class="btn btn-secondary" style="padding: 4px 8px; font-size: 12px; margin-right: 4px;" onclick="editUser(' + user.id + ')">Edit</button>';
				html += '<button class="btn btn-danger" style="padding: 4px 8px; font-size: 12px; background: #dc2626;" onclick="deleteUser(' + user.id + ', \'' + escapeHtml(user.username) + '\')">Delete</button>';
				html += '</td>';
				html += '</tr>';
			});
			tbody.innerHTML = html;
		}
		
		function showAddUserForm() {
			document.getElementById('add-user-form').style.display = 'block';
		}
		
		function hideAddUserForm() {
			document.getElementById('add-user-form').style.display = 'none';
			document.getElementById('new-user-username').value = '';
			document.getElementById('new-user-password').value = '';
			document.getElementById('new-user-role').value = 'user';
		}
		
		async function createUser() {
			const username = document.getElementById('new-user-username').value.trim();
			const password = document.getElementById('new-user-password').value;
			const role = document.getElementById('new-user-role').value;
			if (!username || !password) {
				showMessage('Username and password are required', true);
				return;
			}
			const res = await apiRequest('POST', '/v0/api/admin/users', { username, password, role });
			if (res && res.ok) {
				showMessage('User created successfully');
				hideAddUserForm();
				usersData = null;
				loadUsers();
			} else {
				showMessage('Failed to create user', true);
			}
		}
		
		async function editUser(id) {
			const user = usersData.find(u => u.id === id);
			if (!user) return;
			const newRole = prompt('Enter new role for ' + user.username + ' (user or admin):', user.role);
			if (newRole === null) return;
			const newPassword = prompt('Enter new password (leave empty to keep current):');
			const payload = { role: newRole };
			if (newPassword) payload.password = newPassword;
			const res = await apiRequest('PUT', '/v0/api/admin/users/' + id, payload);
			if (res && res.ok) {
				showMessage('User updated successfully');
				usersData = null;
				loadUsers();
			} else {
				showMessage('Failed to update user', true);
			}
		}
		
		async function deleteUser(id, username) {
			if (!confirm('Are you sure you want to delete user "' + username + '"?')) return;
			const res = await apiRequest('DELETE', '/v0/api/admin/users/' + id);
			if (res && res.status === 204) {
				showMessage('User deleted successfully');
				usersData = null;
				loadUsers();
			} else {
				showMessage('Failed to delete user', true);
			}
		}
		
		// Feedback functions
		let feedbackFilterOptions = null;
		
		async function loadFeedbackFilterOptions() {
			const res = await apiRequest('GET', '/v0/api/admin/feedbackFilterOptions');
			if (!res) return;
			const data = await res.json();
			feedbackFilterOptions = data.options;
			
			// Populate filter dropdowns
			populateFilterSelect('filter-coreVersion', feedbackFilterOptions.coreVersions || []);
			populateFilterSelect('filter-resourceVersion', feedbackFilterOptions.resourceVersions || []);
			populateFilterSelect('filter-platform', feedbackFilterOptions.platforms || []);
			populateFilterSelect('filter-buildId', feedbackFilterOptions.buildIds || []);
			populateFilterSelect('filter-region', feedbackFilterOptions.regions || []);
			populateFilterSelect('filter-lang', feedbackFilterOptions.langs || []);
		}
		
		function populateFilterSelect(selectId, options) {
			const select = document.getElementById(selectId);
			if (!select) return;
			const currentValue = select.value;
			select.innerHTML = '<option value="">All</option>';
			options.forEach(opt => {
				const option = document.createElement('option');
				option.value = opt;
				option.textContent = opt;
				if (opt === currentValue) option.selected = true;
				select.appendChild(option);
			});
		}
		
		function buildFeedbackFilterParams() {
			const params = new URLSearchParams();
			params.append('limit', '50');
			
			const coreVersion = document.getElementById('filter-coreVersion')?.value;
			const resourceVersion = document.getElementById('filter-resourceVersion')?.value;
			const platform = document.getElementById('filter-platform')?.value;
			const buildId = document.getElementById('filter-buildId')?.value;
			const region = document.getElementById('filter-region')?.value;
			const lang = document.getElementById('filter-lang')?.value;
			const startTime = document.getElementById('filter-startTime')?.value;
			const endTime = document.getElementById('filter-endTime')?.value;
			
			if (coreVersion) params.append('coreVersion', coreVersion);
			if (resourceVersion) params.append('resourceVersion', resourceVersion);
			if (platform) params.append('platform', platform);
			if (buildId) params.append('buildId', buildId);
			if (region) params.append('region', region);
			if (lang) params.append('lang', lang);
			if (startTime) params.append('startTime', new Date(startTime).toISOString());
			if (endTime) params.append('endTime', new Date(endTime).toISOString());
			
			return params.toString();
		}
		
		function clearFeedbackFilters() {
			document.getElementById('filter-coreVersion').value = '';
			document.getElementById('filter-resourceVersion').value = '';
			document.getElementById('filter-platform').value = '';
			document.getElementById('filter-buildId').value = '';
			document.getElementById('filter-region').value = '';
			document.getElementById('filter-lang').value = '';
			document.getElementById('filter-startTime').value = '';
			document.getElementById('filter-endTime').value = '';
			loadFeedback();
		}
		
		function applyFeedbackFilters() {
			loadFeedback();
		}
		
		async function loadFeedback() {
			if (!feedbackFilterOptions) {
				await loadFeedbackFilterOptions();
			}
			const params = buildFeedbackFilterParams();
			const res = await apiRequest('GET', '/v0/api/admin/feedbackLogs?' + params);
			if (!res) return;
			const data = await res.json();
			feedbackData = data.feedbackLogs || [];
			renderFeedback();
		}

		function renderFeedback() {
			const container = document.getElementById('feedback-list');
			let view = (feedbackData || []).slice();
			const term = (document.getElementById('feedback-search')?.value || '').trim().toLowerCase();
			if (term) {
				view = view.filter(item =>
					(item.content || '').toLowerCase().includes(term) ||
					(item.deviceId || '').toLowerCase().includes(term) ||
					(item.platform || '').toLowerCase().includes(term) ||
					(item.coreVersion || '').toLowerCase().includes(term));
			}
			const sort = document.getElementById('feedback-sort')?.value || 'time-desc';
			view.sort((a, b) => {
				switch (sort) {
					case 'time-asc': return String(a.receivedAt || '').localeCompare(String(b.receivedAt || ''));
					case 'platform-asc': return (a.platform || '').localeCompare(b.platform || '');
					case 'coreVersion-asc': return (a.coreVersion || '').localeCompare(b.coreVersion || '');
					case 'time-desc':
					default: return String(b.receivedAt || '').localeCompare(String(a.receivedAt || ''));
				}
			});
			if (view.length === 0) {
				container.innerHTML = '<p style="color:#94a3b8;">No feedback entries matching filters.</p>';
				return;
			}
			let html = '<table style="width:100%;border-collapse:collapse;table-layout:fixed;">';
			html += '<thead><tr style="border-bottom:1px solid #374151;">';
			html += '<th style="text-align:left;padding:8px;width:160px;">Time</th>';
			html += '<th style="text-align:left;padding:8px;width:110px;">Platform</th>';
			html += '<th style="text-align:left;padding:8px;width:90px;">Version</th>';
			html += '<th style="text-align:left;padding:8px;">Content</th>';
			html += '<th style="text-align:left;padding:8px;width:80px;">Actions</th>';
			html += '</tr></thead>';
			html += '<tbody>';
			view.forEach(item => {
				const long = (item.content || '').length > 160 || (item.content || '').split('\n').length > 3;
				html += '<tr style="border-bottom:1px solid #1f2937;vertical-align:top;">';
				html += '<td style="padding:8px;color:#94a3b8;font-size:13px;">' + escapeHtml(item.receivedAt || '') + '</td>';
				html += '<td style="padding:8px;color:#94a3b8;font-size:13px;word-break:break-word;">' + escapeHtml(item.platform || '-') + '</td>';
				html += '<td style="padding:8px;color:#94a3b8;font-size:13px;word-break:break-word;">' + escapeHtml(item.coreVersion || '-') + '</td>';
				html += '<td style="padding:8px;">';
				html += '<div class="fb-content' + (long ? ' collapsed' : '') + '" id="fb-content-' + item.id + '">' + escapeHtml(item.content || '') + '</div>';
				if (long) {
					html += '<button class="fb-toggle" onclick="toggleFeedback(' + item.id + ')" id="fb-toggle-' + item.id + '">Show more</button>';
				}
				html += '</td>';
				html += '<td style="padding:8px;"><button class="btn btn-danger" style="padding:6px 12px;font-size:12px;" onclick="deleteFeedback(' + item.id + ')">Delete</button></td>';
				html += '</tr>';
			});
			html += '</tbody></table>';
			html += '<p style="color:#94a3b8;font-size:13px;margin-top:12px;">Showing ' + view.length + ' entries</p>';
			container.innerHTML = html;
		}

		function toggleFeedback(id) {
			const el = document.getElementById('fb-content-' + id);
			const btn = document.getElementById('fb-toggle-' + id);
			if (!el) return;
			const collapsed = el.classList.toggle('collapsed');
			if (btn) btn.innerText = collapsed ? 'Show more' : 'Show less';
		}

		async function deleteFeedback(id) {
			if (!confirm('Delete this feedback entry?')) return;
			const res = await apiRequest('DELETE', '/v0/api/admin/feedbackLogs/' + id);
			if (!res) return;
			if (res.status === 204) {
				feedbackData = (feedbackData || []).filter(f => f.id !== id);
				renderFeedback();
				showMessage('Feedback entry deleted');
			} else {
				const data = await res.json().catch(() => ({}));
				showMessage((data.errors && data.errors[0] && data.errors[0].errorMessage) || 'Failed to delete feedback', true);
			}
		}
		
		// Initialize
		checkAuth();
		applyLang();
		loadStats();
	</script>
</body>
</html>`))

var appUserDashboardTemplate = template.Must(template.New("appUserDashboard").Parse(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>NekoLc Dashboard</title>
	<style>
		* { box-sizing: border-box; }
		body { font-family: "Segoe UI", sans-serif; background: #0f172a; color: #e2e8f0; margin: 0; padding: 0; }
		.header { background: #111827; padding: 16px 24px; display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid #1f2937; }
		.header h1 { margin: 0; font-size: 18px; }
		.user { display: flex; align-items: center; gap: 12px; }
		.user span { color: #94a3b8; }
		.user button { padding: 8px 16px; border: none; border-radius: 6px; background: #374151; color: #e2e8f0; cursor: pointer; }
		.user button:hover { background: #4b5563; }
		.container { max-width: 1000px; margin: 0 auto; padding: 24px; }
		.card { background: #111827; border-radius: 12px; padding: 24px; margin-bottom: 20px; box-shadow: 0 4px 16px rgba(0,0,0,0.25); }
		h2 { margin: 0 0 16px 0; color: #f8fafc; }
		.welcome { font-size: 28px; margin-bottom: 8px; }
		.subtitle { color: #94a3b8; margin-bottom: 24px; }
		.info-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; }
		.info-item { background: #1f2937; padding: 16px; border-radius: 8px; }
		.info-label { color: #94a3b8; font-size: 13px; margin-bottom: 4px; }
		.info-value { font-size: 18px; font-weight: 600; }
		.lang-switch { display: flex; align-items: center; gap: 8px; }
		.lang-switch select { padding: 6px 10px; border-radius: 6px; border: 1px solid #374151; background: #1f2937; color: #e2e8f0; cursor: pointer; }
		.actions { display: flex; gap: 12px; margin-top: 24px; flex-wrap: wrap; }
		.actions a { padding: 12px 20px; border-radius: 8px; text-decoration: none; font-weight: 600; }
		.btn-primary { background: linear-gradient(120deg, #22d3ee, #818cf8); color: #0b1220; }
		.btn-secondary { background: #374151; color: #e2e8f0; }
	</style>
</head>
<body>
	<div class="header">
		<h1>🐱 NekoLc</h1>
		<div class="user">
			<div class="lang-switch">
				<select id="langSelect" onchange="changeLang()">
					<option value="en">English</option>
					<option value="zh-hans">简体中文</option>
					<option value="zh-hant">繁體中文</option>
				</select>
			</div>
			<span id="username">User</span>
			<button onclick="logout()" id="btn-logout">Logout</button>
		</div>
	</div>
	<div class="container">
		<div class="card">
			<div class="welcome" id="welcome">Welcome!</div>
			<div class="subtitle" id="subtitle">Here's your account overview</div>
			<div class="info-grid">
				<div class="info-item">
					<div class="info-label" id="lbl-username">Username</div>
					<div class="info-value" id="info-username">-</div>
				</div>
				<div class="info-item">
					<div class="info-label" id="lbl-role">Role</div>
					<div class="info-value" id="info-role">-</div>
				</div>
				<div class="info-item">
					<div class="info-label" id="lbl-status">Status</div>
					<div class="info-value" id="info-status" style="color: #34d399;">Active</div>
				</div>
			</div>
			<div class="actions">
				<a href="{{.BasePath}}/app" class="btn-secondary" id="link-home">Home</a>
			</div>
		</div>
	</div>
	<script>
		const basePath = '{{.BasePath}}';
		const i18n = {
			'en': { welcome: 'Welcome!', subtitle: "Here's your account overview", username: 'Username', role: 'Role', status: 'Status', active: 'Active', logout: 'Logout', home: 'Home', user: 'User', admin: 'Admin' },
			'zh-hans': { welcome: '欢迎！', subtitle: '这是您的账户概览', username: '用户名', role: '角色', status: '状态', active: '正常', logout: '登出', home: '首页', user: '用户', admin: '管理员' },
			'zh-hant': { welcome: '歡迎！', subtitle: '這是您的帳戶概覽', username: '使用者名稱', role: '角色', status: '狀態', active: '正常', logout: '登出', home: '首頁', user: '使用者', admin: '管理員' }
		};
		function getLang() { return localStorage.getItem('lang') || 'en'; }
		function setLang(lang) { localStorage.setItem('lang', lang); applyLang(); }
		function changeLang() { setLang(document.getElementById('langSelect').value); }
		function applyLang() {
			const lang = getLang();
			document.getElementById('langSelect').value = lang;
			const t = i18n[lang] || i18n['en'];
			document.getElementById('welcome').innerText = t.welcome;
			document.getElementById('subtitle').innerText = t.subtitle;
			document.getElementById('lbl-username').innerText = t.username;
			document.getElementById('lbl-role').innerText = t.role;
			document.getElementById('lbl-status').innerText = t.status;
			document.getElementById('info-status').innerText = t.active;
			document.getElementById('btn-logout').innerText = t.logout;
			document.getElementById('link-home').innerText = t.home;
			// Update role display
			const role = getActiveStorage().getItem('userRole') || 'user';
			document.getElementById('info-role').innerText = role === 'admin' ? t.admin : t.user;
		}
		function getActiveStorage() {
			if (localStorage.getItem('accessToken')) return localStorage;
			if (sessionStorage.getItem('accessToken')) return sessionStorage;
			return localStorage;
		}
		function checkAuth() {
			const storage = getActiveStorage();
			if (!storage.getItem('accessToken')) {
				window.location.href = basePath + '/app/login';
			}
		}
		function logout() {
			localStorage.removeItem('accessToken');
			localStorage.removeItem('refreshToken');
			localStorage.removeItem('userRole');
			localStorage.removeItem('username');
			localStorage.removeItem('rememberMe');
			sessionStorage.removeItem('accessToken');
			sessionStorage.removeItem('refreshToken');
			sessionStorage.removeItem('userRole');
			sessionStorage.removeItem('username');
			sessionStorage.removeItem('rememberMe');
			window.location.href = basePath + '/app/login';
		}
		function init() {
			checkAuth();
			applyLang();
			const storage = getActiveStorage();
			const username = storage.getItem('username') || 'User';
			document.getElementById('username').innerText = username;
			document.getElementById('info-username').innerText = username;
		}
		init();
	</script>
</body>
</html>`))

func (s *Server) handleAppAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	appAdminTemplate.Execute(w, map[string]interface{}{"BasePath": s.basePath})
}

func (s *Server) handleAppUserDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	appUserDashboardTemplate.Execute(w, map[string]interface{}{"BasePath": s.basePath})
}
