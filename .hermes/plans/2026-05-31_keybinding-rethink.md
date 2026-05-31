# Plan: Keybinding Rethink v2

## Changes from v1

User wants `ctrl+s` for "sync all" in addition to the `f→s`, `x→f`, `s→v` remap.

## Final layout

| Key | Action | Context |
|-----|--------|---------|
| `f` | mark read | main (was `x`) |
| `s` | sync mailbox | main (was `f`) |
| `v` | AI summary | main (was `s`) |
| `ctrl+s` | sync all | main (new) |
| `F` | sync all | main (keep as alt) |
| `r` | reply | main |
| `w` | forward | main |
| `c` | compose | main |
| `a` | archive | main |
| `d` | delete | main + account mgr |

Overlay keys unchanged — `ctrl+s` still means "save/send" in overlays because overlay handlers intercept keys before the main handler sees them:

| Key | Action | Context |
|-----|--------|---------|
| `ctrl+s` | send email | compose overlay |
| `ctrl+s` | save account | account manager overlay |
| `ctrl+s` | save settings | settings overlay |

## Why `ctrl+s` doesn't conflict

The key dispatch chain is:

```
handleKey → handleOverlayKey (if overlay active)
          → handleMainKey     (if no overlay)
```

Overlay handlers use raw string checks (`km.String() == "ctrl+s"`) in their own Update methods, not the global `keyMatches(m.keys.SyncAll, msg)`. So when compose/settings/account-manager is open, `ctrl+s` is consumed by the overlay handler and never reaches `handleMainKey`. When no overlay is open, `ctrl+s` reaches `handleMainKey` where it fires sync-all.

This is the same pattern `ctrl+d` already uses: save-attachments in main view, send-email in compose overlay.

## Files to change

### `internal/ui/keys.go` — 4 lines

```
Sync:        "f" → "s"          help: "sync mailbox"
SyncAll:     "F" → "F", "ctrl+s"  help: "sync all"  
MarkRead:    "x" → "f"          help: "mark read"
Summary:     "s" → "v"          help: "AI summary"
```

Adding `"ctrl+s"` as a secondary key for SyncAll — `key.WithKeys("F", "ctrl+s")`.

### `internal/ui/model.go` — 0 lines

Go identifiers don't change (`m.keys.Sync`, `m.keys.MarkRead`, `m.keys.SyncAll`, `m.keys.Summary`). Status bar hints use `b.Help()` which pulls from the binding — auto-updates. The `keyMatches` calls use the same identifiers.

### `internal/ui/help.go` — 0 lines

Uses `bind(keys.Sync)` etc. — auto-updates from binding Help().

### `internal/ui/compose.go` — 0 lines

`km.String() == "ctrl+s"` is a raw string check, not a key binding. Adding `ctrl+s` to SyncAll doesn't affect it — compose overlay intercepts first.

### `internal/ui/settings.go` — 0 lines

Same — raw `"ctrl+s"` check in settings handler.

### `internal/ui/account_manager.go` — 0 lines

Same — raw string or own key binding check.

## Step-by-step

1. Edit `keys.go`:
   - `Sync`: keys `"s"`, help `"s", "sync mailbox"`
   - `SyncAll`: keys `"F", "ctrl+s"`, help `"F/ctrl+s", "sync all"`
   - `MarkRead`: keys `"f"`, help `"f", "mark read"`
   - `Summary`: keys `"v"`, help `"v", "AI summary"`
2. `go build ./...`
3. `go test ./...`
4. Smoke: open tidemail, verify `f` marks read, `s` syncs one, `ctrl+s` syncs all, `v` opens summary, `ctrl+s` still sends in compose

## Risk

Zero. The three overlay contexts that use `ctrl+s` (compose, settings, account manager) all intercept keys before the main handler runs. No behavior change in overlays.
