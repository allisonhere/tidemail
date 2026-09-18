package main

import (
	"os"
	"testing"
)

// This package can reach config.Load and config.Save, which read and write the
// running user's real profile. XDG_CONFIG_HOME and XDG_DATA_HOME redirect the
// files, but the system keychain is global and no variable redirects it — so a
// test that loaded a config whose account names matched the real ones would
// read live passwords through the legacy name-keyed lookup and write copies
// back under fresh keys. Guard the whole package the way internal/ui,
// internal/db and internal/config already do.
func TestMain(m *testing.M) {
	cfgDir, err := os.MkdirTemp("", "tide-main-xdg-config-*")
	if err != nil {
		panic(err)
	}
	dataDir, err := os.MkdirTemp("", "tide-main-xdg-data-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", cfgDir)
	os.Setenv("XDG_DATA_HOME", dataDir)
	os.Setenv("TIDEMAIL_DISABLE_KEYRING", "1")

	code := m.Run()

	os.RemoveAll(cfgDir)
	os.RemoveAll(dataDir)
	os.Exit(code)
}
