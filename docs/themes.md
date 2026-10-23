# Themes

TideMail ships 52 themes in 24 families, plus `match-omarchy`, which follows the
current [Omarchy](https://omarchy.org) desktop theme. Press `T` to open the
picker, or choose one under **Settings → Display → Theme**.

## Using themes

| Where | How |
|---|---|
| Picker | `T`. Themes are grouped by family. `↑`/`↓` (or `j`/`k`) move and **preview the theme across the whole app**; `Enter` keeps it and saves it; `Esc` puts the previous theme back. |
| Search | `/` in the picker, then type. Every word must match the family, the variant, the stored name, or `light` / `dark`: `rose dawn`, `night`, `light`. `Enter` stops typing, `Esc` clears the search, a second `Esc` leaves the search box, and one more closes the picker. |
| Swatch and sample | Each row shows five color cells: accent, success, warning, error and tag. Below the list, a sample shows a selected row, body, muted, link and quoted text, and the success, warning, error, tag and search-match colors in the highlighted theme. |
| Settings | **Settings → Display → Theme** steps through the same list with `←`/`→`. |
| Config file | `theme = "rose-pine-moon"` in `config.toml`. Names are listed below. An unknown name falls back to `catppuccin-mocha`. |

Every name an earlier version accepted still works, so saved settings are
unaffected.

## How a theme is built

Each palette is copied from its upstream project's source, not sampled from a
screenshot. Every hex value in the tables below was checked against the
upstream file it came from. The two exceptions are marked in [Derived
values](#derived-values).

A theme supplies the colors TideMail's interface is drawn with:

| Role | Used for |
|---|---|
| Background, text, muted | Panes, message text, secondary text |
| Border / focus border | Unfocused and focused pane borders (the accent) |
| Selection | Selected rows (the accent, with a readable text color chosen on top) |
| Surface | Status bar, dialogs, command palette, input fields |
| Unread, success | Unread counts and badges, confirmations |
| Warning | Starred-row tint and cautions |
| Error | Error text |
| Link | Hyperlinks in messages |
| Quote | Quoted text and its bars |
| Match | Search matches |
| Tag | The default annotation-tag color (known categories keep their own hues) |
| Cursor | The text cursor in compose |

Where an upstream palette has no direct equivalent, the closest of its own
colors is used: for example Rosé Pine has no green, so foam stands in for
success; Oxocarbon has no yellow, so its pink is the warning color.

### Legibility

Upstream palettes are designed for editor syntax highlighting, and many of
their accent colors are under the 4.5:1 contrast of WCAG AA as text. The
palette values are never edited. Instead, text roles (link, quote, success,
warning, error, tag, unread) are lightness-adjusted at the point of use, keeping
hue and saturation, until they reach 4.5:1 on the theme background (3:1 for
the cursor and search-match colors). Status bar text is adjusted the same way
when needed; that applies to `tokyo-night-day`, `solarized-light` and
`coral-sunset`. Automated tests check body text, status text and every role on
every theme, and check the resolved link, quote, search-match, success, cursor
and tag colors in TideMail itself.

Solarized's body tones (base0 and base00) are under 4.5:1 by design, so the
Solarized themes use the emphasized-content tones its documentation provides
(base1 on dark, base01 on light).

### Retro and original palettes

These five are TideMail's own designs, not ports:

| Name | Description |
|---|---|
| `vt100` | Green phosphor on black, with dim and bright green levels. |
| `vt52` | An amber-monitor-inspired palette: warm golden text on charcoal black with muted bronze accents, and ASCII borders and glyphs. An aesthetic preset, not a claim about the colors of any historical VT52 display. |
| `magenta-geode` | Dark mineral palette: deep plum with polished magenta, fuchsia and violet highlights. |
| `coral-sunset` | Dark coastal-sunset palette: coral, salmon, peach and rose-gold over a muted dusk purple. |
| `lavender-fields-forever` | Gentle lilac palette: heather purple and soft violet with readable neutral text. |

The phosphor themes are plain palettes: there are no CRT effects, animation or
terminal escape tricks. TideMail dims their focus and selection accent a step
below the library's full-bright value (`#116611` for `vt100`, `#664400` for
`vt52`) so the focused-pane border clears its 7:1 contrast floor. The `[display.vt100]` and
`[display.vt52]` config tables can still override their colors.

## Themes and variants

Name is what `config.toml` stores. Colors are the background, body text and
accent of each theme.

### Catppuccin

Upstream: <https://github.com/catppuccin/palette> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `catppuccin-mocha` | Mocha | `#1e1e2e` | `#cdd6f4` | `#89b4fa` |
| `catppuccin-macchiato` | Macchiato | `#24273a` | `#cad3f5` | `#8aadf4` |
| `catppuccin-frappe` | Frappé | `#303446` | `#c6d0f5` | `#8caaee` |
| `catppuccin-latte` | Latte | `#eff1f5` | `#4c4f69` | `#1e66f5` |

### One Dark

Upstream: <https://github.com/Binaryify/OneDark-Pro> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `one-dark` | Pro | `#282c34` | `#abb2bf` | `#61afef` |
| `one-dark-darker` | Pro Darker | `#23272e` | `#abb2bf` | `#61afef` |

### Tokyo Night

Upstream: <https://github.com/folke/tokyonight.nvim> · License: Apache-2.0

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `tokyo-night` | Night | `#1a1b26` | `#c0caf5` | `#7aa2f7` |
| `tokyo-night-storm` | Storm | `#24283b` | `#c0caf5` | `#7aa2f7` |
| `tokyo-night-moon` | Moon | `#222436` | `#c8d3f5` | `#82aaff` |
| `tokyo-night-day` | Day | `#e1e2e7` | `#3760bf` | `#2e7de9` |

### Dracula

Upstream: <https://github.com/dracula/dracula-theme> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `dracula` | Dracula | `#282a36` | `#f8f8f2` | `#bd93f9` |
| `dracula-alucard` | Alucard | `#fffbeb` | `#1f1f1f` | `#644ac9` |

### GitHub

Upstream: <https://github.com/primer/github-vscode-theme> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `github-dark` | Dark | `#0d1117` | `#c9d1d9` | `#58a6ff` |
| `github-dark-dimmed` | Dark Dimmed | `#22272e` | `#adbac7` | `#539bf5` |
| `github-light` | Light | `#ffffff` | `#24292f` | `#0969da` |

### Gruvbox

Upstream: <https://github.com/morhetz/gruvbox> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `gruvbox-dark` | Dark | `#282828` | `#ebdbb2` | `#83a598` |
| `gruvbox-light` | Light | `#fbf1c7` | `#3c3836` | `#076678` |

### Nord

Upstream: <https://github.com/nordtheme/nord> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `nord` | Nord | `#2e3440` | `#d8dee9` | `#88c0d0` |

### Rosé Pine

Upstream: <https://github.com/rose-pine/neovim> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `rose-pine` | Main | `#191724` | `#e0def4` | `#c4a7e7` |
| `rose-pine-moon` | Moon | `#232136` | `#e0def4` | `#c4a7e7` |
| `rose-pine-dawn` | Dawn | `#faf4ed` | `#464261` | `#907aa9` |

### Kanagawa

Upstream: <https://github.com/rebelot/kanagawa.nvim> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `kanagawa-wave` | Wave | `#1f1f28` | `#dcd7ba` | `#7e9cd8` |
| `kanagawa-dragon` | Dragon | `#181616` | `#c5c9c5` | `#8ba4b0` |
| `kanagawa-lotus` | Lotus | `#f2ecbc` | `#545464` | `#4d699b` |

### Solarized

Upstream: <https://github.com/altercation/solarized> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `solarized-dark` | Dark | `#002b36` | `#93a1a1` | `#268bd2` |
| `solarized-light` | Light | `#fdf6e3` | `#586e75` | `#268bd2` |

### Everforest

Upstream: <https://github.com/sainnhe/everforest> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `everforest-dark` | Dark | `#2d353b` | `#d3c6aa` | `#7fbbb3` |
| `everforest-light` | Light | `#fdf6e3` | `#5c6a72` | `#3a94c5` |

### Nightfox

Upstream: <https://github.com/EdenEast/nightfox.nvim> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `nightfox` | Nightfox | `#192330` | `#cdcecf` | `#719cd6` |
| `dayfox` | Dayfox | `#f6f2ee` | `#3d2b5a` | `#2848a9` |
| `dawnfox` | Dawnfox | `#faf4ed` | `#575279` | `#286983` |
| `duskfox` | Duskfox | `#232136` | `#e0def4` | `#569fba` |
| `nordfox` | Nordfox | `#2e3440` | `#cdcecf` | `#81a1c1` |
| `terafox` | Terafox | `#152528` | `#e6eaea` | `#5a93aa` |
| `carbonfox` | Carbonfox | `#161616` | `#f2f4f8` | `#78a9ff` |

### Ayu

Upstream: <https://github.com/ayu-theme/vscode-ayu> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `ayu-dark` | Dark | `#10141c` | `#bfbdb6` | `#e6b450` |
| `ayu-mirage` | Mirage | `#242936` | `#cccac2` | `#ffcc66` |
| `ayu-light` | Light | `#fcfcfc` | `#5c6166` | `#f29718` |

### Palenight

Upstream: <https://github.com/whizkydee/vscode-palenight-theme> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `palenight` | Palenight | `#292d3e` | `#bfc7d5` | `#82aaff` |

### Night Owl

Upstream: <https://github.com/sdras/night-owl-vscode-theme> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `night-owl` | Night Owl | `#011627` | `#d6deeb` | `#82aaff` |
| `night-owl-light` | Light | `#fbfbfb` | `#403f53` | `#288ed7` |

### SynthWave '84

Upstream: <https://github.com/robb0wen/synthwave-vscode> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `synthwave-84` | SynthWave '84 | `#262335` | `#ffffff` | `#ff7edb` |

### Shades of Purple

Upstream: <https://github.com/ahmadawais/shades-of-purple-vscode> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `shades-of-purple` | Shades of Purple | `#2d2b55` | `#ffffff` | `#b362ff` |

### Oxocarbon

Upstream: <https://github.com/nyoom-engineering/oxocarbon.nvim> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `oxocarbon-dark` | Dark | `#161616` | `#f2f2f2` | `#78a9ff` |
| `oxocarbon-light` | Light | `#ffffff` | `#525252` | `#0f62fe` |

### Cyberdream

Upstream: <https://github.com/scottmckendry/cyberdream.nvim> · License: MIT

| Name | Variant | Background | Text | Accent |
|---|---|---|---|---|
| `cyberdream` | Dark | `#16181a` | `#ffffff` | `#5ea1ff` |
| `cyberdream-light` | Light | `#ffffff` | `#16181a` | `#0057d1` |

## Variants not included

To keep the list free of near-duplicates, variants whose colors match an
included one, or that only change editor chrome, are left out:

- **Monokai Pro**: not included. Its palette is a commercial product and its
  repository carries no license permitting redistribution, so no Monokai Pro
  colors, and no approximation labeled as Monokai Pro, ship here.
- **One Dark Pro**: Flat and Mix have the same editor colors as Pro; Night Flat is not included.
- **GitHub**: the high-contrast and color-blind variants.
- **Gruvbox** and **Everforest**: the hard and soft contrast levels; medium is included.
- **Palenight**: the italic, mild-contrast and operator variants.
- **Shades of Purple**: the Super Dark and italic variants.
- **Night Owl**: the no-italic variants, which share colors with the included ones.
- **Cyberdream**: the dimmed variant.
- **SynthWave '84**: the neon glow is a CSS injection in the VS Code extension, not
  part of the palette, so only the palette is included.

## Derived values

Three themes include a value their upstream computes rather than lists:

- `oxocarbon-dark`: the grays `#262626`, `#525252` and `#f2f2f2`, and
  `oxocarbon-light`: `#f2f2f2`, are the results of upstream's own `blend-hex`
  function (HSLuv blending), run on its inputs.
- `carbonfox`: the muted comment color `#6e6f70` is Nightfox's blend of the
  theme's background and foreground at 0.4.

## Licenses and attribution

The palette values come from the projects below, each under the license shown.
TideMail reproduces only color values, not code or other assets.

| Family | License | Copyright |
|---|---|---|
| [Catppuccin](https://github.com/catppuccin/palette) | MIT | © 2021 Catppuccin |
| [One Dark Pro](https://github.com/Binaryify/OneDark-Pro) | MIT | © 2013-2022 Binaryify |
| [Tokyo Night](https://github.com/folke/tokyonight.nvim) | Apache-2.0 | © folke |
| [Dracula](https://github.com/dracula/dracula-theme) | MIT | © 2023 Dracula Theme |
| [GitHub (Primer)](https://github.com/primer/github-vscode-theme) | MIT | © 2018 GitHub Inc. |
| [Gruvbox](https://github.com/morhetz/gruvbox) | MIT/X11 (stated in its README; the repository has no LICENSE file) | © morhetz |
| [Nord](https://github.com/nordtheme/nord) | MIT | © 2016-present Sven Greb |
| [Rosé Pine](https://github.com/rose-pine/neovim) | MIT | © 2023 Rosé Pine |
| [Kanagawa](https://github.com/rebelot/kanagawa.nvim) | MIT | © 2021 Tommaso Laurenzi |
| [Solarized](https://github.com/altercation/solarized) | MIT | © 2011 Ethan Schoonover |
| [Everforest](https://github.com/sainnhe/everforest) | MIT | © 2019 sainnhe |
| [Nightfox](https://github.com/EdenEast/nightfox.nvim) | MIT | © 2021 James Simpson |
| [Ayu](https://github.com/ayu-theme/vscode-ayu) | MIT | © 2016 Ike Kurghinyan |
| [Palenight](https://github.com/whizkydee/vscode-palenight-theme) | MIT | © 2017-present Olaolu Olawuyi |
| [Night Owl](https://github.com/sdras/night-owl-vscode-theme) | MIT | © 2018 Sarah Drasner |
| [SynthWave '84](https://github.com/robb0wen/synthwave-vscode) | MIT | © 2019 Robb Owen |
| [Shades of Purple](https://github.com/ahmadawais/shades-of-purple-vscode) | MIT | © 2015-∞ Ahmad Awais |
| [Oxocarbon](https://github.com/nyoom-engineering/oxocarbon.nvim) | MIT | © 2022 Riccardo Mazzarini |
| [Cyberdream](https://github.com/scottmckendry/cyberdream.nvim) | MIT | © 2023 Scott McKendry |

The Tokyo Night palette is used under the Apache License 2.0, which
<https://www.apache.org/licenses/LICENSE-2.0> sets out in full.

## Where the themes live

The palettes are defined in the shared
[tideui](https://github.com/allisonhere/tideui) library (`themes_collection.go`),
which TideMail and its sibling apps use. TideMail adds the picker, the
accent dimming for `vt100` and `vt52`, and the places the roles are applied.
