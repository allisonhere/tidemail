# tideui migration

TideMail's `internal/ui/` is a fork of the shared
[`github.com/allisonhere/tideui`](https://github.com/allisonhere/tideui)
toolkit (`tideftp` consumes tideui as a library; TideMail carries a copy).
The fork has already drifted — two overlay fixes had to be hand-backported —
so this branch begins de-forking it, starting with the lowest-risk layer.

## Done on this branch (`tideui-migration`)

| Commit | Change | Risk |
|---|---|---|
| Alias `ui.Theme` to `tideui.Theme` | `type Theme = tideui.Theme`. The struct is field-identical; no method is defined on it; every call site and theme var compiles unchanged. The two Theme types can no longer drift. | none (type identity) |
| Source the stock palettes from tideui | The 17 non-VT themes were verified byte-identical (`reflect.DeepEqual` over every field) and are now `var CatppuccinMocha = tideui.CatppuccinMocha` shims. `themes.go`: 391 → 130 lines. | none (proven value-equal) |
| Defer `MergeRetroTweak` to `tideui.ThemeOverrides.Apply` | Identical field mapping; `theme_merge_test.go` pins it. | none (exact equivalence, tested) |

VT52 / VT100 stay defined locally: TideMail dims their focus/selection accent
below tideui's full-bright value (`#664400` vs `#ffb020`, `#116611` vs
`#00ff00`) to clear its stricter 7:1 focused-pane contrast floor. Adopting
tideui's VT palettes would need that floor re-examined.

`internal/ui/terminal.go` is **not** a fork worth removing: TideMail's version
also drives OSC 10 (foreground) and writes through `/dev/tty` via a
`tea.Cmd`, where `tideui.TerminalBackgroundSequences` only emits OSC 11.

## Not done — needs human review (Tier 2+)

The remaining duplication is the **chrome**: `soft_panel.go`
(`renderSoftPanelBox` / `renderSoftRow` / `renderSoftHints` / `newManagerChrome`
/ `softRail`), the overlay pickers, and `color.go`. Migrating these means
rewriting ~70 call sites across `overlays.go`, `settings.go`,
`account_manager.go`, `contact_manager.go`, `compose.go`, `move_picker.go`,
`help.go`, `outbox.go`, `filter_manager.go` from the free-function +
`managerChrome` model to `tideui.Renderer` (`RenderSoftPanel`, `RenderSoftRow`,
`RenderSoftHints`, `SoftPanelOverlay`) and rewiring `overlayThemePicker` to
`tideui.ThemePicker`.

Blockers / decisions before starting Tier 2:

1. **tideui's colour helpers are unexported** (`readableText`, `accentReadableOn`,
   `mutedText`, `mixColors`, `adjustLightness`, `contrastRatio`,
   `selectionBgForRatio`). TideMail calls these 150+ times; `tideftp` hit the
   same wall and had to re-copy the maths (`tideftp/internal/ui/contrast.go`,
   with a TODO to delete it once tideui exports them). **Highest-leverage
   upstream change: export them from tideui** (~20-line diff) — unblocks both
   consumers. Until then TideMail keeps its own `color.go` (fine — it's stable).
2. TideMail's `Styles` has ~40 mail-specific fields (`ArticleUnread`, `FeedItem`,
   `ContentBody`, `PaneHeaderActive`, …) built by its own `BuildStyles`; tideui's
   `Styles` is generic. TideMail keeps its `BuildStyles` regardless — only the
   soft-modal chrome moves to `Renderer`.
3. Settings-screen form primitives (`renderSoftToggle` / `renderSoftPicker` /
   `renderSoftDropdown` / `form_render.go`) aren't in tideui's public API. Either
   keep them local or upstream them.
4. Tier 2 needs golden/snapshot coverage across the ~15 overlays before the
   rewrite, to catch visual regressions.

Estimated Tier 2: ~1–1.5 weeks, medium risk. Tier 3 (TideMail's `internal/ui`
becomes a thin tideui consumer like tideftp — reconcile both `Styles` models,
upstream status bar / focus line / forms, migrate every call site): ~3–4 weeks.

## Verification (this branch)

`gofmt -l`, `CGO_ENABLED=0 go build ./...`, `go vet ./...`, and
`go test ./... -race` are all clean. All 19 themes still pass
`TestAllThemesPassContrastChecks` and
`TestAllThemesFocusedPaneBorderMeetsContrastFloor`.
