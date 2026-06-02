package store

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

type memoryStore struct {
	mu       sync.Mutex
	users    map[string]*User
	nextUser int64

	refresh   map[string]refreshRecord
	tokens    map[string]accountTokenRecord
	logs      []FeedbackLog
	configs   map[string]json.RawMessage
	apiEvents []APIEvent
}

type refreshRecord struct {
	userID    int64
	expiresAt time.Time
	revoked   bool
}

type accountTokenRecord struct {
	userID    int64
	purpose   string
	expiresAt time.Time
	used      bool
}

// NewMemory creates an in-memory store for tests.
func NewMemory() Store {
	return &memoryStore{
		users:     map[string]*User{},
		nextUser:  1,
		refresh:   map[string]refreshRecord{},
		tokens:    map[string]accountTokenRecord{},
		logs:      []FeedbackLog{},
		configs:   map[string]json.RawMessage{},
		apiEvents: []APIEvent{},
	}
}

func (m *memoryStore) Ping(ctx context.Context) error { return nil }

func (m *memoryStore) CreateUser(ctx context.Context, username, passwordHash, email, role string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[username]; ok {
		return 0, errors.New("duplicate username")
	}
	email = normalizeEmail(email)
	if email != "" {
		for _, u := range m.users {
			if normalizeEmail(u.Email) == email {
				return 0, errors.New("duplicate email")
			}
		}
	}
	id := m.nextUser
	m.nextUser++
	m.users[username] = &User{ID: id, Username: username, Password: passwordHash, Email: email, Role: ensureRole(role), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	return id, nil
}

func (m *memoryStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[username]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (m *memoryStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if normalizeEmail(u.Email) == email {
			return u, nil
		}
	}
	return nil, ErrNotFound
}

func (m *memoryStore) GetUserByID(ctx context.Context, id int64) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, ErrNotFound
}

func (m *memoryStore) ListUsers(ctx context.Context, limit, offset int) ([]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	users := make([]User, 0, len(m.users))
	for _, u := range m.users {
		users = append(users, *u)
	}
	// Sort by ID (simple bubble sort for small data)
	for i := 0; i < len(users); i++ {
		for j := i + 1; j < len(users); j++ {
			if users[i].ID > users[j].ID {
				users[i], users[j] = users[j], users[i]
			}
		}
	}
	if offset >= len(users) {
		return []User{}, nil
	}
	end := offset + limit
	if end > len(users) {
		end = len(users)
	}
	return users[offset:end], nil
}

func (m *memoryStore) UpdateUser(ctx context.Context, id int64, passwordHash, email, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	email = normalizeEmail(email)
	if email != "" {
		for _, u := range m.users {
			if u.ID != id && normalizeEmail(u.Email) == email {
				return errors.New("duplicate email")
			}
		}
	}
	for _, u := range m.users {
		if u.ID == id {
			if passwordHash != "" {
				u.Password = passwordHash
			}
			if email != "" {
				u.Email = email
			}
			u.Role = ensureRole(role)
			u.UpdatedAt = time.Now()
			return nil
		}
	}
	return ErrNotFound
}

func (m *memoryStore) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.ID == id {
			u.Password = passwordHash
			u.UpdatedAt = time.Now()
			return nil
		}
	}
	return ErrNotFound
}

func (m *memoryStore) UpdateUserEmail(ctx context.Context, id int64, email string, verified bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	email = normalizeEmail(email)
	if email != "" {
		for _, u := range m.users {
			if u.ID != id && normalizeEmail(u.Email) == email {
				return errors.New("duplicate email")
			}
		}
	}
	for _, u := range m.users {
		if u.ID == id {
			u.Email = email
			u.EmailVerified = verified
			u.UpdatedAt = time.Now()
			return nil
		}
	}
	return ErrNotFound
}

func (m *memoryStore) SetEmailVerified(ctx context.Context, id int64, verified bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.ID == id {
			u.EmailVerified = verified
			u.UpdatedAt = time.Now()
			return nil
		}
	}
	return ErrNotFound
}

func (m *memoryStore) DeleteUser(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for username, u := range m.users {
		if u.ID == id {
			delete(m.users, username)
			return nil
		}
	}
	return ErrNotFound
}

func (m *memoryStore) CountUsers(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.users)), nil
}

func (m *memoryStore) HasUsers(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users) > 0, nil
}

func (m *memoryStore) SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refresh[tokenHash] = refreshRecord{userID: userID, expiresAt: expiresAt}
	return nil
}

func (m *memoryStore) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.refresh[tokenHash]
	if !ok {
		return nil
	}
	rec.revoked = true
	m.refresh[tokenHash] = rec
	return nil
}

func (m *memoryStore) RefreshTokenValid(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.refresh[tokenHash]
	if !ok {
		return false, nil
	}
	if rec.revoked || now.After(rec.expiresAt) {
		return false, nil
	}
	return true, nil
}

func (m *memoryStore) SaveAccountToken(ctx context.Context, userID int64, tokenHash, purpose string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[tokenHash] = accountTokenRecord{userID: userID, purpose: purpose, expiresAt: expiresAt}
	return nil
}

func (m *memoryStore) ConsumeAccountToken(ctx context.Context, tokenHash, purpose string, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.tokens[tokenHash]
	if !ok || rec.purpose != purpose {
		return 0, ErrNotFound
	}
	if rec.used || now.After(rec.expiresAt) {
		return 0, ErrNotFound
	}
	rec.used = true
	m.tokens[tokenHash] = rec
	return rec.userID, nil
}

func (m *memoryStore) SaveFeedback(ctx context.Context, entry FeedbackLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry.ID = int64(len(m.logs) + 1)
	m.logs = append(m.logs, entry)
	return nil
}

func (m *memoryStore) DeleteFeedback(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.logs {
		if m.logs[i].ID == id {
			m.logs = append(m.logs[:i], m.logs[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (m *memoryStore) ListFeedback(ctx context.Context, limit, offset int) ([]FeedbackLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if offset >= len(m.logs) {
		return []FeedbackLog{}, nil
	}
	end := offset + limit
	if end > len(m.logs) {
		end = len(m.logs)
	}
	// newest first
	res := []FeedbackLog{}
	for i := len(m.logs) - 1 - offset; i >= 0 && len(res) < limit; i-- {
		res = append(res, m.logs[i])
	}
	return res, nil
}

func (m *memoryStore) ListFeedbackFiltered(ctx context.Context, filter FeedbackFilter, limit, offset int) ([]FeedbackLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Filter logs
	filtered := []FeedbackLog{}
	for i := len(m.logs) - 1; i >= 0; i-- {
		entry := m.logs[i]
		if filter.CoreVersion != "" && entry.CoreVersion != filter.CoreVersion {
			continue
		}
		if filter.ResourceVersion != "" && entry.ResourceVersion != filter.ResourceVersion {
			continue
		}
		if filter.BuildID != "" && entry.BuildID != filter.BuildID {
			continue
		}
		if filter.Platform != "" && entry.Platform != filter.Platform {
			continue
		}
		if filter.Arch != "" && entry.Arch != filter.Arch {
			continue
		}
		if filter.Region != "" && entry.Region != filter.Region {
			continue
		}
		if filter.Lang != "" && entry.Lang != filter.Lang {
			continue
		}
		if filter.StartTime != nil && entry.ReceivedAt.Before(*filter.StartTime) {
			continue
		}
		if filter.EndTime != nil && entry.ReceivedAt.After(*filter.EndTime) {
			continue
		}
		filtered = append(filtered, entry)
	}

	// Apply pagination
	if offset >= len(filtered) {
		return []FeedbackLog{}, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], nil
}

func (m *memoryStore) GetFeedbackFilterOptions(ctx context.Context) (*FeedbackFilterOptions, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	options := &FeedbackFilterOptions{
		CoreVersions:     []string{},
		ResourceVersions: []string{},
		BuildIDs:         []string{},
		Platforms:        []string{},
		Arches:           []string{},
		Regions:          []string{},
		Langs:            []string{},
	}

	coreVersionsMap := map[string]bool{}
	resourceVersionsMap := map[string]bool{}
	buildIDsMap := map[string]bool{}
	platformsMap := map[string]bool{}
	archesMap := map[string]bool{}
	regionsMap := map[string]bool{}
	langsMap := map[string]bool{}

	for _, entry := range m.logs {
		if entry.CoreVersion != "" {
			coreVersionsMap[entry.CoreVersion] = true
		}
		if entry.ResourceVersion != "" {
			resourceVersionsMap[entry.ResourceVersion] = true
		}
		if entry.BuildID != "" {
			buildIDsMap[entry.BuildID] = true
		}
		if entry.Platform != "" {
			platformsMap[entry.Platform] = true
		}
		if entry.Arch != "" {
			archesMap[entry.Arch] = true
		}
		if entry.Region != "" {
			regionsMap[entry.Region] = true
		}
		if entry.Lang != "" {
			langsMap[entry.Lang] = true
		}
	}

	for v := range coreVersionsMap {
		options.CoreVersions = append(options.CoreVersions, v)
	}
	for v := range resourceVersionsMap {
		options.ResourceVersions = append(options.ResourceVersions, v)
	}
	for v := range buildIDsMap {
		options.BuildIDs = append(options.BuildIDs, v)
	}
	for v := range platformsMap {
		options.Platforms = append(options.Platforms, v)
	}
	for v := range archesMap {
		options.Arches = append(options.Arches, v)
	}
	for v := range regionsMap {
		options.Regions = append(options.Regions, v)
	}
	for v := range langsMap {
		options.Langs = append(options.Langs, v)
	}

	return options, nil
}

func (m *memoryStore) GetConfig(ctx context.Context, key string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.configs[key]; ok {
		return v, nil
	}
	return nil, ErrNotFound
}

func (m *memoryStore) SetConfig(ctx context.Context, key string, value json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[key] = value
	return nil
}

func (m *memoryStore) ListConfigs(ctx context.Context) ([]ConfigEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ConfigEntry, 0, len(m.configs))
	var id int64 = 1
	for k, v := range m.configs {
		out = append(out, ConfigEntry{ID: id, Key: k, Value: v, UpdatedAt: time.Now()})
		id++
	}
	return out, nil
}

func (m *memoryStore) SaveAPIEvent(ctx context.Context, event APIEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	event.ID = int64(len(m.apiEvents) + 1)
	m.apiEvents = append(m.apiEvents, event)
	return nil
}

func (m *memoryStore) GetAPIStats(ctx context.Context, days int) (*APIStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats := &APIStats{
		EndpointCounts:  make(map[string]int64),
		PlatformCounts:  make(map[string]int64),
		DailyStats:      []DailyStat{},
		RecentEndpoints: []EndpointStat{},
		TotalRequests:   int64(len(m.apiEvents)),
		TotalUsers:      int64(len(m.users)),
		TotalFeedback:   int64(len(m.logs)),
	}

	if days <= 0 {
		days = 7
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	cutoff := today.AddDate(0, 0, -days)

	dailyCounts := make(map[string]int64)

	for _, event := range m.apiEvents {
		// Count by endpoint
		stats.EndpointCounts[event.Endpoint]++

		// Count by platform
		if event.Platform != "" {
			stats.PlatformCounts[event.Platform]++
		}

		// Today's count
		eventDate := time.Date(event.CreatedAt.Year(), event.CreatedAt.Month(), event.CreatedAt.Day(), 0, 0, 0, 0, time.UTC)
		if eventDate.Equal(today) {
			stats.TodayRequests++
		}

		// Daily stats
		if event.CreatedAt.After(cutoff) {
			dateStr := event.CreatedAt.Format("2006-01-02")
			dailyCounts[dateStr]++
		}
	}

	// Convert endpoint counts to sorted list
	for endpoint, count := range stats.EndpointCounts {
		stats.RecentEndpoints = append(stats.RecentEndpoints, EndpointStat{Endpoint: endpoint, Count: count})
	}

	// Convert daily counts to sorted list
	for dateStr, count := range dailyCounts {
		stats.DailyStats = append(stats.DailyStats, DailyStat{Date: dateStr, Count: count})
	}

	return stats, nil
}

func (m *memoryStore) CountFeedback(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.logs)), nil
}
