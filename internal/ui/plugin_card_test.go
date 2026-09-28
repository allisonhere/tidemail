package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// resultText is a result's card (the default view) as plain text, then its
// details.
func resultText(m Model, r *pluginResult) string {
	chrome := newManagerChrome(90, m.styles.Theme, true)
	var parts []string
	if r.card != nil {
		parts = append(parts, ansi.Strip(strings.Join(m.cardLines(*r.card, 80, chrome), "\n")))
	}
	return strings.Join(append(parts, r.details...), "\n")
}

// hasFact reports a card row "label ... value".
func hasFact(text, label, value string) bool {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if rest, ok := strings.CutPrefix(l, label+" "); ok && strings.TrimSpace(rest) == value {
			return true
		}
	}
	return false
}

func conf(v float64) *float64 { return &v }

// The spec's acceptance message: annotations only, no presentation.
func acceptanceResult() pluginResultMsg {
	return pluginResultMsg{PluginID: "unsubscribe-plus", Subject: "The Windows clipboard just got a big upgrade",
		Compared: true, Changed: true,
		Result: plugin.MessageMetadataResult{Outcome: plugin.AnnotationsStored, Annotations: []plugin.Annotation{
			{Key: "category", Value: "newsletter", Confidence: conf(0.85)},
			{Key: "newsletter", Value: "true", Confidence: conf(0.85)},
			{Key: "sender_value", Value: "low", Confidence: conf(0.85)},
		}}}
}

func TestFallbackCardIsFriendly(t *testing.T) {
	m, _ := newMailboxListModel(1, 1)
	msg := acceptanceResult()
	card := messageCard("Unsubscribe+", msg, nil)
	r := &pluginResult{card: &card, details: m.messageDetails(msg)}
	text := resultText(m, &pluginResult{card: &card})
	for label, value := range map[string]string{"Type": "Newsletter", "Priority": "Low", "Newsletter": "Yes", "Confidence": "High"} {
		if !hasFact(text, label, value) {
			t.Fatalf("card lacks %s %s:\n%s", label, value, text)
		}
	}
	for _, jargon := range []string{"category", "sender_value", "newsletter=", "0.85", "Key:", "Value:", "Confidence:"} {
		if strings.Contains(text, jargon) {
			t.Fatalf("default card shows %q:\n%s", jargon, text)
		}
	}
	if !strings.Contains(text, "Classification updated") {
		t.Fatalf("a changed reclassify says so:\n%s", text)
	}
	details := strings.Join(r.details, "\n")
	for _, raw := range []string{"category = newsletter  0.85", "sender_value = low  0.85", "Classification changed: yes", "ID: unsubscribe-plus"} {
		if !strings.Contains(details, raw) {
			t.Fatalf("details lack %q:\n%s", raw, details)
		}
	}
}

func TestPresentationCardWins(t *testing.T) {
	m, _ := newMailboxListModel(1, 1)
	msg := acceptanceResult()
	msg.Result.Presentation = &plugin.Presentation{Title: "This looks like a newsletter", Summary: "Automated list mail with low expected priority.",
		Status: plugin.StatusInfo, Confidence: "high", Facts: []plugin.Fact{{Label: "Type", Value: "Newsletter"}, {Label: "Unsubscribe", Value: "Not found"}},
		Reasons: []string{"Matches newsletter/list-mail patterns"}}
	card := messageCard("Unsubscribe+", msg, nil)
	text := resultText(m, &pluginResult{card: &card})
	for _, want := range []string{"This looks like a newsletter", "Automated list mail", "Why", "Matches newsletter/list-mail patterns"} {
		if !strings.Contains(text, want) {
			t.Fatalf("card lacks %q:\n%s", want, text)
		}
	}
	if !hasFact(text, "Unsubscribe", "Not found") || !hasFact(text, "Confidence", "High") {
		t.Fatalf("facts:\n%s", text)
	}
}

func TestMalformedPresentationFallsBack(t *testing.T) {
	m, _ := newMailboxListModel(1, 1)
	msg := acceptanceResult()
	msg.Result.PresentationErr = errors.New("presentation title contains control or escape characters")
	card := messageCard("Unsubscribe+", msg, nil)
	if !hasFact(resultText(m, &pluginResult{card: &card}), "Type", "Newsletter") {
		t.Fatal("annotations still produce a card")
	}
	if !strings.Contains(strings.Join(m.messageDetails(msg), "\n"), "Presentation ignored: presentation title contains control") {
		t.Fatal("the warning belongs in Details")
	}
}

func TestNoChangesCard(t *testing.T) {
	msg := pluginResultMsg{PluginID: "p", Result: plugin.MessageMetadataResult{Outcome: plugin.AnnotationsStored,
		Response: plugin.Response{Data: json.RawMessage(`{"annotations":[]}`)}}}
	card := messageCard("Unsubscribe+", msg, nil)
	if card.title != "No changes" || card.summary != "Unsubscribe+ analyzed this message and found no new classification." || len(card.facts) != 0 {
		t.Fatalf("card = %+v", card)
	}
}

func TestErrorCardsAreFriendlyAndSecretFree(t *testing.T) {
	cases := map[string]string{
		`plugin "u" timed out after 5s`:                      "The plugin took too long to respond.",
		`plugin "u" failed: exit status 1: stderr: TOKEN123`: "The plugin exited unexpectedly.",
		`plugin "u": malformed response: invalid character`:  "The plugin sent a reply TideMail couldn't read.",
		`something odd`: "Something went wrong while running the plugin.",
	}
	m, _ := newMailboxListModel(1, 1)
	for errText, want := range cases {
		card := errorCard("Unsubscribe+", errors.New(errText))
		text := resultText(m, &pluginResult{card: &card})
		if !strings.Contains(text, want) || strings.Contains(text, "TOKEN123") || strings.Contains(text, "stderr") {
			t.Errorf("%s → %q", errText, text)
		}
	}
	if card := errorCard("U", fmt.Errorf("x: %w", plugin.ErrPermissionDenied)); !strings.Contains(card.summary, "isn't allowed") {
		t.Fatalf("permission card = %+v", card)
	}
}

func TestResultCardKeys(t *testing.T) {
	m, _ := newMailboxListModel(1, 1)
	msg := acceptanceResult()
	card := messageCard("Unsubscribe+", msg, nil)
	m.plugins.result = &pluginResult{pluginName: "Unsubscribe+", card: &card, details: m.messageDetails(msg)}
	m.overlay = overlayPluginResult
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Unsubscribe+") || !strings.Contains(view, "d details") || strings.Contains(view, "sender_value") {
		t.Fatalf("card view:\n%s", view)
	}
	next, _ := m.handlePluginKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = next.(Model)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "sender_value = low") || !strings.Contains(view, "d summary") {
		t.Fatalf("details view:\n%s", view)
	}
	next, _ = m.handlePluginKey(tea.KeyMsg{Type: tea.KeyEnter})
	if next.(Model).overlay != overlayNone {
		t.Fatal("enter closes the card")
	}
}

func TestNewsletterCardUsesTideMailUnsubscribeState(t *testing.T) {
	m, _ := newMailboxListModel(1, 1)
	msg := acceptanceResult()
	msg.Result.Presentation = &plugin.Presentation{
		Title: "This looks like a newsletter", Summary: "Automated list mail with low expected priority.",
		Status: plugin.StatusInfo, Confidence: "high",
		Facts:   []plugin.Fact{{Label: "Type", Value: "Newsletter"}, {Label: "Priority", Value: "Low"}, {Label: "Unsubscribe", Value: "Not checked"}},
		Reasons: []string{"Sent from a newsletter address"},
	}
	source := db.Message{ID: msg.MessageID, Headers: "List-Unsubscribe\n<https://list.example.com/leave>\n"}
	card := messageCard("Unsubscribe+", msg, &source)
	text := resultText(m, &pluginResult{card: &card})
	for _, want := range []string{"Newsletter · low priority", "Sent from a newsletter address", "Available — press u"} {
		if !strings.Contains(text, want) {
			t.Fatalf("card lacks %q:\n%s", want, text)
		}
	}
	for _, moved := range []string{"Type", "Priority", "Confidence", "Not checked", "Automated list mail"} {
		if strings.Contains(text, moved) {
			t.Fatalf("compact card still shows %q:\n%s", moved, text)
		}
	}
	if !card.canUnsubscribe {
		t.Fatal("TideMail found a target but the card action is disabled")
	}
	details := strings.Join(m.messageDetails(msg), "\n")
	for _, want := range []string{"Type: Newsletter", "Priority: Low", "Confidence: High"} {
		if !strings.Contains(details, want) {
			t.Fatalf("Details did not retain %q:\n%s", want, details)
		}
	}

	without := source
	without.Headers = ""
	card = messageCard("Unsubscribe+", msg, &without)
	text = resultText(m, &pluginResult{card: &card})
	if !strings.Contains(text, "No unsubscribe option") || strings.Contains(text, "Not checked") || card.canUnsubscribe {
		t.Fatalf("no-target card is inaccurate:\n%s", text)
	}
}

func TestPluginCardUnsubscribeUsesConfirmedTideMailAction(t *testing.T) {
	m, _ := newMailboxListModel(1, 1)
	m.keys = DefaultKeys
	msg := acceptanceResult()
	source := db.Message{ID: msg.MessageID, Headers: "List-Unsubscribe\n<https://list.example.com/leave>\n"}
	card := messageCard("Unsubscribe+", msg, &source)
	m.plugins.result = &pluginResult{pluginName: "Unsubscribe+", card: &card, message: &source}
	m.overlay = overlayPluginResult
	if view := ansi.Strip(m.View()); !strings.Contains(view, "u unsubscribe") {
		t.Fatalf("card does not advertise unsubscribe:\n%s", view)
	}

	next, cmd := m.handlePluginKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	m = next.(Model)
	if cmd != nil || m.overlay != overlayUnsubscribeConfirm || m.pendingUnsubscribe.ID != source.ID {
		t.Fatalf("u must open TideMail confirmation only: overlay=%v pending=%d cmd=%v", m.overlay, m.pendingUnsubscribe.ID, cmd)
	}
}

func TestBulkFoundLabels(t *testing.T) {
	anns := []db.PluginAnnotation{{PluginID: "u", Key: "category", Value: "newsletter"}, {PluginID: "smart", Key: "category", Value: "github"}}
	if got := foundLabels(anns, "u"); len(got) != 1 || got[0] != "Newsletter" {
		t.Fatalf("found = %v", got)
	}
	if got := foundLabels([]db.PluginAnnotation{{PluginID: "u", Key: "automated_sender", Value: "true"}}, "u"); len(got) != 1 || got[0] != "Automated sender" {
		t.Fatalf("found = %v", got)
	}
}

func TestRunPluginKeyAndPicker(t *testing.T) {
	root := t.TempDir()
	installTestPlugin(t, root, "hello", true)
	m := newPluginModel(t, root)
	m.focused = paneMessages
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = next.(Model)
	if m.overlay != overlayPluginPicker || len(m.plugins.pickerMetas) != 1 {
		t.Fatalf("p should open the picker for the current message: overlay %v, %d targets", m.overlay, len(m.plugins.pickerMetas))
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Plugin hello") || strings.Contains(view, "(hello)") {
		t.Fatalf("picker shows names, not IDs:\n%s", view)
	}
	// Selection: every selected message, one picker.
	m.overlay = overlayNone
	for _, msg := range m.filteredMessages[:3] {
		m.selectedMessages[msg.ID] = true
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = next.(Model)
	if m.overlay != overlayPluginPicker || len(m.plugins.pickerMetas) != 3 {
		t.Fatalf("selection: overlay %v, %d targets", m.overlay, len(m.plugins.pickerMetas))
	}
	var label string
	for _, item := range m.mainCommandItems() {
		if item.id == "plugin-run" {
			label = item.label
		}
	}
	if label != "Run plugin on 3 selected messages…" {
		t.Fatalf("palette label = %q", label)
	}
}

func TestRunLastPluginAgain(t *testing.T) {
	root := t.TempDir()
	installTestPlugin(t, root, "hello", true)
	m := newPluginModel(t, root)
	if ids := strings.Join(commandIDs(m), " "); strings.Contains(ids, "plugin-run-last") {
		t.Fatal("no last plugin yet")
	}
	m.plugins.last = "hello"
	var label string
	for _, item := range m.mainCommandItems() {
		if item.id == "plugin-run-last" {
			label = item.label
		}
	}
	if label != "Run Plugin hello again on current message…" {
		t.Fatalf("label = %q", label)
	}
	next, cmd := m.executePluginCommand("plugin-run-last")
	m = next.(Model)
	if m.overlay == overlayPluginPicker || m.plugins.running != "hello" || cmd == nil {
		t.Fatalf("run again skips the picker: overlay %v running %q", m.overlay, m.plugins.running)
	}
}
