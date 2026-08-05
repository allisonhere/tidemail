package filter

import (
	"encoding/json"
	"fmt"
	"strings"
)

var validFields = map[string]bool{
	FieldFrom: true, FieldTo: true, FieldCC: true,
	FieldSubject: true, FieldBody: true, FieldHasAttachment: true,
}

var validOps = map[string]bool{
	OpContains: true, OpNotContains: true, OpEquals: true,
	OpStartsWith: true, OpEndsWith: true, OpIsTrue: true,
}

var validActions = map[string]bool{
	ActionMove: true, ActionMarkRead: true, ActionArchive: true,
	ActionDelete: true, ActionSpam: true,
}

// Parse decodes a rule from JSON. It tolerates surrounding prose or Markdown code
// fences (LLMs often wrap JSON), then validates the result.
func Parse(text string, folders []string) (Rule, error) {
	raw := extractJSON(text)
	if raw == "" {
		return Rule{}, fmt.Errorf("no JSON object found in response")
	}
	var r Rule
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return Rule{}, fmt.Errorf("invalid filter JSON: %w", err)
	}
	if r.Match == "" {
		r.Match = MatchAll
	}
	if r.Action.Type == ActionMove {
		r.Action.Target = strings.TrimSpace(r.Action.Target)
	}
	if err := Validate(r, folders); err != nil {
		return Rule{}, err
	}
	return r, nil
}

// extractJSON returns the substring from the first '{' to the last '}', which
// strips code fences and any leading/trailing commentary.
func extractJSON(text string) string {
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end < 0 || end < start {
		return ""
	}
	return text[start : end+1]
}

// Validate checks that a rule uses known fields/operators/actions and that a
// "move" action has a destination. Move targets may name new folders; TideMail
// creates them in the rule's account scope when the rule is saved.
func Validate(r Rule, _ []string) error {
	switch r.Match {
	case MatchAll, MatchAny:
	default:
		return fmt.Errorf("invalid match mode %q", r.Match)
	}
	if len(r.Conditions) == 0 {
		return fmt.Errorf("rule has no conditions")
	}
	for _, c := range r.Conditions {
		if !validFields[c.Field] {
			return fmt.Errorf("unknown field %q", c.Field)
		}
		if !validOps[c.Op] {
			return fmt.Errorf("unknown operator %q", c.Op)
		}
		if c.Field == FieldHasAttachment {
			continue
		}
		if strings.TrimSpace(c.Value) == "" {
			return fmt.Errorf("condition on %q has an empty value", c.Field)
		}
	}
	if !validActions[r.Action.Type] {
		return fmt.Errorf("unknown action %q", r.Action.Type)
	}
	if r.Action.Type == ActionMove {
		if strings.TrimSpace(r.Action.Target) == "" {
			return fmt.Errorf("move action requires a target folder")
		}
	}
	return nil
}

// Summary renders a short human-readable description of the rule for the UI.
func (r Rule) Summary() string {
	parts := make([]string, 0, len(r.Conditions))
	for _, c := range r.Conditions {
		if c.Field == FieldHasAttachment {
			parts = append(parts, "has attachment")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s %q", c.Field, strings.ReplaceAll(c.Op, "_", " "), c.Value))
	}
	joiner := " AND "
	if r.Match == MatchAny {
		joiner = " OR "
	}
	act := r.Action.Type
	if r.Action.Type == ActionMove && r.Action.Target != "" {
		act = "move → " + r.Action.Target
	}
	return strings.Join(parts, joiner) + "  ⇒  " + act
}
