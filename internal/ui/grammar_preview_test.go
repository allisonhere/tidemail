package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func longGrammarText(lines int) string {
	var sb strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&sb, "Line %03d of the corrected message body.\n", i)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func grammarModel(t *testing.T, w, h, lines int) Model {
	t.Helper()
	m := mouseTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(Model)
	m.grammarCorrected = longGrammarText(lines)
	m.overlay = overlayGrammarPreview
	return m
}

func TestGrammarPreviewLongTextKeepsHintsVisibleAndFitsScreen(t *testing.T) {
	m := grammarModel(t, 100, 30, 200)
	view := ansi.Strip(m.View())
	if got := strings.Count(view, "\n") + 1; got > m.height {
		t.Fatalf("view is %d rows tall on a %d-row screen", got, m.height)
	}
	if !strings.Contains(view, "accept") || !strings.Contains(view, "cancel") {
		t.Fatalf("the accept/cancel hints are clipped off the bottom:\n%s", view)
	}
	if !strings.Contains(view, "Line 001") {
		t.Fatalf("should open at the top of the text:\n%s", view)
	}
	if strings.Contains(view, "Line 200") {
		t.Fatalf("200 lines cannot all fit on a 30-row screen:\n%s", view)
	}
}

func TestGrammarPreviewScrolls(t *testing.T) {
	m := grammarModel(t, 100, 30, 200)
	press := func(k tea.KeyMsg) {
		next, _ := m.Update(k)
		m = next.(Model)
	}
	press(tea.KeyMsg{Type: tea.KeyPgDown})
	view := ansi.Strip(m.View())
	if strings.Contains(view, "Line 001") || !strings.Contains(view, "accept") {
		t.Fatalf("PgDn should move down and keep the hints:\n%s", view)
	}
	press(tea.KeyMsg{Type: tea.KeyEnd})
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "Line 200") || !strings.Contains(view, "accept") {
		t.Fatalf("End should reach the last line with hints still shown:\n%s", view)
	}
	press(tea.KeyMsg{Type: tea.KeyHome})
	if view = ansi.Strip(m.View()); !strings.Contains(view, "Line 001") {
		t.Fatalf("Home should return to the top:\n%s", view)
	}
	if m.overlay != overlayGrammarPreview {
		t.Fatalf("scrolling must not close the preview, overlay=%v", m.overlay)
	}
}

func TestGrammarPreviewShortTextAndTinyScreens(t *testing.T) {
	m := grammarModel(t, 100, 30, 3)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Line 003") || !strings.Contains(view, "accept") {
		t.Fatalf("short text should show fully with hints:\n%s", view)
	}
	for _, size := range [][2]int{{60, 14}, {40, 12}} {
		m := grammarModel(t, size[0], size[1], 80)
		view := ansi.Strip(m.View())
		if rows := strings.Count(view, "\n") + 1; rows > m.height {
			t.Errorf("%dx%d: view is %d rows", size[0], size[1], rows)
		}
		if !strings.Contains(view, "accept") {
			t.Errorf("%dx%d: hints clipped:\n%s", size[0], size[1], view)
		}
	}
}

func TestGrammarPreviewAcceptAndCancelStillWork(t *testing.T) {
	m := grammarModel(t, 100, 30, 50)
	m.grammarQuoteSuffix = ""
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if got := next.(Model); got.overlay != overlayCompose {
		t.Fatalf("n should cancel back to compose, overlay=%v", got.overlay)
	}
}
