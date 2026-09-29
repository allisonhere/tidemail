package db

import (
	"strings"
)

// Tag search: "#tag" terms in a search match the tags TideMail shows on
// message rows, using the effective classification (plugin annotations with
// the user's corrections applied), so a search agrees with the tags on
// screen. Terms match by prefix, so "#git" already finds #github while
// typing; several tags must all match.

// SearchTerms is a search split into full-text words and #tags.
type SearchTerms struct {
	Text string
	Tags []string
}

// ParseSearch splits a search into free text and lowercase #tag names. A
// lone "#" or a tag with no usable characters is ignored.
func ParseSearch(query string) SearchTerms {
	var words []string
	var terms SearchTerms
	for _, f := range strings.Fields(query) {
		if !strings.HasPrefix(f, "#") {
			words = append(words, f)
			continue
		}
		tag := strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
				return r
			}
			return -1
		}, strings.ToLower(strings.TrimLeft(f, "#")))
		if tag != "" {
			terms.Tags = append(terms.Tags, tag)
		}
	}
	terms.Text = strings.Join(words, " ")
	return terms
}

// Attention tags as they appear on rows, with accepted spellings.
var attentionTagNames = []struct {
	names  []string
	key    string
	values []string // plugin values that count
	user   []string // correction values that count
}{
	{[]string{"reply", "needs_reply", "needs-reply", "needsreply"}, AttentionNeedsReply, NeedsReplyValues, []string{"true"}},
	{[]string{"urgent", "urgency"}, AttentionUrgency, UrgencyValues, []string{"high"}},
	{[]string{"important", "importance"}, AttentionImportance, ImportanceValues, []string{"high"}},
}

// tagPredicate is the SQL condition (on messages.id) for one #tag: any
// attention tag whose name starts with it, or an effective category that
// starts with it.
func tagPredicate(tag string) (string, []any) {
	var parts []string
	var args []any
	for _, a := range attentionTagNames {
		for _, name := range a.names {
			if strings.HasPrefix(name, tag) {
				expr, exprArgs := userAwareSignal(a.key, a.values, a.user)
				parts = append(parts, expr)
				args = append(args, exprArgs...)
				break
			}
		}
	}
	// Category: the user's correction wins; otherwise any plugin category.
	// "_" is a LIKE wildcard, so it is escaped.
	like := strings.NewReplacer(`\`, `\\`, `_`, `\_`, `%`, `\%`).Replace(tag) + "%"
	parts = append(parts, `(EXISTS (SELECT 1 FROM classification_overrides co
			WHERE co.message_id = messages.id AND co.key = 'category'
			AND co.value NOT IN ('plugin', 'none') AND co.value LIKE ? ESCAPE '\')
		OR (NOT EXISTS (SELECT 1 FROM classification_overrides co
				WHERE co.message_id = messages.id AND co.key = 'category' AND co.value != 'plugin')
			AND EXISTS (SELECT 1 FROM plugin_annotations pa
				WHERE pa.message_id = messages.id AND pa.key = 'category'
				AND lower(trim(pa.value)) LIKE ? ESCAPE '\')))`)
	args = append(args, like, like)
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// userAwareSignal is effectiveSignalExists, treating a stored "plugin"
// correction as no correction, as the effective classification does.
func userAwareSignal(key string, values, user []string) (string, []any) {
	pluginExpr, pluginArgs := signalExists(key, values)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(user)), ",")
	args := []any{key}
	for _, v := range user {
		args = append(args, v)
	}
	args = append(args, key)
	args = append(args, pluginArgs...)
	return `(EXISTS (SELECT 1 FROM classification_overrides co
		WHERE co.message_id = messages.id AND co.key = ? AND co.value IN (` + placeholders + `))
		OR (NOT EXISTS (SELECT 1 FROM classification_overrides co
			WHERE co.message_id = messages.id AND co.key = ? AND co.value != 'plugin')
			AND ` + pluginExpr + `))`, args
}

// tagsWhere ANDs the predicates for every tag.
func tagsWhere(tags []string) (string, []any) {
	var parts []string
	var args []any
	for _, t := range tags {
		expr, exprArgs := tagPredicate(t)
		parts = append(parts, expr)
		args = append(args, exprArgs...)
	}
	return strings.Join(parts, " AND "), args
}
