# Settings

A plugin can declare settings. TideMail draws them in a generic form, stores
them, and sends them back on every call. Plugins cannot supply their own UI.

Users open the form from **Settings → Advanced → Plugin settings** (select
the plugin and press `s`), or by pressing `s` on the plugin in **Plugins
(experimental)**. Changes save immediately.

## Types

<!-- drift:setting-types -->
```text
bool
select
secret
```

```toml
[[settings]]
key = "mode"                        # required
label = "Classification mode"       # required
type = "select"                     # required: bool, select, or secret
options = ["local", "hybrid"]       # select only: 1-16 options
default = "local"                   # optional; not allowed for secret
help = "Hybrid also asks an online model."  # optional

[[settings]]
key = "notify_only"
label = "Only tag, never summarize"
type = "bool"
default = true

[[settings]]
key = "api_key"
label = "API key"
type = "secret"
help = "Sent only to example.com, only in hybrid mode."
```

## Validation

The manifest is rejected when any setting breaks these rules:

- At most **32** settings, with unique keys.
- `key`: 1 to 32 characters of `a-z`, `0-9`, `_`, starting with a letter
  (`^[a-z][a-z0-9_]{0,31}$`).
- `label`: required, at most **48** characters, no control or format
  characters.
- `help`: optional, at most **240** characters, no control or format
  characters.
- `bool`: `default`, when given, is `true` or `false`; no `options`.
- `select`: 1 to **16** unique, non-empty `options` of at most **48**
  characters each; `default`, when given, is one of them.
- `secret`: no `default` and no `options`.
- Any other `type`, or any unknown field, is an error.

## Defaults

- A `bool` without a default is `false`.
- A `select` without a default is its first option.
- A stored value of the wrong type, or a select value that is no longer an
  option (after a plugin update, say), silently falls back to the default.

## How values reach the plugin

**bool and select** values are in the request's `settings` object, on every
call, including `ping` and `plugin.test`:

```json
{"api": 1, "type": "request", "request_id": "…", "method": "message.received",
 "settings": {"mode": "hybrid", "notify_only": true}, "data": {}}
```

Only keys the plugin itself declares appear. Values for keys it no longer
declares are dropped.

**Secrets are never in the request.** Each secret that is set arrives as an
environment variable of that plugin's process only:

```text
TIDEMAIL_SECRET_<KEY in upper case>        e.g. TIDEMAIL_SECRET_API_KEY
```

An unset secret is simply absent; decide what to do without it (fall back to
local rules, or answer `plugin.test` with a helpful message).

## Storage

- `bool` and `select` values are stored in TideMail's `config.toml` under the
  plugin's ID. They are kept if the plugin is removed and used again if a
  plugin with the same ID returns.
- Secrets are stored in the system keychain under the plugin's ID and key.
  There is no plaintext fallback: without a working keychain a secret is not
  saved. The UI only learns whether a secret is set, never its value, and
  shows it as `************`.

## Secret rules for plugin authors

- A plugin receives **only its own** settings and secrets, never another
  plugin's, and never TideMail's account passwords, OAuth tokens, or API
  keys.
- Never log a secret: not to stderr, not to files. If a secret's value does
  appear in your stdout or stderr, TideMail masks it as `********` before
  parsing or showing anything, but do not rely on that.
- Never echo a secret back in response data.

## Testing a configuration

Declare the capability:

```toml
capabilities = ["plugin.test"]
```

The settings form then offers **Test plugin configuration**, which sends
`plugin.test` (with settings and secrets as usual, no `data`). Answer with a
normal response whose data explains the outcome:

```json
{"api": 1, "type": "response", "request_id": "…", "ok": true,
 "data": {"ok": false, "message": "API key rejected (401)"}}
```

TideMail shows `message` on the status line and the whole `data` in a result
window. Nothing else happens; the test is informational. A good test checks
exactly what a real call would need (a key is present, a service answers)
without changing anything.

## Example: TideMail Smart

TideMail Smart, a separate plugin, uses all three types: a `select` for its
mode (local rules, or hybrid with an online model), `bool` switches for which
signals to emit, and a `secret` for its API key. Its help texts say what is
sent where. Nothing about settings is specific to it.
