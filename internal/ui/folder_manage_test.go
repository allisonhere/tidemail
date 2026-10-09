package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// folderManageModel is a real DB with INBOX, Work, Work/Projects, Work/Projects/Alpha
// and Sent, and no IMAPHost, so the commands persist locally without a network call.
func folderManageModel(t *testing.T) (Model, *db.DB, int64) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	accountID, err := database.AddAccount("", "Personal", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	for _, mb := range []db.Mailbox{
		{Name: "INBOX"}, {Name: "Sent", Flags: []string{`\Sent`}}, {Name: "Work"},
		{Name: "Work/Projects"}, {Name: "Work/Projects/Alpha"},
	} {
		mb.AccountID, mb.Delimiter = accountID, "/"
		if _, err := database.UpsertMailbox(mb); err != nil {
			t.Fatalf("UpsertMailbox: %v", err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: "Personal"}}
	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accountID, Name: "Personal"}},
		Mailboxes: mustListMailboxes(t, database, accountID),
	})
	m = next.(Model)
	m.width, m.height = 100, 40
	m.focused = paneAccounts
	return m, database, accountID
}

func mailboxIDByName(t *testing.T, m Model, name string) int64 {
	t.Helper()
	for _, mb := range m.mailboxes {
		if mb.Name == name {
			return mb.ID
		}
	}
	t.Fatalf("no mailbox %q", name)
	return 0
}

func pressKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	next, cmd := m.Update(k)
	return next.(Model), cmd
}

func typeText(m Model, text string) Model {
	for _, r := range text {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func applyCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

func namesOf(m Model) map[string]bool {
	out := map[string]bool{}
	for _, mb := range m.mailboxes {
		out[mb.Name] = true
	}
	return out
}

func TestNewFolderKeyCreatesASubfolderOfTheFocusedFolder(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !m.folderPrompt.active || m.folderPrompt.parent != "Work" {
		t.Fatalf("n should open a prompt under Work, got %+v", m.folderPrompt)
	}
	m = typeText(m, "Clients")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.folderPrompt.active {
		t.Fatal("prompt should close on enter")
	}
	m = applyCmd(t, m, cmd)

	if !namesOf(m)["Work/Clients"] {
		t.Fatalf("Work/Clients not created: %v", namesOf(m))
	}
	if got := mustListMailboxes(t, database, accountID); len(got) != 6 {
		t.Fatalf("DB has %d mailboxes, want 6", len(got))
	}
}

func TestNewFolderOnAnAccountHeaderCreatesATopLevelFolder(t *testing.T) {
	m, _, _ := folderManageModel(t)
	for i, r := range m.sidebarRows {
		if r.kind == rowKindAccount {
			m.sidebarCursor = i
		}
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = typeText(m, "Receipts")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = applyCmd(t, m, cmd)
	if !namesOf(m)["Receipts"] {
		t.Fatalf("Receipts not created: %v", namesOf(m))
	}
}

func TestNewFolderRejectsClashesAndDelimiters(t *testing.T) {
	m, _, _ := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	m = typeText(m, "projects") // clashes with Work/Projects, case-insensitively
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !m.folderPrompt.active {
		t.Fatalf("a clashing name must keep the prompt open")
	}
	if !m.statusErr {
		t.Fatalf("expected an error status, got %q", m.statusMsg)
	}

	m.folderPrompt.input.SetValue("a/b")
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !m.folderPrompt.active {
		t.Fatalf("a name containing the delimiter must be rejected")
	}

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.folderPrompt.active {
		t.Fatalf("esc should cancel the prompt")
	}
}

func TestRenameFolderRenamesItsSubfolders(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))
	alphaID := mailboxIDByName(t, m, "Work/Projects/Alpha")

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !m.folderPrompt.active || !m.folderPrompt.rename || m.folderPrompt.input.Value() != "Work" {
		t.Fatalf("r should open a rename prompt prefilled with the name, got %+v", m.folderPrompt)
	}
	m.folderPrompt.input.SetValue("Office")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = applyCmd(t, m, cmd)

	names := namesOf(m)
	for _, want := range []string{"Office", "Office/Projects", "Office/Projects/Alpha"} {
		if !names[want] {
			t.Fatalf("%q missing after rename: %v", want, names)
		}
	}
	if names["Work"] || names["Work/Projects"] {
		t.Fatalf("old names should be gone: %v", names)
	}
	if got := m.mailboxByID(alphaID); got == nil || got.Name != "Office/Projects/Alpha" {
		t.Fatalf("Alpha kept its ID but not its new name: %+v", got)
	}
	persisted := map[string]bool{}
	for _, mb := range mustListMailboxes(t, database, accountID) {
		persisted[mb.Name] = true
	}
	if !persisted["Office/Projects/Alpha"] || persisted["Work"] {
		t.Fatalf("DB not renamed: %v", persisted)
	}
}

func TestRenameAndDeleteRefuseSystemFolders(t *testing.T) {
	m, _, _ := folderManageModel(t)
	for _, name := range []string{"INBOX", "Sent"} {
		cursorOnMailbox(t, &m, mailboxIDByName(t, m, name))
		next, _ := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
		if next.folderPrompt.active {
			t.Fatalf("%s must not be renamable", name)
		}
		next, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
		if next.overlay != overlayNone {
			t.Fatalf("%s must not be deletable", name)
		}
		if !next.statusErr {
			t.Fatalf("%s: expected an explanatory error status", name)
		}
	}
}

func TestDeleteFolderConfirmsAndRemovesTheSubtree(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if m.overlay != overlayFolderDeleteConfirm {
		t.Fatalf("d should ask for confirmation, overlay = %v", m.overlay)
	}
	if got := m.folderDeleteConfirmText(); !strings.Contains(got, "2 subfolders") {
		t.Fatalf("confirmation should mention the subfolders, got %q", got)
	}

	// Any other key cancels and deletes nothing.
	cancelled, _ := pressKey(m, tea.KeyMsg{Type: tea.KeyEscape})
	if cancelled.overlay != overlayNone || len(cancelled.mailboxes) != 5 {
		t.Fatalf("esc must cancel without deleting")
	}

	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = applyCmd(t, m, cmd)
	names := namesOf(m)
	if names["Work"] || names["Work/Projects"] || names["Work/Projects/Alpha"] {
		t.Fatalf("subtree should be gone: %v", names)
	}
	if !names["INBOX"] || !names["Sent"] {
		t.Fatalf("unrelated folders were removed: %v", names)
	}
	if got := mustListMailboxes(t, database, accountID); len(got) != 2 {
		t.Fatalf("DB has %d mailboxes, want 2", len(got))
	}
}

func TestHideFolderHidesItsSubtreeAndPersists(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	for _, id := range folderRowIDs(m) {
		if name := m.mailboxByID(id).Name; strings.HasPrefix(name, "Work") {
			t.Fatalf("%s should be hidden with its parent", name)
		}
	}
	if len(m.mailboxes) != 5 {
		t.Fatalf("hiding must not delete anything locally, have %d mailboxes", len(m.mailboxes))
	}
	prefs, err := database.ListMailboxPrefs()
	if err != nil || !prefs[accountID]["Work"].Hidden {
		t.Fatalf("hide not persisted: %v %v", prefs, err)
	}

	// Show hidden folders from the palette: they come back, marked.
	next, _ := m.executeCommand("show-hidden-folders")
	m = next.(Model)
	var marked bool
	for _, r := range m.sidebarRows {
		if r.kind == rowKindMailbox && r.mailboxID == mailboxIDByName(t, m, "Work") {
			marked = r.hidden
		}
	}
	if !marked || len(folderRowIDs(m)) < 4 {
		t.Fatalf("hidden folders should be listed (marked) when shown: %v", folderRowIDs(m))
	}

	// H again, with hidden folders shown, unhides.
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	if m.mailboxPrefs[accountID]["Work"].Hidden {
		t.Fatal("second H should unhide")
	}
}

func TestInboxCannotBeHidden(t *testing.T) {
	m, _, _ := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "INBOX"))
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	if len(m.mailboxPrefs) != 0 || !m.statusErr {
		t.Fatalf("Inbox must refuse to hide, prefs=%v", m.mailboxPrefs)
	}
}

func TestShiftJKReorderSiblingFolders(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	// Top level is INBOX, Sent, Work in rank/name order; add Alpha and Zed.
	for _, name := range []string{"Alpha", "Zed"} {
		id, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: name, Delimiter: "/"})
		if err != nil {
			t.Fatal(err)
		}
		m.mailboxes = append(m.mailboxes, db.Mailbox{ID: id, AccountID: accountID, Name: name, Delimiter: "/"})
	}
	// Work is collapsed, so Zed steps past it instead of walking into it.
	m.collapsedSections[folderCollapseKey(*m.mailboxByID(mailboxIDByName(t, m, "Work")))] = true
	m.rebuildSidebar()

	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Zed"))
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}}) // up
	var top []string
	for _, r := range m.sidebarRows {
		if r.kind == rowKindMailbox && r.depth == 0 {
			top = append(top, m.mailboxByID(r.mailboxID).Name)
		}
	}
	zed, work := slices.Index(top, "Zed"), slices.Index(top, "Work")
	if zed > work {
		t.Fatalf("Zed should have moved up past Work, got %v", top)
	}
	if m.sidebarRows[m.sidebarCursor].mailboxID != mailboxIDByName(t, m, "Zed") {
		t.Fatal("cursor should follow the moved folder")
	}
	prefs, _ := database.ListMailboxPrefs()
	if prefs[accountID]["Zed"].Order == 0 {
		t.Fatalf("order not persisted: %v", prefs)
	}
}

func topLevelNames(m Model) []string {
	var out []string
	for _, r := range m.sidebarRows {
		if r.kind == rowKindMailbox && r.depth == 0 {
			out = append(out, m.mailboxByID(r.mailboxID).Name)
		}
	}
	return out
}

func TestShiftKOnAFirstChildMovesItOutBeforeItsParent(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work/Projects"))
	projectsID := mailboxIDByName(t, m, "Work/Projects")
	alphaID := mailboxIDByName(t, m, "Work/Projects/Alpha")

	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = applyCmd(t, m, cmd)

	names := namesOf(m)
	if !names["Projects"] || !names["Projects/Alpha"] || names["Work/Projects"] || names["Work/Projects/Alpha"] {
		t.Fatalf("Projects should be top level with its subfolder: %v", names)
	}
	if got := m.mailboxByID(alphaID); got == nil || got.Name != "Projects/Alpha" {
		t.Fatalf("Alpha kept its ID but not its new name: %+v", got)
	}
	top := topLevelNames(m)
	if slices.Index(top, "Projects") != slices.Index(top, "Work")-1 {
		t.Fatalf("Projects should sit just before Work, got %v", top)
	}
	if m.sidebarRows[m.sidebarCursor].mailboxID != projectsID {
		t.Fatal("cursor should follow the moved folder")
	}
	persisted := map[string]bool{}
	for _, mb := range mustListMailboxes(t, database, accountID) {
		persisted[mb.Name] = true
	}
	if !persisted["Projects/Alpha"] {
		t.Fatalf("DB not updated: %v", persisted)
	}
}

func TestShiftJOnALastChildMovesItOutAfterItsParent(t *testing.T) {
	m, _, _ := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work/Projects/Alpha"))

	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	m = applyCmd(t, m, cmd)

	if !namesOf(m)["Work/Alpha"] || namesOf(m)["Work/Projects/Alpha"] {
		t.Fatalf("Alpha should now be a child of Work: %v", namesOf(m))
	}
	// It sits right after Projects inside Work.
	var siblings []string
	for _, r := range m.sidebarRows {
		if r.kind == rowKindMailbox && r.depth == 1 {
			siblings = append(siblings, m.mailboxByID(r.mailboxID).Name)
		}
	}
	if !slices.Equal(siblings, []string{"Work/Projects", "Work/Alpha"}) {
		t.Fatalf("Alpha should come right after Projects, got %v", siblings)
	}
}

func TestShiftJKSwapsAMiddleChildInsteadOfMovingItOut(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	for _, name := range []string{"Work/Alpha", "Work/Zeta"} {
		id, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: name, Delimiter: "/"})
		if err != nil {
			t.Fatal(err)
		}
		m.mailboxes = append(m.mailboxes, db.Mailbox{ID: id, AccountID: accountID, Name: name, Delimiter: "/"})
	}
	m.rebuildSidebar() // Work/{Alpha, Projects, Zeta}
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work/Projects"))

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	if !namesOf(m)["Work/Projects"] {
		t.Fatalf("a middle child must swap, not leave Work: %v", namesOf(m))
	}
	var kids []string
	for _, r := range m.sidebarRows {
		if r.kind == rowKindMailbox && r.depth == 1 {
			kids = append(kids, m.mailboxByID(r.mailboxID).Name)
		}
	}
	if len(kids) < 2 || kids[0] != "Work/Projects" || kids[1] != "Work/Alpha" {
		t.Fatalf("Projects should now precede Alpha, got %v", kids)
	}
}

func TestMovingAFolderOutRefusesANameClashAndSystemFolders(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	id, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "Projects", Delimiter: "/"})
	if err != nil {
		t.Fatal(err)
	}
	m.mailboxes = append(m.mailboxes, db.Mailbox{ID: id, AccountID: accountID, Name: "Projects", Delimiter: "/"})
	m.rebuildSidebar()

	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work/Projects"))
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	if !m.statusErr || !strings.Contains(m.statusMsg, "already exists") {
		t.Fatalf("a clash must be refused with an error, got %q", m.statusMsg)
	}
	if !namesOf(m)["Work/Projects"] {
		t.Fatal("the folder must not have moved")
	}
}

func TestMKeyInTheSidebarDoesNotOpenAPicker(t *testing.T) {
	m, _, _ := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work/Projects"))
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if m.overlay != overlayNone {
		t.Fatalf("m in the sidebar should do nothing, overlay = %v", m.overlay)
	}
}

// walkModel has Alpha above an open Work (Notes, Projects) and Zed below it.
func walkModel(t *testing.T) (Model, *db.DB, int64) {
	t.Helper()
	m, database, accountID := folderManageModel(t)
	for _, name := range []string{"Beta", "Zed", "Work/Notes"} {
		id, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: name, Delimiter: "/"})
		if err != nil {
			t.Fatal(err)
		}
		m.mailboxes = append(m.mailboxes, db.Mailbox{ID: id, AccountID: accountID, Name: name, Delimiter: "/"})
	}
	m.rebuildSidebar()
	return m, database, accountID
}

func pressWalk(t *testing.T, m Model, k rune) Model {
	t.Helper()
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
	if cmd != nil && m.statusMsg == "moving folder..." {
		m = applyCmd(t, m, cmd)
	}
	return m
}

func rowDepth(m Model, id int64) int {
	for _, r := range m.sidebarRows {
		if r.kind == rowKindMailbox && r.mailboxID == id {
			return r.depth
		}
	}
	return -1
}

func personalOrder(m Model) []string {
	var out []string
	for _, r := range m.sidebarRows {
		if r.kind == rowKindMailbox {
			out = append(out, m.mailboxByID(r.mailboxID).Name)
		}
	}
	return out
}

func TestShiftJWalksAFolderIntoOpenParentsAndBackOut(t *testing.T) {
	m, database, accountID := walkModel(t)
	beta := mailboxIDByName(t, m, "Beta")
	cursorOnMailbox(t, &m, beta)

	// Beta sits above the open Work (Notes, Projects/Alpha). Each press moves it
	// one row; it is indented while it is inside a parent.
	steps := []struct {
		name  string
		depth int
	}{
		{"Work/Beta", 1},          // into Work, first child
		{"Work/Beta", 1},          // past Notes
		{"Work/Projects/Beta", 2}, // into Projects, first child
		{"Work/Projects/Beta", 2}, // past Projects/Alpha
		{"Work/Beta", 1},          // out of Projects, just after it
		{"Beta", 0},               // out of Work, just after it
	}
	for i, st := range steps {
		m = pressWalk(t, m, 'J')
		if got := m.mailboxByID(beta); got == nil || got.Name != st.name || rowDepth(m, beta) != st.depth {
			t.Fatalf("step %d: Beta = %v (depth %d), want %s at depth %d; tree %v", i+1, got, rowDepth(m, beta), st.name, st.depth, personalOrder(m))
		}
		if m.sidebarRows[m.sidebarCursor].mailboxID != beta {
			t.Fatalf("step %d: cursor left the folder", i+1)
		}
	}
	got := personalOrder(m)
	if slices.Index(got, "Beta") != slices.Index(got, "Zed")-1 {
		t.Fatalf("Beta should end between Work's tree and Zed, got %v", got)
	}
	persisted := map[string]bool{}
	for _, mb := range mustListMailboxes(t, database, accountID) {
		persisted[mb.Name] = true
	}
	if !persisted["Beta"] || persisted["Work/Beta"] {
		t.Fatalf("DB should end with a top-level Beta: %v", persisted)
	}

	// K walks it back the same way.
	for i := len(steps) - 2; i >= 0; i-- {
		m = pressWalk(t, m, 'K')
		if got := m.mailboxByID(beta); got == nil || got.Name != steps[i].name || rowDepth(m, beta) != steps[i].depth {
			t.Fatalf("back step %d: Beta = %v (depth %d), want %s at depth %d; tree %v", i+1, got, rowDepth(m, beta), steps[i].name, steps[i].depth, personalOrder(m))
		}
	}
}

func TestShiftJStepsPastACollapsedParentInsteadOfNesting(t *testing.T) {
	m, _, _ := walkModel(t)
	m.collapsedSections[folderCollapseKey(*m.mailboxByID(mailboxIDByName(t, m, "Work")))] = true
	m.rebuildSidebar()
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Beta"))
	m = pressWalk(t, m, 'J')
	if !namesOf(m)["Beta"] || namesOf(m)["Work/Beta"] {
		t.Fatalf("a collapsed parent must be stepped over, not nested into: %v", namesOf(m))
	}
}

func TestNestingRefusesANameClashAndNeverNestsIntoSystemFolders(t *testing.T) {
	m, database, accountID := walkModel(t)
	id, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "Work/Beta", Delimiter: "/"})
	if err != nil {
		t.Fatal(err)
	}
	m.mailboxes = append(m.mailboxes, db.Mailbox{ID: id, AccountID: accountID, Name: "Work/Beta", Delimiter: "/"})
	m.rebuildSidebar()
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Beta"))
	m = pressWalk(t, m, 'J')
	if !m.statusErr || !strings.Contains(m.statusMsg, "already exists") || !namesOf(m)["Beta"] {
		t.Fatalf("a clash must be refused and nothing may move: %q", m.statusMsg)
	}

	// Sent is a system folder with no subfolders; Work steps past it, never into it.
	m2, _, _ := walkModel(t)
	cursorOnMailbox(t, &m2, mailboxIDByName(t, m2, "Work"))
	m2 = pressWalk(t, m2, 'K')
	if namesOf(m2)["Sent/Work"] {
		t.Fatal("nothing may be nested into a system folder")
	}
}

func TestFolderMoveIsRefusedWhileAnotherFolderChangeIsRunning(t *testing.T) {
	m, _, _ := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))
	m.folderOpBusy = true
	before := m.sidebarCursor

	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	if cmd != nil && m.statusMsg == "moving folder..." {
		t.Fatal("a second move started while one was running")
	}
	if m.statusMsg != folderBusyMsg || m.sidebarCursor != before {
		t.Fatalf("want busy notice and no movement, got %q", m.statusMsg)
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if m.folderPrompt.active {
		t.Fatal("rename prompt opened while a folder change was running")
	}

	next, _ := m.Update(FolderRenamedMsg{AccountID: 1, OldName: "x", NewName: "y", Err: errors.New("boom")})
	if next.(Model).folderOpBusy {
		t.Fatal("a finished (even failed) folder change must free the guard")
	}
}

func TestAngleKeysNestIntoAnEmptyFolderAndBackOut(t *testing.T) {
	m, database, accountID := walkModel(t)
	id, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "Yak", Delimiter: "/"})
	if err != nil {
		t.Fatal(err)
	}
	m.mailboxes = append(m.mailboxes, db.Mailbox{ID: id, AccountID: accountID, Name: "Yak", Delimiter: "/"})
	m.rebuildSidebar()
	zed := mailboxIDByName(t, m, "Zed")
	cursorOnMailbox(t, &m, zed)

	// Yak has no subfolders, so Shift+J could never put Zed inside it.
	m = pressWalk(t, m, '>')
	if got := m.mailboxByID(zed); got == nil || got.Name != "Yak/Zed" || rowDepth(m, zed) != 1 {
		t.Fatalf("> should nest Zed under Yak, got %v; tree %v", got, personalOrder(m))
	}
	if m.sidebarRows[m.sidebarCursor].mailboxID != zed {
		t.Fatal("cursor left the folder")
	}
	m = pressWalk(t, m, '<')
	if got := m.mailboxByID(zed); got == nil || got.Name != "Zed" || rowDepth(m, zed) != 0 {
		t.Fatalf("< should move Zed back out, got %v; tree %v", got, personalOrder(m))
	}
	if got := personalOrder(m); got[len(got)-1] != "Zed" || got[len(got)-2] != "Yak" {
		t.Fatalf("Zed should sit just after Yak, got %v", got)
	}
	// Nothing above the first folder, and the top level has nowhere further out.
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Beta"))
	if m = pressWalk(t, m, '>'); m.mailboxByID(mailboxIDByName(t, m, "Beta")).Name != "Beta" {
		t.Fatal("Beta moved")
	}
	if m = pressWalk(t, m, '<'); m.statusMsg != "already at the top level" {
		t.Fatalf("got %q", m.statusMsg)
	}
}

func TestDeleteRefusesAParentWithAProtectedSubfolder(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	id, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "Work/Sent", Delimiter: "/", Flags: []string{`\Sent`}})
	if err != nil {
		t.Fatal(err)
	}
	m.mailboxes = append(m.mailboxes, db.Mailbox{ID: id, AccountID: accountID, Name: "Work/Sent", Delimiter: "/", Flags: []string{`\Sent`}})
	m.rebuildSidebar()
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if m.overlay == overlayFolderDeleteConfirm {
		t.Fatal("deleting Work would delete its \\Sent subfolder; it must be refused")
	}
	if !strings.Contains(m.statusMsg, "Sent") {
		t.Fatalf("status should name the protected subfolder, got %q", m.statusMsg)
	}
}

func TestMoveRefusesWhenASubfolderWouldCollide(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	// Moving Work (with Projects) to the top level is fine, but Other/Projects
	// already exists, and renaming Work to Other would put Projects on top of it.
	for _, name := range []string{"Other/Projects"} {
		id, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: name, Delimiter: "/"})
		if err != nil {
			t.Fatal(err)
		}
		m.mailboxes = append(m.mailboxes, db.Mailbox{ID: id, AccountID: accountID, Name: name, Delimiter: "/"})
	}
	m.rebuildSidebar()
	work := m.mailboxByID(mailboxIDByName(t, m, "Work"))

	next, _ := m.reparentFolder(*work, "Other", nil)
	m = next.(Model)
	if m.folderOpBusy || m.statusMsg == "moving folder..." {
		t.Fatal("a move whose subfolders would collide must not reach the server")
	}
	if !strings.Contains(m.statusMsg, "Projects") {
		t.Fatalf("status should name the clash, got %q", m.statusMsg)
	}
}

func TestHidingHiddenFoldersClearsTheListOfAHiddenSelectedFolder(t *testing.T) {
	m, _, accountID := folderManageModel(t)
	work := mailboxIDByName(t, m, "Work")
	m.setPref(accountID, "Work", func(p *db.MailboxPref) { p.Hidden = true })
	m.showHiddenFolders = true
	m.rebuildSidebar()
	cursorOnMailbox(t, &m, work)
	m.messages = []db.Message{{ID: 1}}

	next, _ := m.toggleShowHiddenFolders()
	m = next.(Model)
	if sel := m.selectedMailbox(); sel != nil && sel.ID != work && len(m.messages) != 0 {
		t.Fatalf("cursor left hidden Work for %s but its %d messages stayed", sel.Name, len(m.messages))
	}
}
