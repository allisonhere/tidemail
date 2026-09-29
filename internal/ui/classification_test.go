package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
