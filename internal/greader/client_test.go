package greader

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestUnescapeAPIString(t *testing.T) {
	if got := UnescapeAPIString(`  NASA&#039;s feed  `); got != "NASA's feed" {
		t.Fatalf("apostrophe entity: got %q", got)
	}
	if got := UnescapeAPIString(`Tom &amp; Jerry`); got != "Tom & Jerry" {
		t.Fatalf("ampersand entity: got %q", got)
	}
	if got := UnescapeAPIString(`NASA&amp;#039;s Podcast`); got != "NASA's Podcast" {
		t.Fatalf("double-encoded apostrophe: got %q", got)
	}
}

func TestListSubscriptionsAndUnreadCounts(t *testing.T) {
	var authHeader string
	client := New("https://rss.example.com/api/greader.php", "alice", "secret")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			body, _ := io.ReadAll(req.Body)
			if got := string(body); got != "Email=alice&Passwd=secret" {
				t.Fatalf("unexpected login body %q", got)
			}
			return responseWithBody(http.StatusOK, "SID=alice/token\nAuth=alice/token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/subscription/list?output=json":
			authHeader = req.Header.Get("Authorization")
			return responseWithJSON(http.StatusOK, `{
				"subscriptions": [
					{
						"id": "feed/http://example.com/feed.xml",
						"title": "NASA&#039;s Example Feed",
						"url": "http://example.com/feed.xml",
						"htmlUrl": "http://example.com/",
						"categories": [{"id":"user/-/label/Tech","label":"Tom &amp; Jerry"}]
					}
				]
			}`), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/unread-count?output=json":
			return responseWithJSON(http.StatusOK, `{
				"unreadcounts": [
					{"id": "feed/http://example.com/feed.xml", "count": 7}
				]
			}`), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}

	subscriptions, err := client.ListSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("ListSubscriptions returned error: %v", err)
	}
	if len(subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subscriptions))
	}
	if subscriptions[0].Title != "NASA's Example Feed" {
		t.Fatalf("expected decoded title, got %q", subscriptions[0].Title)
	}
	if subscriptions[0].Category != "Tom & Jerry" {
		t.Fatalf("expected decoded category label, got %q", subscriptions[0].Category)
	}
	if authHeader != "GoogleLogin auth=alice/token" {
		t.Fatalf("unexpected Authorization header %q", authHeader)
	}

	counts, err := client.UnreadCounts(context.Background())
	if err != nil {
		t.Fatalf("UnreadCounts returned error: %v", err)
	}
	if got := counts["feed/http://example.com/feed.xml"]; got != 7 {
		t.Fatalf("expected unread count 7, got %d", got)
	}
}

func TestStreamContentsParsesEntries(t *testing.T) {
	var requestedURL string
	client := New("https://rss.example.com/api/greader.php", "alice", "secret")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return responseWithBody(http.StatusOK, "Auth=alice/token\n"), nil
		default:
			requestedURL = req.URL.String()
			return responseWithJSON(http.StatusOK, `{
				"items": [
					{
						"id": "tag:google.com,2005:reader/item/abc123",
						"title": "It&#039;s a match",
						"published": 1710000000,
						"categories": ["user/-/state/com.google/read"],
						"alternate": [{"href":"https://example.com/articles/1"}],
						"summary": {"content":"<p>Hello world</p>"},
						"origin": {"streamId":"feed/http://example.com/feed.xml"}
					}
				]
			}`), nil
		}
	})}

	entries, err := client.StreamContents(context.Background(), "feed/http://example.com/feed.xml", 25)
	if err != nil {
		t.Fatalf("StreamContents returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Link != "https://example.com/articles/1" {
		t.Fatalf("unexpected link %q", entries[0].Link)
	}
	if !entries[0].Read {
		t.Fatal("expected entry to be marked read from categories")
	}
	if entries[0].Title != "It's a match" {
		t.Fatalf("expected decoded article title, got %q", entries[0].Title)
	}
	if !strings.Contains(entries[0].ContentHTML, "Hello world") {
		t.Fatalf("unexpected content %q", entries[0].ContentHTML)
	}
	wantURL := "https://rss.example.com/api/greader.php/reader/api/0/stream/contents/feed%2Fhttp:%2F%2Fexample.com%2Ffeed.xml?n=25&output=json&xt=user%2F-%2Fstate%2Fcom.google%2Fread"
	if requestedURL != wantURL {
		t.Fatalf("unexpected stream request %q want %q", requestedURL, wantURL)
	}
}

func TestQuickAddReturnsStreamID(t *testing.T) {
	client := New("https://rss.example.com/api/greader.php", "alice", "secret")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return responseWithBody(http.StatusOK, "Auth=alice/token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/subscription/quickadd":
			if req.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", req.Method)
			}
			body, _ := io.ReadAll(req.Body)
			if got := string(body); got != "quickadd=https%3A%2F%2Fexample.com" {
				t.Fatalf("unexpected quickadd body %q", got)
			}
			if got := req.Header.Get("Authorization"); got != "GoogleLogin auth=alice/token" {
				t.Fatalf("unexpected Authorization header %q", got)
			}
			return responseWithJSON(http.StatusOK, `{
				"numResults": 1,
				"query": "https://example.com/feed.xml",
				"streamId": "feed/52",
				"streamName": "Example Feed"
			}`), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}

	result, err := client.QuickAdd(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("QuickAdd returned error: %v", err)
	}
	if result.StreamID != "feed/52" {
		t.Fatalf("unexpected stream id %q", result.StreamID)
	}
	if result.StreamName != "Example Feed" {
		t.Fatalf("unexpected stream name %q", result.StreamName)
	}
}

func TestMarkEntryReadUsesTokenAndEditTag(t *testing.T) {
	client := New("https://rss.example.com/api/greader.php", "alice", "secret")
	tokenCalls := 0
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return responseWithBody(http.StatusOK, "Auth=alice/token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/token":
			tokenCalls++
			if got := req.Header.Get("Authorization"); got != "GoogleLogin auth=alice/token" {
				t.Fatalf("unexpected Authorization header %q", got)
			}
			return responseWithBody(http.StatusOK, "csrf-token"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/edit-tag":
			if req.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", req.Method)
			}
			body, _ := io.ReadAll(req.Body)
			if got := string(body); got != "T=csrf-token&a=user%2F-%2Fstate%2Fcom.google%2Fread&i=tag%3Agoogle.com%2C2005%3Areader%2Fitem%2Fabc123&r=user%2F-%2Fstate%2Fcom.google%2Fkept-unread" {
				t.Fatalf("unexpected edit-tag body %q", got)
			}
			return responseWithBody(http.StatusOK, "OK"), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}

	if err := client.MarkEntryRead(context.Background(), "tag:google.com,2005:reader/item/abc123", true); err != nil {
		t.Fatalf("MarkEntryRead returned error: %v", err)
	}
	if err := client.MarkEntryRead(context.Background(), "tag:google.com,2005:reader/item/abc123", true); err != nil {
		t.Fatalf("MarkEntryRead returned error on cached token: %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("expected token endpoint to be called once, got %d", tokenCalls)
	}
}

func TestMarkAllReadUsesTokenAndStreamID(t *testing.T) {
	client := New("https://rss.example.com/api/greader.php", "alice", "secret")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return responseWithBody(http.StatusOK, "Auth=alice/token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/token":
			return responseWithBody(http.StatusOK, "csrf-token"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/mark-all-as-read":
			if req.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", req.Method)
			}
			body, _ := io.ReadAll(req.Body)
			if got := string(body); got != "T=csrf-token&s=feed%2Fhttp%3A%2F%2Fexample.com%2Ffeed.xml" {
				t.Fatalf("unexpected mark-all-as-read body %q", got)
			}
			return responseWithBody(http.StatusOK, "OK"), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}

	if err := client.MarkAllRead(context.Background(), "feed/http://example.com/feed.xml"); err != nil {
		t.Fatalf("MarkAllRead returned error: %v", err)
	}
}

func TestClientLoginRequiresConfig(t *testing.T) {
	client := New("", "", "")

	err := client.ensureAuth(context.Background())
	if err == nil {
		t.Fatal("expected configuration error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func responseWithBody(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func responseWithJSON(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}
