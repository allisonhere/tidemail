# Plan: Compose Modal Visual Redesign

## Current State

The compose modal renders as a bordered overlay (74×36 max) containing:

```
┌─ COMPOSE  ◉ sender@email ─────────────────────────────┐ ← accent header
│ ━ RECIPIENTS ───────────────────────────────────────── │ ← muted section title
│   To      │ [to@example.com          ]                  │ ← form row (renderComposeRow)
│   CC      │ [cc (optional)           ]                  │
│ ━ MESSAGE ──────────────────────────────────────────── │
│   Subject │ [Subject                 ]                  │
│                                                        │ ← blank spacer
│ [body textarea with Padding(0,2)    ]                  │ ← full-width padded body
│                                                        │
│   ▸  Show quoted original (enter to toggle)             │ ← reply quote (muted)
│                                                        │
│ ━ ATTACHMENTS ──────────────────────────────────────── │ ← if attachments
│   📄 invoice.pdf  245.1 KB    [ctrl+r remove]          │ ← emoji icons + accent color
│                                                        │
│ [sending...]                                            │ ← status line
│ ────────────────────────────────────────────────────── │ ← action bar border
│ CTRL+S  SEND              TAB  NEXT FIELD  ESC  CANCEL │ ← action bar
└────────────────────────────────────────────────────────┘
```

Key observations:
- Plain/flat modal surface (all `baseBg`)
- Section titles for a tiny form (3 fields + body) create noise
- Body textarea blends with the rest — no visual weight distinction
- Focused field indicator is a tiny " >" in a 2-char column
- Input backgrounds only subtly shift (`baseBg` → `fieldBg`) on focus
- Outer border is a flat box — no depth or layering

---

## Approach A — Minimalist: Remove section titles, compact layout

Remove "━ RECIPIENTS" / "━ MESSAGE" / "━ ATTACHMENTS" headers entirely. The form has 3 input fields, a body, and optional extras — the grouping is self-evident. This reduces visual noise and gives the tab-focused workflow a cleaner feel.

**Changes:**
- Delete `renderFormGroupTitle` calls from compose View()
- Add a 1-line gap row instead of section headers
- Slightly more vertical space for body textarea

**Pros:** Cleaner, less noise, dead-simple
**Cons:** Loses the sectional structure that settings/account-manager use

---

## Approach B — Depth & Focus: Accent left bar for focused fields, distinct body panel

Replace the subtle `" >"` focus marker with a 3-char accent-colored left bar for the focused field. Give the body textarea a distinct surface (darker `surfaceBg` or a left accent border). This is the most dramatic improvement with the fewest changes.

**Changes:**
- `renderComposeRow`: when focused, use a 3-char `│` accent bar instead of `" >"` in the 2-char marker slot. Non-focused rows become a 2-char empty margin.
- Body textarea: wrap in a container with `chrome.surfaceBg` background (slightly darker/lighter than `baseBg`) so it reads as a distinct "composition area"
- Apply left accent bar when body is focused

**Pros:** High visual impact; body immediately reads as "the thing you're writing"; focused field is unmistakable
**Cons:** The accent bar approach may not fit the 2-char marker column — needs a 1-char width increase or a redesign of the marker column

---

## Approach C — Sophisticated: Grid-based layout with labeled sections

Redesign the modal as a visual grid with clean, prominent section labels (like the settings manager uses its `sectionLabel` panel style). Group recipients into a subtle bordered panel, message fields into another. Give the body a prominent `surfaceBg` container.

**Changes:**
- Titled panels: "RECIPIENTS" panel contains To/CC fields, "MESSAGE" panel contains Subject + body
- Each panel gets `chrome.surfaceBg` background and a thin border
- Body textarea inside the MESSAGE panel with its own subtle styling

**Pros:** Most polished and professional; clear visual hierarchy
**Cons:** More code; borders eat horizontal space; needs careful width calculations

---

## Approach D — Classic Email: Recognizable "From/To/Subject" header block

Model the compose after traditional email UI: a tight header block at the top with From/To/CC/Subject as labeled rows using the compose's existing `renderComposeRow`, then a clear visual break (thin rule line or color change) before the body writing area. The body gets its own visual identity — different background, subtle inset border.

**Changes:**
- Add a "From" display field (read-only, showing sender) above "To"
- Remove section titles
- Insert a thin accent rule line between the header fields and the body
- Body area gets `chrome.surfaceBg` background with inner 2px padding
- Keep existing `renderComposeRow` for field rows

**Pros:** Familiar email-client feel; From field adds context; clear "header → body" split
**Cons:** From field is mostly cosmetic/contextual (can't be edited mid-compose)

---

## Recommendation

**Approach B** is the sweet spot — minimal code change, maximum visual impact. The accent left bar on focused fields + distinct body panel instantly makes the compose feel intentional and polished. If the user wants more sophistication, Approach D adds the "From" context and a clean rule-line separation without going full panel-grid.

---

## Implementation outline (Approach B)

1. **Change focus marker**: In `renderComposeRow`, replace `" >"` (2 chars) with `" │"` (accent-colored bar + space). Non-focused rows: 2-char empty margin `"  "` stays.
   - File: `internal/ui/compose.go` — `renderComposeRow` function (lines 486–490)

2. **Distinct body panel**: Wrap the body textarea in a container with `chrome.surfaceBg` (darker) background, and keep `Padding(0, 2)`.
   - When body is focused, optionally add the same left accent bar treatment.
   - File: `internal/ui/compose.go` — `View()` function body section (lines 577–582)

3. **Test & verify**: `go build ./...` / `go test ./...` / `go vet ./...`

## Files changed
- `internal/ui/compose.go` — `renderComposeRow()` + body area in `View()`

## Risks
- The `│` character may not render on all terminals (VT52 plain mode uses ASCII — need ASCII fallback `|`)
- Accent bar approach changes the marker column width semantics slightly
- `surfaceBg` might not contrast enough on some themes — need to verify on both light and dark
