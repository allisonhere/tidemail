# TideMail — Whole-App Design Review

**Date:** 2026-06-09
**Scope:** the entire application at working-tree state (main + uncommitted drafts-sync work).
**Companion doc:** `CODE_REVIEW-2026-06-09.md` (focused code review of the uncommitted drafts-sync diff).

---

## 1. System overview

Keyboard-first terminal email client in Go on Bubble Tea (Elm architecture). ~29k LOC across 10 packages; 16 direct dependencies, all reputable (charmbracelet, emersion go-imap/v2, modernc sqlite — pure Go, no cgo).

| Package | LOC | Role |
|---|---|---|
| `internal/ui` | 20,850 (66 files, 29 test) | TUI: model, screens, rendering, background cmds |
| `internal/db` | 2,984 | SQLite store: accounts, mailboxes, messages, drafts, contacts, rules |
| `internal/imap` | 1,274 | IMAP client + MIME parsing |
| `internal/ai` | 1,257 | Summaries/grammar/filters across OpenAI, Claude, Gemini, Ollama |
| `internal/config` | 768 | TOML config + system-keyring secrets |
| `internal/smtp` | 739 | Send: plain/STARTTLS/TLS/XOAUTH2 |
| `internal/update` | 684 | GitHub-releases self-update |
| `internal/filter`, `internal/auth` | ~640 | Deterministic mail rules; Gmail OAuth |

**Overall verdict:** a well-layered, well-tested codebase with consistently good instincts — local-first UX, errors surfaced not swallowed, secrets handled carefully, AI used for *generation* while keeping *execution* deterministic. The two structural debts worth paying down before they compound: the **per-operation IMAP connection model** and **UI package growth** concentrated in a few giant files.

---

## 2. What the design gets right

- **Clean layering with one-way dependencies.** `ui → {db, imap, smtp, ai, config, auth}`; no lateral coupling between the service packages. The IMAP/SMTP/AI packages are small, focused, and independently testable (with injection seams like `smtpNewClient`/`smtpDial` used by real tests).
- **Elm architecture used honestly.** All mutation flows through `Update`; background work is `tea.Cmd` closures returning typed messages (`MailboxSyncedMsg`, `MessagesDeletedMsg`, …). State lives in one place, which made every bug this week diagnosable from the message flow alone.
- **Error-surfacing as a stated convention** (CLAUDE.md: status line or fetch log, never swallowed; intentional ignores annotated). The convention held up under audit — the one violation found (bulk-delete status races) was fixed this week with aggregated results.
- **Local-first with server truth.** Messages cache locally; destructive ops are now remote-first (server confirms before local state changes); tombstones (`deleted_messages`, properly unique-indexed) prevent deleted mail resurrecting through date-granular IMAP `SINCE` re-fetches. The `MessageExists` novelty check keeps re-fetches from looking like new mail.
- **Security posture is above-average for a TUI app:** secrets in the system keyring with explicit plaintext-fallback warnings; secrets redacted from errors, status lines, *and* panic output (`main.go` recovers and redacts); config files permission-checked at startup; OAuth refresh-token revocation detected and surfaced as a sticky re-auth prompt rather than a transient error.
- **AI design: generate, then execute deterministically.** Filters are English → AI → reviewed JSON rule → local deterministic matching with zero per-message AI cost. This is the right division of labor and worth preserving as a principle for future AI features.
- **Strong test culture.** 29 test files in `ui` alone; config/data dirs sandboxed via `TestMain`; models driven by `tea.Msg`s; regression tests added with fixes (XOAUTH2 format, delete failure visibility, draft linkage preservation).

---

## 3. Design risks and recommendations

### 3.1 Connection model: fresh IMAP connection per operation — *highest impact*

Eight call sites across five files (`sync.go`, `messages.go`, `drafts.go`, `move_picker.go`, `filter_runner.go`) each do `imapClient.New(acfg) → Connect → op → Close`. Every sync, delete, move, archive, mark-read, and draft cleanup pays a full TCP+TLS+auth handshake (your own fetch log shows 0.4–1.0s connects), and nothing serializes operations per account. This already caused one shipped bug class (bulk delete exceeding Gmail's 15-connection / Dovecot's 10-connection caps), fixed for deletes by batching — but the *pattern* remains for every other operation, and any future feature that fans out per-message will re-create it.

**Recommendation:** introduce a per-account IMAP session manager — a single long-lived connection (or tiny pool of 2) per account with a serialized work queue and idle timeout. All `tea.Cmd`s submit operations to it instead of dialing. This converts a recurring bug class into an impossibility, cuts every operation's latency by the handshake cost, and centralizes reconnect/token-refresh logic that's currently scattered.

### 3.2 UI package growth — *highest maintainability risk*

`internal/ui` is 71% of the codebase. `settings.go` (2,574), `model.go` (2,372), and `account_manager.go` (1,340) are the gravity wells; `Model.Update` is a giant type-switch over ~30 message types plus key handlers that branch on pane, overlay, and mailbox type (the drafts-mailbox special cases in `handleMainKey`/`focusPane` are recent examples). Each new feature adds branches to shared handlers — risk compounds quadratically.

**Recommendation:** no rewrite needed, but adopt a sub-model convention before the next big feature: each overlay/screen owns `Update`/`View` on its own struct (compose and account manager already half-follow this) and `model.go` only routes. Settings is the best first extraction candidate — it's already self-contained and would cut `model.go`'s handler surface meaningfully. Target: no file over ~1,200 lines.

### 3.3 Schema consistency: enforce invariants in SQLite, not in Go

The codebase already knows how to do this right — `messages` has `UNIQUE(mailbox_id, uid)` with a real `ON CONFLICT` upsert, and tombstones have unique indexes. But `drafts` enforces uniqueness by check-then-insert in Go (race-prone across concurrent `tea.Cmd` goroutines; see companion code review, finding #2), and draft↔account linkage uses *name strings* (`account_name`, `account_user`) while everything else uses `account_id` FKs — renaming an account silently orphans its drafts.

**Recommendation:** unique index on `drafts(mailbox_id, remote_uid)` (partial, `WHERE remote_uid != 0`) + import via `INSERT … SELECT … WHERE NOT EXISTS`; migrate drafts to `account_id`. General principle going forward: any "does X already exist?" Go check guarding an insert should be a constraint.

### 3.4 Sync model gaps: UIDVALIDITY and remote deletions

Two protocol-level gaps in an otherwise solid sync design:

1. **UIDVALIDITY is not tracked** (no column on `mailboxes`, not checked on select). If a server resets a mailbox's UIDVALIDITY (rebuilds, migrations, some Dovecot operations), every stored UID becomes meaningless — deletes/moves would target wrong messages and the cache silently diverges. Low frequency, high blast radius.
2. **Sync is append-only**: mail deleted/moved server-side (e.g. from the phone) never disappears locally; there's no reconciliation pass. Users see ghosts until a manual cache reset. The drafts stale-mirror problem is the same gap wearing a different hat.

**Recommendation:** store UIDVALIDITY per mailbox; on mismatch, drop and refetch that mailbox's cache. Add a periodic cheap reconciliation (`UID SEARCH ALL` compare, or `ESEARCH`) to the sync path — this also gives the drafts mirror its pruning step for free.

### 3.5 Smaller observations

- **`drafts` account routing by index** (`account_index` stored in DB, trusted by `NewComposeFromDraft`) is fragile under config reordering — verify name/user before trusting an index (companion review, finding #4).
- **Render-path queries:** sidebar counts run SQL during `View` (per keystroke). Fine at current scale; cache on the model and refresh via messages when it grows.
- **`cleanEmail`/address parsing exists in both `smtp` and `ui` (`parseAddressList`)** — one address-handling home would prevent format drift.
- **Self-update replaces the binary in place** with no signature verification beyond GitHub TLS + release asset naming. Reasonable for the project's threat model; a checksum file in releases would be a cheap upgrade.
- **`go-imap/v2` is a beta dependency** (`v2.0.0-beta.8`). It's the right library, but pin-and-review on bumps; betas move.
- **Dead plumbing:** `MarkDraftRemoteSynced`, `FindDraftsMailbox` are unused until the local→remote draft push lands — fine, but tag them with a TODO referencing the plan so they don't read as cruft.

---

## 4. Prioritized roadmap

| # | Item | Effort | Pays for |
|---|---|---|---|
| 1 | Fix the 3 confirmed drafts-sync bugs (companion review) | S | Correctness of uncommitted work |
| 2 | Drafts unique index + SQL import + `account_id` migration | S–M | Kills race class + identity drift |
| 3 | Per-account IMAP session/queue | M | Latency on every op; removes connection-cap bug class |
| 4 | UIDVALIDITY tracking + deletion reconciliation | M | Cache correctness vs. server reality |
| 5 | Settings → sub-model extraction; cap file growth | M | Sustained velocity in `ui` |
| 6 | Address-handling consolidation, update checksums | S | Hygiene |

---

## 5. Closing note

This week's bugs (XOAUTH2 double-encoding, connection-cap bulk deletes, silent tombstone divergence) share one root: **invariants held in Go code and convention rather than in the type system, schema, or a single owning component**. The codebase's instincts are good — the fixes consistently moved invariants downward (raw bytes to the stdlib encoder, one connection per batch, server-confirmation before local mutation). Items 2–4 above continue that same motion, and they're the difference between a good hobby codebase and one that stays correct as features stack.

*Generated by Claude Code design-review session, 2026-06-09.*
