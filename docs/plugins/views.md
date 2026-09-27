# Structured views

A report can finish with a **view** instead of plain text: stat tiles,
sparklines, bar charts, a heatmap, tables, and text that TideMail lays out and
draws in the user's theme. Category bars use the same colors as category
tags, tones map to the theme's accent and attention colors, and a heatmap
warms from the accent toward the highlight color.

> **Plugins provide semantics. TideMail controls presentation.**
>
> A view says what the numbers are, never how they look. There are no color,
> style, width, or escape fields; unknown fields are errors, and every string
> is plain text.

Reports are described in [queries.md](queries.md). A complete example is the
[TideMail Analytics](https://github.com/allisonhere/tidemail-plugins/tree/main/plugins/analytics)
plugin.

## Returning a view

Finish a report round with `view` instead of `report`:

```json
{"view": {"title": "Mail Analytics", "subtitle": "last 30 days", "blocks": [
  {"type": "stats", "items": [{"label": "Received", "value": "428"}, {"label": "Needs you", "value": "12", "tone": "attention"}]},
  {"type": "sparkline", "title": "Volume per day", "series": [{"label": "in", "values": [12, 18, 9, 22]}], "start": "Sep 24", "end": "Sep 27"}
]}}
```

A round answers with exactly one of `queries`, `report`, or `view`.

TideMail tells a report which view format it draws in `context.views`
(currently `1`). An older TideMail sends no `views`, so fall back to a text
`report` when it is absent:

```text
if context.views >= 1 → view
else                   → report (text)
```

The view opens in a wide result window; the user scrolls it with `↑`/`↓`.
With **plain UI** on, TideMail draws it without color.

## Blocks

<!-- drift:block-types -->
```text
stats
sparkline
bars
heatmap
table
text
```

Every block may have a `title` and a `note` (shown right-aligned beside the
title), except `text`, which takes only `title`.

| Block | Fields | Drawn as |
| --- | --- | --- |
| `stats` | `items`: `{label, value, note?, tone?}` | rounded tiles, wrapping to the width; `value` is display text such as `"1,234"` or `"2h 30m"` |
| `sparkline` | `series`: `{label, values[]}` (1 to 3); `start`, `end` axis labels | one sparkline per series on a shared scale, stretched or summed to fit the width, brighter as values rise; each series gets its own theme hue |
| `bars` | `items`: `{label, count, note?}`; `kind: "category"` | horizontal bars with eighth-block precision; category bars use the user's category tag colors |
| `heatmap` | `columns[]`, `rows`: `{label, values[]}` | a grid shaded from faint to strong; zero cells are dots |
| `table` | `columns[]`, `cells[][]` (one string per column) | aligned columns, the widest shrunk to fit, striped rows |
| `text` | `text` (newlines allowed) | wrapped plain text |

Numbers (`count`, `values`) must be finite and not negative.

## Tones

<!-- drift:tones -->
```text
neutral
positive
attention
critical
```

A tone says what a stat means; TideMail picks the color: `neutral` (the
default) uses the accent, `positive` the success color, `attention` and
`critical` the user's *important* and *urgent* tag colors.

## Limits

<!-- drift:view-limits -->
```text
max_blocks = 32
max_title = 80
max_label = 40
max_value = 24
max_stats = 8
max_series = 3
max_points = 400
max_bars = 24
max_heat_rows = 12
max_heat_columns = 60
max_table_columns = 6
max_table_rows = 50
max_cell = 120
max_text_bytes = 4000
```

Titles, notes, labels, values, and cells are counted in characters and must
be plain text: no control, format (bidi, zero-width), or escape characters.
A view that breaks any rule fails the report with an error naming the block.
