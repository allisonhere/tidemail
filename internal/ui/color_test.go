package ui

import (
	"math"
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

func hueOf(t *testing.T, c lipgloss.Color) float64 {
	t.Helper()
	r, g, b, ok := hexToRGB(c)
	if !ok {
		t.Fatalf("bad colour %q", c)
	}
	h, _, _ := rgbToHSL(r, g, b)
	return h * 360
}

func TestGreenerShiftsCyanTowardGreenOnly(t *testing.T) {
	foam := lipgloss.Color("#9ccfd8") // Rose Pine's Unread, the account manager's success colour
	if got := hueOf(t, greener(foam)); math.Abs(got-162) > 3 {
		t.Fatalf("rose pine foam hue %.0f°, want about 162°", got)
	}
	if pink := lipgloss.Color("#eb6f92"); greener(pink) != pink {
		t.Fatalf("pink changed to %s", greener(pink))
	}
	if gray := lipgloss.Color("#808080"); greener(gray) != gray {
		t.Fatalf("gray changed to %s", greener(gray))
	}
	if green := lipgloss.Color("#a6e3a1"); math.Abs(hueOf(t, greener(green))-hueOf(t, green)) > 15 {
		t.Fatalf("an already-green colour moved too far: %s -> %s", green, greener(green))
	}

	chrome := newManagerChrome(80, RosePine, false)
	if got := hueOf(t, chrome.successFg); math.Abs(got-162) > 6 || contrastRatio(chrome.successFg, chrome.baseBg) < 4.5 {
		t.Fatalf("rose pine success colour %s (hue %.0f°, contrast %.1f)", chrome.successFg, got, contrastRatio(chrome.successFg, chrome.baseBg))
	}
}
