package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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
