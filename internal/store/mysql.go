package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLConfig holds connection info.
type MySQLConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	Database string
	Params   string
}

// MySQLStore implements Store with MySQL backend.
type MySQLStore struct {
	db *sql.DB
}

func NewMySQLStore(cfg MySQLConfig) (*MySQLStore, error) {
	if cfg.Host == "" {
		return nil, errors.New("mysql host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 3306
	}
	if cfg.Database == "" {
		cfg.Database = "nekoserver"
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	if cfg.Params != "" {
		dsn = dsn + "?" + cfg.Params
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	store := &MySQLStore{db: db}
	if err := store.init(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *MySQLStore) Ping(ctx context.Context) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	return s.db.PingContext(ctx)
}

func (s *MySQLStore) init() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
            id BIGINT AUTO_INCREMENT PRIMARY KEY,
            username VARCHAR(255) NOT NULL UNIQUE,
            password_hash VARCHAR(255) NOT NULL,
            role VARCHAR(16) NOT NULL DEFAULT 'user',
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
            id BIGINT AUTO_INCREMENT PRIMARY KEY,
            user_id BIGINT NOT NULL,
            token_hash VARCHAR(255) NOT NULL,
            expires_at DATETIME NOT NULL,
            revoked_at DATETIME NULL,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            UNIQUE KEY token_hash_unique (token_hash),
            INDEX idx_refresh_user (user_id),
            CONSTRAINT fk_refresh_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS feedback_logs (
            id BIGINT AUTO_INCREMENT PRIMARY KEY,
            user_id BIGINT NULL,
            device_id VARCHAR(255) NULL,
            lang VARCHAR(32) NULL,
            client_info JSON NULL,
            content TEXT NOT NULL,
            received_at DATETIME NOT NULL,
            ts BIGINT NOT NULL,
            INDEX idx_feedback_received (received_at DESC),
            CONSTRAINT fk_feedback_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS configs (
            id BIGINT AUTO_INCREMENT PRIMARY KEY,
            config_key VARCHAR(255) NOT NULL UNIQUE,
            config_value JSON NOT NULL,
            updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *MySQLStore) CreateUser(ctx context.Context, username, passwordHash, role string) (int64, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role) VALUES (?,?,?)`, username, passwordHash, ensureRole(role))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
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

func (s *MySQLStore) HasUsers(ctx context.Context) (bool, error) {
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

func (s *MySQLStore) SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES (?,?,?)`, userID, tokenHash, expiresAt)
	return err
}

func (s *MySQLStore) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at = NOW() WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *MySQLStore) RefreshTokenValid(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
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

func (s *MySQLStore) SaveFeedback(ctx context.Context, entry FeedbackLog) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO feedback_logs (user_id, device_id, lang, client_info, content, received_at, ts) VALUES (?,?,?,?,?,?,?)`,
		nullableInt(entry.UserID), entry.DeviceID, entry.Lang, entry.ClientInfo, entry.Content, entry.ReceivedAt, entry.Timestamp)
	return err
}

func (s *MySQLStore) ListFeedback(ctx context.Context, limit, offset int) ([]FeedbackLog, error) {
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
		if err := rows.Scan(&e.ID, &userID, &e.DeviceID, &e.Lang, &e.ClientInfo, &e.Content, &e.ReceivedAt, &e.Timestamp); err != nil {
			return nil, err
		}
		e.UserID = userID
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullableInt(v sql.NullInt64) interface{} {
	if v.Valid {
		return v.Int64
	}
	return nil
}

func (s *MySQLStore) GetConfig(ctx context.Context, key string) (json.RawMessage, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT config_value FROM configs WHERE config_key = ?`, key)
	var value json.RawMessage
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return value, nil
}

func (s *MySQLStore) SetConfig(ctx context.Context, key string, value json.RawMessage) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO configs (config_key, config_value) VALUES (?, ?) ON DUPLICATE KEY UPDATE config_value = VALUES(config_value)`, key, value)
	return err
}

func (s *MySQLStore) ListConfigs(ctx context.Context) ([]ConfigEntry, error) {
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
		if err := rows.Scan(&e.ID, &e.Key, &e.Value, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
