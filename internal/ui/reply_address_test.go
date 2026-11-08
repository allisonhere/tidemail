package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// Issue #102: a sender whose display name holds a comma is stored decoded and
// unquoted ("Merk, Philipp <p@x>"); replying must quote it again so To parses.
func TestReplyToSenderWithCommaInNameIsSendable(t *testing.T) {
	me := config.AccountConfig{ID: "a", User: "me@example.com"}
	original := db.Message{
		From:    "Merk, Philipp <Philipp.Merk@tdsynnex.com>",
		To:      "me@example.com",
		Subject: "daten",
	}
	for name, c := range map[string]ComposeModel{
		"reply":     NewReply(original, me, []config.AccountConfig{me}, nil),
		"reply all": NewReplyAll(original, me, []config.AccountConfig{me}, nil),
	} {
		to := c.toInput.Value()
		if want := `"Merk, Philipp" <philipp.merk@tdsynnex.com>`; to != want {
			t.Errorf("%s: To = %q, want %q", name, to, want)
		}
		if msg := validateAddressList(to); msg != "" {
			t.Errorf("%s: To %q does not validate: %s", name, to, msg)
		}
	}
}

func TestReplyKeepsAWellFormedSenderList(t *testing.T) {
	me := config.AccountConfig{ID: "a", User: "me@example.com"}
	original := db.Message{ReplyTo: "list@example.com, Bob <bob@example.com>", From: "x@example.com"}
	c := NewReply(original, me, []config.AccountConfig{me}, nil)
	got := c.toInput.Value()
	if !strings.HasPrefix(got, "list@example.com, ") || !strings.Contains(got, "<bob@example.com>") {
		t.Fatalf("To = %q", got)
	}
	if msg := validateAddressList(got); msg != "" {
		t.Fatalf("To %q does not validate: %s", got, msg)
	}
}

// Issue #103: the footer names why the grammar check failed.
func TestGrammarFailedStatusShowsReason(t *testing.T) {
	got := grammarFailedStatus(errors.New("claude: HTTP 404: model: claude-sonnet-4\n"))
	if got != "grammar check failed: claude: HTTP 404: model: claude-sonnet-4" {
		t.Fatalf("status = %q", got)
	}
	long := grammarFailedStatus(errors.New(strings.Repeat("x", 500)))
	if n := len([]rune(long)); n > 160 {
		t.Fatalf("status is %d runes, want it capped", n)
	}
}

func TestClaudeModelIndexMapsLegacyNames(t *testing.T) {
	for legacy, want := range map[string]string{
		"claude-haiku": "claude-haiku-5-5", "claude-sonnet-4": "claude-sonnet-5-5", "claude-opus-4": "claude-opus-5-5", "": "claude-haiku-5-5",
	} {
		if got := claudeModelLabels[claudeModelIndex(legacy)]; got != want {
			t.Errorf("claudeModelIndex(%q) picks %q, want %q", legacy, got, want)
		}
	}
}
