# Plan: Full Email Headers Toggle (ctrl+h)

## Goal

Allow viewing the full email headers (From, To, CC, Reply-To, Message-ID, Date) in the content pane via `ctrl+h`. Headers render above the message body in a dimmed metadata block, togglable on/off.

## Current State

- Content pane renders: `Subject` (styled bar) + `Date  From: sender` (meta line) + `body`
- Only Subject, Date, and From are shown
- Full headers (To, CC, Reply-To, Message-ID) exist in the `db.Message` struct but are never displayed
- `db.Message` has fields: `Subject`, `From`, `To`, `CC`, `ReplyTo`, `MessageID`, `Date`

## Approach

Add a `showHeaders bool` toggle. When enabled, render an extra block above the message body with all header fields in a dimmed/compact format. `ctrl+h` in the content pane toggles it.

### State

Add to Model struct:
```go
contentShowHeaders bool
```

Initialize `false` in `setViewportMessage` (reset when switching messages).

### Keybinding

Add to `keys.go`:
```go
ToggleHeaders: key.NewBinding(key.WithKeys("ctrl+h"), key.WithHelp("ctrl+h", "headers")),
```

### Handler

In the main Update switch, after the `ContentSearch` handler:
```go
case keyMatches(msg, m.keys.ToggleHeaders):
    if m.focused == paneContent && m.contentMessageID != 0 {
        m.contentShowHeaders = !m.contentShowHeaders
        if cur := m.currentContentMessage(); cur != nil {
            m.setViewportMessage(*cur)
        }
    }
    return m, nil
```

`setViewportMessage` rebuilds the content, so the toggle takes effect immediately.

### Rendering

In `renderMessageContent`, modify the content building:

If `m.contentShowHeaders` is true, insert a headers block between the meta line and the body:

```go
var headerLines []string
accent := m.styles.Theme.BorderFocus
dim := m.styles.Theme.Dimmed

headers := []struct{ label, value string }{
    {"Date",      msg.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700")},
    {"From",      msg.From},
    {"To",        msg.To},
    {"CC",        msg.CC},
    {"Reply-To",  msg.ReplyTo},
    {"Message-ID", msg.MessageID},
}

for _, h := range headers {
    if h.value == "" {
        continue
    }
    line := lipgloss.NewStyle().
        Foreground(dim).
        Width(bodyWidth).
        Render(fmt.Sprintf("%-12s %s", h.label+":", h.value))
    headerLines = append(headerLines, line)
}
headersBlock := strings.Join(headerLines, "\n")
```

Then prepend the headers block to the body content:

```go
if m.contentShowHeaders {
    content = headersBlock + "\n" + content
}
```

Keep the existing Subject bar and Date/From meta line unchanged — the full headers add more detail below them.

### Pane Hint

Update `renderPaneHint` for `paneContent` to show the binding:
```
progress  k/↑/j/↓ line  / find  r reply  ctrl+h headers  esc back
```

### Files Changed

| File | Change |
|------|--------|
| `internal/ui/model.go` | Add `contentShowHeaders` field; add `ToggleHeaders` handler; modify `renderMessageContent` to render headers block; update `setViewportMessage` to reset state; update pane hint |
| `internal/ui/keys.go` | Add `ToggleHeaders` key binding |

### Tests / Validation

- `go build ./...`, `go test ./...`, `go vet ./...` all pass
- `ctrl+h` in content pane toggles headers on/off
- Headers show only non-empty fields (empty CC/Reply-To/Message-ID omitted)
- Switching messages resets headers to hidden
- `ctrl+h` does nothing when not in content pane

### Risks

- **Wide header values** (long Message-ID, multi-RCPT To/CC) may overflow the content width. Use `truncate()` or let the viewport handle horizontal scroll. For now, let `lipgloss.Width()` truncate naturally — the viewport doesn't scroll horizontally, so long values clip.
- **Date format** — use full RFC822 format in headers view (`Mon, 02 Jan 2006 15:04:05 -0700`) vs the compact format in the meta line. This gives the full picture when toggled on.
