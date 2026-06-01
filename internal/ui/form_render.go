package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func formLabelWidth(width int) int {
	if width < 34 {
		return max(10, width/2)
	}
	return min(28, max(22, width/2+4))
}

func renderFormGroupTitle(label string, width int, chrome managerChrome) string {
	marker := "━"
	rule := "─"
	if chrome.plainUI {
		marker = "="
		rule = "-"
	}
	titleText := strings.ToUpper(strings.TrimSpace(label))
	title := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted).
		Bold(true).
		Render(marker + " " + truncate(titleText, max(1, width-2)))
	row := title
	ruleW := max(0, width-lipgloss.Width(row)-1)
	if ruleW > 0 {
		row += lipgloss.NewStyle().Background(chrome.baseBg).Render(" ")
		row += lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.muted).
			Render(strings.Repeat(rule, ruleW))
	}
	if gap := width - lipgloss.Width(row); gap > 0 {
		row += lipgloss.NewStyle().Background(chrome.baseBg).Render(strings.Repeat(" ", gap))
	}
	return row
}

func renderFormRow(label string, focused bool, control string, width, labelW int, chrome managerChrome) string {
	rowBg := chrome.baseBg
	labelFg := chrome.muted
	if focused {
		labelFg = chrome.text
	}
	markerCell := lipgloss.NewStyle().
		Background(rowBg).
		Width(2).
		Render(" ")
	if focused {
		markerCell = lipgloss.NewStyle().
			Background(rowBg).
			Foreground(chrome.accent).
			Bold(true).
			Width(2).
			Render(" >")
	}
	labelW = min(labelW, max(1, width-lipgloss.Width(markerCell)-1))
	controlW := max(1, width-lipgloss.Width(markerCell)-labelW)
	labelCell := lipgloss.NewStyle().
		Background(rowBg).
		Foreground(labelFg).
		Width(labelW).
		Render(truncate(label, max(1, labelW-1)))
	control = truncateStyled(control, controlW, chrome.baseBg)
	controlCell := padStyled(control, controlW, rowBg)
	return markerCell + labelCell + controlCell
}

func padStyled(s string, width int, bg lipgloss.Color) string {
	s = truncateStyled(s, width, bg)
	if gap := width - lipgloss.Width(s); gap > 0 {
		s += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", gap))
	}
	return s
}

func renderFormControlRow(control string, width int, chrome managerChrome, focused bool) string {
	var markerCell string
	if focused {
		markerCell = lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.accent).
			Bold(true).
			Width(2).
			Render(" >")
	} else {
		markerCell = lipgloss.NewStyle().
			Background(chrome.baseBg).
			Width(2).
			Render(" ")
	}
	controlW := max(1, width-lipgloss.Width(markerCell))
	control = truncateStyled(control, controlW, chrome.baseBg)
	controlCell := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Width(controlW).
		Render(control)
	return markerCell + controlCell
}

func renderInsetControl(control string, width, inset int, chrome managerChrome) string {
	if width <= 0 {
		return ""
	}
	if inset < 0 {
		inset = 0
	}
	maxInset := max(0, (width-1)/2)
	if inset > maxInset {
		inset = maxInset
	}
	left := lipgloss.NewStyle().Background(chrome.baseBg).Render(strings.Repeat(" ", inset))
	rightW := inset
	innerW := max(1, width-inset-rightW)
	control = truncateStyled(control, innerW, chrome.baseBg)
	controlCell := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Width(innerW).
		Render(control)
	right := lipgloss.NewStyle().Background(chrome.baseBg).Render(strings.Repeat(" ", rightW))
	return left + controlCell + right
}

// inputViewWithCursor renders input.View() with a reliably visible block cursor.
//
// bubbles v1.0.0 draws the textinput cursor with reverse-video, which renders
// inconsistently across terminal emulators, and on an *empty* field it routes through
// placeholderView() which forces the cursor to the dim placeholder style so it vanishes
// entirely. We render the cursor through its TextStyle (the non-reverse "blinked-off"
// style) set to a solid block; on an empty field we keep the placeholder hint but
// prepend our own block so the cursor is always visible. The result is one solid,
// non-blinking block in every terminal. Callers set input.Cursor.Style to the desired
// block colours first.
func inputViewWithCursor(input textinput.Model, focused bool) string {
	if !focused {
		input.Cursor.SetMode(cursor.CursorHide)
		return input.View()
	}
	block := input.Cursor.Style
	input.Cursor.TextStyle = block
	input.Cursor.Blink = true // render the block via TextStyle instead of reverse-video
	if input.Value() == "" {
		return block.Render(" ") + input.View()
	}
	return input.View()
}

func renderTextInput(input textinput.Model, width int, focused, masked bool, chrome managerChrome) string {
	if width <= 0 {
		return ""
	}
	input.Width = width
	if focused {
		input.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))
	}
	if masked {
		input.EchoMode = textinput.EchoPassword
	}
	bg := chrome.fieldBg
	if focused {
		bg = chrome.highlight
	}
	view := truncateStyled(inputViewWithCursor(input, focused), width, bg)
	return lipgloss.NewStyle().
		Background(bg).
		Foreground(chrome.text).
		Width(width).
		Render(view)
}

func renderFormHintLines(text string, width int, chrome managerChrome) []string {
	text = strings.TrimRight(text, " \t\r\n")
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return []string{lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")}
	}
	indent := text[:len(text)-len(strings.TrimLeft(text, " \t"))]
	prefix := "  "
	if !chrome.plainUI {
		prefix = "  · "
	}
	firstPrefix := indent + prefix
	contPrefix := strings.Repeat(" ", lipgloss.Width(firstPrefix))
	textW := max(1, width-lipgloss.Width(firstPrefix))
	lines := wrapShellCommand(trimmed, textW)
	style := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted).
		Width(width)
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		linePrefix := firstPrefix
		if i > 0 {
			linePrefix = contPrefix
		}
		out = append(out, style.Render(linePrefix+line))
	}
	return out
}

func renderFormInlineStatus(text string, width int, chrome managerChrome) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")
	}
	return lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted).
		Width(width).
		Render(truncate(text, width))
}

func renderFormBadge(text string, focused bool, chrome managerChrome) string {
	text = strings.ToUpper(strings.TrimSpace(text))
	badgeW := max(7, lipgloss.Width(text)+2)
	if focused {
		return renderFormBadgeStyled(text, badgeW, chrome.highlight, chrome.highlightFg, true)
	}
	return renderFormBadgeStyled(text, badgeW, chrome.accent, chrome.accentFg, false)
}

func renderFormBadgeStyled(text string, width int, bg, fg lipgloss.Color, bold bool) string {
	return lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		Bold(bold).
		Width(width).
		Align(lipgloss.Center).
		Render(text)
}

func truncateStyled(s string, width int, bg lipgloss.Color) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	// ansi.Truncate is SGR-aware, so styled cells — notably the textinput block cursor,
	// which bubbles renders one cell past the field width — survive the clamp. A plain
	// strip-and-re-render here would flatten every colour and drop the cursor entirely.
	return lipgloss.NewStyle().Background(bg).Render(ansi.Truncate(s, width, ""))
}
