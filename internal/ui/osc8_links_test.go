package ui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

const (
	osc8Open  = "\x1b]8;;"
	osc8Close = "\x1b]8;;\x1b\\"
)

// A hyperlink target is interpolated into a terminal escape sequence, so an
// untrusted URL carrying a control byte could close the sequence early and
// inject arbitrary escapes. Link targets are harvested straight from href
// attributes, which never pass through stripEmailInvisibles.
func TestSafeOSC8URIRejectsInjection(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		want bool
	}{
		{"plain https", "https://example.com/path?a=1", true},
		{"plain http", "http://example.com", true},
		{"escape terminator", "https://example.com/\x1b\\evil", false},
		{"bel terminator", "https://example.com/\x07evil", false},
		{"osc 52 payload", "https://x.com/\x1b]52;c;ZXZpbA==\x07", false},
		{"newline", "https://example.com/\nevil", false},
		{"carriage return", "https://example.com/\revil", false},
		{"del", "https://example.com/\x7f", false},
		{"c1 control", "https://example.com/\u0090", false},
		{"javascript scheme", "javascript:alert(1)", false},
		{"file scheme", "file:///etc/passwd", false},
		{"mailto", "mailto:a@example.com", false},
		{"empty", "", false},
		{"overlong", "https://example.com/" + strings.Repeat("a", maxOSC8URILen), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeOSC8URI(tc.uri); got != tc.want {
				t.Fatalf("safeOSC8URI(%q) = %v, want %v", tc.uri, got, tc.want)
			}
		})
	}
}

func TestOSC8LinkFallsBackToPlainLabel(t *testing.T) {
	// plainUI must never emit escapes.
	if got := osc8Link("https://example.com", "label", true); got != "label" {
		t.Fatalf("plainUI emitted a hyperlink: %q", got)
	}
	// An unsafe URI degrades to the bare label rather than a broken sequence.
	if got := osc8Link("https://example.com/\x1b\\x", "label", false); got != "label" {
		t.Fatalf("unsafe URI was linkified: %q", got)
	}
	got := osc8Link("https://example.com", "label", false)
	if !strings.HasPrefix(got, osc8Open) || !strings.HasSuffix(got, osc8Close) {
		t.Fatalf("well-formed URI was not linkified: %q", got)
	}
}

// The whole point of OSC 8: it must not consume display columns.
func TestOSC8LinkIsZeroWidth(t *testing.T) {
	label := "click here"
	linked := osc8Link("https://example.com/some/long/path", label, false)
	if w := lipgloss.Width(linked); w != lipgloss.Width(label) {
		t.Fatalf("hyperlink changed display width: %d vs %d", w, lipgloss.Width(label))
	}
	if got := ansi.Strip(linked); got != label {
		t.Fatalf("ansi.Strip(%q) = %q, want %q", linked, got, label)
	}
}

func TestBodyURLsBecomeHyperlinks(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	got := formatArticleBody("See https://example.com/docs for details.", 80, CatppuccinMocha, false)
	if !strings.Contains(got, osc8Open+"https://example.com/docs") {
		t.Fatalf("body URL was not hyperlinked: %q", got)
	}
	if !strings.Contains(ansi.Strip(got), "https://example.com/docs") {
		t.Fatalf("visible URL text was lost: %q", ansi.Strip(got))
	}
}

// plainUI is the dumb-terminal escape hatch; it must stay escape-free.
func TestPlainUIBodyEmitsNoHyperlinks(t *testing.T) {
	got := formatArticleBody("See https://example.com/docs for details.", 80, CatppuccinMocha, true)
	if strings.Contains(got, "\x1b]8") {
		t.Fatalf("plainUI body emitted OSC 8: %q", got)
	}
}

func TestCleanDetectedURLUnwrapsGenericRedirectTargets(t *testing.T) {
	cases := map[string]string{
		"url":      "https://example.com/article?utm_source=mail",
		"u":        "https://example.com/article",
		"target":   "https://example.com/article",
		"redirect": "https://example.com/article",
	}
	for param, want := range cases {
		raw := "https://tracker.example/click?" + param + "=" + url.QueryEscape(want)
		if got := cleanDetectedURL(raw); got != want {
			t.Fatalf("%s redirect cleaned to %q, want %q", param, got, want)
		}
	}
}

func TestCleanDetectedURLDropsRedditNotificationManagementLinks(t *testing.T) {
	raw := "https://click.redditmail.com/CL0/https:%2F%2Fwww.reddit.com%2Fmail%2Funsubscribe%2Fabc%3Ftoken%3D1/1/token"
	if got := cleanDetectedURL(raw); got != "" {
		t.Fatalf("expected Reddit management link dropped, got %q", got)
	}
}

func TestExtractActionableLinksStopsBeforeMarkdownLabel(t *testing.T) {
	got := extractActionableLinks("https://www.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/[Read/", "")
	want := "https://www.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("expected URL to stop before Markdown label %q, got %#v", want, got)
	}
}

func TestCleanDetectedURLDropsStrayRedditCTASegment(t *testing.T) {
	raw := "https://www.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/V"
	want := "https://www.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/"
	if got := cleanDetectedURL(raw); got != want {
		t.Fatalf("expected stray Reddit CTA segment removed, got %q want %q", got, want)
	}
}

func TestCleanDetectedURLKeepsLikelyRedditCommentPermalink(t *testing.T) {
	raw := "https://old.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/n2abcde/?context=3"
	want := "https://www.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/n2abcde/"
	if got := cleanDetectedURL(raw); got != want {
		t.Fatalf("expected Reddit comment permalink preserved, got %q want %q", got, want)
	}
}

func TestContentLinkListIsHyperlinked(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.height = 30
	m.contentLinks = []string{"https://example.com/first", "https://example.com/second"}

	got := m.renderContentLinks(60)
	for _, want := range m.contentLinks {
		if !strings.Contains(got, osc8Open+want) {
			t.Fatalf("link list entry %q was not hyperlinked: %q", want, got)
		}
	}
}

// The list truncates long URLs for display; the click target must remain the
// full URL rather than the truncated text.
func TestContentLinkListTargetsFullURLWhenTruncated(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	long := "https://example.com/" + strings.Repeat("segment/", 12)
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.height = 30
	m.contentLinks = []string{long}

	got := m.renderContentLinks(40)
	if !strings.Contains(got, osc8Open+long) {
		t.Fatalf("truncated entry lost its full-URL target: %q", got)
	}
	if strings.Contains(ansi.Strip(got), long) {
		t.Fatal("expected the visible text to be truncated")
	}
}

// filter_links previously applied only to the plain-text fallback branch, so
// URLs visible in HTML bodies were never filtered.
//
// Note this covers the cases where a URL is actually visible in the rendered
// body: a bare URL in text, and an anchor whose label is itself the URL. An
// <a href> with a text label never renders its target in the body at all —
// html-to-markdown/glamour drop it upstream — so there is nothing to filter
// there; that target surfaces only in the actionable LINKS list.
func TestFilterLinksAppliesToHTMLBodies(t *testing.T) {
	cases := map[string]string{
		"bare URL in text":    `<p>Visit https://tracker.example.com/abc now.</p>`,
		"anchor labelled URL": `<p><a href="https://tracker.example.com/abc">https://tracker.example.com/abc</a></p>`,
	}
	for name, html := range cases {
		t.Run(name, func(t *testing.T) {
			msg := db.Message{ID: 1, BodyHTML: html}

			on := NewModel(nil, config.DefaultConfig(), "dev", false)
			on.width, on.height = 100, 30
			on.cfg.Display.FilterLinks = true
			if got := ansi.Strip(on.renderMessageBody(msg, 80)); strings.Contains(got, "tracker.example.com") {
				t.Fatalf("filter_links on did not strip the URL: %q", got)
			}

			off := NewModel(nil, config.DefaultConfig(), "dev", false)
			off.width, off.height = 100, 30
			off.cfg.Display.FilterLinks = false
			if got := ansi.Strip(off.renderMessageBody(msg, 80)); !strings.Contains(got, "tracker.example.com") {
				t.Fatalf("filter_links off should keep the URL visible: %q", got)
			}
		})
	}
}

// Only the target is removed; the surrounding wording must survive.
func TestFilterLinksKeepsHTMLLinkText(t *testing.T) {
	html := `<p>Read the <a href="https://tracker.example.com/abc">quarterly report</a> now.</p>`
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width, m.height = 100, 30
	m.cfg.Display.FilterLinks = true

	got := ansi.Strip(m.renderMessageBody(db.Message{ID: 1, BodyHTML: html}, 80))
	for _, want := range []string{"Read the", "quarterly report", "now."} {
		if !strings.Contains(got, want) {
			t.Fatalf("filter_links removed body wording %q: %q", want, got)
		}
	}
}

func TestFilterLinksStillAppliesToPlainTextBodies(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.height = 30
	m.cfg.Display.FilterLinks = true
	msg := db.Message{ID: 1, BodyText: "Visit https://tracker.example.com/abc now."}

	got := ansi.Strip(m.renderMessageBody(msg, 80))
	if strings.Contains(got, "tracker.example.com") {
		t.Fatalf("filter_links did not strip the plain-text URL: %q", got)
	}
}

// The Reddit digest's [Read post] CTA must keep its OSC 8 wrapper *outside* the
// SGR styling. actionStyle underlines, which makes lipgloss style the label per
// rune; wrapping the hyperlink first splits its ESC from the "]8;;<url>" payload,
// so the terminal prints the tracking URL as visible text. That inflates the
// line from 11 columns to ~100, which then wraps and overflows the content pane.
func TestRedditDigestPostHyperlinksSurviveStyling(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	got, ok := renderRedditDigestHTML(redditDigestFixture(), 44, CatppuccinMocha, false)
	if !ok {
		t.Fatal("expected Reddit digest fixture to render")
	}

	const post = "https://www.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/"
	// Both the title and the call to action link to the post, so Enter opens it
	// from either line.
	for _, label := range []string{"Free ai in Omarchy?", "[Read post]"} {
		var found string
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(ansi.Strip(line), label) {
				found = line
				break
			}
		}
		if found == "" {
			t.Fatalf("no %q line in rendered digest: %q", label, ansi.Strip(got))
		}
		if w := ansi.StringWidth(found); w != len(label) {
			t.Fatalf("%q occupies %d columns, want %d — the hyperlink leaked into visible text: %q", label, w, len(label), found)
		}
	}

	stripped := ansi.Strip(got)
	if strings.Contains(stripped, "]8;;") || strings.Contains(stripped, post) {
		t.Fatalf("OSC 8 sequence rendered as literal text: %q", stripped)
	}
	// Two intact pairs: one around the title, one around [Read post].
	if strings.Count(got, osc8Open+post) != 2 || strings.Count(got, osc8Close) != 2 {
		t.Fatalf("expected exactly two intact OSC 8 hyperlinks to %q, got %q", post, got)
	}
}

// ansi.Strip removes the whole escape, so reading a hyperlink back out of a
// rendered line is the only way to resolve it from the focus line.
func TestFirstOSC8TargetParsesHyperlinks(t *testing.T) {
	const uri = "https://example.com/a"
	cases := map[string]struct {
		line string
		want string
	}{
		"ST terminator":     {"\x1b]8;;" + uri + "\x1b\\label\x1b]8;;\x1b\\", uri},
		"BEL terminator":    {"\x1b]8;;" + uri + "\x07label\x1b]8;;\x07", uri},
		"with params":       {"\x1b]8;id=42;" + uri + "\x1b\\label\x1b]8;;\x1b\\", uri},
		"closing only":      {"\x1b]8;;\x1b\\", ""},
		"no escape":         {"just text", ""},
		"literal lookalike": {"see ]8;; in text", ""},
		"unterminated":      {"\x1b]8;;" + uri, ""},
		"first of two wins": {"\x1b]8;;" + uri + "\x1b\\a\x1b]8;;\x1b\\ \x1b]8;;https://example.com/b\x1b\\b\x1b]8;;\x1b\\", uri},
		"styled label":      {"\x1b]8;;" + uri + "\x1b\\\x1b[1mlabel\x1b[0m\x1b]8;;\x1b\\", uri},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := firstOSC8Target(tc.line); got != tc.want {
				t.Fatalf("firstOSC8Target(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

// A title wrapped across several lines must link on every one of them, or the
// focus line lands on a row with no link.
func TestRedditDigestWrappedTitleLinksEveryLine(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	const width = 12
	got, ok := renderRedditDigestHTML(redditDigestFixture(), width, CatppuccinMocha, false)
	if !ok {
		t.Fatal("expected Reddit digest fixture to render")
	}

	const post = "https://www.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/"
	titleLines := 0
	for _, line := range strings.Split(got, "\n") {
		plain := ansi.Strip(line)
		if w := ansi.StringWidth(line); w > width {
			t.Fatalf("line %q occupies %d columns, want <= %d", plain, w, width)
		}
		if !strings.Contains(plain, "Free ai") && !strings.Contains(plain, "Omarchy?") {
			continue
		}
		titleLines++
		if n := strings.Count(line, osc8Open+post); n != 1 {
			t.Fatalf("title line %q carries %d hyperlinks, want 1", plain, n)
		}
	}
	if titleLines < 2 {
		t.Fatalf("expected the title to wrap across several lines at width %d, got %d", width, titleLines)
	}
}

// The vt52 theme renders without escapes, so no hyperlink may be emitted there.
func TestRedditDigestEmitsNoEscapesForPlainUI(t *testing.T) {
	got, ok := renderRedditDigestHTML(redditDigestFixture(), 44, CatppuccinMocha, true)
	if !ok {
		t.Fatal("expected Reddit digest fixture to render")
	}
	if strings.Contains(got, "\x1b]8") {
		t.Fatalf("plainUI output must contain no OSC 8 sequence, got %q", got)
	}
}
