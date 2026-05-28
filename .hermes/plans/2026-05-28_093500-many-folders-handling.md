# Handling Many Gmail Folders / Labels in Tidemail

## Goal

Design a UX strategy for Gmail users with dozens or hundreds of labels (folders). The current
flat list under each account header doesn't scale — the sidebar becomes unusably long.

## Current context

- **Sidebar rendering**: `buildSidebarRows()` in `model.go:3443` groups mailboxes by account,
  sorts via `mailboxRank()` (Inbox→Sent→Drafts→Archive→Trash→Spam→other), renders as a flat
  list. Accounts themselves are collapsible (`collapsed[acc.ID]`).
- **Display names**: `cleanDisplayName()` strips `[Gmail]/` prefix. Nested labels like
  `Projects/Tidemail` are flattened — hierarchy info is lost.
- **IMAP structure**: Gmail exposes labels via IMAP as flat-ish folders with delimiter `/`.
  Nested labels appear as `Parent/Child`. System labels sit under `[Gmail]/` namespace.
- **Gmail's special boxes**: All Mail (contains every message), Starred (virtual), Important
  (virtual), Categories (Primary/Social/Promos/Updates/Forums) — these add noise.
- **User count**: A heavy Gmail user can have 50–200 labels, plus 10+ system folders.

## Proposed approach

A **layered strategy** combining three complementary features, ordered by implementation
complexity. No single technique solves the problem alone — together they cover light,
moderate, and heavy folder counts.

---

### Phase 1: Hide low-value system folders

**What**: Skip rendering mailboxes that are known to be uninteresting for most users.

**Which folders to hide by default** (with a toggle in Settings → Display to show all):
- `[Gmail]/All Mail` — every message is already in Inbox or another label. This is the
  single biggest source of "I have to scroll past this" pain.
- `[Gmail]/Starred` — equivalent to a filter on the Inbox. Only useful if Tidemail surfaces
  the starred flag in the message list (which it doesn't yet).
- `[Gmail]/Important` — Gmail's prediction-based importance marking. Not actionable.
- `[Gmail]/Spam` and `[Gmail]/Trash` — already handled by separate UI affordances
  (delete/spam actions, and the current rank sort puts them near the bottom).
- `[Gmail]/Categories/*` — Gmail's auto-categorization folders. Very noisy, rarely useful
  from IMAP clients.

**How**: Add a filter in `buildSidebarRows()` or before calling it. Gmail system folder
names are predictable (`[Gmail]/All Mail`, `[Gmail]/Starred`, etc.). The `cleanDisplayName`
already strips the prefix, but we need the raw `Name` to identify them at build time.

**Config option**: `display.hide_gmail_system_folders = true` (default: true). When false,
all folders are shown.

**Files**: `internal/ui/model.go` (`buildSidebarRows`), `internal/config/config.go`

---

### Phase 2: Hierarchical nesting with collapse

**What**: Reconstruct the folder hierarchy from IMAP delimiters (`/` for Gmail) and render
nested folders as indented, collapsible subtrees under their parent.

**Example before (flat, 20 labels)**:
```
◉ Personal
  Inbox
  Sent
  Archive
  cat-pics
  clients
  clients/acme-corp
  clients/acme-corp/invoices
  clients/mega-bank
  devops/alerts
  devops/ci
  devops/ci/github-actions
  drafts
  family
  finances
  health
  newsletter
  projects/tidemail
  projects/tidemail/bugs
  projects/tidemail/features
  projects/tidemail/prs
  travel
```

**Example after (hierarchical, with collapsible parents)**:
```
◉ Personal
  Inbox
  Sent
  Archive
  ▸ cat-pics           (1)
  ▾ clients             (3)
    acme-corp
    acme-corp/invoices
    mega-bank
  ▸ devops              (2)
  drafts
  family
  finances
  health
  newsletter
  ▾ projects           (3)
    tidemail
    tidemail/bugs
    tidemail/features
  travel
```

**How**:
1. Parse mailbox `Name` into segments using the IMAP `Delimiter` (stored in `db.Mailbox`).
2. Build a tree structure: `map[string]*folderNode` where each node has children, a
   collapse state, and an optional mailbox ID (leaf nodes or parent-as-folder).
3. In `buildSidebarRows()`, walk the tree. Render collapsed parents as a single `▸ row`
   with a count badge (`N`). Render expanded parents as the parent row + all children
   indented.
4. Add keyboard navigation: `Left` on an expanded parent collapses it; `Right` or `Enter`
   on a collapsed parent expands it. Tab through children as normal.
5. Persist collapse state per-account across sidebar rebuilds (same mechanism as the
   existing `collapsed map[int64]bool` for accounts, but keyed by `mailboxID` of the
   parent).

**Important design decision**: When a parent folder also has unread counts (Gmail labels
show unread count), should the parent row show a sum of children's unread? **Yes** — this
is what Gmail's web UI does and users expect it. The parent row itself may not have a
direct IMAP mailbox (it's a virtual container). Render it as a non-selectable header or
as a selectable pseudo-mailbox.

**Files**: `internal/ui/model.go` (`buildSidebarRows`, `sidebarRow` struct), `internal/ui/model.go`
(sidebar navigation — handle expand/collapse key events), `internal/ui/model.go`
(rendering — `renderSidebarMailboxRow` or new `renderSidebarParentRow`)

---

### Phase 3: Sidebar search/filter

**What**: Type to filter the sidebar to only matching folder names. Works like `less` or
`fzf` — characters typed narrow the list, `Esc` clears the filter.

**How**:
1. When the sidebar pane is focused and the user types a printable character, enter a
   "filter mode" (similar to how `?` shows help).
2. Show a filter input at the top of the sidebar (or overlay it at the bottom).
3. As characters are typed, filter the visible sidebar rows to only those whose
   `DisplayName` matches (case-insensitive substring match).
4. `Esc` or `Backspace` on empty input exits filter mode.
5. `Up`/`Down` navigate the filtered list; `Enter` selects the folder.

**Only needed if the user has very many folders** — Phase 2 alone handles most cases
(50–80 labels). Phase 3 is overkill for <100 labels but essential for power users.

**Files**: `internal/ui/model.go` (sidebar filter state, key handling), `internal/ui/model.go`
(sidebar rendering — show filter prompt, filter rows)

---

## Implementation order

1. **Phase 1** (hide system folders) — quick win, low risk, immediate payoff.
2. **Phase 2** (hierarchical nesting) — the main feature. Most of the UX improvement.
3. **Phase 3** (sidebar search) — only if nesting isn't enough. Could be deferred.

## Files likely to change

| File | Phase | What |
|------|-------|------|
| `internal/ui/model.go` | 1,2,3 | `buildSidebarRows`, sidebar rendering, key handling, state fields |
| `internal/config/config.go` | 1 | Add `HideGmailSystemFolders` to `DisplayConfig` |
| `internal/ui/settings.go` | 1 | Toggle in Settings → Display section |
| `internal/ui/keys.go` | 2 | Maybe additional keybindings for expand/collapse |

## Tests / validation

- Phase 1: Unit test for `buildSidebarRows` with Gmail-style mailbox list → system
  folders filtered out.
- Phase 1: Test that configuration toggle re-shows hidden folders.
- Phase 2: Unit test tree-building from flat mailbox list with delimiter.
- Phase 2: Test sidebarRow ordering in hierarchical mode (parent → children → next sibling).
- Phase 2: Visual/manual test with a real Gmail account (20+ labels).
- Phase 3: Manual — type to filter, verify matching narrows list, Esc clears.

## Risks & tradeoffs

- **Phase 2 tree depth**: Gmail technically allows unlimited nesting. Cap at 3 levels
  to keep rendering sane (parent > child > grandchild). Deeper → show parent with count.
- **Phase 2 performance**: Building a tree from 200 folders on every `buildSidebarRows`
  call is fast enough (O(n) scan, O(n log n) sorting). Only a concern if you have
  1000+ folders (unlikely).
- **Phase 2 selection**: When clicking a collapsed parent row, do you expand it or
  select the parent's mailbox? If the parent has its own mailbox (Gmail gives every label
  its own IMAP folder), expand first. If it's purely virtual (no direct mailbox), expand.
  Right arrow could select the first child.
- **Phase 2 unread counts**: Summing children's unread counts for parent rows requires
  a tree walk on every render. Caching the sums and only recalculating on sync would
  avoid O(n) per frame.
- **Phase 1 false positives**: Some non-Gmail providers might have folders named
  "[Something]/All Mail" — the filter should only apply to Gmail-detected accounts
  (provider == "Gmail" in config) or be user-configurable.

## Open questions

1. Should hidden system folders still count toward unread totals on the account header?
   Probably not — they're hidden because the user doesn't care about them.
2. For hierarchical nesting, should the parent row be selectable (to view messages
   labeled with that parent), or just a collapse toggle? Gmail's IMAP shows every label
   as a selectable folder, even if it has children. **Selectable** is more useful.
3. Should Phase 2 use `db.Mailbox.Delimiter` from IMAP LIST response, or always use `/`
   for Gmail? Use the stored `Delimiter` — it's correct per-account.
4. Collapse state: persist in config or in-memory only? **In-memory** (like the existing
   `collapsed` map) — no need to save ephemeral UI state.
