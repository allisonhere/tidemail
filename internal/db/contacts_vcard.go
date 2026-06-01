package db

import (
	"io"
	"strings"

	"github.com/emersion/go-vcard"
)

func (db *DB) ImportVCard(r io.Reader) (int, error) {
	dec := vcard.NewDecoder(r)
	added := 0
	for {
		card, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return added, err
		}
		name := vcardDisplayName(card)
		meta := vcardContactMetadata(card)
		for _, field := range card[vcard.FieldEmail] {
			if field == nil || strings.TrimSpace(field.Value) == "" {
				continue
			}
			id, err := db.AddContactWithMetadata(field.Value, name, "vcard", meta)
			if err != nil {
				return added, err
			}
			if id != 0 {
				added++
			}
		}
	}
	return added, nil
}

func vcardDisplayName(card vcard.Card) string {
	if name := strings.TrimSpace(card.PreferredValue(vcard.FieldFormattedName)); name != "" {
		return name
	}
	n := card.Name()
	if n == nil {
		return ""
	}
	parts := []string{
		strings.TrimSpace(n.HonorificPrefix),
		strings.TrimSpace(n.GivenName),
		strings.TrimSpace(n.AdditionalName),
		strings.TrimSpace(n.FamilyName),
		strings.TrimSpace(n.HonorificSuffix),
	}
	var kept []string
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

func vcardContactMetadata(card vcard.Card) ContactMetadata {
	return ContactMetadata{
		Phone:        preferredVCardValue(card, vcard.FieldTelephone),
		Organization: preferredVCardValue(card, vcard.FieldOrganization),
		Title:        preferredVCardValue(card, vcard.FieldTitle),
		Note:         preferredVCardValue(card, vcard.FieldNote),
	}
}

func preferredVCardValue(card vcard.Card, field string) string {
	if value := strings.TrimSpace(card.PreferredValue(field)); value != "" {
		return value
	}
	for _, f := range card[field] {
		if f == nil {
			continue
		}
		if value := strings.TrimSpace(f.Value); value != "" {
			return value
		}
	}
	return ""
}

func (db *DB) ExportVCard(w io.Writer) error {
	contacts, err := db.ListContacts()
	if err != nil {
		return err
	}
	enc := vcard.NewEncoder(w)
	for _, c := range contacts {
		card := make(vcard.Card)
		name := c.DisplayName
		if name == "" {
			name = c.Addr
		}
		card.SetValue(vcard.FieldFormattedName, name)
		card.SetValue(vcard.FieldEmail, c.Addr)
		if c.Phone != "" {
			card.SetValue(vcard.FieldTelephone, c.Phone)
		}
		if c.Organization != "" {
			card.SetValue(vcard.FieldOrganization, c.Organization)
		}
		if c.Title != "" {
			card.SetValue(vcard.FieldTitle, c.Title)
		}
		if c.Note != "" {
			card.SetValue(vcard.FieldNote, c.Note)
		}
		vcard.ToV4(card)
		if err := enc.Encode(card); err != nil {
			return err
		}
	}
	return nil
}
