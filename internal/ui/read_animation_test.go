package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
	"time"
)

func TestReadAnimationSharedClockAndCleanup(t *testing.T) {
	m, _, _ := readPendingModel(t)
	m.cfg.Display.Icons = true
	first, second := m.messages[0].ID, m.messages[1].ID
	m.pendingReads = map[int64]pendingRead{first: {Token: 1}, second: {Token: 2}}
	originalFPS := m.spinner.Spinner.FPS
	tick := m.ensureReadAnimation()
	if tick == nil || m.ensureReadAnimation() != nil {
		t.Fatal("missing or duplicate animation loop")
	}
	began := time.Now()
	event := tick()
	if time.Since(began) < readAnimationInterval {
		t.Fatal("tick woke before 250ms")
	}
	before := m.pendingReadIndicator()
	next, follow := m.Update(event)
	m = next.(Model)
	if follow == nil || m.pendingReadIndicator() == before || len(m.pendingReads) != 2 {
		t.Fatal("shared tick did not advance pending frame")
	}
	if m.spinner.Spinner.FPS != originalFPS {
		t.Fatal("changed unrelated spinner speed")
	}
	if !strings.Contains(m.renderMessagesPane(), m.pendingReadIndicator()) {
		t.Fatal("new frame not rendered")
	}
	next, _ = m.Update(readRemoteCompletedMsg{Token: 1, IDs: []int64{first}})
	m = next.(Model)
	if !m.readAnimationRunning {
		t.Fatal("stopped with another pending message")
	}
	next, _ = m.Update(readRemoteCompletedMsg{Token: 2, IDs: []int64{second}})
	m = next.(Model)
	if m.readAnimationRunning || m.ensureReadAnimation() != nil {
		t.Fatal("animation did not stop")
	}
	frame := m.readAnimationFrame
	next, follow = m.Update(event)
	m = next.(Model)
	if follow != nil || m.readAnimationFrame != frame {
		t.Fatal("stale tick restarted idle animation")
	}
	m.pendingReads[first] = pendingRead{Token: 3}
	if m.ensureReadAnimation() == nil {
		t.Fatal("animation did not restart")
	}
	next, follow = m.Update(event)
	m = next.(Model)
	if follow != nil || !m.readAnimationRunning {
		t.Fatal("old tick interfered with new generation")
	}
}

// A read whose completion never arrives must not lock the message: the next
// animation tick releases it once it is older than readPendingMaxAge.
func TestStalePendingReadIsReleased(t *testing.T) {
	m, _, _ := readPendingModel(t)
	m.cfg.Display.Icons = true
	first, second := m.messages[0].ID, m.messages[1].ID
	m.pendingReads = map[int64]pendingRead{
		first:  {Token: 1, Started: time.Now().Add(-readPendingMaxAge - time.Second)},
		second: {Token: 2, Started: time.Now()},
	}
	tick := m.ensureReadAnimation()
	if tick == nil {
		t.Fatal("animation did not start")
	}
	event := tick()
	next, _ := m.Update(event)
	m = next.(Model)
	if _, stuck := m.pendingReads[first]; stuck {
		t.Fatal("a read pending past the limit was not released")
	}
	if _, live := m.pendingReads[second]; !live {
		t.Fatal("a fresh pending read was released")
	}
	if !m.statusErr || !strings.Contains(m.statusMsg, "try it again") {
		t.Fatalf("no explanation shown: err=%v status=%q", m.statusErr, m.statusMsg)
	}
	if !m.readAnimationRunning {
		t.Fatal("animation stopped while another read is pending")
	}
}

// Entries without a start time (and fresh ones) are never expired.
func TestPendingReadWithoutStartTimeNeverExpires(t *testing.T) {
	m, _, _ := readPendingModel(t)
	id := m.messages[0].ID
	m.pendingReads = map[int64]pendingRead{id: {Token: 1}}
	if n := m.expireStaleReads(time.Now().Add(24 * time.Hour)); n != 0 {
		t.Fatalf("released %d entries with no start time", n)
	}
}

// Toggling a message whose last change is still saving must say so, not do
// nothing silently.
func TestToggleReadWhilePendingExplainsItself(t *testing.T) {
	m, _, _ := readPendingModel(t)
	m.pendingReads = map[int64]pendingRead{m.messages[0].ID: {Token: 1, Started: time.Now()}}
	m.messageCursor = 0
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	if !strings.Contains(m.statusMsg, "still saving") {
		t.Fatalf("a skipped toggle gave no explanation, status = %q", m.statusMsg)
	}
	if len(m.pendingReads) != 1 {
		t.Fatalf("pending entries changed: %v", m.pendingReads)
	}
}
