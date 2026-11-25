package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Clickable status bar. The bar's "key description" pairs ("s sync",
// "ctrl+p command", "U update", the undo pill's "ctrl+z undo") are marked as
// the bar renders, using the same zero-width markers as overlays
// (overlay_hits.go). A click on one presses its key, so it does exactly what
// the hint says for the focused pane.

// renderStatusBar renders the status bar and, with mouse support on, records
// where its clickable hints landed. The returned string never has markers.
func (m *Model) renderStatusBar() string {
	if !m.cfg.Display.MouseSupport {
		return m.renderStatusBarMarked()
	}
	prev := hitMarking
	hitMarking = true
	bar := m.renderStatusBarMarked()
	hitMarking = prev
	bar, hits := extractHits(bar)
	w, h := boxSize(bar)
	m.statusHits.record(overlayNone, m.width, m.height, hits, 0, m.mainHeight(), w, h)
	return bar
}

// markHintPairs marks s (one rendered hint segment) as a click on key.
func markHintPairs(s, key string) string {
	if token, ok := hintClickKey(key); ok {
		return markHit("hint:"+token, s)
	}
	return s
}

// markHintText marks each "key description" pair of a plain hint line (pairs
// are separated by two spaces, as renderPaneHint builds them). Pairs whose key
// only navigates (↑/↓, j/k) or isn't a key at all ("45%", "type search") are
// left plain.
func markHintText(text string) string {
	if !hitMarking {
		return text
	}
	parts := strings.Split(text, "  ")
	for i, part := range parts {
		keyText, _, hasDesc := strings.Cut(part, " ")
		if !hasDesc {
			continue
		}
		parts[i] = markHintPairs(part, keyText)
	}
	return strings.Join(parts, "  ")
}

// handleStatusBarClick presses the key of the status bar hint under a click.
func (m Model) handleStatusBarClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	hm := m.statusHits
	if hm == nil || hm.width != m.width || hm.height != m.height {
		return m, nil
	}
	rx, ry := msg.X-hm.originX, msg.Y-hm.originY
	for _, h := range hm.hits {
		if h.row != ry || rx < h.x0 || rx >= h.x1 {
			continue
		}
		token, ok := strings.CutPrefix(h.id, "hint:")
		if !ok {
			break
		}
		key, ok := keyMsgForHint(token)
		if !ok {
			break
		}
		return m.update(key)
	}
	return m, nil
}
