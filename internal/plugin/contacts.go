package plugin

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
)

// MethodContactsSearch asks a plugin for contacts matching text the user typed
// in a recipient field. Only plugins with the contacts_read permission receive
// it.
const MethodContactsSearch = "contacts.search"

// Limits on what a contacts plugin may return. Plugin output is untrusted.
const (
	// MaxContactResults caps how many matches TideMail asks for and keeps.
	MaxContactResults = 50
	maxContactQuery   = 200
	maxContactName    = 200
	maxContactAddr    = 320
)

// ContactsSearchRequest is the data of a contacts.search request.
type ContactsSearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

// ContactMatch is one contact a plugin returns.
type ContactMatch struct {
	Addr string `json:"addr"`
	Name string `json:"name,omitempty"`
}

// ContactsSearchResult is the validated data of a contacts.search response.
type ContactsSearchResult struct {
	Contacts []ContactMatch `json:"contacts"`
}

// SearchContacts sends a typed query to a plugin that declares contacts_read.
// It refuses, without starting the plugin, otherwise. Matches with an invalid
// address or unsafe text are dropped, and the list is capped, so a plugin
// cannot put control characters or an unbounded list into the recipient field.
func (m *Manager) SearchContacts(ctx context.Context, pluginID, query string) ([]ContactMatch, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if err := presentationText("query", query, maxContactQuery, true); err != nil {
		return nil, fmt.Errorf("plugin %q: %w", pluginID, err)
	}
	req := ContactsSearchRequest{Query: query, Limit: MaxContactResults}
	var res ContactsSearchResult
	err := m.cryptoCall(ctx, pluginID, MethodContactsSearch, "contacts_read", req, &res,
		func(p Permissions) bool { return p.ContactsRead })
	if err != nil {
		return nil, err
	}
	return cleanContactMatches(res.Contacts), nil
}

func cleanContactMatches(in []ContactMatch) []ContactMatch {
	out := make([]ContactMatch, 0, len(in))
	for _, c := range in {
		c.Addr = strings.TrimSpace(c.Addr)
		c.Name = strings.TrimSpace(c.Name)
		if presentationText("address", c.Addr, maxContactAddr, true) != nil ||
			presentationText("name", c.Name, maxContactName, false) != nil {
			continue
		}
		parsed, err := mail.ParseAddress(c.Addr)
		if err != nil || parsed.Name != "" || parsed.Address != c.Addr {
			continue
		}
		out = append(out, c)
		if len(out) == MaxContactResults {
			break
		}
	}
	return out
}
