package ui

import (
	"fmt"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// Warm the body and layout before measuring actual link-navigation Update +
// View work. Each large fixture has enough anchors to visit uncached selections.
func BenchmarkReaderLinkNavigation(b *testing.B) {
	for _, size := range []int{21 * 1024, 333 * 1024} {
		b.Run(fmt.Sprintf("%dKB", size/1024), func(b *testing.B) {
			b.Setenv("TIDEMAIL_IMAGE_PROTOCOL", "none")
			m := benchModel(b)
			m.cfg.Display.ActionableLinks = true
			m.cfg.Display.ThreadedConversations = false
			msg := benchMessage(1, benchHTMLBody(size))
			m.messages = []db.Message{msg}
			m.filteredMessages = m.messages
			m.focused = paneContent
			m.setViewportForCurrentRow()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
				m = next.(Model)
				_ = m.View()
			}
		})
	}
}
