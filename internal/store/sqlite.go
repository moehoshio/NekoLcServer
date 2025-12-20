package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteConfig holds connection info.
type SQLiteConfig struct {
	Path string
}

// SQLiteStore implements Store with SQLite backend.
type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(cfg SQLiteConfig) (*SQLiteStore, error) {
	if cfg.Path == "" {
		cfg.Path = "nekoserver.db"
	}
	// Ensure the directory exists
	dir := filepath.Dir(cfg.Path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, err
	}
	// Enable foreign keys for SQLite
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, err
	}
	store := &SQLiteStore{db: db}
	if err := store.init(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	return s.db.PingContext(ctx)
}

func (s *SQLiteStore) init() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            username TEXT NOT NULL UNIQUE,
            password_hash TEXT NOT NULL,
            role TEXT NOT NULL DEFAULT 'user',
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        )`,
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id INTEGER NOT NULL,
            token_hash TEXT NOT NULL UNIQUE,
            expires_at DATETIME NOT NULL,
            revoked_at DATETIME NULL,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
        )`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_user ON refresh_tokens(user_id)`,
		`CREATE TABLE IF NOT EXISTS feedback_logs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id INTEGER NULL,
            device_id TEXT NULL,
            lang TEXT NULL,
            client_info TEXT NULL,
            content TEXT NOT NULL,
            received_at DATETIME NOT NULL,
            ts INTEGER NOT NULL,
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
        )`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_received ON feedback_logs(received_at DESC)`,
		`CREATE TABLE IF NOT EXISTS configs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            config_key TEXT NOT NULL UNIQUE,
            config_value TEXT NOT NULL,
            updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        )`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init statement failed: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) CreateUser(ctx context.Context, username, passwordHash, role string) (int64, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role) VALUES (?,?,?)`, username, passwordHash, ensureRole(role))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, created_at, updated_at FROM users WHERE username=?`, username)
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (s *SQLiteStore) GetUserByID(ctx context.Context, id int64) (*User, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, created_at, updated_at FROM users WHERE id=?`, id)
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (s *SQLiteStore) ListUsers(ctx context.Context, limit, offset int) ([]User, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, password_hash, role, created_at, updated_at FROM users ORDER BY id ASC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateUser(ctx context.Context, id int64, passwordHash, role string) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	if passwordHash != "" {
		_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, role = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, passwordHash, ensureRole(role), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET role = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, ensureRole(role), id)
	return err
}

func (s *SQLiteStore) DeleteUser(ctx context.Context, id int64) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) CountUsers(ctx context.Context) (int64, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`)
	var count int64
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLiteStore) HasUsers(ctx context.Context) (bool, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT 1 FROM users LIMIT 1`)
	var v int
	if err := row.Scan(&v); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *SQLiteStore) SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES (?,?,?)`, userID, tokenHash, expiresAt)
	return err
}

func (s *SQLiteStore) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at = CURRENT_TIMESTAMP WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *SQLiteStore) RefreshTokenValid(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT expires_at, revoked_at FROM refresh_tokens WHERE token_hash = ?`, tokenHash)
	var expires time.Time
	var revoked sql.NullTime
	if err := row.Scan(&expires, &revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if revoked.Valid {
		return false, nil
	}
	if now.After(expires) {
		return false, nil
	}
	return true, nil
}

func (s *SQLiteStore) SaveFeedback(ctx context.Context, entry FeedbackLog) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var userID interface{} = nil
	if entry.UserID.Valid {
		userID = entry.UserID.Int64
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO feedback_logs (user_id, device_id, lang, client_info, content, received_at, ts) VALUES (?,?,?,?,?,?,?)`,
		userID, entry.DeviceID, entry.Lang, string(entry.ClientInfo), entry.Content, entry.ReceivedAt, entry.Timestamp)
	return err
}

func (s *SQLiteStore) ListFeedback(ctx context.Context, limit, offset int) ([]FeedbackLog, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, device_id, lang, client_info, content, received_at, ts FROM feedback_logs ORDER BY received_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackLog{}
	for rows.Next() {
		var e FeedbackLog
		var userID sql.NullInt64
		var clientInfo sql.NullString
		if err := rows.Scan(&e.ID, &userID, &e.DeviceID, &e.Lang, &clientInfo, &e.Content, &e.ReceivedAt, &e.Timestamp); err != nil {
			return nil, err
		}
		e.UserID = userID
		if clientInfo.Valid {
			e.ClientInfo = []byte(clientInfo.String)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetConfig(ctx context.Context, key string) (json.RawMessage, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT config_value FROM configs WHERE config_key = ?`, key)
	var value string
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return json.RawMessage(value), nil
}

func (s *SQLiteStore) SetConfig(ctx context.Context, key string, value json.RawMessage) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO configs (config_key, config_value) VALUES (?, ?) ON CONFLICT(config_key) DO UPDATE SET config_value = excluded.config_value, updated_at = CURRENT_TIMESTAMP`, key, string(value))
	return err
}

func (s *SQLiteStore) ListConfigs(ctx context.Context) ([]ConfigEntry, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id, config_key, config_value, updated_at FROM configs ORDER BY config_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConfigEntry{}
	for rows.Next() {
		var e ConfigEntry
		var value string
		if err := rows.Scan(&e.ID, &e.Key, &value, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Value = json.RawMessage(value)
		out = append(out, e)
	}
	return out, rows.Err()
}
