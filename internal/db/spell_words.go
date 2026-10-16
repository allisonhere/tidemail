package db

import "strings"

// ListSpellWords returns the user's personal spell-check words, sorted.
func (db *DB) ListSpellWords() ([]string, error) {
	rows, err := db.Query(`SELECT word FROM spell_words ORDER BY word`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// AddSpellWord remembers word for spell check. Words are stored lowercase and
// adding one twice is harmless.
func (db *DB) AddSpellWord(word string) error {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return nil
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO spell_words (word) VALUES (?)`, word)
	return err
}

// RemoveSpellWord forgets a personal spell-check word.
func (db *DB) RemoveSpellWord(word string) error {
	_, err := db.Exec(`DELETE FROM spell_words WHERE word = ?`, strings.ToLower(strings.TrimSpace(word)))
	return err
}
