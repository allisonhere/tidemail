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

The installer writes to `~/.local/bin` by default, so it should not ask for a
system password. If an older `tidemail` earlier on `PATH` would still run first,
the installer removes it when possible or prints the exact cleanup command. If
you prefer another writable directory, set `INSTALL_DIR` before running it:

```bash
curl -fsSL https://raw.githubusercontent.com/allisonhere/tidemail/main/install.sh | INSTALL_DIR="$HOME/bin" sh
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
`sync_minutes` controls how an account refreshes on its own:

| Value | Meaning |
| --- | --- |
| `0` | Push — IMAP IDLE delivers mail as it arrives, plus a 30-minute safety poll |
| `N` | Poll every N minutes, with IDLE accelerating it |
| `-1` | Manual only — no polling and no background connection |

Push (`0`) is the default and the best choice for servers that support IDLE,
including Gmail. The safety poll exists because IDLE goes silent on a connection
that wedges without dropping, so push alone can stall with nothing to show for
it. Use `-1` if you want an account to refresh only when you press `s`.

## Reading and organizing mail

Use the three panes to choose an account or folder, select a message, and read
its contents. Press `Tab` or `Shift+Tab` to move between panes.

In the message list, press `Space` to select several messages. You can then
archive them with `a`, delete them with `d`, move them with `m`, or mark them as
read. Press `A` to select every message in the current view.

Press `/` to search, `u` to show unread mail, and `t` to place starred messages
first. Search covers subject, sender, recipients, and message body across every
account and folder, and matches as you type. In the content pane, `Ctrl+E` shows
the full headers and authentication results. `Ctrl+U` opens the mailing list's
unsubscribe option when the message provides one.

Search only reaches mail TideMail has cached. The first sync of a mailbox
fetches the 100 most recent messages; moving down past the last message in the
list pulls the next 100 from the server, so you can page back as far as the
mailbox goes.

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

The standard editor is on by default. Use Shift with the arrow keys to select
text, `Ctrl+C`/`Ctrl+X`/`Ctrl+V` for the system clipboard, `Ctrl+Z`/`Ctrl+Y` for
undo and redo, and Ctrl with the arrow keys to move by word.

Enable Vim keys under Settings → Editor if you prefer modes, motions, counts,
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

TideMail stores IMAP and SMTP passwords, AI API keys, and OAuth refresh tokens
in the system keychain. It uses `secret-tool` on Linux and Keychain on macOS.
When TideMail starts, it loads any blank password, API key, or refresh token
from the keychain. Saving Settings moves loaded secrets into the keychain and
clears them from the config file.

If `secret-tool` is unavailable on Linux, TideMail writes these secrets to
`~/.config/tidemail/config.toml`. Keep that file private.

- Gmail: Google Account → Security → App passwords (requires 2-Step Verification)
- Yahoo: Account Security → Generate app password
- iCloud: Apple ID → Sign-In and Security → App-Specific Passwords

If you expose an app password, revoke it and create a new one.

Gmail requires either an app password or an OAuth sign-in. For a password, turn
on 2-Step Verification, generate one at
[myaccount.google.com/apppasswords](https://myaccount.google.com/apppasswords),
then paste it into the password field in the account manager (`M`) and save with
`Ctrl+S`.

### Gmail OAuth sign-in

TideMail ships no OAuth client — you supply your own, which keeps personal use
free and outside Google's verification requirements:

1. In the [Google Cloud Console](https://console.cloud.google.com/): create a
   project, enable the **Gmail API**, and configure the **OAuth consent screen**
   (External; add your address as a test user). Publish the app to Production so
   the refresh token does not expire weekly — a one-user project needs no
   security review; click past the "unverified app" notice.
2. Create an **OAuth client ID** of type *TVs and Limited Input devices* (device
   code) or *Desktop app* (paste-back).
3. Provide the credentials to TideMail, either as environment variables:

   ```sh
   export TIDEMAIL_GOOGLE_CLIENT_ID=xxxx.apps.googleusercontent.com
   export TIDEMAIL_GOOGLE_CLIENT_SECRET=xxxx
   ```

   or in `config.toml`:

   ```toml
   [oauth]
   google_client_id = "xxxx.apps.googleusercontent.com"
   google_client_secret = "xxxx"
   ```

4. In the account manager, add an account with provider **Gmail**. A new
   **Auth** row appears with a `‹ ›` selector — leave it on *OAuth* (or press
   `‹`/`›` to switch to *App password*). With OAuth selected, press `Ctrl+O`
   (or Enter on the sign-in row). TideMail shows a short code and a URL; open
   the URL on any device,
   sign in, and enter the code. When the device endpoint refuses the mail scope,
   TideMail falls back to a paste-back flow: it copies a sign-in URL to your
   clipboard; open it, approve, and paste the resulting `http://localhost/?code=…`
   URL (or the bare code) into the **Code** field.
5. Save with `Ctrl+S`. TideMail leaves the password field empty and authenticates
   IMAP and SMTP with XOAUTH2.

If the refresh token is revoked (or the consent screen was left in "Testing" and
expired it), the next sync shows "sign-in expired — press M to re-authenticate";
re-open the account and press `Ctrl+O` again.

### Outlook OAuth sign-in

Microsoft retired basic auth for Outlook.com, so an Outlook account signs in with
OAuth. Unlike Gmail this works out of the box — TideMail uses Mozilla
Thunderbird's shared public client ID, which is already consented for IMAP/SMTP
and needs no verification.

1. In the account manager, add an account with provider **Outlook**, leave the
   **Auth** row on *OAuth*, and press `Ctrl+O`.
2. Thunderbird's client can't use the device-code flow, so TideMail copies a
   sign-in URL to your clipboard. Open it, sign in, and you'll land on an
   unreachable `https://localhost` page. Paste that page's URL (or the `code=`
   value from it) into the **Code** field and press Enter.
3. Save with `Ctrl+S`. IMAP and SMTP authenticate with XOAUTH2.

To use your own Azure app registration instead (which enables the device-code
flow — a short code you approve at `microsoft.com/devicelogin`), register a
"Mobile and desktop" app with the `offline_access`,
`https://outlook.office.com/IMAP.AccessAsUser.All`, and
`https://outlook.office.com/SMTP.Send` delegated permissions, then set
`TIDEMAIL_MS_CLIENT_ID` (or `ms_client_id` under `[oauth]`).

Microsoft 365 work/school tenants often disable IMAP entirely; an admin must run
`Set-CASMailbox -ImapEnabled $true` for the mailbox.

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
sync_minutes = 0  # 0 = push (IDLE), N = poll every N min, -1 = manual only
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
| Standard compose editor | Selection, system clipboard, undo/redo, word movement, and Home/End |
| Vim keys in compose | Enable in Settings → Editor. `:w`/`:wq` send; `:q` or double `Esc` cancels |
| `?` | Help |
| `q` | Quit |

## Settings

Press `S` to open Settings.

- Display: icons, date format, mark-read behavior, focus line, show sender, unread-first ordering, actionable links, reading width, browser command, density, show email headers, desktop notifications, and quit confirmation
- Editor: standard or Vim compose keys and the delay that lets you cancel a send with `Ctrl+Z`
- Accounts: connection details, From address, signature, color, and sync interval
- Updates: check, install, restart, or copy a manual install command
- AI: OpenAI, Claude, Gemini, or Ollama summary settings
- Advanced: logs and feed max body size
- About: repository and issue links
