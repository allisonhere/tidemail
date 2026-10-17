package autodiscover

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// fetcher GETs a URL and returns its body; a seam so tests serve canned files.
type fetcher func(ctx context.Context, url string) ([]byte, error)

// maxConfigBytes bounds an autoconfig file; real ones are a few kilobytes.
const maxConfigBytes = 1 << 20

var autoconfigClient = &http.Client{
	// Follow at most a few redirects, and never off HTTPS: a downgrade would let
	// anyone on the path rewrite the servers.
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("autoconfig redirect left HTTPS")
		}
		if len(via) >= 3 {
			return errors.New("too many autoconfig redirects")
		}
		return nil
	},
}

func httpsGet(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := autoconfigClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxConfigBytes))
}

// lookupAutoconfig reads the Mozilla autoconfig file the domain itself
// publishes (cPanel and many hosting panels serve one), over HTTPS only. It
// never asks a third party such as Thunderbird's ISPDB. Fetch failures mean
// "not published": most domains have no such file.
func lookupAutoconfig(ctx context.Context, get fetcher, email, domain string) (Result, bool) {
	q := "?emailaddress=" + url.QueryEscape(email)
	for _, u := range []string{
		"https://autoconfig." + domain + "/mail/config-v1.1.xml" + q,
		"https://" + domain + "/.well-known/autoconfig/mail/config-v1.1.xml" + q,
	} {
		if ctx.Err() != nil {
			return Result{}, false
		}
		body, err := get(ctx, u)
		if err != nil {
			continue
		}
		if res, ok := parseAutoconfig(body, email); ok {
			return res, true
		}
	}
	return Result{}, false
}

type clientConfig struct {
	Providers []struct {
		Incoming []configServer `xml:"incomingServer"`
		Outgoing []configServer `xml:"outgoingServer"`
	} `xml:"emailProvider"`
}

type configServer struct {
	Type       string   `xml:"type,attr"`
	Hostname   string   `xml:"hostname"`
	Port       int      `xml:"port"`
	SocketType string   `xml:"socketType"`
	Username   string   `xml:"username"`
	Auth       []string `xml:"authentication"`
}

// parseAutoconfig picks the first IMAP and SMTP servers TideMail can use
// safely: IMAP over implicit TLS (there is no IMAP STARTTLS), SMTP over TLS on
// 465 or STARTTLS elsewhere, never plaintext, and with password sign-in.
func parseAutoconfig(body []byte, email string) (Result, bool) {
	var cfg clientConfig
	if err := xml.Unmarshal(body, &cfg); err != nil {
		return Result{}, false
	}
	local, domain, _ := strings.Cut(email, "@")
	expand := strings.NewReplacer("%EMAILADDRESS%", email, "%EMAILLOCALPART%", local, "%EMAILDOMAIN%", domain).Replace
	for _, p := range cfg.Providers {
		var res Result
		var imapOK, smtpOK bool
		for _, s := range p.Incoming {
			host := strings.ToLower(strings.TrimSpace(expand(s.Hostname)))
			socket := strings.ToUpper(strings.TrimSpace(s.SocketType))
			if imapOK || !strings.EqualFold(s.Type, "imap") || socket != "SSL" || !usablePort(s.Port) || !validHost(host) || !passwordAuth(s.Auth) {
				continue
			}
			res.IMAPHost, res.IMAPPort, res.IMAPTLS = host, s.Port, true
			if user := strings.TrimSpace(expand(s.Username)); user != "" && user != email {
				res.Username = user
			}
			imapOK = true
		}
		for _, s := range p.Outgoing {
			host := strings.ToLower(strings.TrimSpace(expand(s.Hostname)))
			socket := strings.ToUpper(strings.TrimSpace(s.SocketType))
			usable := (socket == "SSL" && s.Port == 465) || (socket == "STARTTLS" && s.Port != 465)
			if smtpOK || !strings.EqualFold(s.Type, "smtp") || !usable || !usablePort(s.Port) || !validHost(host) || !passwordAuth(s.Auth) {
				continue
			}
			res.SMTPHost, res.SMTPPort, res.SMTPTLS = host, s.Port, true
			smtpOK = true
		}
		if imapOK && smtpOK {
			res.Source = "autoconfig"
			return res, true
		}
	}
	return Result{}, false
}

func usablePort(p int) bool { return p > 0 && p <= 65535 }

// passwordAuth reports whether a server accepts a password sign-in, the only
// kind a Custom account has. No listed method means the default, a password.
func passwordAuth(methods []string) bool {
	if len(methods) == 0 {
		return true
	}
	for _, m := range methods {
		switch strings.ToLower(strings.TrimSpace(m)) {
		case "password-cleartext", "plain", "password-encrypted", "secure":
			return true
		}
	}
	return false
}
