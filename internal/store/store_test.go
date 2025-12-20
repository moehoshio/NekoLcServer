package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}

	ctx := context.Background()

	// Test Ping
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Test CreateUser and GetUserByUsername
	userID, err := store.CreateUser(ctx, "testuser", "hashedpassword", "user")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if userID <= 0 {
		t.Fatalf("expected positive user id, got %d", userID)
	}

	user, err := store.GetUserByUsername(ctx, "testuser")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if user.Username != "testuser" {
		t.Fatalf("expected username 'testuser', got '%s'", user.Username)
	}

	// Test HasUsers
	has, err := store.HasUsers(ctx)
	if err != nil {
		t.Fatalf("has users: %v", err)
	}
	if !has {
		t.Fatalf("expected has users to be true")
	}

	// Test GetUserByUsername - not found
	_, err = store.GetUserByUsername(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Test SaveRefreshToken and RefreshTokenValid
	tokenHash := "testhash123"
	expiresAt := time.Now().Add(time.Hour)
	if err := store.SaveRefreshToken(ctx, userID, tokenHash, expiresAt); err != nil {
		t.Fatalf("save refresh token: %v", err)
	}

	valid, err := store.RefreshTokenValid(ctx, tokenHash, time.Now())
	if err != nil {
		t.Fatalf("refresh token valid: %v", err)
	}
	if !valid {
		t.Fatalf("expected token to be valid")
	}

	// Test RevokeRefreshToken
	if err := store.RevokeRefreshToken(ctx, tokenHash); err != nil {
		t.Fatalf("revoke refresh token: %v", err)
	}

	valid, err = store.RefreshTokenValid(ctx, tokenHash, time.Now())
	if err != nil {
		t.Fatalf("refresh token valid after revoke: %v", err)
	}
	if valid {
		t.Fatalf("expected token to be invalid after revoke")
	}

	// Test SaveFeedback and ListFeedback
	feedback := FeedbackLog{
		DeviceID:   "device123",
		Lang:       "en",
		ClientInfo: []byte(`{"os":"linux"}`),
		Content:    "Test feedback",
		ReceivedAt: time.Now(),
		Timestamp:  time.Now().Unix(),
	}
	if err := store.SaveFeedback(ctx, feedback); err != nil {
		t.Fatalf("save feedback: %v", err)
	}

	feedbacks, err := store.ListFeedback(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if len(feedbacks) != 1 {
		t.Fatalf("expected 1 feedback, got %d", len(feedbacks))
	}
	if feedbacks[0].Content != "Test feedback" {
		t.Fatalf("expected content 'Test feedback', got '%s'", feedbacks[0].Content)
	}

	// Test GetConfig - not found
	_, err = store.GetConfig(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for non-existent config, got %v", err)
	}

	// Test SetConfig and GetConfig
	testConfig := json.RawMessage(`{"key":"value"}`)
	if err := store.SetConfig(ctx, "testkey", testConfig); err != nil {
		t.Fatalf("set config: %v", err)
	}

	config, err := store.GetConfig(ctx, "testkey")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if string(config) != `{"key":"value"}` {
		t.Fatalf("expected config '{\"key\":\"value\"}', got '%s'", string(config))
	}

	// Test SetConfig update (upsert)
	newConfig := json.RawMessage(`{"key":"newvalue"}`)
	if err := store.SetConfig(ctx, "testkey", newConfig); err != nil {
		t.Fatalf("update config: %v", err)
	}

	config, err = store.GetConfig(ctx, "testkey")
	if err != nil {
		t.Fatalf("get updated config: %v", err)
	}
	if string(config) != `{"key":"newvalue"}` {
		t.Fatalf("expected config '{\"key\":\"newvalue\"}', got '%s'", string(config))
	}

	// Test ListConfigs
	configs, err := store.ListConfigs(ctx)
	if err != nil {
		t.Fatalf("list configs: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if configs[0].Key != "testkey" {
		t.Fatalf("expected key 'testkey', got '%s'", configs[0].Key)
	}
}

func TestSQLiteStoreDefaultPath(t *testing.T) {
	// Change to temp directory to avoid creating db in current dir
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("change to temp dir: %v", err)
	}
	defer os.Chdir(origDir)

	store, err := NewSQLiteStore(SQLiteConfig{})
	if err != nil {
		t.Fatalf("create sqlite store with default path: %v", err)
	}

	ctx := context.Background()
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Verify the default database was created
	if _, err := os.Stat(filepath.Join(tmpDir, "nekoserver.db")); os.IsNotExist(err) {
		t.Fatalf("expected default database file to exist")
	}
}

func TestMemoryStoreConfig(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()

	// Test GetConfig - not found
	_, err := store.GetConfig(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for non-existent config, got %v", err)
	}

	// Test SetConfig and GetConfig
	testConfig := json.RawMessage(`{"key":"value"}`)
	if err := store.SetConfig(ctx, "testkey", testConfig); err != nil {
		t.Fatalf("set config: %v", err)
	}

	config, err := store.GetConfig(ctx, "testkey")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if string(config) != `{"key":"value"}` {
		t.Fatalf("expected config '{\"key\":\"value\"}', got '%s'", string(config))
	}

	// Test ListConfigs
	configs, err := store.ListConfigs(ctx)
	if err != nil {
		t.Fatalf("list configs: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
}
