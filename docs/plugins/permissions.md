# Permissions

Permissions live in the manifest's `[permissions]` table. All of them default
to `false`.

```toml
[permissions]
message_metadata = true
annotations = true
network = false
message_body = false
messages_query = false
threads_query = false
annotations_query = false
analytics_read = false
```

| Permission | What TideMail does with it | Enforced by TideMail? |
| --- | --- | --- |
| `message_metadata` | Allows `message.metadata` and `message.received`. Without it the plugin is never started for a message, is not offered in the Reclassify picker, and cannot receive events. | **Yes**, before the process starts, on every call. |
| `annotations` | Allows TideMail to store the `annotations` a metadata response returns. Without it the plugin still runs and its response is shown, but nothing is stored. | **Yes**, on every response. |
| `network` | A declaration that the plugin talks to online services. Shown in the plugin list. | **No.** TideMail cannot stop a process from using the network. |
| `message_body` | Reserved. No API v1 method sends message bodies, whatever this says. | Not applicable. |
| `messages_query` | Allows the `query.messages` report query: allowlisted header fields of messages in a scope, a bounded page at a time. | **Yes**, for every query, before it runs. |
| `threads_query` | Allows `query.threads`: conversation summaries (participants, counts, latest message, Needs You / Waiting on Them state). | **Yes**, for every query. |
| `annotations_query` | Allows `query.annotations`: stored plugin annotations on messages you name. With `analytics_read` it also allows `query.classification`. | **Yes**, for every query. |
| `analytics_read` | Allows the `analytics.*` queries: computed counts and statistics, not message records. | **Yes**, for every query. |

The four query permissions are read-only and independent: none implies
another, `analytics_read` never grants message records, and none of them lets
a plugin read bodies or change mail. They are used only by `report.run`
([queries.md](queries.md)).

> Query permissions allow TideMail to disclose selected structured data to a
> plugin. They do not grant direct database access.

## The trust model

Manifest permissions are two things at once:

- **Declarations** to the user: the plugin list shows them, and a plugin that
  sends data online (for example to an AI service) should declare
  `network = true` and explain in its settings help what it sends.
- **TideMail-enforced application boundaries**: TideMail decides what data to
  hand a plugin and what output to keep based on them, in its plugin runtime
  rather than in the UI, so no screen or code path can skip the check.

They are **not an operating-system sandbox**. A plugin is a program running
with the user's own privileges: it can read files the user can read and open
network connections, whatever its manifest says. Only install plugins you
trust. The runtime restrictions TideMail does apply are listed in
[security.md](security.md).

## Checks on every call

- The Reclassify picker lists only plugins with `message_metadata = true`.
- The runtime checks `message_metadata` again for **each message** of a
  multi-message run, and for every automatic event.
- Generic calls cannot be used to send message data: TideMail's internal
  call path refuses `message.metadata` and `message.received`, which only go
  through the permission-checked entry points.
- `message.received` additionally requires the manifest to declare the event
  and the user to have switched automatic processing on.
- `plugin.test` requires `capabilities = ["plugin.test"]`.
- `report.run` requires `capabilities = ["report.run"]`, and generic calls
  refuse it. Every query a report asks for is validated and checked against
  the permissions above before any of them runs; one refused query fails the
  whole round, so nothing is disclosed.
