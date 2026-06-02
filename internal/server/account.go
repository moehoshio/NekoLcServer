package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/moehoshio/NekoLcServer/internal/auth"
	"github.com/moehoshio/NekoLcServer/internal/mailer"
	"github.com/moehoshio/NekoLcServer/internal/markdown"
	"github.com/moehoshio/NekoLcServer/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// emailRegex is a pragmatic email format check (not a full RFC 5322 parser).
var emailRegex = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

const (
	resetTokenTTL  = 1 * time.Hour
	verifyTokenTTL = 24 * time.Hour
)

func validEmail(email string) bool {
	return emailRegex.MatchString(email)
}

// hashAccountToken hashes an opaque token for at-rest storage.
func hashAccountToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// userIDFromClaims parses the numeric user id from a "user:<id>" subject.
func userIDFromClaims(c *authClaims) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("no claims")
	}
	parts := strings.SplitN(c.Subject, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid subject")
	}
	return strconv.ParseInt(parts[1], 10, 64)
}

// requireUser authenticates the request and loads the corresponding user.
func (s *Server) requireUser(r *http.Request) (*store.User, error) {
	claims, err := s.authenticate(r)
	if err != nil {
		return nil, err
	}
	id, err := userIDFromClaims(claims)
	if err != nil {
		return nil, err
	}
	return s.store.GetUserByID(r.Context(), id)
}

// AccountInfoResponse describes the authenticated user's account details.
type AccountInfoResponse struct {
	UserID        int64  `json:"userId"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	Role          string `json:"role"`
	Meta          Meta   `json:"meta"`
}

func (s *Server) handleAppMe(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil || !s.authService.Enabled() || s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Authentication is disabled")
		return
	}
	user, err := s.requireUser(r)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, s.appConfig.Language.Default, "Unauthorized", "invalid credentials")
		return
	}
	resp := AccountInfoResponse{
		UserID:        user.ID,
		Username:      user.Username,
		Email:         user.Email,
		EmailVerified: user.EmailVerified,
		Role:          user.Role,
		Meta:          s.meta(),
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// ChangePasswordRequest is the body for the authenticated change-password endpoint.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) handleAppChangePassword(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil || !s.authService.Enabled() || s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Authentication is disabled")
		return
	}
	user, err := s.requireUser(r)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, s.appConfig.Language.Default, "Unauthorized", "invalid credentials")
		return
	}
	var req ChangePasswordRequest
	if err := s.decode(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.CurrentPassword)) != nil {
		s.writeError(w, http.StatusUnauthorized, s.appConfig.Language.Default, "Unauthorized", "current password is incorrect")
		return
	}
	newPass := strings.TrimSpace(req.NewPassword)
	if len(newPass) < 6 {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "password must be at least 6 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	if err := s.store.UpdateUserPassword(r.Context(), user.ID, string(hash)); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, AdminMessageResponse{Message: "password updated", Meta: s.meta()})
}

// ForgotPasswordRequest requests a password reset email.
type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

func (s *Server) handleAppForgotPassword(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Account store not configured")
		return
	}
	var req ForgotPasswordRequest
	if err := s.decode(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	// Always respond with success to avoid leaking whether an email exists.
	ok := AdminMessageResponse{Message: "if the email exists, a reset link has been sent", Meta: s.meta()}
	if email == "" || !validEmail(email) {
		s.writeJSON(w, http.StatusOK, ok)
		return
	}
	user, err := s.store.GetUserByEmail(r.Context(), email)
	if err != nil {
		s.writeJSON(w, http.StatusOK, ok)
		return
	}
	if err := s.issueAndSendToken(r.Context(), user, store.AccountTokenPurposeReset); err != nil {
		// Do not leak internal errors; log and respond success.
		fmt.Printf("forgot-password: failed to send reset email: %v\n", err)
	}
	s.writeJSON(w, http.StatusOK, ok)
}

// ResetPasswordRequest resets a password using a single-use token.
type ResetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

func (s *Server) handleAppResetPassword(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Account store not configured")
		return
	}
	var req ResetPasswordRequest
	if err := s.decode(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	token := strings.TrimSpace(req.Token)
	newPass := strings.TrimSpace(req.NewPassword)
	if token == "" {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "token is required")
		return
	}
	if len(newPass) < 6 {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "password must be at least 6 characters")
		return
	}
	userID, err := s.store.ConsumeAccountToken(r.Context(), hashAccountToken(token), store.AccountTokenPurposeReset, time.Now().UTC())
	if err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "invalid or expired token")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	if err := s.store.UpdateUserPassword(r.Context(), userID, string(hash)); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, AdminMessageResponse{Message: "password has been reset", Meta: s.meta()})
}

func (s *Server) handleAppSendVerification(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil || !s.authService.Enabled() || s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Authentication is disabled")
		return
	}
	user, err := s.requireUser(r)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, s.appConfig.Language.Default, "Unauthorized", "invalid credentials")
		return
	}
	if user.Email == "" {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "no email address on file")
		return
	}
	if user.EmailVerified {
		s.writeJSON(w, http.StatusOK, AdminMessageResponse{Message: "email already verified", Meta: s.meta()})
		return
	}
	if err := s.issueAndSendToken(r.Context(), user, store.AccountTokenPurposeVerify); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", "failed to send verification email")
		return
	}
	s.writeJSON(w, http.StatusOK, AdminMessageResponse{Message: "verification email sent", Meta: s.meta()})
}

// VerifyEmailRequest verifies an email address using a single-use token.
type VerifyEmailRequest struct {
	Token string `json:"token"`
}

func (s *Server) handleAppVerifyEmail(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Account store not configured")
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		var req VerifyEmailRequest
		if err := s.decode(r, &req); err == nil {
			token = strings.TrimSpace(req.Token)
		}
	}
	if token == "" {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "token is required")
		return
	}
	userID, err := s.store.ConsumeAccountToken(r.Context(), hashAccountToken(token), store.AccountTokenPurposeVerify, time.Now().UTC())
	if err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "invalid or expired token")
		return
	}
	if err := s.store.SetEmailVerified(r.Context(), userID, true); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, AdminMessageResponse{Message: "email verified", Meta: s.meta()})
}

// issueAndSendToken generates a single-use token of the given purpose, persists
// its hash and emails the corresponding link to the user.
func (s *Server) issueAndSendToken(ctx context.Context, user *store.User, purpose string) error {
	raw, err := auth.GenerateOpaqueToken()
	if err != nil {
		return err
	}
	ttl := resetTokenTTL
	if purpose == store.AccountTokenPurposeVerify {
		ttl = verifyTokenTTL
	}
	if err := s.store.SaveAccountToken(ctx, user.ID, hashAccountToken(raw), purpose, time.Now().UTC().Add(ttl)); err != nil {
		return err
	}
	m := s.currentMailer()
	if m == nil || !m.Enabled() {
		// Email delivery is not configured; the token has still been stored.
		return mailer.ErrDisabled
	}
	smtp := s.currentSMTPConfig()
	base := strings.TrimRight(smtp.BaseURL, "/")
	var subject, body string
	switch purpose {
	case store.AccountTokenPurposeReset:
		link := fmt.Sprintf("%s%s/app/reset-password?token=%s", base, s.basePath, raw)
		subject = "Reset your NekoLc password"
		body = fmt.Sprintf("A password reset was requested for your account.\n\nReset your password using this link (valid for 1 hour):\n%s\n\nIf you did not request this, you can ignore this email.", link)
	case store.AccountTokenPurposeVerify:
		link := fmt.Sprintf("%s%s/app/verify-email?token=%s", base, s.basePath, raw)
		subject = "Verify your NekoLc email"
		body = fmt.Sprintf("Please verify your email address by visiting this link (valid for 24 hours):\n%s", link)
	}
	return m.Send(user.Email, subject, body)
}

// ChangeEmailRequest updates the authenticated user's email address.
type ChangeEmailRequest struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"currentPassword"`
}

func (s *Server) handleAppChangeEmail(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil || !s.authService.Enabled() || s.store == nil {
		s.writeError(w, http.StatusNotImplemented, s.appConfig.Language.Default, "NotImplemented", "Authentication is disabled")
		return
	}
	user, err := s.requireUser(r)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, s.appConfig.Language.Default, "Unauthorized", "invalid credentials")
		return
	}
	var req ChangeEmailRequest
	if err := s.decode(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", err.Error())
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.CurrentPassword)) != nil {
		s.writeError(w, http.StatusUnauthorized, s.appConfig.Language.Default, "Unauthorized", "current password is incorrect")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !validEmail(email) {
		s.writeError(w, http.StatusBadRequest, s.appConfig.Language.Default, "InvalidRequest", "a valid email is required")
		return
	}
	// Reject if the email belongs to another account.
	if existing, err := s.store.GetUserByEmail(r.Context(), email); err == nil && existing.ID != user.ID {
		s.writeError(w, http.StatusConflict, s.appConfig.Language.Default, "Conflict", "email already in use")
		return
	} else if err != nil && err != store.ErrNotFound {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	if err := s.store.UpdateUserEmail(r.Context(), user.ID, email, false); err != nil {
		s.writeError(w, http.StatusInternalServerError, s.appConfig.Language.Default, "InternalError", err.Error())
		return
	}
	// Send a verification email when the policy requires verification.
	if accountCfg := s.currentAccountConfig(); accountCfg != nil && accountCfg.VerifyEmail {
		user.Email = email
		user.EmailVerified = false
		if serr := s.issueAndSendToken(r.Context(), user, store.AccountTokenPurposeVerify); serr != nil && serr != mailer.ErrDisabled {
			fmt.Printf("change-email: failed to send verification email: %v\n", serr)
		}
	}
	s.writeJSON(w, http.StatusOK, AdminMessageResponse{Message: "email updated", Meta: s.meta()})
}

// HomeContentResponse surfaces the admin-authored home content (rendered to safe
// HTML), the current maintenance notice and the latest news items for the user
// homepage/dashboard.
type HomeContentResponse struct {
	ContentHTML     string             `json:"contentHtml"`
	ContentMarkdown string             `json:"contentMarkdown"`
	Maintenance     *MaintenanceNotice `json:"maintenance,omitempty"`
	News            []HomeNewsItem     `json:"news"`
	Meta            Meta               `json:"meta"`
}

// MaintenanceNotice is a compact maintenance summary for the homepage.
type MaintenanceNotice struct {
	Active  bool   `json:"active"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// HomeNewsItem is a compact news entry for the homepage.
type HomeNewsItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Link        string `json:"link"`
	PublishTime string `json:"publishTime"`
}

func (s *Server) handleAppHomeContent(w http.ResponseWriter, r *http.Request) {
	resp := HomeContentResponse{
		ContentMarkdown: s.currentHomeContent(),
		ContentHTML:     markdown.Render(s.currentHomeContent()),
		News:            []HomeNewsItem{},
		Meta:            s.meta(),
	}
	if mc := s.currentMaintenanceConfig(); mc != nil && mc.MaintenanceActive {
		resp.Maintenance = &MaintenanceNotice{
			Active:  true,
			Status:  mc.MaintenanceInfo.Status,
			Message: mc.MaintenanceInfo.Message,
		}
	}
	for _, item := range s.currentNewsItems() {
		resp.News = append(resp.News, HomeNewsItem{
			ID:          item.ID,
			Title:       item.Title,
			Summary:     item.Summary,
			Link:        item.Link,
			PublishTime: item.PublishTime,
		})
		if len(resp.News) >= 10 {
			break
		}
	}
	s.writeJSON(w, http.StatusOK, resp)
}

