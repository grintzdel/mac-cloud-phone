package signaling

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateTokenCreatesPrivateFileOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "token")

	first, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 32 {
		t.Fatalf("token too short: %q", first)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token file mode %o, want 600", perm)
	}

	second, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatal("token must be stable across restarts")
	}
}
