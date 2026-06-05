package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// registerUser creates a "user" account directly in the store and logs in via the
// app API, returning the access token.
func registerAndLogin(t *testing.T, srv *Server, username, password, email string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := srv.store.CreateUser(context.Background(), username, string(hash), email, "user"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	rec := doRequest(t, srv, http.MethodPost, "/app/api/login", map[string]interface{}{
		"username": username, "password": password,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AppLoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return resp.AccessToken
}

func authEnabled(cfg *config.AppConfig) {
	cfg.Authentication.Enabled = true
	cfg.Authentication.Method = "mysql"
}

func TestRegisterRequiresEmailWhenConfigured(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	srv.setAccountConfig(&config.AccountConfig{AllowRegistration: true, RequireEmail: true})

	// Missing email should be rejected.
	rec := doRequest(t, srv, http.MethodPost, "/app/register", map[string]interface{}{
		"registerRequest": map[string]interface{}{"username": "needsmail", "password": "secret123"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without email, got %d: %s", rec.Code, rec.Body.String())
	}

	// With a valid email it should succeed.
	rec = doRequest(t, srv, http.MethodPost, "/app/register", map[string]interface{}{
		"registerRequest": map[string]interface{}{"username": "needsmail", "password": "secret123", "email": "a@example.com"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 with email, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	if _, err := srv.store.CreateUser(context.Background(), "first", "h", "dup@example.com", "user"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := doRequest(t, srv, http.MethodPost, "/app/register", map[string]interface{}{
		"registerRequest": map[string]interface{}{"username": "second", "password": "secret123", "email": "dup@example.com"},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 duplicate email, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChangePassword(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	token := registerAndLogin(t, srv, "changer", "oldpass123", "")

	// Wrong current password fails.
	rec := doAuthRequest(t, srv, http.MethodPost, "/app/api/change-password", map[string]interface{}{
		"currentPassword": "wrong", "newPassword": "newpass123",
	}, token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 wrong current password, got %d", rec.Code)
	}

	// Correct current password succeeds.
	rec = doAuthRequest(t, srv, http.MethodPost, "/app/api/change-password", map[string]interface{}{
		"currentPassword": "oldpass123", "newPassword": "newpass123",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Old password no longer works; new one does.
	if rec := doRequest(t, srv, http.MethodPost, "/app/api/login", map[string]interface{}{"username": "changer", "password": "oldpass123"}); rec.Code == http.StatusOK {
		t.Fatalf("old password should no longer work")
	}
	if rec := doRequest(t, srv, http.MethodPost, "/app/api/login", map[string]interface{}{"username": "changer", "password": "newpass123"}); rec.Code != http.StatusOK {
		t.Fatalf("new password should work, got %d", rec.Code)
	}
}

func TestForgotPasswordDoesNotLeakExistence(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	// Unknown email still returns 200.
	rec := doRequest(t, srv, http.MethodPost, "/app/api/forgot-password", map[string]interface{}{"email": "nobody@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for unknown email, got %d", rec.Code)
	}
}

func TestResetPasswordWithToken(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	hash, _ := bcrypt.GenerateFromPassword([]byte("oldpass123"), bcrypt.DefaultCost)
	id, err := srv.store.CreateUser(context.Background(), "resetme", string(hash), "reset@example.com", "user")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Save a reset token manually (mirrors what issueAndSendToken does).
	raw := "raw-reset-token"
	if err := srv.store.SaveAccountToken(context.Background(), id, hashAccountToken(raw), store.AccountTokenPurposeReset, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("save token: %v", err)
	}
	rec := doRequest(t, srv, http.MethodPost, "/app/api/reset-password", map[string]interface{}{
		"token": raw, "newPassword": "brandnew123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Token is single-use: a second attempt fails.
	rec = doRequest(t, srv, http.MethodPost, "/app/api/reset-password", map[string]interface{}{
		"token": raw, "newPassword": "another123",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on reused token, got %d", rec.Code)
	}
	// New password works.
	if rec := doRequest(t, srv, http.MethodPost, "/app/api/login", map[string]interface{}{"username": "resetme", "password": "brandnew123"}); rec.Code != http.StatusOK {
		t.Fatalf("new password should work, got %d", rec.Code)
	}
}

func TestVerifyEmailWithToken(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	id, err := srv.store.CreateUser(context.Background(), "verifyme", "h", "verify@example.com", "user")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	raw := "raw-verify-token"
	if err := srv.store.SaveAccountToken(context.Background(), id, hashAccountToken(raw), store.AccountTokenPurposeVerify, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("save token: %v", err)
	}
	rec := doRequest(t, srv, http.MethodGet, "/app/api/verify-email?token="+raw, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	user, err := srv.store.GetUserByID(context.Background(), id)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if !user.EmailVerified {
		t.Fatalf("expected email verified")
	}
}

func TestMeReturnsAccountInfo(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	token := registerAndLogin(t, srv, "meuser", "secret123", "me@example.com")
	rec := doAuthRequest(t, srv, http.MethodGet, "/app/api/me", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp AccountInfoResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Username != "meuser" || resp.Email != "me@example.com" {
		t.Fatalf("unexpected account info: %+v", resp)
	}
}

func TestAdminAccountAndHomeContentRoundTrip(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	token := loginAsAdmin(t, srv)

	// Update account policy.
	rec := doAuthRequest(t, srv, http.MethodPut, "/v0/api/admin/account", map[string]interface{}{
		"account": map[string]interface{}{"requireEmail": true, "verifyEmail": true},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("update account expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	if !srv.currentAccountConfig().RequireEmail {
		t.Fatalf("requireEmail not applied")
	}

	// Update home content.
	rec = doAuthRequest(t, srv, http.MethodPut, "/v0/api/admin/homeContent", map[string]interface{}{
		"content": "# Hello\n\nWorld",
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("update home content expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	// Public home-content endpoint renders markdown to HTML.
	rec = doRequest(t, srv, http.MethodGet, "/app/api/home-content", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("home-content expected 200 got %d", rec.Code)
	}
	var hc HomeContentResponse
	if err := json.NewDecoder(rec.Body).Decode(&hc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if hc.ContentHTML == "" || hc.ContentMarkdown == "" {
		t.Fatalf("expected rendered home content, got %+v", hc)
	}
}

func TestAdminSMTPRedactsPassword(t *testing.T) {
	srv := newTestServer(t, authEnabled)
	token := loginAsAdmin(t, srv)
	rec := doAuthRequest(t, srv, http.MethodPut, "/v0/api/admin/smtp", map[string]interface{}{
		"smtp": map[string]interface{}{"enabled": true, "host": "smtp.example.com", "from": "a@example.com", "password": "supersecret"},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("update smtp expected 200 got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doAuthRequest(t, srv, http.MethodGet, "/v0/api/admin/smtp", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("get smtp expected 200 got %d", rec.Code)
	}
	var resp AdminSMTPResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SMTP.Password == "supersecret" {
		t.Fatalf("password should be redacted")
	}
	// The stored password is preserved internally.
	if srv.currentSMTPConfig().Password != "supersecret" {
		t.Fatalf("stored password should be preserved")
	}
}
