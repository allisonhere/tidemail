package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// Benchmarks for the message-body render path, which runs synchronously inside
// Update on every j/k and every Tab. Sizes come from the real database: Gmail
// bodies average 21KB of HTML with a 333KB worst case, and its INBOX threads
// run up to 10 messages.

// benchHTMLBody builds marketing-shaped HTML of roughly the given size — the
// nested tables, inline styles and tracking links that make a real newsletter
// expensive to convert.
func benchHTMLBody(approxBytes int) string {
	var b strings.Builder
	b.WriteString(`<html><body style="margin:0;padding:0"><table width="100%"><tr><td>`)
	i := 0
	for b.Len() < approxBytes {
		fmt.Fprintf(&b, `<table cellpadding="8" style="border-collapse:collapse;font-family:Helvetica">
<tr><td style="color:#333;font-size:14px"><h2>Section %d</h2>
<p>Paragraph %d with <b>bold</b>, <i>italic</i> and a
<a href="https://example.com/track?id=%d&amp;utm_source=news">tracked link</a>.</p>
<ul><li>First item %d</li><li>Second item %d</li></ul>
<p><img src="https://example.com/img/%d.png" alt="promo %d" width="600"></p>
</td></tr></table>`, i, i, i, i, i, i, i)
		i++
	}
	b.WriteString(`</td></tr></table></body></html>`)
	return b.String()
}

func benchMessage(id int64, html string) db.Message {
	return db.Message{
		ID:        id,
		MailboxID: 1,
		UID:       uint32(id),
		MessageID: fmt.Sprintf("<bench-%d@example.com>", id),
		From:      "Sender Name <sender@example.com>",
		Subject:   fmt.Sprintf("Benchmark message %d", id),
		Date:      time.Now(),
		BodyHTML:  html,
		BodyText:  "plain text fallback",
	}
}

// benchModel builds a Model sized like a real terminal, with the display
// settings from the reported slow setup: threaded conversations and full
// headers on.
func benchModel(b *testing.B) Model {
	b.Helper()
	b.Setenv("XDG_DATA_HOME", b.TempDir())
	b.Setenv("XDG_CONFIG_HOME", b.TempDir())

	database, err := db.Open()
	if err != nil {
		b.Fatalf("Open DB: %v", err)
	}
	b.Cleanup(func() { database.Close() })

	cfg := config.DefaultConfig()
	cfg.Display.ThreadedConversations = true
	cfg.Display.ShowHeaders = true

	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(Model)
}

func BenchmarkRenderMessageForDisplay(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{
		{"small_1KB", 1024},
		{"typical_21KB", 21 * 1024},
		{"worst_333KB", 333 * 1024},
	} {
		b.Run(tc.name, func(b *testing.B) {
			m := benchModel(b)
			msg := benchMessage(1, benchHTMLBody(tc.size))
			width := m.contentBodyWidth()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if r := m.renderMessageForDisplay(msg, width); !r.ok {
					b.Fatal("render reported not ok")
				}
			}
		})
	}
}

// BenchmarkSetViewportThread measures one keystroke's worth of work: what
// j/k and Tab pay through setViewportForCurrentRow.
func BenchmarkSetViewportThread(b *testing.B) {
	for _, size := range []int{1, 10} {
		b.Run(fmt.Sprintf("%d_messages", size), func(b *testing.B) {
			m := benchModel(b)
			html := benchHTMLBody(21 * 1024)
			thread := messageThread{Key: "t", Count: size}
			for i := 0; i < size; i++ {
				thread.Messages = append(thread.Messages, benchMessage(int64(i+1), html))
			}
			thread.Representative = thread.Messages[0]

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.setViewportThread(thread)
			}
		})
	}
}
