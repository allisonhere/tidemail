# Plan: Codebase Health & Next Steps

## Current state

v0.2.3 is stable and feature-complete. Recent work has been polish:
- install.sh rewritten (no GitHub API, redirect URLs, gzip validation)
- deploy.sh streamlined (release notes optional)
- Dual-install PATH conflict resolved

No unreleased commits, no TODO/FIXME markers, clean working tree.

## The elephant: model.go

`internal/ui/model.go` is **5,127 lines with 199 functions** — everything from Bubble Tea plumbing to sidebar rendering to AI summary, save-attachment file pickers, update state machines, and keyboard dispatch. It's the single largest file by ~2x (second place: settings.go at 2,558 lines).

**Impact**: every change risks merge conflicts, every new contributor drowns, and 199 functions in one file is impossible to test in isolation. The core model has zero direct tests.

## Recommendation: split model.go into focused files

| New file | Lines (est.) | Extracted from model.go |
|---|---|---|
| `model.go` | ~500 | Model struct, NewModel, Init, Update, View, keyboard dispatch |
| `sidebar.go` | ~600 | renderAccountsPane, sidebarRow building, collapse state, all sidebar styles |
| `messages.go` | ~500 | renderMessagesPane, messageRowStyles, applyFilter, selection, message CRUD cmds |
| `content.go` | ~400 | renderContentPane, viewport, content search, focus line, links, attachments |
| `statusbar.go` | ~250 | renderStatusBar, statusLine, setStatus, addToLog, clearStatusCmd |
| `overlays.go` | ~400 | renderOverlay, all overlay renderers (search, command palette, summary, update, grammar, log, theme, save attach) |
| `commands.go` | ~200 | commandItems, filteredCommandItems, executeCommand, commandMessage |
| `save_attach.go` | ~200 | saveAttachPicker*, saveAttachmentsCmd* |
| `sync.go` | ~400 | syncMailboxCmd, scheduleNextSync, startSyncTimers, load*Cmds |
| `update_ui.go` | ~300 | update state, check/download/install cmds, dismiss, settings update state |
| `summary.go` | ~200 | openSummary, aiSummarizeCmd, saveSummaryMDCmd |
| `util.go` | ~350 | formatTime, relativeTime, truncate, clamp, clampView, fillViewWidth, collapseQuoteBlocks, indentBlock, keyMatches, etc. |

**Total**: ~4,300 lines split across 11 focused files.

## Why this first

1. **Every future feature touches model.go**. Splitting now means less pain later.
2. **Testing**: isolated files can be tested independently. Currently model.go has no tests at all.
3. **Code review**: a 200-line file is reviewable. A 5,127-line file is not.
4. **Merge conflicts**: 11 files means parallel workstreams don't collide on one giant file.

## Approach

1. Create each new file, move the functions, verify compilation after each move
2. Keep all types/enums in model.go (they're shared across all files)
3. Package stays `package ui` — no import changes needed
4. Run `go build ./...` and `go test ./...` after each file extraction
5. Commit each file split as a separate commit for clean blame history

## Risk

- The Bubble Tea `Update()` dispatcher at line 266 (534 lines long) calls into every subsystem. Splitting shouldn't break this, but the dispatch function will reference methods across files. Since they're all on the same `Model` struct in the same package, this works naturally.
- Some private helpers are used by multiple subsystems. These go into `util.go`.

## After split: add model tests

Once split, test each subsystem:
- `sidebar_test.go`: row building, collapse toggle, selection
- `messages_test.go`: filtering, selection, unread counts
- `content_test.go`: link extraction, focus line, content search match cycling
- `commands_test.go`: command filtering, execution dispatch

## Alternative: feature work

If splitting feels premature, the next highest-value features would be:

1. **Forward email** — reply exists but no forward (cc: compose.go already handles compose, so this is a small addition)
2. **Drafts** — save/load draft messages to SQLite
3. **Search all mailboxes** — currently search is per-mailbox only
4. **Signature support** — per-account compose signatures

## Decision needed

Split model.go now, or add a feature first?
