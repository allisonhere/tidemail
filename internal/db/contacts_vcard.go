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
		for _, field := range card[vcard.FieldEmail] {
			if field == nil || strings.TrimSpace(field.Value) == "" {
				continue
			}
			id, err := db.AddContact(field.Value, name, "vcard")
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
		vcard.ToV4(card)
		if err := enc.Encode(card); err != nil {
			return err
		}
	}
	return nil
}
