package server

import (
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
