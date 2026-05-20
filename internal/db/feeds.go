package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Feed struct {
	ID          int64
	URL         string
	Title       string
	Description string
	FaviconURL  string
	LastFetched time.Time
	UnreadCount int64
	FolderID    int64
}

type Folder struct {
	ID       int64
	Name     string
	Position int
	Color    string
}

func (db *DB) ListFeeds() ([]Feed, error) {
	rows, err := db.Query(`
		SELECT f.id, f.url, COALESCE(NULLIF(f.custom_title, ''), f.title), f.description, f.favicon_url, f.last_fetched, f.folder_id,
		       COUNT(CASE WHEN a.read = 0 THEN 1 END) AS unread_count
		FROM feeds f
		LEFT JOIN articles a ON a.feed_id = f.id
		GROUP BY f.id
		ORDER BY COALESCE(NULLIF(f.custom_title, ''), f.title) COLLATE NOCASE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		var f Feed
		var lastFetched int64
		var folderID sql.NullInt64
		if err := rows.Scan(&f.ID, &f.URL, &f.Title, &f.Description, &f.FaviconURL, &lastFetched, &folderID, &f.UnreadCount); err != nil {
			return nil, err
		}
		f.LastFetched = time.Unix(lastFetched, 0)
		if folderID.Valid {
			f.FolderID = folderID.Int64
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

func (db *DB) GetFeed(id int64) (Feed, error) {
	var f Feed
	var lastFetched int64
	var folderID sql.NullInt64
	err := db.QueryRow(
		`SELECT id, url, COALESCE(NULLIF(custom_title, ''), title), description, favicon_url, last_fetched, folder_id FROM feeds WHERE id = ?`, id,
	).Scan(&f.ID, &f.URL, &f.Title, &f.Description, &f.FaviconURL, &lastFetched, &folderID)
	if err != nil {
		return Feed{}, err
	}
	f.LastFetched = time.Unix(lastFetched, 0)
	if folderID.Valid {
		f.FolderID = folderID.Int64
	}
	return f, nil
}

func (db *DB) AddFeed(url, title, description string) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO feeds (url, title, description) VALUES (?, ?, ?)`,
		url, title, description,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
			return 0, fmt.Errorf("feed already exists: %s", url)
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) UpdateFeed(id int64, customTitle, url string) error {
	_, err := db.Exec(`UPDATE feeds SET custom_title = ?, url = ? WHERE id = ?`, customTitle, url, id)
	return err
}

func (db *DB) UpdateFeedURL(id int64, url string) error {
	_, err := db.Exec(`UPDATE feeds SET url = ? WHERE id = ?`, url, id)
	return err
}

func (db *DB) UpdateFeedMeta(id int64, title, description, faviconURL string, lastFetched time.Time) error {
	_, err := db.Exec(
		`UPDATE feeds SET title = ?, description = ?, favicon_url = ?, last_fetched = ? WHERE id = ?`,
		title, description, faviconURL, lastFetched.Unix(), id,
	)
	return err
}

func (db *DB) TouchFeedFetched(id int64, lastFetched time.Time) error {
	_, err := db.Exec(`UPDATE feeds SET last_fetched = ? WHERE id = ?`, lastFetched.Unix(), id)
	return err
}

func (db *DB) DeleteFeed(id int64) error {
	res, err := db.Exec(`DELETE FROM feeds WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("feed %d not found", id)
	}
	return nil
}

func (db *DB) ListFolders() ([]Folder, error) {
	rows, err := db.Query(`
		SELECT id, name, position, color
		FROM folders
		ORDER BY position, name COLLATE NOCASE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.Position, &f.Color); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

func normalizeFolderName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), " "))
}

func (db *DB) AddFolder(name, color string) (int64, error) {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	if name == "" {
		return 0, fmt.Errorf("folder name is required")
	}
	color = strings.TrimSpace(color)

	folders, err := db.ListFolders()
	if err != nil {
		return 0, err
	}
	normalized := normalizeFolderName(name)
	for _, folder := range folders {
		if normalizeFolderName(folder.Name) == normalized {
			if color != "" && folder.Color != color {
				if err := db.SetFolderColor(folder.ID, color); err != nil {
					return 0, err
				}
			}
			return folder.ID, nil
		}
	}

	var position int
	if err := db.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM folders`).Scan(&position); err != nil {
		return 0, err
	}

	res, err := db.Exec(`INSERT INTO folders (name, position, color) VALUES (?, ?, ?)`, name, position, color)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) RenameFolder(id int64, name string) error {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	if name == "" {
		return fmt.Errorf("folder name is required")
	}

	folders, err := db.ListFolders()
	if err != nil {
		return err
	}
	normalized := normalizeFolderName(name)
	for _, folder := range folders {
		if folder.ID != id && normalizeFolderName(folder.Name) == normalized {
			return fmt.Errorf("folder %q already exists", name)
		}
	}

	_, err = db.Exec(`UPDATE folders SET name = ? WHERE id = ?`, name, id)
	return err
}

func (db *DB) DeleteFolder(id int64) error {
	res, err := db.Exec(`DELETE FROM folders WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("folder %d not found", id)
	}
	return nil
}

func (db *DB) SetFolderColor(id int64, color string) error {
	_, err := db.Exec(`UPDATE folders SET color = ? WHERE id = ?`, strings.TrimSpace(color), id)
	return err
}

func (db *DB) SetFeedFolder(feedID, folderID int64) error {
	if folderID == 0 {
		_, err := db.Exec(`UPDATE feeds SET folder_id = NULL WHERE id = ?`, feedID)
		return err
	}
	_, err := db.Exec(`UPDATE feeds SET folder_id = ? WHERE id = ?`, folderID, feedID)
	return err
}

func (db *DB) ReorderFolder(id int64, position int) error {
	_, err := db.Exec(`UPDATE folders SET position = ? WHERE id = ?`, position, id)
	return err
}
