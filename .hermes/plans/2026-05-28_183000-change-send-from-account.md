# Plan: Change Send-From Account While Composing

## Goal

When composing a new message, let the user pick which account the email is sent from, instead of always using the first configured account. Reply should still default to the correct account (it already does), but new compose should offer a choice.

## Current State

- `ComposeModel` stores a single `accountCfg config.AccountConfig` set at creation time
- **New compose** (`c` key, command palette): hardcodes `m.cfg.Accounts[0]`
- **Reply** (`r` key): uses `m.accountCfgForMailbox(msg.MailboxID)` — correct already
- Sender is shown in the header: `"COMPOSE  ◉ user@domain"`
- SMTP config (`smtp_host`, `smtp_port`, `user`, `password`, `from`) is per-account in `AccountConfig`
- Tab cycling: To → CC → Subject → Body
- Compose receives `accountCfg` at creation and never changes it

## Approach Comparison

### Option A — Dedicated "From" field in compose (new focusable field)

Add `composeFieldFrom` before To: `From → To → CC → Subject → Body`. Show a selector that cycles through accounts.

**Pros:** Discoverable during tab cycle; shows the full account list  
**Cons:** Adds a whole new tab-stop; extra visual field for what's shown in the header; changes tab cycle length (existing users may fat-finger past To)

### Option B — Keybinding-only cycle (no visible field)

Add a keybinding (e.g. `ctrl+u`) that cycles accounts. Header updates.

**Pros:** Zero UI footprint; fast to implement  
**Cons:** Hidden — user has to know about it or see it in the action bar; no persistent visual of what's selectable

### Option C — Visible sender row + keybinding to cycle (recommended)

Show a non-tabbable "From" row similar to the other fields (with label + sender address), and bind a key to cycle the sender. The header still shows the sender too. Action bar hint shows the keybinding.

**Pros:** Visual feedback; no tab-cycle disruption; sender is always visible in the field area (consistent with other compose fields)  
**Cons:** Slightly more vertical space used; requires a new keybinding

## Recommended Approach (Option C)

### Step 1 — Store all accounts in ComposeModel

Add to `ComposeModel`:
```go
accounts     []config.AccountConfig
accountIndex int  // index into accounts for current sender
```

`NewCompose` takes `accounts []config.AccountConfig` instead of a single `AccountConfig`. Defaults `accountIndex = 0` if the current mailbox's account isn't found, or remains `0` (first account).

`NewReply` takes all accounts but pre-selects the account that matches the replied-to message. If not found, defaults to first.

### Step 2 — Add "From" row rendering

In `ComposeModel.View()`, before the To row, render a "From" row:
```
▎ From  allie@alliehere.com
```
Styled with a muted label and the account name/email. Not focusable, not tab-stoppable. Uses `renderComposePanelRow` without focus highlight.

### Step 3 — Add keybinding to cycle sender

Add `composeNextAccount` / `composePrevAccount` keybinding to the compose key hander (e.g. `ctrl+shift+u` / `ctrl+u`).

When triggered:
1. Advance `accountIndex` (wrap around)
2. Update the displayed header and From row
3. The `send()` function uses `c.accounts[c.accountIndex]` instead of `c.accountCfg`

Add a key to `KeyMap`:
```go
CycleSender  key.Binding
```

Bound to something unused: `ctrl+left` / `ctrl+right` or `m` `M` (not the most intuitive). Could also use `ctrl+u` / `ctrl+shift+u` since those aren't claimed in compose mode.

### Step 4 — Wire up in model.go

- `keyMatches(msg, m.keys.Compose)`: pass all accounts, not just `m.cfg.Accounts[0]`
- `keyMatches(msg, m.keys.Reply)`: pass all accounts, set starting index to match replied-to account
- Compose model's `Update` handles the cycle-sender key
- `send()` uses `c.accounts[c.accountIndex]` for SMTP config

### Step 5 — Show hint in action bar

Add `"ctrl+u", "sender"` to the compose action keys (or similar for the chosen binding).

## Files changed

| File | Change |
|------|--------|
| `internal/ui/compose.go` | `ComposeModel` struct: add `accounts` + `accountIndex` fields; update `NewCompose`/`NewReply` signatures; add From row in View; add cycle-sender key handler in Update; update `send()` to use selected account |
| `internal/ui/compose_test.go` | Update `NewCompose` call sites (pass account slice) |
| `internal/ui/model.go` | Update compose trigger sites: pass `m.cfg.Accounts` instead of just one |
| `internal/ui/keys.go` | Add `CycleSender` key binding (or reuse existing) |

## Tests / validation

- `go build ./...` / `go test ./...` / `go vet ./...` all pass
- New compose opens with first account selected
- Cycle key switches the displayed sender (header + From row)
- `send()` actually uses the selected account's SMTP config (manual test)
- Reply still auto-selects the correct account

## Risks

- The `accounts` slice won't have duplicate entries — if account configs share the same "from" address, the user sees duplicates. This is a config issue, not code.
- Adding to `ComposeModel` means more data to thread through the reply flow. The reply flow already does `NewReply(*cur, acfg)` — we change it to pass all accounts + pre-select.
- All existing compose call sites need updating to match the new signature (compile-time check, easy).
