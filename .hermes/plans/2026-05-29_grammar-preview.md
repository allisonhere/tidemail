# Plan: Grammar Check Preview Before Applying

## Goal

Show a diff/preview of AI grammar corrections before replacing the compose body text. User can review changes and accept or cancel.

## Current behavior

`ctrl+g` fires AI check → replaces body immediately → shows "grammar checked" status. No preview, no confirmation.

## Proposed approach

Two-phase flow:

1. **`ctrl+g` → fetch corrections** (same as now, but don't apply)
2. **Show preview overlay** — side-by-side or unified diff of original vs corrected text
3. **User chooses:** `y` to accept (replace body), `n` to cancel (keep original)

## Preview design (compact, fits compose modal)

Since the compose window is already a modal overlay, keep the preview minimal:

```
┌─ Grammar Preview ──────────────────────────────┐
│                                                 │
│  ~2 changes found                                │
│                                                 │
│  Original:        teh quick brown fox           │
│  Corrected:       the quick brown fox           │
│                                                 │
│  Original:        jump over the lazy dog        │
│  Corrected:       jumped over the lazy dog      │
│                                                 │
│  y  accept    n  cancel                         │
└─────────────────────────────────────────────────┘
```

Or even simpler: show the full corrected text in a scrollable viewport with key hints.

## Implementation plan

1. Add `grammarPreview` overlay mode
2. On `GrammarCheckedMsg` success: store original + corrected, show preview overlay (don't apply yet)
3. Preview view: show corrected text in a viewport, with "accept/cancel" key hints
4. `y` → apply corrected text to body, close preview
5. `n` → discard, close preview
6. Handle `esc` same as cancel

## Files changed

| File | Change |
|------|--------|
| `internal/ui/model.go` | Add overlayGrammarPreview, handle GrammarCheckedMsg → show preview, handle accept/cancel |
| `internal/ui/msgs.go` | Maybe extend GrammarCheckedMsg with Original field |
| `internal/ui/keys.go` | No changes (y/n already exist) |

## Open question

Show a full diff or just the corrected text? 

- **Full diff** — highlights exactly what changed. Harder to render cleanly in a terminal.
- **Corrected text only** — simpler, user can scan for differences. Less information but faster to review.

Recommend starting with corrected text only (simpler) and adding diff highlighting later if needed.

## Effort

~1 hour. Most of the work is the preview overlay rendering.
