# Manifest reference (`plugin.toml`)

Every plugin directory contains a `plugin.toml`. TideMail reads it at startup,
validates it, and skips the plugin (with a reason shown in **Plugins
(experimental)**) if anything is wrong.

## Where plugins live

```text
~/.config/tidemail/plugins/          ($XDG_CONFIG_HOME/tidemail/plugins/)
    example/
        plugin.toml
        tidemail-plugin-example
    smart/
        plugin.toml
        tidemail-plugin-smart
```

- One directory per plugin. The directory name is not the plugin ID; the
  manifest's `id` is.
- A missing `plugins/` directory means no plugins. It is not an error, and
  TideMail then shows no plugin commands at all.
- Directories are read in name order. Hidden entries (`.name`) and plain files
  are ignored. A symlinked plugin directory is followed.
- A broken plugin (missing or invalid manifest, missing or non-executable
  command) is skipped and reported; its siblings still load.
- If two plugins declare the same `id`, **both** are skipped and reported, so
  directory names never decide which code runs.
- Discovery happens once, at startup. Restart TideMail after installing,
  updating, or removing a plugin.

## A complete manifest

Every field TideMail accepts appears below. Only `id`, `name`, `api`, and
`command` are required.

<!-- drift:manifest -->
```toml
id = "example"                       # required
name = "Example Plugin"              # required
version = "0.1.0"                    # optional, shown in the plugin list
api = 1                              # required; the protocol version
command = "tidemail-plugin-example"  # required; relative to the plugin directory
events = ["message.received"]        # optional; automatic events
capabilities = ["plugin.test"]       # optional; extra protocol features

[permissions]                        # all default to false
message_metadata = true
annotations = true
network = false
message_body = false

[[settings]]                         # optional; zero or more
key = "strict"
label = "Strict matching"
type = "bool"
default = false
help = "Only tag subjects that start with Invoice."
```

## Fields

### `id` (required)

The stable identifier TideMail stores data under: annotations, settings, and
keychain secrets are all keyed by it. Changing it makes TideMail treat the
plugin as a new one.

- 1 to 64 characters: `a-z`, `0-9`, `_`, `-`, starting with a letter or digit
  (`^[a-z0-9][a-z0-9_-]{0,63}$`).
- It is not a display name; use `name` for that.

### `name` (required)

The display name, shown in the plugin list, the Reclassify picker, and
confirmations. It must not be blank.

### `version` (optional)

Your plugin's own version, shown in the plugin list. TideMail does not
interpret it.

### `api` (required)

The plugin protocol version the plugin speaks. This TideMail supports exactly
`api = 1`; any other value rejects the manifest. See
[compatibility.md](compatibility.md).

### `command` (required)

The executable to run, as a path **relative to the plugin directory**.

- Absolute paths and paths that leave the directory (`..`) are rejected.
- It is never looked up on `$PATH`: `command = "python3"` means
  `<plugin dir>/python3`, not the system Python. To use an interpreter, make
  your script executable with a `#!` line and name the script.
- It must be a regular file (a symlink to one is fine) with an executable bit.
- It runs directly, never through a shell, with no arguments, and with the
  plugin directory as its working directory.

### `events` (optional)

Automatic events the plugin can receive. API v1 has exactly one:

<!-- drift:events -->
```text
message.received
```

Any other name, or a name listed twice, rejects the manifest. Declaring an
event only makes the plugin *eligible*: the user must still switch automatic
processing on for it. See [events.md](events.md).

### `capabilities` (optional)

Optional protocol features. API v1 has one:

<!-- drift:capabilities -->
```text
plugin.test
```

`plugin.test` adds a **Test plugin configuration** row to the plugin's
settings form. See [settings.md](settings.md#testing-a-configuration).

### `[permissions]` (optional)

Four booleans, all `false` by default: `message_metadata`, `annotations`,
`network`, `message_body`. What each one allows (and what it cannot prevent)
is in [permissions.md](permissions.md).

### `[[settings]]` (optional)

User settings TideMail renders in a generic form: `bool`, `select`, or
`secret`. At most 32. See [settings.md](settings.md).

## Unknown fields

Unknown keys anywhere in the manifest reject it. A typo such as `[permisions]`
or `mesage_metadata` is reported instead of silently leaving a permission
off. There are no fields for UI, colors, keybindings, or commands; see
[compatibility.md](compatibility.md#not-supported).
