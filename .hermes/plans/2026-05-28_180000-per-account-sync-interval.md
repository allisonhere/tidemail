# Plan: Per-Account Auto-Sync Interval

## Goal

Each IMAP account gets a configurable sync interval, so accounts refresh automatically in the background without manual `f`/`F` presses.

## Current State

- Sync is entirely manual: `f` syncs selected mailbox, `F` syncs all, `f` on Unified Inbox syncs all inboxes (just added)
- No timer/interval mechanism exists
- `AccountConfig` has no interval field
- Account manager form has fields for Name, Color, IMAP, SMTP, User, Pass, From — no interval

## Design

### Config

Add `sync_minutes` int to `AccountConfig`:

```go
SyncMinutes int `toml:"sync_minutes"` // 0 = disabled
```

Default: `0` (no auto-sync — opt-in, no surprise bandwidth/API usage).

### Timer Mechanism

Two approaches:

**A — Per-account timers (`tea.Every` per account)**  
Launch one `tea.Every` per account with its configured interval. Cleanest separation, easy to start/stop individually. Downsides: N timers for N accounts (negligible overhead for <10 accounts).

**B — Single polling loop with per-account due checks**  
One `tea.Every` running at the minimum interval across all accounts (or e.g. every 30s). On each tick, check which accounts are past due and sync them. Fewer timers, but more bookkeeping.

Recommendation: **Approach A** — simpler code, less state to track. The timer count is tiny.

### Implementation Steps

1. **Config**: Add `SyncMinutes int` to `AccountConfig` in `config.go`, wire into `DefaultAccountConfig`

2. **Settings UI**: Add `amFieldSyncInterval` to the account manager form fields, a textinput, focus handling, save/load in `buildCfg()`/`populateFormFrom()`

3. **Model timer management**:
   - In `NewModel`, after loading config, start timers via `startSyncTimers()`
   - On config save (AccountSavedMsg), restart timers
   - Define `autoSyncMsg` type containing the account ID
   - Handler: look up the account, sync its INBOX (or all mailboxes?)
   - On app quit, timers cancel automatically (bubbletea cleanup)

4. **What to sync**: Sync only INBOX for auto-refresh (new mail detection). Full mailbox syncs (`F`) remain manual.

### Files Changed

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `SyncMinutes` to `AccountConfig` |
| `internal/ui/account_manager.go` | Add field, input, focus, save/load, form rendering |
| `internal/ui/model.go` | Timer start/restart logic, auto-sync message handler |
| `internal/ui/msgs.go` | `AutoSyncMsg` type |

### Edge Cases

- **Disabled (0)**: No timer started. Default behavior preserved.
- **Config change**: Restart all timers on config save/account edit.
- **App background**: Bubbletea timers keep running while app is focused; no special handling needed for TUI.
- **Sync failure**: Log/set status, don't crash. Next tick will retry.
- **Rapid config changes**: Debounce by cancelling old timers before starting new ones (bubbletea handles this via command replacement).

### Tests

- Unit test for config round-trip (default → save → load)
- Timer message type test
- Account manager form test for new field

### Risks

- Network usage: auto-sync every 1 minute with large mailboxes could be chatty. Mitigated by 0 default and reasonable min (suggest 1 min minimum, or just trust the user).
- Gmail API rate limits: IMAP SELECT/FETCH on short intervals might trigger Gmail's rate limiting. Suggest minimum of 2-3 minutes for Gmail accounts in docs.
