package filter

import "testing"

import "github.com/allisonhere/tide/internal/db"

func TestRuleMatches(t *testing.T) {
	msg := db.Message{
		From:          "Substack <newsletter@substack.com>",
		To:            "me@example.com",
		Subject:       "Your weekly digest",
		BodyText:      "Read more at the link.",
		HasAttachment: false,
	}

	cases := []struct {
		name string
		rule Rule
		want bool
	}{
		{"from contains", Rule{Match: MatchAll, Conditions: []Condition{{FieldFrom, OpContains, "substack.com"}}}, true},
		{"from not contains", Rule{Match: MatchAll, Conditions: []Condition{{FieldFrom, OpNotContains, "gmail.com"}}}, true},
		{"subject equals (ci)", Rule{Match: MatchAll, Conditions: []Condition{{FieldSubject, OpEquals, "your weekly DIGEST"}}}, true},
		{"subject starts_with", Rule{Match: MatchAll, Conditions: []Condition{{FieldSubject, OpStartsWith, "Your"}}}, true},
		{"body ends_with", Rule{Match: MatchAll, Conditions: []Condition{{FieldBody, OpEndsWith, "link."}}}, true},
		{"all fails one", Rule{Match: MatchAll, Conditions: []Condition{{FieldFrom, OpContains, "substack"}, {FieldSubject, OpContains, "invoice"}}}, false},
		{"any passes one", Rule{Match: MatchAny, Conditions: []Condition{{FieldFrom, OpContains, "nope"}, {FieldSubject, OpContains, "digest"}}}, true},
		{"has_attachment is_true (none)", Rule{Match: MatchAll, Conditions: []Condition{{FieldHasAttachment, OpIsTrue, ""}}}, false},
		{"no conditions never matches", Rule{Match: MatchAll}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rule.Matches(msg); got != tc.want {
				t.Fatalf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseStripsCodeFences(t *testing.T) {
	resp := "Here is your filter:\n```json\n{\"match\":\"all\",\"conditions\":[{\"field\":\"from\",\"op\":\"contains\",\"value\":\"substack.com\"}],\"action\":{\"type\":\"move\",\"target\":\"Reading\"}}\n```\n"
	r, err := Parse(resp, []string{"INBOX", "Reading"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r.Action.Type != ActionMove || r.Action.Target != "Reading" {
		t.Fatalf("unexpected action: %+v", r.Action)
	}
	if len(r.Conditions) != 1 || r.Conditions[0].Field != FieldFrom {
		t.Fatalf("unexpected conditions: %+v", r.Conditions)
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	bad := []Rule{
		{Match: "sometimes", Conditions: []Condition{{FieldFrom, OpContains, "x"}}, Action: Action{Type: ActionDelete}},
		{Match: MatchAll, Conditions: []Condition{{"sender", OpContains, "x"}}, Action: Action{Type: ActionDelete}},
		{Match: MatchAll, Conditions: []Condition{{FieldFrom, "matches", "x"}}, Action: Action{Type: ActionDelete}},
		{Match: MatchAll, Conditions: []Condition{{FieldFrom, OpContains, "x"}}, Action: Action{Type: "forward"}},
		{Match: MatchAll, Conditions: []Condition{{FieldFrom, OpContains, "x"}}, Action: Action{Type: ActionMove, Target: "Nope"}},
		{Match: MatchAll, Conditions: nil, Action: Action{Type: ActionDelete}},
	}
	for i, r := range bad {
		if err := Validate(r, []string{"INBOX", "Reading"}); err == nil {
			t.Fatalf("case %d: expected validation error for %+v", i, r)
		}
	}
}
