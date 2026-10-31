# Ghosted signature preview plan

First correct regex search (multiline anchors and validated capture references),
then checkpoint the existing Ripple and TideMail search changes in separate
commits. Do not push, tag, or release either repo.

## Preview behavior

Show the current sending account's full, trimmed signature as readable muted
text immediately below its valid `⁂` marker. Refresh when the marker or body
changes, undo/redo runs, the sender changes, or a draft loads. Hide the preview
for empty signatures, missing/invalid markers, and the template editor. Keep the
marker editable. Enable previews automatically without a new setting.

Preview text is display-only: exclude it from document values, drafts, clipboard
operations, search, replacements, undo history, and outgoing document text. The
existing send path continues to insert the signature exactly once.

## Implementation

Add Ripple `VirtualBlock{After int, Text string}` and `SetVirtualBlocks`, with
rune offsets anchored at document line ends, plus `Options.VirtualText` styling.
Copy the supplied blocks. Wrap them with Ripple's width rules and include their
rows in display layout and scrolling. Keep caret positions on document text:
Up/Down skip virtual rows; page navigation can reveal long previews. Synchronize
stored and rendered geometry, including search-bar space and resize handling.

Share signature-marker validation with sending so invalid duplicate, inline,
quoted, list, or code markers do not produce a misleading preview. Preserve
existing send errors. Update Ripple API docs/harness and TideMail's guide/help.

## Verification and delivery

Cover new/reply/forward compositions, loaded drafts, sender changes, moved or
deleted markers, undo/redo, navigation, selection across previews, long
signatures, Unicode, small panes, resizing, and search. Verify ghost text never
appears in saved/copied/searched text and sending includes one signature.

Use the local Go workspace documented in `ripple-search-development.md`; leave
TideMail's released Ripple dependency pin unchanged. Run relevant race tests,
builds, vet, and terminal verification. Leave the signature-preview changes
uncommitted for review; publish nothing.
