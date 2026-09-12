package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Data tables are laid out as fixed-width text by tableTextRule and fenced as a
// code block so glamour cannot re-wrap the columns. The fence protects the
// column maths but also hands the table glamour's code colors, so a receipt
// reads as source code. This pass restyles those lines afterwards, the same way
// styleImagePlaceholders restyles image labels.

// bodyTableCollector holds the tables laid out in one render, in document
// order. A converter is built per render, so a collector is never shared.
type bodyTableCollector struct {
	tables [][]string
}

func (c *bodyTableCollector) record(lines []string) {
	if c == nil || len(lines) == 0 {
		return
	}
	block := make([]string, len(lines))
	copy(block, lines)
	c.tables = append(c.tables, block)
}

// isTableRule reports whether a row is the header separator rather than data.
func isTableRule(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return strings.Trim(s, "-+ ") == ""
}

// styleBodyTables recolors the rendered form of each recorded table so it uses
// the theme's table colors instead of the code palette.
func styleBodyTables(rendered string, th Theme, plainUI bool, c *bodyTableCollector) string {
	if plainUI || rendered == "" || c == nil || len(c.tables) == 0 {
		return rendered
	}

	cell := lipgloss.NewStyle().Background(th.Bg).Foreground(th.Fg)
	rule := lipgloss.NewStyle().Background(th.Bg).Foreground(messageMutedColor(th))

	lines := strings.Split(rendered, "\n")
	cursor := 0
	for _, table := range c.tables {
		start := findTableStart(lines, cursor, table[0])
		if start < 0 {
			continue
		}
		for i, row := range table {
			at := start + i
			if at >= len(lines) {
				break
			}
			text := strings.TrimRight(ansi.Strip(lines[at]), " ")
			if strings.TrimSpace(text) == "" {
				continue
			}
			indent := text[:len(text)-len(strings.TrimLeft(text, " "))]
			body := text[len(indent):]
			if isTableRule(row) {
				lines[at] = indent + rule.Render(body)
				continue
			}
			lines[at] = indent + cell.Render(body)
		}
		cursor = start + len(table)
	}
	return strings.Join(lines, "\n")
}

// findTableStart locates the rendered line holding a table's first row.
func findTableStart(lines []string, from int, first string) int {
	want := strings.TrimSpace(first)
	if want == "" {
		return -1
	}
	for i := from; i < len(lines); i++ {
		if strings.TrimSpace(ansi.Strip(lines[i])) == want {
			return i
		}
	}
	return -1
}
