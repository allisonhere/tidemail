package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func BenchmarkComposeEditorSize(b *testing.B) {
	e := newEditorArea()
	e.SetSize(68, 20)
	e.SetValue(strings.Repeat("A paragraph of words to edit and wrap naturally. ", 1000))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e.SetSize(68, 20)
	}
}

func BenchmarkComposeInput(b *testing.B) {
	for _, size := range []struct {
		name    string
		repeats int
	}{{"short", 10}, {"long", 1000}} {
		b.Run(size.name, func(b *testing.B) {
			acfg := config.AccountConfig{ID: "test", User: "test@example.com"}
			m := NewModel(nil, config.Config{Accounts: []config.AccountConfig{acfg}, Display: config.DefaultConfig().Display}, "", false)
			m.width, m.height = 80, 40
			m.overlay = overlayCompose
			m.compose = NewCompose(acfg, m.cfg.Accounts, nil)
			m.compose.focusedField = composeFieldBody
			m.compose.bodyInput.Focus()
			m.compose.bodyInput.SetValue(strings.Repeat("A paragraph of words to edit and wrap naturally. ", size.repeats))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune{'x'}}, {Type: tea.KeyBackspace}, {Type: tea.KeyLeft}, {Type: tea.KeyRight}} {
					next, _ := m.handleCompose(key)
					m = next.(Model)
					_ = m.compose.View(composeOverlayWidth(m.width), min(m.height-4, 36), m.styles)
				}
			}
		})
	}
}
