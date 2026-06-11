# TideMail — Design & Code Review

**Date:** 2026-06-09
**Scope:** the uncommitted remote→local drafts sync (`git diff HEAD`: `internal/db/drafts.go`, `internal/ui/drafts.go`, `internal/ui/model.go`, tests), reviewed in the context of the larger drafts feature and this week's fixes (XOAUTH2 auth, batched bulk delete, terminal foreground).
**Method:** 7 independent review angles (line-by-line, removed-behavior, cross-file tracing, reuse, simplification, efficiency, altitude), candidates verified against the code. Recall-biased: PLAUSIBLE findings are included.

---

## Executive summary

The remote-drafts import is functionally sound for the happy path and well-tested at the unit level (linkage preservation, dedupe, HTML conversion, sidebar count). The session's earlier fixes (single-connection bulk delete, remote-first semantics, honest aggregated status) materially improved the data-integrity story.

The main structural risk is the **dual-representation design**: every remote draft exists twice — as a `messages` row (the sync snapshot) and a `drafts` row (the editable mirror) — and nothing *structural* keeps them consistent. Consistency currently depends on runtime queries (`HasDraftForRemote`, `UnmirroredDraftMessageCount`, `DeleteMessageByUID`) executing in the right order with no interleaving. Two of the three confirmed bugs below are direct consequences of that choice. The recommended deep fixes are cheap: a `UNIQUE` index on the mirror key plus an `INSERT … SELECT … WHERE NOT EXISTS` import collapses the race *and* the N+1 query pattern at once.

**Verdict: ship after fixing findings 1–3; schedule 4–5 and the schema hardening with the local→remote push work.**

---

## Design review

### What's good

- **Local-first with honest failure surfacing.** The remote-first ordering for destructive operations (delete only locally what the server confirmed) adopted in the bulk-delete fix is carried into draft deletion. Status messages aggregate instead of racing. This is the right convention; keep extending it.
- **Linkage preservation in `SaveDraft`.** Compose autosaves rebuild drafts from screen state without remote linkage; the UPDATE's preserve-zero-fields logic correctly prevents autosave from orphaning mirrors. The regression test pins it.
- **Idempotent import with Message-ID-first matching** degrades gracefully to mailbox+UID, and HTML-only webmail drafts get converted to editable text rather than dropped.
- **One-way sync is honestly scoped**: `dirty` tracks divergence and `MarkDraftRemoteSynced` is staged for the future push, rather than pretending edits propagate.

### Design concerns

1. **Consistency by query, not by schema.** The `drafts` table has no `UNIQUE` constraint on `remote_message_id` or `(mailbox_id, remote_uid)`. Dedupe is check-then-insert across goroutines (finding #2). A unique index turns a race into a no-op conflict.
2. **`SaveDraft`'s CASE-WHEN preservation is a bandaid** for `toDraftRecord` not carrying linkage. The deeper fix is for `ComposeModel` to carry the draft's remote linkage (it already carries `draftID`), making `SaveDraft` uniform. The current approach works but means a zero value can never intentionally *clear* a linkage field.
3. **Stale-mirror lifecycle is undefined.** A draft edited in webmail gets a new Message-ID/UID; the old mirror lingers (messages table is append-only during sync) alongside the newly imported version. Acceptable v1, but the eventual reconciliation ("prune non-dirty mirrors whose UID vanished from the mailbox") should be designed together with the local→remote push.
4. **Tombstones now do double duty** (deleted mail + deleted draft mirrors) with no `reason` field. Fine today; revisit if tombstone TTL/pruning semantics ever diverge.

---

## Findings (ranked)

### 1. CONFIRMED — Import with missing mailbox writes orphaned drafts
`internal/ui/drafts.go` (`importRemoteDraftsCmd`)
`draftAccountIdentity(mailboxID)` returns `("", "")` when `mailboxByID` is nil at command-build time (e.g. first-time sync completing before `m.mailboxes` is reloaded, or account removed mid-sync). The import then inserts drafts with empty `account_name`/`account_user`, which `ListDrafts(realName, realUser)` never returns — **invisible orphan rows**.
*Fix:* bail out of the import (return early) when the mailbox or account identity can't be resolved.

### 2. CONFIRMED — Concurrent imports can double-insert the same remote draft
`internal/ui/model.go` (MailboxSyncedMsg hook) + `internal/db/drafts.go`
The auto-sync timer does not consult `m.syncing`, so a timer tick plus a manual `s` can run two syncs → two concurrent imports. `HasDraftForRemote` → `SaveDraft` is not atomic and the schema has no unique constraint, so the same remote draft can be mirrored twice.
*Fix (one change kills this and the N+1):* replace the per-message check-then-insert loop with a single `INSERT INTO drafts (…) SELECT … FROM messages m WHERE m.mailbox_id = ? AND NOT EXISTS (…)`, and add `CREATE UNIQUE INDEX … ON drafts(mailbox_id, remote_uid) WHERE remote_uid != 0` (plus an index on `remote_message_id`).

### 3. CONFIRMED — Failed draft delete leaves the draft hidden but alive
`internal/ui/model.go` (Delete key in drafts mailbox; `DraftDeletedMsg` handler)
The key handler removes the draft from `m.drafts` synchronously; the delete is now remote-first, so on remote failure the draft survives in the DB — but the error handler only sets a status and never reloads the list. The draft vanishes from view yet still exists (and still mirrors a server draft) until something reloads drafts.
*Fix:* on `DraftDeletedMsg` with `Err != nil`, reload the drafts list for the selected mailbox (mirrors how message deletes keep failures visible).

### 4. PLAUSIBLE — `accountIndex` falls back to 0 (first account) on config mismatch
`internal/ui/drafts.go` (`importRemoteDraftsCmd`)
When no `cfg.Accounts` entry matches name+user, imported drafts get `AccountIndex 0`, and `NewComposeFromDraft` trusts an in-range index without re-verifying name/user — a reordered config can route the draft's *send* through the wrong account.
*Fix:* store `-1` on no-match (forcing `NewComposeFromDraft`'s name/user fallback path), and make `NewComposeFromDraft` verify the indexed account matches before trusting it.

### 5. PLAUSIBLE — `GetDraft` error silently skips remote cleanup
`internal/ui/drafts.go` (`deleteDraftCmd`)
`if draft, err := database.GetDraft(id); err == nil && …` — on a transient error (single-connection SQLite under contention) the remote-cleanup block is skipped but the local `DeleteDraft` still runs, so a mirrored draft's server original survives and resurrects on next sync.
*Fix:* on `GetDraft` error, return `DraftDeletedMsg{Err: err}` instead of proceeding with a blind local delete.

---

## Cleanup & efficiency (advisory)

- **N+1 import queries** — folded into finding #2's `INSERT…SELECT` fix. Also stop loading full bodies (`ListMessages` pulls `body_text`/`body_html` for every message) just to *check* mirroring; the SQL form avoids that too.
- **Sidebar count runs per render frame.** `draftsSidebarCount` executes the `UnmirroredDraftMessageCount` correlated subquery on every sidebar draw, with no supporting indexes. The indexes from finding #2 make it cheap; caching the count on the model (refreshed by `DraftsLoadedMsg`) would be cleaner still.
- **Duplicated account-index resolution** — the name+user → index loop now exists in `NewCompose`, `NewComposeFromDraft`, and `importRemoteDraftsCmd`. Extract `findAccountIndex(accounts, name, user) int`.
- **Duplicated IMAP delete pattern** — `deleteDraftCmd`'s connect/timeout/delete block mirrors `deleteBatchRemote` in `messages.go`. Worth one shared helper when either next changes.
- **Converter divergence** — `draftBodyText` builds a bare `md.NewConverter` while `renderHTMLBody` adds `spanStyleRule()`; bold/italic spans in webmail drafts will import flatter than they render. Share a converter constructor.
- **`md.NewConverter` per message** in the import loop — hoist one converter outside the loop (it's stateless).

---

## Suggested fix order

1. Findings 1, 3, 5 — small, local, no schema change (early-return guards + reload-on-error).
2. Finding 2 — schema migration (unique + lookup indexes) and the `INSERT…SELECT` import; subsumes the N+1 and most of the sidebar-count cost.
3. Finding 4 + the `findAccountIndex` helper together.
4. Defer: stale-mirror pruning and local→remote push (design together; `dirty`/`MarkDraftRemoteSynced` are already staged for it).

---

*Generated by Claude Code review session, 2026-06-09. Scope was the working-tree diff; pre-existing code reviewed only where the diff touches it.*
