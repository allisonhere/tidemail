package feed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

// feedHTTPClient never follows redirects so we can track hops manually.
var feedHTTPClient = &http.Client{
	Timeout:   15 * time.Second,
	Transport: newBrowserTransport(),
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// newBrowserTransport returns a transport whose TLS ClientHello mimics Chrome,
// bypassing bot-detection fingerprinting (e.g. Cloudflare JA3 scoring).
// ALPN is restricted to http/1.1 so Go's transport can read the response;
// the ALPN extension type is still present in the hello, preserving the JA3 hash.
func newBrowserTransport() *http.Transport {
	d := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := d.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			host, _, _ := net.SplitHostPort(addr)

			spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
			if err != nil {
				conn.Close()
				return nil, fmt.Errorf("utls spec: %w", err)
			}
			for i, ext := range spec.Extensions {
				if alpn, ok := ext.(*utls.ALPNExtension); ok {
					alpn.AlpnProtocols = []string{"http/1.1"}
					spec.Extensions[i] = alpn
					break
				}
			}

			uconn := utls.UClient(conn, &utls.Config{ServerName: host}, utls.HelloCustom)
			if err := uconn.ApplyPreset(&spec); err != nil {
				conn.Close()
				return nil, fmt.Errorf("utls preset: %w", err)
			}
			if err := uconn.HandshakeContext(ctx); err != nil {
				conn.Close()
				return nil, err
			}
			return uconn, nil
		},
	}
}

// articleHTTPClient is a plain client for article text fetching.
var articleHTTPClient = &http.Client{Timeout: 15 * time.Second}

var errFeedBodyTooLarge = errors.New("feed body exceeds size limit")

// FetchFeed performs a robust fetch of the feed at originalURL.
// It follows redirects manually (up to maxRedirects), detects loops,
// preserves method semantics on 307/308, and classifies all error kinds.
func FetchFeed(originalURL string) *FetchResult {
	return fetchFeed(originalURL, maxRedirects, map[string]bool{originalURL: true})
}

// fetchFeed is the internal implementation. budget and seen are shared across
// recursive calls (HTML <link rel="alternate"> discovery, parser-requested
// redirects) so the redirect cap and loop-detection map can't be bypassed.
func fetchFeed(originalURL string, budget int, seen map[string]bool) *FetchResult {
	r := &FetchResult{
		OriginalURL:   originalURL,
		FinalURL:      originalURL,
		RedirectChain: []string{originalURL},
	}

	method := http.MethodGet
	currentURL := originalURL
	hadPermanent := false
	permanentDest := ""

	for hop := 0; hop < budget; hop++ {
		req, err := http.NewRequest(method, currentURL, nil)
		if err != nil {
			r.Kind = KindNetworkError
			r.Err = fmt.Errorf("build request: %w", err)
			return r
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; tide/1.0; +https://github.com/allisonhere/tide)")
		req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/feed+json, text/xml, application/xml;q=0.9, */*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")

		resp, err := feedHTTPClient.Do(req)
		if err != nil {
			r.Kind = classifyNetErr(err)
			r.Err = err
			return r
		}

		code := resp.StatusCode

		// ── Redirect ──────────────────────────────────────────────────────────
		if code == 301 || code == 302 || code == 303 || code == 307 || code == 308 {
			resp.Body.Close()
			loc := resp.Header.Get("Location")
			if loc == "" {
				r.Kind = KindNetworkError
				r.Err = fmt.Errorf("%d redirect with empty Location", code)
				return r
			}
			resolved, err := resolveURL(currentURL, loc)
			if err != nil {
				r.Kind = KindNetworkError
				r.Err = fmt.Errorf("bad Location %q: %w", loc, err)
				return r
			}
			if seen[resolved] {
				r.Kind = KindRedirectLoop
				r.Err = fmt.Errorf("redirect loop: %s visited twice", resolved)
				return r
			}
			seen[resolved] = true
			r.RedirectChain = append(r.RedirectChain, resolved)
			r.FinalURL = resolved

			if code == 301 || code == 308 {
				hadPermanent = true
				permanentDest = resolved
			}
			// 307/308 preserve the original method; 301/302/303 fall back to GET.
			if code != 307 && code != 308 {
				method = http.MethodGet
			}
			currentURL = resolved
			continue
		}

		// ── Final response ────────────────────────────────────────────────────
		r.StatusCode = code
		r.ContentType = resp.Header.Get("Content-Type")

		if code < 200 || code >= 300 {
			resp.Body.Close()
			r.Kind = KindHttpError
			r.Err = fmt.Errorf("HTTP %d: %s", code, http.StatusText(code))
			return r
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(maxFeedBodyBytes+1)))
		resp.Body.Close()
		if readErr != nil {
			r.Kind = KindNetworkError
			r.Err = fmt.Errorf("read body: %w", readErr)
			return r
		}
		if len(body) > maxFeedBodyBytes {
			body = body[:maxFeedBodyBytes]
			end := snippetLen
			if len(body) < end {
				end = len(body)
			}
			r.Snippet = string(body[:end])
			r.Kind = KindFeedTooLarge
			r.Err = fmt.Errorf("%w (> %d bytes)", errFeedBodyTooLarge, maxFeedBodyBytes)
			return r
		}

		end := snippetLen
		if len(body) < end {
			end = len(body)
		}
		r.Snippet = string(body[:end])

		ct := strings.ToLower(r.ContentType)

		// ── HTML handling ─────────────────────────────────────────────────────
		if strings.Contains(ct, "text/html") || (looksLikeHTML(body) && !bodyIsFeedXML(body)) {
			if feedURL := discoverFeedURL(body); feedURL != "" {
				discovered, _ := resolveURL(currentURL, feedURL)
				if seen[discovered] {
					r.Kind = KindRedirectLoop
					r.Err = fmt.Errorf("redirect loop: %s visited twice", discovered)
					return r
				}
				seen[discovered] = true
				remaining := budget - hop - 1
				if remaining <= 0 {
					r.Kind = KindTooManyRedirects
					r.Err = fmt.Errorf("exceeded %d redirects", maxRedirects)
					return r
				}
				r2 := fetchFeed(discovered, remaining, seen)
				r2.OriginalURL = originalURL
				// Merge our chain with the sub-fetch's chain (skip its duplicate head).
				if len(r2.RedirectChain) > 1 {
					r2.RedirectChain = append(r.RedirectChain, r2.RedirectChain[1:]...)
				} else {
					r2.RedirectChain = append(r.RedirectChain, r2.RedirectChain...)
				}
				return r2
			}
			if isBotProtection(body) {
				r.Kind = KindBotProtectionDetected
				r.Err = fmt.Errorf("bot protection page")
			} else {
				r.Kind = KindHtmlInsteadOfFeed
				r.Err = fmt.Errorf("URL returned HTML, not a feed — try /feed, /rss, or /atom.xml")
			}
			return r
		}

		// ── Non-feed content type ─────────────────────────────────────────────
		if !isFeedContentType(ct) && !bodyIsFeedXML(body) {
			r.Kind = KindInvalidFeedFormat
			r.Err = fmt.Errorf("unexpected content-type %q", r.ContentType)
			return r
		}

		// ── Parse ─────────────────────────────────────────────────────────────
		parsed, parseErr := Parse(bytes.NewReader(body))
		if parseErr != nil {
			var redir *ErrNeedRedirect
			if isRedirect(parseErr, &redir) {
				discovered, _ := resolveURL(currentURL, redir.URL)
				if seen[discovered] {
					r.Kind = KindRedirectLoop
					r.Err = fmt.Errorf("redirect loop: %s visited twice", discovered)
					return r
				}
				seen[discovered] = true
				remaining := budget - hop - 1
				if remaining <= 0 {
					r.Kind = KindTooManyRedirects
					r.Err = fmt.Errorf("exceeded %d redirects", maxRedirects)
					return r
				}
				r2 := fetchFeed(discovered, remaining, seen)
				r2.OriginalURL = originalURL
				if len(r2.RedirectChain) > 1 {
					r2.RedirectChain = append(r.RedirectChain, r2.RedirectChain[1:]...)
				} else {
					r2.RedirectChain = append(r.RedirectChain, r2.RedirectChain...)
				}
				return r2
			}
			r.Kind = KindParseError
			r.Err = parseErr
			return r
		}

		r.Kind = KindSuccess
		r.Feed = parsed
		if hadPermanent && permanentDest != "" && permanentDest != originalURL {
			r.SuggestURLUpdate = true
			r.SuggestedURL = permanentDest
		}
		return r
	}

	r.Kind = KindTooManyRedirects
	r.Err = fmt.Errorf("exceeded %d redirects", maxRedirects)
	return r
}

// FetchAndParse is the legacy interface backed by FetchFeed.
func FetchAndParse(feedURL string) (*ParsedFeed, string, error) {
	r := FetchFeed(feedURL)
	if r.IsSuccess() {
		return r.Feed, r.FinalURL, nil
	}
	return nil, r.FinalURL, r.Err
}

// Fetch is the simple HTTP helper used for article text fetching.
func Fetch(targetURL string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "tide/1.0")
	req.Header.Set("Accept", "text/html, */*")

	resp, err := articleHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", targetURL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, targetURL)
	}
	return resp.Body, nil
}

// ── Internal helpers ─────────────────────────────────────────────────────────

func classifyNetErr(err error) FetchErrorKind {
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return KindTimeout
	}
	return KindNetworkError
}

func isFeedContentType(ct string) bool {
	return strings.Contains(ct, "rss") ||
		strings.Contains(ct, "atom") ||
		strings.Contains(ct, "feed") ||
		strings.Contains(ct, "xml") ||
		strings.Contains(ct, "json")
}

func bodyIsFeedXML(data []byte) bool {
	prefix := strings.TrimSpace(string(data[:min(512, len(data))]))
	return strings.HasPrefix(prefix, "<?xml") ||
		strings.HasPrefix(prefix, "<rss") ||
		strings.HasPrefix(prefix, "<feed") ||
		strings.HasPrefix(prefix, "<atom")
}

// isBotProtection detects common bot-challenge and login-wall signatures.
func isBotProtection(data []byte) bool {
	sample := min(8192, len(data))
	lower := strings.ToLower(string(data[:sample]))
	for _, pat := range []string{
		"javascript is required",
		"please enable javascript",
		"enable javascript to continue",
		"checking your browser",
		"ddos protection by cloudflare",
		"cf-browser-verification",
		"cloudflare ray id",
		"attention required! | cloudflare",
		"please complete the security check",
		"are you human",
		"captcha",
		"enable cookies",
		"your ip has been",
		"this page requires javascript",
		"browser check",
	} {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

func isRedirect(err error, out **ErrNeedRedirect) bool {
	if r, ok := err.(*ErrNeedRedirect); ok {
		*out = r
		return true
	}
	return false
}

func resolveURL(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(r).String(), nil
}
