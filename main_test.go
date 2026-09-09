package main

import (
	"runtime/debug"
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
	opts := parseStartupOptions([]string{"--prototype-forms"})

	if !opts.prototypeForms {
		t.Fatal("expected --prototype-forms to enable prototype form mode")
	}
}

func TestParseStartupOptionsUpdateProgressPreview(t *testing.T) {
	opts := parseStartupOptions([]string{"--preview-update-progress"})

	if !opts.previewUpdateProgress {
		t.Fatal("expected --preview-update-progress to enable update progress preview")
	}
}

func TestParseStartupOptionsDisableGoogleOAuth(t *testing.T) {
	opts := parseStartupOptions([]string{"--disable-google-oauth"})

	if !opts.disableGoogleOAuth {
		t.Fatal("expected --disable-google-oauth to enable the runtime OAuth override")
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
