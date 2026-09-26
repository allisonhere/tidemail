package imap

import (
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

func TestParseBody_MultipartRelatedCIDImage(t *testing.T) {
	raw := []byte("Content-Type: multipart/related; boundary=rel\r\n\r\n" +
		"--rel\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		`<img src="cid:hero-image-123" alt="Hero">` + "\r\n" +
		"--rel\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-ID: <hero-image-123>\r\n" +
		"Content-Disposition: inline\r\n" +
		"Content-Transfer-Encoding: binary\r\n\r\n" +
		"\x89PNGdata\r\n" +
		"--rel--")

	_, html, atts := parseBody(raw)
	if html != `<img src="cid:hero-image-123" alt="Hero">` {
		t.Fatalf("html = %q", html)
	}
	if len(atts) != 1 {
		t.Fatalf("expected 1 image part, got %d", len(atts))
	}
	a := atts[0]
	if a.ContentID != "hero-image-123" {
		t.Errorf("ContentID = %q, want %q", a.ContentID, "hero-image-123")
	}
	if !a.Inline {
		t.Error("expected inline image")
	}
	if a.ContentType != "image/png" {
		t.Errorf("ContentType = %q", a.ContentType)
	}
	if a.Disposition != "inline" {
		t.Errorf("Disposition = %q", a.Disposition)
	}
	if string(a.Data) != "\x89PNGdata" {
		t.Errorf("data = %q", string(a.Data))
	}
}

func TestParseBody_AlternativeInsideRelated(t *testing.T) {
	raw := []byte("Content-Type: multipart/related; boundary=outer\r\n\r\n" +
		"--outer\r\n" +
		"Content-Type: multipart/alternative; boundary=inner\r\n\r\n" +
		"--inner\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"plain\r\n" +
		"--inner\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		`<img src="cid:logo">` + "\r\n" +
		"--inner--\r\n" +
		"--outer\r\n" +
		"Content-Type: image/gif\r\n" +
		"Content-ID: <logo>\r\n\r\n" +
		"GIF89a\r\n" +
		"--outer--")

	text, html, atts := parseBody(raw)
	if text != "plain" {
		t.Errorf("text = %q", text)
	}
	if html != `<img src="cid:logo">` {
		t.Errorf("html = %q", html)
	}
	if len(atts) != 1 || atts[0].ContentID != "logo" {
		t.Fatalf("atts = %+v", atts)
	}
}

func TestParseBody_NormalImageAttachment(t *testing.T) {
	raw := []byte("Content-Type: multipart/mixed; boundary=mix\r\n\r\n" +
		"--mix\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		"<p>see attached</p>\r\n" +
		"--mix\r\n" +
		"Content-Type: image/jpeg\r\n" +
		"Content-Disposition: attachment; filename=\"photo.jpg\"\r\n\r\n" +
		"JPEGDATA\r\n" +
		"--mix--")

	_, _, atts := parseBody(raw)
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	a := atts[0]
	if a.Filename != "photo.jpg" {
		t.Errorf("Filename = %q", a.Filename)
	}
	if a.Inline {
		t.Error("a Content-Disposition: attachment must not be inline")
	}
	if a.ContentID != "" {
		t.Errorf("ContentID = %q, want empty", a.ContentID)
	}
}

func TestParseBody_InlineImageWithFilename(t *testing.T) {
	raw := []byte("Content-Type: multipart/related; boundary=r\r\n\r\n" +
		"--r\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-ID: <img1>\r\n" +
		"Content-Disposition: inline; filename=\"banner.png\"\r\n" +
		"Content-Location: banner.png\r\n\r\n" +
		"PNG\r\n" +
		"--r--")

	_, _, atts := parseBody(raw)
	if len(atts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(atts))
	}
	a := atts[0]
	if a.Filename != "banner.png" {
		t.Errorf("Filename = %q", a.Filename)
	}
	if a.ContentLocation != "banner.png" {
		t.Errorf("ContentLocation = %q", a.ContentLocation)
	}
	if !a.Inline {
		t.Error("expected inline")
	}
}

func TestNormalizeCID(t *testing.T) {
	cases := map[string]string{
		"<hero-image-123>": "hero-image-123",
		"hero-image-123":   "hero-image-123",
		"  <Foo@Bar>  ":    "Foo@Bar",
		"<a> <b>":          "a",
		"":                 "",
		"<>":               "",
	}
	for in, want := range cases {
		if got := normalizeCID(in); got != want {
			t.Errorf("normalizeCID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseBody_MalformedContentID(t *testing.T) {
	// Missing Content-ID on an inline part and an empty Content-ID must both
	// parse without panicking and without inventing an id.
	raw := []byte("Content-Type: multipart/related; boundary=r\r\n\r\n" +
		"--r\r\n" +
		"Content-Type: image/png\r\n\r\n" +
		"PNG1\r\n" +
		"--r\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-ID: <>\r\n\r\n" +
		"PNG2\r\n" +
		"--r--")

	_, _, atts := parseBody(raw)
	if len(atts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(atts))
	}
	for _, a := range atts {
		if a.ContentID != "" {
			t.Errorf("expected empty ContentID, got %q", a.ContentID)
		}
	}
}

func TestParseBody_DuplicateCID(t *testing.T) {
	raw := []byte("Content-Type: multipart/related; boundary=r\r\n\r\n" +
		"--r\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-ID: <dup>\r\n\r\n" +
		"FIRST\r\n" +
		"--r\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-ID: <dup>\r\n\r\n" +
		"SECOND\r\n" +
		"--r--")

	_, _, atts := parseBody(raw)
	if len(atts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(atts))
	}
	if atts[0].ContentID != "dup" || atts[1].ContentID != "dup" {
		t.Fatalf("both parts should keep the duplicate id: %+v", atts)
	}
}

func TestParseBody_NonUTF8Unaffected(t *testing.T) {
	raw := []byte("Content-Type: multipart/alternative; boundary=x\r\n\r\n" +
		"--x\r\n" +
		"Content-Type: text/plain; charset=iso-8859-1\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" +
		"caf\xe9\r\n" +
		"--x--")
	text, _, atts := parseBody(raw)
	if text != "café" {
		t.Errorf("text = %q, want %q", text, "café")
	}
	if len(atts) != 0 {
		t.Errorf("expected no attachments, got %d", len(atts))
	}
}
