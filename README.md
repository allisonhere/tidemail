# TideMail

![TideMail screenshot](screen.png)

A keyboard-first terminal mail client built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss).

TideMail is being refactored from Tide into a dedicated IMAP/SMTP client. The current milestone is focused on multi-account mail workflows: unified inbox, reliable server-backed actions, compose/reply, and a fast command palette.

The reusable UI primitives behind this interface are published as
[`tideui`](https://github.com/allisonhere/tideui).

## Features

- Three-pane mail layout: accounts, messages, content
- Unified Inbox across all configured accounts
- IMAP/SMTP accounts using passwords or app passwords
- Account manager for adding, editing, deleting, and discovering mailboxes
- Server-backed sync, read/unread, archive, delete, compose, and reply
- Archive auto-detection via `\Archive`, `Archive`, `Archives`, or `All Mail`
- Command palette for common mail actions
- Search and unread-only filtering
- Optional actionable links in the message content pane
- AI summaries with copy and save-to-Markdown actions
- Theme-aware dialogs, overlays, and terminal background sync

## Usage

```bash
go build -o tidemail .
./tidemail
```

Config is stored in `~/.config/tidemail/config.toml`. The local SQLite cache is stored in `~/.local/share/tidemail/mail.db` unless `XDG_DATA_HOME` changes that path.

Open account management with `m`, add an IMAP/SMTP account, then sync the selected mailbox with `f`. Press `f` on the Unified Inbox to sync every account's inbox at once. Use `F` to sync all mailboxes. Configure a per-account `sync_minutes` interval for automatic background refresh.

## Credential safety

TideMail currently stores IMAP/SMTP passwords and AI API keys in the local config file. Treat `~/.config/tidemail/config.toml` like a secret.

Recommended setup:

```bash
chmod 700 ~/.config/tidemail
chmod 600 ~/.config/tidemail/config.toml
```

TideMail now writes the config directory as `0700` and the config file as `0600` when it saves settings. On startup it warns if an existing config file is readable by other users. Passwords and AI API keys are redacted from startup errors, runtime status messages, and account-manager test/save failures.

For hosted providers, use provider-specific app passwords rather than your primary account password:

- Gmail: Google Account → Security → App passwords
- Yahoo: Account Security → Generate app password
- iCloud: Apple ID → Sign-In and Security → App-Specific Passwords

If you paste or accidentally expose an app password, revoke it and create a new one.

Example account config:

```toml
theme = "catppuccin-mocha"

[[account]]
name = "Personal"
imap_host = "imap.example.com"
imap_port = 993
imap_tls = true
smtp_host = "smtp.example.com"
smtp_port = 587
smtp_tls = true
user = "alice@example.com"
password = "app-password"
from = "Alice <alice@example.com>"
sync_minutes = 5  # auto-sync every 5 min (0 = off)
```

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `p` or `:` | Command palette |
| `m` | Account manager |
| `f` | Sync current mailbox (Unified Inbox: syncs all inboxes) |
| `F` | Sync all mailboxes |
| `c` | Compose |
| `r` | Toggle read/unread in message list, reply from content |
| `a` | Archive selected message |
| `d` | Delete selected message |
| `R` | Mark selected mailbox/account read |
| `/` | Search messages |
| `u` | Toggle unread-only view |
| `o` | Open selected content link |
| `Ctrl+N` / `Alt+N` | Next content link |
| `Ctrl+P` / `Alt+P` | Previous content link |
| `s` | AI summary |
| `S` | Settings |
| `T` | Theme picker |
| `Ctrl+D` | Save attachments to folder |
| `?` | Help |
| `q` | Quit |

## Settings

Settings are opened with `S`.

- Display: icons, date format, mark-read behavior, focus line, actionable links, reading width, browser command, density, and quit confirmation
- Accounts: edit account details and set a per-account `sync_minutes` interval for automatic background refresh
- Updates: check, install, restart, or copy a manual install command
- AI: OpenAI, Claude, Gemini, or Ollama summary settings
- About: repository and issue links

## Development

Run the full suite:

```bash
go test ./...
```

The mail refactor intentionally removes RSS/GReader/OPML behavior. Any remaining RSS-era naming in internal style names is compatibility debt, not product direction.
