package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func composeMouseModel(t *testing.T, w, h int, body string) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Display.MouseSupport = true
	m := NewModel(nil, cfg, "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(Model)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.bodyInput.SetValue(body)
	m.sizeCompose()
	return m
}

// screenCell finds word in the rendered screen, returning its terminal cell.
func screenCell(t *testing.T, m Model, word string) (x, y int) {
	t.Helper()
	for row, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if at := strings.Index(line, word); at >= 0 {
			return ansi.StringWidth(line[:at]), row
		}
	}
	t.Fatalf("%q not on screen", word)
	return
}

func leftPress(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

// The computed body rectangle must line up with where the body is really drawn,
// for a compact and a maximized overlay.
func TestComposeBodyRectMatchesRenderedScreen(t *testing.T) {
	for _, maximized := range []bool{false, true} {
		m := composeMouseModel(t, 100, 30, "alpha beta gamma\n\nsecond paragraph")
		m.compose.maximized = maximized
		m.sizeCompose()
		x0, y0, _, _, ok := m.composeBodyRect()
		if !ok {
			t.Fatal("body rect unavailable")
		}
		x, y := screenCell(t, m, "alpha")
		if x != x0 || y != y0 {
			t.Fatalf("maximized=%v: rect origin (%d,%d), body drawn at (%d,%d)", maximized, x0, y0, x, y)
		}
	}
}

func TestComposeMouseClickPlacesCaretAndFocusesBody(t *testing.T) {
	m := composeMouseModel(t, 100, 30, "alpha beta gamma\n\nsecond paragraph")
	m.compose.focusedField = composeFieldTo
	x, y := screenCell(t, m, "gamma")
	next, _ := m.Update(leftPress(x+2, y))
	m = next.(Model)
	if m.compose.focusedField != composeFieldBody {
		t.Fatal("click did not focus the body")
	}
	if got, want := m.compose.bodyInput.ed.CursorIndex(), len("alpha beta ga"); got != want {
		t.Fatalf("caret at %d, want %d", got, want)
	}
}

func TestComposeMouseDoubleClickSelectsWord(t *testing.T) {
	m := composeMouseModel(t, 100, 30, "alpha beta gamma")
	x, y := screenCell(t, m, "beta")
	now := time.Now()
	next, _ := m.handleComposeMouse(leftPress(x+1, y), now)
	next, _ = next.(Model).handleComposeMouse(leftPress(x+1, y), now.Add(100*time.Millisecond))
	if got := next.(Model).compose.bodyInput.ed.SelectedText(); got != "beta" {
		t.Fatalf("double-click selected %q, want beta", got)
	}
	// A slow second press is just another click.
	next, _ = m.handleComposeMouse(leftPress(x+1, y), now)
	next, _ = next.(Model).handleComposeMouse(leftPress(x+1, y), now.Add(time.Second))
	if got := next.(Model).compose.bodyInput.ed.SelectedText(); got != "" {
		t.Fatalf("slow second click selected %q", got)
	}
}

func TestComposeMouseDragSelects(t *testing.T) {
	m := composeMouseModel(t, 100, 30, "alpha beta gamma")
	x, y := screenCell(t, m, "beta")
	now := time.Now()
	next, _ := m.handleComposeMouse(leftPress(x, y), now)
	next, _ = next.(Model).handleComposeMouse(tea.MouseMsg{X: x + 9, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}, now)
	if got := next.(Model).compose.bodyInput.ed.SelectedText(); got != "beta gamm" {
		t.Fatalf("drag selected %q, want %q", got, "beta gamm")
	}
	next, _ = next.(Model).handleComposeMouse(tea.MouseMsg{X: x + 9, Y: y, Action: tea.MouseActionRelease}, now)
	if next.(Model).compose.bodyMouse.dragging {
		t.Fatal("release did not end the drag")
	}
}

func TestComposeMouseWheelScrollsAndOutsideIsIgnored(t *testing.T) {
	m := composeMouseModel(t, 100, 20, strings.Repeat("line of text\n", 60))
	x0, y0, _, _, _ := m.composeBodyRect()
	before := m.compose.bodyInput.ed.ViewportTop()
	next, _ := m.Update(tea.MouseMsg{X: x0 + 1, Y: y0 + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if got := next.(Model).compose.bodyInput.ed.ViewportTop(); got != before-wheelStepRows {
		t.Fatalf("wheel moved viewport %d -> %d, want -%d", before, got, wheelStepRows)
	}
	// A click on the title row, outside the body, must not move the caret.
	caret := m.compose.bodyInput.ed.CursorIndex()
	next, _ = m.Update(leftPress(x0, 0))
	if got := next.(Model).compose.bodyInput.ed.CursorIndex(); got != caret {
		t.Fatal("click outside the body moved the caret")
	}
}
