package server

import (
	"net/http"
	"strings"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/store"
)

// AdminSMTPResponse returns the current SMTP settings (password redacted).
type AdminSMTPResponse struct {
	SMTP config.SMTPConfig `json:"smtp"`
	Meta Meta              `json:"meta"`
}

// redactedSMTP returns a copy of the SMTP config with the password masked so it
// is never echoed back to the admin UI.
func redactedSMTP(cfg config.SMTPConfig) config.SMTPConfig {
	if cfg.Password != "" {
		cfg.Password = "********"
	}
	return cfg
}

func (s *Server) handleAdminGetSMTP(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	resp := AdminSMTPResponse{SMTP: redactedSMTP(*s.currentSMTPConfig()), Meta: s.meta()}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminUpdateSMTP(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	var payload struct {
		SMTP config.SMTPConfig `json:"smtp"`
	}
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	cfg := payload.SMTP
	// Preserve the existing password when the UI submits the redacted placeholder
	// or leaves it blank, so the secret is not accidentally cleared.
	if cfg.Password == "" || cfg.Password == "********" {
		cfg.Password = s.currentSMTPConfig().Password
	}
	s.setSMTPConfig(&cfg)
	if err := s.saveConfigValue(store.ConfigKeySMTP, cfg); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, AdminMessageResponse{Message: "SMTP settings updated", Meta: s.meta()})
}

// AdminTestEmailPayload requests a test email be sent to the given recipient.
type AdminTestEmailPayload struct {
	To string `json:"to"`
}

func (s *Server) handleAdminTestEmail(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	var payload AdminTestEmailPayload
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	to := strings.ToLower(strings.TrimSpace(payload.To))
	if to == "" || !validEmail(to) {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "a valid recipient email is required")
		return
	}
	m := s.currentMailer()
	if m == nil || !m.Enabled() {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "SMTP is not enabled")
		return
	}
	if err := m.Send(to, "NekoLc test email", "This is a test email from your NekoLc server. SMTP is configured correctly."); err != nil {
		s.writeError(w, http.StatusBadGateway, s.appConfig.Language.Default, "SendFailed", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, AdminMessageResponse{Message: "test email sent", Meta: s.meta()})
}

// AdminAccountResponse returns the current account policy settings.
type AdminAccountResponse struct {
	Account config.AccountConfig `json:"account"`
	Meta    Meta                 `json:"meta"`
}

func (s *Server) handleAdminGetAccount(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	resp := AdminAccountResponse{Account: *s.currentAccountConfig(), Meta: s.meta()}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminUpdateAccount(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	var payload struct {
		Account config.AccountConfig `json:"account"`
	}
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	cfg := payload.Account
	s.setAccountConfig(&cfg)
	if err := s.saveConfigValue(store.ConfigKeyAccount, cfg); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, AdminMessageResponse{Message: "account settings updated", Meta: s.meta()})
}

// AdminSiteResponse returns the current site configuration settings.
type AdminSiteResponse struct {
	Site config.SiteConfig `json:"site"`
	Meta Meta              `json:"meta"`
}

func (s *Server) handleAdminGetSite(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	resp := AdminSiteResponse{Site: *s.currentSiteConfig(), Meta: s.meta()}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminUpdateSite(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	var payload struct {
		Site config.SiteConfig `json:"site"`
	}
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	cfg := payload.Site
	cfg.SiteName = strings.TrimSpace(cfg.SiteName)
	cfg.SEODescription = strings.TrimSpace(cfg.SEODescription)
	cfg.Announcement = strings.TrimSpace(cfg.Announcement)
	s.setSiteConfig(&cfg)
	if err := s.saveConfigValue(store.ConfigKeySite, cfg); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, AdminMessageResponse{Message: "site settings updated", Meta: s.meta()})
}

// AppSiteConfigResponse is the public, non-sensitive view of the site config
// used by the public-facing pages (announcement banner, branding).
type AppSiteConfigResponse struct {
	SiteName       string `json:"siteName"`
	SEODescription string `json:"seoDescription"`
	Announcement   string `json:"announcement"`
	Meta           Meta   `json:"meta"`
}

func (s *Server) handleAppSiteConfig(w http.ResponseWriter, r *http.Request) {
	site := s.currentSiteConfig()
	resp := AppSiteConfigResponse{
		SiteName:       site.SiteName,
		SEODescription: site.SEODescription,
		Announcement:   site.Announcement,
		Meta:           s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// AdminHomeContentResponse returns the admin-authored Markdown home content.
type AdminHomeContentResponse struct {
	Content string `json:"content"`
	Meta    Meta   `json:"meta"`
}

func (s *Server) handleAdminGetHomeContent(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	resp := AdminHomeContentResponse{Content: s.currentHomeContent(), Meta: s.meta()}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminUpdateHomeContent(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireAdmin(w, r); err != nil {
		return
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := s.decode(r, &payload); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	s.setHomeContent(payload.Content)
	if err := s.saveConfigValue(store.ConfigKeyHomeContent, payload.Content); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, AdminMessageResponse{Message: "home content updated", Meta: s.meta()})
}
