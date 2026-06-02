package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// newStoresForAccountTests returns both backends exercised by the account tests.
func newStoresForAccountTests(t *testing.T) map[string]Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "account.db")
	sq, err := NewSQLiteStore(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	return map[string]Store{
		"sqlite": sq,
		"memory": NewMemory(),
	}
}

func TestUserEmailLifecycle(t *testing.T) {
	ctx := context.Background()
	for name, st := range newStoresForAccountTests(t) {
		t.Run(name, func(t *testing.T) {
			id, err := st.CreateUser(ctx, "alice", "hash", "Alice@Example.com", "user")
			if err != nil {
				t.Fatalf("create user: %v", err)
			}

			// Email is normalized to lowercase and retrievable, case-insensitively.
			u, err := st.GetUserByEmail(ctx, "alice@example.com")
			if err != nil {
				t.Fatalf("get by email: %v", err)
			}
			if u.ID != id || u.Email != "alice@example.com" {
				t.Fatalf("unexpected user: %+v", u)
			}
			if u.EmailVerified {
				t.Fatalf("new user should not be verified")
			}

			// Duplicate email is rejected.
			if _, err := st.CreateUser(ctx, "bob", "hash", "ALICE@example.com", "user"); err == nil {
				t.Fatalf("expected duplicate email to be rejected")
			}

			// Empty email lookups never match.
			if _, err := st.GetUserByEmail(ctx, ""); err != ErrNotFound {
				t.Fatalf("expected ErrNotFound for empty email, got %v", err)
			}

			// Verify and update email.
			if err := st.SetEmailVerified(ctx, id, true); err != nil {
				t.Fatalf("set verified: %v", err)
			}
			if err := st.UpdateUserEmail(ctx, id, "alice2@example.com", false); err != nil {
				t.Fatalf("update email: %v", err)
			}
			u, err = st.GetUserByID(ctx, id)
			if err != nil {
				t.Fatalf("get by id: %v", err)
			}
			if u.Email != "alice2@example.com" || u.EmailVerified {
				t.Fatalf("email update not applied: %+v", u)
			}

			// Password update.
			if err := st.UpdateUserPassword(ctx, id, "newhash"); err != nil {
				t.Fatalf("update password: %v", err)
			}
			u, _ = st.GetUserByID(ctx, id)
			if u.Password != "newhash" {
				t.Fatalf("password not updated: %s", u.Password)
			}
		})
	}
}

func TestAccountTokenSingleUse(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	for name, st := range newStoresForAccountTests(t) {
		t.Run(name, func(t *testing.T) {
			id, err := st.CreateUser(ctx, "carol", "hash", "carol@example.com", "user")
			if err != nil {
				t.Fatalf("create user: %v", err)
			}
			if err := st.SaveAccountToken(ctx, id, "tokenhash", AccountTokenPurposeReset, now.Add(time.Hour)); err != nil {
				t.Fatalf("save token: %v", err)
			}

			// Wrong purpose does not match.
			if _, err := st.ConsumeAccountToken(ctx, "tokenhash", AccountTokenPurposeVerify, now); err != ErrNotFound {
				t.Fatalf("expected ErrNotFound for wrong purpose, got %v", err)
			}

			// Correct consume returns the user id.
			uid, err := st.ConsumeAccountToken(ctx, "tokenhash", AccountTokenPurposeReset, now)
			if err != nil || uid != id {
				t.Fatalf("consume token: uid=%d err=%v", uid, err)
			}

			// Second consume fails (single-use).
			if _, err := st.ConsumeAccountToken(ctx, "tokenhash", AccountTokenPurposeReset, now); err != ErrNotFound {
				t.Fatalf("expected single-use token to be consumed, got %v", err)
			}

			// Expired token is rejected.
			if err := st.SaveAccountToken(ctx, id, "expired", AccountTokenPurposeVerify, now.Add(-time.Minute)); err != nil {
				t.Fatalf("save expired token: %v", err)
			}
			if _, err := st.ConsumeAccountToken(ctx, "expired", AccountTokenPurposeVerify, now); err != ErrNotFound {
				t.Fatalf("expected expired token to be rejected, got %v", err)
			}
		})
	}
}
