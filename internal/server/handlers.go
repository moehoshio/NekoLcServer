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
	"strings"
	"time"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/store"
	"golang.org/x/crypto/bcrypt"
)

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
	switch s.authMethod() {
	case "mysql":
		username := strings.TrimSpace(req.Username)
		password := strings.TrimSpace(req.Password)
		if username == "" || password == "" {
			s.writeError(w, http.StatusBadRequest, lang, "InvalidRequest", "username and password are required")
			return
		}
		if s.store == nil {
			s.writeError(w, http.StatusInternalServerError, lang, "InternalError", "account store not configured")
			return
		}
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
		s.writeJSON(w, http.StatusOK, body)
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
	body := LauncherConfigResponseBody{
		LauncherConfigResponse: *s.launcherConfig,
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
	entries, err := s.store.ListFeedback(r.Context(), limit, offset)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	items := make([]FeedbackLogItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, FeedbackLogItem{
			ID:         e.ID,
			UserID:     nullUserID(e.UserID),
			DeviceID:   e.DeviceID,
			Lang:       e.Lang,
			ClientInfo: jsonRaw(e.ClientInfo),
			Content:    e.Content,
			ReceivedAt: e.ReceivedAt.Format(time.RFC3339),
			Timestamp:  e.Timestamp,
		})
	}
	resp := FeedbackLogsResponseBody{
		FeedbackLogs: items,
		Count:        len(items),
		Meta:         s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// Admin handlers for configuration management

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
