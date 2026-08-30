# Extractable Components

## AppShell
- Source: `cmd/tidemail-gui/frontend/src/App.tsx`
- Category: layout
- Description: Three-pane TideMail desktop shell.
- Extractable props: active view, sidebar state, selected message.
- Hardcoded: TideMail brand mark, pane order, toolbar icon family.

## Sidebar
- Source: `cmd/tidemail-gui/frontend/src/App.tsx`
- Category: layout
- Description: Unified inbox and account mailbox navigation.
- Extractable props: open, accounts, mailboxes, active view.
- Hardcoded: Unified inbox label and local-cache footer.

## MessageRow
- Source: `cmd/tidemail-gui/frontend/src/App.tsx`
- Category: basic
- Description: Sender, subject, preview, date, and status row.
- Extractable props: message, selected, read, starred.
- Hardcoded: Lucide status icons and row structure.

## ReaderToolbar
- Source: `cmd/tidemail-gui/frontend/src/App.tsx`
- Category: basic
- Description: Reply, archive, star, unread, and delete actions.
- Extractable props: read, starred, action callbacks.
- Hardcoded: Action labels and Lucide icons.
