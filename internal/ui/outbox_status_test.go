package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/db"
)

// statusErrorFg is the color StatusError actually renders with, which is the
// theme's error color after readableText has adjusted it for contrast against
// the bar — not the raw theme value.
func statusErrorFg(s Styles) lipgloss.Color {
	return lipgloss.Color(fmt.Sprint(s.StatusError.GetForeground()))
}

// The last open half of issue #25: a send that fails says so for four seconds
// and then nothing on screen mentions it again, so the reporter only found out
// by opening the Outbox. The status bar now keeps saying so.

// newStatusBarModel returns a model wide enough to render the whole bar.
func newStatusBarModel(t *testing.T, items ...db.OutboxItem) Model {
	t.Helper()
	m := newSendTestModel(t, 5)
	m.outboxItems = items
	return m
}

func outboxItem(state string, attempts int) db.OutboxItem {
	return db.OutboxItem{State: state, Attempts: attempts}
}

func TestOutboxTroubleCountsOnlyWhatNeedsAttention(t *testing.T) {
	m := newStatusBarModel(t,
		outboxItem(db.OutboxFailed, 3),
		outboxItem(db.OutboxUncertain, 1),
		outboxItem(db.OutboxQueued, 1),  // failed once, waiting to retry
		outboxItem(db.OutboxQueued, 0),  // a fresh send in its undo window
		outboxItem(db.OutboxSent, 1),    // done
		outboxItem(db.OutboxSending, 1), // in flight right now
	)

	failed, retrying := m.outboxTrouble()
	if failed != 2 {
		t.Fatalf("failed = %d, want 2 (failed + uncertain)", failed)
	}
	if retrying != 1 {
		t.Fatalf("retrying = %d, want 1 (queued after a failed attempt)", retrying)
	}
}

func TestStatusBarReportsFailedSends(t *testing.T) {
	trueColor(t)

	m := newStatusBarModel(t, outboxItem(db.OutboxFailed, 3))
	bar := m.renderStatusBar()

	if !strings.Contains(ansi.Strip(bar), "1 failed send in Outbox  O") {
		t.Fatalf("expected the failure and its key in the status bar, got %q", ansi.Strip(bar))
	}
	if !strings.Contains(bar, foreground(t, statusErrorFg(m.styles))) {
		t.Fatalf("expected the failure to use the theme error color, got %q", bar)
	}

	m = newStatusBarModel(t, outboxItem(db.OutboxFailed, 3), outboxItem(db.OutboxUncertain, 1))
	if got := ansi.Strip(m.renderStatusBar()); !strings.Contains(got, "2 failed sends in Outbox") {
		t.Fatalf("expected a plural count, got %q", got)
	}
}

// A retry still in flight is worth reporting but is not something to act on,
// so it stays in the ordinary bar style.
func TestStatusBarReportsRetriesQuietly(t *testing.T) {
	trueColor(t)

	m := newStatusBarModel(t, outboxItem(db.OutboxQueued, 1))
	bar := m.renderStatusBar()

	if !strings.Contains(ansi.Strip(bar), "retrying 1 in Outbox  O") {
		t.Fatalf("expected the retry to be reported, got %q", ansi.Strip(bar))
	}
	if strings.Contains(bar, foreground(t, statusErrorFg(m.styles))) {
		t.Fatalf("expected a retry in flight to stay out of the error color, got %q", bar)
	}
}

func TestStatusBarStaysQuietWithNothingWrong(t *testing.T) {
	for _, items := range [][]db.OutboxItem{
		nil,
		{outboxItem(db.OutboxSent, 1)},
		{outboxItem(db.OutboxQueued, 0)}, // the normal path for every send
	} {
		m := newStatusBarModel(t, items...)
		if got := ansi.Strip(m.renderStatusBar()); strings.Contains(got, "Outbox") {
			t.Fatalf("items %+v: expected no Outbox warning, got %q", items, got)
		}
	}
}

// The warning is the first part, so a terminal too narrow for the whole bar
// drops something else.
func TestStatusBarKeepsTheOutboxWarningWhenTruncated(t *testing.T) {
	m := newStatusBarModel(t, outboxItem(db.OutboxFailed, 3))

	wide := ansi.Strip(m.renderStatusBar())
	m.width = 34
	narrow := ansi.Strip(m.renderStatusBar())

	if !strings.Contains(narrow, "1 failed send in Outbox") {
		t.Fatalf("expected the warning to survive truncation, got %q", narrow)
	}
	if len(narrow) >= len(wide) {
		t.Fatalf("expected the narrow bar to be truncated: %q vs %q", narrow, wide)
	}
}

// Keys are rebindable, so the hint must come from the binding.
func TestStatusBarOutboxHintFollowsTheKeybinding(t *testing.T) {
	m := newStatusBarModel(t, outboxItem(db.OutboxFailed, 3))
	m.keys.Outbox = key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "outbox"))

	if got := ansi.Strip(m.renderStatusBar()); !strings.Contains(got, "1 failed send in Outbox  ctrl+b") {
		t.Fatalf("expected the rebound key in the hint, got %q", got)
	}
}
