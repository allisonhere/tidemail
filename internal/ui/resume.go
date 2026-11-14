package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Suspend/resume recovery.
//
// Go timers and TCP sockets do not notice a laptop suspend: timers run on the
// monotonic clock, which stops while the machine sleeps, so a poll armed before
// suspend fires late by the whole sleep; and an IDLE connection the network
// dropped in the meantime stays silent, so the watcher never sees an error.
// Mail then stops arriving until a manual sync. A cheap wall-clock ticker
// detects the jump and restarts the IDLE watchers and sync timers.

const (
	resumeTickInterval = 15 * time.Second
	// resumeGapThreshold is how much longer than the tick interval the wall
	// clock must have advanced before the gap counts as a suspend, not a stall.
	resumeGapThreshold = 45 * time.Second
)

// resumeTickMsg carries the wall-clock time of a tick. at has its monotonic
// reading stripped so subtracting two of them measures wall time.
type resumeTickMsg struct{ at time.Time }

func resumeTickCmd() tea.Cmd {
	return tea.Tick(resumeTickInterval, func(time.Time) tea.Msg {
		return resumeTickMsg{at: time.Now().Round(0)}
	})
}

// resumedAfterGap reports whether the wall clock advanced far more than one
// tick interval between prev and now. A zero prev (first tick) never counts.
func resumedAfterGap(prev, now time.Time) bool {
	if prev.IsZero() {
		return false
	}
	return now.Sub(prev) > resumeTickInterval+resumeGapThreshold
}

func (m Model) handleResumeTick(msg resumeTickMsg) (tea.Model, tea.Cmd) {
	prev := m.lastResumeTick
	m.lastResumeTick = msg.at
	if !resumedAfterGap(prev, msg.at) {
		return m, resumeTickCmd()
	}
	logIdle("resume\twall clock advanced %v since last check; restarting watchers and sync timers", msg.at.Sub(prev).Round(time.Second))
	// Drop the possibly dead IDLE connections and start fresh ones, then re-arm
	// every poll timer (invalidating late ones) and sync right away.
	m.stopIdleWatchers()
	return m, tea.Batch(m.startIdleWatchers(), m.startSyncTimers(), resumeTickCmd())
}
