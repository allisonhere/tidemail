# Changelog

All notable changes to TideMail are documented in this file.

## Unreleased

### Added

- **OAuth sign-in for Gmail and Outlook.** The account form's Gmail/Outlook
  **Auth** row is a `‹ ›` selector (App password ⇄ OAuth); the guidance shows
  only while that row or its sub-controls are focused. Gmail is bring-your-own
  client (`TIDEMAIL_GOOGLE_CLIENT_ID` / `_SECRET`) with a device-code flow;
  Outlook uses Thunderbird's shared public client by default (paste-back flow)
  or a custom `TIDEMAIL_MS_CLIENT_ID` (device-code). Both authenticate IMAP and
  SMTP with XOAUTH2; the refresh token is stored in the keychain and
  rotation-persisted. Revoked tokens surface a "press M to re-authenticate"
  hint.

### Fixed

- **Installing to `~/.local/bin` now handles stale earlier binaries.** The
  installer removes writable older `tidemail` binaries that appear earlier on
  `PATH`, and both the installer and in-app updater show the cleanup command
  when a privileged old binary would still run first.

## v1.0.5

**Release highlights:** installs now land in the user-local bin path by default,
the in-app updater has a progress popup with restart handoff, and link cleanup
handles another bit of Reddit tracking noise.

### Added

- **The in-app updater now shows install progress.** Update installs keep the
  popup open with a progress bar, then offer a restart action once the new binary
  is ready.

### Changed

- **The release installer now defaults to `~/.local/bin`.** It no longer asks for
  `sudo` just because `/usr/local/bin` is not writable.
- **The self-updater falls back to the user-local install path.** When the
  current binary directory is not writable, TideMail installs the update at
  `~/.local/bin/tidemail` and shows the manual command if automatic replacement
  is not possible.

### Fixed

- **The installer verifies staged binaries before replacing an existing
  install.** Failed downloads, bad archives, or broken staged binaries leave the
  current `tidemail` binary untouched and remove temporary install files.
- **SMTP reconnects no longer hang on stale post-suspend sessions.** SMTP
  commands now use deadlines so dead connections fail and reconnect instead of
  blocking indefinitely.
- **Reddit links drop trailing tracking fragments like `[Read/` and `V`.** The
  Links view no longer opens malformed Reddit URLs caused by leftover email CTA
  text at the end of the extracted URL.

## v1.0.4

**Release highlights:** noisy HTML emails are easier to read in the terminal:
Reddit digests render as compact post summaries, newsletter-style templates shed
more boilerplate, and tracking redirects resolve to cleaner actionable links.

### Fixed

- **Reddit digest emails now render as compact terminal-native post summaries.**
  TideMail extracts the subreddit, author, age, post title, excerpt, vote/comment
  counts, and read action instead of showing the raw email-template scaffolding.
- **Noisy newsletter-style HTML gets a cleaner fallback view.** Bulky table-heavy
  templates with preheaders, spacer cells, and footer boilerplate now prefer
  meaningful article text and CTA labels when the extractor can do so
  confidently.
- **Actionable links are cleaner for tracked email links.** Reddit click wrappers
  and common redirect parameters such as `url=`, `u=`, `target=`, and `redirect=`
  are normalized so the Links list points at the real destination when possible.

## v1.0.3

**Release highlights:** you can always tell which pane you're in now — a full
colored border wraps the active pane, the current row actually stands out
instead of a faint tint, and pane corners are yours to round off in Settings.

### Added

- **Folders are created for AI move filters.** A rule that moves mail to a folder
  that does not exist yet now creates it, inside the account's own personal
  namespace rather than at the server root.
- **Pane corners are a Display setting.** Pick square (the original look) or
  round, right below Layout density in Settings → Display.

### Fixed

- **Filter rules run against their own scope**, not whatever the sidebar happened
  to have selected, so running a rule from the manager no longer depends on the
  current view.
- **Filter operation failures are handled atomically** — a failed toggle, delete,
  or reorder surfaces its error and leaves the rule list untouched instead of
  half-applied.
- **Full-row message selection highlight** now covers the whole row.
- **Starred rows keep their background continuity** when selected.

### Changed

- **Every pane always shows a full 4-sided border**, not just a single dim
  edge — or none at all, for the Content pane. The focused pane now lights up
  in the theme's accent color on all four sides while the other two stay
  dim, the same treatment you'd get from a tmux or Vim split, and nothing
  resizes as you `Tab` between panes.
- **The focused-pane border and the current selection highlight now guarantee
  real contrast** instead of a fixed color shift that went nearly invisible
  on several themes. Both are checked against a contrast floor (7:1 for the
  pane border, 3:1 for the selection background) and verified automatically
  across all 19 built-in themes.
- **The Go module path is now `github.com/allisonhere/tidemail`.** It previously
  declared `github.com/allisonhere/tide`, which belongs to a separate published
  project, so `go install github.com/allisonhere/tide@latest` fetched the wrong
  program. Internal-only change; no effect on the binary or on installs via
  `install.sh` or GitHub Releases.
- `STATUS.md` rewritten to describe the current system; the June 2026 review docs
  moved to `docs/archive/`.

## v1.0.2

**Release highlights:** mail history goes back further than the first 100
messages, search matches as you type, and `sync_minutes = 0` now means push
instead of nothing.

### Added

- **Older mail loads on demand.** The first sync of a mailbox only caches the
  100 most recent messages, and nothing ever reached past that — the archive
  simply ended, and because search only covers cached mail, it quietly limited
  search too. Moving down past the last message now pulls the next 100 from the
  server, repeatedly, until the mailbox is fully paged back.

### Fixed

- **Search matches partial words.** Search runs on every keystroke, but only
  whole tokens matched, so results vanished mid-word and reappeared once the
  word was finished. The final term is now a prefix match.
- **Search combined with the unread-only toggle no longer hides results.**
  Together they fell through to a subject-only substring filter applied on top
  of the full-text results, dropping every message that matched on sender or
  body.

### Changed

- **`sync_minutes = 0` is now push, not off.** An account set to `0` previously
  got neither polling nor IMAP IDLE, so it refreshed only on launch or a manual
  `s` — with nothing in the UI to say so. It now runs IDLE, delivering mail as
  the server announces it, backed by a 30-minute safety poll for connections
  that wedge without dropping. **Existing accounts set to `0` change behavior**
  and will start refreshing in the background; set `-1` for the old
  manual-only behavior.
- **The account form's sync field is now `Refresh`**, with an inline hint for
  all three modes, and it rejects values it cannot parse. Previously any
  unparseable entry silently became `0`, which was harmless when `0` meant
  "off" but now would opt an account into a persistent connection.

### Fixed

- **Overlapping syncs of the same mailbox are no longer started.** Launch
  timers, the startup sweep, and IDLE nudges all target the inbox and could
  stack up on it. The concurrent DB writes collided (`SQLITE_BUSY`), aborting
  one sync partway through storing messages while another advanced the
  last-synced timestamp past them — so those messages fell outside the next
  sync window and never arrived. A manual sync of an already-syncing mailbox
  now reports "sync already in progress" instead of doing nothing.

## v1.0.1

**Release highlights:** cleaner newsletter rendering and a polished in-app log
viewer.

### Fixed

- **HTML newsletters render with less noise.** TideMail now hides common email
  preheaders, spacer cells, tracking-only elements, and boilerplate-only HTML
  before converting messages for the terminal.
- **Image placeholders stand apart from normal message text.** Useful image alt
  text still appears as `[image: ...]`, now with its own readable theme color in
  rich terminal mode.
- **The Settings log viewer no longer leaks the terminal background at the right
  edge of log rows.** Log lines now fill the same modal width as the title and
  command rows.

### Changed

- **Plain text beats boilerplate HTML.** When an HTML part only says things like
  "View in browser," TideMail falls back to the real plain-text body while still
  storing the original HTML.

## v0.9.6

**Release highlights:** correct display of non-English/legacy-encoded email, plus
a few layout hardening fixes.

### Fixed

- **Emails in non-UTF-8 encodings now display correctly.** Messages in
  ISO-8859-1, Windows-1252, Shift_JIS, and similar legacy charsets used to show
  garbled `�` characters, and an unrecognized encoding could silently drop the
  rest of the message. TideMail now decodes these to proper text. (Messages
  already synced keep their earlier garbling until re-synced.)
- **A message with attachments no longer crashes the content pane on a very
  narrow terminal.**
- **Long attachment filenames are truncated instead of overflowing** and pushing
  the file-size column off-screen.

### Changed

- AI summary and grammar requests no longer clip a multi-byte character in half
  when trimming long content before sending.

## v0.9.5

**Release highlights:** fixes a layout glitch where certain emails pushed the
interface out of alignment.

### Fixed

- **Some messages no longer break the layout by wrapping or shifting the UI up a
  line.** Emails carrying a Unicode line separator (U+2028) and related hard-break
  characters used to render an extra line the width model couldn't see,
  overflowing the message pane. TideMail now normalizes these to real line breaks
  before layout, so the frame stays aligned. Reported against a newsletter from
  ecobee.
- **Undecodable characters no longer nudge a line out of alignment.** The
  replacement glyph shown for bytes that fail to decode occupies a terminal cell
  the width model counted as zero; it's now measured correctly.

## v0.9.4

**Release highlights:** stable message previews in terminals with color emoji,
plus quieter starred-message styling.

### Fixed

- **Scrolling across emoji-heavy messages no longer leaves ghost rows or shifts
  the message pane.** TideMail removes emoji from list-focused previews to keep
  terminal cell widths stable, then restores the original message when you
  enter the content pane.

### Changed

- **Starred messages use a subtle warm row tint instead of a star glyph.** The
  subject column stays aligned and the cursor highlight still takes priority.

## v0.9.3

**Release highlights:** send cancellation, per-account signatures, sender
selection, one-key unsubscribe, and contact suggestions in compose.

### Added

- **Cancel a send with `ctrl+z`.** TideMail holds outgoing mail for five seconds
  by default. Press `ctrl+z` during that window to reopen the message. Change
  the delay in Settings → Editor, or set it to `0` for immediate sending.
- **Each account can use its own signature.** Set a signature in the account
  manager. TideMail adds it when you send from that account.
- **Choose the sender from the From row.** New messages, replies, forwards, and
  reopened drafts share the account picker. `ctrl+u` still cycles accounts.
- **Unsubscribe from the content pane with `ctrl+u`.** TideMail uses the standard
  unsubscribe header when available and can fall back to a labeled unsubscribe
  link in the message.
- **Compose suggests more addresses.** To, CC, and BCC search saved contacts
  first, then addresses found in synced mail.

### Changed

- **The account editor now groups related fields.** Account identity, incoming
  mail, outgoing mail, credentials, sending identity, and sync settings have
  their own sections.
- **README and in-app help cover the new compose and message actions.**

## v0.9.2

**Release highlights:** undo for destructive message actions, more faithful
HTML email rendering, and a living violet Pride DNA signature in About.

### Added

- **Delete, archive, and move actions can be undone.** Message actions disappear
  immediately but wait six seconds before syncing to the server; press `ctrl+z`
  to restore the latest pending single or bulk action.
- **The About screen now carries a living Pride DNA signature.** A responsive
  violet double helix hides a cinematic six-color message for curious
  visitors to discover.

### Changed

- **HTML email rendering is more readable and predictable.** Newsletters,
  semantic tables, reply quotes, calls to action, image descriptions, and wide
  preformatted blocks retain useful structure without loading remote content.

## v0.9.1

**Release highlights:** server-synced message stars, starred-first sorting, and
faster startup with immediate loading feedback and on-demand search indexing.

### Added

- **Messages can be starred and synced with the mail server.** Press `*` to
  toggle IMAP `\Flagged` on the current message or selection. Press `t`, or use
  the new Display setting, to keep starred messages and threads at the top.
- **Initial mailbox loading now has visible progress.** The message pane shows
  an animated loading state until the first account and message load finishes.

### Changed

- **Startup avoids unnecessary full-text index rebuilds.** TideMail now records
  the FTS schema version and only rebuilds when the schema changes or the index
  falls out of sync with the message cache.

### Fixed

- **Quitting no longer appears frozen on slow or wedged IMAP connections.**
  Active operations are interrupted, account sessions close concurrently with
  bounded network waits, and `Safely Quitting...` shimmers in the terminal
  while the remaining database and connection cleanup completes.

## v0.9.0

### Changed

- **Every overlay now wears the soft-panel look.** The restyle that landed for
  Settings and the Account Manager in v0.8.0 now covers the rest of the app:
  compose, help, the theme picker, the command palette, AI summary, the move,
  save-attachment and filter pickers, the contact manager and all five of its
  sub-views, and the save-draft, quit, bulk-delete and update confirms. The old
  square chrome and shouty uppercase headers are gone — overlays are a rounded
  border with the title in the top rule, focus and selection are a left accent
  rail, and key hints are one quiet lowercase line.
- **Compose shows the sender in the From row** instead of a separate header,
  and the vim-mode indicator moved down to the footer.

### Fixed

- **`ctrl+e` toggles full headers in threaded view.** The threaded renderer
  ignored the toggle, so it only worked in the non-threaded reading pane.

## v0.8.0

### Added

- **Instant new mail via IMAP IDLE.** Accounts with `sync_minutes > 0` now hold
  a push connection per account, so new mail syncs — and desktop-notifies — the
  moment the server announces it instead of at the next polling tick. Interval
  polling stays on as the fallback, and servers without IDLE quietly keep
  polling alone. Watchers reconnect with backoff and log diagnostics to
  `fetch.log`.
- **Help is searchable.** Press `/` in the help overlay to filter shortcuts as
  you type; `enter` keeps the filter while you scroll, `esc` clears it.

### Changed

- **Settings and the Account Manager have a new look.** The heavy accent header
  bars are gone: overlay titles now live in a rounded border
  (`╭─ tidemail · settings ─╮`), focus is a left accent rail, toggles read
  `● on / ○ off`, pickers end in `‹›`, and the key hints are one quiet
  lowercase line. Account cards are tighter (3 lines instead of 5). VT52 and
  other ASCII terminals degrade gracefully.
- **The Display settings section is organized into topical groups** —
  Appearance, Terminal colors, Message list, Reading, and Behavior — instead of
  one long list, and keyboard order follows the visual order.

### Removed

- **The Outlook provider preset.** Microsoft disabled password/app-password
  IMAP sign-in for Outlook.com in 2024, so the preset could only ever produce a
  failed login. Existing accounts saved with the Outlook preset now edit as
  Custom with their saved servers intact.

## v0.7.0

### Security

- **Email content can no longer hijack your terminal.** Message subjects,
  sender names, attachment filenames, and bodies were rendered with raw
  control bytes intact, so a crafted message could emit terminal escape
  sequences when viewed — manipulating the clipboard (OSC 52), spoofing the
  UI, or moving the cursor. Control bytes are now stripped from all untrusted
  message text, both when it's stored and when it's drawn (covering escapes
  hidden behind HTML entities such as `&#27;`).
- **In-app updates are now integrity-checked.** Each release publishes a
  `SHA256SUMS` file; the updater pins downloads to GitHub and verifies the
  downloaded archive's checksum before installing, refusing to replace the
  binary if the checksum is missing, unreachable, or doesn't match. Previously
  the self-update trusted whatever it downloaded.
- **Desktop notifications are hardened** against a sender or subject beginning
  with `-` being misread as a `notify-send` option.

### Fixed

- **Deleting a Gmail message no longer reports a false "delete failed".** Gmail
  returns a technically-malformed `COPYUID` response on a successful move to
  Trash that the IMAP library rejected, so the message was moved on the server
  but left visible locally with an error. The move is now recognized as the
  success it is.

## v0.6.3

### Fixed

- **Accounts no longer get stuck on the syncing spinner.** After folder
  auto-refresh was added, a stalled server folder listing could hold an
  account's IMAP connection open indefinitely, blocking that account's inbox
  sync so it spun forever (other accounts kept working). IMAP operations are
  now bounded by a timeout — a hung connection fails and is redialed instead of
  wedging the account.

## v0.6.2

### Fixed

- **Terminal no longer left broken after quitting.** TideMail sets the
  terminal's default colors to match the theme and resets them on exit. That
  reset was skipped when the program ended through an error path or `Ctrl+C`,
  leaving the shell prompt drawn in the theme's colors (an invisible / garbled
  prompt). Cleanup now runs on every exit path.
- **In-app update restart no longer corrupts the terminal.** "Restart after
  update" previously launched the new version while the old one still held the
  terminal, so two processes fought over it. The app now quits first — letting
  the terminal fully restore — then re-execs the freshly installed binary for a
  clean handoff.

## v0.6.1

### Added

- **Toggle hidden files in the file browsers.** The attach-file picker
  (compose) and the save-attachments picker hide dotfiles by default; press
  `.` to show hidden files and folders, and `.` again to hide them. The choice
  persists as you navigate directories. The `.` hint now appears in each
  browser's footer.

### Fixed

- The compose attach-file picker footer no longer clips its second row of key
  hints (it reserved too few lines for the action bar).
- Account form field navigation stops at the first and last field instead of
  wrapping around.
- The inbox now syncs immediately on startup rather than waiting for the first
  timer tick.
- Near-black terminal themes (vt100, vt52) render monochrome phosphor text and
  a more visible selection background, and inline markdown styles set an
  explicit foreground so text no longer falls back to the terminal default.

## v0.6.0

### Added

- **Optional vim (modal) editing in the compose body.** Turn it on in
  Settings → Editor (or `[display] compose_vim` in config). The message body
  gains Normal / Insert / Visual modes with the common vim grammar:
  - Motions: `h j k l`, `w b e`, `0 ^ $`, `gg G`, and numeric counts (e.g. `3j`);
    arrows work too.
  - Edits: `x`, `dd`, `yy`, `p`/`P`, `D`, `C`, `s`, and the `d`/`c`/`y` operators
    with motions (`dw`, `cw`, `d$`, …).
  - Visual `v` / `V`, undo/redo `u` / `ctrl+r`.
  - A `:` command line where `:w` / `:wq` / `:x` send and `:q` (or double-`Esc`)
    cancels.
  - A mode indicator in the compose title and a mode-aware cursor — a block in
    Normal/Visual, a thin bar in Insert.

  Vim mode is **off by default**, so nothing changes unless you enable it. It is
  powered by the [ripple](https://github.com/allisonhere/ripple) editor library.
- A new **Editor** category in Settings, home to the vim toggle.

### Changed

- **`ctrl+p` now opens the command palette everywhere** (it previously pasted
  inside compose). `ctrl+v` is the single paste key. In a vim compose body, `:`
  is the editor's command line — use `ctrl+p` to open the palette there.
- Settings reorganized: the mislabeled "Accounts" section (which only held the
  feed max-body size — not account settings) is replaced by the new **Editor**
  category, and the feed max-body knob moved to **Advanced**. Account details
  are still managed in the Account Manager (`M`).
- Replying now places the cursor at the top of the body, above the quoted text,
  ready to type.

### Internal

- The compose editor library [ripple](https://github.com/allisonhere/ripple) is
  bumped to v0.2.0, which adds the opt-in vim mode.

## v0.5.2

### Internal

- The compose message-body editor now lives in its own published module,
  [`github.com/allisonhere/ripple`](https://github.com/allisonhere/ripple)
  (v0.1.0), instead of the in-tree `internal/editor` package. No user-facing
  changes. The library adds a configurable `KeyMap`; TideMail uses the defaults.

## v0.5.1

### Fixed

- Compose: the message-body caret no longer sticks to the top while typing a
  reply. The stored editor was sized by width only and collapsed to a single
  row, so freshly typed lines scrolled off the top behind the quoted message;
  it is now sized to the same height the form renders.

### Internal

- The compose editor adopts the idiomatic Bubble Tea `Update(msg) (Model,
  tea.Cmd)` signature and owns copy/cut/paste through a pluggable clipboard,
  replacing the host-managed clipboard glue — work toward extracting it as a
  standalone Go TUI editor library.

## v0.5.0

### Highlights

A new, owned keyboard-first compose editor replaces the stock textarea for the
message body, bringing real text editing to compose:

- Text selection (`shift`+arrows), word selection (`ctrl+shift`+arrows), and
  select-all (`ctrl+a`)
- Undo / redo (`ctrl+z` / `ctrl+y`)
- Copy / cut / paste (`ctrl+c` / `ctrl+x` / `ctrl+v`) via the system clipboard
- Word-wise movement (`ctrl`+arrows), logical-line Home/End, and smooth soft-wrap

### Changed

- **Sign in with an App Password — OAuth removed.** Google accounts now
  authenticate with an app password instead of "Sign in with Google", removing
  the forced re-authentication every ~7 days that came with unverified OAuth
  apps.
  - If you used "Sign in with Google": open the Account Manager (`M`), edit your
    Gmail account, and paste a Google **App Password** (enable 2-Step
    Verification → myaccount.google.com/apppasswords). The account form walks
    you through it.
  - If you already use an app password: nothing changes.
- **Attach file moved to `alt+f`** (freeing `ctrl+a` for select-all in the body).

### Fixed

- Compose body no longer spills onto the left edge of the screen when replying
  (carriage returns and grapheme-width mismatches in quoted email).
- Up/down arrow navigation now tracks wrapped lines instead of jumping or sticking.
- Cut (`ctrl+x`) now copies the removed text to the clipboard, not just deletes it.
- BCC edits are now included in compose autosave.

### Internal

- Removed the `golang.org/x/oauth2` dependency.
