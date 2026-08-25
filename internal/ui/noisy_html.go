package ui

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func renderNoisyHTMLArticleMessage(ctx messageRenderContext) messageRenderResult {
	if ctx.msg.BodyHTML == "" || !looksLikeNoisyTemplateHTML(ctx.msg.BodyHTML) {
		return messageRenderResult{}
	}
	blocks := extractNoisyHTMLTextBlocks(ctx.msg.BodyHTML)
	if len(blocks) < 2 {
		return messageRenderResult{}
	}
	bodyText := strings.Join(blocks, "\n\n")
	if len([]rune(bodyText)) < 80 {
		return messageRenderResult{}
	}
	if ctx.filterLinks {
		bodyText = filterLinksFromContent(bodyText)
	}
	return messageRenderResult{
		body:  indentBlock(ctx.bodyStyle.Render(formatArticleBody(bodyText, ctx.width, ctx.theme, ctx.plainUI)), 1),
		links: collectMessageLinks(ctx.msg),
		ok:    true,
	}
}

func looksLikeNoisyTemplateHTML(raw string) bool {
	lower := strings.ToLower(raw)
	return len(raw) >= 5000 ||
		strings.Count(lower, "<table") >= 4 ||
		strings.Contains(lower, "preheader") ||
		strings.Contains(lower, "mso-hide") ||
		strings.Contains(lower, "tracking")
}

func extractNoisyHTMLTextBlocks(raw string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return nil
	}
	doc.Find("script,style,noscript,template,head,meta,link,input,textarea,select").Remove()

	var blocks []string
	seen := map[string]struct{}{}
	doc.Find("h1,h2,h3,h4,p,li,a").Each(func(_ int, selec *goquery.Selection) {
		if len(blocks) >= 24 || noisyHTMLSelectionHidden(selec) {
			return
		}
		if selec.Is("a") && selec.ParentsFiltered("p,li,h1,h2,h3,h4").Length() > 0 {
			return
		}
		text := normalizeInlineSpacing(selec.Text())
		if text == "" || !hasMeaningfulRenderedHTML(text) || isNoisyHTMLBoilerplateText(text) {
			return
		}
		if selec.Is("a") && looksLikeButtonLink(selec) {
			text = "[" + text + "]"
		}
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		blocks = append(blocks, text)
	})
	return blocks
}

func noisyHTMLSelectionHidden(selec *goquery.Selection) bool {
	hidden := false
	selec.ParentsFiltered("[hidden],[aria-hidden],[class],[id],[style]").AddSelection(selec).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if _, ok := s.Attr("hidden"); ok {
			hidden = true
			return false
		}
		if strings.EqualFold(strings.TrimSpace(attrFirst(s, "aria-hidden")), "true") {
			hidden = true
			return false
		}
		if hiddenByEmailIdentity(attrFirst(s, "class") + " " + attrFirst(s, "id")) {
			hidden = true
			return false
		}
		style := parseInlineStyle(attrFirst(s, "style"))
		if style["display"] == "none" || style["visibility"] == "hidden" || style["visibility"] == "collapse" || isZeroDimension(style["opacity"]) {
			hidden = true
			return false
		}
		return true
	})
	return hidden
}

func isNoisyHTMLBoilerplateText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	lower = strings.Trim(lower, "[] ")
	switch lower {
	case "view in browser", "open in browser", "unsubscribe", "manage preferences", "privacy policy":
		return true
	}
	return strings.HasPrefix(lower, "this email was sent to ") ||
		strings.HasPrefix(lower, "you are receiving this email ") ||
		strings.Contains(lower, "unsubscribe from this") ||
		strings.Contains(lower, "manage your email preferences")
}
