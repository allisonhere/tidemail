# Plan: Clarify Search Cancel Instructions

## Goal

Make it obvious how to cancel search and return to the message list. Both search modes (`/` for list, `ctrl+f` for content) already have `esc` bound to cancel — the issue is visibility.

## Current state

- **Message search** (`/`): shows "SEARCH MESSAGES" header with actions "enter apply  esc clear"
- **Content search** (`ctrl+f`): shows search input in content pane header, cancelable via `esc`
- Both work correctly — `esc` clears and returns to message list

## What's missing

The message search overlay's "esc clear" hint is in a small actions bar at the bottom. It may not be obvious enough.

## Proposed approach

Add a dimmed helper line directly below the search input:

```
Search messages...
esc  clear    enter  apply
```

Keep the existing actions bar too (it's already there). This puts the hint right where the user is looking — next to the input field.

Or simpler: just update the placeholder text to include the hint, e.g. `"search messages... (esc to cancel, enter to apply)"`

## Files changed

| File | Change |
|------|--------|
| `internal/ui/model.go` | `renderSearchOverlay` — add hint line or update placeholder |

## Effort

5 minutes. Trivial text change.
