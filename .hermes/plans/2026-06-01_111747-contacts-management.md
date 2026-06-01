# Contacts: autocomplete management & pruning

## Goal

Improve the compose autocomplete address book — currently a raw `SELECT DISTINCT` over every From/To/CC field in all messages, which surfaces every automated email, newsletter, and one-off address with no way to prune or edit.

## Current state

| Component | File | What it does |
|---|---|---|
| `db.ListAddresses()` | `internal/db/messages.go:257` | `SELECT DISTINCT addr FROM (from_addr UNION to_addr UNION cc_addr)` over all messages |
| `Model.loadAddressBookCmd()` | `internal/ui/sync.go:105` | Async tea.Cmd loads addresses on startup |
| `AddressBookLoadedMsg` handler | (model.go) | Sets `m.addressBook` in model state |
| `ComposeModel.SetAddressBook()` | `internal/ui/compose.go:108` | Sets autocomplete suggestions on To/CC text inputs |
| Bubbletea textinput autocomplete | (bubbles dep) | Tab to accept suggestion, filtering as you type |

**Problems:**
1. No way to remove unwanted addresses from suggestions (newsletters, noreply@, one-offs)
2. No way to add custom entries manually (favorite contacts not yet seen in messages)
3. Addresses purely derived from message data — can't persist custom names/labels

## Options

---

### Option 1: Hide-as-you-go (lightweight)

Add a `hidden_addresses` table. When the user sees an unwanted suggestion in compose, a keybinding (e.g. `ctrl+d`) marks it as hidden. `ListAddresses` excludes hidden entries.

**Schema:**
```sql
CREATE TABLE hidden_addresses (
    addr TEXT PRIMARY KEY
);
```

**Changes:**
- `db.HideAddress(addr)` / `db.ListAddresses()` filters OUT addresses in `hidden_addresses`
- `db.ListAddresses()` adds a LEFT JOIN or subquery to exclude hidden
- Compose keybinding: `ctrl+d` on the To/CC field calls `db.HideAddress(ctx.CurrentSuggestion())`
- Feedback: a brief status message "Hidden noreply@..." 

**Pros:** Minimal changes. Addresses still auto-populate from messages. Simple UX — one keypress to dismiss.
**Cons:** No way to undo/restore without SQL directly. No display names or favorites. Accidentally hiding an address means it's gone without a settings UI to view hidden list.

---

### Option 2: Address book table (structured)

Add a proper `contacts` table with display name, address, and metadata. The auto-derived list becomes a seed, but the user can curate.

**Schema:**
```sql
CREATE TABLE contacts (
    id INTEGER PRIMARY KEY,
    addr TEXT UNIQUE NOT NULL,
    display_name TEXT DEFAULT '',
    source TEXT DEFAULT 'auto',  -- 'auto' or 'manual'
    use_count INTEGER DEFAULT 0,
    last_used TEXT
);
```

**Changes:**
- `db.Migrate()`: create contacts table, seed from existing message addresses
- `db.ListAddresses()`: queries contacts table instead of messages
- `db.AddContact(addr, name)` / `db.RemoveContact(addr)`
- On compose send: increment `use_count`, update `last_used`
- Settings → a new "Contacts" section to view/edit/delete entries
- Compose: keybinding (`ctrl+d`) to remove from suggestions with confirmation

**Pros:** Full curation. Display names. Usage tracking enables smart sorting (most-used first). Can add contacts manually.
**Cons:** More code. Migration from message-addresses to contacts table. Need a settings UI for viewing/editing contacts list.

---

### Option 3: Frequency-filtered auto-populate (smart defaults)

Keep deriving from messages but only show addresses that appear ≥ N times or have been manually interacted with. Add a "dismiss" action to temporarily hide individual addresses.

**Changes:**
- `db.ListAddresses()`: add `GROUP BY addr HAVING COUNT(*) >= 2` or a `use_count` threshold
- Add `dismissed_addresses` table for temporary hiding (cleared on restart or persistent)
- Compose keybinding: `ctrl+d` to dismiss

**Pros:** Filters out one-off addresses automatically without user action. Very little code.
**Cons:** Still can't add custom entries or display names. New contacts with one email won't show initially. Threshold is a guess — what's "frequent" to one user isn't to another.

---

### Option 4: Hybrid (Option 1 + contact list in settings)

Start with Option 1's `hidden_addresses` table for quick dismissal, plus a settings pane to view/manage the hidden list (un-hide individual addresses, see the full list).

**Schema (same as Option 1):**
```sql
CREATE TABLE hidden_addresses (addr TEXT PRIMARY KEY);
```

**Changes (Option 1 + extras):**
- All changes from Option 1
- Settings → a new "ADDRESSES" section or an entry under ADVANCED
- Settings pane: scrollable list of hidden addresses with an "Unhide" action per entry
- Status bar message: "Hidden noreply@... (undo with ctrl+z or manage in Settings)"

**Pros:** Same simplicity as Option 1, but adds discoverability and undo. No migration complexity.
**Cons:** Still no display names or favorites. Just a hide/un-hide system.

---

## Recommendation

**Option 4 (Hybrid)** is the sweet spot for this request. The user's primary pain point is "too many unwanted addresses" — a simple hide mechanism solves that immediately. Adding a settings view for the hidden list gives them control and the ability to undo mistakes. Option 2 is the "right" long-term solution but is a significantly larger feature that touches migration, a full settings screen, and compose entry tracking.

If you want display names and favorites eventually, Option 2 can be built on top of Option 4 later — the `hidden_addresses` table becomes a flag in the contacts table.

---

## Files likely to change

| File | Changes |
|---|---|
| `internal/db/messages.go` | Add `hidden_addresses` table in migration; modify `ListAddresses()` to filter hidden |
| `internal/db/db.go` | Migration version bump if needed |
| `internal/ui/compose.go` | Keybinding for hide action; access to db or model to call hide |
| `internal/ui/model.go` | Wire addressBook into compose on open; handle hide events |
| `internal/ui/settings.go` | New ADDRESSES section (or entry under ADVANCED) to view/manage hidden list |
| `internal/ui/account_manager.go` | (maybe) `managerChrome` already covers settings rendering |

## Tests

- `ListAddresses()` with hidden addresses: verify they're excluded
- Settings view: verify hidden addresses are listed
- Unhide action: address reappears in autocomplete
