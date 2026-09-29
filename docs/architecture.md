# TideMail architecture

A map of the packages and the main flows between them. For user-facing
behaviour see [`guide.md`](guide.md); for contribution rules see
[`../CONTRIBUTING.md`](../CONTRIBUTING.md) and [`../AGENTS.md`](../AGENTS.md).

## Packages

| Package | Responsibility |
| --- | --- |
| `.` (`main.go`) | Startup options, config and database loading, running the Bubble Tea program, and post-exit work (flushing pending actions and sends, releasing terminal images, in-app update restart). |
| `internal/ui` | The whole TUI: the `Model`, key handling, panes, overlays, compose, settings, account and contact managers, sync orchestration, and message rendering. |
| `internal/db` | SQLite storage for accounts, messages, attachments, drafts, outbox, contacts, filter rules, and experimental plugin annotations, plus folder reconciliation. |
| `internal/imap` | IMAP client, connection pool, IDLE push, message parsing, and XOAUTH2. |
| `internal/smtp` | Message assembly (including attachments) and SMTP/STARTTLS sending with password or XOAUTH2 auth. |
| `mailcore` | Small public Go API over TideMail's IMAP and SMTP internals for the separate Android repository's Go bridge. It exposes connection settings, mailbox/message projections, and the mail operations Android uses; credential storage and UI state stay with Android. |
| `internal/auth` | Google and Microsoft OAuth flows and token refresh. |
| `internal/config` | Config file load/save, account IDs, and secret storage in the system keyring (`TIDEMAIL_DISABLE_KEYRING` opts out). |
| `internal/filter` | Deterministic mail rules. |
| `internal/ai` | Summary providers: Claude, OpenAI, Gemini, and Ollama. |
| `internal/plugin` | Experimental plugins: discovers plugins under the config dir, validates `plugin.toml`, runs one JSON request/response per process, enforces the `message_metadata`, `annotations`, and read-only query permissions, validates annotations before handing them to an `AnnotationStore`, and drives report rounds (`report.go`), validating every query (`query.go`) before a `QueryExecutor` runs it. It does not import `internal/db`; `internal/ui/plugins.go` adapts the database and holds the manual UI hooks and the annotation cache, and `internal/ui/tags.go` draws annotation tags. See [`plugins.md`](plugins.md) and the Plugin API v1 docs in [`plugins/`](plugins/README.md). |
| `internal/pluginquery` | Executes plugin report queries against the cache: header columns only, fixed allowlists, keyset pagination, sanitized strings, and aggregate analytics. Also loads synthetic fixture mailboxes for `tidemail plugin test`. |
| `internal/conversation` | Pure conversation model shared by the UI and plugin queries: Message-ID threading, participant parsing, and Waiting on Them. |
| `internal/richmail` | Email image model: source parsing, MIME-part resolution, decode limits, remote-fetch SSRF policy, and cell layout. |
| `internal/termimage` | Terminal graphics: protocol detection, cell geometry, and the Kitty Unicode-placeholder backend. |
| `internal/clipboard` | System clipboard access. |
| `internal/omarchy` | Reads the active Omarchy desktop palette for the matching theme. |
| `internal/update` | Release checks, verified downloads, and system-package detection for in-app updates. |
| `cmd/editortest`, `cmd/kittytest` | Manual test harnesses for the editor and Kitty graphics. |

Shared UI primitives come from the separate `github.com/allisonhere/tideui`
module. Email-specific rendering stays in TideMail.

## Key flows

### Model and updates

`ui.Model` is a Bubble Tea model passed by value. Long-running work (sync,
sends, fetches, image uploads) runs in `tea.Cmd`s and reports back as messages
defined mostly in `internal/ui/msgs.go`. `Model.Update` wraps the main
`update` switch and attaches any queued terminal-image upload command.

### Sync

`internal/ui/sync.go` drives mailbox refresh through `internal/imap` and writes
results to `internal/db`. `internal/ui/idle.go` and `internal/imap/idle.go`
handle push via IMAP IDLE.

### Sending

Compose (`internal/ui/compose.go`) hands messages to the outbox
(`internal/ui/outbox.go`, `internal/db/outbox.go`), which supports undo send
and scheduled send before `internal/smtp` delivers them.

### Android bridge

The separate `tidemail-android` repository builds a gomobile AAR from its Go
bridge. That bridge imports `mailcore` from a pinned TideMail revision; it does
not import `internal/*` or ship Android app code in this repository.

### Rendering a message

`internal/ui/message_render.go` picks a renderer: Reddit digest, HTML, or plain
text. For HTML:

1. `normalizeHTMLForRendering` (`html_normalize.go`) removes hidden content,
   tracking pixels, and spacers; converts quoted replies to blockquotes; marks
   newsletter content groups (`email_groups.go`); and flattens layout tables
   while keeping data tables and receipts (`email_tables.go`).
2. When graphics are available, `richmail` builds an image manifest and
   `rich_images.go` plans each image: resolve, decode, and lay out.
3. `html-to-markdown` converts the DOM with TideMail's rules in `format.go`,
   then glamour renders it (`render_markdown.go`).
4. Private-use sentinels are expanded after rendering: image markers become
   Kitty placeholder rows or text placeholders, and group markers become rules.

Rendered bodies and viewport content are cached (`render_cache.go`); cache keys
include the image store's generation.

### Plugins

Plugins are external executables speaking Plugin API v1 (documented for plugin
authors in [`plugins/`](plugins/README.md)). Every path from plugin output to
storage runs through `plugin.Manager.runMetadata`:

```text
tea.Cmd (manual Reclassify)        EventManager worker (message.received)
            │                                   │
            └──────────► runMetadata ◄──────────┘
                            │ permission check (message_metadata)
                            │ acquire: 1 process per plugin, 3 overall
                            │ invoke: exec, stdin/stdout JSON, timeout
                            │ decodeResponse: envelope validation
                            │ ParseAnnotations: all-or-nothing
                            ▼
              AnnotationStore.ReplaceAnnotations (one transaction)
                            │
                            ▼
      Update patches the annotation cache → tags, Needs You refresh
```

Reports (`report.run`, `internal/ui/plugin_report.go`) are the only path from
a plugin to more than one message, and they never write:

```text
enter in the plugin list → tea.Cmd → Manager.Report / RunReport
  round n: acquire slot → invoke report.run (state + results) → decode
           ├─ {report}  → formatPluginData → plugin result overlay
           ├─ {view}    → ParseView (semantic blocks only) → pluginViewLines
           │              (theme + tag colors, plugin_view.go) → overlay
           └─ {queries} → ParseQuery + QueryPermission for every query
                          (fail closed: a bad query runs nothing)
                        → pluginquery.Executor: db.ListHeaders (header
                          columns only), conversation.BuildThreads,
                          attention.FromDB, analytics → sanitized JSON
                        → round n+1   (≤ 8 rounds, 8 queries, 30 s)
```

### Effective classification

Plugin annotations and local user corrections are separate stores. The
canonical interpretation is in `internal/attention/effective.go`; it resolves
the four supported semantic fields with user overrides taking precedence.
Needs You's SQL applies the same precedence for filtering and ranking, while
the UI uses the effective result for tags and explanations. Corrections are
keyed by the cached message row ID, cascade when that message is deleted, and
remain when plugin annotations are refreshed, cleared, or uninstalled. They
are local-only and are never sent to plugins with message metadata or to
remote classification services. Report plugins with `annotations_query` and
`analytics_read` can read the current effective value, with `source: user`,
through `query.classification`; `analytics.categories` counts effective
categories.

### Reclassify

`internal/ui/plugin_bulk.go` turns one palette action into a run over the
current message, the selection, or the current view's loaded rows
(`viewTargets`: `filteredMessages`, so virtual views use what they show, and
nothing is fetched from the server). The plugin is chosen once. One message
goes through the single-run path; several go through a queue that runs one
`runPluginCmd` at a time on a per-run context, so the manager's slot limits
apply and Cancel or quit kills the message in flight. `runPluginCmd` loads the
message's annotations before the call and after a successful store, and
`annotationSetChanged` compares the plugin's normalized `key=value` sets
(order, case, and confidence ignored) to count changed and unchanged
messages. Whole-view runs always confirm; selections confirm at 10. Manual
runs never touch `EventManager` state.

### Annotation tags

`internal/ui/tags.go` is the only tag renderer. `annotationTags` derives up to
three semantic tags (reply, urgent, important, category) from the cache using
the same value lists as Needs You; `layoutTags` fits them into at most half of
the subject's room, stepping from wide labels to glyphs to unpadded text and
then dropping tags; `renderMessageRow` draws colored tags as self-styled
segments so the row background survives. `display.tag_ends` picks the pill
ends (square padding, rounded Nerd Font caps drawn in the pill color, or
none); layout counts their width. Colors resolve per semantic key
(`reply`, `urgent`, `important`, `category`, `category.<name>`): the user's
`display.tag_colors` override, then the theme's value (`themeTagPalettes`, or
one derived from the theme's colors), then TideMail's fallback; invalid values
are skipped. No plugin text is ever used as a style.

### Plugin annotations

A manual plugin run (`internal/ui/plugins.go`) calls
`plugin.Manager.MessageMetadata` inside a `tea.Cmd`. The manager checks
permissions, validates the whole annotation set, and only then asks the
database adapter to replace the plugin's annotations on the message in one
transaction. The command reloads that one message's annotations and returns
them to `Update`, which patches the model's cache. Message list loads batch
annotations for all loaded messages in the same command, so rows render tags
from the cache and never query SQLite.

Cleanup (`internal/ui/plugin_cleanup.go`) follows the same pattern. The plugin
list and annotations overlay hold a typed pending action (one plugin on one
message, all plugins on one message, or one plugin everywhere) behind a single
confirmation overlay. The delete runs in a `tea.Cmd` that also reloads the
per-plugin counts (one `GROUP BY` query), and `Update` patches only the
affected cache entries. Counts are loaded when the plugin list opens and after
annotation writes, never while rendering.

### Plugin events

`plugin.EventManager` delivers `message.received` automatically. It starts one
worker per plugin that declares the event (so one process per plugin), shares a
semaphore that caps processes globally, and keeps bounded, de-duplicated
per-plugin queues with a sliding-window rate limit and a pause after repeated
failures. Each run goes through `Manager.messageEvent`, which shares
`runMetadata` (permissions, parsing, validation, storage) with manual runs.
Finished runs arrive on a channel that `internal/ui/plugin_events.go` reads
with a re-armed `tea.Cmd`, so the model only changes inside `Update`. The UI
enqueues work from `MailboxSyncedMsg.NewMessages`, which sync fills with
stored row IDs, and skips syncs marked `Cold` (first sync or cache rebuild).
Rendering reads a cached status snapshot.

### Needs You

`internal/db/attention.go` holds the qualification rules (conventional
annotation values shared with the UI's tags), `ListNeedsYou` and
`CountNeedsYou` (one query each, `EXISTS` per signal, ranked by an internal
attention score), and dismissals in `message_attention_overrides`.
`internal/ui/needs_you.go` adds the sidebar entry, loads the view like Unified
Inbox (annotations batched in the same command), caches the count, and
refreshes both after syncs, annotation writes and cleanups, and dismissals.
Qualification never refers to a specific plugin.

### Waiting on Them

`internal/ui/waiting.go` computes the view in a background command:
`db.ListWaitingCandidates` loads header-only rows from every folder except
Trash, Junk, and Drafts; the existing `buildMessageThreads` groups them by
Message-ID, In-Reply-To, and References (no subject matching); and a thread
qualifies when its newest meaningful message (not a draft, not from a robot) is
from one of the user's configured addresses and went to someone else. Refreshes
are coalesced (one computation at a time, one queued) and run after every sync,
at startup and account changes, after archive/delete/move, and after Stop or
Resume; a successful send triggers a sync of that account's Sent folder, so
the stored copy starts the wait. "Stop waiting" is stored in
`waiting_dismissals` keyed by the Message-ID of the message that started the
current wait (`db.WaitingKey`), which scopes it to one waiting cycle. It uses
no annotations or plugins.

### Snooze

`internal/db/snooze.go` stores snoozes (`snoozes`: target type, target key,
absolute wake time). Message snoozes are keyed by `db.MessageKey` (normalized
Message-ID, else row ID, so every folder copy shares one key and moves keep
it); waiting-conversation snoozes are keyed by the waiting cycle's key, so a
reply or a new message of the user's ends them (Waiting's refresh deletes
those). A snooze is active while its row exists: Needs You's query excludes
snoozed message keys, and Waiting's computation skips snoozed cycles.
`internal/ui/snooze_timer.go` keeps one cancellable wait for the nearest wake
time; when it fires, or at startup, due rows are deleted, the cache reloads,
the attention views re-evaluate, and the next wait is scheduled. A clock seam
lets tests control time. Threaded rows in the virtual views keep the views'
own order (`keepListOrder`).

### Plugin settings

Manifests may declare `[[settings]]` (bool, select, secret) and
`capabilities` (`plugin.test`), validated in `internal/plugin/settings.go`.
`internal/ui/plugin_settings.go` renders a generic form, stores non-secret
values in `config.toml` (`[plugins.<id>.settings]`) and secrets in the keychain
(`config.StorePluginSecret`, keyed by plugin ID and setting key). The manager
reads both through a `plugin.SettingsSource`: each request carries the
plugin's own resolved settings, and its secrets are set only in that plugin's
process environment and masked out of its output. Plugins such as TideMail
Smart, which may call external services, live outside this repository.

### Plugin runtime observability

`internal/plugin/events.go` keeps automatic-event state in memory: enabled,
queued, running, dropped, consecutive failures, paused, last error, and the
last run/success/failure timestamps. `internal/ui/plugin_events.go` copies a
locked snapshot into the Bubble Tea model and renders only sanitized plugin ID
and error text. No message metadata, credentials, or plugin environment is
included in this status. Manual runs remain independent of automatic-event
failure counts and pause state.

### Terminal images

`rich_images.go` owns the image store. Moving the cursor only records which
images are visible. Encoding and upload happen in a debounced background
command, and uploaded images stay resident in the terminal under an LRU budget.
Cell geometry is re-read on every resize. Remote images require per-message
consent (`i`) and are fetched through `richmail`'s SSRF-checked client.
