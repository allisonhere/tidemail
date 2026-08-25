package ui

import (
	"html"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/lipgloss"
)

type redditDigestPost struct {
	key       string
	link      string
	subreddit string
	author    string
	age       string
	title     string
	excerpt   string
	upvotes   string
	comments  string
}

func renderRedditDigestHTML(raw string, width int, th Theme, plainUI bool) (string, bool) {
	if !strings.Contains(strings.ToLower(raw), "reddit") {
		return "", false
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return "", false
	}

	postsByKey := map[string]*redditDigestPost{}
	var order []string
	postFor := func(key, link, subreddit string) *redditDigestPost {
		if post, ok := postsByKey[key]; ok {
			if post.link == "" {
				post.link = link
			}
			if post.subreddit == "" {
				post.subreddit = subreddit
			}
			return post
		}
		post := &redditDigestPost{key: key, link: link, subreddit: subreddit}
		postsByKey[key] = post
		order = append(order, key)
		return post
	}

	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(attrFirst(link, "href"))
		cleaned := cleanDetectedURL(href)
		key, subreddit, ok := redditPostIdentity(cleaned)
		if !ok {
			return
		}
		post := postFor(key, cleaned, subreddit)
		text := redditLinkText(link)
		marker := strings.ToLower(href + " " + cleaned)

		switch {
		case strings.Contains(marker, "utm_content=post_subreddit"):
			if strings.HasPrefix(text, "r/") {
				post.subreddit = text
			}
		case strings.Contains(marker, "utm_content=post_author"):
			post.author = text
		case strings.Contains(marker, "utm_content=post_timestamp"):
			post.age = text
		case strings.Contains(marker, "utm_content=post_title"):
			post.title = text
		case strings.Contains(marker, "utm_content=post_body"):
			post.excerpt = cleanRedditExcerpt(text)
		case strings.Contains(marker, "utm_content=post_karma"):
			post.upvotes = text
		case strings.Contains(marker, "utm_content=post_comments"):
			post.comments = text
		}
	})

	var posts []*redditDigestPost
	for _, key := range order {
		post := postsByKey[key]
		if post.title == "" || post.subreddit == "" {
			continue
		}
		posts = append(posts, post)
	}
	if len(posts) == 0 {
		return "", false
	}

	return renderRedditDigestPosts(posts, width, th, plainUI), true
}

func extractRedditPostLinks(raw string) []string {
	if !strings.Contains(strings.ToLower(raw), "reddit") {
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var links []string
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		cleaned := cleanDetectedURL(attrFirst(link, "href"))
		if cleaned == "" {
			return
		}
		if _, _, ok := redditPostIdentity(cleaned); !ok {
			return
		}
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		links = append(links, cleaned)
	})
	return links
}

func redditLinkText(link *goquery.Selection) string {
	return normalizeInlineSpacing(html.UnescapeString(link.Text()))
}

func cleanRedditExcerpt(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, "Read More")
	text = strings.TrimSuffix(text, "Read more")
	return normalizeInlineSpacing(text)
}

func redditPostIdentity(raw string) (key, subreddit string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	host := strings.ToLower(u.Hostname())
	if host != "reddit.com" && host != "www.reddit.com" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "r" || parts[2] != "comments" {
		return "", "", false
	}
	subreddit = "r/" + parts[1]
	key = parts[1] + "/" + parts[3]
	return key, subreddit, true
}

func renderRedditDigestPosts(posts []*redditDigestPost, width int, th Theme, plainUI bool) string {
	width = max(8, width)
	metaStyle := lipgloss.NewStyle().Background(th.Bg).Foreground(messageMutedColor(th))
	titleStyle := lipgloss.NewStyle().Background(th.Bg).Foreground(messageHeadingColor(th)).Bold(true)
	bodyStyle := lipgloss.NewStyle().Background(th.Bg)
	actionStyle := lipgloss.NewStyle().Background(th.Bg).Foreground(messageLinkColor(th)).Underline(true)

	render := func(style lipgloss.Style, text string) string {
		if plainUI {
			return text
		}
		return style.Render(text)
	}
	renderBlock := func(style lipgloss.Style, text string) string {
		wrapped := wrapWords(text, width)
		lines := strings.Split(wrapped, "\n")
		for i, line := range lines {
			lines[i] = render(style, line)
		}
		return strings.Join(lines, "\n")
	}

	var blocks []string
	for _, post := range posts {
		var lines []string
		metaParts := []string{post.subreddit}
		if post.author != "" {
			metaParts = append(metaParts, post.author)
		}
		if post.age != "" {
			metaParts = append(metaParts, post.age)
		}
		lines = append(lines, renderBlock(metaStyle, strings.Join(metaParts, " - ")))
		lines = append(lines, renderBlock(titleStyle, post.title))
		if post.excerpt != "" {
			lines = append(lines, renderBlock(bodyStyle, post.excerpt))
		}
		var metrics []string
		if post.upvotes != "" {
			metrics = append(metrics, post.upvotes)
		}
		if post.comments != "" {
			metrics = append(metrics, post.comments)
		}
		if len(metrics) > 0 {
			lines = append(lines, renderBlock(metaStyle, strings.Join(metrics, " - ")))
		}
		if post.link != "" {
			label := osc8Link(post.link, "[Read post]", plainUI)
			lines = append(lines, render(actionStyle, label))
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}
