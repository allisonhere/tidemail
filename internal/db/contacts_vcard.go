package db

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/emersion/go-vcard"
)

// VCardImport is what an import did. A card with no email address cannot be
// kept (contacts are keyed by address), so it is counted instead of dropped
// silently.
type VCardImport struct {
	Added   int // new contacts saved
	Cards   int // cards read
	NoEmail int // cards skipped because they have no email address
}

// Add folds another result into r.
func (r *VCardImport) Add(o VCardImport) {
	r.Added += o.Added
	r.Cards += o.Cards
	r.NoEmail += o.NoEmail
}

// ImportVCard reads vCards from r and returns how many contacts it added.
func (db *DB) ImportVCard(r io.Reader) (int, error) {
	res, err := db.ImportVCardCounts(r)
	return res.Added, err
}

// ImportVCardCounts is ImportVCard with a full account of what was skipped.
func (db *DB) ImportVCardCounts(r io.Reader) (VCardImport, error) {
	dec := vcard.NewDecoder(r)
	var res VCardImport
	for {
		card, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return res, err
		}
		res.Cards++
		name := vcardDisplayName(card)
		meta := vcardContactMetadata(card)
		hasEmail := false
		for _, field := range card[vcard.FieldEmail] {
			if field == nil || strings.TrimSpace(field.Value) == "" {
				continue
			}
			hasEmail = true
			id, err := db.AddContactWithMetadata(field.Value, name, "vcard", meta)
			if err != nil {
				return res, err
			}
			if id != 0 {
				res.Added++
			}
		}
		if !hasEmail {
			res.NoEmail++
		}
	}
	return res, nil
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

// VCardDirImport is what importing a folder of .vcf files did.
type VCardDirImport struct {
	VCardImport
	Files     int    // .vcf files found
	Failed    int    // files that could not be read or parsed
	FirstFail string // the first failing file, with its reason, for the status line
}

// ImportVCardDir imports every .vcf file directly inside dir (subfolders are
// not searched, so one address book is one folder, as vdirsyncer lays them
// out). A file that fails does not stop the rest: it is counted and the import
// carries on, so one odd card cannot hide the other hundreds.
func (db *DB) ImportVCardDir(dir string) (VCardDirImport, error) {
	var res VCardDirImport
	entries, err := os.ReadDir(dir)
	if err != nil {
		return res, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".vcf") {
			continue
		}
		res.Files++
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err == nil {
			var one VCardImport
			one, err = db.ImportVCardCounts(f)
			_ = f.Close()
			res.Add(one) // keep what was read before any error in this file
		}
		if err != nil {
			res.Failed++
			if res.FirstFail == "" {
				res.FirstFail = e.Name() + ": " + err.Error()
			}
		}
	}
	return res, nil
}
