package db

import "time"

// RuleRecord is the persisted form of a filter rule. The Rule definition itself
// lives in internal/filter and is stored here as an opaque JSON blob, so the db
// package stays free of any dependency on the filter engine.
type RuleRecord struct {
	ID        int64
	AccountID int64 // 0 = applies to all accounts
	Priority  int
	Enabled   bool
	Name      string
	JSON      string
	CreatedAt time.Time
}

// ListRules returns all rules ordered by priority (then id) — evaluation order.
func (db *DB) ListRules() ([]RuleRecord, error) {
	rows, err := db.Query(`
		SELECT id, account_id, priority, enabled, name, json, created_at
		FROM rules ORDER BY priority, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RuleRecord
	for rows.Next() {
		var r RuleRecord
		var enabled int
		var created int64
		if err := rows.Scan(&r.ID, &r.AccountID, &r.Priority, &enabled, &r.Name, &r.JSON, &created); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		r.CreatedAt = time.Unix(created, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertRule inserts a new rule (ID == 0) or updates an existing one, returning
// the rule's ID.
func (db *DB) UpsertRule(r RuleRecord) (int64, error) {
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	if r.ID == 0 {
		res, err := db.Exec(`
			INSERT INTO rules (account_id, priority, enabled, name, json, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			r.AccountID, r.Priority, enabled, r.Name, r.JSON, time.Now().Unix())
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := db.Exec(`
		UPDATE rules SET account_id = ?, priority = ?, enabled = ?, name = ?, json = ?
		WHERE id = ?`,
		r.AccountID, r.Priority, enabled, r.Name, r.JSON, r.ID)
	return r.ID, err
}

// DeleteRule removes a rule by id.
func (db *DB) DeleteRule(id int64) error {
	_, err := db.Exec(`DELETE FROM rules WHERE id = ?`, id)
	return err
}

// SetRuleEnabled toggles a rule's enabled flag.
func (db *DB) SetRuleEnabled(id int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := db.Exec(`UPDATE rules SET enabled = ? WHERE id = ?`, v, id)
	return err
}

// SetRulePriority updates a rule's evaluation priority.
func (db *DB) SetRulePriority(id int64, priority int) error {
	_, err := db.Exec(`UPDATE rules SET priority = ? WHERE id = ?`, priority, id)
	return err
}
