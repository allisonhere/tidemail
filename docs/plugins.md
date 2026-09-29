# Plugins (experimental)

TideMail can run external plugins that classify mail from its metadata and
attach **annotations** such as `needs_reply=true` or `category=billing`.
TideMail stores the annotations, shows them as tags in message rows, and uses
them for views like **Needs You**. Plugins never change mail, and nothing a
plugin returns is treated as an instruction.

- A plugin is an external executable plus a `plugin.toml` manifest, installed
  in `$XDG_CONFIG_HOME/tidemail/plugins/<name>/` (or
  `~/.config/tidemail/plugins/<name>/` when `XDG_CONFIG_HOME` is unset).
- TideMail writes one JSON request to its stdin; it writes one JSON response to
  stdout. Any language works.
- Plugins receive header-level metadata only (sender, recipients, subject,
  date, flags), never message bodies, passwords, or tokens.
- Report plugins can ask TideMail read-only, allowlisted queries (message and
  thread lists, stored annotations, aggregate analytics). TideMail runs them
  itself; plugins never get database access.
- Plugins provide semantics; TideMail controls presentation. Tag styles and
  colors come from the user's settings and theme, never from a plugin.
- Plugins are not sandboxed by the operating system. Only install plugins you
  trust.

## For users

How to run plugins (**Run plugin**), read and clear annotations, turn on
automatic processing, change plugin settings, and pick tag styles and colors:
see the user guide, [guide.md › Plugins](guide.md#plugins-experimental).

## For plugin authors: Plugin API v1

The developer documentation is in [`docs/plugins/`](plugins/README.md):

| Page | Covers |
| --- | --- |
| [README](plugins/README.md) | overview and architecture |
| [getting-started](plugins/getting-started.md) | build your first plugin, step by step |
| [manifest](plugins/manifest.md) | `plugin.toml` fields and discovery |
| [protocol](plugins/protocol.md) | envelopes, methods, metadata, stdout/stderr |
| [permissions](plugins/permissions.md) | what each permission allows |
| [presentation](plugins/presentation.md) | explaining results to people |
| [annotations](plugins/annotations.md) | limits, replace-on-success, conventional keys, reclassification |
| [events](plugins/events.md) | `message.received` for new mail |
| [settings](plugins/settings.md) | bool/select/secret settings and `plugin.test` |
| [queries](plugins/queries.md) | `report.run` and read-only queries |
| [analytics](plugins/analytics.md) | aggregate statistics |
| [views](plugins/views.md) | structured report views drawn in the user's theme |
| [security](plugins/security.md) | trust model and runtime restrictions |
| [compatibility](plugins/compatibility.md) | what API v1 promises |

Small, tested example plugins are in
[`examples/plugins/example/`](../examples/plugins/example/) and
[`examples/plugins/analytics/`](../examples/plugins/analytics/) (a report).

## For TideMail contributors

The runtime is `internal/plugin` (discovery, manifest and protocol validation,
process handling, permissions, annotation parsing, automatic events, query
validation and the report round driver in `query.go` and `report.go`). Report
queries execute in `internal/pluginquery`, which reads header columns through
`internal/db` and shares conversation threading with the UI through
`internal/conversation`. The UI side is in `internal/ui` (`plugins.go`,
`plugin_bulk.go` for multi-message runs, `plugin_report.go`,
`plugin_annotations.go`, `tags.go` for tag rendering, `plugin_events.go`,
`plugin_settings.go`), and annotation storage is `internal/db`. See
[architecture.md](architecture.md#plugins). Tests in
`internal/plugin/docs_test.go` check the examples in `docs/plugins/` against
the code, so keep them in step when the protocol changes.
