package feed

import (
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
)

func TestSetMaxFeedBodyBytesFallsBackToDefault(t *testing.T) {
	orig := maxFeedBodyBytes
	t.Cleanup(func() { maxFeedBodyBytes = orig })

	SetMaxFeedBodyBytes(0)

	if maxFeedBodyBytes != defaultMaxFeedBodyBytes {
		t.Fatalf("expected default limit, got %d", maxFeedBodyBytes)
	}
}

func TestFeedTooLargeFriendlyMessage(t *testing.T) {
	orig := maxFeedBodyBytes
	t.Cleanup(func() { maxFeedBodyBytes = orig })
	SetMaxFeedBodyBytes(defaultMaxFeedBodyBytes)

	r := &FetchResult{Kind: KindFeedTooLarge}

	if !strings.Contains(r.FriendlyMessage(), "too large") {
		t.Fatalf("expected too-large message, got %q", r.FriendlyMessage())
	}
	if !r.HasDetails() {
		t.Fatal("expected too-large result to report details")
	}
}

func TestParseItemFallbackGUIDIsStableWithoutGUIDOrLink(t *testing.T) {
	published := time.Unix(1710000000, 0)
	item := parseItem(&gofeed.Item{
		Title:           "Same Title",
		Description:     "Same Description",
		PublishedParsed: &published,
	})
	item2 := parseItem(&gofeed.Item{
		Title:           "Same Title",
		Description:     "Same Description",
		PublishedParsed: &published,
	})

	if item.GUID != item2.GUID {
		t.Fatalf("expected stable fallback GUID, got %q and %q", item.GUID, item2.GUID)
	}
	if !strings.HasPrefix(item.GUID, "fallback:") {
		t.Fatalf("expected fallback prefix, got %q", item.GUID)
	}
}

func TestParseItemFallbackGUIDDiffersForDifferentItems(t *testing.T) {
	first := parseItem(&gofeed.Item{Title: "One", Description: "Alpha"})
	second := parseItem(&gofeed.Item{Title: "Two", Description: "Beta"})

	if first.GUID == second.GUID {
		t.Fatalf("expected distinct fallback GUIDs, got %q", first.GUID)
	}
}
