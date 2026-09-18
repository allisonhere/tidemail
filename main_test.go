package main

import (
	"errors"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
)

func TestResolvedVersionFromBuildInfoPrefersModuleVersion(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "v1.3.2",
		},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdef1234567890"},
			{Key: "vcs.modified", Value: "false"},
		},
	}

	got := resolvedVersionFromBuildInfo(info)
	if got != "v1.3.2" {
		t.Fatalf("expected module version to win, got %q", got)
	}
}

func TestFormatConfigLoadErrorNamesPathAndRecovery(t *testing.T) {
	path := "/tmp/tidemail/config.toml"
	got := formatConfigLoadError(path, errors.New("toml: expected value"), config.DefaultConfig())
	for _, want := range []string{path, "expected value", "restart TideMail", "config parses"} {
		if !strings.Contains(got, want) {
			t.Fatalf("startup error %q does not contain %q", got, want)
		}
	}
}

func TestResolvedVersionFromBuildInfoFallsBackToRevision(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "(devel)",
		},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdef1234567890"},
			{Key: "vcs.modified", Value: "true"},
		},
	}

	got := resolvedVersionFromBuildInfo(info)
	if got != "abcdef1-dirty" {
		t.Fatalf("expected short dirty revision fallback, got %q", got)
	}
}

func TestParseStartupOptionsPrototypeForms(t *testing.T) {
	opts, err := parseStartupOptions([]string{"--prototype-forms"})
	if err != nil {
		t.Fatal(err)
	}

	if !opts.prototypeForms {
		t.Fatal("expected --prototype-forms to enable prototype form mode")
	}
}

func TestParseStartupOptionsUpdateProgressPreview(t *testing.T) {
	opts, err := parseStartupOptions([]string{"--preview-update-progress"})
	if err != nil {
		t.Fatal(err)
	}

	if !opts.previewUpdateProgress {
		t.Fatal("expected --preview-update-progress to enable update progress preview")
	}
}

func TestParseStartupOptionsDisableGoogleOAuth(t *testing.T) {
	opts, err := parseStartupOptions([]string{"--disable-google-oauth"})
	if err != nil {
		t.Fatal(err)
	}

	if !opts.disableGoogleOAuth {
		t.Fatal("expected --disable-google-oauth to enable the runtime OAuth override")
	}
}

// --open names the message a dashboard panel handed over, by the id in the
// cache both programs read.
func TestParseStartupOptionsOpenMessage(t *testing.T) {
	opts, err := parseStartupOptions([]string{"--open", "4821"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.openMessageID != 4821 {
		t.Fatalf("openMessageID = %d, want 4821", opts.openMessageID)
	}

	// The other flags still parse beside it.
	opts, err = parseStartupOptions([]string{"--preview-manual-update", "--open", "12"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.openMessageID != 12 || !opts.previewManualUpdate {
		t.Fatalf("opts = %+v, want the id and the preview flag", opts)
	}

	// A bare positional argument stays ignored, so nothing starts treating one
	// as a value.
	if opts, err = parseStartupOptions([]string{"4821"}); err != nil || opts.openMessageID != 0 {
		t.Fatalf("a positional argument set openMessageID = %d (%v)", opts.openMessageID, err)
	}
}

// A value that is not there, or is not an id, is a startup error: the one
// failure that would otherwise look like the feature quietly not working.
func TestParseStartupOptionsRejectsABadOpenValue(t *testing.T) {
	for _, args := range [][]string{
		{"--open"},
		{"--open", "abc"},
		{"--open", "0"},
		{"--open", "-3"},
	} {
		if _, err := parseStartupOptions(args); err == nil {
			t.Errorf("parseStartupOptions(%v) accepted a bad id", args)
		}
	}
}

func TestDisableGoogleOAuthOverrideIsRuntimeOnlyAndProviderSpecific(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OAuth.GoogleClientID = "google-id"
	cfg.OAuth.GoogleClientSecret = "google-secret"
	cfg.OAuth.MSClientID = "microsoft-id"

	applyStartupOverrides(&cfg, startupOptions{disableGoogleOAuth: true})

	if cfg.OAuth.GoogleClientID != "" || cfg.OAuth.GoogleClientSecret != "" || !cfg.OAuth.GoogleDisabled {
		t.Fatalf("expected Google OAuth credentials disabled, got %#v", cfg.OAuth)
	}
	if cfg.OAuth.MSClientID != "microsoft-id" {
		t.Fatalf("Microsoft OAuth should be unchanged, got %q", cfg.OAuth.MSClientID)
	}
}

func TestProgramOptionsKeepTerminalMouseSelectionAvailable(t *testing.T) {
	opts := programOptions()
	if len(opts) != 1 {
		t.Fatalf("expected only alt-screen option so terminal mouse selection stays available, got %d options", len(opts))
	}
}
