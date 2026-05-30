# Plan: Fix Update Manual Command Hidden by Dismiss

## Problem

When a user dismisses an update (`dismissed_version = "v0.2.0"` in config), the settings view hides the manual install command entirely. The user sees "admin permission required" but no shell command to copy-paste. This happens because `effectiveManualCommand()` returns `""` whenever `updateDismissed` is true — which blocks both the real manual command from a failed auto-install AND the fallback `curl | sh` one-liner.

## Root cause

In `effectiveManualCommand()` (`model.go` line 4115):

```go
func (m Model) effectiveManualCommand() string {
    if s := strings.TrimSpace(m.updateInstall.ManualCommand); s != "" {
        return s  // real manual command from Install() failure
    }
    if m.updateDismissed {
        return ""  // ← blocks both branches!
    }
    // ... fallback: suggested curl | sh script
}
```

When `updateDismissed` is true, the function returns `""` before even reaching the fallback. The settings view checks `s.update.manualCommand != ""` (line 1387) and skips rendering the entire manual command block.

## Fix

Move the dismiss check so it only applies to the fallback, not the real manual command:

```go
func (m Model) effectiveManualCommand() string {
    if s := strings.TrimSpace(m.updateInstall.ManualCommand); s != "" {
        return s  // always show real manual command
    }
    if m.updateDismissed {
        return ""  // suppress fallback when dismissed
    }
    // ... fallback
}
```

Wait — the real manual command (`m.updateInstall.ManualCommand`) is ALREADY non-empty when `RequiresManual` is true. The dismiss check at line 4115 is AFTER the initial check for `m.updateInstall.ManualCommand`. So the real command IS preserved. The issue is that when the user hasn't tried to install yet (they're just checking from Settings), `m.updateInstall.ManualCommand` is empty, and they fall through to the dismiss guard.

The real fix: in `UpdateInstalledMsg` handler, when `RequiresManual` is true, the user just attempted an install. The dismiss should be cleared because the user clearly wants the update. This is the same logic as the `pendingUpdateInstall` override.

Or simpler: after the `RequiresManual` branch sets state, also clear the dismiss:

```go
case UpdateInstalledMsg:
    ...
    if msg.Result.RequiresManual {
        m.updateState = updateStateNeedsElevation
        m.updateDismissed = false                    // add this
        m.cfg.Updates.DismissedVersion = ""           // add this
        m.syncSettingsUpdateState()
        ...
    }
```

This way when the install fails with elevation needed, the manual command is always visible because the dismiss flag was cleared. The user literally just tried to install — they're not ignoring it.

## Files changed

| File | Change |
|------|--------|
| `internal/ui/model.go` | Clear `updateDismissed` and `DismissedVersion` when install requires manual elevation |

## Effort

2 lines. Trivial.
