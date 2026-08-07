package ui

import (
	"context"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/allisonhere/ripple"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/smtp"
)

type composeField int

const (
	composeFieldFrom composeField = iota
	composeFieldTo
	composeFieldCC
	composeFieldBCC
	composeFieldSubject
	composeFieldBody
	composeFieldCount
)

type attachmentFile struct {
	Name string
	Path string
	Data []byte
}

// filePicker is a yazi-inspired directory browser for picking attachment files.
type filePicker struct {
	entries    []fileEntry
	cursor     int
	currentDir string
	active     bool
	showHidden bool // when true, dotfiles/dirs are listed
}

type fileEntry struct {
	name  string
	isDir bool
	size  int64
}

type ComposeModel struct {
	toInput      textinput.Model
	ccInput      textinput.Model
	bccInput     textinput.Model
	subjectInput textinput.Model
	bodyInput    editorArea

	focusedField composeField
	inReplyTo    string
	references   string
	isForward    bool // distinguishes a forward from a plain compose (no inReplyTo)
	accountCfg   config.AccountConfig

	accounts     []config.AccountConfig
	accountIndex int

	// senderPickerOpen exposes the account choice behind the From row without
	// changing accountIndex until the highlighted option is confirmed.
	senderPickerOpen bool
	senderCursor     int

	// Recipient autocomplete: addressBook holds "Name <addr>" candidates;
	// suggestions is the dropdown for the segment being typed in the focused
	// To/CC/BCC field. Esc dismisses the dropdown until the value changes.
	addressBook      []string
	suggestions      []string
	suggestCursor    int
	suggestDismissed bool

	attachments    []attachmentFile
	picker         filePicker
	quoteCollapsed bool
	draftID        int64
	dirty          bool
	lastAutosaved  time.Time

	busy      bool
	statusMsg string
	isErr     bool
}

func NewCompose(acfg config.AccountConfig, accounts []config.AccountConfig, addressBook []string) ComposeModel {
	c := ComposeModel{accountCfg: acfg, accounts: accounts}
	if len(accounts) > 1 {
		// Find the given acfg in the list; default to 0 if not found
		for i, a := range accounts {
			if a.IMAPHost == acfg.IMAPHost && a.User == acfg.User {
				c.accountIndex = i
				break
			}
		}
	}
	c.toInput = newComposeInput("to@example.com")
	c.ccInput = newComposeInput("")
	c.ccInput.Placeholder = "cc (optional)"
	c.bccInput = newComposeInput("")
	c.bccInput.Placeholder = "bcc (optional)"
	c.subjectInput = newComposeInput("Subject")
	c.bodyInput = newEditorArea()
	c.bodyInput.SetPlaceholder("Write your message here...")
	c.focusedField = composeFieldTo
	c.toInput.Focus()
	c.SetAddressBook(addressBook)
	return c
}

func NewComposeFromDraft(draft db.Draft, accounts []config.AccountConfig, addressBook []string) ComposeModel {
	acfg := config.AccountConfig{Name: draft.AccountName, User: draft.AccountUser}
	// Trust the stored index only if it still points at the same account; the
	// config may have been reordered since the draft was saved. Otherwise resolve
	// by name/user so the draft never sends from the wrong account.
	if draft.AccountIndex >= 0 && draft.AccountIndex < len(accounts) &&
		accounts[draft.AccountIndex].Name == draft.AccountName &&
		accounts[draft.AccountIndex].User == draft.AccountUser {
		acfg = accounts[draft.AccountIndex]
	} else {
		for i, account := range accounts {
			if account.Name == draft.AccountName && account.User == draft.AccountUser {
				acfg = account
				draft.AccountIndex = i
				break
			}
		}
	}
	c := NewCompose(acfg, accounts, addressBook)
	c.draftID = draft.ID
	c.accountIndex = draft.AccountIndex
	c.toInput.SetValue(draft.To)
	c.ccInput.SetValue(draft.CC)
	c.bccInput.SetValue(draft.BCC)
	c.subjectInput.SetValue(draft.Subject)
	c.bodyInput.SetValue(draft.BodyText)
	c.inReplyTo = draft.InReplyTo
	c.references = draft.References
	c.dirty = false
	for _, att := range draft.Attachments {
		c.attachments = append(c.attachments, attachmentFile{Name: att.Filename, Path: att.Path, Data: att.Data})
	}
	return c
}

// SetAddressBook populates autocomplete candidates for the To/CC/BCC fields.
func (c *ComposeModel) SetAddressBook(addrs []string) {
	c.addressBook = addrs
}

// NewReply creates a compose model pre-filled for replying to a message.
func NewReply(original db.Message, acfg config.AccountConfig, accounts []config.AccountConfig, addressBook []string) ComposeModel {
	c := NewCompose(acfg, accounts, addressBook)
	c.quoteCollapsed = true
	replyTo := original.ReplyTo
	if replyTo == "" {
		replyTo = original.From
	}
	c.toInput.SetValue(replyTo)
	subject := original.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	c.subjectInput.SetValue(subject)
	c.inReplyTo = original.MessageID
	c.references = original.MessageID

	// Quote the original body text
	if original.BodyText != "" {
		plain := ansi.Strip(original.BodyText)
		quoted := quoteReply(plain, original.From)
		c.bodyInput.SetValue(quoted)
	}

	// To/Subject are already filled for a reply, so land in the body with the
	// caret at the top (above the quote), ready to type — and showing a cursor.
	c.focusBodyAtStart()
	return c
}

// NewForward creates a compose model for forwarding a message.
// To field is left empty for the user to fill in. Subject is prefixed with "Fwd:".
// The original body is quoted with a "Forwarded message" header and original attachments are included.
func NewForward(original db.Message, acfg config.AccountConfig, accounts []config.AccountConfig, addressBook []string) ComposeModel {
	c := NewCompose(acfg, accounts, addressBook)
	c.quoteCollapsed = true
	c.isForward = true

	subject := original.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "fwd:") {
		subject = "Fwd: " + subject
	}
	c.subjectInput.SetValue(subject)

	// Quote the original body text with a forward header
	if original.BodyText != "" {
		plain := ansi.Strip(original.BodyText)
		quoted := quoteForward(plain, original)
		c.bodyInput.SetValue(quoted)
		c.moveBodyCursorToStart()
	}

	// Include original attachments
	for _, att := range original.AttachmentData {
		c.attachments = append(c.attachments, attachmentFile{
			Name: att.Filename,
			Path: "",
			Data: att.Data,
		})
	}

	return c
}

func (c *ComposeModel) moveBodyCursorToStart() {
	c.bodyInput.Focus()
	c.bodyInput, _ = c.bodyInput.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	c.bodyInput.Blur()
}

// focusBodyAtStart moves focus into the message body with the caret at the top
// (above any quoted text) and, in vim mode, Insert mode — so the user sees a
// cursor and can type immediately. Used for replies, where To/Subject are
// already filled in.
func (c *ComposeModel) focusBodyAtStart() {
	c.toInput.Blur()
	c.ccInput.Blur()
	c.bccInput.Blur()
	c.subjectInput.Blur()
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()
	c.bodyInput, _ = c.bodyInput.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	c.bodyInput.EnterInsert()
}

// quoteReply formats a quoted reply block from the original message.
func quoteReply(body, from string) string {
	var buf strings.Builder
	buf.WriteString("\n\n")
	buf.WriteString("On " + from + " wrote:\n")
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		buf.WriteString("> ")
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	return buf.String()
}

// splitReplyAndQuote separates the reply the user wrote from the quoted or
// forwarded original below it, so grammar check only touches the user's text.
// quote keeps its leading blank-line separator and attribution line, so
// reply+quote reconstructs the original body exactly. quote is "" when there is
// no quoted block.
func splitReplyAndQuote(body string) (reply, quote string) {
	lines := strings.Split(body, "\n")
	boundary := -1
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if (strings.HasPrefix(t, "On ") && strings.HasSuffix(t, "wrote:")) ||
			strings.Contains(t, "---------- Forwarded message") ||
			strings.HasPrefix(ln, "> ") {
			boundary = i
			break
		}
	}
	if boundary < 0 {
		return body, ""
	}
	idx := 0
	for i := 0; i < boundary; i++ {
		idx += len(lines[i]) + 1 // +1 for the '\n' that Split removed
	}
	// Pull the blank-line separator into the quote so reply+quote round-trips.
	for idx > 0 && body[idx-1] == '\n' {
		idx--
	}
	return body[:idx], body[idx:]
}

// quoteForward formats a forwarded message block with headers.
func quoteForward(body string, original db.Message) string {
	var buf strings.Builder
	buf.WriteString("\n\n")
	buf.WriteString("---------- Forwarded message ----------\n")
	buf.WriteString("From: " + original.From + "\n")
	if original.Date != (time.Time{}) {
		buf.WriteString("Date: " + original.Date.Format(time.RFC1123Z) + "\n")
	}
	buf.WriteString("Subject: " + original.Subject + "\n")
	if original.To != "" {
		buf.WriteString("To: " + original.To + "\n")
	}
	if original.CC != "" {
		buf.WriteString("CC: " + original.CC + "\n")
	}
	buf.WriteString("\n")
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		buf.WriteString("> ")
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	return buf.String()
}

func newComposeInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 512
	// Drop the default "> " prompt: the soft rows carry their own label column,
	// and renderTextInput (used by the soft-panel View) doesn't clear it.
	ti.Prompt = ""
	return ti
}

// softTitle returns the lowercase mode word embedded in the compose overlay's
// rounded border (mirrors accountManager.softTitle): "reply" for a reply,
// "forward" for a forward, else "compose".
func (c ComposeModel) softTitle() string {
	switch {
	case c.inReplyTo != "":
		return "reply"
	case c.isForward:
		return "forward"
	default:
		return "compose"
	}
}

// listDirEntries reads dir into fileEntry values, dirs-first then alphabetical,
// prefixed with a ".." parent entry unless at the filesystem root. Dotfiles are
// included only when showHidden is true.
func listDirEntries(dir string, showHidden bool) ([]fileEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var fe []fileEntry
	// Add parent-dir entry unless we're at filesystem root
	if dir != "/" {
		fe = append(fe, fileEntry{name: "..", isDir: true})
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		// Skip hidden files unless the user has toggled them on
		if !showHidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		fe = append(fe, fileEntry{
			name:  e.Name(),
			isDir: e.IsDir(),
			size:  info.Size(),
		})
	}

	// Sort: dirs first, then files; alphabetically within each group
	sort.Slice(fe, func(i, j int) bool {
		if fe[i].isDir != fe[j].isDir {
			return fe[i].isDir
		}
		return strings.ToLower(fe[i].name) < strings.ToLower(fe[j].name)
	})

	return fe, nil
}

// openPicker reads the given directory and populates the file picker.
func (c *ComposeModel) openPicker(dir string) {
	fe, err := listDirEntries(dir, c.picker.showHidden)
	if err != nil {
		c.statusMsg = fmt.Sprintf("pick: %v", err)
		c.isErr = true
		c.picker.active = false
		return
	}

	c.picker.currentDir = dir
	c.picker.entries = fe
	c.picker.cursor = 0
	c.picker.active = true
}

// pickerSelected attaches the currently selected file.
func (c *ComposeModel) pickerAttachSelected() {
	if c.picker.cursor < 0 || c.picker.cursor >= len(c.picker.entries) {
		return
	}
	entry := c.picker.entries[c.picker.cursor]
	if entry.isDir {
		return
	}
	path := filepath.Join(c.picker.currentDir, entry.name)
	data, err := os.ReadFile(path)
	if err != nil {
		c.statusMsg = fmt.Sprintf("read: %v", err)
		c.isErr = true
		return
	}
	c.attachments = append(c.attachments, attachmentFile{
		Name: entry.name,
		Path: path,
		Data: data,
	})
	c.statusMsg = fmt.Sprintf("attached: %s (%s)", entry.name, humanSize(len(data)))
	c.isErr = false
	c.picker.active = false
}

func humanSize(b int) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	}
}

// pickerUp moves cursor up.
func (c *ComposeModel) pickerUp() {
	if c.picker.cursor > 0 {
		c.picker.cursor--
	}
}

// pickerDown moves cursor down.
func (c *ComposeModel) pickerDown() {
	if c.picker.cursor < len(c.picker.entries)-1 {
		c.picker.cursor++
	}
}

// pickerEnter enters a directory or selects a file.
func (c *ComposeModel) pickerEnter() {
	entry := c.picker.entries[c.picker.cursor]
	if entry.isDir && entry.name == ".." {
		parent := filepath.Dir(c.picker.currentDir)
		c.openPicker(parent)
		return
	}
	if entry.isDir {
		sub := filepath.Join(c.picker.currentDir, entry.name)
		c.openPicker(sub)
		return
	}
	c.pickerAttachSelected()
}

// pickerUpDir goes to the parent directory.
func (c *ComposeModel) pickerUpDir() {
	parent := filepath.Dir(c.picker.currentDir)
	if parent == c.picker.currentDir {
		// Can't go higher (filesystem root) — close picker
		c.picker.active = false
		return
	}
	c.openPicker(parent)
}

func (c ComposeModel) Update(msg tea.Msg, keys KeyMap) (ComposeModel, tea.Cmd, bool) {
	if c.busy {
		return c, nil, false
	}
	// Host intents emitted by the vim body editor: :w/:wq/:x send; :q or a second
	// Esc in Normal mode cancels (routed through the normal exit/draft-confirm).
	switch msg.(type) {
	case ripple.SubmitMsg:
		return c.send()
	case ripple.CancelMsg:
		return c, nil, true
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		switch c.focusedField {
		case composeFieldTo:
			c.toInput, cmd = c.toInput.Update(msg)
		case composeFieldCC:
			c.ccInput, cmd = c.ccInput.Update(msg)
		case composeFieldBCC:
			c.bccInput, cmd = c.bccInput.Update(msg)
		case composeFieldSubject:
			c.subjectInput, cmd = c.subjectInput.Update(msg)
		case composeFieldBody:
			c.bodyInput, cmd = c.bodyInput.Update(msg)
		}
		return c, cmd, false
	}

	// File picker mode
	if c.picker.active {
		return c.updatePicker(km, keys)
	}
	if c.senderPickerOpen {
		return c.updateSenderPicker(km, keys)
	}

	// Normal compose mode
	switch {
	case keyMatches(km, keys.Cancel):
		// With the autocomplete dropdown open, Esc dismisses it (until the
		// field value changes again) rather than closing compose.
		if c.suggestionsVisible() {
			c.suggestDismissed = true
			return c, nil, false
		}
		// In vim mode the body editor owns Esc (Insert→Normal; a second Esc in
		// Normal or :q emits ripple.CancelMsg, handled above). Route it to the
		// editor instead of closing compose outright.
		if c.focusedField == composeFieldBody && c.bodyInput.vimMode() {
			var cmd tea.Cmd
			c.bodyInput, cmd = c.bodyInput.Update(msg)
			return c, cmd, false
		}
		return c, nil, true

	case km.String() == "ctrl+s" || km.String() == "ctrl+d":
		return c.send()

	case keyMatches(km, keys.Tab):
		// With the dropdown open, Tab accepts the selected suggestion, then
		// advances to the next field either way.
		if c.suggestionsVisible() {
			c.acceptSuggestion()
		}
		c.advanceField(1)
		return c, nil, false

	case km.String() == "shift+tab":
		c.advanceField(-1)
		return c, nil, false

	case c.focusedField == composeFieldFrom && (keyMatches(km, keys.Confirm) || keyMatches(km, keys.Space)):
		c.openSenderPicker()
		return c, nil, false

	// Dropdown navigation. Only the raw arrow/emacs keys — keys.Up/Down also
	// bind j/k, which must keep inserting into the address field.
	case c.suggestionsVisible() && (km.String() == "down" || km.String() == "ctrl+n"):
		c.suggestCursor = (c.suggestCursor + 1) % len(c.suggestions)
		return c, nil, false

	case c.suggestionsVisible() && (km.String() == "up" || km.String() == "ctrl+p"):
		c.suggestCursor = (c.suggestCursor - 1 + len(c.suggestions)) % len(c.suggestions)
		return c, nil, false

	case keyMatches(km, keys.AttachFile):
		// Start picker in user's home directory
		home, err := os.UserHomeDir()
		startDir := home
		if err != nil {
			startDir = "/"
		}
		c.openPicker(startDir)
		return c, nil, false

	case keyMatches(km, keys.RemoveAttach):
		if len(c.attachments) > 0 {
			removed := c.attachments[len(c.attachments)-1].Name
			c.attachments = c.attachments[:len(c.attachments)-1]
			c.statusMsg = fmt.Sprintf("removed: %s", removed)
			c.isErr = false
		}
		return c, nil, false

	case keyMatches(km, keys.CycleSender):
		if len(c.accounts) > 1 {
			c.accountIndex = (c.accountIndex + 1) % len(c.accounts)
			c.statusMsg = fmt.Sprintf("sender: %s", c.selectedAccountLabel())
			c.isErr = false
		}
		return c, nil, false

	default:
		var cmd tea.Cmd
		switch c.focusedField {
		case composeFieldTo:
			if keyMatches(km, keys.Confirm) {
				// Enter picks the highlighted suggestion and stays in the
				// field so another recipient can follow after a comma.
				if c.suggestionsVisible() {
					c.acceptSuggestion()
					return c, nil, false
				}
				c.advanceField(1)
				return c, nil, false
			}
			before := c.toInput.Value()
			c.toInput, cmd = c.toInput.Update(msg)
			c.refreshSuggestions(c.toInput.Value(), before)
		case composeFieldCC:
			if keyMatches(km, keys.Confirm) {
				if c.suggestionsVisible() {
					c.acceptSuggestion()
					return c, nil, false
				}
				c.advanceField(1)
				return c, nil, false
			}
			before := c.ccInput.Value()
			c.ccInput, cmd = c.ccInput.Update(msg)
			c.refreshSuggestions(c.ccInput.Value(), before)
		case composeFieldBCC:
			if keyMatches(km, keys.Confirm) {
				if c.suggestionsVisible() {
					c.acceptSuggestion()
					return c, nil, false
				}
				c.advanceField(1)
				return c, nil, false
			}
			before := c.bccInput.Value()
			c.bccInput, cmd = c.bccInput.Update(msg)
			c.refreshSuggestions(c.bccInput.Value(), before)
		case composeFieldSubject:
			if keyMatches(km, keys.Confirm) {
				c.advanceField(1)
				return c, nil, false
			}
			c.subjectInput, cmd = c.subjectInput.Update(msg)
		case composeFieldBody:
			// Copy/cut/paste are handled inside the editor, which emits the
			// clipboard command through Update.
			c.bodyInput, cmd = c.bodyInput.Update(msg)
		}
		return c, cmd, false
	}
}

// maxRecipientSuggestions caps the autocomplete dropdown height.
const maxRecipientSuggestions = 5

// suggestionsVisible reports whether the autocomplete dropdown should render
// and own its navigation keys.
func (c ComposeModel) suggestionsVisible() bool {
	if c.suggestDismissed || len(c.suggestions) == 0 {
		return false
	}
	switch c.focusedField {
	case composeFieldTo, composeFieldCC, composeFieldBCC:
		return true
	default:
		return false
	}
}

// splitActiveSegment splits a comma-separated recipient list into the part
// already committed (through the last comma and any following spaces) and the
// segment still being typed.
func splitActiveSegment(value string) (prefix, seg string) {
	i := strings.LastIndex(value, ",") + 1
	prefix, seg = value[:i], value[i:]
	trimmed := strings.TrimLeft(seg, " ")
	prefix += seg[:len(seg)-len(trimmed)]
	return prefix, trimmed
}

// matchAddresses returns address-book entries whose "Name <addr>" form
// contains seg (case-insensitive), skipping an already-complete match.
func matchAddresses(book []string, seg string) []string {
	if seg == "" {
		return nil
	}
	needle := strings.ToLower(seg)
	var out []string
	for _, cand := range book {
		if strings.EqualFold(strings.TrimSpace(cand), seg) {
			continue
		}
		if strings.Contains(strings.ToLower(cand), needle) {
			out = append(out, cand)
			if len(out) == maxRecipientSuggestions {
				break
			}
		}
	}
	return out
}

// refreshSuggestions recomputes the dropdown after a keystroke moved the
// focused recipient field from oldValue to newValue. Unchanged values (cursor
// movement) leave the dropdown — and an Esc dismissal — as they were.
func (c *ComposeModel) refreshSuggestions(newValue, oldValue string) {
	if newValue == oldValue {
		return
	}
	c.suggestDismissed = false
	c.suggestCursor = 0
	_, seg := splitActiveSegment(newValue)
	c.suggestions = matchAddresses(c.addressBook, seg)
}

// acceptSuggestion replaces the segment being typed with the highlighted
// candidate, leaving earlier comma-separated recipients intact.
func (c *ComposeModel) acceptSuggestion() {
	input := c.focusedRecipientInput()
	if input == nil || c.suggestCursor >= len(c.suggestions) {
		return
	}
	prefix, _ := splitActiveSegment(input.Value())
	input.SetValue(prefix + c.suggestions[c.suggestCursor])
	input.CursorEnd()
	c.suggestions = nil
	c.suggestCursor = 0
	c.dirty = true
}

func (c *ComposeModel) focusedRecipientInput() *textinput.Model {
	switch c.focusedField {
	case composeFieldTo:
		return &c.toInput
	case composeFieldCC:
		return &c.ccInput
	case composeFieldBCC:
		return &c.bccInput
	default:
		return nil
	}
}

// updatePicker handles key events in the file picker overlay.
func (c ComposeModel) updatePicker(km tea.KeyMsg, keys KeyMap) (ComposeModel, tea.Cmd, bool) {
	switch {
	case keyMatches(km, keys.Cancel):
		c.picker.active = false
		return c, nil, false

	case keyMatches(km, keys.Up):
		c.pickerUp()
		return c, nil, false

	case keyMatches(km, keys.Down):
		c.pickerDown()
		return c, nil, false

	case keyMatches(km, keys.Confirm):
		c.pickerEnter()
		return c, nil, false

	case keyMatches(km, keys.Left), keyMatches(km, keys.Back):
		c.pickerUpDir()
		return c, nil, false

	case km.String() == ".":
		c.picker.showHidden = !c.picker.showHidden
		c.openPicker(c.picker.currentDir)
		return c, nil, false

	default:
		// Single-key quick-jump: press a letter to jump to first entry starting with it
		if len(km.String()) == 1 && km.String() >= "a" && km.String() <= "z" || km.String() >= "A" && km.String() <= "Z" {
			lower := strings.ToLower(km.String())
			for i, e := range c.picker.entries {
				if strings.HasPrefix(strings.ToLower(e.name), lower) {
					c.picker.cursor = i
					return c, nil, false
				}
			}
		}
		return c, nil, false
	}
}

func (c *ComposeModel) advanceField(delta int) {
	c.senderPickerOpen = false
	c.suggestions = nil
	c.suggestCursor = 0
	c.suggestDismissed = false
	next := int(c.focusedField)
	for {
		next = (next + delta + int(composeFieldCount)) % int(composeFieldCount)
		if composeField(next) != composeFieldFrom || len(c.accounts) > 1 {
			break
		}
	}
	c.focusedField = composeField(next)
	c.toInput.Blur()
	c.ccInput.Blur()
	c.bccInput.Blur()
	c.subjectInput.Blur()
	c.bodyInput.Blur()
	switch c.focusedField {
	case composeFieldTo:
		c.toInput.Focus()
	case composeFieldCC:
		c.ccInput.Focus()
	case composeFieldBCC:
		c.bccInput.Focus()
	case composeFieldSubject:
		c.subjectInput.Focus()
	case composeFieldBody:
		c.bodyInput.Focus()
		// In vim mode, land in Insert so the user types right away; Esc then
		// goes Insert→Normal, and a second Esc closes (the double-Esc design).
		// Without this the body sits in Normal and the first Esc closes outright.
		c.bodyInput.EnterInsert()
	}
}

const maxSenderOptions = 5

func (c *ComposeModel) openSenderPicker() {
	if len(c.accounts) < 2 {
		return
	}
	c.senderCursor = clamp(c.accountIndex, 0, len(c.accounts)-1)
	c.senderPickerOpen = true
}

func (c ComposeModel) updateSenderPicker(km tea.KeyMsg, keys KeyMap) (ComposeModel, tea.Cmd, bool) {
	switch {
	case keyMatches(km, keys.Cancel):
		c.senderPickerOpen = false
	case keyMatches(km, keys.Up):
		c.senderCursor = (c.senderCursor - 1 + len(c.accounts)) % len(c.accounts)
	case keyMatches(km, keys.Down):
		c.senderCursor = (c.senderCursor + 1) % len(c.accounts)
	case keyMatches(km, keys.Confirm):
		c.accountIndex = clamp(c.senderCursor, 0, len(c.accounts)-1)
		c.senderPickerOpen = false
		c.statusMsg = fmt.Sprintf("sender: %s", c.selectedAccountLabel())
		c.isErr = false
	}
	return c, nil, false
}

func (c ComposeModel) visibleSenderRange() (start, end int) {
	count := min(maxSenderOptions, len(c.accounts))
	if count == 0 {
		return 0, 0
	}
	start = clamp(c.senderCursor-count/2, 0, len(c.accounts)-count)
	return start, start + count
}

func (c ComposeModel) selectedAccount() config.AccountConfig {
	if c.accountIndex >= 0 && c.accountIndex < len(c.accounts) {
		return c.accounts[c.accountIndex]
	}
	return c.accountCfg
}

func (c ComposeModel) hasContent() bool {
	return strings.TrimSpace(c.toInput.Value()) != "" ||
		strings.TrimSpace(c.ccInput.Value()) != "" ||
		strings.TrimSpace(c.subjectInput.Value()) != "" ||
		strings.TrimSpace(c.bodyInput.Value()) != "" ||
		len(c.attachments) > 0
}

func (c ComposeModel) toDraftRecord() db.Draft {
	account := c.selectedAccount()
	draft := db.Draft{
		ID:           c.draftID,
		AccountName:  account.Name,
		AccountUser:  account.User,
		AccountIndex: c.accountIndex,
		To:           strings.TrimSpace(c.toInput.Value()),
		CC:           strings.TrimSpace(c.ccInput.Value()),
		BCC:          strings.TrimSpace(c.bccInput.Value()),
		Subject:      c.subjectInput.Value(),
		BodyText:     c.bodyInput.Value(),
		InReplyTo:    c.inReplyTo,
		References:   c.references,
		Dirty:        true,
	}
	for i, att := range c.attachments {
		draft.Attachments = append(draft.Attachments, db.DraftAttachment{
			Filename: att.Name,
			Path:     att.Path,
			Data:     att.Data,
			Size:     int64(len(att.Data)),
			Position: i,
		})
	}
	return draft
}

func (c ComposeModel) send() (ComposeModel, tea.Cmd, bool) {
	to := strings.TrimSpace(c.toInput.Value())
	if to == "" {
		c.statusMsg = "TO is required"
		c.isErr = true
		return c, nil, false
	}
	// Validate email addresses
	if err := validateAddressList(to); err != "" {
		c.statusMsg = err
		c.isErr = true
		return c, nil, false
	}
	cc := strings.TrimSpace(c.ccInput.Value())
	if cc != "" {
		if err := validateAddressList(cc); err != "" {
			c.statusMsg = err
			c.isErr = true
			return c, nil, false
		}
	}
	bcc := strings.TrimSpace(c.bccInput.Value())
	if bcc != "" {
		if err := validateAddressList(bcc); err != "" {
			c.statusMsg = err
			c.isErr = true
			return c, nil, false
		}
	}
	acfg := c.selectedAccount()

	// Build attachment list from stored file data
	var atts []smtp.Attachment
	for _, af := range c.attachments {
		atts = append(atts, smtp.Attachment{
			Name: af.Name,
			Data: af.Data,
		})
	}

	body := c.bodyInput.Value()
	if sig := strings.TrimSpace(acfg.Signature); sig != "" {
		body = strings.TrimRight(body, "\n") + "\n\n-- \n" + sig + "\n"
	}
	msg := smtp.OutgoingMessage{
		To:          parseAddressList(to),
		CC:          parseAddressList(c.ccInput.Value()),
		BCC:         parseAddressList(bcc),
		Subject:     c.subjectInput.Value(),
		Body:        body,
		HTMLBody:    smtp.MarkdownToHTML(body),
		InReplyTo:   c.inReplyTo,
		References:  c.references,
		Attachments: atts,
	}
	c.busy = true
	c.statusMsg = "Sending..."
	c.isErr = false
	// The model owns dispatch: it either sends immediately or holds the
	// message for the send-delay undo window (see handleSendQueued).
	return c, func() tea.Msg { return SendQueuedMsg{Account: acfg, Msg: msg} }, false
}

func sendMessageCmd(acfg config.AccountConfig, msg smtp.OutgoingMessage, draftID int64, pendingID uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := smtp.Send(ctx, acfg, msg)
		return MessageSentMsg{Err: err, DraftID: draftID, PendingID: pendingID}
	}
}

func parseAddressList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Extract just the email address from formats like "Name <email>" or "email"
		if addr, err := mail.ParseAddress(p); err == nil && addr.Address != "" {
			out = append(out, addr.Address)
		} else {
			out = append(out, p)
		}
	}
	return out
}

func validateAddressList(s string) string {
	parts := parseAddressList(s)
	for _, p := range parts {
		addr, err := mail.ParseAddress(p)
		if err != nil || addr.Address == "" {
			return fmt.Sprintf("invalid email: %s", p)
		}
	}
	return ""
}

func fileIcon(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".pdf":
		return "📄 "
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg":
		return "🖼 "
	case ".zip", ".tar", ".gz", ".7z", ".rar", ".bz2", ".xz":
		return "📦 "
	case ".doc", ".docx":
		return "📝 "
	case ".xls", ".xlsx", ".csv":
		return "📊 "
	case ".mp3", ".wav", ".flac", ".ogg", ".m4a":
		return "🎵 "
	case ".mp4", ".mov", ".avi", ".mkv", ".webm":
		return "🎬 "
	case ".go", ".py", ".js", ".ts", ".rs", ".java", ".c", ".cpp", ".h", ".sh":
		return "💻 "
	default:
		return "📎 "
	}
}

// renderSuggestionRows renders the autocomplete dropdown rows shown under the
// focused recipient field: a rail marks the highlighted candidate, indented to
// line up with the field's control column.
func (c ComposeModel) renderSuggestionRows(width, labelW int, chrome managerChrome) []string {
	rows := make([]string, 0, len(c.suggestions))
	rowW := max(1, width-2) // minus the 2-cell rail
	indent := strings.Repeat(" ", max(0, labelW))
	for i, s := range c.suggestions {
		selected := i == c.suggestCursor
		bg := chrome.baseBg
		fg := chrome.muted
		if selected {
			bg = chrome.fieldBg
			fg = chrome.text
		}
		label := lipgloss.NewStyle().Background(bg).Foreground(fg).
			Render(indent + " " + truncate(s, max(1, rowW-labelW-2)))
		rows = append(rows, softRail(chrome, selected, bg)+padStyled(label, rowW, bg))
	}
	return rows
}

func (c ComposeModel) selectedAccountLabel() string {
	acfg := c.selectedAccount()
	s := acfg.From
	if s == "" {
		s = acfg.User
	}
	if s == "" {
		s = acfg.Name
	}
	return s
}

func senderOptionLabel(acfg config.AccountConfig) string {
	identity := acfg.From
	if identity == "" {
		identity = acfg.User
	}
	if acfg.Name == "" || acfg.Name == identity {
		return identity
	}
	if identity == "" {
		return acfg.Name
	}
	return acfg.Name + " — " + identity
}

func (c ComposeModel) renderSenderRows(width, labelW int, chrome managerChrome) []string {
	start, end := c.visibleSenderRange()
	rows := make([]string, 0, end-start)
	rowW := max(1, width-2)
	indent := strings.Repeat(" ", max(0, labelW))
	for i := start; i < end; i++ {
		selected := i == c.senderCursor
		bg := chrome.baseBg
		fg := chrome.muted
		if selected {
			bg = chrome.fieldBg
			fg = chrome.text
		}
		label := lipgloss.NewStyle().Background(bg).Foreground(fg).
			Render(indent + " " + truncate(senderOptionLabel(c.accounts[i]), max(1, rowW-labelW-2)))
		rows = append(rows, softRail(chrome, selected, bg)+padStyled(label, rowW, bg))
	}
	return rows
}

// composeOverlayWidth is the width of the compose overlay box for a given
// terminal width. renderOverlay (overlays.go) and the body-editor sizing must
// agree on this value.
func composeOverlayWidth(termWidth int) int {
	return min(termWidth-4, 74)
}

// composeBodyWidth is the content width of the body editor inside the compose
// overlay. handleCompose uses it to keep the stored editor wrapping identically
// to the rendered copy, so vertical navigation matches what the user sees.
func composeBodyWidth(termWidth int) int {
	return max(1, composeOverlayWidth(termWidth)-4)
}

// composeLayout builds the soft-panel hint footer and computes the body editor's
// content width and visible height for the given overlay content size. It is the
// single source of the height budget: View renders with these values, and
// handleCompose sizes the *stored* editor to the same bodyW/bodyH so vertical
// navigation and the rendered viewport stay in lockstep. (View sizes only a
// copy — it is a value receiver — so the stored editor must be synced separately,
// and if it isn't, it keeps height 1 and the caret sticks to the top of the body.)
//
// The soft look has no header bar: the mode/title lives in the overlay border and
// the sender lives in the From row. fixedLines below must match, one-for-one, the
// non-body lines View appends — if they drift, clampView clips the footer or the
// caret desyncs from the viewport.
func (c ComposeModel) composeLayout(width, height int, styles Styles) (hints string, bodyW, bodyH int) {
	chrome := newManagerChrome(width, styles.Theme, styles.PlainUI)
	hints = c.composeHints(width, chrome)

	fixedLines := 0
	fixedLines += 2 // recipients group title + blank
	fixedLines += 4 // From / To / CC / BCC
	fixedLines++    // gap before message group
	fixedLines += 2 // message group title + blank
	fixedLines++    // Subject
	fixedLines++    // spacer before body
	if c.suggestionsVisible() {
		fixedLines += len(c.suggestions) // autocomplete dropdown rows
	}
	if c.senderPickerOpen {
		start, end := c.visibleSenderRange()
		fixedLines += end - start
	}
	if c.inReplyTo != "" {
		fixedLines++ // quote toggle line
	}
	if len(c.attachments) > 0 {
		fixedLines += 3 + len(c.attachments) // gap + group title + blank + one row per file
	}
	if c.statusMsg != "" {
		fixedLines++
	}
	fixedLines += lipgloss.Height(hints)

	bodyH = max(1, height-fixedLines)
	// Body editor content width = overlay width minus internal padding.
	bodyW = max(1, width-4)
	return hints, bodyW, bodyH
}

// composeHints renders the quiet lowercase soft-panel footer. When the body is
// focused in vim mode it leads with the mode / ":" command-line indicator (kept
// verbatim, not lowercased, so typed commands read correctly). Parts are greedily
// packed into as many lines as needed for the width; composeLayout accounts for
// the resulting height via lipgloss.Height.
func (c ComposeModel) composeHints(width int, chrome managerChrome) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	keyStyle := base.Foreground(chrome.text)
	lblStyle := base.Foreground(chrome.muted)

	if c.busy {
		// While sending, the status line carries the message; keep a blank footer.
		return padStyled(base.Render("  "), width, chrome.baseBg)
	}

	var parts []string
	if c.focusedField == composeFieldBody && c.bodyInput.vimMode() {
		if cl := c.bodyInput.CommandLine(); cl != "" {
			parts = append(parts, base.Foreground(chrome.accent).Render(cl))
		} else if mode := c.bodyInput.Mode(); mode != "" {
			parts = append(parts, base.Foreground(chrome.accent).Render("-- "+mode+" --"))
		}
	}

	pairs := []string{"^s", "send", "^g", "grammar", "^u", "sender", "alt+f", "attach", "tab", "next"}
	if c.senderPickerOpen {
		pairs = []string{"↑↓", "choose sender", "enter", "select", "esc", "cancel"}
	} else {
		if c.focusedField == composeFieldFrom {
			pairs = append(pairs, "enter", "pick sender")
		}
		if len(c.attachments) > 0 {
			pairs = append(pairs, "^r", "remove")
		}
		if c.focusedField == composeFieldBody {
			// Copy/cut act on the editor selection, so only show them in the body.
			pairs = append(pairs, "^c", "copy", "^x", "cut")
		}
		pairs = append(pairs, "esc", "cancel")
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+lblStyle.Render(" "+pairs[i+1]))
	}

	// Greedy-pack into lines no wider than width, indented 2, separated by 3 spaces.
	indent := base.Render("  ")
	indentW := 2
	sep := base.Render("   ")
	var lines []string
	cur := indent
	curW := indentW
	for _, p := range parts {
		pw := lipgloss.Width(p)
		if curW > indentW && curW+3+pw > width {
			lines = append(lines, padStyled(cur, width, chrome.baseBg))
			cur = indent
			curW = indentW
		}
		if curW > indentW {
			cur += sep
			curW += 3
		}
		cur += p
		curW += pw
	}
	lines = append(lines, padStyled(cur, width, chrome.baseBg))
	return strings.Join(lines, "\n")
}

func (c ComposeModel) View(width, height int, styles Styles) string {
	chrome := newManagerChrome(width, styles.Theme, styles.PlainUI)

	// File picker view
	if c.picker.active {
		return c.pickerView(width, height, chrome)
	}

	hints, bodyInputW, bodyH := c.composeLayout(width, height, styles)

	// Soft-panel rows share a short label column; the input text is inset 2 cells
	// to match the settings / account-manager forms.
	labelW := min(10, formLabelWidth(width))

	blankRow := func(bg lipgloss.Color) string {
		return lipgloss.NewStyle().Background(bg).Width(width).Render("")
	}

	// softInput renders one To/CC/BCC/Subject field as a soft row: an accent rail
	// marks focus and the bubbles textinput sits inset in the control column.
	softInput := func(label string, ti textinput.Model, field composeField) string {
		focused := c.focusedField == field
		rowFieldW := max(1, width-2-labelW)
		control := renderInsetControl(renderTextInput(ti, max(1, rowFieldW-4), focused, false, chrome), rowFieldW, 2, chrome)
		return renderSoftRow(label, focused, control, width, labelW, chrome)
	}

	c.bodyInput.SetSize(bodyInputW, bodyH)

	bodyBg := chrome.baseBg
	if c.focusedField == composeFieldBody {
		bodyBg = chrome.fieldBg
	}

	// Style the owned editor for this render: accent block cursor, a muted
	// selection highlight, and a dim placeholder. The per-line background
	// wrapping below covers the remainder of each line.
	c.bodyInput.CursorStyle = lipgloss.NewStyle().Background(chrome.accent).Foreground(bodyBg)
	c.bodyInput.SelectedStyle = lipgloss.NewStyle().Background(chrome.muted).Foreground(chrome.text)
	c.bodyInput.PlaceholderStyle = lipgloss.NewStyle().Background(bodyBg).Foreground(chrome.muted)

	// Gate the cursor to the focused field — a blurred body renders no cursor,
	// so inactive compose fields leave no cursor artifact. This is the render
	// copy; advanceField keeps the model's focus state in sync.
	if c.focusedField == composeFieldBody {
		c.bodyInput.Focus()
	} else {
		c.bodyInput.Blur()
	}

	raw := c.bodyInput.View()
	// The editor is authoritative on wrapping — it has already fit each line to
	// bodyInputW. We must only FRAME that output, never re-wrap it: letting
	// lipgloss.Width() reflow the lines re-measures grapheme widths with a
	// different model (x/ansi vs the editor's go-runewidth), and for emoji/flags
	// the two disagree, so a line the editor considered full overflows and the
	// wrapped remainder escapes to the app's left edge. Instead, clip and pad
	// each line to the content width using the terminal's own width measure, so
	// nothing can overflow, then frame with bodyBg padding.
	bgWrap := lipgloss.NewStyle().Background(bodyBg).Foreground(chrome.text)
	pad := bgWrap.Render("  ")
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = ansi.Truncate(line, bodyInputW, "")
		if gap := bodyInputW - ansi.StringWidth(line); gap > 0 {
			line += strings.Repeat(" ", gap)
		}
		// Re-apply bodyBg after every reset so the cursor/selection styles (whose
		// trailing \033[0m clears the background) don't punch holes in the line.
		if strings.Contains(line, "\033[0m") {
			segs := strings.Split(line, "\033[0m")
			for j, seg := range segs {
				segs[j] = bgWrap.Render(seg)
				if j < len(segs)-1 {
					segs[j] += "\033[0m"
				}
			}
			line = strings.Join(segs, "")
		} else {
			line = bgWrap.Render(line)
		}
		lines = append(lines, pad+line+pad)
	}
	bodyRow := strings.Join(lines, "\n")

	// Status line
	statusLine := ""
	if c.statusMsg != "" {
		fg := chrome.pendingFg
		if c.isErr {
			fg = chrome.errorFg
		}
		statusLine = lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(fg).
			Padding(0, 2).
			Render(c.statusMsg)
		if gap := width - lipgloss.Width(statusLine); gap > 0 {
			statusLine += lipgloss.NewStyle().Background(chrome.baseBg).Render(strings.Repeat(" ", gap))
		}
	}

	// ── Build rows ── (line count must match composeLayout's fixedLines)
	var rows []string

	// recipients
	rows = append(rows, renderSoftGroupTitle("recipients", width, chrome))
	rows = append(rows, blankRow(chrome.baseBg))
	fromFocused := c.focusedField == composeFieldFrom
	fromW := max(1, width-2-labelW)
	fromLabel := senderOptionLabel(c.selectedAccount())
	fromControl := renderSoftPicker(fromW, fromLabel, fromFocused, chrome)
	if len(c.accounts) < 2 {
		fromText := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render(fromLabel)
		fromControl = renderInsetControl(fromText, fromW, 2, chrome)
	}
	rows = append(rows, renderSoftRow("From", fromFocused, fromControl, width, labelW, chrome))
	if c.senderPickerOpen {
		rows = append(rows, c.renderSenderRows(width, labelW, chrome)...)
	}
	// Each recipient row is followed by the autocomplete dropdown when it is
	// the focused field; composeLayout budgets the extra lines.
	suggestionRows := func(field composeField) {
		if c.suggestionsVisible() && c.focusedField == field {
			rows = append(rows, c.renderSuggestionRows(width, labelW, chrome)...)
		}
	}
	rows = append(rows, softInput("To", c.toInput, composeFieldTo))
	suggestionRows(composeFieldTo)
	rows = append(rows, softInput("CC", c.ccInput, composeFieldCC))
	suggestionRows(composeFieldCC)
	rows = append(rows, softInput("BCC", c.bccInput, composeFieldBCC))
	suggestionRows(composeFieldBCC)

	// message
	rows = append(rows, blankRow(chrome.baseBg)) // gap between groups
	rows = append(rows, renderSoftGroupTitle("message", width, chrome))
	rows = append(rows, blankRow(chrome.baseBg))
	rows = append(rows, softInput("Subject", c.subjectInput, composeFieldSubject))
	rows = append(rows, blankRow(chrome.baseBg)) // spacer before body
	rows = append(rows, bodyRow)

	// reply quote toggle
	if c.inReplyTo != "" {
		qIcon := "▸"
		if !c.quoteCollapsed {
			qIcon = "▾"
		}
		if chrome.plainUI {
			qIcon = ">"
			if !c.quoteCollapsed {
				qIcon = "v"
			}
		}
		qText := fmt.Sprintf("  %s  quoted original (enter to toggle)", qIcon)
		qLine := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render(qText)
		rows = append(rows, padStyled(qLine, width, chrome.baseBg))
	}

	// attachments
	if len(c.attachments) > 0 {
		rows = append(rows, blankRow(chrome.baseBg)) // gap before group
		rows = append(rows, renderSoftGroupTitle("attachments", width, chrome))
		rows = append(rows, blankRow(chrome.baseBg))
		for _, af := range c.attachments {
			txt := fmt.Sprintf("  %s%s  %s", fileIcon(af.Name), af.Name, humanSize(len(af.Data)))
			line := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Render(txt)
			rows = append(rows, padStyled(line, width, chrome.baseBg))
		}
	}

	if statusLine != "" {
		rows = append(rows, statusLine)
	}
	rows = append(rows, hints)

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return clampView(content, width, height, chrome.baseBg)
}

// pickerView renders the yazi-style file browser.
func (c ComposeModel) pickerView(width, height int, chrome managerChrome) string {
	// Current directory breadcrumb (the "attach file" title lives in the overlay
	// border via ComposeModel.softTitle when the picker is active).
	pathLine := padStyled(
		lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.accent).Render("  "+c.picker.currentDir),
		width, chrome.baseBg)

	// Quiet soft footer — render first so the file list can reserve its true height.
	hints := renderSoftHints(width, chrome, "↑↓", "navigate", "enter", "open/attach", "esc/h", "up dir", ".", "hidden")

	// File listing — fit within available height (path line + footer).
	availH := height - lipgloss.Height(pathLine) - lipgloss.Height(hints)
	if availH < 1 {
		availH = 1
	}

	// Scroll offset
	start := 0
	if c.picker.cursor >= availH {
		start = c.picker.cursor - availH + 1
	}
	shown := c.picker.entries
	if start > len(shown) {
		start = len(shown) - availH
	}
	if start < 0 {
		start = 0
	}
	end := start + availH
	if end > len(shown) {
		end = len(shown)
	}
	visible := shown[start:end]

	railW := 2
	contentW := max(1, width-railW)
	var lines []string
	for i, entry := range visible {
		idx := start + i
		focused := idx == c.picker.cursor

		// Selection is a left accent rail (soft-panel convention) — no full-width
		// accent fill. The label brightens to text when focused.
		fg := chrome.text
		var label, size string
		switch {
		case entry.isDir && entry.name == "..":
			label = "../"
			if !focused {
				fg = chrome.muted
			}
		case entry.isDir:
			label = entry.name + "/"
			if !focused {
				fg = chrome.accent
			}
		default:
			label = entry.name
			size = humanSize(int(entry.size))
			if !focused {
				fg = chrome.text
			}
		}

		labelStyle := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg)
		var content string
		if size != "" {
			sizeCell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render(size + " ")
			left := labelStyle.Render(" " + truncate(label, max(1, contentW-lipgloss.Width(sizeCell)-2)))
			gap := max(1, contentW-lipgloss.Width(left)-lipgloss.Width(sizeCell))
			content = left + lipgloss.NewStyle().Background(chrome.baseBg).Render(strings.Repeat(" ", gap)) + sizeCell
		} else {
			content = labelStyle.Render(" " + truncate(label, max(1, contentW-1)))
		}
		lines = append(lines, softRail(chrome, focused, chrome.baseBg)+padStyled(content, contentW, chrome.baseBg))
	}

	// Fill remaining space
	for len(lines) < availH {
		lines = append(lines, lipgloss.NewStyle().
			Background(chrome.baseBg).
			Width(width).
			Render(strings.Repeat(" ", width)))
	}

	fileList := lipgloss.JoinVertical(lipgloss.Left, lines...)

	content := lipgloss.JoinVertical(lipgloss.Left,
		pathLine,
		fileList,
		hints,
	)
	return clampView(content, width, height, chrome.baseBg)
}

// statusMsg helpers used by model.go
func (c ComposeModel) StatusMsg() string { return c.statusMsg }
func (c ComposeModel) IsErr() bool       { return c.isErr }
