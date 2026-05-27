# Tidemail Status

Tidemail is a TUI email client for the terminal, built in Go. It supports
multiple IMAP/SMTP accounts, threaded conversations, AI summaries, and
incremental sync.

## Working

- **Multi-account IMAP**: connect, list mailboxes, fetch messages, move,
  delete, mark read/unread. UID SEARCH SINCE for incremental sync.
- **SMTP send**: plain, STARTTLS, and direct TLS (port 465). Reply/forward
  threading headers.
- **Three-pane UI**: accounts sidebar, message list, message content.
- **Threading**: messages grouped by subject/references.
- **AI summaries**: per-thread and per-message summaries (OpenAI, Claude,
  Gemini, Ollama).
- **Search**: full-text search across local message DB.
- **Settings**: theme, display density, AI provider config, update checks.
- **Update checker**: github-releases check, auto-update with running
  process restart.

## Needs Tests

- `internal/imap/` — client.go, parse.go (0 tests)
- `internal/smtp/` — smtp.go (0 tests)

## Branch

Work is on `codex-multi-account-mail-client` (not yet merged to `main`).
