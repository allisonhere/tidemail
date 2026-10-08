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

### App launcher entry (optional)

The Arch Linux package (`tidemail-bin`) installs a desktop entry and the
TideMail icon for you. With the install script or a source build, you can add
them yourself from a checkout of the repository:

```bash
install -Dm644 images/tidemail-icon.png ~/.local/share/icons/tidemail.png
install -Dm644 packaging/linux/tidemail.desktop ~/.local/share/applications/tidemail.desktop
```

The entry runs `tidemail` in your default terminal, so `tidemail` must be on
your `PATH`.

If your launcher or installer asks for an icon URL instead (Omarchy's TUI
installer does), use the 1024×1024 PNG with a transparent background:
`https://raw.githubusercontent.com/allisonhere/tidemail/main/images/tidemail-icon.png`

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
back as a labeled field rather than a login failure. A rejected row turns red
and takes the cursor, with the reason on the status line; editing it clears the
mark. Both hosts are required and are hostnames, not URLs, paths, or
`host:port` pairs — the port belongs in its own field, where it must be a
number between 1 and 65535. Leave a port blank to take the standard one. On
the Gmail, Outlook, Yahoo, and iCloud
providers the **Email** row must be a full address; a **Custom** account's
**Username** is whatever your host issues, which need not be an address at all
— cPanel's `alice+example.com`, an Exchange `DOMAIN\alice`, a bare ISP login.
When it is not an address, the **From address** row becomes required: there is
no address to send as otherwise, and the form says so rather than leaving the
first send to be refused by the server.

Any field holding an address rejects `#` typed where `@` belongs —
`info#example.com`. On a Nordic layout `@` is AltGr+2 and `#` is Shift+3, and
nothing downstream can recover from the slip: `#` is legal inside an address,
so the value stays a perfectly good string that no server can route. An address
that really contains one, like `info#sales@example.com`, is untouched.

After you add an account, press `s` to sync the selected mailbox. Press `s` on
the Unified Inbox to sync every account's inbox, or `F` to sync all mailboxes.
With more than 10 folders, `F` first asks for confirmation, since syncing them
all can take a while.
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
account and folder, and matches as you type.

Type `#` and a tag name to search by the tags shown on message rows:
`#github`, `#billing`, or any other category, and `#reply`, `#urgent`, or
`#important`. Tags match as you type (`#git` already finds `#github`), several
tags must all match, and they combine with words: `invoice #billing` finds
billing mail mentioning an invoice. Your corrections count, so a message you
recategorized is found under its new tag, not the plugin's. In the content pane, `Ctrl+E` shows
the full headers and authentication results. `Ctrl+U` opens the mailing list's
unsubscribe option when the message provides one.

### Managing folders

Folders are your mail server's real folders, shown as a tree: a folder such as
`Work/Projects` sits under `Work`, and a folder with subfolders has a `▾`/`▸`
marker. In the left pane:

- `Enter` or `Space` on a folder with subfolders collapses or expands it (the
  state is remembered). `←` collapses an open folder, or moves to its parent
  folder; `→` expands a collapsed one. A collapsed folder's unread count includes
  everything inside it. To fetch a folder that has subfolders, use `s`.
- `n` creates a folder: on a folder it makes a subfolder, on an account or
  section header a top-level folder. Type the name in the prompt row and press
  `Enter` (or `Esc` to cancel).
- `r` renames the selected folder; its subfolders come along.
- `d` deletes the selected folder from the server after asking. The prompt says
  how many subfolders go with it and that every message in them is deleted, and
  `y` or `Enter` confirms. The Inbox and system folders (Sent, Drafts, Trash,
  Junk, Archive, and Gmail's `[Gmail]` folders) can't be renamed or deleted.
- `H` hides the selected folder (and its subfolders) from TideMail's sidebar
  only; nothing changes on the server. "Show hidden folders" in the command
  palette lists them again, marked `(hidden)`, and `H` on one unhides it.
- `Shift+K` / `Shift+J` walk the selected folder up or down the tree one row at a
  time, and it is shown indented while it is inside a parent. Passing a sibling
  just reorders it (the order is local to TideMail). Moving onto an open folder
  that has subfolders puts it inside, as the first child going down or the last
  going up; moving past a parent's last or first subfolder takes it out, to just
  after or before that parent. Collapsed folders are stepped over, never entered.
  Moving in or out is a real move on the server, subfolders included, and is
  refused if the destination already has a folder of the same name. The Inbox
  and system folders can be reordered but never moved in or out.
- `>` nests the selected folder under the folder just above it, as its last
  child, even if that folder has no subfolders yet (Shift+J/K can only move into
  a folder that already has some). `<` moves it out of its parent to just after
  it. Both are real moves on the server, subfolders included. While a rename,
  move or delete is still running on the server, further ones are refused with a
  short notice, so a quick second keypress can't act on a name that is changing.

The command palette has matching entries: New folder, Rename selected folder,
Delete selected folder, Hide or unhide selected folder, and Show hidden folders.
Creating a folder is also possible from the move picker with `n`.

### Needs You

**Needs You**, just below Unified Inbox in the sidebar, gathers the inbox
messages across every account that need you: ones tagged as needing a reply,
urgent, or important. Its number is how many there are, read or not. Reading a
message does not remove it; Needs You is about what still needs doing, not what
is unread (`u` still narrows the list to unread mail).

The tags come from annotations stored by plugins (for example TideMail Smart (JEV)),
from any plugin, installed or since removed. Without such a plugin Needs You
stays empty ("Nothing needs your attention."). Opening it only reads what is
already stored: it never runs a plugin or goes online.

The most pressing messages come first: urgent, then needing a reply, then
important (a message with several of these ranks higher), newest first within
each. With threaded conversations a whole thread appears if any of its
messages qualifies, and its tags combine the thread's signals.

If a message doesn't belong there, press `X` (or choose **Dismiss from Needs
You** in the palette). The message and its tags are untouched; TideMail just
remembers not to show it here, even if a plugin later tags it again. `Ctrl+Z`
while still in Needs You brings back the latest dismissal, and **Restore to
Needs You** in the palette does the same for a dismissed message opened from
its normal folder. **Why is this in Needs You?** lists the reasons and the
annotations behind them. All the usual actions (reply, archive, delete, move,
star, mark read) work on messages here.

### Waiting on Them

**Waiting on Them**, below Needs You, lists conversations where you sent the
latest message and are waiting for someone else to answer, across every
account. It is worked out from the conversation itself, not from plugins:
nothing is classified and nothing goes online.

- A conversation appears when its newest real message is from you (from any of
  your configured accounts' addresses; display names don't count) and went to
  at least one other person. First messages count; a reply beforehand is not
  needed.
- It leaves as soon as someone else's reply arrives, and returns when you
  answer again.
- Mail in Inbox, Sent, Archive, and other folders counts, so archiving a
  conversation doesn't hide it. Trash, Spam, and Drafts are ignored.
- Mail between your own addresses, and mail only to robots or lists
  (`noreply@`, `notifications@`, `mailer-daemon@`, obvious mailing lists), is
  left out. Bounces and drafts don't change a conversation's state.
- Mail you send from TideMail appears once TideMail has fetched your Sent
  folder, which it does right after sending. Mail sent from your phone or
  webmail appears after your next sync of Sent (`s` in Waiting on Them syncs
  inboxes and Sent folders).
- The longest wait comes first. The sender column shows who you're waiting
  for (`↗ Sarah +1`); the number beside the entry counts conversations.

Press `X` (or **Stop waiting** in the palette) to stop tracking a
conversation; `Ctrl+Z` right after, or **Resume waiting**, brings it back. This
only affects the current wait: if they reply and you answer again, or you send
another nudge, the conversation appears again. Stopping never changes your
mail or any plugin annotations. **Why is this in Waiting on Them?** says when
you sent the latest message and who you're waiting for.

Waiting on Them and Needs You are independent. After you reply, an old plugin
tag may still keep the message in Needs You until you dismiss it there.

### Snooze

Press `Z` (or **Snooze** in the palette) to hide something from Needs You or
Waiting on Them until later. In Needs You it snoozes the message (or every
selected message); in Waiting on Them it snoozes the conversation; elsewhere it
snoozes messages so they stay out of Needs You. With several messages selected,
the picker opens once and snoozes them all to the same time.

| Choice | Wakes at |
| --- | --- |
| Later today | 7:00 PM today; after 5:00 PM, three hours after the next full hour |
| Tomorrow morning | 8:00 AM tomorrow |
| Tomorrow evening | 7:00 PM tomorrow |
| This weekend | 9:00 AM Saturday (next Saturday once this Saturday's has passed) |
| Next week | 9:00 AM next Monday |
| Pick date/time | any future time, typed as `YYYY-MM-DD HH:MM` (past times are refused) |

Times are your local time (daylight-saving changes keep 9:00 AM at 9:00 AM)
and are stored as absolute times.

Snooze is local to TideMail. It creates no folder on the server, doesn't move
or archive anything, doesn't change read state, and leaves plugin tags alone:
the mail stays in its normal folders. **Snoozed**, below Waiting on Them, lists
what's snoozed, soonest to wake first, with the wake time in place of the date;
`Z` there unsnoozes, and **Why is this snoozed?** shows until when.

When a snooze ends, TideMail checks the item again: it comes back only if it
still needs you or is still waiting, using whatever plugin tags it has by then.
A dismissal or Stop waiting still applies after a snooze ends; snoozing never
clears them. If someone replies to a snoozed waiting conversation, the snooze
ends at once (the conversation isn't waiting any more, though the reply may
show up in Needs You). If you write again, that's a new wait and it shows
normally. Snoozes that came due while TideMail was closed end when it starts.
There's no notification when a snooze ends; the item simply reappears.

Receipt tables keep item descriptions, quantities, and prices together, even
when the sender uses ordinary cells for headings or merges cells for totals.
Amounts align on the right. When columns would be too cramped, each row becomes
a labelled record; tables without headings use label/value pairs for two-column
rows. This adapts automatically as you resize the reading pane. Complex tables
with row-spanning cells retain the simpler text fallback.

Newsletters keep each card together: an image stays next to its heading,
description, and button, and a thin rule separates one card from the next.
Cards that sit side by side in a browser stack in reading order, left to right
and then top to bottom. Quoted and forwarded messages are not grouped.

On a terminal with Kitty graphics support — Ghostty, Kitty, Rio —
images embedded in the message (CID parts, inline data URIs, and
content-location references) are drawn as real raster images inside the reading
pane. They are scaled to the pane width and to the sender's width and height,
never stretched or enlarged, and scroll, clip, and reflow with the surrounding
text. Changing the terminal font size re-lays them out for the new cell size. Remote images are blocked for
privacy: press `i` in the content pane to load them for the message you are
reading. On terminals without graphics support, every image becomes a labelled
text placeholder such as `[image: Fall sale]`, and the message stays readable.
Set `[display] images = "off"` to use placeholders everywhere, including on a
graphics-capable terminal.

Images supplied as MIME parts stay local even when their Content-Location is
an HTTPS URL. If a previously loaded remote image has left the image cache,
press `i` again when prompted to reload it.

Choose **Settings → Display → Reading → Images** (`Auto` or `Off`) and press
`Ctrl+S` to save. The change takes effect immediately; remote images still require
`i` to load. The setting also shows whether inline images are supported.

**foot and WezTerm: inline images are currently unsupported.** TideMail uses
text placeholders there because the renderer requires Kitty Unicode placeholders
and virtual placements. foot supports Sixel images, which this renderer does not
use. Email text and attachment saving still work; pressing `i` does not enable
image display in these terminals.

Search only reaches mail TideMail has cached. The first sync fetches the 100
most recent inbox messages and 25 messages from other folders, whose attachments
can make each batch much larger. Moving down past the last message in a folder
pulls the next 100 from the server, so you can page back as far as it goes.

## Writing mail

Press `c` to compose a message, or `r` to reply and `Ctrl+R` to reply all (from the
message list or content pane). Reply all puts the sender (or Reply-To) in To and the
other recipients in Cc, leaving out your own addresses. Recipient lines too long for
the field scroll sideways as you type or move the cursor. TideMail
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
Settings → Editor, or set it to `0` to send at once.

The **Outbox** has a row of its own in the accounts pane, under the Unified
Inbox — outgoing mail spans accounts the same way. Its badge counts the sends
that need you; a message passing through on its way out adds nothing to it.
Resting on the row lists what is waiting and why, and `Enter`, `Space`, or `O`
from anywhere opens the Outbox itself, where `r` retries a failed message and
`e` moves an unsent one back into compose.

The row is a doorway rather than a folder: the message pane previews it, but
archiving, starring and the other folder keys do not apply to a message that
has not been sent yet.

A send that fails does not go quiet. The status bar reports it — `retrying 1 in
Outbox` while automatic retries run, then `1 failed send in Outbox (2m)` once
they are spent — and keeps reporting it until the Outbox has nothing left
needing you. A message still inside its undo window is not a failure and says
nothing.

The time in brackets is how long that message has been waiting, which is what
tells you whether the warning is about the message you just sent or one that
has been stuck since yesterday. Both look the same otherwise.

A delivery interrupted mid-flight, which TideMail finds when it starts up
again, reads `1 unconfirmed send in Outbox` instead. That wording is the point:
the server may have taken the message before the connection went, so it is not
a failure and TideMail will not retry it on its own. Check Sent mail before
you retry one by hand.

Every attempt takes the sending address from the account as it stands at that
moment, not from a copy saved when the message was queued. So if a send failed
because the account's **From address** was wrong, correcting it and pressing
`r` is enough — the queued message does not have to be rewritten or composed
again.

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

Gmail currently requires an app password while Google approval is pending. Turn
on 2-Step Verification, generate one at
[myaccount.google.com/apppasswords](https://myaccount.google.com/apppasswords),
then paste it into the password field in the account manager (`M`) and save with
`Ctrl+S`.

### Gmail OAuth sign-in

Google OAuth is temporarily unavailable while TideMail's Google app awaits
approval. The account form and installer both call this out. Use a Google App
Password instead; existing saved Gmail OAuth accounts are left unchanged.

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
images = "auto"  # "auto" (inline + blocked remote), or "off" (text placeholders)

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

`from` is the **From address** row in the account form. Give it an address —
`alice@example.com` or `Alice <alice@example.com>` — or leave it blank to send
as `user`, which works only when `user` is itself an address. A display name on
its own is not enough: the account form refuses to save one, because SMTP has
no address to send from and would only fail once the message was already
queued.

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

### Plugins (experimental)

TideMail can run external plugins placed in
`$XDG_CONFIG_HOME/tidemail/plugins/` (or `~/.config/tidemail/plugins/` when
`XDG_CONFIG_HOME` is unset).
When any are installed, the command palette (`:`) gains **Plugins
(experimental)**, a read-only list of installed plugins, and **Run plugin**.
Plugins receive a message's sender, recipients, subject, date, and flags, never
its body, and they never change your mail.

**Run plugin** runs a plugin you choose on mail, whether or not it has already
been classified, for example after updating a plugin or changing its settings.
Press `p` in the message list or reader, or use the command palette. The
picker lists each plugin by name with a short description of what it does;
you pick once:

- **Run plugin on current message…** runs it on the message you are on and
  opens a result card that explains what the plugin found in plain words:
  for example **Newsletter · low priority**, followed by a short **Why**.
  Type, priority, confidence, raw tags, and technical information live under
  `d` **Details**; press `d` again to go back and `Enter` or `Esc` to close.
  When TideMail itself finds an unsubscribe option in the message, the card
  says **Available — press u**. `u` uses the same safe flow as `Ctrl+U` in the
  reader: TideMail shows the destination and requires confirmation before a
  web unsubscribe, or opens a prefilled unsubscribe draft for email links.
  Otherwise the card says **No unsubscribe option**. The plugin never receives
  the unsubscribe link and cannot unsubscribe on its own. If the plugin found
  nothing new, the card says **No changes**; if it failed or took too long, the
  card says so in plain words, with the technical reason under Details.
- With messages selected (`Space`), the same command reads **Run plugin on N
  selected messages…** and runs on the selection (asking first for 10 or
  more).
- **Run plugin on all N messages in Inbox…** (or the current folder, Unified
  Inbox, search results, Needs You, Waiting on Them, or Snoozed) runs on every
  message the list currently shows. It always asks first, with the count.
  Only mail already in TideMail's cache is used; nothing is downloaded.

After you have run a plugin, the palette also offers **Run *name* again**, which
skips the picker.

Several messages run one at a time, with progress on the status line
(`running on 312 messages in Inbox… 87 / 312`), and end with one summary card:
how many were processed, changed, unchanged, or failed, and what the plugin
found (for example **Newsletter 9**, **Automated sender 2**). Failure reasons
are under Details. A failing message does not stop the run, and its earlier
annotations are kept. **Cancel plugin run** in the palette stops a run; quitting
TideMail stops it too. The run works whether or not the plugin processes new
mail automatically, and does not affect that.

A plugin that also declares the `annotations` permission can attach short
notes to a message, such as "needs reply" or "category: github". TideMail
stores them in its local cache and shows them as tags in message rows, the
same way in every folder and view: `↩ REPLY`, `! URGENT`, `◆ IMPORTANT`, and
the category name (`GITHUB`). On narrower screens tags shrink to their
symbols, with the category becoming a single glyph chosen for that category
(`↩ ◆ ⚙` for github), then to a plain `#github`, or disappear before the
subject does. Each plugin run replaces
that plugin's notes on the message; a successful run with nothing to say
clears them. **Message annotations** in the palette lists all of a message's
annotations. Running a plugin can move messages into or out of Needs You at once;
snoozed messages stay snoozed, and Waiting on Them does not use annotations.

With a message highlighted or open, press `e` to edit its tags. You can also
search the command palette for **Edit tags / Correct classification**, or press
`e` in **Message annotations**. Use `↑`/`↓` to choose a field, `←`/`→` to
choose a value, and `Enter` to save it. You can correct needs reply, urgency,
importance, or category. A correction overrides every plugin for that field,
while the original plugin annotations remain visible. **Use plugin decision**
or **Reset all corrections** removes the local decision. Corrections survive
plugin reruns, annotation cleanup, and plugin removal. They never leave your
computer: they are not sent to Jev, TypeSafe, or any plugin with a message.
The one exception is report plugins that declare permission to read
classifications (`annotations_query` and `analytics_read`). Those can see a
message's current classification, including that it came from your
correction. Reports with `analytics_read` also see corrected categories as
part of category totals.

Choose how tags look under **Settings → Display → Appearance → Annotation
Tags**:

- **Tag style**: use `←`/`→` on the single picker to choose **Pills (Square)**,
  **Pills (Round)**, **Pills (None)**, **Compact**, **Plain**, or
  **Glyph Pills (Round)**. The picker
  names and previews every choice. Square pills pad each side, Round uses
  rounded caps (needs a Nerd Font), and None removes the padding. Compact uses
  colored text such as `↩REPLY #github`; Plain uses uncolored
  `[REPLY] [github]`. Glyph Pills (Round) shows only the matching category
  glyph (for example, ⚙ for GitHub or ✉ for newsletters) inside rounded
  colored pills, even when full labels would fit. Unknown categories use ●;
  disabling icons uses ASCII markers. On narrow rows the caps may drop to
  preserve subject space.
- **Tag colors** opens an editor for the background and foreground of each
  tag: needs reply, urgency, importance, the default category color, and the
  known categories (github, shipping, security, calendar, newsletter, support,
  social, billing, receipt, notification, personal). Other categories use the
  default category color. Select a tag and press `b` or `f` to open a visual
  color picker for that swatch: `tab` moves between the RGB sliders (or the
  HSL field in HSL mode, toggled with `m`) and the hex/R/G/B/H/S/L fields,
  `↑↓←→` adjust whatever has focus, and `enter` on a field types an exact
  number while `enter` elsewhere applies the color and returns to the list.
  `#` jumps straight to typing a hex value, `y` copies the current hex to the
  clipboard, and `esc` cancels without changing anything. Press `r` on a tag
  to reset both its colors to follow the theme. Changes apply and save the
  moment you confirm; colors you do not set follow the theme, and plugins can
  never choose colors.

Annotations stay after you remove a plugin, so its tags remain until you clear
them. In **Message annotations**, select a plugin and press `c` to clear its
annotations from that message, or `C` to clear all of them. In **Plugins
(experimental)**, select a plugin (including one listed under **Stored data from
removed plugins**) and press `c` to clear everything it stored. TideMail asks
before clearing, and clearing never deletes or changes your mail.

A plugin can also process new mail automatically, if it supports that and you
turn it on: in **Plugins (experimental)**, select it and press `a`, then
confirm. It then receives the same metadata (never message bodies) for each
newly arrived unread message, and its tags appear as mail comes in. Old mail,
first-time setup of an account, and loading older messages are never sent.
The plugin list shows its current queue, running state, dropped events,
failures, last success, last failure, and a sanitized last error when those
values exist. Three consecutive automatic failures pause that plugin; press
`r` on it to resume. Manual **Run plugin** actions remain available while
automatic processing is paused.

Each plugin's entry in **Plugins (experimental)** reads like a short product
page: its name and description, its permissions as a checklist in plain words
(✓ Message details, ✓ Save tags, ✗ Message body, ✗ Network), and whether
**Automatic processing** is on.

Automatic processing is off for every plugin until you enable it. A plugin that
fails three times in a row is paused; select it and press `r` to resume.

Some plugins are **reports**, such as mail analytics. The plugin list shows
**enter  run report** for them: select one in **Settings → Plugins** or
**Plugins (experimental)** and press `enter`. The report opens in the plugin
result window when it finishes; closing a report launched from Settings
returns to the Plugins section. Press `r` in the plugin list to reopen the
last result. Reports can be charts as
well as text: stat tiles, sparklines, bar charts, heatmaps, and tables, drawn
in your theme. Category bars use your category tag colors, so changing those
under **Settings → Display → Appearance** changes the charts too. Scroll with
`↑`/`↓`. To answer, a report asks
TideMail read-only questions, only those its permissions allow (the list shows
them): for example message counts per day, category totals, or a list of
conversations. TideMail answers from its local cache. A report never sees
message bodies, cannot change your mail or TideMail's state, and cannot draw
its own screens or colors. If a folder or account is selected in the sidebar,
the report can treat it as the current one.

Open **Settings → Plugins** to administer plugins directly in the right-hand
pane. Press `→` to focus the list, `↑`/`↓` to select a plugin, and `s` to edit
its settings in the same pane. The list also offers automatic-processing,
resume, and stored-data controls when available. In a plugin form, `esc`
returns to the list; from the list, `esc` returns to the Settings sidebar.
`Tab` also returns to the sidebar when you are not typing a secret.
Plugin changes save immediately. The command palette's **Plugins (experimental)**
view still offers `s` to open a plugin's settings. Secret values such as API keys are stored in your system keychain, never
in `config.toml`, and are shown only as `************`. Plugins are separate
programs, not part of TideMail's mail handling: for example, TideMail Smart (JEV)
(an external plugin) can call TypeSafe's Jev model over the network, and its
settings describe exactly what it sends.

Plugin support is experimental. Only install plugins you trust. To write a
plugin, see [`plugins.md`](plugins.md) and the Plugin API v1 docs in
[`plugins/`](plugins/README.md).

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
| `Enter` on a folder | Fetch that folder now; on a folder with subfolders, collapse or expand it |
| `n` / `r` / `d` in Accounts | New, rename, delete the selected folder (see Managing folders) |
| `H` in Accounts | Hide or unhide the selected folder (local only) |
| `>` / `<` on a folder | Nest it under the folder above / move it out of its parent |
| `Shift+K` / `Shift+J` on a folder | Walk the folder up / down the tree: reorder, move into an open parent, or out of its parent |
| `←` / `→` on a folder | Collapse or go to parent / expand |
| `F` | Sync all mailboxes (asks first when there are more than 10 folders) |
| `Enter` in Drafts | Reopen selected draft in compose |
| `d` in Drafts | Delete selected draft |
| `r` | Reply to the selected or open message |
| `Ctrl+R` | Reply all to the selected or open message |
| `x` | Mark selected message(s) read |
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
| `X` | In Needs You: dismiss the message; in Waiting on Them: stop waiting (mail and tags untouched) |
| `Z` | Snooze from Needs You / Waiting on Them (local only); in Snoozed: unsnooze |
| `Ctrl+Z` | Cancel a queued send; in Needs You or Waiting on Them, undo the latest dismiss or stop; otherwise undo the latest pending delete, archive, or move |
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

- Display: theme, icons, annotation tag style and colors, date format, mark-read behavior, focus line, show sender, unread-first ordering, actionable links, reading width, browser command, density, show email headers, desktop notifications, and quit confirmation
- Editor: compose keys, send delay, and maximum delivery attempts
- Accounts: connection details, From address, signature, color, and sync interval
- Updates: check, install, restart, or copy a manual install command
- AI: OpenAI, Claude, Gemini, or Ollama summary settings
- Advanced: logs and feed max body size
- Support: choose the Ko-fi Open or Copy link button with Up/Down and press Enter; Ctrl+C also copies the link, and Esc returns to Settings sections
- About: Support Tidemail, repository, and issue links

Open the command palette with `:` or `Ctrl+P` and choose **Support Tidemail**
to go directly to the same Support page. Browser-launch failures leave the URL
in the status line so it can still be copied. Tidemail does not show donation
prompts during normal email use. A Pac-Man and coffee cup circle the page's
white dotted border only while Support is open.
