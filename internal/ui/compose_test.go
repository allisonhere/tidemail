package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func composeFieldLine(view, label string) string {
	stripped := ansi.Strip(view)
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, label) {
			return line
		}
	}
	return ""
}

func TestComposeActionsStayVisibleInShortView(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()
	c.bodyInput.SetValue(strings.Repeat("this is a long compose line that wraps inside the body editor ", 20))

	view := c.View(60, 13, BuildStyles(CatppuccinMocha, "compact"))
	stripped := ansi.Strip(view)

	if !strings.Contains(stripped, "send") {
		t.Fatalf("expected compose actions to stay visible in short view, got %q", stripped)
	}
}

func TestComposeActionsStayVisibleInOverlay(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	m = next.(Model)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.Focus()
	m.compose.bodyInput.SetValue(strings.Repeat("this is a long compose line that wraps inside the body editor ", 40))

	view := m.View()
	stripped := ansi.Strip(view)

	if !strings.Contains(stripped, "send") {
		t.Fatalf("expected compose actions to stay visible in overlay, got %q", stripped)
	}
}

func TestComposeActionsWrapInNarrowView(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	view := c.View(32, 16, BuildStyles(CatppuccinMocha, "compact"))
	stripped := ansi.Strip(view)

	for _, want := range []string{"send", "grammar"} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("expected compose action %q to stay visible in narrow view, got %q", want, stripped)
		}
	}
}

func TestComposeTabAdvancesAfterAcceptingRecipientSuggestion(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, []string{"Alice <alice@example.com>"})

	var cmd tea.Cmd
	c, cmd, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ali"), Paste: true}, DefaultKeys)
	if cmd != nil {
		cmd()
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if got := c.toInput.Value(); got != "Alice <alice@example.com>" {
		t.Fatalf("expected tab to accept To suggestion, got %q", got)
	}
	if c.focusedField != composeFieldCC {
		t.Fatalf("tab should accept To suggestion and advance to CC, got focus %v", c.focusedField)
	}
	toLine := composeFieldLine(c.View(90, 24, BuildStyles(CatppuccinMocha, "compact")), "To")
	if strings.Contains(toLine, " >") {
		t.Fatalf("expected To row not to show focus marker after advancing to CC, got %q", toLine)
	}
}

func TestComposeTabAdvancesAfterAcceptingCCSuggestion(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, []string{"Carol <carol@example.com>"})
	c.advanceField(1)

	var cmd tea.Cmd
	c, cmd, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("car"), Paste: true}, DefaultKeys)
	if cmd != nil {
		cmd()
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if got := c.ccInput.Value(); got != "Carol <carol@example.com>" {
		t.Fatalf("expected tab to accept CC suggestion, got %q", got)
	}
	if c.focusedField != composeFieldBCC {
		t.Fatalf("tab should accept CC suggestion and advance to BCC, got focus %v", c.focusedField)
	}
}

func TestComposeMultiRecipientSegmentCompletion(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, []string{"Bob <bob@example.com>"})

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("alice@x.com, bo"), Paste: true}, DefaultKeys)
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)

	if got := c.toInput.Value(); got != "alice@x.com, Bob <bob@example.com>" {
		t.Fatalf("expected tab to complete only the last segment, got %q", got)
	}
	if c.focusedField != composeFieldCC {
		t.Fatalf("tab should advance to CC after accepting, got focus %v", c.focusedField)
	}
}

func TestComposeDropdownNavigationEnterSelectsAndStays(t *testing.T) {
	book := []string{"Alice <alice@example.com>", "Alicia <alicia@example.com>"}
	c := NewCompose(config.AccountConfig{}, nil, book)

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ali"), Paste: true}, DefaultKeys)
	if len(c.suggestions) != 2 {
		t.Fatalf("expected 2 suggestions for %q, got %v", "ali", c.suggestions)
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyDown}, DefaultKeys)
	if c.suggestCursor != 1 {
		t.Fatalf("expected down to move dropdown cursor to 1, got %d", c.suggestCursor)
	}

	view := c.View(90, 30, BuildStyles(CatppuccinMocha, "compact"))
	if !strings.Contains(view, "alicia@example.com") {
		t.Fatal("expected the open dropdown to render its candidates")
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if got := c.toInput.Value(); got != "Alicia <alicia@example.com>" {
		t.Fatalf("expected enter to insert the highlighted candidate, got %q", got)
	}
	if c.focusedField != composeFieldTo {
		t.Fatalf("enter should keep focus in the To field, got %v", c.focusedField)
	}
}

func TestComposeEscDismissesDropdownWithoutClosing(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, []string{"Alice <alice@example.com>"})

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ali"), Paste: true}, DefaultKeys)
	if !c.suggestionsVisible() {
		t.Fatal("expected dropdown to open after typing a partial match")
	}

	var exit bool
	c, _, exit = c.Update(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if exit {
		t.Fatal("esc with the dropdown open must dismiss it, not close compose")
	}
	if c.suggestionsVisible() {
		t.Fatal("expected esc to dismiss the dropdown")
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c"), Paste: true}, DefaultKeys)
	if !c.suggestionsVisible() {
		t.Fatal("expected typing after esc to reopen the dropdown")
	}
}

func TestComposeNoSuggestionsForEmptySegmentOrExactMatch(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, []string{"Alice <alice@example.com>"})
	if c.suggestionsVisible() {
		t.Fatal("expected no dropdown before typing")
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Alice <alice@example.com>"), Paste: true}, DefaultKeys)
	if c.suggestionsVisible() {
		t.Fatalf("expected no dropdown for an exact match, got %v", c.suggestions)
	}
}

func composeSenderAccounts() []config.AccountConfig {
	return []config.AccountConfig{
		{Name: "Personal", User: "allie@personal.example", From: "Allie <allie@personal.example>", Signature: "Personal sig"},
		{Name: "Work", User: "allie@work.example", From: "Allie at Work <allie@work.example>", Signature: "Work sig"},
	}
}

func TestComposeSenderDropdownSelectsAndCancels(t *testing.T) {
	accounts := composeSenderAccounts()
	c := NewCompose(accounts[0], accounts, nil)

	// From is immediately before To, so reverse-tab reaches it without making
	// new-message compose start anywhere other than the recipient field.
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyShiftTab}, DefaultKeys)
	if c.focusedField != composeFieldFrom {
		t.Fatalf("expected shift+tab from To to focus From, got %v", c.focusedField)
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if !c.senderPickerOpen || c.senderCursor != 0 {
		t.Fatalf("expected sender picker at current account, open=%v cursor=%d", c.senderPickerOpen, c.senderCursor)
	}
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyDown}, DefaultKeys)
	if c.senderCursor != 1 || c.accountIndex != 0 {
		t.Fatalf("highlight must not commit sender, cursor=%d account=%d", c.senderCursor, c.accountIndex)
	}

	view := ansi.Strip(c.View(74, 30, BuildStyles(CatppuccinMocha, "compact")))
	for _, want := range []string{"Personal", "Work", "allie@work.example"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected sender dropdown to show %q, got %q", want, view)
		}
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if c.senderPickerOpen || c.accountIndex != 0 {
		t.Fatalf("expected escape to cancel sender choice, open=%v account=%d", c.senderPickerOpen, c.accountIndex)
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeySpace}, DefaultKeys)
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyDown}, DefaultKeys)
	before := c
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if c.senderPickerOpen || c.accountIndex != 1 {
		t.Fatalf("expected enter to choose Work, open=%v account=%d", c.senderPickerOpen, c.accountIndex)
	}
	if !composeChanged(before, c) {
		t.Fatal("expected confirmed sender choice to participate in draft autosave")
	}
	draft := c.toDraftRecord()
	if draft.AccountIndex != 1 || draft.AccountName != "Work" || draft.AccountUser != "allie@work.example" {
		t.Fatalf("expected draft to retain selected sender, got %+v", draft)
	}
}

func TestComposeSenderDropdownUsesSelectedAccountWhenSending(t *testing.T) {
	accounts := composeSenderAccounts()
	c := NewForward(db.Message{From: "source@example.com", Subject: "News", BodyText: "Original"}, accounts[0], accounts, nil)
	c.focusedField = composeFieldFrom
	c.openSenderPicker()
	c.senderCursor = 1
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	c.toInput.SetValue("reader@example.com")
	c.bodyInput.SetValue("Forwarding this")

	_, cmd, _ := c.send()
	queued := cmd().(SendQueuedMsg)
	if queued.Account.Name != "Work" || queued.Account.User != "allie@work.example" {
		t.Fatalf("expected Work SMTP account, got %+v", queued.Account)
	}
	if !strings.Contains(queued.Msg.Body, "-- \nWork sig") {
		t.Fatalf("expected Work signature, got %q", queued.Msg.Body)
	}
}

func TestComposeSenderDropdownCapsVisibleRows(t *testing.T) {
	accounts := make([]config.AccountConfig, 7)
	for i := range accounts {
		accounts[i] = config.AccountConfig{Name: fmt.Sprintf("Account %d", i+1), User: fmt.Sprintf("user%d@example.com", i+1)}
	}
	c := NewCompose(accounts[0], accounts, nil)
	c.focusedField = composeFieldFrom
	c.openSenderPicker()
	c.senderCursor = len(accounts) - 1

	rows := c.renderSenderRows(74, 10, newManagerChrome(74, CatppuccinMocha, false))
	if len(rows) != maxSenderOptions {
		t.Fatalf("expected at most %d sender rows, got %d", maxSenderOptions, len(rows))
	}
	view := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(view, "Account 7") || strings.Contains(view, "Account 1") {
		t.Fatalf("expected sender window to scroll to final account, got %q", view)
	}
}

func TestComposeSenderFieldSkippedForSingleAccount(t *testing.T) {
	account := config.AccountConfig{Name: "Only", User: "only@example.com"}
	c := NewCompose(account, []config.AccountConfig{account}, nil)
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyShiftTab}, DefaultKeys)
	if c.focusedField != composeFieldBody {
		t.Fatalf("expected single-account compose to skip From, got %v", c.focusedField)
	}
}

func TestComposeCtrlUStillQuickCyclesSender(t *testing.T) {
	accounts := composeSenderAccounts()
	c := NewCompose(accounts[0], accounts, nil)
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlU}, DefaultKeys)
	if c.accountIndex != 1 || c.senderPickerOpen {
		t.Fatalf("expected ctrl+u to quick-cycle without opening picker, account=%d open=%v", c.accountIndex, c.senderPickerOpen)
	}
}

func TestSenderPickerIsAvailableAcrossComposeModes(t *testing.T) {
	accounts := composeSenderAccounts()
	message := db.Message{From: "source@example.com", Subject: "Update", BodyText: "Original", MessageID: "<original@example.com>"}
	draft := db.Draft{AccountName: "Work", AccountUser: "allie@work.example", AccountIndex: 1}

	models := []ComposeModel{
		NewCompose(accounts[1], accounts, nil),
		NewReply(message, accounts[1], accounts, nil),
		NewForward(message, accounts[1], accounts, nil),
		NewComposeFromDraft(draft, accounts, nil),
	}
	for i, c := range models {
		if c.accountIndex != 1 {
			t.Fatalf("compose mode %d did not preserve its initial sender: %d", i, c.accountIndex)
		}
		c.focusedField = composeFieldFrom
		c.openSenderPicker()
		if !c.senderPickerOpen || c.senderCursor != 1 {
			t.Fatalf("compose mode %d did not open sender picker at current account", i)
		}
	}
}

func TestComposeLayoutBudgetsSenderDropdownRows(t *testing.T) {
	accounts := composeSenderAccounts()
	c := NewCompose(accounts[0], accounts, nil)
	_, _, closedH := c.composeLayout(74, 32, BuildStyles(CatppuccinMocha, "compact"))
	c.focusedField = composeFieldFrom
	c.openSenderPicker()
	_, _, openH := c.composeLayout(74, 32, BuildStyles(CatppuccinMocha, "compact"))
	if closedH-openH != len(accounts) {
		t.Fatalf("expected dropdown to reserve %d rows, closed=%d open=%d", len(accounts), closedH, openH)
	}
}

func TestReplyRecipientAutocomplete(t *testing.T) {
	c := NewReply(db.Message{From: "alice@example.com", MessageID: "<x>", Subject: "S"},
		config.AccountConfig{}, nil, []string{"Bob <bob@example.com>"})

	// Reply lands in the body; wrap around to the prefilled To field and add a
	// second recipient.
	c.advanceField(1)
	if c.focusedField != composeFieldTo {
		t.Fatalf("expected focus on To after advancing from body, got %v", c.focusedField)
	}
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(", bo"), Paste: true}, DefaultKeys)
	if len(c.suggestions) != 1 || c.suggestions[0] != "Bob <bob@example.com>" {
		t.Fatalf("expected reply compose to suggest Bob, got %v", c.suggestions)
	}
}

func TestSendAppendsAccountSignature(t *testing.T) {
	acfg := config.AccountConfig{Signature: "Allie\nTideMail"}
	c := NewCompose(acfg, nil, nil)
	c.toInput.SetValue("bob@example.com")
	c.bodyInput.SetValue("hi bob")

	_, cmd, _ := c.send()
	if cmd == nil {
		t.Fatal("expected send to produce a command")
	}
	queued, ok := cmd().(SendQueuedMsg)
	if !ok {
		t.Fatalf("expected SendQueuedMsg, got %T", cmd())
	}
	want := "hi bob\n\n-- \nAllie\nTideMail\n"
	if queued.Msg.Body != want {
		t.Fatalf("expected signature appended, got %q", queued.Msg.Body)
	}
}

func TestSendWithoutSignatureLeavesBodyAlone(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.toInput.SetValue("bob@example.com")
	c.bodyInput.SetValue("hi bob")

	_, cmd, _ := c.send()
	queued := cmd().(SendQueuedMsg)
	if queued.Msg.Body != "hi bob" {
		t.Fatalf("expected untouched body, got %q", queued.Msg.Body)
	}
}

func TestReplyTypingStartsAboveQuotedMessage(t *testing.T) {
	c := NewReply(db.Message{
		From:      "alice@example.com",
		Subject:   "Plans",
		MessageID: "<plans@example.com>",
		BodyText:  "quoted line",
	}, config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")}, DefaultKeys)

	got := c.bodyInput.Value()
	if !strings.HasPrefix(got, "H\n\nOn alice@example.com wrote:\n> quoted line") {
		t.Fatalf("expected typed reply text above quoted original, got %q", got)
	}
}

func TestComposeBodyNeverEscapesWidth(t *testing.T) {
	// The editor wraps with go-runewidth while lipgloss measures with x/ansi;
	// for emoji/flags they disagree. Compose must frame (clip+pad), not re-wrap,
	// the editor output — otherwise overflow escapes to the app's left edge.
	styles := BuildStyles(BuiltinThemes[0], "compact")
	contents := []string{
		strings.Repeat("🇺🇸", 40), // regional-indicator flags (worst offender)
		strings.Repeat("👍", 60),  // emoji
		strings.Repeat("x", 300), // long unbroken ASCII
		strings.Repeat("hello world ", 40),
	}
	for _, w := range []int{30, 40, 60, 80} {
		for _, content := range contents {
			c := NewCompose(config.AccountConfig{}, nil, nil)
			c.focusedField = composeFieldBody
			c.bodyInput.Focus()
			c.bodyInput.SetValue(content)
			for i, line := range strings.Split(c.View(w, 24, styles), "\n") {
				if got := ansi.StringWidth(line); got != w {
					t.Fatalf("width=%d content=%.8q: line %d has display width %d, want %d",
						w, content, i, got, w)
				}
			}
		}
	}
}

// runCmd runs a tea.Cmd and returns every leaf message it produces, recursing
// into batched commands so tests can observe commands wrapped in a Batch.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestComposeCutWritesClipboardWhileAutosaving(t *testing.T) {
	// Cut changes the body, which triggers autosave in handleCompose. The
	// clipboard-write command must survive that, not be swallowed by the
	// draft-save command.
	var copied string
	defer stubClipboardWrite(t, &copied)()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.Focus()
	m.compose.bodyInput.SetValue("cut me")

	n2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlA}) // select all
	m = n2.(Model)
	n3, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX}) // cut
	m = n3.(Model)

	runCmd(cmd)
	if copied != "cut me" {
		t.Fatalf("cut should write the selection to the clipboard, got %q", copied)
	}
	if got := m.compose.bodyInput.Value(); got != "" {
		t.Fatalf("cut should empty the body, got %q", got)
	}
}

func TestComposeBodyEditorSyncedToRenderWidth(t *testing.T) {
	// The stored body editor must wrap at the same width it's rendered at, or
	// up/down navigation skips/sticks relative to the displayed wrapping.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m.overlay = overlayCompose
	m.compose = NewReply(db.Message{From: "A", MessageID: "<x>", Subject: "S", BodyText: "hello"},
		config.AccountConfig{}, nil, nil)
	m.compose.focusedField = composeFieldBody

	// Any key routed through handleCompose should size the stored editor.
	n, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = n.(Model)

	if got, want := m.compose.bodyInput.w, composeBodyWidth(120); got != want {
		t.Fatalf("stored body editor width = %d, want render width %d", got, want)
	}
}

func TestComposeAttachMovedOffCtrlA(t *testing.T) {
	// ctrl+a in the body selects all and does NOT open the picker.
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi there")}, DefaultKeys)
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlA}, DefaultKeys)
	if c.picker.active {
		t.Fatal("ctrl+a must not open the attach picker anymore")
	}
	if got := c.bodyInput.SelectedText(); got != "hi there" {
		t.Fatalf("ctrl+a should select all, got %q", got)
	}

	// alt+f opens the attach picker.
	c2 := NewCompose(config.AccountConfig{}, nil, nil)
	c2.focusedField = composeFieldBody
	c2, _, _ = c2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f"), Alt: true}, DefaultKeys)
	if !c2.picker.active {
		t.Fatal("alt+f should open the attach picker")
	}
}

func TestComposeBodyCopyCutToClipboard(t *testing.T) {
	var copied string
	defer stubClipboardWrite(t, &copied)()

	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")}, DefaultKeys)
	// ctrl+a selects all in the body (it no longer triggers attach).
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlA}, DefaultKeys)
	if got := c.bodyInput.SelectedText(); got != "hello" {
		t.Fatalf("ctrl+a should select all body text, got %q", got)
	}

	// Copy: clipboard receives the selection; the body is unchanged.
	var cmd tea.Cmd
	c, cmd, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlC}, DefaultKeys)
	if cmd == nil {
		t.Fatal("ctrl+c should return a clipboard write command")
	}
	cmd()
	if copied != "hello" {
		t.Fatalf("copy should put the selection on the clipboard, got %q", copied)
	}
	if got := c.bodyInput.Value(); got != "hello" {
		t.Fatalf("copy must not modify the body, got %q", got)
	}

	// Cut: clipboard receives the selection; the body is emptied.
	copied = ""
	c, cmd, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlX}, DefaultKeys)
	if cmd == nil {
		t.Fatal("ctrl+x should return a clipboard write command")
	}
	cmd()
	if copied != "hello" {
		t.Fatalf("cut should put the selection on the clipboard, got %q", copied)
	}
	if got := c.bodyInput.Value(); got != "" {
		t.Fatalf("cut should remove the selection from the body, got %q", got)
	}
}

// TestComposeBodyCaretFollowsTypingAboveQuote guards against the body editor
// pinning the caret to the top while typing a reply: the stored editor must be
// sized to the rendered body height (not left at height 1), or each keystroke
// scrolls the freshly typed lines off the top behind the quote.
func TestComposeBodyCaretFollowsTypingAboveQuote(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 33})
	m = mm.(Model)

	// Reply-shaped body: a long quote below, caret moved to the top.
	var quote strings.Builder
	quote.WriteString("\n\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&quote, "> QUOTED%02d\n", i)
	}
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.SetValue(quote.String())
	m.compose.bodyInput.Focus()
	m.compose.bodyInput, _ = m.compose.bodyInput.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})

	// Type a few reply lines at the top.
	for i := 0; i < 4; i++ {
		mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(fmt.Sprintf("MYREPLY%02d", i))})
		m = mm.(Model)
		mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = mm.(Model)
	}

	if top := m.compose.bodyInput.ed.ViewportTop(); top != 0 {
		t.Fatalf("typing at the top of a reply should not scroll the body (viewportTop=%d)", top)
	}
	view := ansi.Strip(m.View())
	for i := 0; i < 4; i++ {
		if marker := fmt.Sprintf("MYREPLY%02d", i); !strings.Contains(view, marker) {
			t.Fatalf("typed reply line %q scrolled off the top of the body", marker)
		}
	}
}

func TestForwardTypingStartsAboveQuotedMessage(t *testing.T) {
	c := NewForward(db.Message{
		From:      "alice@example.com",
		Subject:   "Plans",
		MessageID: "<plans@example.com>",
		BodyText:  "forwarded line",
	}, config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")}, DefaultKeys)

	got := c.bodyInput.Value()
	if !strings.HasPrefix(got, "H\n\n---------- Forwarded message ----------\nFrom: alice@example.com") {
		t.Fatalf("expected typed forward text above quoted original, got %q", got)
	}
	if !strings.Contains(got, "> forwarded line") {
		t.Fatalf("expected forwarded body to remain quoted, got %q", got)
	}
}

func TestCtrlVPastesClipboardIntoComposeBody(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.Focus()

	restore := stubClipboardRead(t, "pasted text")
	defer restore()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected ctrl+v in compose to read clipboard")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)

	if got := m.compose.bodyInput.Value(); got != "pasted text" {
		t.Fatalf("expected clipboard text pasted into compose body, got %q", got)
	}
}

func TestCtrlPOpensCommandPaletteInCompose(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.Focus()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(Model)

	if m.overlay != overlayCommandPalette {
		t.Fatalf("expected ctrl+p to open the command palette, overlay = %v", m.overlay)
	}
}

func stubClipboardRead(t *testing.T, text string) func() {
	t.Helper()
	orig := clipboardReadCmd
	clipboardReadCmd = func() tea.Cmd {
		return func() tea.Msg {
			return ClipboardReadMsg{Text: text}
		}
	}
	return func() { clipboardReadCmd = orig }
}

func TestComposeFromDraftRestoresFieldsAndAttachments(t *testing.T) {
	draft := db.Draft{
		ID:           7,
		AccountName:  "Personal",
		AccountUser:  "allie@example.com",
		AccountIndex: 1,
		To:           "bob@example.com",
		CC:           "carol@example.com",
		Subject:      "Saved subject",
		BodyText:     "Saved body",
		InReplyTo:    "<orig@example.com>",
		References:   "<orig@example.com>",
		Attachments:  []db.DraftAttachment{{Filename: "notes.txt", Path: "/tmp/notes.txt", Data: []byte("notes")}},
	}
	accounts := []config.AccountConfig{{Name: "Other", User: "other@example.com"}, {Name: "Personal", User: "allie@example.com"}}

	c := NewComposeFromDraft(draft, accounts, nil)

	if c.draftID != draft.ID || c.toInput.Value() != draft.To || c.ccInput.Value() != draft.CC || c.subjectInput.Value() != draft.Subject || c.bodyInput.Value() != draft.BodyText {
		t.Fatalf("draft fields not restored: %+v", c)
	}
	if c.inReplyTo != draft.InReplyTo || c.references != draft.References || c.accountIndex != 1 {
		t.Fatalf("draft metadata not restored: %+v", c)
	}
	if len(c.attachments) != 1 || c.attachments[0].Name != "notes.txt" || string(c.attachments[0].Data) != "notes" {
		t.Fatalf("draft attachments not restored: %+v", c.attachments)
	}
}

func TestComposeEscapeWithContentOpensDraftCloseConfirm(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{Name: "Personal", User: "allie@example.com"}, []config.AccountConfig{{Name: "Personal", User: "allie@example.com"}}, nil)
	m.compose.subjectInput.SetValue("Keep me")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if cmd != nil {
		t.Fatal("escape should open confirmation modal before saving")
	}
	if m.overlay != overlayDraftCloseConfirm {
		t.Fatalf("expected draft close confirmation overlay, got %v", m.overlay)
	}
}

func TestDraftCloseConfirmSavesDraft(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.overlay = overlayDraftCloseConfirm
	m.compose = NewCompose(config.AccountConfig{Name: "Personal", User: "allie@example.com"}, []config.AccountConfig{{Name: "Personal", User: "allie@example.com"}}, nil)
	m.compose.toInput.SetValue("bob@example.com")
	m.compose.subjectInput.SetValue("Keep me")
	m.compose.bodyInput.SetValue("Body")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = next.(Model)
	if m.overlay != overlayNone {
		t.Fatalf("expected compose closed after saving draft, got overlay %v", m.overlay)
	}
	if cmd == nil {
		t.Fatal("expected save draft command")
	}
	msg, ok := cmd().(DraftSavedMsg)
	if !ok || msg.Err != nil {
		t.Fatalf("expected successful DraftSavedMsg, got %#v", msg)
	}
	drafts, err := database.ListDrafts("Personal", "allie@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].Subject != "Keep me" || drafts[0].BodyText != "Body" {
		t.Fatalf("expected saved draft, got %+v", drafts)
	}
}

func TestDraftCloseConfirmDiscardsWithLowercaseD(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.overlay = overlayDraftCloseConfirm
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.bodyInput.SetValue("Body")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(Model)

	if m.overlay != overlayNone {
		t.Fatalf("expected compose closed after discarding draft, got overlay %v", m.overlay)
	}
	if cmd != nil {
		t.Fatalf("expected discard without existing draft to need no command, got %#v", cmd)
	}
}

func TestEnterOnDraftRowReopensCompose(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.focused = paneMessages
	m.accounts = []db.Account{{ID: 1, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{{ID: 2, AccountID: 1, Name: "Drafts"}}
	m.sidebarRows = []sidebarRow{{kind: rowKindMailbox, mailboxID: 2, accountID: 1}}
	m.drafts = []db.Draft{{ID: 9, AccountName: "Personal", Subject: "Reopen me", BodyText: "draft body"}}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("reopening local draft should not need a command")
	}
	if m.overlay != overlayCompose {
		t.Fatalf("expected compose overlay, got %v", m.overlay)
	}
	if m.compose.draftID != 9 || m.compose.subjectInput.Value() != "Reopen me" || m.compose.bodyInput.Value() != "draft body" {
		t.Fatalf("expected reopened draft in compose, got %+v", m.compose)
	}
}

func TestComposeEditAutosavesDraft(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{Name: "Personal", User: "allie@example.com"}, []config.AccountConfig{{Name: "Personal", User: "allie@example.com"}}, nil)
	m.compose.focusedField = composeFieldSubject
	m.compose.toInput.Blur()
	m.compose.subjectInput.Focus()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Draft"), Paste: true})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected edit to autosave draft")
	}
	var saved *DraftSavedMsg
	for _, msg := range runCmd(cmd) {
		if ds, ok := msg.(DraftSavedMsg); ok {
			saved = &ds
		}
	}
	if saved == nil || saved.Err != nil || saved.DraftID == 0 {
		t.Fatalf("expected successful DraftSavedMsg, got %#v", saved)
	}
}

func TestSplitReplyAndQuote(t *testing.T) {
	body := "My reply here.\n\nOn bob@example.com wrote:\n> original line\n> more"
	reply, quote := splitReplyAndQuote(body)
	if reply != "My reply here." {
		t.Fatalf("reply = %q, want %q", reply, "My reply here.")
	}
	if reply+quote != body {
		t.Fatalf("reply+quote != body:\n got %q\nwant %q", reply+quote, body)
	}
	// No quote → whole body is the reply.
	r2, q2 := splitReplyAndQuote("just a new message")
	if r2 != "just a new message" || q2 != "" {
		t.Fatalf("no-quote split = (%q,%q)", r2, q2)
	}
	// Forward header is a boundary too.
	fb := "see below\n\n---------- Forwarded message ----------\nFrom: a@b.com"
	rf, qf := splitReplyAndQuote(fb)
	if rf != "see below" || rf+qf != fb {
		t.Fatalf("forward split = (%q,%q)", rf, qf)
	}
}
