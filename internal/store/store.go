package store

import (
	"context"
	"database/sql"
	"encoding/json"
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
	ID              int64
	UserID          sql.NullInt64
	DeviceID        string
	Lang            string
	ClientInfo      []byte
	Content         string
	ReceivedAt      time.Time
	Timestamp       int64
	CoreVersion     string
	ResourceVersion string
	BuildID         string
	Platform        string
	Arch            string
	Region          string
}

// FeedbackFilter defines filter criteria for listing feedback logs.
type FeedbackFilter struct {
	CoreVersion     string
	ResourceVersion string
	BuildID         string
	Platform        string
	Arch            string
	Region          string
	Lang            string
	StartTime       *time.Time
	EndTime         *time.Time
}

// ConfigEntry represents a configuration stored in database.
type ConfigEntry struct {
	ID        int64
	Key       string
	Value     json.RawMessage
	UpdatedAt time.Time
}

// APIEvent represents a tracked API call event.
type APIEvent struct {
	ID         int64
	Endpoint   string
	Method     string
	StatusCode int
	DeviceID   string
	Platform   string
	Arch       string
	CreatedAt  time.Time
}

// APIStats holds aggregated statistics for API usage.
type APIStats struct {
	TotalRequests   int64            `json:"totalRequests"`
	TodayRequests   int64            `json:"todayRequests"`
	EndpointCounts  map[string]int64 `json:"endpointCounts"`
	PlatformCounts  map[string]int64 `json:"platformCounts"`
	DailyStats      []DailyStat      `json:"dailyStats"`
	TotalUsers      int64            `json:"totalUsers"`
	TotalFeedback   int64            `json:"totalFeedback"`
	RecentEndpoints []EndpointStat   `json:"recentEndpoints"`
}

// DailyStat represents daily request counts.
type DailyStat struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// EndpointStat represents endpoint-specific statistics.
type EndpointStat struct {
	Endpoint string `json:"endpoint"`
	Count    int64  `json:"count"`
}

// FeedbackFilterOptions contains the available values for feedback filters.
type FeedbackFilterOptions struct {
	CoreVersions     []string `json:"coreVersions"`
	ResourceVersions []string `json:"resourceVersions"`
	BuildIDs         []string `json:"buildIds"`
	Platforms        []string `json:"platforms"`
	Arches           []string `json:"arches"`
	Regions          []string `json:"regions"`
	Langs            []string `json:"langs"`
}

// Store defines the persistence operations we need.
type Store interface {
	Ping(ctx context.Context) error

	// Users
	CreateUser(ctx context.Context, username, passwordHash, role string) (int64, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	GetUserByID(ctx context.Context, id int64) (*User, error)
	ListUsers(ctx context.Context, limit, offset int) ([]User, error)
	UpdateUser(ctx context.Context, id int64, passwordHash, role string) error
	DeleteUser(ctx context.Context, id int64) error
	CountUsers(ctx context.Context) (int64, error)
	HasUsers(ctx context.Context) (bool, error)

	// Refresh tokens
	SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	RefreshTokenValid(ctx context.Context, tokenHash string, now time.Time) (bool, error)

	// Feedback logs
	SaveFeedback(ctx context.Context, entry FeedbackLog) error
	ListFeedback(ctx context.Context, limit, offset int) ([]FeedbackLog, error)
	ListFeedbackFiltered(ctx context.Context, filter FeedbackFilter, limit, offset int) ([]FeedbackLog, error)
	GetFeedbackFilterOptions(ctx context.Context) (*FeedbackFilterOptions, error)

	// Configuration storage
	GetConfig(ctx context.Context, key string) (json.RawMessage, error)
	SetConfig(ctx context.Context, key string, value json.RawMessage) error
	ListConfigs(ctx context.Context) ([]ConfigEntry, error)

	// API event tracking
	SaveAPIEvent(ctx context.Context, event APIEvent) error
	GetAPIStats(ctx context.Context, days int) (*APIStats, error)
	CountFeedback(ctx context.Context) (int64, error)
}

var ErrNotFound = errors.New("not found")

// Configuration keys
const (
	ConfigKeyLauncher    = "launcher"
	ConfigKeyMaintenance = "maintenance"
	ConfigKeyNews        = "news"
	ConfigKeyUpdates     = "updates"
	ConfigKeyLanguages   = "languages"
	ConfigKeyApp         = "app"
)

func ensureRole(role string) string {
	if role == "admin" {
		return role
	}
	return "user"
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 5*time.Second)
}
