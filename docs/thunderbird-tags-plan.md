# Thunderbird-compatible tags plan

Status: implemented in the working tree on `feat/thunderbird-tags`; validation
is recorded below.

Issue: https://github.com/allisonhere/tidemail/issues/54

Working branch: `feat/thunderbird-tags`. The implementation remains
uncommitted. This revision replaces the separate Mail tags and Classification
tabs while retaining the useful backend work.

## Intended experience

Keep TideMail's familiar `e` reclassification workflow. Include Thunderbird's
default choices in those existing controls, reusing matching choices such as
Personal instead of creating duplicates:

| Choice | Thunderbird IMAP keyword |
| --- | --- |
| Important | `$label1` |
| Work | `$label2` |
| Personal | `$label3` |
| To Do | `$label4` |
| Later | `$label5` |

When a user deliberately selects a compatible tag, save its keyword through
IMAP automatically. Thunderbird can then read that assignment. Assignments
previously saved by Thunderbird should appear in TideMail after synchronization.
Keep technical keyword identifiers out of the ordinary workflow.

Remove the initial implementation's separate Mail tags tab and keyword-entry
form. Keep AI/JEV behavior unchanged for this revision: this plan does not
authorize automatically publishing all AI classifications as server tags.

Preserve multiple existing Thunderbird tags. Changing a category must not
silently erase other tags on the message.

## What the server stores

Use IMAP keywords to synchronize message tag assignments where the server
supports them. Standard IMAP does not synchronize tag display names or colors;
Thunderbird stores those definitions separately. Recognize the five defaults
above, but do not assume custom names and colors can be recovered from the
server. Preserve unknown keywords during changes.

## Optional tag icons

Add **Tag icons** as an option under **Settings → Display → Tag style**.

- Show Thunderbird-like tag-shaped glyphs in message rows, in each tag's color.
- Prefer the Nerd Font Font Awesome tag glyph, `U+F02B` (``).
- Use `#` when icons are disabled.
- Include a preview in Settings and keep full names available in the editor.
- Preserve the user's existing style until they select the new option.
- Change appearance only; assignment and synchronization remain the same.

## Implementation scope

Reuse the branch's keyword storage, synchronization, validation and safety
checks, search, and renderer support. Revise the UI around the existing
reclassification controls rather than replacing that backend.

Retain protection against unsupported keywords, read-only mailboxes, stale
UIDVALIDITY, missing messages, disconnects, and stale asynchronous UI updates.
Surface failures without presenting an unconfirmed server change as saved.

When implementing the revised behavior, update the relevant guide, in-app help,
README, STATUS, and keyboard references wherever they describe the changed
workflow. Update behavior tests alongside the implementation.

## Multi-tag interaction

The Category field contains the five compatible choices once each, followed by
custom tags discovered for the account. Checkboxes show server membership.
Enter on a compatible default adds its server tag before saving the local
category correction. Enter on a discovered custom tag adds it without changing
the category. Space explicitly toggles one server tag without changing the
local correction or other assignments. Resetting a local category leaves server
tags intact. Users can browse longer choice lists with the existing arrow keys.

The separate keyword-entry form is removed. Settings retains a names-and-colors
editor for discovered tags. Matching local category and server-tag badges are
shown once in message rows.

## Acceptance criteria

- `e` opens the familiar reclassification workflow with the five compatible
  choices and no duplicate Personal choice.
- Deliberate selection saves the corresponding keyword, and existing
  Thunderbird assignments appear after synchronization.
- Changing one assignment preserves other tags, including unknown keywords.
- AI/JEV behavior remains unchanged.
- The separate Mail tags tab and keyword-entry form are removed.
- Tag icons can be selected and previewed in Settings without changing existing
  users' appearance preferences or synchronization behavior.
- Backend safety checks and relevant synchronization tests continue to pass;
  revised editor behavior receives focused verification.
- Verify interoperability with Thunderbird when available. Live Thunderbird
  validation has not yet been performed because it is not installed.

## Verification

Focused tests cover the unified editor, account isolation, local names/colors,
server-write failures, pending operations, and the icon preference and fallback.
A two-client IMAP test verifies category writes preserve existing custom and
default tags, and that external assignments refresh on synchronization. Live
Thunderbird validation remains outstanding because Thunderbird is not installed.

Validation completed for this revision:

- `go build ./...` and `go vet ./...` passed.
- `go test ./... -race` passed; focused race tests also passed after the final
  editor layout and sync-refresh changes.
- Changed-code lint reported zero issues. Full lint retains the pre-existing
  unused `pickableThemeLabelAt` helper in `internal/ui/themes.go`.
- `govulncheck` reported no vulnerabilities affecting this code; six findings
  exist in required modules that the code does not appear to call.
- `git diff --check` passed.
