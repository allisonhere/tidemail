package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestComposeKeyOnlyPillsAndPlainFallback(t *testing.T) {
	trueColor(t)
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	normal := ansi.Strip(c.composeHints(110, newManagerChrome(110, CatppuccinMocha, false)))
	if !strings.Contains(normal, pillCapLeft+"^S"+pillCapRight+" Send") {
		t.Fatal("Send key is not a separate pill")
	}
	if !strings.Contains(normal, pillCapLeft+"F6"+pillCapRight+" Templates") {
		t.Fatal("template key is not a separate pill")
	}
	if strings.Contains(normal, pillCapLeft+"⁂") {
		t.Fatal("signature marker information is not a keybinding")
	}
	plain := ansi.Strip(c.composeHints(110, newManagerChrome(110, VT52, true)))
	if !strings.Contains(plain, "[^S] Send") || strings.Contains(plain, pillCapLeft) || strings.Contains(plain, pillCapRight) {
		t.Fatal("plain theme must use bracketed keycaps")
	}
}

func TestComposeFindFooterKeepsBarAndKeys(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.SetValue("cat cat")
	c.bodyInput.ed.OpenReplace()
	c.bodyInput.ed.SetSearchQuery("cat")
	for _, width := range []int{24, 32, 52, 74, 110} {
		hints := c.composeHints(width, newManagerChrome(width, CatppuccinMocha, false))
		if lipgloss.Width(hints) > width || lipgloss.Height(hints) != 3 {
			t.Fatalf("footer is %dx%d at width %d", lipgloss.Width(hints), lipgloss.Height(hints), width)
		}
		lines := strings.Split(plainComposeHintText(hints), "\n")
		if !strings.Contains(lines[2], "enter next") {
			t.Fatalf("lost the primary key at width %d: %q", width, lines)
		}
		if width >= 52 {
			if !strings.Contains(lines[0], "find") || !strings.Contains(lines[1], "with") || !strings.Contains(lines[2], "esc close") {
				t.Fatalf("find footer at width %d: %q", width, lines)
			}
		}
	}
	// The bar's gaps and inputs must carry the footer background, not leak the
	// terminal default.
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	for row, line := range strings.Split(c.composeHints(74, newManagerChrome(74, CatppuccinMocha, false)), "\n") {
		if cell := unpaintedCell(line); cell >= 0 {
			t.Fatalf("unpainted find footer row %d at cell %d: %q", row, cell, line)
		}
	}
	// The pills name their own keys; Enter is hinted only in Find, where no
	// pill shows it.
	chrome := newManagerChrome(110, CatppuccinMocha, false)
	lines := strings.Split(plainComposeHintText(c.composeHints(110, chrome)), "\n")
	if !strings.Contains(lines[2], "enter next") {
		t.Fatalf("Find should advertise Enter next: %q", lines)
	}
	for _, want := range []string{"aa ⌥c", "replace ⏎", "all ⌥⏎"} {
		if !strings.Contains(strings.Join(lines[:2], "\n"), want) {
			t.Fatalf("pill %q missing: %q", want, lines)
		}
	}
	c.bodyInput, _ = c.bodyInput.Update(tea.KeyMsg{Type: tea.KeyTab})
	if hints := plainComposeHintText(c.composeHints(110, chrome)); strings.Contains(hints, "enter ") {
		t.Fatalf("Enter is already on the Replace pill: %q", hints)
	}
}

func TestComposeFooterHeightNeverMovesTheBody(t *testing.T) {
	styles := BuildStyles(CatppuccinMocha, "compact", "square")
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.bodyInput.SetValue("cat cat")
	_, _, want, _ := c.composeLayout(80, 30, styles)
	check := func(state string) {
		t.Helper()
		hints, _, bodyH, _ := c.composeLayout(80, 30, styles)
		if lipgloss.Height(hints) != 3 || bodyH != want {
			t.Fatalf("%s: footer %d rows, body %d (want 3, %d)", state, lipgloss.Height(hints), bodyH, want)
		}
	}
	for _, field := range []composeField{composeFieldFrom, composeFieldTo, composeFieldCC, composeFieldBCC, composeFieldSubject, composeFieldBody} {
		c.focusedField = field
		check("focus")
	}
	c.statusMsg = "Draft saved"
	check("status message")
	c.statusMsg = ""
	c.bodyInput.ed.OpenSearch()
	check("find")
	c.bodyInput.ed.OpenReplace()
	check("replace")
	c.bodyInput.ed.CloseSearch()
	c.busy = true
	check("sending")
}

func TestComposeFooterRowsAreStateActionsContext(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.pgpAvailable, c.pgpEncrypt = true, true
	c.statusMsg = "Draft saved"
	chrome := newManagerChrome(110, CatppuccinMocha, false)
	lines := strings.Split(plainComposeHintText(c.composeHints(110, chrome)), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 footer rows, got %q", lines)
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "draft saved") || !strings.Contains(lines[0], "alt+e encrypt ●") || !strings.Contains(lines[0], "alt+s sign ○") {
		t.Fatalf("state row: %q", lines[0])
	}
	if !strings.Contains(lines[1], "^p commands") || strings.Contains(lines[1], "schedule") {
		t.Fatalf("Ctrl+P opens the command palette: %q", lines[1])
	}
	if !strings.Contains(lines[2], "^f find") || !strings.Contains(lines[2], "^c copy") {
		t.Fatalf("body keys missing: %q", lines[2])
	}
	// Pills share columns: the body's keys snap to the action row's columns.
	col := func(line, word string) int { return lipgloss.Width(line[:strings.Index(line, word)]) }
	if col(lines[1], "^s") != col(lines[2], "esc") || col(lines[1], "^p") != col(lines[2], "^g") || col(lines[1], "^u") != col(lines[2], "^f") {
		t.Fatalf("columns do not line up:\n%s\n%s", lines[1], lines[2])
	}
	c.focusedField = composeFieldFrom
	if text := plainComposeHintText(c.composeHints(110, chrome)); !strings.Contains(text, "enter pick sender") || strings.Contains(text, "^c copy") {
		t.Fatalf("From keys: %q", text)
	}
}

func TestComposeFooterKeepsStaticHintsInPlace(t *testing.T) {
	setEditorSpell(true, "", nil)
	t.Cleanup(func() { setEditorSpell(false, "", nil) })
	col := func(line, word string) int { return lipgloss.Width(line[:strings.Index(line, word)]) }
	for _, width := range []int{76, 110} {
		chrome := newManagerChrome(width, CatppuccinMocha, false)
		c := NewCompose(config.AccountConfig{}, nil, nil)
		var actions string
		grammarCol := -1
		for _, field := range []composeField{composeFieldFrom, composeFieldTo, composeFieldCC, composeFieldBCC, composeFieldSubject, composeFieldBody} {
			c.focusedField = field
			lines := strings.Split(c.composeHints(width, chrome), "\n")
			if actions == "" {
				actions = lines[1]
			} else if lines[1] != actions {
				t.Fatalf("width %d: action row changed with focus %v", width, field)
			}
			plain := strings.Split(plainComposeHintText(strings.Join(lines, "\n")), "\n")
			if !strings.HasPrefix(strings.TrimSpace(plain[1]), "^s send") {
				t.Fatalf("width %d: Send must lead row 2: %q", width, plain[1])
			}
			if fields := strings.Fields(plain[2]); len(fields) < 6 || strings.Join(fields[:6], " ") != "esc cancel ^g grammar f7 spell" {
				t.Fatalf("width %d: Esc, then the proofing keys, must lead row 3: %q", width, plain[2])
			}
			if grammarCol < 0 {
				grammarCol = col(plain[2], "^g")
			} else if col(plain[2], "^g") != grammarCol || col(plain[2], "esc") != col(plain[1], "^s") || col(plain[2], "f7") != col(plain[1], "^u") {
				t.Fatalf("width %d: Esc or proofing keys moved for focus %v: %q", width, field, plain[2])
			}
		}
		if grammarCol != col(strings.Split(plainComposeHintText(actions), "\n")[0], "^p") {
			t.Fatalf("width %d: Grammar is not under Commands", width)
		}
	}
}

func TestComposeEscOpensTheLastRowInEveryMode(t *testing.T) {
	chrome := newManagerChrome(110, CatppuccinMocha, false)
	accounts := composeSenderAccounts()
	c := NewCompose(accounts[0], accounts, nil)
	c.openSenderPicker()
	if lines := strings.Split(plainComposeHintText(c.composeHints(110, chrome)), "\n"); !strings.HasPrefix(strings.TrimSpace(lines[2]), "esc cancel") {
		t.Fatalf("sender picker: %q", lines[2])
	}
	c = NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.ed.OpenSearch()
	if lines := strings.Split(plainComposeHintText(c.composeHints(110, chrome)), "\n"); !strings.HasPrefix(strings.TrimSpace(lines[2]), "esc close") {
		t.Fatalf("find: %q", lines[2])
	}
	c.bodyInput.ed.OpenReplace()
	if lines := strings.Split(plainComposeHintText(c.composeHints(110, chrome)), "\n"); !strings.HasPrefix(strings.TrimSpace(lines[2]), "esc close") {
		t.Fatalf("find and replace: %q", lines[2])
	}
}

func TestComposeEscPillIsRedAndSendIsGreen(t *testing.T) {
	trueColor(t)
	chrome := newManagerChrome(110, CatppuccinMocha, false)
	// The SGR parameters lipgloss emits for a background color; it may merge
	// them with the foreground into one sequence.
	bgSeq := func(color lipgloss.Color) string {
		probe := lipgloss.NewStyle().Background(color).Render("x")
		return strings.TrimSuffix(strings.TrimPrefix(probe[:strings.Index(probe, "x")], "\x1b["), "m")
	}
	red := bgSeq(mixColors(chrome.baseBg, chrome.errorFg, 0.16))
	green := bgSeq(mixColors(chrome.baseBg, chrome.successFg, 0.16))
	if red == green {
		t.Fatal("theme must give Esc and Send different tints")
	}
	if esc := composeKeyHint("esc", "close", chrome); !strings.Contains(esc, red) || strings.Contains(esc, green) {
		t.Fatalf("Esc pill is not red: %q", esc)
	}
	if send := composeKeyHint("^s", "send", chrome); !strings.Contains(send, green) {
		t.Fatalf("Send pill is not green: %q", send)
	}
	if neutral := composeKeyHint("f6", "templates", chrome); strings.Contains(neutral, red) || strings.Contains(neutral, green) {
		t.Fatal("other pills must stay neutral")
	}
	if plain := ansi.Strip(composeKeyHint("esc", "cancel", newManagerChrome(110, VT52, true))); plain != "[Esc] Cancel" {
		t.Fatalf("plain Esc: %q", plain)
	}
}

func TestComposeVimBodyHintsFollowTheMode(t *testing.T) {
	setEditorVimMode(true)
	t.Cleanup(func() { setEditorVimMode(false) })
	trueColor(t)
	chrome := newManagerChrome(110, CatppuccinMocha, false)
	probe := lipgloss.NewStyle().Background(mixColors(chrome.baseBg, chrome.errorFg, 0.16)).Render("x")
	red := strings.TrimSuffix(strings.TrimPrefix(probe[:strings.Index(probe, "x")], "\x1b["), "m")

	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.SetValue("the cat sat")
	key := func(msg tea.KeyMsg) { c.bodyInput, _ = c.bodyInput.Update(msg) }
	normal := func() { c.bodyInput.ed.StartInsert(); key(tea.KeyMsg{Type: tea.KeyEsc}) }
	cases := []struct {
		mode  string
		enter func()
		row   string
	}{
		{"NORMAL", normal, "esc cancel ^g grammar / find y yank p put u undo"},
		{"INSERT", func() { c.bodyInput.ed.StartInsert() }, "esc normal ^g grammar"},
		{"VISUAL", func() { normal(); key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")}) }, "esc normal ^g grammar y yank d delete c change"},
		{"COMMAND", func() { normal(); key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")}) }, "esc normal ^g grammar enter run :w send :q cancel"},
	}
	var actions string
	escCol := -1
	for _, tc := range cases {
		tc.enter()
		if c.bodyInput.Mode() != tc.mode {
			t.Fatalf("setup reached %q, want %q", c.bodyInput.Mode(), tc.mode)
		}
		footer := c.composeHints(110, chrome)
		lines := strings.Split(footer, "\n")
		if actions == "" {
			actions = lines[1]
		} else if lines[1] != actions {
			t.Fatalf("%s: action row changed with the vim mode", tc.mode)
		}
		plain := strings.Split(plainComposeHintText(footer), "\n")
		if got := strings.Join(strings.Fields(plain[2]), " "); got != tc.row {
			t.Fatalf("%s: row 3 = %q, want %q", tc.mode, got, tc.row)
		}
		if col := strings.Index(plain[2], "esc"); escCol < 0 {
			escCol = col
		} else if col != escCol {
			t.Fatalf("%s: Esc moved", tc.mode)
		}
		// Red marks an Esc that cancels compose; Esc back to Normal is neutral.
		if strings.Contains(lines[2], red) != (tc.mode == "NORMAL") {
			t.Fatalf("%s: Esc tint is wrong: %q", tc.mode, lines[2])
		}
	}
	// Vim keys keep their case: y yanks, Y would yank the whole line.
	if yank := ansi.Strip(composeKeyHint("y", "yank", chrome)); !strings.HasPrefix(yank, pillCapLeft+"y"+pillCapRight) {
		t.Fatalf("vim key changed case: %q", yank)
	}
}

func TestComposeFindPillsAndMatchTint(t *testing.T) {
	trueColor(t)
	chrome := newManagerChrome(110, CatppuccinMocha, false)
	params := func(color lipgloss.Color) string {
		probe := lipgloss.NewStyle().Background(color).Render("x")
		return strings.TrimSuffix(strings.TrimPrefix(probe[:strings.Index(probe, "x")], "\x1b["), "m")
	}
	tint, accent := params(mixColors(chrome.baseBg, chrome.accent, 0.28)), params(chrome.accent)
	on, off := composeSearchCell("Aa", false, true, true, chrome), composeSearchCell(".*", false, false, true, chrome)
	if lipgloss.Width(on) != 4 || lipgloss.Width(off) != 4 {
		t.Fatal("cells must fill exactly the label plus Ripple's two padding cells")
	}
	if !strings.Contains(on, pillCapLeft) || !strings.Contains(on, tint) {
		t.Fatalf("a switched-on toggle is a tinted pill: %q", on)
	}
	if strings.Contains(off, pillCapLeft) {
		t.Fatalf("a switched-off toggle is bare text: %q", off)
	}
	// Focus keeps the toggle's state visible: off gains a neutral pill, not the accent.
	if focusedOff := composeSearchCell(".*", true, false, true, chrome); !strings.Contains(focusedOff, pillCapLeft) || strings.Contains(focusedOff, accent) {
		t.Fatalf("focused off toggle: %q", focusedOff)
	}
	if focused := composeSearchCell("All", true, true, false, chrome); !strings.Contains(focused, accent) {
		t.Fatalf("a focused button is an accent pill: %q", focused)
	}
	if unavailable := composeSearchCell("Replace", false, false, false, chrome); strings.Contains(unavailable, pillCapLeft) {
		t.Fatal("an unavailable button is bare text")
	}

	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.SetValue("cat cat")
	c.bodyInput.ed.OpenReplace()
	c.bodyInput.ed.SetSearchQuery("cat")
	view := c.View(110, 30, BuildStyles(CatppuccinMocha, "compact", "square"))
	if !strings.Contains(view, params(mixColors(chrome.fieldBg, chrome.accent, 0.3))) {
		t.Fatal("other matches need the tinted background")
	}
	if regexp.MustCompile(`\x1b\[(?:[0-9]+;)*4(?:;[0-9]+)*m`).MatchString(view) {
		t.Fatal("underline is reserved for spell check")
	}
	plain := c.composeHints(110, newManagerChrome(110, VT52, true))
	if strings.Contains(plain, pillCapLeft) || !strings.Contains(ansi.Strip(plain), "[x] Aa") {
		t.Fatalf("plain themes keep bracketed controls: %q", ansi.Strip(plain))
	}
}

// Replace All works straight from the inputs: no Tab onto the button.
func TestComposeReplaceAllWithoutTabbing(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()
	c.bodyInput.SetValue("cat sat on the cat mat")
	press := func(msg tea.KeyMsg) {
		t.Helper()
		next, _, done := c.Update(msg, DefaultKeys)
		c = next
		if done {
			t.Fatalf("%s closed compose", msg)
		}
	}
	press(tea.KeyMsg{Type: tea.KeyCtrlF})
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("cat")})
	press(tea.KeyMsg{Type: tea.KeyCtrlR})
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("dog")})
	press(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if got := c.bodyInput.Value(); got != "dog sat on the dog mat" {
		t.Fatalf("Alt+Enter did not replace all: %q", got)
	}
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c"), Alt: true})
	if c.bodyInput.ed.Search().CaseSensitive || c.bodyInput.ed.Search().Replacement != "dog" {
		t.Fatalf("Alt+C must toggle case without typing: %+v", c.bodyInput.ed.Search())
	}
}

func TestComposeHasNoGroupTitles(t *testing.T) {
	view := ansi.Strip(NewCompose(config.AccountConfig{}, nil, nil).View(80, 30, BuildStyles(CatppuccinMocha, "compact", "square")))
	for _, line := range strings.Split(view, "\n") {
		if title := strings.TrimSpace(line); title == "recipients" || title == "message" {
			t.Fatalf("group title %q still rendered", title)
		}
	}
}

func TestComposeLargerGeometryAndStoredEditorAgree(t *testing.T) {
	if composeOverlayWidth(160) != 110 || composeOverlayHeight(80) != 48 {
		t.Fatal("compose must use the larger desktop size")
	}
	for _, size := range [][2]int{{80, 24}, {120, 40}, {160, 60}} {
		m, _ := newMailboxListModel(7, 1)
		m.width, m.height = size[0], size[1]
		m.overlay = overlayCompose
		m.compose = NewCompose(config.AccountConfig{}, nil, nil)
		m.compose.focusedField = composeFieldBody
		m.compose.bodyInput.SetValue(strings.Repeat("A paragraph that wraps in the compose editor. ", 60))
		next, _ := m.handleCompose(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
		width, height := composeOverlayWidth(m.width), composeOverlayHeight(m.height)
		_, bodyW, bodyH, _ := m.compose.composeLayout(width, height, m.styles)
		storedW, storedH := m.compose.bodyInput.ed.Size()
		if storedW != bodyW || storedH != bodyH {
			t.Fatalf("stored size %dx%d differs from render %dx%d", storedW, storedH, bodyW, bodyH)
		}
		view := m.View()
		if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
			t.Fatalf("compose exceeds %dx%d", m.width, m.height)
		}
		if !strings.Contains(plainComposeHintText(view), "send") {
			t.Fatal("Send hidden by enlarged geometry")
		}
	}
}
