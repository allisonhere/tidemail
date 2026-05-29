# Plan: Multi-Select Messages via Space Bar

## Goal

Allow selecting multiple messages in the Messages pane using the space bar, then performing bulk operations on the selection: delete, archive, move to mailbox, and mark read/unread.

## Current State

- Only one message is tracked at a time via `messageCursor int`
- All actions (`a` archive, `d` delete, `x` mark read) operate on `filteredMessages[messageCursor]` only
- `Space` key does nothing in paneMessages (only used for sidebar toggle)
- No "move to mailbox" action exists at all
- Message rows render with: `[dot] [subject] [age]`
- Command palette has single-message items: archive, delete, toggle-read

## Proposed Approach

**Option A — Selection set** (recommended): a `map[int64]bool` keyed by message ID. Space toggles the current message in/out. Actions operate on the selection when non-empty, else fall back to the single cursor message.

**Option B — Range + start anchor:** track a selection-origin cursor position + active flag. Space selects the range from origin to current cursor. Closer to desktop email clients but more complex state management and harder to implement correctly in a TUI with scrolling/filtering.

**Going with Option A** — it's simpler, maps well to terminal MUA conventions (mutt, alpine, aerc), and covers the user's stated use case (bulk operations) with less surface area for bugs.

## Step-by-step Plan

### Phase 1 — Selection state model

**Model changes** (`internal/ui/model.go`):
- Add `selectedMessages map[int64]bool` field (initialized as empty in NewModel)
- Add `selectionActive bool` (true when any message is selected)

**Selection clear triggers** (add at each site):
- Mailbox switch (`setMailbox` / sidebar selection change)
- Search/filter change
- Mailbox sync that replaces messages
- `esc` with selection active (clear selection first, exit on second press)

### Phase 2 — Space bar toggle

**Key handling** (in the paneMessages-focused handler):
```go
case keyMatches(msg, m.keys.Space):
    if m.focused == paneMessages && len(m.filteredMessages) > 0 {
        msg2 := m.filteredMessages[m.messageCursor]
        if m.selectedMessages[msg2.ID] {
            delete(m.selectedMessages, msg2.ID)
        } else {
            m.selectedMessages[msg2.ID] = true
        }
        // Auto-advance cursor to next message for rapid multi-select
        if m.messageCursor < len(m.filteredMessages)-1 {
            m.messageCursor++
        }
        return m, nil
    }
```

Space already exists in `KeyMap` — no new key binding needed, just need to wire it in the paneMessages context. Currently it only fires in the paneAccounts context (sidebar toggle).

### Phase 3 — Selection visual indicator

**Message row rendering** (`renderMessagesPane`):
- When a message is in `selectedMessages`, add a selection marker in the prefix
- Replace the dot prefix with `◆` (selected, U+25C6) or `▣` (selected, U+25A3)
- Use the accent/selected-row foreground colour for the selection marker
- Consider a subtle highlight on selected rows (even when not the cursor) — maybe a distinct background shift similar to unread but in the opposite direction

```go
prefix := m.messageRowPrefix(msg2.Read)
if m.selectedMessages[msg2.ID] {
    prefix = "◆ "
    // Or use a colored/emphasized version of the existing prefix
}
```

### Phase 4 — Bulk action dispatch

**Modify existing action handlers** in `model.go`:

Each handler (`Archive`, `Delete`, `MarkRead`) should:
```go
if len(m.selectedMessages) > 0 {
    // Operate on all selected messages
    for _, msg2 := range m.filteredMessages {
        if m.selectedMessages[msg2.ID] {
            cmds = append(cmds, operationCmd(msg2))
        }
    }
    m.selectedMessages = nil  // clear selection
    return m, tea.Batch(cmds...)
}
// Fall back to single message at cursor
msg2 := m.filteredMessages[m.messageCursor]
return m, operationCmd(msg2)
```

Functions that need updating:
- `MarkRead` handler (~line 886)
- `Archive` handler (~line 895)  
- `Delete` handler (~line 902)

### Phase 5 — Move to mailbox

**Add "move" action** — this is new functionality and needs a target mailbox picker.

**Approaches:**
1. **Sidebar-style mailbox picker overlay** — list all mailboxes (excluding current), pick one, move all selected messages there
2. **Key + sidebar interaction** — press `m` (move) in messages pane, then click the target mailbox in the sidebar
3. **Command palette** — `move` command shows mailbox list

**Recommended: Sidebar-style mailbox picker overlay** (Option 1) — similar to the existing file picker pattern, lists mailboxes, Enter picks one, Esc cancels.

State additions:
```go
moveTargetPicker   []db.Mailbox
moveTargetCursor   int
moveTargetActive   bool
```

Key binding: `M` (shift+m) or `ctrl+m` for move.

### Phase 6 — Pane hint and command palette

**Pane hint** update (`renderPaneHint`):
```
k/↑/j/↓ move  space select  x read  a archive  d delete  M move  p command
```

**Command palette** items:
- `move` — enabled when selection active or hasMessage
- `select-all` — selects all visible messages
- `clear-selection` — clears selection

### Phase 7 — Select all / clear

Add convenience actions:
- `ctrl+a` in messages pane: select all visible messages
- `esc` in messages pane with active selection: clear selection
- `esc` in messages pane without active selection: escape to sidebar (current behavior)

## Files changed

| File | Changes |
|------|---------|
| `internal/ui/model.go` | Add `selectedMessages map[int64]bool` field; Space key handler in paneMessages; modify Archive/Delete/MarkRead for bulk; add move target picker state + handler; selection clear triggers; SelectAll action |
| `internal/ui/keys.go` | Add `MoveMessage` and `SelectAll` key bindings (or reuse existing Space + `ctrl+a`) |
| `internal/ui/model.go` (`renderMessagesPane`) | Selection visual indicator in message rows |
| `internal/ui/model.go` (`renderPaneHint`) | Update hint for space/M keys |
| `internal/ui/model.go` (`commandItems`) | Add move, select-all, clear-selection |
| `internal/ui/model.go` (`executeCommand`) | Wire new command IDs |

## Tests / validation

- `go build ./...`, `go test ./...`, `go vet ./...` all pass
- New tests in a `multiselect_test.go` or existing msg/command test files:
  - Space toggles message in/out of selection set
  - Bulk delete fires N commands instead of 1 when selection is populated
  - Selection clears on mailbox switch / search
  - Esc clears selection without exiting messages pane when selection is active
  - Move picker opens and target mailbox dispatches correctly

## Risks

- **Message IDs might be stale** after a sync. The `filteredMessages` slice may have been rebuilt. Selection by ID is still correct — the message rows reference by index, and we look up by ID in the selection map. If a synced message has a different ID, it won't be in the old selection. This is acceptable.
- **Selection UX in filtered views** — when switching to unread-only or search results, selection should clear to avoid accidentally operating on wrong messages.
- **Move target picker needs mailbox access** — need to pass available mailboxes (for the current account/all accounts) to the picker.
- **Perf** — iterating over `filteredMessages` to find selected ones is O(n) in the message count. For large mailboxes this is fine since it's a single pass on user action.
