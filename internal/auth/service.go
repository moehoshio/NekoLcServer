package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/moehoshio/NekoLcServer/internal/config"
)

// Service is an in-memory token manager that satisfies the API requirements.
type Service struct {
	enabled       bool
	secret        string
	accessTTL     time.Duration
	refreshTTL    time.Duration
	mu            sync.Mutex
	accessTokens  map[string]tokenRecord
	refreshTokens map[string]tokenRecord
}

type tokenRecord struct {
	subject   string
	expiresAt time.Time
}

// NewService bootstraps the authentication service from configuration.
func NewService(cfg *config.AppConfig) *Service {
	accessTTL := time.Duration(cfg.Authentication.TokenExpirationSec) * time.Second
	if accessTTL <= 0 {
		accessTTL = time.Hour
	}
	refreshTTL := time.Duration(cfg.Authentication.RefreshTokenExpirationDays) * 24 * time.Hour
	if refreshTTL <= 0 {
		refreshTTL = 30 * 24 * time.Hour
	}
	return &Service{
		enabled:       cfg.Authentication.Enabled,
		secret:        cfg.Authentication.JWTSecret,
		accessTTL:     accessTTL,
		refreshTTL:    refreshTTL,
		accessTokens:  map[string]tokenRecord{},
		refreshTokens: map[string]tokenRecord{},
	}
}

// Enabled reports whether authentication is active.
func (s *Service) Enabled() bool {
	return s.enabled
}

// IssueTokens creates a fresh access/refresh token pair for the subject.
func (s *Service) IssueTokens(subject string) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	access := uuid.NewString()
	refresh := uuid.NewString()
	now := time.Now().UTC()
	s.accessTokens[access] = tokenRecord{subject: subject, expiresAt: now.Add(s.accessTTL)}
	s.refreshTokens[refresh] = tokenRecord{subject: subject, expiresAt: now.Add(s.refreshTTL)}
	return access, refresh
}

// Refresh issues a new token pair by validating the provided refresh token.
func (s *Service) Refresh(refreshToken string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.refreshTokens[refreshToken]
	if !ok {
		return "", errors.New("invalid refresh token")
	}
	if time.Now().UTC().After(record.expiresAt) {
		delete(s.refreshTokens, refreshToken)
		return "", errors.New("refresh token expired")
	}
	access := uuid.NewString()
	now := time.Now().UTC()
	s.accessTokens[access] = tokenRecord{subject: record.subject, expiresAt: now.Add(s.accessTTL)}
	s.refreshTokens[refreshToken] = tokenRecord{subject: record.subject, expiresAt: now.Add(s.refreshTTL)}
	return access, nil
}

// ValidateAccess ensures the provided access token exists and is not expired.
func (s *Service) ValidateAccess(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.accessTokens[token]
	if !ok {
		return false
	}
	if time.Now().UTC().After(record.expiresAt) {
		delete(s.accessTokens, token)
		return false
	}
	return true
}

// Revoke removes the tokens provided from storage.
func (s *Service) Revoke(accessToken, refreshToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if accessToken != "" {
		delete(s.accessTokens, accessToken)
	}
	if refreshToken != "" {
		delete(s.refreshTokens, refreshToken)
	}
}

// VerifySignature validates the identifier signature for login requests.
func (s *Service) VerifySignature(identifier string, timestamp int64, signature string) bool {
	payload := fmt.Sprintf("%s:%d:%s", identifier, timestamp, s.secret)
	sum := sha256.Sum256([]byte(payload))
	expected := base64.StdEncoding.EncodeToString(sum[:])
	return expected == signature
}
