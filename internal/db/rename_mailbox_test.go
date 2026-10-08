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
