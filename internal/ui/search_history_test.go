package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func historyModel(t *testing.T) Model {
	t.Helper()
	m := newFocusLineModel(t)
	m.focused = paneMessages
	return m
}

func hkey(m Model, k tea.KeyMsg) Model {
	next, cmd := m.Update(k)
	m = next.(Model)
	for _, msg := range runCmd(cmd) { // run searches and history writes, and feed results back
		if msg != nil {
			next, _ = m.Update(msg)
			m = next.(Model)
		}
	}
	return m
}

func histType(m Model, s string) Model {
	for _, r := range s {
		m = hkey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

var (
	hEnter   = tea.KeyMsg{Type: tea.KeyEnter}
	keyUpArr = tea.KeyMsg{Type: tea.KeyUp}
	keyDnArr = tea.KeyMsg{Type: tea.KeyDown}
	hTab     = tea.KeyMsg{Type: tea.KeyTab}
	hEsc     = tea.KeyMsg{Type: tea.KeyEsc}
)

func hOpen(m Model) Model { return hkey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}) }

func hRecorded(t *testing.T, m Model) []string {
	t.Helper()
	got, err := m.db.RecentSearches("", 50)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSearchHistoryRecordsOnEnterOnly(t *testing.T) {
	m := hOpen(historyModel(t))
	m = histType(m, "invoice")
	if got := hRecorded(t, m); len(got) != 0 {
		t.Fatalf("typing hRecorded %v; only Enter should", got)
	}
	m = hkey(m, hEnter)
	if got := hRecorded(t, m); !reflect.DeepEqual(got, []string{"invoice"}) {
		t.Fatalf("after Enter: %v", got)
	}
	// Esc abandons a search without remembering it.
	m = hkey(hOpen(m), hEsc)
	m = histType(hOpen(m), "scratch")
	m = hkey(m, hEsc)
	if got := hRecorded(t, m); !reflect.DeepEqual(got, []string{"invoice"}) {
		t.Fatalf("Esc hRecorded something: %v", got)
	}
}

func TestEmptySearchBoxListsHistoryAndEnterRunsEntry(t *testing.T) {
	m := historyModel(t)
	for _, q := range []string{"to:work@example.com", "invoice"} {
		if err := m.db.RecordSearch(q); err != nil {
			t.Fatal(err)
		}
	}
	m = hOpen(m)
	if !m.searchHistoryVisible() || !reflect.DeepEqual(m.searchHistory, []string{"invoice", "to:work@example.com"}) {
		t.Fatalf("visible=%v history=%v", m.searchHistoryVisible(), m.searchHistory)
	}
	pane := ansi.Strip(m.renderMessagesPane())
	if !strings.Contains(pane, "recent searches") || !strings.Contains(pane, "to:work@example.com") {
		t.Fatalf("the pane does not list history:\n%s", pane)
	}
	m = hkey(m, keyDnArr)
	m = hkey(m, keyDnArr) // second entry
	if m.searchHistoryCursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.searchHistoryCursor)
	}
	m = hkey(m, hEnter)
	if m.searchQuery != "to:work@example.com" || m.searchEditing {
		t.Fatalf("Enter on an entry: query=%q editing=%v", m.searchQuery, m.searchEditing)
	}
	if got := hRecorded(t, m); got[0] != "to:work@example.com" {
		t.Fatalf("a re-run entry should move to the front: %v", got)
	}
}

func TestUpFromFirstEntryReturnsToNoHighlight(t *testing.T) {
	m := historyModel(t)
	_ = m.db.RecordSearch("alpha")
	m = hOpen(m)
	m = hkey(m, keyDnArr)
	m = hkey(m, keyUpArr)
	if m.searchHistoryCursor != -1 {
		t.Fatalf("cursor = %d, want -1", m.searchHistoryCursor)
	}
	m = hkey(m, hEnter) // nothing highlighted: plain Enter on an empty query
	if m.searchEditing {
		t.Fatal("Enter should leave the box")
	}
}

func TestTypedQueryOpensFilteredHistoryOnArrow(t *testing.T) {
	m := historyModel(t)
	for _, q := range []string{"to:work@example.com", "to:home@example.com", "invoice"} {
		_ = m.db.RecordSearch(q)
	}
	m = histType(hOpen(m), "to:")
	if m.searchHistoryVisible() {
		t.Fatal("typing must not show the history list; results own the pane")
	}
	m = hkey(m, keyDnArr)
	if !m.searchHistoryVisible() || !reflect.DeepEqual(m.searchHistory, []string{"to:home@example.com", "to:work@example.com"}) {
		t.Fatalf("filtered list = %v (visible %v)", m.searchHistory, m.searchHistoryVisible())
	}
	m = histType(m, "w") // typing closes it again
	if m.searchHistoryVisible() {
		t.Fatal("typing should close the list")
	}
}

func TestTabCompletesFromHistory(t *testing.T) {
	m := historyModel(t)
	_ = m.db.RecordSearch("to:work@example.com")
	m = histType(hOpen(m), "to:wo")
	m = hkey(m, hTab)
	if got := m.searchInput.Value(); got != "to:work@example.com" {
		t.Fatalf("Tab filled %q", got)
	}
	if !m.searchEditing {
		t.Fatal("Tab completes; it must not commit the search")
	}
	m = hkey(m, hTab) // nothing longer to complete: no change
	if got := m.searchInput.Value(); got != "to:work@example.com" {
		t.Fatalf("second Tab changed the box to %q", got)
	}
}

func TestCtrlDDeletesTheHighlightedEntry(t *testing.T) {
	m := historyModel(t)
	for _, q := range []string{"keep", "drop"} {
		_ = m.db.RecordSearch(q)
	}
	m = hOpen(m)
	m = hkey(m, keyDnArr) // "drop" is newest
	m = hkey(m, tea.KeyMsg{Type: tea.KeyCtrlD})
	if got := hRecorded(t, m); !reflect.DeepEqual(got, []string{"keep"}) {
		t.Fatalf("after ctrl+d: %v", got)
	}
	if !reflect.DeepEqual(m.searchHistory, []string{"keep"}) {
		t.Fatalf("list not refreshed: %v", m.searchHistory)
	}
}

func TestJAndKAreTextInTheSearchBox(t *testing.T) {
	m := histType(hOpen(historyModel(t)), "jk")
	if m.searchInput.Value() != "jk" {
		t.Fatalf("box = %q; j/k must stay typed text, not history navigation", m.searchInput.Value())
	}
}

func TestClearSearchHistoryCommand(t *testing.T) {
	m := historyModel(t)
	_ = m.db.RecordSearch("alpha")
	next, _ := m.executeCommand("clear-search-history")
	m = next.(Model)
	if got := hRecorded(t, m); len(got) != 0 {
		t.Fatalf("history not cleared: %v", got)
	}
	m = hOpen(m)
	if pane := ansi.Strip(m.renderMessagesPane()); !strings.Contains(pane, "no recent searches") {
		t.Fatalf("empty history should say so:\n%s", pane)
	}
}
