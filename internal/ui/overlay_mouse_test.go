package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// clickOverlayHit renders m, finds the marked piece id in the open overlay,
// and returns a left click on its first cell.
func clickOverlayHit(t *testing.T, m Model, id string) tea.MouseMsg {
	t.Helper()
	_ = m.View()
	hm := m.overlayHits
	for _, h := range hm.hits {
		if h.id == id {
			return leftClick(hm.originX+h.x0, hm.originY+h.row)
		}
	}
	ids := make([]string, 0, len(hm.hits))
	for _, h := range hm.hits {
		ids = append(ids, h.id)
	}
	t.Fatalf("no hit %q in overlay %v; have %v", id, m.overlay, ids)
	return tea.MouseMsg{}
}

func TestOverlayHitsStripMarkersFromView(t *testing.T) {
	m := mouseTestModel(t)
	m.overlay = overlayQuitConfirm
	view := m.View()
	if strings.Contains(view, "\x1b["+hitOpenParam) || strings.Contains(view, "\x1b["+hitCloseParam) {
		t.Fatal("hit markers leaked into the rendered view")
	}
	if len(m.overlayHits.hits) == 0 {
		t.Fatal("expected the quit dialog's hints to be recorded")
	}
}

func TestOverlayHitLandsOnHintText(t *testing.T) {
	m := mouseTestModel(t)
	m.overlay = overlayQuitConfirm
	click := clickOverlayHit(t, m, "hint:esc")
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	row := []rune(lines[click.Y])
	if got := string(row[click.X : click.X+3]); got != "esc" {
		t.Fatalf("hint:esc hit points at %q, want \"esc\"", got)
	}
}

func TestOverlayHintClickConfirmsQuit(t *testing.T) {
	m := mouseTestModel(t)
	m.overlay = overlayQuitConfirm
	next, cmd := m.Update(clickOverlayHit(t, m, "hint:y"))
	if !next.(Model).quitActivated || cmd == nil {
		t.Fatal("expected clicking the y hint to quit")
	}
}

func TestOverlayHintClickCancels(t *testing.T) {
	m := mouseTestModel(t)
	m.overlay = overlaySyncAllConfirm
	next, _ := m.Update(clickOverlayHit(t, m, "hint:esc"))
	if next.(Model).overlay != overlayNone {
		t.Fatalf("expected clicking esc to close the dialog, overlay=%v", next.(Model).overlay)
	}
}

func TestOverlayClickOutsideKeepsModalOpen(t *testing.T) {
	m := mouseTestModel(t)
	m.overlay = overlaySettings
	_ = m.View()
	next, _ := m.Update(leftClick(0, 0))
	if next.(Model).overlay != overlaySettings {
		t.Fatal("a click outside Settings must not close it")
	}
}

func TestOverlayHitsNotRecordedWithMouseOff(t *testing.T) {
	m := mouseTestModel(t)
	m.cfg.Display.MouseSupport = false
	m.overlay = overlayQuitConfirm
	_ = m.View()
	if len(m.overlayHits.hits) != 0 {
		t.Fatal("expected no hit recording while mouse support is off")
	}
}

func TestHintClickKey(t *testing.T) {
	cases := map[string]string{"y/enter": "y", "esc/?/q": "esc", "^s": "^s", "enter": "enter", "d": "d", "U": "U"}
	for in, want := range cases {
		if got, ok := hintClickKey(in); !ok || got != want {
			t.Errorf("hintClickKey(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if key, ok := keyMsgForHint("U"); !ok || key.String() != "U" {
		t.Fatalf("uppercase status hint must press U, got %q (ok=%v)", key.String(), ok)
	}
	for _, in := range []string{"↑↓", "←→/↑↓", "pgup/pgdown", "j/k"} {
		if _, ok := hintClickKey(in); ok {
			t.Errorf("hintClickKey(%q) should not be clickable", in)
		}
	}
}

func paletteMouseModel(t *testing.T) Model {
	t.Helper()
	m := mouseTestModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(Model)
	if m.overlay != overlayCommandPalette {
		t.Fatalf("palette did not open: %v", m.overlay)
	}
	return m
}

func TestOverlayRowClickMovesCursor(t *testing.T) {
	m := paletteMouseModel(t)
	next, _ := m.Update(clickOverlayHit(t, m, "row:2"))
	got := next.(Model)
	if got.commandCursor != 2 || got.overlay != overlayCommandPalette {
		t.Fatalf("expected the click to select row 2 and keep the palette open, cursor=%d overlay=%v", got.commandCursor, got.overlay)
	}
}

func TestGotoFolderModalClickSelectsResult(t *testing.T) {
	m := mouseTestModel(t)
	m.overlay = overlayGotoFolder
	m.gotoFolder.matches = []folderCandidate{{label: "Inbox", mailboxID: 1}, {label: "Archive", mailboxID: 2}}
	next, _ := m.Update(clickOverlayHit(t, m, "row:1"))
	if got := next.(Model).gotoFolder.cursor; got != 1 {
		t.Fatalf("clicked folder cursor = %d, want 1", got)
	}
}

func TestScheduleTimeModalClickSelectsPreset(t *testing.T) {
	m := mouseTestModel(t)
	m.openScheduleSend()
	next, _ := m.Update(clickOverlayHit(t, m, "row:2"))
	if got := next.(Model).schedulePicker.Index(); got != 2 {
		t.Fatalf("clicked schedule preset = %d, want 2", got)
	}
}

func TestTideContactsModalClickSelectsAndFocuses(t *testing.T) {
	m := mouseTestModel(t)
	m.overlay = overlayContactManager
	m.simpleContacts = &simpleContacts{contacts: []plugin.BookContact{{Name: "Alice", Addr: "alice@example.com"}, {Name: "Bob", Addr: "bob@example.com"}}}
	next, _ := m.Update(clickOverlayHit(t, m, "row:1"))
	m = next.(Model)
	if got := m.simpleContacts.cursor; got != 1 {
		t.Fatalf("clicked contact cursor = %d, want 1", got)
	}
	m.simpleContacts.editing = true
	m.simpleContacts.fields = make([]textinput.Model, 5)
	for i := range m.simpleContacts.fields {
		m.simpleContacts.fields[i] = newAMInput("", false)
	}
	next, _ = m.Update(clickOverlayHit(t, m, simpleContactFieldHit+"3"))
	if got := next.(Model).simpleContacts.field; got != 3 {
		t.Fatalf("clicked contact field = %d, want 3", got)
	}
}

func TestClassificationModalClickSelectsChoice(t *testing.T) {
	m := mouseTestModel(t)
	m.overlay = overlayClassification
	m.classification = classificationEditor{field: classificationNeedsReply, choices: []string{"plugin", "true", "false"}}
	next, _ := m.Update(clickOverlayHit(t, m, classificationChoiceHit+"1"))
	if got := next.(Model).classification.choice; got != 1 {
		t.Fatalf("clicked classification choice = %d, want 1", got)
	}
}

func TestOverlayRowDoubleClickRunsItem(t *testing.T) {
	m := paletteMouseModel(t)
	// Row 0 is "Open Outbox" (enabled without accounts); the first click
	// lands on the cursor row, the second runs it.
	click := clickOverlayHit(t, m, "row:0")
	next, _ := m.Update(click)
	m = next.(Model)
	if m.overlay != overlayCommandPalette {
		t.Fatal("a single click must not run the item")
	}
	_ = m.View()
	next, _ = m.Update(click)
	if got := next.(Model).overlay; got != overlayOutbox {
		t.Fatalf("expected a double-click to run Open Outbox, overlay=%v", got)
	}
}

func TestOverlayWheelMovesListCursor(t *testing.T) {
	m := paletteMouseModel(t)
	_ = m.View()
	hm := m.overlayHits
	wheel := tea.MouseMsg{X: hm.originX + 2, Y: hm.originY + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
	next, _ := m.Update(wheel)
	if got := next.(Model).commandCursor; got != wheelStepRows {
		t.Fatalf("expected the wheel to move the cursor %d rows, got %d", wheelStepRows, got)
	}
}

func TestExtractHitsMultiLineAndStyled(t *testing.T) {
	hitMarking = true
	card := markHit("row:1", "\x1b[1mab\x1b[0m\ncdef")
	box := "xx" + markHit("hint:y", "yes") + " z\n" + card
	hitMarking = false
	clean, hits := extractHits(box)
	if clean != "xxyes z\n\x1b[1mab\x1b[0m\ncdef" {
		t.Fatalf("markers not stripped: %q", clean)
	}
	want := []overlayHit{{"hint:y", 0, 2, 5}, {"row:1", 1, 0, 2}, {"row:1", 2, 0, 4}}
	if len(hits) != len(want) {
		t.Fatalf("hits = %+v, want %+v", hits, want)
	}
	for i := range want {
		if hits[i] != want[i] {
			t.Fatalf("hit %d = %+v, want %+v", i, hits[i], want[i])
		}
	}
}

func TestOverlayHelpCategoryClickAndPaneWheel(t *testing.T) {
	m := mouseTestModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(Model)
	if m.overlay != overlayHelp {
		t.Fatalf("help did not open: %v", m.overlay)
	}
	next, _ = m.Update(clickOverlayHit(t, m, "helpcat:2"))
	m = next.(Model)
	if m.helpCat != 2 || m.helpRight {
		t.Fatalf("expected category 2 selected with the list focused, cat=%d right=%v", m.helpCat, m.helpRight)
	}
	click := clickOverlayHit(t, m, "helppane")
	wheel := tea.MouseMsg{X: click.X, Y: click.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
	next, _ = m.Update(wheel)
	got := next.(Model)
	if got.helpCat != 2 {
		t.Fatalf("wheel over the text pane must not change category, got %d", got.helpCat)
	}
	if got.helpVP.YOffset == 0 && got.helpVP.TotalLineCount() > got.helpVP.Height {
		t.Fatal("expected the wheel to scroll the help text")
	}
}

func settingsMouseModel(t *testing.T) Model {
	t.Helper()
	m := mouseTestModel(t)
	next, _ := m.executeCommand("settings")
	m = next.(Model)
	if m.overlay != overlaySettings {
		t.Fatalf("settings did not open: %v", m.overlay)
	}
	return m
}

func TestOverlaySettingsToggleFlipsOnClick(t *testing.T) {
	m := settingsMouseModel(t)
	// Focus the toggle once so the detail pane scrolls it into view, then
	// move focus away: the click must refocus it and flip it in one go.
	m.settings.setFocusedPane(settingsPaneDetail)
	m.settings.setFocusedField(sfShowPaneHeaders)
	m.settings.setFocusedField(sfIcons)
	before := m.settings.showPaneHeaders
	id := settingsHitToggle + strconv.Itoa(int(sfShowPaneHeaders))
	next, _ := m.Update(clickOverlayHit(t, m, id))
	got := next.(Model)
	if got.settings.focusedField != sfShowPaneHeaders || got.settings.showPaneHeaders == before {
		t.Fatalf("expected one click to focus and flip the toggle: field=%v on=%v (was %v)", got.settings.focusedField, got.settings.showPaneHeaders, before)
	}
	if got.overlay != overlaySettings {
		t.Fatal("settings closed unexpectedly")
	}
}

func TestOverlaySettingsSectionClick(t *testing.T) {
	m := settingsMouseModel(t)
	target := settingsSection(2)
	next, _ := m.Update(clickOverlayHit(t, m, settingsHitSection+"2"))
	got := next.(Model)
	if got.settings.activeSection != target || got.settings.focusedPane != settingsPaneSidebar {
		t.Fatalf("expected section %v active with the list focused, got %v pane=%v", target, got.settings.activeSection, got.settings.focusedPane)
	}
}

func TestOverlaySettingsPickerCyclesOnSecondClick(t *testing.T) {
	m := settingsMouseModel(t)
	id := settingsHitField + strconv.Itoa(int(sfDateFormat))
	click := clickOverlayHit(t, m, id)
	next, _ := m.Update(click)
	m = next.(Model)
	before := m.settings.dateFormatIdx
	if m.settings.focusedField != sfDateFormat {
		t.Fatalf("first click should focus the picker, got %v", m.settings.focusedField)
	}
	next, _ = m.Update(clickOverlayHit(t, m, id))
	if next.(Model).settings.dateFormatIdx == before {
		t.Fatal("second click should cycle the picker")
	}
}

func TestOverlayAccountFieldClickFocusesAndToggles(t *testing.T) {
	m := mouseTestModel(t)
	m.accountManager.mode = amAdd
	m.accountManager.provider = "Custom" // show the IMAP/SMTP rows
	m.overlay = overlayAccountManager
	id := accountHitField + strconv.Itoa(int(amFieldIMAPTLS))
	next, _ := m.Update(clickOverlayHit(t, m, id))
	m = next.(Model)
	if m.accountManager.focusedField != amFieldIMAPTLS {
		t.Fatalf("expected the click to focus IMAP TLS, got %v", m.accountManager.focusedField)
	}
	before := m.accountManager.imapTLS
	next, _ = m.Update(clickOverlayHit(t, m, id))
	if next.(Model).accountManager.imapTLS == before {
		t.Fatal("expected a click on the focused toggle to flip it")
	}
}

func TestOverlayContactFieldClickFocuses(t *testing.T) {
	m := mouseTestModel(t)
	m.contactManager = NewContactManager(newContactTestDB(t))
	m.contactManager.beginAdd()
	m.overlay = overlayContactManager
	next, _ := m.Update(clickOverlayHit(t, m, contactHitField+"2"))
	if got := next.(Model).contactManager.formFocus; got != 2 {
		t.Fatalf("expected the Phone field focused, got %d", got)
	}
}

func TestOverlayScheduleCalendarClickPicksDay(t *testing.T) {
	m := mouseTestModel(t)
	m.overlay = overlayScheduleSend
	m.scheduleCustom = true
	m.scheduleSendInput = textinput.New()
	m.scheduleCalendar = time.Date(2030, 3, 10, 0, 0, 0, 0, time.Local)
	id := scheduleHitDay + "2030-03-14"
	click := clickOverlayHit(t, m, id)
	next, _ := m.Update(click)
	m = next.(Model)
	if m.scheduleCalendar.Day() != 14 || !m.scheduleDate.IsZero() {
		t.Fatalf("expected a first click to select the 14th, got %v (date %v)", m.scheduleCalendar, m.scheduleDate)
	}
	next, _ = m.Update(clickOverlayHit(t, m, id))
	if got := next.(Model).scheduleDate; got.Day() != 14 {
		t.Fatalf("expected a second click to choose the 14th, scheduleDate=%v", got)
	}
}

func TestOverlayComposeHeaderClickFocusesField(t *testing.T) {
	m := composeMouseModel(t, 100, 40, "hello")
	id := composeHitField + strconv.Itoa(int(composeFieldSubject))
	next, _ := m.Update(clickOverlayHit(t, m, id))
	m = next.(Model)
	if m.compose.focusedField != composeFieldSubject {
		t.Fatalf("expected Subject focused, got %v", m.compose.focusedField)
	}
	if m.overlay != overlayCompose {
		t.Fatal("compose closed unexpectedly")
	}
	// The body still takes clicks through the editor's own handler.
	x, y := screenCell(t, m, "hello")
	next, _ = m.Update(leftPress(x, y))
	if got := next.(Model).compose.focusedField; got != composeFieldBody {
		t.Fatalf("expected a body click to focus the body, got %v", got)
	}
}

func TestComposeBodyRectFromHitsMatchesLayout(t *testing.T) {
	for _, maximized := range []bool{false, true} {
		m := composeMouseModel(t, 110, 42, "one\ntwo\nthree")
		m.compose.maximized = maximized
		m.sizeCompose()
		hm := m.overlayHits
		*hm = overlayHitMap{}
		x0, y0, w0, h0, ok0 := m.composeBodyRect() // computed: no hits yet
		_ = m.View()
		x1, y1, w1, h1, ok1 := m.composeBodyRectFromHits()
		if !ok0 || !ok1 || x0 != x1 || y0 != y1 || w0 != w1 || h0 != h1 {
			t.Fatalf("maximized=%v: layout rect (%d,%d %dx%d %v) != hit rect (%d,%d %dx%d %v)",
				maximized, x0, y0, w0, h0, ok0, x1, y1, w1, h1, ok1)
		}
	}
}

// A wide emoji in the base (the reader body) straddling either edge of a modal
// must not shift the modal's row: the box always starts at its own column.
func TestOverlayAtWideGlyphStraddlingEdgesKeepsBoxColumn(t *testing.T) {
	const width, height, boxX = 40, 8, 9
	box := strings.Join([]string{"[" + strings.Repeat("=", 18) + "]", "[" + strings.Repeat(" ", 18) + "]", "[" + strings.Repeat("=", 18) + "]"}, "\n")
	boxEnd := boxX + 20
	var base []string
	for shift := 0; shift < height; shift++ {
		// Moves ☀️ across the left edge and 🟢 across the right edge.
		base = append(base, strings.Repeat("a", 5+shift)+"☀️"+strings.Repeat("b", 16)+"🟢"+strings.Repeat("c", 20))
	}
	for _, overlayY := range []int{0, 3, 5} {
		out := overlayAt(strings.Join(base, "\n"), box, boxX, overlayY, width, height, lipgloss.Color("#000000"), true)
		for i, line := range strings.Split(out, "\n") {
			if i < overlayY || i >= overlayY+3 {
				continue
			}
			plain := ansi.Strip(line)
			open := strings.Index(plain, "[")
			closing := strings.Index(plain, "]")
			if got := ansi.StringWidth(plain[:open]); got != boxX {
				t.Fatalf("row %d: box starts at column %d, want %d: %q", i, got, boxX, plain)
			}
			if got := ansi.StringWidth(plain[:closing]); got != boxEnd-1 {
				t.Fatalf("row %d: box ends at column %d, want %d: %q", i, got, boxEnd-1, plain)
			}
			if got := ansi.StringWidth(line); got != width {
				t.Fatalf("row %d: width %d, want %d", i, got, width)
			}
		}
	}
}
