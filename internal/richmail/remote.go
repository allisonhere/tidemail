package richmail

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Remote fetch errors.
var (
	ErrRemoteBlocked   = errors.New("richmail: remote image blocked")
	ErrRemoteTooLarge  = errors.New("richmail: remote image too large")
	ErrRemoteBadType   = errors.New("richmail: remote image has unsupported content type")
	ErrRemoteRedirect  = errors.New("richmail: too many redirects")
	ErrRemoteTransport = errors.New("richmail: remote fetch failed")
)

// RemoteOptions bounds a remote image fetch. Every field is deliberately
// conservative: HTML email is untrusted input and opening a message must not
// become a way to probe a local network or download gigabytes.
type RemoteOptions struct {
	Timeout      time.Duration
	MaxBytes     int64
	MaxRedirects int
	UserAgent    string
}

// DefaultRemoteOptions are the bounds used by TideMail.
func DefaultRemoteOptions() RemoteOptions {
	return RemoteOptions{
		Timeout:      8 * time.Second,
		MaxBytes:     8 << 20, // 8 MiB
		MaxRedirects: 4,
		UserAgent:    "TideMail/1.0 (image fetch)",
	}
}

// allowedRemoteTypes are the media types a remote response may declare.
var allowedRemoteTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// Fetch retrieves a remote image with SSRF protection, redirect limits, and a
// hard byte cap. It validates the URL before any connection, validates the
// resolved IP at dial time (so DNS cannot rebind into a private range), and
// validates the response content type before returning bytes.
func (o RemoteOptions) Fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	o = o.withDefaults()
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrRemoteBlocked, err)
	}
	if err := ValidateRemoteURL(u); err != nil {
		return nil, "", err
	}

	client := &http.Client{
		Timeout: o.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= o.MaxRedirects {
				return ErrRemoteRedirect
			}
			return ValidateRemoteURL(req.URL)
		},
		Transport: &http.Transport{
			Proxy:                 nil, // do not inherit HTTP_PROXY: it can point anywhere
			DialContext:           safeDialContext(o.Timeout),
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          2,
			IdleConnTimeout:       10 * time.Second,
			TLSHandshakeTimeout:   o.Timeout,
			ResponseHeaderTimeout: o.Timeout,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrRemoteBlocked, err)
	}
	if o.UserAgent != "" {
		req.Header.Set("User-Agent", o.UserAgent)
	}
	req.Header.Set("Accept", "image/png,image/jpeg,image/gif,image/webp;q=0.9,*/*;q=0.1")

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrRemoteBlocked) || errors.Is(err, ErrRemoteRedirect) {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("%w: %v", ErrRemoteTransport, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("%w: status %d", ErrRemoteTransport, resp.StatusCode)
	}

	ct := normalizeMediaType(resp.Header.Get("Content-Type"))
	if ct == "" || !allowedRemoteTypes[ct] {
		return nil, "", fmt.Errorf("%w: %q", ErrRemoteBadType, ct)
	}

	// Reject obviously oversized bodies from the declared length, then still cap
	// the actual read in case the server lies.
	if resp.ContentLength > o.MaxBytes {
		return nil, "", ErrRemoteTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, o.MaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrRemoteTransport, err)
	}
	if int64(len(data)) > o.MaxBytes {
		return nil, "", ErrRemoteTooLarge
	}
	return data, ct, nil
}

func (o RemoteOptions) withDefaults() RemoteOptions {
	d := DefaultRemoteOptions()
	if o.Timeout <= 0 {
		o.Timeout = d.Timeout
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = d.MaxBytes
	}
	if o.MaxRedirects <= 0 {
		o.MaxRedirects = d.MaxRedirects
	}
	if o.UserAgent == "" {
		o.UserAgent = d.UserAgent
	}
	return o
}

// ValidateRemoteURL rejects any URL that is not a plain http(s) fetch from a
// non-internal host. It is exported so tests and other callers can share the
// exact policy.
func ValidateRemoteURL(u *url.URL) error {
	if u == nil {
		return ErrRemoteBlocked
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: scheme %q", ErrRemoteBlocked, scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: empty host", ErrRemoteBlocked)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrRemoteBlocked)
	}
	// A literal IP can be judged immediately. A hostname is judged when it
	// resolves, inside safeDialContext.
	if ip := net.ParseIP(host); ip != nil {
		if err := CheckPublicIP(ip); err != nil {
			return err
		}
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return fmt.Errorf("%w: host %q", ErrRemoteBlocked, host)
	}
	return nil
}

// CheckPublicIP rejects addresses that could reach the local machine, the local
// network, or cloud metadata endpoints.
func CheckPublicIP(ip net.IP) error {
	if ip == nil {
		return ErrRemoteBlocked
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsPrivate() {
		return fmt.Errorf("%w: address %s", ErrRemoteBlocked, ip)
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127: // 100.64.0.0/10 CGNAT
			return fmt.Errorf("%w: address %s", ErrRemoteBlocked, ip)
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0: // 192.0.0.0/24
			return fmt.Errorf("%w: address %s", ErrRemoteBlocked, ip)
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19): // benchmarking
			return fmt.Errorf("%w: address %s", ErrRemoteBlocked, ip)
		case v4[0] == 0:
			return fmt.Errorf("%w: address %s", ErrRemoteBlocked, ip)
		}
		return nil
	}
	// IPv6: block unique-local fc00::/7 explicitly (IsPrivate covers it in newer
	// Go, but being explicit documents the intent and survives version drift).
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return fmt.Errorf("%w: address %s", ErrRemoteBlocked, ip)
	}
	return nil
}

// safeDialContext dials through a net.Dialer whose Control hook rejects a
// connection once the remote address is actually resolved. Checking at dial
// time — not just at URL-parse time — closes the DNS-rebinding window.
func safeDialContext(timeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 15 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			h, _, err := net.SplitHostPort(address)
			if err != nil {
				return ErrRemoteBlocked
			}
			ip := net.ParseIP(h)
			if ip == nil {
				return fmt.Errorf("%w: unresolved address %q", ErrRemoteBlocked, h)
			}
			return CheckPublicIP(ip)
		},
	}
	return dialer.DialContext
}

// normalizeMediaType lower-cases and strips parameters from a Content-Type.
func normalizeMediaType(raw string) string {
	mt, _, err := mime.ParseMediaType(raw)
	if err != nil {
		if i := strings.IndexByte(raw, ';'); i >= 0 {
			raw = raw[:i]
		}
		return strings.ToLower(strings.TrimSpace(raw))
	}
	return strings.ToLower(strings.TrimSpace(mt))
}
