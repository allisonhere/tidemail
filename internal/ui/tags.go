package ui

// Annotation tags: how TideMail shows the conventional plugin annotation keys
// in message rows. Plugins supply meaning only (needs_reply=true,
// urgency=high, importance=high, category=github); every label, glyph,
// color and layout decision is TideMail's. Nothing a plugin returns is ever
// used as a color or style: tags are built from a fixed set of kinds, and a
// category value is only a short lower-case identifier looked up by name.
//
// One renderer serves every list (mailboxes, Unified Inbox, search, Needs
// You, Waiting on Them, Snoozed), so a tag means the same thing everywhere.

import (
	"regexp"
	"strings"

	"github.com/allisonhere/tidemail/internal/attention"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/charmbracelet/lipgloss"
)

// tagKind is one kind of annotation tag.
type tagKind int

const (
	tagReply tagKind = iota
	tagUrgent
	tagImportant
	tagCategory
)

// annotationTag is one tag on a row: a kind, plus the category name for
// category tags.
type annotationTag struct {
	kind     tagKind
	category string
}

// Semantic color keys. Theme authors and users set colors by these names.
const (
	tagKeyReply     = "reply"
	tagKeyUrgent    = "urgent"
	tagKeyImportant = "important"
	tagKeyCategory  = "category"
)

// knownTagCategories have their own color keys ("category.github"); any
// other category uses the "category" default.
var knownTagCategories = []string{
	"github", "shipping", "security", "calendar", "newsletter", "support",
	"social", "billing", "receipt", "notification", "personal",
}

// tagColorKeys lists every editable key, in the order Settings shows them.
func tagColorKeys() []string {
	keys := []string{tagKeyReply, tagKeyUrgent, tagKeyImportant, tagKeyCategory}
	for _, c := range knownTagCategories {
		keys = append(keys, tagKeyCategory+"."+c)
	}
	return keys
}

// colorKey is the semantic key whose colors this tag uses.
func (t annotationTag) colorKey() string {
	switch t.kind {
	case tagReply:
		return tagKeyReply
	case tagUrgent:
		return tagKeyUrgent
	case tagImportant:
		return tagKeyImportant
	}
	for _, c := range knownTagCategories {
		if t.category == c {
			return tagKeyCategory + "." + c
		}
	}
	return tagKeyCategory
}

// categoryTagPattern limits category tags to short, plain identifiers, so a
// row never carries free-form plugin text.
var categoryTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,11}$`)

// maxAnnotationTags caps tags per row.
const maxAnnotationTags = 3

// annotationTags derives a row's tags from the annotation cache, in priority
// order (reply, urgent, important, category), at most one per kind whichever
// plugin set it. The value lists are shared with Needs You, so a tag and
// Needs You always agree. It never queries the database.
func (m Model) annotationTags(messageIDs ...int64) []annotationTag {
	return tagsFromEffective(m.effectiveClassificationFor(messageIDs...))
}

func tagsFromEffective(att attention.EffectiveClassification) []annotationTag {
	var tags []annotationTag
	if att.NeedsReply {
		tags = append(tags, annotationTag{kind: tagReply})
	}
	if att.Urgent {
		tags = append(tags, annotationTag{kind: tagUrgent})
	}
	if att.Important {
		tags = append(tags, annotationTag{kind: tagImportant})
	}
	if categoryTagPattern.MatchString(att.Category) {
		tags = append(tags, annotationTag{kind: tagCategory, category: att.Category})
	}
	if len(tags) > maxAnnotationTags {
		tags = tags[:maxAnnotationTags]
	}
	return tags
}

// ── Colors ───────────────────────────────────────────────────────────────────

// tagColor is a resolved foreground and background.
type tagColor struct {
	fg, bg lipgloss.Color
}

// tagFallbacks are TideMail's own tag colors, used when neither the user nor
// the theme supplies a valid one.
var tagFallbacks = map[string]string{
	tagKeyReply:             "#3b82f6",
	tagKeyUrgent:            "#e5484d",
	tagKeyImportant:         "#f5a524",
	tagKeyCategory:          "#6b7280",
	"category.github":       "#8250df",
	"category.shipping":     "#b0703a",
	"category.security":     "#c2410c",
	"category.calendar":     "#2f80ed",
	"category.newsletter":   "#0f9d8a",
	"category.support":      "#2e9d4f",
	"category.social":       "#d6409f",
	"category.billing":      "#b8860b",
	"category.receipt":      "#7a8b2b",
	"category.notification": "#64748b",
	"category.personal":     "#0891b2",
}

// themeTagPalettes are per-theme tag colors by semantic key, for themes whose
// look calls for something other than the derived defaults. Theme authors add
// entries here; keys not listed fall through to the derived value or
// TideMail's fallback.
var themeTagPalettes = map[string]map[string]config.TagColor{
	ThemeNameVT100: monochromeTagPalette("#33ff33", "#000000"),
	ThemeNameVT52:  monochromeTagPalette("#ffcc66", "#000000"),
}

// monochromeTagPalette gives every tag the same inverse-video look, for the
// phosphor themes.
func monochromeTagPalette(bg, fg string) map[string]config.TagColor {
	out := map[string]config.TagColor{}
	for _, k := range tagColorKeys() {
		out[k] = config.TagColor{Fg: fg, Bg: bg}
	}
	return out
}

// themeTagColor is the theme's semantic value for key: its palette entry, or
// one derived from its colors for the attention kinds and the category
// default. Known categories have no derived value; they use TideMail's hues.
func themeTagColor(t Theme, key string) config.TagColor {
	if c, ok := themeTagPalettes[t.Name][key]; ok {
		return c
	}
	switch key {
	case tagKeyReply:
		return config.TagColor{Bg: string(t.BorderFocus)}
	case tagKeyUrgent:
		return config.TagColor{Bg: string(t.Error)}
	case tagKeyImportant:
		return config.TagColor{Bg: string(starColor(t))}
	case tagKeyCategory:
		if t.Bg != "" && t.Fg != "" {
			return config.TagColor{Bg: string(mixColors(t.Bg, t.Fg, 0.3)), Fg: string(t.Fg)}
		}
	}
	return config.TagColor{}
}

// resolveTagColor applies the precedence user override → theme semantic
// value → TideMail fallback, separately for the background and foreground.
// Invalid values are skipped. A foreground nobody set is chosen for contrast
// with the background.
func resolveTagColor(key string, user map[string]config.TagColor, t Theme) tagColor {
	if _, known := tagFallbacks[key]; !known {
		key = tagKeyCategory
	}
	theme := themeTagColor(t, key)
	bg := firstValidHex(user[key].Bg, theme.Bg, tagFallbacks[key])
	fg := firstValidHex(user[key].Fg, theme.Fg)
	if fg == "" {
		fg = string(contrastFg(lipgloss.Color(bg)))
	}
	return tagColor{fg: lipgloss.Color(fg), bg: lipgloss.Color(bg)}
}

func firstValidHex(candidates ...string) string {
	for _, c := range candidates {
		if hex, ok := config.NormalizeTagHex(c); ok {
			return hex
		}
	}
	return ""
}

// tagColorFor resolves the colors for one tag with the current settings.
func (m Model) tagColorFor(key string) tagColor {
	return resolveTagColor(key, m.cfg.Display.TagColors, m.styles.Theme)
}

// ── Layout ───────────────────────────────────────────────────────────────────

// tagLevel is how much room a tag takes.
type tagLevel int

const (
	// tagWide spells labels out: "↩ REPLY", "GITHUB".
	tagWide tagLevel = iota
	// tagMedium shrinks every tag to a single glyph, category included: "↩", "⚙".
	tagMedium
	// tagNarrow drops the pill padding: "↩ ◆ #github".
	tagNarrow
)

// tagChip is one laid-out tag: its text, whether it is drawn as a filled
// pill (and how the pill ends), and whether it is colored at all.
type tagChip struct {
	text     string
	colorKey string
	filled   bool
	colored  bool
	// ends is the pill's end style (config.TagEnds*), for filled chips.
	ends string
}

// Rounded pill caps (Nerd Font powerline glyphs), drawn in the pill color on
// the row background.
const (
	pillCapLeft  = "\ue0b6"
	pillCapRight = "\ue0b4"
)

// width is the chip's cell width, including its pill ends.
func (c tagChip) width() int {
	w := lipgloss.Width(c.text)
	if c.filled && c.ends != config.TagEndsNone {
		w += 2 // a padding cell or a rounded cap on each side
	}
	return w
}

func chipsWidth(chips []tagChip) int {
	if len(chips) == 0 {
		return 0
	}
	w := len(chips) - 1 // one space between chips
	for _, c := range chips {
		w += c.width()
	}
	return w
}

func tagGlyph(k tagKind, icons bool) string {
	if icons {
		return [...]string{"↩", "!", "◆"}[k]
	}
	return [...]string{"R", "!", "^"}[k]
}

var tagLabels = [...]string{"REPLY", "URGENT", "IMPORTANT"}

// categoryGlyphs give each known category a single glyph for the medium
// pill level (icon, then its ASCII fallback), chosen to evoke the category
// at a glance: a gear for CI/PR activity, a key for security, and so on.
var categoryGlyphs = map[string][2]string{
	"github":       {"⚙", "G"},
	"shipping":     {"▣", "P"},
	"security":     {"⚿", "K"},
	"calendar":     {"▦", "D"},
	"newsletter":   {"✉", "N"},
	"support":      {"☎", "H"},
	"social":       {"☺", "@"},
	"billing":      {"$", "$"},
	"receipt":      {"▤", "#"},
	"notification": {"◉", "B"},
	"personal":     {"⌂", "~"},
}

// categoryGlyph is the medium-level glyph for a category tag: the glyph that
// best represents a known category, or a generic marker for any other
// category value.
func categoryGlyph(category string, icons bool) string {
	g, ok := categoryGlyphs[category]
	if !ok {
		g = [2]string{"●", "*"}
	}
	if icons {
		return g[0]
	}
	return g[1]
}

// chipFor lays out one tag in a style at a level.
func chipFor(t annotationTag, style string, level tagLevel, icons bool) tagChip {
	c := tagChip{colorKey: t.colorKey()}
	isCategory := t.kind == tagCategory
	switch style {
	case config.TagStyleGlyphPills:
		c.colored = true
		c.filled = level != tagNarrow
		if isCategory {
			c.text = categoryGlyph(t.category, icons)
		} else {
			c.text = tagGlyph(t.kind, icons)
		}
		return c
	case config.TagStylePlain:
		switch {
		case isCategory:
			c.text = "[" + t.category + "]"
		case level == tagWide:
			c.text = "[" + tagLabels[t.kind] + "]"
		default:
			c.text = "[" + tagGlyph(t.kind, false) + "]"
		}
		return c
	case config.TagStyleCompact:
		c.colored = true
		switch {
		case isCategory:
			c.text = "#" + t.category
		case level == tagWide:
			c.text = tagGlyph(t.kind, icons) + tagLabels[t.kind]
		default:
			c.text = tagGlyph(t.kind, icons)
		}
		return c
	}
	// Pills.
	c.colored = true
	c.filled = level != tagNarrow
	switch {
	case isCategory && level == tagNarrow:
		c.text = "#" + t.category
	case isCategory && level == tagMedium:
		c.text = categoryGlyph(t.category, icons)
	case isCategory:
		c.text = strings.ToUpper(t.category)
	case level == tagWide && icons:
		c.text = tagGlyph(t.kind, icons) + " " + tagLabels[t.kind]
	case level == tagWide:
		c.text = tagLabels[t.kind]
	default:
		c.text = tagGlyph(t.kind, icons)
	}
	return c
}

// layoutTags fits tags into budget cells: each level in turn (wide, medium,
// narrow), then without the category, then dropping the lowest-priority
// tags, and finally nothing. The subject's room is decided by the caller, so
// tags never crowd it out.
func layoutTags(tags []annotationTag, style, ends string, icons bool, budget int) []tagChip {
	for len(tags) > 0 && budget > 0 {
		for _, level := range []tagLevel{tagWide, tagMedium, tagNarrow} {
			chips := make([]tagChip, len(tags))
			for i, t := range tags {
				chips[i] = chipFor(t, style, level, icons)
				chips[i].ends = ends
			}
			if chipsWidth(chips) <= budget {
				return chips
			}
		}
		// Too wide even when narrow: drop the category first, then the
		// lowest-priority attention tag.
		dropped := false
		for i, t := range tags {
			if t.kind == tagCategory {
				tags = append(append([]annotationTag(nil), tags[:i]...), tags[i+1:]...)
				dropped = true
				break
			}
		}
		if !dropped {
			tags = tags[:len(tags)-1]
		}
	}
	return nil
}

// tagStyle is the effective style: the setting, or Plain in the plain UI.
func (m Model) tagStyle() string {
	if m.styles.PlainUI {
		return config.TagStylePlain
	}
	return config.NormalizeTagStyle(m.cfg.Display.TagStyle)
}

// tagEnds is the effective pill end style.
func (m Model) tagEnds() string {
	return config.NormalizeTagEnds(m.cfg.Display.TagEnds)
}

// renderChips draws laid-out chips inside a row whose own style is row (no
// padding or width). Every segment carries its own background, so the row
// color continues across and after the tags.
func (m Model) renderChips(chips []tagChip, row lipgloss.Style) string {
	rowBg := terminalColorAsColor(row.GetBackground())
	if rowBg == "" {
		rowBg = m.styles.Theme.Bg
	}
	return renderChipsWith(chips, m.tagColorFor, rowBg, row)
}

// renderChipsWith draws chips exactly like renderChips, but resolves each
// chip's color through colorFor instead of a Model's configured tag colors.
// Settings uses this with fixed pastel demo colors to preview a tag style or
// pill-ends choice without needing a Model.
func renderChipsWith(chips []tagChip, colorFor func(string) tagColor, rowBg lipgloss.Color, row lipgloss.Style) string {
	var b strings.Builder
	for i, c := range chips {
		if i > 0 {
			b.WriteString(row.Render(" "))
		}
		switch {
		case c.filled:
			col := colorFor(c.colorKey)
			body := lipgloss.NewStyle().Background(col.bg).Foreground(col.fg).Bold(true)
			switch c.ends {
			case config.TagEndsNone:
				b.WriteString(body.Render(c.text))
			case config.TagEndsRound:
				caps := row.Foreground(col.bg)
				b.WriteString(caps.Render(pillCapLeft) + body.Render(c.text) + caps.Render(pillCapRight))
			default:
				b.WriteString(body.Render(" " + c.text + " "))
			}
		case c.colored:
			col := colorFor(c.colorKey)
			b.WriteString(row.Foreground(accentReadableOn(col.bg, rowBg, 3)).Render(c.text))
		default:
			b.WriteString(row.Render(c.text))
		}
	}
	return b.String()
}

// chipsText is the plain text of uncolored chips.
func chipsText(chips []tagChip) string {
	parts := make([]string, len(chips))
	for i, c := range chips {
		parts[i] = c.text
	}
	return strings.Join(parts, " ")
}

// ── Settings previews ───────────────────────────────────────────────────────
//
// The Tag style and Pill ends pickers in Settings show a live rendering of
// each choice, rather than describing it in prose. The preview uses fixed
// pastel demo colors instead of the user's configured (or theme-derived) tag
// colors, so it reads the same regardless of theme or customization and
// never mistakes itself for the real thing.

// tagExampleTag is the one sample tag previewed in Settings: a category, the
// same "github" example used throughout the docs and tests.
var tagExampleTag = annotationTag{kind: tagCategory, category: "github"}

// tagExamplePastels are the light, low-saturation demo colors for those
// previews.
var tagExamplePastels = map[string]tagColor{
	tagKeyReply:    {fg: "#3a3a3a", bg: "#cfe8ff"},
	tagKeyCategory: {fg: "#3a3a3a", bg: "#e6dcfb"},
}

func tagExampleColor(key string) tagColor {
	if c, ok := tagExamplePastels[key]; ok {
		return c
	}
	return tagExamplePastels[tagKeyCategory]
}

// renderTagExample previews one tag style/ends combination on background bg
// as a single pill: a filled pastel pill for Pills (with the chosen
// endcaps), pastel-colored text for Compact, or plain bracketed text for
// Plain — exactly what that choice looks like, nothing else mixed in.
func renderTagExample(style, ends string, icons bool, bg lipgloss.Color) string {
	chip := chipFor(tagExampleTag, style, tagWide, icons)
	chip.ends = ends
	return renderChipsWith([]tagChip{chip}, tagExampleColor, bg, lipgloss.NewStyle().Background(bg))
}

// minSubjectCols is the least room a subject keeps before tags shrink.
const minSubjectCols = 12

// tagBudget is how many cells tags may use when title and tags share
// titleSpace cells: never more than half, so the subject stays primary.
func tagBudget(titleSpace int) int {
	keep := max(minSubjectCols, (titleSpace+1)/2)
	return max(0, titleSpace-keep-2) // two spaces between subject and tags
}

// renderMessageRow lays out one message row: prefix, star, optional sender
// column, subject, tags, and the age column, styled with style. Rows without
// tags are exactly the classic layout.
func (m Model) renderMessageRow(style lipgloss.Style, prefix, star, sender string, showSender bool, senderW int, title string, tags []annotationTag, age string, width int) string {
	classic := func(age string) string {
		if showSender {
			return style.Width(width).Render(renderArticleRowWithSender(prefix, star, sender, title, age, width, senderW))
		}
		return style.Width(width).Render(renderArticleRow(prefix, star, title, age, width))
	}
	if len(tags) == 0 {
		return classic(age)
	}

	// Room shared by the subject and tags, as the classic layout computes it.
	fixed := lipgloss.Width(prefix) + lipgloss.Width(star)
	if showSender {
		fixed += senderW + 1
	}
	if age != "" {
		fixed += lipgloss.Width(age) + 4 // gap before age, trailing space
	}
	titleSpace := max(0, width-fixed)
	chips := layoutTags(tags, m.tagStyle(), m.tagEnds(), m.iconsEnabled() && !m.styles.PlainUI, tagBudget(titleSpace))
	if len(chips) == 0 {
		return classic(age)
	}
	uncolored := true
	for _, c := range chips {
		uncolored = uncolored && !c.colored
	}
	if uncolored {
		// Plain tags are ordinary text in front of the age column.
		combined := chipsText(chips)
		if age != "" {
			combined += "  " + age
		}
		return classic(combined)
	}

	// Colored tags: draw the row in self-styled segments so each keeps the
	// row background, then let the row style add its padding.
	inline := style.UnsetPadding().UnsetWidth().UnsetMaxWidth()
	chipsW := chipsWidth(chips)
	titleW := max(0, titleSpace-chipsW-2)
	left := prefix + star
	if showSender {
		left += padRight(truncate(sender, senderW), senderW) + " "
	}
	left += padRight(truncate(title, titleW), titleW) + "  "
	right := ""
	if age != "" {
		right = "  " + age + "  "
	}
	line := inline.Render(left) + m.renderChips(chips, inline)
	if rest := width - lipgloss.Width(left) - chipsW; rest > 0 {
		line += inline.Render(padRight(right, rest))
	}
	return style.Width(width).Render(line)
}
