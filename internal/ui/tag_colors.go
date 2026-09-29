package ui

// The annotation tag color editor (Settings → Appearance → Annotation Tags →
// Tag colors). Each semantic key has an optional user foreground and
// background; anything unset follows the theme. Changes are validated,
// saved to config at once, and apply to the message list immediately.

import (
	"maps"
	"strings"

	"github.com/allisonhere/tidemail/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// tagColorEditor is the editor's state on the Model.
type tagColorEditor struct {
	cursor int
	origin overlayMode
}

// tagColorLabel names a key for people.
func tagColorLabel(key string) string {
	switch key {
	case tagKeyReply:
		return "Needs reply"
	case tagKeyUrgent:
		return "Urgency"
	case tagKeyImportant:
		return "Importance"
	case tagKeyCategory:
		return "Category default"
	}
	return "Category: " + strings.TrimPrefix(key, tagKeyCategory+".")
}

// sampleTag is the tag previewed for a key.
func sampleTag(key string) annotationTag {
	switch key {
	case tagKeyReply:
		return annotationTag{kind: tagReply}
	case tagKeyUrgent:
		return annotationTag{kind: tagUrgent}
	case tagKeyImportant:
		return annotationTag{kind: tagImportant}
	case tagKeyCategory:
		return annotationTag{kind: tagCategory, category: "other"}
	}
	return annotationTag{kind: tagCategory, category: strings.TrimPrefix(key, tagKeyCategory+".")}
}

func (m Model) openTagColors(origin overlayMode) (tea.Model, tea.Cmd) {
	m.tagColors = tagColorEditor{origin: origin}
	m.overlay = overlayTagColors
	return m, nil
}

// setTagColor stores one override field (value "" removes it) and saves.
func (m *Model) setTagColor(key, field, value string) {
	colors := maps.Clone(m.cfg.Display.TagColors)
	if colors == nil {
		colors = map[string]config.TagColor{}
	}
	c := colors[key]
	switch field {
	case "fg":
		c.Fg = value
	case "bg":
		c.Bg = value
	}
	if c == (config.TagColor{}) {
		delete(colors, key)
	} else {
		colors[key] = c
	}
	if len(colors) == 0 {
		colors = nil
	}
	m.cfg.Display.TagColors = colors
	_ = m.saveConfig()
}

func (m Model) handleTagColorsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := &m.tagColors
	keys := tagColorKeys()
	key := keys[clamp(e.cursor, 0, len(keys)-1)]
	switch {
	case keyMatches(msg, m.keys.Cancel, m.keys.Back):
		m.overlay = e.origin
	case keyMatches(msg, m.keys.Up):
		e.cursor = max(0, e.cursor-1)
	case keyMatches(msg, m.keys.Down):
		e.cursor = min(len(keys)-1, e.cursor+1)
	case msg.String() == "b", msg.String() == "f", keyMatches(msg, m.keys.Confirm):
		field := "bg"
		if msg.String() == "f" {
			field = "fg"
		}
		resolved := m.tagColorFor(key)
		hex := string(resolved.bg)
		if field == "fg" {
			hex = string(resolved.fg)
		}
		r, g, b, _ := parseHexRGB(hex)
		return m.openColorPicker(overlayTagColors, key, field, r, g, b)
	case msg.String() == "r":
		m.setTagColor(key, "fg", "")
		m.setTagColor(key, "bg", "")
	}
	return m, nil
}

func (m Model) renderTagColors() string {
	winW := max(1, min(m.width-4, 72))
	chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
	e := m.tagColors
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	text := base.Foreground(chrome.text)
	muted := base.Foreground(chrome.muted)
	bodyW := max(1, winW-4)

	style := tagStyleLabels[max(0, indexOf(tagStyleValues, m.tagStyle()))]
	lines := []string{
		muted.Render(truncate("Plugins supply meaning; these colors are yours. Unset colors follow the theme.", bodyW)),
		muted.Render(truncate("Tag style: "+style+" (change under Appearance)", bodyW)),
		"",
	}
	keys := tagColorKeys()
	icons := m.iconsEnabled() && !m.styles.PlainUI
	for i, key := range keys {
		user := m.cfg.Display.TagColors[key]
		resolved := m.tagColorFor(key)
		chip := chipFor(sampleTag(key), config.TagStylePills, tagWide, icons)
		chip.ends = m.tagEnds()
		preview := m.renderChips([]tagChip{chip}, base)
		source := "theme"
		if _, ok := config.NormalizeTagHex(user.Bg); ok {
			source = "custom"
		} else if _, ok := config.NormalizeTagHex(user.Fg); ok {
			source = "custom"
		}
		label := padRight(truncate(tagColorLabel(key), 22), 22)
		detail := "bg " + string(resolved.bg) + "  fg " + string(resolved.fg) + "  " + source
		row := text.Render(label) + base.Render(" ") + preview + base.Render(" ")
		row += muted.Render(truncate(detail, max(1, bodyW-2-lipgloss.Width(row))))
		lines = append(lines, softRail(chrome, i == e.cursor, chrome.baseBg)+row)
	}
	pairs := []string{"↑↓", "choose", "b", "background", "f", "foreground", "r", "reset", "esc", "back"}
	body := lipgloss.NewStyle().Background(chrome.baseBg).Width(winW).Padding(1, 2).Render(strings.Join(lines, "\n"))
	hints := renderSoftHints(winW, chrome, pairs...)
	inner := lipgloss.JoinVertical(lipgloss.Left, body, hints)
	inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
	return renderSoftPanelBox(inner, winW, "tidemail", "tag colors", chrome)
}

func indexOf(list []string, v string) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}
