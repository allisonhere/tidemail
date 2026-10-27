package ui

import (
	"fmt"
	"maps"
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/charmbracelet/lipgloss"
)

type messageRenderContext struct {
	msg         db.Message
	width       int
	theme       Theme
	plainUI     bool
	filterLinks bool
	bodyStyle   lipgloss.Style
	images      *imageRenderContext
}

type messageRenderResult struct {
	body       string
	links      []string
	linkLabels map[string]string
	images     []renderImage
	ok         bool
}

type messageRenderer func(messageRenderContext) messageRenderResult

func (m Model) renderMessageForDisplay(msg db.Message, width int) messageRenderResult {
	msg = m.pgpOverlay(msg)
	key := bodyCacheKey{
		messageID:   msg.ID,
		width:       width,
		theme:       m.styles.Theme.Name,
		plainUI:     m.styles.PlainUI,
		filterLinks: m.cfg.Display.FilterLinks,
		fingerprint: len(msg.BodyHTML) + len(msg.BodyText),
		imageKey:    m.imageRenderKey(msg.ID),
		pgpPlugin:   m.hasPGPPlugin(),
	}
	// A message with no ID has no stable identity to cache under — a preview
	// of something not yet stored — so it always renders fresh.
	cacheable := msg.ID != 0
	if cacheable {
		if res, ok := m.bodyCache.get(key); ok {
			// Hand back a copy of the links: callers merge and filter them,
			// and a shared backing array would let one caller's edits reach
			// the cached result.
			res.links = append([]string(nil), res.links...)
			res.linkLabels = maps.Clone(res.linkLabels)
			res.images = append([]renderImage(nil), res.images...)
			return res
		}
	}

	ctx := messageRenderContext{
		msg:         msg,
		width:       width,
		theme:       m.styles.Theme,
		plainUI:     m.styles.PlainUI,
		filterLinks: m.cfg.Display.FilterLinks,
		bodyStyle:   m.styles.ContentBody.Width(width),
		images:      m.imageContext(msg, width),
	}
	result := renderMessageWithContext(ctx)
	if m.pgpEncryptedUnread(msg) {
		result.body = indentBlock(ctx.bodyStyle.Render(m.pgpCallout()), 1)
	}
	if cacheable {
		stored := result
		stored.links = append([]string(nil), result.links...)
		stored.linkLabels = maps.Clone(result.linkLabels)
		stored.images = append([]renderImage(nil), result.images...)
		m.bodyCache.put(key, stored)
	}
	return result
}

// imageRenderKey folds every image-state input that changes a body's bytes
// into the body cache key: whether raster images are enabled, whether remote
// images were allowed for this message, and the store's generation (bumped
// when a remote fetch lands).
func (m Model) imageRenderKey(messageID int64) string {
	if m.images == nil {
		return "off"
	}
	if !m.images.graphics() {
		return "none"
	}
	return fmt.Sprintf("%t:%d", m.images.isAllowed(messageID), m.images.gen())
}

func renderMessageWithContext(ctx messageRenderContext) messageRenderResult {
	for _, renderer := range []messageRenderer{
		renderRedditMessage,
		renderHTMLMessage,
		renderPlainTextMessage,
	} {
		if result := renderer(ctx); result.ok {
			return result
		}
	}
	return messageRenderResult{
		body: indentBlock(ctx.bodyStyle.Render("No message body."), 1),
		ok:   true,
	}
}

func renderRedditMessage(ctx messageRenderContext) messageRenderResult {
	if ctx.msg.BodyHTML == "" || !isRedditMessage(ctx.msg) {
		return messageRenderResult{}
	}
	rendered, ok := renderRedditDigestHTML(ctx.msg.BodyHTML, ctx.width, ctx.theme, ctx.plainUI)
	if !ok {
		return messageRenderResult{}
	}
	links := extractRedditPostLinks(ctx.msg.BodyHTML)
	return messageRenderResult{
		body:       indentBlock(ctx.bodyStyle.Render(rendered), 1),
		links:      links,
		linkLabels: extractLinkLabels(ctx.msg.BodyHTML, links),
		ok:         true,
	}
}

func renderHTMLMessage(ctx messageRenderContext) messageRenderResult {
	if ctx.msg.BodyHTML == "" {
		return messageRenderResult{}
	}
	body, images := renderHTMLBodyWithImages(ctx.msg.BodyHTML, ctx.width, ctx.theme, ctx.plainUI, ctx.filterLinks, ctx.images)
	if strings.TrimSpace(body) == "" {
		return messageRenderResult{}
	}
	links, labels := extractHTMLLinks(ctx.msg.BodyHTML, "")
	return messageRenderResult{
		body:       body,
		links:      links,
		linkLabels: labels,
		images:     images,
		ok:         true,
	}
}

func renderPlainTextMessage(ctx messageRenderContext) messageRenderResult {
	content := ctx.msg.BodyText
	if content == "" {
		return messageRenderResult{}
	}
	if ctx.filterLinks {
		content = filterLinksFromContent(content)
	}
	return messageRenderResult{
		body:  indentBlock(ctx.bodyStyle.Render(formatArticleBody(content, ctx.width, ctx.theme, ctx.plainUI)), 1),
		links: extractActionableLinks(ctx.msg.BodyText, ""),
		ok:    true,
	}
}
