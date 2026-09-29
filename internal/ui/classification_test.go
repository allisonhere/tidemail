package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func TestEffectiveTagsUseUserClassification(t *testing.T) {
	m, msgs := newMailboxListModel(7, 1)
	id := msgs[0].ID
	m.plugins.annotations = map[int64][]db.PluginAnnotation{id: {
		cacheAnn("smart", "needs_reply", "true"),
		cacheAnn("smart", "urgency", "high"),
		cacheAnn("smart", "category", "shipping"),
	}}
	m.plugins.overrides = map[int64]map[string]db.ClassificationOverride{id: {
		db.ClassificationNeedsReply: {MessageID: id, Key: db.ClassificationNeedsReply, Value: "false"},
		db.ClassificationUrgency:    {MessageID: id, Key: db.ClassificationUrgency, Value: "normal"},
		db.ClassificationCategory:   {MessageID: id, Key: db.ClassificationCategory, Value: "billing"},
	}}
	tags := m.annotationTags(id)
	if len(tags) != 1 || tags[0].kind != tagCategory || tags[0].category != "billing" {
		t.Fatalf("effective tags = %+v", tags)
	}
}

func TestClassificationCorrectionCommandAndDetails(t *testing.T) {
	m, msgs := newMailboxListModel(7, 1)
	m.focused = paneMessages
	m.plugins.annotations = map[int64][]db.PluginAnnotation{msgs[0].ID: {cacheAnn("smart", "category", "shipping")}}
	m.plugins.overrides = map[int64]map[string]db.ClassificationOverride{}
	ids := []string{}
	for _, item := range m.mainCommandItems() {
		ids = append(ids, item.id)
	}
	if !containsString(ids, "classification-correct") {
		t.Fatalf("commands = %v", ids)
	}
	m.plugins.annotationsFor = msgs[0].ID
	m.overlay = overlayPluginAnnotations
	if !strings.Contains(m.View(), "Effective") {
		t.Fatal("annotation details should show effective classification")
	}
}

func TestEditTagsShortcutOpensCurrentMessage(t *testing.T) {
	m, msgs := newMailboxListModel(7, 2)
	m.focused = paneMessages
	m.messageCursor = 1
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m = next.(Model)
	if m.overlay != overlayClassification || m.classification.messageID != msgs[1].ID {
		t.Fatalf("edit tags opened overlay %v for message %d, want message %d", m.overlay, m.classification.messageID, msgs[1].ID)
	}
}

func TestEditTagsShortcutFromContent(t *testing.T) {
	m, msgs := newMailboxListModel(7, 2)
	m.focused = paneContent
	m.contentMessageID = msgs[1].ID
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m = next.(Model)
	if m.overlay != overlayClassification || m.classification.messageID != msgs[1].ID {
		t.Fatalf("edit tags opened overlay %v for message %d, want message %d", m.overlay, m.classification.messageID, msgs[1].ID)
	}
}

func TestEditTagsPaletteSearch(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	m.focused = paneMessages
	m.commandInput.SetValue("tags")
	if items := m.filteredCommandItems(); len(items) != 1 || items[0].id != "classification-correct" {
		t.Fatalf("tags search = %+v", items)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
