# Tidemail Status

Tidemail is a keyboard-first TUI email client for the terminal, built in Go with
Bubble Tea. It supports multiple IMAP/SMTP accounts, a unified inbox, AI summaries,
and incremental sync. See `CLAUDE.md` for the architecture map.

## Working

- **Multi-account IMAP**: connect, list mailboxes, fetch messages, move, delete,
  mark read/unread. Delete moves remote mail to Trash when a Trash mailbox is
  available, with permanent expunge as the fallback. UID SEARCH SINCE for
  incremental sync.
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
- Lint config in `.golangci.yml`; `golangci-lint` runs clean and is enforced in CI.

## Test coverage

- Covered: `internal/config`, `internal/db`, `internal/imap`, `internal/smtp`,
  `internal/auth` (token (de)serialization + refresh), and parts of `internal/ui`.
- Thin / wanted:
  - `internal/ui` — the large `model.go` Update loop has only targeted tests.
  - `internal/auth` — the interactive browser callback flow in
    `StartGmailOAuthFlow` is not exercised (token exchange/refresh is).
- Lint backlog cleared: `golangci-lint run` is clean across the repo.
