# Presentation: explaining results to people

> **Annotations are for machines. Presentation is for people.**

A `message.metadata` or `message.received` response can carry, next to its
`annotations`, an optional **presentation**: a short, human explanation that
TideMail shows in its result card when someone runs your plugin by hand.

```text
annotations  → stored, shown as tags, searched (#newsletter), used by Needs You
presentation → shown once in the result card; never stored, never acted on
```

Keep both. Annotations stay machine-readable (`category=newsletter`);
presentation says the same thing in words ("This looks like a newsletter").
Never put presentation text into annotation values, or annotation syntax
into presentation.

## Shape

```json
{
  "annotations": [{"key": "category", "value": "newsletter", "confidence": 0.85}],
  "presentation": {
    "title": "This looks like a newsletter",
    "summary": "Automated list mail with low expected priority.",
    "status": "info",
    "confidence": "high",
    "facts": [
      {"label": "Type", "value": "Newsletter"},
      {"label": "Priority", "value": "Low"}
    ],
    "reasons": ["Matches newsletter/list-mail patterns", "Sent by an automated mailing source"]
  }
}
```

| Field | Required | Notes |
| --- | --- | --- |
| `title` | yes | One line: what you concluded. |
| `summary` | no | A sentence or two; newlines allowed. |
| `status` | no | See below; default `info`. |
| `confidence` | no | `high`, `medium`, or `low`: words, not numbers. |
| `facts` | no | Label/value rows. |
| `reasons` | no | Short bullets under **Why**. |

TideMail draws it: layout, colors, keys, and wrapping are TideMail's, from
the user's theme. There are no color, style, width, escape, link, or button
fields: result-card actions belong to TideMail.

TideMail may add a host-owned action when it can do so safely from data it did
not share with the plugin. For example, a card with an **Unsubscribe** fact is
corrected from TideMail's cached message and offers `u` only when TideMail
finds a valid unsubscribe target. The key uses TideMail's normal confirmation
flow. The plugin never receives that target and cannot invoke the action.

## Statuses

<!-- drift:presentation-statuses -->
```text
info
success
warning
danger
neutral
```

TideMail maps them to theme colors: `info` the accent, `success` the success
color, `warning` the user's *important* tag color, `danger` the error color,
`neutral` muted text.

## Limits and validation

<!-- drift:presentation-limits -->
```text
title = 100
summary = 500
facts = 8
fact_label = 40
fact_value = 160
reasons = 6
reason = 180
```

Lengths are in characters. Every string must be valid UTF-8 plain text: no
control, format (bidi, zero-width), or escape characters. Unknown fields,
statuses, or confidence words are errors. Do not put URLs or secrets in
presentation text.

**A bad presentation never costs you your annotations.** If the block is
invalid, TideMail keeps and stores the annotations, ignores the presentation,
and records a warning the user can see under **Details**.
`tidemail plugin test` shows the same warning.

## Without a presentation

Plugins that return only annotations still get a friendly card. TideMail
describes the annotations it understands in words: `category` becomes
**Type**, `sender_value` **Priority**, `needs_reply` **Needs reply**,
`true`/`false` become **Yes**/**No**, and the strongest annotation
confidence becomes **High** (≥ 0.85), **Medium** (≥ 0.65), or **Low**.
Unknown keys are shown with a readable label. A run that returns nothing
shows **No changes**.

Raw keys, values, confidence decimals, storage outcome, and the full response
are always one keypress away: `d` in the result card toggles **Details**.

## Previewing

```sh
tidemail plugin test ./my-plugin             # prints a presentation preview
tidemail plugin test --verbose ./my-plugin   # also the raw response JSON
```
