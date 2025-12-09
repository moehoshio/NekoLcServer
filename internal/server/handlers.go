package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/moehoshio/NekoLcServer/internal/config"
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
	case "account":
		s.writeError(w, http.StatusNotImplemented, lang, "NotImplemented", "Account authentication is not available")
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
	access, refresh, err := s.authService.IssueTokens(req.Identifier)
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
		coreCurrent := strings.TrimSpace(client.App.CoreVersion)
		isMandatory = !strings.EqualFold(latest.CoreVersion, coreCurrent)
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

func (s *Server) resolveUpdateFiles(client *ClientInfo) ([]UpdateFileResponse, bool, config.FullPackage) {
	if s.updateConfig == nil {
		return nil, false, config.FullPackage{}
	}
	archCfg, ok := s.archUpdates(client.System)
	if !ok {
		return nil, false, config.FullPackage{}
	}

	latest := archCfg.Latest
	coreLatest := strings.TrimSpace(latest.CoreVersion)
	resLatest := strings.TrimSpace(latest.ResourceVersion)
	coreCurrent := ""
	resCurrent := ""
	if client.App != nil {
		coreCurrent = strings.TrimSpace(client.App.CoreVersion)
		resCurrent = strings.TrimSpace(client.App.ResourceVersion)
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
			if diff.CoreVersionPath != "" {
				if diffFiles := s.diffFilesFromPath(diff.CoreVersionPath, true); len(diffFiles) > 0 {
					files = append(files, diffFiles...)
					coreSatisfied = true
				} else {
					files = append(files, s.fileFromPath(diff.CoreVersionPath, true))
					coreSatisfied = true
				}
			} else if diff.CoreDownloadURL != "" {
				files = append(files, s.fileFromPath(diff.CoreDownloadURL, true))
				coreSatisfied = true
			}
		}
		if !coreSatisfied && latest.CoreDownloadURL != "" {
			files = append(files, s.fileFromPath(latest.CoreDownloadURL, true))
		}
	}

	if needResource {
		resSatisfied := false
		if diff := findDiff(archCfg.Diffs, resCurrent, false); diff != nil {
			if diff.ResourceVersionPath != "" {
				if diffFiles := s.diffFilesFromPath(diff.ResourceVersionPath, false); len(diffFiles) > 0 {
					files = append(files, diffFiles...)
					resSatisfied = true
				} else {
					files = append(files, s.fileFromPath(diff.ResourceVersionPath, false))
					resSatisfied = true
				}
			} else if diff.ResourceDownloadURL != "" {
				files = append(files, s.fileFromPath(diff.ResourceDownloadURL, false))
				resSatisfied = true
			}
		}
		if !resSatisfied && latest.ResourceDownloadURL != "" {
			files = append(files, s.fileFromPath(latest.ResourceDownloadURL, false))
		}
	}

	if len(files) == 0 && (coreCurrent == "" || resCurrent == "") {
		if latest.CoreDownloadURL != "" {
			files = append(files, s.fileFromPath(latest.CoreDownloadURL, true))
		}
		if latest.ResourceDownloadURL != "" {
			files = append(files, s.fileFromPath(latest.ResourceDownloadURL, false))
		}
	}

	return files, len(files) > 0, latest
}

func findDiff(diffs []config.DiffFile, clientVersion string, isCore bool) *config.DiffFile {
	for i := range diffs {
		diff := &diffs[i]
		if isCore && diff.FromCoreVersion != "" && strings.EqualFold(diff.FromCoreVersion, clientVersion) {
			return diff
		}
		if !isCore && diff.FromResourceVersion != "" && strings.EqualFold(diff.FromResourceVersion, clientVersion) {
			return diff
		}
	}
	return nil
}

func (s *Server) archUpdates(system *SystemInfo) (config.ArchUpdates, bool) {
	if system == nil || s.updateConfig == nil {
		return config.ArchUpdates{}, false
	}
	platform, ok := findPlatform(s.updateConfig.Platforms, system.OS)
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

func (s *Server) fileFromPath(path string, isCore bool) UpdateFileResponse {
	abs := strings.HasPrefix(strings.ToLower(path), "http://") || strings.HasPrefix(strings.ToLower(path), "https://")
	fileName := filepath.Base(path)
	return UpdateFileResponse{
		URL:      path,
		FileName: fileName,
		Checksum: "",
		DownloadMeta: DownloadMeta{
			HashAlgorithm:      "sha256",
			SuggestMultiThread: false,
			IsCoreFile:         isCore,
			IsAbsoluteURL:      abs,
		},
	}
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
	w.WriteHeader(http.StatusNoContent)
}
