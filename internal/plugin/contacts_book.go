package plugin

import (
	"context"
	"fmt"
	"net/mail"
)

// BookContact is the editable, basic contact projection; CRM-only fields never
// cross this boundary. Revision prevents overwriting edits made in another app.
type BookContact struct {
	ID           string `json:"id"`
	Revision     int    `json:"revision"`
	Name         string `json:"name"`
	Addr         string `json:"addr"`
	Phone        string `json:"phone"`
	Organization string `json:"organization"`
	Note         string `json:"note"`
	ReadOnly     bool   `json:"read_only,omitempty"`
	Source       string `json:"source,omitempty"`
}

func validateBookContact(c BookContact) error {
	if c.ID == "" || len(c.ID) > 100 || c.Revision < 1 {
		return fmt.Errorf("invalid contact identity")
	}
	a, err := mail.ParseAddress(c.Addr)
	if err != nil || a.Address != c.Addr {
		return fmt.Errorf("invalid contact email")
	}
	for name, value := range map[string]string{"name": c.Name, "address": c.Addr, "phone": c.Phone, "organization": c.Organization} {
		if err := presentationText(name, value, 500, false); err != nil {
			return err
		}
	}
	if len(c.Note) > 65536 {
		return fmt.Errorf("contact note too long")
	}
	return nil
}
func (m *Manager) ListBookContacts(ctx context.Context, id, query string) ([]BookContact, error) {
	contacts, _, err := m.ListBookContactsStatus(ctx, id, query)
	return contacts, err
}
func (m *Manager) ListBookContactsStatus(ctx context.Context, id, query string) ([]BookContact, string, error) {
	if err := presentationText("query", query, 200, false); err != nil {
		return nil, "", err
	}
	var result struct {
		Contacts []BookContact `json:"contacts"`
		Warning  string        `json:"warning"`
	}
	err := m.cryptoCall(ctx, id, "contacts.list", "contacts_read", map[string]any{"query": query, "limit": 200}, &result, func(p Permissions) bool { return p.ContactsRead })
	if err != nil {
		return nil, "", err
	}
	if len(result.Contacts) > 200 {
		return nil, "", fmt.Errorf("too many contacts returned")
	}
	if err := presentationText("warning", result.Warning, 65536, false); err != nil {
		return nil, "", err
	}
	for _, c := range result.Contacts {
		if err := validateBookContact(c); err != nil {
			return nil, "", err
		}
		if err := presentationText("source", c.Source, 500, false); err != nil {
			return nil, "", err
		}
	}
	return result.Contacts, result.Warning, nil
}
func (m *Manager) SaveBookContact(ctx context.Context, id string, c BookContact) (BookContact, error) {
	var result struct {
		Contact BookContact `json:"contact"`
	}
	err := m.cryptoCall(ctx, id, "contacts.save", "contacts_write", c, &result, func(p Permissions) bool { return p.ContactsWrite })
	if err != nil {
		return BookContact{}, err
	}
	return result.Contact, validateBookContact(result.Contact)
}
