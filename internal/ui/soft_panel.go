package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Soft-panel chrome: the restyled overlay look used by Settings and the Account
// Manager — rounded border with the title embedded in the top run, a left accent
// rail for focus instead of highlight blocks, quiet lowercase hint footers.
// Other overlays still use the renderManager*/renderForm* chrome and can migrate
// here later.

func (c managerChrome) softChevrons() string {
	if c.plainUI {
		return "<>"
	}
	return "‹›"
}

// softDropdownGlyph is the affordance for a control that expands into a list of
// rows below it: ▾ while closed, ▴ while the list is open. Distinct from
// softChevrons, which means "cycle this value in place".
func (c managerChrome) softDropdownGlyph(open bool) string {
	if c.plainUI {
		if open {
			return "^"
		}
		return "v"
	}
	if open {
		return "▴"
	}
	return "▾"
}

func (c managerChrome) softToggleGlyph(on bool) string {
	if c.plainUI {
		if on {
			return "(x)"
		}
		return "( )"
	}
	if on {
		return "●"
	}
	return "○"
}

// softRail renders the 2-cell focus marker column: an accent rail when active,
// blank otherwise. bg lets selected surfaces (account cards) keep their fill.
func softRail(chrome managerChrome, active bool, bg lipgloss.Color) string {
	glyph := "▌ "
	if chrome.plainUI {
		glyph = "> "
	}
	if !active {
		return lipgloss.NewStyle().Background(bg).Width(2).Render("")
	}
	return lipgloss.NewStyle().
		Background(bg).
		Foreground(chrome.accent).
		Bold(true).
		Width(2).
		Render(glyph)
}

// renderSoftPanelBox wraps inner (already padded to width) in a rounded border
// whose top run carries the title: ╭─ prefix · title ────╮.
func renderSoftPanelBox(inner string, width int, prefix, title string, chrome managerChrome) string {
	if chrome.plainUI {
		titleLine := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render(" "+prefix+" · ") +
			lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.accent).Bold(true).Render(title)
		titleLine = padStyled(titleLine, width, chrome.baseBg)
		return renderChromeOverlayBox(titleLine+"\n"+inner, width, chrome, chrome.border)
	}

	borderStyle := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.border)
	// Top line spans width+2 cells (the side borders). Truncate the title before
	// computing the trailing rule so long titles can't overflow the run.
	label := prefix + " · "
	title = ansi.Truncate(title, max(1, width-lipgloss.Width(label)-6), "…")
	top := borderStyle.Render("╭─ ") +
		lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render(label) +
		lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.accent).Bold(true).Render(title) +
		borderStyle.Render(" ")
	gap := max(0, width+2-lipgloss.Width(top)-1)
	top += borderStyle.Render(strings.Repeat("─", gap) + "╮")

	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Border(lipgloss.RoundedBorder(), false, true, true, true).
		BorderForeground(chrome.border).
		BorderBackground(chrome.baseBg).
		Width(width).
		Render(inner)
	return top + "\n" + body
}

// renderSoftRow is the soft-panel sibling of renderFormRow: same column math,
// but focus is a rail instead of " >" and rows never get a highlight fill.
func renderSoftRow(label string, focused bool, control string, width, labelW int, chrome managerChrome) string {
	labelFg := chrome.muted
	if focused {
		labelFg = chrome.text
	}
	marker := softRail(chrome, focused, chrome.baseBg)
	labelW = min(labelW, max(1, width-lipgloss.Width(marker)-1))
	controlW := max(1, width-lipgloss.Width(marker)-labelW)
	labelCell := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(labelFg).
		Width(labelW).
		Render(truncate(label, max(1, labelW-1)))
	control = truncateStyled(control, controlW, chrome.baseBg)
	return marker + labelCell + padStyled(control, controlW, chrome.baseBg)
}

func renderSoftToggle(on, focused bool, chrome managerChrome) string {
	word := "off"
	glyphFg := chrome.muted
	wordFg := chrome.muted
	if on {
		word = "on"
		glyphFg = chrome.accent
		wordFg = chrome.text
	}
	if focused && !on {
		wordFg = chrome.text
	}
	glyph := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(glyphFg).Render(chrome.softToggleGlyph(on))
	return glyph + lipgloss.NewStyle().Background(chrome.baseBg).Foreground(wordFg).Render(" "+word)
}

// renderSoftPicker renders a picker value with the trailing ‹› affordance
// pinned at the right edge of width.
func renderSoftPicker(width int, value string, focused bool, chrome managerChrome) string {
	chevrons := chrome.softChevrons()
	chevronW := lipgloss.Width(chevrons) + 1 // trailing space
	valueFg := chrome.muted
	valueBg := chrome.baseBg
	chevronFg := chrome.muted
	if focused {
		valueFg = chrome.text
		valueBg = chrome.fieldBg
		chevronFg = chrome.accent
	}
	value = " " + truncate(value, max(1, width-chevronW-3)) + " "
	left := lipgloss.NewStyle().Background(valueBg).Foreground(valueFg).Render(value)
	gap := max(1, width-lipgloss.Width(left)-chevronW)
	return left +
		lipgloss.NewStyle().Background(chrome.baseBg).Render(strings.Repeat(" ", gap)) +
		lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chevronFg).Bold(focused).Render(chevrons) +
		lipgloss.NewStyle().Background(chrome.baseBg).Render(" ")
}

// renderSoftDropdown renders a value with a vertical chevron — ▾ closed, ▴ while
// the list is open — pinned to the right edge of width, for controls that expand
// into a list of rows below them instead of cycling in place.
//
// Unlike renderSoftPicker it adds no padding of its own: the value is flush left
// so the caller can wrap it in renderInsetControl at the same inset as the
// neighbouring text inputs and have the two line up in the same columns.
func renderSoftDropdown(width int, value string, focused, open bool, chrome managerChrome) string {
	glyph := chrome.softDropdownGlyph(open)
	glyphW := lipgloss.Width(glyph)
	valueFg := chrome.muted
	valueBg := chrome.baseBg
	glyphFg := chrome.muted
	if focused {
		valueFg = chrome.text
		valueBg = chrome.fieldBg
		glyphFg = chrome.accent
	}
	left := lipgloss.NewStyle().Background(valueBg).Foreground(valueFg).
		Render(truncate(value, max(1, width-glyphW-1)))
	gap := max(1, width-lipgloss.Width(left)-glyphW)
	return left +
		lipgloss.NewStyle().Background(chrome.baseBg).Render(strings.Repeat(" ", gap)) +
		lipgloss.NewStyle().Background(chrome.baseBg).Foreground(glyphFg).Bold(focused).Render(glyph)
}

// renderSoftGroupTitle renders a group heading as a plain accent word — no rule.
func renderSoftGroupTitle(label string, width int, chrome managerChrome) string {
	title := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.accent).
		Render("  " + truncate(strings.TrimSpace(label), max(1, width-2)))
	return padStyled(title, width, chrome.baseBg)
}

// renderSoftHints renders the footer as one quiet lowercase line:
// "↑↓ move   ‹› change   ^s save   esc close". No border, no badges.
func renderSoftHints(width int, chrome managerChrome, pairs ...string) string {
	key := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
	label := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
	gap := lipgloss.NewStyle().Background(chrome.baseBg).Render("   ")
	parts := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, key.Render(strings.ToLower(pairs[i]))+label.Render(" "+strings.ToLower(pairs[i+1])))
	}
	line := lipgloss.NewStyle().Background(chrome.baseBg).Render("  ") + strings.Join(parts, gap)
	return padStyled(line, width, chrome.baseBg)
}
