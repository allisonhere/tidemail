package ui

import (
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// TestSyncPollIntervalModes pins the three-way meaning of sync_minutes. The
// value 0 is the easy one to get wrong: it reads like "off" but means push, so
// it still needs a (slow) safety poll.
func TestSyncPollIntervalModes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		syncMinutes int
		want        time.Duration
		wantOK      bool
	}{
		{"manual only", -1, 0, false},
		{"push gets the safety poll", 0, pushSafetyPollInterval, true},
		{"explicit interval", 5, 5 * time.Minute, true},
		{"large interval", 120, 2 * time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := syncPollInterval(tc.syncMinutes)
			if ok != tc.wantOK {
				t.Fatalf("syncPollInterval(%d) ok = %v, want %v", tc.syncMinutes, ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Fatalf("syncPollInterval(%d) = %v, want %v", tc.syncMinutes, got, tc.want)
			}
		})
	}
}

func TestScheduleNextSyncHonoursMode(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{
		{Name: "Poll", SyncMinutes: 5},
		{Name: "Push", SyncMinutes: 0},
		{Name: "ManualOnly", SyncMinutes: -1},
	}
	m := NewModel(database, cfg, "dev", false)
	m.accounts = []db.Account{{ID: 1, Name: "Poll"}, {ID: 2, Name: "Push"}, {ID: 3, Name: "ManualOnly"}}

	if m.armSyncTimer(1) == nil {
		t.Fatal("expected a timer for a polling account")
	}
	// Push still gets a timer — the safety poll — even though IDLE does the work.
	if m.armSyncTimer(2) == nil {
		t.Fatal("expected a safety-poll timer for a push account")
	}
	if m.armSyncTimer(3) != nil {
		t.Fatal("expected no timer for a manual-only account")
	}
}

// TestSyncMailboxCmdGuardsConcurrentSyncs covers the re-entrancy guard. Launch
// timers, the startup sweep, and IDLE nudges all target the inbox, and
// overlapping syncs of one mailbox collide on the DB write (SQLITE_BUSY),
// aborting one sync partway through storing while its sibling still advances
// last_synced — which drops those messages out of the next sync window.
func TestSyncMailboxCmdGuardsConcurrentSyncs(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}

	m := NewModel(database, config.DefaultConfig(), "dev", false)

	if cmd := m.syncMailboxCmd(mailboxID, false); cmd == nil {
		t.Fatal("expected the first sync of an idle mailbox to be admitted")
	}
	if cmd := m.syncMailboxCmd(mailboxID, false); cmd != nil {
		t.Fatal("expected a second concurrent sync of the same mailbox to be refused")
	}

	// The completion message is the only thing that clears the guard, so a sync
	// that never reports would wedge the mailbox permanently.
	next, _ := m.Update(MailboxSyncedMsg{MailboxID: mailboxID})
	m = next.(Model)

	if m.syncing[mailboxID] {
		t.Fatal("expected MailboxSyncedMsg to clear the in-flight marker")
	}
	if cmd := m.syncMailboxCmd(mailboxID, false); cmd == nil {
		t.Fatal("expected a later sync to be admitted once the mailbox is idle again")
	}
}

// A refused manual sync must say so — a keypress that silently does nothing
// reads as the app being broken.
func TestSyncMailboxCmdReportsRefusedManualSync(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.syncing[42] = true

	if cmd := m.syncMailboxCmd(42, true); cmd == nil {
		t.Fatal("expected a refused manual sync to return a status-clear command")
	}
	if m.statusMsg == "" {
		t.Fatal("expected a refused manual sync to set a status message")
	}

	m.statusMsg = ""
	if cmd := m.syncMailboxCmd(42, false); cmd != nil {
		t.Fatal("expected a refused auto sync to return no command")
	}
	if m.statusMsg != "" {
		t.Fatalf("expected a refused auto sync to stay silent, got status %q", m.statusMsg)
	}
}

// TestParseSyncMinutes covers the field that cannot fall back to a default the
// way the port fields do: every unparseable string yields 0, which now means
// push, so a typo would silently opt an account into a persistent connection.
func TestParseSyncMinutes(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		want   int
		wantOK bool
	}{
		{"5", 5, true},
		{"0", 0, true},
		{"-1", -1, true},
		{" 15 ", 15, true},
		{"", 0, true}, // new account taking the push default
		{"abc", 0, false},
		{"5m", 0, false},
		{"-7", 0, false}, // below the -1 floor; must not read as manual
		{"1.5", 0, false},
	} {
		got, ok := parseSyncMinutes(tc.raw)
		if ok != tc.wantOK {
			t.Fatalf("parseSyncMinutes(%q) ok = %v, want %v", tc.raw, ok, tc.wantOK)
		}
		if ok && got != tc.want {
			t.Fatalf("parseSyncMinutes(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

// A bad refresh value must block save rather than quietly becoming push.
func TestValidateFormRejectsBadSyncMinutes(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.nameInput.SetValue("Personal")
	am.imapHostInput.SetValue("imap.example.com")
	am.smtpHostInput.SetValue("smtp.example.com")
	am.userInput.SetValue("alice@example.com")

	am.syncInput.SetValue("abc")
	if status := am.validateForm(am.buildCfg()); status == "" {
		t.Fatal("expected an unparseable refresh value to fail validation")
	}

	am.syncInput.SetValue("0")
	if status := am.validateForm(am.buildCfg()); status != "" {
		t.Fatalf("expected push mode to validate, got %q", status)
	}

	am.syncInput.SetValue("-1")
	if status := am.validateForm(am.buildCfg()); status != "" {
		t.Fatalf("expected manual-only mode to validate, got %q", status)
	}
}

// TestAutoSyncRetiresSupersededTimerChain is the regression test for auto-sync
// timer chains multiplying. tea.Every cannot be cancelled, so re-arming an
// account's timer used to leave the previous self-rescheduling chain running
// alongside the new one. Each chain reschedules itself forever, so every account
// edit permanently added another stream of syncs and connections.
//
// A tick reschedules by returning a command; a retired tick must return none,
// which is what lets the old chain die out.
func TestAutoSyncRetiresSupersededTimerChain(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: "Poll", SyncMinutes: 5}}
	m := NewModel(nil, cfg, "dev", false)
	m.accounts = []db.Account{{ID: 1, Name: "Poll"}}

	if cmd := m.armSyncTimer(1); cmd == nil {
		t.Fatal("expected a timer for a polling account")
	}
	retired := m.syncGen[1]

	// Re-arm, as saving the account does.
	if cmd := m.armSyncTimer(1); cmd == nil {
		t.Fatal("expected the re-armed timer")
	}
	live := m.syncGen[1]
	if live == retired {
		t.Fatal("re-arming must supersede the previous generation")
	}

	next, cmd := m.Update(AutoSyncMsg{AccountID: 1, Gen: retired})
	if cmd != nil {
		t.Fatal("a tick from the retired chain rescheduled itself; the old chain is still alive")
	}
	m = next.(Model)

	if _, cmd := m.Update(AutoSyncMsg{AccountID: 1, Gen: live}); cmd == nil {
		t.Fatal("the live chain stopped rescheduling itself")
	}
}

// TestAutoSyncZeroGenerationIsNeverLive verifies a zero-value tick cannot keep a
// chain alive. Generations start at 1 so the zero value never matches, which
// matters because the map returns 0 for an account that has no armed chain.
func TestAutoSyncZeroGenerationIsNeverLive(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: "Poll", SyncMinutes: 5}}
	m := NewModel(nil, cfg, "dev", false)
	m.accounts = []db.Account{{ID: 1, Name: "Poll"}}

	// Never armed: nothing is live, so nothing should reschedule.
	if _, cmd := m.Update(AutoSyncMsg{AccountID: 1}); cmd != nil {
		t.Fatal("a zero-generation tick armed a chain that was never started")
	}
	if m.armSyncTimer(1) == nil {
		t.Fatal("expected a timer")
	}
	if _, cmd := m.Update(AutoSyncMsg{AccountID: 1}); cmd != nil {
		t.Fatal("a zero-generation tick kept running alongside the live chain")
	}
}

// TestAccountSavedRetiresPreviousTimerChain covers the path that actually caused
// the accumulation: saving an account re-arms its timer, so any chain armed
// before the save must stop rescheduling. Changing sync_minutes otherwise left
// the old chain ticking at the old interval as well as the new one.
func TestAccountSavedRetiresPreviousTimerChain(t *testing.T) {
	orig := configSave
	configSave = func(config.Config) error { return nil }
	defer func() { configSave = orig }()

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: "Poll", SyncMinutes: 5}}
	m := NewModel(nil, cfg, "dev", false)
	m.accounts = []db.Account{{ID: 1, Name: "Poll"}}

	if m.armSyncTimer(1) == nil {
		t.Fatal("expected an initial timer")
	}
	before := m.syncGen[1]

	next, _ := m.Update(AccountSavedMsg{
		Account:    db.Account{ID: 1, Name: "Poll"},
		AccountCfg: config.AccountConfig{Name: "Poll", SyncMinutes: 15},
	})
	m = next.(Model)

	if m.syncGen[1] == before {
		t.Fatal("saving the account left the previous timer chain live")
	}
	if _, cmd := m.Update(AutoSyncMsg{AccountID: 1, Gen: before}); cmd != nil {
		t.Fatal("the pre-save chain is still rescheduling itself at the old interval")
	}
}
