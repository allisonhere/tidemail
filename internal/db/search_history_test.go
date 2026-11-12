package db

import (
	"fmt"
	"reflect"
	"testing"
)

func TestSearchHistoryOrderDedupeAndPrefix(t *testing.T) {
	d := newTestDB(t)
	for _, q := range []string{"to:work@example.com", "invoice", "  ", "From:pat", "invoice"} {
		if err := d.RecordSearch(q); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.RecentSearches("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"invoice", "From:pat", "to:work@example.com"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %v, want %v (newest first, repeat moved up, blank skipped)", got, want)
	}
	// A repeat that differs only by case is the same entry, shown as last typed.
	if err := d.RecordSearch("INVOICE"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.RecentSearches("inv", 10); !reflect.DeepEqual(got, []string{"INVOICE"}) {
		t.Fatalf("prefix inv = %v", got)
	}
	if got, _ := d.RecentSearches("TO:", 10); !reflect.DeepEqual(got, []string{"to:work@example.com"}) {
		t.Fatalf("prefix is case-insensitive: %v", got)
	}
	// LIKE wildcards in what the user typed are literal.
	if got, _ := d.RecentSearches("%", 10); len(got) != 0 {
		t.Fatalf("'%%' matched %v", got)
	}
}

func TestSearchHistoryCapDeleteClear(t *testing.T) {
	d := newTestDB(t)
	for i := 0; i < MaxSearchHistory+5; i++ {
		if err := d.RecordSearch(fmt.Sprintf("query %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := d.RecentSearches("", 100)
	if len(got) != MaxSearchHistory || got[0] != fmt.Sprintf("query %d", MaxSearchHistory+4) {
		t.Fatalf("kept %d entries, first %q", len(got), got[0])
	}
	if err := d.DeleteSearch(got[0]); err != nil {
		t.Fatal(err)
	}
	if after, _ := d.RecentSearches("", 100); len(after) != MaxSearchHistory-1 {
		t.Fatalf("delete left %d", len(after))
	}
	if err := d.ClearSearchHistory(); err != nil {
		t.Fatal(err)
	}
	if after, _ := d.RecentSearches("", 100); len(after) != 0 {
		t.Fatalf("clear left %d", len(after))
	}
}
