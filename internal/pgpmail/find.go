// Package pgpmail finds the OpenPGP-protected parts of a received message and
// turns a plugin's decrypt/verify answers into something to display. It does
// the MIME work so a plugin only has to perform the cryptography: TideMail
// hands over the exact bytes and never trusts the plugin's idea of structure.
package pgpmail

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/textproto"
	"regexp"
	"strings"
)

// Kind says how a message is protected.
type Kind int

const (
	// KindEncrypted is PGP/MIME (RFC 3156) multipart/encrypted. Ciphertext
	// decrypts to a MIME entity.
	KindEncrypted Kind = iota + 1
	// KindSigned is PGP/MIME multipart/signed: Entity is signed by Signature.
	KindSigned
	// KindInline is an ASCII-armored message in the body text. Ciphertext holds
	// the armor, which is either an encrypted message or a clear-signed one.
	KindInline
)

// Protected is the protected part of a message, ready to send to a plugin.
type Protected struct {
	Kind       Kind
	Ciphertext []byte // KindEncrypted, KindInline: the armored text
	Entity     []byte // KindSigned: the signed entity, byte for byte, CRLF endings
	Signature  []byte // KindSigned: the armored detached signature
}

// ClearSigned reports whether an inline Protected is a clear-signed message
// (readable without decrypting) rather than an encrypted one.
func (p Protected) ClearSigned() bool {
	return p.Kind == KindInline && bytes.HasPrefix(p.Ciphertext, []byte("-----BEGIN PGP SIGNED MESSAGE-----"))
}

var inlineArmor = regexp.MustCompile(`(?s)-----BEGIN PGP (?:SIGNED )?MESSAGE-----.*?-----END PGP (?:MESSAGE|SIGNATURE)-----`)

// Find returns the protected part of a raw RFC 822 message or entity, or nil
// when it has none. Only the top level is inspected, which is where PGP/MIME
// puts it; an inline armored block is looked for in the text of a
// non-multipart message.
func Find(raw []byte) *Protected {
	hdr, body := splitEntity(raw)
	ct, params, err := mime.ParseMediaType(hdr.Get("Content-Type"))
	if err != nil {
		ct = "text/plain"
	}
	switch {
	case ct == "multipart/encrypted" && strings.EqualFold(params["protocol"], "application/pgp-encrypted"):
		parts := splitMultipart(body, params["boundary"])
		if len(parts) != 2 {
			return nil
		}
		ph, pb := splitEntity(parts[1])
		return &Protected{Kind: KindEncrypted, Ciphertext: decodeTransfer(ph, pb)}
	case ct == "multipart/signed" && strings.EqualFold(params["protocol"], "application/pgp-signature"):
		parts := splitMultipart(body, params["boundary"])
		if len(parts) != 2 {
			return nil
		}
		sh, sb := splitEntity(parts[1])
		return &Protected{Kind: KindSigned, Entity: canonicalCRLF(parts[0]), Signature: decodeTransfer(sh, sb)}
	case strings.HasPrefix(ct, "multipart/"):
		return nil
	}
	text := decodeTransfer(hdr, body)
	if loc := inlineArmor.Find(text); loc != nil {
		return &Protected{Kind: KindInline, Ciphertext: loc}
	}
	return nil
}

// splitEntity separates an entity's header block from its body.
func splitEntity(raw []byte) (textproto.MIMEHeader, []byte) {
	r := bufio.NewReader(bytes.NewReader(raw))
	hdr, _ := textproto.NewReader(r).ReadMIMEHeader()
	body, _ := io.ReadAll(r)
	if hdr == nil {
		hdr = textproto.MIMEHeader{}
	}
	return hdr, body
}

// decodeTransfer undoes the Content-Transfer-Encoding of a part body.
func decodeTransfer(hdr textproto.MIMEHeader, body []byte) []byte {
	switch strings.ToLower(strings.TrimSpace(hdr.Get("Content-Transfer-Encoding"))) {
	case "quoted-printable":
		if out, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body))); err == nil {
			return out
		}
	case "base64":
		clean := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, body)
		if out, err := base64.StdEncoding.DecodeString(string(clean)); err == nil {
			return out
		}
	}
	return body
}

// splitMultipart returns the raw parts (headers and body) between a
// multipart body's boundary lines. The line break before each delimiter
// belongs to the delimiter, as RFC 2046 says, so a signed part is exactly the
// bytes the sender signed.
func splitMultipart(body []byte, boundary string) [][]byte {
	if boundary == "" {
		return nil
	}
	delim := []byte("--" + boundary)
	var parts [][]byte
	cur := -1
	for start := 0; start <= len(body); {
		next, lineEnd := len(body), len(body)
		if i := bytes.IndexByte(body[start:], '\n'); i >= 0 {
			lineEnd, next = start+i, start+i+1
		}
		line := bytes.TrimRight(body[start:lineEnd], " \t\r")
		if bytes.HasPrefix(line, delim) {
			rest := line[len(delim):]
			closing := bytes.Equal(rest, []byte("--"))
			if closing || len(rest) == 0 {
				if cur >= 0 {
					end := start
					if end > cur && body[end-1] == '\n' {
						end--
						if end > cur && body[end-1] == '\r' {
							end--
						}
					}
					parts = append(parts, body[cur:end])
				}
				if closing {
					return parts
				}
				cur = next
			}
		}
		if next >= len(body) {
			break
		}
		start = next
	}
	return parts
}

// canonicalCRLF turns bare line feeds into CRLF, the form signatures cover.
// Bytes that already have CRLF are left alone.
func canonicalCRLF(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
}
