# Fix: Settings panel visual bleed-through

## Goal

Fix two background bleed-through issues in the settings screen:

1. **Vertical panel separator** — the gap between the "CATEGORIES" sidebar and the detail pane is just the base background showing through, with no visible divider.
2. **AI model selector** — the text input fields for Model (OpenAI/Claude/Gemini/Ollama) blend into the surrounding form because their background matches the form row background.

## Current context

### Toky-Night theme colors (user's preferred theme)
- `Bg`: `#1a1b26`
- `Border`: `#414868`
- `baseBg` (modal surface): `adjustLightness(Bg, 0.06)` ≈ `#1e1f2d`
- `surfaceBg`: `adjustLightness(baseBg, 0.04)` ≈ `#202132`
- `fieldBg`: `adjustLightness(baseBg, 0.08)` ≈ `#222438`
- `border` (overlay): `#7aa2f7`

### Issue 1: Vertical separator
`settings.go:1249` — `viewSplit()` renders the separator as:
```go
separator := lipgloss.NewStyle().Background(chrome.baseBg).Render(" ")
```
This is a single space with the panel background color — no visible line at all. The left and right panes just touch invisibly through the gap.

**Fix**: Replace with a `│` (vertical bar) character, styled with `chrome.border` foreground and `chrome.baseBg` background, width=1.

**Alternative considered**: Using `lipgloss.JoinHorizontal` with actual borders on the panes — but that's more invasive and the panes don't currently use lipgloss borders. The single-character separator is minimal and consistent with the TUI aesthetic.

### Issue 2: AI model selector text inputs
`form_render.go:131-150` — `renderTextInput()` uses `chrome.baseBg` for both the style background and the truncateStyled background:
```go
view := truncateStyled(input.View(), width, chrome.baseBg)
return lipgloss.NewStyle().
    Background(chrome.baseBg).
    Foreground(chrome.text).
    Width(width).
    Render(view)
```

`renderInsetControl()` (called by `addInput()`) also uses `chrome.baseBg`. The result: the Model text fields on the AI section have no visual demarcation — they look like background "showing through."

Meanwhile, `renderSettingsPicker()` (used by Provider dropdown) correctly uses `chrome.surfaceBg` — so the provider picker looks distinct but the text fields don't.

The compose panel (`compose.go:571`) uses `chrome.fieldBg` when a compose field is focused, as does the account manager. The `fieldBg` color is designed exactly for this purpose: subtly elevated background for input areas.

**Fix**: Change `renderTextInput()` to use `chrome.fieldBg` instead of `chrome.baseBg` as the background color (both in the truncateStyled call and the style render).

## Files to change

| File | Lines | Change |
|---|---|---|
| `internal/ui/settings.go` | 1249 | `separator`: use `│` with `chrome.border` fg on `chrome.baseBg` bg, width=1 |
| `internal/ui/form_render.go` | 144, 146 | `renderTextInput()`: change `chrome.baseBg` → `chrome.fieldBg` |

## Test plan

1. `go build ./...` — compilation check
2. `go test ./internal/ui/...` — existing tests must pass
3. Manual visual verification with `go run .`:
   - Navigate to Settings → verify vertical separator is visible between CATEGORIES sidebar and detail pane
   - Navigate to Settings → AI section → verify Model text inputs have a subtly distinct background from the surrounding form

## Risks and open questions

- `renderTextInput()` is used in **all** settings text inputs (not just AI model). Changing its background to `fieldBg` affects: Reading width, Browser command, Feed max body, Retro color fields, API key fields, Model fields, Ollama URL, Save path, and the Manual install command. All of them should benefit from a slightly elevated background — they all have the same "bg showing through" problem. This is a general improvement, not specific to the AI section.
- The `fieldBg` for tokyo-night is `#222438` — already provides enough contrast against `#1e1f2d` (`baseBg`) to be visible.
- Need to verify `truncateStyled` also passes the bg color correctly when the text overflows its box.
