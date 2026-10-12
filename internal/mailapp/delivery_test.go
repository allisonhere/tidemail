package mailapp

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/smtp"
)

func fixture(t *testing.T) (*db.DB, config.AccountConfig, int64) {
	t.Helper()
	database, err := db.OpenPath(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	a := config.AccountConfig{ID: "sender", Name: "Sender", User: "sender@example.com", From: "sender@example.com"}
	id, err := Enqueue(database, config.Config{}, a, smtp.OutgoingMessage{To: []string{"to@example.com"}, BCC: []string{"secret@example.com"}, Subject: "subject", Body: "body"}, db.Draft{}, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return database, a, id
}
func TestDeliveryRecordsSuccessBeforeSentAppend(t *testing.T) {
	database, a, id := fixture(t)
	var identity string
	d := Delivery{Send: func(_ context.Context, _ config.AccountConfig, m smtp.OutgoingMessage) error {
		identity = m.MessageID
		return nil
	}, Append: func(database *db.DB, _ *imapClient.SessionPool, _ config.AccountConfig, m smtp.OutgoingMessage) error {
		item, err := database.GetOutbox(id)
		if err != nil {
			t.Fatal(err)
		}
		if item.State != db.OutboxSent {
			t.Fatalf("append before durable success: %s", item.State)
		}
		if identity == "" || identity != m.MessageID {
			t.Fatal("identity mismatch")
		}
		return errors.New("append failed")
	}}
	r := d.Deliver(context.Background(), database, nil, id, a, false, nil)
	if r.Err != nil || r.SentCopyErr == nil {
		t.Fatalf("result: %+v", r)
	}
	item, _ := database.GetOutbox(id)
	if item.Recipients != "to@example.com" {
		t.Fatalf("BCC leaked into summary: %q", item.Recipients)
	}
}
func TestUncertainDeliveryNeverAutomaticallyRetries(t *testing.T) {
	database, a, id := fixture(t)
	d := Delivery{Send: func(context.Context, config.AccountConfig, smtp.OutgoingMessage) error {
		return smtp.ErrDeliveryUncertain
	}}
	r := d.Deliver(context.Background(), database, nil, id, a, false, nil)
	if !errors.Is(r.Err, smtp.ErrDeliveryUncertain) {
		t.Fatalf("result %+v", r)
	}
	item, _ := database.GetOutbox(id)
	if item.State != db.OutboxUncertain {
		t.Fatalf("state %s", item.State)
	}
	if r = d.Deliver(context.Background(), database, nil, id, a, false, nil); !r.Skipped {
		t.Fatalf("retried uncertain delivery: %+v", r)
	}
}
func TestConcurrentDeliveryAndFutureScheduling(t *testing.T) {
	database, a, id := fixture(t)
	_, err := database.Exec(`UPDATE outbox SET next_attempt=? WHERE id=?`, time.Now().Add(time.Hour).Unix(), id)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	d := Delivery{Send: func(context.Context, config.AccountConfig, smtp.OutgoingMessage) error { calls.Add(1); return nil }}
	if r := d.Deliver(context.Background(), database, nil, id, a, false, nil); !r.Skipped {
		t.Fatalf("future mail attempted: %+v", r)
	}
	_, err = database.Exec(`UPDATE outbox SET next_attempt=0 WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); d.Deliver(context.Background(), database, nil, id, a, false, nil) }()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("SMTP calls: %d", calls.Load())
	}
}
func TestRecoveryDoesNotInterruptLiveWorker(t *testing.T) {
	database, _, id := fixture(t)
	ok, err := database.ClaimDueOutbox(id)
	if err != nil || !ok {
		t.Fatalf("claim %v %v", ok, err)
	}
	if err = database.RecoverStaleOutbox(); err != nil {
		t.Fatal(err)
	}
	item, _ := database.GetOutbox(id)
	if item.State != db.OutboxSending {
		t.Fatalf("live worker recovered: %s", item.State)
	}
	_, err = database.Exec(`UPDATE outbox SET updated_at=? WHERE id=?`, time.Now().Add(-3*time.Minute).Unix(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.RecoverStaleOutbox(); err != nil {
		t.Fatal(err)
	}
	item, _ = database.GetOutbox(id)
	if item.State != db.OutboxUncertain {
		t.Fatalf("stale state: %s", item.State)
	}
}
func TestGmailSentCopiesAreSkipped(t *testing.T) {
	for _, a := range []config.AccountConfig{{Provider: "Gmail"}, {SMTPHost: "smtp.gmail.com"}, {SMTPHost: "smtp-relay.gmail.com"}} {
		if !ServerFilesSentMail(a) {
			t.Fatalf("Gmail unrecognized: %+v", a)
		}
	}
	if ServerFilesSentMail(config.AccountConfig{SMTPHost: "mail.example.com"}) {
		t.Fatal("ordinary SMTP treated as Gmail")
	}
}

func TestDeliveryAcrossDatabaseConnectionsOnlySendsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	first, err := db.OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := db.OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	a := config.AccountConfig{ID: "sender", User: "sender@example.com"}
	id, err := Enqueue(first, config.Config{}, a, smtp.OutgoingMessage{To: []string{"to@example.com"}, Body: "body"}, db.Draft{}, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	d := Delivery{Send: func(context.Context, config.AccountConfig, smtp.OutgoingMessage) error { calls.Add(1); return nil }}
	var wg sync.WaitGroup
	for _, database := range []*db.DB{first, second} {
		wg.Add(1)
		go func(database *db.DB) {
			defer wg.Done()
			r := d.Deliver(context.Background(), database, nil, id, a, false, nil)
			if r.Err != nil {
				t.Errorf("delivery: %v", r.Err)
			}
		}(database)
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("SMTP calls across processes: %d", calls.Load())
	}
}

func TestRetryPersistsIdentityAndResolvesCorrectedSender(t *testing.T) {
	database, a, id := fixture(t)
	if _, err := database.Exec(`UPDATE outbox SET max_attempts=3 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	var identities []string
	d := Delivery{Send: func(_ context.Context, _ config.AccountConfig, m smtp.OutgoingMessage) error {
		identities = append(identities, m.MessageID)
		if len(identities) == 1 {
			return errors.New("temporary outage")
		}
		if m.From != "corrected@example.com" {
			t.Errorf("sender was not resolved again: %q", m.From)
		}
		return nil
	}}
	first := d.Deliver(context.Background(), database, nil, id, a, false, nil)
	if first.Err == nil {
		t.Fatal("expected temporary error")
	}
	item, _ := database.GetOutbox(id)
	if item.State != db.OutboxQueued || item.NextAttempt <= time.Now().Unix() {
		t.Fatalf("retry not durably delayed: %+v", item)
	}
	if r := d.Deliver(context.Background(), database, nil, id, a, false, nil); !r.Skipped {
		t.Fatalf("retry delay ignored: %+v", r)
	}
	a.From = "corrected@example.com"
	second := d.Deliver(context.Background(), database, nil, id, a, true, nil)
	if second.Err != nil {
		t.Fatal(second.Err)
	}
	if len(identities) != 2 || identities[0] == "" || identities[0] != identities[1] {
		t.Fatalf("identity changed: %v", identities)
	}
}
