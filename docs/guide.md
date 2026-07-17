# TideMail

![TideMail screenshot](../screen.png)

A keyboard-first terminal mail client built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss).
[ripple](https://github.com/allisonhere/ripple), TideMail's standalone Go editor library, powers compose editing.

## Install

**Quick install** (Linux/macOS, amd64/arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/allisonhere/tidemail/main/install.sh | sh
```

Or download a binary from the [latest release](https://github.com/allisonhere/tidemail/releases/latest).

**From source:**

```bash
git clone https://github.com/allisonhere/tidemail
cd tidemail
go build -o tidemail .
./tidemail
```

## Features

- Three-pane mail layout: accounts, messages, content
- Unified Inbox across all configured accounts
- Select messages with `Space`, then delete, archive, move, or mark the group as read
- Toggle full email headers with `Ctrl+E`, or keep them open from Settings
- See color-coded SPF, DKIM, and DMARC results in the header view
- Copy message text from the content pane with Vim-style `v`/`V` selection, then `y` or `Ctrl+C`
- Select all messages in the current view with `A`
- Turn on Vim keys in Settings → Editor for modes, motions, counts, operators such as `dw`, `cw`, and `dd`, and a `:` command line. `:w` sends and `:q` cancels.
- IMAP/SMTP accounts using passwords or app passwords (stored in system keychain)
- Account manager for adding, editing, deleting, and discovering mailboxes
- Compose suggests saved contacts first, followed by addresses from mail you have received
- Manage contacts by hand, import or export vCards, and start a message from selected contacts
- vCard import/export preserves email, display name, phone, organization, title, and notes
- Server-backed sync, read/unread, archive, move, delete, compose, and reply
- TideMail saves drafts to the sending account while you type. Reopen one with `Enter` or delete it with `d`.
- Choose the sending account from the From row. Each account can have its own From address and signature.
- Sent mail waits five seconds before delivery by default. Press `Ctrl+Z` during that window to reopen the message.
- Delete hides a message at once, moves server mail to Trash when available, and keeps it from returning on the next sync
- Archive auto-detection via `\Archive`, `Archive`, `Archives`, or `All Mail`
- Trash auto-detection via `\Trash`, `Trash`, `Deleted Items`, `Deleted Messages`, or Gmail's Trash label
- Command palette for main mail actions, plus contextual commands in compose, AI summary, and save-attachments overlays
- Global message search (`/` enters persistent search mode; type to filter, esc to exit) and unread-only filtering
- Optional actionable links in the message content pane
- File browsers for attaching and saving attachments hide dotfiles by default; press `.` to toggle hidden files and folders
- AI summaries with copy and save-to-Markdown actions
- AI grammar & spell check in compose with preview overlay
- Describe a mail rule in plain English, such as "move newsletters from substack to Reading." TideMail turns it into a local filter that can move, archive, delete, mark read, or mark spam. Open filters from the command palette.
- Theme-aware dialogs, overlays, and terminal background sync
- Collapsible account folders (System, Labels) in sidebar
- Desktop notifications show the sender and subject for new unread mail
- Accounts with `sync_minutes > 0` use IMAP IDLE for push delivery and keep interval polling as a fallback
- Press `Ctrl+U` in the content pane to use a message's unsubscribe header or labeled unsubscribe link

## Usage

```bash
go build -o tidemail .
./tidemail
```

TideMail keeps config in `~/.config/tidemail/config.toml` and its SQLite cache in `~/.local/share/tidemail/mail.db`. `XDG_DATA_HOME` can change the cache location.

Open account management with `M`, add an IMAP/SMTP account, then sync the selected mailbox with `s`. Press `s` on the Unified Inbox to sync every account's inbox at once. Use `F` to sync all mailboxes. Configure a per-account `sync_minutes` interval for automatic background refresh.

TideMail saves a compose draft to the sending account as you type. Closing a message with content asks whether to save or discard it. Open Drafts and press `Enter` to keep writing, or `d` to delete the draft. TideMail removes the draft after a successful send.

The From row becomes a picker when you have more than one account. Focus it with `Shift+Tab` from To, then press `Enter` or `Space` to choose an account. `Ctrl+U` still cycles accounts. TideMail uses the chosen account's SMTP settings, From address, and signature.

By default, TideMail holds sent mail for five seconds. Press `Ctrl+Z` before the timer ends to cancel the send and reopen your draft. Change the delay in Settings → Editor, or set it to `0` for immediate sending.

Open Contacts with `C`. Compose lists saved contacts before addresses found in your mail. Press `n` to add a contact, `f` to browse seen addresses, `i` to import a vCard, or `x` to export `contacts.vcf`. Select contacts with `Space` and press `c` to write to them. TideMail keeps names, email addresses, phone numbers, organizations, titles, and notes in vCard files.

## Credential safety

TideMail stores IMAP/SMTP passwords and AI API keys in the system keychain through `secret-tool` on Linux and Keychain on macOS. At startup, TideMail reads empty password and API-key fields from the keychain. Saving Settings moves loaded secrets into the keychain and clears them from the config file.

Without `secret-tool`, TideMail writes passwords and API keys to `~/.config/tidemail/config.toml`. Treat that file as a secret.

- Gmail: Google Account → Security → App passwords (requires 2-Step Verification)
- Yahoo: Account Security → Generate app password
- iCloud: Apple ID → Sign-In and Security → App-Specific Passwords

If you expose an app password, revoke it and create a new one.

Gmail needs an app password. TideMail removed "Sign in with Google" in
v0.5.0. Turn on 2-Step Verification, generate a password at
[myaccount.google.com/apppasswords](https://myaccount.google.com/apppasswords), then paste
it into the password field in the account manager (`M`) and save with `Ctrl+S`.

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
| `:` or `Ctrl+P` | Open the command palette. In a Vim compose body, `:` belongs to the editor, so use `Ctrl+P` for the palette. |
| `m` | Move selected message(s) to folder/label |
| `c` | Compose (autosaves to Drafts as you type) |
| `C` | Contacts manager |
| `c` in Contacts | Compose to selected contact(s) |
| `M` | Account manager |
| `s` | Sync current mailbox (Unified Inbox: syncs all inboxes) |
| `F` | Sync all mailboxes |
| `Enter` in Drafts | Reopen selected draft in compose |
| `d` in Drafts | Delete selected draft |
| `r` | Toggle read/unread in message list, reply from content |
| `*` | Toggle star (IMAP `\Flagged`) on selected message(s); syncs to the server |
| `a` | Archive selected message |
| `d` | Delete selected message |
| `Space` | Multi-select messages; auto-advances and keeps the cursor visible (then `d`/`a`/`m`/`x` for bulk actions) |
| `A` | Select all messages in current view |
| `R` | Mark selected mailbox/account read |
| `/` | Search messages |
| `Shift+Left` / `Shift+Right` | Resize the accounts pane |
| `Shift+Up` / `Shift+Down` | Resize the messages/content split |
| `u` | Toggle unread-only view |
| `t` | Toggle starred-first sort (starred messages float to the top) |
| `Ctrl+Z` | Cancel a queued send, or undo the latest pending delete, archive, or move |
| `o` | Open link on the focus line (falls back to the selected content link) |
| `Ctrl+U` | Unsubscribe from the mailing list for the open message |
| `Ctrl+U` in compose | Cycle the sending account; focus From and press `Enter` to open the account picker |
| `Ctrl+N` / `Alt+N` | Next content link |
| `Alt+P` | Previous content link |
| `Ctrl+E` | Toggle email headers on/off |
| `Ctrl+F` | Find in message |
| `v` / `V` | Visual select line range / whole message |
| `` ` `` | AI summary |
| `S` | Settings |
| `T` | Theme picker |
| `Ctrl+D` | Save attachments to folder |
| `Ctrl+G` | AI grammar & spell check (compose) |
| Vim keys in compose | Enable in Settings → Editor. `:w`/`:wq` send; `:q` or double `Esc` cancels |
| `?` | Help |
| `q` | Quit |

## Settings

Press `S` to open Settings.

- Display: icons, date format, mark-read behavior, focus line, show sender, unread-first ordering, actionable links, reading width, browser command, density, show email headers, desktop notifications, and quit confirmation
- Editor: vim keys and the delay that lets you cancel a send with `Ctrl+Z`
- Accounts: connection details, From address, signature, color, and sync interval
- Updates: check, install, restart, or copy a manual install command
- AI: OpenAI, Claude, Gemini, or Ollama summary settings
- Advanced: logs and feed max body size
- About: repository and issue links

## Related projects

Two libraries came out of this one:

- [ripple](https://github.com/allisonhere/ripple) powers the compose editor as a standalone dependency.
- [tideui](https://github.com/allisonhere/tideui) is a themeable multi-pane toolkit for Bubble Tea. Its layout and theming still live in `internal/ui`.

## Development

Run the full suite:

```bash
go test ./...
```

The mail refactor removed RSS, GReader, and OPML behavior. A few internal style names still use the old terminology.
