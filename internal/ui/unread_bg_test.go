package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestUnreadBackgroundIsDifferent(t *testing.T) {
	// Lipgloss only emits background when Width is set
	s1 := lipgloss.NewStyle().Background(lipgloss.Color("#ff0000")).Width(20).Render("test")
	t.Logf("Width(20): %q [has bg: %v]", s1[:min(60, len(s1))], strings.Contains(s1, "48;"))

	s2 := lipgloss.NewStyle().Background(lipgloss.Color("#ff0000")).Width(20).Padding(0, 1).Render("test")
	t.Logf("Width+Padding: %q [has bg: %v]", s2[:min(80, len(s2))], strings.Contains(s2, "48;"))

	// Now check ArticleUnread rendering in context — how it's used in the message list
	styles := BuildStyles(LavenderFieldsForever, "compact")
	unread := styles.ArticleUnread.Width(40)
	rendered := unread.Render("test subject  2m")
	t.Logf("ArticleUnread Width(40): %q", rendered[:min(80, len(rendered))])
	t.Logf("Has bg48: %v", strings.Contains(rendered, "48;"))
	t.Logf("Has bg: %v", strings.Contains(rendered, "\x1b[48"))

	// Check with full ANSI escape
	for i, b := range []byte(rendered) {
		if b == 0x1b {
			t.Logf("ESC at byte %d: %q", i, rendered[i:min(i+30, len(rendered))])
			break
		}
	}
}
