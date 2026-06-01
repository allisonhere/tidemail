package db

import (
	"net/mail"
	"sort"
	"strings"
	"time"
)

type Contact struct {
	ID           int64
	Addr         string
	DisplayName  string
	Source       string
	CreatedAt    time.Time
	Phone        string
	Organization string
	Title        string
	Note         string
}

type ContactMetadata struct {
	Phone        string
	Organization string
	Title        string
	Note         string
}

func normalizeAddress(raw string) (addr, name string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if a, err := mail.ParseAddress(raw); err == nil {
		return strings.ToLower(strings.TrimSpace(a.Address)), strings.TrimSpace(a.Name)
	}
	if strings.Contains(raw, "@") && !strings.ContainsAny(raw, " <>") {
		return strings.ToLower(raw), ""
	}
	return "", ""
}

func scanCuratedContacts(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]Contact, error) {
	var out []Contact
	for rows.Next() {
		var c Contact
		var createdAt int64
		if err := rows.Scan(&c.ID, &c.Addr, &c.DisplayName, &c.Source, &createdAt, &c.Phone, &c.Organization, &c.Title, &c.Note); err != nil {
			return out, err
		}
		if createdAt > 0 {
			c.CreatedAt = time.Unix(createdAt, 0)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (db *DB) ListContacts() ([]Contact, error) {
	rows, err := db.Query(`
		SELECT id, addr, display_name, source, created_at, phone, organization, title, note
		FROM contacts
		ORDER BY (display_name = '') ASC, display_name COLLATE NOCASE, addr`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCuratedContacts(rows)
}

func (db *DB) AddContact(rawAddr, rawName, source string) (int64, error) {
	return db.AddContactWithMetadata(rawAddr, rawName, source, ContactMetadata{})
}

func (db *DB) AddContactWithMetadata(rawAddr, rawName, source string, meta ContactMetadata) (int64, error) {
	addr, parsedName := normalizeAddress(rawAddr)
	if addr == "" {
		return 0, nil
	}
	name := strings.TrimSpace(rawName)
	if name == "" {
		name = parsedName
	}
	if source == "" {
		source = "manual"
	}
	now := time.Now().Unix()
	_, err := db.Exec(`
		INSERT INTO contacts (addr, display_name, source, created_at, phone, organization, title, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(addr) DO UPDATE SET
			display_name = excluded.display_name,
			source       = excluded.source,
			phone        = CASE WHEN excluded.phone != '' THEN excluded.phone ELSE contacts.phone END,
			organization = CASE WHEN excluded.organization != '' THEN excluded.organization ELSE contacts.organization END,
			title        = CASE WHEN excluded.title != '' THEN excluded.title ELSE contacts.title END,
			note         = CASE WHEN excluded.note != '' THEN excluded.note ELSE contacts.note END`,
		addr, name, source, now, strings.TrimSpace(meta.Phone), strings.TrimSpace(meta.Organization), strings.TrimSpace(meta.Title), strings.TrimSpace(meta.Note))
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.QueryRow(`SELECT id FROM contacts WHERE addr = ?`, addr).Scan(&id)
	return id, err
}

func (db *DB) UpdateContact(id int64, rawAddr, rawName string) error {
	addr, parsedName := normalizeAddress(rawAddr)
	if addr == "" {
		return nil
	}
	name := strings.TrimSpace(rawName)
	if name == "" {
		name = parsedName
	}
	_, err := db.Exec(`UPDATE contacts SET addr = ?, display_name = ? WHERE id = ?`, addr, name, id)
	return err
}

func (db *DB) UpdateContactWithMetadata(id int64, rawAddr, rawName string, meta ContactMetadata) error {
	addr, parsedName := normalizeAddress(rawAddr)
	if addr == "" {
		return nil
	}
	name := strings.TrimSpace(rawName)
	if name == "" {
		name = parsedName
	}
	_, err := db.Exec(`
		UPDATE contacts
		SET addr = ?, display_name = ?, phone = ?, organization = ?, title = ?, note = ?
		WHERE id = ?`,
		addr, name, strings.TrimSpace(meta.Phone), strings.TrimSpace(meta.Organization), strings.TrimSpace(meta.Title), strings.TrimSpace(meta.Note), id)
	return err
}

func (db *DB) DeleteContact(id int64) error {
	_, err := db.Exec(`DELETE FROM contacts WHERE id = ?`, id)
	return err
}

func (db *DB) ContactAddresses() ([]string, error) {
	contacts, err := db.ListContacts()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(contacts))
	for _, c := range contacts {
		if c.DisplayName != "" {
			out = append(out, c.DisplayName+" <"+c.Addr+">")
		} else {
			out = append(out, c.Addr)
		}
	}
	return out, nil
}

func (db *DB) SeenAddresses() ([]Contact, error) {
	knownContacts, err := db.ListContacts()
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(knownContacts))
	for _, c := range knownContacts {
		known[c.Addr] = true
	}

	addrs, err := db.ListAddresses()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]Contact)
	for _, raw := range addrs {
		addr, name := normalizeAddress(raw)
		if addr == "" || known[addr] {
			continue
		}
		if existing, ok := seen[addr]; ok {
			if existing.DisplayName == "" && name != "" {
				existing.DisplayName = name
				seen[addr] = existing
			}
			continue
		}
		seen[addr] = Contact{Addr: addr, DisplayName: name, Source: "seen"}
	}
	out := make([]Contact, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		li := strings.ToLower(out[i].DisplayName)
		lj := strings.ToLower(out[j].DisplayName)
		if li == lj {
			return out[i].Addr < out[j].Addr
		}
		return li < lj
	})
	return out, nil
}
