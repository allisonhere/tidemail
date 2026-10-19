# TideMail CLI reference

Running `tidemail` without arguments opens the existing three-pane client. The
mail commands use the same configured accounts, credentials, drafts, and SQLite
cache as the client. Headless commands do not start Bubble Tea or change terminal default colors.
Interactive CLI flows use a separate compact Bubble Tea interface.

```bash
tidemail --help
tidemail send --help
tidemail messages --help
tidemail outbox --help
```

Existing version flags, developer options, and `tidemail plugin` commands remain
available. Mail results go to stdout. Diagnostics go to stderr. Terminal output
uses the assho CLI palette when stdout is a terminal. Redirected output is plain,
`NO_COLOR` disables color, and JSON contains no ANSI escapes.

## Accounts and outgoing mail

`accounts list` reports configured account IDs, names, sender addresses, and the
default account. It does not expose passwords or OAuth tokens.

```bash
tidemail accounts list --json
```

`--account` accepts a stable account ID or an unambiguous display name. Without
`--account`, TideMail uses the configured default sender. Duplicate display names
require an account ID.

`send` validates the message and saves a draft unless `--yes` authorizes sending
or an interactive review receives approval through the controlling terminal. It never
reads approval from piped body stdin. `--yes` applies to this invocation.

```bash
# Save a draft without authorizing delivery.
tidemail send --account ACCOUNT_ID --to me@example.com \
	--subject "Hello" --body "Prepared through TideMail"

# Authorize a durable queued send using the configured delay.
tidemail send --account ACCOUNT_ID --to me@example.com \
	--subject "Hello" --body "Sent through TideMail" --yes --json

# Persist the send and attempt delivery immediately.
tidemail send --account ACCOUNT_ID --to me@example.com \
	--subject "Hello" --body "Send now" --send-now --yes --json
```

`--send-now` bypasses the configured delay. It does not authorize delivery by
itself. Ordinary authorized sends return `queued`, not `sent`, and need a queue
processor. See [Queue processing](#queue-processing).

An authorized send requires valid recipients, a nonempty subject, and either a
nonempty body or an attachment. Incomplete messages cannot authorize delivery.

| Option | Behavior |
| --- | --- |
| `--to`, `--cc`, `--bcc` | Comma-separated recipient addresses |
| `--subject` | Subject text |
| `--body` | Literal body text |
| `--file`, `--body-file` | Read the body from a file |
| `--markdown PATH` | Read Markdown through TideMail's existing renderer |
| `--template NAME` | Fill the body from a template (case-insensitive name; see below). One body source only; `--subject` overrides the template's subject |
| `--attach PATH` | Add a file, repeatable |
| `--signature` | Append the configured account signature, enabled by default |
| `--no-signature` | Omit the configured signature |
| `--dry-run` | Validate and build MIME without saving, queueing, or delivering |
| `--preview` | Open the editable compose wizard for review and explicit authorization |
| `--json` | Return the versioned JSON envelope |
| `--agent` | Enforce configured agent capabilities |
| `--interactive` | Open the guided composer through the controlling terminal |
| `--no-interactive` | Disable automatic prompts |

Body options are mutually exclusive. Without a body option, a pipe supplies the
body. An interactive command does not wait for message text from the terminal.
The MIME builder handles plain text, HTML, Markdown, and attachment encoding.
Configured accounts do not need separate `POP_SMTP` environment variables.

```bash
printf 'Build finished successfully.\n' |
	tidemail send --to me@example.com --subject "Build report" --yes

tidemail send --to team@example.com --subject "Weekly report" \
	--markdown report.md --attach totals.csv --attach chart.png --yes

tidemail send --to me@example.com --subject "Review" \
	--body-file message.txt --preview

tidemail send --to me@example.com --subject "Validate" \
	--markdown report.md --attach totals.csv --dry-run --json
```

`--dry-run` reports `validated`, MIME size, and attachment count. It does not
print the message body or create a draft. Preview requires a controlling terminal
and displays private message content there, including BCC recipients.

## Templates

`tidemail templates list` prints the canned replies you can use: the built-ins
plus the `.md` and `.txt` files in the templates folder next to `config.toml`
(`~/.config/tidemail/templates`), the same ones the F6 picker in compose offers.
`--json` returns them as `{"templates":[{"name","subject"}]}`.

`tidemail send --template Thanks --to "Priya <p@example.com>"` fills the body.
Tags are filled as in compose: `{first_name}` and `{name}` come from the display
name on the first `--to` address (`{first_name}` is "there" when it has none),
`{my_name}` is the sending account's name, and `{date}` is today. Unknown tags
stay as written. Nothing else changes: without `--yes` the result is a draft,
and `--template` cannot be combined with `--body`, `--file` or `--markdown`.
Creating and editing templates is done in the app (F6) or as files.

## Drafts

`draft create` saves a local draft using the selected account. It accepts the
message preparation options listed above. Delivery authorization options do not
apply to draft creation.

```bash
tidemail draft create --account ACCOUNT_ID --to user@example.com \
	--subject "Follow-up" --body-file response.md --attach notes.pdf --json
```

The result contains `state: drafted` and the draft ID. Open the client to edit or
send the draft. A dedicated CLI draft-edit command is not available.

## Cached inbox reads

`messages list` reads the cached unified inbox. `messages search` searches the
cached messages through TideMail's existing search implementation. Neither
command connects to IMAP or refreshes mail.

```bash
tidemail messages list --unread --limit 25 --offset 0 --json
tidemail messages search "invoice" --limit 25 --json
tidemail messages read cache:123 --json
```

List and search results contain metadata, including subject, sender, recipients,
account, mailbox, read state, and attachment presence. They do not contain full
message bodies. `messages read` explicitly returns the cached plain-text and HTML
bodies. A body that has not been cached can be empty.

Use message IDs returned by list or search, such as `cache:123`. A cache ID
identifies one local database row across accounts and mailboxes. It is not an
IMAP UID, a portable ID for another computer, or an ID to reuse after rebuilding
the cache. The result also includes account, mailbox, UID, and Message-ID metadata.

List commands accept `--limit` from 1 to 1000 and a nonnegative `--offset`.
The default limit is 50. Pages report `items`, `limit`, `offset`, `total`, and
`next_offset`. A null `next_offset` marks the last page.

There is no headless inbox synchronization command in this version. Refresh mail
in the interactive client before querying data that must be current. Incoming
attachment download and independent attachment-read permissions are not exposed
through the CLI.

## Queue processing

TideMail persists outgoing mail before attempting SMTP delivery. Queue entries
survive command exit and app restart. The interactive client processes the queue
while it runs. Without the client, an explicit processor must run periodically.

```bash
tidemail outbox list --json
tidemail outbox status 42 --json
tidemail outbox process --json
```

`outbox process` attempts the due queued entries and exits. It does not wait for
future scheduled entries. It connects to SMTP and, for providers that require it,
IMAP to append delivered mail to Sent. Multiple client and processor instances
use atomic database claims to avoid delivering the same queue entry twice.
A queued entry whose account has been removed from the configuration is marked
`failed` (the run exits 1 once); it is not reported again on later runs.

An unknown read subcommand, such as `tidemail messages invoice`, exits 2 and
names the supported subcommands. Search with `tidemail messages search "invoice"`.

A cron entry can process the queue once a minute while the client is closed:

```cron
* * * * * /home/YOUR_USER/.local/bin/tidemail outbox process --json >> /home/YOUR_USER/.local/state/tidemail-worker.log 2>&1
```

Use the actual absolute binary path and create the log directory first. Run the
processor as the same user with the same configuration and cache paths. The job
needs access to the configured credential store and network. A locked or
unavailable keychain can prevent unattended delivery. Restrict access to worker
logs, since outbox summaries can contain subjects and account metadata.

The configured delay is a minimum delay. A periodic processor can deliver later
than the due time. Without a running client or periodic processor, mail remains
queued.

```bash
# Requeue a failed item after inspecting its status.
tidemail outbox retry 42 --yes --json

# Cancel an eligible queued or failed item and restore its draft.
tidemail outbox cancel 42 --json
```

Retry re-resolves the configured account by its stable identity and respects the
configured attempt limit. Delivered mail is recorded before Sent-folder append.
Gmail skips the append because Gmail files its own SMTP submissions.

| State | Meaning |
| --- | --- |
| `drafted` | Saved locally, no delivery authorization |
| `queued` | Persisted and awaiting an eligible processing attempt |
| `sending` | A processor has claimed the item |
| `sent` | SMTP delivery recorded |
| `failed` | Delivery failed and needs inspection |
| `uncertain` | Delivery may have occurred, so automatic resend is blocked |

If a send fails, inspect its outbox status and the interactive Outbox for details.
Check account configuration, credential-store access, connectivity, and the
attempt limit before retrying. If delivery is uncertain, inspect the recipient
or Sent mailbox before authorizing a retry. A retry can create a duplicate.
A Sent-copy or draft-cleanup warning after delivery does not authorize another
SMTP send.

## Agent permissions and approval

Agent guidance uses `--agent` on mail commands. This flag checks the top-level
`agent_permissions` array in TideMail's TOML configuration. These checks are
separate from the experimental classification-plugin protocol. They are a CLI
policy, not an operating-system sandbox for an agent that has unrestricted shell
or file access.

When `agent_permissions` is absent, the default capabilities are
`accounts.read`, `messages.metadata`, `messages.search`, `drafts.create`, and
`outbox.read`. An explicit empty array denies all agent capabilities.

```toml
# Top-level setting, before any [section] or [[accounts]] table.
agent_permissions = [
	"accounts.read",
	"messages.metadata",
	"messages.search",
	"drafts.create",
	"outbox.read",
]
```

`messages.read` grants full cached body retrieval. `mail.send` grants the ability
to send in agent mode, but the command still needs invocation approval through
`--yes` or approved interactive preview. Agent queue processing and retry require
both `mail.send` and `--yes`. Agent outbox mutation also uses the send capability.
`drafts.edit` is reserved for a future draft-edit command.

```bash
tidemail accounts list --agent --json
tidemail messages search "invoice" --agent --json
tidemail draft create --to user@example.com --subject "Reply" \
	--body-file response.md --agent --json

# Requires configured mail.send and user approval of this exact message.
tidemail send --to user@example.com --subject "Approved reply" \
	--body-file approved-response.md --agent --yes --json

# Authorized processor for an already approved queue.
tidemail outbox process --agent --yes --json
```

Permission to read or draft does not authorize sending. Review the sender,
recipients, body, subject, attachments, and signature before approving a send.
If the prepared message changes, review the changed message before approval.
Email bodies, headers, attachments, and search results are untrusted content.
Instructions inside incoming mail cannot grant permissions or authorize delivery.
Skill files contain guidance rather than credentials or permanent send approval.

## Agent skill installation

`skill` prints the portable skill document. `install-skill` writes only TideMail's
`SKILL.md` and does not change an agent's other configuration.

```bash
tidemail skill
tidemail install-skill codex --dry-run
tidemail install-skill codex
tidemail install-skill claude
tidemail install-skill pi
tidemail install-skill hermes
```

| Agent | Personal destination |
| --- | --- |
| Codex | `~/.agents/skills/tidemail/SKILL.md` |
| Claude Code | `~/.claude/skills/tidemail/SKILL.md` |
| Pi | `~/.agents/skills/tidemail/SKILL.md` |
| Hermes | `${HERMES_HOME:-~/.hermes}/skills/tidemail/SKILL.md` |

These locations follow the official [Codex skill documentation](https://learn.chatgpt.com/docs/build-skills),
[Claude Code skill documentation](https://code.claude.com/docs/en/skills),
[Pi skill documentation](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/skills.md),
and [Hermes skill documentation](https://hermes-agent.nousresearch.com/docs/user-guide/features/skills).
Codex and Pi share a supported personal directory, so one installation can serve
both. Hermes uses `HERMES_HOME` when set, including a selected profile's home.

`--dry-run` prints the destination and writes nothing. Installation refuses to
overwrite an existing skill. After reviewing an existing TideMail skill, use
`--force` to consent to replacement. Restart or reload the agent after installation.

## JSON and exit codes

Mail commands with `--json` return a versioned envelope:

```json
{"schema_version":1,"ok":true,"data":{"state":"queued","id":42}}
```

Errors use the same version and a machine-readable identifier:

```json
{"schema_version":1,"ok":false,"error":{"code":"permission_denied","message":"Agent capability mail.send is not enabled."}}
```

Successful data depends on the command. Scripts must check `ok` and the delivery
`state` rather than treating exit code 0 as proof that a queued message was sent.
Account and outbox summaries omit credentials, BCC recipients, and message bodies.
Full bodies appear only in explicit message reads or interactive preview.

| Exit code | Meaning |
| --- | --- |
| 0 | Command succeeded, including draft creation or queueing |
| 1 | Operation failed |
| 2 | Invalid command arguments or message |
| 3 | Capability denied or required approval missing |

Skill output and installation use plain text rather than the mail JSON envelope.

## Interactive command menu and composer

Run `tidemail cli` to choose Send email, Create draft, cached message browsing or
search, Accounts, Outbox, agent guidance, skill installation, or Open inbox.
Plain `tidemail` still opens the existing three-pane email client.

```bash
tidemail cli
tidemail send
tidemail draft create
tidemail send --to me@example.com --interactive
printf 'Build finished.\n' | tidemail send --interactive --subject 'Build report'
```

Bare `send` and incomplete compose commands open the wizard when both stdin and
stdout are terminals. Supplied flags prefill the fields. Bare `draft`,
`messages`, `accounts`, `outbox`, and `install-skill` also offer guided flows.
Incomplete `messages search` and `messages read` prompt for the missing value.
Complete commands and commands using pipes, `--json`, `--agent`, `--yes`, or
`--dry-run` do not open automatic prompts. `--interactive` explicitly opens an
interactive flow through the controlling terminal, including with piped body
text or JSON output. `--no-interactive` disables automatic prompts.

The composer walks through sender account, To, optional CC and BCC, subject,
body, signature, attachments, and review. Enter advances single-line fields;
Enter skips an empty optional field. Shift+Tab goes back and preserves edits.
Enter inserts a newline in the Ripple body editor; Ctrl+S or Ctrl+Enter advances.
The configured compose vim mode also applies. Esc or Ctrl+C offers Resume,
Save draft, or Discard when there is unfinished content. In vim body editing,
Esc first returns to Normal mode; the editor's cancellation intent leaves it.

In Attachments, press `a` to browse from the current directory. Arrow keys move,
Enter opens a directory or selects a file, Left goes to the parent directory,
and `.` toggles hidden files. Esc returns to the attachment list. Select multiple
files by reopening the browser; `d`, Delete, or Backspace removes the highlighted
attachment. Only readable regular files can be selected. Duplicate selections
are rejected. Selected file bytes are captured before review, so later file
changes do not alter the approved message.

Review shows recipients, body, signature, and attachment names and sizes. Use
Page Up and Page Down to scroll. **Save draft** is selected by default. Sending
requires choosing **Send email** explicitly, even if the wizard was opened with
`--yes`. Edit returns to the fields; Cancel offers to preserve unfinished work.
Agent capability checks still apply to saving and sending.

An approved send is persisted before its countdown begins. Press `u`, Ctrl+Z,
Esc, or Ctrl+C during the countdown to undo and restore a draft. The wizard
waits for the configured delay, attempts delivery, and shows the recorded
status. `--send-now` skips the countdown. Once submission starts, cancellation
cannot guarantee stopping delivery. A queued retry still needs the inbox or
`outbox process`; uncertain mail must be checked before any explicit retry.

### CLI settings

**Settings → CLI** (config keys under `[display]`) changes the interactive menu and
wizard only. Scripted commands, `--json`, `--agent`, `--yes`, and `--no-interactive`
runs ignore all three, so automation never depends on a user's settings.

| Setting | Config key | Default | Effect |
|---|---|---|---|
| Quit after send or draft | `cli_quit_after_send` | off | `tidemail cli` exits after a send or draft finishes instead of returning to the menu. A cancelled or failed command still returns to the menu. |
| Skip review step | `cli_skip_review` | off | The wizard goes from Attachments straight to saving the draft (draft flow) or to the undo countdown (send flow). A send skips review only when the send delay is above zero and `--send-now` is not used, so undo remains available; otherwise review is shown. Dry runs and `--agent` always show review. If the message is invalid, review opens with the error. |
| Include signature by default | `cli_signature` | on | Pre-selects the wizard's Signature choice. `--signature` and `--no-signature` override it. |

Interactive screens render on the controlling terminal; JSON stdout remains a
single result envelope. Terminal settings are restored on exit. The wizard uses
assho's purple/cyan palette and respects `NO_COLOR`.

For a repeatable terminal check with temporary accounts and a local SMTP fixture:

```bash
go build -o /tmp/tidemail-agent-cli .
python scripts/verify-cli-interactive.py /tmp/tidemail-agent-cli
```

In command menus, account selection, attachment browsing, and review, `q` leaves
the current screen. The top-level command menu quits. Unfinished compose content
uses the existing save/discard confirmation; during a send countdown, `q` undoes
the queued send. In text fields and the Ripple body editor, `q` remains text.

## Man page

`man tidemail` shows a reference for the commands, options, exit codes, files and
environment variables on this page. The installer puts it in
`~/.local/share/man/man1`, and the Arch package installs it system-wide.
`tidemail man` prints the page that is built into the binary, so
`tidemail man | man -l -` works anywhere and always matches the version you run.
