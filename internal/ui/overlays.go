package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderOverlay(base string) string {
	var box string

	switch m.overlay {
	case overlayQuitConfirm:
		quitW := 40
		qt := m.styles.Theme
		chrome := newManagerChrome(quitW, qt, m.styles.PlainUI)
		header := renderManagerHeader("QUIT TIDE?", quitW, chrome)
		body := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.text).
			Width(quitW).
			Padding(1, 2).
			Render("Exit Tide now?")
		actions := renderManagerActions(quitW, chrome,
			"y", "quit",
			"esc", "cancel",
		)
		inner := lipgloss.JoinVertical(lipgloss.Left, header, body, actions)
		inner = clampView(inner, quitW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, quitW, chrome, chrome.accent)

	case overlayDraftCloseConfirm:
		winW := 48
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		header := renderManagerHeader("SAVE DRAFT?", winW, chrome)
		body := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.text).
			Width(winW).
			Padding(1, 2).
			Render("Save this draft before closing compose?")
		actions := renderManagerActions(winW, chrome,
			"y/enter", "save",
			"d", "discard",
			"esc", "cancel",
		)
		inner := lipgloss.JoinVertical(lipgloss.Left, header, body, actions)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayBulkDeleteConfirm:
		winW := 48
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		header := renderManagerHeader("DELETE MESSAGES?", winW, chrome)
		count := len(m.pendingBulkDelete)
		body := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.text).
			Width(winW).
			Padding(1, 2).
			Render(fmt.Sprintf("Delete %d selected messages?", count))
		actions := renderManagerActions(winW, chrome,
			"y/enter", "delete",
			"esc", "cancel",
		)
		inner := lipgloss.JoinVertical(lipgloss.Left, header, body, actions)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlaySearch:
		winW := min(m.width-4, 52)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderSearchOverlay(winW, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayThemePicker:
		winW := min(m.width-4, 40)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderThemePicker(winW, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayAccountManager:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 40)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.accountManager.View(winW, winH, m.styles)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayContactManager:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 40)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.contactManager.View(winW, winH, m.styles)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayCompose:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 36)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.compose.View(winW, winH, m.styles)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlaySaveAttach:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 36)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderSaveAttachPicker(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayMoveMessage:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 36)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderMovePicker(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayFilterManager:
		winW := min(m.width-4, 78)
		winH := min(m.height-4, 32)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderFilterManager(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayGrammarPreview:
		winW := min(m.width-10, 70)
		winH := min(m.height-6, 24)
		box = m.renderGrammarPreview(winW, winH)

	case overlayLogViewer:
		winW := min(m.width-6, 90)
		winH := min(m.height-4, 38)
		box = m.renderLogViewer(winW, winH)

	case overlayHelp:
		winW := min(m.width-6, 90)
		winH := min(m.height-4, 38)
		t := m.styles.Theme
		surface := modalSurface(t)
		border := t.OverlayBorder
		if border == "" {
			border = t.BorderFocus
		}
		m.helpVP.Style = lipgloss.NewStyle().Background(surface)
		footer := m.styles.OverlayHint.
			MarginTop(1).
			Width(max(1, winW-1)).
			Padding(0, 1, 0, 4).
			Render("[esc/?/q] close  [j/k/↑↓] scroll")
		box = lipgloss.NewStyle().
			Background(surface).
			Border(lipPaneBorder(m.styles.PlainUI)).
			BorderForeground(border).
			Width(winW).Height(winH).
			Render(m.helpVP.View() + "\n" + footer)

	case overlaySettings:
		winW := min(m.width-4, 62)
		winH := min(m.height-4, 36)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.settings.View(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayUpdateConfirm:
		winW := min(m.width-8, 72)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderUpdateConfirmOverlay(winW, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlaySummary:
		winW := min(m.width-8, 76)
		winH := min(m.height-6, 20)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderSummaryOverlay(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayCommandPalette:
		winW := min(m.width-6, 72)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderCommandPalette(winW, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)
	}

	return overlayOnBase(base, box, m.width, m.height, m.styles.Theme.Bg)
}

func (m Model) renderSearchOverlay(width int, chrome managerChrome) string {
	header := renderManagerHeader("SEARCH MESSAGES", width, chrome)
	input := m.searchInput
	inputW := max(1, width-4)
	input.Width = inputW
	input.PromptStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.accent).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
	input.PlaceholderStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
	input.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))
	input.Cursor.TextStyle = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))

	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(1, 2, 0, 2).
		Render(inputViewWithCursor(input, true))
	hint := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted).
		Width(width).
		Padding(0, 2, 0, 2).
		Render("esc  cancel    enter  apply")
	actions := renderManagerActions(width, chrome, "enter", "apply", "esc", "clear")
	return lipgloss.JoinVertical(lipgloss.Left, header, body, hint, actions)
}

func (m Model) renderCommandPalette(width int, chrome managerChrome) string {
	header := renderManagerHeader("COMMAND", width, chrome)
	input := m.commandInput
	inputW := max(1, width-4)
	input.Width = inputW
	input.PromptStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.accent).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
	input.PlaceholderStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
	input.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))
	input.Cursor.TextStyle = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))

	items := m.filteredCommandItems()
	rows := []string{inputViewWithCursor(input, true), ""}
	if len(items) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(chrome.muted).Render("No commands"))
	} else {
		limit := min(8, len(items))
		start := 0
		if m.commandCursor >= limit {
			start = m.commandCursor - limit + 1
		}
		for i := start; i < min(start+limit, len(items)); i++ {
			item := items[i]
			prefix := "  "
			style := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
			if !item.enabled {
				style = style.Foreground(chrome.muted)
			}
			if i == m.commandCursor {
				prefix = "> "
				style = style.Background(chrome.accent).Foreground(contrastFg(chrome.accent)).Bold(true)
			}
			rows = append(rows, style.Width(max(1, width-4)).Render(truncate(prefix+item.label, max(1, width-4))))
		}
	}
	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(1, 2).
		Render(strings.Join(rows, "\n"))
	actions := renderManagerActions(width, chrome, "enter", "run", "esc", "close")
	return lipgloss.JoinVertical(lipgloss.Left, header, body, actions)
}

func (m Model) renderSummaryOverlay(width, height int, chrome managerChrome) string {
	header := renderManagerHeader("AI SUMMARY", width, chrome)

	var bodyText string
	switch {
	case m.summaryGenerating:
		bodyText = m.spinner.View() + " Generating summary…"
	case m.summaryErr != "":
		bodyText = "Error: " + m.summaryErr
	default:
		bodyText = formatSummaryBody(m.summaryMessage.Summary, width-4, m.styles.PlainUI)
	}

	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(1, 2).
		Render(bodyText)

	var hints string
	if !m.summaryGenerating && m.summaryErr == "" && m.summaryMessage.Summary != "" {
		provider := ""
		if m.summarizer != nil {
			prefix := "  ·  "
			if m.styles.PlainUI {
				prefix = " | "
			}
			provider = prefix + m.summarizer.ProviderName()
		}
		providerLine := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.muted).
			Width(width).
			Padding(0, 2).
			Render(provider)
		hints = lipgloss.JoinVertical(lipgloss.Left,
			providerLine,
			renderManagerActions(width, chrome, "c", "copy", "M", "save .md", "esc", "close"),
		)
	} else {
		hints = renderManagerActions(width, chrome, "esc", "close")
	}

	bodyH := max(1, height-lipgloss.Height(header)-lipgloss.Height(hints))
	body = clampView(body, width, bodyH, chrome.baseBg)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, hints)
}

func (m Model) renderUpdateConfirmOverlay(width int, chrome managerChrome) string {
	header := renderManagerHeader("INSTALL TIDE UPDATE?", width, chrome)

	target, _ := os.Executable()
	bodyLines := []string{
		"Install Tide " + m.updateInfo.Version + "?",
	}
	if summary := strings.TrimSpace(m.updateInfo.Summary); summary != "" {
		bodyLines = append(bodyLines, "", "What's new: "+summary)
	}
	bodyLines = append(bodyLines,
		"",
		"Asset: "+m.updateInfo.AssetName+".tar.gz",
		"Target: "+target,
		"",
		"The update will download first, then replace the current binary if the install path is writable.",
	)
	bodyText := strings.Join(bodyLines, "\n")

	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(1, 2, 0, 2).
		Render(bodyText)

	note := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted).
		Width(width).
		Padding(0, 2, 1, 2).
		Render("Also available in Settings > Updates")

	actions := renderManagerActions(width, chrome, "enter", "install", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, body, note, actions)
}

func (m Model) renderGrammarPreview(width, height int) string {
	t := m.styles.Theme
	bg := modalSurface(t)
	border := t.OverlayBorder
	if border == "" {
		border = t.BorderFocus
	}

	title := lipgloss.NewStyle().Background(bg).Foreground(t.BorderFocus).Bold(true).Width(width).Padding(0, 1).Render("Grammar Preview")
	correctedText := m.grammarCorrected
	if correctedText == "" {
		correctedText = "(no changes)"
	}

	bodyW := max(1, width-4)
	bodyStyle := lipgloss.NewStyle().Background(bg).Foreground(t.Fg).Width(bodyW)
	body := bodyStyle.Render(correctedText)

	hints := lipgloss.NewStyle().Background(bg).Foreground(readableText(t.Dimmed, bg, 3.0)).Width(width).Padding(0, 1).Render("y  accept    n  cancel")

	return lipgloss.NewStyle().
		Background(bg).
		Border(lipPaneBorder(m.styles.PlainUI)).
		BorderForeground(border).
		Width(width).Height(height).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", hints))
}

func (m Model) renderLogViewer(width, height int) string {
	t := m.styles.Theme
	bg := modalSurface(t)
	border := t.OverlayBorder
	if border == "" {
		border = t.BorderFocus
	}

	// Build log lines
	var logLines []string
	vpWidth := max(1, width-4)
	for _, entry := range m.logBuffer {
		timeStr := entry.Time.Format("15:04:05")
		fg := readableText(t.Dimmed, bg, 3.0)
		if entry.IsError {
			fg = t.Error
		}
		line := lipgloss.NewStyle().
			Background(bg).
			Foreground(fg).
			Width(vpWidth).
			Render(fmt.Sprintf("%-8s %s", timeStr, entry.Message))
		logLines = append(logLines, line)
	}
	if len(logLines) == 0 {
		logLines = []string{lipgloss.NewStyle().Background(bg).Foreground(readableText(t.Dimmed, bg, 3.0)).Width(vpWidth).Render("(no log entries yet)")}
	}

	// Reverse so newest first
	for i, j := 0, len(logLines)-1; i < j; i, j = i+1, j-1 {
		logLines[i], logLines[j] = logLines[j], logLines[i]
	}

	m.helpVP.SetContent(strings.Join(logLines, "\n"))
	m.helpVP.Style = lipgloss.NewStyle().Background(bg)
	m.helpVP.Width = vpWidth
	m.helpVP.Height = max(1, height-4)

	// Viewport view
	title := lipgloss.NewStyle().Background(bg).Foreground(t.BorderFocus).Bold(true).Width(width).Padding(0, 1).Render(fmt.Sprintf("LOGS (%d)", len(logLines)))
	footer := lipgloss.NewStyle().Background(bg).Foreground(readableText(t.Dimmed, bg, 3.0)).Width(width).Padding(0, 1).Render("[esc] close  [↑↓/j/k] scroll")

	return lipgloss.NewStyle().
		Background(bg).
		Border(lipPaneBorder(m.styles.PlainUI)).
		BorderForeground(border).
		Width(width).Height(height).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, m.helpVP.View(), footer))
}

func (m Model) renderThemePicker(width int, chrome managerChrome) string {
	header := renderManagerHeader("THEME", width, chrome)
	rows := make([]string, 0, len(BuiltinThemes))
	for i, t := range BuiltinThemes {
		if i == m.themeCursor {
			rows = append(rows, renderManagerSelectedRow(width, m.styles.ThemePickerCursor()+t.Name, chrome, m.styles))
		} else {
			rows = append(rows, clampView(
				lipgloss.NewStyle().
					Background(chrome.baseBg).
					Foreground(chrome.text).
					Padding(0, 1).
					Render("  "+t.Name),
				width,
				1,
				chrome.baseBg,
			))
		}
	}
	body := clampView(lipgloss.JoinVertical(lipgloss.Left, rows...), width, len(rows), chrome.baseBg)
	hints := renderManagerActions(width, chrome, "enter", "confirm", "esc", "revert")
	return lipgloss.JoinVertical(lipgloss.Left, header, body, hints)
}
