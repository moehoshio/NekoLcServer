package store

import (
	"context"
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
