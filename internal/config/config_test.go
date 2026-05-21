package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigDisplayDensityCompact(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Display.Density != "compact" {
		t.Fatalf("expected default display density compact, got %q", cfg.Display.Density)
	}
	if cfg.Display.MarkReadOnFocus {
		t.Fatal("expected mark-read-on-focus to default off")
	}
	if !cfg.Display.FocusLine {
		t.Fatal("expected focus line to default on")
	}
}

func TestNormalizeDisplayDensity(t *testing.T) {
	if got := NormalizeDisplayDensity(""); got != "compact" {
		t.Fatalf("empty: got %q", got)
	}
	if got := NormalizeDisplayDensity("COMPACT"); got != "compact" {
		t.Fatalf("compact: got %q", got)
	}
	if got := NormalizeDisplayDensity("comfortable"); got != "comfortable" {
		t.Fatalf("comfortable: got %q", got)
	}
	if got := NormalizeDisplayDensity("unknown"); got != "compact" {
		t.Fatalf("unknown: got %q", got)
	}
}

func TestDefaultConfigIncludesUpdateDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Updates.CheckOnStartup {
		t.Fatal("expected update checks to be enabled by default")
	}
	if cfg.Updates.CheckIntervalHours != 24 {
		t.Fatalf("expected 24 hour update interval, got %d", cfg.Updates.CheckIntervalHours)
	}
	if len(cfg.Accounts) != 0 {
		t.Fatalf("expected default accounts to be empty, got %#v", cfg.Accounts)
	}
}

func TestLoadPreservesUpdateConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgPath := filepath.Join(dir, "tidemail", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	data := `
theme = "catppuccin-mocha"

[display]
icons = true
date_format = "relative"
mark_read_on_open = true
mark_read_on_focus = true
focus_line = false
browser = ""
density = "compact"

[updates]
check_on_startup = false
check_interval_hours = 12
last_checked_unix = 1710000000
dismissed_version = "v1.2.3"
available_version = "v1.3.0"
available_summary = "New version available."
available_published_unix = 1710001234

[[account]]
name = "Personal"
imap_host = "imap.example.com"
imap_port = 993
imap_tls = true
smtp_host = "smtp.example.com"
smtp_port = 587
smtp_tls = true
user = "alice"
password = "secret"
from = "alice@example.com"
`
	if err := os.WriteFile(cfgPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Updates.CheckOnStartup {
		t.Fatal("expected check_on_startup to be false")
	}
	if cfg.Updates.CheckIntervalHours != 12 {
		t.Fatalf("expected interval 12, got %d", cfg.Updates.CheckIntervalHours)
	}
	if cfg.Updates.LastCheckedUnix != 1710000000 {
		t.Fatalf("unexpected last_checked_unix: %d", cfg.Updates.LastCheckedUnix)
	}
	if cfg.Updates.DismissedVersion != "v1.2.3" {
		t.Fatalf("unexpected dismissed_version: %q", cfg.Updates.DismissedVersion)
	}
	if cfg.Updates.AvailableVersion != "v1.3.0" {
		t.Fatalf("unexpected available_version: %q", cfg.Updates.AvailableVersion)
	}
	if cfg.Updates.AvailableSummary != "New version available." {
		t.Fatalf("unexpected available_summary: %q", cfg.Updates.AvailableSummary)
	}
	if cfg.Updates.AvailablePublished != 1710001234 {
		t.Fatalf("unexpected available_published_unix: %d", cfg.Updates.AvailablePublished)
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("expected one account, got %d", len(cfg.Accounts))
	}
	if cfg.Accounts[0].Name != "Personal" {
		t.Fatalf("unexpected account name: %q", cfg.Accounts[0].Name)
	}
	if cfg.Accounts[0].IMAPHost != "imap.example.com" {
		t.Fatalf("unexpected imap host: %q", cfg.Accounts[0].IMAPHost)
	}
	if cfg.Accounts[0].User != "alice" {
		t.Fatalf("unexpected account user: %q", cfg.Accounts[0].User)
	}
	if cfg.Accounts[0].Password != "secret" {
		t.Fatalf("unexpected account password: %q", cfg.Accounts[0].Password)
	}
	if cfg.Display.Density != "compact" {
		t.Fatalf("expected display density compact, got %q", cfg.Display.Density)
	}
	if !cfg.Display.MarkReadOnFocus {
		t.Fatal("expected mark_read_on_focus to load true")
	}
	if cfg.Display.FocusLine {
		t.Fatal("expected focus_line to load false")
	}
}
