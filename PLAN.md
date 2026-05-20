# RSS TUI Reader — Implementation Plan

## Context
Building a Go TUI RSS reader using Charm's ecosystem (Bubble Tea + Lip Gloss + Bubbles).

---

## Tech Stack

```
github.com/charmbracelet/bubbletea          — TUI framework
github.com/charmbracelet/lipgloss           — styling
github.com/charmbracelet/bubbles            — viewport, textinput, spinner
modernc.org/sqlite                          — pure Go SQLite (no CGo)
github.com/mmcdole/gofeed                   — RSS/Atom/JSON feed parser
github.com/JohannesKaufmann/html-to-markdown — HTML → readable text
github.com/BurntSushi/toml                  — config file parsing
```

---

## Layout

```
┌──────────────┬──────────────────────────────────┐
│ Feeds        │ Articles                         │
│──────────────│──────────────────────────────────│
│ ▶ HN    (5) │ ● Story 1 with long title    2h  │
│   Lobsters  │ ● Story 2                    4h  │
│   LWN       │   Story 3                    1d  │
│             ├──────────────────────────────────│
│             │ # Article Title                  │
│             │                                  │
│             │ Article content (scrollable)...  │
└─────────────┴──────────────────────────────────┘
[status bar]
```

- Left: 28% width — feeds list
- Right: 72% width, split vertically: top 40% articles, bottom 60% content viewport
- Status bar: 1 row at bottom

---

## File Structure

```
main.go
go.mod
PLAN.md
internal/
  db/
    db.go           — open DB, WAL mode, foreign keys, migrate
    feeds.go        — Feed CRUD + ListFeeds with unread count JOIN
    articles.go     — Article CRUD, UpsertArticle, MarkRead, MarkAllRead
  feed/
    fetcher.go      — HTTP GET with timeout, status check, user-agent
    parser.go       — wraps gofeed, returns internal ParsedFeed type
  config/
    config.go       — load/save ~/.config/rss/config.toml (theme selection)
  opml/
    opml.go         — import/export feeds as OPML (stdlib encoding/xml only)
  ui/
    model.go        — root Bubble Tea model (Init/Update/View + routing)
    panes.go        — pane dimension helpers
    feed_manager.go — full-screen feed manager view + its own Update logic
    help.go         — full-screen help view, grouped keybinding reference
    overlay.go      — quit confirm, search, theme picker overlay renderers
    themes.go       — all built-in Theme structs
    styles.go       — BuildStyles(Theme) Styles — no package-level style vars
    keys.go         — key binding definitions (bubbles/key)
    msgs.go         — all tea.Msg types
```

---

## Database Schema

```sql
CREATE TABLE IF NOT EXISTS feeds (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    url          TEXT    NOT NULL UNIQUE,
    title        TEXT    NOT NULL DEFAULT '',
    description  TEXT    NOT NULL DEFAULT '',
    favicon_url  TEXT    NOT NULL DEFAULT '',
    last_fetched INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS articles (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id      INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    guid         TEXT    NOT NULL,
    title        TEXT    NOT NULL DEFAULT '',
    link         TEXT    NOT NULL DEFAULT '',
    content      TEXT    NOT NULL DEFAULT '',
    published_at INTEGER NOT NULL DEFAULT 0,
    read         INTEGER NOT NULL DEFAULT 0,
    UNIQUE(feed_id, guid)
);

CREATE INDEX IF NOT EXISTS idx_articles_feed_id ON articles(feed_id);
```

Pragmas on open: `journal_mode=WAL`, `foreign_keys=ON`

`ListArticles` always uses `ORDER BY published_at DESC LIMIT 100`

---

## Go Types

```go
type Feed struct {
    ID, UnreadCount int64
    URL, Title, Description, FaviconURL string
    LastFetched time.Time
}

type Article struct {
    ID, FeedID     int64
    GUID, Title, Link, Content string
    PublishedAt    time.Time
    Read           bool
}
```

---

## App Modes

```go
type appMode int
const (
    modeMain        appMode = iota  // normal 3-pane view
    modeFeedManager                 // full-screen feed manager
    modeHelp                        // full-screen keybinding reference
)

type overlayMode int
const (
    overlayNone overlayMode = iota
    overlayQuitConfirm
    overlaySearch
    overlayThemePicker
    overlayConfirmDelete
)
```

---

## Root Model

```go
type Model struct {
    db            *db.DB
    width, height int
    mode          appMode
    focused       pane        // paneFeeds | paneArticles | paneContent

    // Feed pane
    feeds      []db.Feed
    feedCursor int

    // Article pane
    articles         []db.Article
    filteredArticles []db.Article
    articleCursor    int
    listOffset       int
    searchQuery      string

    // Content pane
    viewport viewport.Model

    // Overlay
    overlay       overlayMode
    textInput     textinput.Model
    confirmTarget int64

    // Theming
    confirmedTheme int
    activeTheme    int
    styles         Styles

    // Feed manager
    feedManager FeedManagerModel

    // Status
    statusMsg string
    statusErr bool

    // Async
    refreshing  map[int64]bool
    keys        KeyMap
    mdConverter *md.Converter
}
```

---

## Key Bindings

### Main View

| Key | Action |
|-----|--------|
| `Tab` / `Shift-Tab` | Cycle panes: Feeds → Articles → Content → wrap |
| `h` / `←` | Focus left (Articles → Feeds) |
| `l` / `→` | Focus right (Feeds → Articles) |
| `j` / `↓` | Navigate down within pane |
| `k` / `↑` | Navigate up within pane |
| `Enter` | Open article in content pane (auto-marks read) |
| `Esc` | Return to Articles from Content |
| `r` | Toggle read/unread on selected article |
| `R` | Mark all articles in current feed as read |
| `f` | Refresh selected feed |
| `F` | Refresh all feeds |
| `o` | Open article in browser |
| `/` | Search/filter articles |
| `m` | Open Feed Manager |
| `T` | Open theme picker (live preview) |
| `?` | Open help screen |
| `q` | Quit (with confirmation) |

### Feed Manager

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate feed list |
| `a` | Add new feed |
| `e` / `Enter` | Edit selected feed |
| `d` | Delete selected feed (with confirm) |
| `i` | Import OPML |
| `x` | Export OPML to `~/.config/rss/feeds.opml` |
| `Esc` | Return to main view |

### Edit Mode (inside Feed Manager)

| Key | Action |
|-----|--------|
| `Tab` | Toggle between Title / URL fields |
| `Enter` | Save |
| `Esc` | Cancel |

---

## Theming

17 built-in themes in `internal/ui/themes.go`:

**Standard:** catppuccin-mocha, catppuccin-latte, catppuccin-frappe, catppuccin-macchiato, nord, dracula, gruvbox-dark, gruvbox-light, tokyo-night, tokyo-night-day, rose-pine, rose-pine-moon, rose-pine-dawn, one-dark

**Custom** (sourced from paperheartdesign.com/blog/color-palette-pleasantly-purple):

| Theme | Bg | Accent | Fg |
|---|---|---|---|
| magenta-geode | `#47003c` | `#c83fa9` | `#f3b0dc` |
| coral-sunset | `#444154` | `#ff7062` | `#fec9c1` |
| lavender-fields-forever | `#382d72` | `#a080e1` | `#e5ccf4` |

`styles.go` exposes `BuildStyles(t Theme) Styles` — no package-level vars. Model rebuilds styles on theme change.

**Theme picker:** `T` opens overlay. `j`/`k` moves cursor and immediately rebuilds styles for live preview. `Enter` confirms + saves to config. `Esc` reverts.

Config: `~/.config/rss/config.toml` → `theme = "catppuccin-mocha"`

---

## Feed Refresh Strategy

- **On launch:** immediately dispatch `refreshFeedCmd` for all feeds as `tea.Batch` after `FeedsLoadedMsg`. DB articles display instantly; fresh content arrives in background.
- **Manual:** `f` = selected feed, `F` = all feeds.
- **No polling timer.**
- Status bar shows `↻ feed-name` while refreshing.

---

## Empty State

On `FeedsLoadedMsg` with zero feeds: set `mode = modeFeedManager` with add-feed input pre-focused. Never shown again once a feed exists.

---

## OPML

`internal/opml/opml.go` — stdlib `encoding/xml` only.

- `Import(path) ([]Outline, error)` — parse file, bulk-insert feeds, trigger refresh
- `Export(feeds, path) error` — write to `~/.config/rss/feeds.opml`

---

## Status Bar

```
HN · 5/42 unread · refreshed 3m ago · [↻ Lobsters] · ? help
```

Errors replace the full bar in error color, auto-clear after 4s via `tea.Tick`.

---

## Critical Implementation Notes

- **HTTP:** always check `resp.StatusCode`; close body and return error on non-2xx
- **GUID fallback:** `item.GUID` → `item.Link` → hash of `title+published`
- **UpsertArticle:** `INSERT OR IGNORE` then `UPDATE` non-key fields; never overwrite `read`
- **Cursor clamping:** after every mutation — `clamp(v, 0, max(0, len(slice)-1))`
- **No I/O in View():** all DB/HTTP inside `tea.Cmd` closures only
- **mdConverter:** allocate once in `NewModel()`, reuse across goroutines
- **Browser:** `cmd.Start()` not `cmd.Run()`; handle `darwin` (`open`) vs default (`xdg-open`)
- **Terminal cleanup:** `tea.WithAltScreen()` + panic recovery calling `p.Kill()`

---

## Implementation Order

1. `go.mod` + `go get` all deps
2. `internal/db/` — schema, migrations, CRUD
3. `internal/feed/` — fetcher + parser
4. `internal/config/` — load/save TOML
5. `internal/opml/` — import/export
6. `internal/ui/themes.go` — all 17 Theme structs
7. `internal/ui/styles.go` — `BuildStyles(Theme)`
8. `internal/ui/msgs.go`, `keys.go`
9. `internal/ui/model.go` skeleton — Init, WindowSizeMsg, placeholder View
10. Feed pane render + navigation
11. Article pane render + navigation + read toggle + search filter
12. Content viewport + markdown rendering
13. Background refresh (f/F) + status bar + spinner
14. `internal/ui/feed_manager.go` — add/edit/delete + OPML
15. Empty state → auto-open feed manager
16. Theme picker overlay with live preview
17. Quit confirmation overlay
18. `internal/ui/help.go` — help screen
19. `main.go` — panic recovery + terminal cleanup
20. End-to-end smoke test with real feed URLs

---

## Verification

1. `go build ./...` — no errors
2. `go vet ./...` — no issues
3. Launch → empty state opens feed manager automatically
4. Add `https://hnrss.org/frontpage` → articles load, unread count shows
5. `f` refresh, `r` toggle read, `R` mark all read, `o` open browser, `/` search
6. `T` theme picker → live preview on j/k, Esc reverts, Enter saves
7. `m` feed manager → edit/delete/OPML export
8. `?` help screen → all keys listed correctly
9. `q` → confirmation → quit → terminal fully restored
10. Kill mid-fetch with Ctrl+C → terminal fully restored
