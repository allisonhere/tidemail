package ui

import (
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// sentFolderModel builds a model whose sidebar cursor sits on a Sent folder
// that has never been synced — the state the bug report describes.
func sentFolderModel(t *testing.T, syncMinutes int) (Model, int64) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	sentID, err := database.UpsertMailbox(db.Mailbox{
		AccountID: accountID, Name: "INBOX.Sent", Flags: []string{`\Sent`},
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{
		{Name: "Personal", IMAPHost: "mail.example.com", SyncMinutes: syncMinutes},
	}
	m := NewModel(database, cfg, "dev", false)
	m.accounts = []db.Account{{ID: accountID, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{{ID: sentID, AccountID: accountID, Name: "INBOX.Sent", Flags: []string{`\Sent`}}}
	m.sidebarRows = []sidebarRow{{kind: rowKindMailbox, mailboxID: sentID, accountID: accountID}}
	m.focused = paneAccounts
	return m, sentID
}

// Resting on a folder that has never synced is what fills it.
func TestSettledTickSyncsNeverSyncedFolder(t *testing.T) {
	m, sentID := sentFolderModel(t, 0)
	m.folderSettleSeq = 7

	next, cmd := m.Update(FolderSettledMsg{Seq: 7, MailboxID: sentID})
	if cmd == nil {
		t.Fatal("expected a sync command for a never-synced folder")
	}
	if !next.(Model).syncing[sentID] {
		t.Fatal("expected the folder to be marked syncing")
	}
	if next.(Model).syncVisible[sentID] {
		t.Fatal("a passive folder refresh must not animate sync chrome")
	}
}

// Scrolling past a folder must not sync it: the tick armed for it is stale by
// the time it lands, which is the whole point of the counter.
func TestSettledTickIgnoredAfterCursorMoves(t *testing.T) {
	m, sentID := sentFolderModel(t, 0)
	m.folderSettleSeq = 8

	next, cmd := m.Update(FolderSettledMsg{Seq: 7, MailboxID: sentID})
	if cmd != nil {
		t.Fatal("a superseded tick must not sync")
	}
	if len(next.(Model).syncing) != 0 {
		t.Fatal("a superseded tick must not mark anything syncing")
	}
}

// A tick that lands after the selection changed must not sync the old folder.
func TestSettledTickIgnoredWhenSelectionChanged(t *testing.T) {
	m, sentID := sentFolderModel(t, 0)
	m.folderSettleSeq = 3

	_, cmd := m.Update(FolderSettledMsg{Seq: 3, MailboxID: sentID + 999})
	if cmd != nil {
		t.Fatal("a tick for a different mailbox must not sync")
	}
}

// Revisiting a folder synced moments ago must not cost another round trip.
func TestSettledTickSkipsFreshFolder(t *testing.T) {
	m, sentID := sentFolderModel(t, 0)
	m.mailboxes[0].LastSynced = time.Now()
	m.folderSettleSeq = 1

	_, cmd := m.Update(FolderSettledMsg{Seq: 1, MailboxID: sentID})
	if cmd != nil {
		t.Fatal("a freshly synced folder must not resync on the passive path")
	}
}

// Manual-only means manual: the passive path never touches the network.
func TestSettledTickSkipsManualOnlyAccount(t *testing.T) {
	m, sentID := sentFolderModel(t, -1)
	m.folderSettleSeq = 1

	_, cmd := m.Update(FolderSettledMsg{Seq: 1, MailboxID: sentID})
	if cmd != nil {
		t.Fatal("a manual-only account must not sync just because a folder was opened")
	}
}

// Inboxes already have timers and IDLE; the settle path would duplicate them.
func TestSettledTickSkipsInbox(t *testing.T) {
	m, _ := sentFolderModel(t, 0)
	m.mailboxes[0].Name = "INBOX"
	m.mailboxes[0].Flags = nil
	m.folderSettleSeq = 1

	_, cmd := m.Update(FolderSettledMsg{Seq: 1, MailboxID: m.mailboxes[0].ID})
	if cmd != nil {
		t.Fatal("the inbox is already covered by the sync timers")
	}
}

// Enter on a folder row is the explicit request, so it works even where the
// passive path deliberately refuses.
func TestEnterSyncsFolderEvenWhenManualOnly(t *testing.T) {
	m, sentID := sentFolderModel(t, -1)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected Enter on a folder row to sync it")
	}
	if !next.(Model).syncing[sentID] {
		t.Fatal("expected the folder to be marked syncing")
	}
	// The armed settle must not then fire a second sync for the same folder.
	if next.(Model).folderSettleSeq == m.folderSettleSeq {
		t.Fatal("Enter should supersede any pending settle tick")
	}
}

// A keypress that does nothing reads as the app being broken.
func TestSyncKeyOnNonFolderRowExplainsWhy(t *testing.T) {
	m, _ := sentFolderModel(t, 0)
	m.sidebarRows = []sidebarRow{{kind: rowKindAccount, accountID: 1}}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if got := next.(Model).statusMsg; got == "" {
		t.Fatal("expected a status message explaining that no folder is selected")
	}
}

// The reported keys: s syncs, F syncs everything. Neither had any test.
func TestSyncKeysStartSyncs(t *testing.T) {
	m, sentID := sentFolderModel(t, 0)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil || !next.(Model).syncing[sentID] {
		t.Fatal("expected s to sync the selected folder")
	}
	if !next.(Model).syncVisible[sentID] {
		t.Fatal("an explicit sync should retain visible progress")
	}

	m2, _ := sentFolderModel(t, 0)
	next2, cmd2 := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	if cmd2 == nil || !next2.(Model).syncing[sentID] {
		t.Fatal("expected F to sync every mailbox")
	}
}

// The debounce is only useful if moving the sidebar cursor actually arms it.
// Bumping the counter is the observable side effect of scheduleFolderSettle.
func TestSidebarMovementArmsTheSettleTick(t *testing.T) {
	m, sentID := sentFolderModel(t, 0)
	second, err := m.db.UpsertMailbox(db.Mailbox{AccountID: 1, Name: "INBOX.Archive"})
	if err != nil {
		t.Fatal(err)
	}
	m.mailboxes = append(m.mailboxes, db.Mailbox{ID: second, AccountID: 1, Name: "INBOX.Archive"})
	m.sidebarRows = append(m.sidebarRows, sidebarRow{kind: rowKindMailbox, mailboxID: second, accountID: 1})
	m.sidebarCursor = 0

	before := m.folderSettleSeq
	next, cmd := m.handleDown()
	if cmd == nil {
		t.Fatal("expected moving the cursor to produce commands")
	}
	after := next.(Model).folderSettleSeq
	if after == before {
		t.Fatal("moving the sidebar cursor must arm a settle tick")
	}
	if next.(Model).syncing[sentID] {
		t.Fatal("movement alone must not sync — that is what the debounce is for")
	}
}

func TestSupersededSettleCommandReturnsNoMessage(t *testing.T) {
	m, _ := sentFolderModel(t, 0)
	first := m.scheduleFolderSettle()
	if first == nil {
		t.Fatal("expected the first settle command")
	}
	if second := m.scheduleFolderSettle(); second == nil {
		t.Fatal("expected the replacement settle command")
	}
	if msg := first(); msg != nil {
		t.Fatalf("superseded settle command returned %T; want nil to avoid a redraw", msg)
	}
}

func TestMovingOntoNonFolderCancelsSettleCommand(t *testing.T) {
	m, _ := sentFolderModel(t, 0)
	m.sidebarRows = append(m.sidebarRows, sidebarRow{kind: rowKindAccount, accountID: 1})
	settle := m.scheduleFolderSettle()

	next, _ := m.handleDown()
	if next.(Model).folderSettlePending != 0 {
		t.Fatal("moving away from a folder must clear a deferred passive refresh")
	}
	if msg := settle(); msg != nil {
		t.Fatalf("moving onto a non-folder left settle message %T armed", msg)
	}
}

func TestPassiveRefreshDefersBehindSameAccountSync(t *testing.T) {
	m, sentID := sentFolderModel(t, 0)
	inboxID, err := m.db.UpsertMailbox(db.Mailbox{AccountID: 1, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	m.mailboxes = append(m.mailboxes, db.Mailbox{ID: inboxID, AccountID: 1, Name: "INBOX"})
	m.syncing[inboxID] = true
	m.folderSettleSeq = 4

	next, cmd := m.Update(FolderSettledMsg{Seq: 4, MailboxID: sentID})
	got := next.(Model)
	if cmd != nil {
		t.Fatal("passive refresh must not queue behind a same-account sync")
	}
	if got.syncing[sentID] {
		t.Fatal("deferred folder must not be marked syncing")
	}
	if got.folderSettlePending != sentID {
		t.Fatalf("pending folder = %d, want %d", got.folderSettlePending, sentID)
	}

	next, cmd = got.Update(MailboxSyncedMsg{MailboxID: inboxID})
	if cmd == nil {
		t.Fatal("completing the account sync should rearm the selected folder")
	}
	if next.(Model).folderSettlePending != 0 {
		t.Fatal("rearmed folder should no longer remain pending")
	}
}

func TestSuccessfulSyncUpdatesFreshnessImmediately(t *testing.T) {
	m, sentID := sentFolderModel(t, 0)
	syncedAt := time.Now()

	next, _ := m.Update(MailboxSyncedMsg{MailboxID: sentID, SyncedAt: syncedAt})
	got := next.(Model).mailboxByID(sentID)
	if got == nil || !got.LastSynced.Equal(syncedAt) {
		t.Fatalf("LastSynced = %v, want %v", got, syncedAt)
	}
	if next.(Model).shouldAutoSyncFolder(*got) {
		t.Fatal("a just-completed sync must be fresh before the account reload lands")
	}
}
