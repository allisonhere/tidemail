package main

import (
	"runtime/debug"
	"testing"
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

func TestProgramOptionsKeepTerminalMouseSelectionAvailable(t *testing.T) {
	opts := programOptions()
	if len(opts) != 1 {
		t.Fatalf("expected only alt-screen option so terminal mouse selection stays available, got %d options", len(opts))
	}
}
