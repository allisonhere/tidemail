# TideMail Setup and Usage Guide

This guide covers installation, account setup, everyday mail tasks, settings,
and keyboard shortcuts. For an overview of TideMail, see the
[main README](../README.md).

## Installation

### Install a release

On Linux or macOS (amd64 or arm64), run:

```bash
curl -fsSL https://raw.githubusercontent.com/allisonhere/tidemail/main/install.sh | sh
```

You can also download a binary from the
[latest release](https://github.com/allisonhere/tidemail/releases/latest).

### Build from source

```bash
git clone https://github.com/allisonhere/tidemail
cd tidemail
go build -o tidemail .
./tidemail
```

## First run

Start TideMail with:

```bash
tidemail
```

Press `M` to open the account manager, then add your IMAP and SMTP details.
TideMail can discover common server settings from your email address. If your
provider requires an app password, create one before saving the account.

After you add an account, press `s` to sync the selected mailbox. Press `s` on
the Unified Inbox to sync every account's inbox, or `F` to sync all mailboxes.
Set `sync_minutes` on an account if you want background refresh. Accounts with
a sync interval also use IMAP IDLE when the server supports it.

## Reading and organizing mail

Use the three panes to choose an account or folder, select a message, and read
its contents. Press `Tab` or `Shift+Tab` to move between panes.

In the message list, press `Space` to select several messages. You can then
archive them with `a`, delete them with `d`, move them with `m`, or mark them as
read. Press `A` to select every message in the current view.

Press `/` to search, `u` to show unread mail, and `t` to place starred messages
first. In the content pane, `Ctrl+E` shows the full headers and authentication
results. `Ctrl+U` opens the mailing list's unsubscribe option when the message
provides one.

## Writing mail

Press `c` to compose a message or `r` from the content pane to reply. TideMail
saves your work to the sending account's Drafts folder as you type. Open Drafts
and press `Enter` to continue writing, or `d` to delete a draft.

If you have more than one account, the From row works as an account picker.
Focus it with `Shift+Tab` from the To row, then press `Enter` or `Space` and
choose the account. `Ctrl+U` cycles through the same list. TideMail uses that
account's SMTP settings, From address, and signature.

TideMail waits five seconds before sending by default. Press `Ctrl+Z` during
that window to cancel delivery and reopen the draft. Change the delay under
Settings → Editor, or set it to `0` to send at once.

Enable Vim keys under Settings → Editor if you want modes, motions, counts,
operators such as `dw`, `cw`, and `dd`, and a command line. `:w` sends the
message, while `:q` cancels it.

## Contacts

Press `C` to open Contacts. Press `n` to add someone, `f` to browse addresses
found in your mail, `i` to import a vCard, or `x` to export `contacts.vcf`.
Select contacts with `Space`, then press `c` to write to them.

Compose suggestions show saved contacts before addresses gathered from your
mail. vCard imports and exports include names, email addresses, phone numbers,
organizations, titles, and notes.

## Passwords and API keys

TideMail stores IMAP and SMTP passwords and AI API keys in the system keychain.
It uses `secret-tool` on Linux and Keychain on macOS. When TideMail starts, it
loads any blank password or API key from the keychain. Saving Settings moves
loaded secrets into the keychain and clears them from the config file.

If `secret-tool` is unavailable on Linux, TideMail writes passwords and API
keys to `~/.config/tidemail/config.toml`. Keep that file private.

- Gmail: Google Account → Security → App passwords (requires 2-Step Verification)
- Yahoo: Account Security → Generate app password
- iCloud: Apple ID → Sign-In and Security → App-Specific Passwords

If you expose an app password, revoke it and create a new one.

Gmail requires an app password. Turn on 2-Step Verification, generate a password
at [myaccount.google.com/apppasswords](https://myaccount.google.com/apppasswords),
then paste it into the password field in the account manager (`M`) and save with
`Ctrl+S`.

## Configuration files

TideMail stores its config in `~/.config/tidemail/config.toml` and its SQLite
cache in `~/.local/share/tidemail/mail.db`. Set `XDG_DATA_HOME` to use a
different cache location.

Example configuration:

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

## Keyboard shortcuts

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
