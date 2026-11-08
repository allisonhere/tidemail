package conversation

import "testing"

func TestParseParticipantsKeepsCommaInUnquotedName(t *testing.T) {
	got := ParseParticipants("Merk, Philipp <Philipp.Merk@tdsynnex.com>, bob@example.com")
	if len(got) != 2 {
		t.Fatalf("got %d participants: %+v", len(got), got)
	}
	if got[0].Name != "Merk, Philipp" || got[0].Addr != "philipp.merk@tdsynnex.com" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Addr != "bob@example.com" {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestParseParticipantsDropsTrailingNameWithoutAddress(t *testing.T) {
	got := ParseParticipants("a@example.com, Nobody")
	if len(got) != 1 || got[0].Addr != "a@example.com" {
		t.Fatalf("got %+v", got)
	}
}
