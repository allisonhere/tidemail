package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Every non-retro built-in theme carries the fixed alert palette, in the
// variant that matches its background.
func TestBuiltinThemesUseFixedAlertColors(t *testing.T) {
	for _, th := range BuiltinThemes {
		if th.Name == ThemeNameVT52 || th.Name == ThemeNameVT100 {
			continue
		}
		wantErr, wantWarn, wantOK := alertRedLight, alertYellowLight, alertGreenLight
		if isDark(th.Bg) {
			wantErr, wantWarn, wantOK = alertRedDark, alertYellowDark, alertGreenDark
		}
		if th.Error != wantErr || th.Warning != wantWarn || th.Success != wantOK {
			t.Errorf("%s: got error %s warning %s success %s, want %s %s %s",
				th.Name, th.Error, th.Warning, th.Success, wantErr, wantWarn, wantOK)
		}
	}
}

// The retro themes are single-hue and keep the colors tideui gives them.
func TestRetroThemesKeepTheirAlertColors(t *testing.T) {
	for _, th := range []Theme{VT52, VT100} {
		got, _ := ThemeByName(th.Name)
		if got.Error != th.Error || got.Success != th.Success || got.Warning != th.Warning {
			t.Errorf("%s: alert colors changed", th.Name)
		}
	}
}

// The status pills draw the user's fill and text pair exactly, on dark and
// light themes alike.
func TestStatusPillsUseExactAlertPairs(t *testing.T) {
	trueColor(t)
	has := func(t *testing.T, what, got string, p alertPill) {
		t.Helper()
		if !strings.Contains(got, background(t, p.fill)) || !strings.Contains(got, foreground(t, p.text)) {
			t.Fatalf("%s: want fill %s text %s, got %q", what, p.fill, p.text, got)
		}
	}
	drained := alertPill{fill: alertPillYellow.text, text: alertPillYellow.fill}
	for _, name := range []string{"catppuccin-mocha", "catppuccin-latte"} {
		th, _ := ThemeByName(name)
		m := Model{}
		m.styles = BuildStyles(th, "", "")

		has(t, name+" done", m.pillText("deleted"), alertPillGreen)

		const prompt = "delete pending · ctrl+z undo"
		full := m.gaugePillText(prompt, destructiveUndoWindow)
		has(t, name+" full gauge", full, alertPillYellow)
		half := m.gaugePillText(prompt, destructiveUndoWindow/2)
		has(t, name+" half gauge", half, alertPillYellow)
		has(t, name+" half gauge drained", half, drained)
		has(t, name+" empty gauge", m.gaugePillText(prompt, 0), drained)

		m.width = 120
		m.setStatus("delete failed: boom", true)
		has(t, name+" error", m.renderStatusBar(), alertPillRed)
	}
}

// Plain themes keep bracketed, uncolored errors.
func TestPlainErrorStatusIsNotAPill(t *testing.T) {
	th, _ := ThemeByName(ThemeNameVT52)
	m := Model{}
	m.styles = BuildStyles(th, "", "")
	m.styles.PlainUI = true
	m.width = 120
	m.setStatus("delete failed: boom", true)
	if row := ansi.Strip(m.renderStatusBar()); strings.Contains(row, pillCapLeft) {
		t.Fatalf("plain error drew a pill: %q", row)
	}
}
