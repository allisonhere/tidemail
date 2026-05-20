package feed

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"
)

func FetchArticleText(articleURL string) (string, error) {
	body, err := Fetch(articleURL)
	if err != nil {
		return "", err
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		return "", fmt.Errorf("read article: %w", err)
	}

	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("parse article html: %w", err)
	}

	root := findContentRoot(doc)
	if root == nil {
		return "", fmt.Errorf("article body not found")
	}

	var blocks []string
	collectArticleBlocks(root, &blocks)
	text := strings.Join(compactBlocks(blocks), "\n\n")
	if text == "" {
		return "", fmt.Errorf("article text not found")
	}
	return text, nil
}

func findContentRoot(n *html.Node) *html.Node {
	if found := findFirstElement(n, "article"); found != nil {
		return found
	}
	if found := findFirstElement(n, "main"); found != nil {
		return found
	}
	return findFirstElement(n, "body")
}

func findFirstElement(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirstElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func collectArticleBlocks(n *html.Node, blocks *[]string) {
	if n == nil {
		return
	}
	if n.Type == html.ElementNode && skipArticleNode(n.Data) {
		return
	}
	if n.Type == html.ElementNode && blockArticleNode(n.Data) {
		if text := normalizeArticleText(extractNodeText(n)); text != "" {
			*blocks = append(*blocks, text)
			return
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectArticleBlocks(c, blocks)
	}
}

func skipArticleNode(tag string) bool {
	switch tag {
	case "script", "style", "noscript", "svg", "nav", "footer", "header", "form":
		return true
	default:
		return false
	}
}

func blockArticleNode(tag string) bool {
	switch tag {
	case "p", "h1", "h2", "h3", "h4", "blockquote", "pre", "li":
		return true
	default:
		return false
	}
}

func extractNodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur == nil {
			return
		}
		if cur.Type == html.TextNode {
			b.WriteString(cur.Data)
			b.WriteByte(' ')
		}
		if cur.Type == html.ElementNode && skipArticleNode(cur.Data) {
			return
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func normalizeArticleText(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func compactBlocks(blocks []string) []string {
	out := make([]string, 0, len(blocks))
	seen := map[string]struct{}{}
	for _, b := range blocks {
		if len(b) < 30 {
			continue
		}
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		out = append(out, b)
	}
	return out
}
