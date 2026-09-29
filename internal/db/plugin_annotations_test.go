package db

import (
	"fmt"
	"testing"
	"time"
)

// newAnnotationTestDB returns a database with one mailbox holding n messages
// (UIDs 1..n) and their row IDs.
func newAnnotationTestDB(t *testing.T, n int) (*DB, int64, []int64) {
	t.Helper()
	d := newTestDB(t)
	accountID, err := d.AddAccount("", "Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := d.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX", Delimiter: "/"})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, n)
	for i := range n {
		uid := uint32(i + 1)
		if err := d.UpsertMessage(Message{MailboxID: mailboxID, UID: uid, Subject: fmt.Sprintf("m%d", uid), Date: time.Unix(int64(1000+i), 0)}); err != nil {
			t.Fatal(err)
		}
		if err := d.QueryRow(`SELECT id FROM messages WHERE mailbox_id = ? AND uid = ?`, mailboxID, uid).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	return d, mailboxID, ids
}

func ann(key, value string, confidence ...float64) PluginAnnotation {
	a := PluginAnnotation{Key: key, Value: value}
	if len(confidence) > 0 {
		c := confidence[0]
		a.Confidence = &c
	}
	return a
}

// kv flattens annotations to "plugin:key=value" strings in stored order.
func kv(anns []PluginAnnotation) []string {
	var out []string
	for _, a := range anns {
		out = append(out, a.PluginID+":"+a.Key+"="+a.Value)
	}
	return out
}

func mustList(t *testing.T, d *DB, messageID int64) []PluginAnnotation {
	t.Helper()
	anns, err := d.ListPluginAnnotations(messageID)
	if err != nil {
		t.Fatal(err)
	}
	return anns
}

func assertKV(t *testing.T, got []PluginAnnotation, want ...string) {
	t.Helper()
	g := kv(got)
	if fmt.Sprint(g) != fmt.Sprint(want) {
		t.Fatalf("annotations = %v, want %v", g, want)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	d := newTestDB(t)
	var on int
	if err := d.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatal(err)
	}
	if on != 1 {
		t.Fatal("foreign keys are off; annotation cleanup relies on ON DELETE CASCADE")
	}
}

func TestReplacePluginAnnotationsInsertAndRead(t *testing.T) {
	d, _, ids := newAnnotationTestDB(t, 1)
	if err := d.ReplacePluginAnnotations("jev", ids[0], []PluginAnnotation{ann("urgency", "high", 0.82), ann("needs_reply", "true")}); err != nil {
		t.Fatal(err)
	}
	got := mustList(t, d, ids[0])
	assertKV(t, got, "jev:needs_reply=true", "jev:urgency=high")
	if got[0].Confidence != nil {
		t.Fatal("confidence should stay unset when not supplied")
	}
	if got[1].Confidence == nil || *got[1].Confidence != 0.82 {
		t.Fatalf("confidence = %v", got[1].Confidence)
	}
	if got[0].MessageID != ids[0] || got[0].UpdatedAt.IsZero() {
		t.Fatalf("row = %+v", got[0])
	}
}

func TestReplacePluginAnnotationsUpdatesAndRemovesStaleKeys(t *testing.T) {
	d, _, ids := newAnnotationTestDB(t, 1)
	if err := d.ReplacePluginAnnotations("jev", ids[0], []PluginAnnotation{ann("urgency", "high"), ann("needs_reply", "true")}); err != nil {
		t.Fatal(err)
	}
	if err := d.ReplacePluginAnnotations("jev", ids[0], []PluginAnnotation{ann("needs_reply", "false")}); err != nil {
		t.Fatal(err)
	}
	assertKV(t, mustList(t, d, ids[0]), "jev:needs_reply=false")
}

func TestReplacePluginAnnotationsLeavesOtherPlugins(t *testing.T) {
	d, _, ids := newAnnotationTestDB(t, 1)
	if err := d.ReplacePluginAnnotations("other", ids[0], []PluginAnnotation{ann("category", "receipt")}); err != nil {
		t.Fatal(err)
	}
	if err := d.ReplacePluginAnnotations("jev", ids[0], []PluginAnnotation{ann("urgency", "high")}); err != nil {
		t.Fatal(err)
	}
	if err := d.ReplacePluginAnnotations("jev", ids[0], []PluginAnnotation{ann("urgency", "low")}); err != nil {
		t.Fatal(err)
	}
	assertKV(t, mustList(t, d, ids[0]), "jev:urgency=low", "other:category=receipt")
}

func TestReplacePluginAnnotationsEmptyClearsOnlyThatSet(t *testing.T) {
	d, _, ids := newAnnotationTestDB(t, 2)
	for _, id := range ids {
		if err := d.ReplacePluginAnnotations("jev", id, []PluginAnnotation{ann("urgency", "high")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.ReplacePluginAnnotations("other", ids[0], []PluginAnnotation{ann("category", "receipt")}); err != nil {
		t.Fatal(err)
	}
	if err := d.ReplacePluginAnnotations("jev", ids[0], nil); err != nil {
		t.Fatal(err)
	}
	assertKV(t, mustList(t, d, ids[0]), "other:category=receipt")
	assertKV(t, mustList(t, d, ids[1]), "jev:urgency=high")
}

func TestReplacePluginAnnotationsFailureKeepsPriorSet(t *testing.T) {
	d, _, ids := newAnnotationTestDB(t, 1)
	if err := d.ReplacePluginAnnotations("jev", ids[0], []PluginAnnotation{ann("urgency", "high")}); err != nil {
		t.Fatal(err)
	}
	// A duplicate key violates the primary key after the old rows were
	// deleted inside the transaction; the rollback must restore them.
	err := d.ReplacePluginAnnotations("jev", ids[0], []PluginAnnotation{ann("needs_reply", "true"), ann("needs_reply", "false")})
	if err == nil {
		t.Fatal("expected a primary key violation")
	}
	assertKV(t, mustList(t, d, ids[0]), "jev:urgency=high")

	// A message that no longer exists fails the foreign key and writes nothing.
	if err := d.ReplacePluginAnnotations("jev", 999999, []PluginAnnotation{ann("urgency", "high")}); err == nil {
		t.Fatal("expected a foreign key violation for a missing message")
	}
	if err := d.ReplacePluginAnnotations("", ids[0], nil); err == nil {
		t.Fatal("empty plugin id should be refused")
	}
}

func TestListPluginAnnotationsForMessagesBatches(t *testing.T) {
	const n = annotationBatchSize + 25 // crosses a batch boundary
	d, _, ids := newAnnotationTestDB(t, n)
	annotated := []int64{ids[0], ids[annotationBatchSize-1], ids[annotationBatchSize], ids[n-1]}
	for _, id := range annotated {
		if err := d.ReplacePluginAnnotations("jev", id, []PluginAnnotation{ann("urgency", fmt.Sprint(id))}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.ListPluginAnnotationsForMessages(ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(annotated) {
		t.Fatalf("got annotations for %d messages, want %d", len(got), len(annotated))
	}
	for _, id := range annotated {
		if a := got[id]; len(a) != 1 || a[0].Value != fmt.Sprint(id) {
			t.Fatalf("message %d: %+v", id, a)
		}
	}
	if empty, err := d.ListPluginAnnotationsForMessages(nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty id list = %v, %v", empty, err)
	}
}

func TestDeletingMessageRemovesAnnotations(t *testing.T) {
	d, mailboxID, ids := newAnnotationTestDB(t, 3)
	for _, id := range ids {
		if err := d.ReplacePluginAnnotations("jev", id, []PluginAnnotation{ann("urgency", "high")}); err != nil {
			t.Fatal(err)
		}
	}
	countRows := func() int {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM plugin_annotations`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Local delete.
	if err := d.DeleteMessage(ids[0]); err != nil {
		t.Fatal(err)
	}
	if n := countRows(); n != 2 {
		t.Fatalf("rows after DeleteMessage = %d, want 2", n)
	}
	// A message that vanished from the server (UID reconcile).
	if _, err := d.ReconcileMailboxUIDs(mailboxID, []uint32{3}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(); n != 1 {
		t.Fatalf("rows after reconcile = %d, want 1", n)
	}
	// A full cache reset.
	if err := d.ResetMailboxCache(mailboxID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(); n != 0 {
		t.Fatalf("orphaned annotation rows: %d", n)
	}
}

func TestResyncKeepsAnnotations(t *testing.T) {
	d, mailboxID, ids := newAnnotationTestDB(t, 1)
	if err := d.ReplacePluginAnnotations("jev", ids[0], []PluginAnnotation{ann("urgency", "high")}); err != nil {
		t.Fatal(err)
	}
	// Re-syncing the same UID updates the row in place; it must not delete and
	// re-insert it (which would cascade the annotations away).
	if err := d.UpsertMessage(Message{MailboxID: mailboxID, UID: 1, Subject: "edited", Read: true}); err != nil {
		t.Fatal(err)
	}
	assertKV(t, mustList(t, d, ids[0]), "jev:urgency=high")
}

// seedCleanupDB gives two messages annotations from two plugins:
// jev has 2 keys on m0 and 1 on m1; other has 1 key on each.
func seedCleanupDB(t *testing.T) (*DB, []int64) {
	t.Helper()
	d, _, ids := newAnnotationTestDB(t, 2)
	seed := []struct {
		plugin string
		msg    int64
		anns   []PluginAnnotation
	}{
		{"jev", ids[0], []PluginAnnotation{ann("urgency", "high"), ann("needs_reply", "true")}},
		{"jev", ids[1], []PluginAnnotation{ann("urgency", "low")}},
		{"other", ids[0], []PluginAnnotation{ann("category", "receipt")}},
		{"other", ids[1], []PluginAnnotation{ann("category", "github")}},
	}
	for _, s := range seed {
		if err := d.ReplacePluginAnnotations(s.plugin, s.msg, s.anns); err != nil {
			t.Fatal(err)
		}
	}
	return d, ids
}

func messageCount(t *testing.T, d *DB) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDeletePluginAnnotationsOnePluginOneMessage(t *testing.T) {
	d, ids := seedCleanupDB(t)
	if err := d.DeletePluginAnnotations("jev", ids[0]); err != nil {
		t.Fatal(err)
	}
	assertKV(t, mustList(t, d, ids[0]), "other:category=receipt")
	assertKV(t, mustList(t, d, ids[1]), "jev:urgency=low", "other:category=github")
	if n := messageCount(t, d); n != 2 {
		t.Fatalf("messages = %d, want 2", n)
	}
}

func TestDeleteMessagePluginAnnotations(t *testing.T) {
	d, ids := seedCleanupDB(t)
	if err := d.DeleteMessagePluginAnnotations(ids[0]); err != nil {
		t.Fatal(err)
	}
	assertKV(t, mustList(t, d, ids[0]))
	assertKV(t, mustList(t, d, ids[1]), "jev:urgency=low", "other:category=github")
	if n := messageCount(t, d); n != 2 {
		t.Fatalf("messages = %d, want 2", n)
	}
}

func TestDeletePluginAnnotationsForPlugin(t *testing.T) {
	d, ids := seedCleanupDB(t)
	if err := d.DeletePluginAnnotationsForPlugin("jev"); err != nil {
		t.Fatal(err)
	}
	assertKV(t, mustList(t, d, ids[0]), "other:category=receipt")
	assertKV(t, mustList(t, d, ids[1]), "other:category=github")
	if n := messageCount(t, d); n != 2 {
		t.Fatalf("messages = %d, want 2", n)
	}
}

func TestPluginAnnotationCounts(t *testing.T) {
	d, _ := seedCleanupDB(t)
	counts, err := d.PluginAnnotationCounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 2 || counts["jev"] != 3 || counts["other"] != 2 {
		t.Fatalf("counts = %v", counts)
	}
	if err := d.DeletePluginAnnotationsForPlugin("jev"); err != nil {
		t.Fatal(err)
	}
	counts, err = d.PluginAnnotationCounts()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := counts["jev"]; ok || counts["other"] != 2 {
		t.Fatalf("after clearing jev: %v", counts)
	}
	empty := newTestDB(t)
	if counts, err := empty.PluginAnnotationCounts(); err != nil || len(counts) != 0 {
		t.Fatalf("empty db: %v %v", counts, err)
	}
}

func TestAnnotationDeletesOfMissingRowsSucceed(t *testing.T) {
	d, ids := seedCleanupDB(t)
	if err := d.DeletePluginAnnotations("nobody", ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := d.DeletePluginAnnotations("jev", 999999); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteMessagePluginAnnotations(999999); err != nil {
		t.Fatal(err)
	}
	if err := d.DeletePluginAnnotationsForPlugin("nobody"); err != nil {
		t.Fatal(err)
	}
	if counts, _ := d.PluginAnnotationCounts(); counts["jev"] != 3 || counts["other"] != 2 {
		t.Fatalf("counts changed: %v", counts)
	}
	for _, err := range []error{
		d.DeletePluginAnnotations("", ids[0]),
		d.DeleteMessagePluginAnnotations(0),
		d.DeletePluginAnnotationsForPlugin(""),
	} {
		if err == nil {
			t.Fatal("empty identifiers should be refused, not treated as delete-everything")
		}
	}
}
