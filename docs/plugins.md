# Plugins (experimental, developer notes)

> **Status:** experimental. TideMail loads plugins at startup, but a plugin
> only ever runs when you start it by hand, and nothing it returns changes your
> mail. The format below may change without notice until plugins ship.

The `internal/plugin` package discovers plugins on disk, validates their
manifests, and exchanges one JSON request/response with a plugin process.
TideMail uses it for one manual action: sending the current message's metadata
to a plugin and showing what comes back.

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
- Results and failures are also written to the in-app log (**Settings → Advanced →
  View Logs**). The log records status lines only, never metadata or response data,
  and is not written to disk.

Nothing in a response is treated as an instruction. TideMail never moves,
deletes, or marks mail, sends mail, opens URLs, runs commands, or changes
settings because of plugin output.

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
| `message.metadata` | one message's metadata (below) | anything; shown to the user, never acted on |

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
- `[permissions]` decides what TideMail sends. `message_metadata` is enforced:
  without it, a plugin never receives message data. The flags cannot limit
  what the plugin process itself does, so `network` in particular is only a
  declaration.
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

The package tests use the Go test binary itself as the plugin:
`internal/plugin/plugin_test.go` symlinks it into a temporary plugin directory,
and `TestMain` chooses a behavior from the name it was started as.
