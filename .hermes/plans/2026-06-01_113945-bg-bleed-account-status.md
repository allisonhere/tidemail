# Fix: remaining bg bleed from Width()-based padding

## Issue

`Width(width).Render(text)` in lipgloss can leave unpadded gaps when ANSI-styled text is narrower than its visual width calculation. Already fixed hints and compose status/quote lines. Two more remain.

## Remaining instances

| File | Line | Context |
|---|---|---|
| `account_manager.go` | 958 | Account detail status line: `.Width(width).Padding(0,1).Render(status)` |
| `account_manager.go` | 885 | Account list body wrapper (multi-line, probably fine but belt-and-suspenders) |
| `account_manager.go` | 1180 | Edit-account form body wrapper (same) |

## Fix

Replace `Width(width).Render(text)` with explicit gap padding, same pattern as previous fixes:

```go
rendered := style.Render(text)
if gap := width - lipgloss.Width(rendered); gap > 0 {
    rendered += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", gap))
}
```

Lines 885/1180 wrap multi-line bodies where individual rows are already full-width — skip those, they're safe.

## Files

- `internal/ui/account_manager.go` — line 958 only
