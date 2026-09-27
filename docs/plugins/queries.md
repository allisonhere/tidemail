# Reports and read-only queries

A **report** is how a plugin reads more than one message: mail volume,
category breakdowns, conversation lists, contact statistics. The plugin asks
TideMail structured, read-only **queries**; TideMail validates them, runs them
against its own cache, and hands back sanitized results.

> Query permissions allow TideMail to disclose selected structured data to a
> plugin. They do not grant direct database access.

A plugin never gets a database path, a database handle, SQL, message bodies,
HTML, raw headers, attachments, or any way to change mail. Every method,
field, scope, filter, and limit is on a fixed allowlist.

For the statistics queries (`analytics.*`) see [analytics.md](analytics.md).
A complete, tested example is
[`examples/plugins/analytics/`](../../examples/plugins/analytics/).

## Opting in

```toml
capabilities = ["report.run"]

[permissions]
analytics_read = true      # only what you need; see the table below
```

`report.run` makes the plugin runnable as a report: in TideMail, open
**Plugins (experimental)** from the command palette, select the plugin, and
press `enter`. The final report appears in the plugin result window.

The capability grants no data by itself. Each query needs its permission:

| Query | Permission |
| --- | --- |
| `query.messages` | `messages_query` |
| `query.threads` | `threads_query` |
| `query.annotations` | `annotations_query` |
| `query.classification` | `annotations_query` **and** `analytics_read` |
| `analytics.*` | `analytics_read` |

No permission implies another: `analytics_read` never returns message
records, and `messages_query` never returns annotations.

## How a report runs: rounds

The process model does not change: every call is still one process, one JSON
request on stdin, one JSON response on stdout, exit. A report is a short
series of such calls, called **rounds**.

```text
round 1   TideMail → report.run {round: 1, context}
          plugin   → {queries: {…}, state: …}
          TideMail validates and runs the queries
round 2   TideMail → report.run {round: 2, context, state, results: {…}}
          plugin   → {queries: {…}, state: …}     ask again (next page, follow-up)
             … or  → {report: …}                  done
```

Each round is a fresh process with the usual 5 second timeout, trimmed
environment, and output limits ([security.md](security.md)). Nothing is
bidirectional and nothing stays running. Carry anything you need between
rounds in `state`: TideMail echoes it back verbatim and never reads it.

### The request

```json
{
  "api": 1,
  "type": "request",
  "request_id": "5c1e0b9a6f2d7e3a4b8c9d01",
  "method": "report.run",
  "data": {
    "round": 2,
    "context": {
      "account_name": "Work",
      "mailbox_name": "INBOX",
      "now": "2026-09-27T12:00:00-05:00",
      "timezone": "America/Chicago"
    },
    "state": {"page": 1},
    "results": {
      "volume": {"group_by": "day", "received": 428, "sent": 73, "buckets": []}
    }
  }
}
```

| Field | Notes |
| --- | --- |
| `round` | 1 for the first call. |
| `context` | Where the report was started: the folder or account selected in TideMail's sidebar (either may be absent), the report's fixed clock `now`, and the local `timezone`. The same in every round. |
| `state` | Whatever your previous response put in `state`; absent in round 1. |
| `results` | Your previous round's queries' results, under the names you gave them; absent in round 1. |

### The response

Either ask:

```json
{"queries": {"volume": {"method": "analytics.volume", "range": "30d"}}, "state": {"page": 1}}
```

or finish:

```json
{"report": "Last 30 days: 428 received, 73 sent"}
```

- Exactly one of `queries` and `report`; neither, or both, fails the report.
- Query names are yours: 1 to 32 characters of `a-z`, `0-9`, `_`, `.`, `-`.
- `report` is display-only. A string is shown line by line as is; an object
  or array is shown as a readable key/value tree. TideMail strips control and
  escape characters: **plugins cannot draw terminal UI or choose colors**.

### Limits

<!-- drift:query-limits -->
```text
default_limit = 100
max_limit = 500
max_message_ids = 500
max_contacts = 50
max_rounds = 8
max_queries = 8
max_state_bytes = 65536
max_results_bytes = 4194304
report_timeout_s = 30
max_volume_buckets = 2000
```

- At most **8 rounds** and **8 queries per round**; a report still asking for
  queries in round 8 fails.
- `state` at most **64 KiB**; one round's results at most **4 MiB** of JSON.
- The whole report must finish within **30 seconds**.
- Invalid or unpermitted queries fail the whole round **before any of them
  runs**, with an error naming the query: fail closed, nothing disclosed.

## Query methods

<!-- drift:query-methods -->
```text
query.messages
query.threads
query.annotations
query.classification
analytics.volume
analytics.categories
analytics.attention
analytics.response_times
analytics.contacts
```

Every query is an object with `method` and that method's parameters. A
parameter the method does not take, an unknown field, or an unknown value is
an `invalid_query` error: typos never silently widen a query.

### `query.messages`

```json
{
  "method": "query.messages",
  "scope": "inbox",
  "fields": ["id", "sender", "subject", "date", "read"],
  "limit": 100,
  "sort": "date_desc",
  "cursor": "",
  "filters": {"read": false, "date_from": "2026-09-01T00:00:00Z"}
}
```

Result:

```json
{
  "messages": [
    {"id": 1234, "sender": "Ann <ann@example.com>", "subject": "Invoice", "date": "2026-09-26T10:00:00-05:00", "read": false}
  ],
  "next_cursor": "eyJkIjoxNzkwNTQy…"
}
```

**`fields`** (required) lists exactly what you want back; nothing else is
returned. The allowlist is the metadata `message.metadata` already sends,
never bodies:

<!-- drift:message-fields -->
```text
id
message_id
sender
recipients
cc
reply_to
subject
date
read
starred
has_attachment
account_name
mailbox_name
flags
```

`sender`, `recipients`, `cc`, and `reply_to` are header display strings
(`Name <addr>`, comma-separated). `date` is RFC 3339 in the report's timezone.
`id` is TideMail's local row number: use it with `query.annotations` and
`query.classification`, but it is not stable across cache resets.

**`scope`** (required) selects folders:

<!-- drift:scopes -->
```text
all_cached
current_account
current_mailbox
inbox
sent
```

| Scope | Covers |
| --- | --- |
| `inbox` | every account's inbox |
| `sent` | every Sent folder |
| `all_cached` | every cached folder, including Trash and Junk |
| `current_account` | the account selected when the report started |
| `current_mailbox` | the folder selected when the report started |

The `current_*` scopes fail with `scope_unavailable` when the report was not
started from an account or folder. A message copied into several folders is
listed once per copy.

**`filters`** (optional): `read`, `starred`, `has_attachment` (booleans),
`account`, `mailbox` (names, case-insensitive), `date_from`, `date_to`
(RFC 3339; `[date_from, date_to)`). There are no subject or sender filters
and no expressions.

**`sort`**: `date_desc` (default) or `date_asc`. **`limit`**: 1 to 500,
default 100.

### Pagination

A page with more after it carries `next_cursor`. Pass it back as `cursor` in
an otherwise identical query (usually in the next round, remembering where
you are in `state`); the last page has no `next_cursor`.

Cursors are opaque keyset positions, not offsets: messages arriving between
rounds do not shift pages. A cursor is bound to its query's scope, filters,
sort, and context; reusing it with a different query fails with
`invalid_cursor`.

### `query.threads`

```json
{"method": "query.threads", "scope": "all_cached", "fields": ["thread_id", "participants", "message_count", "latest_date", "waiting_on_them"], "limit": 50}
```

Result: `{"threads": [...], "next_cursor": "…"}`, newest conversation first.

<!-- drift:thread-fields -->
```text
thread_id
participants
message_count
latest_sender
latest_subject
latest_date
latest_from_me
needs_you
waiting_on_them
```

- Conversations come from TideMail's own threading (Message-ID, In-Reply-To,
  References), the same as the message list, over the messages in `scope`:
  with `inbox`, your own replies in Sent are not part of the thread; use
  `all_cached` for whole conversations. Unrelated messages with the same
  subject stay separate.
- Copies of one message in several folders count once.
- `thread_id` is an opaque hash for matching threads within one report. It
  can change when the conversation's cached messages change, so do not store
  it as a permanent key.
- `participants`: `[{"name": "Ann", "address": "ann@example.com"}]`, at most
  50.
- `needs_you` and `waiting_on_them` are TideMail's current local state, the
  same as its Needs You and Waiting on Them views. No plugin or online
  service is asked.
- Filters: `account`, `mailbox`, `date_from`, `date_to`. `limit` and
  `cursor` work as for messages; the order is always newest first.

### `query.annotations`

Stored **plugin** annotations: what plugins said, including other plugins.

```json
{"method": "query.annotations", "message_ids": [1234, 1235], "plugin_ids": ["smart"], "keys": ["category"]}
```

```json
{"annotations": [{"message_id": 1234, "plugin_id": "smart", "key": "category", "value": "billing", "confidence": 0.9}]}
```

`message_ids` (1 to 500) is required; `plugin_ids` and `keys` (at most 32
each) narrow the result. Unknown IDs are skipped. User corrections never
appear here.

### `query.classification`

TideMail's **effective** classification: plugin annotations with the user's
corrections applied, which is what TideMail itself shows and acts on. Each
field says where it came from.

```json
{"method": "query.classification", "message_ids": [1234]}
```

```json
{"classifications": [{
  "message_id": 1234,
  "needs_reply": {"value": true, "source": "plugin"},
  "urgent": {"value": false, "source": "none"},
  "important": {"value": false, "source": "none"},
  "category": {"value": "personal", "source": "user"}
}]}
```

`source` is `plugin`, `user` (a correction the user made), or `none`. Only
the current state is available, never a history of corrections. Because it
reveals the user's own decisions, it needs both `annotations_query` and
`analytics_read`.

## Errors

A failed query ends the report, and TideMail shows `query "name": code:
message` on its status line.

| Code | Meaning |
| --- | --- |
| `invalid_query` | Unknown method, parameter, field, scope, filter, or value, or a limit out of range. |
| `permission_denied` | The manifest does not declare the query's permission. |
| `scope_unavailable` | `current_account` or `current_mailbox` without a selected account or folder. |
| `invalid_cursor` | The cursor is not from an earlier page of this same query. |
| `query_failed` | TideMail could not read its cache. |

## Privacy boundaries

- Only header metadata ever leaves TideMail: never bodies, HTML, raw headers,
  summaries, attachments, OAuth tokens, passwords, API keys, file paths, or
  other plugins' settings or secrets.
- Strings are stripped of control, format (bidi, zero-width), and separator
  characters before they reach a plugin.
- Prefer `analytics.*`: they return counts, not records, and need only
  `analytics_read`.
- Everything is read-only. No query can mark, star, move, delete, archive,
  send, edit, snooze, or correct anything.

## Testing a report

`tidemail plugin test` runs a full report for plugins that declare
`report.run`, against a synthetic mailbox in a throwaway database: never your
real mail. Use `--report-fixture FILE` to supply your own mailbox. See
[developer-tools.md](developer-tools.md#reports).
