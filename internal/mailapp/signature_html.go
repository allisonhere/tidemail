package mailapp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/allisonhere/tidemail/internal/config"
	"golang.org/x/net/html"
)

// MaxSignatureHTMLBytes bounds a signature file; a signature is not a page.
const MaxSignatureHTMLBytes = 64 * 1024

// SignatureHTMLError says why a file cannot be the HTML signature. Reason is
// short and carries no path, so a form can show it as is.
type SignatureHTMLError struct {
	Path   string
	Reason string
}

func (e *SignatureHTMLError) Error() string {
	return fmt.Sprintf("HTML signature file %q: %s", e.Path, e.Reason)
}

// SignatureHTMLReason is the short, path-free reason for err.
func SignatureHTMLReason(err error) string {
	var se *SignatureHTMLError
	if errors.As(err, &se) {
		return se.Reason
	}
	return err.Error()
}

// ExpandHome turns a leading "~" or "~/" into the user's home directory.
func ExpandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// CheckSignatureHTMLFile reports why path cannot be used as an HTML signature.
func CheckSignatureHTMLFile(path string) error {
	_, err := readSignatureHTML(path)
	return err
}

// bannedSignatureTags are elements that run code or embed other documents. A
// signature is formatted text, and mail clients that honour these are the ones
// where they do harm.
var bannedSignatureTags = map[string]bool{"script": true, "iframe": true, "object": true, "embed": true, "form": true}

// readSignatureHTML loads and vets the signature file. Anything that is not
// plainly an HTML signature is refused, because the contents go out in every
// message: a wrong pick such as a key or config file must never be sent.
func readSignatureHTML(path string) (string, error) {
	path = ExpandHome(strings.TrimSpace(path))
	refuse := func(reason string) (string, error) {
		return "", &SignatureHTMLError{Path: path, Reason: reason}
	}
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".html" && ext != ".htm" {
		return refuse("not an HTML file (use .html or .htm)")
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return refuse("file not found")
	case err != nil:
		return refuse(err.Error())
	case !info.Mode().IsRegular():
		return refuse("not a file")
	case info.Size() > MaxSignatureHTMLBytes:
		return refuse(fmt.Sprintf("too large (%d KB max)", MaxSignatureHTMLBytes/1024))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return refuse(err.Error())
	}
	src := strings.TrimSpace(string(data))
	if src == "" {
		return refuse("file is empty")
	}
	if !utf8.ValidString(src) || strings.ContainsAny(src, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x0b\x0c\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f") {
		return refuse("not an HTML file (binary content)")
	}
	hasTag, hasBody, problem := scanSignatureHTML(src)
	switch {
	case problem != "":
		return refuse(problem)
	case !hasTag:
		return refuse("not an HTML file (no HTML tags)")
	case hasBody:
		body, err := signatureBody(src)
		if err != nil {
			return refuse("cannot read HTML: " + err.Error())
		}
		if body == "" {
			return refuse("HTML body is empty")
		}
		return body, nil
	}
	return src, nil
}

// scanSignatureHTML walks the tags once: whether there are any, whether the
// source is a whole document, and the first reason to refuse it.
func scanSignatureHTML(src string) (hasTag, hasBody bool, problem string) {
	z := html.NewTokenizer(strings.NewReader(src))
	for {
		switch z.Next() {
		case html.ErrorToken:
			if err := z.Err(); err != nil && !errors.Is(err, io.EOF) {
				return hasTag, hasBody, "cannot read HTML: " + err.Error()
			}
			return hasTag, hasBody, ""
		case html.StartTagToken, html.SelfClosingTagToken:
			name, more := z.TagName()
			tag := string(name)
			hasTag = true
			if tag == "body" {
				hasBody = true
			}
			if bannedSignatureTags[tag] {
				return hasTag, hasBody, "contains <" + tag + "> (not allowed in a signature)"
			}
			for more {
				var key, val []byte
				key, val, more = z.TagAttr()
				k := strings.ToLower(string(key))
				if len(k) > 2 && strings.HasPrefix(k, "on") {
					return hasTag, hasBody, "contains an event attribute (" + k + ")"
				}
				switch k {
				case "href", "src", "action", "formaction", "xlink:href":
					if activeURL(string(val)) {
						return hasTag, hasBody, "contains a script link"
					}
				}
			}
		}
	}
}

// activeURL reports a javascript:/vbscript: URL, ignoring the whitespace and
// control characters browsers skip when reading a scheme.
func activeURL(v string) bool {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		if r > ' ' && r != 0x7f {
			b.WriteRune(r)
		}
	}
	s := b.String()
	return strings.HasPrefix(s, "javascript:") || strings.HasPrefix(s, "vbscript:") || strings.HasPrefix(s, "data:text/html")
}

// signatureBody returns the markup inside <body> of a whole HTML document.
func signatureBody(src string) (string, error) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", err
	}
	var find func(*html.Node) *html.Node
	find = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode && n.Data == "body" {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if found := find(c); found != nil {
				return found
			}
		}
		return nil
	}
	body := find(doc)
	if body == nil {
		return "", errors.New("no body")
	}
	var out bytes.Buffer
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&out, c); err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(out.String()), nil
}

// signatureHTMLFor returns the account's HTML signature file contents, or ""
// when none is configured. It is read at send time so edits apply at once, and
// a file that has gone missing or no longer passes the checks is an error,
// never a silently unsigned message.
func signatureHTMLFor(account config.AccountConfig) (string, error) {
	if strings.TrimSpace(account.SignatureHTMLFile) == "" {
		return "", nil
	}
	return readSignatureHTML(account.SignatureHTMLFile)
}
