package imap

import (
	"strings"
	"testing"
)

// A text/plain part declared as ISO-8859-1 must decode to UTF-8 rather than
// leaving raw high bytes that render as U+FFFD. 0xE9 is é in ISO-8859-1.
func TestParseBody_ISO8859Charset(t *testing.T) {
	raw := []byte("Content-Type: text/plain; charset=ISO-8859-1\r\n\r\ncaf\xe9")

	text, _, _ := parseBody(raw)

	if want := "café"; text != want {
		t.Fatalf("parseBody did not decode ISO-8859-1: got %q, want %q", text, want)
	}
	if strings.ContainsRune(text, '�') {
		t.Fatalf("decoded body still contains the replacement char: %q", text)
	}
}

// A part with a genuinely unknown charset must not abort the whole message: its
// body is still read (as raw bytes) and later parts survive.
func TestParseBody_UnknownCharsetKeepsGoing(t *testing.T) {
	raw := []byte("Content-Type: multipart/mixed; boundary=b\r\n\r\n" +
		"--b\r\n" +
		"Content-Type: text/plain; charset=x-unknown-charset-xyz\r\n\r\n" +
		"first part\r\n" +
		"--b\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=doc.pdf\r\n\r\n" +
		"PDFDATA\r\n" +
		"--b--\r\n")

	_, _, atts := parseBody(raw)

	if len(atts) != 1 {
		t.Fatalf("expected the attachment after an unknown-charset part to survive, got %d attachments", len(atts))
	}
}
