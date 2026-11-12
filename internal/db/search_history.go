package db

import (
	"strings"
	"time"
)

// MaxSearchHistory is how many recent searches are kept.
const MaxSearchHistory = 30

// RecordSearch remembers a committed search, newest first. A repeat (ignoring
// case) moves to the front instead of being listed twice, and only the most
// recent MaxSearchHistory are kept.
func (db *DB) RecordSearch(query string) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	// Nanoseconds keep two searches in the same second in order.
	if _, err := tx.Exec(`INSERT INTO search_history (query, used_at) VALUES (?, ?)
		ON CONFLICT(query) DO UPDATE SET query = excluded.query, used_at = excluded.used_at`,
		query, time.Now().UnixNano()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM search_history WHERE query NOT IN
		(SELECT query FROM search_history ORDER BY used_at DESC LIMIT ?)`, MaxSearchHistory); err != nil {
		return err
	}
	return tx.Commit()
}

// RecentSearches lists remembered searches, newest first. A non-empty prefix
// keeps only those starting with it, ignoring case.
func (db *DB) RecentSearches(prefix string, limit int) ([]string, error) {
	like := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(prefix)) + "%"
	rows, err := db.Query(`SELECT query FROM search_history WHERE query LIKE ? ESCAPE '\'
		ORDER BY used_at DESC LIMIT ?`, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var q string
		if err := rows.Scan(&q); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// DeleteSearch forgets one remembered search.
func (db *DB) DeleteSearch(query string) error {
	_, err := db.Exec(`DELETE FROM search_history WHERE query = ?`, strings.TrimSpace(query))
	return err
}

// ClearSearchHistory forgets every remembered search.
func (db *DB) ClearSearchHistory() error {
	_, err := db.Exec(`DELETE FROM search_history`)
	return err
}
