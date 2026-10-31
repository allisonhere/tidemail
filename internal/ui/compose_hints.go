package ui

import (
	"strings"
	"unicode"

	"github.com/allisonhere/ripple"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// composeHint is one footer action. A narrow window drops the highest prio
// first; the hints that survive keep their listed order. A fixed hint shows
// regardless of focus and sizes the grid's columns; the others follow it.
type composeHint struct {
	key, label string
	prio       int
	fixed      bool
}

// composeHints renders the compose footer: always three rows, so neither focus
// changes, Find, nor transient messages resize the message body. The first row
// is state (vim mode or command line, status messages, PGP toggles, the
// signature marker); the other two are column-aligned key hints — message
// actions, then keys for the focused field. While the body's Find is open the
// footer becomes the Find/Replace bar plus the keys that drive it. The spell
// and template panels replace the footer with their own height.
func (c ComposeModel) composeHints(width int, chrome managerChrome) string {
	if c.spellOpen {
		return c.spellPanel(width, chrome)
	}
	if c.tmplOpen {
		return c.templatesPanel(width, chrome)
	}
	blank := padStyled("", width, chrome.baseBg)
	if c.busy {
		// While sending, the state line carries the message.
		return strings.Join([]string{c.composeStateLine(width, chrome), blank, blank}, "\n")
	}
	if c.focusedField == composeFieldBody && c.bodyInput.ed.SearchActive() {
		return c.composeSearchFooter(width, chrome)
	}

	// Esc opens the last row in every footer mode, so it always sits in the
	// bottom-left corner. Row 2 is the same in every field: send, then send
	// options and setup. Row 3 follows Esc with the tools that work from any
	// field, then the focused field's keys as a group.
	var actions, context []composeHint
	if c.senderPickerOpen {
		actions = []composeHint{{"enter", "select", 0, true}, {"↑↓", "choose sender", 1, true}}
		context = []composeHint{{"esc", "cancel", 0, true}}
	} else {
		actions = []composeHint{{"^s", "send", 0, true}, {"^p", "commands", 3, true}, {"^u", "sender", 5, true}, {"f6", "templates", 4, true}}
		// In vim mode Esc only cancels from Normal; elsewhere it returns there.
		vimMode := ""
		if c.focusedField == composeFieldBody && c.bodyInput.vimMode() {
			vimMode = c.bodyInput.Mode()
		}
		esc := composeHint{"esc", "cancel", 1, true}
		if vimMode != "" && vimMode != "NORMAL" {
			esc.label = "normal"
		}
		context = append(context, esc, composeHint{"^g", "grammar", 6, true})
		if editorSpell != nil {
			context = append(context, composeHint{"f7", "spell", 9, true})
		}
		if len(c.attachments) > 0 {
			context = append(context, composeHint{"^r", "remove", 3, true})
		}
		switch c.focusedField {
		case composeFieldFrom:
			context = append(context, composeHint{"enter", "pick sender", 2, false})
		case composeFieldBody:
			context = append(context, composeBodyHints(vimMode)...)
		}
	}
	rows := append([]string{c.composeStateLine(width, chrome)}, composeKeyGrid([][]composeHint{actions, context}, width, chrome)...)
	return strings.Join(rows, "\n")
}

// composeBodyHints is the body's key group. The standard editor copies and
// cuts the selection with the system shortcuts. Vim mode advertises its own
// keys for the active sub-mode instead: its y/d already reach the system
// clipboard, and Insert needs none — typing is the point of it.
func composeBodyHints(vimMode string) []composeHint {
	switch vimMode {
	case "":
		return []composeHint{{"^f", "find", 2, false}, {"^c", "copy", 7, false}, {"^x", "cut", 8, false}}
	case "NORMAL":
		return []composeHint{{"/", "find", 2, false}, {"y", "yank", 7, false}, {"p", "put", 7, false}, {"u", "undo", 8, false}}
	case "VISUAL", "V-LINE":
		return []composeHint{{"y", "yank", 2, false}, {"d", "delete", 7, false}, {"c", "change", 8, false}}
	case "COMMAND":
		return []composeHint{{"enter", "run", 2, false}, {":w", "send", 7, false}, {":q", "cancel", 8, false}}
	}
	return nil
}

// composeStateLine is the footer's first row: what is happening at the left
// (the vim mode or command line, then any status message) and what is switched
// on at the right (PGP toggles and the signature marker).
func (c ComposeModel) composeStateLine(width int, chrome managerChrome) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	var left, right []string
	if c.focusedField == composeFieldBody && c.bodyInput.vimMode() && !c.busy {
		if cl := c.bodyInput.CommandLine(); cl != "" {
			left = append(left, base.Foreground(chrome.accent).Render(cl))
		} else if mode := c.bodyInput.Mode(); mode != "" {
			left = append(left, base.Foreground(chrome.accent).Render("-- "+mode+" --"))
		}
	}
	if c.statusMsg != "" {
		fg := chrome.pendingFg
		if c.isErr {
			fg = chrome.errorFg
		}
		left = append(left, base.Foreground(fg).Render(c.statusMsg))
	}
	if !c.senderPickerOpen && !c.busy {
		if c.pgpAvailable {
			right = append(right, composeToggleHint("alt+s", "sign", c.pgpSign, chrome), composeToggleHint("alt+e", "encrypt", c.pgpEncrypt, chrome))
		}
		if c.focusedField == composeFieldBody {
			// Informational marker only; it does not define a keybinding.
			right = append(right, base.Foreground(chrome.text).Render("⁂")+base.Foreground(chrome.muted).Render(" sig"))
		}
	}
	gap := base.Render("   ")
	return composeSplitLine(strings.Join(left, gap), strings.Join(right, gap), width, chrome)
}

// composeSplitLine places left at the footer indent and right against the
// footer's right margin. When both cannot fit, the left side wins: it carries
// the command line being typed and errors.
func composeSplitLine(left, right string, width int, chrome managerChrome) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	avail := max(1, width-4)
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	switch {
	case rw == 0 || (lw > 0 && lw+3+rw > avail):
		return padStyled(base.Render("  ")+left, width, chrome.baseBg)
	case lw == 0 && rw > avail:
		return padStyled(base.Render("  ")+right, width, chrome.baseBg)
	}
	line := base.Render("  ") + left + base.Render(strings.Repeat(" ", avail-lw-rw)) + right
	return padStyled(line, width, chrome.baseBg)
}

// composeKeyGrid lays rows of hints out in shared columns so the pills line up
// vertically. Only fixed hints (each row's leading run) size the columns, so a
// focused field's own hints never move anything else; each of those starts at
// the next column boundary it can reach. Hints that do not fit are dropped by
// prio rather than wrapped, keeping the grid at exactly len(rows) lines.
func composeKeyGrid(rows [][]composeHint, width int, chrome managerChrome) []string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	type cell struct {
		text    string
		w, prio int
		fixed   bool
		at      int // start column, set by place
	}
	grid := make([][]cell, len(rows))
	count := 0
	for r, row := range rows {
		for _, h := range row {
			text := composeKeyHint(h.key, h.label, chrome)
			grid[r] = append(grid[r], cell{text: text, w: lipgloss.Width(text), prio: h.prio, fixed: h.fixed})
			count++
		}
	}
	avail := max(1, width-4)
	gap := 3
	// place positions every cell with the current gap and reports whether all
	// rows fit, and whether the fixed hints alone do.
	place := func() (all, fixed bool) {
		var colW []int
		for _, row := range grid {
			for i, c := range row {
				if !c.fixed {
					break
				}
				if i == len(colW) {
					colW = append(colW, 0)
				}
				colW[i] = max(colW[i], c.w)
			}
		}
		starts := make([]int, len(colW))
		for i := 1; i < len(colW); i++ {
			starts[i] = starts[i-1] + colW[i-1] + gap
		}
		all, fixed = true, true
		for _, row := range grid {
			next := 0
			for i := range row {
				row[i].at = next
				for _, s := range starts {
					if s >= next {
						row[i].at = s
						break
					}
				}
				next = row[i].at + row[i].w + gap
				if row[i].at+row[i].w > avail {
					all = false
					fixed = fixed && !row[i].fixed
				}
			}
		}
		return all, fixed
	}
	for {
		// The gap depends on the fixed hints alone, so a focused field's keys
		// can never re-space the rows; they are dropped first instead.
		gap = 3
		if _, fixed := place(); !fixed {
			gap = 2
		}
		if all, _ := place(); all || count <= 1 {
			break
		}
		dropR, dropI := -1, -1
		for r, row := range grid {
			for i, c := range row {
				if dropR < 0 {
					dropR, dropI = r, i
					continue
				}
				if d := grid[dropR][dropI]; (d.fixed && !c.fixed) || (d.fixed == c.fixed && c.prio >= d.prio) {
					dropR, dropI = r, i
				}
			}
		}
		grid[dropR] = append(grid[dropR][:dropI], grid[dropR][dropI+1:]...)
		count--
	}

	lines := make([]string, len(grid))
	for r, row := range grid {
		line := base.Render("  ")
		cursor := 0
		for _, c := range row {
			line += base.Render(strings.Repeat(" ", c.at-cursor)) + c.text
			cursor = c.at + c.w
		}
		lines[r] = padStyled(line, width, chrome.baseBg)
	}
	return lines
}

// composeSearchFooter is the footer while the body's Find is open: the Find
// row (and With row while replacing), then the keys for the focused control.
// A regex or replacement error takes a key row so its full text is readable.
func (c ComposeModel) composeSearchFooter(width int, chrome managerChrome) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	fn := func(st lipgloss.Style) func(string) string { return func(s string) string { return st.Render(s) } }
	opts := ripple.Options{
		SearchText:    fn(base.Foreground(chrome.text)),
		SearchMuted:   fn(base.Foreground(chrome.muted)),
		SearchRail:    fn(base.Foreground(chrome.accent).Bold(true)),
		SearchControl: fn(lipgloss.NewStyle().Background(chrome.accent).Foreground(accentReadableOn(chrome.text, chrome.accent, 4.5))),
		SearchError:   fn(base.Foreground(chrome.errorFg)),
		SearchInput: func(input textinput.Model, w int, focused bool) string {
			return renderTextInput(input, w, focused, false, chrome)
		},
		SearchPlain: chrome.plainUI,
		// A compact widget, like an editor's find box, rather than a field
		// spanning the whole footer.
		SearchInputMax: min(40, width/2),
		// Each control names its direct key, so none needs Tabbing onto.
		SearchShowKeys: true,
	}
	if !chrome.plainUI {
		opts.SearchCell = func(label string, focused, on, toggle bool) string {
			return composeSearchCell(label, focused, on, toggle, chrome)
		}
	}
	// The bar's rail sits in the footer's 2-cell indent; keep the right margin.
	var rows []string
	for _, row := range c.bodyInput.ed.SearchBarView(max(1, width-2), opts) {
		rows = append(rows, padStyled(row, width, chrome.baseBg))
	}

	status := c.bodyInput.ed.Search()
	// The pills name their own keys, so Enter is hinted only in Find, where
	// no pill shows what it does.
	var lead []composeHint
	if status.Field == ripple.SearchFieldFind {
		primary := composeHint{"enter", "next", 0, true}
		if status.Prompt {
			primary.label = "accept"
		}
		lead = append(lead, primary)
	}
	replace := composeHint{"^r", "replace", 3, true}
	if status.Replacing {
		replace.label = "hide replace"
	}
	// Esc opens the last row here too, matching its corner in the normal footer.
	esc := composeHint{"esc", "close", 1, true}
	keys := [][]composeHint{
		append(lead, composeHint{"⇧f3", "prev", 2, true}, replace),
		{esc, {"tab", "fields", 4, true}, {"^s", "send", 5, true}},
	}
	if detail := composeSearchError(status); detail != "" {
		rows = append(rows, padStyled(base.Render("  ")+base.Foreground(chrome.errorFg).Render(detail), width, chrome.baseBg))
	}
	if free := 3 - len(rows); free == 1 {
		keys = [][]composeHint{append([]composeHint{esc}, append(keys[0], keys[1][1:]...)...)}
	}
	if free := 3 - len(rows); free > 0 {
		rows = append(rows, composeKeyGrid(keys[:free], width, chrome)...)
	}
	return strings.Join(rows[:3], "\n")
}

// composeSearchCell draws a Find/Replace control in the footer's pill
// language. An available button is a neutral pill and a switched-on toggle a
// tinted one; an unavailable button or switched-off toggle is bare muted
// text, so state shows as shape, not color alone. Focus fills the pill with
// the accent — a switched-off toggle instead gains the neutral pill — so a
// focused toggle still shows its state. The caps take the two cells Ripple
// reserves around the label.
func composeSearchCell(label string, focused, on, toggle bool, chrome managerChrome) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	neutral := mixColors(chrome.baseBg, chrome.text, 0.12)
	var bg, fg lipgloss.Color
	switch {
	case focused && toggle && !on:
		bg, fg = neutral, accentReadableOn(chrome.accent, neutral, 4.5)
	case focused:
		bg, fg = chrome.accent, accentReadableOn(chrome.text, chrome.accent, 4.5)
	case toggle && on:
		bg = mixColors(chrome.baseBg, chrome.accent, 0.28)
		fg = accentReadableOn(chrome.accent, bg, 4.5)
	case on:
		bg, fg = neutral, readableText(chrome.text, neutral, 4.5)
	default:
		return base.Foreground(chrome.muted).Render(" " + label + " ")
	}
	return renderChipsWith([]tagChip{{text: label, filled: true, ends: config.TagEndsRound}}, func(string) tagColor { return tagColor{bg: bg, fg: fg} }, chrome.baseBg, base)
}

func composeSearchError(status ripple.SearchStatus) string {
	switch {
	case status.Error != "":
		return "Invalid regex: " + status.Error
	case status.ReplacementError != "" && status.Replacing:
		return status.ReplacementError
	}
	return ""
}

// composeToggleHint is a key hint whose action is a state that stays on, so
// it carries the on/off glyph instead of relying on color alone.
func composeToggleHint(key, label string, on bool, chrome managerChrome) string {
	fg := chrome.muted
	if on {
		fg = chrome.accent
	}
	glyph := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Render(" " + chrome.softToggleGlyph(on))
	return composeKeyHint(key, label, chrome) + glyph
}

// Only the key gets a pill; the action remains ordinary text beside it.
// Reuse the same rounded caps as message tags, with a plain-terminal fallback.
func composeKeyHint(key, label string, chrome managerChrome) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	// Vim keys are case-sensitive (y is not Y), so single characters and ":"
	// commands show exactly as typed; only named keys are capitalized.
	displayKey := key
	if len([]rune(key)) > 1 && !strings.HasPrefix(key, ":") {
		displayKey = strings.ToUpper(key)
	}
	switch key {
	case "esc":
		displayKey = "Esc"
	case "enter":
		displayKey = "Enter"
	case "tab":
		displayKey = "Tab"
	}
	displayLabel := []rune(label)
	if len(displayLabel) > 0 {
		displayLabel[0] = unicode.ToUpper(displayLabel[0])
	}
	var keycap string
	if chrome.plainUI {
		keycap = base.Foreground(chrome.text).Render("[" + displayKey + "]")
	} else {
		bg := mixColors(chrome.baseBg, chrome.text, 0.12)
		fg := readableText(chrome.text, bg, 4.5)
		// Send and a cancelling Esc bracket the footer's most consequential
		// choice, so their pills carry the theme's success and error tints
		// wherever they appear. Esc back to vim Normal stays neutral.
		switch {
		case key == "^s":
			bg = mixColors(chrome.baseBg, chrome.successFg, 0.16)
			fg = accentReadableOn(chrome.successFg, bg, 4.5)
		case key == "esc" && (label == "cancel" || label == "close"):
			bg = mixColors(chrome.baseBg, chrome.errorFg, 0.16)
			fg = accentReadableOn(chrome.errorFg, bg, 4.5)
		}
		keycap = renderChipsWith([]tagChip{{text: displayKey, filled: true, ends: config.TagEndsRound}}, func(string) tagColor { return tagColor{bg: bg, fg: fg} }, chrome.baseBg, base)
	}
	return keycap + base.Foreground(chrome.text).Render(" "+string(displayLabel))
}
