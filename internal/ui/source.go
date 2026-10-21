package ui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
)

// Viewing a message's source. Ctrl+O fetches the message's exact RFC 822
// bytes from the server (without marking it read) and shows them in an
// overlay; c copies them to the clipboard unchanged. Nothing is stored: the
// bytes live in sourceState until the overlay closes.

// sourceTimeout bounds one fetch.
const sourceTimeout = time.Minute

// maxSourceDisplay caps how much of a message the overlay draws; copying
// always takes the whole message.
const maxSourceDisplay = 1 << 20

type sourceState struct {
	messageID int64
	subject   string
	pending   bool
	err       string
	raw       string // exact bytes, for copying
	vp        viewport.Model
}

// SourceFetchedMsg carries a fetched message source back to the model.
type SourceFetchedMsg struct {
	MessageID int64
	Raw       []byte
	Err       error
}

// openSource opens the source overlay for msg and starts the fetch.
func (m Model) openSource(msg db.Message) (tea.Model, tea.Cmd) {
	mailbox := m.mailboxByID(msg.MailboxID)
	acfg, err := m.accountCfgForMailbox(msg.MailboxID)
	if mailbox == nil || err != nil || acfg.IMAPHost == "" || msg.UID == 0 {
		m.setStatus("This message can't be fetched from a server", true)
		return m, m.clearStatusCmd()
	}
	m.source = sourceState{messageID: msg.ID, subject: msg.Subject, pending: true}
	m.overlay = overlaySource
	m.layoutSource()

	sessions := m.sessions
	mailboxName, uid, id := mailbox.Name, msg.UID, msg.ID
	fetch := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sourceTimeout)
		defer cancel()
		var raw []byte
		err := sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
			var ferr error
			raw, ferr = client.FetchRaw(ctx, mailboxName, uid)
			return ferr
		})
		if err != nil {
			return SourceFetchedMsg{MessageID: id, Err: fmt.Errorf("couldn't fetch the message: %w", err)}
		}
		return SourceFetchedMsg{MessageID: id, Raw: raw}
	}
	return m, fetch
}

func (m Model) handleSourceFetched(msg SourceFetchedMsg) (tea.Model, tea.Cmd) {
	// A late reply for an overlay that was closed or moved on is dropped.
	if m.overlay != overlaySource || msg.MessageID != m.source.messageID {
		return m, nil
	}
	m.source.pending = false
	if msg.Err != nil {
		m.source.err = msg.Err.Error()
	} else {
		m.source.raw = string(msg.Raw)
	}
	m.layoutSource()
	return m, nil
}

func (m Model) handleSourceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Back, m.keys.Quit, m.keys.ViewSource):
		m.overlay = overlayNone
		m.source = sourceState{}
		return m, nil
	case msg.String() == "c" || msg.String() == "y" || keyMatches(msg, m.keys.CopyText):
		if m.source.raw != "" {
			return m, clipboardWriteCmd(m.source.raw)
		}
		return m, nil
	case msg.String() == "g" || msg.String() == "home":
		m.source.vp.GotoTop()
		return m, nil
	case msg.String() == "G" || msg.String() == "end":
		m.source.vp.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.source.vp, cmd = m.source.vp.Update(msg)
	return m, cmd
}

// sourceGeometry is the overlay's outer content width and height, shared by
// layout and rendering so the viewport always fits its box.
func (m Model) sourceGeometry() (width, height int) {
	return max(1, min(m.width-6, 100)), max(5, min(m.height-4, 44))
}

// layoutSource sizes the viewport and fills it. It runs on open, when the
// fetch returns, and on resize, so scrolling in Update sees the real bounds.
func (m *Model) layoutSource() {
	width, height := m.sourceGeometry()
	chrome := newManagerChrome(width, m.styles.Theme, m.styles.PlainUI)
	offset := m.source.vp.YOffset
	m.source.vp = viewport.New(width, max(1, height-3)) // border top/bottom, hints row
	m.source.vp.Style = lipgloss.NewStyle().Background(chrome.baseBg)
	m.source.vp.SetContent(m.sourceContent(width, chrome))
	m.source.vp.SetYOffset(offset)
}

func (m Model) sourceContent(width int, chrome managerChrome) string {
	text := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
	muted := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
	switch {
	case m.source.pending:
		return muted.Width(width).Render(" Fetching message source…")
	case m.source.err != "":
		return lipgloss.NewStyle().Background(chrome.baseBg).Foreground(m.styles.Theme.Error).Width(width).
			Render(" " + sanitizePluginLine(m.source.err))
	}
	shown, note := m.source.raw, ""
	if len(shown) > maxSourceDisplay {
		shown = shown[:maxSourceDisplay]
		note = fmt.Sprintf("… showing the first %d KB of %d KB; c copies all of it", maxSourceDisplay>>10, len(m.source.raw)>>10)
	}
	lines := sourceDisplayLines(shown, max(1, width-1))
	for i, l := range lines {
		lines[i] = text.Width(width).Render(" " + l)
	}
	if note != "" {
		lines = append(lines, muted.Width(width).Render(" "+note))
	}
	return strings.Join(lines, "\n")
}

// sourceDisplayLines makes raw message bytes safe to draw: CRLF line endings
// become plain lines, tabs expand to spaces, control and format characters
// (escape sequences, bidi overrides) are dropped, invalid UTF-8 shows as "?",
// and long lines wrap at width instead of being cut off.
func sourceDisplayLines(raw string, width int) []string {
	raw = strings.ToValidUTF8(raw, "?")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimSuffix(raw, "\n")
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		var b strings.Builder
		col := 0
		for _, r := range line {
			switch {
			case r == '\t':
				n := 8 - col%8
				b.WriteString(strings.Repeat(" ", n))
				col += n
			case unicode.Is(unicode.Cc, r), unicode.Is(unicode.Cf, r):
			default:
				b.WriteRune(r)
				col++
			}
		}
		wrapped := ansi.Hardwrap(b.String(), width, true)
		out = append(out, strings.Split(wrapped, "\n")...)
	}
	return out
}

func (m Model) renderSourceOverlay() string {
	width, _ := m.sourceGeometry()
	chrome := newManagerChrome(width, m.styles.Theme, m.styles.PlainUI)
	pairs := []string{"j/k", "scroll", "home/end", "top/bottom"}
	if m.source.raw != "" {
		pairs = append(pairs, "c", "copy")
	}
	pairs = append(pairs, "esc", "close")
	hints := renderSoftHints(width, chrome, pairs...)
	inner := lipgloss.JoinVertical(lipgloss.Left, m.source.vp.View(), hints)
	inner = clampView(inner, width, strings.Count(inner, "\n")+1, chrome.baseBg)
	title := "message source"
	if s := sanitizePluginLine(m.source.subject); s != "" {
		title += " · " + s
	}
	return renderSoftPanelBox(inner, width, "tidemail", title, chrome)
}
