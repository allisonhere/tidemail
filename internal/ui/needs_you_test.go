package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// settle runs cmd and feeds every resulting message back through Update, the
// way the Bubble Tea runtime would, following batches and follow-up commands.
// Commands that do not finish within 200ms (status timers, listeners) are
// abandoned.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && steps < 200; steps++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(200 * time.Millisecond):
			continue
		}
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			next, follow := m.Update(msg)
			m = next.(Model)
			queue = append(queue, follow)
		}
	}
	return m
}

type needsYouFixture struct {
	t        *testing.T
	database *db.DB
	inbox    int64
	trash    int64
	uid      uint32
}

// newNeedsYouModel has one account with an inbox and a trash folder, and the
// Needs You row selected.
func newNeedsYouModel(t *testing.T) (Model, *needsYouFixture) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	acc, _ := database.AddAccount("acct-personal", "Personal", "")
	inbox, _ := database.UpsertMailbox(db.Mailbox{AccountID: acc, Name: "INBOX", Delimiter: "/"})
	trash, _ := database.UpsertMailbox(db.Mailbox{AccountID: acc, Name: "Trash", Delimiter: "/", Flags: []string{"\\Trash"}})
	f := &needsYouFixture{t: t, database: database, inbox: inbox, trash: trash}

	cfg := config.DefaultConfig()
	// No IMAP host: mail actions only touch the local cache.
	cfg.Accounts = []config.AccountConfig{{ID: "acct-personal", Name: "Personal"}}
	m := NewModel(database, cfg, "dev", false)
	m.width, m.height = 110, 30
	m.cfg.Display.Icons = true
	m.accounts, _ = database.ListAccounts()
	m.mailboxes, _ = database.ListMailboxes(acc)
	m.rebuildSidebar()
	return m, f
}

// add stores a message minutesAgo old with plugin:key=value annotations.
func (f *needsYouFixture) add(mailbox int64, subject string, minutesAgo int, anns ...string) int64 {
	f.t.Helper()
	f.uid++
	date := time.Now().Add(-time.Duration(minutesAgo) * time.Minute)
	if err := f.database.UpsertMessage(db.Message{MailboxID: mailbox, UID: f.uid, Subject: subject, From: "Sam <sam@example.com>", Date: date}); err != nil {
		f.t.Fatal(err)
	}
	id, err := f.database.MessageIDByUID(mailbox, f.uid)
	if err != nil {
		f.t.Fatal(err)
	}
	f.annotate(id, anns...)
	return id
}

// annotate replaces the named plugins' annotations on a message.
func (f *needsYouFixture) annotate(id int64, anns ...string) {
	f.t.Helper()
	byPlugin := map[string][]db.PluginAnnotation{}
	for _, s := range anns {
		parts := strings.SplitN(s, " ", 3)
		byPlugin[parts[0]] = append(byPlugin[parts[0]], db.PluginAnnotation{Key: parts[1], Value: parts[2]})
	}
	for plugin, list := range byPlugin {
		if err := f.database.ReplacePluginAnnotations(plugin, id, list); err != nil {
			f.t.Fatal(err)
		}
	}
}

// openNeedsYou selects the Needs You row and loads it (and the count).
func openNeedsYou(t *testing.T, m Model) Model {
	t.Helper()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindNeedsYou {
			m.sidebarCursor = i
		}
	}
	if !m.selectedNeedsYou() {
		t.Fatal("no Needs You row")
	}
	m.focused = paneMessages
	return settle(t, m, tea.Batch(m.loadNeedsYouCmd(), loadNeedsYouCountCmd(m.db)))
}

func listedSubjects(m Model) []string {
	out := []string{}
	for _, msg := range m.filteredMessages {
		out = append(out, msg.Subject)
	}
	return out
}

func TestNeedsYouSidebarRowAndEmptyState(t *testing.T) {
	m, _ := newNeedsYouModel(t)
	m = openNeedsYou(t, m)
	view := m.View()
	if !strings.Contains(view, "Needs You") || !strings.Contains(view, "Nothing needs your attention.") {
		t.Fatal("empty Needs You should render its row and empty state")
	}
	if m.needsYou.count != 0 || strings.Contains(m.renderAccountsPane(), "Needs You (") {
		t.Fatal("zero count should show no badge")
	}
}

func TestNeedsYouLoadsQualifyingMessagesAndCount(t *testing.T) {
	m, f := newNeedsYouModel(t)
	f.add(f.inbox, "reply to me", 5, "smart needs_reply true")
	f.add(f.inbox, "urgent thing", 60, "other urgency high")
	f.add(f.inbox, "shipping only", 1, "smart category shipping")
	f.add(f.trash, "trashed request", 2, "smart needs_reply true")
	f.add(f.inbox, "plain", 3)
	m = openNeedsYou(t, m)

	if got := listedSubjects(m); fmt.Sprint(got) != "[urgent thing reply to me]" {
		t.Fatalf("listed = %v", got)
	}
	if m.needsYou.count != 2 || !strings.Contains(m.renderAccountsPane(), "(2)") {
		t.Fatalf("count = %d", m.needsYou.count)
	}
	// Badges come from the same cache as everywhere else.
	if line := rowLine(t, m, "reply to me"); !strings.Contains(line, "↩") {
		t.Fatalf("row = %q", line)
	}
}

func TestNeedsYouActionsTargetRealMessage(t *testing.T) {
	m, f := newNeedsYouModel(t)
	id := f.add(f.inbox, "reply to me", 5, "smart needs_reply true")
	m = openNeedsYou(t, m)
	if cur := m.commandMessage(); cur == nil || cur.ID != id {
		t.Fatalf("current message = %+v", cur)
	}
	// Marking it read acts on the stored message, and it stays in Needs You.
	m = settle(t, m, m.setMessageReadCmd(*m.commandMessage(), true, false))
	stored, err := f.database.GetMessage(id)
	if err != nil || !stored.Read {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	m = settle(t, m, m.loadNeedsYouCmd())
	if got := listedSubjects(m); fmt.Sprint(got) != "[reply to me]" {
		t.Fatalf("a read message must stay in Needs You, got %v", got)
	}
}

func TestNeedsYouUnreadStylingAndFilter(t *testing.T) {
	m, f := newNeedsYouModel(t)
	read := f.add(f.inbox, "already read", 5, "smart needs_reply true")
	f.add(f.inbox, "still unread", 6, "smart needs_reply true")
	if err := f.database.MarkRead(read, true); err != nil {
		t.Fatal(err)
	}
	m = openNeedsYou(t, m)
	if line := rowLine(t, m, "still unread"); !strings.Contains(line, "⬤") {
		t.Fatalf("unread row = %q", line)
	}
	if line := rowLine(t, m, "already read"); strings.Contains(line, "⬤") {
		t.Fatalf("read row = %q", line)
	}
	m.showUnreadOnly = true
	m = settle(t, m, m.loadNeedsYouCmd())
	if got := listedSubjects(m); fmt.Sprint(got) != "[still unread]" {
		t.Fatalf("unread only = %v", got)
	}
	m = settle(t, m, loadNeedsYouCountCmd(m.db))
	if m.needsYou.count != 2 {
		t.Fatal("the sidebar count stays the total actionable count")
	}
}

func TestNeedsYouRendersWithoutDatabase(t *testing.T) {
	m, msgs := newMailboxListModel(7, 2)
	for i, row := range m.sidebarRows {
		if row.kind == rowKindNeedsYou {
			m.sidebarCursor = i
		}
	}
	m.cfg.Display.Icons = true
	m.needsYou.count = 1
	m.plugins.annotations = map[int64][]db.PluginAnnotation{msgs[0].ID: {cacheAnn("p", "needs_reply", "true")}}
	if m.db != nil {
		t.Fatal("expected no database")
	}
	// Rendering reads only cached state; with no database a query would panic.
	if view := m.View(); !strings.Contains(view, "Needs You") || !strings.Contains(view, "(1)") {
		t.Fatal("Needs You should render from the cache")
	}
}

func TestNeedsYouLiveInsertAndRemove(t *testing.T) {
	m, f := newNeedsYouModel(t)
	a := f.add(f.inbox, "becomes urgent", 5)
	b := f.add(f.inbox, "two signals", 6, "one needs_reply true", "two importance high")
	m = openNeedsYou(t, m)
	if got := listedSubjects(m); fmt.Sprint(got) != "[two signals]" {
		t.Fatalf("start = %v", got)
	}

	// An automatic plugin result adds urgency: the count and the open view
	// update without changing folders.
	f.annotate(a, "smart urgency high")
	next, cmd := m.Update(pluginAnnotationsRefreshedMsg{MessageID: a, Annotations: mustAnnotations(t, f.database, a)})
	m = settle(t, next.(Model), cmd)
	// needs_reply + importance (3+2) outranks urgency alone (4).
	if got := listedSubjects(m); fmt.Sprint(got) != "[two signals becomes urgent]" || m.needsYou.count != 2 {
		t.Fatalf("after insert = %v (count %d)", got, m.needsYou.count)
	}

	// Clearing one of two signals keeps the message.
	if err := f.database.DeletePluginAnnotations("one", b); err != nil {
		t.Fatal(err)
	}
	next, cmd = m.Update(pluginAnnotationsClearedMsg{Action: pluginConfirmAction{kind: clearPluginOnMessage, pluginID: "one", messageID: b, label: "one"}})
	m = settle(t, next.(Model), cmd)
	// importance alone (2) now ranks below urgency (4).
	if got := listedSubjects(m); fmt.Sprint(got) != "[becomes urgent two signals]" {
		t.Fatalf("after clearing one signal = %v", got)
	}

	// Clearing the last signal removes it.
	if err := f.database.DeletePluginAnnotations("two", b); err != nil {
		t.Fatal(err)
	}
	next, cmd = m.Update(pluginAnnotationsClearedMsg{Action: pluginConfirmAction{kind: clearPluginOnMessage, pluginID: "two", messageID: b, label: "two"}})
	m = settle(t, next.(Model), cmd)
	if got := listedSubjects(m); fmt.Sprint(got) != "[becomes urgent]" || m.needsYou.count != 1 {
		t.Fatalf("after clearing the last signal = %v (count %d)", got, m.needsYou.count)
	}

	// Clearing a plugin everywhere updates it too.
	if err := f.database.DeletePluginAnnotationsForPlugin("smart"); err != nil {
		t.Fatal(err)
	}
	next, cmd = m.Update(pluginAnnotationsClearedMsg{Action: pluginConfirmAction{kind: clearPluginEverywhere, pluginID: "smart", label: "smart"}})
	m = settle(t, next.(Model), cmd)
	if got := listedSubjects(m); len(got) != 0 || m.needsYou.count != 0 {
		t.Fatalf("after global clear = %v (count %d)", got, m.needsYou.count)
	}
}

func mustAnnotations(t *testing.T, database *db.DB, id int64) []db.PluginAnnotation {
	t.Helper()
	anns, err := database.ListPluginAnnotations(id)
	if err != nil {
		t.Fatal(err)
	}
	return anns
}

func TestNeedsYouRemovedPluginStillQualifies(t *testing.T) {
	m, f := newNeedsYouModel(t)
	f.add(f.inbox, "from a removed plugin", 5, "uninstalled importance high")
	m = openNeedsYou(t, m)
	if got := listedSubjects(m); fmt.Sprint(got) != "[from a removed plugin]" {
		t.Fatalf("got %v", got)
	}
}

func TestNeedsYouDismissAndUndo(t *testing.T) {
	m, f := newNeedsYouModel(t)
	keep := f.add(f.inbox, "keep", 1, "smart needs_reply true")
	gone := f.add(f.inbox, "false positive", 2, "smart needs_reply true")
	m = openNeedsYou(t, m)
	for m.commandMessage().ID != gone {
		m, _ = press(t, m, "down")
	}

	m, cmd := press(t, m, "X")
	m = settle(t, m, cmd)
	if got := listedSubjects(m); fmt.Sprint(got) != "[keep]" || m.needsYou.count != 1 {
		t.Fatalf("after dismiss = %v (count %d)", got, m.needsYou.count)
	}
	if !strings.Contains(m.statusMsg, "dismissed from Needs You") || !strings.Contains(m.statusMsg, "ctrl+z") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	// The mail and its annotations are untouched.
	stored, err := f.database.GetMessage(gone)
	if err != nil || stored.Read || stored.Starred {
		t.Fatalf("message changed: %+v %v", stored, err)
	}
	if anns := mustAnnotations(t, f.database, gone); len(anns) != 1 || anns[0].Value != "true" {
		t.Fatalf("annotations changed: %v", anns)
	}

	// A later reclassification does not bring it back.
	f.annotate(gone, "smart urgency critical")
	next, cmd := m.Update(pluginAnnotationsRefreshedMsg{MessageID: gone, Annotations: mustAnnotations(t, f.database, gone)})
	m = settle(t, next.(Model), cmd)
	if got := listedSubjects(m); fmt.Sprint(got) != "[keep]" {
		t.Fatalf("dismissal lost after reclassification: %v", got)
	}

	// ctrl+z restores it.
	m, cmd = press(t, m, "ctrl+z")
	m = settle(t, m, cmd)
	if got := listedSubjects(m); len(got) != 2 || m.needsYou.count != 2 {
		t.Fatalf("after undo = %v (count %d)", got, m.needsYou.count)
	}
	_ = keep
}

func TestNeedsYouRestoreCommandFromInbox(t *testing.T) {
	m, f := newNeedsYouModel(t)
	id := f.add(f.inbox, "dismissed earlier", 1, "smart needs_reply true")
	if err := f.database.SetNeedsYouDismissed(id, true); err != nil {
		t.Fatal(err)
	}
	m = openNeedsYou(t, m) // loads the dismissed set
	if len(m.filteredMessages) != 0 {
		t.Fatal("dismissed message listed")
	}
	// From the Unified Inbox, the palette offers to restore it.
	for i, row := range m.sidebarRows {
		if row.kind == rowKindUnified {
			m.sidebarCursor = i
		}
	}
	m = settle(t, m, m.loadUnifiedInboxCmd())
	found := false
	for _, it := range m.mainCommandItems() {
		found = found || it.id == "needs-you-restore"
	}
	if !found {
		t.Fatal("Restore to Needs You should be offered for a dismissed message")
	}
	next, cmd := m.executeCommand("needs-you-restore")
	m = settle(t, next.(Model), cmd)
	if m.needsYou.count != 1 || m.needsYou.dismissed[id] {
		t.Fatalf("restore failed: count %d", m.needsYou.count)
	}
}

func TestNeedsYouDismissFailureKeepsRow(t *testing.T) {
	m, f := newNeedsYouModel(t)
	id := f.add(f.inbox, "stays", 1, "smart needs_reply true")
	m = openNeedsYou(t, m)
	next, _ := m.Update(needsYouDismissMsg{MessageIDs: []int64{id}, Dismissed: true, Err: fmt.Errorf("database is locked")})
	m = next.(Model)
	if got := listedSubjects(m); fmt.Sprint(got) != "[stays]" || !m.statusErr {
		t.Fatalf("got %v, status %q", got, m.statusMsg)
	}
	if len(m.needsYou.lastDismissed) != 0 {
		t.Fatal("a failed dismissal must not be undoable")
	}
}

func TestNeedsYouWhyExplanation(t *testing.T) {
	m, f := newNeedsYouModel(t)
	f.add(f.inbox, "why me", 1, "smart needs_reply true", "smart urgency high", "other importance high", "other note x\x1b[31m\u202e")
	m = openNeedsYou(t, m)
	found := false
	for _, it := range m.mainCommandItems() {
		found = found || it.id == "needs-you-why"
	}
	if !found {
		t.Fatal("Why is this in Needs You? missing from the palette")
	}
	next, _ := m.executeCommand("needs-you-why")
	m = next.(Model)
	view := m.View()
	for _, want := range []string{"Needs You because:", "↩ needs reply", "! urgent", "◆ important", "smart (not installed)", "other (not installed)"} {
		if !strings.Contains(view, want) {
			t.Errorf("why view missing %q", want)
		}
	}
	if strings.Index(view, "Needs You because:") > strings.Index(view, "other (not installed)") {
		t.Error("the explanation should come before the plugin groups")
	}
	// The injected escape is stripped (its printable tail remains) and the
	// bidi override is gone; the view's own styling escapes are expected.
	if !strings.Contains(view, "x[31m") || strings.ContainsRune(view, 0x202e) {
		t.Error("annotation text not sanitized")
	}
}

func TestNeedsYouWhyShowsOnlyApplyingReasons(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	id := m.filteredMessages[0].ID
	m.plugins.annotations = map[int64][]db.PluginAnnotation{id: {cacheAnn("p", "importance", "high"), cacheAnn("p", "category", "billing")}}
	m.cfg.Display.Icons = false
	reasons := m.needsYouReasons(id)
	if fmt.Sprint(reasons) != "[^ important]" {
		t.Fatalf("reasons = %v", reasons)
	}
	m.plugins.annotations[id] = []db.PluginAnnotation{cacheAnn("p", "category", "shipping")}
	if len(m.needsYouReasons(id)) != 0 {
		t.Fatal("a category alone is no reason")
	}
}

func TestNeedsYouThreadBadgesCombineMembers(t *testing.T) {
	m, f := newNeedsYouModel(t)
	older := f.add(f.inbox, "Plan", 60, "smart needs_reply true")
	newer := f.add(f.inbox, "Re: Plan", 5, "smart urgency high")
	for _, fix := range []struct {
		id        int64
		messageID string
		inReplyTo string
	}{{older, "<a@x>", ""}, {newer, "<b@x>", "<a@x>"}} {
		if _, err := f.database.Exec(`UPDATE messages SET message_id = ?, in_reply_to = ? WHERE id = ?`, fix.messageID, fix.inReplyTo, fix.id); err != nil {
			t.Fatal(err)
		}
	}
	m.cfg.Display.ThreadedConversations = true
	m = openNeedsYou(t, m)
	if len(m.messageThreads) != 1 || m.messageThreads[0].Count != 2 {
		t.Fatalf("threads = %+v", m.messageThreads)
	}
	if line := rowLine(t, m, "Re: Plan"); !strings.Contains(line, "↩") || !strings.Contains(line, "!") {
		t.Fatalf("thread row should combine its members' signals: %q", line)
	}
}

func TestNeedsYouCountUpdatesDuringDestructiveActions(t *testing.T) {
	for _, kind := range []string{"delete", "archive", "move", "failed move"} {
		t.Run(kind, func(t *testing.T) {
			m, f := newNeedsYouModel(t)
			id := f.add(f.inbox, "actionable", 1, "smart needs_reply true")
			f.add(f.inbox, "keep", 2, "smart urgency high")
			plain := f.add(f.inbox, "ordinary", 3)
			m = openNeedsYou(t, m)
			// Act from another folder, where the list also includes ordinary mail.
			for i, row := range m.sidebarRows {
				if row.kind == rowKindUnified {
					m.sidebarCursor = i
				}
			}
			m = settle(t, m, m.loadUnifiedInboxCmd())
			msg, err := f.database.GetMessage(id)
			if err != nil {
				t.Fatal(err)
			}
			schedule := func() {
				switch kind {
				case "delete":
					m.scheduleDelete([]db.Message{msg})
				case "archive":
					m.mailboxes[1].Flags = []string{"\\Archive"}
					if _, err := f.database.UpsertMailbox(m.mailboxes[1]); err != nil {
						t.Fatal(err)
					}
					m.scheduleArchive([]db.Message{msg})
				case "move":
					m.scheduleMove([]db.Message{msg}, *m.mailboxByID(f.trash))
				case "failed move":
					m.scheduleMove([]db.Message{msg}, db.Mailbox{ID: 999999, AccountID: m.mailboxes[0].AccountID})
				}
			}
			schedule()
			if got := m.displayNeedsYouCount(); got != 1 || !strings.Contains(m.renderNeedsYouRow(false, 40), "(1)") {
				t.Fatalf("pending badge = %d, want 1", got)
			}
			// Reloading the badge during the undo window must keep it reduced.
			m = settle(t, m, loadNeedsYouCountCmd(m.db))
			if got := m.displayNeedsYouCount(); got != 1 {
				t.Fatalf("refreshed pending badge = %d, want 1", got)
			}
			m.undoLatestDestructive()
			if got := m.displayNeedsYouCount(); got != 2 {
				t.Fatalf("undo badge = %d, want 2", got)
			}
			ordinary, err := f.database.GetMessage(plain)
			if err != nil {
				t.Fatal(err)
			}
			m.scheduleDelete([]db.Message{ordinary})
			if got := m.displayNeedsYouCount(); got != 2 {
				t.Fatalf("ordinary mail changed badge to %d", got)
			}
			m.undoLatestDestructive()
			schedule()
			cmd := m.beginDestructiveCommit(m.pendingDestructiveActions[0].ID)
			next, refresh := m.Update(cmd())
			m = next.(Model)
			want := 1
			if kind == "failed move" {
				want = 2
			}
			if got := m.displayNeedsYouCount(); got != want {
				t.Fatalf("commit badge = %d, want %d", got, want)
			}
			m = settle(t, m, refresh)
			if m.needsYou.count != want {
				t.Fatalf("stored badge = %d, want %d", m.needsYou.count, want)
			}
			m = openNeedsYou(t, m)
			if len(m.filteredMessages) != want {
				t.Fatalf("visible messages = %d, want %d", len(m.filteredMessages), want)
			}
		})
	}
}
