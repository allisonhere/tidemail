package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/charmbracelet/x/ansi"
)

func TestComposePickerFooterShowsHiddenHint(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", ".dotfile"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.openPicker(dir)
	if !c.picker.active {
		t.Fatal("expected picker to be active after openPicker")
	}

	// A constrained height is where the footer used to get clipped: the file list
	// must reserve the footer's true height so the "." hidden hint stays visible.
	view := ansi.Strip(c.View(74, 14, BuildStyles(CatppuccinMocha, "compact", "square")))
	if !strings.Contains(view, "hidden") {
		t.Fatalf("expected attach picker footer to show the hidden hint, got:\n%s", view)
	}
	if !strings.Contains(view, "up dir") {
		t.Fatalf("expected attach picker footer to show the up dir hint, got:\n%s", view)
	}
}

func TestListDirEntriesHidesDotfilesByDefault(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"visible.txt", ".hidden", ".config"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	hidden, err := listDirEntries(dir, false)
	if err != nil {
		t.Fatalf("listDirEntries(showHidden=false): %v", err)
	}
	for _, e := range hidden {
		if e.name == ".hidden" || e.name == ".config" {
			t.Fatalf("expected dotfile %q to be hidden when showHidden=false", e.name)
		}
	}
	if !containsEntry(hidden, "visible.txt") {
		t.Fatalf("expected visible.txt to be listed, got %v", names(hidden))
	}

	shown, err := listDirEntries(dir, true)
	if err != nil {
		t.Fatalf("listDirEntries(showHidden=true): %v", err)
	}
	for _, name := range []string{".hidden", ".config", "visible.txt"} {
		if !containsEntry(shown, name) {
			t.Fatalf("expected %q to be listed when showHidden=true, got %v", name, names(shown))
		}
	}
}

func TestListDirEntriesParentDirSortsFirst(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".config"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	entries, err := listDirEntries(dir, true)
	if err != nil {
		t.Fatalf("listDirEntries: %v", err)
	}
	if len(entries) == 0 || entries[0].name != ".." {
		t.Fatalf("expected \"..\" to sort first, got %v", names(entries))
	}
}

func containsEntry(entries []fileEntry, name string) bool {
	for _, e := range entries {
		if e.name == name {
			return true
		}
	}
	return false
}

func names(entries []fileEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.name
	}
	return out
}
