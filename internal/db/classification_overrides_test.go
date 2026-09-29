package db

import "testing"

func TestClassificationOverridesSetReplaceDeleteAndBatch(t *testing.T) {
	d, _, ids := newAnnotationTestDB(t, 2)
	if err := d.SetClassificationOverride(ids[0], ClassificationNeedsReply, "true"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetClassificationOverride(ids[0], ClassificationNeedsReply, "false"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetClassificationOverride(ids[0], ClassificationCategory, "Billing"); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetClassificationOverrides(ids[0])
	if err != nil || got[ClassificationNeedsReply].Value != "false" || got[ClassificationCategory].Value != "billing" {
		t.Fatalf("overrides = %+v, %v", got, err)
	}
	all, err := d.GetClassificationOverridesForMessages(ids)
	if err != nil || len(all[ids[0]]) != 2 || len(all[ids[1]]) != 0 {
		t.Fatalf("batch = %+v, %v", all, err)
	}
	if err := d.DeleteClassificationOverride(ids[0], ClassificationNeedsReply); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteClassificationOverride(ids[0], ClassificationNeedsReply); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteClassificationOverrides(ids[0]); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.GetClassificationOverrides(ids[0]); len(got) != 0 {
		t.Fatalf("reset = %+v", got)
	}
}

func TestClassificationOverrideValidation(t *testing.T) {
	d, _, ids := newAnnotationTestDB(t, 1)
	valid := [][2]string{{ClassificationNeedsReply, "false"}, {ClassificationUrgency, "normal"}, {ClassificationImportance, "high"}, {ClassificationCategory, "custom_tag"}, {ClassificationCategory, ClassificationNone}, {ClassificationCategory, ClassificationPlugin}}
	for _, pair := range valid {
		if err := d.SetClassificationOverride(ids[0], pair[0], pair[1]); err != nil {
			t.Errorf("valid %v: %v", pair, err)
		}
	}
	invalid := [][2]string{{"mood", "high"}, {ClassificationNeedsReply, "maybe"}, {ClassificationUrgency, "low"}, {ClassificationImportance, "low"}, {ClassificationCategory, "bad value"}, {ClassificationCategory, "way-too-long-category"}}
	for _, pair := range invalid {
		if err := d.SetClassificationOverride(ids[0], pair[0], pair[1]); err == nil {
			t.Errorf("invalid %v accepted", pair)
		}
	}
}

func TestClassificationOverridesCascadeWithMessage(t *testing.T) {
	d, _, ids := newAnnotationTestDB(t, 1)
	if err := d.SetClassificationOverride(ids[0], ClassificationCategory, "billing"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM messages WHERE id = ?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetClassificationOverrides(ids[0])
	if err != nil || len(got) != 0 {
		t.Fatalf("cascade = %+v, %v", got, err)
	}
}

func TestNeedsYouHonorsClassificationOverrides(t *testing.T) {
	a := newAttentionDB(t)
	reply := a.add(a.inbox, "reply", 1, "p needs_reply true")
	other := a.add(a.inbox, "other", 2, "p needs_reply true", "q urgency high")
	if err := a.SetClassificationOverride(reply, ClassificationNeedsReply, "false"); err != nil {
		t.Fatal(err)
	}
	if got := a.subjects(false); len(got) != 1 || got[0] != "other" {
		t.Fatalf("suppressed = %v", got)
	}
	if err := a.SetClassificationOverride(reply, ClassificationNeedsReply, "true"); err != nil {
		t.Fatal(err)
	}
	if got := a.count(); got != 2 {
		t.Fatalf("added = %d", got)
	}
	if err := a.SetClassificationOverride(other, ClassificationUrgency, "normal"); err != nil {
		t.Fatal(err)
	}
	if got := a.subjects(false); len(got) != 2 || got[0] != "reply" {
		t.Fatalf("ranking = %v", got)
	}
}
