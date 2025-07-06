package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Storage interface defines the contract for all storage implementations
type Storage interface {
	StoreFeedbackLog(log *FeedbackLog) error
	StoreAuthToken(token *AuthToken) error
	GetAuthToken(tokenHash string) (*AuthToken, error)
	RevokeAuthToken(tokenHash string) error
	RevokeAllUserTokens(userID string) error
	Close() error
}

type Database struct {
	db *sql.DB
}

type FeedbackLog struct {
	ID              int       `json:"id"`
	OS              string    `json:"os"`
	Arch            string    `json:"arch"`
	CoreVersion     string    `json:"coreVersion"`
	ResourceVersion string    `json:"resourceVersion"`
	Timestamp       int64     `json:"timestamp"`
	Content         string    `json:"content"`
	CreatedAt       time.Time `json:"createdAt"`
}

type AuthToken struct {
	ID           int       `json:"id"`
	TokenHash    string    `json:"tokenHash"`
	TokenType    string    `json:"tokenType"` // "access" or "refresh"
	UserID       string    `json:"userId"`
	ExpiresAt    time.Time `json:"expiresAt"`
	CreatedAt    time.Time `json:"createdAt"`
	IsRevoked    bool      `json:"isRevoked"`
}

func NewDatabase(dbPath string) (*Database, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	storage := &Database{db: db}
	if err := storage.createTables(); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return storage, nil
}

func (d *Database) createTables() error {
	// Create feedback_logs table
	feedbackTableSQL := `
	CREATE TABLE IF NOT EXISTS feedback_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		os TEXT NOT NULL,
		arch TEXT NOT NULL,
		core_version TEXT NOT NULL,
		resource_version TEXT NOT NULL,
		timestamp INTEGER NOT NULL,
		content TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	// Create auth_tokens table
	authTokensTableSQL := `
	CREATE TABLE IF NOT EXISTS auth_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_hash TEXT UNIQUE NOT NULL,
		token_type TEXT NOT NULL,
		user_id TEXT NOT NULL,
		expires_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		is_revoked BOOLEAN DEFAULT FALSE
	);`

	if _, err := d.db.Exec(feedbackTableSQL); err != nil {
		return fmt.Errorf("failed to create feedback_logs table: %w", err)
	}

	if _, err := d.db.Exec(authTokensTableSQL); err != nil {
		return fmt.Errorf("failed to create auth_tokens table: %w", err)
	}

	return nil
}

func (d *Database) StoreFeedbackLog(log *FeedbackLog) error {
	query := `
		INSERT INTO feedback_logs (os, arch, core_version, resource_version, timestamp, content)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err := d.db.Exec(query, log.OS, log.Arch, log.CoreVersion, log.ResourceVersion, log.Timestamp, log.Content)
	if err != nil {
		return fmt.Errorf("failed to store feedback log: %w", err)
	}
	return nil
}

func (d *Database) StoreAuthToken(token *AuthToken) error {
	query := `
		INSERT INTO auth_tokens (token_hash, token_type, user_id, expires_at)
		VALUES (?, ?, ?, ?)
	`
	_, err := d.db.Exec(query, token.TokenHash, token.TokenType, token.UserID, token.ExpiresAt)
	if err != nil {
		return fmt.Errorf("failed to store auth token: %w", err)
	}
	return nil
}

func (d *Database) GetAuthToken(tokenHash string) (*AuthToken, error) {
	query := `
		SELECT id, token_hash, token_type, user_id, expires_at, created_at, is_revoked
		FROM auth_tokens
		WHERE token_hash = ? AND is_revoked = FALSE
	`
	row := d.db.QueryRow(query, tokenHash)
	
	var token AuthToken
	err := row.Scan(&token.ID, &token.TokenHash, &token.TokenType, &token.UserID, &token.ExpiresAt, &token.CreatedAt, &token.IsRevoked)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Token not found
		}
		return nil, fmt.Errorf("failed to get auth token: %w", err)
	}
	
	return &token, nil
}

func (d *Database) RevokeAuthToken(tokenHash string) error {
	query := `UPDATE auth_tokens SET is_revoked = TRUE WHERE token_hash = ?`
	_, err := d.db.Exec(query, tokenHash)
	if err != nil {
		return fmt.Errorf("failed to revoke auth token: %w", err)
	}
	return nil
}

func (d *Database) RevokeAllUserTokens(userID string) error {
	query := `UPDATE auth_tokens SET is_revoked = TRUE WHERE user_id = ?`
	_, err := d.db.Exec(query, userID)
	if err != nil {
		return fmt.Errorf("failed to revoke user tokens: %w", err)
	}
	return nil
}

func (d *Database) Close() error {
	return d.db.Close()
}