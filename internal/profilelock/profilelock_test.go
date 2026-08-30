package profilelock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquirePreventsSecondProcessLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close() //nolint:errcheck

	if _, err := Acquire(path); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("Acquire() error = %v, want ErrAlreadyRunning", err)
	}
}

func TestAcquireCanReopenReleasedLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close() //nolint:errcheck
}
