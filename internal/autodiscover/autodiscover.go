// Package autodiscover finds an email domain's IMAP and SMTP servers: RFC 6186
// SRV records first, then MX records that identify one of the large hosted
// providers, then the Mozilla autoconfig file the domain itself publishes over
// HTTPS. DNS answers are unauthenticated, so callers must only pre-fill
// settings for the user to review, never connect or save on their own.
package autodiscover

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"strings"
)

// Servers is a complete set of connection settings. IMAPTLS means implicit TLS
// (TideMail's IMAP has no STARTTLS); SMTPTLS means implicit TLS on port 465 and
// required STARTTLS on any other port.
type Servers struct {
	IMAPHost string
	IMAPPort int
	IMAPTLS  bool
	SMTPHost string
	SMTPPort int
	SMTPTLS  bool
}

// The hosted providers TideMail has presets for.
var (
	Gmail   = Servers{"imap.gmail.com", 993, true, "smtp.gmail.com", 587, true}
	Outlook = Servers{"outlook.office365.com", 993, true, "smtp.office365.com", 587, true}
	Yahoo   = Servers{"imap.mail.yahoo.com", 993, true, "smtp.mail.yahoo.com", 587, true}
	ICloud  = Servers{"imap.mail.me.com", 993, true, "smtp.mail.me.com", 587, true}
)

// Result is what Discover found and how.
type Result struct {
	Servers
	// Preset names the provider preset ("Gmail", "Outlook", "Yahoo", "iCloud")
	// when the domain's MX records show it is hosted there; empty for SRV.
	Preset string
	// Source says where the settings came from, for display: "DNS SRV",
	// "MX records" or "autoconfig".
	Source string
	// Username is the login the domain's autoconfig file gives when it is not
	// the full address (some hosts log in with the local part); else empty.
	Username string
}

var (
	ErrNoDomain = errors.New("not an email address")
	ErrNotFound = errors.New("no server settings published")
)

// mxProviders maps MX host suffixes to the preset of the provider behind them.
// Custom domains on these hosts (Google Workspace, Microsoft 365, iCloud+
// custom domains, Yahoo) use the provider's own servers.
var mxProviders = []struct {
	suffix, preset string
	servers        Servers
}{
	{".google.com", "Gmail", Gmail},
	{".googlemail.com", "Gmail", Gmail},
	{".mail.protection.outlook.com", "Outlook", Outlook},
	{".mail.icloud.com", "iCloud", ICloud},
	{".yahoodns.net", "Yahoo", Yahoo},
}

type resolver interface {
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
}

// Discover looks up the servers for email's domain.
func Discover(ctx context.Context, email string) (Result, error) {
	return discover(ctx, net.DefaultResolver, httpsGet, email)
}

func discover(ctx context.Context, r resolver, get fetcher, email string) (Result, error) {
	address, domain, err := splitAddress(email)
	if err != nil {
		return Result{}, err
	}
	if s, ok := lookupSRV(ctx, r, domain); ok {
		return Result{Servers: s, Source: "DNS SRV"}, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	// An MX match is definitive for the big hosts and spares them HTTP requests.
	mxs, mxErr := r.LookupMX(ctx, domain)
	for _, mx := range mxs {
		host := "." + strings.ToLower(strings.TrimSuffix(mx.Host, "."))
		for _, p := range mxProviders {
			if strings.HasSuffix(host, p.suffix) {
				return Result{Servers: p.servers, Preset: p.preset, Source: "MX records"}, nil
			}
		}
	}
	if res, ok := lookupAutoconfig(ctx, get, address, domain); ok {
		return res, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if mxErr != nil && len(mxs) == 0 && !isNotFound(mxErr) {
		return Result{}, fmt.Errorf("look up MX for %s: %w", domain, mxErr)
	}
	return Result{}, ErrNotFound
}

// lookupSRV returns servers only when both an implicit-TLS IMAP record and a
// submission record exist. Plain _imap._tcp is ignored: without STARTTLS it
// would mean sending the password unencrypted.
func lookupSRV(ctx context.Context, r resolver, domain string) (Servers, bool) {
	imapHost, imapPort, ok := firstSRV(ctx, r, "imaps", domain)
	if !ok {
		return Servers{}, false
	}
	s := Servers{IMAPHost: imapHost, IMAPPort: imapPort, IMAPTLS: true, SMTPTLS: true}
	// TideMail speaks implicit TLS only on 465 and STARTTLS everywhere else, so
	// an implicit-TLS submissions record on another port is unusable.
	if host, port, ok := firstSRV(ctx, r, "submissions", domain); ok && port == 465 {
		s.SMTPHost, s.SMTPPort = host, port
		return s, true
	}
	if host, port, ok := firstSRV(ctx, r, "submission", domain); ok {
		s.SMTPHost, s.SMTPPort = host, port
		return s, true
	}
	return Servers{}, false
}

// firstSRV returns the most preferred usable target of a service. LookupSRV
// already orders records by priority and weight. A target of "." means the
// domain explicitly does not offer the service (RFC 6186).
func firstSRV(ctx context.Context, r resolver, service, domain string) (string, int, bool) {
	_, addrs, err := r.LookupSRV(ctx, service, "tcp", domain)
	if err != nil {
		return "", 0, false
	}
	for _, a := range addrs {
		host := strings.TrimSuffix(a.Target, ".")
		if host == "" {
			return "", 0, false
		}
		if a.Port != 0 && validHost(host) {
			return strings.ToLower(host), int(a.Port), true
		}
	}
	return "", 0, false
}

// splitAddress returns the bare address and its lowercased domain.
func splitAddress(email string) (address, domain string, err error) {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return "", "", ErrNoDomain
	}
	at := strings.LastIndex(addr.Address, "@")
	if at < 0 {
		return "", "", ErrNoDomain
	}
	domain = strings.ToLower(strings.TrimSuffix(addr.Address[at+1:], "."))
	if !validHost(domain) || !strings.Contains(domain, ".") {
		return "", "", ErrNoDomain
	}
	return addr.Address, domain, nil
}

// validHost accepts DNS hostnames: dot-separated labels of letters, digits and
// inner hyphens, at most 253 characters.
func validHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

func isNotFound(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}
