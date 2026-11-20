# Handoff

> Update, 2026-11-18: Audit **#4 is fixed locally** in `bbb7813` on
> `feat/notification-actions` (`65ba90e` on `fix/reject-zero-uids`). Shared IMAP
> validation rejects UID zero for fetch/raw fetch, read/star, move, and delete;
> invalid batches fail before any command. Focused IMAP/mailcore race tests,
> build, vet, lint, and vulnerability checks passed. Full race verification
> passed: the saved log covers packages before UI; after daemon interruption,
> UI (350.547s), update, and mailcore passed separately. Source not pushed. #5/#6 remain on the unmerged
> saved-search branch; the next independently fixable numbered item is **#7**.

> Update, 2026-11-17: Audit **#3 is now fixed locally** in commit `0964fe9`
> on `feat/notification-actions` (also `e1d1a6e` on `fix/headless-oauth-token`).
> CLI startup installs keychain-backed OAuth refresh-token persistence before
> dispatch. Regression tests cover stable account IDs and unavailable storage.
> Build, vet, full race suite, lint, and vulnerability checks passed.
> Source fix has not been pushed or merged. Next numbered unfixed claim is **#4**.

> Generated: 2026-11-16 12:XX CST
> Project: `/home/allie/Projects/tidemail`
> Session summary: Validated all 41 claims in an external bug-audit document against the live codebase (no subagents — the user has banned them project-wide after a prior fan-out burned a 5-hour usage window), fixed and merged the two highest-severity confirmed bugs, and picked up several unrelated loose ends along the way. Validation is now complete; 39 confirmed bugs remain unfixed.

## Goal

The user asked to "validate" `/home/allie/.opencode/plan/tidemail-bug-audit.md` — a bug-audit document written by a different AI tool ("opencode") against this repo at commit `b2402ec`. It claims 35 numbered bugs plus a "25. Smaller items" bullet list of 6 more (41 distinct claims total), each with a severity/confidence rating and a suggested fix. The end state the user wants: every claim independently confirmed or refuted against the actual code (not just trusted at face value), and — per explicit follow-up instructions — the worst ones actually fixed, committed, and PR'd, working down the list only as far as the user's limited session time allows before they have to stop and pick this up later.

**Hard constraint, stated by the user and now saved in memory project-wide (not just this repo):** never spawn subagents (Agent tool, any subagent_type including `fork`) for this kind of work. A 4-way parallel fork fan-out to validate this exact audit burned a huge, measurable chunk of the user's 5-hour usage window (two of the four agents hit "You've hit your 5-hour limit" mid-task) — do everything in the main thread instead, even if slower in wall-clock time. Also saved: don't run the full `go test ./...` suite for a docs/prose-only change (e.g. a CHANGELOG.md merge-conflict resolution) — `go build` is enough when no `.go` file changed.

## Current State

**Validation: COMPLETE.** All 41 claims in the audit have now been independently read against the actual code (not just trusted the audit's description) — 0 refuted. See the table below for exact locations.

**Working / done:**
- The two most severe confirmed bugs are fixed, merged into `main`:
  - **Audit #26** (top risk — filter `has_attachment` condition ignored its own operator, so `not_contains "true"` or `is_true value:"false"` matched mail *with* an attachment, backwards, and dangerous on a delete/spam rule): fixed in `internal/filter/filter.go`/`parse.go`/`generate.go`, PR **#139**, state **MERGED**.
  - **Audit #1 + #2** (Outbox: editing an `OutboxUncertain` delivery silently converted it to a draft with no warning, unlike retry which already confirms; and the transient status line called an uncertain delivery "send failed," inviting a duplicate resend): fixed in `internal/ui/outbox.go`/`model.go`, PR **#140**, state **MERGED**.
- `main` is currently at `28854ee` (fetched fresh this pass; confirmed via `git log --oneline -1 origin/main`).

**Important nuance found during this validation pass: #5 and #6 are not bugs in shipped code.** `internal/ui/search_saved.go` (and the saved-search sidebar row kind it defines) does not exist on `origin/main` at all — it only exists on the unpushed `feat/saved-searches-113` branch (commit `b2402ec`, the exact commit the audit was run against). Both claims were re-validated by reading that branch directly (worktree still at `/tmp/claude-1000/-home-allie-Projects-tidemail/c1595770-68e5-4034-8852-056a452e1794/scratchpad/audit-validate`, detached at `b2402ec`) and are real bugs *in that branch*, but they cannot be "fixed on main" until/unless that branch is pushed and merged — see loose end #1 below. #25c's `search_saved.go` reference has the same caveat (noted already in the table).

**Newly validated this pass — all 15 CONFIRMED, 0 refuted:**
- **#3** (OAuth refresh-token persistence): confirmed by tracing the full call path — `RunHeadless` (the entry point for `tidemail send`/`tidemail outbox process`) never calls `ui.NewModel` or `mailcore.SetRefreshTokenSaver`, the only two places that replace the no-op `auth.PersistRefreshToken`. A rotated token from a headless run is genuinely discarded.
- **#5, #6**: confirmed on the unpushed branch — see nuance above.
- **#7** (schedule-send calendar timezone): confirmed exactly — `time.Now().AddDate(0,0,1).Truncate(24*time.Hour)` truncates on absolute/UTC time, not local midnight.
- **#8** (archive-in-Archive self-move): confirmed — `scheduleArchive` has no analogue of `scheduleDelete`'s `trash.ID != source.ID && trash.Name != source.Name` guard.
- **#9** (search-history scroll/click math): confirmed, and worse than a one-line issue — `handleSearchHistoryClick` does raw `y - interiorTop - 1` with no `ListItemLineStride()` call at all, and `searchHistoryRows` renders every entry with no `listOffset` windowing (unlike the drafts/messages render branches, which both slice by `listOffset`/`articleRowsVisible()`).
- **#27** (env OAuth creds written to config.toml): confirmed — `Save` only clears Google creds when they equal the bundled default; an env override never matches that check. `MSClientID` has no clearing logic at all, confirming the audit's "always pins the default" claim too.
- **#28** (Homebrew never detected as package-managed): confirmed — `probeOwningPackage` returns "not owned" as soon as the directory is writable, before ever reaching its pacman/dpkg/rpm-only probe list; `shadowedBy` uses `filepath.Abs` only, never `EvalSymlinks`.
- **#29** (plugin report scope leaks across same-named accounts): confirmed — scoping is `strings.EqualFold(mb.AccountName, rc.AccountName)`; duplicate account names are warned about, not blocked.
- **#30** (`draft --no-interactive` still opens the wizard): confirmed by tracing the actual recursive call — `interactiveRoute` strips the flag and calls `RunHeadless(["draft"])`, which calls `interactiveRoute` *again* with the flag already gone; on a TTY, `missing=true` and `automatic=true` together fail to trip the early-return, so `cliform.OpenTerminal()` runs anyway.
- **#31** (Accounts/Outbox CLI list renders as a map dump): confirmed — `captureRead`'s JSON round-trip into `map[string]any` always decodes nested lists as `[]any`; `readSuccess`'s type assertion to `[]map[string]any` therefore always fails for this path, regardless of actual content.
- **#32** (undated messages dropped by content-page cursor): confirmed mechanism — zero `time.Time.Unix()` is `-62135596800`, the DB's "no date" sentinel is `0`, and `0 < -62135596800` is false, so the next-page `WHERE date < ?` can never match those rows.
- **#33** (release-summary UTF-8 truncation): confirmed, exact match — `line[:137]` is a byte-index slice.
- **#34** (staged update download leaked in /tmp): confirmed — both the package-managed path and the unwritable-target path set `RequiresManual = true`, but the deferred cleanup's skip condition only checks that one flag, not which of the two manual commands actually needs the staged file.
- **#35** (symlinked config.toml replaced, not followed): confirmed — `Save` always does `os.CreateTemp` in the same dir + `os.Rename` over `path`; no `filepath.EvalSymlinks` call anywhere in `internal/config/config.go`.

**Confirmed-but-not-yet-fixed (39 claims now — the 24 from before plus the 15 above, minus #5/#6's branch caveat):** #3, #4, #7, #8, #9, #10–24, #25a–f, #27–35. See the "Confirmed, unfixed" table below for exact file:line and the audit's own suggested fix for each — they still need your own read-through before trusting the audit's fix verbatim, since two of them (below) need judgment calls, not a straight fix. #5 and #6 are confirmed but blocked on the `feat/saved-searches-113` push/merge decision (loose end #1 below), not independently fixable on `main` right now.

**Two unrelated, unresolved threads sitting in this repo from before/during this session — not part of the bug-audit task, but real loose ends the next session needs to know about:**
1. **`feat/saved-searches-113`** (local branch, commit `b2402ec`) — a complete, tested saved-search sidebar feature, never pushed to `origin`. `git ls-remote origin feat/saved-searches-113` returns nothing. Push it and open a PR, or ask the user if they still want it.
2. **`feat/notification-actions`** branch currently checked out in this exact working directory has an extra, unrelated commit `7559397` ("Add a local address-book plugin capability and TideContacts demo") sitting on top of `ef4bace`, which is itself already merged into `main` via PR #135. That commit is **not on `main`** and not on any PR branch — it's an orphaned commit on a dead branch. The user asked for it to be committed (not reviewed in depth) and has not yet said what to do with it; last offer was "move this commit onto a fresh branch off main and push/PR it properly, or leave it as-is" — unanswered.
3. **`HANDOFF.md`** itself (this file) and `sig.html` in the repo root are untracked scratch files — don't commit them.

## Files in Play

| File | Why It Matters |
|------|---------------|
| `/home/allie/.opencode/plan/tidemail-bug-audit.md` | The audit document itself — source of truth for every claim number referenced here. Read the specific item before trusting this handoff's one-line summary of it. |
| `internal/filter/filter.go`, `parse.go`, `generate.go` | Fixed for #26. New `attachmentWant(op, value) (want, ok bool)` helper in `filter.go` is the single source of truth for `has_attachment` boolean semantics now — `matches()`, `Validate`, and `Summary` all call it instead of re-deriving the boolean themselves. |
| `internal/ui/outbox.go`, `model.go` | Fixed for #1/#2. New `editOutboxItem(item)` helper extracted so both the direct "e"/"enter" path and the confirmed-after-y/n path share it. New `Model.outboxConfirmEdit bool` field distinguishes what `y`/`n` on `outboxConfirmID` resumes (retry vs. edit). |
| `internal/ui/outbox_uncertain_test.go` | New test file for #1/#2 — `uncertainOutboxTestModel(t)` helper uses the real `ClaimOutbox`+`RecoverOutbox` sequence (not a hand-built fixture) to get a genuine `OutboxUncertain` row, matching how `internal/db/outbox_test.go`'s own `TestOutboxClaimLimitsAndRecovery` does it. |
| `internal/filter/filter_test.go` | New tests for #26 — `TestHasAttachmentOperatorInversionRegression` is the exact regression case (the audit's own example: `not_contains "true"` and `is_true value:"false"` on a message that DOES have an attachment, asserting neither matches). |
| `/home/allie/.claude/plans/https-github-com-allisonhere-tidemail-is-staged-feigenbaum.md` | The plan file for this validation task (plan-mode history). Explains why Explore-type subagents were rejected for this work (their own description excludes "design-doc auditing, cross-file consistency checks") in favor of either fork subagents (later banned) or direct reads. |
| `/home/allie/.claude/projects/-home-allie-Projects-tidemail/memory/no-subagents.md`, `match-verification-to-change-type.md` | The two feedback memories from this session, now copied into **every** project's memory directory under `/home/allie/.claude/projects/*/memory/`, not just this one — the user explicitly asked for both to apply everywhere. |

## What Has Been Tried (and Why It Failed)

### Attempt 1: Parallel fork-subagent validation (4-way split, items 1-9/10-19/20-25/26-35)
- **What:** Split the 41 claims into 4 batches, launched a `fork` subagent per batch to independently read the cited code and report CONFIRMED/REFUTED/PARTIALLY-CORRECT verdicts.
- **Why it failed (functionally):** Two of the four batches (10-19 and 20-25) completed fine and gave solid, trustworthy reports (used directly in the tables below). The other two (1-9 and 26-35) hit `HTTP 429 rate_limit — You've hit your 5-hour limit`, were resumed via `SendMessage`, and were then killed via `TaskStop` when the user said "stop" — before they could re-report. So those two batches produced **zero** usable output; everything known about #1, #2, #4, #26 came from redoing the work by hand afterward, not from these agents.
- **Why it's now banned:** the user said this had already been requested once before ("don't use subagents ever, I'd said that a long time ago") and it wasn't retained in project memory, so it silently regressed. Now saved in memory under every project, not just this one — see `no-subagents.md`. **Do not repeat this approach, even for the 15 still-unvalidated claims.**

### Attempt 2: Full `go test ./...` after a CHANGELOG.md-only merge-conflict resolution
- **What:** After PR #137 (an unrelated, external contributor's macOS Keychain fix) merged to `main` first, PR #139's branch got a trivial CHANGELOG.md conflict (both PRs added a bullet in the same spot). Resolved it by keeping both bullets, then ran the *entire* `go test ./...` suite before pushing.
- **Why it was wrong:** only a `.md` file changed — no `.go` file was touched, so the test suite could not have caught anything `go build` wouldn't already catch. The user called this out directly. Also saved to memory (`match-verification-to-change-type.md`): for a docs/prose-only diff, `go build ./...` alone is enough; save the full suite for actual code changes.

## Current Best Theory

The remaining unvalidated claims (#3, #5-9, #27-35) are almost certainly mostly real — the 25 validated so far came back 25/25 real (23 fully CONFIRMED, 2 PARTIALLY-CORRECT with a narrower scope than claimed, 0 flat-out REFUTED), so this audit tool has a strong hit rate on this codebase. Don't skip validating the rest just because the trend looks good — PARTIALLY-CORRECT ones (#18, #25d) show the audit does sometimes overstate scope or miss a mitigating guard clause, so each item still needs its own read before acting on it.

Priority for whichever gets picked up next, by the same reasoning used to pick #26 and #1/#2 first: look for anything that can **silently and automatically destroy or misdirect real user mail with no review step** — that was the deciding factor for #26 over e.g. #4 (UID-0-as-newest only matters for `mailcore` callers like a hypothetical Android client, not the TUI) and #13 (Gemini key leak is a credential-disclosure bug, serious, but not mail-destructive). None of #3/#5-9/#27-35 have been read yet, so this can't be ranked without doing that reading first — don't guess at severity from the audit's own self-assigned "Confidence/Severity" line alone, the fork batches already caught at least one case (#18) where the audit's "Severity medium-low" framing undersold that it's actually a deliberate design trade-off, not a bug at all.

## Next Steps

Validation is done — there is nothing left to re-read for confidence. Remaining work is all fixing:

1. For the 39 confirmed-but-unfixed claims (everything except #1/#2/#26, already merged), re-run the same worktree-per-fix pattern used for #26 and #1/#2: `git worktree add /tmp/.../scratch-name -b fix/<name> origin/main`, fix, add tests, `gofmt -l .`/`go build ./...`/`go vet ./...`/`GOOS=windows GOARCH=amd64 go vet ./...`/`golangci-lint run ./...`/`go test ./...` (full suite — these ARE `.go` changes), update `CHANGELOG.md` + `docs/guide.md` if user-facing, commit, push, `gh pr create`. Resolve any CHANGELOG.md conflict with whatever else has merged since (check `gh pr view <N> --json mergeable,mergeStateStatus` after pushing) by keeping both bullets, as done twice already this session. Suggested order by what's actually destructive/credential-leaking first: **#13** (Gemini key leak) → **#20** (SMTP header injection) → **#27** (OAuth creds in plaintext config) → **#3** (dropped refresh tokens break headless auth) → **#8** (archive-in-Archive data loss) → the rest roughly in audit order.
2. Judgment calls to make explicitly with the user before "fixing" (the audit's suggested fix may not be what's wanted):
   - **#18** (delete tombstones match by Message-ID, not UID): the fork-batch report concluded this is likely *deliberate* — matching by Message-ID is what stops a re-APPENDed/moved copy from resurrecting a deleted message. The audit's suggested fix (match on `(mailbox_id, uid)` instead) could reintroduce that resurrection bug. Ask the user which trade-off they want before touching `internal/db/messages.go:350-366`.
   - **#25d** (plugin `test`/`ping` bypass): only half-confirmed — the process-slot skip is real, but the capability-check bypass is narrower than claimed (only reachable through the generic `Manager.Call(..., "plugin.test")`, not through `Manager.Test` itself, which does check `HasCapability`). Decide whether the narrower bug is still worth fixing before doing the audit's broader suggested fix.
   - **#5, #6** (saved-search sidebar): cannot be fixed on `main` at all until `feat/saved-searches-113` is pushed and merged (loose end #1 below) — fixing them pre-merge means fixing them on that branch, which then needs its own review pass before merging.
3. Decide what to do with the two unrelated loose ends under "Current State" above (`feat/saved-searches-113` unpushed; the orphaned `7559397` commit on `feat/notification-actions`) — both are blocking nothing technically, but they're stale uncommitted/unpushed work that will keep showing up in `git status`/`git branch` until resolved one way or another. This also now gates #5/#6 directly, not just hygiene.

## Key Constraints

- **Never spawn subagents in this project** (or any of the user's other projects — this was explicitly extended to all of them). This includes `fork`. Do all investigation and fixing directly, turn by turn, even for large tasks like "validate 41 bug claims."
- **Match verification depth to what changed.** `go build ./...` for docs/prose-only diffs; the full `gofmt`/`build`/`vet`/Windows-vet/`golangci-lint`/`go test ./...` pass only for actual `.go` changes.
- **The user's session time/usage budget is limited and was already damaged once this session** by the subagent fan-out — they explicitly said "sucks myt time is limited no because of you memory" after learning why. Be efficient; don't re-derive things already confirmed in this handoff or in memory.
- Every fix this session used an isolated `git worktree` off `origin/main` rather than editing the live working directory directly, specifically because the live working directory (`/home/allie/Projects/tidemail`, currently on `feat/notification-actions`) has unrelated uncommitted/committed work in it that is not safe to disturb (see "Current State" above). Keep doing this for any further fix.
- Attribution footer convention already in use this session for every commit/PR — keep using it unless a newer system reminder overrides it: commits end with `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>` + `Claude-Session: https://claude.ai/code/session_01F5dEbQSjS4ZT48emxZsbgt`; PR bodies end with `🤖 Generated with [Claude Code](https://claude.com/claude-code)` + the same session URL. (Check the live system reminder for the current session URL/model name before reusing verbatim — it has changed mid-session before, e.g. to Sonnet 5.5, then back.)

## Memory Pointers

- `#4645` — why Explore-type subagents were rejected for this validation task in the first place (their own description excludes design-doc auditing).
- `#4642` — baseline: the audit's own repo-at-`b2402ec` build/vet/lint/test-clean starting point, before any validation began.
- `#4710`, `#4711` — the exact before/after of the #26 root cause and fix in `internal/filter/filter.go` (`attachmentWant` helper).
- `#4743`, `#4740` — the exact before/after of the #1/#2 fix in `internal/ui/outbox.go`/`model.go` (`outboxConfirmEdit`, `editOutboxItem`).
- `#4848` — confirms PRs #139 and #140 are merged, and that `feat/saved-searches-113` is not on origin.
- `#3157`, `#3123` — **do not confuse with the current audit**: these are from a *different*, earlier 22-item pass over the same `tidemail-bug-audit.md` path on 2026-11-13/14, already fully fixed and merged (commit `dda08c6`). The file got reused for a fresh 35-item pass on 2026-11-16, which is the one this handoff is about.
- Searches worth re-running: `search("tidemail-bug-audit.md items validated confirmed refuted")` for the full validation trail; `search("PR 140 outbox uncertain edit confirmation merged")` for the #1/#2 fix trail.

## Confirmed, unfixed — exact locations (24 claims)

| # | File:line | One-line verdict |
|---|---|---|
| 4 | `internal/imap/client.go` (`MarkFlagged`, `uidSetOf`), `mailcore/mailcore.go:203-245` | CONFIRMED mechanism — UID 0 has no guard outside `mail_tags.go:18`; only matters for `mailcore` callers (not the TUI, which already filters `UID != 0`). |
| 10 | `internal/pgpmail/decode.go:36-68`, `find.go:77-80` | CONFIRMED — re-scanning a verified/decrypted body for inline armor lets a quoted PGP block replace the real body. |
| 11 | `internal/ui/sync.go:~921`, `internal/db/reconcile.go:71-110` | CONFIRMED, medium — `ServerState` snapshot taken before the body fetch; no write-time guard against a mark-read made mid-sync. |
| 12 | `internal/ui/sync.go:1003-1015`, `model.go:1187-1219` | CONFIRMED, medium — a bookkeeping (`SetMailboxLastSynced`) write failure is treated as a full fetch failure; needs a rare SQLite write failure to trigger. |
| 13 | `internal/ai/gemini.go:40,52-53`, `validate.go:~127-146`, `internal/ui/model.go:1921`, `overlays.go:414-415` | CONFIRMED — API key in URL query string; `*url.Error` text (which includes the full URL) reaches the UI unredacted on any network error. |
| 14 | `internal/plugin/manager.go:137` | CONFIRMED — `info.Mode().Perm()&0o111==0` is always true on Windows; every plugin rejected there. |
| 15 | `internal/ai/validate.go:~199` | CONFIRMED, low-medium — exact-match against `llama3.2` fails on Ollama's actual `/api/tags` name `llama3.2:latest`. |
| 16 | `internal/mailapp/signature_html.go:58-61,143-148` | CONFIRMED, low-medium — `meta`/`base`/`style` are not banned; needs a deliberately crafted signature file. |
| 17 | `internal/imap/parse.go:580-593` | CONFIRMED — RFC 2047 encoded-word attachment filenames shown literally; only RFC 2231 is decoded. |
| 18 | `internal/db/messages.go:350-360`, `internal/ui/sync.go:1039-1047` | CONFIRMED mechanism, **likely deliberate trade-off** — see "judgment calls" in Next Steps before fixing. |
| 19 | `internal/imap/idle.go:20-27,153-157,183-190` | CONFIRMED mechanism, low likelihood — a transient CAPABILITY fetch failure permanently disables IDLE for the account until restart. |
| 20 | `internal/smtp/smtp.go:356-373`, `mailcore/mailcore.go:281,290` | CONFIRMED — `To`/`Cc`/`In-Reply-To`/`References` built by plain string concat, no CR/LF guard; in-repo callers' safety not independently re-verified by the fork batch. |
| 21 | `internal/mailtemplate/mailtemplate.go:82-104,141-146` | CONFIRMED — a template body starting with `---` (e.g. a horizontal rule) is truncated on reload. |
| 22 | `internal/omarchy/omarchy.go:189,191,244-267` | CONFIRMED — quote-trim runs before comment-strip, and the comment-strip's `i>0` guard never fires when `#` is the very first char; `parseAlacritty` has no comment handling at all. |
| 23 | `internal/ui/snooze.go:86-87,98-99,111` | CONFIRMED — "Later today" can cross midnight past 20:00 local. Extra, audit-missed finding: the doc comment says "three hours after the next full hour" but the code adds **four**. |
| 24 | `internal/ui/model.go:1140-1142` vs `:1063-1067` | CONFIRMED — the non-search `MessagesLoadedMsg` branch has no `else { clearViewportMessage() }` that the search branch already has. |
| 25a | `internal/ui/snooze.go:337-343,354` | CONFIRMED, medium confidence — no staleness/`ReadVersion` guard on the snooze-state load overwriting `items`/`byMessageID`. |
| 25b | `internal/ui/messages.go:346-351`, `threading.go:53,196-215` | CONFIRMED, low — a pending-destructive-action hidden message breaks conversation-collapse bookkeeping; undo re-expands a thread the user just collapsed. |
| 25c | `internal/ui/search_saved.go:195`, `search_history.go:133,183` | CONFIRMED — delete errors silently swallowed (`_ = m.db.DeleteSavedSearch(id)` etc.); `clearSearchHistory` has the same pattern too, which the audit didn't list. Note: `search_saved.go` is new code from *this session's* earlier saved-searches feature work, not pre-existing. |
| 25d | `internal/plugin/manager.go:222-224,274,310,385-401` | PARTIALLY CORRECT — process-slot skip for `test`/`ping` is real; capability-check bypass is narrower than claimed (see "judgment calls" in Next Steps). |
| 25e | `internal/clipboard/clipboard.go:22`, callers in `summary.go:58`, `editor_area.go:17`, `color_picker.go:560` | CONFIRMED, narrow — only misfires when stderr is redirected AND none of wl-copy/xclip/xsel/pbcopy is installed. |
| 25f | `internal/db/drafts.go:312-364`, `internal/ui/drafts.go:255` | CONFIRMED — dedupe is `(mailbox_id, remote_uid)` only, despite the caller comment claiming Message-ID-first; depends on server re-APPEND behavior to actually trigger. |
| 26 | ~~`internal/filter/filter.go`/`parse.go`~~ | **FIXED, merged** (PR #139). Listed here only for completeness of the audit cross-reference. |
| 3 | `internal/auth/oauth.go:42-45,140-146`, `internal/smtp/smtp.go:193-208` | CONFIRMED (mechanism traced end-to-end) — headless `tidemail send`/`outbox process` never call `ui.NewModel` or `mailcore.SetRefreshTokenSaver`, so a rotated refresh token is silently discarded; next headless run gets `invalid_grant`. |
| 5 | `internal/ui/model.go:2901-2913` (on `feat/saved-searches-113` only — not on `main`) | CONFIRMED, **blocked on branch merge** — `idx == m.sidebarCursor` early-return fires before the saved-search re-run check; Enter is also a no-op (falls through every toggle to a nil `selectedMailbox()`). |
| 6 | `internal/ui/model.go:4556-4568` (same unmerged branch) | CONFIRMED, **blocked on branch merge** — `sameSidebarRow` has no `rowKindSavedSearch` case, falls through to `return true`. |
| 7 | `internal/ui/schedule_send.go:67` | CONFIRMED, exact — `time.Now().AddDate(0,0,1).Truncate(24*time.Hour)` truncates on UTC, opens on "today" west of UTC. |
| 8 | `internal/ui/destructive_undo.go:83-104` vs `:130-150` | CONFIRMED — `scheduleArchive` has no analogue of `scheduleDelete`'s `trash.ID != source.ID && trash.Name != source.Name` guard; archiving from Archive self-evicts with no tombstone. |
| 9 | `internal/ui/search_history.go:153-176`, `internal/ui/messages.go:29-31` | CONFIRMED, worse than described — `handleSearchHistoryClick` has no `ListItemLineStride()` call at all, and `searchHistoryRows` has zero `listOffset` windowing (the drafts/messages branches both window; this one doesn't). |
| 27 | `internal/config/config.go:155-175,760-763` | CONFIRMED — `Save` only clears Google creds matching the bundled default; env overrides never match, so they're written in plaintext. `MSClientID` is never cleared at all. |
| 28 | `internal/update/system_package.go:145-169`, `internal/update/update.go:693-718` | CONFIRMED — `probeOwningPackage` exits as "not owned" on a writable dir before reaching its pacman/dpkg/rpm-only probe list; `shadowedBy` uses `filepath.Abs`, never resolves symlinks. |
| 29 | `internal/pluginquery/executor.go:118-134` | CONFIRMED — scope matching is `strings.EqualFold(mb.AccountName, rc.AccountName)`; duplicate account names are only warned about (`account_manager.go:1819`), never blocked. |
| 30 | `internal/cli/interactive.go:106-124`, `internal/cli/mail.go:36-78` | CONFIRMED by tracing the recursive call — `RunHeadless(["draft"])` re-enters `interactiveRoute`, where `missing=true` + `automatic=true` fail to trip the early-return, opening `cliform.OpenTerminal()` anyway. |
| 31 | `internal/cli/interactive.go:224-236`, `internal/cli/mail_reads.go:242-250` | CONFIRMED — `captureRead`'s JSON round-trip always decodes nested lists as `[]any`; `readSuccess`'s `[]map[string]any` type assertion always fails for this path. |
| 32 | `internal/pluginquery/listing.go:42-48`, `internal/db/plugin_queries.go:108-125` | CONFIRMED mechanism — zero `time.Time.Unix()` (`-62135596800`) vs. the DB's `0` sentinel for "no date" means the cursor's `<` comparison can never match those rows. |
| 33 | `internal/update/update.go:501-512` | CONFIRMED, exact — `line[:137]` is a byte-index slice, can split a multi-byte rune. |
| 34 | `internal/update/update.go:355-387` | CONFIRMED — both `RequiresManual` paths skip temp cleanup, but only the unwritable-target path's manual command actually uses the staged binary. |
| 35 | `internal/config/config.go:776-797` | CONFIRMED — `Save` always `os.Rename`s a temp file over `path`; no `filepath.EvalSymlinks` anywhere in the file, so a symlinked config is replaced, not written through. |

(#1, #2 also fixed/merged, PR #140 — omitted from the table above since they're done, not "confirmed, unfixed".)
