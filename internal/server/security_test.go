package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveSafePathRejectsSiblingPrefix ensures that a sibling directory that
// merely shares a textual prefix with the base directory is not treated as being
// inside the base. This guards against the prefix-boundary path traversal bug.
func TestResolveSafePathRejectsSiblingPrefix(t *testing.T) {
	base := filepath.Join(t.TempDir(), "assets")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	sibling := base + "-evil"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("mkdir sibling: %v", err)
	}

	if _, err := resolveSafePath(base, sibling); err == nil {
		t.Fatalf("expected sibling directory with shared prefix to be rejected")
	}
}

// TestResolveSafePathRejectsTraversal ensures relative traversal escaping the
// base directory is rejected.
func TestResolveSafePathRejectsTraversal(t *testing.T) {
	base := filepath.Join(t.TempDir(), "assets")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}

	if _, err := resolveSafePath(base, "../../etc/passwd"); err == nil {
		t.Fatalf("expected traversal path to be rejected")
	}
}

// TestResolveSafePathAllowsWithinBase ensures legitimate relative paths inside
// the base directory are accepted.
func TestResolveSafePathAllowsWithinBase(t *testing.T) {
	base := filepath.Join(t.TempDir(), "assets")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}

	resolved, err := resolveSafePath(base, "sub/file.txt")
	if err != nil {
		t.Fatalf("expected path within base to be accepted: %v", err)
	}
	absBase, _ := filepath.Abs(base)
	if !pathWithinBase(resolved, absBase) {
		t.Fatalf("resolved path %q should be within base %q", resolved, absBase)
	}
}

// TestSecurityHeadersMiddleware ensures the defensive response headers are set.
func TestSecurityHeadersMiddleware(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("header %q = %q, want %q", k, got, v)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("expected a Content-Security-Policy header to be set")
	}
}

// TestSameOriginWebSocket validates the Cross-Site WebSocket Hijacking guard.
func TestSameOriginWebSocket(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		host   string
		allow  bool
	}{
		{"no origin (non-browser client)", "", "example.com", true},
		{"matching origin", "https://example.com", "example.com", true},
		{"matching origin with port", "http://example.com:8080", "example.com:8080", true},
		{"cross origin", "https://evil.com", "example.com", false},
		{"malformed origin", "://bad", "example.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/v0/ws", nil)
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := sameOriginWebSocket(r); got != tc.allow {
				t.Errorf("sameOriginWebSocket(origin=%q, host=%q) = %v, want %v", tc.origin, tc.host, got, tc.allow)
			}
		})
	}
}
