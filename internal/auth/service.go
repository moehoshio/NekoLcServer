package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/store"
)

// Service is an in-memory token manager that satisfies the API requirements.
type Service struct {
	enabled    bool
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	mu         sync.Mutex
	revoked    map[string]time.Time
	store      store.Store
}

type claims struct {
	TokenType string `json:"tokenType"`
	Role      string `json:"role,omitempty"`
	jwt.RegisteredClaims
}

// NewService bootstraps the authentication service from configuration and an optional persistence store.
func NewService(cfg *config.AppConfig, st store.Store) *Service {
	accessTTL := time.Duration(cfg.Authentication.TokenExpirationSec) * time.Second
	if accessTTL <= 0 {
		accessTTL = time.Hour
	}
	refreshTTL := time.Duration(cfg.Authentication.RefreshTokenExpirationDays) * 24 * time.Hour
	if refreshTTL <= 0 {
		refreshTTL = 30 * 24 * time.Hour
	}
	secret := cfg.Authentication.JWTSecret
	if cfg.Authentication.JWT.JWTSecret != "" {
		secret = cfg.Authentication.JWT.JWTSecret
	}
	return &Service{
		enabled:    cfg.Authentication.Enabled,
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		revoked:    map[string]time.Time{},
		store:      st,
	}
}

// Enabled reports whether authentication is active.
func (s *Service) Enabled() bool {
	return s.enabled
}

// IssueTokens creates a fresh access/refresh token pair for the subject and role.
func (s *Service) IssueTokens(subject, role string) (string, string, error) {
	access, err := s.signToken(subject, role, tokenTypeAccess, s.accessTTL)
	if err != nil {
		return "", "", err
	}
	refresh, err := s.signToken(subject, role, tokenTypeRefresh, s.refreshTTL)
	if err != nil {
		return "", "", err
	}
	if s.store != nil {
		if err := s.store.SaveRefreshToken(defaultCtx(), subjectToID(subject), hashToken(refresh), time.Now().UTC().Add(s.refreshTTL)); err != nil {
			return "", "", err
		}
	}
	return access, refresh, nil
}

// Refresh issues a new token pair by validating the provided refresh token.
func (s *Service) Refresh(refreshToken string) (string, error) {
	claims, err := s.parseToken(refreshToken, tokenTypeRefresh)
	if err != nil {
		return "", err
	}
	if s.store != nil {
		valid, err := s.store.RefreshTokenValid(defaultCtx(), hashToken(refreshToken), time.Now().UTC())
		if err != nil || !valid {
			return "", errors.New("refresh token revoked or expired")
		}
	} else if s.isRevoked(refreshToken) {
		return "", errors.New("refresh token revoked")
	}
	return s.signToken(claims.Subject, claims.Role, tokenTypeAccess, s.accessTTL)
}

// ValidateAccess ensures the provided access token exists and is not expired.
func (s *Service) ValidateAccess(token string) bool {
	if token == "" {
		return false
	}
	if s.isRevoked(token) {
		return false
	}
	_, err := s.parseToken(token, tokenTypeAccess)
	return err == nil
}

// ParseAccess returns the parsed claims for an access token.
func (s *Service) ParseAccess(token string) (*claims, error) {
	parsed, err := s.parseToken(token, tokenTypeAccess)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

// Revoke removes the tokens provided from storage.
func (s *Service) Revoke(accessToken, refreshToken string) {
	s.revokeToken(accessToken)
	if s.store != nil && refreshToken != "" {
		_ = s.store.RevokeRefreshToken(defaultCtx(), hashToken(refreshToken))
	}
	s.revokeToken(refreshToken)
}

// VerifySignature validates the identifier signature for login requests.
func (s *Service) VerifySignature(identifier string, timestamp int64, signature string) bool {
	payload := fmt.Sprintf("%s:%d:%s", identifier, timestamp, string(s.secret))
	sum := sha256.Sum256([]byte(payload))
	expected := base64.StdEncoding.EncodeToString(sum[:])
	return expected == signature
}

const (
	tokenTypeAccess  = "access"
	tokenTypeRefresh = "refresh"
)

func (s *Service) signToken(subject, role, tokenType string, ttl time.Duration) (string, error) {
	if len(s.secret) == 0 {
		return "", errors.New("jwt secret is not configured")
	}
	now := time.Now().UTC()
	claims := claims{
		TokenType: tokenType,
		Role:      role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *Service) parseToken(raw string, expectedType string) (*claims, error) {
	if raw == "" {
		return nil, errors.New("token is required")
	}
	parsed, err := jwt.ParseWithClaims(raw, &claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claim, ok := parsed.Claims.(*claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token claims")
	}
	if expectedType != "" && claim.TokenType != expectedType {
		return nil, fmt.Errorf("unexpected token type: %s", claim.TokenType)
	}
	return claim, nil
}

func (s *Service) revokeToken(token string) {
	if token == "" {
		return
	}
	claims, err := s.parseToken(token, "")
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupRevokedLocked()
	if claims.ExpiresAt != nil {
		s.revoked[token] = claims.ExpiresAt.Time
	}
}

func (s *Service) isRevoked(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isRevokedLocked(token)
}

func (s *Service) isRevokedLocked(token string) bool {
	s.cleanupRevokedLocked()
	expires, ok := s.revoked[token]
	if !ok {
		return false
	}
	if time.Now().UTC().After(expires) {
		delete(s.revoked, token)
		return false
	}
	return true
}

func (s *Service) cleanupRevokedLocked() {
	now := time.Now().UTC()
	for token, exp := range s.revoked {
		if now.After(exp) {
			delete(s.revoked, token)
		}
	}
}

func defaultCtx() context.Context {
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	return ctx
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// GenerateOpaqueToken produces a secure random token string.
func GenerateOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// subjectToID parses a subject of form "user:<id>" to int64, otherwise 0.
func subjectToID(sub string) int64 {
	var id int64
	_, err := fmt.Sscanf(sub, "user:%d", &id)
	if err != nil {
		return 0
	}
	return id
}
