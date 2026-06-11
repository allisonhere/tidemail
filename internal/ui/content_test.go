package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

	if got, want := ansi.StringWidth(firstLine), m.articlesPaneWidth(); got != want {
		t.Fatalf("expected title line to span content pane width %d, got %d in %q", want, got, ansi.Strip(firstLine))
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

	for _, line := range strings.Split(got, "\n") {
		if width := ansi.StringWidth(line); width > 42 {
			t.Fatalf("expected rendered HTML line width <= 42, got %d in %q from %q", width, line, got)
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
	m = next.(Model)
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
	orig := clipboardWriteCmd
	clipboardWriteCmd = func(text string) tea.Cmd {
		return func() tea.Msg {
			*copied = text
			return ClipboardCopiedMsg{}
		}
	}
	return func() { clipboardWriteCmd = orig }
}
