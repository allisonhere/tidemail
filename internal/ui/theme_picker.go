package ui

// The theme picker (T): themes grouped by family, a type-to-filter search, a
// palette swatch beside each variant and a sample of the highlighted theme.
// Moving the cursor previews the theme across the whole app; Enter keeps it
// and writes it to config, Esc puts the confirmed theme back.

import (
	"strings"
	"unicode/utf8"

	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// themePickerEntry is one line of the picker: a family header or a theme row.
type themePickerEntry struct {
	header string // family name, for a header line
	idx    int    // index into PickableThemes, for a theme row (-1 for headers)
	solo   bool   // the only theme of its family: shown without a header
}

func themePickerFamily(th Theme) string {
	if th.Family == "" {
		return "System"
	}
	return th.Family
}

// themeSearchText is what a filter word is matched against: the family and
// variant, the stored name, and whether the theme is light or dark.
func themeSearchText(th Theme) string {
	polarity := "dark"
	if th.IsLight() {
		polarity = "light"
	}
	return strings.ToLower(th.Family + " " + th.Variant + " " + th.Name + " " + polarity)
}

// themePickerEntries lists the themes matching every word of filter, grouped
// under a header per family.
func themePickerEntries(pt []Theme, filter string) []themePickerEntry {
	words := strings.Fields(strings.ToLower(filter))
	var matched []int
	for i, th := range pt {
		text := themeSearchText(th)
		ok := true
		for _, w := range words {
			if !strings.Contains(text, w) {
				ok = false
				break
			}
		}
		if ok {
			matched = append(matched, i)
		}
	}
	var out []themePickerEntry
	for start := 0; start < len(matched); {
		fam := themePickerFamily(pt[matched[start]])
		end := start + 1
		for end < len(matched) && themePickerFamily(pt[matched[end]]) == fam {
			end++
		}
		if end-start == 1 {
			out = append(out, themePickerEntry{idx: matched[start], solo: true})
		} else {
			out = append(out, themePickerEntry{header: fam, idx: -1})
			for _, i := range matched[start:end] {
				out = append(out, themePickerEntry{idx: i})
			}
		}
		start = end
	}
	return out
}

// themeEntryPos is the position of theme idx in entries, or -1.
func themeEntryPos(entries []themePickerEntry, idx int) int {
	for i, e := range entries {
		if e.idx == idx && e.header == "" {
			return i
		}
	}
	return -1
}

// themeSwatch draws a theme's accent, success, warning, error and tag colors
// as colored cells, so variants can be told apart before they are previewed.
func themeSwatch(th Theme, plain bool) string {
	var b strings.Builder
	for _, c := range []lipgloss.Color{th.BorderFocus, th.SuccessColor(), th.WarningColor(), th.Error, th.TagColor()} {
		cell := " "
		if plain {
			cell = "#"
		}
		b.WriteString(lipgloss.NewStyle().Background(c).Foreground(c).Render(cell))
	}
	return b.String()
}

func (m Model) renderThemePicker(width, height int, chrome managerChrome) string {
	pt := PickableThemes()
	entries := themePickerEntries(pt, m.themeFilter)
	labelW := max(1, width-2) // minus the 2-cell rail
	plain := chrome.plainUI
	bg := chrome.baseBg
	line := func(s string) string { return padStyled(s, width, bg) }

	// Search line.
	searchStyle := lipgloss.NewStyle().Background(bg).Foreground(chrome.muted)
	var search string
	switch {
	case m.themeFiltering:
		search = searchStyle.Render("  / ") + lipgloss.NewStyle().Background(bg).Foreground(chrome.text).Render(m.themeFilter+"▏")
	case m.themeFilter != "":
		search = searchStyle.Render("  / " + m.themeFilter)
	default:
		search = searchStyle.Render("  / to search")
	}

	// The sample takes three lines and a blank; the hints take one.
	sample := m.renderThemeSample(width, chrome)
	sampleH := strings.Count(sample, "\n") + 2
	listH := max(1, height-1-1-sampleH)

	cursorPos := max(0, themeEntryPos(entries, m.themeCursor))
	rows := make([]string, 0, listH)
	if len(entries) == 0 {
		rows = append(rows, line(searchStyle.Render("  no matching themes")))
	}
	first, last := tideui.VisibleRange(len(entries), cursorPos, listH)
	for pos := first; pos < last; pos++ {
		e := entries[pos]
		if e.header != "" {
			head := lipgloss.NewStyle().Background(bg).Foreground(chrome.muted).Bold(true).Render(" " + truncate(e.header, labelW-1))
			rows = append(rows, line(softRail(chrome, false, bg)+padStyled(head, labelW, bg)))
			continue
		}
		th := pt[e.idx]
		label := th.Variant
		if label == "" || e.solo {
			label = th.Label()
		}
		if th.Family == "" {
			label = th.Name
		}
		indent := "  "
		if e.solo {
			indent = " "
		}
		mark := ""
		if e.idx == m.confirmedTheme {
			mark = " ●"
			if plain {
				mark = " *"
			}
		}
		// match-omarchy takes its colors from the desktop at runtime; the
		// placeholder palette's swatch would be misleading.
		swatch := ""
		if !isMatchOmarchy(th.Name) {
			swatch = themeSwatch(th, plain)
		}
		textW := max(1, labelW-lipgloss.Width(swatch)-2)
		text := lipgloss.NewStyle().Background(bg).Foreground(chrome.text).Render(indent + truncate(label+mark, textW-len(indent)))
		cell := padStyled(text, textW, bg) + lipgloss.NewStyle().Background(bg).Render(" ") + swatch
		rows = append(rows, line(softRail(chrome, e.idx == m.themeCursor, bg)+padStyled(cell, labelW, bg)))
	}
	for len(rows) < listH {
		rows = append(rows, line(""))
	}

	hints := renderSoftHints(width, chrome, "/", "search", "enter", "confirm", "esc", "revert")
	if m.themeFiltering {
		hints = renderSoftHints(width, chrome, "type", "filter", "enter", "done", "esc", "clear")
	}
	return strings.Join([]string{search, strings.Join(rows, "\n"), line(""), sample, hints}, "\n")
}

// renderThemeSample shows the highlighted theme's colors in use: a selected
// row, body, muted, link and quote text, and the status and tag colors. The
// picker draws in the previewed theme already; this makes each role visible
// side by side.
func (m Model) renderThemeSample(width int, chrome managerChrome) string {
	th := m.styles.Theme
	bg := chrome.baseBg
	on := func(fg lipgloss.Color, s string) string {
		return lipgloss.NewStyle().Background(bg).Foreground(fg).Render(s)
	}
	gap := on(chrome.text, "  ")
	selected := padStyled(lipgloss.NewStyle().Background(chrome.highlight).Foreground(chrome.highlightFg).Bold(true).Render(" selected row"), width-2, chrome.highlight)
	text := strings.Join([]string{
		on(chrome.text, "text"), on(chrome.muted, "muted"), on(th.LinkColor(), "link"), on(messageQuoteColor(th), "│ quote"),
	}, gap)
	pill := func(c lipgloss.Color, s string) string {
		return lipgloss.NewStyle().Background(c).Foreground(contrastFg(c)).Render(" " + s + " ")
	}
	status := strings.Join([]string{
		on(chrome.successFg, "ok"), on(th.WarningColor(), "warn"), on(chrome.errorFg, "error"),
		pill(th.TagColor(), "tag"), pill(th.MatchColor(), "match"),
	}, gap)
	return strings.Join([]string{
		line2(selected, width, bg),
		padStyled(on(chrome.text, "  ")+text, width, bg),
		padStyled(on(chrome.text, "  ")+status, width, bg),
	}, "\n")
}

func line2(s string, width int, bg lipgloss.Color) string {
	return padStyled(lipgloss.NewStyle().Background(bg).Render("  ")+s, width, bg)
}

// openThemePicker shows the picker on the confirmed theme with no filter.
func (m *Model) openThemePicker() {
	m.overlay = overlayThemePicker
	m.themeCursor = m.confirmedTheme
	m.themeFilter = ""
	m.themeFiltering = false
}

// previewTheme applies theme idx across the app without saving it.
func (m *Model) previewTheme(idx int) {
	m.themeCursor = idx
	m.activeTheme = idx
	m.setStyles(BuildStyles(MergedBuiltinThemeAtIndex(m.cfg, m.activeTheme), m.cfg.Display.Density, m.cfg.Display.PaneCorners))
	if m.activeMessageRowCount() > 0 {
		m.setViewportForCurrentRow()
	}
}

// stepTheme moves the cursor delta theme rows through the visible entries,
// stopping at the ends.
func (m *Model) stepTheme(delta int) {
	entries := themePickerEntries(PickableThemes(), m.themeFilter)
	var themes []int
	for _, e := range entries {
		if e.header == "" {
			themes = append(themes, e.idx)
		}
	}
	if len(themes) == 0 {
		return
	}
	pos := -1
	for i, idx := range themes {
		if idx == m.themeCursor {
			pos = i
		}
	}
	next := max(0, min(len(themes)-1, pos+delta))
	if pos == -1 {
		next = 0
	}
	if themes[next] != m.themeCursor {
		m.previewTheme(themes[next])
	}
}

// keepThemeVisible moves the cursor onto the first visible theme when a filter
// change hides the highlighted one.
func (m *Model) keepThemeVisible() {
	entries := themePickerEntries(PickableThemes(), m.themeFilter)
	if themeEntryPos(entries, m.themeCursor) >= 0 {
		return
	}
	for _, e := range entries {
		if e.header == "" {
			m.previewTheme(e.idx)
			return
		}
	}
}

func (m Model) handleThemePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	prevTheme := m.activeTheme
	var themeCmd tea.Cmd

	if m.themeFiltering {
		switch msg.Type {
		case tea.KeyEsc:
			if m.themeFilter != "" {
				m.themeFilter = ""
				m.keepThemeVisible()
			} else {
				m.themeFiltering = false
			}
		case tea.KeyEnter:
			m.themeFiltering = false
		case tea.KeyBackspace:
			if _, size := utf8.DecodeLastRuneInString(m.themeFilter); size > 0 {
				m.themeFilter = m.themeFilter[:len(m.themeFilter)-size]
				m.keepThemeVisible()
			}
		case tea.KeyUp:
			m.stepTheme(-1)
		case tea.KeyDown:
			m.stepTheme(1)
		case tea.KeyRunes, tea.KeySpace:
			m.themeFilter += string(msg.Runes)
			m.keepThemeVisible()
		}
	} else {
		switch {
		case msg.String() == "/":
			m.themeFiltering = true
		case keyMatches(msg, m.keys.Up):
			m.stepTheme(-1)
		case keyMatches(msg, m.keys.Down):
			m.stepTheme(1)
		case keyMatches(msg, m.keys.Confirm):
			if themeEntryPos(themePickerEntries(PickableThemes(), m.themeFilter), m.themeCursor) < 0 {
				break
			}
			m.confirmedTheme = m.themeCursor
			m.overlay = overlayNone
			m.cfg.Theme = pickableThemeNameAt(m.confirmedTheme)
			_ = m.saveConfig()
			themeCmd = m.startOmarchyWatchIfNeeded()
			if m.activeMessageRowCount() > 0 {
				m.setViewportForCurrentRow()
			}
		case keyMatches(msg, m.keys.Cancel):
			m.activeTheme = m.confirmedTheme
			m.setStyles(BuildStyles(MergedBuiltinThemeAtIndex(m.cfg, m.activeTheme), m.cfg.Display.Density, m.cfg.Display.PaneCorners))
			m.overlay = overlayNone
			if m.activeMessageRowCount() > 0 {
				m.setViewportForCurrentRow()
			}
		}
	}
	if m.activeTheme != prevTheme {
		return m, tea.Batch(themeCmd, setTermColorsCmd(m.styles.Theme.Fg, m.styles.Theme.Bg))
	}
	return m, themeCmd
}
