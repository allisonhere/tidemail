# TideMail

![TideMail screenshot](screen.png)

TideMail is a keyboard-first terminal mail client built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss).

The compose body uses [ripple](https://github.com/allisonhere/ripple), TideMail's extracted Go TUI editor library. Ripple supports selection, undo and redo, system clipboard copy, cut, and paste, word movement, and soft wrap.

## Install

Quick install for Linux and macOS on amd64 or arm64:

```bash
curl -fsSL https://raw.githubusercontent.com/allisonhere/tidemail/main/install.sh | sh
```

You can also download a binary from the [latest release](https://github.com/allisonhere/tidemail/releases/latest).

Build from source:

```bash
git clone https://github.com/allisonhere/tidemail
cd tidemail
go build -o tidemail .
./tidemail
```

## Features

- Three-pane mail layout for accounts, messages, and content
- Unified Inbox across configured accounts
- Multi-select with `Space` for bulk delete, archive, move, and mark-read actions
- Select all messages in the current view with `A`
- Full email headers, toggled with `Ctrl+E` and configurable in Settings
- Spam and auth headers with color-coded SPF, DKIM, and DMARC results
- Message text copy from the content pane with Vim-style `v` or `V`, then `y` or `Ctrl+C`
- Compose body editor with selection, select-all, undo and redo, system clipboard copy, cut, and paste, word movement, and soft wrap
- Optional vim editing in compose, enabled in Settings -> Editor or `[display] compose_vim`
- IMAP and SMTP accounts with passwords or app passwords stored in the system keychain
- Account manager for adding, editing, deleting, and discovering mailboxes
- Compose suggestions from saved contacts and addresses found in synced mail
- Per-account From addresses, signatures, and a sender picker in compose
- vCard import/export for email, display name, phone, organization, title, and notes
- Server-backed sync, read and unread state, archive, move, delete, compose, and reply
- Local drafts that save to the selected sender account while you type
- A configurable send delay that lets `Ctrl+Z` cancel and reopen queued mail
- One-key unsubscribe from the content pane with `Ctrl+U`
- Local-first delete that hides messages at once, moves remote mail to Trash when available, and keeps deleted mail from returning on later syncs
- Archive detection for `\Archive`, `Archive`, `Archives`, and `All Mail`
- Trash detection for `\Trash`, `Trash`, `Deleted Items`, `Deleted Messages`, and Gmail's Trash label
- Command palette for mail actions, compose commands, AI summaries, and save-attachment actions
- Global message search with `/` and unread-only filtering
- Optional actionable links in the message content pane
- File browsers for attachments and saved attachments, with dotfiles hidden until you press `.`
- AI summaries with copy and save-to-Markdown actions
- AI grammar and spell check in compose with a preview overlay
- AI mail filters that turn a plain-English rule into a deterministic local filter
- Theme-aware dialogs, overlays, and terminal background sync
- Collapsible account folders in the sidebar
- Desktop notifications for new unread mail from auto-sync, with sender and subject details

Optional vim editing in compose adds Normal, Insert, and Visual modes; motions such as `hjkl`, `w`, `b`, `e`, `0`, `^`, `$`, `gg`, and `G`; counts; edits such as `x`, `dd`, `yy`, `p`, `dw`, and `cw`; visual `v` and `V`; undo and redo with `u` and `Ctrl+R`; and a `:` command line where `:w` and `:wq` send and `:q` cancels. TideMail keeps it off by default.

AI mail filters let you describe a rule, such as "move newsletters from substack to Reading." TideMail turns it into a local rule with no per-message AI cost. You choose one account or all accounts, review the generated rule, then save it, run it once on a mailbox, or run it across existing mail. Filters support move, mark read, archive, delete, and spam actions. Open filters from the command palette with `p`, then choose "filters."

## Usage

```bash
go build -o tidemail .
./tidemail
```

TideMail stores config in `~/.config/tidemail/config.toml`. It stores the local SQLite cache in `~/.local/share/tidemail/mail.db` unless `XDG_DATA_HOME` changes that path.

Press `M` to open account management, add an IMAP/SMTP account, then press `s` to sync the selected mailbox. Press `s` on the Unified Inbox to sync each account's inbox. Press `F` to sync all mailboxes. Set `sync_minutes` per account for background refresh.

Compose autosaves your message to the sending account's Drafts mailbox while you type. If you close compose with content, TideMail prompts you to save or discard. Open the Drafts mailbox to see saved drafts, press `Enter` to reopen a draft, and press `d` to delete it. Sending a draft removes it from Drafts.

Press `C` to open Contacts. Compose lists saved contacts before addresses found in your mail. Press `n` to add a contact, `f` to browse seen addresses, `i` to import a vCard file, or `x` to export `contacts.vcf`.

## Credential Safety

TideMail stores IMAP/SMTP passwords and AI API keys in the system keychain through `secret-tool` on Linux and Keychain on macOS. Empty `password`, `openai_key`, `claude_key`, and `gemini_key` fields in the config file come from the keychain at startup. When you save settings, TideMail moves in-memory secrets to the keychain and removes them from the config file.

If `secret-tool` is missing, TideMail stores passwords and API keys in `~/.config/tidemail/config.toml`. Treat that file as a secret.

- Gmail: enable 2-Step Verification, then use Google Account -> Security -> App passwords
- Yahoo: use Account Security -> Generate app password
- iCloud: use Apple ID -> Sign-In and Security -> App-Specific Passwords

If you paste or expose an app password, revoke it and create a new one.

## Gmail

Gmail requires a Google App Password. TideMail removed OAuth "Sign in with Google" in v0.5.0.

1. Enable 2-Step Verification on your Google account.
2. Create an app password at myaccount.google.com/apppasswords.
3. Press `M` to open the account manager, then add or edit your Gmail account.
4. Paste the app password into the password field and save with `Ctrl+S`.

TideMail stores the app password in the system keychain, or in the config file when `secret-tool` is unavailable.

Example account config:

```toml
theme = "catppuccin-mocha"

[display]
send_delay_seconds = 5

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
signature = "Alice\nSent with TideMail"
sync_minutes = 5  # auto-sync every 5 min (0 = off)
```

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `:` or `Ctrl+P` | Command palette. Also opens contextual commands in compose, AI summary, and save-attachments overlays. In a vim compose body, `:` opens the editor command line, so use `Ctrl+P` there. |
| `m` | Move selected messages to a folder or label |
| `M` | Account manager |
| `C` | Contacts manager |
| `c` in Contacts | Compose to selected contacts |
| `s` | Sync current mailbox. On Unified Inbox, sync all inboxes. |
| `F` | Sync all mailboxes |
| `c` | Compose and autosave to Drafts while typing |
| `Enter` in Drafts | Reopen selected draft in compose |
| `d` in Drafts | Delete selected draft |
| `r` | Toggle read/unread in the message list, or reply from content |
| `a` | Archive selected message |
| `d` | Delete selected message |
| `Space` | Multi-select messages. The cursor advances and stays visible. Use `d`, `a`, `m`, or `x` for bulk actions. |
| `A` | Select all messages in current view |
| `R` | Mark selected mailbox or account read |
| `/` | Search messages |
| `Shift+Left` / `Shift+Right` | Resize the accounts pane |
| `Shift+Up` / `Shift+Down` | Resize the messages/content split |
| `u` | Toggle unread-only view |
| `Ctrl+Z` | Cancel a queued send, or undo the latest pending delete, archive, or move |
| `o` | Open the link on the focus line, or the selected content link |
| `Ctrl+U` | Unsubscribe from the mailing list for the open message |
| `Ctrl+U` in compose | Cycle the sending account; focus From and press `Enter` to open the account picker |
| `Ctrl+N` / `Alt+N` | Next content link |
| `Alt+P` | Previous content link |
| `Ctrl+E` | Toggle email headers |
| `Ctrl+F` | Find in message |
| `v` / `V` | Select a line range or the full message |
| `` ` `` | AI summary |
| `S` | Settings |
| `T` | Theme picker |
| `Ctrl+D` | Save attachments to a folder |
| `Ctrl+G` | AI grammar and spell check in compose |
| Vim editing in compose | Enable in Settings -> Editor. `:w` and `:wq` send. `:q` or double `Esc` cancels. |
| `?` | Help |
| `q` | Quit |

## Settings

Press `S` to open Settings.

- Display: icons, date format, mark-read behavior, focus line, sender display, unread-first ordering, actionable links, reading width, browser command, density, email headers, desktop notifications, and quit confirmation
- Editor: vim keys and the delay that lets you cancel a send with `Ctrl+Z`
- Updates: check, install, restart, or copy a manual install command
- AI: OpenAI, Claude, Gemini, or Ollama summary settings
- Advanced: logs and feed max body size
- About: repository and issue links

Use the Account Manager for account details. Press `M` to open it.

## Related Projects

TideMail grew alongside two extracted libraries:

- [tideui](https://github.com/allisonhere/tideui): a themeable multi-pane terminal UI toolkit for Bubble Tea and Lipgloss. TideMail extracted it from this project. The multi-pane layout, theming, and styles continue in `internal/ui`.
- [ripple](https://github.com/allisonhere/ripple): the keyboard-first text editor behind the compose body. TideMail extracted it into a standalone library and depends on it as `github.com/allisonhere/ripple`.

## Development

Run the full suite:

```bash
go test ./...
```

The mail refactor removed RSS, GReader, and OPML behavior. RSS-era names that remain in internal style names are compatibility debt.
