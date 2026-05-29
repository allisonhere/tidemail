# Plan: Add TIDE Backronym to About Section

## Goal

Add a cute subtitle under the hero block in Settings → About explaining what TIDEMAIL stands for: **TermInal Information Delivery Engine**, for the curious.

## Current state

The About section renders:
1. "TIDEMAIL" hero block with animated rainbow signal bar
2. "your mail, your rules" tagline
3. Version number (new, just added)
4. Repository · Issues links (compact, just shrank)
5. "Thanks for taking a look -allie ♥" closing note

## Proposed approach

Insert one centered line directly under the tagline, before the version number:

```
terminal information delivery engine
```

Rendered in muted/italic style, small, centered. Added to `renderAboutHero` as the third text line.

## Step-by-step

1. In `renderAboutHero`, add a third centered text line: `"terminal information delivery engine"` on row 2, not bold
2. Style it as muted + italic (same as the closing note style)
3. This adds exactly one line of height — no layout adjustments needed elsewhere

## Files changed

| File | Change |
|------|--------|
| `internal/ui/settings.go` | `renderAboutHero` — add one centered muted text line |

## Tests

- `TestSettingsAboutViewShowsLinksAndTagline` — add `"terminal information delivery engine"` to expected strings

## Risk

None. One-line addition, no layout impact.
