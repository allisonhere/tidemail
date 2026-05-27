# Replace About-Screen "Tide" Animation with Tidemail Mail Animation

## Goal

Replace the ocean/tide animation in the Settings → About hero panel with a
terminal-email themed ANSI animation that better fits Tidemail.

The current animation renders:
- A gradient sky-to-ocean background (4 rows)
- Ocean wave ASCII art (`~~`, `/\`, `..`) with shimmer/foam
- The word "TIDE" overlaid with a sweeping reveal mask
- Tagline "Your feeds, no algorithm, no bullshit" (stale RSS-era text)

## What to Keep From Existing Infrastructure

The about-screen animation framework is solid and should be reused:

- **`aboutGradientFrame`** — int counter incremented every 120ms via `settingsAboutPulseMsg`
- **`renderAboutHero(width, chrome)`** — panel wrapper with border + padding
- **`renderAboutHeroTextLine(text, width, row, bold)`** — per-character render loop with cell-level background/foreground/mask
- **`renderAboutHeroCell(ch, bg, fg, bold)`** — single styled cell
- **`aboutHeroRevealMask(frame, row, col, width)`** — sweeping reveal sweep (reuse as-is or tweak timing)
- **`aboutHeroBackground(frame, row, col, width)`** — replace palette and pattern
- **`aboutHeroTextForeground(bg, row, ch)`** — keep the pattern, adjust colors
- **The hero panel itself** (border, black background, padding) — keep

## Proposed Animation: "Mail Terminal"

A retro terminal/CRT scene with:

### Row 0: Border art
```
┌─────────────────────────────────────┐
```
A subtle top border of a "terminal window" in a dim cyan/green CRT color.

### Row 1: Title bar
```
│  TIDEMAIL  ──  your mail, your rules │
```
The app name in bold accent color, with a dimmed separator and tagline.

### Row 2: "Signal" scan line
```
│  ▓░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  │
```
A pulsing horizontal progress/activity bar that oscillates left-to-right
(like a modem carrier signal or mail sync indicator).

### Row 3: Bottom border
```
└─────────────────────────────────────┘
```

The reveal mask sweeps left-to-right (reuse `aboutHeroRevealMask`), revealing
the terminal chrome and text as if a CRT monitor is warming up.

### Color Palette

Replace ocean blues with CRT/terminal-inspired colors:

| Role | Dark Theme | Light Theme |
|------|-----------|-------------|
| BG | `#0a0e14` (dark terminal) | `#f4f4f4` (paper) |
| Border/title | `#84c5d4` (cyan) | `#1a6b7a` (teal) |
| Signal bar | `#98d379` (green) | `#3a7b2e` (dark green) |
| Body text | `#b3b1ad` (warm gray) | `#5c5c5c` |

### Frame Animation

The `aboutGradientFrame` drives:
1. **Signal bar position** — a short block `▓` slides horizontally across
   row 2, with a fading trail behind it (amplitude: oscillator using frame % width)
2. **Reveal mask** — unchanged from current, sweeps the whole scene from left edge
   to right edge over ~3 seconds at 120ms ticks
3. **Title shimmer** — subtle brightness oscillation on the "TIDEMAIL" letters
   via `renderAboutHeroCell`'s bold flag (pulse amplitude derived from frame)

## Files That Change

| File | Changes |
|------|---------|
| `internal/ui/settings.go` | Replace `renderAboutHero`, `aboutHeroBackground`, `aboutHeroWavePattern`, `aboutHeroWaveForeground`, `aboutHeroFoamSample`, `aboutHeroWaveLine` with new mail-themed equivalents. Update tagline in `renderAboutHero`. Remove unused wave helpers. |

**Functions to remove:**
- `aboutHeroWavePattern` — no more wave ASCII art
- `aboutHeroWaveForeground` — wave-specific colors
- `aboutHeroFoamSample` — returns 0 always
- `renderAboutHeroWaveLine` — no longer called
- Wave-specific color helpers

**Functions to rewrite:**
- `aboutHeroBackground` → `aboutHeroCRTBackground` (or just rewrite in place)
- `renderAboutHero` — new layout (terminal window)

**Functions to keep:**
- `aboutHeroRevealMask` — reuse as-is
- `renderAboutHeroTextLine` — reuse as-is
- `renderAboutHeroCell` — reuse as-is
- `aboutHeroTextForeground` — amend palette only
- `aboutHeroMaskedBackground` / `aboutHeroMaskedForeground` — likely unchanged

## Step-by-Step Implementation

1. **Read & understand** the full current `renderAboutHero` → wave helpers chain
2. **Replace `renderAboutHero`** — new 4-row layout with terminal border art
3. **Replace `aboutHeroBackground`** — new palette (CRT dark or paper light)
4. **Replace `aboutHeroTextForeground`** — new colors matching new palette
5. **Add signal-bar oscillator** — small function that maps frame+col to opacity
6. **Remove dead functions** — `aboutHeroWavePattern`, `aboutHeroWaveForeground`,
   `aboutHeroFoamSample`, `renderAboutHeroWaveLine`
7. **Update tagline** — `"Your feeds, no algorithm, no bullshit"` → `"your mail, your rules"`
8. **Build** — `go build ./...`
9. **Test** — `go test ./internal/ui/`
10. **Visual test** — run the app and navigate to Settings → About

## Risks & Tradeoffs

1. **Width sensitivity** — The terminal border art (`┌─┐`, `│ │`, `└─┘`) needs
   minimum width (≈30 cols). The current code already handles truncation via
   `truncate()` and `aboutCenterText()`, but the border art will look broken
   on very narrow terminals. Mitigation: if `contentW < 30`, fall back to
   plain text without border art.

2. **PlainUI (VT52) mode** — The current wave uses Unicode line-drawing chars
   (`/`, `\`, `~`) which work in ASCII. The new border art uses box-drawing
   chars (`┌`, `─`, `┐`, `│`, `└`, `┘`). These need to fall back to ASCII
   equivalents (`+`, `-`, `+`, `|`, `+`, `+`) when `chrome.plainUI` is true,
   matching the pattern used in `lipPaneBorder()`.

3. **Animation performance** — Same 120ms tick as current; cell-level rendering
   uses lipgloss per character which is the same cost. No regression expected.

4. **The reveal mask sweeping left-to-right** creates a "CRT warmup" feel when
   paired with the terminal border. This is the key visual hook — keep it.

## Validation

- `go test ./internal/ui/ -run Settings` — existing settings tests should pass
- Visual: open Settings → About, observe animation loop
- Visual: switch to VT52 theme, verify ASCII fallback borders
- Visual: resize terminal to very narrow, verify graceful fallback
