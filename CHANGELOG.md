# Changelog

All notable changes to TideMail are documented in this file.

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
