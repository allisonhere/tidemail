package ui

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func readerLinksModel(t *testing.T, html, plain string) Model {
	t.Helper()
	t.Setenv("TIDEMAIL_IMAGE_PROTOCOL", "none")
	m := cacheTestModel(t)
	m.cfg.Display.ActionableLinks = true
	m.cfg.Display.FocusLine = true
	m.cfg.Display.ThreadedConversations = false
	msg := db.Message{ID: 1, MailboxID: 1, Subject: "Links fixture", BodyHTML: html, BodyText: plain}
	m.messages = []db.Message{msg}
	m.filteredMessages = m.messages
	m.focused = paneContent
	m.setViewportForCurrentRow()
	return m
}

func assertReaderLinkSelection(t *testing.T, m Model, want int) {
	t.Helper()
	if m.contentLinkIdx != want {
		t.Fatalf("selected=%d, want %d", m.contentLinkIdx, want)
	}
	if len(m.contentLinkRows) != len(m.contentLinks) {
		t.Fatalf("rows=%d, links=%d", len(m.contentLinkRows), len(m.contentLinks))
	}
	row := m.contentLinkRows[want]
	for i, linkRow := range m.contentLinkRows {
		selected := strings.HasPrefix(strings.TrimSpace(m.contentLines[linkRow]), ">")
		if selected != (i == want) {
			t.Fatalf("row %d selection=%v, want %v: %q", i, selected, i == want, m.contentLines[linkRow])
		}
	}
	if row < m.viewport.YOffset || row >= m.viewport.YOffset+m.contentBodyHeight() {
		t.Fatalf("selected row %d outside viewport [%d,%d)", row, m.viewport.YOffset, m.viewport.YOffset+m.contentBodyHeight())
	}
	if m.contentFocusLine != row {
		t.Fatalf("focus=%d, selected row=%d", m.contentFocusLine, row)
	}
	if !strings.Contains(ansi.Strip(m.renderStatusBar()), fmt.Sprintf("link %d/%d", want+1, len(m.contentLinks))) {
		t.Fatal("status counter disagrees with selection")
	}
	if !strings.Contains(ansi.Strip(m.renderContentPane()), strings.TrimSpace(m.contentLines[row])) {
		t.Fatal("selected entry is absent from rendered pane")
	}
}

func TestReaderLinksWarmCacheNavigation(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	m := readerLinksModel(t, `<a href="https://example.com/first">First action</a><a href="https://example.com/second">Second action</a>`, "")
	// The viewport cache starts neutral; only navigation selects a link.
	for _, step := range []struct {
		key  tea.KeyMsg
		want int
	}{
		{tea.KeyMsg{Type: tea.KeyCtrlN}, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: true}, 1},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: true}, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}, Alt: true}, 1},
	} {
		next, _ := m.Update(step.key)
		m = next.(Model)
		assertReaderLinkSelection(t, m, step.want)
		row := m.contentLinkRows[step.want] - m.viewport.YOffset
		// Use the renderer's own color conversion, including its rounding.
		probe := lipgloss.NewStyle().Background(m.styles.Theme.BorderFocus).Render("x")
		background := regexp.MustCompile(`48;2;[0-9]+;[0-9]+;[0-9]+`).FindString(probe)
		for _, raw := range strings.Split(ansi.Strip(m.viewport.View()), "\n") {
			if strings.HasPrefix(strings.TrimSpace(raw), ">") {
				t.Fatal("viewport cache contains selection-dependent presentation")
			}
		}
		painted := strings.Split(m.renderContentFocusLine(m.viewport.View(), m.contentPaneContentWidth(), m.contentBodyHeight(), false), "\n")[row]
		if background == "" || !strings.Contains(painted, background) {
			t.Fatal("selected footer row lost its active background")
		}
		if firstOSC8Target(painted) != m.contentLinks[step.want] {
			t.Fatal("focus highlight lost the full terminal hyperlink")
		}
	}
	if m.bodyCache.len() != 1 {
		t.Fatal("link navigation rebuilt the HTML body")
	}
	if m.viewportCache.len() != 1 {
		t.Fatal("link navigation rebuilt the viewport layout")
	}
}

func TestReaderGraphicsAboveFooterPreservesLinkSelection(t *testing.T) {
	m, _ := graphicsModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m.cfg.Display.ActionableLinks = true
	m.cfg.Display.FocusLine = true
	m.cfg.Display.ThreadedConversations = false
	msg := db.Message{ID: 1, Subject: "Synthetic graphics", BodyHTML: `<a href="https://example.com/graphic"><img src="` + pngDataURI(t, 240, 120) + `" alt="Graphic"></a><a href="mailto:action@example.com?subject=Review">Approve</a>`}
	m.messages = []db.Message{msg}
	m.filteredMessages = m.messages
	m.focused = paneContent
	m.setViewportForCurrentRow()
	if len(m.contentLinks) != 2 || m.contentLinkLabels[m.contentLinks[0]] != "[image: Graphic]" {
		t.Fatalf("graphics links or labels changed: %#v, %#v", m.contentLinks, m.contentLinkLabels)
	}
	if len(m.renderMessageForDisplay(msg, m.contentBodyWidth()).images) != 1 {
		t.Fatal("fixture did not render a raster image")
	}
	for _, step := range []struct {
		key   tea.KeyMsg
		index int
	}{
		{tea.KeyMsg{Type: tea.KeyCtrlN}, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: true}, 1},
	} {
		next, _ = m.Update(step.key)
		m = next.(Model)
		assertReaderLinkSelection(t, m, step.index)
		if got, ok := m.focusedLineLink(); !ok || got != m.contentLinks[step.index] {
			t.Fatalf("graphics footer lost target: %q, %v", got, ok)
		}
	}
	if m.viewportCache.len() != 1 {
		t.Fatal("graphics link navigation rebuilt the viewport layout")
	}
}

func TestReaderLinksScrollToSelectedEntry(t *testing.T) {
	var html strings.Builder
	for i := 0; i < 82; i++ {
		fmt.Fprintf(&html, `<p><a href="https://example.com/%d">Action %02d</a></p>`, i, i)
	}
	m := readerLinksModel(t, html.String(), "")
	for i := 0; i < 82; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
		m = next.(Model)
		assertReaderLinkSelection(t, m, i)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = next.(Model)
	assertReaderLinkSelection(t, m, 0)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: true})
	m = next.(Model)
	assertReaderLinkSelection(t, m, 81)
}

func TestReaderCachedFooterSelectionAtReadingWidthCap(t *testing.T) {
	m := readerLinksModel(t, `<a href="mailto:workflow@example.com?subject=Approve">Review this synthetic approval request</a><a href="https://example.com/report">Read the synthetic report</a>`, "")
	m.cfg.Display.ReadingWidth = 20
	m.cfg.Display.FocusLine = false
	m.setViewportForCurrentRow()
	cacheEntries := m.viewportCache.len()
	for _, want := range []int{0, 1, 0} {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
		m = next.(Model)
		assertReaderLinkSelection(t, m, want)
		row := m.contentLinkRows[want] - m.viewport.YOffset
		painted := strings.Split(m.renderContentFocusLine(m.viewport.View(), m.contentPaneContentWidth(), m.contentBodyHeight(), true), "\n")[row]
		if !strings.HasPrefix(strings.TrimSpace(ansi.Strip(painted)), ">") || ansi.StringWidth(painted) > 20 {
			t.Fatalf("narrow selected row changed width or chevron: %q", painted)
		}
	}
	if m.viewportCache.len() != cacheEntries {
		t.Fatal("narrow link navigation rebuilt the viewport layout")
	}
}

func TestReaderLinkLabelsAndDestinations(t *testing.T) {
	longURL := "https://tracking.example/click?id=" + strings.Repeat("abcdef", 500)
	html := `<a href="` + longURL + `"><strong>View the UNO Events Calendar</strong></a>
 <a href="https://example.com/banner"><img src="cid:banner" alt="Newsletter banner"></a>
 <a href="https://example.com/social"><img src="cid:social" alt="LinkedIn logo"></a>
 <a href="https://example.com/fallback"></a>
 <a href="` + longURL + `">Repeated calendar</a>`
	wantTargets := extractActionableLinksFromHTML(html, "")
	for _, mode := range []struct{ plainUI, focusLine bool }{{false, true}, {true, true}, {false, false}} {
		t.Run(fmt.Sprintf("plainUI=%v/focusLine=%v", mode.plainUI, mode.focusLine), func(t *testing.T) {
			m := readerLinksModel(t, html, "")
			m.styles.PlainUI = mode.plainUI
			m.cfg.Display.FocusLine = mode.focusLine
			m.setViewportForCurrentRow()
			if !reflect.DeepEqual(m.contentLinks, wantTargets) {
				t.Fatal("display labels changed destination order or extraction policy")
			}
			view := ansi.Strip(m.renderContentLinks(m.contentBodyWidth()))
			for _, label := range []string{"View the UNO Events Calendar", "[image: Newsletter banner]", "[image: LinkedIn logo]", "https://example.com/fallback"} {
				if !strings.Contains(view, label) {
					t.Fatalf("missing label %q in %q", label, view)
				}
			}
			// The destination host is shown so a label cannot hide where a link
			// goes, but the full tracking URL must stay out of the list.
			if strings.Contains(view, "https://tracking.example") || strings.Contains(view, "?") {
				t.Fatal("descriptive anchor displayed its tracking URL")
			}
			if !strings.Contains(view, "(tracking.example)") {
				t.Fatal("descriptive anchor did not show its destination host")
			}
			if strings.Count(view, "View the UNO Events Calendar") != 1 {
				t.Fatal("repeated destination changed existing deduplication")
			}
			originalOpen, originalCopy := browserOpen, clipboardWriteCmd
			t.Cleanup(func() { browserOpen, clipboardWriteCmd = originalOpen, originalCopy })
			var opened, copied string
			browserOpen = func(_, target string) error { opened = target; return nil }
			clipboardWriteCmd = func(text string) tea.Cmd { copied = text; return nil }
			// Navigate to the long URL, whose OSC 8 target is deliberately rejected.
			m.contentLinkIdx = len(m.contentLinks) - 1
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
			m = next.(Model)
			assertReaderLinkSelection(t, m, 0)
			for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune{'o'}}, {Type: tea.KeyEnter}} {
				_, cmd := m.Update(key)
				if cmd == nil {
					t.Fatal("activation produced no command")
				}
				cmd()
				if opened != longURL {
					t.Fatalf("opened altered target: %q", opened)
				}
			}
			_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			if copied != longURL {
				t.Fatalf("copied altered target: %q", copied)
			}
			// Explicit selection still copies displayed text rather than a URL.
			m.startContentSelection()
			if got := m.contentSelectionText(true); !strings.Contains(got, "View the UNO Events Calendar") {
				t.Fatalf("selected text copy changed: %q", got)
			}
		})
	}
}

func TestReaderPlainTextLinksAndBodyCopy(t *testing.T) {
	target := "https://example.com/plain"
	m := readerLinksModel(t, "", "Some prose\n"+target)
	if len(m.contentLinks) != 1 || m.contentLinks[0] != target {
		t.Fatal("plain-text URL changed")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = next.(Model)
	assertReaderLinkSelection(t, m, 0)
	if m.contentSelectionText(true) != target {
		t.Fatal("plain-text footer URL copy changed")
	}
	m.contentFocusLine = 0
	if got := m.contentSelectionText(true); strings.TrimSpace(got) != "Links fixture" {
		t.Fatalf("ordinary line copying changed: %q", got)
	}
}

func TestReaderLinkLabelSanitizationAndCacheOwnership(t *testing.T) {
	m := readerLinksModel(t, `<a href="https://example.com">Hello &amp; &#27;[31mworld&#10; next</a>`, "")
	label := m.contentLinkLabels["https://example.com"]
	if strings.ContainsAny(label, "\x1b\n\r") || !strings.Contains(label, "Hello &") {
		t.Fatalf("unsafe or lost label %q", label)
	}
	m.contentLinkLabels["https://example.com"] = "mutated"
	m.setViewportForCurrentRow()
	if m.contentLinkLabels["https://example.com"] != label {
		t.Fatal("caller mutated cached label metadata")
	}
}

func TestReaderLinksFollowLayoutChanges(t *testing.T) {
	m := readerLinksModel(t, `<p>First paragraph.</p><blockquote><p>A quoted paragraph.</p></blockquote><p><a href="https://example.com/a">First action</a><a href="https://example.com/b">Second action</a></p>`, "")
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 140, Height: 48}} {
		next, _ := m.Update(size)
		m = next.(Model)
		m.contentShowHeaders = true
		m.contentQuotesCollapsed = true
		m.setViewportForCurrentRow()
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
		m = next.(Model)
		assertReaderLinkSelection(t, m, m.contentLinkIdx)
	}
}

func TestReaderThreadLinksUseFinalRowOffsets(t *testing.T) {
	m := readerLinksModel(t, `<a href="https://example.com/one">First message action</a>`, "")
	first := m.messages[0]
	second := db.Message{ID: 2, Subject: first.Subject, BodyHTML: `<p>Second message.</p><a href="https://example.com/two"><img src="cid:banner" alt="Second message banner"></a>`}
	thread := messageThread{Key: "test", Representative: first, Messages: []db.Message{first, second}, Count: 2}
	m.setViewportThread(thread)
	m.stepContentLink(1)
	m.stepContentLink(1)
	m.setViewportThread(thread)
	m.revealContentLink()
	assertReaderLinkSelection(t, m, 1)
	if got := m.contentLinkLabels["https://example.com/two"]; got != "[image: Second message banner]" {
		t.Fatalf("thread lost label: %q", got)
	}
	if got, ok := m.focusedLineLink(); !ok || got != "https://example.com/two" {
		t.Fatalf("thread destination = %q, %v", got, ok)
	}
}

func TestReaderLinksBeforeAttachmentsScrollToEntry(t *testing.T) {
	m := readerLinksModel(t, `<a href="https://example.com/one">First action</a><a href="https://example.com/two">Second action</a>`, "")
	msg := m.messages[0]
	accountID, err := m.db.AddAccount("", "Synthetic", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := m.db.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	msg.MailboxID = mailboxID
	msg.UID = 1
	if err := m.db.UpsertMessage(msg); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if _, err := m.db.SaveAttachment(msg.ID, db.Attachment{Filename: fmt.Sprintf("attachment-%02d.txt", i), ContentType: "text/plain", Data: []byte("test")}); err != nil {
			t.Fatal(err)
		}
	}
	msg.HasAttachment = true
	m.messages[0] = msg
	m.filteredMessages[0] = msg
	m.setViewportForCurrentRow()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = next.(Model)
	assertReaderLinkSelection(t, m, 0)
	if m.viewport.YOffset == m.viewport.TotalLineCount()-m.viewport.Height {
		t.Fatal("link navigation jumped to bottom of attachments")
	}
}

func TestReaderLinksClearMetadataWhenChangingMessages(t *testing.T) {
	m := readerLinksModel(t, `<a href="https://example.com/one">First action</a>`, "")
	m.setViewportMessage(db.Message{ID: 2, Subject: "No links", BodyText: "An ordinary paragraph."})
	if len(m.contentLinks) != 0 || len(m.contentLinkRows) != 0 || len(m.contentLinkLabels) != 0 {
		t.Fatal("the new message inherited old link metadata")
	}
	if _, ok := m.currentContentLink(); ok {
		t.Fatal("the new message retained a selected link")
	}
}
