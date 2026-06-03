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

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/smtp"
)

type composeField int

const (
	composeFieldTo composeField = iota
	composeFieldCC
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
}

type fileEntry struct {
	name  string
	isDir bool
	size  int64
}

type ComposeModel struct {
	toInput      textinput.Model
	ccInput      textinput.Model
	subjectInput textinput.Model
	bodyInput    textarea.Model

	focusedField composeField
	inReplyTo    string
	references   string
	accountCfg   config.AccountConfig

	accounts     []config.AccountConfig
	accountIndex int

	attachments    []attachmentFile
	picker         filePicker
	quoteCollapsed bool

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
	c.subjectInput = newComposeInput("Subject")
	c.bodyInput = textarea.New()
	c.bodyInput.Placeholder = "Write your message here..."
	c.bodyInput.ShowLineNumbers = false
	c.bodyInput.Prompt = ""
	// Remove the default black CursorLine background from the original's
	// FocusedStyle — Focus() stores a pointer to it, and View() copies inherit
	// that pointer. If left as default (black on dark mode), the cursor line
	// renders with black background regardless of per-line wrapping.
	c.bodyInput.FocusedStyle.CursorLine = lipgloss.NewStyle()
	c.focusedField = composeFieldTo
	c.toInput.Focus()
	c.SetAddressBook(addressBook)
	return c
}

// SetAddressBook populates autocomplete suggestions for To and CC fields.
func (c *ComposeModel) SetAddressBook(addrs []string) {
	if len(addrs) == 0 {
		return
	}
	c.toInput.ShowSuggestions = true
	c.toInput.SetSuggestions(addrs)
	c.ccInput.ShowSuggestions = true
	c.ccInput.SetSuggestions(addrs)
}

// NewReply creates a compose model pre-filled for replying to a message.
func NewReply(original db.Message, acfg config.AccountConfig, accounts []config.AccountConfig) ComposeModel {
	c := NewCompose(acfg, accounts, nil)
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
		c.moveBodyCursorToStart()
	}

	return c
}

// NewForward creates a compose model for forwarding a message.
// To field is left empty for the user to fill in. Subject is prefixed with "Fwd:".
// The original body is quoted with a "Forwarded message" header and original attachments are included.
func NewForward(original db.Message, acfg config.AccountConfig, accounts []config.AccountConfig) ComposeModel {
	c := NewCompose(acfg, accounts, nil)
	c.quoteCollapsed = true

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
	return ti
}

// openPicker reads the given directory and populates the file picker.
func (c *ComposeModel) openPicker(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		c.statusMsg = fmt.Sprintf("pick: %v", err)
		c.isErr = true
		c.picker.active = false
		return
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
		// Skip hidden files
		if strings.HasPrefix(e.Name(), ".") {
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
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		switch c.focusedField {
		case composeFieldTo:
			c.toInput, cmd = c.toInput.Update(msg)
		case composeFieldCC:
			c.ccInput, cmd = c.ccInput.Update(msg)
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

	// Normal compose mode
	switch {
	case keyMatches(km, keys.Cancel):
		return c, nil, true

	case km.String() == "ctrl+s" || km.String() == "ctrl+d":
		return c.send()

	case keyMatches(km, keys.Tab):
		// If the focused input has matched suggestions, let it accept one.
		// Otherwise Tab advances to the next field.
		if c.focusedField == composeFieldTo && hasPendingComposeSuggestion(c.toInput) {
			c.toInput, _ = c.toInput.Update(msg)
			c.advanceField(1)
			return c, nil, false
		}
		if c.focusedField == composeFieldCC && hasPendingComposeSuggestion(c.ccInput) {
			c.ccInput, _ = c.ccInput.Update(msg)
			c.advanceField(1)
			return c, nil, false
		}
		c.advanceField(1)
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
				c.advanceField(1)
				return c, nil, false
			}
			c.toInput, cmd = c.toInput.Update(msg)
		case composeFieldCC:
			if keyMatches(km, keys.Confirm) {
				c.advanceField(1)
				return c, nil, false
			}
			c.ccInput, cmd = c.ccInput.Update(msg)
		case composeFieldSubject:
			if keyMatches(km, keys.Confirm) {
				c.advanceField(1)
				return c, nil, false
			}
			c.subjectInput, cmd = c.subjectInput.Update(msg)
		case composeFieldBody:
			c.bodyInput, cmd = c.bodyInput.Update(msg)
		}
		return c, cmd, false
	}
}

func hasPendingComposeSuggestion(input textinput.Model) bool {
	matches := input.MatchedSuggestions()
	if len(matches) == 0 {
		return false
	}
	value := strings.TrimSpace(input.Value())
	for _, match := range matches {
		if strings.EqualFold(value, strings.TrimSpace(match)) {
			return false
		}
	}
	return true
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
	next := (int(c.focusedField) + delta + int(composeFieldCount)) % int(composeFieldCount)
	c.focusedField = composeField(next)
	c.toInput.Blur()
	c.ccInput.Blur()
	c.subjectInput.Blur()
	c.bodyInput.Blur()
	switch c.focusedField {
	case composeFieldTo:
		c.toInput.Focus()
	case composeFieldCC:
		c.ccInput.Focus()
	case composeFieldSubject:
		c.subjectInput.Focus()
	case composeFieldBody:
		c.bodyInput.Focus()
	}
}

func (c ComposeModel) selectedAccount() config.AccountConfig {
	if c.accountIndex >= 0 && c.accountIndex < len(c.accounts) {
		return c.accounts[c.accountIndex]
	}
	return c.accountCfg
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
	acfg := c.selectedAccount()

	// Build attachment list from stored file data
	var atts []smtp.Attachment
	for _, af := range c.attachments {
		atts = append(atts, smtp.Attachment{
			Name: af.Name,
			Data: af.Data,
		})
	}

	msg := smtp.OutgoingMessage{
		To:          parseAddressList(to),
		CC:          parseAddressList(c.ccInput.Value()),
		Subject:     c.subjectInput.Value(),
		Body:        c.bodyInput.Value(),
		InReplyTo:   c.inReplyTo,
		References:  c.references,
		Attachments: atts,
	}
	c.busy = true
	c.statusMsg = "Sending..."
	c.isErr = false
	return c, sendMessageCmd(acfg, msg), false
}

func sendMessageCmd(acfg config.AccountConfig, msg smtp.OutgoingMessage) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := smtp.Send(ctx, acfg, msg)
		if err != nil {
			return MessageSentMsg{Err: err}
		}
		return MessageSentMsg{}
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

// renderComposePanel wraps content rows in a surfaceBg section with an
// accent title bar. No border — the surfaceBg background change provides
// visual separation.
func renderComposePanel(title string, rows []string, width int, chrome managerChrome) string {
	titleBar := lipgloss.NewStyle().
		Background(chrome.surfaceBg).
		Foreground(chrome.accent).
		Bold(true).
		Width(width).
		Padding(0, 2).
		Render(title)
	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	inner := lipgloss.JoinVertical(lipgloss.Left, titleBar, body)
	return lipgloss.NewStyle().
		Background(chrome.surfaceBg).
		Width(width).
		Render(inner)
}

func renderComposePanelRow(ti textinput.Model, label string, focused bool, width, labelW, ctrlW int, chrome managerChrome) string {
	bg := chrome.surfaceBg
	labelFg := chrome.muted
	if focused {
		bg = chrome.fieldBg
		labelFg = chrome.text
	}
	// Marker + label sit on the panel background (surfaceBg) so To/CC/Subject match
	// the RECIPIENTS/MESSAGE panel and the From row. Only the input cell shifts to
	// fieldBg on focus.
	marker := lipgloss.NewStyle().Background(chrome.surfaceBg).Width(2).Render(" ")
	if focused {
		marker = lipgloss.NewStyle().Background(chrome.surfaceBg).Foreground(chrome.accent).Bold(true).Width(2).Render(" >")
	}
	labelCell := lipgloss.NewStyle().Background(chrome.surfaceBg).Foreground(labelFg).Width(labelW).Render(truncate(label, max(1, labelW-1)))
	ti.Width = ctrlW
	ti.Prompt = ""
	ti.PromptStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.accent)
	ti.TextStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.text)
	ti.PlaceholderStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.muted)
	ti.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))
	if focused {
		ti.Focus()
	} else {
		ti.Blur()
	}
	raw := inputViewWithCursor(ti, focused)
	// bubbles pads the placeholder/value out to ti.Width with *unstyled* spaces
	// (textinput.placeholderView), which leak the terminal's default background to
	// the right of the hint. Trim that bare padding — styled cells (the block cursor,
	// styled text) end in a reset SGR and survive TrimRight — then refill via
	// padStyled so every gap cell carries bg.
	raw = strings.TrimRight(raw, " ")
	ctrlCell := lipgloss.NewStyle().Background(bg).Foreground(chrome.text).Render(padStyled(raw, ctrlW, bg))
	return marker + labelCell + ctrlCell
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

// renderComposeFromRow renders a non-focusable "From" row showing the current sender account.
func renderComposeFromRow(label, value string, width, labelW, ctrlW int, chrome managerChrome) string {
	bg := chrome.surfaceBg
	marker := lipgloss.NewStyle().Background(bg).Width(2).Render(" ")
	labelCell := lipgloss.NewStyle().Background(bg).Foreground(chrome.muted).Width(labelW).Render(truncate(label, max(1, labelW-1)))
	valueCell := lipgloss.NewStyle().Background(bg).Foreground(chrome.text).Width(ctrlW).Render(truncate(value, ctrlW))
	return marker + labelCell + valueCell
}

func (c ComposeModel) View(width, height int, styles Styles) string {
	chrome := newManagerChrome(width, styles.Theme, styles.PlainUI)

	// File picker view
	if c.picker.active {
		return c.pickerView(width, height, chrome)
	}

	title := "COMPOSE"
	if c.inReplyTo != "" {
		title = "REPLY"
	}
	// Add sender badge from selected account
	acfg := c.selectedAccount()
	sender := acfg.From
	if sender == "" {
		sender = acfg.User
	}
	if sender != "" {
		title += "  ◉ " + sender
	}
	header := renderManagerHeader(title, width, chrome)

	// Panel content area (full width, no border inset needed)
	panelLabelW := min(10, formLabelWidth(width))
	panelCtrlW := max(1, width-panelLabelW-2)

	blankRow := func(bg lipgloss.Color) string {
		return lipgloss.NewStyle().Background(bg).Width(width).Render("")
	}

	// Action bar
	actionKeys := []string{"ctrl+s", "send", "ctrl+g", "grammar", "ctrl+u", "sender", "ctrl+a", "attach"}
	navKeys := []string{"tab", "next", "esc", "cancel"}
	if len(c.attachments) > 0 {
		navKeys = []string{"tab", "next", "ctrl+r", "remove", "esc", "cancel"}
	}
	actions := renderManagerActionGroups(width, chrome, actionKeys, navKeys)
	if c.busy {
		actions = renderManagerActions(width, chrome)
	}

	// Height budget: panel overhead (title bar) + gaps + reply
	overheadPerPanel := 1 // title bar only (no border)
	panelCount := 2       // recipients + message
	if len(c.attachments) > 0 {
		panelCount++
	}
	panelOH := panelCount * overheadPerPanel
	gapRows := panelCount - 1 // gaps between panels
	if c.inReplyTo != "" {
		gapRows++ // quote toggle line
	}
	attachLines := 0
	if len(c.attachments) > 0 {
		attachLines = len(c.attachments)
	}

	fixedH := lipgloss.Height(header) + panelOH + gapRows + lipgloss.Height(actions)
	if c.statusMsg != "" {
		fixedH++
	}
	bodyH := max(1, height-fixedH-attachLines-3) // -3 for From + subject + spacer inside panels
	if bodyH < 1 {
		bodyH = 1
	}

	// Body textarea inside Message panel — content area minus internal padding
	bodyInputW := max(1, width-4)
	c.bodyInput.SetWidth(bodyInputW)
	c.bodyInput.SetHeight(bodyH)

	// Clear all textarea style backgrounds — Inline(true) in computed styles
	// strips margins/padding but keeps background. We force a uniform bg by
	// setting each style layer and also wrapping every line post-render.
	bodyBg := chrome.baseBg
	if c.focusedField == composeFieldBody {
		bodyBg = chrome.fieldBg
	}

	// Don't set background on textarea style layers — the Focus() method on the
	// original stores a pointer to its own FocusedStyle, and View() copies keep
	// pointing to the original's FocusedStyle (with default black cursor line).
	// Instead, clear everything to a plain style and let the per-line wrapping
	// below apply the uniform background.
	plain := lipgloss.NewStyle()
	c.bodyInput.FocusedStyle.Base = plain
	c.bodyInput.FocusedStyle.Text = plain
	c.bodyInput.FocusedStyle.Prompt = plain
	c.bodyInput.FocusedStyle.Placeholder = plain
	c.bodyInput.FocusedStyle.CursorLine = plain
	c.bodyInput.FocusedStyle.CursorLineNumber = plain
	c.bodyInput.FocusedStyle.LineNumber = plain
	c.bodyInput.FocusedStyle.EndOfBuffer = plain
	c.bodyInput.BlurredStyle.Base = plain
	c.bodyInput.BlurredStyle.Text = plain
	c.bodyInput.BlurredStyle.Prompt = plain
	c.bodyInput.BlurredStyle.Placeholder = plain
	c.bodyInput.BlurredStyle.CursorLine = plain
	c.bodyInput.BlurredStyle.CursorLineNumber = plain
	c.bodyInput.BlurredStyle.LineNumber = plain
	c.bodyInput.BlurredStyle.EndOfBuffer = plain
	// Cursor character: after Reverse(true) gives accent-bg with bodyBg-text.
	c.bodyInput.Cursor.Style = plain.Background(bodyBg).Foreground(chrome.accent)

	// Apply focus only to the render copy. Focus() points the textarea at the copy's
	// FocusedStyle; Blur() prevents inactive compose fields from leaving cursor
	// artifacts when focus moves back to To/CC/Subject.
	if c.focusedField == composeFieldBody {
		c.bodyInput.Focus()
	} else {
		c.bodyInput.Blur()
	}

	raw := c.bodyInput.View()
	// Wrap each line individually with bodyBg so the background covers
	// every line. The cursor character's ANSI reset (\033[0m) clears the
	// background for the rest of the line — split on resets and wrap each
	// segment individually so bodyBg is re-applied after each one.
	bgWrap := lipgloss.NewStyle().Background(bodyBg)
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		if strings.Contains(line, "\033[0m") {
			segs := strings.Split(line, "\033[0m")
			for j, seg := range segs {
				segs[j] = bgWrap.Render(seg)
				if j < len(segs)-1 {
					segs[j] += "\033[0m"
				}
			}
			lines[i] = strings.Join(segs, "")
		} else {
			lines[i] = bgWrap.Render(line)
		}
	}
	bodyRow := lipgloss.NewStyle().Background(bodyBg).Width(width).Padding(0, 2).Render(strings.Join(lines, "\n"))

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

	// ── Build rows ──
	var rows []string
	rows = append(rows, header)

	// ═══════════════════ RECIPIENTS ═══════════════════
	recipRows := []string{
		renderComposeFromRow("From", c.selectedAccountLabel(), width, panelLabelW, panelCtrlW, chrome),
		renderComposePanelRow(c.toInput, "To", c.focusedField == composeFieldTo, width, panelLabelW, panelCtrlW, chrome),
		renderComposePanelRow(c.ccInput, "CC", c.focusedField == composeFieldCC, width, panelLabelW, panelCtrlW, chrome),
	}
	rows = append(rows, renderComposePanel("RECIPIENTS", recipRows, width, chrome))

	// ═══════════════════ MESSAGE ═══════════════════
	msgRows := []string{
		renderComposePanelRow(c.subjectInput, "Subject", c.focusedField == composeFieldSubject, width, panelLabelW, panelCtrlW, chrome),
		lipgloss.NewStyle().Background(chrome.surfaceBg).Width(width).Render(""),
		bodyRow,
	}
	rows = append(rows, blankRow(chrome.baseBg)) // gap between panels
	rows = append(rows, renderComposePanel("MESSAGE", msgRows, width, chrome))

	// ── Reply quote (outside panels) ──
	if c.inReplyTo != "" {
		qIcon := "▸"
		if !c.quoteCollapsed {
			qIcon = "▾"
		}
		qLine := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.muted).
			Padding(0, 2).
			Render(fmt.Sprintf("  %s  Show quoted original (enter to toggle)", qIcon))
		if gap := width - lipgloss.Width(qLine); gap > 0 {
			qLine += lipgloss.NewStyle().Background(chrome.baseBg).Render(strings.Repeat(" ", gap))
		}
		rows = append(rows, qLine)
	}

	// ═══════════════════ ATTACHMENTS ═══════════════════
	if len(c.attachments) > 0 {
		var attachRows []string
		for _, af := range c.attachments {
			icon := fileIcon(af.Name)
			line := fmt.Sprintf("  %s%s  %s    [ctrl+r remove]", icon, af.Name, humanSize(len(af.Data)))
			attachLine := lipgloss.NewStyle().
				Background(chrome.surfaceBg).
				Foreground(chrome.accent).
				Padding(0, 2).
				Render(line)
			if gap := width - lipgloss.Width(attachLine); gap > 0 {
				attachLine += lipgloss.NewStyle().Background(chrome.surfaceBg).Render(strings.Repeat(" ", gap))
			}
			attachRows = append(attachRows, attachLine)
		}
		rows = append(rows, blankRow(chrome.baseBg))
		rows = append(rows, renderComposePanel("ATTACHMENTS", attachRows, width, chrome))
	}

	if statusLine != "" {
		rows = append(rows, statusLine)
	}
	rows = append(rows, actions)

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return clampView(content, width, height, chrome.baseBg)
}

// pickerView renders the yazi-style file browser.
func (c ComposeModel) pickerView(width, height int, chrome managerChrome) string {
	header := renderManagerHeader("ATTACH FILE", width, chrome)

	// Path line
	pathLine := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.accent).
		Width(width).
		Padding(0, 2).
		Render(c.picker.currentDir)

	// File listing — fit within available height
	availH := height - lipgloss.Height(header) - lipgloss.Height(pathLine) - 2 // 2 for action bar
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

	var lines []string
	for i, entry := range visible {
		idx := start + i
		focused := idx == c.picker.cursor
		var line string
		if entry.isDir {
			if entry.name == ".." {
				line = "  ../"
			} else {
				line = fmt.Sprintf("  %s/", entry.name)
			}
		} else {
			sz := humanSize(int(entry.size))
			// Pad name to align sizes to the right
			line = fmt.Sprintf("  %-*s  %s", max(1, width-14), entry.name, sz)
		}
		if len(line) > width {
			line = line[:width]
		}

		if focused {
			highlightStyle := lipgloss.NewStyle().
				Background(chrome.accent).
				Foreground(contrastFg(chrome.accent))
			if idx < width && len(line) < width {
				line = line + strings.Repeat(" ", width-len(line))
			}
			lines = append(lines, highlightStyle.Width(width).Render(line))
		} else {
			bg := chrome.baseBg
			if entry.isDir {
				fg := chrome.accent
				if entry.name == ".." {
					fg = chrome.muted
				}
				lines = append(lines, lipgloss.NewStyle().
					Background(bg).Foreground(fg).
					Width(width).Render(line))
			} else {
				lines = append(lines, lipgloss.NewStyle().
					Background(bg).Foreground(chrome.text).
					Width(width).Render(line))
			}
		}
	}

	// Fill remaining space
	for len(lines) < availH {
		lines = append(lines, lipgloss.NewStyle().
			Background(chrome.baseBg).
			Width(width).
			Render(strings.Repeat(" ", width)))
	}

	fileList := lipgloss.JoinVertical(lipgloss.Left, lines...)

	// Actions
	actions := renderManagerActionGroups(width, chrome,
		[]string{"↑/↓", "navigate", "enter", "open/attach"},
		[]string{"esc/h", "up dir"},
	)

	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		pathLine,
		fileList,
		actions,
	)
	return clampView(content, width, height, chrome.baseBg)
}

// statusMsg helpers used by model.go
func (c ComposeModel) StatusMsg() string { return c.statusMsg }
func (c ComposeModel) IsErr() bool       { return c.isErr }
