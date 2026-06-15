package ui

import (
	"github.com/allisonhere/tide/internal/clipboard"
	"github.com/allisonhere/tide/internal/editor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// systemClipboard adapts internal/clipboard to editor.Clipboard so the editor
// owns copy/cut/paste without importing the host's clipboard package directly.
type systemClipboard struct{}

func (systemClipboard) Read() (string, error)   { return clipboard.Read() }
func (systemClipboard) Write(text string) error { return clipboard.Copy(text) }

// editorClipboard is the clipboard wired into every compose editor. It is a
// package var so tests can substitute an in-memory fake.
var editorClipboard editor.Clipboard = systemClipboard{}

// editorArea adapts editor.Model to the subset of the Bubble Tea textarea API
// that compose relies on (Value/SetValue/Focus/Blur/SetWidth/SetHeight/Update/
// View), so swapping the textarea for the owned editor is a minimal diff.
//
// Copy/cut/paste are owned by the editor itself (wired to the system clipboard
// via SetClipboard in newEditorArea); the host no longer intercepts those keys.
type editorArea struct {
	ed   editor.Model
	w, h int

	// Styling, configurable by the consumer (e.g. from the active theme).
	CursorStyle      lipgloss.Style
	SelectedStyle    lipgloss.Style
	PlaceholderStyle lipgloss.Style
}

func newEditorArea() editorArea {
	ed := editor.New()
	ed.SetClipboard(editorClipboard)
	return editorArea{
		ed:               ed,
		CursorStyle:      lipgloss.NewStyle().Reverse(true),
		SelectedStyle:    lipgloss.NewStyle().Reverse(true),
		PlaceholderStyle: lipgloss.NewStyle().Faint(true),
	}
}

func (e editorArea) Value() string { return e.ed.Value() }

func (e *editorArea) SetValue(s string) { e.ed.SetValue(s) }

func (e *editorArea) SetPlaceholder(s string) { e.ed.SetPlaceholder(s) }

func (e *editorArea) Focus() { e.ed.Focus() }

func (e *editorArea) Blur() { e.ed.Blur() }

// SetWidth and SetHeight mirror the textarea's separate sizing calls; the
// editor takes both at once, so each remembers its dimension and re-applies.
func (e *editorArea) SetWidth(w int)  { e.w = w; e.ed.SetSize(e.w, e.h) }
func (e *editorArea) SetHeight(h int) { e.h = h; e.ed.SetSize(e.w, e.h) }

// SetSize sets width and height together so the editor's viewport is computed
// once against both real dimensions. Sizing via SetWidth then SetHeight would
// briefly clamp the still-unset dimension to 1 — and a height-1 pass pins the
// viewport to the cursor row, which sticks the caret to the top of the body.
func (e *editorArea) SetSize(w, h int) { e.w, e.h = w, h; e.ed.SetSize(w, h) }

func (e editorArea) SelectedText() string { return e.ed.SelectedText() }

// Update forwards every message to the editor and propagates the command it
// returns (e.g. a copy/cut clipboard write, or the read that produces an
// editor.PasteMsg), so clipboard side effects ride back with the model.
func (e editorArea) Update(msg tea.Msg) (editorArea, tea.Cmd) {
	var cmd tea.Cmd
	e.ed, cmd = e.ed.Update(msg)
	return e, cmd
}

func (e editorArea) View() string {
	return e.ed.View(editor.Options{
		Cursor:      e.CursorStyle.Render(" "),
		Selected:    func(s string) string { return e.SelectedStyle.Render(s) },
		Placeholder: func(s string) string { return e.PlaceholderStyle.Render(s) },
	})
}
