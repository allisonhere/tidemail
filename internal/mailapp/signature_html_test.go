package mailapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

func writeSig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sig.html")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHTMLSignatureFileReplacesHTMLPartOnly(t *testing.T) {
	const sigHTML = `<p><b>Alice</b> <a href="https://example.com">site</a></p>`
	a := config.AccountConfig{User: "sender@example.com", Signature: "Alice\nsite", SignatureHTMLFile: writeSig(t, sigHTML+"\n")}

	for name, prepare := range map[string]func() (string, string, error){
		"marker": func() (string, string, error) {
			m, err := PrepareCompose(a, db.Draft{To: "r@example.com", BodyText: "Hi\n\n⁂\n"})
			return m.Body, m.HTMLBody, err
		},
		"explicit": func() (string, string, error) {
			m, err := Prepare(a, db.Draft{To: "r@example.com", BodyText: "Hi"}, true)
			return m.Body, m.HTMLBody, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			body, html, err := prepare()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(html, sigHTML) {
				t.Fatalf("HTML part lacks the signature file: %q", html)
			}
			if strings.Contains(html, "Alice<br") || strings.Contains(html, "&lt;") {
				t.Fatalf("text signature also rendered into HTML: %q", html)
			}
			if !strings.Contains(body, "-- \nAlice\nsite") || strings.Contains(body, "<b>") {
				t.Fatalf("plain part should carry only the text signature: %q", body)
			}
		})
	}
}

func TestHTMLSignatureWithoutTextSignature(t *testing.T) {
	a := config.AccountConfig{User: "sender@example.com", SignatureHTMLFile: writeSig(t, "<i>Bye</i>")}
	m, err := PrepareCompose(a, db.Draft{To: "r@example.com", BodyText: "Hi\n\n⁂\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.HTMLBody, "<i>Bye</i>") || strings.Contains(m.Body, "-- ") {
		t.Fatalf("html=%q body=%q", m.HTMLBody, m.Body)
	}
}

func TestHTMLSignatureFileProblemsRefuseTheSend(t *testing.T) {
	big := writeSig(t, strings.Repeat("x", MaxSignatureHTMLBytes+1))
	for name, path := range map[string]string{
		"missing": filepath.Join(t.TempDir(), "gone.html"),
		"dir":     t.TempDir(),
		"large":   big,
	} {
		t.Run(name, func(t *testing.T) {
			a := config.AccountConfig{User: "sender@example.com", Signature: "x", SignatureHTMLFile: path}
			if _, err := PrepareCompose(a, db.Draft{To: "r@example.com", BodyText: "Hi\n\n⁂\n"}); err == nil {
				t.Fatal("expected an error, not an unsigned message")
			}
			if CheckSignatureHTMLFile(path) == nil {
				t.Fatal("CheckSignatureHTMLFile accepted it")
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	if got := ExpandHome("~/s.html"); got != "/home/x/s.html" {
		t.Fatalf("got %q", got)
	}
	if got := ExpandHome("/abs/s.html"); got != "/abs/s.html" {
		t.Fatalf("got %q", got)
	}
}

func writeNamed(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHTMLSignatureFileValidation(t *testing.T) {
	for _, tc := range []struct {
		name, file, content string
		want                string // substring of the reason; "" means accepted
	}{
		{"fragment", "s.html", `<p><b>Alice</b></p>`, ""},
		{"htm ext, upper", "S.HTM", `<p>Alice</p>`, ""},
		{"wrong extension", "s.txt", `<p>Alice</p>`, "not an HTML file"},
		{"no extension", "sig", `<p>Alice</p>`, "not an HTML file"},
		{"private key", "id_rsa.html", "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----", "no HTML tags"},
		{"plain text", "s.html", "Alice\nSent from TideMail", "no HTML tags"},
		{"binary", "s.html", "<p>\x00\x01</p>", "binary"},
		{"invalid utf8", "s.html", "<p>\xff\xfe</p>", "binary"},
		{"empty", "s.html", "  \n", "empty"},
		{"script", "s.html", `<p>hi</p><script>alert(1)</script>`, "<script>"},
		{"iframe", "s.html", `<IFRAME src="https://x"></IFRAME>`, "<iframe>"},
		{"object", "s.html", `<object data="x"></object>`, "<object>"},
		{"embed", "s.html", `<embed src="x">`, "<embed>"},
		{"form", "s.html", `<form action="https://x"><input></form>`, "<form>"},
		{"event attribute", "s.html", `<p onClick="x()">hi</p>`, "event attribute"},
		{"obfuscated javascript link", "s.html", `<a href=" JaVa&#x09;script:alert(1)">x</a>`, "script link"},
		{"javascript link plain", "s.html", `<a href="javascript:alert(1)">x</a>`, "script link"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckSignatureHTMLFile(writeNamed(t, tc.file, tc.content))
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("expected it to be accepted, got %v", err)
			case tc.want != "" && err == nil:
				t.Fatal("expected it to be refused")
			case tc.want != "" && !strings.Contains(SignatureHTMLReason(err), tc.want):
				t.Fatalf("reason %q lacks %q", SignatureHTMLReason(err), tc.want)
			}
		})
	}
}

func TestHTMLSignatureWholeDocumentUsesBodyOnly(t *testing.T) {
	path := writeNamed(t, "s.html", `<!DOCTYPE html><html><head><title>T</title><style>p{color:red}</style></head>
<body><p><b>Alice</b></p></body></html>`)
	a := config.AccountConfig{User: "sender@example.com", Signature: "Alice", SignatureHTMLFile: path}
	m, err := PrepareCompose(a, db.Draft{To: "r@example.com", BodyText: "Hi\n\n⁂\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.HTMLBody, "<p><b>Alice</b></p>") {
		t.Fatalf("body contents missing: %q", m.HTMLBody)
	}
	for _, bad := range []string{"<html", "<head", "<title", "DOCTYPE"} {
		if strings.Contains(m.HTMLBody, bad) {
			t.Fatalf("document wrapper %q leaked into the message: %q", bad, m.HTMLBody)
		}
	}
}

// The file is vetted again at send time, so one swapped for something else
// after it was chosen is still refused.
func TestHTMLSignatureRevalidatedAtSend(t *testing.T) {
	path := writeNamed(t, "s.html", `<p>ok</p>`)
	a := config.AccountConfig{User: "sender@example.com", Signature: "x", SignatureHTMLFile: path}
	if _, err := PrepareCompose(a, db.Draft{To: "r@example.com", BodyText: "Hi\n\n⁂\n"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`<p>ok</p><script>x()</script>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareCompose(a, db.Draft{To: "r@example.com", BodyText: "Hi\n\n⁂\n"}); err == nil {
		t.Fatal("a file that no longer passes the checks was sent")
	}
}
