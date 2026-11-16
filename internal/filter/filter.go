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
		want, ok := attachmentWant(c.Op, c.Value)
		// Validate rejects any rule reaching this, so ok=false here is inert
		// rather than a silent inversion of what the rule's author meant.
		return ok && m.HasAttachment == want
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

// attachmentWant decodes a has_attachment condition's operator/value into the
// boolean it actually tests for. Only is_true and equals are meaningful on a
// boolean field; every other operator (contains, not_contains, starts_with,
// ends_with) previously fell through to treating the literal value text as
// the answer, which inverted the rule's own stated operator — "not_contains
// true" matched mail WITH an attachment. Validate rejects any other operator
// before a rule can be saved, so ok=false here should be unreachable for a
// validated rule.
func attachmentWant(op, value string) (want bool, ok bool) {
	v := strings.TrimSpace(value)
	switch {
	case op != OpIsTrue && op != OpEquals:
		return false, false
	case v == "" && op == OpIsTrue:
		// is_true with no value is the common shorthand for "has an attachment".
		return true, true
	case strings.EqualFold(v, "true"):
		return true, true
	case strings.EqualFold(v, "false"):
		return false, true
	default:
		return false, false
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
