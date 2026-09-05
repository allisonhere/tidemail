package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestRenderMessageContentAddsBlankLineAfterMessageID(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.contentShowHeaders = true

	view := ansi.Strip(m.renderMessageContent(db.Message{
		Subject:   "Hello",
		From:      "alice@example.com",
		To:        "bob@example.com",
		Date:      time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		MessageID: "<message-id@example.com>",
		BodyText:  "Body starts here.",
	}))

	messageIDLine := -1
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Message-ID:") {
			messageIDLine = i
			break
		}
	}
	if messageIDLine < 0 {
		t.Fatalf("expected Message-ID header in rendered content, got %q", view)
	}
	if messageIDLine+1 >= len(lines) || strings.TrimSpace(lines[messageIDLine+1]) != "" {
		t.Fatalf("expected blank line after Message-ID, got next line %q in %q", lines[messageIDLine+1], view)
	}
}

func TestRenderMessageTitleSpansPaneWhenReadingWidthIsCapped(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.ReadingWidth = 32
	m := NewModel(nil, cfg, "dev", false)
	m.width = 100

	view := m.renderMessageContent(db.Message{
		Subject:  "A message with a title bar",
		Date:     time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		BodyText: "Body starts here.",
	})
	firstLine := strings.Split(view, "\n")[0]

	if got, want := ansi.StringWidth(firstLine), m.contentPaneContentWidth(); got != want {
		t.Fatalf("expected title line to span content pane width %d, got %d in %q", want, got, ansi.Strip(firstLine))
	}
}

func TestContentTitlesRemoveEmojiBeforePadding(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.height = 30
	for _, subject := range []string{"Make it a BOGO weekend ✌\u00a0‼", "Sale ✅", "LEVEL UP 🥇"} {
		msg := db.Message{Subject: subject, BodyText: "Message body"}
		thread := messageThread{Representative: msg, Messages: []db.Message{msg}, Count: 1}
		for name, view := range map[string]string{
			"message": m.renderMessageContent(msg),
			"thread":  m.renderThreadContent(thread),
		} {
			title := strings.Split(view, "\n")[0]
			want := messageListDisplayText(subject)
			if got := strings.TrimSpace(ansi.Strip(title)); got != want {
				t.Errorf("%s title = %q, want %q", name, got, want)
			}
			if got := ansi.StringWidth(title); got != m.contentPaneContentWidth() {
				t.Errorf("%s title width = %d, want %d", name, got, m.contentPaneContentWidth())
			}
		}
	}
}

// Body content must not reflow as focus moves between panes.
func TestContentRenderIsFocusIndependent(t *testing.T) {
	msg := db.Message{
		ID:       1,
		Subject:  "LEVEL UP 🥇 ",
		From:     "Friendly 🙌 Sender <sender@example.com>",
		BodyText: "Save money 💰 and move fast 💨. Victory ✌️",
		Date:     time.Date(2026, 7, 19, 19, 0, 28, 0, time.Local),
	}

	render := func(focus pane) string {
		m := NewModel(nil, config.DefaultConfig(), "dev", false)
		m.width = 100
		m.height = 30
		m.focused = focus
		return m.renderMessageContent(msg)
	}

	unfocused, focused := render(paneMessages), render(paneContent)
	if unfocused != focused {
		t.Fatalf("content render differs by focus:\nunfocused: %q\nfocused:   %q", unfocused, focused)
	}
	for _, emoji := range []string{"🙌", "💰", "💨", "✌️"} {
		if !strings.Contains(focused, emoji) {
			t.Fatalf("content dropped emoji %q: %q", emoji, ansi.Strip(focused))
		}
	}
}

func TestHTMLContentKeepsEmojiRegardlessOfFocus(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.height = 30
	m.focused = paneMessages

	view := ansi.Strip(m.renderMessageContent(db.Message{
		Subject:  "Sale ✅",
		BodyHTML: "<p>Celebrate 🙌 and save 💰</p>",
	}))
	for _, emoji := range []string{"🙌", "💰"} {
		if !strings.Contains(view, emoji) {
			t.Fatalf("HTML content dropped emoji %q: %q", emoji, view)
		}
	}
	if !strings.Contains(view, "Celebrate") || !strings.Contains(view, "save") {
		t.Fatalf("HTML content lost ordinary content: %q", view)
	}
}

// Emoji stripping in the message list is intentional and stays: the list
// repaints on every cursor move, where color-emoji width quirks cause tearing.
func TestMessageListStillStripsEmoji(t *testing.T) {
	got := messageListDisplayText("LEVEL UP 🥇 now")
	if strings.ContainsAny(got, "🥇") {
		t.Fatalf("message list retained emoji: %q", got)
	}
	if !strings.Contains(got, "LEVEL UP") || !strings.Contains(got, "now") {
		t.Fatalf("message list lost ordinary content: %q", got)
	}
}

func TestRenderHTMLBodyPreservesTablesAsReadableRows(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<table><tr><th>Name</th><th>Status</th></tr><tr><td>Ada</td><td>Done</td></tr></table>`, 80, CatppuccinMocha, true))

	if !strings.Contains(got, "Name") || !strings.Contains(got, "Status") || !strings.Contains(got, "Ada") || !strings.Contains(got, "Done") {
		t.Fatalf("expected table cell text in rendered body, got %q", got)
	}
	if !strings.Contains(got, "Name | Status") || !strings.Contains(got, "Ada  | Done") {
		t.Fatalf("expected table to render as aligned readable rows, got %q", got)
	}
}

func TestRenderHTMLBodyDoesNotDuplicateNestedLayoutTables(t *testing.T) {
	html := `<table><tr><td><table><tr><td>Feature story</td></tr></table></td></tr></table>`

	got := ansi.Strip(renderHTMLBody(html, 80, CatppuccinMocha, true))

	if count := strings.Count(got, "Feature story"); count != 1 {
		t.Fatalf("expected nested layout table content once, got %d occurrences in %q", count, got)
	}
}

func TestRenderHTMLBodyDoesNotFormatSingleCellLayoutTablesAsDataTables(t *testing.T) {
	html := `<table><tr><td><p>Feature story</p><a href="https://example.com/story"><img alt="Story photo" src="https://cdn.example.com/story.png"></a></td></tr></table>`

	got := ansi.Strip(renderHTMLBody(html, 80, CatppuccinMocha, true))

	if !strings.Contains(got, "Feature story") || !strings.Contains(got, "[image: Story photo]") {
		t.Fatalf("expected layout table content to render normally, got %q", got)
	}
	if strings.Contains(got, "```") || strings.Contains(got, " |") {
		t.Fatalf("expected single-cell layout table not to render as a data table, got %q", got)
	}
}

func TestRenderHTMLBodySeparatesLayoutTableCells(t *testing.T) {
	html := `<a href="https://example.com/post"><table><tr><td><table><tr><td><p>Story text.</p></td><td><p>Author Name</p></td></tr></table></td></tr></table></a><a href="https://example.com/post"><table><tr><td><p>61</p></td><td><p>57</p></td></tr></table></a><a href="https://example.com/post"><table><tr><td><p>People replied</p></td><td><a href="https://example.com/post" style="display:block;background:#1B8751;padding:8px">View post</a></td></tr></table></a>`

	got := ansi.Strip(renderHTMLBody(html, 80, CatppuccinMocha, true))

	for _, bad := range []string{"text.Author", "Name61", "6157", "replied[View post]"} {
		if strings.Contains(got, bad) {
			t.Fatalf("expected layout table cells separated, found %q in %q", bad, got)
		}
	}
	for _, want := range []string{"Story text.", "Author Name", "61", "57", "People replied", "View post"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in rendered body, got %q", want, got)
		}
	}
}

func TestRenderHTMLBodyKeepsLooseListItemContinuation(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<ul><li><p>First line.</p><p>Second line.</p></li></ul>`, 80, CatppuccinMocha, true))

	if !strings.Contains(got, "First line.") || !strings.Contains(got, "Second line.") {
		t.Fatalf("expected loose list item continuation text, got %q", got)
	}
}

func TestRenderHTMLBodyStripsTrackingInvisibles(t *testing.T) {
	hidden := strings.Repeat("\u034f \u00ad \u200b ", 80)
	got := renderHTMLBody(`<p>`+hidden+`Venmo is useful.</p>`, 42, CatppuccinMocha, true)

	for _, bad := range []string{"\u034f", "\u00ad", "\u200b"} {
		if strings.Contains(got, bad) {
			t.Fatalf("expected rendered body to strip hidden tracking rune %q from %q", bad, got)
		}
	}
	lines := strings.Split(ansi.Strip(got), "\n")
	if len(lines) != 1 || strings.TrimSpace(lines[0]) != "Venmo is useful." {
		t.Fatalf("expected only visible body text, got %q", got)
	}
}

func TestFormatArticleBodyStripsTrackingInvisibles(t *testing.T) {
	hidden := strings.Repeat("\u034f \u00ad \u200b ", 80)
	got := formatArticleBody(hidden+"Visible text.", 42, CatppuccinMocha, true)

	for _, bad := range []string{"\u034f", "\u00ad", "\u200b"} {
		if strings.Contains(got, bad) {
			t.Fatalf("expected formatted body to strip hidden tracking rune %q from %q", bad, got)
		}
	}
	if strings.TrimSpace(ansi.Strip(got)) != "Visible text." {
		t.Fatalf("expected only visible body text, got %q", got)
	}
}

func TestHTMLOnlyMessagePopulatesActionableLinks(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.ActionableLinks = true
	m := NewModel(nil, cfg, "dev", false)
	m.width = 100

	m.setViewportMessage(db.Message{
		ID:       1,
		Subject:  "HTML link",
		Date:     time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		BodyHTML: `<p>Read <a href="https://example.com/article">the article</a>.</p>`,
	})

	if len(m.contentLinks) != 1 || m.contentLinks[0] != "https://example.com/article" {
		t.Fatalf("expected HTML link in actionable links, got %#v", m.contentLinks)
	}
}

func TestRenderHTMLBodyShowsSafeImagePlaceholders(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<p>Chart below</p><img src="https://cdn.example.com/chart.png" alt="Quarterly chart">`, 80, CatppuccinMocha, true))

	if !strings.Contains(got, "[image: Quarterly chart]") {
		t.Fatalf("expected safe image placeholder with alt text, got %q", got)
	}
	if strings.Contains(got, "https://cdn.example.com/chart.png") {
		t.Fatalf("expected image URL hidden from body text, got %q", got)
	}
}

func TestRenderHTMLBodyStylesImagePlaceholders(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	got := renderHTMLBody(`<p>Chart below</p><img src="https://cdn.example.com/chart.png" alt="Quarterly chart">`, 80, CatppuccinMocha, false)

	if !strings.Contains(ansi.Strip(got), "[image: Quarterly chart]") {
		t.Fatalf("expected visible image placeholder, got %q", ansi.Strip(got))
	}
	if got == ansi.Strip(got) {
		t.Fatalf("expected styled image placeholder to emit ANSI, got %q", got)
	}
}

func TestRenderHTMLBodyLeavesImagePlaceholdersPlainInPlainUI(t *testing.T) {
	got := renderHTMLBody(`<img src="https://cdn.example.com/chart.png" alt="Quarterly chart">`, 80, CatppuccinMocha, true)

	if got != ansi.Strip(got) {
		t.Fatalf("expected plain UI image placeholder without ANSI, got %q", got)
	}
	if !strings.Contains(got, "[image: Quarterly chart]") {
		t.Fatalf("expected visible image placeholder, got %q", got)
	}
}

func TestRenderHTMLBodyDropsDecorativeInlineImages(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<p>Hello<img src="https://cdn.example.com/pic.png" alt="Profile photo">World</p>`, 80, CatppuccinMocha, true))

	if strings.Contains(got, "[image: Profile photo]") {
		t.Fatalf("expected decorative profile image hidden from body text, got %q", got)
	}
	if !strings.Contains(got, "Hello World") {
		t.Fatalf("expected surrounding text to stay readable, got %q", got)
	}
}

func TestRenderHTMLBodyDropsEmptyVisualLinks(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<a href="https://example.com/red-dot" style="display:block;width:8px;height:8px"></a><p>Body text</p>`, 80, CatppuccinMocha, true))

	if strings.Contains(got, "https://example.com/red-dot") || strings.Contains(got, "[]") {
		t.Fatalf("expected empty visual link hidden from body text, got %q", got)
	}
	if !strings.Contains(got, "Body text") {
		t.Fatalf("expected body text to remain, got %q", got)
	}
}

func TestRenderHTMLBodyFormatsButtonLinks(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<a href="https://example.com/confirm" style="display:inline-block;background:#444;color:#fff;padding:12px">Confirm account</a>`, 80, CatppuccinMocha, true))

	if !strings.Contains(got, "[Confirm account]") {
		t.Fatalf("expected button-like link to render as readable action text, got %q", got)
	}
	if strings.Contains(got, "https://example.com/confirm") {
		t.Fatalf("expected button link URL hidden from body text, got %q", got)
	}
}

func TestRenderHTMLBodyDoesNotInlineLongNamedLinkDestinations(t *testing.T) {
	longURL := "https://c.gle/" + strings.Repeat("AOExmq1KwLPZWDtpZUmEruS3WaeyYD0bG7DoKy", 4)
	got := ansi.Strip(renderHTMLBody(`<p>You may <a href="`+longURL+`">unsubscribe</a> or <a href="`+longURL+`">add partners</a> who should receive messages.</p>`, 42, CatppuccinMocha, true))

	if !strings.Contains(got, "unsubscribe") || !strings.Contains(got, "add partners") {
		t.Fatalf("expected named link text to remain readable, got %q", got)
	}
	if strings.Contains(got, "AOExmq1KwLPZWD") || strings.Contains(got, longURL) {
		t.Fatalf("expected long href hidden from body text, got %q", got)
	}
}

func TestRenderHTMLBodyDoesNotInlineLayoutTableLinkDestinations(t *testing.T) {
	longURL := "https://nextdoor.com/p/" + strings.Repeat("token", 20)
	got := ansi.Strip(renderHTMLBody(`<a href="`+longURL+`"><table><tr><td>View post</td></tr></table></a>`, 42, CatppuccinMocha, true))

	if !strings.Contains(got, "View post") {
		t.Fatalf("expected layout-table link text to remain readable, got %q", got)
	}
	if strings.Contains(got, "nextdoor.com") || strings.Contains(got, "tokentoken") {
		t.Fatalf("expected layout-table href hidden from body text, got %q", got)
	}
}

func TestHTMLNamedLinkStillPopulatesActionableLinks(t *testing.T) {
	longURL := "https://c.gle/" + strings.Repeat("AOExmq1KwLPZWDtpZUmEruS3WaeyYD0bG7DoKy", 4)
	cfg := config.DefaultConfig()
	cfg.Display.ActionableLinks = true
	m := NewModel(nil, cfg, "dev", false)
	m.width = 80

	m.setViewportMessage(db.Message{
		ID:       1,
		Subject:  "HTML named link",
		Date:     time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		BodyHTML: `<p>You may <a href="` + longURL + `">add partners</a>.</p>`,
	})

	if len(m.contentLinks) != 1 || m.contentLinks[0] != longURL {
		t.Fatalf("expected long named href in actionable links, got %#v", m.contentLinks)
	}
}

func TestMessageRenderResultKeepsBodyAndLinksTogether(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.ActionableLinks = true
	m := NewModel(nil, cfg, "dev", false)
	m.width = 90

	msg := db.Message{
		ID: 1,
		BodyHTML: `<table role="presentation"><tr><td><h2>Weekly report</h2>` +
			`<p>Everything important.</p>` +
			`<a href="https://track.example/click?url=https%3A%2F%2Fexample.com%2Freport%3Futm_source%3Dmail">View report</a>` +
			`</td></tr></table>`,
	}
	result := m.renderMessageForDisplay(msg, 60)
	body := ansi.Strip(result.body)

	if !strings.Contains(body, "Weekly report") || !strings.Contains(body, "Everything important.") || !strings.Contains(body, "View report") {
		t.Fatalf("expected generic noisy newsletter body, got %q", body)
	}
	if len(result.links) != 1 || result.links[0] != "https://example.com/report?utm_source=mail" {
		t.Fatalf("expected cleaned redirect target in render result links, got %#v", result.links)
	}

	m.setViewportMessage(msg)
	if len(m.contentLinks) != 1 || m.contentLinks[0] != result.links[0] {
		t.Fatalf("expected synced content links to use render result links, got %#v want %#v", m.contentLinks, result.links)
	}
}

func TestNoisyHTMLArticleExtractorCleansGenericTemplate(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	noisyPadding := strings.Repeat(`<table role="presentation"><tr><td>&nbsp;</td></tr></table>`, 90)
	msg := db.Message{BodyHTML: `<html><body>` +
		`<div class="preheader">Hidden preview text</div>` +
		noisyPadding +
		`<h2>Product update</h2>` +
		`<p>The release is ready for review and includes the changes people asked for.</p>` +
		`<p>You can test it today and reply with anything that feels off.</p>` +
		`<a class="primary-cta" href="https://tracker.example/click?target=https%3A%2F%2Fexample.com%2Frelease">Read now</a>` +
		`<p>You are receiving this email because you subscribed.</p>` +
		`</body></html>`}

	result := m.renderMessageForDisplay(msg, 52)
	body := ansi.Strip(result.body)
	for _, want := range []string{"Product update", "The release is ready", "You can test it today", "[Read now]"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected noisy template body to contain %q, got %q", want, body)
		}
	}
	for _, bad := range []string{"Hidden preview text", "receiving this email", "tracker.example"} {
		if strings.Contains(body, bad) {
			t.Fatalf("expected noisy template body to omit %q, got %q", bad, body)
		}
	}
	if len(result.links) != 1 || result.links[0] != "https://example.com/release" {
		t.Fatalf("expected cleaned CTA link, got %#v", result.links)
	}
}

func TestRenderHTMLBodyPreservesPreformattedSpacing(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<pre>alpha
  beta
    gamma</pre>`, 80, CatppuccinMocha, true))

	if !strings.Contains(got, "  beta") || !strings.Contains(got, "    gamma") {
		t.Fatalf("expected preformatted indentation preserved, got %q", got)
	}
}

func TestRenderHTMLBodyRendersEmailQuoteBlocks(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<div>New reply</div><div class="gmail_quote"><blockquote>Earlier message</blockquote></div>`, 80, CatppuccinMocha, true))

	if !strings.Contains(got, "New reply") || !strings.Contains(got, "| Earlier message") {
		t.Fatalf("expected email quote rendered as quote lines, got %q", got)
	}
}

func TestRenderHTMLBodyNormalizesProviderQuoteContainers(t *testing.T) {
	for _, marker := range []string{"yahoo_quoted", "protonmail_quote", "divRplyFwdMsg", "appendonsend"} {
		t.Run(marker, func(t *testing.T) {
			got := ansi.Strip(renderHTMLBody(`<div>New reply</div><div class="`+marker+`">Earlier message</div>`, 80, CatppuccinMocha, true))
			if !strings.Contains(got, "New reply") || !strings.Contains(got, "| Earlier message") {
				t.Fatalf("expected %s container rendered as a quote, got %q", marker, got)
			}
		})
	}
}

func TestRenderHTMLBodyRemovesHiddenContentAndTrackingPixels(t *testing.T) {
	html := `<p>Visible introduction.</p>` +
		`<p style=" DISPLAY : NONE !important ">display secret</p>` +
		`<p style="visibility: collapse">visibility secret</p>` +
		`<p style="opacity: 0.0">opacity secret</p>` +
		`<p style="font-size: 0rem">font secret</p>` +
		`<p aria-hidden=" TRUE ">aria secret</p>` +
		`<img width="1" height="1" src="https://tracker.example/open.gif" alt="tracking pixel">` +
		`<img src="https://cdn.example/chart.png" alt="Quarterly chart">`

	got := ansi.Strip(renderHTMLBody(html, 80, CatppuccinMocha, true))
	for _, hidden := range []string{"display secret", "visibility secret", "opacity secret", "font secret", "aria secret", "tracking pixel", "tracker.example"} {
		if strings.Contains(got, hidden) {
			t.Fatalf("expected hidden or tracking content %q removed from %q", hidden, got)
		}
	}
	if !strings.Contains(got, "Visible introduction.") || !strings.Contains(got, "[image: Quarterly chart]") {
		t.Fatalf("expected visible content and useful image alt text, got %q", got)
	}
}

func TestRenderHTMLBodyKeepsImageDescriptionInsideDataTable(t *testing.T) {
	html := `<table><tr><th>Metric</th><th>Chart</th></tr><tr><td>Revenue</td><td><img src="https://cdn.example/chart.png" alt="Revenue by quarter"></td></tr></table>`
	got := ansi.Strip(renderHTMLBody(html, 56, CatppuccinMocha, true))

	if !strings.Contains(got, "[image: Revenue by quarter]") || strings.Contains(got, "cdn.example") {
		t.Fatalf("expected data-table image description without remote URL, got %q", got)
	}
}

func TestRenderHTMLBodyTreatsCaptionAndRoleTablesAsData(t *testing.T) {
	for name, html := range map[string]string{
		"caption": `<table><caption>Builds</caption><tr><td>Main</td><td>Passing</td></tr></table>`,
		"role":    `<table role="table"><tr><td>API</td><td>Healthy</td></tr></table>`,
	} {
		t.Run(name, func(t *testing.T) {
			got := ansi.Strip(renderHTMLBody(html, 40, CatppuccinMocha, true))
			if !strings.Contains(got, " | ") {
				t.Fatalf("expected semantic table rendered in columns, got %q", got)
			}
			if name == "caption" && !strings.Contains(got, "Builds") {
				t.Fatalf("expected table caption preserved, got %q", got)
			}
		})
	}
}

func TestRenderHTMLBodyPreservesDataTableInsideLayoutTable(t *testing.T) {
	html := `<table><tr><td><p>Report details</p><table><tr><th>Name</th><th>Status</th></tr><tr><td>API</td><td>Healthy</td></tr></table></td></tr></table>`
	got := ansi.Strip(renderHTMLBody(html, 48, CatppuccinMocha, true))

	if strings.Count(got, "Report details") != 1 || !strings.Contains(got, "Name | Status") || !strings.Contains(got, "API  | Healthy") {
		t.Fatalf("expected nested data table preserved inside flattened layout, got %q", got)
	}
}

func TestRenderHTMLBodyKeepsNewsletterSemanticsInLayoutTables(t *testing.T) {
	html := `<table><tr><td><h2>Weekly report</h2><p>Everything important.</p>` +
		`<ul><li>First item</li><li>Second item</li></ul>` +
		`<a role="button" href="https://example.com/report">View report</a></td></tr></table>`
	got := ansi.Strip(renderHTMLBody(html, 48, CatppuccinMocha, true))

	for _, want := range []string{"Weekly report", "Everything important.", "First item", "Second item", "[View report]"} {
		if count := strings.Count(got, want); count != 1 {
			t.Fatalf("expected %q exactly once, got %d occurrences in %q", want, count, got)
		}
	}
	if strings.Contains(got, "https://example.com/report") {
		t.Fatalf("expected CTA destination kept out of body text, got %q", got)
	}
}

func TestRenderRedditDigestHTMLCompactsPostNotification(t *testing.T) {
	html := redditDigestFixture()
	got, ok := renderRedditDigestHTML(html, 44, CatppuccinMocha, true)
	if !ok {
		t.Fatal("expected Reddit digest fixture to render")
	}
	stripped := ansi.Strip(got)
	for _, want := range []string{
		"r/omarchy - u/Borbit85 - 8h ago",
		"Free ai in Omarchy?",
		"I've installed this thing",
		"of like it.",
		"12 upvotes - 14 comments",
		"[Read post]",
	} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("expected %q in rendered Reddit digest, got %q", want, stripped)
		}
	}
	for _, bad := range []string{
		"Hidden preview",
		"Read More",
		"Hide r/omarchy",
		"click.redditmail.com",
		"redditstatic.com",
		"[image:",
	} {
		if strings.Contains(stripped, bad) {
			t.Fatalf("expected Reddit digest to omit %q, got %q", bad, stripped)
		}
	}
	for i, line := range strings.Split(stripped, "\n") {
		if width := ansi.StringWidth(line); width > 44 {
			t.Fatalf("line %d width = %d, want <= 44: %q", i, width, line)
		}
	}
}

func TestRenderRedditDigestHTMLFallsBackWhenNoPostsFound(t *testing.T) {
	if got, ok := renderRedditDigestHTML(`<p>Reddit notification settings changed.</p>`, 44, CatppuccinMocha, true); ok || got != "" {
		t.Fatalf("expected non-digest Reddit HTML to fall back, got ok=%v body=%q", ok, got)
	}
}

func TestRenderRedditDigestHTMLReadsRawLayoutBeforeGenericCleanup(t *testing.T) {
	html := `<table><tr><th style="font-size:0pt;line-height:0pt">` + redditDigestFixture() + `</th></tr></table>`
	normalized := normalizeHTMLForRendering(html)
	if strings.Contains(normalized, "post_title") {
		t.Fatalf("test fixture should exercise links dropped by generic cleanup, got %q", normalized)
	}
	got, ok := renderRedditDigestHTML(html, 44, CatppuccinMocha, true)
	if !ok || !strings.Contains(ansi.Strip(got), "Free ai in Omarchy?") {
		t.Fatalf("expected raw Reddit extractor to run before generic cleanup, ok=%v body=%q", ok, ansi.Strip(got))
	}
}

func TestRenderMessageBodyUsesRedditDigestForRedditMail(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	got := ansi.Strip(m.renderMessageBody(db.Message{
		From:     "Reddit <noreply@redditmail.com>",
		BodyHTML: redditDigestFixture(),
	}, 44))

	if !strings.Contains(got, "Free ai in Omarchy?") || !strings.Contains(got, "[Read post]") {
		t.Fatalf("expected compact Reddit body, got %q", got)
	}
	if strings.Contains(got, "Hide r/omarchy") || strings.Contains(got, "click.redditmail.com") {
		t.Fatalf("expected Reddit mail clutter removed, got %q", got)
	}
}

func TestRedditClickLinksBecomeActionablePostLinks(t *testing.T) {
	links := extractActionableLinksFromHTML(redditDigestFixture(), "")
	want := "https://www.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/"
	if len(links) != 1 || links[0] != want {
		t.Fatalf("expected only the decoded Reddit post link %q, got %#v", want, links)
	}
}

func TestRedditClickLinksSurviveGenericLayoutCleanup(t *testing.T) {
	html := `<table><tr><th style="font-size:0pt;line-height:0pt">` + redditDigestFixture() + `</th></tr></table>`
	links := extractActionableLinksFromHTML(html, "")
	want := "https://www.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/"
	if len(links) != 1 || links[0] != want {
		t.Fatalf("expected decoded Reddit post link from raw layout HTML %q, got %#v", want, links)
	}
}

func TestRenderHTMLBodyRemovesNewsletterPreheaderAndSpacers(t *testing.T) {
	html := `<div class="preheader">Hidden preview copy that should not render</div>` +
		`<table role="presentation"><tr><td width="1">&nbsp;</td><td><h2>Product update</h2></td></tr>` +
		`<tr><td style="height:1px;line-height:1px">&nbsp;</td><td>Useful details.</td></tr></table>`

	got := ansi.Strip(renderHTMLBody(html, 48, CatppuccinMocha, true))

	if strings.Contains(got, "Hidden preview") {
		t.Fatalf("expected preheader hidden, got %q", got)
	}
	for _, want := range []string{"Product update", "Useful details."} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected newsletter content %q in %q", want, got)
		}
	}
	if strings.Contains(got, "\u00a0") {
		t.Fatalf("expected spacer nbsp removed from %q", got)
	}
}

func redditDigestFixture() string {
	post := "https:%2F%2Fwww.reddit.com%2Fr%2Fomarchy%2Fcomments%2F1vxc6xv%2Ffree_ai_in_omarchy%2F%3Futm_content="
	click := func(marker string) string {
		return "https://click.redditmail.com/CL0/" + post + marker + "/1/token"
	}
	return `<html><body>` +
		`<span class="mcnPreviewText" style="display:none">Hidden preview</span>` +
		`<table role="presentation"><tr><td>` +
		`<a href="` + click("post_subreddit") + `"><strong>r/omarchy</strong></a>` +
		`<a href="` + click("post_author") + `">u/Borbit85</a>` +
		`<a href="` + click("post_timestamp") + `">8h ago</a>` +
		`<a href="` + click("post_title") + `"><strong>Free ai in Omarchy?</strong></a>` +
		`<a href="` + click("post_body") + `">I've installed this thing and I think I kind of like it. <span>Read More</span></a>` +
		`<a href="` + click("post_karma") + `"><img src="https://www.redditstatic.com/emaildigest/upvote.png" alt="">12 upvotes</a>` +
		`<a href="` + click("post_comments") + `"><img src="https://www.redditstatic.com/emaildigest/reddit_comment.png" alt="">14 comments</a>` +
		`<a href="https://click.redditmail.com/CL0/https:%2F%2Fwww.reddit.com%2Fmail%2Fnotification_off%2Fabc%3Futm_content=post_hide_subreddit/1/token">Hide r/omarchy</a>` +
		`</td></tr></table></body></html>`
}

func TestRenderHTMLBodyKeepsImageOnlyCTALabel(t *testing.T) {
	html := `<a class="primary-cta" href="https://example.com/read" aria-label="Read the full story">` +
		`<img src="https://cdn.example.com/button.png" alt="Read now"></a>`

	got := ansi.Strip(renderHTMLBody(html, 48, CatppuccinMocha, true))

	if !strings.Contains(got, "[Read now]") {
		t.Fatalf("expected image-only CTA label, got %q", got)
	}
	if strings.Contains(got, "https://example.com/read") || strings.Contains(got, "cdn.example.com") {
		t.Fatalf("expected CTA URLs hidden from body text, got %q", got)
	}
}

func TestRenderHTMLBodyConstrainsWideSemanticBlocks(t *testing.T) {
	longCell := strings.Repeat("quarterly-results-", 8)
	longCode := "    " + strings.Repeat("configuration-value-", 8)
	html := `<table><tr><th>Item</th><th>Value</th></tr><tr><td>Report</td><td>` + longCell + `</td></tr></table>` +
		`<pre>` + longCode + `</pre>`

	for _, plainUI := range []bool{true, false} {
		got := renderHTMLBody(html, 36, CatppuccinMocha, plainUI)
		for _, line := range strings.Split(got, "\n") {
			if gotWidth := ansi.StringWidth(line); gotWidth > 36 {
				t.Fatalf("plainUI=%t: expected width <= 36, got %d in %q", plainUI, gotWidth, ansi.Strip(line))
			}
		}
	}
}

func TestRenderMessageBodyFallsBackWhenHTMLIsOnlyBoilerplate(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	got := ansi.Strip(m.renderMessageBody(db.Message{
		BodyHTML: `<p><a href="https://example.com/browser">View in browser</a></p>`,
		BodyText: "Plain text has the actual message.",
	}, 48))

	if !strings.Contains(got, "Plain text has the actual message.") || strings.Contains(got, "View in browser") {
		t.Fatalf("expected plain-text fallback for boilerplate HTML, got %q", got)
	}
}

func TestRenderMessageBodyFallsBackWhenHTMLHasNoVisibleContent(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	got := ansi.Strip(m.renderMessageBody(db.Message{
		BodyHTML: `<script>alert("no")</script><p style="display:none">hidden</p>`,
		BodyText: "Readable fallback body.",
	}, 48))

	if !strings.Contains(got, "Readable fallback body.") || strings.Contains(got, "alert") || strings.Contains(got, "hidden") {
		t.Fatalf("expected plain-text fallback for empty HTML rendering, got %q", got)
	}
}

func TestRenderHTMLBodyHandlesMalformedMarkup(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<div><h2>Update<p>Still readable<table><tr><td>One<td>Two`, 40, CatppuccinMocha, true))
	for _, want := range []string{"Update", "Still readable", "One", "Two"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected malformed HTML content %q preserved in %q", want, got)
		}
	}
}

func TestWrapWordsBreaksLongURLsWithoutEllipsis(t *testing.T) {
	url := "https://example.com/" + strings.Repeat("really-long-path-segment-", 5)
	got := wrapWords("Open "+url+" today", 32)

	if strings.Contains(got, "…") {
		t.Fatalf("expected long URL to wrap without truncation, got %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if width := ansi.StringWidth(line); width > 32 {
			t.Fatalf("expected wrapped line width <= 32, got %d in %q from %q", width, line, got)
		}
	}
	if !strings.Contains(strings.ReplaceAll(got, "\n", ""), url) {
		t.Fatalf("expected wrapped text to preserve full URL, got %q", got)
	}
}

func TestRenderHTMLBodyNeverExceedsContentWidthForWideBlocks(t *testing.T) {
	longURL := "https://c.gle/" + strings.Repeat("AOExmq2fudFAlHts7GyfWH4hPGt0Cxr4jV7k9VE2rmd4n85bjVAhw", 3)
	html := `<table><tr><td>Search Console</td><td><a href="` + longURL + `">Add site variations</a></td></tr></table>` +
		`<pre>` + longURL + `</pre>`
	got := ansi.Strip(renderHTMLBody(html, 42, CatppuccinMocha, true))

	// Verify the output renders without panicking and contains core content.
	if !strings.Contains(got, "Search Console") {
		t.Fatalf("expected table cell text in rendered output, got %q", got)
	}
	// Width check: skip lines that are part of indented code blocks (glamour
	// preserves <pre> blocks as-is; long URLs inside them may exceed width).
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "  ") {
			continue
		}
		if width := ansi.StringWidth(line); width > 42 {
			t.Fatalf("expected rendered HTML line width <= 42 (non-code), got %d in %q from %q", width, line, got)
		}
	}
}

func TestContentFocusKeepsThreePaneLayout(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.height = 24
	m.viewport.Width = m.contentBodyWidth()
	m.viewport.Height = m.contentBodyHeight()
	m.focused = paneContent
	m.accounts = []db.Account{{ID: 1, Name: "Work Account"}}
	m.sidebarRows = []sidebarRow{{kind: rowKindAccount, accountID: 1}}
	m.filteredMessages = []db.Message{{
		ID:       1,
		Subject:  "Inbox List Subject",
		From:     "alice@example.com",
		Date:     time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		BodyText: "Only this message text should be selectable.",
	}}
	m.setViewportMessage(m.filteredMessages[0])

	view := ansi.Strip(m.View())
	for _, want := range []string{"Accounts", "Messages", "Work Account", "Only this message text should be selectable.", "Inbox List Subject"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected content focus to preserve three-pane text %q, got %q", want, view)
		}
	}
}

func newContentSelectionModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.height = 24
	m.viewport.Width = m.contentBodyWidth()
	m.viewport.Height = m.contentBodyHeight()
	m.focused = paneContent
	m.filteredMessages = []db.Message{{
		ID:       1,
		Subject:  "Selectable Subject",
		From:     "alice@example.com",
		Date:     time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		BodyText: "alpha\n\nbeta\n\ngamma",
	}}
	m.setViewportMessage(m.filteredMessages[0])
	return m
}

func TestVisualSelectionYanksOnlyContentLines(t *testing.T) {
	m := newContentSelectionModel(t)
	m.contentFocusLine = indexContentLine(t, m, "alpha")
	var copied string
	restore := stubClipboardWrite(t, &copied)
	defer restore()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected yank to return clipboard command")
	}
	if msg := cmd(); msg != (ClipboardCopiedMsg{}) {
		t.Fatalf("expected successful clipboard message, got %#v", msg)
	}

	if !strings.Contains(copied, "alpha") || !strings.Contains(copied, "beta") {
		t.Fatalf("expected copied range to include selected content, got %q", copied)
	}
	for _, unwanted := range []string{"Accounts", "Messages"} {
		if strings.Contains(copied, unwanted) {
			t.Fatalf("expected copied text to exclude side-pane text %q, got %q", unwanted, copied)
		}
	}
	if m.contentSelectionActive {
		t.Fatal("expected yank to clear visual selection")
	}
}

func TestVisualLineYanksFullRenderedMessage(t *testing.T) {
	m := newContentSelectionModel(t)
	var copied string
	restore := stubClipboardWrite(t, &copied)
	defer restore()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	_ = next
	if cmd == nil {
		t.Fatal("expected yank to return clipboard command")
	}
	if msg := cmd(); msg != (ClipboardCopiedMsg{}) {
		t.Fatalf("expected successful clipboard message, got %#v", msg)
	}

	for _, want := range []string{"Selectable Subject", "alpha", "beta", "gamma"} {
		if !strings.Contains(copied, want) {
			t.Fatalf("expected full copied message to include %q, got %q", want, copied)
		}
	}
}

func TestCtrlCCopiesFocusedContentLineWithoutSelection(t *testing.T) {
	m := newContentSelectionModel(t)
	m.contentFocusLine = indexContentLine(t, m, "beta")
	var copied string
	restore := stubClipboardWrite(t, &copied)
	defer restore()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected ctrl+c to copy focused content line")
	}
	if msg := cmd(); msg != (ClipboardCopiedMsg{}) {
		t.Fatalf("expected successful clipboard message, got %#v", msg)
	}
	if strings.TrimSpace(copied) != "beta" {
		t.Fatalf("expected focused line copy, got %q", copied)
	}
}

func TestEscCancelsContentSelectionBeforeLeavingContentPane(t *testing.T) {
	m := newContentSelectionModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = next.(Model)
	if !m.contentSelectionActive {
		t.Fatal("expected visual selection active")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.contentSelectionActive {
		t.Fatal("expected esc to cancel selection")
	}
	if m.focused != paneContent {
		t.Fatalf("expected focus to remain in content pane after canceling selection, got %v", m.focused)
	}
}

func TestContentSelectionKeepsThreePaneLayout(t *testing.T) {
	m := newContentSelectionModel(t)
	m.accounts = []db.Account{{ID: 1, Name: "Work Account"}}
	m.sidebarRows = []sidebarRow{{kind: rowKindAccount, accountID: 1}}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = next.(Model)
	view := ansi.Strip(m.View())
	for _, want := range []string{"Accounts", "Messages", "Work Account", "alpha"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected visual selection to preserve three-pane text %q, got %q", want, view)
		}
	}
}

func indexContentLine(t *testing.T, m Model, needle string) int {
	t.Helper()
	for i, line := range m.contentLines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	t.Fatalf("line containing %q not found in %#v", needle, m.contentLines)
	return 0
}

func stubClipboardWrite(t *testing.T, copied *string) func() {
	t.Helper()
	origCmd := clipboardWriteCmd
	clipboardWriteCmd = func(text string) tea.Cmd {
		return func() tea.Msg {
			*copied = text
			return ClipboardCopiedMsg{}
		}
	}
	// The compose editor owns copy/cut and writes via its own clipboard, so
	// stub that seam too. Set it before any editor is constructed.
	origEditor := editorClipboard
	editorClipboard = fakeClipboard{copied: copied}
	return func() {
		clipboardWriteCmd = origCmd
		editorClipboard = origEditor
	}
}

// fakeClipboard captures editor writes in tests instead of touching the OS.
type fakeClipboard struct{ copied *string }

func (f fakeClipboard) Read() (string, error)   { return "", nil }
func (f fakeClipboard) Write(text string) error { *f.copied = text; return nil }
