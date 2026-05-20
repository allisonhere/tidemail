package ui

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/greader"
)

func (m Model) hasGReaderConfig() bool {
	return strings.TrimSpace(m.cfg.Source.GReaderURL) != "" &&
		strings.TrimSpace(m.cfg.Source.GReaderLogin) != "" &&
		strings.TrimSpace(m.cfg.Source.GReaderPassword) != ""
}

func (m Model) isRemoteFeed(feedID int64) bool {
	_, ok := m.greaderStreams[feedID]
	return ok
}

func (m Model) articleIsRemote(articleID int64) bool {
	for i := range m.articles {
		if m.articles[i].ID == articleID {
			return m.isRemoteFeed(m.articles[i].FeedID)
		}
	}
	for i := range m.filteredArticles {
		if m.filteredArticles[i].ID == articleID {
			return m.isRemoteFeed(m.filteredArticles[i].FeedID)
		}
	}
	if articleID < 0 {
		return true
	}
	return false
}

func (m *Model) resetSourceClient() {
	m.greaderStreams = map[int64]string{}
	if !m.hasGReaderConfig() {
		m.greaderClient = nil
		return
	}
	m.greaderClient = greader.New(m.cfg.Source.GReaderURL, m.cfg.Source.GReaderLogin, m.cfg.Source.GReaderPassword)
}

func (m Model) loadGReaderFeeds(ctx context.Context) ([]db.Feed, map[int64]string, error) {
	if m.greaderClient == nil {
		return nil, nil, fmt.Errorf("google reader source not configured")
	}

	subscriptions, err := m.greaderClient.ListSubscriptions(ctx)
	if err != nil {
		return nil, nil, err
	}
	counts, err := m.greaderClient.UnreadCounts(ctx)
	if err != nil {
		return nil, nil, err
	}
	remotePrefs := map[int64]db.RemoteFeedPref{}
	if m.db != nil {
		remotePrefs, err = m.db.ListRemoteFeedPrefs()
		if err != nil {
			return nil, nil, err
		}
	}

	now := time.Now()
	streams := make(map[int64]string, len(subscriptions))
	feeds := make([]db.Feed, 0, len(subscriptions))
	for _, sub := range subscriptions {
		if strings.TrimSpace(sub.ID) == "" {
			continue
		}

		feedID := remoteStableID("feed", sub.ID)
		streams[feedID] = sub.ID

		pref := remotePrefs[feedID]
		title := greader.UnescapeAPIString(sub.Title)
		if title == "" {
			title = sub.ID
		}
		url := strings.TrimSpace(sub.HTMLURL)
		if url == "" {
			url = strings.TrimSpace(sub.FeedURL)
		}

		feeds = append(feeds, db.Feed{
			ID:          feedID,
			URL:         url,
			Title:       title,
			Description: greader.UnescapeAPIString(sub.Category),
			LastFetched: now,
			UnreadCount: counts[sub.ID],
			FolderID:    pref.FolderID,
		})
	}

	sort.Slice(feeds, func(i, j int) bool {
		return strings.ToLower(feeds[i].Title) < strings.ToLower(feeds[j].Title)
	})
	return feeds, streams, nil
}

func (m Model) loadGReaderArticles(ctx context.Context, feedID int64) ([]db.Article, error) {
	if m.greaderClient == nil {
		return nil, fmt.Errorf("google reader source not configured")
	}

	streamID, ok := m.greaderStreams[feedID]
	if !ok || strings.TrimSpace(streamID) == "" {
		return nil, fmt.Errorf("unknown remote feed")
	}

	entries, err := m.greaderClient.StreamContents(ctx, streamID, 100)
	if err != nil {
		return nil, err
	}

	articles := make([]db.Article, 0, len(entries))
	for _, entry := range entries {
		title := greader.UnescapeAPIString(entry.Title)
		if title == "" {
			title = "(untitled)"
		}
		articles = append(articles, db.Article{
			ID:          remoteStableID("article", entry.ID),
			FeedID:      feedID,
			GUID:        entry.ID,
			Title:       title,
			Link:        strings.TrimSpace(entry.Link),
			Content:     m.normalizeRemoteArticleContent(entry.ContentHTML, entry.Link),
			PublishedAt: entry.PublishedAt,
			Read:        entry.Read,
		})
	}

	sort.Slice(articles, func(i, j int) bool {
		return articles[i].PublishedAt.After(articles[j].PublishedAt)
	})
	return articles, nil
}

func (m Model) normalizeRemoteArticleContent(contentHTML, link string) string {
	contentHTML = strings.TrimSpace(contentHTML)
	if contentHTML == "" {
		if link != "" {
			return "No content from server. Press o to open in browser.\n\n" + link
		}
		return ""
	}
	if m.mdConverter != nil {
		if converted, err := m.mdConverter.ConvertString(contentHTML); err == nil {
			converted = strings.TrimSpace(converted)
			if converted != "" {
				return converted
			}
		}
	}
	return contentHTML
}

func remoteStableID(kind, value string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(value))
	id := int64(h.Sum64() & 0x7fffffffffffffff)
	if id == 0 {
		id = 1
	}
	return -id
}
