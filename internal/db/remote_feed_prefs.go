package db

import "strings"

type RemoteFeedPref struct {
	FolderID int64
	Title    string
}

func (db *DB) ListRemoteFeedPrefs() (map[int64]RemoteFeedPref, error) {
	rows, err := db.Query(`
		SELECT remote_feed_id, COALESCE(folder_id, 0), title
		FROM remote_feed_prefs
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prefs := map[int64]RemoteFeedPref{}
	for rows.Next() {
		var remoteFeedID int64
		var pref RemoteFeedPref
		if err := rows.Scan(&remoteFeedID, &pref.FolderID, &pref.Title); err != nil {
			return nil, err
		}
		prefs[remoteFeedID] = pref
	}
	return prefs, rows.Err()
}

func (db *DB) ListRemoteFeedFolders() (map[int64]int64, error) {
	prefs, err := db.ListRemoteFeedPrefs()
	if err != nil {
		return nil, err
	}

	assignments := map[int64]int64{}
	for remoteFeedID, pref := range prefs {
		if pref.FolderID != 0 {
			assignments[remoteFeedID] = pref.FolderID
		}
	}
	return assignments, nil
}

func (db *DB) SetRemoteFeedFolder(remoteFeedID, folderID int64) error {
	if folderID == 0 {
		if _, err := db.Exec(`UPDATE remote_feed_prefs SET folder_id = NULL WHERE remote_feed_id = ?`, remoteFeedID); err != nil {
			return err
		}
		return db.pruneRemoteFeedPref(remoteFeedID)
	}

	_, err := db.Exec(`
		INSERT INTO remote_feed_prefs (remote_feed_id, folder_id)
		VALUES (?, ?)
		ON CONFLICT(remote_feed_id) DO UPDATE SET folder_id = excluded.folder_id
	`, remoteFeedID, folderID)
	return err
}

func (db *DB) SetRemoteFeedTitle(remoteFeedID int64, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		if _, err := db.Exec(`UPDATE remote_feed_prefs SET title = '' WHERE remote_feed_id = ?`, remoteFeedID); err != nil {
			return err
		}
		return db.pruneRemoteFeedPref(remoteFeedID)
	}

	_, err := db.Exec(`
		INSERT INTO remote_feed_prefs (remote_feed_id, title)
		VALUES (?, ?)
		ON CONFLICT(remote_feed_id) DO UPDATE SET title = excluded.title
	`, remoteFeedID, title)
	return err
}

func (db *DB) pruneRemoteFeedPref(remoteFeedID int64) error {
	_, err := db.Exec(`
		DELETE FROM remote_feed_prefs
		WHERE remote_feed_id = ? AND folder_id IS NULL AND title = ''
	`, remoteFeedID)
	return err
}
