package attention

import (
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
)

func TestEffectiveClassificationPrecedence(t *testing.T) {
	anns := []Annotation{{Key: db.ClassificationNeedsReply, Value: "true"}, {Key: db.ClassificationUrgency, Value: "high"}, {Key: db.ClassificationImportance, Value: "high"}, {Key: db.ClassificationCategory, Value: "shipping"}}
	c := Compute(anns, nil)
	if !c.NeedsReply || !c.Urgent || !c.Important || c.Category != "shipping" {
		t.Fatalf("plugin = %+v", c)
	}
	c = Compute(anns, map[string]Override{
		db.ClassificationNeedsReply: {Key: db.ClassificationNeedsReply, Value: "false"},
		db.ClassificationUrgency:    {Key: db.ClassificationUrgency, Value: "normal"},
		db.ClassificationImportance: {Key: db.ClassificationImportance, Value: "normal"},
		db.ClassificationCategory:   {Key: db.ClassificationCategory, Value: "billing"},
	})
	if c.NeedsReply || c.Urgent || c.Important || c.Category != "billing" {
		t.Fatalf("user = %+v", c)
	}
	if c.NeedsReplySource != SourceUser || c.CategorySource != SourceUser {
		t.Fatalf("sources = %+v", c)
	}
}

func TestEffectiveUserCanAddAndReset(t *testing.T) {
	c := Compute(nil, map[string]Override{
		db.ClassificationNeedsReply: {Value: "true"},
		db.ClassificationCategory:   {Value: db.ClassificationNone},
	})
	if !c.NeedsReply || c.Category != "" {
		t.Fatalf("user add/suppress = %+v", c)
	}
	c = Compute([]Annotation{{Key: db.ClassificationCategory, Value: "shipping"}}, map[string]Override{db.ClassificationCategory: {Value: db.ClassificationPlugin}})
	if c.Category != "shipping" || c.CategorySource != SourcePlugin {
		t.Fatalf("reset = %+v", c)
	}
}
