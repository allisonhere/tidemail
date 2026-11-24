package ui

import "testing"

func TestFuzzyScoreMatchesSubsequences(t *testing.T) {
	for _, tc := range []struct {
		query, target string
		want          bool
	}{
		{"", "anything", true},
		{"acin", "Projects/Acme/Invoices", true},
		{"INV", "invoices", true},
		{"a c", "Acme", true}, // spaces in the query are ignored
		{"xyz", "Projects/Acme", false},
		{"ba", "ab", false}, // order matters
	} {
		if _, ok := fuzzyScore(tc.query, tc.target); ok != tc.want {
			t.Errorf("fuzzyScore(%q, %q) matched=%v, want %v", tc.query, tc.target, ok, tc.want)
		}
	}
}

func TestRankFoldersPrefersTighterMatches(t *testing.T) {
	cands := []folderCandidate{
		{1, "Archive/2019/Notes"},
		{2, "Projects/Acme/Invoices"},
		{3, "Invoices"},
		{4, "Misc/Inbox/Venue Offices"},
	}
	got := rankFolders("inv", cands)
	if len(got) < 2 || got[0].mailboxID != 3 || got[1].mailboxID != 2 {
		t.Fatalf("rankFolders(inv) = %+v", got)
	}
	if got := rankFolders("acin", cands); len(got) == 0 || got[0].mailboxID != 2 {
		t.Fatalf("rankFolders(acin) = %+v", got)
	}
	if got := rankFolders("  ", cands); len(got) != len(cands) || got[0].mailboxID != 1 {
		t.Fatalf("empty query should keep order, got %+v", got)
	}
	if got := rankFolders("zzz", cands); len(got) != 0 {
		t.Fatalf("no match should be empty, got %+v", got)
	}
}
