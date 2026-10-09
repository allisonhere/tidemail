package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
)

// fetch.log is appended to forever; past the cap it is rotated to fetch.log.1
// so it can't grow without bound.
func TestFetchLogIsRotatedPastItsSizeCap(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := config.LogPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, maxFetchLogBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}

	logIdle("hello %d", 1)

	info, err := os.Stat(path)
	if err != nil || info.Size() > 1024 {
		t.Fatalf("fetch.log should have restarted small, got %v (err %v)", info, err)
	}
	if old, err := os.Stat(path + ".1"); err != nil || old.Size() <= int64(maxFetchLogBytes) {
		t.Fatalf("the old log should be kept as fetch.log.1, got %v (err %v)", old, err)
	}
}
