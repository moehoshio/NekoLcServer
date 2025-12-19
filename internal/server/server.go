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
	dirCacheMu            sync.RWMutex
	dirCache              map[string]dirCacheEntry
	updateConfigMu        sync.RWMutex
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

	mount := func(router chi.Router) {
		router.Get("/v0/testing/ping", s.handlePing)
		router.Post("/v0/testing/echo", s.handleEcho)

		router.Route("/v0/api", func(r chi.Router) {
			r.Route("/auth", func(authRouter chi.Router) {
				authRouter.Post("/login", s.handleLogin)
				authRouter.Post("/register", s.handleRegister)
				authRouter.Post("/refresh", s.handleRefresh)
				authRouter.Post("/validate", s.handleValidate)
				authRouter.Post("/logout", s.handleLogout)
			})
			r.Post("/launcherConfig", s.handleLauncherConfig)
			r.Post("/maintenance", s.handleMaintenance)
			r.Post("/checkUpdates", s.handleCheckUpdates)
			r.Post("/news", s.handleNews)
			r.Post("/feedbackLog", s.handleFeedbackLog)
			r.Get("/feedbackLogs", s.handleFeedbackLogs)

			// Admin API routes for configuration management
			r.Route("/admin", func(adminRouter chi.Router) {
				adminRouter.Get("/maintenance", s.handleAdminGetMaintenance)
				adminRouter.Put("/maintenance", s.handleAdminUpdateMaintenance)
				adminRouter.Get("/updates", s.handleAdminGetUpdates)
				adminRouter.Put("/updates", s.handleAdminUpdateUpdates)
				adminRouter.Get("/news", s.handleAdminGetNews)
				adminRouter.Put("/news", s.handleAdminUpdateNews)
				adminRouter.Post("/scanPath", s.handleAdminScanPath)
				adminRouter.Post("/generateUpdates", s.handleAdminGenerateUpdates)
			})
		})

		if s.debug {
			router.Get("/debug/feedback", s.handleFeedbackView)
			router.Get("/debug/feedback.json", s.handleFeedbackJSON)
		}

		router.Get("/app/login", s.handleAppLogin)
		router.Get("/app/register", s.handleAppRegister)
		router.Get("/app/feedback", s.handleAppFeedback)
		router.Get("/app/admin", s.handleAppAdmin)
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

const appLoginPage = `<!doctype html>
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
		input { width: 100%; padding: 10px; margin-top: 6px; border-radius: 8px; border: 1px solid #1f2937; background: #0b1220; color: #e2e8f0; box-sizing: border-box; }
		button { width: 100%; margin-top: 18px; padding: 12px; border: none; border-radius: 10px; background: linear-gradient(120deg,#22d3ee,#818cf8); color: #0b1220; font-weight: 700; cursor: pointer; }
		button:hover { filter: brightness(1.05); }
		.error { color: #f87171; margin-top: 10px; min-height: 20px; }
		.link { text-align: center; margin-top: 16px; }
		.link a { color: #22d3ee; text-decoration: none; }
		.link a:hover { text-decoration: underline; }
	</style>
</head>
<body>
	<div class="card">
		<h1>Sign in</h1>
		<label>Username</label>
		<input id="username" autocomplete="username" />
		<label>Password</label>
		<input id="password" type="password" autocomplete="current-password" />
		<button onclick="login()">Login</button>
		<div class="error" id="error"></div>
		<div class="link">Don't have an account? <a href="/app/register">Register</a></div>
	</div>
	<script>
		async function login() {
			const username = document.getElementById('username').value.trim();
			const password = document.getElementById('password').value;
			const res = await fetch('/v0/api/auth/login', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ loginRequest: { username, password } })
			});
			if (!res.ok) {
				const data = await res.json().catch(()=>({}));
				document.getElementById('error').innerText = (data.errors && data.errors[0] && data.errors[0].errorMessage) || 'Login failed';
				return;
			}
			const data = await res.json();
			localStorage.setItem('accessToken', data.loginResponse.accessToken);
			localStorage.setItem('refreshToken', data.loginResponse.refreshToken);
			window.location.href = '/app/admin';
		}
	</script>
</body>
</html>`

const appRegisterPage = `<!doctype html>
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
	</style>
</head>
<body>
	<div class="card">
		<h1>Create Account</h1>
		<label>Username</label>
		<input id="username" autocomplete="username" placeholder="3-50 characters" />
		<label>Password</label>
		<input id="password" type="password" autocomplete="new-password" placeholder="At least 6 characters" />
		<label>Confirm Password</label>
		<input id="confirmPassword" type="password" autocomplete="new-password" />
		<button onclick="register()">Register</button>
		<div class="error" id="error"></div>
		<div class="success" id="success"></div>
		<div class="link">Already have an account? <a href="/app/login">Sign in</a></div>
	</div>
	<script>
		async function register() {
			const username = document.getElementById('username').value.trim();
			const password = document.getElementById('password').value;
			const confirmPassword = document.getElementById('confirmPassword').value;
			document.getElementById('error').innerText = '';
			document.getElementById('success').innerText = '';
			if (password !== confirmPassword) {
				document.getElementById('error').innerText = 'Passwords do not match';
				return;
			}
			const res = await fetch('/v0/api/auth/register', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ registerRequest: { username, password } })
			});
			if (!res.ok) {
				const data = await res.json().catch(()=>({}));
				document.getElementById('error').innerText = (data.errors && data.errors[0] && data.errors[0].errorMessage) || 'Registration failed';
				return;
			}
			document.getElementById('success').innerText = 'Account created! Redirecting to login...';
			setTimeout(() => { window.location.href = '/app/login'; }, 1500);
		}
	</script>
</body>
</html>`

const appFeedbackPage = `<!doctype html>
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
		async function loadLogs() {
			const token = localStorage.getItem('accessToken');
			if (!token) { window.location.href = '/app/login'; return; }
			const res = await fetch('/v0/api/feedbackLogs?limit=50', { headers: { 'Authorization': 'Bearer ' + token } });
			if (res.status === 401 || res.status === 403) { localStorage.removeItem('accessToken'); window.location.href = '/app/login'; return; }
			if (!res.ok) { document.getElementById('error').innerText = 'Failed to load logs'; return; }
			const data = await res.json();
			const tbody = document.querySelector('#table tbody');
			tbody.innerHTML = '';
			for (const item of (data.feedbackLogs || [])) {
				const tr = document.createElement('tr');
				const when = document.createElement('td'); when.innerText = item.receivedAt || ''; tr.appendChild(when);
				const content = document.createElement('td'); content.className='content'; content.innerText = item.content || ''; tr.appendChild(content);
				const info = document.createElement('td'); info.innerHTML = '<div class="code">' + JSON.stringify(item.clientInfo || {}, null, 2) + '</div>'; tr.appendChild(info);
				tbody.appendChild(tr);
			}
		}
		loadLogs();
	</script>
</body>
</html>`

func (s *Server) handleAppLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(appLoginPage))
}

func (s *Server) handleAppRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(appRegisterPage))
}

func (s *Server) handleAppFeedback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(appFeedbackPage))
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

// saveMaintenanceConfig writes the current maintenance configuration to its file.
func (s *Server) saveMaintenanceConfig() error {
	if s.maintenanceConfigPath == "" {
		return errors.New("maintenance config path not set")
	}
	return saveJSONFile(s.maintenanceConfigPath, s.maintenanceConfig)
}

// saveUpdatesConfig writes the current updates configuration to its file.
func (s *Server) saveUpdatesConfig() error {
	if s.updateConfigPath == "" {
		return errors.New("updates config path not set")
	}
	s.updateConfigMu.RLock()
	cfg := s.updateConfig
	s.updateConfigMu.RUnlock()
	return saveJSONFile(s.updateConfigPath, cfg)
}

// saveNewsConfig writes the news configuration to its file.
func (s *Server) saveNewsConfig(cfg *config.NewsConfig) error {
	if s.newsConfigPath == "" {
		return errors.New("news config path not set")
	}
	return saveJSONFile(s.newsConfigPath, cfg)
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

const appAdminPage = `<!doctype html>
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
	</style>
</head>
<body>
	<div class="header">
		<h1>🐱 NekoLc Admin Dashboard</h1>
		<div class="user">
			<span id="username">Admin</span>
			<button onclick="logout()">Logout</button>
		</div>
	</div>
	<div class="container">
		<div class="sidebar">
			<button class="active" onclick="showSection('maintenance')">🔧 Maintenance</button>
			<button onclick="showSection('updates')">📦 Updates</button>
			<button onclick="showSection('news')">📰 News</button>
			<button onclick="showSection('feedback')">💬 Feedback</button>
		</div>
		<div class="main">
			<div id="message" class="message hidden"></div>
			
			<!-- Maintenance Section -->
			<div id="section-maintenance" class="section">
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
						<label for="maint-message">Message</label>
						<textarea id="maint-message" rows="2"></textarea>
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
			</div>
			
			<!-- Updates Section -->
			<div id="section-updates" class="section hidden">
				<div class="card">
					<h2>Auto-Generate from Directory</h2>
					<p style="color: #94a3b8; margin-bottom: 16px;">Scan a directory to automatically generate update files with checksums.</p>
					<div class="form-row">
						<div class="form-group">
							<label for="scan-path">Directory Path</label>
							<input type="text" id="scan-path" placeholder="./updates/windows-x64" />
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
					<h2>Updates Configuration</h2>
					<p style="color: #94a3b8; margin-bottom: 16px;">Current update packages for each platform and architecture.</p>
					<div id="updates-content">
						<div class="loading">Loading updates configuration...</div>
					</div>
					<div class="actions">
						<button class="btn btn-primary" onclick="saveUpdates()">Save Changes</button>
						<button class="btn btn-secondary" onclick="loadUpdates()">Reload</button>
					</div>
				</div>
			</div>
			
			<!-- News Section -->
			<div id="section-news" class="section hidden">
				<div class="card">
					<h2>News Items</h2>
					<div id="news-list"></div>
					<button class="btn btn-secondary" style="margin-top: 16px;" onclick="addNewsItem()">+ Add News Item</button>
					<div class="actions">
						<button class="btn btn-primary" onclick="saveNews()">Save Changes</button>
						<button class="btn btn-secondary" onclick="loadNews()">Reload</button>
					</div>
				</div>
			</div>
			
			<!-- Feedback Section -->
			<div id="section-feedback" class="section hidden">
				<div class="card">
					<h2>Feedback Logs</h2>
					<div id="feedback-list">
						<div class="loading">Loading feedback...</div>
					</div>
				</div>
			</div>
		</div>
	</div>
	
	<script>
		let maintenanceData = null;
		let updatesData = null;
		let newsData = null;
		
		function getToken() {
			return localStorage.getItem('accessToken');
		}
		
		function checkAuth() {
			if (!getToken()) {
				window.location.href = '/app/login';
			}
		}
		
		function logout() {
			localStorage.removeItem('accessToken');
			localStorage.removeItem('refreshToken');
			window.location.href = '/app/login';
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
			
			if (name === 'maintenance' && !maintenanceData) loadMaintenance();
			if (name === 'updates' && !updatesData) loadUpdates();
			if (name === 'news' && !newsData) loadNews();
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
			const res = await fetch(path, opts);
			if (res.status === 401 || res.status === 403) {
				logout();
				return null;
			}
			return res;
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
		}
		
		async function saveMaintenance() {
			const payload = {
				maintenance: {
					maintenanceActive: document.getElementById('maint-active').checked,
					maintenanceInfo: {
						status: document.getElementById('maint-status').value,
						message: document.getElementById('maint-message').value,
						startTime: document.getElementById('maint-start').value ? new Date(document.getElementById('maint-start').value).toISOString() : '',
						exEndTime: document.getElementById('maint-end').value ? new Date(document.getElementById('maint-end').value).toISOString() : '',
						posterUrl: document.getElementById('maint-poster').value,
						link: document.getElementById('maint-link').value
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
				html += '<h3>' + platform.charAt(0).toUpperCase() + platform.slice(1) + '</h3>';
				if (pdata.architectures) {
					for (const [arch, adata] of Object.entries(pdata.architectures)) {
						html += '<div class="arch-item">';
						html += '<h4>' + arch + '</h4>';
						html += '<div class="form-row">';
						html += '<div class="form-group"><label>Core Version</label>';
						html += '<input type="text" id="upd-' + platform + '-' + arch + '-core" value="' + (adata.latest?.coreVersion || '') + '" /></div>';
						html += '<div class="form-group"><label>Resource Version</label>';
						html += '<input type="text" id="upd-' + platform + '-' + arch + '-res" value="' + (adata.latest?.resourceVersion || '') + '" /></div>';
						html += '</div>';
						html += '</div>';
					}
				}
				html += '</div>';
			}
			container.innerHTML = html || '<p style="color:#94a3b8;">No platforms configured.</p>';
		}
		
		async function saveUpdates() {
			// Collect updated versions from inputs
			if (updatesData && updatesData.platforms) {
				for (const [platform, pdata] of Object.entries(updatesData.platforms)) {
					if (pdata.architectures) {
						for (const [arch, adata] of Object.entries(pdata.architectures)) {
							const coreInput = document.getElementById('upd-' + platform + '-' + arch + '-core');
							const resInput = document.getElementById('upd-' + platform + '-' + arch + '-res');
							if (coreInput && adata.latest) adata.latest.coreVersion = coreInput.value;
							if (resInput && adata.latest) adata.latest.resourceVersion = resInput.value;
						}
					}
				}
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
				html += '<div style="margin-bottom:4px;">' + f.fileName + ' <span style="color:#94a3b8;">(' + f.checksum.substring(0,16) + '...)</span></div>';
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
			let html = '';
			newsData.items.forEach((item, idx) => {
				html += '<div class="news-item" id="news-item-' + idx + '">';
				html += '<div class="header-row"><h3>' + (item.title || 'Untitled') + '</h3>';
				html += '<button class="btn btn-danger" onclick="removeNewsItem(' + idx + ')">Remove</button></div>';
				html += '<div class="form-row">';
				html += '<div class="form-group"><label>ID</label><input type="text" value="' + (item.id || '') + '" onchange="updateNewsField(' + idx + ', \'id\', this.value)" /></div>';
				html += '<div class="form-group"><label>Title</label><input type="text" value="' + (item.title || '') + '" onchange="updateNewsField(' + idx + ', \'title\', this.value)" /></div>';
				html += '</div>';
				html += '<div class="form-group"><label>Summary</label><textarea onchange="updateNewsField(' + idx + ', \'summary\', this.value)">' + (item.summary || '') + '</textarea></div>';
				html += '<div class="form-row">';
				html += '<div class="form-group"><label>Category</label><input type="text" value="' + (item.category || '') + '" onchange="updateNewsField(' + idx + ', \'category\', this.value)" /></div>';
				html += '<div class="form-group"><label>Priority</label><input type="number" value="' + (item.priority || 0) + '" onchange="updateNewsField(' + idx + ', \'priority\', parseInt(this.value))" /></div>';
				html += '</div>';
				html += '<div class="form-group"><label>Link</label><input type="text" value="' + (item.link || '') + '" onchange="updateNewsField(' + idx + ', \'link\', this.value)" /></div>';
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
		
		// Feedback functions
		async function loadFeedback() {
			const res = await apiRequest('GET', '/v0/api/feedbackLogs?limit=50');
			if (!res) return;
			const data = await res.json();
			const container = document.getElementById('feedback-list');
			if (!data.feedbackLogs || data.feedbackLogs.length === 0) {
				container.innerHTML = '<p style="color:#94a3b8;">No feedback entries.</p>';
				return;
			}
			let html = '<table style="width:100%;border-collapse:collapse;">';
			html += '<thead><tr style="border-bottom:1px solid #374151;"><th style="text-align:left;padding:8px;">Time</th><th style="text-align:left;padding:8px;">Content</th></tr></thead>';
			html += '<tbody>';
			data.feedbackLogs.forEach(item => {
				html += '<tr style="border-bottom:1px solid #1f2937;">';
				html += '<td style="padding:8px;color:#94a3b8;white-space:nowrap;">' + (item.receivedAt || '') + '</td>';
				html += '<td style="padding:8px;white-space:pre-wrap;">' + (item.content || '') + '</td>';
				html += '</tr>';
			});
			html += '</tbody></table>';
			container.innerHTML = html;
		}
		
		// Initialize
		checkAuth();
		loadMaintenance();
	</script>
</body>
</html>`

func (s *Server) handleAppAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(appAdminPage))
}
