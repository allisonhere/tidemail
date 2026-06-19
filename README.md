# TideMail

![TideMail screenshot](screen.png)

A keyboard-first terminal mail client built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss).
The compose message body is powered by [ripple](https://github.com/allisonhere/ripple) — an owned keyboard-first text editor (selection, undo/redo, system-clipboard copy/cut/paste, word movement, soft-wrap) extracted from TideMail into a standalone Go TUI editor library.

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
- Multi-select messages with space bar — auto-advances for bulk delete, archive, move, mark read
- Select all with `A` — selects every message in current view
- Full email headers display — toggle with `ctrl+e`, configurable default in Settings
- Spam/auth headers — SPF, DKIM, DMARC results color-coded in header view
- Message text can be copied from the content pane with Vim-style `v`/`V` selection, then `y` or `ctrl+c`
- **Owned compose editor** for the message body — text selection (`shift`/`ctrl+shift`+arrows), select-all (`ctrl+a`), undo/redo (`ctrl+z`/`ctrl+y`), system-clipboard copy/cut/paste (`ctrl+c`/`ctrl+x`/`ctrl+v`), word movement, and soft-wrap
- **Optional vim editing in compose** — enable it in Settings → Editor (or `[display] compose_vim`); the body gets Normal/Insert/Visual modes, motions (`hjkl`, `w/b/e`, `0/^/$`, `gg/G`, counts), edits (`x`, `dd`, `yy`, `p`, `dw`, `cw`), visual `v`/`V`, `u`/`ctrl+r`, and a `:` command line where `:w`/`:wq` send and `:q` cancels. Off by default
- IMAP/SMTP accounts using passwords or app passwords (stored in system keychain)
- Account manager for adding, editing, deleting, and discovering mailboxes
- Contacts manager for curated autocomplete, manual entries, adding seen senders, composing to selected contacts, and vCard import/export
- vCard import/export preserves email, display name, phone, organization, title, and notes
- Server-backed sync, read/unread, archive, move, delete, compose, and reply
- **Local drafts** — composes autosave to a per-account Drafts mailbox as you type; reopen one with `Enter`, delete with `d`, and switching the sender account moves the draft with it
- Local-first delete hides messages immediately, moves remote mail to Trash when available, and prevents deleted mail from reappearing on later syncs
- Archive auto-detection via `\Archive`, `Archive`, `Archives`, or `All Mail`
- Trash auto-detection via `\Trash`, `Trash`, `Deleted Items`, `Deleted Messages`, or Gmail's Trash label
- Command palette for main mail actions, plus contextual commands in compose, AI summary, and save-attachments overlays
- Global message search (`/` enters persistent search mode; type to filter, esc to exit) and unread-only filtering
- Optional actionable links in the message content pane
- File browsers for attaching and saving attachments hide dotfiles by default; press `.` to toggle hidden files and folders
- AI summaries with copy and save-to-Markdown actions
- AI grammar & spell check in compose with preview overlay
- **AI mail filters** — describe a rule in plain English ("move newsletters from substack to Reading") and AI turns it into a real, deterministic filter that runs locally with no per-message AI cost. Choose which account it applies to (or **All accounts**), review the generated rule, then save (auto-applied to new mail on sync), run once on a mailbox, or run on all existing mail. Actions: move, mark read, archive, delete, spam. Open from the command palette (`p` → "filters").
- Theme-aware dialogs, overlays, and terminal background sync
- Collapsible account folders (System, Labels) in sidebar
- **Desktop notifications** on genuinely new unread mail via auto-sync (notify-send), with sender and subject details

## Usage

```bash
go build -o tidemail .
./tidemail
```

Config is stored in `~/.config/tidemail/config.toml`. The local SQLite cache is stored in `~/.local/share/tidemail/mail.db` unless `XDG_DATA_HOME` changes that path.

Open account management with `M`, add an IMAP/SMTP account, then sync the selected mailbox with `s`. Press `s` on the Unified Inbox to sync every account's inbox at once. Use `F` to sync all mailboxes. Configure a per-account `sync_minutes` interval for automatic background refresh.

Composing autosaves your message to the sending account's Drafts mailbox as you type, so closing the compose modal never loses work — closing with content prompts to save or discard. Open the Drafts mailbox to see saved drafts, press `Enter` to reopen one in compose, and `d` to delete it. Sending a draft removes it from Drafts automatically.

Open Contacts with `C`. Contacts are the curated address book used for compose autocomplete. Add contacts manually with `n`, add addresses already seen in mail with `f`, import a vCard file with `i`, export to `contacts.vcf` with `x`, select multiple contacts with `Space`, press `c` to compose to the selected contact(s), and delete selected contacts with `d`. vCard import/export keeps email, display name, phone, organization, title, and note fields.

## Credential safety

TideMail stores IMAP/SMTP passwords and AI API keys in the system keychain via `secret-tool` (libsecret on Linux, Keychain on macOS). Empty `password` and `openai_key`/`claude_key`/`gemini_key` fields in the config file are looked up from the keychain at startup. When you save settings, any in-memory secrets are moved to the keychain and removed from the config file automatically.

If `secret-tool` is not installed, passwords and API keys fall back to being stored directly in `~/.config/tidemail/config.toml`. Treat the config file like a secret in that case.

- Gmail: enable 2-Step Verification, then Google Account → Security → App passwords
- Yahoo: Account Security → Generate app password
- iCloud: Apple ID → Sign-In and Security → App-Specific Passwords

If you paste or accidentally expose an app password, revoke it and create a new one.

## Gmail

Gmail requires a Google **App Password** (OAuth "Sign in with Google" was removed in v0.5.0):
1. Enable 2-Step Verification on your Google account
2. Create an app password at myaccount.google.com/apppasswords
3. Press `M` to open the account manager, add or edit your Gmail account
4. Paste the app password into the password field and save (`Ctrl+S`)

The app password is stored in the system keychain (or the config file if `secret-tool` is unavailable).

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
| `:` or `Ctrl+P` | Command palette (also contextual commands in compose, AI summary, and save-attachments overlays). In a vim compose body `:` is the editor's command line — use `Ctrl+P` there |
| `m` | Move selected message(s) to folder/label |
| `M` | Account manager |
| `C` | Contacts manager |
| `c` in Contacts | Compose to selected contact(s) |
| `s` | Sync current mailbox (Unified Inbox: syncs all inboxes) |
| `F` | Sync all mailboxes |
| `c` | Compose (autosaves to Drafts as you type) |
| `Enter` in Drafts | Reopen selected draft in compose |
| `d` in Drafts | Delete selected draft |
| `r` | Toggle read/unread in message list, reply from content |
| `a` | Archive selected message |
| `d` | Delete selected message |
| `Space` | Multi-select messages; auto-advances and keeps the cursor visible (then `d`/`a`/`m`/`x` for bulk actions) |
| `A` | Select all messages in current view |
| `R` | Mark selected mailbox/account read |
| `/` | Search messages |
| `Shift+Left` / `Shift+Right` | Resize the accounts pane |
| `Shift+Up` / `Shift+Down` | Resize the messages/content split |
| `u` | Toggle unread-only view |
| `o` | Open link on the focus line (falls back to the selected content link) |
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
| Vim editing (compose) | Optional — enable in Settings → Editor; `:w`/`:wq` send, `:q` or double-`Esc` cancels |
| `?` | Help |
| `q` | Quit |

## Settings

Settings are opened with `S`.

- Display: icons, date format, mark-read behavior, focus line, show sender, unread-first ordering, actionable links, reading width, browser command, density, show email headers, desktop notifications, and quit confirmation
- Editor: vim keys in the compose body (off by default)
- Updates: check, install, restart, or copy a manual install command
- AI: OpenAI, Claude, Gemini, or Ollama summary settings
- Advanced: view logs and feed max body size
- About: repository and issue links

(Account details are managed in the Account Manager — press `M` — not in Settings.)

## Related projects

TideMail grew alongside two of its own libraries:

- **[tideui](https://github.com/allisonhere/tideui)** — a themeable multi-pane terminal UI toolkit for Bubble Tea and Lipgloss. It was extracted from this project and used to build the base of TideMail's interface (the multi-pane layout, theming, and styles live on in `internal/ui`).
- **[ripple](https://github.com/allisonhere/ripple)** — the keyboard-first text editor behind the compose message body, later extracted into a standalone library. TideMail depends on it directly (`github.com/allisonhere/ripple`).

## Development

Run the full suite:

```bash
go test ./...
```

The mail refactor intentionally removes RSS/GReader/OPML behavior. Any remaining RSS-era naming in internal style names is compatibility debt, not product direction.
