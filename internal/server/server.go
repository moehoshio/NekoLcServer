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
	appConfig         *config.AppConfig
	router            chi.Router
	launcherConfig    *config.LauncherConfig
	maintenanceConfig *config.MaintenanceConfig
	newsItems         []config.NewsItem
	updateConfig      *config.UpdateConfig
	updateConfigPath  string
	updateAssetsDir   string
	localizer         *localization.Localizer
	authService       *auth.Service
	store             store.Store
	feedbackLogPath   string
	debug             bool
	basePath          string
	dirCacheMu        sync.RWMutex
	dirCache          map[string]dirCacheEntry
	updateConfigMu    sync.RWMutex
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
	newsCfg *config.NewsConfig,
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
		appConfig:         appCfg,
		launcherConfig:    launcherCfg,
		maintenanceConfig: maintenanceCfg,
		newsItems:         normalizeNewsItems(newsCfg),
		updateConfig:      updateCfg,
		updateConfigPath:  updateCfgPath,
		updateAssetsDir:   updateAssetsDir,
		localizer:         localizer,
		authService:       authSvc,
		store:             store,
		feedbackLogPath:   feedbackPath,
		debug:             appCfg.Debug.Enabled,
		basePath:          normalizeBasePath(appCfg.Server.BasePath),
		dirCache:          map[string]dirCacheEntry{},
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
		})

		if s.debug {
			router.Get("/debug/feedback", s.handleFeedbackView)
			router.Get("/debug/feedback.json", s.handleFeedbackJSON)
		}

		router.Get("/app/login", s.handleAppLogin)
		router.Get("/app/feedback", s.handleAppFeedback)
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
		input { width: 100%; padding: 10px; margin-top: 6px; border-radius: 8px; border: 1px solid #1f2937; background: #0b1220; color: #e2e8f0; }
		button { width: 100%; margin-top: 18px; padding: 12px; border: none; border-radius: 10px; background: linear-gradient(120deg,#22d3ee,#818cf8); color: #0b1220; font-weight: 700; cursor: pointer; }
		button:hover { filter: brightness(1.05); }
		.error { color: #f87171; margin-top: 10px; min-height: 20px; }
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
			window.location.href = '/app/feedback';
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
