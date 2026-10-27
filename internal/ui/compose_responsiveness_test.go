package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestEditorUnchangedSizeDoesNotReflowAfterInput(t *testing.T) {
	e := newEditorArea()
	e.SetSize(20, 5)
	e.SetValue(strings.Repeat("a long reply paragraph ", 100))
	e.Focus()
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'x'}}, {Type: tea.KeyBackspace},
		{Type: tea.KeyLeft}, {Type: tea.KeyDelete},
	} {
		e, _ = e.Update(key)
		before, cursor, top := e.Value(), e.ed.CursorIndex(), e.ed.ViewportTop()
		// Ripple's reflow allocates visual-line slices. Unchanged dimensions
		// must avoid that document-wide work even immediately after editing.
		if allocs := testing.AllocsPerRun(100, func() { e.SetSize(20, 5) }); allocs != 0 {
			t.Fatalf("same-size layout after %s allocated %v times", key.String(), allocs)
		}
		if e.Value() != before || e.ed.CursorIndex() != cursor || e.ed.ViewportTop() != top {
			t.Fatal("same-size layout changed editor state")
		}
	}
}

func TestEditorResizesWithoutChangingBody(t *testing.T) {
	e := newEditorArea()
	e.SetValue("abcdefghi\n\n> quoted reply\nhttps://example.com/long/unbroken/path")
	body := e.Value()
	for _, width := range []int{20, 4, 12, 40} {
		e.SetSize(width, 30)
		e.Blur()
		view := ansi.Strip(e.View())
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d: overflowing line %q", width, line)
			}
		}
		if e.Value() != body {
			t.Fatal("resizing inserted hard newlines")
		}
		prefix := "abcdefghi\n\n"
		if width == 4 {
			prefix = "abcd\nefgh\ni\n\n"
		}
		if !strings.HasPrefix(view, prefix) {
			t.Fatalf("paragraph layout at width %d: %q", width, view)
		}
	}
}

func TestEditorMovementAndDeletionAcrossVisualBoundary(t *testing.T) {
	e := newEditorArea()
	e.SetSize(5, 6)
	e.SetValue("abcdefghij\n\n> reply")
	e.Focus()
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := e.ed.CursorIndex(); got != 5 {
		t.Fatalf("down across wrap = %d, want 5", got)
	}
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := e.Value(); got != "abcdfghij\n\n> reply" {
		t.Fatalf("backspace = %q", got)
	}
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyDelete})
	if got := e.Value(); got != "abcdghij\n\n> reply" {
		t.Fatalf("delete = %q", got)
	}
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	for range 6 {
		e, _ = e.Update(tea.KeyMsg{Type: tea.KeyShiftRight})
	}
	if got := e.SelectedText(); got != "abcdgh" {
		t.Fatalf("selection across wrap = %q", got)
	}
}

func TestComposeVisualWrappingPreservesDraftAndSendBody(t *testing.T) {
	acfg := config.AccountConfig{ID: "test", User: "me@example.com"}
	c := NewCompose(acfg, []config.AccountConfig{acfg}, nil)
	body := strings.Repeat("A paragraph with words and hyperlinks https://example.com/path ", 5) + "\n\n> quoted reply\n> second quoted line"
	c.toInput.SetValue("you@example.com")
	c.bodyInput.SetValue(body)
	styles := BuildStyles(CatppuccinMocha, "compact", "square")
	for _, width := range []int{40, 80, 60} {
		c.bodyInput.SetSize(width-4, 20)
		_ = c.View(width, 30, styles)
		if got := c.toDraftRecord().BodyText; got != body {
			t.Fatalf("draft body changed at width %d", width)
		}
	}
	_, cmd, _ := c.send()
	if cmd == nil {
		t.Fatal("send did not queue message")
	}
	queued := cmd().(SendQueuedMsg)
	if queued.Msg.Body != body {
		t.Fatalf("send body changed: %q", queued.Msg.Body)
	}
}
