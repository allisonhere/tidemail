package mailcore

import (
	"strings"
	"testing"
)

func TestBuildDraftCarriesMessageIDAndAllowsNoRecipients(t *testing.T) {
	account := Account{User: "me@example.com", From: "me@example.com"}
	raw, id := BuildDraft(account, OutgoingMessage{From: "me@example.com", Subject: "Hello", Body: "Body text"})
	text := string(raw)
	if id == "" || !strings.Contains(text, "Message-ID: "+id) {
		t.Fatalf("draft should carry its Message-ID %q:\n%s", id, text)
	}
	for _, want := range []string{"From: me@example.com", "Subject: Hello", "Body text"} {
		if !strings.Contains(text, want) {
			t.Errorf("draft missing %q:\n%s", want, text)
		}
	}
}

func TestBuildDraftIncludesAttachmentsAndRecipients(t *testing.T) {
	account := Account{User: "me@example.com"}
	raw, _ := BuildDraft(account, OutgoingMessage{
		From: "me@example.com", To: []string{"a@x.com"}, CC: []string{"b@y.com"}, Subject: "S", Body: "B",
		Attachments: []OutgoingAttachment{{Name: "note.txt", Data: []byte("hi")}},
	})
	text := string(raw)
	for _, want := range []string{"To: a@x.com", "Cc: b@y.com", "note.txt", "multipart/mixed"} {
		if !strings.Contains(text, want) {
			t.Errorf("draft missing %q", want)
		}
	}
}
