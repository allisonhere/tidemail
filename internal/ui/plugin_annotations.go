package ui

// Plugin annotations in the UI: the database adapter the plugin manager
// writes through, batch loading for message lists, the result note, row
// badges, and the read-only annotations overlay. Cleanup lives in
// plugin_cleanup.go.

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/charmbracelet/lipgloss"
)

// dbAnnotationStore adapts the database to plugin.AnnotationStore. It is only
// ever handed to Manager.MessageMetadata, which checks permissions and
// validates the set before calling it.
type dbAnnotationStore struct{ db *db.DB }

func (s dbAnnotationStore) ReplaceAnnotations(pluginID string, messageID int64, anns []plugin.Annotation) error {
	rows := make([]db.PluginAnnotation, len(anns))
	for i, a := range anns {
		rows[i] = db.PluginAnnotation{Key: a.Key, Value: a.Value, Confidence: a.Confidence}
	}
	return s.db.ReplacePluginAnnotations(pluginID, messageID, rows)
}

// loadMessageAnnotations batch-loads annotations for a message list inside a
// load command. Failure only costs the badges, never the message list.
func loadMessageAnnotations(database *db.DB, msgs []db.Message) map[int64][]db.PluginAnnotation {
	if database == nil || len(msgs) == 0 {
		return nil
	}
	ids := make([]int64, len(msgs))
	for i, msg := range msgs {
		ids[i] = msg.ID
	}
	anns, err := database.ListPluginAnnotationsForMessages(ids)
	if err != nil {
		return nil
	}
	return anns
}

// annotationNote describes what happened to a run's annotations.
func annotationNote(r plugin.MessageMetadataResult) string {
	n := len(r.Annotations)
	switch r.Outcome {
	case plugin.AnnotationsStored:
		if n == 0 {
			return "annotations: none returned; this plugin's previous annotations were cleared"
		}
		return fmt.Sprintf("annotations: %d stored", n)
	case plugin.AnnotationsNotPermitted:
		if n == 0 {
			return ""
		}
		return fmt.Sprintf("annotations: %d ignored; this plugin lacks the annotations permission", n)
	case plugin.AnnotationsRejected:
		return "annotations rejected, nothing changed: " + sanitizePluginLine(r.AnnotationErr.Error())
	case plugin.AnnotationsNotStored:
		return "annotations not saved, nothing changed: " + sanitizePluginLine(r.AnnotationErr.Error())
	}
	return ""
}

// ── Annotations ──────────────────────────────────────────────────────────────

// annotationBadgeRule maps one conventional annotation key to a row badge.
// These keys are display conventions only: the protocol and storage accept
// any key, and unknown keys never appear in rows (they stay inspectable in the
// annotations overlay).
type annotationBadgeRule struct {
	key   string
	icon  string
	ascii string
	// badge returns the badge text for a value, or "" for no badge.
	badge func(rule annotationBadgeRule, value string, icons bool) string
}

func fixedBadge(values ...string) func(annotationBadgeRule, string, bool) string {
	return func(rule annotationBadgeRule, value string, icons bool) string {
		for _, v := range values {
			if value == v {
				if icons {
					return rule.icon
				}
				return rule.ascii
			}
		}
		return ""
	}
}

// categoryBadgePattern limits the category tag to short, plain identifiers so
// a row never carries free-form plugin text.
var categoryBadgePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,11}$`)

var annotationBadgeRules = []annotationBadgeRule{
	// The value lists are shared with Needs You, so a badge and Needs You
	// always agree on what counts.
	{key: db.AttentionNeedsReply, icon: "↩", ascii: "R", badge: fixedBadge(db.NeedsReplyValues...)},
	{key: db.AttentionUrgency, icon: "!", ascii: "!", badge: fixedBadge(db.UrgencyValues...)},
	{key: db.AttentionImportance, icon: "◆", ascii: "^", badge: fixedBadge(db.ImportanceValues...)},
	{key: "category", badge: func(_ annotationBadgeRule, value string, _ bool) string {
		if categoryBadgePattern.MatchString(value) {
			return "#" + value
		}
		return ""
	}},
}

// maxAnnotationBadges caps badges per row.
const maxAnnotationBadges = 3

// annotationBadges renders the row badges for messages from the cache, in
// rule order, at most one per key whichever plugin set it. It never queries
// the database.
func (m Model) annotationBadges(messageIDs ...int64) string {
	var anns []db.PluginAnnotation
	for _, id := range messageIDs {
		anns = append(anns, m.plugins.annotations[id]...)
	}
	if len(anns) == 0 {
		return ""
	}
	icons := m.iconsEnabled() && !m.styles.PlainUI
	var badges []string
	for _, rule := range annotationBadgeRules {
		for _, a := range anns {
			if a.Key != rule.key {
				continue
			}
			if b := rule.badge(rule, strings.ToLower(strings.TrimSpace(a.Value)), icons); b != "" {
				badges = append(badges, b)
				break
			}
		}
		if len(badges) == maxAnnotationBadges {
			break
		}
	}
	return strings.Join(badges, " ")
}

// withAnnotationBadges puts a message's badges in front of its age column.
func (m Model) withAnnotationBadges(age string, messageIDs ...int64) string {
	badges := m.annotationBadges(messageIDs...)
	switch {
	case badges == "":
		return age
	case age == "":
		return badges
	}
	return badges + "  " + age
}

// annotationLines renders the annotations overlay: every stored annotation on
// the message, grouped by plugin.
func (m Model) annotationLines(width int, chrome managerChrome) ([]string, []int) {
	width = max(1, width-2) // minus the rail
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	head := base.Foreground(chrome.accent).Bold(true)
	text := base.Foreground(chrome.text)
	muted := base.Foreground(chrome.muted)

	anns := m.plugins.annotations[m.plugins.annotationsFor]
	lines := m.needsYouExplanationLines(m.plugins.annotationsFor, chrome)
	for _, l := range m.waitingExplanationLines(m.plugins.annotationsFor, time.Now()) {
		lines = append(lines, text.Render(truncate(l, width)))
	}
	for _, l := range m.snoozeExplanationLines(m.plugins.annotationsFor) {
		lines = append(lines, text.Render(truncate(l, width)))
	}
	if len(anns) == 0 {
		if len(lines) == 0 {
			return []string{muted.Render("no annotations")}, nil
		}
		return append(lines, "", muted.Render("no plugin annotations")), nil
	}
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	blankRail := softRail(chrome, false, chrome.baseBg)
	var starts []int
	keyW := 0
	for _, a := range anns {
		keyW = max(keyW, len(a.Key))
	}
	keyW = min(keyW, 24)

	prev := ""
	for _, a := range anns { // sorted by plugin ID, then key
		if a.PluginID != prev {
			if prev != "" {
				lines = append(lines, "")
			}
			prev = a.PluginID
			title := sanitizePluginLine(a.PluginID) + " (not installed)"
			if p, ok := m.plugins.manager.Plugin(a.PluginID); ok {
				title = sanitizePluginLine(p.Manifest.Name) + "  (" + p.Manifest.ID + ")"
			}
			starts = append(starts, len(lines))
			rail := softRail(chrome, len(starts)-1 == m.plugins.annCursor, chrome.baseBg)
			lines = append(lines, rail+head.Render(truncate(title, width)))
		}
		confidence := ""
		if a.Confidence != nil {
			confidence = fmt.Sprintf("%3.0f%%", *a.Confidence*100)
		}
		key := padRight(truncate(sanitizePluginLine(a.Key), keyW), keyW)
		valueW := max(1, width-2-keyW-2-lipgloss.Width(confidence)-2)
		value := padRight(truncate(sanitizePluginLine(a.Value), valueW), valueW)
		row := "  " + key + "  " + value
		if confidence != "" {
			row += "  " + confidence
		}
		lines = append(lines, blankRail+text.Render(row))
	}
	lines = append(lines, "", muted.Render("annotations never change your mail; clearing them never deletes mail"))
	return lines, starts
}
