# Permissions

Permissions live in the manifest's `[permissions]` table. All four default to
`false`.

```toml
[permissions]
message_metadata = true
annotations = true
network = false
message_body = false
```

| Permission | What TideMail does with it | Enforced by TideMail? |
| --- | --- | --- |
| `message_metadata` | Allows `message.metadata` and `message.received`. Without it the plugin is never started for a message, is not offered in the Reclassify picker, and cannot receive events. | **Yes**, before the process starts, on every call. |
| `annotations` | Allows TideMail to store the `annotations` a metadata response returns. Without it the plugin still runs and its response is shown, but nothing is stored. | **Yes**, on every response. |
| `network` | A declaration that the plugin talks to online services. Shown in the plugin list. | **No.** TideMail cannot stop a process from using the network. |
| `message_body` | Reserved. No API v1 method sends message bodies, whatever this says. | Not applicable. |

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
