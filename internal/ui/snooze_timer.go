package ui

// The snooze timer: one cancellable wait for the nearest wake time. When it
// fires, due snoozes are deleted and the next wait is scheduled. Scheduling a
// new wait always cancels the previous one, so at most one timer goroutine
// exists, and quitting cancels it. There is no polling.

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// snoozeClock is the time source for snoozes; tests replace it.
type snoozeClock interface {
	Now() time.Time
	// Wait blocks for d and reports true, or returns false early when ctx is
	// cancelled.
	Wait(ctx context.Context, d time.Duration) bool
}

type realSnoozeClock struct{}

func (realSnoozeClock) Now() time.Time { return time.Now() }

func (realSnoozeClock) Wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

var clockForSnooze snoozeClock = realSnoozeClock{}

// snoozeTickMsg means the scheduled wake time arrived.
type snoozeTickMsg struct{ gen uint64 }

// scheduleSnoozeTimer replaces any pending wait with one for next (or none).
func (m *Model) scheduleSnoozeTimer(next time.Time, ok bool) tea.Cmd {
	if m.snooze.timerCancel != nil {
		m.snooze.timerCancel()
		m.snooze.timerCancel = nil
	}
	m.snooze.deadline = time.Time{}
	if !ok || m.snooze.ctx == nil {
		return nil
	}
	m.snooze.gen++
	gen := m.snooze.gen
	ctx, cancel := context.WithCancel(m.snooze.ctx)
	m.snooze.timerCancel = cancel
	m.snooze.deadline = next
	clock := clockForSnooze // captured: the wait never reads shared state later
	d := max(next.Sub(clock.Now()), 0)
	return func() tea.Msg {
		if !clock.Wait(ctx, d) {
			return nil
		}
		return snoozeTickMsg{gen: gen}
	}
}

// handleSnoozeTick expires due snoozes for the current wait; a tick from a
// replaced wait is ignored.
func (m Model) handleSnoozeTick(msg snoozeTickMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.snooze.gen {
		return m, nil
	}
	m.snooze.timerCancel = nil
	return m, expireSnoozesCmd(m.db)
}

// stopSnoozeTimer cancels the pending wait on quit.
func (m Model) stopSnoozeTimer() {
	if m.snooze.cancel != nil {
		m.snooze.cancel()
	}
}
