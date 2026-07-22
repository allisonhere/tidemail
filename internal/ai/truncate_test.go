package ai

import (
	"testing"
	"unicode/utf8"
)

func TestTruncateContent_RuneBoundary(t *testing.T) {
	// "aé" — é is 2 bytes (0xC3 0xA9). Truncating to 2 bytes would split it.
	s := "aé"
	got := truncateContent(s, 2)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateContent produced invalid UTF-8: %q", got)
	}
	if got != "a" {
		t.Fatalf("truncateContent(%q, 2) = %q, want %q", s, got, "a")
	}
	// Truncation at an exact boundary keeps the whole rune.
	if got := truncateContent(s, 3); got != "aé" {
		t.Fatalf("truncateContent(%q, 3) = %q, want %q", s, got, "aé")
	}
	// No truncation needed.
	if got := truncateContent("hi", 10); got != "hi" {
		t.Fatalf("truncateContent short string changed it: %q", got)
	}
}
