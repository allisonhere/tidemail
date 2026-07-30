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
		body := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.text).
			Width(quitW).
			Padding(1, 2).
			Render("Exit Tide now?")
		hints := renderSoftHints(quitW, chrome, "y", "quit", "esc", "cancel")
		inner := lipgloss.JoinVertical(lipgloss.Left, body, hints)
		inner = clampView(inner, quitW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, quitW, "tidemail", "quit tide?", chrome)

	case overlayUnsubscribeConfirm:
		winW := 52
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		method := "opens the list's page in your browser"
		if supportsOneClickUnsubscribe(m.pendingUnsubscribe.Headers) {
			method = "one-click, handled by tidemail"
		}
		text := fmt.Sprintf("Unsubscribe via %s?\n(%s)", unsubscribeHost(m.pendingUnsubscribe), method)
		body := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.text).
			Width(winW).
			Padding(1, 2).
			Render(text)
		hints := renderSoftHints(winW, chrome, "y/enter", "unsubscribe", "esc", "cancel")
		inner := lipgloss.JoinVertical(lipgloss.Left, body, hints)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "unsubscribe?", chrome)

	case overlayDraftCloseConfirm:
		winW := 48
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		body := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.text).
			Width(winW).
			Padding(1, 2).
			Render("Save this draft before closing compose?")
		hints := renderSoftHints(winW, chrome, "y/enter", "save", "d", "discard", "esc", "cancel")
		inner := lipgloss.JoinVertical(lipgloss.Left, body, hints)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "save draft?", chrome)

	case overlayBulkDeleteConfirm:
		winW := 48
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		count := len(m.pendingBulkDelete)
		body := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.text).
			Width(winW).
			Padding(1, 2).
			Render(fmt.Sprintf("Delete %d selected messages?", count))
		hints := renderSoftHints(winW, chrome, "y/enter", "delete", "esc", "cancel")
		inner := lipgloss.JoinVertical(lipgloss.Left, body, hints)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "delete messages?", chrome)

	case overlayThemePicker:
		winW := min(m.width-4, 40)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderThemePicker(winW, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "theme", chrome)

	case overlayAccountManager:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 40)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.accountManager.View(winW, winH, m.styles)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", m.accountManager.softTitle(), chrome)

	case overlayContactManager:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 40)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.contactManager.View(winW, winH, m.styles)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", m.contactManager.softTitle(), chrome)

	case overlayCompose:
		winW := composeOverlayWidth(m.width)
		winH := min(m.height-4, 36)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.compose.View(winW, winH, m.styles)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		title := m.compose.softTitle()
		if m.compose.picker.active {
			title = "attach file"
		}
		box = renderSoftPanelBox(inner, winW, "tidemail", title, chrome)

	case overlaySaveAttach:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 36)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderSaveAttachPicker(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "save attachments", chrome)

	case overlayMoveMessage:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 36)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderMovePicker(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "move to", chrome)

	case overlayFilterManager:
		winW := min(m.width-4, 78)
		winH := min(m.height-4, 32)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderFilterManager(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "filters", chrome)

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
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		m.helpVP.Style = lipgloss.NewStyle().Background(chrome.baseBg)
		var footer string
		switch {
		case m.helpSearchActive:
			input := m.helpSearchInput
			if input.Value() == "" {
				// bubbles' placeholderView pads to Width with unstyled spaces
				// when Width is set, leaking the terminal's default background.
				// Leave padding to padStyled below, which styles it.
				input.Width = 0
			} else {
				input.Width = max(1, winW-6)
			}
			input.Prompt = "/ "
			input.PromptStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.accent).Bold(true)
			input.TextStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
			input.PlaceholderStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
			input.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(accentReadableOn(chrome.text, chrome.accent, 4.5))
			footer = padStyled("  "+inputViewWithCursor(input, true), winW, chrome.baseBg)
		case m.helpSearchInput.Value() != "":
			footer = renderSoftHints(winW, chrome, "/", "edit search", "esc", "clear", "?/q", "close", "j/k", "scroll")
		default:
			footer = renderSoftHints(winW, chrome, "/", "search", "esc/?/q", "close", "j/k", "scroll")
		}
		inner := m.helpVP.View() + "\n\n" + footer
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "help", chrome)

	case overlaySettings:
		winW := min(m.width-4, 62)
		winH := min(m.height-4, 36)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.settings.View(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "settings", chrome)

	case overlayUpdateConfirm:
		winW := min(m.width-8, 72)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderUpdateConfirmOverlay(winW, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "install update?", chrome)

	case overlaySummary:
		winW := min(m.width-8, 76)
		winH := min(m.height-6, 20)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderSummaryOverlay(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "ai summary", chrome)

	case overlayCommandPalette:
		winW := min(m.width-6, 72)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderCommandPalette(winW, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderSoftPanelBox(inner, winW, "tidemail", "command", chrome)
	}

	return overlayOnBase(base, box, m.width, m.height, m.styles.Theme.Bg)
}

func (m Model) renderCommandPalette(width int, chrome managerChrome) string {
	input := m.commandInput
	if input.Value() == "" {
		// bubbles' placeholderView pads to Width with unstyled spaces when
		// Width is set, leaking the terminal's default background. Leave
		// padding to the outer Width(width) wrap below, which styles it.
		input.Width = 0
	} else {
		input.Width = max(1, width-4)
	}
	input.PromptStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.accent).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
	input.PlaceholderStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
	input.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(accentReadableOn(chrome.text, chrome.accent, 4.5))
	input.Cursor.TextStyle = lipgloss.NewStyle().Background(chrome.accent).Foreground(accentReadableOn(chrome.text, chrome.accent, 4.5))

	items := m.filteredCommandItems()
	rows := []string{inputViewWithCursor(input, true), ""}
	if len(items) == 0 {
		rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render("No commands"))
	} else {
		limit := min(8, len(items))
		start := 0
		if m.commandCursor >= limit {
			start = m.commandCursor - limit + 1
		}
		for i := start; i < min(start+limit, len(items)); i++ {
			item := items[i]
			selected := i == m.commandCursor
			fg := chrome.text
			if !item.enabled {
				fg = chrome.muted
			} else if selected {
				fg = chrome.text
			}
			labelW := max(1, width-4-2) // minus the 2-cell rail
			label := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Render(" " + truncate(item.label, max(1, labelW-1)))
			rows = append(rows, softRail(chrome, selected, chrome.baseBg)+padStyled(label, labelW, chrome.baseBg))
		}
	}
	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(1, 2).
		Render(strings.Join(rows, "\n"))
	hints := renderSoftHints(width, chrome, "enter", "run", "esc", "close")
	return lipgloss.JoinVertical(lipgloss.Left, body, hints)
}

func (m Model) renderSummaryOverlay(width, height int, chrome managerChrome) string {
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
			renderSoftHints(width, chrome, "c", "copy", "M", "save .md", "esc", "close"),
		)
	} else {
		hints = renderSoftHints(width, chrome, "esc", "close")
	}

	bodyH := max(1, height-lipgloss.Height(hints)-1)
	body = clampView(body, width, bodyH, chrome.baseBg)
	return lipgloss.JoinVertical(lipgloss.Left, body, hints)
}

func (m Model) renderUpdateConfirmOverlay(width int, chrome managerChrome) string {
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

	hints := renderSoftHints(width, chrome, "enter", "install", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, body, note, hints)
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
	labelW := max(1, width-2) // minus the 2-cell rail
	rows := make([]string, 0, len(BuiltinThemes))
	for i, t := range BuiltinThemes {
		selected := i == m.themeCursor
		label := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Render(" " + truncate(t.Name, max(1, labelW-1)))
		rows = append(rows, softRail(chrome, selected, chrome.baseBg)+padStyled(label, labelW, chrome.baseBg))
	}
	body := clampView(lipgloss.JoinVertical(lipgloss.Left, rows...), width, len(rows), chrome.baseBg)
	hints := renderSoftHints(width, chrome, "enter", "confirm", "esc", "revert")
	return lipgloss.JoinVertical(lipgloss.Left, body, hints)
}
