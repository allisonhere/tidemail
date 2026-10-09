package db

import "testing"

func TestRenameMailboxTreeMovesSubfoldersAndKeepsIDs(t *testing.T) {
	d := newTestDB(t)
	acc, err := d.AddAccount("", "Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, name := range []string{"Work", "Work/Projects", "Workshop", "Other"} {
		id, err := d.UpsertMailbox(Mailbox{AccountID: acc, Name: name, Delimiter: "/"})
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = id
	}

	if err := d.RenameMailboxTree(acc, "Work", "Office", "/", func(n string) string { return "D:" + n }); err != nil {
		t.Fatal(err)
	}
	got := map[int64]Mailbox{}
	list, _ := d.ListMailboxes(acc)
	for _, mb := range list {
		got[mb.ID] = mb
	}
	if got[ids["Work"]].Name != "Office" || got[ids["Work/Projects"]].Name != "Office/Projects" {
		t.Fatalf("tree not renamed: %+v", got)
	}
	if got[ids["Workshop"]].Name != "Workshop" || got[ids["Other"]].Name != "Other" {
		t.Fatalf("a sibling with a shared name prefix must not move: %+v", got)
	}
	if got[ids["Work/Projects"]].DisplayName != "D:Office/Projects" {
		t.Fatalf("display name = %q", got[ids["Work/Projects"]].DisplayName)
	}
	if err := d.RenameMailboxTree(acc, "Missing", "X", "/", func(n string) string { return n }); err == nil {
		t.Fatal("renaming a missing mailbox should fail")
	}
}

// SQLite's substr counts characters, not bytes, so a non-ASCII parent must
// still find its children.
func TestRenameMailboxTreeFindsChildrenOfANonASCIIParent(t *testing.T) {
	d := newTestDB(t)
	acc, _ := d.AddAccount("", "Personal", "")
	ids := map[string]int64{}
	for _, name := range []string{"Café", "Café/Projects"} {
		id, err := d.UpsertMailbox(Mailbox{AccountID: acc, Name: name, Delimiter: "/"})
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = id
	}
	if err := d.SetMailboxOrders(acc, map[string]int{"Café/Projects": 20}); err != nil {
		t.Fatal(err)
	}

	if err := d.RenameMailboxTree(acc, "Café", "Office", "/", func(n string) string { return n }); err != nil {
		t.Fatal(err)
	}
	list, _ := d.ListMailboxes(acc)
	names := map[int64]string{}
	for _, mb := range list {
		names[mb.ID] = mb.Name
	}
	if names[ids["Café/Projects"]] != "Office/Projects" {
		t.Fatalf("child was not renamed with its parent: %v", names)
	}
	prefs, _ := d.ListMailboxPrefs()
	if prefs[acc]["Office/Projects"].Order != 20 {
		t.Fatalf("child's pref did not follow: %+v", prefs[acc])
	}
}

// A leftover pref at the destination name (its folder was removed elsewhere)
// must not abort the rename with a primary-key error.
func TestRenameMailboxTreeToleratesAStalePrefAtTheNewName(t *testing.T) {
	d := newTestDB(t)
	acc, _ := d.AddAccount("", "Personal", "")
	for _, name := range []string{"Other"} {
		if _, err := d.UpsertMailbox(Mailbox{AccountID: acc, Name: name, Delimiter: "/"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.SetMailboxOrders(acc, map[string]int{"Other": 10, "Target": 50}); err != nil {
		t.Fatal(err)
	}
	if err := d.RenameMailboxTree(acc, "Other", "Target", "/", func(n string) string { return n }); err != nil {
		t.Fatalf("rename failed on a stale pref: %v", err)
	}
	prefs, _ := d.ListMailboxPrefs()
	if prefs[acc]["Target"].Order != 10 {
		t.Fatalf("the moved folder's own pref should win, got %+v", prefs[acc])
	}
}
