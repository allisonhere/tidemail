package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestRenderArticleRowLeavesTwoCellsAfterTime(t *testing.T) {
	row := renderArticleRow("· ", "  ", "Subject", "2m", 24)

	if got := lipgloss.Width(row); got != 24 {
		t.Fatalf("expected row width 24, got %d in %q", got, row)
	}
	if !strings.HasSuffix(row, "2m  ") {
		t.Fatalf("expected two trailing cells after time, got %q", row)
	}
}

func TestRenderArticleRowStarColumnKeepsWidth(t *testing.T) {
	starred := renderArticleRow("· ", "★ ", "Subject", "2m", 24)
	plain := renderArticleRow("· ", "  ", "Subject", "2m", 24)

	if !strings.Contains(starred, "★") {
		t.Fatalf("expected star glyph in starred row, got %q", starred)
	}
	if strings.Contains(plain, "★") {
		t.Fatalf("did not expect star glyph in non-starred row, got %q", plain)
	}
	if a, b := lipgloss.Width(starred), lipgloss.Width(plain); a != b || a != 24 {
		t.Fatalf("expected both rows width 24, got starred=%d plain=%d", a, b)
	}
}

func TestMessageRowStarInheritsWholeRowBackground(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	star := m.messageRowStar(true)

	if strings.Contains(star, "\x1b[") {
		t.Fatalf("star column must not contain ANSI styling that resets the row background: %q", star)
	}
	if got := lipgloss.Width(star); got != 2 {
		t.Fatalf("expected star column width 2, got %d", got)
	}
}

func TestSelectedStarredMessageKeepsHighlightAcrossEntireRow(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	cfg := config.DefaultConfig()
	cfg.Display.Density = "compact"
	m := NewModel(nil, cfg, "dev", false)
	m.width = 80
	m.height = 20
	m.filteredMessages = []db.Message{{
		ID:      1,
		Subject: "Selected subject",
		From:    "sender@example.com",
		Date:    time.Now(),
		Starred: true,
	}}
	m.messageCursor = 0

	line := strings.Split(m.renderMessagesPane(), "\n")[2]
	starAt := strings.Index(line, "★")
	subjectAt := strings.Index(line, "Selected subject")
	if starAt < 0 || subjectAt <= starAt {
		t.Fatalf("expected starred selected row, got %q", line)
	}
	if strings.Contains(line[starAt:subjectAt], "\x1b[0m") {
		t.Fatalf("selected-row style reset before subject, leaving part of row unhighlighted: %q", line)
	}
}

func TestCurrentMessageRowUsesRightArrowPrefix(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 80
	m.height = 20
	m.filteredMessages = []db.Message{{ID: 1, Subject: "Current", Date: time.Now()}}

	view := ansi.Strip(m.renderMessagesPane())
	if !strings.Contains(view, "→ ") {
		t.Fatalf("current message row should have a right arrow prefix, got %q", view)
	}
}

// renderedForeground renders a throwaway cell with the given foreground and
// returns its SGR code, so a test can match against the real render pipeline
// (accentReadableOn's lightness adjustment can round slightly differently
// than re-deriving the escape code by hand from the hex string).
func renderedForeground(t *testing.T, c lipgloss.Color) string {
	t.Helper()
	sample := lipgloss.NewStyle().Foreground(c).Render("x")
	start := strings.IndexByte(sample, '[')
	end := strings.IndexByte(sample, 'm')
	if start < 0 || end < 0 || end < start {
		t.Fatalf("could not extract SGR code from %q", sample)
	}
	return sample[start+1 : end]
}

// mixedAccountModel builds a model with two colored accounts, one inbox
// mailbox each, and the Unified Inbox selected, ready for a filteredMessages
// set by the caller.
func mixedAccountModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 80
	m.height = 20
	m.accounts = []db.Account{
		{ID: 10, Name: "Personal", Color: "#c41e3a"},
		{ID: 11, Name: "Work", Color: "#1e90ff"},
	}
	m.mailboxes = []db.Mailbox{
		{ID: 20, AccountID: 10, Name: "INBOX"},
		{ID: 21, AccountID: 11, Name: "INBOX"},
	}
	m.sidebarRows = []sidebarRow{{kind: rowKindUnified}}
	m.sidebarCursor = 0
	return m
}

func TestUnifiedInboxRowsUseEachMessagesAccountColor(t *testing.T) {
	trueColor(t)

	m := mixedAccountModel(t)
	m.filteredMessages = []db.Message{
		{ID: 1, MailboxID: 20, Subject: "From personal", Date: time.Now()},
		{ID: 2, MailboxID: 21, Subject: "From work", Date: time.Now()},
	}
	m.messageCursor = -1 // keep both rows off the cursor/selected style

	unread, _, _, _, _, _ := m.messageRowStyles()
	bg := terminalColorAsColor(unread.GetBackground())
	if bg == "" {
		bg = m.styles.Theme.Bg
	}
	wantPersonal := renderedForeground(t, accentReadableOn(lipgloss.Color("#c41e3a"), bg, 3))
	wantWork := renderedForeground(t, accentReadableOn(lipgloss.Color("#1e90ff"), bg, 3))
	wantDefault := renderedForeground(t, terminalColorAsColor(unread.GetForeground()))

	view := m.renderMessagesPane()
	personalLine := lineContaining(t, view, "From personal")
	workLine := lineContaining(t, view, "From work")

	if !strings.Contains(personalLine, wantPersonal) {
		t.Fatalf("expected personal row tinted with its account color, got %q", personalLine)
	}
	if !strings.Contains(workLine, wantWork) {
		t.Fatalf("expected work row tinted with its account color, got %q", workLine)
	}
	if strings.Contains(personalLine, wantDefault) || strings.Contains(workLine, wantDefault) {
		t.Fatalf("expected account-tinted rows, not the plain unread foreground")
	}
}

func TestSingleMailboxPaneAccentUnaffectedByMixedAccountTinting(t *testing.T) {
	trueColor(t)

	m := mixedAccountModel(t)
	m.sidebarRows = []sidebarRow{{kind: rowKindMailbox, mailboxID: 20, accountID: 10}}
	m.filteredMessages = []db.Message{{ID: 1, MailboxID: 20, Subject: "From personal", Date: time.Now()}}
	m.messageCursor = -1

	unread, _, _, _, _, _ := m.messageRowStyles()
	bg := terminalColorAsColor(unread.GetBackground())
	if bg == "" {
		bg = m.styles.Theme.Bg
	}
	wantPersonal := renderedForeground(t, accentReadableOn(lipgloss.Color("#c41e3a"), bg, 3))

	line := lineContaining(t, m.renderMessagesPane(), "From personal")
	if !strings.Contains(line, wantPersonal) {
		t.Fatalf("expected the existing single-mailbox pane accent to still color the row, got %q", line)
	}
}

func TestUncoloredAccountRowRendersUnchangedInUnifiedInbox(t *testing.T) {
	trueColor(t)

	m := mixedAccountModel(t)
	m.accounts[1].Color = ""
	m.filteredMessages = []db.Message{{ID: 2, MailboxID: 21, Subject: "From work", Date: time.Now()}}
	m.messageCursor = -1

	unread, _, _, _, _, _ := m.messageRowStyles()
	wantDefault := renderedForeground(t, terminalColorAsColor(unread.GetForeground()))

	line := lineContaining(t, m.renderMessagesPane(), "From work")
	if !strings.Contains(line, wantDefault) {
		t.Fatalf("expected an uncolored account's row to keep the plain unread foreground, got %q", line)
	}
}

func TestCursorRowKeepsSelectedStyleOverAccountTint(t *testing.T) {
	trueColor(t)

	m := mixedAccountModel(t)
	m.filteredMessages = []db.Message{{ID: 1, MailboxID: 20, Subject: "From personal", Date: time.Now()}}
	m.messageCursor = 0

	_, _, selected, _, _, _ := m.messageRowStyles()
	wantSelected := renderedForeground(t, terminalColorAsColor(selected.GetForeground()))
	bg := m.styles.Theme.Bg
	notWantAccent := renderedForeground(t, accentReadableOn(lipgloss.Color("#c41e3a"), bg, 3))

	line := lineContaining(t, m.renderMessagesPane(), "From personal")
	if !strings.Contains(line, wantSelected) {
		t.Fatalf("expected cursor row to use the selected style, got %q", line)
	}
	if strings.Contains(line, notWantAccent) && notWantAccent != wantSelected {
		t.Fatalf("expected cursor row to not carry the account tint, got %q", line)
	}
}
