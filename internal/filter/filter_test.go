package filter

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
)

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
		{"has_attachment is_true false means no attachment", Rule{Match: MatchAll, Conditions: []Condition{{FieldHasAttachment, OpIsTrue, "false"}}}, true},
		{"has_attachment equals false means no attachment", Rule{Match: MatchAll, Conditions: []Condition{{FieldHasAttachment, OpEquals, "false"}}}, true},
		{"has_attachment equals true means has attachment (no match here)", Rule{Match: MatchAll, Conditions: []Condition{{FieldHasAttachment, OpEquals, "true"}}}, false},
		// Regression for the operator-inversion bug: any op other than is_true/
		// equals must never match, in either direction — not invert to match
		// the opposite of what the rule says.
		{"has_attachment with an unsupported op never matches", Rule{Match: MatchAll, Conditions: []Condition{{FieldHasAttachment, OpNotContains, "true"}}}, false},
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

// TestHasAttachmentOperatorInversionRegression guards against the bug where
// has_attachment ignored its own operator and derived the answer from the
// literal value text instead: "not_contains true" and "is_true value:false"
// both matched a message WITH an attachment — the opposite of what either
// condition says.
func TestHasAttachmentOperatorInversionRegression(t *testing.T) {
	withAttachment := db.Message{HasAttachment: true}

	// "not_contains" is not a meaningful operator on a boolean field: Validate
	// must reject it outright, so such a rule can never be saved, and Matches
	// must never match on it either (defense in depth for an old, already-
	// saved rule).
	unsupported := Rule{
		Match:      MatchAll,
		Conditions: []Condition{{FieldHasAttachment, OpNotContains, "true"}},
		Action:     Action{Type: ActionDelete},
	}
	if err := Validate(unsupported, nil); err == nil {
		t.Fatal("expected Validate to reject not_contains on has_attachment")
	}
	if unsupported.Matches(withAttachment) {
		t.Fatal(`has_attachment not_contains "true" matched a message WITH an attachment: inverted`)
	}

	// "is_true" with value "false" is valid and means "no attachment" — it
	// must NOT match a message that has one (the original bug: is_true
	// ignored its value and always meant true).
	noAttachment := Rule{
		Match:      MatchAll,
		Conditions: []Condition{{FieldHasAttachment, OpIsTrue, "false"}},
		Action:     Action{Type: ActionDelete},
	}
	if err := Validate(noAttachment, nil); err != nil {
		t.Fatalf(`expected is_true "false" to validate: %v`, err)
	}
	if noAttachment.Matches(withAttachment) {
		t.Fatal(`has_attachment is_true "false" matched a message WITH an attachment: inverted`)
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

func TestParseAllowsNewMoveTarget(t *testing.T) {
	resp := `{"match":"all","conditions":[{"field":"from","op":"contains","value":"agentmail"}],"action":{"type":"move","target":" SystemStats "}}`
	r, err := Parse(resp, []string{"INBOX"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r.Action.Target != "SystemStats" {
		t.Fatalf("target = %q, want SystemStats", r.Action.Target)
	}
}

func TestPromptAllowsExplicitNewFolder(t *testing.T) {
	prompt := Prompt("move all AgentMail to a new folder named SystemStats", []string{"INBOX"})
	for _, want := range []string{"Existing folders: INBOX", "new folder", "SystemStats"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	bad := []Rule{
		{Match: "sometimes", Conditions: []Condition{{FieldFrom, OpContains, "x"}}, Action: Action{Type: ActionDelete}},
		{Match: MatchAll, Conditions: []Condition{{"sender", OpContains, "x"}}, Action: Action{Type: ActionDelete}},
		{Match: MatchAll, Conditions: []Condition{{FieldFrom, "matches", "x"}}, Action: Action{Type: ActionDelete}},
		{Match: MatchAll, Conditions: []Condition{{FieldFrom, OpContains, "x"}}, Action: Action{Type: "forward"}},
		{Match: MatchAll, Conditions: []Condition{{FieldFrom, OpContains, "x"}}, Action: Action{Type: ActionMove}},
		{Match: MatchAll, Conditions: nil, Action: Action{Type: ActionDelete}},
		{Match: MatchAll, Conditions: []Condition{{FieldHasAttachment, OpContains, "true"}}, Action: Action{Type: ActionDelete}},
		{Match: MatchAll, Conditions: []Condition{{FieldHasAttachment, OpNotContains, "true"}}, Action: Action{Type: ActionDelete}},
		{Match: MatchAll, Conditions: []Condition{{FieldHasAttachment, OpStartsWith, "true"}}, Action: Action{Type: ActionDelete}},
		{Match: MatchAll, Conditions: []Condition{{FieldHasAttachment, OpEquals, "yes"}}, Action: Action{Type: ActionDelete}},
		{Match: MatchAll, Conditions: []Condition{{FieldHasAttachment, OpIsTrue, "maybe"}}, Action: Action{Type: ActionDelete}},
	}
	for i, r := range bad {
		if err := Validate(r, []string{"INBOX", "Reading"}); err == nil {
			t.Fatalf("case %d: expected validation error for %+v", i, r)
		}
	}
}

func TestValidateAcceptsHasAttachmentBooleanForms(t *testing.T) {
	good := []Condition{
		{FieldHasAttachment, OpIsTrue, ""},
		{FieldHasAttachment, OpIsTrue, "true"},
		{FieldHasAttachment, OpIsTrue, "false"},
		{FieldHasAttachment, OpEquals, "true"},
		{FieldHasAttachment, OpEquals, "false"},
		{FieldHasAttachment, OpEquals, "TRUE"},
	}
	for _, c := range good {
		r := Rule{Match: MatchAll, Conditions: []Condition{c}, Action: Action{Type: ActionDelete}}
		if err := Validate(r, nil); err != nil {
			t.Fatalf("condition %+v: expected to validate, got %v", c, err)
		}
	}
}

func TestSummaryStatesHasAttachmentPolarity(t *testing.T) {
	cases := []struct {
		cond Condition
		want string
	}{
		{Condition{FieldHasAttachment, OpIsTrue, ""}, "has attachment"},
		{Condition{FieldHasAttachment, OpIsTrue, "true"}, "has attachment"},
		{Condition{FieldHasAttachment, OpIsTrue, "false"}, "has no attachment"},
		{Condition{FieldHasAttachment, OpEquals, "false"}, "has no attachment"},
	}
	for _, tc := range cases {
		r := Rule{Match: MatchAll, Conditions: []Condition{tc.cond}, Action: Action{Type: ActionDelete}}
		if got := r.Summary(); !strings.Contains(got, tc.want) {
			t.Fatalf("condition %+v: Summary() = %q, want it to contain %q", tc.cond, got, tc.want)
		}
	}
}
