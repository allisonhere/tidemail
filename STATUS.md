# TideMail Status

TideMail is a keyboard-first TUI email client for the terminal, built in Go with
Bubble Tea. It supports multiple IMAP/SMTP accounts, a unified inbox, AI summaries,
and push or interval sync. See `CLAUDE.md` for the architecture map.

Current release: **v1.0.5**.

## Working

- **Multi-account IMAP**: connect, list mailboxes, fetch, move, delete, archive,
  mark read/unread, star. Delete moves remote mail to Trash when one is available,
  with permanent expunge as the fallback. Incremental sync via UID SEARCH SINCE,
  with pagination past the first 100 messages.
- **Connection model**: a per-account session pool (`internal/imap/pool.go`) keeps one
  reusable connection per account, serializes that account's operations, revalidates
  with NOOP, redials on config change, and reaps after 3 minutes idle. Every UI
  operation goes through `SessionPool.Do`; the two exceptions are the credential-check
  paths in `account_manager.go`, which deliberately dial unpooled because they verify
  configs that may be wrong or unsaved.
- **Push mail**: an IMAP IDLE watcher (`internal/imap/idle.go`) holds its own dedicated
  connection per account — deliberately outside the pool, since IDLE monopolizes a
  connection and would otherwise serialize every other operation behind it.
  `sync_minutes = 0` selects push; a positive value polls on that interval.
- **Cache correctness**: UIDVALIDITY is tracked per mailbox and invalidates the local
  cache when a server renumbers (`internal/db/reconcile.go`). Tombstones keep deleted
  mail from resurrecting through date-granular re-fetches.
- **SMTP send**: plain, STARTTLS, and direct TLS (port 465). Auth is app-password over
  PLAIN, or XOAUTH2 for OAuth Gmail accounts. Reply/forward threading headers; display
  name preserved in `From:`.
- **Drafts**: local drafts plus a one-way mirror of server drafts, deduplicated by a
  partial unique index on `(mailbox_id, remote_uid)`.
- **Three-pane UI**: accounts sidebar, message list, message content. Threaded view,
  HTML rendering, attachment saving, contacts manager.
- **Undo** for destructive message actions and for sending.
- **AI**: summaries and grammar across OpenAI, Claude, Gemini, and Ollama. Mail rules
  are AI-generated once into reviewed JSON, then matched locally and deterministically —
  no per-message AI cost.
- **Compose**: optional vim mode (`compose_vim`), backed by the extracted
  [`ripple`](https://github.com/allisonhere/ripple) editor library.
- **Gmail OAuth** (optional): the account form's Gmail **Auth** row is a
  `‹ ›` selector (App password ⇄ OAuth); the OAuth guidance shows only while
  that cluster is focused. Bring-your-own OAuth client
  (`TIDEMAIL_GOOGLE_CLIENT_ID` / `_SECRET`), device-code sign-in (`ctrl+o`) with
  an authorization-code paste-back fallback. XOAUTH2 for IMAP and SMTP; refresh
  token stored in the keychain and rotation-persisted.
- **Search**, unread-only filtering, command palette, desktop notifications.
- **Settings**: theme, display density, AI provider config, editor, update checks.
- **Update checker**: GitHub-releases check with checksum-verified self-update,
  user-local install fallback, and an in-app progress popup.

Authentication is app-password for every provider, plus optional OAuth2 (XOAUTH2)
for Gmail. TideMail bundles no OAuth client — the user supplies their own via
`[oauth]` / env vars, which keeps personal use free of Google's CASA
verification. `internal/auth` holds the Google device-code and
authorization-code flows, a shared access-token cache with refresh-token
rotation, and the `IsAuthFailure` / `IsTokenRevoked` classifiers that drive the
"press M to re-authenticate" hint. Microsoft/Outlook OAuth is built but parked on
the `outlook-oauth` branch — reviving it is a follow-up.

## Quality gates

- CI (`.github/workflows/ci.yml`) runs `gofmt`, `go build`, `go vet`,
  `go test ./... -race`, and `golangci-lint` on every push/PR.
- Lint config in `.golangci.yml`; `golangci-lint` runs clean and is enforced in CI.
- The tree currently builds clean, vets clean, and the full test suite passes.

## Test coverage

- Covered: `internal/config`, `internal/db`, `internal/imap` (including the session
  pool), `internal/smtp`, `internal/filter`, `internal/update`, and much of
  `internal/ui` — 53 test files there against 46 source files.
- Thin / wanted:
  - `internal/ui` — the large `model.go` Update loop has only targeted tests.
  - No UI test fakes or injects IMAP. `SessionPool` and `Client` are both concrete
    types, so UI commands that touch the network are unexercised; tests avoid the path
    by using accounts with an empty `IMAPHost`. Introducing an interface seam on the
    Model's pool field is the lever if that coverage becomes worth having.

## Known debt

- **`internal/ui` file growth.** `model.go` (~3,100 lines) and `settings.go` (~2,800)
  concentrate most of the package's 32k lines. Extracting Settings into its own
  sub-model with an `Init`/`Update`/`View` — rather than branching inside the master
  Update switch — is the recommended next step. This is the main open item from the
  June 2026 design review; the rest of that roadmap has shipped.
