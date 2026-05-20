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
	if cfg.Source != (SourceConfig{}) {
		t.Fatalf("expected default source config to be empty, got %#v", cfg.Source)
	}
}

func TestLoadPreservesUpdateConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgPath := filepath.Join(dir, "rss", "config.toml")
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

[feed]
max_body_mib = 10

[updates]
check_on_startup = false
check_interval_hours = 12
last_checked_unix = 1710000000
dismissed_version = "v1.2.3"
available_version = "v1.3.0"
available_summary = "New version available."
available_published_unix = 1710001234

[source]
greader_url = "https://rss.example.com/api/greader.php"
greader_login = "alice"
greader_password = "secret"
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
	if cfg.Source.GReaderURL != "https://rss.example.com/api/greader.php" {
		t.Fatalf("unexpected greader_url: %q", cfg.Source.GReaderURL)
	}
	if cfg.Source.GReaderLogin != "alice" {
		t.Fatalf("unexpected greader_login: %q", cfg.Source.GReaderLogin)
	}
	if cfg.Source.GReaderPassword != "secret" {
		t.Fatalf("unexpected greader_password: %q", cfg.Source.GReaderPassword)
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
