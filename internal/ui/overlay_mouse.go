package ui

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Mouse support inside overlays. Rendering records where clickable pieces
// landed (overlay_hits.go); this file turns a click on one into the action
// its keyboard equivalent takes, so every mouse action has a key behind it.

// hintClickKey returns the key a click on a footer hint presses: the first of
// the hint's alternatives ("y/enter" -> "y", "esc/?/q" -> "esc"). Hints that
// only describe navigation (arrows, ranges) are not clickable.
func hintClickKey(keys string) (string, bool) {
	token := strings.ToLower(strings.TrimSpace(keys))
	if i := strings.Index(token, "/"); i > 0 {
		token = token[:i]
	}
	if _, ok := keyMsgForHint(token); !ok {
		return "", false
	}
	return token, true
}

// keyMsgForHint converts a hint's key label to the key press it names.
func keyMsgForHint(token string) (tea.KeyMsg, bool) {
	if strings.ContainsAny(token, "↑↓←→‹›") || token == "" {
		return tea.KeyMsg{}, false
	}
	switch token {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}, true
	case "enter", "⏎", "return":
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}, true
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}, true
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, true
	case "del", "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}, true
	case "backspace", "bksp":
		return tea.KeyMsg{Type: tea.KeyBackspace}, true
	case "pgup", "pgdown", "home", "end", "j", "k", "h", "l":
		return tea.KeyMsg{}, false // navigation, not an action
	}
	if rest, ok := strings.CutPrefix(token, "^"); ok {
		token = "ctrl+" + rest
	}
	if rest, ok := strings.CutPrefix(token, "ctrl+"); ok && len([]rune(rest)) == 1 {
		r := []rune(rest)[0]
		if r >= 'a' && r <= 'z' {
			return tea.KeyMsg{Type: tea.KeyCtrlA + tea.KeyType(r-'a')}, true
		}
		return tea.KeyMsg{}, false
	}
	if r := []rune(token); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}, true
	}
	return tea.KeyMsg{}, false
}

// handleOverlayMouse handles a mouse event while an overlay other than the
// context menu is open. handled is false when the event should fall through
// to the overlay's own mouse code (the compose body editor).
func (m Model) handleOverlayMouse(msg tea.MouseMsg, now time.Time) (tea.Model, tea.Cmd, bool) {
	ev := tea.MouseEvent(msg)
	if ev.IsWheel() {
		return m.handleOverlayWheel(msg)
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil, false
	}
	hit, found, _ := m.overlayHitAt(msg.X, msg.Y)
	if !found {
		// Clicks outside any control (or outside the box) never dismiss a
		// modal: Esc stays the only way out, so a stray click can't lose work.
		return m, nil, false
	}
	if token, ok := strings.CutPrefix(hit.id, "hint:"); ok {
		key, ok := keyMsgForHint(token)
		if !ok {
			return m, nil, true
		}
		next, cmd := m.update(key)
		return next, cmd, true
	}
	if m.overlay == overlayHelp {
		if next, cmd, ok := m.clickHelp(hit.id); ok {
			return next, cmd, true
		}
	}
	if m.overlay == overlayAccountManager {
		if next, cmd, ok := m.clickAccountField(hit.id); ok {
			return next, cmd, true
		}
	}
	if m.overlay == overlayContactManager {
		if m.simpleContacts != nil {
			if d, ok := strings.CutPrefix(hit.id, simpleContactFieldHit); ok {
				if i, err := strconv.Atoi(d); err == nil && m.simpleContacts.editing && i >= 0 && i < len(m.simpleContacts.fields) {
					s := m.simpleContacts
					s.fields[s.field].Blur()
					s.field = i
					return m, s.fields[i].Focus(), true
				}
				return m, nil, true
			}
		}
		if d, ok := strings.CutPrefix(hit.id, contactHitField); ok {
			if i, err := strconv.Atoi(d); err == nil {
				m.contactManager.setFormFocus(i)
			}
			return m, nil, true
		}
	}
	if m.overlay == overlayScheduleSend {
		if d, ok := strings.CutPrefix(hit.id, scheduleHitDay); ok {
			return m.clickScheduleDay(d, msg, now)
		}
	}
	if m.overlay == overlayColorPicker && !m.colorPicker.editing {
		if d, ok := strings.CutPrefix(hit.id, colorHitFocus); ok {
			kind, idx, _ := strings.Cut(d, ":")
			k, err1 := strconv.Atoi(kind)
			i, err2 := strconv.Atoi(idx)
			if err1 == nil && err2 == nil {
				m.colorPicker.focus = colorPickerFocus{kind: colorPickerFocusKind(k), idx: i}
			}
			return m, nil, true
		}
	}
	if m.overlay == overlayCompose {
		if d, ok := strings.CutPrefix(hit.id, composeHitField); ok {
			if n, err := strconv.Atoi(d); err == nil {
				m.compose.focusFieldByClick(composeField(n))
			}
			return m, nil, true
		}
	}
	if m.overlay == overlaySettings {
		if next, cmd, ok := m.clickSettings(hit.id); ok {
			return next, cmd, true
		}
	}
	if m.overlay == overlayClassification && !m.classification.custom {
		if d, ok := strings.CutPrefix(hit.id, classificationFieldHit); ok {
			if i, err := strconv.Atoi(d); err == nil && i >= 0 && i <= int(classificationCategory) {
				m.classification.field = classificationField(i)
				m.classification.choice = 0
				m.classification.setChoices(m.effectiveClassification(m.classification.messageID))
			}
			return m, nil, true
		}
		if d, ok := strings.CutPrefix(hit.id, classificationChoiceHit); ok {
			if i, err := strconv.Atoi(d); err == nil && i >= 0 && i < len(m.classification.choices) {
				if i == m.classification.choice && !m.overlayClick.at.IsZero() && now.Sub(m.overlayClick.at) <= doubleClickWindow && m.overlayClick.x == msg.X && m.overlayClick.y == msg.Y {
					m.overlayClick = overlayClickState{}
					next, cmd := m.update(tea.KeyMsg{Type: tea.KeyEnter})
					return next, cmd, true
				}
				m.classification.choice = i
				m.overlayClick = overlayClickState{at: now, x: msg.X, y: msg.Y, overlay: m.overlay}
			}
			return m, nil, true
		}
	}
	if d, ok := strings.CutPrefix(hit.id, "row:"); ok {
		delta, err := strconv.Atoi(d)
		if err != nil {
			return m, nil, true
		}
		if m.overlay == overlayContactManager && m.simpleContacts != nil && m.simpleContacts.searching {
			m.simpleContacts.searching = false
			m.simpleContacts.query.Blur()
		}
		return m.clickOverlayRow(delta, msg, now)
	}
	return m, nil, false
}

// listRowHit marks a list row by its distance from the list's cursor, in
// cursor steps. A click presses Up or Down that many times, so the list's own
// key handling (wrapping, previews, scrolling) runs exactly as if typed, and
// no overlay needs its own click-to-index code.
func listRowHit(rowIdx, cursor int, s string) string {
	return markHit("row:"+strconv.Itoa(rowIdx-cursor), s)
}

// clickOverlayRow moves an overlay list's cursor to the clicked row. A second
// click on the same cell within the double-click window presses Enter.
func (m Model) clickOverlayRow(delta int, msg tea.MouseMsg, now time.Time) (tea.Model, tea.Cmd, bool) {
	o := &m.overlayClick
	double := delta == 0 && !o.at.IsZero() && now.Sub(o.at) <= doubleClickWindow &&
		o.x == msg.X && o.y == msg.Y && o.overlay == m.overlay
	if double {
		m.overlayClick = overlayClickState{}
		next, cmd := m.update(tea.KeyMsg{Type: tea.KeyEnter})
		return next, cmd, true
	}
	m.overlayClick = overlayClickState{at: now, x: msg.X, y: msg.Y, overlay: m.overlay}
	next, cmd := m.pressRepeat(delta)
	return next, cmd, true
}

// pressRepeat sends |n| Down (n > 0) or Up (n < 0) presses to the overlay.
func (m Model) pressRepeat(n int) (tea.Model, tea.Cmd) {
	key := tea.KeyMsg{Type: tea.KeyDown}
	if n < 0 {
		key, n = tea.KeyMsg{Type: tea.KeyUp}, -n
	}
	var model tea.Model = m
	var cmds []tea.Cmd
	for i := 0; i < n; i++ {
		next, cmd := model.(Model).update(key)
		model = next
		cmds = append(cmds, cmd)
	}
	return model, tea.Batch(cmds...)
}

// overlayClickState remembers the last overlay click for double-clicks.
type overlayClickState struct {
	at      time.Time
	x, y    int
	overlay overlayMode
}

// wheelOverlays scroll or move their selection with the wheel. Forms and
// Compose are left out: there the wheel would hop between fields.
func wheelScrolls(o overlayMode) bool {
	switch o {
	case overlayThemePicker, overlayCommandPalette, overlayMoveMessage, overlayGotoFolder, overlaySaveAttach,
		overlaySnooze, overlayMailTags, overlayTagColors, overlaySpellDictionary,
		overlayPluginCatalog, overlayPlugins, overlayPluginPicker, overlayOutbox, overlayContactManager,
		overlayFilterManager, overlayScheduleSend, overlayHelp, overlaySummary,
		overlaySource, overlayLogViewer, overlayPluginResult, overlayPluginAnnotations,
		overlayGrammarPreview, overlayUpdateConfirm:
		return true
	}
	return false
}

// handleOverlayWheel turns the wheel over an overlay's box into Up/Down
// presses, three rows per notch like the main panes.
func (m Model) handleOverlayWheel(msg tea.MouseMsg) (tea.Model, tea.Cmd, bool) {
	if m.overlay == overlayContactManager && (m.simpleContacts == nil || m.simpleContacts.editing || m.simpleContacts.searching) {
		return m, nil, true
	}
	if !wheelScrolls(m.overlay) {
		return m, nil, false
	}
	hit, found, inBox := m.overlayHitAt(msg.X, msg.Y)
	if !inBox {
		return m, nil, true
	}
	if m.overlay == overlayHelp {
		return m.wheelHelp(msg, found && strings.HasPrefix(hit.id, "helpcat:"))
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		next, cmd := m.pressRepeat(-wheelStepRows)
		return next, cmd, true
	case tea.MouseButtonWheelDown:
		next, cmd := m.pressRepeat(wheelStepRows)
		return next, cmd, true
	}
	return m, nil, true
}

// clickHelp handles clicks in Help: a category selects it (left pane focus),
// the text pane takes focus for keyboard scrolling.
func (m Model) clickHelp(id string) (tea.Model, tea.Cmd, bool) {
	if id == "helppane" {
		m.helpRight = true
		return m, nil, true
	}
	d, ok := strings.CutPrefix(id, "helpcat:")
	if !ok {
		return m, nil, false
	}
	i, err := strconv.Atoi(d)
	if err != nil || i < 0 || i >= len(m.helpSectionsFiltered()) {
		return m, nil, true
	}
	m.helpRight = false
	if i != m.helpCat {
		m.helpCat = i
		m.resetHelpVP()
	}
	return m, nil, true
}

// wheelHelp scrolls whichever Help pane is under the pointer without moving
// keyboard focus: the category list steps one section per notch, the text
// pane scrolls three rows.
func (m Model) wheelHelp(msg tea.MouseMsg, overCategories bool) (tea.Model, tea.Cmd, bool) {
	dir := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		dir = -1
	case tea.MouseButtonWheelDown:
		dir = 1
	}
	if dir == 0 {
		return m, nil, true
	}
	if overCategories {
		n := len(m.helpSectionsFiltered())
		if next := clamp(m.helpCat+dir, 0, max(0, n-1)); next != m.helpCat {
			m.helpCat = next
			m.resetHelpVP()
		}
		return m, nil, true
	}
	if dir < 0 {
		m.helpVP.ScrollUp(wheelStepRows)
	} else {
		m.helpVP.ScrollDown(wheelStepRows)
	}
	return m, nil, true
}

// Settings hit ids: a section in the left list, or a field row in the detail
// pane (the field number follows the prefix).
const (
	settingsHitSection = "ssection:"
	settingsHitField   = "sfield:"
	settingsHitToggle  = "stoggle:"
)

// clickSettings handles clicks in Settings. A section click opens it with the
// list focused. A field click focuses the field; a toggle flips at once, a
// picker cycles forward on each further click, and an action or the back
// link runs on a second click, so nothing irreversible starts by accident.
func (m Model) clickSettings(id string) (tea.Model, tea.Cmd, bool) {
	if d, ok := strings.CutPrefix(id, settingsHitSection); ok {
		i, err := strconv.Atoi(d)
		if err != nil {
			return m, nil, true
		}
		m.settings.setFocusedPane(settingsPaneSidebar)
		if settingsSection(i) != m.settings.activeSection {
			m.settings.setActiveSection(settingsSection(i))
		}
		return m, nil, true
	}
	toggle := false
	d, ok := strings.CutPrefix(id, settingsHitField)
	if !ok {
		if d, ok = strings.CutPrefix(id, settingsHitToggle); !ok {
			return m, nil, false
		}
		toggle = true
	}
	n, err := strconv.Atoi(d)
	if err != nil {
		return m, nil, true
	}
	field := settingsField(n)
	wasFocused := m.settings.focusedPane == settingsPaneDetail && m.settings.focusedField == field
	if !wasFocused {
		m.settings.setFocusedPane(settingsPaneDetail)
		m.settings.setFocusedField(field)
	}
	switch {
	case toggle:
		next, cmd := m.update(tea.KeyMsg{Type: tea.KeyEnter})
		return next, cmd, true
	case !wasFocused, m.settings.isTextInput():
		return m, nil, true
	case m.settings.isPickerField():
		next, cmd := m.update(tea.KeyMsg{Type: tea.KeyRight})
		return next, cmd, true
	}
	next, cmd := m.update(tea.KeyMsg{Type: tea.KeyEnter})
	return next, cmd, true
}

const accountHitField = "amfield:"

// clickAccountField focuses an Account Manager form field. A further click on
// a focused toggle flips it, on a picker cycles it, and on the OAuth sign-in
// row starts sign-in, exactly as Space or Right would.
func (m Model) clickAccountField(id string) (tea.Model, tea.Cmd, bool) {
	d, ok := strings.CutPrefix(id, accountHitField)
	if !ok {
		return m, nil, false
	}
	n, err := strconv.Atoi(d)
	if err != nil {
		return m, nil, true
	}
	field := amField(n)
	if m.accountManager.focusedField != field {
		m.accountManager.focusField(field)
		return m, nil, true
	}
	switch field {
	case amFieldIMAPTLS, amFieldSMTPTLS, amFieldOAuthSignIn:
		next, cmd := m.update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
		return next, cmd, true
	case amFieldProvider, amFieldColor, amFieldAuthMethod:
		next, cmd := m.update(tea.KeyMsg{Type: tea.KeyRight})
		return next, cmd, true
	}
	return m, nil, true
}

// contactHitField marks a Contact Manager form field; a click focuses it.
const contactHitField = "cmfield:"

// simpleContactFieldHit marks an editable TideContacts field.
const simpleContactFieldHit = "scfield:"

const (
	classificationFieldHit  = "classfield:"
	classificationChoiceHit = "classchoice:"
)

// scheduleHitDay marks a day in the schedule-send calendar.
const scheduleHitDay = "calday:"

// clickScheduleDay selects the clicked day; a second click on the selected
// day chooses it, as Enter does.
func (m Model) clickScheduleDay(day string, msg tea.MouseMsg, now time.Time) (tea.Model, tea.Cmd, bool) {
	date, err := time.ParseInLocation(time.DateOnly, day, m.scheduleCalendar.Location())
	if err != nil {
		return m, nil, true
	}
	if sameDay(date, m.scheduleCalendar) {
		return m.clickOverlayRow(0, msg, now)
	}
	m.scheduleCalendar = time.Date(date.Year(), date.Month(), date.Day(),
		m.scheduleCalendar.Hour(), m.scheduleCalendar.Minute(), 0, 0, m.scheduleCalendar.Location())
	m.overlayClick = overlayClickState{at: now, x: msg.X, y: msg.Y, overlay: m.overlay}
	return m, nil, true
}

// composeHitField marks a compose header row (From, To, CC, BCC, Subject).
const composeHitField = "cfield:"

// focusFieldByClick moves compose focus to field the way Tab does, so the
// same blur/focus and suggestion bookkeeping runs. A click on the From row
// while it is focused opens or closes the sender picker, like Enter.
func (c *ComposeModel) focusFieldByClick(field composeField) {
	if c.focusedField == field {
		if field == composeFieldFrom && len(c.accounts) > 1 {
			if c.senderPickerOpen {
				c.senderPickerOpen = false
			} else {
				c.openSenderPicker()
			}
		}
		return
	}
	for range int(composeFieldCount) {
		c.advanceField(1)
		if c.focusedField == field {
			return
		}
	}
}
