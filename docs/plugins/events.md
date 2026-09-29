# Automatic events

A plugin can process new mail as it arrives. API v1 has one automatic event:

```text
message.received
```

## Two opt-ins

1. **The plugin declares it**, in the manifest, together with
   `message_metadata`:

   ```toml
   events = ["message.received"]

   [permissions]
   message_metadata = true
   annotations = true
   ```

2. **The user enables it** for that plugin: select it in **Plugins
   (experimental)** and press `a`, then confirm. Every plugin starts
   **disabled**, and declaring the event never turns it on.

## Protocol contract

This part is the API and does not change within API v1:

- The method is `message.received`, and `data` is exactly the
  [`message.metadata` payload](protocol.md#messagemetadata): header-level
  fields only, never the body.
- Your response is handled exactly like a `message.metadata` response: same
  validation, same permission check, same
  [replace-on-success](annotations.md#replace-on-success) storage.
- Settings and secrets arrive as for any other call.
- Delivery is **at most once, best effort**. An event can be dropped (see
  below) and is never replayed. The user can always rerun your plugin by hand
  with **Reclassify**, so do not rely on seeing every message.
- A message can also reach you by `message.metadata`, before or after
  `message.received`. Classify the same way whichever method arrives.

## Which mail counts as new

Only messages a sync finds that TideMail did not already have, and that are
unread. These never produce events:

- opening a folder or starting TideMail (cached mail is reloaded, not new);
- loading older mail from the server;
- the first sync of a folder, or a sync that rebuilds the cache after the
  server reset the folder (these fetch history, not new mail);
- mail the user's filter rules move or delete on arrival.

Mail that arrived while TideMail was closed counts as new on the next sync.

## Current runtime limits

These are how this version of TideMail schedules events. They protect the
user's machine and may change without a new API version:

| Limit | Current value |
| --- | --- |
| Queued messages per plugin | 50; further events are **dropped** and counted |
| Processes per plugin | 1 at a time |
| Plugin processes at once, all plugins and all message calls | 3 |
| Automatic calls per plugin | 30 per minute; extra work waits in the queue |
| Timeout per call | 5 seconds |
| Consecutive failures before pausing | 3 |

- **Dedupe:** a message already queued or running for a plugin is not queued
  again.
- **Bounded concurrency:** manual Reclassify runs and automatic events share
  the same per-plugin and global process limits.
- **Pause:** after 3 consecutive failures (timeout, crash, invalid response,
  rejected annotations, failed save) the plugin is paused: its queue is
  emptied, new events are dropped and counted, and the user sees why. A
  success resets the count. The user resumes with `r` in the plugin list;
  dropped messages are not replayed.
- **Manual runs are independent:** Reclassify works while automatic mode is
  off, on, or paused, and never changes the failure count, pause state, or
  dropped count.
- Quitting TideMail cancels queued work and kills running plugin processes.
