package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/charmbracelet/x/ansi"
)

// A very narrow pane must not panic (the divider count used to go negative) and
// a long filename must be truncated instead of overflowing the pane width.
func TestRenderAttachmentListNarrowWidth(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.contentAttachments = []db.Attachment{
		{Filename: "a-really-long-attachment-filename-that-would-overflow.pdf", ContentType: "application/pdf", Size: 1234567},
	}

	// Tiny widths must not panic (the divider repeat count used to go negative).
	for _, width := range []int{1, 2, 4, 8, 12} {
		_ = m.renderAttachmentList(width)
	}

	// At realistic widths the long filename must be truncated so no line
	// overflows. The block is indented one column by indentBlock, so the bound
	// is width+1.
	for _, width := range []int{24, 40, 100} {
		out := m.renderAttachmentList(width)
		for _, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w > width+1 {
				t.Fatalf("width %d: rendered line exceeds pane (%d): %q", width, w, ansi.Strip(line))
			}
		}
	}
}
