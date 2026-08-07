// Package filter implements deterministic mail rules: a Rule is a set of
// conditions plus an action. Rules are authored once (often generated from plain
// English by an AI provider) and then matched locally against messages with no
// per-message AI cost.
package filter

import (
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
)

// Match modes.
const (
	MatchAll = "all" // every condition must match
	MatchAny = "any" // any condition matches
)

// Condition fields.
const (
	FieldFrom          = "from"
	FieldTo            = "to"
	FieldCC            = "cc"
	FieldSubject       = "subject"
	FieldBody          = "body"
	FieldHasAttachment = "has_attachment"
)

// Condition operators.
const (
	OpContains    = "contains"
	OpNotContains = "not_contains"
	OpEquals      = "equals"
	OpStartsWith  = "starts_with"
	OpEndsWith    = "ends_with"
	OpIsTrue      = "is_true"
)

// Action types.
const (
	ActionMove     = "move"
	ActionMarkRead = "mark_read"
	ActionArchive  = "archive"
	ActionDelete   = "delete"
	ActionSpam     = "spam"
)

// Rule is a single filter: conditions combined per Match, then an Action.
type Rule struct {
	Match      string      `json:"match"`
	Conditions []Condition `json:"conditions"`
	Action     Action      `json:"action"`
}

// Condition is a single field/operator/value test.
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// Action is performed on messages the rule matches. Target is the destination
// folder for the "move" action; it is ignored by the others.
type Action struct {
	Type   string `json:"type"`
	Target string `json:"target,omitempty"`
}

// Matches reports whether m satisfies the rule. A rule with no conditions never
// matches (so an empty/garbage rule is inert rather than catch-all).
func (r Rule) Matches(m db.Message) bool {
	if len(r.Conditions) == 0 {
		return false
	}
	any := r.Match == MatchAny
	for _, c := range r.Conditions {
		ok := c.matches(m)
		if any && ok {
			return true
		}
		if !any && !ok {
			return false
		}
	}
	// all-mode reaching here means every condition matched; any-mode means none did.
	return !any
}

func (c Condition) matches(m db.Message) bool {
	if c.Field == FieldHasAttachment {
		// has_attachment is a boolean field; is_true (or contains/equals "true").
		want := c.Op == OpIsTrue || strings.EqualFold(strings.TrimSpace(c.Value), "true")
		return m.HasAttachment == want
	}
	hay := strings.ToLower(fieldValue(m, c.Field))
	needle := strings.ToLower(strings.TrimSpace(c.Value))
	switch c.Op {
	case OpContains:
		return strings.Contains(hay, needle)
	case OpNotContains:
		return !strings.Contains(hay, needle)
	case OpEquals:
		return hay == needle
	case OpStartsWith:
		return strings.HasPrefix(hay, needle)
	case OpEndsWith:
		return strings.HasSuffix(hay, needle)
	default:
		return false
	}
}

func fieldValue(m db.Message, field string) string {
	switch field {
	case FieldFrom:
		return m.From
	case FieldTo:
		return m.To
	case FieldCC:
		return m.CC
	case FieldSubject:
		return m.Subject
	case FieldBody:
		return m.BodyText
	default:
		return ""
	}
}
