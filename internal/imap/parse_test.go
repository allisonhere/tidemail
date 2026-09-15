package imap

import (
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"plain text", "hello world", "hello world"},
		{"simple tags", "<p>hello</p><br/><b>world</b>", "hello\nworld"},
		{"nested tags", "<div><p>nested</p></div>", "nested"},
		{"with attrs", `<a href="http://example.com">link</a>`, "link"},
		{"whitespace trimming", "  <p>  spaced  </p>  ", "spaced"},
		{"multiple lines", "<p>line1</p>\n<p>line2</p>", "line1\nline2"},
		{"self-closing", "<br /><hr /><p>text</p>", "text"},
		{"scripts stripped", "<script>alert('x')</script><p>ok</p>", "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTML(tt.input)
			if got != tt.want {
				t.Errorf("stripHTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAddressList(t *testing.T) {
	tests := []struct {
		name  string
		addrs []imap.Address
		want  string
	}{
		{"nil", nil, ""},
		{"empty", []imap.Address{}, ""},
		{"single with name", []imap.Address{{Name: "Alice", Mailbox: "alice", Host: "example.com"}}, "Alice <alice@example.com>"},
		{"single without name", []imap.Address{{Mailbox: "bob", Host: "test.org"}}, "bob@test.org"},
		{"multiple", []imap.Address{
			{Name: "Alice", Mailbox: "alice", Host: "example.com"},
			{Name: "Bob", Mailbox: "bob", Host: "test.org"},
		}, "Alice <alice@example.com>, Bob <bob@test.org>"},
		{"empty components", []imap.Address{{Name: "", Mailbox: "", Host: ""}}, "@"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addressList(tt.addrs)
			if got != tt.want {
				t.Errorf("addressList(%+v) = %q, want %q", tt.addrs, got, tt.want)
			}
		})
	}
}

func TestParseBody_PlainText(t *testing.T) {
	raw := []byte("Content-Type: text/plain\r\n\r\nHello, World!")
	text, html, atts := parseBody(raw)
	if text != "Hello, World!" {
		t.Errorf("text = %q, want %q", text, "Hello, World!")
	}
	if html != "" {
		t.Errorf("html = %q, want empty", html)
	}
	if len(atts) != 0 {
		t.Error("expected no attachments")
	}
}

func TestParseBody_PlainTextMultipart(t *testing.T) {
	raw := []byte("Content-Type: multipart/alternative; boundary=xyz\r\n\r\n" +
		"--xyz\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"plain body\r\n" +
		"--xyz\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		"<p>html body</p>\r\n" +
		"--xyz--")
	text, html, atts := parseBody(raw)
	if text != "plain body" {
		t.Errorf("text = %q, want %q", text, "plain body")
	}
	if html != "<p>html body</p>" {
		t.Errorf("html = %q, want %q", html, "<p>html body</p>")
	}
	if len(atts) != 0 {
		t.Error("expected no attachments")
	}
}

func TestParseBody_WithAttachment(t *testing.T) {
	raw := []byte("Content-Type: multipart/mixed; boundary=xyz\r\n\r\n" +
		"--xyz\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"body\r\n" +
		"--xyz\r\n" +
		"Content-Type: application/pdf\r\n\r\n" +
		"%PDF-1.4...\r\n" +
		"--xyz--")
	_, _, atts := parseBody(raw)
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	if atts[0].ContentType != "application/pdf" {
		t.Errorf("content type = %q, want %q", atts[0].ContentType, "application/pdf")
	}
	if string(atts[0].Data) != "%PDF-1.4..." {
		t.Errorf("data = %q, want %q", string(atts[0].Data), "%PDF-1.4...")
	}
}

func TestParseBody_InvalidMIME(t *testing.T) {
	// Non-MIME data: should fall back to raw string as plain text
	raw := []byte("just raw text")
	text, html, atts := parseBody(raw)
	if text != "just raw text" {
		t.Errorf("text = %q, want %q", text, "just raw text")
	}
	if html != "" {
		t.Errorf("html = %q, want empty", html)
	}
	if len(atts) != 0 {
		t.Error("expected no attachments")
	}
}

func TestParseBody_HTMLOnlyMultipart(t *testing.T) {
	raw := []byte("Content-Type: multipart/alternative; boundary=xyz\r\n\r\n" +
		"--xyz\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		"<p>hello</p>\r\n" +
		"--xyz--")
	text, html, atts := parseBody(raw)
	if text != "hello" {
		// text should be stripHTML of the HTML part
		t.Errorf("text = %q, want %q", text, "hello")
	}
	if html != "<p>hello</p>" {
		t.Errorf("html = %q, want %q", html, "<p>hello</p>")
	}
	if len(atts) != 0 {
		t.Error("expected no attachments")
	}
}

func TestParseBody_HTMLOnlyBoilerplateDoesNotBecomePlainText(t *testing.T) {
	raw := []byte("Content-Type: multipart/alternative; boundary=xyz\r\n\r\n" +
		"--xyz\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		`<p><a href="https://example.com/browser">View in browser</a></p>` + "\r\n" +
		"--xyz--")
	text, html, atts := parseBody(raw)
	if text != "" {
		t.Errorf("text = %q, want empty boilerplate fallback", text)
	}
	if html == "" {
		t.Error("expected HTML to remain stored")
	}
	if len(atts) != 0 {
		t.Error("expected no attachments")
	}
}

func TestParseBody_PlainTextBeatsBoilerplateHTML(t *testing.T) {
	raw := []byte("Content-Type: multipart/alternative; boundary=xyz\r\n\r\n" +
		"--xyz\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"plain body with useful details\r\n" +
		"--xyz\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		`<p><a href="https://example.com/browser">View in browser</a></p>` + "\r\n" +
		"--xyz--")
	text, html, atts := parseBody(raw)
	if text != "plain body with useful details" {
		t.Errorf("text = %q, want useful plain text", text)
	}
	if html == "" {
		t.Error("expected HTML to remain stored")
	}
	if len(atts) != 0 {
		t.Error("expected no attachments")
	}
}

func TestParseIMAPMessage_Basic(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	msg := &imapclient.FetchMessageBuffer{
		UID: 42,
		Flags: []imap.Flag{
			imap.FlagSeen,
			imap.FlagFlagged,
		},
		Envelope: &imap.Envelope{
			Subject:   "Test Subject",
			MessageID: "<abc123@example.com>",
			InReplyTo: []string{"<parent@example.com>"},
			Date:      now,
			From:      []imap.Address{{Name: "Alice", Mailbox: "alice", Host: "example.com"}},
			To:        []imap.Address{{Name: "Bob", Mailbox: "bob", Host: "test.org"}},
			Cc:        []imap.Address{{Name: "Carol", Mailbox: "carol", Host: "other.com"}},
			ReplyTo:   []imap.Address{{Name: "Alice", Mailbox: "alice", Host: "example.com"}},
		},
		BodySection: []imapclient.FetchBodySectionBuffer{
			{Bytes: []byte("Content-Type: text/plain\r\n\r\nHello World")},
			{
				Section: &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader},
				Bytes:   []byte("References: <root@example.com>\r\n <parent@example.com>\r\nAuthentication-Results: mx.example.com; spf=pass\r\n\r\n"),
			},
		},
	}

	m, err := parseIMAPMessage(msg)
	if err != nil {
		t.Fatalf("parseIMAPMessage() error = %v", err)
	}

	if m.UID != 42 {
		t.Errorf("UID = %d, want 42", m.UID)
	}
	if !m.Read {
		t.Error("Read = false, want true")
	}
	if m.Subject != "Test Subject" {
		t.Errorf("Subject = %q, want %q", m.Subject, "Test Subject")
	}
	if m.MessageID != "<abc123@example.com>" {
		t.Errorf("MessageID = %q, want %q", m.MessageID, "<abc123@example.com>")
	}
	if m.InReplyTo != "<parent@example.com>" {
		t.Errorf("InReplyTo = %q, want %q", m.InReplyTo, "<parent@example.com>")
	}
	if m.References != "<root@example.com> <parent@example.com>" {
		t.Errorf("References = %q, want folded references", m.References)
	}
	if !m.Date.Equal(now) {
		t.Errorf("Date = %v, want %v", m.Date, now)
	}
	if m.From != "Alice <alice@example.com>" {
		t.Errorf("From = %q, want %q", m.From, "Alice <alice@example.com>")
	}
	if m.To != "Bob <bob@test.org>" {
		t.Errorf("To = %q, want %q", m.To, "Bob <bob@test.org>")
	}
	if m.CC != "Carol <carol@other.com>" {
		t.Errorf("CC = %q, want %q", m.CC, "Carol <carol@other.com>")
	}
	if m.ReplyTo != "Alice <alice@example.com>" {
		t.Errorf("ReplyTo = %q, want %q", m.ReplyTo, "Alice <alice@example.com>")
	}
	if m.BodyText != "Hello World" {
		t.Errorf("BodyText = %q, want %q", m.BodyText, "Hello World")
	}
}

func TestParseIMAPMessage_NoBody(t *testing.T) {
	msg := &imapclient.FetchMessageBuffer{
		UID:   1,
		Flags: []imap.Flag{},
	}

	m, err := parseIMAPMessage(msg)
	if err != nil {
		t.Fatalf("parseIMAPMessage() error = %v", err)
	}
	if m.UID != 1 {
		t.Errorf("UID = %d, want 1", m.UID)
	}
	if m.Read {
		t.Error("Read = true, want false")
	}
	if m.BodyText != "" {
		t.Errorf("BodyText = %q, want empty", m.BodyText)
	}
}

func TestParseIMAPMessage_NoEnvelope(t *testing.T) {
	msg := &imapclient.FetchMessageBuffer{
		UID:   2,
		Flags: []imap.Flag{imap.FlagSeen},
	}

	m, err := parseIMAPMessage(msg)
	if err != nil {
		t.Fatalf("parseIMAPMessage() error = %v", err)
	}
	if m.UID != 2 {
		t.Errorf("UID = %d, want 2", m.UID)
	}
	if m.Subject != "" {
		t.Errorf("Subject = %q, want empty", m.Subject)
	}
}

func TestParseIMAPMessage_AttachmentFlag(t *testing.T) {
	msg := &imapclient.FetchMessageBuffer{
		UID: 3,
		BodySection: []imapclient.FetchBodySectionBuffer{
			{Bytes: []byte("Content-Type: multipart/mixed; boundary=x\r\n\r\n" +
				"--x\r\nContent-Type: text/plain\r\n\r\nbody\r\n" +
				"--x\r\nContent-Type: application/zip\r\n\r\n...\r\n--x--")},
		},
	}

	m, err := parseIMAPMessage(msg)
	if err != nil {
		t.Fatalf("parseIMAPMessage() error = %v", err)
	}
	if !m.HasAttachment {
		t.Error("HasAttachment = false, want true")
	}
}

func TestSanitizeControl(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello world", "hello world"},
		{"keeps tab/newline", "a\tb\nc", "a\tb\nc"},
		{"strips ESC", "a\x1b[31mb", "a[31mb"},
		{"strips BEL and OSC payload delimiters", "x\x07y", "xy"},
		{"strips OSC52 clipboard hijack", "\x1b]52;c;ZXZpbAo=\x07done", "]52;c;ZXZpbAo=done"},
		{"strips DEL", "a\x7fb", "ab"},
		{"keeps unicode", "café — π", "café — π"},
	}
	for _, tc := range cases {
		if got := sanitizeControl(tc.in); got != tc.want {
			t.Errorf("%s: sanitizeControl(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestParseAuthHeadersKeepsListUnsubscribe(t *testing.T) {
	raw := []byte("Return-Path: <news@list.example.com>\r\n" +
		"List-Unsubscribe: <mailto:leave@list.example.com>, <https://list.example.com/leave>\r\n" +
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n" +
		"Subject: hi\r\n\r\n")
	got := parseAuthHeaders(raw)
	want := "Return-Path\n<news@list.example.com>\n" +
		"List-Unsubscribe\n<mailto:leave@list.example.com>, <https://list.example.com/leave>\n" +
		"List-Unsubscribe-Post\nList-Unsubscribe=One-Click\n"
	if got != want {
		t.Fatalf("parseAuthHeaders = %q, want %q", got, want)
	}
}

// TestParseIMAPMessageBracketsEnvelopeIDs verifies message identifiers are stored
// in RFC 5322 form. go-imap parses envelope IDs through mail.Header.MessageID(),
// which strips the angle brackets, and the bare value flowed into outgoing
// In-Reply-To / References headers that other clients will not thread on.
func TestParseIMAPMessageBracketsEnvelopeIDs(t *testing.T) {
	msg := &imapclient.FetchMessageBuffer{
		UID: 7,
		Envelope: &imap.Envelope{
			Subject:   "Re: Plan",
			MessageID: "reply@example.com",                                // as go-imap delivers it
			InReplyTo: []string{"root@example.com", "middle@example.com"}, // multi-ID
			Date:      time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		},
	}

	m, err := parseIMAPMessage(msg)
	if err != nil {
		t.Fatalf("parseIMAPMessage: %v", err)
	}
	if m.MessageID != "<reply@example.com>" {
		t.Fatalf("expected a bracketed Message-ID, got %q", m.MessageID)
	}
	if want := "<root@example.com> <middle@example.com>"; m.InReplyTo != want {
		t.Fatalf("expected a bracketed msg-id list %q, got %q", want, m.InReplyTo)
	}
}

// TestBracketMessageIDRejectsNonIdentifiers verifies values that cannot be a
// msg-id are dropped rather than wrapped into a malformed header.
func TestBracketMessageIDRejectsNonIdentifiers(t *testing.T) {
	for _, in := range []string{"", "   ", "no-at-sign", "has space@example.com", "a@b\r\nBcc: x@y"} {
		if got := bracketMessageID(in); got != "" {
			t.Fatalf("bracketMessageID(%q) = %q, want empty", in, got)
		}
	}
	if got := bracketMessageID("  <already@example.com>  "); got != "<already@example.com>" {
		t.Fatalf("expected an already-bracketed id preserved, got %q", got)
	}
}

// TestParseBodyKeepsPartsOnUnknownTopLevelCharset verifies an unrecognised
// charset on the top-level header does not cost us the parsed message.
// mail.CreateReader still returns a usable reader alongside that error, but the
// old code bailed and returned the entire raw payload — headers included — as
// the body text, dropping the HTML alternative and every attachment.
func TestParseBodyKeepsPartsOnUnknownTopLevelCharset(t *testing.T) {
	raw := []byte("MIME-Version: 1.0\r\n" +
		"Subject: Hi\r\n" +
		"Content-Type: text/plain; charset=\"x-not-a-real-charset\"\r\n" +
		"\r\n" +
		"the actual body\r\n")

	text, html, attachments := parseBody(raw)

	if strings.Contains(text, "Content-Type:") || strings.Contains(text, "MIME-Version:") {
		t.Fatalf("raw headers leaked into the body text: %q", text)
	}
	if !strings.Contains(text, "the actual body") {
		t.Fatalf("expected the decoded body, got %q", text)
	}
	_, _ = html, attachments
}

// TestParseBodyKeepsHTMLAndAttachmentsOnUnknownCharset covers the multipart case:
// an unknown charset must not cost the HTML alternative or the attachments.
func TestParseBodyKeepsHTMLAndAttachmentsOnUnknownCharset(t *testing.T) {
	raw := []byte("MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"bnd\"\r\n\r\n" +
		"--bnd\r\n" +
		"Content-Type: text/plain; charset=\"x-not-a-real-charset\"\r\n\r\nplain part\r\n" +
		"--bnd\r\n" +
		"Content-Type: text/html\r\n\r\n<p>html part</p>\r\n" +
		"--bnd\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"notes.pdf\"\r\n\r\nattached\r\n" +
		"--bnd--\r\n")

	text, html, attachments := parseBody(raw)

	if !strings.Contains(text, "plain part") {
		t.Fatalf("expected the plain part, got %q", text)
	}
	if !strings.Contains(html, "html part") {
		t.Fatalf("expected the HTML alternative preserved, got %q", html)
	}
	if len(attachments) != 1 || attachments[0].Filename != "notes.pdf" {
		t.Fatalf("expected the attachment preserved, got %+v", attachments)
	}
}

// TestAddressListQuotesAmbiguousDisplayNames verifies stored address lists stay
// splittable. A display name containing a comma, written bare, is
// indistinguishable from a separator once the list is joined.
func TestAddressListQuotesAmbiguousDisplayNames(t *testing.T) {
	got := addressList([]imap.Address{
		{Name: "Doe, John", Mailbox: "john", Host: "x.com"},
		{Name: "Alice", Mailbox: "alice", Host: "x.com"},
		{Mailbox: "bare", Host: "x.com"},
	})
	want := `"Doe, John" <john@x.com>, Alice <alice@x.com>, bare@x.com`
	if got != want {
		t.Fatalf("addressList = %q, want %q", got, want)
	}
}

// TestAddressListKeepsNonASCIINamesDecoded guards the interaction with
// charset-aware header decoding: quoting must not RFC 2047-encode a name, or the
// raw =?utf-8?q?...?= would be stored and shown to the user again.
func TestAddressListKeepsNonASCIINamesDecoded(t *testing.T) {
	got := addressList([]imap.Address{{Name: "Café", Mailbox: "cafe", Host: "x.com"}})
	if want := "Café <cafe@x.com>"; got != want {
		t.Fatalf("addressList = %q, want %q", got, want)
	}
}

// TestParseBodyKeepsTextAttachments verifies a part the sender marked as an
// attachment is collected whatever its content type. Routing on Content-Type
// alone sent an attached .log or .csv into the text/plain branch, where the
// "first part wins" guard dropped it: it was neither shown as the body nor
// listed as an attachment, so the file vanished from the message entirely.
func TestParseBodyKeepsTextAttachments(t *testing.T) {
	raw := []byte("MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"b\"\r\n\r\n" +
		"--b\r\nContent-Type: text/plain\r\n\r\nHere is the log you asked for.\r\n" +
		"--b\r\nContent-Type: text/plain; name=\"server.log\"\r\n" +
		"Content-Disposition: attachment; filename=\"server.log\"\r\n\r\nERROR line one\r\n" +
		"--b\r\nContent-Type: text/html\r\n" +
		"Content-Disposition: attachment; filename=\"report.html\"\r\n\r\n<p>report</p>\r\n" +
		"--b--\r\n")

	text, html, attachments := parseBody(raw)

	if text != "Here is the log you asked for." {
		t.Fatalf("body text = %q, want the inline part only", text)
	}
	if html != "" {
		t.Fatalf("an attached HTML file must not become the message body, got %q", html)
	}
	if len(attachments) != 2 {
		t.Fatalf("expected both attachments, got %d: %+v", len(attachments), attachments)
	}
	for i, want := range []struct{ name, ct string }{
		{"server.log", "text/plain"},
		{"report.html", "text/html"},
	} {
		if attachments[i].Filename != want.name || attachments[i].ContentType != want.ct {
			t.Errorf("attachment %d = %s (%s), want %s (%s)",
				i, attachments[i].Filename, attachments[i].ContentType, want.name, want.ct)
		}
	}
	if got := string(attachments[0].Data); !strings.Contains(got, "ERROR line one") {
		t.Errorf("attachment content lost: %q", got)
	}
}

// TestParseBodyPlainPartsStayBody guards the ordinary case: parts with no
// Content-Disposition, and inline parts, are body content and must not be pulled
// out into the attachment list.
func TestParseBodyPlainPartsStayBody(t *testing.T) {
	raw := []byte("MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"b\"\r\n\r\n" +
		"--b\r\nContent-Type: text/plain\r\n" +
		"Content-Disposition: inline\r\n\r\nplain body\r\n" +
		"--b\r\nContent-Type: text/html\r\n\r\n<p>html body</p>\r\n" +
		"--b--\r\n")

	text, html, attachments := parseBody(raw)

	if text != "plain body" {
		t.Fatalf("body text = %q", text)
	}
	if !strings.Contains(html, "html body") {
		t.Fatalf("html body = %q", html)
	}
	if len(attachments) != 0 {
		t.Fatalf("body parts were treated as attachments: %+v", attachments)
	}
}

// TestParseBodyInlineImageStaysAttachment guards embedded images, which are
// marked inline but still need to reach the attachment list — they get there by
// content type, not disposition.
func TestParseBodyInlineImageStaysAttachment(t *testing.T) {
	raw := []byte("MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/related; boundary=\"b\"\r\n\r\n" +
		"--b\r\nContent-Type: text/html\r\n\r\n<img src=\"cid:logo\">\r\n" +
		"--b\r\nContent-Type: image/png; name=\"logo.png\"\r\n" +
		"Content-Disposition: inline; filename=\"logo.png\"\r\n" +
		"Content-ID: <logo>\r\n\r\nPNGDATA\r\n" +
		"--b--\r\n")

	_, _, attachments := parseBody(raw)

	if len(attachments) != 1 || attachments[0].Filename != "logo.png" {
		t.Fatalf("expected the inline image collected, got %+v", attachments)
	}
}
