package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderArticleRowLeavesTwoCellsAfterTime(t *testing.T) {
	row := renderArticleRow("· ", "Subject", "2m", 24)

	if got := lipgloss.Width(row); got != 24 {
		t.Fatalf("expected row width 24, got %d in %q", got, row)
	}
	if !strings.HasSuffix(row, "2m  ") {
		t.Fatalf("expected two trailing cells after time, got %q", row)
	}
}
