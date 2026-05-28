# Compose UI improvements

## Goal

Improve the visual polish and usability of the compose/reply overlay in
Tidemail. The current layout works but is bare-bones — flat labels, no visual
grouping, plain attachment rendering, no reply context.

## Current layout

```
COMPOSE
To
[___________________________]
CC
[___________________________]
Subject
[___________________________]
Attachments (if any)
  file.pdf  (245 KB)
Body (ctrl+s to send)
[ body textarea                   ]
[                                 ]
[                                 ]
ctrl+s  send      ctrl+a  attach
tab     next      esc     cancel
```

## Proposed improvements

Grouped into three phases by impact + effort.

---

### Phase A: Field layout & visual hierarchy

**A1 — Group fields with a section header**

Replace the flat "To" / "CC" / "Subject" labels with a grouped section
like Settings (via `addGroup` / `addInput` style):

```
COMPOSE
── Recipients ──
To           [________]
CC           [________]
── Message ──
Subject      [________]
Body         [       ]
```

This uses the existing `renderFormGroupTitle` + `renderFormRow` from
`form_render.go` — same pattern as Settings and the Account Manager.
Gives the compose form the same polished look as the rest of the app.

**Files**: `compose.go` — replace label() rows with form builder calls.

**A2 — Sender/account badge**

Show which account is sending at the top of the compose view, like:

```
COMPOSE  ◉ allie@alliehere.com  (in reply to: Re: …)
```

The account info is already in `c.accountCfg`. Could show it as a
compact header row with the account's color.

**A3 — "To" field with address validation visual hint**

If the To field has content that doesn't look like an email, show a
subtle warning indicator (e.g. muted "?" icon). Only on blur or after
a short delay, not while typing.

---

### Phase B: Attachment display

**Current state**: Attachments show as:
```
Attachments
  report.pdf  (245 KB)
```

**B1 — File type icons** (matching the `attachmentIcon` helper used in
the email content pane — similar to `renderAttachmentIcon` in
`model.go` or `content.go`). Show an icon prefix based on extension.

```
Attachments
  📄 report.pdf  245 KB
  🖼 photo.jpg   1.2 MB
  📦 data.zip    4.5 MB
```

**B2 — Remove attachment action**

There's currently no way to remove an attached file without cancelling
the whole compose. Add a `ctrl+r` / `x` action to remove the selected
(or last-added) attachment. Show a hint like "ctrl+r to remove" in the
attachments section when there are attachments.

**Files**: `compose.go` — key handler for remove, `keys.go` — new
binding `RemoveAttach`.

---

### Phase C: Reply context

When replying (`c.inReplyTo != ""`), the reply is currently sent with
proper In-Reply-To / References headers, but the UI doesn't show what
you're replying to.

**C1 — Quoted original** — Show the original message body quoted at the
bottom of the body textarea (like most mail clients). Use `> ` prefix
for each line of the original. Trim to first ~20 lines for long messages.

**C2 — Subject prefix** — Auto-add "Re: " to the Subject field when
composing a reply (this may already work — need to check where the
reply compose is initialized).

**C3 — Inline reply indicator** — Show a collapsed preview of the
original message in the compose view (subject + from + date snippet),
expandable with Enter/Space.

---

## Implementation order

1. **Phase A1** (form builder pattern) — biggest visual impact, reuses
   existing UI patterns, low risk.
2. **Phase A2** (sender badge) — small change, immediately useful.
3. **Phase B1** (file icons) — consistent with existing rendering.
4. **Phase B2** (remove attachment) — fills a missing UX action.
5. **Phase C** (reply context) — bigger scope, defer unless needed.

## Files likely to change

| File | Phase | What |
|------|-------|------|
| `internal/ui/compose.go` | A,B,C | View rendering, key handling |
| `internal/ui/keys.go` | B2 | RemoveAttach key binding |
| `internal/ui/form_render.go` | A1 | Maybe nothing — reusable helpers already exist |

## Tests / validation

- Visual: compose opens, fields show with section headers, focus
  highlighting works, fields resize with terminal width
- Attachments: ctrl+a → pick file → file shows with icon → ctrl+r
  removes it → ctrl+s sends
- Reply: reply compose shows quoted original, Re: subject prefix

## Open questions

1. For the form builder approach (A1), should we use the existing
   `settingsFormBuilder` or create a standalone `composeFormBuilder`?
   The existing builder is tightly coupled to the Settings struct.
   A standalone builder in `compose.go` or `form_render.go` would be
   cleaner.
2. File icons (B1): use emoji (📄🖼📦) like the sidebar, or text-based
   glyphs? Emoji is simpler but terminal-dependent.
3. Reply quoting (C1): quote the full original or truncate? 20-line
   default with "… (N more lines)" is standard practice.
