package ui

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

// displayKey contains only fixed-size display state and immutable strings. Mail,
// annotations, styles and prepared content are covered by a model-owned revision
// advanced at their mutation boundaries. Neither constructing nor comparing a
// hit key visits mailbox collections or message payloads.
type displayKey struct {
	initialLoading, needsYouLoaded, waitingLoaded, snoozeLoaded                         bool
	selectedCount, expandedCount                                                        int
	revision, clock                                                                     uint64
	width, height                                                                       int
	focused                                                                             pane
	sidebarCursor, sidebarOffset, messageCursor, listOffset                             int
	viewportOffset, viewportWidth, viewportHeight                                       int
	contentFocus, contentLink, searchIndex, selectionAnchor                             int
	contentPending, selection, selectionAll, searchMode, searchEditing, unread, starred bool
	searchQuery, searchInput, spinner                                                   string
	readFrame, pendingReads, pendingBulkReads, syncCount                                int
	needsYou, waiting, snoozed                                                          int
	profile                                                                             int
	dark                                                                                bool
	bg                                                                                  lipgloss.Color
	animation                                                                           int64
}

type composeStatusState struct {
	panes                           displayKey
	message                         string
	isErr, pill, dismissed          bool
	fadeStart                       time.Time
	fadeFrom                        alertPill
	update                          updateState
	updateVersion, installedVersion string
	animation                       int64
}

// invalidateComposePanes belongs at display-data mutation/installation boundaries,
// never at mouse or cursor dispatch. Model owns the revision by value; the render
// artifacts remain pointer-owned across Bubble Tea's value receivers.
func (m *Model) invalidateComposePanes() { m.composeDisplayRevision++ }

func (m *Model) composePaneKey() displayKey {
	var clock uint64
	if c := m.composeBackdrop; c != nil {
		if !c.labelsExpire.IsZero() && !time.Now().Before(c.labelsExpire) {
			c.clockRevision++
			c.labelsExpire = time.Time{}
		}
		clock = c.clockRevision
	}
	var animation int64
	// Pending destructive actions draw countdowns/working indicators. They are
	// uncommon and genuinely animated; do not cache their background frames.
	if len(m.pendingDestructiveActions) > 0 {
		animation = time.Now().UnixNano()
	}
	return displayKey{
		initialLoading: m.initialLoading, needsYouLoaded: m.needsYou.countLoaded, waitingLoaded: m.waiting.loaded, snoozeLoaded: m.snooze.loaded, selectedCount: len(m.selectedMessages), expandedCount: len(m.expandedMessages),
		revision: m.composeDisplayRevision, clock: clock, width: m.width, height: m.height, focused: m.focused,
		sidebarCursor: m.sidebarCursor, sidebarOffset: m.sidebarOffset, messageCursor: m.messageCursor, listOffset: m.listOffset,
		viewportOffset: m.viewport.YOffset, viewportWidth: m.viewport.Width, viewportHeight: m.viewport.Height,
		contentFocus: m.contentFocusLine, contentLink: m.contentLinkIdx, searchIndex: m.contentSearchIdx, selectionAnchor: m.contentSelectionAnchor,
		contentPending: m.contentFocusPending, selection: m.contentSelectionActive, selectionAll: m.contentSelectionAll,
		searchMode: m.searchMode, searchEditing: m.searchEditing, unread: m.showUnreadOnly, starred: m.starredFirst,
		searchQuery: m.searchQuery, searchInput: m.searchInput.Value(), spinner: m.spinner.View(), readFrame: m.readAnimationFrame,
		pendingReads: len(m.pendingReads), pendingBulkReads: len(m.pendingBulkReads), syncCount: len(m.syncVisible),
		needsYou: m.needsYou.count, waiting: m.waiting.count, snoozed: m.snooze.count,
		profile: int(lipgloss.ColorProfile()), dark: lipgloss.HasDarkBackground(), bg: m.styles.Theme.Bg, animation: animation,
	}
}

func (m *Model) composeStatusKey(panes displayKey) composeStatusState {
	var animation int64
	if len(m.pendingDestructiveActions) > 0 || (!m.pillFadeStart.IsZero() && time.Since(m.pillFadeStart) < pillFadeDuration) {
		animation = time.Now().UnixNano()
	}
	return composeStatusState{panes: panes, message: m.statusMsg, isErr: m.statusErr, pill: m.statusPill,
		fadeStart: m.pillFadeStart, fadeFrom: m.pillFadeFrom, update: m.updateState, updateVersion: m.updateInfo.Version,
		installedVersion: m.updateInstall.Version, dismissed: m.updateDismissed, animation: animation}
}

// nextRelativeLabelChange matches relativeTime's exact rounding boundaries.
// Absolute/hidden dates have no timer. This runs only on a pane cache miss.
func nextRelativeLabelChange(at, now time.Time) time.Time {
	d := now.Sub(at)
	switch {
	case d < time.Minute:
		return at.Add(time.Minute)
	case d < time.Hour:
		return at.Add((d/time.Minute + 1) * time.Minute)
	case d < 24*time.Hour:
		return at.Add((d/time.Hour + 1) * time.Hour)
	case d < 7*24*time.Hour:
		return at.Add((d/(24*time.Hour) + 1) * 24 * time.Hour)
	case d < 28*24*time.Hour:
		return at.Add((d/(7*24*time.Hour) + 1) * 7 * 24 * time.Hour)
	default:
		return time.Time{}
	}
}

// Only visible dates can change the cached panes. Compute their next expiry once
// when building an entry; hits only compare the saved deadline with the clock.
func (m *Model) composeLabelExpiry(now time.Time) time.Time {
	var deadline time.Time
	add := func(t time.Time) {
		if !t.IsZero() && (deadline.IsZero() || t.Before(deadline)) {
			deadline = t
		}
	}
	if m.selectedSnoozed() {
		y, month, d := now.Date()
		add(time.Date(y, month, d+1, 0, 0, 0, 0, now.Location()))
	}
	if m.cfg.Display.DateFormat == "absolute" || m.cfg.Display.DateFormat == "none" {
		return deadline
	}
	if m.selectedDraftsMailbox() {
		for i := m.listOffset; i < min(len(m.drafts), m.listOffset+m.articleRowsVisible()); i++ {
			add(nextRelativeLabelChange(m.drafts[i].UpdatedAt, now))
		}
	} else {
		for i := m.listOffset; i < min(m.activeMessageRowCount(), m.listOffset+m.articleRowsVisible()); i++ {
			if msg := m.rowMessage(i); msg != nil {
				add(nextRelativeLabelChange(msg.Date, now))
			}
		}
	}
	if mb := m.selectedMailbox(); mb != nil && !mb.LastSynced.IsZero() {
		add(nextRelativeLabelChange(mb.LastSynced, now))
	}
	if since := m.summarizeOutbox().since; !since.IsZero() {
		add(nextRelativeLabelChange(since, now))
	}
	return deadline
}

func (m *Model) viewCachedCompose() string {
	c := m.composeBackdrop
	key := m.composePaneKey()
	main := c.paneStage.Get(key, func() string {
		c.labelsExpire = m.composeLabelExpiry(time.Now())
		return lipgloss.JoinHorizontal(lipgloss.Top, m.renderAccountsPane(), lipgloss.JoinVertical(lipgloss.Left, m.renderMessagesPane(), m.renderContentPane()))
	})
	c.panes, c.valid = main, true
	status := c.statusStage.Get(m.composeStatusKey(key), func() string { return m.renderStatusBar() })
	background := c.backgroundStage.Get(composeBackgroundKey{main, status}, func() string { return lipgloss.JoinVertical(lipgloss.Left, main, status) })
	view := m.renderOverlay(background)
	view = c.finish.View(view, m.width, m.height, m.styles.Theme.Bg)
	if m.cfg.Display.Transparent {
		view = transparentBackground(view, m.styles.Theme.Bg)
	}
	return view
}
