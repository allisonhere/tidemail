package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/mailtemplate"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestComposeMaximize(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(120, 50, false)
	m.compose.bodyInput.SetValue("hello prototype\nsecond line")
	m.compose.bodyInput, _ = m.compose.bodyInput.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m.compose.bodyInput, _ = m.compose.bodyInput.Update(tea.KeyMsg{Type: tea.KeyShiftLeft})
	m.compose.draftID = 42
	before := m.compose
	cursor := before.bodyInput.ed.CursorIndex()
	selection := before.bodyInput.SelectedText()
	if selection == "" {
		t.Fatal("selection fixture is empty")
	}
	toggle := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true}
	for _, maximized := range []bool{true, false} {
		next, cmd := m.Update(toggle)
		m = next.(Model)
		if cmd != nil {
			t.Fatal("toggle produced a command")
		}
		if m.compose.maximized != maximized {
			t.Fatal("toggle state")
		}
		if composeChanged(before, m.compose) || m.compose.dirty != before.dirty || m.compose.draftID != 42 || m.compose.focusedField != before.focusedField || m.compose.bodyInput.ed.CursorIndex() != cursor || m.compose.bodyInput.SelectedText() != selection {
			t.Fatal("toggle changed compose state")
		}
		w, h := m.compose.overlayDimensions(m.width, m.height)
		if maximized && (w != 118 || h != 48) {
			t.Fatalf("max dimensions %d %d", w, h)
		}
		if !maximized && (w != 110 || h != 46) {
			t.Fatalf("compact dimensions %d %d", w, h)
		}
		_, bw, bh, _ := m.compose.composeLayout(w, h, m.styles)
		if m.compose.bodyInput.w != bw || m.compose.bodyInput.h != bh {
			t.Fatal("editor sizing mismatch")
		}
		hint := "Maximize"
		if maximized {
			hint = "Restore"
		}
		if !strings.Contains(strings.Join(strings.Fields(ansi.Strip(m.View())), " "), hint) {
			t.Fatalf("missing footer hint %q:\n%s", hint, ansi.Strip(m.View()))
		}
		if maximized && m.composeBackdrop.valid {
			t.Fatal("maximized view rendered backdrop")
		}
	}
	next, _ := m.Update(toggle)
	m = next.(Model)
	next, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(Model)
	w, h := m.compose.overlayDimensions(m.width, m.height)
	_, bw, bh, _ := m.compose.composeLayout(w, h, m.styles)
	if w != 98 || h != 38 || m.compose.bodyInput.w != bw || m.compose.bodyInput.h != bh {
		t.Fatal("resize mismatch")
	}
	next, _ = m.Update(toggle)
	m = next.(Model)
	next, _ = m.Update(tea.WindowSizeMsg{Width: 90, Height: 35})
	m = next.(Model)
	w, h = m.compose.overlayDimensions(m.width, m.height)
	_, bw, bh, _ = m.compose.composeLayout(w, h, m.styles)
	if w != 86 || h != 31 || m.compose.bodyInput.w != bw || m.compose.bodyInput.h != bh {
		t.Fatal("compact resize mismatch")
	}
	acfg := m.compose.accountCfg
	for _, c := range []ComposeModel{
		NewCompose(acfg, nil, nil),
		NewReply(db.Message{}, acfg, nil, nil),
		NewReplyAll(db.Message{}, acfg, nil, nil),
		NewForward(db.Message{}, acfg, nil, nil),
		NewComposeFromDraft(db.Draft{}, nil, nil),
	} {
		if c.maximized {
			t.Fatal("new composer must start compact")
		}
	}
}

// Maximized compose owns every row and never renders the hidden panes or the
// application footer. The composer footer remains inside the panel on resize.
func TestComposeMaximizeFooterAndResize(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(120, 50, false)
	m.compose.maximized = true
	for _, size := range [][2]int{{120, 50}, {80, 24}, {140, 55}, {60, 20}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		m.setStatus("background application status", true)
		view := ansi.Strip(m.View())
		rows := strings.Split(view, "\n")
		if len(rows) != m.height || strings.Contains(view, "background application status") {
			t.Fatalf("application footer visible or wrong height at %v:\n%s", size, view)
		}
		if !strings.Contains(view, "Restore") || !strings.Contains(view, "Send") || !strings.Contains(view, "Cancel") {
			t.Fatalf("missing composer footer at %v:\n%s", size, view)
		}
		if !strings.HasPrefix(rows[len(rows)-1], "╰") {
			t.Fatal("panel does not occupy bottom row")
		}
		if m.composeBackdrop.valid {
			t.Fatal("maximized view rendered underlying panes")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != m.width {
				t.Fatalf("row width %d, want %d", ansi.StringWidth(row), m.width)
			}
		}
	}
}

func TestComposeConditionalStatusRouting(t *testing.T) {
	preserveComposeEditorState(t)
	for _, maximized := range []bool{false, true} {
		for _, tc := range []struct {
			name  string
			msg   tea.Msg
			text  string
			isErr bool
		}{
			{"autosave", DraftSavedMsg{Err: errors.New("disk full")}, "draft save failed: disk full", true},
			{"copy error", ClipboardCopiedMsg{Err: errors.New("unavailable")}, "copy failed: unavailable", true},
			{"copy", ClipboardCopiedMsg{}, "copied to clipboard", false},
			{"paste error", ClipboardReadMsg{Err: errors.New("unavailable")}, "paste failed: unavailable", true},
			{"paste", ClipboardReadMsg{Text: "pasted text"}, "pasted 11 chars", false},
		} {
			t.Run(fmt.Sprintf("%s/max=%v", tc.name, maximized), func(t *testing.T) {
				m := composeEventModel(120, 50, false)
				m.compose.maximized = maximized
				m.compose.statusMsg = "native composer status"
				next, _ := m.Update(tc.msg)
				m = next.(Model)
				if maximized {
					if m.compose.statusMsg != tc.text || m.compose.isErr != tc.isErr || m.statusMsg != "" {
						t.Fatalf("incorrect routed status: compose=%q app=%q", m.compose.statusMsg, m.statusMsg)
					}
				} else if m.statusMsg != tc.text || m.statusErr != tc.isErr || m.compose.statusMsg != "native composer status" {
					t.Fatalf("compact routing changed: compose=%q app=%q", m.compose.statusMsg, m.statusMsg)
				}
				if !strings.Contains(ansi.Strip(m.View()), tc.text) {
					t.Fatal("notification not visible")
				}
				next, _ = m.Update(StatusClearMsg{})
				m = next.(Model)
				if m.statusMsg != "" || (maximized && m.compose.statusMsg != "") {
					t.Fatal("transient message did not clear")
				}
				if !maximized && m.compose.statusMsg != "native composer status" {
					t.Fatal("application clear erased native status")
				}
			})
		}
	}
}

func TestComposeRoutedStatusModeSwitchAndNativeMessages(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(120, 50, false)
	m.compose.maximized = true
	next, _ := m.Update(DraftSavedMsg{Err: errors.New("disk full")})
	m = next.(Model)
	// Unrelated activity must neither enter compose nor replace its error.
	m.setStatus("background sync complete", false)
	if m.compose.statusMsg != "draft save failed: disk full" {
		t.Fatal("background status replaced compose error")
	}
	for _, maximized := range []bool{false, true} {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true})
		m = next.(Model)
		if m.compose.maximized != maximized || strings.Contains(ansi.Strip(m.View()), "disk full") || m.compose.statusMsg != "" {
			t.Fatal("routed message lingered across mode switch")
		}
	}
	m.setComposeStatus("copy failed", true)
	// A native send validation message owns the status area after it replaces
	// the routed notice; clearing application status must leave it alone.
	m.compose.statusMsg, m.compose.isErr = "TO is required", true
	next, _ = m.Update(StatusClearMsg{})
	m = next.(Model)
	if m.compose.statusMsg != "TO is required" || !m.compose.isErr {
		t.Fatal("native error cleared")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true})
	m = next.(Model)
	if m.compose.statusMsg != "TO is required" || !m.compose.isErr {
		t.Fatal("mode switch cleared native error")
	}
	// Compact notifications are never imported when maximizing later.
	m.compose.statusMsg, m.compose.isErr = "", false
	m.setComposeStatus("compact-only notification", false)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true})
	m = next.(Model)
	if strings.Contains(ansi.Strip(m.View()), "compact-only notification") {
		t.Fatal("old compact status imported")
	}
}

func TestComposeMaximizeHeaderStateAndHints(t *testing.T) {
	preserveComposeEditorState(t)
	for field := composeFieldFrom; field < composeFieldCount; field++ {
		m := composeEventModel(120, 50, false)
		m.compose.focusedField = field
		m.compose.toInput.SetValue(longRecipients)
		m.compose.ccInput.SetValue("cc@example.com")
		m.compose.bccInput.SetValue("bcc@example.com")
		m.compose.subjectInput.SetValue("subject")
		m.compose.toInput.SetCursor(13)
		m.compose.attachments = []attachmentFile{{Name: "file.txt", Data: []byte("attachment")}}
		m.sizeCompose()
		before := m.compose
		for _, maximized := range []bool{true, false} {
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true})
			m = next.(Model)
			if cmd != nil || composeChanged(before, m.compose) || m.compose.toInput.Position() != 13 || m.compose.focusedField != field || m.compose.dirty != before.dirty {
				t.Fatalf("toggle disturbed field %v", field)
			}
			label := "Maximize"
			if maximized {
				label = "Restore"
			}
			if !strings.Contains(ansi.Strip(m.View()), label) {
				t.Fatalf("field %v missing %s", field, label)
			}
		}
	}
}

func TestComposeMaximizeNavigation(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(100, 30, false)
	m.compose.bodyInput.SetValue(strings.Repeat("short line\n", 200))
	m.sizeCompose()
	// Start away from the document edge, where resizing naturally clamps the last page.
	for i := 0; i < 40; i++ {
		m.compose.bodyInput, _ = m.compose.bodyInput.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	cursor := m.compose.bodyInput.ed.CursorIndex()
	top := m.compose.bodyInput.ed.ViewportTop()
	for _, size := range [][2]int{{100, 30}, {120, 45}, {80, 24}} {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true})
		m = next.(Model)
		if m.compose.bodyInput.ed.CursorIndex() != cursor || m.compose.bodyInput.ed.ViewportTop() != top {
			t.Fatalf("toggle moved cursor %d -> %d or viewport %d -> %d at %v", cursor, m.compose.bodyInput.ed.CursorIndex(), top, m.compose.bodyInput.ed.ViewportTop(), size)
		}
		next, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		for i := 0; i < 20; i++ {
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
			m = next.(Model)
		}
		if m.compose.bodyInput.ed.CursorIndex() >= cursor {
			t.Fatal("up did not navigate")
		}
		cursor = m.compose.bodyInput.ed.CursorIndex()
		top = m.compose.bodyInput.ed.ViewportTop()
		w, h := m.compose.overlayDimensions(m.width, m.height)
		_, bw, bh, _ := m.compose.composeLayout(w, h, m.styles)
		if m.compose.bodyInput.w != bw || m.compose.bodyInput.h != bh {
			t.Fatal("stored geometry differs")
		}
	}
}

func TestComposeStaticCursorAndBackdrop(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(100, 40, false)
	for _, input := range []*textinput.Model{&m.compose.toInput, &m.compose.ccInput, &m.compose.bccInput, &m.compose.subjectInput} {
		if input.Cursor.Mode() != cursor.CursorStatic {
			t.Fatal("header cursor is not static")
		}
		if cmd := input.Focus(); cmd != nil {
			t.Fatal("static focus scheduled blinking")
		}
		if input.Cursor.Blink {
			t.Fatal("focused cursor hidden")
		}
		input.Blur()
		if !input.Cursor.Blink {
			t.Fatal("unfocused cursor visible")
		}
	}
	m.compose.focusedField = composeFieldSubject
	m.compose.subjectInput.Focus()
	m.compose.subjectInput.SetValue(strings.Repeat("x", 200))
	m.sizeCompose()
	_ = m.View()
	original := m.composeBackdrop.panes
	for _, msg := range []tea.Msg{cursor.Blink(), cursor.BlinkMsg{}} {
		next, _ := m.Update(msg)
		m = next.(Model)
		if !m.composeBackdrop.valid {
			t.Fatal("cursor event invalidated backdrop")
		}
	}
	// A repeat burst must consume every key without creating blink commands.
	for i := 0; i < 100; i++ {
		c, cmd, _ := m.compose.Update(tea.KeyMsg{Type: tea.KeyBackspace}, m.keys)
		m.compose = c
		if cmd != nil {
			t.Fatal("backspace scheduled a cursor command")
		}
	}
	if len(m.compose.subjectInput.Value()) != 100 {
		t.Fatal("backspace burst lost input")
	}
	if !m.composeBackdrop.valid || m.composeBackdrop.panes != original {
		t.Fatal("editing rebuilt backdrop")
	}
}

func TestComposeRoutedStatusInSubpanels(t *testing.T) {
	preserveComposeEditorState(t)
	for _, panel := range []string{"search", "replace", "spell", "template", "picker"} {
		for _, failure := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/error=%v", panel, failure), func(t *testing.T) {
				m := composeEventModel(120, 50, false)
				m.compose.maximized = true
				switch panel {
				case "search", "replace":
					m.compose.bodyInput.ed.OpenSearch()
					if panel == "replace" {
						m.compose.bodyInput, _ = m.compose.bodyInput.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
					}
				case "spell":
					m.compose.spellOpen = true
				case "template":
					m.compose.tmplOpen = true
					m.compose.tmplList = []mailtemplate.Template{{Name: "test", Body: "template body"}}
					m.compose.tmplEd = newEditorArea()
				case "picker":
					m.compose.picker.active = true
				}
				m.sizeCompose()
				msg := tea.Msg(ClipboardCopiedMsg{})
				want := "copied to clipboard"
				if failure {
					msg = DraftSavedMsg{Err: errors.New("disk full")}
					want = "draft save failed: disk full"
				}
				next, _ := m.Update(msg)
				m = next.(Model)
				if !strings.Contains(ansi.Strip(m.View()), want) {
					t.Fatalf("notification hidden in %s:\n%s", panel, ansi.Strip(m.View()))
				}
				next, _ = m.Update(StatusClearMsg{})
				m = next.(Model)
				if strings.Contains(ansi.Strip(m.View()), want) {
					t.Fatal("notification did not clear")
				}
			})
		}
	}
}

func TestComposeAutosaveErrorWhileSearchHasError(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(120, 50, false)
	m.compose.maximized = true
	previousRegex := editorSearchRegex
	t.Cleanup(func() { setEditorSearchRegex(previousRegex) })
	setEditorSearchRegex(true)
	m.compose.bodyInput.ed.SetRegexSearchEnabled(true)
	m.compose.bodyInput.ed.OpenSearch()
	m.compose.bodyInput.ed.SetSearchRegex(true)
	m.compose.bodyInput.ed.SetSearchQuery("[")
	if m.compose.bodyInput.ed.Search().Error == "" {
		t.Fatal("missing regex error fixture")
	}
	next, _ := m.Update(DraftSavedMsg{Err: errors.New("disk full")})
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.View()), "draft save failed: disk full") {
		t.Fatal("search error hid autosave failure")
	}
	next, _ = m.Update(StatusClearMsg{})
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.View()), "Invalid regex") {
		t.Fatal("search error not restored after transient error cleared")
	}
}
