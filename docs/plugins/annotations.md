# Annotations

Annotations are the only plugin output TideMail keeps. A `message.metadata`
or `message.received` response attaches them in `data.annotations`:

```json
{
  "annotations": [
    {"key": "needs_reply", "value": "true", "confidence": 0.84},
    {"key": "category", "value": "billing"}
  ],
  "summary": "Anything else in data is shown to the user and never acted on."
}
```

Each annotation has:

| Field | Required | Notes |
| --- | --- | --- |
| `key` | yes | What the fact is about. |
| `value` | yes | A string; may be empty. |
| `confidence` | no | A number from 0 to 1. Omitted and `0` are different. |

Other fields inside an annotation are ignored.

## Limits

<!-- drift:limits -->
```text
max_annotations = 32
max_key_length = 64
max_value_bytes = 512
```

- At most **32** annotations per response, and no duplicate keys.
- `key`: 1 to **64** characters of lowercase `a-z`, `0-9`, `_`, `.`, `-`,
  starting with a letter or digit (`^[a-z0-9][a-z0-9_.-]*$`).
- `value`: at most **512** bytes of valid UTF-8, with no control characters
  (escapes, newlines, tabs), format characters (bidi overrides, zero-width
  characters), or line/paragraph separators.
- `confidence`: when present, from 0 to 1 inclusive.
- `annotations` must be an array, and `data` a JSON object. A missing or
  `null` `annotations` counts as an empty set.

Validation is **all or nothing**: one invalid entry rejects the whole set,
and stored annotations stay exactly as they were. The user sees why
(`annotations rejected, nothing changed: …`).

## Replace on success

Annotations are stored per plugin and per message, in TideMail's local cache.

- A successful response makes the returned set the plugin's **complete** set
  for that message, in one transaction. Keys you no longer return are
  removed.
- An **empty** set (`"annotations": []`) is a real answer: it clears your
  plugin's annotations on that message.
- A **failed** call (timeout, crash, non-zero exit, invalid response,
  `ok: false`, rejected set) never changes stored annotations.
- Other plugins' annotations on the same message are never touched.
- Annotations are stored only when the manifest declares both
  `message_metadata` and `annotations`. See [permissions.md](permissions.md).

Stored annotations stay when a plugin is removed: they are the user's data
about their mail. Users clear them explicitly (per message, or per plugin
everywhere). Deleting a message from TideMail's cache deletes its
annotations.

## Reclassification and idempotency

Users can rerun any plugin on existing mail at any time with **Reclassify**:
the current message, a selection, or every message in the current view.
They do this after updating your plugin, changing its settings, or changing
thresholds, and TideMail reports how many messages' classifications
**changed** and how many stayed the **same**.

So your classifier should be:

- **deterministic** for identical inputs and settings, so an unchanged
  plugin reports "unchanged";
- **idempotent**: running it twice on a message leaves the same result as
  running it once;
- **free of irreversible side effects**: no sending, no counting, no
  "first time seen" state that a rerun would trip.

Do not assume `message.metadata` (or `message.received`) is called only once
per message. If your plugin must behave otherwise (for example because an
online model is not deterministic), say so in its documentation.

For the changed/unchanged count, TideMail compares your plugin's stored set
before and after a successful run as normalized `key=value` pairs: order does
not matter, values are compared trimmed and case-insensitively, and
`confidence` is ignored. A different confidence alone is "unchanged".

## Conventional keys

Any valid key is stored and listed under **Message annotations**. A few keys
have meaning in TideMail's own views:

| Key | Values TideMail acts on (case-insensitive) | Used for |
| --- | --- | --- |
| `needs_reply` | `true`, `yes`, `1` | reply tag; Needs You |
| `urgency` | `high`, `urgent`, `critical` | urgent tag; Needs You |
| `importance` | `high` | important tag; Needs You |
| `category` | a short identifier: `a-z`, `0-9`, `_`, `-`, up to 12 characters | category tag |

- Emit **positive signals only**. `needs_reply=false` or `urgency=low` are
  valid but have no effect; leaving the key out has the same result and
  keeps the annotations view tidy.
- A category is trimmed and lower-cased before it is checked. One that still
  does not fit the pattern (spaces, punctuation, more than 12 characters) is
  stored but never becomes a tag.
- Known categories with their own colors: `github`, `shipping`, `security`,
  `calendar`, `newsletter`, `support`, `social`, `billing`, `receipt`,
  `notification`, `personal`. Any other valid category (say `project`) gets
  the generic category color.

TideMail may apply a local user correction on top of these annotations. A
correction for `needs_reply`, `urgency`, `importance`, or `category` wins over
all plugin judgments for that field, but never rewrites or deletes the stored
plugin rows. Re-running a plugin does not clear corrections, and corrections
remain after plugin annotations are cleared or the plugin is removed. Plugin
authors should treat annotations as recommendations, not authoritative final
presentation state.

### Needs You

A message in an inbox folder appears in **Needs You** when any plugin's
annotation carries `needs_reply`, `urgency`, or `importance` with one of the
values above. A positive signal from any plugin wins; TideMail has no plugin
priority or voting. `category` alone never qualifies. User corrections are
applied before qualification and ranking. Reclassifying updates Needs You
live: a new signal may bring a message in, a removed one may take it out.
Snoozed and dismissed messages stay out until the user brings them back.

## Presentation belongs to TideMail

> **Plugins provide semantics. TideMail controls presentation.**

Valid:

```text
category=github
urgency=high
```

Not valid, and never used for display:

```text
color=purple
style=pill
foreground=#ff00ff
```

Such keys are stored like any other unknown key and listed under **Message
annotations**, but TideMail never turns annotation text into colors, styles,
or glyphs. Tags are drawn from a fixed set of kinds (reply, urgent,
important, category), in the user's chosen style (pills, compact, or plain),
with colors from the user's settings, then the theme, then TideMail's
defaults. A category value is only ever looked up by name.
