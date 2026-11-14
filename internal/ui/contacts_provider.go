package ui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
)

// External contact lookup. In a recipient field, Ctrl+T sends the text being
// typed to the plugin named by [contacts] provider and shows what it returns
// in the same dropdown as the local suggestions. It never runs while typing,
// so a slow plugin cannot make typing lag, and nothing it returns is stored.

// contactsLookupTimeout bounds one contacts.search call.
const contactsLookupTimeout = 5 * time.Second

// ContactsFoundMsg carries the outcome of one contacts.search lookup. Query is
// the segment that was sent, so a late answer for text the user has since
// changed is dropped.
type ContactsFoundMsg struct {
	Query   string
	Matches []plugin.ContactMatch
	Err     error
}

// contactsProvider returns the plugin chosen by [contacts] provider, and a
// reason when there is none usable.
func (m Model) contactsProvider() (plugin.Plugin, string) {
	id := strings.TrimSpace(m.cfg.Contacts.Provider)
	if id == "" {
		return plugin.Plugin{}, "No contacts provider set (add [contacts] provider to config.toml)"
	}
	p, ok := m.plugins.manager.Plugin(id)
	if !ok {
		return plugin.Plugin{}, "Contacts provider " + id + " is not installed"
	}
	if !p.Manifest.Permissions.ContactsRead {
		return plugin.Plugin{}, "Contacts provider " + id + " does not declare contacts_read"
	}
	return p, ""
}

// lookupContacts starts a lookup for the recipient segment being typed.
func (m Model) lookupContacts() (tea.Model, tea.Cmd) {
	input := m.compose.focusedRecipientInput()
	if input == nil {
		return m, nil
	}
	_, seg := splitActiveSegment(input.Value())
	seg = strings.TrimSpace(seg)
	m.compose.isErr = false
	if seg == "" {
		m.compose.statusMsg = "Type part of a name or address, then press ctrl+t"
		return m, nil
	}
	p, why := m.contactsProvider()
	if why != "" {
		m.compose.isErr = true
		m.compose.statusMsg = why
		return m, nil
	}
	m.compose.statusMsg = "Looking up contacts…"
	mgr, parent, id := m.plugins.manager, m.plugins.ctx, p.Manifest.ID
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, contactsLookupTimeout)
		defer cancel()
		matches, err := mgr.SearchContacts(ctx, id, seg)
		return ContactsFoundMsg{Query: seg, Matches: matches, Err: err}
	}
}

func (m Model) handleContactsFound(msg ContactsFoundMsg) (tea.Model, tea.Cmd) {
	if m.overlay != overlayCompose {
		return m, nil
	}
	input := m.compose.focusedRecipientInput()
	if input == nil {
		return m, nil
	}
	if _, seg := splitActiveSegment(input.Value()); strings.TrimSpace(seg) != msg.Query {
		return m, nil // the text changed while the plugin ran
	}
	if msg.Err != nil {
		m.compose.isErr = true
		m.compose.statusMsg = "Contact lookup failed: " + sanitizePluginLine(msg.Err.Error())
		return m, nil
	}
	if len(msg.Matches) == 0 {
		m.compose.isErr = false
		m.compose.statusMsg = "No matching contacts"
		return m, nil
	}
	rows := make([]string, 0, maxRecipientSuggestions)
	for _, c := range msg.Matches {
		rows = append(rows, db.FormatMailbox(c.Name, c.Addr))
		if len(rows) == maxRecipientSuggestions {
			break
		}
	}
	m.compose.suggestions = rows
	m.compose.suggestCursor = 0
	m.compose.suggestDismissed = false
	m.compose.isErr = false
	m.compose.statusMsg = ""
	return m, nil
}
