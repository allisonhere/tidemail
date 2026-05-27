# About-Screen Animation Spec — "Mail Terminal"

## Location

Settings → About section hero panel. Displayed inside a bordered
container at the top of the about page.

## Visual Layout

```
┌ <pulsing border> ─────────────────────────────── ┐
│                                                   │
│              T I D E M A I L                      │  Row 0
│                                                   │
│         your mail, your rules                     │  Row 1
│                                                   │
│  ░░░░░░▓█▓▒░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  │  Row 2
│                                                   │  Row 3 (empty)
└───────────────────────────────────────────────────┘
```

4 rows of content, rendered inside a bordered panel on a black
(`#000000`) background.

## Row 0 — Title

**Text:** `TIDEMAIL` (centered horizontally)  
**Foreground color:** `#84c5d4` (bright cyan)  
**Font weight:** Bold  
**Background:** `#0a0e14` (near-black) with horizontal scanline
alternation (`#0b0f16` on even rows)

**Animations:**
- Enters via a left-to-right gaussian reveal sweep on first view
  (not on loop — one-time entrance)
- After reveal, no additional animation on this row

## Row 1 — Tagline

**Text:** `your mail, your rules` (centered horizontally)  
**Foreground color:** `#84c5d4` dimmed — computed via
`readableText(cyan, bg, 3.5)` to ensure ~3.5:1 contrast ratio  
**Font weight:** Normal  
**Background:** Same scanline BG as Row 0

**Animations:**
- Revealed by the same entrance sweep as Row 0
- Static after reveal

## Row 2 — Signal Bar

**Type:** Horizontal activity/progress indicator  
**Width:** Full content width of the panel (varies with terminal size)

**Visual composition:**
```
ahead →     ░░░░░░░░░░░▓█▓▒░░░░░░░░░  ← trail behind
            ^head (█)
```

The bar uses Unicode block characters in order of density:

| Char | Hex   | Name          | Use                          |
|------|-------|---------------|------------------------------|
| █    | U+2588 | Full block    | Brightest point (the "head") |
| ▓    | U+2593 | Dark shade    | Near-head trail (0.35+)      |
| ▒    | U+2592 | Medium shade  | Mid trail (0.12–0.35)        |
| ░    | U+2591 | Light shade   | Faint trail (0.03–0.12)      |
| ░    | U+2591 | Light shade   | Ahead / very faint below 0.03 |

**Foreground color:** `#98d379` (green, like a terminal cursor/activity
indicator), adjusted for contrast via `readableText(green, bg, 4.5)`

**Animation — smooth sinusoidal sweep:**
- Position: `head = sin(frame * 0.025) * 0.5 + 0.5`, mapped to
  `0..(width-1)` range
- Period: at 120ms tick rate, one full cycle ≈ 209 frames ≈ 25 seconds
- The head moves continuously (no discrete stepping)
- Behind the head, intensity falls off as a gaussian:
  `intensity = exp(-(dist²) / (width * 0.045))`
- The entire bar also breathes: `pulse = sin(frame * 0.06) * 0.15 + 0.85`
  — modulates the trail intensity by ±15% at ~3.5-second cycle

**Overall effect:** Looks like a ping/radar sweep or a modem carrier
signal — a bright point glides left-to-right across the panel with a
smoothly decaying glowing trail behind it, pulsing subtly in brightness.

## Row 3 — Empty

Blank row. No content. Same scanline background.

## Panel Border

**Style:** `lipgloss.NormalBorder()` (Unicode rounded: `╭─╮`, `│ │`,
`╰─╯`) or `lipgloss.ASCIIBorder()` in VT52 plainUI mode (`+--+`, `| |`,
`+--+`)

**Color:** Pulses subtly with the animation frame:
- Computed from: `brightness = 0.22 + 0.08 * sin(frame * 0.015)`
- RGB: `rgb(20*b, 58*b, 74*b)` — very dark desaturated cyan-teal
- At peak: approx `#143a4a`, at trough: approx `#061319`
- Period: ≈ 349 frames ≈ 42 seconds for a full cycle

**Animation:** The border's color oscillates slowly between very dim
and slightly-less-dim cyan, like a CRT monitor that's breathing. The
change is subtle — never bright enough to be distracting.

## Background — CRT Scanline

**Base color:** `#0a0e14` (very dark blue-gray)  
**Alternate scanline:** `#0b0f16` (barely lighter, every other row)  

The two colors alternate row-by-row: even rows get the base, odd rows
get the alternate. This creates a very subtle CRT scanline effect that
is almost invisible unless you're looking for it — it's there for
texture, not for show.

## Entrance Animation (first load only)

When the about section is first opened, all content enters via a
**gaussian reveal sweep**:

- A bright band ~12% of the panel width sweeps from left to right
- Behind the band, content fades in smoothly (gaussian ramp, not a hard
  edge)
- In front of the band, content is completely black (hidden)
- The sweep takes about 3 seconds at 120ms ticks
- Rules used: `aboutHeroRevealMask`, `aboutHeroMaskedBackground`,
  `aboutHeroMaskedForeground` in the source

After the sweep completes, content remains fully visible. The animation
does not repeat — it only runs on the initial entrance.

## Frame Timing

- Tick interval: **120ms** (`settingsAboutPulsePeriod`)
- All animations derive their position from a monotonically increasing
  frame counter (`aboutGradientFrame`)
- Frame 0 is reset each time the about section is entered
- After ~4096 frames (~8.2 minutes), the counter wraps to 0 to prevent
  float precision issues

## Color Palette Summary

| Role              | Hex       | Description               |
|-------------------|-----------|---------------------------|
| Panel BG          | `#000000` | Pure black panel backdrop |
| Scanline base     | `#0a0e14` | Very dark blue-gray       |
| Scanline alt      | `#0b0f16` | Slightly lighter          |
| Title text        | `#84c5d4` | Bright cyan               |
| Tagline text      | dimmed    | Same cyan at 3.5:1 ratio  |
| Signal bar        | `#98d379` | Terminal green            |
| Border peak       | `#143a4a` | Dim cyan-teal             |
| Border trough     | `#061319` | Very dim cyan-teal        |
| Reveal sweep      | `#7edfff` | Light blue shimmer band   |

## Code Location

File: `internal/ui/settings.go`

Key functions:
- `renderAboutHero` — top-level layout (row assembly)
- `renderSignalBar` — generates the sweep bar string
- `aboutHeroBackground` — per-cell BG color (scanline)
- `aboutHeroTextForeground` — per-cell text color  
- `aboutBorderColor` — pulsing border color
- `aboutHeroRevealMask` — entrance sweep gaussian
- `renderAboutHeroTextLine` — per-cell rendering loop
- `renderAboutHeroCell` — single styled cell output
