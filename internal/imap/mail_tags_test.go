package imap

import (
	"context"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/mailtag"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

func TestMailTagRoundTripBetweenClients(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()
	uid := appendTestMessage(t, port, "INBOX", true)
	ctx := context.Background()
	cfg := config.AccountConfig{IMAPHost: "127.0.0.1", IMAPPort: port, User: "testuser", Password: "testpass"}
	first, second := New(cfg), New(cfg)
	for _, client := range []*Client{first, second} {
		if err := client.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		defer client.Close()
	}
	state, validity, err := first.ServerState(ctx, "INBOX")
	if err != nil || len(state) != 1 {
		t.Fatalf("state: %+v %v", state, err)
	}
	if err := first.MarkFlagged(ctx, "INBOX", uid, true); err != nil {
		t.Fatal(err)
	}
	for _, keyword := range []string{"$label1", "Project-X"} {
		flags, err := first.SetKeyword(ctx, "INBOX", uid, validity, keyword, true)
		if err != nil {
			t.Fatal(err)
		}
		if !mailtag.Has(flags, keyword) || !mailtag.Has(flags, `\Seen`) || !mailtag.Has(flags, `\Flagged`) {
			t.Fatalf("flags lost: %q", flags)
		}
	}
	state, _, err = second.ServerState(ctx, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if !mailtag.Has(state[0].Flags, "$label1") || !mailtag.Has(state[0].Flags, "project-x") {
		t.Fatalf("other client cannot see tags: %+v", state)
	}
	flags, err := second.SetKeyword(ctx, "INBOX", uid, validity, "$label1", false)
	if err != nil {
		t.Fatal(err)
	}
	if mailtag.Has(flags, "$label1") || !mailtag.Has(flags, "Project-X") {
		t.Fatalf("removal damaged tags: %q", flags)
	}
	state, _, err = first.ServerState(ctx, "INBOX")
	if err != nil || mailtag.Has(state[0].Flags, "$label1") {
		t.Fatalf("removal not visible: %+v %v", state, err)
	}
	if _, err := first.SetKeyword(ctx, "INBOX", uid, validity+1, "Project-X", false); err == nil {
		t.Fatal("accepted stale UIDVALIDITY")
	}
	state, _, _ = first.ServerState(ctx, "INBOX")
	if !mailtag.Has(state[0].Flags, "Project-X") {
		t.Fatal("stale UIDVALIDITY changed a tag")
	}
	if _, err := first.SetKeyword(ctx, "INBOX", uid+100, validity, "$label1", true); err == nil {
		t.Fatal("claimed success for missing message")
	}
	for _, keyword := range []string{`\Seen`, "bad tag"} {
		if _, err := first.SetKeyword(ctx, "INBOX", uid, validity, keyword, true); err == nil {
			t.Fatalf("accepted %q", keyword)
		}
	}
}

func TestMailTagPermanentFlags(t *testing.T) {
	for _, test := range []struct {
		flags   []imap.Flag
		keyword string
		want    bool
	}{
		{nil, "$label1", false},
		{[]imap.Flag{imap.FlagSeen, imap.FlagFlagged}, "$label1", false},
		{[]imap.Flag{"$label1"}, "$LABEL1", true},
		{[]imap.Flag{"$label1"}, "custom", false},
		{[]imap.Flag{imap.FlagWildcard}, "custom", true},
	} {
		if got := keywordWritable(test.flags, test.keyword); got != test.want {
			t.Errorf("%v / %q = %v", test.flags, test.keyword, got)
		}
	}
}

type restrictedTagSession struct {
	imapserver.Session
	conn *imapserver.Conn
	mode string
}

func (s *restrictedTagSession) Select(mailbox string, options *imap.SelectOptions) (*imap.SelectData, error) {
	data, err := s.Session.Select(mailbox, options)
	if err == nil && s.mode == "unsupported" {
		data.PermanentFlags = []imap.Flag{imap.FlagSeen}
	}
	return data, err
}

func (s *restrictedTagSession) Store(w *imapserver.FetchWriter, nums imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	if s.mode == "read-only" {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "mailbox is read-only"}
	}
	err := s.Session.Store(w, nums, flags, options)
	if err == nil && s.mode == "disconnect" {
		_ = s.conn.NetConn().Close()
	}
	return err
}

func TestMailTagRejectedOrUnconfirmedWrites(t *testing.T) {
	for _, mode := range []string{"unsupported", "read-only", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			port, cleanup := startTestServerWithSession(t, func(session imapserver.Session, conn *imapserver.Conn) imapserver.Session {
				return &restrictedTagSession{Session: session, conn: conn, mode: mode}
			})
			defer cleanup()
			uid := appendTestMessage(t, port, "INBOX", true)
			client := newAppendTestClient(t, port)
			ctx := context.Background()
			_, validity, err := client.ServerState(ctx, "INBOX")
			if err != nil {
				t.Fatal(err)
			}
			flags, err := client.SetKeyword(ctx, "INBOX", uid, validity, "$label1", true)
			if err == nil || flags != nil {
				t.Fatalf("claimed success: flags=%v err=%v", flags, err)
			}
			observer := newAppendTestClient(t, port)
			state, _, err := observer.ServerState(ctx, "INBOX")
			if err != nil {
				t.Fatal(err)
			}
			wantApplied := mode == "disconnect"
			if mailtag.Has(state[0].Flags, "$label1") != wantApplied {
				t.Fatalf("server state = %+v, want tag applied=%v", state, wantApplied)
			}
		})
	}
}
