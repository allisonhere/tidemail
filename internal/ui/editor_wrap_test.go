package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/charmbracelet/x/ansi"
)

func wrappedRows(t *testing.T, word bool) []string {
	t.Helper()
	prev := editorWordWrap
	t.Cleanup(func() { setEditorWordWrap(prev) })
	setEditorWordWrap(word)
	e := newEditorArea()
	e.SetSize(16, 6)
	e.SetValue("hello wonderfulness today")
	var rows []string
	for _, row := range strings.Split(ansi.Strip(e.View()), "\n") {
		if r := strings.TrimRight(row, " "); r != "" {
			rows = append(rows, r)
		}
	}
	return rows
}

func TestComposeEditorWrapsAtWordBoundariesByDefault(t *testing.T) {
	rows := wrappedRows(t, true)
	if len(rows) != 3 || rows[0] != "hello" || rows[1] != "wonderfulness" || rows[2] != "today" {
		t.Fatalf("word wrap should keep words whole, got %q", rows)
	}
}

func TestComposeEditorCharacterWrapWhenDisabled(t *testing.T) {
	rows := wrappedRows(t, false)
	if len(rows) < 2 || rows[0] != "hello wonderfuln" {
		t.Fatalf("character wrap should fill the row, got %q", rows)
	}
}

func TestComposeWordWrapDefaultsOn(t *testing.T) {
	if !config.DefaultConfig().Display.ComposeWordWrap {
		t.Fatal("compose_word_wrap should default to true")
	}
}
