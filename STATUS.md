# Tidemail Status

Tidemail is a keyboard-first TUI email client for the terminal, built in Go with
Bubble Tea. It supports multiple IMAP/SMTP accounts, a unified inbox, AI summaries,
and incremental sync. See `CLAUDE.md` for the architecture map.

## Working

- **Multi-account IMAP**: connect, list mailboxes, fetch messages, move, delete,
  mark read/unread. UID SEARCH SINCE for incremental sync.
- **SMTP send**: plain, STARTTLS, and direct TLS (port 465), plus Gmail XOAUTH2.
  Reply/forward threading headers; display name preserved in `From:`.
- **Three-pane UI**: accounts sidebar, message list, message content.
- **AI summaries & grammar**: OpenAI, Claude, Gemini, Ollama.
- **Search**, unread-only filtering, command palette, desktop notifications.
- **Settings**: theme, display density, AI provider config, update checks.
- **Update checker**: GitHub-releases check with in-place self-update.

## Quality gates

- CI (`.github/workflows/ci.yml`) runs `gofmt`, `go build`, `go vet`,
  `go test ./... -race`, and `golangci-lint` on every push/PR.
- Lint config in `.golangci.yml`; CI uses `only-new-issues`, so the existing
  backlog is grandfathered and only new issues fail the build.

## Test coverage

- Covered: `internal/config`, `internal/db`, `internal/imap`, `internal/smtp`,
  `internal/auth` (token (de)serialization + refresh), and parts of `internal/ui`.
- Thin / wanted:
  - `internal/ui` — the large `model.go` Update loop has only targeted tests.
  - `internal/auth` — the interactive browser callback flow in
    `StartGmailOAuthFlow` is not exercised (token exchange/refresh is).
- Backlog: `golangci-lint run` reports ~80 pre-existing issues (unused funcs,
  unchecked errors, staticcheck) that are grandfathered by CI; worth burning down.
