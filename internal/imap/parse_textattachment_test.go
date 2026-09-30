package imap

import "testing"

// A text/plain part with Content-Disposition: attachment is a file, not the
// message body.
func TestParseBodyTextAttachment(t *testing.T) {
	raw := []byte("From: a@b.c\r\nTo: d@e.f\r\nSubject: x\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=BB\r\n\r\n" +
		"--BB\r\nContent-Type: text/plain\r\n\r\nhi\r\n" +
		"--BB\r\nContent-Type: text/plain; name=\"hello.txt\"\r\n" +
		"Content-Disposition: attachment; filename=\"hello.txt\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\naGVsbG8gYXR0YWNobWVudAo=\r\n--BB--\r\n")
	text, _, atts := parseBody(raw)
	if text != "hi" {
		t.Fatalf("text = %q, want %q", text, "hi")
	}
	if len(atts) != 1 || atts[0].Filename != "hello.txt" || string(atts[0].Data) != "hello attachment\n" {
		t.Fatalf("attachments = %+v", atts)
	}
}
