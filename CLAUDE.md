# CLAUDE.md

Architecture and working notes for TideMail — a keyboard-first terminal email client
built in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea). This file
orients both contributors and AI assistants; see `README.md` for user-facing docs.

## Layout

- `main.go` — entry point; sets up the Bubble Tea program and recovers from panics with
  secret redaction.
- `internal/ui` — the TUI. `Model` + the `Update` loop live in `model.go`; screens are
  split across `account_manager.go`, `compose.go`, `content.go`, `messages.go`,
  `sidebar.go`, `settings.go`, `overlays.go`. Rendering helpers in `form_render.go`,
  `styles.go`, `themes.go`. Background work (sync, AI, update) is dispatched as
  `tea.Cmd`s — see `sync.go`.
- `internal/imap` — IMAP client over `go-imap/v2` (`client.go`), message parsing
  (`parse.go`).
- `internal/smtp` — SMTP send: plain, STARTTLS, TLS(465), and Gmail XOAUTH2.
  `cleanEmail` produces the bare envelope address; `buildRaw` builds the MIME message
  (the `From:` header keeps the display name).
- `internal/db` — SQLite via `modernc.org/sqlite` (pure Go, no cgo): accounts,
  mailboxes, messages.
- `internal/config` — TOML config at `~/.config/tidemail/config.toml`; secrets in the
  system keyring with a config fallback (`keyring.go`).
- `internal/auth` — Gmail OAuth2 (browser flow, token exchange/refresh).
- `internal/ai` — summaries/grammar across OpenAI, Claude, Gemini, Ollama.
- `internal/update` — GitHub-releases update check + in-place self-update.

## Key flows

- **Sync**: `Model.syncMailboxCmd` loads the mailbox/account from the DB, connects via
  `imap.Client`, `FetchSince(lastSynced)`, stores new messages, updates last-synced and
  unread counts, and logs timings via `logFetch` to `config.LogPath()`. Errors flow back
  as a `MailboxSyncedMsg{Err}` and surface on the status line.
- **Config writes**: always go through `Model.saveConfig`, which surfaces a failed write
  on the status line (don't call `config.Save` directly from the UI — failures must be
  visible). `configSave` is a seam for tests.
- **Paths**: DB at `~/.local/share/tidemail/mail.db` (honors `XDG_DATA_HOME`); logs at
  `config.LogPath()`.

## Dev commands

```bash
go build -o tidemail .      # build
go test ./...               # tests (add -race in CI)
gofmt -w .                  # format (CI fails on unformatted files)
golangci-lint run           # lint (config: .golangci.yml)
```

CI (`.github/workflows/ci.yml`) runs build/vet/test/lint on push and PR; releases are
built by `.github/workflows/release.yml` on `v*` tags (see `deploy.sh`).

## Conventions

- UI error paths should surface to the user (`setStatus`) or the fetch log, not be
  swallowed. Intentional ignores are annotated `//nolint:errcheck`.
- Tests sandbox config/data dirs via `internal/ui/testmain_test.go`; build a model with
  `NewModel(db, cfg, "dev", false)` and drive it with `tea.Msg`s.
- Lint runs in CI but is currently **advisory** (the golangci-lint job uses
  `continue-on-error`) because of a ~80-issue backlog; burning it down lets it become a
  hard gate. Run `golangci-lint run` locally and keep your own changes clean.
