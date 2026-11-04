package ui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestLinksRejectUnicodeHardBreaksWithoutMisroutingRows(t *testing.T) {
	for _, separator := range []string{"\u2028", "\u2029"} {
		t.Run(separator, func(t *testing.T) {
			unsafe := "https://evil.example/x" + separator + "hide"
			if cleanDetectedURL(unsafe) != "" || safeOSC8URI(unsafe) {
				t.Fatal("accepted hard break in URL")
			}
			m := readerLinksModel(t, "", "see "+unsafe+" and https://bank.example/login now")
			if len(m.contentLinks) != 1 || m.contentLinks[0] != "https://bank.example/login" {
				t.Fatalf("links: %q", m.contentLinks)
			}
			row := m.contentLinkRows[0]
			if !strings.Contains(ansi.Strip(m.contentLines[row]), "bank.example/login") || m.contentLinkIndexAt(row) != 0 {
				t.Fatal("display row mismatches activation target")
			}
			html := `<a href="https://evil.example/x` + separator + `hide">Unsafe</a><a href="https://bank.example/login">Bank` + separator + `login</a><a href="https://example.com/next">Next</a>`
			m = readerLinksModel(t, html, "")
			if len(m.contentLinks) != 2 {
				t.Fatalf("HTML links: %q", m.contentLinks)
			}
			for i, want := range []string{"Bank login", "Next"} {
				row := m.contentLinkRows[i]
				if !strings.Contains(ansi.Strip(m.contentLines[row]), want) || m.contentLinkIndexAt(row) != i {
					t.Fatalf("label/target mismatch at row %d: %q", row, m.contentLines[row])
				}
			}
		})
	}
}

func TestBareCarriageReturnCannotReachPreformattedTerminalRows(t *testing.T) {
	input := "  first\r\n  second\rOVERWRITE\n  third"
	got := stripEmailInvisibles(input)
	if strings.ContainsRune(got, '\r') || got != "  first\n  secondOVERWRITE\n  third" {
		t.Fatalf("sanitized: %q", got)
	}
	m := readerLinksModel(t, "", input)
	view := m.renderContentPane()
	if strings.ContainsRune(view, '\r') {
		t.Fatalf("terminal row contains bare CR: %q", view)
	}
	if !strings.Contains(ansi.Strip(view), "secondOVERWRITE") {
		t.Fatal("preformatted content was lost")
	}
}
