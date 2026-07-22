package ui

import (
	"fmt"
	"strings"

	"github.com/allisonhere/tide/internal/db"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) renderContentPane() string {
	w := m.articlesPaneWidth()
	paneH := m.contentPaneOuterHeight()
	bodyH := m.contentBodyHeight()
	bg := m.styles.Theme.Bg

	focused := m.focused == paneContent
	searching := m.overlay == overlayContentSearch

	vpH := bodyH
	if searching {
		vpH = max(1, bodyH-1)
	}

	vp := m.viewport
	vp.Width = w
	vp.Height = vpH
	vp.Style = lipgloss.NewStyle().Background(bg)
	body := m.renderContentFocusLine(vp.View(), w, vpH, focused)
	body = clampView(body, w, vpH, bg)

	header := m.renderPaneHeader(paneContent, "Content", focused, w)
	content := header + "\n" + body

	if searching {
		matchInfo := ""
		if len(m.contentSearchMatches) > 0 {
			matchInfo = fmt.Sprintf("  [%d/%d]", m.contentSearchIdx+1, len(m.contentSearchMatches))
		} else if m.contentSearchQuery != "" {
			matchInfo = "  [no matches]"
		}
		input := m.contentSearchInput
		input.Cursor.Style = lipgloss.NewStyle().Background(m.styles.Theme.BorderFocus).Foreground(accentReadableOn(m.styles.Theme.Fg, m.styles.Theme.BorderFocus, 4.5))
		searchBar := m.styles.ContentBody.Width(w).Render(inputViewWithCursor(input, true) + matchInfo)
		content = header + "\n" + searchBar + "\n" + body
	}

	inner := m.styles.ContentPane.
		Width(w).
		Height(paneH).
		Render(content)

	return lipgloss.NewStyle().
		Background(bg).
		Width(w).Height(paneH).
		Render(inner)
}

func (m Model) renderMessageContent(msg db.Message) string {
	paneWidth := m.articlesPaneWidth()
	contentWidth := m.contentBodyWidth()
	bodyWidth := m.contentBodyWidth()
	titleWidth := max(1, paneWidth-m.styles.ContentTitle.GetHorizontalFrameSize())
	metaWidth := max(1, contentWidth-m.styles.ContentMeta.GetHorizontalFrameSize())
	title := m.styles.ContentTitle.Width(paneWidth).Render(truncate(unescapeDisplayText(msg.Subject), titleWidth))
	metaStr := msg.Date.Format("Mon, 02 Jan 2006 15:04")
	if msg.From != "" {
		metaStr += "  From: " + msg.From
	}
	meta := " " + m.styles.ContentMeta.Width(contentWidth).Render(truncate(metaStr, metaWidth))

	// Full headers block (togglable via ctrl+e)
	var fullHeaders string
	if m.contentShowHeaders {
		if block := m.renderFullHeaders(msg, contentWidth); block != "" {
			fullHeaders = block + "\n"
		}
	}

	body := m.renderMessageBody(msg, bodyWidth)

	body = collapseQuoteBlocks(body, m.contentQuotesCollapsed)

	if m.actionableLinksEnabled() && len(m.contentLinks) > 0 {
		body += "\n\n" + m.renderContentLinks(bodyWidth)
	}

	if len(m.contentAttachments) > 0 {
		body += "\n\n" + m.renderAttachmentList(bodyWidth)
	}

	content := title + "\n" + meta + "\n\n" + fullHeaders + body
	if m.focused != paneContent {
		content = stripEmojiGraphemes(content)
	}
	content = normalizeHardBreaks(content)
	return fillViewWidth(content, paneWidth, m.styles.Theme.Bg)
}

func (m Model) renderMessageBody(msg db.Message, bodyWidth int) string {
	var body string
	if msg.BodyHTML != "" {
		body = renderHTMLBody(msg.BodyHTML, bodyWidth, m.styles.Theme, m.styles.PlainUI)
	}
	if body == "" {
		content := msg.BodyText
		if content == "" {
			content = "No message body."
		}
		if m.cfg.Display.FilterLinks {
			content = filterLinksFromContent(content)
		}
		body = indentBlock(m.styles.ContentBody.Width(bodyWidth).Render(formatArticleBody(content, bodyWidth, m.styles.Theme, m.styles.PlainUI)), 1)
	}
	return body
}

func (m Model) renderAttachmentList(width int) string {
	if len(m.contentAttachments) == 0 {
		return ""
	}
	th := m.styles.Theme
	accent := lipgloss.NewStyle().Foreground(messageLinkColor(th))
	dimmed := lipgloss.NewStyle().Foreground(messageMutedColor(th))
	body := m.styles.ContentBody.Width(width)

	header := accent.Render("── " + strings.ToUpper("Attachments") + " ──")
	lines := []string{
		header + dimmed.Render(strings.Repeat("─", max(0, width-ansi.StringWidth(header)))),
	}
	maxSizeLen := 0
	for _, a := range m.contentAttachments {
		if l := len(formatFileSize(a.Size)); l > maxSizeLen {
			maxSizeLen = l
		}
	}
	for _, a := range m.contentAttachments {
		icon := fileTypeIcon(a.Filename, a.ContentType)
		sizeStr := formatFileSize(a.Size)
		iconStyled := accent.Render(" " + icon + " ")
		// Truncate the filename so a long name can't overflow the pane and shove
		// the size column off-screen; leave room for the icon, size, and gap.
		nameW := max(1, width-ansi.StringWidth(iconStyled)-maxSizeLen-3)
		line := iconStyled + truncate(a.Filename, nameW)
		paddedSize := fmt.Sprintf("%*s", maxSizeLen, sizeStr)
		// Right-align size by padding to column end
		used := ansi.StringWidth(line)
		pad := width - used - maxSizeLen - 2
		if pad < 1 {
			pad = 1
		}
		line += strings.Repeat(" ", pad) + dimmed.Render(paddedSize)
		lines = append(lines, line)
	}
	lines = append(lines, "")
	lines = append(lines, dimmed.Render("  ctrl+d  save all to folder"))
	return indentBlock(body.Render(strings.Join(lines, "\n")), 1)
}

func (m Model) renderContentLinks(width int) string {
	lines := make([]string, 0, len(m.contentLinks)+1)
	lines = append(lines, strings.ToUpper("Links"))
	activeStyle := lipgloss.NewStyle().
		Background(m.styles.Theme.BorderFocus).
		Foreground(accentReadableOn(m.styles.Theme.Fg, m.styles.Theme.BorderFocus, 4.5)).
		Bold(true)
	linkStyle := lipgloss.NewStyle().Foreground(messageLinkColor(m.styles.Theme))
	for i, link := range m.contentLinks {
		prefix := "  "
		if i == m.contentLinkIdx {
			prefix = "> "
		}
		line := truncate(prefix+link, max(8, width))
		if i == m.contentLinkIdx {
			line = activeStyle.Render(line)
		} else {
			line = linkStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return indentBlock(m.styles.ContentBody.Width(width).Render(strings.Join(lines, "\n")), 1)
}

func (m Model) actionableLinksEnabled() bool {
	return m.cfg.Display.ActionableLinks
}

// focusedLineLink returns the first URL on the currently highlighted focus line,
// when the focus-line feature is active. This lets `o` open whatever link sits
// under the highlight, independent of the actionable-links list.
func (m Model) focusedLineLink() (string, bool) {
	if !m.cfg.Display.FocusLine {
		return "", false
	}
	if m.contentFocusLine < 0 || m.contentFocusLine >= len(m.contentLines) {
		return "", false
	}
	links := extractActionableLinks(m.contentLines[m.contentFocusLine], "")
	if len(links) == 0 {
		return "", false
	}
	return links[0], true
}

func (m *Model) setViewportMessage(msg db.Message) {
	sameMsg := m.contentMessageID == msg.ID && m.contentLineCount > 0
	m.syncContentLinks(msg)
	m.contentAttachments = nil
	m.contentQuotesCollapsed = false
	m.clearContentSelection()
	if !sameMsg {
		m.contentShowHeaders = true
	}
	if msg.HasAttachment {
		if atts, err := m.db.GetAttachments(msg.ID); err == nil {
			m.contentAttachments = atts
		}
	}
	content := m.renderMessageContent(msg)
	m.contentSearchMatches = collectSearchMatches(content, m.contentSearchQuery)
	m.viewport.SetContent(content)
	m.contentMessageID = msg.ID
	m.contentLines = strings.Split(ansi.Strip(content), "\n")
	m.contentLineCount = len(m.contentLines)
	m.contentFocusable = messageFocusableLines(content)
	m.contentFocusLine = clamp(m.contentFocusLine, 0, max(0, m.contentLineCount-1))
	if !sameMsg {
		m.contentFocusLine = firstFocusableLine(m.contentFocusable)
		m.viewport.GotoTop()
	}
	m.ensureContentFocusVisible()
}

func (m *Model) setViewportThread(thread messageThread) {
	rep := thread.Representative
	sameMsg := m.contentMessageID == rep.ID && m.contentLineCount > 0
	m.syncThreadContentLinks(thread)
	m.contentAttachments = nil
	m.contentQuotesCollapsed = false
	m.clearContentSelection()
	if !sameMsg {
		m.contentShowHeaders = true
	}
	content := m.renderThreadContent(thread)
	m.contentSearchMatches = collectSearchMatches(content, m.contentSearchQuery)
	m.viewport.SetContent(content)
	m.contentMessageID = rep.ID
	m.contentLines = strings.Split(ansi.Strip(content), "\n")
	m.contentLineCount = len(m.contentLines)
	m.contentFocusable = messageFocusableLines(content)
	m.contentFocusLine = clamp(m.contentFocusLine, 0, max(0, m.contentLineCount-1))
	if !sameMsg {
		m.contentFocusLine = firstFocusableLine(m.contentFocusable)
		m.viewport.GotoTop()
	}
	m.ensureContentFocusVisible()
}

func (m *Model) setViewportForCurrentRow() {
	if m.threadedMessagesEnabled() {
		if m.messageCursor >= 0 && m.messageCursor < len(m.messageThreads) {
			m.setViewportThread(m.messageThreads[m.messageCursor])
		}
		return
	}
	if msg := m.currentRowMessage(); msg != nil {
		m.setViewportMessage(*msg)
	}
}

func (m *Model) clearViewportMessage() {
	m.viewport.SetContent("")
	m.contentLinks = nil
	m.contentLinkIdx = -1
	m.contentMessageID = 0
	m.contentFocusLine = 0
	m.contentLineCount = 0
	m.contentFocusable = nil
	m.contentLines = nil
	m.contentAttachments = nil
	m.clearContentSelection()
	m.clearContentSearch()
	m.viewport.GotoTop()
}

func (m Model) renderThreadContent(thread messageThread) string {
	if len(thread.Messages) == 0 {
		return ""
	}
	paneWidth := m.articlesPaneWidth()
	contentWidth := m.contentBodyWidth()
	titleWidth := max(1, paneWidth-m.styles.ContentTitle.GetHorizontalFrameSize())
	titleText := unescapeDisplayText(thread.Representative.Subject)
	if thread.Count > 1 {
		titleText = fmt.Sprintf("%s (%d messages)", titleText, thread.Count)
	}
	title := m.styles.ContentTitle.Width(paneWidth).Render(truncate(titleText, titleWidth))

	var blocks []string
	for _, msg := range thread.Messages {
		header := m.threadMessageHeader(msg, contentWidth)
		body := m.renderMessageBody(msg, contentWidth)
		if strings.TrimSpace(body) == "" {
			body = "No message body."
		}
		blocks = append(blocks, header+"\n\n"+body)
	}
	body := collapseQuoteBlocks(strings.Join(blocks, "\n\n"), m.contentQuotesCollapsed)
	if m.actionableLinksEnabled() && len(m.contentLinks) > 0 {
		body += "\n\n" + m.renderContentLinks(contentWidth)
	}
	content := title + "\n\n" + body
	if m.focused != paneContent {
		content = stripEmojiGraphemes(content)
	}
	content = normalizeHardBreaks(content)
	return fillViewWidth(content, paneWidth, m.styles.Theme.Bg)
}

func (m Model) threadMessageHeader(msg db.Message, width int) string {
	meta := msg.Date.Format("Mon, 02 Jan 2006 15:04")
	if msg.From != "" {
		meta += "  From: " + msg.From
	}
	line := "── " + meta + " ──"
	if lipgloss.Width(line) > width {
		line = truncate(line, width)
	}
	header := m.styles.ContentMeta.Width(width).Render(line)
	if m.contentShowHeaders {
		if block := m.renderFullHeaders(msg, width); block != "" {
			header += "\n" + block
		}
	}
	return header
}

// renderFullHeaders renders the toggleable full-header block (Date, From, To,
// CC, Reply-To, Message-ID) shared by the single-message and threaded views.
// Returns "" when the message has no populated header fields.
func (m Model) renderFullHeaders(msg db.Message, width int) string {
	dim := readableText(m.styles.Theme.Dimmed, m.styles.Theme.Bg, 3.0)
	type headerField struct{ label, value string }
	fields := []headerField{
		{"Date", msg.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700")},
		{"From", msg.From},
		{"To", msg.To},
		{"CC", msg.CC},
		{"Reply-To", msg.ReplyTo},
		{"Message-ID", msg.MessageID},
	}
	var headerLines []string
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		line := lipgloss.NewStyle().Background(m.styles.Theme.Bg).Foreground(dim).Width(width).Render(fmt.Sprintf("  %-12s %s", f.label+":", f.value))
		headerLines = append(headerLines, line)
		if f.label == "Message-ID" {
			headerLines = append(headerLines, lipgloss.NewStyle().Background(m.styles.Theme.Bg).Width(width).Render(""))
		}
	}
	if len(headerLines) == 0 {
		return ""
	}
	return strings.Join(headerLines, "\n")
}

func (m *Model) clearContentSearch() {
	m.overlay = overlayNone
	m.contentSearchQuery = ""
	m.contentSearchMatches = nil
	m.contentSearchIdx = -1
	m.contentSearchInput.Blur()
	m.clearContentSelection()
	if msg := m.currentContentMessage(); msg != nil {
		m.setViewportMessage(*msg)
	}
}

func (m *Model) applyContentSearch() {
	q := strings.ToLower(strings.TrimSpace(m.contentSearchInput.Value()))
	m.contentSearchQuery = q
	if msg := m.currentContentMessage(); msg != nil {
		m.setViewportMessage(*msg)
	}
	if len(m.contentSearchMatches) > 0 {
		m.contentSearchIdx = 0
		m.scrollToContentMatch(0)
	} else {
		m.contentSearchIdx = -1
	}
}

func (m *Model) cycleContentSearchMatch(delta int) {
	if len(m.contentSearchMatches) == 0 {
		return
	}
	n := len(m.contentSearchMatches)
	m.contentSearchIdx = ((m.contentSearchIdx+delta)%n + n) % n
	m.scrollToContentMatch(m.contentSearchIdx)
}

func (m *Model) scrollToContentMatch(idx int) {
	if idx < 0 || idx >= len(m.contentSearchMatches) {
		return
	}
	line := m.contentSearchMatches[idx]
	m.viewport.SetYOffset(max(0, line-m.viewport.Height/2))
}

func (m *Model) moveContentFocusLine(delta int) {
	if m.contentLineCount <= 0 {
		return
	}
	m.contentFocusLine = nextContentFocusLine(m.contentFocusLine, delta, m.contentFocusable, m.contentLineCount)
	m.ensureContentFocusVisible()
}

func (m *Model) startContentSelection() {
	if m.contentLineCount == 0 {
		return
	}
	if m.contentSelectionActive && !m.contentSelectionAll {
		m.clearContentSelection()
		return
	}
	m.contentSelectionActive = true
	m.contentSelectionAll = false
	m.contentSelectionAnchor = clamp(m.contentFocusLine, 0, m.contentLineCount-1)
}

func (m *Model) selectAllContentLines() {
	if m.contentLineCount == 0 {
		return
	}
	m.contentSelectionActive = true
	m.contentSelectionAll = true
	m.contentSelectionAnchor = 0
}

func (m *Model) clearContentSelection() {
	m.contentSelectionActive = false
	m.contentSelectionAll = false
	m.contentSelectionAnchor = 0
}

func (m Model) contentSelectionText(fallbackFocused bool) string {
	if len(m.contentLines) == 0 {
		return ""
	}
	var start, end int
	switch {
	case m.contentSelectionActive && m.contentSelectionAll:
		start, end = 0, len(m.contentLines)-1
	case m.contentSelectionActive:
		start = clamp(m.contentSelectionAnchor, 0, len(m.contentLines)-1)
		end = clamp(m.contentFocusLine, 0, len(m.contentLines)-1)
		if start > end {
			start, end = end, start
		}
	case fallbackFocused:
		start = clamp(m.contentFocusLine, 0, len(m.contentLines)-1)
		end = start
	default:
		return ""
	}

	lines := make([]string, 0, end-start+1)
	for _, line := range m.contentLines[start : end+1] {
		lines = append(lines, strings.TrimRight(line, " \t"))
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (m Model) contentLineSelected(line int) bool {
	if !m.contentSelectionActive || m.contentLineCount == 0 {
		return false
	}
	if m.contentSelectionAll {
		return line >= 0 && line < m.contentLineCount
	}
	start := clamp(m.contentSelectionAnchor, 0, m.contentLineCount-1)
	end := clamp(m.contentFocusLine, 0, m.contentLineCount-1)
	if start > end {
		start, end = end, start
	}
	return line >= start && line <= end
}

func (m Model) renderContentFocusLine(body string, width, height int, focused bool) string {
	hasSearch := len(m.contentSearchMatches) > 0
	hasFocus := m.cfg.Display.FocusLine && focused && m.contentLineCount > 0
	hasSelection := m.contentSelectionActive && m.contentLineCount > 0

	if !hasSearch && !hasFocus && !hasSelection {
		return body
	}
	if width <= 0 || height <= 0 {
		return body
	}

	lines := strings.Split(body, "\n")

	styleLine := func(lineIdx int, style lipgloss.Style) {
		viewIdx := lineIdx - m.viewport.YOffset
		if viewIdx < 0 || viewIdx >= height || viewIdx >= len(lines) {
			return
		}
		l := ansi.Truncate(ansi.Strip(lines[viewIdx]), width, "")
		if pad := width - lipgloss.Width(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		lines[viewIdx] = style.Width(width).Render(l)
	}

	if hasSearch {
		for _, matchLine := range m.contentSearchMatches {
			styleLine(matchLine, m.styles.SearchMatch)
		}
		if m.contentSearchIdx >= 0 && m.contentSearchIdx < len(m.contentSearchMatches) {
			styleLine(m.contentSearchMatches[m.contentSearchIdx], m.styles.ContentFocusLine)
		}
	}

	if hasSelection {
		for lineIdx := range m.contentLines {
			if m.contentLineSelected(lineIdx) {
				styleLine(lineIdx, m.styles.ContentFocusLine)
			}
		}
	}

	if hasFocus {
		styleLine(m.contentFocusLine, m.styles.ContentFocusLine)
	}

	return strings.Join(lines, "\n")
}

func (m *Model) syncContentLinks(msg db.Message) {
	if !m.actionableLinksEnabled() {
		m.contentLinks = nil
		m.contentLinkIdx = -1
		return
	}

	links := extractActionableLinks(msg.BodyText, "")
	if msg.BodyHTML != "" {
		links = mergeActionableLinks(links, extractActionableLinksFromHTML(msg.BodyHTML, ""))
	}
	if len(links) == 0 {
		m.contentLinks = nil
		m.contentLinkIdx = -1
		return
	}

	if cur, ok := m.currentContentLink(); ok {
		for i, link := range links {
			if link == cur {
				m.contentLinks = links
				m.contentLinkIdx = i
				return
			}
		}
	}

	m.contentLinks = links
	m.contentLinkIdx = 0
}

func (m *Model) syncThreadContentLinks(thread messageThread) {
	if !m.actionableLinksEnabled() {
		m.contentLinks = nil
		m.contentLinkIdx = -1
		return
	}
	var links []string
	for _, msg := range thread.Messages {
		msgLinks := extractActionableLinks(msg.BodyText, "")
		if msg.BodyHTML != "" {
			msgLinks = mergeActionableLinks(msgLinks, extractActionableLinksFromHTML(msg.BodyHTML, ""))
		}
		links = mergeActionableLinks(links, msgLinks)
	}
	if len(links) == 0 {
		m.contentLinks = nil
		m.contentLinkIdx = -1
		return
	}
	if cur, ok := m.currentContentLink(); ok {
		for i, link := range links {
			if link == cur {
				m.contentLinks = links
				m.contentLinkIdx = i
				return
			}
		}
	}
	m.contentLinks = links
	m.contentLinkIdx = 0
}

func mergeActionableLinks(primary, secondary []string) []string {
	if len(primary) == 0 {
		return secondary
	}
	if len(secondary) == 0 {
		return primary
	}
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	merged := make([]string, 0, len(primary)+len(secondary))
	for _, link := range append(primary, secondary...) {
		if link == "" {
			continue
		}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}
		merged = append(merged, link)
	}
	return merged
}

func (m *Model) stepContentLink(delta int) {
	if len(m.contentLinks) == 0 {
		m.contentLinkIdx = -1
		return
	}
	if m.contentLinkIdx < 0 {
		m.contentLinkIdx = 0
	}
	m.contentLinkIdx = (m.contentLinkIdx + delta + len(m.contentLinks)) % len(m.contentLinks)
}
