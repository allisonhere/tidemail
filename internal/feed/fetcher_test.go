package feed

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func serve(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

const validRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <description>A test feed</description>
    <item><title>Item 1</title><link>https://example.com/1</link><guid>1</guid></item>
  </channel>
</rss>`

const htmlWithFeedLink = `<!DOCTYPE html>
<html>
<head>
  <link rel="alternate" type="application/rss+xml" href="/feed.xml">
  <title>Blog</title>
</head>
<body><p>Hello</p></body>
</html>`

const plainHTML = `<!DOCTYPE html>
<html><head><title>Blog</title></head><body><p>Hello</p></body></html>`

const botHTML = `<!DOCTYPE html>
<html><head><title>Checking your browser</title></head>
<body>Checking your browser before accessing the site. DDoS protection by Cloudflare.</body>
</html>`

const jsRequiredHTML = `<!DOCTYPE html>
<html><head><title>Please enable JavaScript</title></head>
<body>JavaScript is required to view this page.</body>
</html>`

const invalidBody = `this is not xml or html at all, just garbage data`

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestFetchFeed_Success(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, validRSS)
	})

	result := FetchFeed(srv.URL)

	if !result.IsSuccess() {
		t.Fatalf("expected success, got kind=%d err=%v", result.Kind, result.Err)
	}
	if result.Feed == nil {
		t.Fatal("expected non-nil feed")
	}
	if result.Feed.Title != "Test Feed" {
		t.Errorf("title = %q, want %q", result.Feed.Title, "Test Feed")
	}
	if len(result.Feed.Items) != 1 {
		t.Errorf("items = %d, want 1", len(result.Feed.Items))
	}
	if result.FinalURL != srv.URL {
		t.Errorf("FinalURL = %q, want %q", result.FinalURL, srv.URL)
	}
}

func TestFetchFeed_SingleRedirect(t *testing.T) {
	var feedURL string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, feedURL+"/feed", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, validRSS)
	})
	feedURL = srv.URL

	result := FetchFeed(srv.URL)

	if !result.IsSuccess() {
		t.Fatalf("expected success after redirect, got kind=%d err=%v", result.Kind, result.Err)
	}
	if len(result.RedirectChain) != 2 {
		t.Errorf("chain len = %d, want 2", len(result.RedirectChain))
	}
	if result.FinalURL != srv.URL+"/feed" {
		t.Errorf("FinalURL = %q", result.FinalURL)
	}
}

func TestFetchFeed_PermanentRedirectSuggestsURLUpdate(t *testing.T) {
	var feedURL string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, feedURL+"/new-feed", http.StatusMovedPermanently)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, validRSS)
	})
	feedURL = srv.URL

	result := FetchFeed(srv.URL)

	if !result.IsSuccess() {
		t.Fatalf("expected success, got kind=%d err=%v", result.Kind, result.Err)
	}
	if !result.SuggestURLUpdate {
		t.Error("expected SuggestURLUpdate=true for 301")
	}
	if result.SuggestedURL != srv.URL+"/new-feed" {
		t.Errorf("SuggestedURL = %q", result.SuggestedURL)
	}
}

func TestFetchFeed_TemporaryRedirectNoURLUpdateSuggestion(t *testing.T) {
	var feedURL string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, feedURL+"/temp", http.StatusFound) // 302
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, validRSS)
	})
	feedURL = srv.URL

	result := FetchFeed(srv.URL)

	if !result.IsSuccess() {
		t.Fatalf("expected success, got kind=%d err=%v", result.Kind, result.Err)
	}
	if result.SuggestURLUpdate {
		t.Error("expected SuggestURLUpdate=false for 302")
	}
}

func TestFetchFeed_307PreservesMethod(t *testing.T) {
	var feedURL string
	seenMethod := ""
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, feedURL+"/dest", http.StatusTemporaryRedirect) // 307
			return
		}
		seenMethod = r.Method
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, validRSS)
	})
	feedURL = srv.URL

	// FetchFeed always uses GET internally, so 307 should preserve GET.
	result := FetchFeed(srv.URL)
	if !result.IsSuccess() {
		t.Fatalf("expected success: %v", result.Err)
	}
	if seenMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", seenMethod)
	}
}

func TestFetchFeed_TooManyRedirects(t *testing.T) {
	count := 0
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		http.Redirect(w, r, fmt.Sprintf("/?n=%d", count), http.StatusFound)
	})

	result := FetchFeed(srv.URL)

	if result.Kind != KindTooManyRedirects {
		t.Errorf("kind = %d, want KindTooManyRedirects", result.Kind)
	}
}

func TestFetchFeed_RedirectLoop(t *testing.T) {
	var aURL, bURL string
	srvA := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, bURL, http.StatusFound)
	})
	srvB := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, aURL, http.StatusFound)
	})
	aURL = srvA.URL
	bURL = srvB.URL

	result := FetchFeed(aURL)

	if result.Kind != KindRedirectLoop {
		t.Errorf("kind = %d, want KindRedirectLoop", result.Kind)
	}
}

func TestFetchFeed_HttpError(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	result := FetchFeed(srv.URL)

	if result.Kind != KindHttpError {
		t.Errorf("kind = %d, want KindHttpError", result.Kind)
	}
	if result.StatusCode != 404 {
		t.Errorf("status = %d, want 404", result.StatusCode)
	}
}

func TestFetchFeed_HtmlInsteadOfFeed(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, plainHTML)
	})

	result := FetchFeed(srv.URL)

	if result.Kind != KindHtmlInsteadOfFeed {
		t.Errorf("kind = %d, want KindHtmlInsteadOfFeed", result.Kind)
	}
}

func TestFetchFeed_HtmlWithFeedLinkAutoDiscovery(t *testing.T) {
	var srvURL string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed.xml" {
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprint(w, validRSS)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, htmlWithFeedLink)
	})
	srvURL = srv.URL

	result := FetchFeed(srvURL)

	if !result.IsSuccess() {
		t.Fatalf("expected success after auto-discovery, got kind=%d err=%v", result.Kind, result.Err)
	}
	if result.Feed.Title != "Test Feed" {
		t.Errorf("title = %q", result.Feed.Title)
	}
}

func TestFetchFeed_BotProtection_Cloudflare(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, botHTML)
	})

	result := FetchFeed(srv.URL)

	if result.Kind != KindBotProtectionDetected {
		t.Errorf("kind = %d, want KindBotProtectionDetected", result.Kind)
	}
}

func TestFetchFeed_BotProtection_JSRequired(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, jsRequiredHTML)
	})

	result := FetchFeed(srv.URL)

	if result.Kind != KindBotProtectionDetected {
		t.Errorf("kind = %d, want KindBotProtectionDetected", result.Kind)
	}
}

func TestFetchFeed_InvalidFeedFormat(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(w, invalidBody)
	})

	result := FetchFeed(srv.URL)

	if result.Kind != KindInvalidFeedFormat {
		t.Errorf("kind = %d, want KindInvalidFeedFormat", result.Kind)
	}
}

func TestFetchFeed_ParseError_InvalidXML(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel><UNCLOSED`)
	})

	result := FetchFeed(srv.URL)

	if result.Kind != KindParseError {
		t.Errorf("kind = %d, want KindParseError", result.Kind)
	}
}

func TestFetchFeed_SnippetCaptured(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, plainHTML)
	})

	result := FetchFeed(srv.URL)

	if result.Snippet == "" {
		t.Error("expected non-empty snippet")
	}
	if !strings.Contains(result.Snippet, "<!DOCTYPE") {
		t.Errorf("snippet doesn't look like HTML: %q", result.Snippet)
	}
}

func TestFetchFeed_RedirectChainRecorded(t *testing.T) {
	var baseURL string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.Redirect(w, r, baseURL+"/a", http.StatusFound)
		case "/a":
			http.Redirect(w, r, baseURL+"/feed", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprint(w, validRSS)
		}
	})
	baseURL = srv.URL

	result := FetchFeed(srv.URL)

	if !result.IsSuccess() {
		t.Fatalf("expected success: %v", result.Err)
	}
	if len(result.RedirectChain) != 3 {
		t.Errorf("chain len = %d, want 3: %v", len(result.RedirectChain), result.RedirectChain)
	}
	if result.RedirectChain[0] != srv.URL {
		t.Errorf("chain[0] = %q, want %q", result.RedirectChain[0], srv.URL)
	}
}

func TestFetchFeed_FriendlyMessages(t *testing.T) {
	cases := []struct {
		kind FetchErrorKind
		want string
	}{
		{KindNetworkError, "network error"},
		{KindTimeout, "timed out"},
		{KindTooManyRedirects, "too many redirects"},
		{KindRedirectLoop, "redirect loop"},
		{KindHttpError, "error"},
		{KindHtmlInsteadOfFeed, "HTML page"},
		{KindBotProtectionDetected, "bot protection"},
		{KindInvalidFeedFormat, "not valid"},
		{KindParseError, "parse"},
	}
	for _, tc := range cases {
		r := &FetchResult{Kind: tc.kind, StatusCode: 500}
		msg := r.FriendlyMessage()
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(tc.want)) {
			t.Errorf("kind=%d: message %q doesn't contain %q", tc.kind, msg, tc.want)
		}
	}
}

func TestStripAntiScraperNotice(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"clean content", "Hello world", "Hello world"},
		{"notice stripped", "Great article. This RSS feed is intended for readers, not scrapers.", "Great article."},
		{"case insensitive", "Post content. this rss feed is intended for readers, not scrapers", "Post content."},
		{"mid-sentence preserves separator", "Intro. This RSS feed is intended for readers, not scrapers. Outro.", "Intro.   Outro."},
		{"html paragraph", "<p>Body</p><p>This RSS feed is intended for readers, not scrapers.</p>", "<p>Body</p><p> </p>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripAntiScraperNotice(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsBotProtection(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{"Checking your browser before accessing the site", true},
		{"DDoS protection by Cloudflare Ray ID", true},
		{"Please enable JavaScript and cookies to continue", true},
		{"JavaScript is required to view this page", true},
		{"<html><body>Normal blog post here</body></html>", false},
	}
	for _, tc := range cases {
		got := isBotProtection([]byte(tc.body))
		if got != tc.want {
			t.Errorf("isBotProtection(%q) = %v, want %v", tc.body[:min(50, len(tc.body))], got, tc.want)
		}
	}
}
