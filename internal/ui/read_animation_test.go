package ui

import (
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
