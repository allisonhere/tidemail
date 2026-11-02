# TideMail bug-fix handoff

## Current state

- Workspace: `/home/allie/Projects/tidemail`.
- Branch: `fix/mail-safety-and-reliability`.
- Parent of the commit containing this handoff: `bbf35bd`
  (`fix: require TLS for OAuth mail authentication`). Use `git log -1` for the
  current commit identifier.
- The branch is pushed to origin. No PR has been opened.
- Findings **12–15 are implemented and verified**, and are included in the commit
  containing this handoff. The user explicitly requested committing both.
- Findings **21 and 23 are fixed** (contact upsert keeps saved names; one
  message-key rule for Go and SQL). No audit findings remain open. All other audit
  findings 1–27 have been addressed, subject to the qualifications below.
- This handoff is intentionally committed alongside fixes 12–15.

## Commits already made

| Commit | Findings | Summary |
| --- | --- | --- |
| `6059228` | 1–11 | Draft/message action safety, mailbox-scoped UID handling, server mark-all-read, remote-first filter deletes, serialized draft persistence, incremental body pages, link controls, SMTP upload budget, stable multipart boundaries, nested MIME cleanup. Also updates vulnerable `x/net` dependencies. |
| `d6fa8c6` | 18, 20, 27, 24, 22 | Fresh-connection COPY cleanup, account-isolated OAuth refresh, grammar cancellation, atomic message deletion and Needs You dismissal/restore. |
| `f61117a` | 16, 17 | Reject Unicode hard breaks in link targets; strip bare CR from parsed/rendered mail. |
| `07800af` | 25, 26 | Transactional account FTS cleanup and SQLite DSN settings on replacement connections. |
| `bbf35bd` | 19 | Require TLS for OAuth authentication in IMAP and SMTP, including localhost; preserve implicit SMTP TLS on port 465. |

## Findings 12–15 included with this handoff

### 12: Short-terminal list offsets

`internal/ui/model.go` uses at least one logical visible row for Down/Space
scroll calculations. `clampMessageListViewport` runs on resize, clamps the cursor
and offset, and uses draft count when Drafts is selected. The physical visible
row helper still reports zero when no complete row fits.

### 13: Single-account compose focus

`internal/ui/compose.go` starts at `firstHeaderField()`: To for zero/one configured
account, From for multiple accounts. To receives actual text-input focus.
Replies/forwards inherit this behavior; reopening drafts retains its existing
body focus. Existing multi-account sender-picker behavior is preserved.

`internal/ui/compose_test.go` updates `focusToField` to seek To explicitly instead
of always advancing once. The reply recipient-autocomplete test now asserts the
new single-account initial To focus before typing.

### 14: Legacy envelope charsets

`internal/imap/client.go` always creates IMAP options with
`mime.WordDecoder{CharsetReader: charset.Reader}`. Optional unilateral update
handlers are assigned to those same options without replacing the decoder.

`internal/imap/envelope_charset_test.go` sends ISO-8859-1 subjects, Windows-1252
names and Shift_JIS names as encoded words in the actual IMAP ENVELOPE response,
with and without update callbacks. The custom session matters: the ordinary
in-memory backend decodes headers itself and would mask a missing client decoder.
This fixes fetched envelopes; there is no migration that rewrites existing cache
rows containing previously undecoded headers.

### 15: Indented body clipping

`internal/ui/message_render.go` reserves the explicit left inset before wrapping
and padding plain-text and Reddit bodies, plus empty/PGP callouts. At a one-cell
width it omits the inset. HTML/image geometry is unchanged because that renderer
does not receive this additional external indent.

`internal/ui/reader_compose_fixes_test.go` covers Down/Space on short terminals,
resize recovery, draft counts, immediate recipient typing, and preserving every
character of full-width tokens, URLs and wide Unicode text after viewport clipping.

Docs/help updated alongside these fixes: `CHANGELOG.md`, `README.md`, `STATUS.md`,
`docs/guide.md`, `internal/ui/help.go`, and `keybindings.csv`.

## Validation

On the current application code, all checks passed:

```sh
go build ./...
go vet ./...
go test ./... -race -timeout 10m
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
git diff --check
```

- Full UI race run: **333.696 seconds**; allow ten minutes, not a two-minute cap.
- Lint: **0 issues**. Vulnerability scan: **No vulnerabilities found**.
- Latest complete logs, if still present:
  `/tmp/tidemail-12-15-final-tests.log`,
  `/tmp/tidemail-12-15-final-lint.log`,
  `/tmp/tidemail-12-15-final-vuln.log`.
- An earlier full run failed only the old reply-focus test. That test was updated
  and the subsequent complete suite passed. The strengthened envelope wire test
  also passed separately and is included in the final full run.
- Sandbox restrictions can block Go cache writes and local IMAP/OAuth listeners.
  Use the normal escalation mechanism when required; these are not code failures.

## Remaining findings

### 21: Contact upsert can erase a saved name (fixed)

Fixed: the conflict update keeps `contacts.display_name` when the incoming name
is empty. `UpdateContact` still clears names on explicit edits. Tests cover
unnamed re-adds, blank names with metadata, nameless vCard imports and renames.

Original finding:

`internal/db/contacts.go`, `AddContactWithMetadata`: the conflict update assigns
`display_name = excluded.display_name` even if the incoming name is empty.
Metadata fields already preserve stored values when the incoming value is empty.

Preserve the stored display name for empty incoming names, while allowing a
nonempty new name to replace it. Add tests for unnamed address additions, vCard
imports without a name, and explicit nonempty updates. Keep any intentional
explicit-edit clearing behavior separate from the additive/import upsert.

### 23: Message-key computation differs between Go and SQL (fixed)

Fixed: `MessageKey` trims and rejects the same ASCII whitespace set
(space, tab, LF, VT, FF, CR) and lowercases ASCII only; `messageKeySQL` applies
exactly that rule, and `PruneWaitingDismissals` now uses `messageKeySQL`
instead of its own inline key. Normal Message-IDs keep their keys. Keys that
did change for surrounding Unicode whitespace or non-ASCII capitals already
disagreed with SQL, so their snoozes were pruned at startup anyway. The only
agreeing keys that change are Message-IDs with an internal VT or FF, which now
key by row like other whitespace.
Tests cover Go/SQL parity, snooze exclusion, startup pruning and stop-waiting
pruning.

Original finding:

`internal/db/snooze.go`: `MessageKey` normalizes with Go trimming/lowercasing and
rejects internal space/tab/CR/LF; `messageKeySQL` uses SQLite trimming/lowercasing
and checks only literal space. For an internal tab, Go returns `row:<id>` while
SQL can construct a bracketed Message-ID. This affects snooze matching/pruning
and stop-waiting dismissals.

Align the canonical key rules without invalidating normal stored snoozes. Test
Go/SQL parity for internal and surrounding whitespace, empty IDs and angle
brackets; then test snooze exclusion, startup pruning and stop-waiting behavior.
Do not reverse the Go/SQL mismatch when describing the defect.

## Constraints and qualifications

- Read repository `AGENTS.md`: user-facing behavior requires matching docs/help
  updates in the same change. Relevant skills already used in this conversation:
  `go-cli-workflow` and `tide-tui-ecosystem`.
- User explicitly rejects webmail referrals as recovery: fixes must keep recovery
  inside TideMail.
- Esc during grammar checks cancels the check and keeps editing (explicit user
  choice). Late results are ignored; persistence busy states remain protected.
- COPY recovery must never repeat a completed COPY automatically. It reconnects
  once for source cleanup, validates UIDVALIDITY and reports `PartialMoveError`
  if cleanup cannot be confirmed.
- OAuth transport enforcement deliberately includes localhost. SMTP port 465
  uses implicit TLS independently of the STARTTLS toggle. Finding 19 did not
  disable explicitly configured plaintext IMAP password authentication.
- SMTP submission budget is ten minutes, with 30-second connection/authentication
  setup. Multipart byte stability assumes a stamped message identity.
- Incremental sync uses bounded continuation pages and holds its watermark until
  completion. It also searches unknown UIDs above the cached history floor to
  recover old-dated moved messages.
- The original Gmail `NO System Error (Failure)` report was not independently
  reproduced or established as fixed. Do not claim the COPYUID recovery fixes
  that different server rejection; no generic retry for that error was added.
- Next expected steps: push/open a PR when the user authorizes those actions,
  and decide whether this handoff file should be removed before merge. Do not merge or release
  merely because a handoff exists.
