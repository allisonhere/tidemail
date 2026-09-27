package db

import (
	"reflect"
	"sort"
	"testing"
)

func (a *attentionDB) search(query string) []string {
	a.t.Helper()
	msgs, err := a.SearchAllMessages(query, false)
	if err != nil {
		a.t.Fatalf("search %q: %v", query, err)
	}
	out := []string{}
	for _, m := range msgs {
		out = append(out, m.Subject)
	}
	sort.Strings(out)
	return out
}

func TestParseSearch(t *testing.T) {
	got := ParseSearch("  invoice #Billing # #GitHub! due ##x.y ")
	want := SearchTerms{Text: "invoice due", Tags: []string{"billing", "github", "x.y"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseSearch = %+v, want %+v", got, want)
	}
}

func TestSearchByTag(t *testing.T) {
	a := newAttentionDB(t)
	a.add(a.inbox, "ci failed", 1, "smart category github")
	a.add(a.inbox, "invoice due", 2, "smart category billing", "smart needs_reply true")
	a.add(a.inbox2, "server down", 3, "smart urgency high", "smart category security")
	a.add(a.inbox, "board deck", 4, "smart importance high")
	a.add(a.trash, "old github mail", 5, "smart category github")
	a.add(a.inbox, "plain", 6)
	corrected := a.add(a.inbox, "pr review", 7, "smart category github")
	if err := a.SetClassificationOverride(corrected, ClassificationCategory, "personal"); err != nil {
		t.Fatal(err)
	}
	silenced := a.add(a.inbox, "fyi", 8, "smart needs_reply true")
	if err := a.SetClassificationOverride(silenced, ClassificationNeedsReply, "false"); err != nil {
		t.Fatal(err)
	}
	backToPlugin := a.add(a.inbox, "release notes", 9, "smart category github")
	if err := a.SetClassificationOverride(backToPlugin, ClassificationCategory, ClassificationPlugin); err != nil {
		t.Fatal(err)
	}
	a.add(a.inbox, "wildcard bait", 11, "smart category axb")
	added := a.add(a.inbox, "lunch", 10)
	if err := a.SetClassificationOverride(added, ClassificationNeedsReply, "true"); err != nil {
		t.Fatal(err)
	}

	cases := map[string][]string{
		"#github":         {"ci failed", "old github mail", "release notes"}, // every folder, like search
		"#git":            {"ci failed", "old github mail", "release notes"}, // prefix while typing
		"#GitHub":         {"ci failed", "old github mail", "release notes"},
		"#personal":       {"pr review"},            // the correction wins
		"#reply":          {"invoice due", "lunch"}, // plugin, or the user's own
		"#needs_reply":    {"invoice due", "lunch"}, // the full tag name works too
		"#a_b":            {},                       // "_" is literal, not a LIKE wildcard
		"#urgent":         {"server down"},
		"#important":      {"board deck"},
		"#billing #reply": {"invoice due"}, // every tag must match
		"invoice #bill":   {"invoice due"}, // text and tags together
		"due #github":     {},
		"#nosuchtag":      {},
	}
	for query, want := range cases {
		if got := a.search(query); !reflect.DeepEqual(got, want) {
			t.Errorf("search %q = %v, want %v", query, got, want)
		}
	}
	if got := a.search("#"); len(got) != 0 {
		t.Fatalf("a lone # searches nothing: %v", got)
	}
	// Plain text search is unchanged.
	if got := a.search("invoice"); !reflect.DeepEqual(got, []string{"invoice due"}) {
		t.Fatalf("text search = %v", got)
	}
}

func TestSearchTagCorrectedToNone(t *testing.T) {
	a := newAttentionDB(t)
	id := a.add(a.inbox, "spammy", 1, "smart category newsletter")
	if err := a.SetClassificationOverride(id, ClassificationCategory, ClassificationNone); err != nil {
		t.Fatal(err)
	}
	if got := a.search("#newsletter"); len(got) != 0 {
		t.Fatalf("a message corrected to no category has no category tag: %v", got)
	}
	if got := a.search("#none"); len(got) != 0 {
		t.Fatalf("none is not a tag: %v", got)
	}
}
