package feed

import "fmt"

// maxRedirects is the maximum number of HTTP redirects followed before giving up.
const maxRedirects = 10

// snippetLen is the number of body bytes captured in FetchResult.Snippet.
const snippetLen = 300

// defaultMaxFeedBodyBytes is the default feed body size accepted for parsing.
const defaultMaxFeedBodyBytes = 10 << 20 // 10 MiB

var maxFeedBodyBytes = defaultMaxFeedBodyBytes

func SetMaxFeedBodyBytes(limit int) {
	if limit <= 0 {
		maxFeedBodyBytes = defaultMaxFeedBodyBytes
		return
	}
	maxFeedBodyBytes = limit
}

// FetchErrorKind classifies the outcome of a FetchFeed call.
type FetchErrorKind int

const (
	KindSuccess               FetchErrorKind = iota
	KindNetworkError                         // DNS failure, connection refused, etc.
	KindTimeout                              // deadline exceeded
	KindTooManyRedirects                     // more than maxRedirects hops
	KindRedirectLoop                         // same URL appeared twice in the chain
	KindHttpError                            // non-2xx final status code
	KindHtmlInsteadOfFeed                    // 200 HTML page, no feed auto-discovered
	KindBotProtectionDetected                // JS challenge, captcha, Cloudflare wall
	KindFeedTooLarge                         // body exceeds maxFeedBodyBytes
	KindInvalidFeedFormat                    // body is neither HTML nor valid feed XML
	KindParseError                           // gofeed parse failure
)

// FetchResult is the full outcome of a FetchFeed call.
type FetchResult struct {
	Kind          FetchErrorKind
	OriginalURL   string
	RedirectChain []string // every URL visited, including OriginalURL; len >= 1
	FinalURL      string
	StatusCode    int // 0 when no HTTP response was received (e.g. network error)
	ContentType   string
	Snippet       string // up to snippetLen bytes of the response body
	Err           error
	Feed          *ParsedFeed // non-nil only on KindSuccess

	// SuggestURLUpdate is true when a 301/308 permanent redirect was followed and
	// the final URL differs from the original — the caller can update the stored URL.
	SuggestURLUpdate bool
	SuggestedURL     string
}

// IsSuccess reports whether the fetch produced a parsed feed.
func (r *FetchResult) IsSuccess() bool { return r.Kind == KindSuccess }

// FriendlyMessage returns a short human-readable description of the result.
// Returns an empty string on success.
func (r *FetchResult) FriendlyMessage() string {
	switch r.Kind {
	case KindSuccess:
		return ""
	case KindNetworkError:
		return "network error — check your connection"
	case KindTimeout:
		return "connection timed out"
	case KindTooManyRedirects:
		return fmt.Sprintf("too many redirects (>%d)", maxRedirects)
	case KindRedirectLoop:
		return "redirect loop detected"
	case KindHttpError:
		switch r.StatusCode {
		case 401:
			return "authentication required (401)"
		case 403:
			return "access denied (403)"
		case 404:
			return "feed not found (404)"
		case 429:
			return "rate limited (429)"
		default:
			return fmt.Sprintf("server error (%d)", r.StatusCode)
		}
	case KindHtmlInsteadOfFeed:
		return "URL returned an HTML page, not a feed"
	case KindBotProtectionDetected:
		return "bot protection or login wall detected"
	case KindFeedTooLarge:
		return fmt.Sprintf("feed is too large to parse (>%d MB)", maxFeedBodyBytes>>20)
	case KindInvalidFeedFormat:
		return "response is not valid RSS/Atom/JSON"
	case KindParseError:
		return "failed to parse feed"
	default:
		return "unknown error"
	}
}

// HasDetails reports whether the result carries diagnostic info worth
// showing in a details view (status code, content-type, URL chain, snippet).
func (r *FetchResult) HasDetails() bool {
	switch r.Kind {
	case KindHttpError, KindHtmlInsteadOfFeed, KindBotProtectionDetected,
		KindTooManyRedirects, KindRedirectLoop, KindFeedTooLarge, KindInvalidFeedFormat, KindParseError:
		return true
	}
	return false
}
