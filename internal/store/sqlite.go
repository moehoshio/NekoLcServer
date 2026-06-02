package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
            core_version TEXT NULL,
            resource_version TEXT NULL,
            build_id TEXT NULL,
            platform TEXT NULL,
            arch TEXT NULL,
            region TEXT NULL,
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
        )`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_received ON feedback_logs(received_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_platform ON feedback_logs(platform)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_core_version ON feedback_logs(core_version)`,
		`CREATE TABLE IF NOT EXISTS configs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            config_key TEXT NOT NULL UNIQUE,
            config_value TEXT NOT NULL,
            updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        )`,
		`CREATE TABLE IF NOT EXISTS api_events (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            endpoint TEXT NOT NULL,
            method TEXT NOT NULL,
            status_code INTEGER NOT NULL,
            device_id TEXT NULL,
            platform TEXT NULL,
            arch TEXT NULL,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        )`,
		`CREATE INDEX IF NOT EXISTS idx_api_events_created ON api_events(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_api_events_endpoint ON api_events(endpoint)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init statement failed: %w", err)
		}
	}
	// Add new columns if they don't exist (migration for existing databases)
	migrations := []string{
		`ALTER TABLE feedback_logs ADD COLUMN core_version TEXT NULL`,
		`ALTER TABLE feedback_logs ADD COLUMN resource_version TEXT NULL`,
		`ALTER TABLE feedback_logs ADD COLUMN build_id TEXT NULL`,
		`ALTER TABLE feedback_logs ADD COLUMN platform TEXT NULL`,
		`ALTER TABLE feedback_logs ADD COLUMN arch TEXT NULL`,
		`ALTER TABLE feedback_logs ADD COLUMN region TEXT NULL`,
	}
	for _, stmt := range migrations {
		// Ignore "duplicate column" errors which occur when column already exists
		// SQLite error message contains "duplicate column name"
		_, err := s.db.Exec(stmt)
		if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migration failed: %w", err)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO feedback_logs (user_id, device_id, lang, client_info, content, received_at, ts, core_version, resource_version, build_id, platform, arch, region) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		userID, entry.DeviceID, entry.Lang, string(entry.ClientInfo), entry.Content, entry.ReceivedAt, entry.Timestamp,
		entry.CoreVersion, entry.ResourceVersion, entry.BuildID, entry.Platform, entry.Arch, entry.Region)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, device_id, lang, client_info, content, received_at, ts, COALESCE(core_version,''), COALESCE(resource_version,''), COALESCE(build_id,''), COALESCE(platform,''), COALESCE(arch,''), COALESCE(region,'') FROM feedback_logs ORDER BY received_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackLog{}
	for rows.Next() {
		var e FeedbackLog
		var userID sql.NullInt64
		var clientInfo sql.NullString
		if err := rows.Scan(&e.ID, &userID, &e.DeviceID, &e.Lang, &clientInfo, &e.Content, &e.ReceivedAt, &e.Timestamp, &e.CoreVersion, &e.ResourceVersion, &e.BuildID, &e.Platform, &e.Arch, &e.Region); err != nil {
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

func (s *SQLiteStore) ListFeedbackFiltered(ctx context.Context, filter FeedbackFilter, limit, offset int) ([]FeedbackLog, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	query := `SELECT id, user_id, device_id, lang, client_info, content, received_at, ts, COALESCE(core_version,''), COALESCE(resource_version,''), COALESCE(build_id,''), COALESCE(platform,''), COALESCE(arch,''), COALESCE(region,'') FROM feedback_logs WHERE 1=1`
	args := []interface{}{}

	if filter.CoreVersion != "" {
		query += ` AND core_version = ?`
		args = append(args, filter.CoreVersion)
	}
	if filter.ResourceVersion != "" {
		query += ` AND resource_version = ?`
		args = append(args, filter.ResourceVersion)
	}
	if filter.BuildID != "" {
		query += ` AND build_id = ?`
		args = append(args, filter.BuildID)
	}
	if filter.Platform != "" {
		query += ` AND platform = ?`
		args = append(args, filter.Platform)
	}
	if filter.Arch != "" {
		query += ` AND arch = ?`
		args = append(args, filter.Arch)
	}
	if filter.Region != "" {
		query += ` AND region = ?`
		args = append(args, filter.Region)
	}
	if filter.Lang != "" {
		query += ` AND lang = ?`
		args = append(args, filter.Lang)
	}
	if filter.StartTime != nil {
		query += ` AND received_at >= ?`
		args = append(args, *filter.StartTime)
	}
	if filter.EndTime != nil {
		query += ` AND received_at <= ?`
		args = append(args, *filter.EndTime)
	}

	query += ` ORDER BY received_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackLog{}
	for rows.Next() {
		var e FeedbackLog
		var userID sql.NullInt64
		var clientInfo sql.NullString
		if err := rows.Scan(&e.ID, &userID, &e.DeviceID, &e.Lang, &clientInfo, &e.Content, &e.ReceivedAt, &e.Timestamp, &e.CoreVersion, &e.ResourceVersion, &e.BuildID, &e.Platform, &e.Arch, &e.Region); err != nil {
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

func (s *SQLiteStore) GetFeedbackFilterOptions(ctx context.Context) (*FeedbackFilterOptions, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	options := &FeedbackFilterOptions{
		CoreVersions:     []string{},
		ResourceVersions: []string{},
		BuildIDs:         []string{},
		Platforms:        []string{},
		Arches:           []string{},
		Regions:          []string{},
		Langs:            []string{},
	}

	distinctQuery := func(column string) ([]string, error) {
		query := fmt.Sprintf(`SELECT DISTINCT %s FROM feedback_logs WHERE %s IS NOT NULL AND %s != '' ORDER BY %s`, column, column, column, column)
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var result []string
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				return nil, err
			}
			result = append(result, v)
		}
		return result, rows.Err()
	}

	var err error
	if options.CoreVersions, err = distinctQuery("core_version"); err != nil {
		return nil, err
	}
	if options.ResourceVersions, err = distinctQuery("resource_version"); err != nil {
		return nil, err
	}
	if options.BuildIDs, err = distinctQuery("build_id"); err != nil {
		return nil, err
	}
	if options.Platforms, err = distinctQuery("platform"); err != nil {
		return nil, err
	}
	if options.Arches, err = distinctQuery("arch"); err != nil {
		return nil, err
	}
	if options.Regions, err = distinctQuery("region"); err != nil {
		return nil, err
	}
	if options.Langs, err = distinctQuery("lang"); err != nil {
		return nil, err
	}

	return options, nil
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

func (s *SQLiteStore) SaveAPIEvent(ctx context.Context, event APIEvent) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_events (endpoint, method, status_code, device_id, platform, arch, created_at) VALUES (?,?,?,?,?,?,?)`,
		event.Endpoint, event.Method, event.StatusCode, event.DeviceID, event.Platform, event.Arch, event.CreatedAt)
	return err
}

func (s *SQLiteStore) GetAPIStats(ctx context.Context, days int) (*APIStats, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	stats := &APIStats{
		EndpointCounts:  make(map[string]int64),
		PlatformCounts:  make(map[string]int64),
		DailyStats:      []DailyStat{},
		RecentEndpoints: []EndpointStat{},
	}

	// Total requests
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_events`)
	if err := row.Scan(&stats.TotalRequests); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	// Today's requests
	row = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_events WHERE DATE(created_at) = DATE('now')`)
	if err := row.Scan(&stats.TodayRequests); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	// Total users
	row = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`)
	if err := row.Scan(&stats.TotalUsers); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	// Total feedback
	row = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM feedback_logs`)
	if err := row.Scan(&stats.TotalFeedback); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	// Endpoint counts
	rows, err := s.db.QueryContext(ctx, `SELECT endpoint, COUNT(*) as cnt FROM api_events GROUP BY endpoint ORDER BY cnt DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var endpoint string
		var count int64
		if err := rows.Scan(&endpoint, &count); err != nil {
			return nil, err
		}
		stats.EndpointCounts[endpoint] = count
		stats.RecentEndpoints = append(stats.RecentEndpoints, EndpointStat{Endpoint: endpoint, Count: count})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Platform counts
	rows2, err := s.db.QueryContext(ctx, `SELECT platform, COUNT(*) as cnt FROM api_events WHERE platform IS NOT NULL AND platform != '' GROUP BY platform ORDER BY cnt DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var platform string
		var count int64
		if err := rows2.Scan(&platform, &count); err != nil {
			return nil, err
		}
		stats.PlatformCounts[platform] = count
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	// Daily stats for the last N days
	if days <= 0 {
		days = 7
	}
	rows3, err := s.db.QueryContext(ctx, `SELECT DATE(created_at) as day, COUNT(*) as cnt FROM api_events WHERE created_at >= DATE('now', '-' || ? || ' days') GROUP BY DATE(created_at) ORDER BY day ASC`, days)
	if err != nil {
		return nil, err
	}
	defer rows3.Close()
	for rows3.Next() {
		var day string
		var count int64
		if err := rows3.Scan(&day, &count); err != nil {
			return nil, err
		}
		stats.DailyStats = append(stats.DailyStats, DailyStat{Date: day, Count: count})
	}
	if err := rows3.Err(); err != nil {
		return nil, err
	}

	return stats, nil
}

func (s *SQLiteStore) DeleteFeedback(ctx context.Context, id int64) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `DELETE FROM feedback_logs WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) CountFeedback(ctx context.Context) (int64, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM feedback_logs`)
	var count int64
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
