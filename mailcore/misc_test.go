package mailcore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
)

func TestProjectMessageCopiesEveryFieldAndAttachment(t *testing.T) {
	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	got := projectMessage(db.Message{
		UID: 7, From: "a@x", Subject: "s", BodyText: "t", BodyHTML: "<p>h</p>", To: "b@x", CC: "c@x",
		Date: when, Read: true, Starred: true, MessageID: "id@x",
		AttachmentData: []db.Attachment{
			{Filename: "f.pdf", ContentType: "application/pdf", ContentID: "cid1", Inline: true, Size: 3, Data: []byte("abc")},
			{Filename: "g.txt", ContentType: "text/plain", Size: 1, Data: []byte("z")},
		},
	})
	if got.UID != 7 || got.From != "a@x" || got.Subject != "s" || got.BodyText != "t" || got.BodyHTML != "<p>h</p>" ||
		got.To != "b@x" || got.CC != "c@x" || !got.Date.Equal(when) || !got.Read || !got.Starred || got.MessageID != "id@x" {
		t.Fatalf("fields not copied: %+v", got)
	}
	if len(got.Attachments) != 2 {
		t.Fatalf("got %d attachments, want 2", len(got.Attachments))
	}
	a := got.Attachments[0]
	if a.Filename != "f.pdf" || a.ContentType != "application/pdf" || a.ContentID != "cid1" || !a.Inline || a.Size != 3 || string(a.Data) != "abc" {
		t.Errorf("attachment not copied: %+v", a)
	}
	if got.Attachments[1].Inline || got.Attachments[1].ContentID != "" {
		t.Errorf("second attachment should be a plain download: %+v", got.Attachments[1])
	}
	if empty := projectMessage(db.Message{}); empty.Attachments == nil {
		t.Error("a message without attachments must project an empty, non-nil slice")
	}
}

func TestToSMTPCarriesEveryOutgoingField(t *testing.T) {
	out := toSMTP(OutgoingMessage{
		From: "f@x", To: []string{"t@x"}, CC: []string{"c@x"}, BCC: []string{"b@x"},
		Subject: "s", Body: "b", InReplyTo: "<r@x>", References: "<r@x> <q@x>",
		Attachments: []OutgoingAttachment{{Name: "n.txt", Data: []byte("d")}},
	})
	if out.From != "f@x" || out.Subject != "s" || out.Body != "b" || out.InReplyTo != "<r@x>" || out.References != "<r@x> <q@x>" ||
		len(out.To) != 1 || len(out.CC) != 1 || len(out.BCC) != 1 {
		t.Fatalf("fields not carried: %+v", out)
	}
	if len(out.Attachments) != 1 || out.Attachments[0].Name != "n.txt" || string(out.Attachments[0].Data) != "d" {
		t.Errorf("attachment not carried: %+v", out.Attachments)
	}
}

func TestCheckAIOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"models":[{"name":"llama3.2"}]}`)) //nolint:errcheck
	}))
	defer srv.Close()
	ctx := context.Background()

	if err := CheckAI(ctx, AIConfig{Provider: "ollama", URL: srv.URL, Model: "llama3.2"}); err != nil {
		t.Errorf("a reachable server with the model should pass: %v", err)
	}
	if err := CheckAI(ctx, AIConfig{Provider: "ollama", URL: srv.URL, Model: "missing-model"}); err == nil {
		t.Error("a model the server lacks should fail")
	}
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	if err := CheckAI(ctx, AIConfig{Provider: "ollama", URL: down.URL}); err == nil {
		t.Error("an unreachable server should fail")
	}
	if err := CheckAI(ctx, AIConfig{}); err == nil {
		t.Error("no provider should fail")
	}
	if err := CheckAI(ctx, AIConfig{Provider: "openai"}); err == nil {
		t.Error("an empty API key should fail without any network call")
	}
}

func TestGoogleSignInURLCancelAndSignals(t *testing.T) {
	s, err := StartGoogleSignIn(context.Background(), "g-client-id", "g-secret")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(s.AuthURL())
	if err != nil || u.Query().Get("client_id") != "g-client-id" || !strings.HasPrefix(u.Query().Get("redirect_uri"), "http://") {
		t.Fatalf("unexpected sign-in URL %q (%v)", s.AuthURL(), err)
	}
	select {
	case <-s.Done():
		t.Fatal("Done must stay open while the sign-in is pending")
	case <-s.Received():
		t.Fatal("Received must stay open until the browser returns")
	default:
	}
	s.Cancel()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done should close once the sign-in is cancelled")
	}
	if tok, err := s.Wait(); err == nil || tok != "" {
		t.Fatalf("a cancelled sign-in must fail, got %q, %v", tok, err)
	}
	s.Cancel() // cancelling twice must be safe
}

func TestMicrosoftSignInDoneClosesOnCancel(t *testing.T) {
	s, err := StartMicrosoftSignIn(context.Background(), "ms-client-id")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
		t.Fatal("Done must stay open while the sign-in is pending")
	default:
	}
	s.Cancel()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done should close once the sign-in is cancelled")
	}
}

func TestProjectMessageCarriesRawHeaders(t *testing.T) {
	got := projectMessage(db.Message{RawHeaders: "Received: from a by b\r\nSubject: hi\r\n"})
	if got.Headers != "Received: from a by b\r\nSubject: hi\r\n" {
		t.Fatalf("Headers = %q", got.Headers)
	}
}
