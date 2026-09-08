package ui

import (
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
)

// MergeRetroTweak defers to tideui.ThemeOverrides.Apply; this pins the field
// mapping so an upstream change can't silently alter retro-theme overrides.
func TestMergeRetroTweakFieldMapping(t *testing.T) {
	base := VT100

	got := MergeRetroTweak(base, config.RetroTerminalTweak{
		Bg:     "#010203",
		Fg:     "#0a0b0c",
		Accent: "#111213",
	})

	if got.Bg != "#010203" {
		t.Errorf("Bg = %s, want #010203", got.Bg)
	}
	if got.Fg != "#0a0b0c" || got.StatusFg != "#0a0b0c" {
		t.Errorf("Fg/StatusFg = %s/%s, want #0a0b0c for both", got.Fg, got.StatusFg)
	}
	if got.BorderFocus != "#111213" || got.Selected != "#111213" || got.OverlayBorder != "#111213" {
		t.Errorf("accent fields = %s/%s/%s, want #111213", got.BorderFocus, got.Selected, got.OverlayBorder)
	}
	// Untouched fields keep the base value.
	if got.Border != base.Border || got.Unread != base.Unread || got.Error != base.Error {
		t.Errorf("non-override fields changed: %+v", got)
	}
}

func TestMergeRetroTweakEmptyTweakIsNoOp(t *testing.T) {
	base := VT52
	if got := MergeRetroTweak(base, config.RetroTerminalTweak{}); got != base {
		t.Errorf("empty tweak changed the theme:\n got  %+v\n want %+v", got, base)
	}
}

func TestMergeRetroTweakPartialOverride(t *testing.T) {
	base := VT52
	got := MergeRetroTweak(base, config.RetroTerminalTweak{Accent: "#abcdef"})
	if got.BorderFocus != "#abcdef" || got.Selected != "#abcdef" || got.OverlayBorder != "#abcdef" {
		t.Errorf("accent not applied: %+v", got)
	}
	if got.Bg != base.Bg || got.Fg != base.Fg || got.StatusFg != base.StatusFg {
		t.Errorf("bg/fg changed by an accent-only tweak: %+v", got)
	}
}
