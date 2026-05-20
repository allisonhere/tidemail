package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestAccentReadableOnKeepsStrongColors(t *testing.T) {
	// Dark red on light bg already has good contrast.
	accent := lipgloss.Color("#c41e3a")
	bg := lipgloss.Color("#eff1f5")
	got := accentReadableOn(accent, bg, 3)
	if got != accent {
		t.Fatalf("expected unchanged accent, got %q want %q", got, accent)
	}
	if contrastRatio(got, bg) < 3 {
		t.Fatalf("contrast %f below 3", contrastRatio(got, bg))
	}
}

func TestAccentReadableOnDarkensPastelOnLightBg(t *testing.T) {
	accent := lipgloss.Color("#e0af68") // Gold from folder presets
	bg := lipgloss.Color("#eff1f5")     // Catppuccin Latte base
	if contrastRatio(accent, bg) >= 3 {
		t.Fatal("expected test accent to start below 3:1 on this bg")
	}
	got := accentReadableOn(accent, bg, 3)
	if contrastRatio(got, bg) < 3 {
		t.Fatalf("expected contrast >= 3, got %f (color %q)", contrastRatio(got, bg), got)
	}
}

func TestAccentReadableOnLightensOnDarkBg(t *testing.T) {
	accent := lipgloss.Color("#1e2030") // very dark
	bg := lipgloss.Color("#1e1e2e")     // dark pane
	if contrastRatio(accent, bg) >= 3 {
		t.Skip("accent already readable on bg")
	}
	got := accentReadableOn(accent, bg, 3)
	if contrastRatio(got, bg) < 3 {
		t.Fatalf("expected contrast >= 3, got %f (color %q)", contrastRatio(got, bg), got)
	}
}

func TestAccentReadableOnEmptyAccent(t *testing.T) {
	got := accentReadableOn("", lipgloss.Color("#ffffff"), 3)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
