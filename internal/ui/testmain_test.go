package ui

import (
	"os"
	"testing"
)

// TestMain redirects XDG_CONFIG_HOME / XDG_DATA_HOME / HOME to throwaway dirs
// so tests that call config.Save or touch user dirs can never clobber the
// running user's real ~/.config/tidemail/config.toml. -allie
//
// It also turns credential storage off. The system keychain is per-user and
// global — no XDG variable redirects it — so a test config whose account names
// match the real ones would read live passwords out of it (via the legacy
// name-keyed lookup) and write copies back under fresh keys.
func TestMain(m *testing.M) {
	cfgDir, err := os.MkdirTemp("", "tide-ui-xdg-config-*")
	if err != nil {
		panic(err)
	}
	dataDir, err := os.MkdirTemp("", "tide-ui-xdg-data-*")
	if err != nil {
		panic(err)
	}
	homeDir, err := os.MkdirTemp("", "tide-ui-home-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", cfgDir)
	os.Setenv("XDG_DATA_HOME", dataDir)
	os.Setenv("HOME", homeDir)
	os.Setenv("TIDEMAIL_DISABLE_KEYRING", "1")

	code := m.Run()

	os.RemoveAll(cfgDir)
	os.RemoveAll(dataDir)
	os.RemoveAll(homeDir)
	os.Exit(code)
}
