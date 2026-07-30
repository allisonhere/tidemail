package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

// Glamour renders data tables with │ as a column separator. The old detector
// was strings.Contains(line, "│"), so tables collapsed as if they were quotes.
func TestCollapseQuoteBlocksIgnoresGlamourTables(t *testing.T) {
	table := strings.Join([]string{
		"┌──────┬─────┐",
		"│ Name │ Qty │",
		"├──────┼─────┤",
		"│ Nut  │ 12  │",
		"└──────┴─────┘",
	}, "\n")
	got := collapseQuoteBlocks(table, true)
	if got != table {
		t.Fatalf("table was collapsed:\nwant: %q\ngot:  %q", table, got)
	}
}

func TestCollapseQuoteBlocksCollapsesQuotedText(t *testing.T) {
	body := strings.Join([]string{
		"Reply text.",
		"│ quoted one",
		"│ quoted two",
		"│ quoted three",
		"Trailing text.",
	}, "\n")
	got := collapseQuoteBlocks(body, true)
	if !strings.Contains(got, "[+3 lines of quoted text") {
		t.Fatalf("expected a 3-line placeholder, got: %q", got)
	}
	for _, keep := range []string{"Reply text.", "Trailing text."} {
		if !strings.Contains(got, keep) {
			t.Fatalf("collapse dropped surrounding text %q: %q", keep, got)
		}
	}
}

// plainUI themes render the bar as ASCII "|", which the old detector never
// matched, so collapse silently did nothing on those terminals.
func TestCollapseQuoteBlocksCollapsesASCIIBars(t *testing.T) {
	body := "Reply.\n| quoted one\n| quoted two\nEnd."
	got := collapseQuoteBlocks(body, true)
	if !strings.Contains(got, "[+2 lines of quoted text") {
		t.Fatalf("ASCII quote bars did not collapse: %q", got)
	}
	if !strings.HasPrefix(strings.Split(got, "\n")[1], "|") {
		t.Fatalf("placeholder should use the ASCII bar: %q", got)
	}
}

func TestCollapseQuoteBlocksIsNoOpWhenDisabled(t *testing.T) {
	body := "Reply.\n│ quoted\nEnd."
	if got := collapseQuoteBlocks(body, false); got != body {
		t.Fatalf("collapse ran while disabled: %q", got)
	}
}

func newQuoteTestModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.height = 30
	m.focused = paneContent
	m.viewport.Width = m.contentBodyWidth()
	m.viewport.Height = m.contentBodyHeight()
	msg := db.Message{
		ID:       1,
		Subject:  "Re: report",
		BodyText: "My reply.\n\n> quoted line one\n> quoted line two\n> quoted line three",
	}
	m.messages = []db.Message{msg}
	m.filteredMessages = []db.Message{msg}
	m.setViewportForCurrentRow()
	return m
}

// The z binding used to live in handleSummaryKey, reachable only while the
// summary overlay was open, and never re-rendered the viewport.
func TestToggleQuoteKeyCollapsesFromContentPane(t *testing.T) {
	m := newQuoteTestModel(t)
	if m.contentQuotesCollapsed {
		t.Fatal("quotes should start expanded")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = next.(Model)
	if !m.contentQuotesCollapsed {
		t.Fatal("z did not set the collapsed flag")
	}
	if !strings.Contains(m.viewport.View(), "[+") {
		t.Fatalf("viewport was not re-rendered collapsed: %q", m.viewport.View())
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = next.(Model)
	if m.contentQuotesCollapsed {
		t.Fatal("second z did not expand")
	}
	if !strings.Contains(m.viewport.View(), "quoted line one") {
		t.Fatalf("expanding did not restore quoted text: %q", m.viewport.View())
	}
}

// setViewportMessage used to reset the flag unconditionally, so the re-render
// triggered by the keypress itself immediately undid the toggle.
func TestQuoteCollapseSurvivesSameMessageRerender(t *testing.T) {
	m := newQuoteTestModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = next.(Model)

	m.setViewportForCurrentRow()
	if !m.contentQuotesCollapsed {
		t.Fatal("same-message re-render cleared the collapse flag")
	}
}

func TestQuoteCollapseResetsOnMessageChange(t *testing.T) {
	m := newQuoteTestModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = next.(Model)

	other := db.Message{ID: 2, Subject: "Other", BodyText: "Different body."}
	m.setViewportMessage(other)
	if m.contentQuotesCollapsed {
		t.Fatal("opening a different message should expand quotes")
	}
}
