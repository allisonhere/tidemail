package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/db"
)

var fixtureWidths = []int{24, 40, 80, 120}

func readMailFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "mail", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// compact removes all whitespace so assertions survive word wrap and hard
// wrap at narrow widths.
func compact(s string) string {
	return strings.Join(strings.Fields(s), "")
}

func isGroupRuleLine(line string) bool {
	s := strings.TrimSpace(ansi.Strip(line))
	return s != "" && (strings.Trim(s, "-") == "" || strings.Trim(s, "─") == "")
}

// groupSegments splits rendered output at separator rules.
func groupSegments(rendered string) []string {
	var segments []string
	var cur []string
	for _, line := range strings.Split(ansi.Strip(rendered), "\n") {
		if isGroupRuleLine(line) {
			segments = append(segments, compact(strings.Join(cur, " ")))
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	return append(segments, compact(strings.Join(cur, " ")))
}

func assertNoSentinels(t *testing.T, rendered string) {
	t.Helper()
	for _, r := range []rune{groupSeparatorRune, imageMarkerOpen, imageMarkerClose} {
		if strings.ContainsRune(rendered, r) {
			t.Fatalf("sentinel %U leaked into output: %q", r, ansi.Strip(rendered))
		}
	}
}

func assertWidth(t *testing.T, rendered string, width int) {
	t.Helper()
	for _, line := range strings.Split(rendered, "\n") {
		if w := ansi.StringWidth(line); w > width {
			t.Fatalf("line width %d exceeds %d: %q", w, width, ansi.Strip(line))
		}
	}
}

func TestNewsletterGroupsKeepCardsTogether(t *testing.T) {
	html := readMailFixture(t, "newsletter.html")
	cards := [][]string{
		{"[image: Autumn collection hero]", "The Autumn Collection", "Layers for cold mornings", "Shop autumn"},
		{"[image: Wool scarf]", "Wool Scarf", "Soft merino in five colours", "View scarf"},
		{"[image: Canvas tote]", "Canvas Tote", "Waxed canvas with leather", "View tote"},
		{"[image: Ceramic mug]", "Ceramic Mug", "Stoneware glazed in deep", "View mug"},
		{"[image: Shop the sale]"},
	}
	for _, plain := range []bool{true, false} {
		for _, width := range fixtureWidths {
			rendered := renderHTMLBody(html, width, CatppuccinMocha, plain)
			assertNoSentinels(t, rendered)
			assertWidth(t, rendered, width)
			segments := groupSegments(rendered)
			whole := compact(ansi.Strip(rendered))
			last := -1
			for _, card := range cards {
				seg := -1
				for i, s := range segments {
					if strings.Contains(s, compact(card[0])) {
						seg = i
						break
					}
				}
				if seg < 0 {
					t.Fatalf("plain=%v width=%d: %q missing from %q", plain, width, card[0], ansi.Strip(rendered))
				}
				if seg <= last {
					t.Fatalf("plain=%v width=%d: card %q shares or precedes a previous group", plain, width, card[0])
				}
				last = seg
				for _, part := range card {
					if !strings.Contains(segments[seg], compact(part)) {
						t.Fatalf("plain=%v width=%d: %q separated from %q:\n%s", plain, width, part, card[0], ansi.Strip(rendered))
					}
					if n := strings.Count(whole, compact(part)); n != 1 {
						t.Fatalf("plain=%v width=%d: %q appears %d times", plain, width, part, n)
					}
				}
			}
		}
	}
	links := extractActionableLinksFromHTML(html, "")
	for _, want := range []string{"https://example.com/autumn", "https://example.com/scarf", "https://example.com/tote", "https://example.com/mug", "https://example.com/sale"} {
		if !slices.Contains(links, want) {
			t.Fatalf("link %s not actionable: %v", want, links)
		}
	}
}

func TestNewsletterGroupsSkipQuotedMail(t *testing.T) {
	rendered := ansi.Strip(renderHTMLBody(readMailFixture(t, "forwarded_newsletter.html"), 80, CatppuccinMocha, true))
	if len(groupSegments(rendered)) != 1 {
		t.Fatalf("quoted newsletter should not gain separators:\n%s", rendered)
	}
	shirt, hat := strings.Index(rendered, "Linen Shirt"), strings.Index(rendered, "Straw Hat")
	if shirt < 0 || hat < 0 || shirt > hat {
		t.Fatalf("quoted cards lost or reordered:\n%s", rendered)
	}
}

func TestNewsletterGroupDetection(t *testing.T) {
	card := func(name string) string {
		return `<img src="cid:x" alt="` + name + `" width="200" height="200"><h3>` + name +
			`</h3><p>A short description of this product.</p><a class="btn" href="https://example.com/p">Buy</a>`
	}
	for _, tc := range []struct {
		name, html string
		want       []string // tag names of marked elements, in document order
	}{
		{"navigation", `<table><tr><td><a href="https://example.com/a">Shop</a> <a href="https://example.com/b">Blog</a></td></tr></table>`, nil},
		{"logoHeader", `<table><tr><td><img src="cid:l" alt="Store logo"></td><td><a href="https://example.com/v">View in browser</a></td></tr></table>`, nil},
		{"iconAndLink", `<table><tr><td><img src="cid:i" width="16" height="16"><a href="https://example.com/v">Share</a></td></tr></table>`, nil},
		{"singleCard", `<table><tr><td>` + card("Boots") + `</td></tr></table>`, []string{"td"}},
		{"twoCards", `<table><tr><td>` + card("Boots") + `</td><td>` + card("Coat") + `</td></tr></table>`, []string{"td", "td"}},
		{"imageBesideText", `<table><tr><td><img src="cid:m" alt="Mug" width="200" height="200"></td><td><h3>Mug</h3><p>Glazed stoneware that keeps coffee warm.</p></td></tr></table>`, []string{"tr"}},
		{"nestedWrapper", `<table><tr><td><table><tr><td>` + card("Boots") + `</td><td>` + card("Coat") + `</td></tr></table></td></tr></table>`, []string{"td", "td"}},
		{"imageOnlyCTA", `<table><tr><td><a href="https://example.com/sale"><img src="cid:s" alt="Sale" width="600" height="100"></a></td></tr></table>`, []string{"td"}},
		{"receipt", `<table><tr><td><h2>Order</h2><p>Thanks for your order today.</p><table><tr><td>Coffee</td><td>$12.00</td></tr><tr><td>Total</td><td>$12.00</td></tr></table></td></tr></table>`, nil},
		{"quoted", `<blockquote><table><tr><td>` + card("Boots") + `</td></tr></table></blockquote>`, nil},
		{"signature", `<div class="gmail_signature"><table><tr><td>` + card("Sam") + `</td></tr></table></div>`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tc.html))
			if err != nil {
				t.Fatal(err)
			}
			markNewsletterGroups(doc)
			var got []string
			doc.Find("[" + groupAttr + "]").Each(func(_ int, s *goquery.Selection) {
				got = append(got, goquery.NodeName(s))
			})
			if !slices.Equal(got, tc.want) {
				t.Fatalf("marked %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExpandGroupSeparators(t *testing.T) {
	sep := groupSeparator
	for _, tc := range []struct{ name, in, want string }{
		{"edgesDropped", sep + "\n\nA\n\n" + sep, "A"},
		{"adjacentCollapse", "A\n\n" + sep + "\n\n" + sep + "\n\nB", "A\n\n--------\n\nB"},
		{"indentKept", "A\n\n  " + sep + "\n\n  B", "A\n\n  --------\n\n  B"},
		{"inlineRemoved", "A " + sep + " B", "A  B"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := expandGroupSeparators(tc.in, 40, CatppuccinMocha, true); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMailCannotForgeGroupSeparator(t *testing.T) {
	html := `<p>Before</p><p>` + groupSeparator + `</p><p>After ` + groupSeparator + ` text</p>`
	for _, plain := range []bool{true, false} {
		rendered := renderHTMLBody(html, 40, CatppuccinMocha, plain)
		assertNoSentinels(t, rendered)
		if len(groupSegments(rendered)) != 1 {
			t.Fatalf("forged sentinel produced a separator: %q", ansi.Strip(rendered))
		}
	}
}

func TestReceiptFixtureHasNoGroupSeparators(t *testing.T) {
	html := readMailFixture(t, "receipt.html")
	for _, width := range fixtureWidths {
		rendered := ansi.Strip(renderHTMLBody(html, width, CatppuccinMocha, true))
		if len(groupSegments(rendered)) != 1 {
			t.Fatalf("width=%d: receipt gained separators:\n%s", width, rendered)
		}
		if !strings.Contains(rendered, "$29.70") || !strings.Contains(rendered, "Total") {
			t.Fatalf("width=%d: receipt total lost:\n%s", width, rendered)
		}
	}
}

// TestMailFixturesRenderBounded is the shared smoke test for every synthetic
// fixture: output exists, fits the pane, and carries no internal markers.
func TestMailFixturesRenderBounded(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "mail"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		html := readMailFixture(t, entry.Name())
		for _, plain := range []bool{true, false} {
			for _, width := range fixtureWidths {
				rendered := renderHTMLBody(html, width, CatppuccinMocha, plain)
				if strings.TrimSpace(ansi.Strip(rendered)) == "" {
					t.Fatalf("%s plain=%v width=%d: empty render", entry.Name(), plain, width)
				}
				assertNoSentinels(t, rendered)
				assertWidth(t, rendered, width)
			}
		}
	}
}

func TestNewsletterGroupsWithRasterImages(t *testing.T) {
	m, _ := graphicsModel(t)
	cell := func(name string) string {
		return `<td><img src="` + pngDataURI(t, 120, 80) + `" alt="` + name + `"><h3>` + name +
			`</h3><p>Made by hand in small batches.</p><a class="btn" href="https://example.com/p">Buy</a></td>`
	}
	html := `<table><tr>` + cell("Boots") + cell("Coat") + `</tr></table>`
	res := m.renderMessageForDisplay(db.Message{ID: 40, BodyHTML: html}, 70)
	if len(res.images) != 2 {
		t.Fatalf("expected 2 images, got %d (body=%q)", len(res.images), ansi.Strip(res.body))
	}
	assertNoSentinels(t, res.body)
	assertWidth(t, res.body, 70)
	segments := groupSegments(res.body)
	if len(segments) != 2 || !strings.Contains(segments[0], "Boots") || !strings.Contains(segments[1], "Coat") {
		t.Fatalf("cards not separated: %q", ansi.Strip(res.body))
	}
	for i, seg := range segments {
		if !strings.ContainsRune(seg, placeholderRune) {
			t.Fatalf("segment %d lost its image: %q", i, seg)
		}
	}
}
