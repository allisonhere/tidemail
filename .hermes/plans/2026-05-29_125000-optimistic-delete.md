# Plan: Optimistic Deletion with Background IMAP Sync

## Goal

Eliminate the perceived lag when deleting messages. The IMAP delete (STORE \Deleted + EXPUNGE) takes 1-3 seconds, and currently the UI blocks entirely during that time. Make the message vanish instantly from the UI, then reconcile with the server in the background.

## Current behavior

```
User presses d (or D for multi-select)
  → deleteMessageCmd closure:
      1. IMAP connect (TCP + TLS + LOGIN = ~500ms-1s)
      2. IMAP STORE \Deleted + EXPUNGE (~200-500ms)
      3. DB DeleteMessage (~5ms)
      4. Tea.Batch([...cmds]) → MessageDeletedMsg
  → MessageDeletedMsg handler:
      5. removeMessageFromMemory() → message disappears from UI
      6. adjustMailboxUnreadCount()
      7. status bar: "deleted"
```

Total blocking time: ~700ms-2s per message. With multi-select (N messages), `tea.Batch` runs them in parallel goroutines, so N connections at once — each still blocks the UI through the tea.Cmd closure (tea serializes all messages back through the update loop).

## Proposed approach

### Optimistic delete with rollback

Replace the single `deleteMessageCmd` with two-phase operation:

**Phase 1 — Instant (in the key handler / update loop):**
- Remove the message from `m.filteredMessages` immediately (`removeMessageFromMemory`)
- Delete from local DB immediately (`database.DeleteMessage`)
- Adjust unread count immediately
- Show "deleted" status immediately

**Phase 2 — Background IMAP sync (fire-and-forget goroutine):**
- Spawn a goroutine that connects to IMAP and issues the delete
- If it succeeds: nothing more needed (message already gone from UI + DB)
- If it fails: re-insert the message into the DB, re-insert into `m.filteredMessages`, show error status

### Key design decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| DB delete timing | Optimistic (immediate) | If IMAP later fails, re-insert to DB; DB ops are ~5ms, rollback cost is low |
| Re-insertion on failure | Full re-insert | The message data is already in memory (the `db.Message` struct); we can stash it in a closure |
| Error signal | Status bar + restore message | Don't toast-and-forget — the message should visibly reappear |
| Multi-select | Same two-phase pattern per message | Each one fires its own background goroutine, messages vanish instantly from the list |

### Risks

1. **Rollback complexity** — if the app crashes between DB delete and IMAP response, the message is permanently lost from the local DB. It still exists on the server, so a full re-sync will pick it up. Acceptable.

2. **Race with auto-sync** — if auto-sync fires while an optimistic delete is still pending, the message could be re-fetched from the server and re-inserted into the DB. Mitigation: track pending deletes by UID+mailbox; skip re-insertion for pending UIDs.

3. **Duplicate status lines** — "deleted" status could overlap with an error status from the background goroutine. Mitigation: sequential status updates with `clearStatusCmd` chaining.

### Files changed

| File | Change |
|------|--------|
| `internal/ui/model.go` | Rewrite `deleteMessageCmd` → optimistic + background IMAP; update `MessageDeletedMsg` handler; add pending-delete tracking |
| `internal/ui/msgs.go` | Maybe add `MessagePendingDeleteMsg` or reuse `MessageDeletedMsg` with a new field |
| `internal/db/db.go` or `internal/db/messages.go` | Maybe add `UndeleteMessage` for rollback |

### Tests

- `TestDeleteMessage_Optimistic` — verify message removed from filteredMessages before IMAP runs
- `TestDeleteMessage_IMAPFail_Rollback` — simulate IMAP error, verify message re-appears
- `TestDeleteMessage_MultiSelect` — batch delete, verify all messages vanish instantly
- `TestDeleteMessage_RaceWithSync` — pending UID prevents re-fetch

### Alternatives considered

**Alternative 1: Just make IMAP faster.** Share one IMAP connection across operations (pooled connection). Doesn't eliminate the latency, just reduces it. More complex (connection lifecycle, reconnection).

**Alternative 2: Non-optimistic with spinner.** Show a spinner/indicator per message while deleting. Still feels slow — moves the problem, doesn't solve it.

**Alternative 3: Batch IMAP deletes into one connection.** Instead of connecting N times for N messages, connect once per mailbox, send all STORE commands, then EXPUNGE once. Combine with optimistic removal for the best of both worlds.
