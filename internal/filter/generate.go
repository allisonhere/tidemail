package filter

import (
	"fmt"
	"strings"
)

// Prompt builds the instruction sent to an AI provider to turn an English filter
// description into a JSON rule matching this package's schema. The caller passes
// the account's folder names so the model can reuse a real folder for moves,
// while still allowing an explicitly requested new destination.
func Prompt(english string, folders []string) string {
	folderList := "(none)"
	if len(folders) > 0 {
		folderList = strings.Join(folders, ", ")
	}
	return fmt.Sprintf(`You convert an email filter described in English into a JSON rule.

Return ONLY a JSON object (no prose, no code fences) with this exact shape:
{
  "match": "all" | "any",
  "conditions": [ { "field": <field>, "op": <op>, "value": <string> } ],
  "action": { "type": <action>, "target": <folder name, only for "move"> }
}

Allowed field: from, to, cc, subject, body, has_attachment
Allowed op: contains, not_contains, equals, starts_with, ends_with, is_true
  (use is_true only with field "has_attachment")
Allowed action.type: move, mark_read, archive, delete, spam
Existing folders: %s
For action "move", use the destination folder named by the user exactly. The
target may be an existing folder or a new folder that TideMail will create when
the rule is saved.

Use "all" when every condition must hold, "any" when one is enough.
Keep values minimal (e.g. a domain like "substack.com", not a full address).

English description:
%s`, folderList, english)
}
