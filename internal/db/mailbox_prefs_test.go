package db

import "testing"

func TestMailboxPrefsRoundTripAndFollowRename(t *testing.T) {
	d := newTestDB(t)
	acc, err := d.AddAccount("", "Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Work", "Work/Projects"} {
		if _, err := d.UpsertMailbox(Mailbox{AccountID: acc, Name: name, Delimiter: "/"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.SetMailboxHidden(acc, "Work", true); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMailboxOrders(acc, map[string]int{"Work": 10, "Work/Projects": 20}); err != nil {
		t.Fatal(err)
	}
	prefs, err := d.ListMailboxPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if got := prefs[acc]["Work"]; !got.Hidden || got.Order != 10 {
		t.Fatalf("Work pref = %+v", got)
	}

	if err := d.RenameMailboxTree(acc, "Work", "Office", "/", func(n string) string { return n }); err != nil {
		t.Fatal(err)
	}
	prefs, _ = d.ListMailboxPrefs()
	if !prefs[acc]["Office"].Hidden || prefs[acc]["Office/Projects"].Order != 20 {
		t.Fatalf("prefs did not follow the rename: %+v", prefs[acc])
	}
	if _, stale := prefs[acc]["Work"]; stale {
		t.Fatalf("old name should be gone: %+v", prefs[acc])
	}

	if err := d.DeleteMailboxPrefs(acc, "Office"); err != nil {
		t.Fatal(err)
	}
	prefs, _ = d.ListMailboxPrefs()
	if _, ok := prefs[acc]["Office"]; ok {
		t.Fatal("pref should be deleted")
	}
}
