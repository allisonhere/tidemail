package ui

import (
	"context"
	"fmt"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type simpleContacts struct {
	demo                     bool
	want                     string
	noteInitial              string
	provider                 string
	contacts                 []plugin.BookContact
	cursor                   int
	query                    textinput.Model
	searching, editing, busy bool
	fields                   []textinput.Model
	field                    int
	edit                     plugin.BookContact
	status                   string
	syncWarning              string
	seq                      uint64
	recent                   []string
}
type simpleContactsMsg struct {
	want     string
	owner    *simpleContacts
	seq      uint64
	contacts []plugin.BookContact
	recent   []string
	err      error
	saved    bool
	warning  string
}

func (m Model) openSimpleContacts() (tea.Model, tea.Cmd) {
	id := m.cfg.Contacts.Provider
	if id == "" {
		id = "tidecontacts"
	}
	if id != "tidecontacts" {
		return m, nil
	}
	p, ok := m.plugins.manager.Plugin(id)
	if !ok || !p.Manifest.Permissions.ContactsRead {
		return m, nil
	}
	s := &simpleContacts{provider: id, query: newAMInput("Search people", false), demo: m.tideContactsDemo()}
	m.simpleContacts = s
	m.overlay = overlayContactManager
	return m, m.simpleContactsLoad(false)
}
func (m Model) simpleContactsLoad(recent bool) tea.Cmd {
	s := m.simpleContacts
	s.seq++
	seq := s.seq
	s.busy = true
	manager := m.plugins.manager
	parent := m.plugins.ctx
	if parent == nil {
		parent = context.Background()
	}
	provider := s.provider
	database := m.db
	query := s.query.Value()
	demoOnly := s.demo && os.Getenv("TIDECONTACTS_DEMO") != "1"
	var selected plugin.BookContact
	if len(s.contacts) > 0 {
		selected = s.contacts[min(s.cursor, len(s.contacts)-1)]
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		msg := simpleContactsMsg{owner: s, seq: seq}
		if recent {
			if demoOnly {
				msg.recent = []string{}
			} else {
				msg.recent, msg.err = recentContactMail(ctx, database, selected.Addr)
			}
		} else {
			msg.contacts, msg.warning, msg.err = manager.ListBookContactsStatus(ctx, provider, query)
		}
		return msg
	}
}
func recentContactMail(ctx context.Context, database *db.DB, addr string) ([]string, error) {
	if addr == "" {
		return []string{}, nil
	}
	rows, err := database.QueryContext(ctx, `SELECT subject,from_addr,to_addr,cc_addr,date FROM messages WHERE instr(lower(from_addr||' '||to_addr||' '||cc_addr),lower(?))>0 ORDER BY date DESC,id DESC LIMIT 100`, addr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var subject, from, to, cc string
		var at int64
		if err := rows.Scan(&subject, &from, &to, &cc, &at); err != nil {
			return nil, err
		}
		match := false
		for _, header := range []string{from, to, cc} {
			list, e := mail.ParseAddressList(header)
			if e != nil {
				continue
			}
			for _, a := range list {
				if strings.EqualFold(a.Address, addr) {
					match = true
				}
			}
		}
		if match {
			out = append(out, time.Unix(at, 0).Format("2006-01-02")+"  "+subject)
		}
		if len(out) >= 15 {
			break
		}
	}
	return out, rows.Err()
}
func (m Model) handleSimpleContacts(msg tea.Msg) (tea.Model, tea.Cmd) {
	s := m.simpleContacts
	if result, ok := msg.(simpleContactsMsg); ok {
		if result.owner != s || result.seq != s.seq {
			return m, nil
		}
		s.busy = false
		if result.err != nil {
			s.status = result.err.Error()
			return m, nil
		}
		if result.saved {
			s.editing = false
			s.want = result.want
			s.status = "Contact saved"
			return m, m.simpleContactsLoad(false)
		}
		if result.contacts != nil {
			s.contacts = result.contacts
			if result.warning != "" || s.status == s.syncWarning {
				s.status = result.warning
			}
			s.syncWarning = result.warning
			if s.want != "" {
				for i, c := range s.contacts {
					if c.ID == s.want {
						s.cursor = i
						break
					}
				}
				s.want = ""
			}
			s.cursor = min(s.cursor, max(0, len(s.contacts)-1))
			return m, m.simpleContactsLoad(true)
		}
		s.recent = result.recent
		return m, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if s.editing {
		if s.busy {
			return m, nil
		}
		switch k.String() {
		case "esc":
			if !s.busy {
				s.editing = false
			}
			return m, nil
		case "tab", "down":
			s.fields[s.field].Blur()
			s.field = (s.field + 1) % len(s.fields)
			return m, s.fields[s.field].Focus()
		case "shift+tab", "up":
			s.fields[s.field].Blur()
			s.field = (s.field + len(s.fields) - 1) % len(s.fields)
			return m, s.fields[s.field].Focus()
		case "enter", "ctrl+s":
			if s.busy {
				return m, nil
			}
			c := s.edit
			c.Name = s.fields[0].Value()
			c.Addr = s.fields[1].Value()
			c.Phone = s.fields[2].Value()
			c.Organization = s.fields[3].Value()
			if s.fields[4].Value() != s.noteInitial {
				c.Note = s.fields[4].Value()
			}
			manager := m.plugins.manager
			parent := m.plugins.ctx
			if parent == nil {
				parent = context.Background()
			}
			provider := s.provider
			s.busy = true
			s.seq++
			seq := s.seq
			return m, func() tea.Msg {
				ctx, cancel := context.WithTimeout(parent, 15*time.Second)
				defer cancel()
				saved, err := manager.SaveBookContact(ctx, provider, c)
				return simpleContactsMsg{owner: s, seq: seq, err: err, saved: true, want: saved.ID}
			}
		}
		var cmd tea.Cmd
		s.fields[s.field], cmd = s.fields[s.field].Update(msg)
		return m, cmd
	}
	if s.searching {
		switch k.String() {
		case "esc":
			s.searching = false
			s.query.SetValue("")
			s.query.Blur()
			return m, m.simpleContactsLoad(false)
		case "enter":
			s.searching = false
			s.query.Blur()
			return m, m.simpleContactsLoad(false)
		}
		var cmd tea.Cmd
		s.query, cmd = s.query.Update(msg)
		return m, cmd
	}
	switch k.String() {
	case "esc", "q":
		m.simpleContacts = nil
		m.overlay = overlayNone
		return m, nil
	case "L":
		m.simpleContacts = nil
		m.contactManager = NewContactManager(m.db)
		return m, nil
	case "/":
		s.searching = true
		return m, s.query.Focus()
	case "j", "down":
		if s.cursor < len(s.contacts)-1 {
			s.cursor++
			return m, m.simpleContactsLoad(true)
		}
	case "k", "up":
		if s.cursor > 0 {
			s.cursor--
			return m, m.simpleContactsLoad(true)
		}
	case "r":
		if !s.busy {
			return m, m.simpleContactsLoad(false)
		}
		return m, nil
	case "n", "e":
		if s.busy {
			return m, nil
		}
		s.edit = plugin.BookContact{}
		if k.String() == "e" {
			if len(s.contacts) == 0 {
				return m, nil
			}
			s.edit = s.contacts[s.cursor]
			if s.edit.ReadOnly {
				s.status = "Read-only vCard folder contact. Edit the source vCard, then press r to refresh."
				return m, nil
			}
		}
		s.editing = true
		s.field = 0
		s.status = ""
		for i, value := range []string{s.edit.Name, s.edit.Addr, s.edit.Phone, s.edit.Organization, s.edit.Note} {
			in := newAMInput("", false)
			in.SetValue(value)
			if i < len(s.fields) {
				s.fields[i] = in
			} else {
				s.fields = append(s.fields, in)
			}
		}
		s.noteInitial = s.fields[4].Value()
		return m, s.fields[0].Focus()
	case "c", "enter":
		if len(s.contacts) == 0 {
			return m, nil
		}
		c := s.contacts[s.cursor]
		acfg, _ := m.cfg.DefaultAccount()
		m.compose = NewCompose(acfg, m.cfg.Accounts, m.addressBook)
		m.compose.toInput.SetValue(db.FormatMailbox(c.Name, c.Addr))
		m.simpleContacts = nil
		m.overlay = overlayCompose
		return m, nil
	case "A":
		return m, m.openTideContactsAdvanced(s.demo)

	}
	return m, nil
}
func (s *simpleContacts) View(width, height int, styles Styles) string {
	chrome := newManagerChrome(width, styles.Theme, styles.PlainUI)
	lines := []string{}
	if s.editing {
		labels := []string{"Name", "Email", "Phone", "Organization", "Note"}
		for i, in := range s.fields {
			in.Width = max(1, width-20)
			rail := "  "
			if i == s.field {
				rail = "> "
			}
			lines = append(lines, rail+fmt.Sprintf("%-14s", labels[i])+in.View())
		}
		lines = append(lines, "", "tab next field · enter save · esc cancel")
	} else {
		title := "People"
		if s.demo {
			title = "People · DEMO"
		}
		lines = append(lines, title+" · "+s.query.Value(), "")
		if s.searching {
			in := s.query
			in.Width = max(1, width-5)
			lines[0] = in.View()
		}
		limit := max(1, min(8, height/3))
		start := max(0, s.cursor-limit+1)
		for i := start; i < len(s.contacts) && i < start+limit; i++ {
			c := s.contacts[i]
			label := c.Name
			if label == "" {
				label = c.Addr
			}
			prefix := "  "
			if i == s.cursor {
				prefix = "> "
			}
			if c.ReadOnly {
				label += " [folder · read-only]"
			}
			lines = append(lines, truncate(prefix+label+" · "+c.Addr, width))
		}
		if len(s.contacts) == 0 {
			lines = append(lines, "No people yet. Press n to add a person.")
		}
		lines = append(lines, "", "Recent correspondence")
		if len(s.recent) == 0 {
			lines = append(lines, "No matching mail in the local cache.")
		}
		for _, line := range s.recent {
			if len(lines) >= height-8 {
				break
			}
			lines = append(lines, truncate(sanitizeSimpleText(line), width))
		}
		lines = append(lines, "", "n add · e edit · c write email · / search · r refresh", "A Advanced CRM · L local contacts (separate) · esc close")
	}
	if s.busy {
		lines = append(lines, "Working…")
	}
	if s.status != "" {
		lines = append(lines, truncate(sanitizeSimpleText(s.status), width))
	}
	source := "Address book: TideContacts"
	shared := "Shared with standalone TideContacts (same profile)."
	if s.demo {
		source = "Address book: TideContacts DEMO"
		shared = "Shared with Advanced CRM demo only; real contacts are separate."
	}
	lines = append([]string{truncate(source, width), truncate(shared, width), ""}, lines...)
	return lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Width(width).Render(strings.Join(lines, "\n"))
}
func sanitizeSimpleText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}

type advancedCRMClosedMsg struct{ err error }

func (m Model) tideContactsSetting(key, fallback string) string {
	p, ok := m.plugins.manager.Plugin("tidecontacts")
	if !ok {
		return fallback
	}
	values := plugin.ResolveSettings(p.Manifest.Settings, m.cfg.PluginStoredSettings("tidecontacts"))
	if value, ok := values[key].(string); ok {
		return value
	}
	return fallback
}
func (m Model) tideContactsDemo() bool {
	return os.Getenv("TIDECONTACTS_DEMO") == "1" || m.tideContactsSetting("data_profile", "Real contacts") == "Demo contacts"
}
func (m Model) tideContactsViewEnabled() bool {
	return m.tideContactsSetting("default_view", "TideContacts") == "TideContacts"
}
func (m Model) openTideContactsAdvanced(useDemo bool) tea.Cmd {
	path, err := exec.LookPath("tidecontacts")
	if p, ok := m.plugins.manager.Plugin("tidecontacts"); ok {
		candidate := filepath.Join(p.Dir, "tidecontacts")
		if info, e := os.Stat(candidate); e == nil && info.Mode().IsRegular() {
			path = candidate
			err = nil
		}
	}
	if err != nil {
		return func() tea.Msg {
			return advancedCRMClosedMsg{fmt.Errorf("advanced CRM is not installed; reinstall the TideContacts plugin")}
		}
	}
	args := []string{}
	if useDemo || os.Getenv("TIDECONTACTS_DEMO") == "1" {
		args = append(args, "--demo")
	}
	cmd := advancedCRMCommand(path, args)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return advancedCRMClosedMsg{err} })
}

// TideMail's renderer uses SerializedStdout, an io.Writer wrapper. Letting
// Bubble Tea forward that wrapper makes os/exec create a pipe, hiding the TTY
// from interactive children. ExecProcess releases the parent terminal before
// Run; bind the child's streams directly to the terminal for that interval.
func advancedCRMCommand(path string, args []string) *exec.Cmd {
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}
