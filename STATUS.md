# TideMail Status

TideMail is a keyboard-first TUI email client for the terminal, built in Go with
Bubble Tea. It supports multiple IMAP/SMTP accounts, a unified inbox, AI summaries,
and push or interval sync. See `docs/architecture.md` for the architecture map.

Current release: **v1.7.0**.

## Working

- **Interactive CLI**: `tidemail cli` command chooser, step-by-step compose/draft
  wizard with Ripple, attachment browser, review, durable sending and undo.
  Automatic prompts respect pipes, JSON, agents, and `--no-interactive`; explicit
  `--interactive` renders through the controlling terminal.
  Settings → CLI adds switches for quitting after a send, skipping review, and the
  default signature (interactive use only).
- **Headless mail CLI**: configured-account sends and drafts, Markdown and
  attachments, cached inbox list/search/read, account and outbox metadata,
  explicit queue processing, and portable agent skills for Codex, Claude, Pi,
  and Hermes. Agent capabilities and invocation approval separate reading,
  drafting, and sending. See the [CLI reference](docs/cli.md) for queue scheduling,
  JSON output, installation, and current limits.

- **Standard folders**: Gmail's Sent Mail, Drafts, All Mail, Trash, Spam,
  Starred, and Important remain visible under unselectable server containers.
  Explicit folder hiding and section collapse still apply.

- **Multi-account IMAP**: connect, list mailboxes, fetch, move, delete, archive,
  mark read/unread, star. Delete moves remote mail to Trash when one is available,
  with permanent expunge as the fallback. Incremental sync via UID SEARCH SINCE,
  with a 100-message inbox first page, a 25-message first page for attachment-heavy
  folders, and 100-message pagination. Resting on a stale non-inbox folder starts
  one cancellable, silent refresh; `Enter` or `s` refreshes it immediately.
  Config, database rows, drafts, queued sends, sessions, OAuth caches, and
  keychain entries share a stable account ID, so renames and duplicate display
  names cannot cross-wire accounts.
- **Server discovery** (`internal/autodiscover`): a Custom account's servers are
  looked up from the email domain: RFC 6186 SRV (implicit-TLS IMAP only), then MX
  mapped to the Gmail/Outlook/Yahoo/iCloud presets, then the domain's own Mozilla
  autoconfig file over HTTPS (no ISPDB). It pre-fills the form only. Changing the
  address clears inferred settings while preserving server values the user edited.
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
  PLAIN, or XOAUTH2 for OAuth Gmail accounts. Reply/reply-all/forward threading headers; display
  name preserved in `From:`. Delivered mail is appended to the account's discovered
  Sent folder for providers that do not file SMTP submissions themselves; Gmail is
  skipped to avoid duplicate copies.
- **Drafts**: local drafts plus a one-way mirror of server drafts, deduplicated by a
  partial unique index on `(mailbox_id, remote_uid)`.
- **Folder management**: nested folder tree with collapse; create, rename, and delete real IMAP
  folders from the sidebar (`n`/`r`/`d`, subtree-aware, system folders protected); local hide and
  reorder and walk in/out of parents (`H`, `Shift+J`/`K`, `>`/`<`) stored in `mailbox_prefs`.
  NIL-delimiter servers use flat literal folder names: top-level creation and reorder remain,
  while nesting and reparenting are unavailable.
- **Three-pane UI**: accounts sidebar, message list, message content. Pane header
  bars can be shown or removed; when removed, the focused title and shortcuts
  move to the status line and each pane gains a content row.
  Global search uses a dedicated row above message subjects. Threaded view, HTML
  rendering with inline raster images (Kitty graphics on Ghostty/Kitty/Rio,
  text placeholders elsewhere; remote images blocked until `i`), attachment
  saving, message source view with copy (`Ctrl+O`), open HTML in the browser
  (`Ctrl+B`), labelled actionable links that keep the selected entry visible,
  full-URL copying, labelled `mailto:` actions, contacts manager. HTML LINKS
  exclude source-only resource URLs.
- **Receipt tables**: detect simple item/amount tables without semantic headers,
  preserve merged-cell totals, align amounts, and use labelled records in narrow
  panes. Complex row-spanning tables retain the text fallback.
- **Newsletter cards**: layout cells holding an image, heading, description, or
  button stay together and are separated by a thin rule. Side-by-side cards
  stack in reading order; quoted mail is not grouped.
- **Image settings**: Settings → Display → Reading → Images offers Auto/Off,
  terminal capability information, and immediate application on save.
- **foot/WezTerm limitation**: inline images are currently unsupported. foot
  supports Sixel, but TideMail's renderer requires Kitty Unicode placeholders.
  TideMail uses text placeholders automatically; email text and attachment
  saving still work.
- **Undo** for destructive message actions and for sending.
- **Spell check** (opt-in, Settings → Editor): offline English spell check in compose via the
  `tidespell` library. Misspellings are underlined in the body (ripple `StyleKey` hooks), `F7`
  steps through them with suggestions, skip, ignore, and add-to-dictionary (stored in the
  `spell_words` table; Settings → Editor → Personal dictionary lists, adds, and removes them). Quoted and forwarded text is never checked. The word under the caret is
  not underlined while typing; correspondents' names, local parts and domains from the mail cache
  are accepted; suggestions use `tidespell` v0.2.0 scored ranking (edit distance, typing patterns,
  phonetics, frequency tiers).
- **AI**: summaries and grammar across OpenAI, Claude, Gemini, and Ollama. Mail rules
  are AI-generated once into reviewed JSON, then matched locally and deterministically —
  no per-message AI cost.
- **Compose layout**: grows up to 110×48 cells, with matching stored/rendered
  editor geometry. Key-only rounded pills highlight Send in the success color;
  plain themes use bracketed keys. A fixed three-row footer keeps Esc first on
  its last row and shows the sign/encrypt toggles and status on the state line.
- **Compose Find/Replace**: `Ctrl+F` opens live Find; `Ctrl+R` in the bar shows
  replacement controls. Replace All is one undo step; optional regex is exposed
  by Settings → Editor → Advanced regex search. Vim supports `/`, `?`, `n`, `N`.
  The bar sits in the footer; `Alt+Enter` replaces all, `Alt+C` toggles case and
  `Alt+R` regex. Requires Ripple v0.5.0.
- **Compose**: optional vim mode (`compose_vim`), backed by the extracted
  [`ripple`](https://github.com/allisonhere/ripple) editor library.
- **Gmail & Outlook OAuth** (optional): the account form's **Auth** row is a
  `‹ ›` selector (App password ⇄ OAuth) for both providers; the OAuth guidance
  shows only while that cluster is focused. Gmail uses bundled Desktop app
  credentials in official builds with automatic browser sign-in; Outlook
  defaults to Thunderbird's shared public client (paste-back flow) or a custom
  `TIDEMAIL_MS_CLIENT_ID` (device-code). XOAUTH2 for IMAP and SMTP; refresh
  token stored in the keychain and rotation-persisted.
- **Thunderbird / IMAP mail tags**: discover server keywords, display default
  Thunderbird labels, map custom names and colors per account, search by keyword
  or name, and manage tags within the Category choices in `e`. Compatible category
  selections save to IMAP; Space toggles individual tags and preserves the rest.
  Optional Tag icons use colored tag glyphs with an ASCII fallback. External changes refresh on sync;
  unsupported servers reject edits without claiming success. No profile import,
  offline queue, bulk tagging, or automatic AI assignment.
- **Search**, unread-only filtering, command palette, desktop notifications.
- **Experimental plugins**: manual and automatic metadata classification,
  stored tags with local corrections (`e` on a message), Needs You with immediate
  sidebar count updates for archive/move and undo (delete on confirmation), result cards
  with technical Details, and a host-owned `u` unsubscribe action that uses TideMail's existing confirmed flow without
  exposing unsubscribe links to plugins.
- **Plugin catalog**: Settings → Plugins → `b` (and the palette) lists the
  official plugins from the plugins repository's release `catalog.json` and
  installs, updates and removes them (HTTPS GitHub only, SHA-256 checked, plain
  files only, atomic swap, never run during install, live reload). Needs a
  published plugins release; Windows plugin builds are not offered yet.
- **PGP via plugin** (experimental): a plugin with the `message_raw` and
  `message_compose` permissions decrypts/verifies received mail (`D`, offered in the
  reading pane and key hints, memory only) and signs/encrypts outgoing mail (`Alt+S`/`Alt+E` in
  compose; PGP/MIME, fails closed). The `pgp` plugin in the plugins repository uses
  GnuPG. Not yet: attachments inside encrypted mail, encrypted-mail search,
  CLI sending, key management.
- **Settings**: theme, display density, pane corners, pane header bars, Needs You / Waiting on Them sidebar rows, modal
  drop shadow, AI provider config, editor, update checks, a dedicated Plugins
  section with inline administration and configuration, a read-only Changelog
  view (the embedded CHANGELOG.md, scrollable, works offline), and an optional
  Support page with open/copy actions for the GitHub Sponsors link. About is an
  animated "Ocean night" scene and Support an animated "Café" scene; plain
  (VT52) themes keep the original static pages.
- **Mouse support** (opt-in, Settings → Display): click to select rows/focus panes or select LINKS entries (clicks never open reader links),
  double-click to open a message, scroll wheel over the sidebar, list, and reading pane,
  right-click for a context menu of that row's actions (folder, account, or message), and click the search row to resume
  editing. Overlays and the status line are not yet clickable; no drag support.
- **Update checker**: GitHub-releases check with checksum-verified self-update,
  user-local install fallback, and an in-app progress popup. `U` offers an
  update whatever way TideMail was installed: it installs in place where it can,
  and where a distro package owns the binary it opens a window naming that
  package and offering the right command to copy — an AUR helper for a foreign
  pacman package, since `pacman -Syu` silently skips those.

Authentication is app-password for every provider, plus optional OAuth2 (XOAUTH2)
for Gmail and Outlook. The method is an explicit, persisted per-account setting
(`auth_method` in `config.toml`, default `password`); OAuth never activates from
an inferred signal such as a leftover keychain token. Gmail bundles no client —
the user brings their own via `[oauth]` / env vars, keeping personal use free of
Google's CASA verification.
Outlook falls back to Thunderbird's shared public client (no setup) and needs no
verification on Microsoft's side; the same path also covers Microsoft 365 /
Exchange Online mailboxes. `internal/auth` holds per-provider device-code +
authorization-code (paste-back) flows over a shared access-token cache with
refresh-token rotation (`internal/auth/oauth.go`), and the `IsAuthFailure` /
`IsTokenRevoked` classifiers that drive the "press M to re-authenticate" hint.
SASL support is XOAUTH2 and PLAIN only — no NTLM/GSSAPI, so an on-prem Exchange
server with basic auth disabled is out of reach (basic-auth on-prem works via the
Custom provider).

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

Delete countdown prototype: messages remain visible for the six-second undo
window and show pending feedback until server confirmation. Delete counts
change on confirmation; archive and move retain their existing presentation.
