package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// resolveSafePath resolves a user-provided path relative to a base directory,
// ensuring the resolved path stays within the base directory to prevent path traversal.
func resolveSafePath(basePath, userPath string) (string, error) {
	if userPath == "" {
		return "", errors.New("path is required")
	}
	// Clean the user path first
	cleanPath := filepath.Clean(userPath)
	// If path is absolute, check it's within base
	if filepath.IsAbs(cleanPath) {
		if basePath != "" {
			absBase, err := filepath.Abs(basePath)
			if err != nil {
				return "", err
			}
			// Ensure the absolute path is within base directory
			if !strings.HasPrefix(cleanPath, absBase) {
				return "", errors.New("path must be within the assets directory")
			}
		}
		return cleanPath, nil
	}
	// For relative paths, join with base and ensure it stays within
	if basePath == "" {
		return "", errors.New("relative paths require a base directory")
	}
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return "", err
	}
	resolved := filepath.Join(absBase, cleanPath)
	resolved = filepath.Clean(resolved)
	// Verify the resolved path is still within the base directory
	if !strings.HasPrefix(resolved, absBase) {
		return "", errors.New("path traversal detected")
	}
	return resolved, nil
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	type pingResponse struct {
		Message string `json:"message"`
		Meta    Meta   `json:"meta"`
	}
	resp := pingResponse{Message: "pong", Meta: s.meta()}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleEcho(w http.ResponseWriter, r *http.Request) {
	if !s.debug {
		http.NotFound(w, r)
		return
	}
	if r.Body == nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "Body is required")
		return
	}
	limited := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer limited.Close()
	data, err := io.ReadAll(limited)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	type echoResponse struct {
		Echo json.RawMessage `json:"echo"`
		Meta Meta            `json:"meta"`
	}
	resp := echoResponse{Echo: json.RawMessage(data), Meta: s.meta()}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil || !s.authService.Enabled() {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Authentication is disabled")
		return
	}
	var payload LoginPayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.languageFromPreferences(payload.Preferences), "InvalidRequest", err.Error())
		return
	}
	lang := s.languageFromPreferences(payload.Preferences)
	req := payload.LoginRequest

	// Try username/password login if store is available and credentials provided
	if s.store != nil && req.Username != "" && req.Password != "" {
		username := strings.TrimSpace(req.Username)
		password := strings.TrimSpace(req.Password)
		user, err := s.lookupUser(username)
		if err != nil {
			s.writeError(w, http.StatusUnauthorized, lang, "Unauthorized", "invalid credentials")
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
			s.writeError(w, http.StatusUnauthorized, lang, "Unauthorized", "invalid credentials")
			return
		}
		subject := fmt.Sprintf("user:%d", user.ID)
		access, refresh, err := s.authService.IssueTokens(subject, user.Role)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, lang, "InternalError", err.Error())
			return
		}
		body := LoginResponseBody{Meta: s.meta()}
		body.LoginResponse.AccessToken = access
		body.LoginResponse.RefreshToken = refresh
		body.LoginResponse.UserID = user.ID
		body.LoginResponse.Username = user.Username
		body.LoginResponse.Role = user.Role
		s.writeJSON(w, http.StatusOK, body)
		return
	}

	// Fall back to JWT signature-based login or require username/password for mysql method
	switch s.authMethod() {
	case "mysql":
		// Username/password is required for MySQL mode but was not provided
		s.writeError(w, http.StatusBadRequest, lang, "InvalidRequest", "username and password are required")
		return
	case "jwt":
		if req.Identifier == "" || req.Signature == "" || req.Timestamp == 0 {
			s.writeError(w, http.StatusBadRequest, lang, "InvalidRequest", "identifier, signature, and timestamp are required for JWT login")
			return
		}
		if err := s.validateSignature(req); err != nil {
			s.writeError(w, http.StatusUnauthorized, lang, "Unauthorized", err.Error())
			return
		}
	default:
		s.writeError(w, http.StatusNotImplemented, lang, "NotImplemented", "Authentication method not supported")
		return
	}
	access, refresh, err := s.authService.IssueTokens(req.Identifier, "user")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, lang, "InternalError", err.Error())
		return
	}
	body := LoginResponseBody{Meta: s.meta()}
	body.LoginResponse.AccessToken = access
	body.LoginResponse.RefreshToken = refresh
	s.writeJSON(w, http.StatusOK, body)
}

func (s *Server) validateSignature(req LoginRequest) error {
	if req.Timestamp == 0 {
		return errors.New("timestamp missing")
	}
	if !s.appConfig.Authentication.IgnoreTokenExpiration {
		now := time.Now().UTC().Unix()
		delta := now - req.Timestamp
		if delta < 0 {
			delta = -delta
		}
		if delta > 600 {
			return errors.New("timestamp outside allowed window")
		}
	}
	if !s.authService.VerifySignature(req.Identifier, req.Timestamp, req.Signature) {
		return errors.New("invalid signature")
	}
	return nil
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil || !s.authService.Enabled() {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Authentication is disabled")
		return
	}
	var payload RefreshPayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.languageFromPreferences(payload.Preferences), "InvalidRequest", err.Error())
		return
	}
	lang := s.languageFromPreferences(payload.Preferences)
	refreshToken := strings.TrimSpace(payload.RefreshRequest.RefreshToken)
	if refreshToken == "" {
		s.writeError(w, http.StatusBadRequest, lang, "InvalidRequest", "refreshToken is required")
		return
	}
	access, err := s.authService.Refresh(refreshToken)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, lang, "Unauthorized", err.Error())
		return
	}
	body := RefreshResponseBody{Meta: s.meta()}
	body.RefreshResponse.AccessToken = access
	s.writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil || !s.authService.Enabled() {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Authentication is disabled")
		return
	}
	var payload ValidatePayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.languageFromPreferences(payload.Preferences), "InvalidRequest", err.Error())
		return
	}
	lang := s.languageFromPreferences(payload.Preferences)
	token := strings.TrimSpace(payload.ValidateRequest.AccessToken)
	if token == "" {
		s.writeError(w, http.StatusBadRequest, lang, "InvalidRequest", "accessToken is required")
		return
	}
	if !s.authService.ValidateAccess(token) {
		s.writeError(w, http.StatusUnauthorized, lang, "Unauthorized", "accessToken invalid or expired")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil || !s.authService.Enabled() {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Authentication is disabled")
		return
	}
	var payload LogoutPayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.languageFromPreferences(payload.Preferences), "InvalidRequest", err.Error())
		return
	}
	access := strings.TrimSpace(payload.LogoutRequest.AccessToken)
	refresh := strings.TrimSpace(payload.LogoutRequest.RefreshToken)
	if access == "" && refresh == "" {
		s.writeError(w, http.StatusBadRequest, s.languageFromPreferences(payload.Preferences), "InvalidRequest", "accessToken or refreshToken required")
		return
	}
	s.authService.Revoke(access, refresh)
	w.WriteHeader(http.StatusNoContent)
}

// RegisterGetResponse is the response body for GET /v0/api/auth/register
// Per API spec: returns registerResponse.registerUrl
type RegisterGetResponse struct {
	RegisterResponse struct {
		RegisterURL string `json:"registerUrl"`
	} `json:"registerResponse"`
	Meta Meta `json:"meta"`
}

func (s *Server) handleRegisterInfo(w http.ResponseWriter, r *http.Request) {
	// Per API spec: return HTTP 200 with registerResponse.registerUrl
	// The registerUrl should point to the UI registration page
	registerURL := s.basePath + "/app/register"
	if s.launcherConfig != nil && s.launcherConfig.Security.UI.RegisterURL != "" {
		registerURL = s.prependBasePath(s.launcherConfig.Security.UI.RegisterURL)
	}

	resp := RegisterGetResponse{
		Meta: s.meta(),
	}
	resp.RegisterResponse.RegisterURL = registerURL
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLauncherConfig(w http.ResponseWriter, r *http.Request) {
	var payload LauncherConfigPayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.languageFromPreferences(payload.Preferences), "InvalidRequest", err.Error())
		return
	}
	if s.launcherConfig == nil {
		s.writeError(w, http.StatusInternalServerError, s.languageFromPreferences(payload.Preferences), "InternalError", "Launcher configuration missing")
		return
	}
	// Create a copy of the launcher config with basePath prepended to security URLs
	cfgCopy := *s.launcherConfig
	cfgCopy.Security.LoginURL = s.prependBasePath(cfgCopy.Security.LoginURL)
	cfgCopy.Security.LogoutURL = s.prependBasePath(cfgCopy.Security.LogoutURL)
	cfgCopy.Security.RefreshURL = s.prependBasePath(cfgCopy.Security.RefreshURL)
	cfgCopy.Security.RegisterURL = s.prependBasePath(cfgCopy.Security.RegisterURL)
	cfgCopy.Security.UI.RegisterURL = s.prependBasePath(cfgCopy.Security.UI.RegisterURL)

	body := LauncherConfigResponseBody{
		LauncherConfigResponse: cfgCopy,
		Meta:                   s.meta(),
	}
	s.writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleMaintenance(w http.ResponseWriter, r *http.Request) {
	var payload MaintenancePayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.languageFromPreferences(payload.Preferences), "InvalidRequest", err.Error())
		return
	}
	lang := s.languageFromPreferences(payload.Preferences)
	info, ok := s.maintenanceForClient(payload.MaintenanceRequest.ClientInfo)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Apply localized messages if available
	info = s.localizeMaintenanceInfo(info, lang)
	if info.Message == "" && s.localizer != nil {
		info.Message = s.localizer.Maintenance(lang, info.Status)
	}
	body := MaintenanceResponseBody{
		MaintenanceResponse: info,
		Meta:                s.meta(),
	}
	s.writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleCheckUpdates(w http.ResponseWriter, r *http.Request) {
	if s.updateConfig == nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", "Update configuration missing")
		return
	}
	var payload UpdatePayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.languageFromPreferences(payload.Preferences), "InvalidRequest", err.Error())
		return
	}
	lang := s.languageFromPreferences(payload.Preferences)
	client := payload.UpdateRequest.ClientInfo
	if client == nil || client.App == nil || client.System == nil {
		s.writeError(w, http.StatusBadRequest, lang, "InvalidRequest", "clientInfo.app and clientInfo.system are required")
		return
	}
	if info, active := s.maintenanceForClient(client); active {
		if info.Message == "" && s.localizer != nil {
			info.Message = s.localizer.Maintenance(lang, info.Status)
		}
		body := MaintenanceResponseBody{MaintenanceResponse: info, Meta: s.meta()}
		s.writeJSON(w, http.StatusServiceUnavailable, body)
		return
	}
	files, needsUpdate, latest := s.resolveUpdateFiles(client)
	if !needsUpdate {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	isMandatory := false
	if latest.CoreVersion != "" && client.App != nil {
		coreCurrent := normalizeVersion(client.App.CoreVersion)
		latestCore := normalizeVersion(latest.CoreVersion)
		isMandatory = latestCore != "" && latestCore != coreCurrent
	}
	title := "New version available"
	description := "Updates available"
	if s.localizer != nil {
		if msg := s.localizer.Updates(lang, "available"); msg != "" {
			title = msg
		}
		if msg := s.localizer.Updates(lang, "description"); msg != "" {
			description = msg
		}
	}
	resp := UpdateResponseBody{
		UpdateResponse: UpdateResponsePayload{
			Title:           title,
			Description:     description,
			PosterURL:       "",
			PublishTime:     time.Now().UTC().Format(time.RFC3339),
			ResourceVersion: latest.ResourceVersion,
			IsMandatory:     isMandatory,
			Files:           files,
		},
		Meta: s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) filterNewsItems(categories []string) []config.NewsItem {
	if len(s.newsItems) == 0 {
		return nil
	}
	if len(categories) == 0 {
		return cloneNewsItems(s.newsItems)
	}
	allowed := map[string]struct{}{}
	for _, cat := range categories {
		trimmed := strings.ToLower(strings.TrimSpace(cat))
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return cloneNewsItems(s.newsItems)
	}
	filtered := []config.NewsItem{}
	for _, item := range s.newsItems {
		cat := strings.ToLower(strings.TrimSpace(item.Category))
		if _, ok := allowed[cat]; ok {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func findNewsIndex(items []config.NewsItem, lastID string) int {
	target := strings.TrimSpace(lastID)
	if target == "" {
		return -1
	}
	for i, item := range items {
		if strings.TrimSpace(item.ID) == target {
			return i
		}
	}
	return -1
}

func cloneNewsItems(items []config.NewsItem) []config.NewsItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]config.NewsItem, len(items))
	copy(out, items)
	return out
}

func (s *Server) handleNews(w http.ResponseWriter, r *http.Request) {
	var payload NewsPayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.languageFromPreferences(payload.Preferences), "InvalidRequest", err.Error())
		return
	}
	lang := s.languageFromPreferences(payload.Preferences)
	req := payload.NewsRequest
	if info, active := s.maintenanceForClient(req.ClientInfo); active {
		if info.Message == "" && s.localizer != nil {
			info.Message = s.localizer.Maintenance(lang, info.Status)
		}
		body := MaintenanceResponseBody{MaintenanceResponse: info, Meta: s.meta()}
		s.writeJSON(w, http.StatusServiceUnavailable, body)
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	} else if limit > 50 {
		limit = 50
	}
	items := s.filterNewsItems(req.Categories)
	if req.LastID != "" {
		idx := findNewsIndex(items, req.LastID)
		if idx == -1 {
			s.writeError(w, http.StatusBadRequest, lang, "InvalidRequest", "lastId not found")
			return
		}
		if idx+1 >= len(items) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		items = items[idx+1:]
	}
	if len(items) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	hasMore := false
	if len(items) > limit {
		hasMore = true
		items = items[:limit]
	}
	resp := NewsResponseBody{
		NewsResponse: NewsResponsePayload{
			Items:   cloneNewsItems(items),
			HasMore: hasMore,
		},
		Meta: s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) resolveUpdateFiles(client *ClientInfo) ([]UpdateFileResponse, bool, config.FullPackage) {
	updateCfg := s.currentUpdateConfig()
	if updateCfg == nil {
		return nil, false, config.FullPackage{}
	}
	archCfg, ok := s.archUpdatesWithConfig(client.System, updateCfg)
	if !ok {
		return nil, false, config.FullPackage{}
	}

	latest := archCfg.Latest
	coreLatest := normalizeVersion(latest.CoreVersion)
	resLatest := normalizeVersion(latest.ResourceVersion)
	coreCurrent := ""
	resCurrent := ""
	if client.App != nil {
		coreCurrent = normalizeVersion(client.App.CoreVersion)
		resCurrent = normalizeVersion(client.App.ResourceVersion)
	}

	needCore := coreLatest != "" && !strings.EqualFold(coreLatest, coreCurrent)
	needResource := resLatest != "" && !strings.EqualFold(resLatest, resCurrent)

	if !needCore && !needResource {
		return nil, false, latest
	}

	var files []UpdateFileResponse

	if needCore {
		coreSatisfied := false
		if diff := findDiff(archCfg.Diffs, coreCurrent, true); diff != nil {
			if entries := s.filesFromEntries(diff.Core, true); len(entries) > 0 {
				files = append(files, entries...)
				coreSatisfied = true
			}
		}
		if !coreSatisfied {
			if entries := s.filesFromEntries(latest.Core, true); len(entries) > 0 {
				files = append(files, entries...)
			}
		}
	}

	if needResource {
		resSatisfied := false
		if diff := findDiff(archCfg.Diffs, resCurrent, false); diff != nil {
			if entries := s.filesFromEntries(diff.Resource, false); len(entries) > 0 {
				files = append(files, entries...)
				resSatisfied = true
			}
		}
		if !resSatisfied {
			if entries := s.filesFromEntries(latest.Resource, false); len(entries) > 0 {
				files = append(files, entries...)
			}
		}
	}

	if len(files) == 0 && (coreCurrent == "" || resCurrent == "") {
		files = append(files, s.filesFromEntries(latest.Core, true)...)
		files = append(files, s.filesFromEntries(latest.Resource, false)...)
	}

	return files, len(files) > 0, latest
}

func findDiff(diffs []config.DiffFile, clientVersion string, isCore bool) *config.DiffFile {
	for i := range diffs {
		diff := &diffs[i]
		if isCore {
			candidate := normalizeVersion(diff.FromCoreVersion)
			if candidate != "" && candidate == clientVersion {
				return diff
			}
			continue
		}
		candidate := normalizeVersion(diff.FromResourceVersion)
		if candidate != "" && candidate == clientVersion {
			return diff
		}
	}
	return nil
}

func normalizeVersion(v string) string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "v") || strings.HasPrefix(trimmed, "V") {
		trimmed = trimmed[1:]
	}
	return trimmed
}

func (s *Server) archUpdatesWithConfig(system *SystemInfo, updateCfg *config.UpdateConfig) (config.ArchUpdates, bool) {
	if system == nil || updateCfg == nil {
		return config.ArchUpdates{}, false
	}
	platform, ok := findPlatform(updateCfg.Platforms, system.OS)
	if !ok {
		return config.ArchUpdates{}, false
	}
	arch, ok := findArch(platform.Architectures, system.Arch)
	if !ok {
		return config.ArchUpdates{}, false
	}
	return arch, true
}

func findPlatform(platforms map[string]config.PlatformUpdates, osName string) (config.PlatformUpdates, bool) {
	for key, platform := range platforms {
		if strings.EqualFold(key, osName) {
			return platform, true
		}
	}
	return config.PlatformUpdates{}, false
}

func findArch(architectures map[string]config.ArchUpdates, arch string) (config.ArchUpdates, bool) {
	for key, cfg := range architectures {
		if strings.EqualFold(key, arch) {
			return cfg, true
		}
	}
	return config.ArchUpdates{}, false
}

func (s *Server) filesFromEntries(entries []config.DownloadEntry, isCore bool) []UpdateFileResponse {
	var out []UpdateFileResponse
	for _, entry := range entries {
		out = append(out, s.filesFromEntry(entry, isCore)...)
	}
	return out
}

func (s *Server) filesFromEntry(entry config.DownloadEntry, isCore bool) []UpdateFileResponse {
	url := strings.TrimSpace(entry.URL)
	path := strings.TrimSpace(entry.Path)
	meta := entry.DownloadMeta
	if meta.HashAlgorithm == "" {
		meta.HashAlgorithm = "sha256"
	}

	// Path may describe a local directory (preferred) or a JSON list of URLs.
	if url == "" && path != "" {
		resolved := s.resolveUpdateAssetPath(path)
		if info, err := os.Stat(resolved); err == nil && info.IsDir() {
			return s.filesFromDirectory(resolved, entry, isCore)
		}
		return s.diffFilesFromPathWithMeta(path, entry, isCore)
	}

	if url == "" {
		return nil
	}
	lower := strings.ToLower(url)
	abs := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || filepath.IsAbs(url)
	fileName := entry.FileName
	if strings.TrimSpace(fileName) == "" {
		fileName = filepath.Base(url)
	}
	return []UpdateFileResponse{createFileResponse(url, fileName, entry.Checksum, entry.Size, meta, isCore, abs)}
}

func createFileResponse(url, fileName, checksum string, size int64, meta config.DownloadMeta, isCore, abs bool) UpdateFileResponse {
	checksum = normalizeChecksum(checksum)
	return UpdateFileResponse{
		URL:      url,
		FileName: fileName,
		Checksum: checksum,
		Size:     size,
		DownloadMeta: DownloadMeta{
			HashAlgorithm:      meta.HashAlgorithm,
			SuggestMultiThread: meta.SuggestMultiThread,
			IsCoreFile:         isCore,
			IsAbsoluteURL:      abs,
		},
	}
}

func (s *Server) filesFromDirectory(root string, entry config.DownloadEntry, isCore bool) []UpdateFileResponse {
	info, err := os.Stat(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stat directory %s: %v\n", root, err)
		return nil
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "path %s is not a directory\n", root)
		return nil
	}

	currentMod, err := latestModTime(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stat directory %s: %v\n", root, err)
		return nil
	}
	meta := entry.DownloadMeta
	baseURL := strings.TrimSpace(entry.BaseURL)
	if meta.HashAlgorithm == "" {
		meta.HashAlgorithm = "sha256"
	}
	s.dirCacheMu.RLock()
	cached, ok := s.dirCache[root]
	s.dirCacheMu.RUnlock()
	if ok && !currentMod.After(cached.lastModified) && cached.hashAlgorithm == meta.HashAlgorithm && cached.baseURL == baseURL && cached.isCore == isCore {
		return cloneUpdateFiles(cached.files)
	}

	files, maxMod, scanErr := collectDirectoryFiles(root, meta, baseURL, isCore)
	if scanErr != nil {
		fmt.Fprintf(os.Stderr, "scan directory %s: %v\n", root, scanErr)
		if ok {
			return cloneUpdateFiles(cached.files)
		}
		return nil
	}
	if maxMod.Before(currentMod) {
		maxMod = currentMod
	}

	s.dirCacheMu.Lock()
	s.dirCache[root] = dirCacheEntry{files: cloneUpdateFiles(files), lastModified: maxMod, hashAlgorithm: meta.HashAlgorithm, baseURL: baseURL, isCore: isCore}
	s.dirCacheMu.Unlock()
	return files
}

func collectDirectoryFiles(root string, meta config.DownloadMeta, baseURL string, isCore bool) ([]UpdateFileResponse, time.Time, error) {
	files := []UpdateFileResponse{}
	maxMod := time.Time{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		checksum, err := hashFile(path, meta.HashAlgorithm)
		if err != nil {
			return err
		}
		url, abs := buildURL(baseURL, rel)
		files = append(files, createFileResponse(url, rel, checksum, info.Size(), meta, isCore, abs))
		if info.ModTime().After(maxMod) {
			maxMod = info.ModTime()
		}
		return nil
	})
	return files, maxMod, err
}

func buildURL(baseURL, relative string) (string, bool) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return relative, false
	}
	base = strings.TrimRight(base, "/") + "/"
	url := base + relative
	lower := strings.ToLower(base)
	abs := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || filepath.IsAbs(base)
	return url, abs
}

func latestModTime(root string) (time.Time, error) {
	maxMod := time.Time{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(maxMod) {
			maxMod = info.ModTime()
		}
		return nil
	})
	return maxMod, err
}

func hashFile(path, algorithm string) (string, error) {
	algo := strings.ToLower(strings.TrimSpace(algorithm))
	if algo != "" && algo != "sha256" {
		return "", fmt.Errorf("unsupported hash algorithm: %s", algorithm)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func normalizeChecksum(checksum string) string {
	trimmed := strings.TrimSpace(checksum)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "sha256:") {
		return trimmed[len("sha256:"):]
	}
	return trimmed
}

func cloneUpdateFiles(files []UpdateFileResponse) []UpdateFileResponse {
	if len(files) == 0 {
		return nil
	}
	out := make([]UpdateFileResponse, len(files))
	copy(out, files)
	return out
}

func deviceIDFromClient(info *ClientInfo) string {
	if info == nil {
		return ""
	}
	return strings.TrimSpace(info.DeviceID)
}

func (s *Server) diffFilesFromPathWithMeta(path string, entry config.DownloadEntry, isCore bool) []UpdateFileResponse {
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
	meta := entry.DownloadMeta
	if meta.HashAlgorithm == "" {
		meta.HashAlgorithm = "sha256"
	}
	files := make([]UpdateFileResponse, 0, len(urls))
	for _, raw := range urls {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		abs := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || filepath.IsAbs(trimmed)
		fileName := entry.FileName
		if strings.TrimSpace(fileName) == "" {
			fileName = filepath.Base(trimmed)
		}
		files = append(files, createFileResponse(trimmed, fileName, entry.Checksum, 0, meta, isCore, abs))
	}
	return files
}

func (s *Server) handleFeedbackLog(w http.ResponseWriter, r *http.Request) {
	var payload FeedbackLogPayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.languageFromPreferences(payload.Preferences), "InvalidRequest", err.Error())
		return
	}
	lang := s.languageFromPreferences(payload.Preferences)
	req := payload.FeedbackLogRequest
	if strings.TrimSpace(req.Content) == "" {
		s.writeError(w, http.StatusBadRequest, lang, "InvalidRequest", "content is required")
		return
	}
	if s.store != nil {
		clientInfo, _ := json.Marshal(req.ClientInfo)
		entry := store.FeedbackLog{
			DeviceID:   deviceIDFromClient(req.ClientInfo),
			Lang:       lang,
			ClientInfo: clientInfo,
			Content:    req.Content,
			ReceivedAt: time.Now().UTC(),
			Timestamp:  req.Timestamp,
		}
		// Extract filterable fields from client info
		if req.ClientInfo != nil {
			if req.ClientInfo.App != nil {
				entry.CoreVersion = req.ClientInfo.App.CoreVersion
				entry.ResourceVersion = req.ClientInfo.App.ResourceVersion
				entry.BuildID = req.ClientInfo.App.BuildID
			}
			if req.ClientInfo.System != nil {
				entry.Platform = req.ClientInfo.System.OS
				entry.Arch = req.ClientInfo.System.Arch
			}
			// Try to extract region from extra field if present
			if req.ClientInfo.Extra != nil {
				if region, ok := req.ClientInfo.Extra["region"].(string); ok {
					entry.Region = region
				}
			}
		}
		if err := s.store.SaveFeedback(context.Background(), entry); err != nil {
			s.writeError(w, http.StatusInternalServerError, lang, "InternalError", err.Error())
			return
		}
	} else {
		entry := map[string]interface{}{
			"receivedAt": time.Now().UTC().Format(time.RFC3339),
			"clientInfo": req.ClientInfo,
			"timestamp":  req.Timestamp,
			"content":    req.Content,
		}
		if err := s.logFeedback(entry); err != nil {
			s.writeError(w, http.StatusInternalServerError, lang, "InternalError", err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFeedbackLogs(w http.ResponseWriter, r *http.Request) {
	claims, err := s.requireAdmin(w, r)
	if err != nil {
		return
	}
	_ = claims
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "feedback storage not configured")
		return
	}
	limit, offset := parseLimitOffset(r)

	// Parse filter parameters
	query := r.URL.Query()
	filter := store.FeedbackFilter{
		CoreVersion:     query.Get("coreVersion"),
		ResourceVersion: query.Get("resourceVersion"),
		BuildID:         query.Get("buildId"),
		Platform:        query.Get("platform"),
		Arch:            query.Get("arch"),
		Region:          query.Get("region"),
		Lang:            query.Get("lang"),
	}

	// Parse time filters
	if startStr := query.Get("startTime"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			filter.StartTime = &t
		}
	}
	if endStr := query.Get("endTime"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			filter.EndTime = &t
		}
	}

	// Use filtered query if any filters are set
	var entries []store.FeedbackLog
	hasFilters := filter.CoreVersion != "" || filter.ResourceVersion != "" || filter.BuildID != "" ||
		filter.Platform != "" || filter.Arch != "" || filter.Region != "" || filter.Lang != "" ||
		filter.StartTime != nil || filter.EndTime != nil

	if hasFilters {
		entries, err = s.store.ListFeedbackFiltered(r.Context(), filter, limit, offset)
	} else {
		entries, err = s.store.ListFeedback(r.Context(), limit, offset)
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	items := make([]FeedbackLogItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, FeedbackLogItem{
			ID:              e.ID,
			UserID:          nullUserID(e.UserID),
			DeviceID:        e.DeviceID,
			Lang:            e.Lang,
			ClientInfo:      jsonRaw(e.ClientInfo),
			Content:         e.Content,
			ReceivedAt:      e.ReceivedAt.Format(time.RFC3339),
			Timestamp:       e.Timestamp,
			CoreVersion:     e.CoreVersion,
			ResourceVersion: e.ResourceVersion,
			BuildID:         e.BuildID,
			Platform:        e.Platform,
			Arch:            e.Arch,
			Region:          e.Region,
		})
	}
	resp := FeedbackLogsResponseBody{
		FeedbackLogs: items,
		Count:        len(items),
		Meta:         s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// handleFeedbackFilterOptions returns available filter options for feedback logs.
func (s *Server) handleFeedbackFilterOptions(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "feedback storage not configured")
		return
	}
	options, err := s.store.GetFeedbackFilterOptions(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	resp := FeedbackFilterOptionsResponse{
		Options: *options,
		Meta:    s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// Admin handlers for configuration management

func (s *Server) handleAdminGetLauncher(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	cfg := s.launcherConfig
	if cfg == nil {
		cfg = &config.LauncherConfig{}
	}
	resp := AdminLauncherResponse{
		Launcher: *cfg,
		Meta:     s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminUpdateLauncher(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	var payload AdminLauncherUpdatePayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	// Update in-memory config
	s.launcherConfig = &payload.Launcher
	// Save to database and file
	if err := s.saveLauncherConfig(); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	resp := AdminMessageResponse{
		Message: "Launcher configuration updated successfully",
		Meta:    s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminGetMaintenance(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	cfg := s.maintenanceConfig
	if cfg == nil {
		cfg = &config.MaintenanceConfig{}
	}
	resp := AdminMaintenanceResponse{
		Maintenance: *cfg,
		Meta:        s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminUpdateMaintenance(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	var payload AdminMaintenanceUpdatePayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	// Update in-memory config
	s.maintenanceConfig = &payload.Maintenance
	// Save to file
	if err := s.saveMaintenanceConfig(); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	resp := AdminMessageResponse{
		Message: "Maintenance configuration updated successfully",
		Meta:    s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminGetUpdates(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	cfg := s.currentUpdateConfig()
	if cfg == nil {
		cfg = &config.UpdateConfig{Platforms: map[string]config.PlatformUpdates{}}
	}
	resp := AdminUpdatesResponse{
		Updates: *cfg,
		Meta:    s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminUpdateUpdates(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	var payload AdminUpdatesUpdatePayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	// Update in-memory config - acquire locks in consistent order and release in reverse order
	s.dirCacheMu.Lock()
	s.dirCache = map[string]dirCacheEntry{}
	s.dirCacheMu.Unlock()

	s.updateConfigMu.Lock()
	s.updateConfig = &payload.Updates
	s.updateConfigMu.Unlock()
	// Save to file
	if err := s.saveUpdatesConfig(); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	resp := AdminMessageResponse{
		Message: "Updates configuration updated successfully",
		Meta:    s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminGetNews(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	items := s.newsItems
	if items == nil {
		items = []config.NewsItem{}
	}
	resp := AdminNewsResponse{
		News: config.NewsConfig{Items: items},
		Meta: s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminUpdateNews(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	var payload AdminNewsUpdatePayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	// Update in-memory config
	s.newsItems = normalizeNewsItems(&payload.News)
	// Save to file
	if err := s.saveNewsConfig(&payload.News); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	resp := AdminMessageResponse{
		Message: "News configuration updated successfully",
		Meta:    s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminScanPath(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	var payload AdminScanPathPayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	path := strings.TrimSpace(payload.Path)
	// Resolve path securely to prevent path traversal
	resolved, err := resolveSafePath(s.updateAssetsDir, path)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	// Check if path exists and is a directory
	info, err := os.Stat(resolved)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "path does not exist: "+err.Error())
		return
	}
	if !info.IsDir() {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "path is not a directory")
		return
	}
	// Scan the directory and compute checksums
	entry := config.DownloadEntry{
		Path:    resolved,
		BaseURL: strings.TrimSpace(payload.BaseURL),
		DownloadMeta: config.DownloadMeta{
			HashAlgorithm:      "sha256",
			SuggestMultiThread: false,
		},
	}
	files := s.filesFromDirectory(resolved, entry, payload.IsCore)
	resp := AdminScanPathResponse{
		Files: files,
		Count: len(files),
		Meta:  s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminGenerateUpdates(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	var payload AdminScanPathPayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	path := strings.TrimSpace(payload.Path)
	platform := strings.TrimSpace(payload.Platform)
	arch := strings.TrimSpace(payload.Architecture)
	if platform == "" || arch == "" {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "platform and architecture are required")
		return
	}
	// Resolve path securely to prevent path traversal
	resolved, err := resolveSafePath(s.updateAssetsDir, path)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	// Check if path exists
	info, err := os.Stat(resolved)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "path does not exist: "+err.Error())
		return
	}
	if !info.IsDir() {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "path is not a directory")
		return
	}
	// Scan directory
	entry := config.DownloadEntry{
		Path:    resolved,
		BaseURL: strings.TrimSpace(payload.BaseURL),
		DownloadMeta: config.DownloadMeta{
			HashAlgorithm:      "sha256",
			SuggestMultiThread: false,
		},
	}
	files := s.filesFromDirectory(resolved, entry, payload.IsCore)
	// Build download entries for config
	downloadEntries := []config.DownloadEntry{{
		Path:    path,
		BaseURL: strings.TrimSpace(payload.BaseURL),
		DownloadMeta: config.DownloadMeta{
			HashAlgorithm:      "sha256",
			SuggestMultiThread: false,
		},
	}}
	// Update the in-memory config
	s.updateConfigMu.Lock()
	if s.updateConfig == nil {
		s.updateConfig = &config.UpdateConfig{Platforms: map[string]config.PlatformUpdates{}}
	}
	if s.updateConfig.Platforms == nil {
		s.updateConfig.Platforms = map[string]config.PlatformUpdates{}
	}
	platformCfg, exists := s.updateConfig.Platforms[platform]
	if !exists {
		platformCfg = config.PlatformUpdates{Architectures: map[string]config.ArchUpdates{}}
	}
	if platformCfg.Architectures == nil {
		platformCfg.Architectures = map[string]config.ArchUpdates{}
	}
	archCfg := platformCfg.Architectures[arch]
	if payload.IsCore {
		archCfg.Latest.Core = downloadEntries
	} else {
		archCfg.Latest.Resource = downloadEntries
	}
	platformCfg.Architectures[arch] = archCfg
	s.updateConfig.Platforms[platform] = platformCfg
	s.updateConfigMu.Unlock()
	// Clear directory cache
	s.dirCacheMu.Lock()
	s.dirCache = map[string]dirCacheEntry{}
	s.dirCacheMu.Unlock()
	// Save to file
	if err := s.saveUpdatesConfig(); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	resp := AdminScanPathResponse{
		Files: files,
		Count: len(files),
		Meta:  s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// AdminUserListResponse is the response for listing users.
type AdminUserListResponse struct {
	Users []AdminUserInfo `json:"users"`
	Total int64           `json:"total"`
	Meta  Meta            `json:"meta"`
}

// AdminUserInfo represents user information for admin views (without password).
type AdminUserInfo struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// AdminUpdateUserRequest is the request body for updating a user.
type AdminUpdateUserRequest struct {
	Password string `json:"password,omitempty"`
	Role     string `json:"role"`
}

// AdminCreateUserRequest is the request body for creating a user.
type AdminCreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "User store not configured")
		return
	}
	ctx := r.Context()
	users, err := s.store.ListUsers(ctx, 200, 0)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	total, _ := s.store.CountUsers(ctx)
	resp := AdminUserListResponse{
		Users: make([]AdminUserInfo, 0, len(users)),
		Total: total,
		Meta:  s.meta(),
	}
	for _, u := range users {
		resp.Users = append(resp.Users, AdminUserInfo{
			ID:        u.ID,
			Username:  u.Username,
			Role:      u.Role,
			CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt: u.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "User store not configured")
		return
	}
	var req AdminCreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	if strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.Password) == "" {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "username and password required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	ctx := r.Context()
	id, err := s.store.CreateUser(ctx, strings.TrimSpace(req.Username), string(hash), req.Role)
	if err != nil {
		s.writeError(w, http.StatusConflict, s.appConfig.Language.Default, "InvalidRequest", "username already exists or error: "+err.Error())
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":   id,
		"meta": s.meta(),
	})
}

func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "User store not configured")
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "invalid user id")
		return
	}
	var req AdminUpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	ctx := r.Context()
	var passwordHash string
	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
			return
		}
		passwordHash = string(hash)
	}
	if err := s.store.UpdateUser(ctx, id, passwordHash, req.Role); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "User updated successfully",
		"meta":    s.meta(),
	})
}

func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "User store not configured")
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "invalid user id")
		return
	}
	ctx := r.Context()
	if err := s.store.DeleteUser(ctx, id); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// App-specific API handlers (for NekoLcServer UI, separate from NekoLcApi)

// AppLoginRequest is the request body for app UI login
type AppLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AppLoginResponse is the response body for app UI login
type AppLoginResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	UserID       int64  `json:"userId"`
	Username     string `json:"username"`
	Role         string `json:"role"`
}

func (s *Server) handleAppAPILogin(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil || !s.authService.Enabled() {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Authentication is disabled")
		return
	}
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Account store not configured")
		return
	}

	var req AppLoginRequest
	if err := s.decode(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}

	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)
	if username == "" || password == "" {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "username and password are required")
		return
	}

	user, err := s.lookupUser(username)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, s.appConfig.Language.Default, "Unauthorized", "invalid credentials")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		s.writeError(w, http.StatusUnauthorized, s.appConfig.Language.Default, "Unauthorized", "invalid credentials")
		return
	}

	subject := fmt.Sprintf("user:%d", user.ID)
	access, refresh, err := s.authService.IssueTokens(subject, user.Role)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}

	resp := AppLoginResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		UserID:       user.ID,
		Username:     user.Username,
		Role:         user.Role,
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAppAPILogout(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil || !s.authService.Enabled() {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Authentication is disabled")
		return
	}

	var payload struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}

	access := strings.TrimSpace(payload.AccessToken)
	refresh := strings.TrimSpace(payload.RefreshToken)
	if access == "" && refresh == "" {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "accessToken or refreshToken required")
		return
	}

	s.authService.Revoke(access, refresh)
	w.WriteHeader(http.StatusNoContent)
}

// AdminStatsResponse returns statistics for the admin dashboard.
type AdminStatsResponse struct {
	Stats store.APIStats `json:"stats"`
	Meta  Meta           `json:"meta"`
}

func (s *Server) handleAdminGetStats(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Store not configured")
		return
	}

	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}

	stats, err := s.store.GetAPIStats(r.Context(), days)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}

	resp := AdminStatsResponse{
		Stats: *stats,
		Meta:  s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}
