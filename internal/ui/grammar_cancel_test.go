package ui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestGrammarCancelKeepsComposeAndRejectsLateResults(t *testing.T) {
	m := draftSafetyModel(t)
	m.compose.bodyInput.SetValue("original")
	ctx, cancel := context.WithCancel(context.Background())
	request := &grammarRequest{cancel: cancel}
	m.compose.grammarRequest = request
	m.compose.busy = true
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if ctx.Err() == nil || m.compose.busy || m.overlay != overlayCompose || m.compose.bodyInput.Value() != "original" {
		t.Fatal("cancel did not preserve editable compose")
	}
	next, _ = m.Update(GrammarCheckedMsg{Request: request, Corrected: "late"})
	m = next.(Model)
	if m.overlay != overlayCompose || m.compose.bodyInput.Value() != "original" {
		t.Fatal("late grammar response changed compose")
	}
}

func TestGrammarCancelDoesNotUnlockPersistence(t *testing.T) {
	m := draftSafetyModel(t)
	m.compose.busy = true
	m.compose.closeAfterSave = true
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if !m.compose.busy || !m.compose.closeAfterSave {
		t.Fatal("Esc bypassed persistence protection")
	}
}
