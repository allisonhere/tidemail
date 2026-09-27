# Plugins (experimental, developer notes)

> **Status:** experimental groundwork. TideMail does not load or run plugins
> yet. Nothing in this document changes how TideMail handles mail. The format
> below may change without notice until plugins ship.

The `internal/plugin` package can discover plugins on disk, validate their
manifests, and exchange one JSON request/response with a plugin process. The
only method so far is `ping`.

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
- `[permissions]` is capability metadata. It records what a plugin asks for so
  later milestones can decide what to send it. TideMail cannot enforce it on
  the plugin process. The `network` flag, in particular, is only a
  declaration.

## Writing a test plugin

A `ping` plugin needs only a few lines in any language. The package tests use
the Go test binary itself as the plugin: `internal/plugin/plugin_test.go`
symlinks it into a temporary plugin directory, and `TestMain` chooses a
behavior from the name it was started as.
