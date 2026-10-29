package mailapp

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/smtp"
)

func TestPrepareComposeMarker(t *testing.T) {
	a := config.AccountConfig{User: "sender@example.com", Signature: "Alice <alice@example.com>\nR&D <script>\nSecond line"}
	for _, tc := range []struct {
		name, body string
		bad        bool
	}{
		{"no marker", "# Hello", false}, {"marker", "Hello\n\n⁂\n\nOn sender wrote:\n> Original\n", false},
		{"whitespace", "Hello\n\n ⁂ \t\n", false}, {"crlf", "Hello\r\n\r\n⁂\r\n", false},
		{"only", "⁂", false}, {"unicode", "世界\n\n⁂\n", false},
		{"duplicates", "⁂\n\n⁂", true}, {"inline", "Hello ⁂ world", true}, {"quote", "> ⁂", true},
		{"fence", "```\n⁂\n```", true}, {"indent", "    ⁂", true}, {"list", "- ⁂", true},
		{"paragraph", "Hello\n⁂", true}, {"heading", "⁂\n---", true},
		{"html", "<div>\n⁂\n</div>", true}, {"entity", "&#x2042;", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := db.Draft{To: "reader@example.com", BodyText: tc.body}
			msg, err := PrepareCompose(a, draft)
			if tc.bad {
				if err == nil {
					t.Fatalf("accepted reserved marker: %#v", msg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if draft.BodyText != tc.body {
				t.Fatal("draft mutated")
			}
			if strings.Contains(msg.Body+msg.HTMLBody, SignatureMarker) {
				t.Fatal("marker leaked")
			}
			if !strings.Contains(tc.body, SignatureMarker) {
				if msg.Body != tc.body || strings.Contains(msg.HTMLBody, "Alice") {
					t.Fatal("unsigned body changed")
				}
				return
			}
			if strings.Count(msg.Body, a.Signature) != 1 || !strings.Contains(msg.HTMLBody, "Alice &lt;alice@example.com&gt;<br>") || strings.Contains(msg.HTMLBody, "<script>") {
				t.Fatalf("formatting: %#v", msg)
			}
			if strings.Contains(tc.body, "Original") && (strings.Index(msg.Body, "Alice") > strings.Index(msg.Body, "> Original") || strings.Index(msg.HTMLBody, "Alice") > strings.Index(msg.HTMLBody, "<blockquote>")) {
				t.Fatal("signature below quote")
			}
			assertComposeMIME(t, a, msg)
		})
	}
}

// Check decoded wire alternatives, not just the signature formatter's return.
func assertComposeMIME(t *testing.T, a config.AccountConfig, msg smtp.OutgoingMessage) {
	t.Helper()
	m, e := mail.ReadMessage(strings.NewReader(string(smtp.BuildRaw(a, msg))))
	if e != nil {
		t.Fatal(e)
	}
	_, params, e := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if e != nil {
		t.Fatal(e)
	}
	mr := multipart.NewReader(m.Body, params["boundary"])
	parts := map[string]string{}
	for {
		p, e := mr.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		typ, _, e := mime.ParseMediaType(p.Header.Get("Content-Type"))
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(p)
		if e != nil {
			t.Fatal(e)
		}
		parts[typ] = strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	if parts["text/plain"] != msg.Body || parts["text/html"] != msg.HTMLBody {
		t.Fatalf("MIME changed bodies: %#v", parts)
	}
	for _, body := range parts {
		if strings.Contains(body, SignatureMarker) || strings.Contains(body, "Signature appended on send") {
			t.Fatal("placeholder leaked into MIME")
		}
	}
}

func TestPrepareComposeWholeDocumentMarkdown(t *testing.T) {
	a := config.AccountConfig{User: "sender@example.com", Signature: "Alice\nExample Org"}
	body := "[reference][id]\n\n⁂\n\n> quoted **text**\n\n[id]: https://example.com\n"
	msg, e := PrepareCompose(a, db.Draft{To: "reader@example.com", BodyText: body})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(msg.HTMLBody, `href="https://example.com"`) || !strings.Contains(msg.HTMLBody, "<strong>text</strong>") {
		t.Fatal(msg.HTMLBody)
	}
	if want := strings.Replace(body, "⁂\n", "-- \nAlice\nExample Org\n", 1); msg.Body != want {
		t.Fatalf("body %q want %q", msg.Body, want)
	}
	assertComposeMIME(t, a, msg)
	for _, signature := range []string{"", " \n\t"} {
		a.Signature = signature
		m, e := PrepareCompose(a, db.Draft{To: "reader@example.com", BodyText: body})
		if e != nil || strings.Contains(m.Body, "-- ") || strings.Contains(m.HTMLBody, "<div>") || strings.Contains(m.Body, SignatureMarker) {
			t.Fatalf("empty signature: %#v %v", m, e)
		}
	}
	a.Signature = "contains ⁂"
	if _, e := PrepareCompose(a, db.Draft{To: "reader@example.com", BodyText: "⁂"}); e == nil {
		t.Fatal("signature rune leaked")
	}
}

func TestPrepareComposeQueueRetryAndRestore(t *testing.T) {
	database, e := db.OpenPath(filepath.Join(t.TempDir(), "mail.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	a := config.AccountConfig{ID: "sender", User: "sender@example.com", Signature: "Original signature"}
	draft := db.Draft{To: "reader@example.com", BodyText: "Response\n\n⁂\n\n> Original"}
	msg, e := PrepareCompose(a, draft)
	if e != nil {
		t.Fatal(e)
	}
	cfg := config.DefaultConfig()
	cfg.Display.SendMaxAttempts = 3
	id, e := Enqueue(database, cfg, a, msg, draft, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	a.Signature = "Changed signature"
	calls := 0
	delivery := Delivery{Send: func(_ context.Context, _ config.AccountConfig, m smtp.OutgoingMessage) error {
		calls++
		if m.Body != msg.Body || m.HTMLBody != msg.HTMLBody {
			t.Fatal("retry re-prepared body")
		}
		if calls == 1 {
			return errors.New("temporary outage")
		}
		return nil
	}, Append: func(*db.DB, *imapClient.SessionPool, config.AccountConfig, smtp.OutgoingMessage) error { return nil }}
	if r := delivery.Deliver(context.Background(), database, nil, id, a, false, nil); !r.Skipped || calls != 0 {
		t.Fatal("future schedule sent")
	}
	if r := delivery.Deliver(context.Background(), database, nil, id, a, true, nil); r.Err == nil {
		t.Fatal("missing retry error")
	}
	if r := delivery.Deliver(context.Background(), database, nil, id, a, true, nil); r.Err != nil || calls != 2 {
		t.Fatalf("retry failed: %+v", r)
	}
	id, e = Enqueue(database, cfg, a, msg, draft, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	restored, e := database.EditOutbox(id)
	if e != nil || restored.BodyText != draft.BodyText {
		t.Fatalf("restored %#v %v", restored, e)
	}
	// Headless callers still own their explicit append choice, including literal
	// rune text: it is reserved only in interactive composition.
	for _, appendSig := range []bool{false, true} {
		m, e := Prepare(a, draft, appendSig)
		if e != nil || !strings.Contains(m.Body, SignatureMarker) || strings.Contains(m.Body, a.Signature) != appendSig {
			t.Fatalf("CLI behavior changed: %#v %v", m, e)
		}
	}
}

func TestPrepareComposeBeforePGP(t *testing.T) {
	a := config.AccountConfig{User: "sender@example.com", Signature: "PGP account signature"}
	msg, err := PrepareCompose(a, db.Draft{To: "reader@example.com", BodyText: "Response\n\n⁂\n\n> Original"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Protect(context.Background(), func(_ context.Context, request plugin.PrepareRequest) (plugin.PrepareResult, error) {
		entity := string(request.Entity)
		if strings.Contains(entity, SignatureMarker) || !strings.Contains(entity, a.Signature) {
			t.Fatalf("PGP input was not prepared: %s", entity)
		}
		return plugin.PrepareResult{Signature: []byte(armoredSig), MicAlg: "pgp-sha256"}, nil
	}, msg, true, false)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(smtp.BuildRaw(a, out))
	if !strings.Contains(raw, "multipart/signed") || strings.Contains(raw, SignatureMarker) || !strings.Contains(raw, a.Signature) {
		t.Fatalf("protected MIME lost prepared signature: %s", raw)
	}
}
