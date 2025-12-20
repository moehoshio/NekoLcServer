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

	refresh map[string]refreshRecord
	logs    []FeedbackLog
	configs map[string]json.RawMessage
}

type refreshRecord struct {
	userID    int64
	expiresAt time.Time
	revoked   bool
}

// NewMemory creates an in-memory store for tests.
func NewMemory() Store {
	return &memoryStore{
		users:    map[string]*User{},
		nextUser: 1,
		refresh:  map[string]refreshRecord{},
		logs:     []FeedbackLog{},
		configs:  map[string]json.RawMessage{},
	}
}

func (m *memoryStore) Ping(ctx context.Context) error { return nil }

func (m *memoryStore) CreateUser(ctx context.Context, username, passwordHash, role string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[username]; ok {
		return 0, errors.New("duplicate username")
	}
	id := m.nextUser
	m.nextUser++
	m.users[username] = &User{ID: id, Username: username, Password: passwordHash, Role: ensureRole(role), CreatedAt: time.Now(), UpdatedAt: time.Now()}
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

func (m *memoryStore) UpdateUser(ctx context.Context, id int64, passwordHash, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.ID == id {
			if passwordHash != "" {
				u.Password = passwordHash
			}
			u.Role = ensureRole(role)
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

func (m *memoryStore) SaveFeedback(ctx context.Context, entry FeedbackLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry.ID = int64(len(m.logs) + 1)
	m.logs = append(m.logs, entry)
	return nil
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
