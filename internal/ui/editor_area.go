package ui

import (
	"github.com/allisonhere/tide/internal/editor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// editorArea adapts editor.Model to the subset of the Bubble Tea textarea API
// that compose relies on (Value/SetValue/Focus/Blur/SetWidth/SetHeight/Update/
// View), so swapping the textarea for the owned editor is a minimal diff.
//
// It also surfaces SelectedText/DeleteSelection, which the textarea lacks, so
// compose can add copy/cut against the system clipboard (internal/clipboard).
//
// Not yet wired into compose — compose stays on the textarea until the editor
// is validated in cmd/editortest.
type editorArea struct {
	ed   editor.Model
	w, h int

	// Styling, configurable by the consumer (e.g. from the active theme).
	CursorStyle      lipgloss.Style
	SelectedStyle    lipgloss.Style
	PlaceholderStyle lipgloss.Style
}

func newEditorArea() editorArea {
	return editorArea{
		ed:               editor.New(),
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

func (e editorArea) SelectedText() string { return e.ed.SelectedText() }

func (e *editorArea) DeleteSelection() { e.ed.DeleteSelection() }

// Update mirrors textarea.Model.Update: it applies a key message and returns
// the updated value (non-key messages are ignored). The returned command is
// always nil today; the signature matches the textarea for a drop-in swap.
func (e editorArea) Update(msg tea.Msg) (editorArea, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		e.ed.UpdateKey(k)
	}
	return e, nil
}

func (e editorArea) View() string {
	return e.ed.View(editor.Options{
		Cursor:      e.CursorStyle.Render(" "),
		Selected:    func(s string) string { return e.SelectedStyle.Render(s) },
		Placeholder: func(s string) string { return e.PlaceholderStyle.Render(s) },
	})
}
