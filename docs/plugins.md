# Plugins (experimental, developer notes)

> **Status:** experimental plugin support. TideMail loads plugins at startup,
> but a plugin only ever runs when you start it by hand. Its only lasting
> output is annotations, and nothing it returns changes your mail. There are
> no automatic events and no plugin actions yet. The format below may change
> without notice.

The `internal/plugin` package discovers plugins on disk, validates their
manifests, and exchanges one JSON request/response with a plugin process.
TideMail uses it for one manual action: sending the current message's metadata
to a plugin, showing what comes back, and storing any annotations the plugin
is allowed to attach.

## Using plugins in TideMail

With no `plugins/` directory, TideMail looks exactly as before: the plugin
commands appear in the command palette (`:`) only when at least one plugin, or
a plugin problem, was found at startup.

- **Plugins (experimental)** opens a read-only list. Each valid plugin shows its
  ID, name, version, API version, and declared permissions. Each broken plugin
  shows why it was skipped. Press `r` there to reopen the last result.
- **Run plugin on current message** opens a picker listing only plugins that
  declare `message_metadata = true`. Choosing one sends that message's metadata
  (see `message.metadata` below) and never its body. The plugin runs in the
  background, so TideMail stays responsive. The status line shows
  `running plugin <id>…` and then the outcome.
- A successful response opens a read-only result window with the response data
  pretty-printed. If another window (compose, settings) was opened meanwhile,
  the status line says the result is ready instead of interrupting it.
- Only one plugin runs at a time. A plugin still running when TideMail quits is
  killed.
- Annotations the plugin may store show as small badges in the message list
  (see [Annotations](#annotations)). **Message annotations** in the palette
  lists every annotation on the current message, grouped by plugin. It appears
  only when that message has annotations, and it stays available after a
  plugin is uninstalled.
- Results and failures are also written to the in-app log (**Settings → Advanced →
  View Logs**). The log records status lines only, never metadata or response data,
  and is not written to disk.

Nothing in a response is treated as an instruction. TideMail never moves,
deletes, or marks mail, sends mail, opens URLs, runs commands, or changes
settings because of plugin output. Annotations are the only plugin output
TideMail keeps.

## Directory layout

Plugins live under `$XDG_CONFIG_HOME/tidemail/plugins/` (normally
`~/.config/tidemail/plugins/`, from `config.PluginDir()`), one directory per
plugin:

```text
~/.config/tidemail/plugins/
    hello/
        plugin.toml
        tidemail-plugin-hello
```

- A missing `plugins/` directory means zero plugins. It is not an error.
- Subdirectories are read in name order. Hidden entries (`.name`) and plain
  files are ignored. A symlinked plugin directory is followed.
- A broken plugin (missing or invalid manifest, missing or non-executable
  command) is skipped and reported by `Manager.Errors()`. Its siblings still
  load.
- If two plugins share an `id`, **both** are skipped and reported.

## Manifest (`plugin.toml`)

```toml
id = "hello"                       # required; stable identifier, not a display name
name = "Hello Plugin"              # required
version = "0.1.0"                  # optional, informational
api = 1                            # required; must equal plugin.APIVersion
command = "tidemail-plugin-hello"  # required; relative to the plugin directory

[permissions]                      # all default to false
message_metadata = false
message_body = false
network = false
annotations = false
```

Validation rules:

- `id` must match `^[a-z0-9][a-z0-9_-]{0,63}$`.
- `command` must be a relative path that stays inside the plugin directory, so
  absolute paths and `..` are rejected. It is never looked up on `$PATH`: a
  bare `command = "sh"` means `<plugin dir>/sh`.
- Unknown keys are rejected, so a typo such as `[permisions]` is caught instead
  of silently leaving every permission off.

## Protocol v1

Each call starts the plugin once. TideMail writes a single JSON request to
stdin, followed by a newline, and closes stdin. The plugin writes a single JSON
response to stdout and exits with status 0.

Request:

```json
{"api": 1, "type": "request", "request_id": "3f9c…", "method": "ping", "data": {}}
```

Success response:

```json
{"api": 1, "type": "response", "request_id": "3f9c…", "ok": true, "data": {"message": "pong"}}
```

Error response:

```json
{"api": 1, "type": "response", "request_id": "3f9c…", "ok": false,
 "error": {"code": "unknown_method", "message": "frobnicate"}}
```

TideMail rejects a response in any of these cases:

- `api` is not 1.
- `type` is not `"response"`.
- `request_id` differs from the request's.
- `ok` and `error` disagree.
- stdout is not exactly one JSON value (whitespace around it is allowed).
- stdout is larger than 1 MiB.

A non-zero exit status is also a failure.

### Methods

| Method | Data sent | Expected `data` |
| --- | --- | --- |
| `ping` | none | `{"message": "pong"}` |
| `message.metadata` | one message's metadata (below) | a JSON object; `annotations` is read (below), everything else is shown and never acted on |

`message.metadata` is sent only to plugins whose manifest declares
`message_metadata = true`. The check happens in `internal/plugin` before the
process starts, not just in the picker. A plugin without the permission is
never launched, and `Manager.Call` refuses this method outright so the check
cannot be bypassed. The data is:

```json
{
  "id": 1234,
  "message_id": "<abc@example.com>",
  "from": "Ann <ann@example.com>",
  "to": "me@example.com",
  "cc": "",
  "reply_to": "",
  "subject": "Your build passed",
  "date": "2026-09-26T10:00:00Z",
  "read": false,
  "starred": false,
  "has_attachment": false,
  "flags": ["\\Seen"],
  "account_name": "Work",
  "mailbox_name": "INBOX"
}
```

`id` is TideMail's own message number, only for matching a result to its
message. Empty fields are omitted. The body, HTML, raw headers, attachments,
and the AI summary are never sent, and neither are passwords, tokens, API keys,
or file paths.

## Annotations

A `message.metadata` response may attach annotations to the message:

```json
{
  "summary": "Looks like a CI notification.",
  "annotations": [
    {"key": "needs_reply", "value": "false", "confidence": 0.94},
    {"key": "category", "value": "notification"}
  ]
}
```

`annotations` is the only field TideMail interprets. Every other field, and
any unknown field inside an annotation, is display-only.

### Permission

Annotations are stored only when the manifest declares **both**
`message_metadata = true` (to receive the message at all) and
`annotations = true`. Without `annotations`, the plugin still runs and its
response is still shown, but nothing is stored. This is enforced in
`internal/plugin` (`Manager.MessageMetadata`), which is the only code path from
plugin output to storage, not in the picker.

### Rules

The whole set is validated before anything is written. One invalid entry
rejects the entire response, and stored annotations stay as they were.

- At most **32** annotations per response, with no duplicate keys.
- `key`: required, at most **64** characters, lowercase `a-z`, `0-9`, `_`, `.`,
  or `-`, starting with a letter or digit.
- `value`: a string of at most **512** bytes of valid UTF-8, with no control
  characters (escapes, newlines), format characters (bidi overrides,
  zero-width), or line/paragraph separators. It may be empty.
- `confidence`: optional; when present, a number from 0 to 1 inclusive.
  Omitted and `0` are different.
- `annotations` must be an array and the response data a JSON object.
  Missing or `null` annotations count as an empty set.

### Storage: replace on success

Annotations live in TideMail's local cache database, in the
`plugin_annotations` table (plugin ID, message, key, value, confidence, update
time). The `messages` table is not changed.

- A successful run makes the returned set the plugin's **complete** set for
  that message, in one transaction: keys the plugin no longer returns are
  removed. Returning no annotations clears that plugin's annotations on the
  message.
- Other plugins' annotations on the same message are never touched.
- A failed run (crash, timeout, bad response, plugin error), a rejected set, or
  a failed write leaves stored annotations unchanged.
- Annotations belong to TideMail's local copy of a message. Deleting the
  message from the cache (locally, when it disappears from the server, or on a
  cache reset) deletes its annotations. Re-syncing a message keeps them. A
  message that moves to another folder on the server is re-cached as a new
  message there and starts without annotations.

### Display

Rows show at most three small badges before the date, from these
**conventional** keys only. They are display conventions, not schema: any
other key is stored and shown under **Message annotations**, but never in a
row.

| Key | Value | Badge | ASCII (icons off) |
| --- | --- | --- | --- |
| `needs_reply` | `true`, `yes`, `1` | `↩` | `R` |
| `urgency` | `high`, `urgent`, `critical` | `!` | `!` |
| `importance` | `high` | `◆` | `^` |
| `category` | a short tag (`a-z0-9_-`, up to 12 chars) | `#tag` | `#tag` |

Values are matched case-insensitively. If several plugins set the same key, the
row shows one badge. With threaded conversations, a thread's row shows the
badges of its representative (newest) message only; annotations are not
combined across a thread yet.

All displayed annotation text is sanitized again before drawing, even though it
was validated before storage.

## Security boundary

Plugins are ordinary programs that run with your user account's privileges.
**TideMail does not sandbox them.** Only install plugins you trust. TideMail
limits what it hands them:

- The command runs directly with `exec`, never through a shell. No arguments
  are passed, and the working directory is the plugin directory.
- The environment is reduced to `PATH`, `HOME`, `USER`, `LANG`, `LC_ALL`,
  `LC_CTYPE`, `LC_MESSAGES`, `TMPDIR`, and `TZ`. `TIDEMAIL_*` client secrets
  and provider API keys are not passed on.
- Plugins never receive database handles, account passwords, OAuth tokens, or
  config secrets. They only see what a request carries.
- Each call has a timeout (5 seconds by default). On Unix, the plugin's whole
  process group is killed when the timeout expires.
- Plugin stderr is kept only for error messages. It is capped at 16 KiB, cut to
  512 characters in errors, and stripped of control characters so a plugin
  cannot inject terminal escape sequences.
- `[permissions]` decides what TideMail sends and keeps. `message_metadata` and
  `annotations` are enforced: without the first, a plugin never receives
  message data; without both, nothing it returns is stored. The flags cannot
  limit what the plugin process itself does, so `network` in particular is
  only a declaration.
- Annotations are stored through parameterized SQL, only after the whole set is
  validated. Keys and values are never used as code, commands, URLs, or file
  paths.
- Response data is displayed, never interpreted. Before display it is
  pretty-printed, stripped of control and format characters (terminal escapes,
  bidi overrides, zero-width characters), and capped at 16 KiB and 200 lines.
  Displayed errors replace the plugin directory path with `plugins/`.

## Writing a test plugin

A plugin needs only a few lines in any language. This one echoes each request
back, which is handy for seeing exactly what TideMail sends. Save it as
`~/.config/tidemail/plugins/echo/run`, make it executable, and add a manifest
with `command = "run"` and `message_metadata = true`:

```sh
#!/bin/sh
read -r line
id=$(printf '%s' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
printf '{"api":1,"type":"response","request_id":"%s","ok":true,"data":{"request":%s}}\n' "$id" "$line"
```

To try annotations, add `annotations = true` to the manifest and have the
plugin print something like
`{"annotations":[{"key":"needs_reply","value":"true"}]}` as its `data`.

The package tests use the Go test binary itself as the plugin:
`internal/plugin/plugin_test.go` symlinks it into a temporary plugin directory,
and `TestMain` chooses a behavior from the name it was started as.
