package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
            core_version VARCHAR(64) NULL,
            resource_version VARCHAR(64) NULL,
            build_id VARCHAR(128) NULL,
            platform VARCHAR(64) NULL,
            arch VARCHAR(64) NULL,
            region VARCHAR(64) NULL,
            INDEX idx_feedback_received (received_at DESC),
            INDEX idx_feedback_platform (platform),
            INDEX idx_feedback_core_version (core_version),
            CONSTRAINT fk_feedback_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS configs (
            id BIGINT AUTO_INCREMENT PRIMARY KEY,
            config_key VARCHAR(255) NOT NULL UNIQUE,
            config_value JSON NOT NULL,
            updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS api_events (
            id BIGINT AUTO_INCREMENT PRIMARY KEY,
            endpoint VARCHAR(255) NOT NULL,
            method VARCHAR(16) NOT NULL,
            status_code INT NOT NULL,
            device_id VARCHAR(255) NULL,
            platform VARCHAR(64) NULL,
            arch VARCHAR(64) NULL,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            INDEX idx_api_events_created (created_at DESC),
            INDEX idx_api_events_endpoint (endpoint)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	// Add new columns if they don't exist (migration for existing databases)
	migrations := []string{
		`ALTER TABLE feedback_logs ADD COLUMN core_version VARCHAR(64) NULL`,
		`ALTER TABLE feedback_logs ADD COLUMN resource_version VARCHAR(64) NULL`,
		`ALTER TABLE feedback_logs ADD COLUMN build_id VARCHAR(128) NULL`,
		`ALTER TABLE feedback_logs ADD COLUMN platform VARCHAR(64) NULL`,
		`ALTER TABLE feedback_logs ADD COLUMN arch VARCHAR(64) NULL`,
		`ALTER TABLE feedback_logs ADD COLUMN region VARCHAR(64) NULL`,
	}
	for _, stmt := range migrations {
		// Ignore "Duplicate column name" errors which occur when column already exists
		// MySQL error code 1060 indicates duplicate column name
		_, err := s.db.Exec(stmt)
		if err != nil && !strings.Contains(err.Error(), "Duplicate column name") {
			return fmt.Errorf("migration failed: %w", err)
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

func (s *MySQLStore) GetUserByID(ctx context.Context, id int64) (*User, error) {
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

func (s *MySQLStore) ListUsers(ctx context.Context, limit, offset int) ([]User, error) {
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

func (s *MySQLStore) UpdateUser(ctx context.Context, id int64, passwordHash, role string) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	if passwordHash != "" {
		_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, role = ? WHERE id = ?`, passwordHash, ensureRole(role), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, ensureRole(role), id)
	return err
}

func (s *MySQLStore) DeleteUser(ctx context.Context, id int64) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *MySQLStore) CountUsers(ctx context.Context) (int64, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`)
	var count int64
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO feedback_logs (user_id, device_id, lang, client_info, content, received_at, ts, core_version, resource_version, build_id, platform, arch, region) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		nullableInt(entry.UserID), entry.DeviceID, entry.Lang, entry.ClientInfo, entry.Content, entry.ReceivedAt, entry.Timestamp,
		entry.CoreVersion, entry.ResourceVersion, entry.BuildID, entry.Platform, entry.Arch, entry.Region)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, device_id, lang, client_info, content, received_at, ts, COALESCE(core_version,''), COALESCE(resource_version,''), COALESCE(build_id,''), COALESCE(platform,''), COALESCE(arch,''), COALESCE(region,'') FROM feedback_logs ORDER BY received_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackLog{}
	for rows.Next() {
		var e FeedbackLog
		var userID sql.NullInt64
		if err := rows.Scan(&e.ID, &userID, &e.DeviceID, &e.Lang, &e.ClientInfo, &e.Content, &e.ReceivedAt, &e.Timestamp, &e.CoreVersion, &e.ResourceVersion, &e.BuildID, &e.Platform, &e.Arch, &e.Region); err != nil {
			return nil, err
		}
		e.UserID = userID
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *MySQLStore) ListFeedbackFiltered(ctx context.Context, filter FeedbackFilter, limit, offset int) ([]FeedbackLog, error) {
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
		if err := rows.Scan(&e.ID, &userID, &e.DeviceID, &e.Lang, &e.ClientInfo, &e.Content, &e.ReceivedAt, &e.Timestamp, &e.CoreVersion, &e.ResourceVersion, &e.BuildID, &e.Platform, &e.Arch, &e.Region); err != nil {
			return nil, err
		}
		e.UserID = userID
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetFeedbackFilterOptions(ctx context.Context) (*FeedbackFilterOptions, error) {
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

func (s *MySQLStore) SaveAPIEvent(ctx context.Context, event APIEvent) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_events (endpoint, method, status_code, device_id, platform, arch, created_at) VALUES (?,?,?,?,?,?,?)`,
		event.Endpoint, event.Method, event.StatusCode, event.DeviceID, event.Platform, event.Arch, event.CreatedAt)
	return err
}

func (s *MySQLStore) GetAPIStats(ctx context.Context, days int) (*APIStats, error) {
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
	row = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_events WHERE DATE(created_at) = CURDATE()`)
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
	rows3, err := s.db.QueryContext(ctx, `SELECT DATE(created_at) as day, COUNT(*) as cnt FROM api_events WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL ? DAY) GROUP BY DATE(created_at) ORDER BY day ASC`, days)
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

func (s *MySQLStore) CountFeedback(ctx context.Context) (int64, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM feedback_logs`)
	var count int64
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
