package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// ── Pure qualification ───────────────────────────────────────────────────────

var testMe = myAddresses([]accountIdentity{
	{user: "me@work.example", from: "Me <me@work.example>"},
	{user: "me@home.example"},
})

var base = time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)

type mb struct {
	id                                 int64
	from, to, cc, messageID, inReplyTo string
	hours                              int
	flags                              []string
}

func msgsOf(specs ...mb) []db.Message {
	out := make([]db.Message, len(specs))
	for i, s := range specs {
		out[i] = db.Message{ID: s.id, From: s.from, To: s.to, CC: s.cc, MessageID: s.messageID,
			InReplyTo: s.inReplyTo, Date: base.Add(time.Duration(s.hours) * time.Hour), Flags: s.flags, Subject: fmt.Sprintf("m%d", s.id)}
	}
	return out
}

func waitingIDs(threads []waitingThread) []int64 {
	out := []int64{}
	for _, t := range threads {
		out = append(out, t.message.ID)
	}
	return out
}

func TestWaitingQualification(t *testing.T) {
	cases := []struct {
		name string
		msgs []db.Message
		want []int64
	}{
		{"sent to a person", msgsOf(mb{id: 1, from: "Me <me@work.example>", to: "Sarah <sarah@x.example>", messageID: "<a@x>"}), []int64{1}},
		{"latest is inbound", msgsOf(
			mb{id: 1, from: "me@work.example", to: "sarah@x.example", messageID: "<a@x>"},
			mb{id: 2, from: "Sarah <sarah@x.example>", to: "me@work.example", messageID: "<b@x>", inReplyTo: "<a@x>", hours: 1}), []int64{}},
		{"reply again", msgsOf(
			mb{id: 1, from: "sarah@x.example", to: "me@work.example", messageID: "<a@x>"},
			mb{id: 2, from: "me@work.example", to: "sarah@x.example", messageID: "<b@x>", inReplyTo: "<a@x>", hours: 1},
			mb{id: 3, from: "sarah@x.example", to: "me@work.example", messageID: "<c@x>", inReplyTo: "<b@x>", hours: 2},
			mb{id: 4, from: "me@work.example", to: "sarah@x.example", messageID: "<d@x>", inReplyTo: "<c@x>", hours: 3}), []int64{4}},
		{"second identity is me", msgsOf(mb{id: 1, from: "me@home.example", to: "bob@x.example", messageID: "<a@x>"}), []int64{1}},
		{"display name alone is not me", msgsOf(mb{id: 1, from: "Me <someone@else.example>", to: "bob@x.example", messageID: "<a@x>"}), []int64{}},
		{"self mail", msgsOf(mb{id: 1, from: "me@work.example", to: "me@home.example", messageID: "<a@x>"}), []int64{}},
		{"noreply only", msgsOf(mb{id: 1, from: "me@work.example", to: "noreply@service.example", messageID: "<a@x>"}), []int64{}},
		{"automated only", msgsOf(mb{id: 1, from: "me@work.example", to: "notifications@github.com, bounce+x@list.example", messageID: "<a@x>"}), []int64{}},
		{"mailing list only", msgsOf(mb{id: 1, from: "me@work.example", to: "team@googlegroups.com", messageID: "<a@x>"}), []int64{}},
		{"real plus automated", msgsOf(mb{id: 1, from: "me@work.example", to: "noreply@svc.example", cc: "Sarah <sarah@x.example>", messageID: "<a@x>"}), []int64{1}},
		{"draft ignored", msgsOf(
			mb{id: 1, from: "sarah@x.example", to: "me@work.example", messageID: "<a@x>"},
			mb{id: 2, from: "me@work.example", to: "sarah@x.example", messageID: "<b@x>", inReplyTo: "<a@x>", hours: 1, flags: []string{`\Draft`}}), []int64{}},
		{"bounce does not end the wait", msgsOf(
			mb{id: 1, from: "me@work.example", to: "sarah@x.example", messageID: "<a@x>"},
			mb{id: 2, from: "MAILER-DAEMON@mx.example", to: "me@work.example", messageID: "<b@x>", inReplyTo: "<a@x>", hours: 1}), []int64{1}},
		{"no identities configured", nil, []int64{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			me := testMe
			if c.msgs == nil {
				c.msgs = msgsOf(mb{id: 1, from: "me@work.example", to: "sarah@x.example"})
				me = map[string]bool{}
			}
			got, _ := computeWaiting(c.msgs, me, nil)
			if fmt.Sprint(waitingIDs(got)) != fmt.Sprint(c.want) {
				t.Fatalf("waiting = %v, want %v", waitingIDs(got), c.want)
			}
		})
	}
}

func TestWaitingThreadsAndOrdering(t *testing.T) {
	msgs := msgsOf(
		// Thread A: three nudges from me, one row, waiting since the newest.
		mb{id: 1, from: "me@work.example", to: "sarah@x.example", messageID: "<a1@x>", hours: 0},
		mb{id: 2, from: "me@work.example", to: "sarah@x.example", messageID: "<a2@x>", inReplyTo: "<a1@x>", hours: 10},
		mb{id: 3, from: "me@work.example", to: "sarah@x.example", messageID: "<a3@x>", inReplyTo: "<a2@x>", hours: 20},
		// Thread B: same subject, unrelated, older wait.
		mb{id: 4, from: "me@work.example", to: "mike@x.example", messageID: "<b1@x>", hours: 5},
		// Thread C: no Message-ID at all: stands alone.
		mb{id: 5, from: "me@work.example", to: "zoe@x.example", hours: 5},
		// The same message stored twice (Sent and All Mail) is one thread.
		mb{id: 6, from: "me@work.example", to: "ann@x.example", messageID: "<dup@x>", hours: 30},
		mb{id: 7, from: "me@work.example", to: "ann@x.example", messageID: "<dup@x>", hours: 30},
	)
	for i := range msgs {
		msgs[i].Subject = "Contract"
	}
	got, _ := computeWaiting(msgs, testMe, nil)
	// Oldest wait first; equal times (4 and 5) tie-break on row ID.
	if fmt.Sprint(waitingIDs(got)) != "[4 5 3 7]" && fmt.Sprint(waitingIDs(got)) != "[4 5 3 6]" {
		t.Fatalf("order = %v", waitingIDs(got))
	}
	if !got[2].since.Equal(base.Add(20 * time.Hour)) {
		t.Fatalf("waiting since = %v; the newest sent message decides", got[2].since)
	}
	if len(got) != 4 {
		t.Fatalf("duplicates or merges: %v", waitingIDs(got))
	}
}

func TestWaitingStoppedCycles(t *testing.T) {
	first := msgsOf(mb{id: 1, from: "me@work.example", to: "sarah@x.example", messageID: "<a@x>"})
	stoppedKeys := map[string]bool{"<a@x>": true}
	got, stopped := computeWaiting(first, testMe, stoppedKeys)
	if len(got) != 0 || stopped[1] != "<a@x>" {
		t.Fatalf("stopped cycle still shown: %v %v", waitingIDs(got), stopped)
	}
	// Sarah replies (the old wait ends), then I reply again: a new cycle that
	// the old dismissal does not hide.
	later := append(first, msgsOf(
		mb{id: 2, from: "sarah@x.example", to: "me@work.example", messageID: "<b@x>", inReplyTo: "<a@x>", hours: 1},
		mb{id: 3, from: "me@work.example", to: "sarah@x.example", messageID: "<c@x>", inReplyTo: "<b@x>", hours: 2})...)
	got, _ = computeWaiting(later, testMe, stoppedKeys)
	if fmt.Sprint(waitingIDs(got)) != "[3]" {
		t.Fatalf("new cycle = %v", waitingIDs(got))
	}
	// Even a direct second nudge without a reply is a new cycle.
	nudge := append(first, msgsOf(mb{id: 4, from: "me@work.example", to: "sarah@x.example", messageID: "<d@x>", inReplyTo: "<a@x>", hours: 3})...)
	got, _ = computeWaiting(nudge, testMe, stoppedKeys)
	if fmt.Sprint(waitingIDs(got)) != "[4]" {
		t.Fatalf("nudge = %v", waitingIDs(got))
	}
}

func TestWaitingExplanationText(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	id := m.filteredMessages[0].ID
	m.waiting.byID = map[int64]waitingThread{id: {
		since:      base,
		waitingFor: []string{"Sarah", "Mike", "Zoe", "Ann"},
	}}
	lines := m.waitingExplanationLines(id, base.Add(49*time.Hour))
	got := strings.Join(lines, "\n")
	for _, want := range []string{"Waiting on Them because:", "You sent the latest message 2 days ago.", "Waiting for: Sarah, Mike +2"} {
		if !strings.Contains(got, want) {
			t.Errorf("explanation missing %q:\n%s", want, got)
		}
	}
	if summarizeNames([]string{"Sarah"}) != "Sarah" || summarizeNames([]string{"A", "B"}) != "A, B" {
		t.Fatal("short name lists should be listed in full")
	}
	// Names are sanitized: no escapes, and addresses are used without names.
	p := parseParticipants("\"Evil\x1b[31m\u202e\" <e@x.example>, plain@x.example")
	// Control characters become spaces (TideMail's display convention); the
	// escape and the bidi override are gone.
	if len(p) != 2 || displayParticipant(p[0]) != "Evil [31m" || displayParticipant(p[1]) != "plain@x.example" {
		t.Fatalf("participants = %q, %q", displayParticipant(p[0]), displayParticipant(p[1]))
	}
}

// ── Integration ──────────────────────────────────────────────────────────────

type waitingFixture struct {
	t                                         *testing.T
	database                                  *db.DB
	inbox, sent, archive, trash, spam, drafts int64
	uid                                       uint32
}

func newWaitingModel(t *testing.T) (Model, *waitingFixture) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	acc, _ := database.AddAccount("acct-work", "Work", "")
	mk := func(name string, flags ...string) int64 {
		id, err := database.UpsertMailbox(db.Mailbox{AccountID: acc, Name: name, Delimiter: "/", Flags: flags})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f := &waitingFixture{t: t, database: database,
		inbox: mk("INBOX"), sent: mk("Sent", `\Sent`), archive: mk("Archive"),
		trash: mk("Trash", `\Trash`), spam: mk("Spam", `\Junk`), drafts: mk("Drafts", `\Drafts`)}
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{ID: "acct-work", Name: "Work", User: "me@work.example", From: "Me <me@work.example>"}}
	cfg.Display.ShowSender = true
	m := NewModel(database, cfg, "dev", false)
	m.width, m.height = 120, 30
	m.cfg.Display.Icons = true
	m.accounts, _ = database.ListAccounts()
	m.mailboxes, _ = database.ListMailboxes(acc)
	m.rebuildSidebar()
	return m, f
}

// add stores a message hoursAgo old and returns its row ID.
func (f *waitingFixture) add(mailbox int64, from, to, messageID, inReplyTo, subject string, hoursAgo int) int64 {
	f.t.Helper()
	f.uid++
	if err := f.database.UpsertMessage(db.Message{MailboxID: mailbox, UID: f.uid, From: from, To: to, MessageID: messageID,
		InReplyTo: inReplyTo, Subject: subject, BodyText: "BODY", Date: time.Now().Add(-time.Duration(hoursAgo) * time.Hour)}); err != nil {
		f.t.Fatal(err)
	}
	id, err := f.database.MessageIDByUID(mailbox, f.uid)
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func openWaiting(t *testing.T, m Model) Model {
	t.Helper()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindWaiting {
			m.sidebarCursor = i
		}
	}
	if !m.selectedWaiting() {
		t.Fatal("no Waiting on Them row")
	}
	m.focused = paneMessages
	return settle(t, m, m.virtualViewCmd())
}

func refreshWaiting(t *testing.T, m Model) Model {
	t.Helper()
	return settle(t, m, m.requestWaitingRefresh())
}

func TestWaitingFoldersAndView(t *testing.T) {
	m, f := newWaitingModel(t)
	f.add(f.sent, "Me <me@work.example>", "Sarah <sarah@x.example>", "<sent@x>", "", "Sent only", 48)
	f.add(f.archive, "me@work.example", "Mike <mike@x.example>", "<arch@x>", "", "Archived", 24)
	f.add(f.trash, "me@work.example", "t@x.example", "<trash@x>", "", "Trashed", 10)
	f.add(f.spam, "me@work.example", "s@x.example", "<spam@x>", "", "Spam", 10)
	f.add(f.drafts, "me@work.example", "d@x.example", "<draft@x>", "", "Draft", 10)
	f.add(f.sent, "me@work.example", "noreply@svc.example", "<nr@x>", "", "To a robot", 5)
	m = openWaiting(t, m)

	if got := listedSubjects(m); fmt.Sprint(got) != "[Sent only Archived]" {
		t.Fatalf("listed = %v", got)
	}
	if m.waiting.count != 2 || !strings.Contains(m.renderAccountsPane(), "Waiting on Them") || !strings.Contains(m.renderAccountsPane(), "(2)") {
		t.Fatalf("count = %d", m.waiting.count)
	}
	// The sender column shows who you are waiting for, not yourself.
	if line := rowLine(t, m, "Sent only"); !strings.Contains(line, "↗ Sarah") {
		t.Fatalf("row = %q", line)
	}
	// Rows are the real messages, with bodies, for normal actions.
	if cur := m.commandMessage(); cur == nil || cur.BodyText != "BODY" {
		t.Fatalf("current = %+v", cur)
	}
}

func TestWaitingEmptyAndNoDatabaseRendering(t *testing.T) {
	m, _ := newWaitingModel(t)
	m = openWaiting(t, m)
	if !strings.Contains(m.View(), "You're not waiting on any replies.") {
		t.Fatal("empty state missing")
	}
	// Rendering reads the cache only: a model without a database draws it.
	plain, msgs := newMailboxListModel(7, 1)
	for i, row := range plain.sidebarRows {
		if row.kind == rowKindWaiting {
			plain.sidebarCursor = i
		}
	}
	plain.waiting.count = 3
	plain.waiting.byID = map[int64]waitingThread{msgs[0].ID: {waitingFor: []string{"Sarah"}}}
	if view := plain.View(); !strings.Contains(view, "(3)") {
		t.Fatal("cached count not rendered")
	}
}

func TestWaitingLiveReplyAndSent(t *testing.T) {
	m, f := newWaitingModel(t)
	a := f.add(f.sent, "me@work.example", "sarah@x.example", "<a@x>", "", "Contract", 10)
	f.add(f.sent, "me@work.example", "mike@x.example", "<m@x>", "", "Other", 20)
	m = openWaiting(t, m)
	if m.waiting.count != 2 {
		t.Fatalf("count = %d", m.waiting.count)
	}
	for m.commandMessage().ID != a {
		m, _ = press(t, m, "down")
	}

	// Sarah's reply arrives in the inbox through a sync: the thread leaves
	// the open view and the cursor stays valid.
	reply := f.add(f.inbox, "Sarah <sarah@x.example>", "me@work.example", "<r@x>", "<a@x>", "Re: Contract", 1)
	next, cmd := m.Update(MailboxSyncedMsg{MailboxID: f.inbox, NewCount: 1, SyncedAt: time.Now()})
	m = settle(t, next.(Model), cmd)
	if got := listedSubjects(m); fmt.Sprint(got) != "[Other]" || m.waiting.count != 1 {
		t.Fatalf("after reply = %v (count %d)", got, m.waiting.count)
	}
	if m.messageCursor < 0 || m.messageCursor >= m.activeMessageRowCount() {
		t.Fatalf("cursor %d out of range", m.messageCursor)
	}

	// My answer, known once the Sent folder syncs (from TideMail or any other
	// client), starts a new wait.
	f.add(f.sent, "me@work.example", "sarah@x.example", "<b@x>", "<r@x>", "Re: Contract", 0)
	next, cmd = m.Update(MailboxSyncedMsg{MailboxID: f.sent, NewCount: 0, SyncedAt: time.Now()})
	m = settle(t, next.(Model), cmd)
	if got := listedSubjects(m); fmt.Sprint(got) != "[Other Re: Contract]" || m.waiting.count != 2 {
		t.Fatalf("after my reply = %v (count %d)", got, m.waiting.count)
	}
	_ = reply
}

func TestWaitingCountUpdatesOutsideTheView(t *testing.T) {
	m, f := newWaitingModel(t)
	m = refreshWaiting(t, m) // startup
	if m.waiting.count != 0 || !m.waiting.loaded {
		t.Fatalf("count = %d", m.waiting.count)
	}
	f.add(f.sent, "me@work.example", "sarah@x.example", "<a@x>", "", "Hello", 1)
	next, cmd := m.Update(MailboxSyncedMsg{MailboxID: f.sent, SyncedAt: time.Now()})
	m = settle(t, next.(Model), cmd)
	if m.waiting.count != 1 {
		t.Fatalf("count after Sent sync = %d", m.waiting.count)
	}
}

func TestWaitingStopAndResume(t *testing.T) {
	m, f := newWaitingModel(t)
	a := f.add(f.sent, "me@work.example", "sarah@x.example", "<a@x>", "", "Contract", 10)
	f.add(f.sent, "me@work.example", "mike@x.example", "<m@x>", "", "Other", 20)
	if err := f.database.ReplacePluginAnnotations("smart", a, []db.PluginAnnotation{{Key: "needs_reply", Value: "true"}}); err != nil {
		t.Fatal(err)
	}
	m = openWaiting(t, m)
	for m.commandMessage().ID != a {
		m, _ = press(t, m, "down")
	}
	found := false
	for _, it := range m.mainCommandItems() {
		found = found || it.id == "waiting-stop"
	}
	if !found {
		t.Fatal("Stop waiting missing from the palette")
	}

	m, cmd := press(t, m, "X")
	m = settle(t, m, cmd)
	if got := listedSubjects(m); fmt.Sprint(got) != "[Other]" || m.waiting.count != 1 {
		t.Fatalf("after stop = %v", got)
	}
	if !strings.Contains(m.statusMsg, "stopped waiting") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	// Mail and annotations untouched.
	if stored, err := f.database.GetMessage(a); err != nil || stored.Read || stored.Starred || stored.Subject != "Contract" {
		t.Fatalf("message changed: %+v %v", stored, err)
	}
	if anns := mustAnnotations(t, f.database, a); len(anns) != 1 {
		t.Fatalf("annotations changed: %v", anns)
	}
	// It survives a restart (a fresh computation from the database).
	m.waiting = waitingState{}
	m = refreshWaiting(t, m)
	if m.waiting.count != 1 {
		t.Fatal("stop did not persist")
	}
	// ctrl+z is gone after the reset; Resume from the palette works.
	if _, ok := m.waiting.stopped[a]; !ok {
		t.Fatal("stopped cycle not tracked for resume")
	}
	m.messages = append(m.messages, mustMessage(t, f.database, a))
	m.applyFilter()
	for m.commandMessage().ID != a {
		m, _ = press(t, m, "down")
	}
	next, cmd := m.executeCommand("waiting-resume")
	m = settle(t, next.(Model), cmd)
	if m.waiting.count != 2 {
		t.Fatalf("resume: count = %d", m.waiting.count)
	}
}

func mustMessage(t *testing.T, database *db.DB, id int64) db.Message {
	t.Helper()
	msg, err := database.GetMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestWaitingUndoAndFailure(t *testing.T) {
	m, f := newWaitingModel(t)
	a := f.add(f.sent, "me@work.example", "sarah@x.example", "<a@x>", "", "Contract", 10)
	m = openWaiting(t, m)
	m, cmd := press(t, m, "X")
	m = settle(t, m, cmd)
	if m.waiting.count != 0 {
		t.Fatal("stop failed")
	}
	m, cmd = press(t, m, "ctrl+z")
	m = settle(t, m, cmd)
	if m.waiting.count != 1 {
		t.Fatal("ctrl+z should resume waiting")
	}
	// A failed write changes nothing on screen.
	next, _ := m.Update(waitingStopMsg{Keys: []string{"<a@x>"}, Stopped: true, Err: fmt.Errorf("database is locked")})
	m = next.(Model)
	if got := listedSubjects(m); fmt.Sprint(got) != "[Contract]" || !m.statusErr || len(m.waiting.lastStopped) != 0 {
		t.Fatalf("got %v, status %q", got, m.statusMsg)
	}
	_ = a
}

func TestWaitingWhyView(t *testing.T) {
	m, f := newWaitingModel(t)
	f.add(f.sent, "me@work.example", "\"Sarah\x1b[31m\" <sarah@x.example>, Mike <mike@x.example>, zoe@x.example", "<a@x>", "", "Contract", 50)
	m = openWaiting(t, m)
	next, _ := m.executeCommand("waiting-why")
	m = next.(Model)
	view := m.View()
	for _, want := range []string{"Waiting on Them because:", "You sent the latest message 2 days ago.", "Waiting for: Sarah [31m, Mike +1", "no plugin annotations"} {
		if !strings.Contains(view, want) {
			t.Errorf("why view missing %q", want)
		}
	}
	if strings.Contains(view, "Received:") || strings.Contains(view, "BODY") {
		t.Error("raw headers or body leaked")
	}
}

func TestWaitingIndependentOfNeedsYou(t *testing.T) {
	m, f := newWaitingModel(t)
	a := f.add(f.sent, "me@work.example", "sarah@x.example", "<a@x>", "", "Contract", 10)
	// Stale annotations say it needs a reply; Waiting doesn't care.
	if err := f.database.ReplacePluginAnnotations("smart", a, []db.PluginAnnotation{{Key: "needs_reply", Value: "true"}}); err != nil {
		t.Fatal(err)
	}
	m = refreshWaiting(t, m)
	if m.waiting.count != 1 {
		t.Fatal("waiting should not depend on or be blocked by annotations")
	}
	// Sarah replies and a plugin marks the reply actionable: it leaves
	// Waiting and enters Needs You, independently.
	reply := f.add(f.inbox, "sarah@x.example", "me@work.example", "<r@x>", "<a@x>", "Re: Contract", 1)
	if err := f.database.ReplacePluginAnnotations("smart", reply, []db.PluginAnnotation{{Key: "needs_reply", Value: "true"}}); err != nil {
		t.Fatal(err)
	}
	next, cmd := m.Update(MailboxSyncedMsg{MailboxID: f.inbox, NewCount: 1, SyncedAt: time.Now()})
	m = settle(t, next.(Model), cmd)
	if m.waiting.count != 0 || m.needsYou.count != 1 {
		t.Fatalf("waiting %d, needs you %d", m.waiting.count, m.needsYou.count)
	}
	if anns := mustAnnotations(t, f.database, a); len(anns) != 1 {
		t.Fatal("waiting changed annotations")
	}
}

func TestWaitingSyncKeyIncludesSent(t *testing.T) {
	m, f := newWaitingModel(t)
	m = openWaiting(t, m)
	_, cmd := press(t, m, "s")
	if cmd == nil {
		t.Fatal("sync on Waiting on Them should sync inboxes and Sent folders")
	}
	_ = f
}

func TestSyncSentAfterSendTargetsTheAccountsSentFolder(t *testing.T) {
	m, f := newWaitingModel(t)
	if m.syncSentAfterSendCmd("acct-work") == nil {
		t.Fatal("a successful send should sync that account's Sent folder")
	}
	if m.syncSentAfterSendCmd("unknown") != nil {
		t.Fatal("no Sent folder, no sync")
	}
	_ = f
}
