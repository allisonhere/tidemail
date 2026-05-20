package db

import (
	"time"
)

type Article struct {
	ID          int64
	FeedID      int64
	GUID        string
	Title       string
	Link        string
	Content     string
	Summary     string
	PublishedAt time.Time
	Read        bool
}

func (db *DB) ListArticles(feedID int64) ([]Article, error) {
	rows, err := db.Query(`
		SELECT id, feed_id, guid, title, link, content, summary, published_at, read
		FROM articles
		WHERE feed_id = ?
		ORDER BY published_at DESC
		LIMIT 100
	`, feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var articles []Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		articles = append(articles, a)
	}
	return articles, rows.Err()
}

func (db *DB) ListUnreadArticles(feedID int64) ([]Article, error) {
	rows, err := db.Query(`
		SELECT id, feed_id, guid, title, link, content, summary, published_at, read
		FROM articles
		WHERE feed_id = ? AND read = 0
		ORDER BY published_at DESC
		LIMIT 100
	`, feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var articles []Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		articles = append(articles, a)
	}
	return articles, rows.Err()
}

func (db *DB) UpsertArticle(a Article) error {
	_, err := db.Exec(`
		INSERT INTO articles (feed_id, guid, title, link, content, published_at, read)
		VALUES (?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(feed_id, guid) DO UPDATE SET
			title        = excluded.title,
			link         = excluded.link,
			content      = excluded.content,
			published_at = excluded.published_at
	`, a.FeedID, a.GUID, a.Title, a.Link, a.Content, a.PublishedAt.Unix())
	return err
}

func (db *DB) MarkRead(id int64, read bool) error {
	v := 0
	if read {
		v = 1
	}
	_, err := db.Exec(`UPDATE articles SET read = ? WHERE id = ?`, v, id)
	return err
}

func (db *DB) MarkAllRead(feedID int64) error {
	_, err := db.Exec(`UPDATE articles SET read = 1 WHERE feed_id = ?`, feedID)
	return err
}

func (db *DB) UpdateArticleContent(id int64, content string) error {
	_, err := db.Exec(`UPDATE articles SET content = ? WHERE id = ?`, content, id)
	return err
}

func (db *DB) SaveSummary(id int64, summary string) error {
	_, err := db.Exec(`UPDATE articles SET summary = ? WHERE id = ?`, summary, id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanArticle(s scanner) (Article, error) {
	var a Article
	var publishedAt int64
	var read int
	err := s.Scan(&a.ID, &a.FeedID, &a.GUID, &a.Title, &a.Link, &a.Content, &a.Summary, &publishedAt, &read)
	if err != nil {
		return Article{}, err
	}
	a.PublishedAt = time.Unix(publishedAt, 0)
	a.Read = read != 0
	return a, nil
}
