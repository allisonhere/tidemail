package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestOpenMigratesLegacyConfigDB(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))

	legacyDir := filepath.Join(tmp, ".config", "rss")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}

	legacy, err := openSQLite(filepath.Join(legacyDir, "rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.init(); err != nil {
		t.Fatal(err)
	}

	feedID, err := legacy.AddFeed("https://example.com/feed.xml", "Example Feed", "desc")
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.UpsertArticle(Article{
		FeedID:      feedID,
		GUID:        "article-1",
		Title:       "Hello",
		Link:        "https://example.com/hello",
		Content:     "content",
		PublishedAt: unixTime(1710000000),
	}); err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	feeds, err := db.ListFeeds()
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 {
		t.Fatalf("expected 1 migrated feed, got %d", len(feeds))
	}
	if feeds[0].Title != "Example Feed" {
		t.Fatalf("expected migrated title %q, got %q", "Example Feed", feeds[0].Title)
	}

	articles, err := db.ListArticles(feeds[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected 1 migrated article, got %d", len(articles))
	}
}

func TestDBFoldersCRUDAndFeedAssignment(t *testing.T) {
	tmp := t.TempDir()
	db, err := openSQLite(filepath.Join(tmp, "rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.init(); err != nil {
		t.Fatal(err)
	}

	folderID, err := db.AddFolder("Tech", "#7aa2f7")
	if err != nil {
		t.Fatal(err)
	}
	duplicateID, err := db.AddFolder(" tech ", "")
	if err != nil {
		t.Fatal(err)
	}
	if duplicateID != folderID {
		t.Fatalf("expected duplicate folder create to reuse %d, got %d", folderID, duplicateID)
	}

	feedID, err := db.AddFeed("https://example.com/feed.xml", "Example Feed", "desc")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetFeedFolder(feedID, folderID); err != nil {
		t.Fatal(err)
	}

	feed, err := db.GetFeed(feedID)
	if err != nil {
		t.Fatal(err)
	}
	if feed.FolderID != folderID {
		t.Fatalf("expected feed folder %d, got %d", folderID, feed.FolderID)
	}
	folders, err := db.ListFolders()
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].Color != "#7aa2f7" {
		t.Fatalf("expected stored folder color, got %+v", folders)
	}

	if err := db.SetFolderColor(folderID, "#f7768e"); err != nil {
		t.Fatal(err)
	}
	folders, err = db.ListFolders()
	if err != nil {
		t.Fatal(err)
	}
	if folders[0].Color != "#f7768e" {
		t.Fatalf("expected updated folder color, got %q", folders[0].Color)
	}

	if err := db.DeleteFolder(folderID); err != nil {
		t.Fatal(err)
	}
	feed, err = db.GetFeed(feedID)
	if err != nil {
		t.Fatal(err)
	}
	if feed.FolderID != 0 {
		t.Fatalf("expected deleted folder to clear feed assignment, got %d", feed.FolderID)
	}
}

func TestListArticlesReturnsUnreadOnly(t *testing.T) {
	tmp := t.TempDir()
	db, err := openSQLite(filepath.Join(tmp, "rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.init(); err != nil {
		t.Fatal(err)
	}

	feedID, err := db.AddFeed("https://example.com/feed.xml", "Example Feed", "desc")
	if err != nil {
		t.Fatal(err)
	}

	seed := []Article{
		{
			FeedID:      feedID,
			GUID:        "old-unread",
			Title:       "Old Unread",
			Link:        "https://example.com/old-unread",
			Content:     "old",
			PublishedAt: unixTime(1710000000),
		},
		{
			FeedID:      feedID,
			GUID:        "new-read",
			Title:       "New Read",
			Link:        "https://example.com/new-read",
			Content:     "read",
			PublishedAt: unixTime(1710000200),
		},
		{
			FeedID:      feedID,
			GUID:        "new-unread",
			Title:       "New Unread",
			Link:        "https://example.com/new-unread",
			Content:     "new",
			PublishedAt: unixTime(1710000100),
		},
	}
	for _, article := range seed {
		if err := db.UpsertArticle(article); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.Exec(`UPDATE articles SET read = 1 WHERE feed_id = ? AND guid = ?`, feedID, "new-read"); err != nil {
		t.Fatal(err)
	}

	articles, err := db.ListArticles(feedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 3 {
		t.Fatalf("expected 3 articles, got %d", len(articles))
	}
	if articles[0].GUID != "new-read" {
		t.Fatalf("expected newest article first, got %q", articles[0].GUID)
	}
	if !articles[0].Read {
		t.Fatal("expected newest article to remain marked read")
	}
	if articles[1].GUID != "new-unread" {
		t.Fatalf("expected second-newest article second, got %q", articles[1].GUID)
	}
	if articles[2].GUID != "old-unread" {
		t.Fatalf("expected oldest article last, got %q", articles[2].GUID)
	}
}

func TestOpenMigratesFolderSchemaToVersion5(t *testing.T) {
	tmp := t.TempDir()
	db, err := openSQLite(filepath.Join(tmp, "rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE feeds (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			url          TEXT    NOT NULL UNIQUE,
			title        TEXT    NOT NULL DEFAULT '',
			description  TEXT    NOT NULL DEFAULT '',
			favicon_url  TEXT    NOT NULL DEFAULT '',
			last_fetched INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE articles (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			feed_id      INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
			guid         TEXT    NOT NULL,
			title        TEXT    NOT NULL DEFAULT '',
			link         TEXT    NOT NULL DEFAULT '',
			content      TEXT    NOT NULL DEFAULT '',
			published_at INTEGER NOT NULL DEFAULT 0,
			read         INTEGER NOT NULL DEFAULT 0,
			UNIQUE(feed_id, guid)
		);
		PRAGMA user_version = 1;
	`); err != nil {
		t.Fatal(err)
	}

	if err := db.migrateSchema(); err != nil {
		t.Fatal(err)
	}

	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 6 {
		t.Fatalf("expected schema version 6, got %d", version)
	}

	rows, err := db.Query(`PRAGMA table_info(feeds)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	foundFolderID := false
	foundCustomTitle := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "folder_id" {
			foundFolderID = true
		}
		if name == "custom_title" {
			foundCustomTitle = true
		}
	}
	if !foundFolderID {
		t.Fatal("expected feeds.folder_id column after migration")
	}
	if !foundCustomTitle {
		t.Fatal("expected feeds.custom_title column after migration")
	}
	rows.Close()

	rows, err = db.Query(`PRAGMA table_info(folders)`)
	if err != nil {
		t.Fatal(err)
	}

	foundColor := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "color" {
			foundColor = true
			break
		}
	}
	if !foundColor {
		t.Fatal("expected folders.color column after migration")
	}
	rows.Close()

	rows, err = db.Query(`PRAGMA table_info(remote_feed_prefs)`)
	if err != nil {
		t.Fatal(err)
	}

	foundRemoteFeedID := false
	foundRemoteFolderID := false
	foundRemoteTitle := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "remote_feed_id" {
			foundRemoteFeedID = true
		}
		if name == "folder_id" {
			foundRemoteFolderID = true
		}
		if name == "title" {
			foundRemoteTitle = true
		}
	}
	if !foundRemoteFeedID || !foundRemoteFolderID || !foundRemoteTitle {
		t.Fatalf("expected remote_feed_prefs columns after migration, found remote_feed_id=%v folder_id=%v title=%v", foundRemoteFeedID, foundRemoteFolderID, foundRemoteTitle)
	}
	rows.Close()
}

func TestDBRemoteFeedFolderAssignment(t *testing.T) {
	tmp := t.TempDir()
	db, err := openSQLite(filepath.Join(tmp, "rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.init(); err != nil {
		t.Fatal(err)
	}

	folderID, err := db.AddFolder("Remote", "#7aa2f7")
	if err != nil {
		t.Fatal(err)
	}

	if err := db.SetRemoteFeedFolder(-42, folderID); err != nil {
		t.Fatal(err)
	}

	assignments, err := db.ListRemoteFeedFolders()
	if err != nil {
		t.Fatal(err)
	}
	if assignments[-42] != folderID {
		t.Fatalf("expected remote feed -42 to map to folder %d, got %d", folderID, assignments[-42])
	}

	if err := db.DeleteFolder(folderID); err != nil {
		t.Fatal(err)
	}

	assignments, err = db.ListRemoteFeedFolders()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := assignments[-42]; ok {
		t.Fatalf("expected deleted folder to clear remote feed assignment, got %+v", assignments)
	}
}

func TestDBRemoteFeedTitleOverridePersistsWithoutFolder(t *testing.T) {
	tmp := t.TempDir()
	db, err := openSQLite(filepath.Join(tmp, "rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.init(); err != nil {
		t.Fatal(err)
	}

	folderID, err := db.AddFolder("Remote", "#7aa2f7")
	if err != nil {
		t.Fatal(err)
	}

	if err := db.SetRemoteFeedTitle(-42, "Custom Remote"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRemoteFeedFolder(-42, folderID); err != nil {
		t.Fatal(err)
	}

	prefs, err := db.ListRemoteFeedPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if got := prefs[-42].Title; got != "Custom Remote" {
		t.Fatalf("expected remote title override to be stored, got %q", got)
	}
	if got := prefs[-42].FolderID; got != folderID {
		t.Fatalf("expected remote folder %d, got %d", folderID, got)
	}

	if err := db.SetRemoteFeedFolder(-42, 0); err != nil {
		t.Fatal(err)
	}

	prefs, err = db.ListRemoteFeedPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if got := prefs[-42].Title; got != "Custom Remote" {
		t.Fatalf("expected title override to survive clearing folder, got %q", got)
	}
	if got := prefs[-42].FolderID; got != 0 {
		t.Fatalf("expected cleared folder id 0, got %d", got)
	}

	if err := db.SetRemoteFeedTitle(-42, ""); err != nil {
		t.Fatal(err)
	}

	prefs, err = db.ListRemoteFeedPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := prefs[-42]; ok {
		t.Fatalf("expected empty remote pref row to be pruned, got %+v", prefs[-42])
	}
}

func openSQLite(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	return &DB{conn}, nil
}

func unixTime(ts int64) time.Time {
	return time.Unix(ts, 0)
}
