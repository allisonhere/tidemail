# Compatibility

Plugin API v1 is versioned independently of TideMail releases. Breaking
protocol changes require a new API version.

## What `api = 1` means

The same number appears in three places, and all three must be `1`:

- the manifest's `api` field: the protocol the plugin speaks;
- every request's `api`: the protocol TideMail is using;
- every response's `api`: the protocol the plugin answered in.

A TideMail build supports one API version. A manifest with another `api` is
rejected at startup with a message naming both versions, and a response with
another `api` fails the call.

## Stable within API v1

- the request and response envelopes, and the error object;
- the four methods (`ping`, `message.metadata`, `message.received`,
  `plugin.test`) and their payloads;
- the `message.metadata` fields;
- the manifest fields, permission names, and setting types;
- the annotation format, its validation limits, and replace-on-success;
- stdout for the response only, stderr for diagnostics.

## May change without a new API version

These are additions or runtime policy, not protocol:

- **New optional fields**, in requests, metadata, or envelopes. Ignore fields
  you do not recognize.
- **New methods, events, or capabilities**, which a plugin receives only if it
  declares them or TideMail calls them by hand. Answer unknown methods with an
  error.
- **Runtime limits:** timeouts, queue sizes, rate limits, process counts,
  pause thresholds (see [events.md](events.md#current-runtime-limits)).
- **Presentation:** how, where, and in what colors annotations appear, and
  which conventional keys and values TideMail's views use.
- **User interface** for running, configuring, and inspecting plugins.

## Would require a new API version

- removing or renaming a field, method, permission, or setting type;
- changing a field's type or meaning;
- tightening validation so that previously valid responses fail;
- changing replace-on-success semantics.

## Not supported

These do not exist in API v1. Do not rely on them:

- custom plugin panes, screens, or HTML UI;
- plugin-defined colors, styles, or glyphs;
- toolbar or menu extensions, or plugin-defined keybindings;
- mutating mail (moving, deleting, flagging, sending);
- long-running background daemons: every call is one short-lived process;
- message bodies (the `message_body` permission is reserved).

Plugin support in TideMail is experimental. The protocol rules above apply to
API v1, but the plugin UI and runtime policy may change freely, and there is no
promise yet about how long a given API version will be supported.
