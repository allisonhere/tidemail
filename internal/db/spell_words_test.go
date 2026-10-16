package db

import (
	"slices"
	"testing"
)

func TestSpellWords(t *testing.T) {
	d := newTestDB(t)
	if err := d.AddSpellWord("  Zorblax "); err != nil {
		t.Fatal(err)
	}
	if err := d.AddSpellWord("zorblax"); err != nil { // duplicate, differently cased
		t.Fatal(err)
	}
	if err := d.AddSpellWord("Allie"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddSpellWord("   "); err != nil { // ignored
		t.Fatal(err)
	}
	got, err := d.ListSpellWords()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"allie", "zorblax"}; !slices.Equal(got, want) {
		t.Fatalf("ListSpellWords = %v; want %v", got, want)
	}
	if err := d.RemoveSpellWord("Zorblax"); err != nil {
		t.Fatal(err)
	}
	got, _ = d.ListSpellWords()
	if want := []string{"allie"}; !slices.Equal(got, want) {
		t.Fatalf("after remove = %v; want %v", got, want)
	}
}
