# Plan: Advanced Settings with Log Viewer

## Goal

Add an "Advanced" section in Settings with a scrollable log viewer so users can review errors and status messages that flash by too quickly on the status bar.

## Current state

- Status bar shows temporary messages (success/error) that auto-clear after a few seconds
- No persistence — once the message clears, it's gone forever
- Debugging issues (sync failures, IMAP errors, keychain problems) requires catching them live

## Proposed approach

### Log buffer

Add a ring buffer on the Model that captures every `setStatus` call:

```go
type logEntry struct {
    Time    time.Time
    Message string
    IsError bool
}
logBuffer []logEntry  // ring buffer, max 100 entries
```

Every call to `setStatus(msg, isError)` appends to the log buffer. Status bar behavior unchanged — messages still flash and clear normally.

### Advanced settings section

Add `ssAdvanced` to the settings sections enum. Renders:
- "View Logs" — opens a scrollable log viewer overlay
- Future: any other power-user settings

### Log viewer overlay

Modal overlay (like the theme picker or help) that shows the log buffer:
- Header: "LOGS" with count
- Scrollable list of entries, newest first
- Each entry: timestamp + message, dimmed for normal, red for errors
- Keyboard: `j`/`k` or `↑`/`↓` to scroll, `esc`/`q` to close

### Log capture

Modify `setStatus` to also push to the log buffer:

```go
func (m *Model) setStatus(msg string, isError bool) {
    m.statusMsg = msg
    m.statusIsError = isError
    m.logBuffer = append(m.logBuffer, logEntry{Time: time.Now(), Message: msg, IsError: isError})
    if len(m.logBuffer) > 100 {
        m.logBuffer = m.logBuffer[1:]
    }
}
```

## Files changed

| File | Change |
|------|--------|
| `internal/ui/model.go` | Add logBuffer, modify setStatus, add log viewer overlay |
| `internal/ui/settings.go` | Add ssAdvanced section + view |
| `internal/ui/keys.go` | No new keys needed (uses settings navigation) |

## Step-by-step

1. Add `logEntry` type and `logBuffer` to Model struct
2. Modify `setStatus` to push entries to log buffer
3. Add `ssAdvanced` to settings sections
4. Add "View Logs" to Advanced settings view
5. Add `overlayLogViewer` overlay mode
6. Render log viewer modal (scrollable list, timestamp + message)
7. Handle scroll + close keys

## Effort

~2 hours. Most work is the log viewer overlay rendering.

## Open question

Where in settings to put "Advanced"? Options:
- New bottom section after "About"
- Inside "About" as a hidden gem
- Separate "Advanced" section between "AI" and "About"

Recommend: new "Advanced" section between "AI" and "About". It's a natural progression and won't clutter existing sections.
