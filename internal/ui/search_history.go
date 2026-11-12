package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tidemail/internal/db"
)

// Search history: committed searches (Enter) are remembered. With an empty
// search box the message pane lists them, newest first; Up/Down on a typed
// query open the same list filtered to entries starting with the typed text.
// Tab completes from it. The list never competes with live results: typing
// closes it.

// historyEntryLimit bounds how many entries a list shows.
const historyEntryLimit = db.MaxSearchHistory

// searchHistoryVisible reports whether the pane shows the history list
// instead of messages.
func (m Model) searchHistoryVisible() bool {
	return m.searchMode && m.searchEditing &&
		(strings.TrimSpace(m.searchInput.Value()) == "" || m.searchHistoryOpen)
}

// refreshSearchHistory reloads the list for the given prefix and drops the
// highlight.
func (m *Model) refreshSearchHistory(prefix string) {
	m.searchHistory, m.searchHistoryCursor = nil, -1
	if m.db == nil {
		return
	}
	if entries, err := m.db.RecentSearches(prefix, historyEntryLimit); err == nil {
		m.searchHistory = entries
	}
}

// beginSearchEditing is called whenever the search box starts taking input.
func (m *Model) beginSearchEditing() {
	m.searchHistoryOpen = false
	m.refreshSearchHistory("")
}

// closeSearchHistory hides the list after the user types or leaves the box.
func (m *Model) closeSearchHistory() {
	m.searchHistoryOpen = false
	m.searchHistoryCursor = -1
}

// moveSearchHistory handles Up (dir < 0) and Down (dir > 0). On a typed query
// the first press opens the list filtered to that text. Up from the first
// entry returns to no highlight, so the user can get back to typing.
func (m *Model) moveSearchHistory(dir int) {
	typed := strings.TrimSpace(m.searchInput.Value())
	if typed != "" && !m.searchHistoryOpen {
		m.refreshSearchHistory(typed)
		if len(m.searchHistory) == 0 {
			return
		}
		m.searchHistoryOpen = true
		m.searchHistoryCursor = 0
		return
	}
	if len(m.searchHistory) == 0 {
		return
	}
	next := m.searchHistoryCursor + dir
	switch {
	case next < -1:
		next = -1
	case next >= len(m.searchHistory):
		next = len(m.searchHistory) - 1
	}
	m.searchHistoryCursor = next
}

// highlightedSearch is the entry under the highlight, if the list is showing.
func (m Model) highlightedSearch() (string, bool) {
	if !m.searchHistoryVisible() || m.searchHistoryCursor < 0 || m.searchHistoryCursor >= len(m.searchHistory) {
		return "", false
	}
	return m.searchHistory[m.searchHistoryCursor], true
}

// completeSearch is what Tab fills in: the highlighted entry, else the newest
// remembered search that starts with the typed text and is longer than it.
func (m Model) completeSearch() (string, bool) {
	if entry, ok := m.highlightedSearch(); ok {
		return entry, true
	}
	typed := strings.TrimSpace(m.searchInput.Value())
	if typed == "" || m.db == nil {
		return "", false
	}
	entries, err := m.db.RecentSearches(typed, historyEntryLimit)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if len(e) > len(typed) {
			return e, true
		}
	}
	return "", false
}

// commitSearch ends editing, keeping the results, and remembers the query.
func (m Model) commitSearch() (Model, tea.Cmd) {
	m.searchEditing = false
	m.searchInput.Blur()
	m.closeSearchHistory()
	return m, m.recordSearchCmd(m.searchQuery)
}

func (m Model) recordSearchCmd(query string) tea.Cmd {
	database := m.db
	if database == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	return func() tea.Msg {
		_ = database.RecordSearch(query)
		return nil
	}
}

// deleteHighlightedSearch forgets the highlighted entry.
func (m *Model) deleteHighlightedSearch() {
	entry, ok := m.highlightedSearch()
	if !ok || m.db == nil {
		return
	}
	_ = m.db.DeleteSearch(entry)
	prefix := ""
	if m.searchHistoryOpen {
		prefix = strings.TrimSpace(m.searchInput.Value())
	}
	cursor := m.searchHistoryCursor
	m.refreshSearchHistory(prefix)
	m.searchHistoryCursor = min(cursor, len(m.searchHistory)-1)
}

// searchHistoryRows draws the list for the message pane.
func (m Model) searchHistoryRows(width int, normal, selected lipgloss.Style) []string {
	if len(m.searchHistory) == 0 {
		return []string{normal.Render("  no recent searches")}
	}
	rows := make([]string, 0, len(m.searchHistory)+1)
	rows = append(rows, normal.Width(width).Render(truncate("  recent searches", width)))
	for i, entry := range m.searchHistory {
		style, prefix := normal, "  "
		if i == m.searchHistoryCursor {
			style, prefix = selected, "→ "
		}
		rows = append(rows, style.Width(width).Render(truncate(prefix+entry, width)))
	}
	return rows
}

// clearSearchHistory forgets every remembered search.
func (m *Model) clearSearchHistory() {
	if m.db != nil {
		_ = m.db.ClearSearchHistory()
	}
	m.searchHistory, m.searchHistoryCursor, m.searchHistoryOpen = nil, -1, false
}
