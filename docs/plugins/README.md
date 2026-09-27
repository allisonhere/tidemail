# TideMail Plugin API v1

> **Status:** experimental. Plugin API v1 is versioned independently of
> TideMail releases. Breaking protocol changes require a new API version (see
> [compatibility.md](compatibility.md)).

A TideMail plugin is an external executable.

- TideMail starts it directly: no shell, no arguments.
- TideMail writes **one JSON request** to its stdin.
- The plugin writes **one JSON response** to its stdout and exits.

That is the whole integration. There is no Go plugin ABI and no shared
library, so any language that can read stdin and write stdout can implement a
plugin: Go, Rust, Python, Node, a shell script.

A plugin can:

- classify a message from its **metadata** (sender, recipients, subject, date,
  flags; never the body), by hand or automatically for new mail;
- return **annotations**, short `key=value` facts such as `needs_reply=true` or
  `category=billing`, which TideMail stores, shows as tags, and uses for views
  like Needs You;
- declare **settings** (bool, select, secret) that TideMail shows in a generic
  form and hands back on every call;
- run as a **report**: ask TideMail read-only, allowlisted **queries** (message
  and thread lists, stored annotations, aggregate **analytics** such as volume
  and category counts) and return a report the user reads in TideMail.

A plugin cannot change mail, send mail, trigger TideMail actions, add UI,
choose colors, access TideMail's database directly, or read message bodies.
It sees another plugin's annotations only through `query.annotations`, with
permission. Anything a plugin returns other than annotations is only
displayed.

> **Plugins provide semantics. TideMail controls presentation.**
>
> Return `category=github` or `urgency=high`. Never `color=purple`,
> `style=pill`, or `foreground=#ff0000`: TideMail ignores such keys, and
> themes and user settings decide how tags look.

## Start here

1. [getting-started.md](getting-started.md): build your first plugin in a few
   minutes, from an empty directory to a tag in TideMail's message list.
2. [developer-tools.md](developer-tools.md): use TideMail's headless
   `plugin scaffold`, `plugin validate`, and `plugin test` commands.
3. The reference:

| Page | Covers |
| --- | --- |
| [manifest.md](manifest.md) | `plugin.toml`: every field, where plugins live, discovery rules |
| [protocol.md](protocol.md) | request/response envelopes, methods, metadata fields, errors, stdout/stderr |
| [permissions.md](permissions.md) | what each permission allows, and what it does not |
| [annotations.md](annotations.md) | returning annotations, limits, replace-on-success, conventional keys, reclassification |
| [events.md](events.md) | `message.received`: automatic processing of new mail |
| [settings.md](settings.md) | bool/select/secret settings, storage, secrets, `plugin.test` |
| [queries.md](queries.md) | `report.run`: rounds, read-only queries, fields, scopes, pagination, limits |
| [analytics.md](analytics.md) | aggregate statistics: volume, categories, attention, response times, contacts |
| [security.md](security.md) | the trust model and every runtime restriction |
| [compatibility.md](compatibility.md) | what API v1 promises, and what may change |

Complete, tested examples live in
[`examples/plugins/example/`](../../examples/plugins/example/) (message
metadata and annotations) and
[`examples/plugins/analytics/`](../../examples/plugins/analytics/) (a report
built from aggregate analytics).

## How a call flows

Manual run (**Run plugin** in the command palette):

```text
TideMail
   │
   │ safe metadata (JSON request on stdin)
   ▼
plugin executable
   │
   │ JSON response (stdout)
   ▼
protocol validation        api, type, request_id, ok/error, one JSON value, size
   │
   ▼
permission / annotation    message_metadata + annotations declared?
validation                 whole set valid? (all or nothing)
   │
   ▼
annotation storage         replaces this plugin's set for this message
   │
   ▼
TideMail UI                tags in message rows, Needs You, Message annotations
```

Annotations are recommendations. TideMail may apply a local user correction
for the conventional semantic fields; re-running the plugin does not clear
that correction, and the correction is never sent back to the plugin or to a
remote classifier with message metadata. Report plugins can read the current
effective value, marked as the user's, only through `query.classification`
([queries.md](queries.md#queryclassification)).

Automatic run (new mail, when the plugin declares `message.received` and the
user switches it on):

```text
mail sync
   → event queue          per plugin, bounded, deduplicated
   → plugin process       same request, one process per plugin at a time
   → validation           same checks as a manual run
   → annotation storage   same replace-on-success rule
   → live UI update       tags and Needs You refresh as results arrive
```

## Where do plugins live?

In `~/.config/tidemail/plugins/` (more precisely
`$XDG_CONFIG_HOME/tidemail/plugins/`), one directory per plugin, each with a
`plugin.toml` and the executable it names. TideMail looks for plugins at
startup; restart it after installing or updating one. See
[manifest.md](manifest.md#where-plugins-live).

## What users see

Users find plugins in the command palette (`:`): **Plugins (experimental)**,
**Run plugin…**, and **Message annotations**. Reports run with `enter` on the
plugin in **Plugins (experimental)**. The user guide describes them:
[guide.md › Plugins](../guide.md#plugins-experimental).
