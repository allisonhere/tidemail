# Changelog

All notable changes to TideMail are documented in this file.

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
