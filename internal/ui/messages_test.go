package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/charmbracelet/lipgloss"
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

	line := strings.Split(m.renderMessagesPane(), "\n")[1]
	starAt := strings.Index(line, "★")
	subjectAt := strings.Index(line, "Selected subject")
	if starAt < 0 || subjectAt <= starAt {
		t.Fatalf("expected starred selected row, got %q", line)
	}
	if strings.Contains(line[starAt:subjectAt], "\x1b[0m") {
		t.Fatalf("selected-row style reset before subject, leaving part of row unhighlighted: %q", line)
	}
}
