package ui

import (
	"context"
	"fmt"
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

	attachments    []attachmentFile
	picker         filePicker
	quoteCollapsed bool

	busy      bool
	statusMsg string
	isErr     bool
}

func NewCompose(acfg config.AccountConfig) ComposeModel {
	c := ComposeModel{accountCfg: acfg}
	c.toInput = newComposeInput("to@example.com")
	c.ccInput = newComposeInput("")
	c.ccInput.Placeholder = "cc (optional)"
	c.subjectInput = newComposeInput("Subject")
	c.bodyInput = textarea.New()
	c.bodyInput.Placeholder = "Write your message here..."
	c.bodyInput.ShowLineNumbers = false
	c.focusedField = composeFieldTo
	c.toInput.Focus()
	return c
}

func NewReply(original db.Message, acfg config.AccountConfig) ComposeModel {
	c := NewCompose(acfg)
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

	return c
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

func (c ComposeModel) send() (ComposeModel, tea.Cmd, bool) {
	to := strings.TrimSpace(c.toInput.Value())
	if to == "" {
		c.statusMsg = "TO is required"
		c.isErr = true
		return c, nil, false
	}
	acfg := c.accountCfg

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
		return MessageSentMsg{Err: err}
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
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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

// renderComposeRow renders a form row with seamless background across
// marker, label, and control cells — all share the same rowBg.
func renderComposeRow(label string, focused bool, control string, width, labelW int, chrome managerChrome) string {
	rowBg := chrome.baseBg
	labelFg := chrome.muted
	if focused {
		rowBg = chrome.fieldBg
		labelFg = chrome.text
	}
	marker := lipgloss.NewStyle().Background(rowBg).Width(2).Render(" ")
	if focused {
		marker = lipgloss.NewStyle().Background(rowBg).Foreground(chrome.accent).Bold(true).Width(2).Render(" >")
	}
	labelCell := lipgloss.NewStyle().Background(rowBg).Foreground(labelFg).Width(labelW).Render(truncate(label, max(1, labelW-1)))
	ctrlW := max(1, width-lipgloss.Width(marker)-labelW)
	control = truncateStyled(control, ctrlW, rowBg)
	ctrlCell := lipgloss.NewStyle().Background(rowBg).Width(ctrlW).Render(control)
	return marker + labelCell + ctrlCell
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
	// Add sender badge
	sender := c.accountCfg.From
	if sender == "" {
		sender = c.accountCfg.User
	}
	if sender != "" {
		title += "  ◉ " + sender
	}
	header := renderManagerHeader(title, width, chrome)

	labelW := min(10, formLabelWidth(width)) // tight for short compose labels
	ctrlW := max(1, width-labelW-2) // available width inside the form row

	// Reusable text input for compose fields
	composeField := func(ti textinput.Model, field composeField) string {
		focused := c.focusedField == field
		bg := chrome.baseBg
		if focused {
			bg = chrome.fieldBg
		}
		ti.Width = ctrlW
		ti.Prompt = ""
		ti.PromptStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.accent)
		ti.TextStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.text)
		ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(chrome.muted)
		ti.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))
		view := truncateStyled(ti.View(), ctrlW, bg)
		return lipgloss.NewStyle().Background(bg).Foreground(chrome.text).Width(ctrlW).Render(view)
	}

	// Action bar
	actionKeys := []string{"ctrl+s", "send", "ctrl+a", "attach"}
	navKeys := []string{"tab", "next field", "esc", "cancel"}
	if len(c.attachments) > 0 {
		navKeys = []string{"tab", "next", "ctrl+r", "remove", "esc", "cancel"}
	}
	actions := renderManagerActionGroups(width, chrome, actionKeys, navKeys)
	if c.busy {
		actions = renderManagerActions(width, chrome)
	}

	// Count lines needed for attachments + quote
	attachLines := 0
	if len(c.attachments) > 0 {
		attachLines = 1 + len(c.attachments)
	}
	quoteLines := 0
	if c.inReplyTo != "" && !c.quoteCollapsed {
		quoteLines = 2 // toggle line + 1 line of preview
	} else if c.inReplyTo != "" {
		quoteLines = 1 // toggle line only
	}

	fixedH := lipgloss.Height(header) + 4 + lipgloss.Height(actions)
	if c.statusMsg != "" {
		fixedH++
	}
	bodyH := max(1, height-fixedH-attachLines-quoteLines)
	if bodyH < 1 {
		bodyH = 1
	}
	c.bodyInput.SetWidth(max(1, width-4))
	c.bodyInput.SetHeight(bodyH)
	c.bodyInput.FocusedStyle.Base = lipgloss.NewStyle().Background(chrome.fieldBg)
	c.bodyInput.BlurredStyle.Base = lipgloss.NewStyle().Background(chrome.baseBg)

	// Body with background fill and left accent border
	bodyBg := chrome.baseBg
	if c.focusedField == composeFieldBody {
		bodyBg = chrome.fieldBg
	}
	bodyInputView := c.bodyInput.View()
	bodyBorder := lipgloss.NewStyle().Background(bodyBg).Foreground(chrome.highlight).Render("▎")
	bodyContent := lipgloss.NewStyle().Background(bodyBg).Width(max(1, width-2)).Render(bodyInputView)
	bodyView := bodyBorder + bodyContent

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
			Width(width).
			Padding(0, 2).
			Render(c.statusMsg)
	}

	// Build rows
	var rows []string
	rows = append(rows, header)

	// ── Recipients ──
	rows = append(rows, renderFormGroupTitle("Recipients", width, chrome))
	rows = append(rows, renderComposeRow("To", c.focusedField == composeFieldTo, composeField(c.toInput, composeFieldTo), width, labelW, chrome))
	rows = append(rows, renderComposeRow("CC", c.focusedField == composeFieldCC, composeField(c.ccInput, composeFieldCC), width, labelW, chrome))

	// ── Message ──
	rows = append(rows, renderFormGroupTitle("Message", width, chrome))
	rows = append(rows, renderComposeRow("Subject", c.focusedField == composeFieldSubject, composeField(c.subjectInput, composeFieldSubject), width, labelW, chrome))
	rows = append(rows, bodyView)

	// ── Reply quote ──
	if c.inReplyTo != "" {
		qIcon := "▸"
		if !c.quoteCollapsed {
			qIcon = "▾"
		}
		qLine := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.muted).
			Width(width).
			Padding(0, 2).
			Render(fmt.Sprintf("  %s  Show quoted original (enter to toggle)", qIcon))
		rows = append(rows, qLine)
		if !c.quoteCollapsed {
			previewBg := chrome.baseBg
			preview := lipgloss.NewStyle().
				Background(previewBg).
				Foreground(chrome.muted).
				Width(width).
				Padding(0, 2).
				Render("  >  Original message quoted in body")
			rows = append(rows, preview)
		}
	}

	// ── Attachments ──
	if len(c.attachments) > 0 {
		rows = append(rows, renderFormGroupTitle("Attachments", width, chrome))
		for _, af := range c.attachments {
			icon := fileIcon(af.Name)
			line := fmt.Sprintf("  %s%s  %s    [ctrl+r remove]", icon, af.Name, humanSize(len(af.Data)))
			rows = append(rows, lipgloss.NewStyle().
				Background(chrome.baseBg).
				Foreground(chrome.accent).
				Width(width).
				Padding(0, 2).
				Render(line))
		}
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
				line = fmt.Sprintf("  ../")
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

// clearStatus resets the status after model.go shows it in the status bar.
func (c *ComposeModel) clearStatus() {
	c.statusMsg = ""
	c.isErr = false
}

// sentStatusLine returns a brief label for the status bar.
func composeSentStatus(err error) string {
	if err != nil {
		return fmt.Sprintf("send failed: %v", err)
	}
	return "message sent"
}
