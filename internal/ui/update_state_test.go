package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

func updateStateModel(t *testing.T, cfg config.Config) (Model, *db.DB) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return NewModel(database, cfg, "dev", false), database
}

// A saved config.toml must not carry the update checker's runtime state, or a
// config kept in a dotfiles repo changes on every update check (issue #46).
func TestSaveConfigKeepsUpdateStateOutOfTheFile(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Updates.CheckIntervalHours = 12
	m, _ := updateStateModel(t, cfg)
	m.cfg.Updates.LastCheckedUnix = 1791513779
	m.cfg.Updates.DismissedVersion = "v9.9.9"
	m.cfg.Updates.AvailableVersion = "v9.9.9"
	m.cfg.Updates.AvailableSummary = "big release"
	m.cfg.Updates.AvailablePublished = 1791500000

	if err := m.saveConfig(); err != nil {
		t.Fatal(err)
	}
	path, _ := config.Path()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, key := range []string{"last_checked_unix", "dismissed_version", "available_version", "available_summary", "available_published_unix"} {
		if strings.Contains(text, key) {
			t.Fatalf("config.toml still contains %s:\n%s", key, text)
		}
	}
	if !strings.Contains(text, "check_interval_hours = 12") || !strings.Contains(text, "check_on_startup") {
		t.Fatalf("the two real update settings must stay in config.toml:\n%s", text)
	}
	if m.cfg.Updates.LastCheckedUnix != 1791513779 {
		t.Fatal("saving must not clear the live in-memory state")
	}

	// The point of the change: another update check leaves the file untouched.
	m.cfg.Updates.LastCheckedUnix = 1791600000
	m.cfg.Updates.DismissedVersion = "v10.0.0"
	if err := m.saveConfig(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != text {
		t.Fatalf("config.toml changed after another update check:\n--- before\n%s\n--- after\n%s", text, after)
	}
}

// The state comes back from the database on the next start.
func TestUpdateStateSurvivesARestart(t *testing.T) {
	m, database := updateStateModel(t, config.DefaultConfig())
	m.cfg.Updates.LastCheckedUnix = 1791513779
	m.cfg.Updates.DismissedVersion = "v9.9.9"
	if err := m.saveConfig(); err != nil {
		t.Fatal(err)
	}

	again := NewModel(database, config.DefaultConfig(), "dev", false)
	if again.cfg.Updates.LastCheckedUnix != 1791513779 || again.cfg.Updates.DismissedVersion != "v9.9.9" {
		t.Fatalf("state not restored: %+v", again.cfg.Updates)
	}
}

// An older config.toml still carries the values: adopt them once, then the
// database is the source of truth.
func TestUpdateStateMigratesFromAnOlderConfigOnce(t *testing.T) {
	legacy := config.DefaultConfig()
	legacy.Updates.LastCheckedUnix = 1700000000
	legacy.Updates.DismissedVersion = "v1.0.0"
	m, database := updateStateModel(t, legacy)
	if m.cfg.Updates.LastCheckedUnix != 1700000000 || m.cfg.Updates.DismissedVersion != "v1.0.0" {
		t.Fatalf("legacy values should be kept on first run: %+v", m.cfg.Updates)
	}

	// Later runs read the database, even if a stale file still has old values.
	m.cfg.Updates.LastCheckedUnix = 1800000000
	if err := m.saveUpdateState(); err != nil {
		t.Fatal(err)
	}
	again := NewModel(database, legacy, "dev", false)
	if again.cfg.Updates.LastCheckedUnix != 1800000000 {
		t.Fatalf("the database must win over the file after migration, got %d", again.cfg.Updates.LastCheckedUnix)
	}
}

// With nowhere to keep the state, it stays in the file rather than being lost.
func TestUpdateStateStaysInTheFileWithoutADatabase(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.cfg.Updates.LastCheckedUnix = 1791513779
	if err := m.saveConfig(); err != nil {
		t.Fatal(err)
	}
	path, _ := config.Path()
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "last_checked_unix") {
		t.Fatalf("state must not be dropped when there is no database:\n%s", raw)
	}
}
