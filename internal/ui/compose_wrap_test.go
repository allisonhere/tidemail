package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// These are the recovered prototype's basic word/long-token expectations;
// Ripple owns the comprehensive wrapping algorithm tests.
func TestComposeWordWrapAcrossConstructors(t *testing.T) {
	preserveComposeEditorState(t)
	editorVimMode = false
	account := config.AccountConfig{User: "me@example.com"}
	message := db.Message{From: "source@example.com", BodyText: "quoted original"}
	models := map[string]ComposeModel{
		"new":       NewCompose(account, nil, nil),
		"reply":     NewReply(message, account, nil, nil),
		"reply-all": NewReplyAll(message, account, nil, nil),
		"forward":   NewForward(message, account, nil, nil),
		"draft":     NewComposeFromDraft(db.Draft{BodyText: "saved original"}, nil, nil),
	}
	for name, c := range models {
		t.Run(name, func(t *testing.T) {
			e := &c.bodyInput
			e.SetSize(10, 8)
			e.Blur()
			e.SetValue("hello world again")
			rows := strings.Split(ansi.Strip(e.View()), "\n")
			for i, want := range []string{"hello", "world", "again"} {
				if strings.TrimSpace(rows[i]) != want {
					t.Fatalf("row %d = %q, want %q", i, rows[i], want)
				}
			}
			e.SetValue("abcdefghijkl")
			e.SetWidth(5)
			rows = strings.Split(ansi.Strip(e.View()), "\n")
			if strings.TrimSpace(rows[0]) != "abcde" || strings.TrimSpace(rows[1]) != "fghij" {
				t.Fatalf("long-token fallback: %q", rows)
			}
		})
	}
	e := newEditorArea() // Template and other editors retain Ripple's default.
	e.SetSize(10, 8)
	e.Blur()
	e.SetValue("hello world again")
	if row := strings.Split(ansi.Strip(e.View()), "\n")[0]; strings.TrimSpace(row) != "hello worl" {
		t.Fatalf("shared editor changed: %q", row)
	}
}

func TestComposeWordWrapNavigationAndEditing(t *testing.T) {
	preserveComposeEditorState(t)
	editorVimMode = false
	c := NewCompose(config.AccountConfig{}, nil, nil)
	e := &c.bodyInput
	e.SetSize(10, 8)
	e.SetValue("hello world again")
	press := func(key tea.KeyType) { *e, _ = e.Update(tea.KeyMsg{Type: key}) }
	press(tea.KeyCtrlHome)
	press(tea.KeyDown)
	if got := e.ed.CursorIndex(); got != 6 {
		t.Fatalf("down across word wrap = %d, want 6", got)
	}
	press(tea.KeyShiftRight)
	if e.SelectedText() != "w" {
		t.Fatalf("selection = %q", e.SelectedText())
	}
	*e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})
	if e.Value() != "hello World again" {
		t.Fatal(e.Value())
	}
	press(tea.KeyCtrlZ)
	if e.Value() != "hello world again" {
		t.Fatal("undo failed")
	}
	press(tea.KeyCtrlY)
	if e.Value() != "hello World again" {
		t.Fatal("redo failed")
	}
	press(tea.KeyEnd)
	if got := e.ed.CursorIndex(); got != len(e.Value()) {
		t.Fatalf("End = %d, want logical line end", got)
	}
	press(tea.KeyHome)
	if e.ed.CursorIndex() != 0 {
		t.Fatal("Home did not reach logical line start")
	}
	e.SetWidth(20)
	press(tea.KeyCtrlHome)
	press(tea.KeyEnd)
	if got := e.ed.CursorIndex(); got != len(e.Value()) {
		t.Fatalf("End after reflow = %d", got)
	}
}

func TestComposeWordWrapReflowPreservesDraft(t *testing.T) {
	preserveComposeEditorState(t)
	editorVimMode = false
	m := composeEventModel(120, 50, false)
	body := "Leading  spaces and trailing spaces  \n\n⁂\n\n---------- Forwarded message ----------\nFrom: alice@example.com\n> " + strings.Repeat("quoted words ", 30) + "\n\nhttps://example.com/" + strings.Repeat("path", 60) + "\n"
	m.compose = NewComposeFromDraft(db.Draft{BodyText: body}, m.cfg.Accounts, nil)
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.Focus()
	m.sizeCompose()
	for _, msg := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true},
		tea.WindowSizeMsg{Width: 70, Height: 30},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true},
		tea.WindowSizeMsg{Width: 140, Height: 55},
	} {
		cursor := m.compose.bodyInput.ed.CursorIndex()
		next, _ := m.Update(msg)
		m = next.(Model)
		w, h := m.compose.overlayDimensions(m.width, m.height)
		_, bw, bh, _ := m.compose.composeLayout(w, h, m.styles)
		e := &m.compose.bodyInput
		if e.w != bw || e.h != bh {
			t.Fatal("editor geometry differs from compose layout")
		}
		if e.Value() != body || m.compose.toDraftRecord().BodyText != body {
			t.Fatal("reflow changed draft text")
		}
		if e.ed.CursorIndex() != cursor {
			t.Fatal("reflow moved caret")
		}
		for _, row := range strings.Split(e.View(), "\n") {
			if ansi.StringWidth(row) > bw {
				t.Fatal("editor exceeds body width")
			}
		}
		_ = m.View()
	}
}

func TestComposeWordWrapSearchAndSpellRendering(t *testing.T) {
	preserveComposeEditorState(t)
	editorVimMode = false
	c := spellCompose(t, "hello recieve again")
	e := &c.bodyInput
	e.SetSize(10, 8)
	e.Blur()
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	var styled strings.Builder
	view := e.View()
	for _, match := range regexp.MustCompile(`\x1b\[4;[0-9;]*m(.)\x1b\[0m`).FindAllStringSubmatch(view, -1) {
		styled.WriteString(match[1])
	}
	if styled.String() != "recieve" {
		t.Fatalf("spell underline lost across wrap: %q, %q", styled.String(), view)
	}
	// Isolate search styling from the spell underline.
	e.TypoStyle = lipgloss.NewStyle()
	e.ed.OpenSearch()
	e.ed.SetSearchQuery("recieve")
	if len(e.ed.Search().Matches) != 1 {
		t.Fatal("find lost wrapped word")
	}
	var highlighted strings.Builder
	for _, match := range regexp.MustCompile(`\x1b\[7m([^\x1b]*)\x1b\[0m`).FindAllStringSubmatch(e.View(), -1) {
		highlighted.WriteString(match[1])
	}
	if highlighted.String() != "recieve" {
		t.Fatalf("search highlight = %q; view = %q", highlighted.String(), e.View())
	}
	e.ed.CloseSearch()
	if e.Value() != "hello recieve again" {
		t.Fatal("render/search changed text")
	}
}
