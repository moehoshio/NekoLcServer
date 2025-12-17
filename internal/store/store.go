package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// User represents an account in persistent storage.
type User struct {
	ID        int64
	Username  string
	Password  string // bcrypt hash
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RefreshToken represents a persisted refresh token.
type RefreshToken struct {
	ID        int64
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	RevokedAt sql.NullTime
	CreatedAt time.Time
}

// FeedbackLog represents a stored feedback entry.
type FeedbackLog struct {
	ID         int64
	UserID     sql.NullInt64
	DeviceID   string
	Lang       string
	ClientInfo []byte
	Content    string
	ReceivedAt time.Time
	Timestamp  int64
}

// Store defines the persistence operations we need.
type Store interface {
	Ping(ctx context.Context) error

	// Users
	CreateUser(ctx context.Context, username, passwordHash, role string) (int64, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	HasUsers(ctx context.Context) (bool, error)

	// Refresh tokens
	SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	RefreshTokenValid(ctx context.Context, tokenHash string, now time.Time) (bool, error)

	// Feedback logs
	SaveFeedback(ctx context.Context, entry FeedbackLog) error
	ListFeedback(ctx context.Context, limit, offset int) ([]FeedbackLog, error)
}

var ErrNotFound = errors.New("not found")

func ensureRole(role string) string {
	if role == "admin" {
		return role
	}
	return "user"
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 5*time.Second)
}
