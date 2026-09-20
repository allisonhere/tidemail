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

`Ctrl+S` and `Ctrl+T` check the form before anything connects, so a typo comes
back as a labeled field rather than a login failure. Both hosts are required
and are hostnames, not URLs, paths, or `host:port` pairs — the port belongs in
its own field, where it must be a number between 1 and 65535. Leave a port
blank to take the standard one. On the Gmail, Outlook, Yahoo, and iCloud
providers the **Email** row must be a full address; a **Custom** account's
**Username** is whatever your host issues, including logins like
`alice#example.com` that are not addresses at all.

After you add an account, press `s` to sync the selected mailbox. Press `s` on
the Unified Inbox to sync every account's inbox, or `F` to sync all mailboxes.
When you stop on a non-inbox folder, TideMail silently refreshes it if its cache
is more than 15 minutes old. Scrolling past folders does not queue refreshes;
press `Enter` on a folder row when you want to fetch it immediately. Manual-only
accounts refresh only when you explicitly use `Enter`, `s`, or `F`.
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

Search only reaches mail TideMail has cached. The first sync fetches the 100
most recent inbox messages and 25 messages from other folders, whose attachments
can make each batch much larger. Moving down past the last message in a folder
pulls the next 100 from the server, so you can page back as far as it goes.

## Writing mail

Press `c` to compose a message or `r` from the content pane to reply. TideMail
saves your work to the sending account's Drafts folder as you type. Open Drafts
and press `Enter` to continue writing, or `d` to delete a draft.

If you have more than one account, the From row works as an account picker.
Focus it with `Shift+Tab` from the To row, then press `Enter` or `Space` and
choose the account. `Ctrl+U` cycles through the same list. TideMail uses that
account's SMTP settings, From address, and signature.

A new message starts from your default account — the one marked `★ [default]`
in **Accounts**. Set it by highlighting an account there and pressing `Space`.
Replies and forwards ignore the default and use the account the message
arrived on. Without a default, a new message starts from the first account in
your config.

After delivery, TideMail saves a copy to the sending account's server-side Sent
folder. Gmail already files SMTP submissions itself, so TideMail does not append
a duplicate there. Other providers use the server's `\Sent` folder flag, with
common names such as `Sent`, `Sent Items`, and `INBOX.Sent` as fallbacks.

TideMail waits five seconds before sending by default. Press `Ctrl+Z` during
that window to cancel delivery and reopen the draft. Change the delay under
Settings → Editor, or set it to `0` to send at once. Press `O` to open the
Outbox. Use `r` to retry a failed message or `e` to move an unsent message back
into compose.

Failed deliveries retry after one minute. The maximum attempt setting includes
the first try; it defaults to `3`, and `1` disables automatic retries. An
interrupted delivery is marked uncertain because the server may have accepted
it. Check Sent mail before manually retrying an uncertain message.

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

- Gmail: Google Account → Security → App passwords (requires 2-Step Verification),
  or sign in with OAuth (below)
- Outlook.com / Microsoft 365 / Exchange Online: OAuth only — pick the **Outlook**
  provider and sign in (below); Microsoft no longer accepts passwords here
- Yahoo: Account Security → Generate app password
- iCloud: Apple ID → Sign-In and Security → App-Specific Passwords
- On-premises Exchange or any other server: **Custom** provider with your normal
  username and password (basic auth over TLS)

If you expose an app password, revoke it and create a new one.

Gmail requires either an app password or an OAuth sign-in. For a password, turn
on 2-Step Verification, generate one at
[myaccount.google.com/apppasswords](https://myaccount.google.com/apppasswords),
then paste it into the password field in the account manager (`M`) and save with
`Ctrl+S`.

### Gmail OAuth sign-in

Official builds supply TideMail's Google app credentials; there is no user
configuration step.

1. Press `M`, add an account, choose **Gmail**, and enter the account details.
2. Leave **Auth** on **OAuth** and press `Ctrl+O` or Enter on **Sign in with Google**.
3. Approve Google access in the browser. The browser shows a confirmation and
   TideMail finishes signing in automatically.
4. Return to TideMail and save with `Ctrl+S`.

For SSH or manual sign-in, press `Ctrl+P` while waiting for browser approval.
TideMail displays the URL and a **Code** field. Open the URL, approve access, and
paste the full redirect URL back into TideMail, then press Enter. If the browser
is on another computer, its redirect may show a connection error; copy that URL
anyway. This fallback also appears if TideMail cannot launch the browser. With a
working local callback, approval can still finish automatically during manual entry.

`Esc` cancels sign-in, and attempts expire after five minutes. Denied access or
an expired attempt can be retried with `Ctrl+O`. If a refresh token expires or is
revoked, open the account and sign in again.

Existing app-password accounts keep working; use the **Auth** selector to switch
methods deliberately. For maintainer registration, developer overrides, and
Google verification requirements, see [Google OAuth setup](google-oauth.md).

Developers can preview the Gmail App Password-only account form without changing
saved credentials:

```sh
go run . --disable-google-oauth
```

See [Development flags](flags.md) for the behavior and other UI preview options.

### Outlook OAuth sign-in

The **Outlook** provider covers Outlook.com, Hotmail/Live, and Microsoft 365 /
Exchange Online mailboxes. Microsoft retired basic auth for all of these, so the
account signs in with OAuth. TideMail
uses Mozilla Thunderbird's shared public client ID, which is already consented
for IMAP/SMTP and needs no verification.

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

### On-premises Exchange Server

A self-hosted Exchange server (2016/2019/Subscription Edition) is **not** the
Outlook provider — it does not authenticate against Microsoft's cloud. Use the
**Custom** provider:

- IMAP host: your Exchange server's hostname, port 993, TLS
- SMTP host: the same server, port 587 (STARTTLS) or 465
- Username: your login (often `DOMAIN\user` or your UPN)
- Password: your normal mailbox password

On-premises Exchange still accepts basic auth over TLS — Microsoft only removed it
from Exchange Online. TideMail speaks XOAUTH2 and PLAIN only, so it cannot connect
to an on-prem server whose admin has disabled basic auth and requires NTLM or
Kerberos (GSSAPI).

## Configuration files

TideMail stores its config in `~/.config/tidemail/config.toml` and its SQLite
cache in `~/.local/share/tidemail/mail.db`. Set `XDG_DATA_HOME` to use a
different cache location.

Example configuration:

```toml
theme = "catppuccin-mocha"
default_account = "8f43c9e644384bd5a25a27ff0d9c2701"

[display]
send_delay_seconds = 5
send_max_attempts = 3

[[account]]
id = "8f43c9e644384bd5a25a27ff0d9c2701" # generated by TideMail; do not copy or edit
name = "Personal"
imap_host = "imap.example.com"
imap_port = 993
imap_tls = true
smtp_host = "smtp.example.com"
smtp_port = 587
smtp_tls = true
user = "alice@example.com"
auth_method = "password"  # "password" (default) or "oauth2"
password = "app-password"
from = "Alice <alice@example.com>"
signature = "Alice\nSent with TideMail"
sync_minutes = 0  # 0 = push (IDLE), N = poll every N min, -1 = manual only
```

`from` is the **From address** row in the account form. Leave it blank to send
as `user`, or give it an address — `alice@example.com` or
`Alice <alice@example.com>`. A display name on its own is not enough: the
account form refuses to save one, because SMTP has no address to send from and
would only fail once the message was already queued.

The signature is a multi-line box in the account form — press `Enter` for a new
line, `Tab` to move on, and `Ctrl+S` to save without leaving it. Arrow keys walk
its lines and step to the next field at the top and bottom. Editing the file by
hand works too; a TOML multi-line string is easier to read than escapes:

```toml
signature = """
Alice
Sent with TideMail"""
```

`auth_method` is set for you: it becomes `"oauth2"` once you complete a `Ctrl+O`
sign-in for a Gmail/Outlook account, otherwise `"password"`. The refresh token
and password live in the system keychain, not this file. An account never
switches to OAuth on its own — a leftover keychain token cannot promote it.

`default_account` names the account a new message is sent from, and the account
TideMail focuses at startup. It holds an account `id`, not a position, so
renaming or reordering accounts never moves it. Delete it, or the account it
names, and TideMail falls back to the first account and opens on the Unified
Inbox. `Space` in **Accounts** writes this line for you.

The order of the `[[account]]` blocks is the order accounts appear in: the
sidebar, the account list, and the `Ctrl+U` sender picker in compose. Reorder
them here, or with `Shift+J` and `Shift+K` in **Accounts**.

The generated account `id` links this block to cached mail, drafts, queued mail,
and keychain credentials. It stays fixed when the display name changes. TideMail
adds missing IDs on upgrade and first saves the original file as
`config.toml.pre-account-ids.bak`.

If `config.toml` is malformed or an automatic migration cannot be saved,
TideMail stops instead of starting with defaults that could overwrite the real
settings. The startup error names the exact config path and the parse or
permission problem to fix. If an account's cached folders no longer have a
matching config block, affected actions stop with a status-line explanation;
open **Accounts** and re-enter that account's server details.

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `:` or `Ctrl+P` | Open the command palette. In a Vim compose body, `:` belongs to the editor, so use `Ctrl+P` for the palette. |
| `m` | Move selected message(s) to folder/label |
| `c` | Compose (autosaves to Drafts as you type) |
| `C` | Contacts manager |
| `c` in Contacts | Compose to selected contact(s) |
| `M` | Account manager |
| `Space` in Accounts | Mark the highlighted account `★ [default]` — the sender for new messages |
| `Shift+J` / `Shift+K` in Accounts | Move the highlighted account down / up the list |
| `Ctrl+T` in the account form | Test the connection without saving |
| `s` | Sync current mailbox (Unified Inbox: syncs all inboxes) |
| `Enter` on a folder | Fetch that folder now |
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
| `O` | Open the Outbox |
| `o` or `Enter` | Open link on the focus line (falls back to the selected content link) |
| `Ctrl+U` | Unsubscribe from the mailing list for the open message |
| `Ctrl+U` in compose | Cycle the sending account; focus From and press `Enter` to open the account picker |
| `Ctrl+N` / `Alt+N` | Next content link |
| `Alt+P` | Previous content link |
| `Ctrl+E` | Toggle email headers on/off |
| `Ctrl+F` | Find in message |
| `v` / `V` | Visual select line range / whole message |
| `` ` `` | AI summary |
| In the summary overlay | `C` copies, `M` saves a `.md` file to the AI save path, `z` toggles quoted text, `Esc` closes |
| `S` | Settings |
| `T` | Theme picker |
| `Alt+F` in compose | Attach a file (opens the file picker) |
| `Ctrl+R` in compose | Remove the last attachment |
| `Ctrl+D` | Save attachments to folder |
| `Ctrl+G` | AI grammar & spell check (compose) |
| Standard compose editor | Selection, system clipboard, undo/redo, word movement, and Home/End |
| Vim keys in compose | Enable in Settings → Editor. `:w`/`:wq` send; `:q` or double `Esc` cancels |
| `?` | Help |
| `q` | Quit |

## Themes

Cycle the theme with `T`, or pick one under Settings → Display. Every built-in
theme is checked against WCAG-style contrast minimums by automated tests.

`match-omarchy` is a special entry for [Omarchy](https://omarchy.org) users: it
follows your current Omarchy desktop theme, remapped and contrast-corrected so
TideMail stays readable whatever palette Omarchy is on. It reads the palette
from `omarchy-theme-color` (falling back to
`~/.local/state/omarchy/current/theme/colors.toml`), works for both light and
dark Omarchy themes, applies the palette to both styled UI and terminal-default
text at startup, and repaints within a couple of seconds when you switch your
desktop theme — no restart. If Omarchy isn't installed it falls back to
`catppuccin-mocha`.

## Settings

Press `S` to open Settings.

Under **Display**, **Pane header bars** controls the title-and-shortcut row above
each pane. When disabled, those rows disappear to make more room for content;
the focused pane's title and shortcuts appear in the bottom status line instead.
Global message search uses a dedicated row above message subjects while active.

- Display: theme, icons, date format, mark-read behavior, focus line, show sender, unread-first ordering, actionable links, reading width, browser command, density, show email headers, desktop notifications, and quit confirmation
- Editor: compose keys, send delay, and maximum delivery attempts
- Accounts: connection details, From address, signature, color, and sync interval
- Updates: check, install, restart, or copy a manual install command
- AI: OpenAI, Claude, Gemini, or Ollama summary settings
- Advanced: logs and feed max body size
- About: repository and issue links
