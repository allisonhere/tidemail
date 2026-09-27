package pluginquery

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/conversation"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
)

// benchHarness is a cache of n messages over the year before testNow: an
// inbox and a Sent folder, threads of about four messages, and a category
// annotation on most received mail.
func benchHarness(b *testing.B, n int) *harness {
	b.Helper()
	database, err := db.OpenPath(filepath.Join(b.TempDir(), "mail.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { database.Close() })
	acc, err := database.AddAccount("", "Bench", "")
	if err != nil {
		b.Fatal(err)
	}
	inbox, _ := database.UpsertMailbox(db.Mailbox{AccountID: acc, Name: "INBOX", Delimiter: "/"})
	sent, _ := database.UpsertMailbox(db.Mailbox{AccountID: acc, Name: "Sent", Delimiter: "/", Flags: []string{`\Sent`}})

	tx, err := database.Begin()
	if err != nil {
		b.Fatal(err)
	}
	msgStmt, err := tx.Prepare(`INSERT INTO messages (mailbox_id, uid, message_id, in_reply_to, references_text,
		subject, from_addr, to_addr, date, read, flags) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '[]')`)
	if err != nil {
		b.Fatal(err)
	}
	annStmt, err := tx.Prepare(`INSERT INTO plugin_annotations (plugin_id, message_id, key, value, updated_at) VALUES ('bench', ?, 'category', ?, 0)`)
	if err != nil {
		b.Fatal(err)
	}
	categories := []string{"work", "github", "newsletter", "billing", "personal"}
	for i := 0; i < n; i++ {
		thread, pos := i/4, i%4
		fromMe := pos%2 == 1
		mailbox, from, to := inbox, fmt.Sprintf("Person %d <p%d@d%d.example>", thread%500, thread%500, thread%40), "me@example.com"
		if fromMe {
			mailbox, from, to = sent, "me@example.com", fmt.Sprintf("p%d@d%d.example", thread%500, thread%40)
		}
		msgID := fmt.Sprintf("<t%d.%d@bench>", thread, pos)
		parent := ""
		if pos > 0 {
			parent = fmt.Sprintf("<t%d.%d@bench>", thread, pos-1)
		}
		date := testNow.Add(-time.Duration(n-i) * (365 * 24 * time.Hour / time.Duration(n)))
		res, err := msgStmt.Exec(mailbox, i+1, msgID, parent, parent, fmt.Sprintf("Subject %d", thread), from, to, date.Unix(), i%3 == 0)
		if err != nil {
			b.Fatal(err)
		}
		if !fromMe && i%10 < 7 {
			id, _ := res.LastInsertId()
			if _, err := annStmt.Exec(id, categories[thread%len(categories)]); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	return &harness{db: database,
		exec: &Executor{DB: database, Me: conversation.MyAddresses("me@example.com"), Location: time.UTC},
		rc:   plugin.ReportContext{AccountName: "Bench", MailboxName: "INBOX", Now: testNow.Format(time.RFC3339), Timezone: "UTC"},
	}
}

func benchQuery(b *testing.B, h *harness, raw string) {
	b.Helper()
	q, err := plugin.ParseQuery("q", json.RawMessage(raw))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, err := h.exec.ExecuteQuery(context.Background(), q, h.rc)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := json.Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQueryMessages(b *testing.B) {
	h := benchHarness(b, 50_000)
	for _, limit := range []int{100, 500} {
		b.Run(fmt.Sprintf("rows=%d/cache=50k", limit), func(b *testing.B) {
			benchQuery(b, h, fmt.Sprintf(`{"method":"query.messages","scope":"all_cached","fields":["id","sender","subject","date","read"],"limit":%d}`, limit))
		})
	}
}

func BenchmarkAnalytics(b *testing.B) {
	for _, n := range []int{10_000, 50_000} {
		h := benchHarness(b, n)
		size := fmt.Sprintf("cache=%dk", n/1000)
		b.Run("volume/"+size, func(b *testing.B) {
			benchQuery(b, h, `{"method":"analytics.volume","range":"365d","group_by":"day"}`)
		})
		b.Run("categories/"+size, func(b *testing.B) {
			benchQuery(b, h, `{"method":"analytics.categories","range":"365d"}`)
		})
		b.Run("threads/"+size, func(b *testing.B) {
			benchQuery(b, h, `{"method":"query.threads","scope":"all_cached","fields":["thread_id","message_count","latest_date","latest_from_me"],"limit":100}`)
		})
	}
}
