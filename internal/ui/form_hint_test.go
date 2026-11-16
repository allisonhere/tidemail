package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
)

func textIsFaint(t *testing.T, rendered, text string) bool {
	t.Helper()
	plain := ansi.Strip(rendered)
	byteOffset := strings.Index(plain, text)
	if byteOffset < 0 {
		t.Fatalf("rendered line does not contain %q: %q", text, plain)
	}
	x := lipgloss.Width(plain[:byteOffset])
	buf := cellbuf.NewBuffer(max(1, lipgloss.Width(rendered)), 1)
	cellbuf.SetContent(buf, rendered)
	cell := buf.Cell(x, 0)
	if cell == nil {
		t.Fatalf("rendered line has no cell at %d: %q", x, plain)
	}
	return cell.Style.Attrs.Contains(cellbuf.FaintAttr)
}

func TestFormHintFocusControlsFaintAttribute(t *testing.T) {
	trueColor(t)
	chrome := newManagerChrome(60, CatppuccinMocha, false)

	focused := renderFormHintLines("field guidance", 60, chrome, true)[0]
	if textIsFaint(t, focused, "field guidance") {
		t.Fatalf("focused field help should not be faint: %q", focused)
	}

	unfocused := renderFormHintLines("field guidance", 60, chrome, false)[0]
	if !textIsFaint(t, unfocused, "field guidance") {
		t.Fatalf("unfocused field help should be faint: %q", unfocused)
	}
}

func TestColoredFormHintKeepsMarkerBright(t *testing.T) {
	trueColor(t)
	chrome := newManagerChrome(60, CatppuccinMocha, false)
	line := renderFormHintLinesColored("status guidance", 60, chrome, "ERR", chrome.errorFg, false)[0]

	if textIsFaint(t, line, "ERR") {
		t.Fatalf("semantic marker should stay bright: %q", line)
	}
	if !textIsFaint(t, line, "status guidance") {
		t.Fatalf("unfocused status guidance should be faint: %q", line)
	}
}
