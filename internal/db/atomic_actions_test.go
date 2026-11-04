package db

import (
	"fmt"
	"testing"
)

func TestDeleteMessageRollsBackEveryStage(t *testing.T) {
	for _, stage := range []string{"tombstone", "attachments", "message"} {
		t.Run(stage, func(t *testing.T) {
			d := newAttentionDB(t)
			id := d.add(d.inbox, "atomic", 1)
			msg, err := d.GetMessage(id)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.SaveAttachment(id, Attachment{Filename: "file", Data: []byte("content")}); err != nil {
				t.Fatal(err)
			}
			target, event := "messages", "DELETE"
			if stage == "attachments" {
				target = "attachments"
			}
			if stage == "tombstone" {
				target, event = "deleted_messages", "INSERT"
			}
			if _, err := d.Exec(fmt.Sprintf(`CREATE TRIGGER fail_atomic BEFORE %s ON %s BEGIN SELECT RAISE(ABORT, 'forced failure'); END`, event, target)); err != nil {
				t.Fatal(err)
			}
			if err := d.DeleteMessage(id); err == nil {
				t.Fatal("delete unexpectedly succeeded")
			}
			if _, err := d.GetMessage(id); err != nil {
				t.Fatal("message disappeared", err)
			}
			var count int
			if err := d.QueryRow(`SELECT count(*) FROM attachments WHERE message_id=?`, id).Scan(&count); err != nil || count != 1 {
				t.Fatalf("attachments count %d err %v", count, err)
			}
			if err := d.QueryRow(`SELECT count(*) FROM messages_fts WHERE rowid=?`, id).Scan(&count); err != nil || count != 1 {
				t.Fatalf("FTS count %d err %v", count, err)
			}
			deleted, err := d.MessageDeletedLocally(msg.MailboxID, msg.UID, msg.MessageID)
			if err != nil || deleted {
				t.Fatalf("failed delete left tombstone: %v %v", deleted, err)
			}
			if _, err := d.Exec(`DROP TRIGGER fail_atomic`); err != nil {
				t.Fatal(err)
			}
			if err := d.DeleteMessage(id); err != nil {
				t.Fatal(err)
			}
			deleted, err = d.MessageDeletedLocally(msg.MailboxID, msg.UID, msg.MessageID)
			if err != nil || !deleted {
				t.Fatalf("successful delete missing tombstone: %v %v", deleted, err)
			}
			if err := d.QueryRow(`SELECT count(*) FROM messages_fts WHERE rowid=?`, id).Scan(&count); err != nil || count != 0 {
				t.Fatal("successful delete left FTS row")
			}
		})
	}
}

func TestDismissalBatchRollsBackOnLaterFailure(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(fmt.Sprint(restore), func(t *testing.T) {
			d := newAttentionDB(t)
			ids := []int64{d.add(d.inbox, "first", 1), d.add(d.inbox, "second", 2)}
			event := "INSERT"
			if restore {
				if err := d.SetNeedsYouDismissedBatch(ids, true); err != nil {
					t.Fatal(err)
				}
				event = "DELETE"
			}
			row := "NEW"
			if restore {
				row = "OLD"
			}
			if _, err := d.Exec(fmt.Sprintf(`CREATE TRIGGER fail_batch BEFORE %s ON message_attention_overrides WHEN %s.message_id = %d BEGIN SELECT RAISE(ABORT, 'forced second failure'); END`, event, row, ids[1])); err != nil {
				t.Fatal(err)
			}
			if err := d.SetNeedsYouDismissedBatch(ids, !restore); err == nil {
				t.Fatal("batch unexpectedly succeeded")
			}
			dismissed, err := d.DismissedFromNeedsYou()
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range ids {
				if dismissed[id] != restore {
					t.Fatalf("partial write: %v", dismissed)
				}
			}
			if _, err := d.Exec(`DROP TRIGGER fail_batch`); err != nil {
				t.Fatal(err)
			}
			if err := d.SetNeedsYouDismissedBatch(ids, !restore); err != nil {
				t.Fatal(err)
			}
			dismissed, err = d.DismissedFromNeedsYou()
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range ids {
				if dismissed[id] == restore {
					t.Fatalf("retry did not apply entire batch: %v", dismissed)
				}
			}
		})
	}
}
