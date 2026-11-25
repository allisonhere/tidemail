package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// clickStatusHit renders m and returns a click on the status bar hint id.
func clickStatusHit(t *testing.T, m Model, id string) tea.MouseMsg {
	t.Helper()
	_ = m.View()
	hm := m.statusHits
	var ids []string
	for _, h := range hm.hits {
		if h.id == id {
			return leftClick(hm.originX+h.x0, hm.originY+h.row)
		}
		ids = append(ids, h.id)
	}
	t.Fatalf("no status hit %q; have %v", id, ids)
	return tea.MouseMsg{}
}

func TestStatusBarHintsAreRecordedOnTheStatusRow(t *testing.T) {
	m := mouseTestModel(t)
	view := m.View()
	if strings.Contains(view, "\x1b["+hitOpenParam) {
		t.Fatal("hit markers leaked into the status bar")
	}
	click := clickStatusHit(t, m, "hint:?")
	lines := strings.Split(ansi.Strip(view), "\n")
	if click.Y != len(lines)-1 {
		t.Fatalf("status hits should be on the last row %d, got %d", len(lines)-1, click.Y)
	}
	if got := []rune(lines[click.Y])[click.X]; got != '?' {
		t.Fatalf("hint:? points at %q", got)
	}
}

func TestStatusBarHintClickPressesKey(t *testing.T) {
	m := mouseTestModel(t)
	next, _ := m.Update(clickStatusHit(t, m, "hint:?"))
	if got := next.(Model).overlay; got != overlayHelp {
		t.Fatalf("clicking the help hint should open Help, overlay=%v", got)
	}
	m = mouseTestModel(t)
	next, _ = m.Update(clickStatusHit(t, m, "hint::"))
	if got := next.(Model).overlay; got != overlayCommandPalette {
		t.Fatalf("clicking the command hint should open the palette, overlay=%v", got)
	}
	m = mouseTestModel(t)
	next, _ = m.Update(clickStatusHit(t, m, "hint:S"))
	if got := next.(Model).overlay; got != overlaySettings {
		t.Fatalf("clicking uppercase S should open Settings, overlay=%v", got)
	}
}

func TestStatusBarPlainTextIsNotClickable(t *testing.T) {
	m := mouseTestModel(t)
	m.setStatus("status message", false)
	view := m.View()
	hm := m.statusHits
	line := ansi.Strip(strings.Split(view, "\n")[m.height-1])
	x := strings.Index(line, "status message")
	if x < 0 {
		t.Fatalf("status message not rendered: %q", line)
	}
	click := leftClick(hm.originX+x+2, hm.originY)
	next, _ := m.Update(click)
	if next.(Model).overlay != overlayNone {
		t.Fatal("plain status text must not trigger an action")
	}
}

func TestStatusBarPaneHintsSkipNavigation(t *testing.T) {
	hitMarking = true
	marked := markHintText("↑/↓ move  enter toggle/sync  s sync  45%")
	hitMarking = false
	_, hits := extractHits(marked)
	got := []string{}
	for _, h := range hits {
		got = append(got, h.id)
	}
	if strings.Join(got, ",") != "hint:enter,hint:s" {
		t.Fatalf("pane hint hits = %v, want enter and s only", got)
	}
}

func TestStatusBarClickIgnoredWithMouseOff(t *testing.T) {
	m := mouseTestModel(t)
	_ = m.View()
	click := clickStatusHit(t, m, "hint:?")
	m.cfg.Display.MouseSupport = false
	next, _ := m.Update(click)
	if next.(Model).overlay != overlayNone {
		t.Fatal("status clicks must do nothing with mouse support off")
	}
}

func TestStatusBarClickDoesNotPassThroughModal(t *testing.T) {
	m := mouseTestModel(t)
	click := clickStatusHit(t, m, "hint:?")
	m.overlay = overlayQuitConfirm
	_ = m.View()
	next, _ := m.Update(click)
	if next.(Model).overlay != overlayQuitConfirm {
		t.Fatal("a status hint under a modal must not open another overlay")
	}
}

func TestStatusBarHitsStayWithinNarrowScreen(t *testing.T) {
	for _, width := range []int{24, 40, 100} {
		m := mouseTestModel(t)
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 18})
		m = next.(Model)
		view := m.View()
		if strings.Contains(view, "\x1b["+hitOpenParam) {
			t.Fatalf("width %d leaked hit markers", width)
		}
		for _, hit := range m.statusHits.hits {
			if hit.row != 0 || hit.x0 < 0 || hit.x1 > width || hit.x1 <= hit.x0 {
				t.Fatalf("width %d invalid status hit: %+v", width, hit)
			}
		}
	}
}
