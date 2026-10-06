package mailcore

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	imapgo "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

const (
	testUser = "testuser"
	testPass = "testpass"
)

// startIMAP runs an in-memory IMAP server with a few empty mailboxes and returns an
// Account pointing at it.
func startIMAP(t *testing.T) Account {
	t.Helper()
	mem := imapmemserver.New()
	user := imapmemserver.NewUser(testUser, testPass)
	mem.AddUser(user)
	for _, name := range []string{"INBOX", "Sent", "Archive", "Drafts", "Trash"} {
		if err := user.Create(name, nil); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		InsecureAuth: true,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close(); ln.Close() })
	return Account{
		IMAPHost: "127.0.0.1",
		IMAPPort: ln.Addr().(*net.TCPAddr).Port,
		User:     testUser,
		Password: testPass,
	}
}

// seed appends a message to mailbox over a separate connection and returns its UID.
func seed(t *testing.T, account Account, mailbox, subject, body string, seen bool) uint32 {
	t.Helper()
	conn, err := net.Dial("tcp", net.JoinHostPort(account.IMAPHost, strconv.Itoa(account.IMAPPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := imapclient.New(conn, nil)
	defer c.Close()
	if err := c.Login(testUser, testPass).Wait(); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf("From: Alice <alice@example.test>\r\nTo: bob@example.test\r\nSubject: %s\r\nMessage-ID: <%s@example.test>\r\nDate: Mon, 02 Jan 2006 15:04:05 +0000\r\n\r\n%s", subject, strings.ReplaceAll(subject, " ", "-"), body)
	var flags []imapgo.Flag
	if seen {
		flags = append(flags, imapgo.FlagSeen)
	}
	cmd := c.Append(mailbox, int64(len(raw)), &imapgo.AppendOptions{Flags: flags})
	if _, err := cmd.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := cmd.Wait()
	if err != nil {
		t.Fatal(err)
	}
	return uint32(data.UID)
}

func connect(t *testing.T, account Account) *Client {
	t.Helper()
	c := NewClient(account)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func uids(msgs []Message) []uint32 {
	out := make([]uint32, len(msgs))
	for i, m := range msgs {
		out[i] = m.UID
	}
	return out
}

func TestConnectRejectsBadPassword(t *testing.T) {
	account := startIMAP(t)
	account.Password = "wrong"
	if err := NewClient(account).Connect(context.Background()); err == nil {
		t.Fatal("expected a bad password to be refused")
	}
}

func TestListMailboxes(t *testing.T) {
	account := startIMAP(t)
	c := connect(t, account)
	boxes, err := c.ListMailboxes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, b := range boxes {
		got[b.Name] = true
	}
	for _, want := range []string{"INBOX", "Sent", "Archive", "Drafts", "Trash"} {
		if !got[want] {
			t.Errorf("mailbox %q missing from %v", want, boxes)
		}
	}
}

func TestFetchMessageProjectsFields(t *testing.T) {
	account := startIMAP(t)
	uid := seed(t, account, "INBOX", "Hello there", "the body text", false)
	c := connect(t, account)

	msg, err := c.FetchMessage(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if msg.UID != uid || msg.Subject != "Hello there" || msg.Starred {
		t.Fatalf("unexpected message: %+v", msg)
	}
	if !strings.Contains(msg.From, "alice@example.test") || !strings.Contains(msg.To, "bob@example.test") {
		t.Errorf("from/to not projected: from=%q to=%q", msg.From, msg.To)
	}
	if !strings.Contains(msg.BodyText, "the body text") {
		t.Errorf("body not projected: %q", msg.BodyText)
	}
	if msg.MessageID == "" {
		t.Error("message ID not projected")
	}
	if msg.Attachments == nil {
		t.Error("Attachments should be an empty slice, not nil, for gomobile callers")
	}

	if _, err := c.FetchMessage(context.Background(), uid+100); err == nil {
		t.Error("fetching a UID that does not exist should fail")
	}
}

func TestFetchInboxAndMailboxNewestFirstWithLimit(t *testing.T) {
	account := startIMAP(t)
	first := seed(t, account, "INBOX", "one", "a", false)
	second := seed(t, account, "INBOX", "two", "b", false)
	third := seed(t, account, "INBOX", "three", "c", false)
	seed(t, account, "Sent", "sent one", "x", true)
	c := connect(t, account)

	all, err := c.FetchInbox(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d messages, want 3: %v", len(all), uids(all))
	}
	seen := map[uint32]bool{}
	for _, m := range all {
		seen[m.UID] = true
	}
	for _, uid := range []uint32{first, second, third} {
		if !seen[uid] {
			t.Errorf("uid %d missing from %v", uid, uids(all))
		}
	}

	limited, err := c.FetchInbox(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 {
		t.Fatalf("limit 2 returned %d messages", len(limited))
	}
	for _, m := range limited {
		if m.UID == first {
			t.Errorf("the oldest message should be cut by the limit, got %v", uids(limited))
		}
	}

	sent, err := c.FetchMailbox(context.Background(), "Sent", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].Subject != "sent one" || !sent[0].Read {
		t.Fatalf("unexpected Sent contents: %+v", sent)
	}
	if _, err := c.FetchMailbox(context.Background(), "NoSuchBox", 10); err == nil {
		t.Error("fetching a missing mailbox should fail")
	}
}

func TestFetchOlderMailboxPagesByUID(t *testing.T) {
	account := startIMAP(t)
	var all []uint32
	for i := 0; i < 5; i++ {
		all = append(all, seed(t, account, "INBOX", fmt.Sprintf("msg %d", i), "b", false))
	}
	c := connect(t, account)

	older, err := c.FetchOlderInbox(context.Background(), all[3], 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(older) != 3 {
		t.Fatalf("got %v, want the 3 messages below UID %d", uids(older), all[3])
	}
	for _, m := range older {
		if m.UID >= all[3] {
			t.Errorf("uid %d is not older than %d", m.UID, all[3])
		}
	}

	page, err := c.FetchOlderMailbox(context.Background(), "INBOX", all[3], 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("limit 2 returned %v", uids(page))
	}

	none, err := c.FetchOlderInbox(context.Background(), all[0], 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("nothing is older than the oldest message, got %v", uids(none))
	}
}

func TestSearchMailbox(t *testing.T) {
	account := startIMAP(t)
	hit := seed(t, account, "INBOX", "invoice for march", "please pay", false)
	seed(t, account, "INBOX", "lunch", "sandwiches", false)
	c := connect(t, account)

	got, err := c.SearchMailbox(context.Background(), "INBOX", "invoice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].UID != hit {
		t.Fatalf("search returned %v, want just uid %d", uids(got), hit)
	}
	got, err = c.SearchMailbox(context.Background(), "INBOX", "zzz-no-match", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a search with no hits should be empty, got %v", uids(got))
	}
}

func TestSetReadAndSetFlagged(t *testing.T) {
	account := startIMAP(t)
	uid := seed(t, account, "INBOX", "flags", "b", false)
	other := seed(t, account, "Archive", "elsewhere", "b", false)
	c := connect(t, account)
	ctx := context.Background()

	flags := func(mailbox string, uid uint32) map[string]bool {
		return flagsOf(t, account, mailbox, uid)
	}

	if err := c.SetRead(ctx, uid, true); err != nil {
		t.Fatal(err)
	}
	if !flags("INBOX", uid)[`\seen`] {
		t.Error("message should be read")
	}
	if err := c.SetRead(ctx, uid, false); err != nil {
		t.Fatal(err)
	}
	if flags("INBOX", uid)[`\seen`] {
		t.Error("message should be unread again")
	}

	if err := c.SetFlagged(ctx, uid, true); err != nil {
		t.Fatal(err)
	}
	if !flags("INBOX", uid)[`\flagged`] {
		t.Error("message should be starred")
	}
	if err := c.SetFlagged(ctx, uid, false); err != nil {
		t.Fatal(err)
	}
	if flags("INBOX", uid)[`\flagged`] {
		t.Error("message should be unstarred")
	}

	// The *In variants act on the named mailbox, not INBOX. Both mailboxes hold UID 1.
	if other != uid {
		t.Fatalf("test needs the same UID in both mailboxes, got %d and %d", uid, other)
	}
	if err := c.SetReadIn(ctx, "Archive", other, true); err != nil {
		t.Fatal(err)
	}
	if err := c.SetFlaggedIn(ctx, "Archive", other, true); err != nil {
		t.Fatal(err)
	}
	if got := flags("Archive", other); !got[`\seen`] || !got[`\flagged`] {
		t.Errorf("Archive message should be read and starred: %v", got)
	}
	if got := flags("INBOX", uid); got[`\seen`] || got[`\flagged`] {
		t.Errorf("changing Archive must not touch the INBOX message with the same UID: %v", got)
	}
}

func TestMoveAndMoveFrom(t *testing.T) {
	account := startIMAP(t)
	uid := seed(t, account, "INBOX", "to archive", "b", false)
	c := connect(t, account)
	ctx := context.Background()

	if err := c.Move(ctx, uid, "Archive"); err != nil {
		t.Fatal(err)
	}
	inbox, _ := c.FetchInbox(ctx, 10)
	if len(inbox) != 0 {
		t.Fatalf("INBOX should be empty after the move, got %v", uids(inbox))
	}
	archived, err := c.FetchMailbox(ctx, "Archive", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 || archived[0].Subject != "to archive" {
		t.Fatalf("Archive should hold the moved message: %+v", archived)
	}

	if err := c.MoveFrom(ctx, "Archive", archived[0].UID, "Trash"); err != nil {
		t.Fatal(err)
	}
	trash, _ := c.FetchMailbox(ctx, "Trash", 10)
	if len(trash) != 1 {
		t.Fatalf("Trash should hold the message, got %v", uids(trash))
	}

	if err := c.Move(ctx, seed(t, account, "INBOX", "x", "b", false), "NoSuchBox"); err == nil {
		t.Error("moving to a missing mailbox should fail")
	}
}

func TestDeleteAndDeleteFrom(t *testing.T) {
	account := startIMAP(t)
	keep := seed(t, account, "INBOX", "keep", "b", false)
	gone := seed(t, account, "INBOX", "gone", "b", false)
	goneSent := seed(t, account, "Sent", "gone from sent", "b", true)
	c := connect(t, account)
	ctx := context.Background()

	if err := c.Delete(ctx, gone); err != nil {
		t.Fatal(err)
	}
	inbox, _ := c.FetchInbox(ctx, 10)
	if len(inbox) != 1 || inbox[0].UID != keep {
		t.Fatalf("only the kept message should remain: %v", uids(inbox))
	}

	if err := c.DeleteFrom(ctx, "Sent", goneSent); err != nil {
		t.Fatal(err)
	}
	sent, _ := c.FetchMailbox(ctx, "Sent", 10)
	if len(sent) != 0 {
		t.Errorf("Sent should be empty, got %v", uids(sent))
	}
}

func TestUnseenCounts(t *testing.T) {
	account := startIMAP(t)
	seed(t, account, "INBOX", "a", "b", false)
	seed(t, account, "INBOX", "b", "b", false)
	seed(t, account, "INBOX", "c", "b", true)
	seed(t, account, "Archive", "d", "b", true)
	c := connect(t, account)

	counts, err := c.UnseenCounts(context.Background(), []string{"INBOX", "Archive", "NoSuchBox"})
	if err != nil {
		t.Fatal(err)
	}
	if counts["INBOX"] != 2 || counts["Archive"] != 0 {
		t.Errorf("counts = %v, want INBOX 2 and Archive 0", counts)
	}
	if _, ok := counts["NoSuchBox"]; ok {
		t.Error("a mailbox the server will not report on must be missing, not zero")
	}
}

func TestMailboxCreateRenameDelete(t *testing.T) {
	account := startIMAP(t)
	c := connect(t, account)
	ctx := context.Background()

	names := func() map[string]bool {
		t.Helper()
		boxes, err := c.ListMailboxes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, b := range boxes {
			out[b.Name] = true
		}
		return out
	}

	if err := c.CreateMailbox(ctx, "Projects"); err != nil {
		t.Fatal(err)
	}
	if !names()["Projects"] {
		t.Fatal("Projects was not created")
	}
	if err := c.CreateMailbox(ctx, "Projects"); err == nil {
		t.Error("creating an existing mailbox should fail")
	}

	if err := c.RenameMailbox(ctx, "Projects", "Work"); err != nil {
		t.Fatal(err)
	}
	if got := names(); got["Projects"] || !got["Work"] {
		t.Fatalf("rename did not take effect: %v", got)
	}
	if err := c.RenameMailbox(ctx, "NoSuchBox", "X"); err == nil {
		t.Error("renaming a missing mailbox should fail")
	}

	if err := c.DeleteMailbox(ctx, "Work"); err != nil {
		t.Fatal(err)
	}
	if names()["Work"] {
		t.Error("Work was not deleted")
	}
	if err := c.DeleteMailbox(ctx, "Work"); err == nil {
		t.Error("deleting a missing mailbox should fail")
	}
}

func TestAppendSentAndAppendDraftFlags(t *testing.T) {
	account := startIMAP(t)
	c := connect(t, account)
	ctx := context.Background()
	out := OutgoingMessage{From: "me@example.test", To: []string{"you@example.test"}, Subject: "saved copy", Body: "hi"}
	raw, id := BuildDraft(account, out)
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	if err := c.AppendSent(ctx, "Sent", raw, when); err != nil {
		t.Fatal(err)
	}
	sent, err := c.FetchMailbox(ctx, "Sent", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].Subject != "saved copy" || !sent[0].Read {
		t.Fatalf("Sent copy should exist and be marked read: %+v", sent)
	}
	// BuildDraft returns the ID in header form (<id@host>); a fetched message reports it
	// without the angle brackets, so a caller matching the two must trim them.
	if sent[0].MessageID != strings.Trim(id, "<>") {
		t.Errorf("stored Message-ID %q does not match the draft's %q", sent[0].MessageID, id)
	}

	if err := c.AppendDraft(ctx, "Drafts", raw, when); err != nil {
		t.Fatal(err)
	}
	drafts, err := c.FetchMailbox(ctx, "Drafts", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || !drafts[0].Read {
		t.Fatalf("draft should exist and be marked read: %+v", drafts)
	}
	// \Draft is not exposed on Message, so confirm it on the wire.
	if got := flagsOf(t, account, "Drafts", drafts[0].UID); !got[`\draft`] || !got[`\seen`] {
		t.Errorf("the draft needs \\Draft and \\Seen, has %v", got)
	}
	if got := flagsOf(t, account, "Sent", sent[0].UID); got[`\draft`] || !got[`\seen`] {
		t.Errorf("the sent copy needs \\Seen and not \\Draft, has %v", got)
	}

	if err := c.AppendSent(ctx, "Sent", nil, when); err == nil {
		t.Error("appending an empty message should fail")
	}
	if err := c.AppendDraft(ctx, "NoSuchBox", raw, when); err == nil {
		t.Error("appending to a missing mailbox should fail")
	}
}

// flagsOf reads a message's flags over a separate connection with a flags-only FETCH. Reading
// through Client.FetchMessage would not do: it fetches BODY[], which the in-memory server (unlike
// a real one, for a read-only SELECT) answers by setting \\Seen, disturbing what is being checked.
func flagsOf(t *testing.T, account Account, mailbox string, uid uint32) map[string]bool {
	t.Helper()
	conn, err := net.Dial("tcp", net.JoinHostPort(account.IMAPHost, strconv.Itoa(account.IMAPPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := imapclient.New(conn, nil)
	defer c.Close()
	if err := c.Login(testUser, testPass).Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Select(mailbox, &imapgo.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		t.Fatal(err)
	}
	msgs, err := c.Fetch(imapgo.UIDSetNum(imapgo.UID(uid)), &imapgo.FetchOptions{Flags: true, UID: true}).Collect()
	if err != nil || len(msgs) != 1 {
		t.Fatalf("fetch flags: %v (%d messages)", err, len(msgs))
	}
	out := map[string]bool{}
	for _, f := range msgs[0].Flags {
		out[strings.ToLower(string(f))] = true
	}
	return out
}

func TestClientMethodsFailBeforeConnect(t *testing.T) {
	c := NewClient(Account{IMAPHost: "127.0.0.1", IMAPPort: 1})
	ctx := context.Background()
	if _, err := c.ListMailboxes(ctx); err == nil {
		t.Error("ListMailboxes before Connect should fail")
	}
	if _, err := c.FetchInbox(ctx, 5); err == nil {
		t.Error("FetchInbox before Connect should fail")
	}
	if err := c.SetRead(ctx, 1, true); err == nil {
		t.Error("SetRead before Connect should fail")
	}
	if err := c.Delete(ctx, 1); err == nil {
		t.Error("Delete before Connect should fail")
	}
	if err := c.Connect(ctx); err == nil {
		t.Error("connecting to a closed port should fail")
	}
}

// --- SMTP -------------------------------------------------------------------------------

type fakeSMTP struct {
	ln   net.Listener
	mu   sync.Mutex
	from string
	rcpt []string
	data string
}

// startSMTP runs a minimal plaintext SMTP server that accepts AUTH PLAIN and records the
// envelope and message data of the first transaction. rejectRcpt makes RCPT fail.
func startSMTP(t *testing.T, rejectRcpt bool) (*fakeSMTP, Account) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn, rejectRcpt)
		}
	}()
	return s, Account{
		SMTPHost: "127.0.0.1",
		SMTPPort: ln.Addr().(*net.TCPAddr).Port,
		User:     testUser,
		Password: testPass,
		From:     "me@example.test",
	}
}

func (s *fakeSMTP) serve(conn net.Conn, rejectRcpt bool) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck
	r := bufio.NewReader(conn)
	reply := func(line string) { fmt.Fprintf(conn, "%s\r\n", line) }
	reply("220 smtp.test ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			reply("250-smtp.test")
			reply("250 AUTH PLAIN")
		case strings.HasPrefix(upper, "AUTH"):
			reply("235 ok")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			s.mu.Lock()
			s.from = line[len("MAIL FROM:"):]
			s.mu.Unlock()
			reply("250 ok")
		case strings.HasPrefix(upper, "RCPT TO:"):
			if rejectRcpt {
				reply("550 no such user")
				continue
			}
			s.mu.Lock()
			s.rcpt = append(s.rcpt, line[len("RCPT TO:"):])
			s.mu.Unlock()
			reply("250 ok")
		case upper == "DATA":
			reply("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.data = b.String()
			s.mu.Unlock()
			reply("250 queued")
		case upper == "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

func (s *fakeSMTP) snapshot() (from string, rcpt []string, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.from, append([]string(nil), s.rcpt...), s.data
}

func TestSendDeliversToAllRecipients(t *testing.T) {
	srv, account := startSMTP(t, false)
	msg := OutgoingMessage{
		From:    "me@example.test",
		To:      []string{"to@example.test"},
		CC:      []string{"cc@example.test"},
		BCC:     []string{"bcc@example.test"},
		Subject: "Quarterly numbers",
		Body:    "see attached",
		Attachments: []OutgoingAttachment{
			{Name: "report.txt", Data: []byte("report contents")},
		},
	}
	if err := Send(context.Background(), account, msg); err != nil {
		t.Fatal(err)
	}
	from, rcpt, data := srv.snapshot()
	if !strings.Contains(from, "me@example.test") {
		t.Errorf("envelope sender = %q", from)
	}
	if len(rcpt) != 3 {
		t.Fatalf("envelope recipients = %v, want To, CC and BCC", rcpt)
	}
	for _, want := range []string{"Subject: Quarterly numbers", "To: to@example.test", "Cc: cc@example.test", "report.txt"} {
		if !strings.Contains(data, want) {
			t.Errorf("message data is missing %q", want)
		}
	}
	if strings.Contains(data, "bcc@example.test") {
		t.Error("Bcc recipients must not appear in the message headers")
	}
}

func TestSendRequiresARecipient(t *testing.T) {
	_, account := startSMTP(t, false)
	err := Send(context.Background(), account, OutgoingMessage{From: "me@example.test", Subject: "nobody"})
	if err == nil {
		t.Fatal("a message with no recipients should be refused")
	}
}

func TestSendSurfacesRecipientRejection(t *testing.T) {
	_, account := startSMTP(t, true)
	err := Send(context.Background(), account, OutgoingMessage{From: "me@example.test", To: []string{"x@example.test"}, Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("a rejected recipient should be an error")
	}
}

func TestSendFailsWhenServerIsDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	account := Account{SMTPHost: "127.0.0.1", SMTPPort: port, User: testUser, Password: testPass, From: "me@example.test"}
	if err := Send(context.Background(), account, OutgoingMessage{To: []string{"x@example.test"}, Subject: "s"}); err == nil {
		t.Fatal("sending to a closed port should fail")
	}
}

func TestSendWithCopyReturnsTheTransmittedBytes(t *testing.T) {
	srv, account := startSMTP(t, false)
	msg := OutgoingMessage{From: "me@example.test", To: []string{"to@example.test"}, Subject: "copy me", Body: "line one\nline two"}
	raw, err := SendWithCopy(context.Background(), account, msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("no copy returned")
	}
	_, _, data := srv.snapshot()
	// The Sent copy must be byte-for-byte what went over the wire, even for a body typed with
	// bare LF line endings (net/smtp would otherwise rewrite them to CRLF in transit).
	if data != string(raw) {
		t.Errorf("the returned copy differs from what was transmitted\n--- sent:\n%q\n--- copy:\n%q", data, raw)
	}
	if !strings.Contains(string(raw), "Message-ID:") || !strings.Contains(string(raw), "Date:") {
		t.Error("the copy should carry Message-ID and Date so the Sent folder matches the sent mail")
	}
}

func TestSendWithCopyReturnsNothingOnFailure(t *testing.T) {
	_, account := startSMTP(t, true)
	raw, err := SendWithCopy(context.Background(), account, OutgoingMessage{From: "me@example.test", To: []string{"x@example.test"}, Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("expected the rejection to be returned")
	}
	if raw != nil {
		t.Error("a failed send must not return a copy to file under Sent")
	}
}

// --- Watcher ----------------------------------------------------------------------------

func TestWatcherSignalsOnNewMailAndStopsOnClose(t *testing.T) {
	account := startIMAP(t)
	w := NewWatcher(account, "INBOX")

	// One event follows the initial connect, to cover the gap before IDLE began.
	select {
	case <-w.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("expected the initial connect event")
	}
	time.Sleep(200 * time.Millisecond) // let it enter IDLE
	seed(t, account, "INBOX", "push me", "b", false)
	select {
	case <-w.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("expected an event when new mail arrived")
	}

	select {
	case <-w.Done():
		t.Fatal("Done must stay open while the watcher runs")
	default:
	}
	closed := make(chan struct{})
	go func() { w.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
	select {
	case <-w.Done():
	case <-time.After(time.Second):
		t.Fatal("Done should be closed after Close")
	}
	w.Close() // closing twice must be safe
}
