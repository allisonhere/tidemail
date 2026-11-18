package imap

import (
	"strings"
	"testing"
)

func TestAttachmentFilenamesDecodeUnicode(t *testing.T) {
	for name, tc := range map[string]struct{ header, want string }{
		"rfc2231":      {`Content-Disposition: attachment; filename*=UTF-8''%E6%8A%A5%E5%91%8A.pdf`, "报告.pdf"},
		"encoded-word": {`Content-Disposition: attachment; filename="=?UTF-8?B?5oql5ZGKLnBkZg==?="`, "报告.pdf"},
		"gb2312":       {`Content-Disposition: attachment; filename="=?GB2312?B?sqnQxy5wZGY=?="`, "博星.pdf"},
		"ct-name":      {`Content-Type: application/pdf; name="=?UTF-8?B?5oql5ZGKLnBkZg==?="`, "报告.pdf"},
	} {
		raw := "From: a@example.com\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n" +
			"--b\r\nContent-Type: text/plain\r\n\r\nhi\r\n--b\r\n"
		if !strings.HasPrefix(tc.header, "Content-Type") {
			raw += "Content-Type: application/pdf\r\n"
		}
		raw += tc.header + "\r\n\r\ndata\r\n--b--\r\n"
		_, _, atts := parseBody([]byte(raw))
		if len(atts) != 1 {
			t.Fatalf("%s: got %d attachments", name, len(atts))
		}
		if got := atts[0].Filename; got != tc.want {
			t.Errorf("%s: filename = %q, want %q", name, got, tc.want)
		}
	}
}
