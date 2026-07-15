# Changelog

All notable changes to TideMail are documented in this file.

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
