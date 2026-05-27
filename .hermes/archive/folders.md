You are implementing folder support for Tide, a terminal RSS reader built with Go,
charmbracelet/bubbletea, charmbracelet/lipgloss, and modernc.org/sqlite.

## Repo structure

internal/db/db.go          — SQLite schema + migrations (user_version tracking)
internal/db/feeds.go       — Feed struct + CRUD (ListFeeds, AddFeed, UpdateFeed, DeleteFeed)
internal/db/articles.go    — Article struct + CRUD
internal/ui/model.go       — Main bubbletea Model, all pane rendering, key handling
internal/ui/feed_manager.go — Feed manager overlay (add/edit/delete/import/export)
internal/opml/opml.go      — OPML import/export (currently flat — no folder nesting)

## Current feed pane model (to be replaced)

The Model struct in model.go currently has:
  feeds      []db.Feed
  feedCursor int   // direct index into feeds[]

renderFeedsPane() iterates `range m.feeds`, handleUp/handleDown increment/decrement feedCursor,
and everywhere uses m.feeds[m.feedCursor] to get the selected feed.

## What to build

Add single-level folder grouping to the feed pane. No nested folders.

---

### 1. Database — internal/db/db.go + internal/db/feeds.go

Add a schema migration at user_version 2:

  CREATE TABLE folders (
      id       INTEGER PRIMARY KEY AUTOINCREMENT,
      name     TEXT    NOT NULL UNIQUE,
      position INTEGER NOT NULL DEFAULT 0
  );
  ALTER TABLE feeds ADD COLUMN folder_id INTEGER REFERENCES folders(id) ON DELETE SET NULL;

Add to feeds.go:

  type Folder struct {
      ID       int64
      Name     string
      Position int
  }

  func (db *DB) ListFolders() ([]Folder, error)          // ORDER BY position, name COLLATE NOCASE
  func (db *DB) AddFolder(name string) (int64, error)
  func (db *DB) RenameFolder(id int64, name string) error // return error if name already exists
  func (db *DB) DeleteFolder(id int64) error              // ON DELETE SET NULL handles feeds
  func (db *DB) SetFeedFolder(feedID, folderID int64) error // 0 = clear folder (set NULL)
  func (db *DB) ReorderFolder(id int64, position int) error

Update Feed struct: add FolderID int64 (0 = uncategorized).
Update ListFeeds() to SELECT folder_id.

---

### 2. UI model — internal/ui/model.go

Replace the flat feed cursor with a sidebar row system.

#### New types (add near top of model.go):

  type sidebarRowKind int

  const (
      rowKindFolder sidebarRowKind = iota
      rowKindFeed
  )

  type sidebarRow struct {
      kind   sidebarRowKind
      folder *db.Folder // non-nil when rowKindFolder
      feed   *db.Feed   // non-nil when rowKindFeed
  }

#### Model struct changes:

Remove:
  feeds      []db.Feed
  feedCursor int

Add:
  feeds            []db.Feed
  folders          []db.Folder
  sidebarRows      []sidebarRow
  sidebarCursor    int
  collapsedFolders map[int64]bool  // folderID -> collapsed; 0 = Uncategorized group

#### rebuildSidebar()

  func (m *Model) rebuildSidebar() {
      byFolder := map[int64][]db.Feed{}
      for i := range m.feeds {
          byFolder[m.feeds[i].FolderID] = append(byFolder[m.feeds[i].FolderID], m.feeds[i])
      }
      m.sidebarRows = nil
      for i := range m.folders {
          fld := &m.folders[i]
          m.sidebarRows = append(m.sidebarRows, sidebarRow{kind: rowKindFolder, folder: fld})
          if !m.collapsedFolders[fld.ID] {
              for j := range byFolder[fld.ID] {
                  f := byFolder[fld.ID][j]
                  m.sidebarRows = append(m.sidebarRows, sidebarRow{kind: rowKindFeed, feed: &f})
              }
          }
      }
      // Uncategorized: feeds with folder_id = 0, only show group header if any exist
      if uncategorized := byFolder[0]; len(uncategorized) > 0 {
          phantom := &db.Folder{ID: 0, Name: "Uncategorized"}
          m.sidebarRows = append(m.sidebarRows, sidebarRow{kind: rowKindFolder, folder: phantom})
          if !m.collapsedFolders[0] {
              for j := range uncategorized {
                  f := uncategorized[j]
                  m.sidebarRows = append(m.sidebarRows, sidebarRow{kind: rowKindFeed, feed: &f})
              }
          }
      }
  }

Call rebuildSidebar() wherever feeds or folders are reloaded (FeedsLoadedMsg handler, after
feed add/edit/delete in feed manager). Also initialise collapsedFolders as map[int64]bool{} on
Model creation.

#### Helper — always use this instead of m.feeds[m.sidebarCursor]:

  func (m Model) selectedFeed() *db.Feed {
      if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
          return nil
      }
      row := m.sidebarRows[m.sidebarCursor]
      if row.kind == rowKindFeed {
          return row.feed
      }
      return nil
  }

Audit every callsite that was using m.feeds[m.feedCursor] and replace with selectedFeed().
Guard all of them against nil.

#### Navigation — handleUp / handleDown:

  case paneFeeds:
      if m.sidebarCursor > 0 {
          m.sidebarCursor--
          if f := m.selectedFeed(); f != nil {
              return m, m.loadArticlesCmd(f.ID)
          }
      }

Same pattern for handleDown (bound check against len(m.sidebarRows)-1).

Cursor lands on folder headers — that is fine. No article load fires.

#### Enter/Space on a folder row — toggles collapse:

When the focused pane is paneFeeds and the user presses enter/space and selectedFeed() == nil
(i.e. cursor is on a folder header):

  row := m.sidebarRows[m.sidebarCursor]
  if row.kind == rowKindFolder {
      id := row.folder.ID
      m.collapsedFolders[id] = !m.collapsedFolders[id]
      m.rebuildSidebar()
      // Re-anchor cursor: find the folder header row we were on
      for i, r := range m.sidebarRows {
          if r.kind == rowKindFolder && r.folder.ID == id {
              m.sidebarCursor = i
              break
          }
      }
  }

#### Edge case — selected feed's folder is collapsed from outside:

After any rebuildSidebar() call, if sidebarCursor is now out of bounds, clamp it:

  m.sidebarCursor = clamp(m.sidebarCursor, 0, max(0, len(m.sidebarRows)-1))

Then if selectedFeed() == nil and sidebarRows is non-empty, the article pane should show
empty (no articles loaded). Do not panic.

#### renderFeedsPane():

Replace the existing range loop with:

  for i, row := range m.sidebarRows {
      selected := i == m.sidebarCursor
      switch row.kind {
      case rowKindFolder:
          icon := "▼"
          if m.collapsedFolders[row.folder.ID] {
              icon = "▶"
          }
          unread := m.folderUnreadCount(row.folder.ID)
          rows = append(rows, m.renderFolderHeader(icon, row.folder.Name, unread, selected, innerW))
      case rowKindFeed:
          rows = append(rows, m.renderFeedRow(row.feed, selected, innerW))
      }
  }

renderFolderHeader: render with subtle style (dimmer foreground, no indent, bold name).
If selected, use the same selection highlight as feed rows.
Show unread count in parens only if > 0: "▼ Tech (16)".

renderFeedRow: existing feed row logic, but prefix with two spaces of indent ("  ") before
the existing prefix character.

folderUnreadCount(folderID int64) int64: sum UnreadCount for all feeds in m.feeds where
FolderID == folderID (folderID 0 = uncategorized). O(n), fine.

If sidebarRows is empty, show the existing empty-feeds hint as before.

Footer: change "N feeds" to show both counts:
  "  3 folders · 12 feeds"  (omit folders part if zero folders exist)

#### Mark all read (R key):

If cursor is on a folder header: mark all feeds in that folder as read (batch).
If cursor is on a feed: existing behaviour (mark that feed's articles read).
Add a MarkFolderReadCmd that fires MarkFeedReadCmd for each feed in the folder.

---

### 3. Feed manager — internal/ui/feed_manager.go

Add a folder picker field to the add/edit form.

#### FeedManager struct additions:

  folders      []db.Folder
  folderCursor int  // index into [no folder, ...folders, + New folder]
  newFolderInput textinput.Model
  showNewFolder  bool

#### Folder options slice (computed):

  // index 0 = "(no folder)", 1..n = folders[0..n-1], n+1 = "+ New folder"

#### Add/edit form layout:

  Title   [ _________________________ ]
  URL     [ _________________________ ]
  Folder  [◀ Tech                   ▶]

Left/right arrow on Folder field cycles options. When "+ New folder" is selected, a fourth
row appears:

  New     [ _________________________ ]

focusedField becomes: 0=title, 1=url, 2=folder, 3=new-folder-name (only when showNewFolder).

Tab cycles title → url → folder → (new-folder-name if visible) → title.

On save: if showNewFolder and new folder name is non-empty, call db.AddFolder first to get
the folder ID, then use it for the feed. If folder name already exists, call db.AddFolder
which returns the existing ID (use INSERT OR IGNORE + SELECT).

On edit: pre-select the feed's current folder in the picker.

#### Feed manager list view:

Show folder name as a dimmed suffix on each feed row in the list:
  "Hacker News  [Tech]"
Feeds with no folder show nothing.

#### Reload:

Update fm.reload() to also load folders: fm.folders, _ = fm.db.ListFolders()

---

### 4. OPML — internal/opml/opml.go

#### Outline struct — add children:

  type Outline struct {
      Text     string    `xml:"text,attr"`
      Title    string    `xml:"title,attr,omitempty"`
      Type     string    `xml:"type,attr,omitempty"`
      XMLURL   string    `xml:"xmlUrl,attr,omitempty"`
      HTMLURL  string    `xml:"htmlUrl,attr,omitempty"`
      Outlines []Outline `xml:"outline"` // children (folder contents)
  }

#### Import:

  type ImportedFeed struct {
      Outline
      FolderName string // empty = uncategorized
  }

  func Import(path string) ([]ImportedFeed, error)

Walk body outlines:
- If outline has xmlUrl → it's a feed, FolderName = ""
- If outline has no xmlUrl but has children → it's a folder; walk children for feeds,
  set FolderName = outline.Text
- Nested folders (children that are also folders): flatten to single level, use the
  immediate parent folder name (the deepest named ancestor that contains this feed directly).
- A feed appearing under multiple folder outlines (rare but valid OPML): import once, use
  first folder encountered.

Update the import handler in feed_manager.go to use ImportedFeed, creating folders as needed
via db.AddFolder (idempotent), then calling db.SetFeedFolder.

#### Export:

  type ExportFeed struct {
      URL        string
      Title      string
      FolderName string
  }

  func Export(feeds []ExportFeed, path string) error

Group feeds by FolderName. Feeds with empty FolderName go directly in body as flat outlines
(no wrapper). Feeds with a folder name go inside a folder outline:

  <outline text="Tech" title="Tech">
      <outline type="rss" text="HN" xmlUrl="..." />
  </outline>

---

### Edge cases to handle explicitly

1. Cursor out of bounds after rebuildSidebar: always clamp sidebarCursor after rebuild.
2. selectedFeed() returns nil when on folder header: every caller must nil-check.
3. Delete currently-selected feed: after reload, if sidebarCursor points past end, clamp,
   then load articles for new selectedFeed() if non-nil, else clear article pane.
4. Delete a folder (not yet exposed in UI, but db layer supports it): ON DELETE SET NULL
   moves feeds to uncategorized automatically.
5. Empty folder (all feeds deleted): keep the folder header — user created it intentionally.
6. All folders collapsed: sidebarCursor lands on a folder header, article pane stays at
   last loaded content (do not reload or clear).
7. Rename folder to existing name: RenameFolder must return a descriptive error; feed manager
   shows it in statusMsg.
8. Zero folders: rebuildSidebar produces only rowKindFeed rows (no headers). Visually identical
   to current flat list. No regression for existing users.
9. Mark-all-read on folder header: fires per-feed MarkAllRead for every feed in that folder.
10. Refresh spinner: spinner prefix applies to the feed row, not the folder header, even when
    a feed inside a collapsed folder is refreshing.

---

### Style guidance

- Folder headers: use existing theme colours. Name in normal weight.
  Dimmed foreground when not selected, accent foreground when selected (same as feed selection).
- Feed rows: indent with two leading spaces to visually nest under folder.
- Do not add new theme fields — derive folder header colours from existing Theme fields
  (Dimmed, Accent, Text, etc.).
- Keep folder header height = 1 line (same as feed rows) — no decorative separators.

---

### What NOT to do

- Do not add nested folders (no parent_id).
- Do not add drag-and-drop reordering (position column exists for future use; no UI yet).
- Do not add a separate folder management screen — folder creation happens inline in the
  feed manager's new-folder input.
- Do not change any behaviour unrelated to folders.
- Do not add comments or docstrings to code you didn't touch.
