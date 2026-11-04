package ui

import (
	"fmt"
	"testing"
)

func TestNeedsYouFailedUndoRemainsRetryable(t *testing.T) {
	m, f := newNeedsYouModel(t)
	ids := []int64{f.add(f.inbox, "one", 1, "smart needs_reply true"), f.add(f.inbox, "two", 2, "smart needs_reply true")}
	m = openNeedsYou(t, m)
	if err := f.database.SetNeedsYouDismissedBatch(ids, true); err != nil {
		t.Fatal(err)
	}
	m.needsYou.lastDismissed = ids
	if _, err := f.database.Exec(fmt.Sprintf(`CREATE TRIGGER fail_undo BEFORE DELETE ON message_attention_overrides WHEN OLD.message_id=%d BEGIN SELECT RAISE(ABORT, 'forced undo failure'); END`, ids[1])); err != nil {
		t.Fatal(err)
	}
	cmd, ok := m.undoNeedsYouDismissal()
	if !ok || cmd == nil || len(m.needsYou.lastDismissed) != 2 {
		t.Fatal("undo lost before acknowledgment")
	}
	if duplicate, handled := m.undoNeedsYouDismissal(); duplicate != nil || !handled {
		t.Fatal("overlapping undo was not consumed")
	}
	result := cmd().(needsYouDismissMsg)
	if result.Err == nil {
		t.Fatal("expected undo failure")
	}
	next, _ := m.Update(result)
	m = next.(Model)
	if len(m.needsYou.lastDismissed) != 2 || m.needsYou.dismissalPending {
		t.Fatal("failed undo cannot be retried")
	}
	if _, err := f.database.Exec(`DROP TRIGGER fail_undo`); err != nil {
		t.Fatal(err)
	}
	cmd, ok = m.undoNeedsYouDismissal()
	if !ok || cmd == nil {
		t.Fatal("undo retry unavailable")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(m.needsYou.lastDismissed) != 0 || m.needsYou.dismissalPending {
		t.Fatal("successful undo did not clear history")
	}
	dismissed, err := f.database.DismissedFromNeedsYou()
	if err != nil || dismissed[ids[0]] || dismissed[ids[1]] {
		t.Fatalf("undo not restored atomically: %v %v", dismissed, err)
	}
}
