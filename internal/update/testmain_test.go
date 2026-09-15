package update

import (
	"os"
	"testing"
)

// TestMain redirects HOME / XDG_CONFIG_HOME / XDG_DATA_HOME to throwaway dirs,
// mirroring the guard in internal/ui.
//
// This package is the one that most needs it. installTarget falls back to
// ~/.local/bin whenever the current executable's directory is not writable, so
// an Install test pointed at a read-only temp dir does not fail — it quietly
// resolves to the real ~/.local/bin/tidemail and overwrites the user's actual
// installed binary with the test's fixture content. That is not hypothetical:
// it happened, and the only symptom was a test reporting RequiresManual=false.
//
// Stubbing userHomeDir per-test guards the tests that remember to; this guards
// the ones that do not. -allie
func TestMain(m *testing.M) {
	var dirs []string
	for _, env := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		dir, err := os.MkdirTemp("", "tide-update-testenv-*")
		if err != nil {
			panic(err)
		}
		dirs = append(dirs, dir)
		os.Setenv(env, dir) //nolint:errcheck
	}

	code := m.Run()

	for _, dir := range dirs {
		os.RemoveAll(dir) //nolint:errcheck
	}
	os.Exit(code)
}
