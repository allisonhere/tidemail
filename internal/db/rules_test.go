package db

import "testing"

func TestRulesCRUD(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	id1, err := database.UpsertRule(RuleRecord{AccountID: 0, Priority: 2, Enabled: true, Name: "b", JSON: "{}"})
	if err != nil {
		t.Fatalf("insert 1: %v", err)
	}
	if _, err := database.UpsertRule(RuleRecord{AccountID: 0, Priority: 1, Enabled: true, Name: "a", JSON: "{}"}); err != nil {
		t.Fatalf("insert 2: %v", err)
	}

	rules, err := database.ListRules()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	// ordered by priority: "a" (1) before "b" (2)
	if rules[0].Name != "a" || rules[1].Name != "b" {
		t.Fatalf("unexpected order: %q, %q", rules[0].Name, rules[1].Name)
	}

	if err := database.SetRuleEnabled(id1, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := database.SetRulePriority(id1, 0); err != nil {
		t.Fatalf("reprioritize: %v", err)
	}
	rules, _ = database.ListRules()
	if rules[0].ID != id1 || rules[0].Enabled {
		t.Fatalf("expected id1 first and disabled, got %+v", rules[0])
	}

	if err := database.DeleteRule(id1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rules, _ = database.ListRules()
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule after delete, got %d", len(rules))
	}
}
