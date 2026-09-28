package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	replyTag     = annotationTag{kind: tagReply}
	urgentTag    = annotationTag{kind: tagUrgent}
	importantTag = annotationTag{kind: tagImportant}
	githubTag    = annotationTag{kind: tagCategory, category: "github"}
)

func chipTexts(chips []tagChip) string {
	parts := make([]string, len(chips))
	for i, c := range chips {
		parts[i] = c.text
	}
	return strings.Join(parts, "|")
}

// pill is how a filled pill renders with the model's resolved colors.
func pill(m Model, key, text string) string {
	c := m.tagColorFor(key)
	return lipgloss.NewStyle().Background(c.bg).Foreground(c.fg).Bold(true).Render(" " + text + " ")
}

// Cases 27–30: each kind has its wide pill label and semantic color key.
func TestTagPillsPerKind(t *testing.T) {
	cases := []struct {
		tag       annotationTag
		text, key string
	}{
		{replyTag, "↩ REPLY", "reply"},
		{urgentTag, "! URGENT", "urgent"},
		{importantTag, "◆ IMPORTANT", "important"},
		{githubTag, "GITHUB", "category.github"},
	}
	for _, tc := range cases {
		c := chipFor(tc.tag, config.TagStylePills, tagWide, true)
		if c.text != tc.text || c.colorKey != tc.key || !c.filled {
			t.Errorf("%v: chip %+v, want %q/%q filled", tc.tag, c, tc.text, tc.key)
		}
	}
	// Without icons the labels stand alone.
	if c := chipFor(replyTag, config.TagStylePills, tagWide, false); c.text != "REPLY" {
		t.Errorf("ascii reply = %q", c.text)
	}
}

// Cases 31–35: color precedence and fallbacks.
func TestTagColorPrecedence(t *testing.T) {
	theme := CatppuccinMocha
	norm := func(c lipgloss.Color) string {
		hex, _ := config.NormalizeTagHex(string(c))
		return hex
	}

	// 33: the theme's semantic value when the user set nothing.
	if got := resolveTagColor("reply", nil, theme); string(got.bg) != norm(theme.BorderFocus) {
		t.Fatalf("reply bg = %s, want theme accent %s", got.bg, theme.BorderFocus)
	}
	if got := resolveTagColor("urgent", nil, theme); string(got.bg) != norm(theme.Error) {
		t.Fatalf("urgent bg = %s, want theme error %s", got.bg, theme.Error)
	}

	// 32: a user override wins, for just that key and field.
	user := map[string]config.TagColor{"reply": {Bg: "#123456"}}
	got := resolveTagColor("reply", user, theme)
	if got.bg != "#123456" || got.fg != contrastFg("#123456") {
		t.Fatalf("override = %+v", got)
	}
	if other := resolveTagColor("urgent", user, theme); string(other.bg) != norm(theme.Error) {
		t.Fatal("overriding one tag must leave the others on the theme")
	}
	user = map[string]config.TagColor{"reply": {Fg: "#ABC"}}
	if got := resolveTagColor("reply", user, theme); got.fg != "#aabbcc" || string(got.bg) != norm(theme.BorderFocus) {
		t.Fatalf("fg-only override = %+v", got)
	}

	// 35: invalid values fall back safely.
	for _, bad := range []string{"purple", "#12345", "#gggggg", "rgb(1,2,3)", "#1234567", " "} {
		user := map[string]config.TagColor{"reply": {Fg: bad, Bg: bad}}
		if got := resolveTagColor("reply", user, theme); string(got.bg) != norm(theme.BorderFocus) {
			t.Errorf("invalid %q: bg = %s", bad, got.bg)
		}
	}

	// 34: TideMail's fallback when the theme has no usable value, and for
	// known categories, which themes don't derive.
	blank := Theme{Name: "blank"}
	if got := resolveTagColor("reply", nil, blank); got.bg != lipgloss.Color(tagFallbacks["reply"]) {
		t.Fatalf("fallback reply = %s", got.bg)
	}
	if got := resolveTagColor("category.github", nil, theme); got.bg != lipgloss.Color(tagFallbacks["category.github"]) {
		t.Fatalf("github = %s", got.bg)
	}
	// A theme palette entry beats the derived value.
	if got := resolveTagColor("category.github", nil, VT100); got.bg != "#33ff33" {
		t.Fatalf("vt100 github = %s", got.bg)
	}

	// 31: an unknown category uses the category default, including the
	// user's override of it; a specific key doesn't leak to others.
	project := annotationTag{kind: tagCategory, category: "project"}
	if project.colorKey() != "category" {
		t.Fatalf("unknown category key = %q", project.colorKey())
	}
	user = map[string]config.TagColor{"category": {Bg: "#224466"}, "category.github": {Bg: "#aa00aa"}}
	if got := resolveTagColor(project.colorKey(), user, theme); got.bg != "#224466" {
		t.Fatalf("project = %s", got.bg)
	}
	if got := resolveTagColor("category.nonsense", user, theme); got.bg != "#224466" {
		t.Fatalf("an unknown key resolves as the category default, got %s", got.bg)
	}
	if got := resolveTagColor("category.shipping", user, theme); got.bg != lipgloss.Color(tagFallbacks["category.shipping"]) {
		t.Fatalf("shipping = %s", got.bg)
	}
}

// Cases 36–38: the three styles.
func TestTagStyles(t *testing.T) {
	tags := []annotationTag{replyTag, urgentTag, githubTag}
	if got := chipTexts(layoutTags(tags, config.TagStylePills, config.TagEndsSquare, true, 100)); got != "↩ REPLY|! URGENT|GITHUB" {
		t.Errorf("pills = %q", got)
	}
	if got := chipTexts(layoutTags(tags, config.TagStyleCompact, config.TagEndsSquare, true, 100)); got != "↩REPLY|!URGENT|#github" {
		t.Errorf("compact = %q", got)
	}
	plain := layoutTags(tags, config.TagStylePlain, config.TagEndsSquare, true, 100)
	if got := chipTexts(plain); got != "[REPLY]|[URGENT]|[github]" {
		t.Errorf("plain = %q", got)
	}
	for _, c := range plain {
		if c.colored || c.filled {
			t.Errorf("plain chip %+v is colored", c)
		}
	}
	for _, c := range layoutTags(tags, config.TagStyleCompact, config.TagEndsSquare, true, 100) {
		if !c.colored || c.filled {
			t.Errorf("compact chip %+v should be colored text", c)
		}
	}
	if config.NormalizeTagStyle("") != config.TagStylePills || config.NormalizeTagStyle("bogus") != config.TagStylePills {
		t.Error("pills is the default")
	}
}

// Cases 39–41: wide, medium, narrow, then dropping.
func TestTagAdaptiveWidth(t *testing.T) {
	tags := []annotationTag{replyTag, importantTag, githubTag}
	cases := []struct {
		budget int
		want   string
	}{
		{80, "↩ REPLY|◆ IMPORTANT|GITHUB"}, // wide: 9+1+13+1+8 = 32
		{20, "↩|◆|⚙"},                      // medium: 3+1+3+1+3 = 11 (github's glyph pill)
		{8, "↩|◆"},                         // neither medium nor narrow fits all 3: category dropped first
		{2, "↩"},                           // then the lowest priority
		{0, ""},
	}
	for _, tc := range cases {
		chips := layoutTags(tags, config.TagStylePills, config.TagEndsSquare, true, tc.budget)
		if got := chipTexts(chips); got != tc.want {
			t.Errorf("budget %d: %q, want %q", tc.budget, got, tc.want)
		}
		if w := chipsWidth(chips); w > tc.budget {
			t.Errorf("budget %d: width %d", tc.budget, w)
		}
	}

	// A category's medium level is a filled, round-able glyph pill; its
	// narrow level drops to unpadded "#name" text, same as the other kinds.
	if c := chipFor(githubTag, config.TagStylePills, tagMedium, true); c.text != "⚙" || !c.filled {
		t.Errorf("medium category chip = %+v", c)
	}
	if c := chipFor(githubTag, config.TagStylePills, tagNarrow, true); c.text != "#github" || c.filled {
		t.Errorf("narrow category chip = %+v", c)
	}
	// Without icons, the medium glyph falls back to a plain ASCII marker.
	if c := chipFor(githubTag, config.TagStylePills, tagMedium, false); c.text != "G" {
		t.Errorf("ascii medium category chip = %+v", c)
	}
	// An unlisted category still gets a generic glyph rather than empty text.
	project := annotationTag{kind: tagCategory, category: "project"}
	if c := chipFor(project, config.TagStylePills, tagMedium, true); c.text != "●" {
		t.Errorf("medium unknown category chip = %+v", c)
	}
}

// Case 42: the subject keeps at least half the room (and never less than
// minSubjectCols) at every width, and rows keep their exact width.
func TestTagsKeepSubjectReadable(t *testing.T) {
	m, msgs := newMailboxListModel(7, 1)
	subject := "Quarterly planning notes for the whole team"
	m.messages[0].Subject = subject
	m.applyFilter()
	m.plugins.annotations = map[int64][]db.PluginAnnotation{msgs[0].ID: {
		cacheAnn("a", "needs_reply", "true"), cacheAnn("a", "urgency", "high"), cacheAnn("a", "category", "github"),
	}}
	tags := m.annotationTags(msgs[0].ID)
	style := m.styles.ArticleRead
	for _, w := range []int{30, 40, 50, 60, 80, 120, 160} {
		row := m.renderMessageRow(style, "  ", m.messageRowStar(false), "", false, 0, subject, tags, "Dec 31", w)
		first := strings.Split(ansi.Strip(row), "\n")[0]
		if got := lipgloss.Width(first); got != w {
			t.Errorf("width %d: row is %d wide: %q", w, got, first)
		}
		fixed := 2 + lipgloss.Width(m.messageRowStar(false)) + len("Dec 31") + 4
		titleSpace := w - fixed
		keep := min(len(subject), max(minSubjectCols, (titleSpace+1)/2))
		if !strings.Contains(first, subject[:max(0, keep-1)]) {
			t.Errorf("width %d: subject squeezed: %q", w, first)
		}
	}
}

// Case 43: category labels are identifiers only, and only upper-cased by
// TideMail; anything else never reaches the row.
func TestTagCategorySanitized(t *testing.T) {
	m, msgs := newMailboxListModel(7, 1)
	id := msgs[0].ID
	for _, v := range []string{"git\x1b[31mhub", "a b", "\u202egithub", "x\ny", "way-too-long-category", "<b>"} {
		m.plugins.annotations = map[int64][]db.PluginAnnotation{id: {cacheAnn("a", "category", v)}}
		if tags := m.annotationTags(id); len(tags) != 0 {
			t.Errorf("category %q produced %+v", v, tags)
		}
	}
	m.plugins.annotations = map[int64][]db.PluginAnnotation{id: {cacheAnn("a", "category", " GitHub ")}}
	tags := m.annotationTags(id)
	if len(tags) != 1 || tags[0].category != "github" || tags[0].colorKey() != "category.github" {
		t.Fatalf("tags = %+v", tags)
	}
}

// Case 44: annotations that try to carry presentation are ignored: they are
// not conventional keys, and a category can't name a color.
func TestPluginsCannotInjectPresentation(t *testing.T) {
	trueColor(t)
	m, msgs := newMailboxListModel(7, 1)
	id := msgs[0].ID
	m.plugins.annotations = map[int64][]db.PluginAnnotation{id: {
		cacheAnn("a", "color", "#ff0000"),
		cacheAnn("a", "background", "#ff0000"),
		cacheAnn("a", "foreground", "#ff0000"),
		cacheAnn("a", "pill_style", "rounded"),
		cacheAnn("a", "category", "#ff0000"),
		cacheAnn("a", "needs_reply", "true"),
	}}
	tags := m.annotationTags(id)
	if len(tags) != 1 || tags[0].kind != tagReply {
		t.Fatalf("tags = %+v", tags)
	}
	pane := m.renderMessagesPane()
	if strings.Contains(pane, "255;0;0") || strings.Contains(pane, "ff0000") {
		t.Fatal("a plugin-supplied color reached the screen")
	}
	if !strings.Contains(pane, pill(m, "reply", "↩ REPLY")) {
		t.Fatal("the reply pill should use TideMail's colors")
	}
}

// Case 45: the same pill appears, identically styled, in a mailbox, Needs
// You, and Snoozed.
func TestTagsSameAcrossViews(t *testing.T) {
	trueColor(t)
	m, f := newNeedsYouModel(t)
	m.width = 200
	a := f.add(f.inbox, "listed", 1, "p needs_reply true", "p category github")
	b := f.add(f.inbox, "sleeping", 2, "p needs_reply true", "p category github")
	if err := f.database.SetSnooze(db.SnoozeMessage, db.MessageKey(db.Message{ID: b}), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	reply, github := pill(m, "reply", "↩ REPLY"), pill(m, "category.github", "GITHUB")

	m = openNeedsYou(t, m)
	if line := rowLine(t, m, "listed"); !strings.Contains(line, reply) || !strings.Contains(line, github) {
		t.Fatalf("Needs You row = %q", line)
	}
	m = openSnoozed(t, m)
	if line := rowLine(t, m, "sleeping"); !strings.Contains(line, reply) || !strings.Contains(line, github) {
		t.Fatalf("Snoozed row = %q", line)
	}
	for i, row := range m.sidebarRows {
		if row.kind == rowKindMailbox && row.mailboxID == f.inbox {
			m.sidebarCursor = i
		}
	}
	m.messages, _ = f.database.ListMessages(f.inbox)
	m.plugins.annotations = loadMessageAnnotations(f.database, m.messages)
	m.applyFilter()
	for _, subject := range []string{"listed", "sleeping"} {
		if line := rowLine(t, m, subject); !strings.Contains(line, reply) || !strings.Contains(line, github) {
			t.Fatalf("mailbox row = %q", line)
		}
	}
	_ = a
}

// Colored segments keep the row background: nothing after the tags falls
// back to the terminal default.
func TestTagRowKeepsBackground(t *testing.T) {
	trueColor(t)
	m, msgs := newMailboxListModel(7, 1)
	m.plugins.annotations = map[int64][]db.PluginAnnotation{msgs[0].ID: {cacheAnn("a", "urgency", "high")}}
	for _, style := range []string{config.TagStylePills, config.TagStyleCompact} {
		m.cfg.Display.TagStyle = style
		line := rowLine(t, m, "msg 1")
		idx := strings.LastIndex(line, "URGENT")
		if idx < 0 {
			idx = strings.LastIndex(line, "!")
		}
		tail := line[idx:]
		// After the tag's reset, the age column is re-styled before any text.
		reset := strings.Index(tail, "\x1b[0m")
		if reset < 0 || !strings.Contains(tail[reset:], "\x1b[") {
			t.Fatalf("%s: tail %q", style, tail)
		}
	}
}

// Case 46: style and colors survive a save and reload, and Settings edits
// the style.
func TestTagSettingsRoundTrip(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Display.TagStyle != config.TagStylePills {
		t.Fatalf("default style = %q", cfg.Display.TagStyle)
	}
	s := newSettings(cfg, settingsUpdateState{})
	if s.tagStyleIdx != 0 {
		t.Fatalf("idx = %d", s.tagStyleIdx)
	}
	s.tagStyleIdx = 1
	s.tagEndsIdx = 2
	cfg = s.ApplyTo(cfg)
	cfg.Display.TagColors = map[string]config.TagColor{"reply": {Fg: "#ffffff", Bg: "#123456"}, "category.github": {Bg: "#aa00aa"}}

	path := filepath.Join(t.TempDir(), "config.toml")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		t.Fatal(err)
	}
	f.Close()
	var back config.Config
	if _, err := toml.DecodeFile(path, &back); err != nil {
		t.Fatal(err)
	}
	if back.Display.TagStyle != config.TagStyleCompact || back.Display.TagEnds != config.TagEndsNone {
		t.Fatalf("style = %q, ends = %q", back.Display.TagStyle, back.Display.TagEnds)
	}
	if back.Display.TagColors["reply"] != (config.TagColor{Fg: "#ffffff", Bg: "#123456"}) || back.Display.TagColors["category.github"].Bg != "#aa00aa" {
		t.Fatalf("colors = %+v", back.Display.TagColors)
	}
	if s := newSettings(back, settingsUpdateState{}); s.tagStyleIdx != 1 || s.tagEndsIdx != 2 {
		t.Fatalf("reloaded idx = %d", s.tagStyleIdx)
	}
}

// The editor opens the color picker for background/foreground, which
// validates typed input, applies on confirm, and can be canceled; the
// tag-list-level reset still works, and Settings edits the style.
func TestTagColorEditor(t *testing.T) {
	var saved config.Config
	orig := configSave
	configSave = func(c config.Config) error { saved = c; return nil }
	t.Cleanup(func() { configSave = orig })
	m, _ := newMailboxListModel(7, 1)
	next, _ := m.openTagColors(overlaySettings)
	m = next.(Model)
	if m.overlay != overlayTagColors || !strings.Contains(m.View(), "Category: github") {
		t.Fatal("editor should list the known categories")
	}
	m, _ = press(t, m, "b")
	if m.overlay != overlayColorPicker || m.colorPicker.key != "reply" || m.colorPicker.field != "bg" {
		t.Fatalf("b should open the color picker for reply/bg, got overlay=%v picker=%+v", m.overlay, m.colorPicker)
	}
	m, _ = press(t, m, "#")
	m.colorPicker.input.SetValue("purple")
	m, _ = press(t, m, "enter")
	if m.colorPicker.err == "" || m.cfg.Display.TagColors != nil {
		t.Fatal("an invalid color must be refused and not stored")
	}
	m.colorPicker.input.SetValue("abc")
	m, _ = press(t, m, "enter")
	if m.colorPicker.hex() != "#aabbcc" {
		t.Fatalf("picker color after commit = %s", m.colorPicker.hex())
	}
	if m.cfg.Display.TagColors != nil {
		t.Fatal("editing a field must not write to config until the picker is confirmed")
	}
	// Move off the hex field so Enter closes and applies rather than
	// reopening that field's text edit.
	m.colorPicker.focus = colorPickerFocus{kind: cpFocusRGBSlider}
	m, _ = press(t, m, "enter")
	if m.overlay != overlayTagColors {
		t.Fatalf("enter should apply and return to the tag list, got %v", m.overlay)
	}
	if got := m.cfg.Display.TagColors["reply"]; got.Bg != "#aabbcc" {
		t.Fatalf("stored = %+v", m.cfg.Display.TagColors)
	}
	if m.tagColorFor("reply").bg != "#aabbcc" {
		t.Fatal("the override should apply at once")
	}
	if saved.Display.TagColors["reply"].Bg != "#aabbcc" {
		t.Fatalf("saved = %+v", saved.Display.TagColors)
	}
	m, _ = press(t, m, "r")
	if m.cfg.Display.TagColors != nil {
		t.Fatalf("reset left %+v", m.cfg.Display.TagColors)
	}
	m, _ = press(t, m, "esc")
	if m.overlay != overlaySettings {
		t.Fatalf("esc returns to Settings, got %v", m.overlay)
	}
}

// Canceling the picker (Esc, not editing a field) discards the change.
func TestColorPickerCancelDiscards(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	next, _ := m.openTagColors(overlaySettings)
	m = next.(Model)
	m, _ = press(t, m, "b")
	m, _ = press(t, m, "#")
	m.colorPicker.input.SetValue("00ff00")
	m, _ = press(t, m, "enter")
	if m.colorPicker.hex() != "#00ff00" {
		t.Fatalf("picker color = %s", m.colorPicker.hex())
	}
	m.colorPicker.focus = colorPickerFocus{kind: cpFocusRGBSlider}
	m, _ = press(t, m, "esc")
	if m.overlay != overlayTagColors {
		t.Fatalf("esc should return to the tag list, got %v", m.overlay)
	}
	if m.cfg.Display.TagColors != nil {
		t.Fatalf("canceling must not store the edited color: %+v", m.cfg.Display.TagColors)
	}
}

// Pill ends: square pads each side, round draws caps in the pill color on
// the row background, none drops both to save two cells per pill.
func TestTagPillEnds(t *testing.T) {
	trueColor(t)
	m, _ := newMailboxListModel(7, 1)
	tags := []annotationTag{replyTag, githubTag}
	widths := map[string]int{}
	for _, ends := range []string{config.TagEndsSquare, config.TagEndsRound, config.TagEndsNone} {
		chips := layoutTags(tags, config.TagStylePills, ends, true, 100)
		widths[ends] = chipsWidth(chips)
		m.cfg.Display.TagEnds = ends
		out := m.renderChips(chips, m.styles.ArticleRead.UnsetPadding())
		if got := lipgloss.Width(out); got != widths[ends] {
			t.Errorf("%s: rendered %d cells, layout says %d", ends, got, widths[ends])
		}
		hasCaps := strings.Contains(out, pillCapLeft) && strings.Contains(out, pillCapRight)
		if hasCaps != (ends == config.TagEndsRound) {
			t.Errorf("%s: caps present = %v", ends, hasCaps)
		}
		if ends == config.TagEndsNone && strings.Contains(ansi.Strip(out), " GITHUB ") {
			t.Errorf("none should not pad: %q", ansi.Strip(out))
		}
	}
	// "↩ REPLY" + "GITHUB" + one gap = 14; each pill adds 2 unless none.
	if widths[config.TagEndsSquare] != 18 || widths[config.TagEndsRound] != 18 || widths[config.TagEndsNone] != 14 {
		t.Fatalf("widths = %v", widths)
	}
	// Narrow text tags never get ends. (A category's medium glyph pill is
	// already as tight as its narrow "#name" form or tighter, so this uses
	// plain attention tags, whose narrow level drops padding a medium glyph
	// pill wouldn't.)
	for _, c := range layoutTags([]annotationTag{replyTag, urgentTag}, config.TagStylePills, config.TagEndsRound, true, 5) {
		if c.width() != lipgloss.Width(c.text) {
			t.Errorf("narrow chip %+v has ends", c)
		}
	}
	if config.NormalizeTagEnds("") != config.TagEndsSquare || config.NormalizeTagEnds("ROUND") != config.TagEndsRound {
		t.Error("square is the default; values are case-insensitive")
	}
}
