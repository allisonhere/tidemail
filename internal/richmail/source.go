// Package richmail turns untrusted MIME/HTML email into a small, testable
// representation of its rich content — chiefly images — and lays that content
// out for a terminal reading pane.
//
// The package deliberately separates three concerns:
//
//   - parsing: HTML elements and MIME identity become an Image
//   - resolution: an Image's source becomes decoded pixels (CID, data URI, or a
//     privacy-gated remote fetch)
//   - layout: decoded pixels and terminal cell geometry become a Placement
//
// Nothing here touches the terminal or the UI; that keeps the security-critical
// parts (URL policy, size limits, CID matching) unit-testable in isolation.
package richmail

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// maxDataURIBytes caps the decoded size of an inline data: image. Data URIs are
// part of the message body itself, so an unbounded one is simply a way to make
// opening a message allocate arbitrarily large buffers.
const maxDataURIBytes = 4 << 20 // 4 MiB

// SourceKind classifies where an <img> points.
type SourceKind int

const (
	// SourceUnknown is anything TideMail will not attempt to load.
	SourceUnknown SourceKind = iota
	// SourceCID references a MIME part by Content-ID; resolved locally.
	SourceCID
	// SourceData is an inline data: URI; resolved locally.
	SourceData
	// SourceRemote is an http(s) URL; resolved only with user consent.
	SourceRemote
)

func (k SourceKind) String() string {
	switch k {
	case SourceCID:
		return "cid"
	case SourceData:
		return "data"
	case SourceRemote:
		return "remote"
	default:
		return "unknown"
	}
}

// ImageSource is a parsed, sanitized image location.
type ImageSource struct {
	Kind SourceKind
	// Raw is the original (bounded, control-stripped) src value, retained for
	// diagnostics. It is never used to build terminal escapes.
	Raw string
	// CID is the normalized Content-ID when Kind is SourceCID.
	CID string
	// URL is the http(s) URL when Kind is SourceRemote.
	URL string
	// Data holds the decoded bytes when Kind is SourceData.
	Data []byte
	// MIMEType is the declared media type for data URIs.
	MIMEType string
}

// ErrBlockedURL is returned when a URL is refused by policy rather than merely
// unavailable.
var ErrBlockedURL = errors.New("richmail: url blocked")

// ErrUnsupportedSource is returned for sources TideMail will never load.
var ErrUnsupportedSource = errors.New("richmail: unsupported image source")

// allowedDataMIMETypes are the inline image types TideMail will decode. Anything
// else (notably image/svg+xml, which can carry script) is refused even though it
// is nominally an image.
var allowedDataMIMETypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/jpg":  true,
	"image/gif":  true,
	"image/webp": true,
}

// ParseSource parses an <img src> (or CSS-equivalent) value into an ImageSource.
// It is total: malformed input yields SourceUnknown rather than an error, so one
// bad attribute cannot abort rendering the rest of a message.
func ParseSource(raw string) ImageSource {
	src := strings.TrimSpace(stripControl(raw))
	out := ImageSource{Raw: src}
	if src == "" {
		return out
	}

	lower := strings.ToLower(src)
	switch {
	case strings.HasPrefix(lower, "cid:"):
		cid := normalizeCIDValue(src[len("cid:"):])
		if cid == "" {
			return out
		}
		out.Kind = SourceCID
		out.CID = cid
		return out

	case strings.HasPrefix(lower, "data:"):
		parsed, err := parseDataURI(src)
		if err != nil {
			out.Kind = SourceUnknown
			return out
		}
		out.Kind = SourceData
		out.Data = parsed.data
		out.MIMEType = parsed.mime
		return out

	case strings.HasPrefix(lower, "//"):
		// Protocol-relative: default to https, never inherit a caller scheme.
		if u, err := url.Parse("https:" + src); err == nil {
			out.Kind = SourceRemote
			out.URL = u.String()
			return out
		}
		return out

	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		u, err := url.Parse(src)
		if err != nil || u.Host == "" {
			return out
		}
		u.Scheme = strings.ToLower(u.Scheme)
		out.Kind = SourceRemote
		out.URL = u.String()
		return out
	}

	// file:, ftp:, javascript:, relative paths and anything else are never
	// fetched. Callers may still show the alt text.
	out.Kind = SourceUnknown
	return out
}

// parseDataURI decodes a data: URI into bytes and media type, enforcing type and
// size limits. It accepts base64 and percent-encoded payloads.
func parseDataURI(src string) (struct {
	data []byte
	mime string
}, error) {
	var out struct {
		data []byte
		mime string
	}
	rest := src[len("data:"):]
	meta, payload, ok := strings.Cut(rest, ",")
	if !ok {
		return out, ErrUnsupportedSource
	}

	parts := strings.Split(meta, ";")
	mime := strings.ToLower(strings.TrimSpace(parts[0]))
	if mime == "" {
		mime = "text/plain"
	}
	if !allowedDataMIMETypes[mime] {
		return out, ErrUnsupportedSource
	}

	isBase64 := false
	for _, p := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(p), "base64") {
			isBase64 = true
		}
	}

	// Bound the encoded input before decoding so a hostile body cannot make us
	// allocate for a gigabyte payload that we would then reject.
	if len(payload) > maxDataURIBytes*2 {
		return out, ErrUnsupportedSource
	}

	var data []byte
	var err error
	if isBase64 {
		payload = strings.TrimSpace(payload)
		data, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			data, err = base64.RawStdEncoding.DecodeString(payload)
		}
		if err != nil {
			if d, e := base64.URLEncoding.DecodeString(payload); e == nil {
				data, err = d, nil
			}
		}
	} else {
		var sb strings.Builder
		for i := 0; i < len(payload); i++ {
			if payload[i] == '%' && i+2 < len(payload) {
				if v, e := hexByte(payload[i+1], payload[i+2]); e == nil {
					sb.WriteByte(v)
					i += 2
					continue
				}
			}
			sb.WriteByte(payload[i])
		}
		data = []byte(sb.String())
	}
	if err != nil {
		return out, ErrUnsupportedSource
	}
	if len(data) == 0 {
		return out, ErrUnsupportedSource
	}
	if len(data) > maxDataURIBytes {
		return out, ErrUnsupportedSource
	}
	out.data = data
	out.mime = mime
	return out, nil
}

func hexByte(a, b byte) (byte, error) {
	hi, ok1 := hexNibble(a)
	lo, ok2 := hexNibble(b)
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("bad hex")
	}
	return hi<<4 | lo, nil
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// normalizeCIDValue strips the RFC 2045 angle brackets and surrounding
// whitespace/controls from a Content-ID or cid: reference. Matching Content-IDs
// is case-insensitive in practice (domains are), so a case-folded form is also
// used by the resolver; this function preserves the original case for display.
func normalizeCIDValue(raw string) string {
	s := strings.TrimSpace(stripControl(raw))
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, " \t\r\n,"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "<>")
	return strings.TrimSpace(s)
}

// NormalizeCID exposes CID normalization to the MIME layer and tests.
func NormalizeCID(raw string) string { return normalizeCIDValue(raw) }

// CIDKey produces the map key used to match a cid: reference against MIME parts.
// Case-folding is deliberate: senders are inconsistent about the case of CIDs,
// and a miss shows the reader a placeholder instead of the image.
func CIDKey(raw string) string {
	return strings.ToLower(normalizeCIDValue(raw))
}

// stripControl removes C0/C1 control bytes (and DEL) from untrusted attribute
// text. Newlines and tabs are collapsed to spaces rather than dropped so an
// attacker cannot smuggle an escape sequence across a "safe" split.
func stripControl(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			continue
		case r >= 0x80 && r <= 0x9f:
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
