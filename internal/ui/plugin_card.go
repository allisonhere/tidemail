package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Plugin result cards: the human-facing view of a plugin run.
//
// Annotations are for machines; presentation is for people. A card comes
// from the plugin's optional presentation block, or is synthesized from the
// annotations TideMail understands. Raw data (keys, values, confidence
// decimals, storage outcome, errors) lives in Details, one keypress away.
// Cards are plain data (title, facts, reasons, actions) so rendering and a
// future mouse hit-map can both be derived from them.

// resultCard is what the result window shows by default.
type resultCard struct {
	title      string
	summary    string
	status     string // plugin.Status*
	confidence string // "High", "Medium", "Low", or ""
	facts      []plugin.Fact
	reasons    []string
	// sections are extra titled fact lists (bulk "Found").
	sections       []cardSection
	canUnsubscribe bool
}

type cardSection struct {
	title string
	facts []plugin.Fact
}

// Friendly labels and values for annotation keys TideMail understands.
var annotationFactLabels = map[string]string{
	"category":              "Type",
	"newsletter":            "Newsletter",
	"sender_value":          "Priority",
	"needs_reply":           "Needs reply",
	"urgency":               "Urgency",
	"importance":            "Importance",
	"automated_sender":      "Sender",
	"unsubscribe_available": "Unsubscribe",
	"unsubscribe_method":    "Unsubscribe via",
	"unsubscribe_one_click": "One-click unsubscribe",
}

// annotationFactOrder puts the most meaningful facts first.
var annotationFactOrder = []string{
	"category", "sender_value", "needs_reply", "urgency", "importance", "newsletter",
	"automated_sender", "unsubscribe_available", "unsubscribe_method", "unsubscribe_one_click",
}

// friendlyValue turns an annotation value into words.
func friendlyValue(key, value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	switch key {
	case "automated_sender":
		if v == "true" {
			return "Automated"
		}
	case "unsubscribe_available":
		if v == "true" {
			return "Available"
		}
		return "Not found"
	case "unsubscribe_method":
		switch v {
		case "https":
			return "Secure web link"
		case "http":
			return "Web link"
		case "mailto":
			return "Email"
		}
	}
	switch v {
	case "true", "yes":
		return "Yes"
	case "false", "no":
		return "No"
	}
	return titleWord(sanitizePluginLine(value))
}

// titleWord capitalizes a lowercase identifier-like value ("newsletter" →
// "Newsletter", "high" → "High"); other text is left alone.
func titleWord(s string) string {
	if s == "" || strings.ToLower(s) != s {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ReplaceAll(s[1:], "_", " ")
}

// annotationFacts are friendly fact rows for annotations, known keys first,
// and the strongest confidence among them.
func annotationFacts(anns []plugin.Annotation) ([]plugin.Fact, float64) {
	byKey := map[string]plugin.Annotation{}
	var unknown []string
	best := 0.0
	for _, a := range anns {
		byKey[a.Key] = a
		if _, ok := annotationFactLabels[a.Key]; !ok {
			unknown = append(unknown, a.Key)
		}
		if a.Confidence != nil && *a.Confidence > best {
			best = *a.Confidence
		}
	}
	sort.Strings(unknown)
	var facts []plugin.Fact
	for _, key := range append(append([]string{}, annotationFactOrder...), unknown...) {
		a, ok := byKey[key]
		if !ok {
			continue
		}
		label := annotationFactLabels[key]
		if label == "" {
			label = humanPluginLabel(sanitizePluginLine(key))
		}
		facts = append(facts, plugin.Fact{Label: label, Value: friendlyValue(key, a.Value)})
	}
	return facts, best
}

// messageCard is the card for a single-message run.
func messageCard(name string, msg pluginResultMsg, source *db.Message) resultCard {
	res := msg.Result
	anns := res.Annotations
	if p := res.Presentation; p != nil {
		c := resultCard{
			title: p.Title, summary: p.Summary, status: p.Status,
			facts: append([]plugin.Fact(nil), p.Facts...), reasons: append([]string(nil), p.Reasons...),
		}
		if p.Confidence != "" {
			c.confidence = titleWord(p.Confidence)
		}
		if msg.Compared && !msg.Changed && c.summary == "" {
			c.summary = "No change from the last run."
		}
		return finishMessageCard(c, source)
	}
	switch res.Outcome {
	case plugin.AnnotationsRejected, plugin.AnnotationsNotStored:
		return finishMessageCard(resultCard{title: name + " returned a result TideMail couldn't save",
			summary: "Nothing was changed. Details has the technical reason.", status: plugin.StatusWarning}, source)
	}
	facts, best := annotationFacts(anns)
	if len(facts) == 0 {
		if extra := displayOnlyData(res.Response.Data); extra != "" {
			return finishMessageCard(resultCard{title: "Plugin result", summary: extra, status: plugin.StatusInfo}, source)
		}
		return finishMessageCard(resultCard{title: "No changes", status: plugin.StatusNeutral,
			summary: name + " analyzed this message and found no new classification."}, source)
	}
	c := resultCard{title: "Plugin result", status: plugin.StatusInfo, facts: facts}
	switch {
	case msg.Compared && msg.Changed:
		c.title, c.status = "Classification updated", plugin.StatusSuccess
	case msg.Compared:
		c.title, c.summary = "Classification unchanged", "Same result as the last run."
	}
	if best > 0 {
		c.confidence = plugin.ConfidenceLabel(best)
	}
	return finishMessageCard(c, source)
}

// finishMessageCard lets TideMail add message-owned behavior to a plugin's
// presentation. The plugin never receives the unsubscribe target and cannot
// declare or trigger the action itself.
func finishMessageCard(c resultCard, source *db.Message) resultCard {
	if source == nil {
		return c
	}
	c.canUnsubscribe = messageUnsubscribeTarget(*source) != ""
	unsubscribeFact := -1
	typeValue, priorityValue := "", ""
	for i, fact := range c.facts {
		switch strings.ToLower(strings.TrimSpace(fact.Label)) {
		case "type":
			typeValue = sanitizePluginLine(fact.Value)
		case "priority":
			priorityValue = sanitizePluginLine(fact.Value)
		case "unsubscribe":
			unsubscribeFact = i
		}
	}
	if unsubscribeFact < 0 {
		return c
	}
	if c.canUnsubscribe {
		c.facts[unsubscribeFact].Value = "Available — press u"
	} else {
		c.facts[unsubscribeFact].Value = "No unsubscribe option"
	}

	// Newsletter-style cards need only the verdict, reason, and action at a
	// glance. The complete presentation remains in Details.
	if typeValue != "" && priorityValue != "" {
		c.title = typeValue + " · " + strings.ToLower(priorityValue) + " priority"
		c.summary = ""
		c.confidence = ""
		facts := c.facts[:0]
		for _, fact := range c.facts {
			switch strings.ToLower(strings.TrimSpace(fact.Label)) {
			case "type", "priority", "method":
				continue
			default:
				facts = append(facts, fact)
			}
		}
		c.facts = facts
	}
	return c
}

// displayOnlyData is the response data other than annotations and
// presentation, formatted for reading, or "".
func displayOnlyData(data json.RawMessage) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil {
		return ""
	}
	delete(obj, "annotations")
	delete(obj, "presentation")
	if len(obj) == 0 {
		return ""
	}
	raw, _ := json.Marshal(obj)
	return formatPluginData(raw)
}

// errorCard explains a failed run in plain words; the raw error is in Details.
func errorCard(name string, err error) resultCard {
	text := err.Error()
	c := resultCard{title: name + " could not analyze this message", status: plugin.StatusDanger}
	switch {
	case strings.Contains(text, "timed out"):
		c.title, c.summary = name+" timed out", "The plugin took too long to respond."
	case errors.Is(err, plugin.ErrPermissionDenied):
		c.summary = "This plugin isn't allowed to read message details."
	case strings.Contains(text, "exit status") || strings.Contains(text, "signal:") || strings.Contains(text, " failed:"):
		c.summary = "The plugin exited unexpectedly."
	case strings.Contains(text, "response") || strings.Contains(text, "malformed"):
		c.summary = "The plugin sent a reply TideMail couldn't read."
	case strings.Contains(text, "context canceled"):
		c.title, c.summary, c.status = name+" was cancelled", "The run stopped before it finished.", plugin.StatusNeutral
	case strings.Contains(text, "no such file") || strings.Contains(text, "permission denied"):
		c.summary = "The plugin program could not be started."
	default:
		c.summary = "Something went wrong while running the plugin."
	}
	return c
}

// messageDetails is the raw, developer-facing record of a run.
func (m Model) messageDetails(msg pluginResultMsg) []string {
	lines := m.pluginIdentityLines(msg.PluginID)
	lines = append(lines, "Message: "+sanitizePluginLine(msg.Subject), "")
	if msg.Err != nil {
		lines = append(lines, "Error:")
		for _, l := range m.displayPluginErrorLines(msg.Err) {
			lines = append(lines, "  "+l)
		}
		return lines
	}
	res := msg.Result
	if len(res.Annotations) == 0 {
		lines = append(lines, "Annotations: none")
	} else {
		lines = append(lines, "Annotations:")
		for _, a := range res.Annotations {
			conf := ""
			if a.Confidence != nil {
				conf = fmt.Sprintf("  %.2f", *a.Confidence)
			}
			lines = append(lines, "  "+sanitizePluginLine(a.Key)+" = "+sanitizePluginLine(a.Value)+conf)
		}
	}
	lines = append(lines, "")
	if note := annotationNote(res); note != "" {
		lines = append(lines, "Storage: "+strings.TrimPrefix(note, "annotations: "))
	}
	if msg.Compared {
		changed := "no"
		if msg.Changed {
			changed = "yes"
		}
		lines = append(lines, "Classification changed: "+changed)
	}
	if res.PresentationErr != nil {
		lines = append(lines, "Presentation ignored: "+sanitizePluginLine(res.PresentationErr.Error()))
	}
	if p := res.Presentation; p != nil {
		lines = append(lines, "", "Presentation:", "  Title: "+sanitizePluginLine(p.Title))
		if p.Summary != "" {
			lines = append(lines, "  Summary: "+sanitizePluginLine(p.Summary))
		}
		if p.Status != "" {
			lines = append(lines, "  Status: "+titleWord(p.Status))
		}
		if p.Confidence != "" {
			lines = append(lines, "  Confidence: "+titleWord(p.Confidence))
		}
		for _, fact := range p.Facts {
			lines = append(lines, "  "+sanitizePluginLine(fact.Label)+": "+sanitizePluginLine(fact.Value))
		}
	}
	lines = append(lines, fmt.Sprintf("Time: %v", msg.Elapsed.Round(1e6)))
	if data := formatPluginData(res.Response.Data); data != "" {
		lines = append(lines, "", "Response data:")
		lines = append(lines, strings.Split(data, "\n")...)
	}
	return lines
}

func (m Model) pluginIdentityLines(pluginID string) []string {
	lines := []string{"Plugin: " + m.pluginDisplayName(pluginID), "ID: " + sanitizePluginLine(pluginID)}
	if p, ok := m.plugins.manager.Plugin(pluginID); ok && p.Manifest.Version != "" {
		lines = append(lines, "Version: "+sanitizePluginLine(p.Manifest.Version))
	}
	return lines
}

// ── Rendering ────────────────────────────────────────────────────────────────

// cardStatusColor maps a status to the theme.
func (m Model) cardStatusColor(status string, chrome managerChrome) lipgloss.Color {
	switch status {
	case plugin.StatusSuccess:
		return accentReadableOn(chrome.successFg, chrome.baseBg, 3)
	case plugin.StatusWarning:
		return accentReadableOn(m.tagColorFor(tagKeyImportant).bg, chrome.baseBg, 3)
	case plugin.StatusDanger:
		return accentReadableOn(chrome.errorFg, chrome.baseBg, 3)
	case plugin.StatusNeutral:
		return chrome.muted
	}
	return accentReadableOn(chrome.accent, chrome.baseBg, 3)
}

// cardLines renders a card in the result window, whose border carries the
// plugin name.
func (m Model) cardLines(c resultCard, width int, chrome managerChrome) []string {
	width = max(20, width)
	plain := chrome.plainUI
	style := func(fg lipgloss.Color) lipgloss.Style {
		if plain {
			return lipgloss.NewStyle()
		}
		return lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg)
	}
	text, muted := style(chrome.text), style(chrome.muted)
	statusFg := m.cardStatusColor(c.status, chrome)
	wrap := func(s string, st lipgloss.Style, indent string) []string {
		var out []string
		for _, l := range strings.Split(ansi.Wrap(s, max(1, width-len(indent)), ""), "\n") {
			out = append(out, st.Render(indent+l))
		}
		return out
	}
	var lines []string
	marker := "● "
	if plain {
		marker = "* "
	}
	lines = append(lines, style(statusFg).Render(marker)+style(chrome.text).Bold(true).Render(truncate(sanitizePluginLine(c.title), width-2)))
	if c.summary != "" {
		lines = append(lines, "")
		for _, para := range strings.Split(sanitizePluginText(c.summary), "\n") {
			lines = append(lines, wrap(para, text, "")...)
		}
	}
	factBlock := func(facts []plugin.Fact) {
		labelW := 0
		for _, f := range facts {
			labelW = max(labelW, lipgloss.Width(sanitizePluginLine(f.Label)))
		}
		labelW = min(labelW+2, width/2)
		for _, f := range facts {
			label := padRight(truncate(sanitizePluginLine(f.Label), labelW-2), labelW)
			value := wrap(sanitizePluginLine(f.Value), text, "")
			for i, v := range value {
				if i == 0 {
					lines = append(lines, muted.Render(label)+v)
				} else {
					lines = append(lines, muted.Render(strings.Repeat(" ", labelW))+v)
				}
			}
		}
	}
	if len(c.facts) > 0 {
		lines = append(lines, "")
		factBlock(c.facts)
	}
	for _, s := range c.sections {
		lines = append(lines, "", style(chrome.text).Bold(true).Render(s.title))
		factBlock(s.facts)
	}
	if len(c.reasons) > 0 {
		lines = append(lines, "", style(chrome.text).Bold(true).Render("Why"))
		bullet := "• "
		if plain {
			bullet = "- "
		}
		for _, r := range c.reasons {
			for i, l := range strings.Split(ansi.Wrap(sanitizePluginLine(r), max(1, width-2), ""), "\n") {
				prefix := "  "
				if i == 0 {
					prefix = bullet
				}
				lines = append(lines, style(statusFg).Render(prefix)+text.Render(l))
			}
		}
	}
	if c.confidence != "" {
		lines = append(lines, "")
		factBlock([]plugin.Fact{{Label: "Confidence", Value: c.confidence}})
	}
	return lines
}
