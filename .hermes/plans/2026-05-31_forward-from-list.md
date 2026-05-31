# Plan: Forward from Message List

## Goal

Pressing `f` on the message list (not just in the content pane) opens compose pre-filled as a forward of the highlighted message.

## Current behavior

Forward only works from the content pane (`m.focused == paneContent`). On the message list, `f` does nothing.

## Change

In `model.go` lines 1019-1027, extend the Forward handler to also match when focused on the messages pane:

```go
case keyMatches(msg, m.keys.Forward):
    var cur *db.Message
    if m.focused == paneContent && m.contentMessageID != 0 {
        cur = m.currentContentMessage()
    } else if m.focused == paneMessages && len(m.filteredMessages) > 0 {
        cur = &m.filteredMessages[m.messageCursor]
    }
    if cur != nil {
        acfg := m.accountCfgForMailbox(cur.MailboxID)
        m.compose = NewForward(*cur, acfg, m.cfg.Accounts)
        m.overlay = overlayCompose
    }
    return m, nil
```

This follows the same pattern Delete uses — it checks `m.focused != paneAccounts` and operates on the cursor-selected or content-visible message.

## Files changed

One file, one block: `internal/ui/model.go` lines 1019-1027.

## Should Reply get the same treatment?

Reply (`r`) has the same limitation — only works from content pane. Could extend it the same way. But the user only asked about Forward. Can add Reply later if desired — same pattern, copy-paste.

## Validation

- `go build ./...`
- `go test ./...`
- Smoke: navigate message list with `j/k`, press `f` → compose opens with "Fwd:" subject and quoted original + attachments
