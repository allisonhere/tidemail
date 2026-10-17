package autodiscover

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// cpanelConfig is shaped like the file cPanel serves at autoconfig.<domain>:
// POP3 first, an IMAP STARTTLS entry TideMail can't use, then IMAP over SSL.
const cpanelConfig = `<?xml version="1.0"?>
<clientConfig version="1.1">
 <emailProvider id="example.org">
  <domain>example.org</domain>
  <incomingServer type="pop3">
   <hostname>mail.%EMAILDOMAIN%</hostname><port>995</port><socketType>SSL</socketType>
   <username>%EMAILADDRESS%</username><authentication>password-cleartext</authentication>
  </incomingServer>
  <incomingServer type="imap">
   <hostname>mail.%EMAILDOMAIN%</hostname><port>143</port><socketType>STARTTLS</socketType>
   <username>%EMAILADDRESS%</username><authentication>password-cleartext</authentication>
  </incomingServer>
  <incomingServer type="imap">
   <hostname>Mail.%EMAILDOMAIN%</hostname><port>993</port><socketType>SSL</socketType>
   <username>%EMAILADDRESS%</username><authentication>password-cleartext</authentication>
  </incomingServer>
  <outgoingServer type="smtp">
   <hostname>mail.%EMAILDOMAIN%</hostname><port>465</port><socketType>SSL</socketType>
   <username>%EMAILADDRESS%</username><authentication>password-cleartext</authentication>
  </outgoingServer>
 </emailProvider>
</clientConfig>`

func config(incoming, outgoing string) string {
	return `<clientConfig><emailProvider id="x">` + incoming + outgoing + `</emailProvider></clientConfig>`
}

func server(kind, typ, host, port, socket, user, auth string) string {
	return `<` + kind + ` type="` + typ + `"><hostname>` + host + `</hostname><port>` + port + `</port><socketType>` + socket +
		`</socketType><username>` + user + `</username>` + auth + `</` + kind + `>`
}

var (
	imapSSL     = server("incomingServer", "imap", "imap.example.org", "993", "SSL", "%EMAILADDRESS%", "")
	smtpStart   = server("outgoingServer", "smtp", "smtp.example.org", "587", "STARTTLS", "%EMAILADDRESS%", "")
	imapLocal   = server("incomingServer", "imap", "imap.example.org", "993", "SSL", "%EMAILLOCALPART%", "")
	imapStart   = server("incomingServer", "imap", "imap.example.org", "143", "STARTTLS", "%EMAILADDRESS%", "")
	smtpPlain   = server("outgoingServer", "smtp", "smtp.example.org", "25", "plain", "%EMAILADDRESS%", "")
	smtpSSL587  = server("outgoingServer", "smtp", "smtp.example.org", "587", "SSL", "%EMAILADDRESS%", "")
	imapOAuth   = server("incomingServer", "imap", "imap.example.org", "993", "SSL", "%EMAILADDRESS%", "<authentication>OAuth2</authentication>")
	imapBadHost = server("incomingServer", "imap", "imap.example.org/evil", "993", "SSL", "%EMAILADDRESS%", "")
)

// serve answers only the listed URLs (by path) and records every request.
func serve(files map[string]string, requested *[]string) fetcher {
	return func(_ context.Context, u string) ([]byte, error) {
		*requested = append(*requested, u)
		p, err := url.Parse(u)
		if err != nil {
			return nil, err
		}
		if body, ok := files[p.Host+p.Path]; ok {
			return []byte(body), nil
		}
		return nil, errors.New("404 Not Found")
	}
}

func TestAutoconfig(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  Result
		err   error
	}{
		{"cpanel file", map[string]string{"autoconfig.example.org/mail/config-v1.1.xml": cpanelConfig},
			Result{Servers: Servers{"mail.example.org", 993, true, "mail.example.org", 465, true}, Source: "autoconfig"}, nil},
		{"well-known fallback", map[string]string{"example.org/.well-known/autoconfig/mail/config-v1.1.xml": config(imapSSL, smtpStart)},
			Result{Servers: Servers{"imap.example.org", 993, true, "smtp.example.org", 587, true}, Source: "autoconfig"}, nil},
		{"local-part login", map[string]string{"autoconfig.example.org/mail/config-v1.1.xml": config(imapLocal, smtpStart)},
			Result{Servers: Servers{"imap.example.org", 993, true, "smtp.example.org", 587, true}, Source: "autoconfig", Username: "alice"}, nil},
		{"imap starttls only is unusable", map[string]string{"autoconfig.example.org/mail/config-v1.1.xml": config(imapStart, smtpStart)},
			Result{}, ErrNotFound},
		{"plaintext smtp only is unusable", map[string]string{"autoconfig.example.org/mail/config-v1.1.xml": config(imapSSL, smtpPlain)},
			Result{}, ErrNotFound},
		{"implicit tls off 465 is unusable", map[string]string{"autoconfig.example.org/mail/config-v1.1.xml": config(imapSSL, smtpSSL587)},
			Result{}, ErrNotFound},
		{"oauth-only server is skipped", map[string]string{"autoconfig.example.org/mail/config-v1.1.xml": config(imapOAuth, smtpStart)},
			Result{}, ErrNotFound},
		{"bad hostname is skipped", map[string]string{"autoconfig.example.org/mail/config-v1.1.xml": config(imapBadHost, smtpStart)},
			Result{}, ErrNotFound},
		{"malformed file", map[string]string{"autoconfig.example.org/mail/config-v1.1.xml": "<clientConfig><emailProvider"},
			Result{}, ErrNotFound},
		{"nothing published", nil, Result{}, ErrNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var requested []string
			got, err := discover(context.Background(), fakeResolver{}, serve(c.files, &requested), "Alice <alice@example.org>")
			if !errors.Is(err, c.err) || got != c.want {
				t.Fatalf("got %+v, %v; want %+v, %v", got, err, c.want, c.err)
			}
			for _, u := range requested {
				p, _ := url.Parse(u)
				if p.Scheme != "https" || (p.Host != "autoconfig.example.org" && p.Host != "example.org") {
					t.Fatalf("request left the user's own domain over HTTPS: %s", u)
				}
				if p.Query().Get("emailaddress") != "alice@example.org" {
					t.Fatalf("request does not carry the bare address: %s", u)
				}
			}
		})
	}
}

func TestMXPresetSkipsAutoconfigRequests(t *testing.T) {
	var requested []string
	r := fakeResolver{mx: map[string][]*net.MX{"corp.example": mx("aspmx.l.google.com.")}}
	got, err := discover(context.Background(), r, serve(nil, &requested), "a@corp.example")
	if err != nil || got.Preset != "Gmail" || len(requested) != 0 {
		t.Fatalf("got %+v, %v; requests %v", got, err, requested)
	}
}

func TestAutoconfigBeatsMXFailure(t *testing.T) {
	var requested []string
	boom := &net.DNSError{Err: "server misbehaving", Name: "example.org", IsTemporary: true}
	files := map[string]string{"autoconfig.example.org/mail/config-v1.1.xml": config(imapSSL, smtpStart)}
	got, err := discover(context.Background(), fakeResolver{mxErr: boom}, serve(files, &requested), "alice@example.org")
	if err != nil || got.Source != "autoconfig" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestAutoconfigRedirectsStayOnHTTPS(t *testing.T) {
	via := []*http.Request{{}}
	for raw, ok := range map[string]bool{"https://mail.example.org/x.xml": true, "http://mail.example.org/x.xml": false} {
		u, _ := url.Parse(raw)
		err := autoconfigClient.CheckRedirect(&http.Request{URL: u}, via)
		if (err == nil) != ok {
			t.Fatalf("redirect to %s: err %v", raw, err)
		}
	}
	u, _ := url.Parse("https://mail.example.org/x.xml")
	if err := autoconfigClient.CheckRedirect(&http.Request{URL: u}, make([]*http.Request, 3)); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("redirect chain not bounded: %v", err)
	}
}

func TestHTTPSGet(t *testing.T) {
	big := strings.Repeat("x", maxConfigBytes+10)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("<clientConfig/>"))
		case "/big":
			_, _ = w.Write([]byte(big))
		case "/downgrade":
			http.Redirect(w, r, "http://"+r.Host+"/ok", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := autoconfigClient.Transport
	autoconfigClient.Transport = srv.Client().Transport // trust the test certificate
	defer func() { autoconfigClient.Transport = old }()
	ctx := context.Background()

	if body, err := httpsGet(ctx, srv.URL+"/ok"); err != nil || string(body) != "<clientConfig/>" {
		t.Fatalf("ok: %q %v", body, err)
	}
	if _, err := httpsGet(ctx, srv.URL+"/missing"); err == nil {
		t.Fatal("404 was returned as a config file")
	}
	if body, err := httpsGet(ctx, srv.URL+"/big"); err != nil || len(body) != maxConfigBytes {
		t.Fatalf("oversized body: %d bytes, %v", len(body), err)
	}
	if _, err := httpsGet(ctx, srv.URL+"/downgrade"); err == nil || !strings.Contains(err.Error(), "left HTTPS") {
		t.Fatalf("redirect to plain HTTP was followed: %v", err)
	}
}
