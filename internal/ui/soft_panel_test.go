package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
)

func TestSoftPanelBoxEmbedsTitleInTopBorder(t *testing.T) {
	chrome := newManagerChrome(40, CatppuccinMocha, false)
	box := renderSoftPanelBox("hello", 40, "tidemail", "settings", chrome)

	lines := strings.Split(box, "\n")
	top := lines[0]
	if got := lipgloss.Width(top); got != 42 {
		t.Fatalf("expected top border to span width+2 = 42 cells, got %d", got)
	}
	stripped := ansi.Strip(top)
	if !strings.HasPrefix(stripped, "╭─ tidemail · settings ") {
		t.Fatalf("expected embedded title in top border, got %q", stripped)
	}
	if !strings.HasSuffix(stripped, "╮") {
		t.Fatalf("expected top border to end with ╮, got %q", stripped)
	}
	bottom := ansi.Strip(lines[len(lines)-1])
	if !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
		t.Fatalf("expected rounded bottom corners, got %q", bottom)
	}
}

func TestSoftPanelBoxPlainUIFallsBackToASCII(t *testing.T) {
	chrome := newManagerChrome(40, VT52, true)
	box := renderSoftPanelBox("hello", 40, "tidemail", "settings", chrome)

	if strings.Contains(box, "╭") || strings.Contains(box, "─") {
		t.Fatalf("expected plainUI soft panel to avoid unicode box drawing, got %q", box)
	}
	if !strings.Contains(ansi.Strip(box), "tidemail · settings") {
		t.Fatalf("expected plainUI soft panel to keep a title line, got %q", box)
	}
}

func TestSoftToggleGlyphs(t *testing.T) {
	chrome := newManagerChrome(40, CatppuccinMocha, false)
	if got := ansi.Strip(renderSoftToggle(true, false, chrome)); got != "● on" {
		t.Fatalf("expected soft toggle on to render %q, got %q", "● on", got)
	}
	if got := ansi.Strip(renderSoftToggle(false, false, chrome)); got != "○ off" {
		t.Fatalf("expected soft toggle off to render %q, got %q", "○ off", got)
	}

	plain := newManagerChrome(40, VT52, true)
	if got := ansi.Strip(renderSoftToggle(true, false, plain)); got != "(x) on" {
		t.Fatalf("expected plainUI soft toggle on to render %q, got %q", "(x) on", got)
	}
	if got := ansi.Strip(renderSoftToggle(false, false, plain)); got != "( ) off" {
		t.Fatalf("expected plainUI soft toggle off to render %q, got %q", "( ) off", got)
	}
}

func TestSoftPickerPinsChevronsAtRightEdge(t *testing.T) {
	chrome := newManagerChrome(40, CatppuccinMocha, false)
	row := renderSoftPicker(30, "catppuccin-mocha", true, chrome)
	if got := lipgloss.Width(row); got != 30 {
		t.Fatalf("expected picker row width 30, got %d", got)
	}
	stripped := ansi.Strip(row)
	if !strings.HasSuffix(stripped, "‹› ") {
		t.Fatalf("expected picker to end with the ‹› affordance, got %q", stripped)
	}
}

func TestSettingsViewDropsHeaderBarAndUsesSoftHints(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	view := ansi.Strip(s.View(62, 24, newManagerChrome(62, CatppuccinMocha, false)))

	if strings.Contains(view, "SETTINGS") {
		t.Fatalf("expected soft settings view to drop the SETTINGS header bar, got %q", view)
	}
	if !strings.Contains(view, "esc") {
		t.Fatalf("expected soft settings view to keep esc hint, got %q", view)
	}
}
