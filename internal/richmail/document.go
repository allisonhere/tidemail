package richmail

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// imageIDLabelRe matches the UUID-shaped alt text that mail clients sometimes
// emit for otherwise unlabelled images; it is never useful to a reader.
var imageIDLabelRe = regexp.MustCompile(`^[a-fA-F0-9]{8}(-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}$`)

// decorativeAltTokens are lowercase substrings that mark an image as chrome
// rather than content. Matched against alt/title/aria-label.
var decorativeAltTokens = []string{
	"avatar",
	"badge",
	"comment icon",
	"icon",
	"logo",
	"profile photo",
	"reaction",
	"share icon",
}

// IsDecorativeImageAlt reports whether an image's text label marks it as UI
// chrome (an icon, avatar, logo, spacer) rather than message content.
func IsDecorativeImageAlt(label string) bool {
	lower := strings.ToLower(strings.TrimSpace(label))
	if lower == "" || lower == "image" {
		return true
	}
	if imageIDLabelRe.MatchString(label) {
		return true
	}
	if strings.HasPrefix(lower, "commentor") {
		return true
	}
	for _, token := range decorativeAltTokens {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

// Image is one <img> element in document order, with everything needed to
// decide whether and how to render it. Index is stable and matches the
// data-tidemail-image attribute stamped on the element during extraction.
type Image struct {
	Index       int
	Source      ImageSource
	Alt         string
	Title       string
	Link        string
	HintWidth   int
	HintHeight  int
	MaxWidth    int
	Align       Align
	Decorative  bool
	Tracking    bool
	InFormatted bool
}

// Label returns the best human label for the image, preferring alt text and
// falling back to title. Used for both the text placeholder and accessibility.
func (im Image) Label() string {
	if strings.TrimSpace(im.Alt) != "" {
		return strings.TrimSpace(im.Alt)
	}
	return strings.TrimSpace(im.Title)
}

// Manifest is the ordered set of images found in a normalized HTML document.
type Manifest struct {
	Images  []Image
	byIndex map[int]*Image
}

// Lookup returns the image with the given data-tidemail-image index.
func (m *Manifest) Lookup(index int) (Image, bool) {
	if m == nil {
		return Image{}, false
	}
	im, ok := m.byIndex[index]
	if !ok {
		return Image{}, false
	}
	return *im, true
}

// HasRenderableImages reports whether the document contains at least one image
// that could plausibly be shown (not a tracking pixel or decorative chrome).
func (m *Manifest) HasRenderableImages() bool {
	if m == nil {
		return false
	}
	for i := range m.Images {
		if m.Images[i].renderableCandidate() {
			return true
		}
	}
	return false
}

// HasMeaningfulImageContent reports whether the document's images constitute
// content on their own — the basis for not treating an image-heavy email as an
// empty HTML body.
func (m *Manifest) HasMeaningfulImageContent() bool {
	if m == nil {
		return false
	}
	for i := range m.Images {
		im := &m.Images[i]
		if im.Tracking {
			continue
		}
		if im.Decorative {
			continue
		}
		if im.renderableCandidate() {
			return true
		}
	}
	return false
}

// renderableCandidate is a cheap, pre-decode screen. The renderer still checks
// decoded dimensions (an alt-less hero must render; a tiny icon must not).
func (im Image) renderableCandidate() bool {
	if im.Tracking {
		return false
	}
	switch im.Source.Kind {
	case SourceCID, SourceData, SourceRemote:
		return true
	default:
		return false
	}
}

// ExtractImages walks a normalized document in order, stamps each image with a
// stable index attribute, and returns the manifest. It never mutates the DOM
// beyond that attribute. Passing a nil document returns an empty manifest.
//
// The stamped attribute is what imagePlaceholderRule reads, so the markdown
// pipeline and the manifest agree on which image is which without having to
// correlate by source or position after the fact.
func ExtractImages(doc *goquery.Document) *Manifest {
	m := &Manifest{byIndex: map[int]*Image{}}
	if doc == nil {
		return m
	}
	index := 0
	doc.Find("img").Each(func(_ int, sel *goquery.Selection) {
		im := extractImage(sel, index)
		_ = sel.SetAttr("data-tidemail-image", strconv.Itoa(index))
		m.Images = append(m.Images, im)
		index++
	})
	// Build the index lookup only after every append, so the stored pointers
	// reference the final backing array rather than a reallocated one.
	for i := range m.Images {
		m.byIndex[i] = &m.Images[i]
	}
	return m
}

func extractImage(sel *goquery.Selection, index int) Image {
	alt := strings.TrimSpace(stripControl(attr(sel, "alt")))
	title := strings.TrimSpace(stripControl(attr(sel, "title")))
	label := attr(sel, "alt", "title", "aria-label")
	im := Image{
		Index:       index,
		Source:      ParseSource(attr(sel, "src", "data-src")),
		Alt:         alt,
		Title:       title,
		InFormatted: hasAncestor(sel, "pre", "code"),
	}

	if href := enclosingHref(sel); href != "" {
		im.Link = href
	}

	im.HintWidth = lengthHint(attr(sel, "width"))
	im.HintHeight = lengthHint(attr(sel, "height"))
	style := parseStyle(attr(sel, "style"))
	if v, ok := lengthHintOK(style["width"]); ok {
		im.HintWidth = v
	}
	if v, ok := lengthHintOK(style["height"]); ok {
		im.HintHeight = v
	}
	if v, ok := lengthHintOK(style["max-width"]); ok {
		im.MaxWidth = v
	}
	im.Align = imageAlign(sel)
	im.Decorative = IsDecorativeImageAlt(strings.TrimSpace(stripControl(label)))
	im.Tracking = im.HintWidth == 1 && im.HintHeight == 1 ||
		(im.HintWidth > 0 && im.HintWidth <= 1) ||
		(im.HintHeight > 0 && im.HintHeight <= 1)
	return im
}

// enclosingHref returns the href of the nearest anchor ancestor, sanitized to
// the two schemes TideMail will open.
func enclosingHref(sel *goquery.Selection) string {
	var href string
	sel.ParentsFiltered("a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href = strings.TrimSpace(stripControl(attr(a, "href")))
		return false
	})
	if strings.HasPrefix(strings.ToLower(href), "http://") || strings.HasPrefix(strings.ToLower(href), "https://") {
		return href
	}
	return ""
}

// imageAlign guesses the horizontal alignment of an image from its own style
// and a few levels of ancestors. This is a deliberately small heuristic: the
// common newsletter patterns, not full CSS.
func imageAlign(sel *goquery.Selection) Align {
	node := sel
	for depth := 0; node != nil && depth < 4; depth++ {
		tag := strings.ToLower(goquery.NodeName(node))
		style := parseStyle(attr(node, "style"))
		if tag == "center" || strings.EqualFold(attr(node, "align"), "center") ||
			strings.Contains(style["text-align"], "center") {
			return AlignCenter
		}
		if strings.EqualFold(attr(node, "align"), "right") || strings.Contains(style["text-align"], "right") {
			return AlignRight
		}
		if strings.Contains(strings.ToLower(attr(node, "class")), "center") {
			return AlignCenter
		}
		node = node.Parent()
	}
	return AlignLeft
}

func hasAncestor(sel *goquery.Selection, names ...string) bool {
	for _, name := range names {
		if sel.ParentsFiltered(name).Length() > 0 {
			return true
		}
	}
	return false
}

func attr(sel *goquery.Selection, names ...string) string {
	for _, name := range names {
		if v, ok := sel.Attr(name); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseStyle(raw string) map[string]string {
	out := map[string]string{}
	for _, decl := range strings.Split(raw, ";") {
		name, value, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.TrimSpace(strings.TrimSuffix(value, "!important"))
		if name != "" {
			out[name] = value
		}
	}
	return out
}

// lengthHint parses a bare or px-valued length, ignoring percentages and other
// relative units (which carry no useful ceiling for us).
func lengthHint(raw string) int {
	v, _ := lengthHintOK(raw)
	return v
}

func lengthHintOK(raw string) (int, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimSuffix(s, "px")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if strings.ContainsAny(s, "%") {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}
