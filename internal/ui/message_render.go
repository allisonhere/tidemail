package ui

import (
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
}

type messageRenderResult struct {
	body  string
	links []string
	ok    bool
}

type messageRenderer func(messageRenderContext) messageRenderResult

func (m Model) renderMessageForDisplay(msg db.Message, width int) messageRenderResult {
	ctx := messageRenderContext{
		msg:         msg,
		width:       width,
		theme:       m.styles.Theme,
		plainUI:     m.styles.PlainUI,
		filterLinks: m.cfg.Display.FilterLinks,
		bodyStyle:   m.styles.ContentBody.Width(width),
	}
	result := renderMessageWithContext(ctx)
	if len(result.links) == 0 {
		result.links = collectMessageLinks(msg)
	}
	return result
}

func renderMessageWithContext(ctx messageRenderContext) messageRenderResult {
	for _, renderer := range []messageRenderer{
		renderRedditMessage,
		renderNoisyHTMLArticleMessage,
		renderHTMLMessage,
		renderPlainTextMessage,
	} {
		if result := renderer(ctx); result.ok {
			return result
		}
	}
	return messageRenderResult{
		body:  indentBlock(ctx.bodyStyle.Render("No message body."), 1),
		links: collectMessageLinks(ctx.msg),
		ok:    true,
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
	return messageRenderResult{
		body:  indentBlock(ctx.bodyStyle.Render(rendered), 1),
		links: mergeActionableLinks(extractActionableLinks(ctx.msg.BodyText, ""), extractRedditPostLinks(ctx.msg.BodyHTML)),
		ok:    true,
	}
}

func renderHTMLMessage(ctx messageRenderContext) messageRenderResult {
	if ctx.msg.BodyHTML == "" {
		return messageRenderResult{}
	}
	body := renderHTMLBodyOpts(ctx.msg.BodyHTML, ctx.width, ctx.theme, ctx.plainUI, ctx.filterLinks)
	if strings.TrimSpace(body) == "" {
		return messageRenderResult{}
	}
	return messageRenderResult{
		body:  body,
		links: collectMessageLinks(ctx.msg),
		ok:    true,
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
		links: collectMessageLinks(ctx.msg),
		ok:    true,
	}
}

func collectMessageLinks(msg db.Message) []string {
	links := extractActionableLinks(msg.BodyText, "")
	if msg.BodyHTML != "" {
		links = mergeActionableLinks(links, extractActionableLinksFromHTML(msg.BodyHTML, ""))
	}
	return links
}
