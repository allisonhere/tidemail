# Inline Image Fixes Plan

Status: approved, pending implementation. Recorded September 8, 2026, against
`70f7987` on `feat/email-images`.

## Summary

Keep icons near their intended size, load images without clearing the screen,
and make the saved image setting control automatic loading. The user explicitly
selected automatic remote loading when **Images in message** is enabled.
Images remain off by default.

## Confirmed causes and implementation handoff

- `internal/imagepreview/inline.go`: `Grid` expands images to the available
  reading width without a natural-size cap, enlarging icons.
- `internal/ui/inline_images.go`: `handleInlineLoaded` returns `tea.ClearScreen`
  after every completed image. The output writer invalidates uploaded-image
  tracking on a screen clear, so subsequent writes re-upload existing images.
  The clear was added to address stale text seen during live toggle/resize tests;
  removing it must include verification that stale text does not return.
- The saved setting enables rendering but `requestedMessageID` separately gates
  remote downloads. If the setting is already on, `i` first disables it; a second
  press enables it and grants the current message permission. This is the source
  of the reported two-press behavior.

## Implementation changes

- Cap image size at its natural pixel dimensions and any smaller positive HTML
  width/height or inline CSS pixel dimensions. Preserve aspect ratio and shrink
  to fit the reading pane. Use terminal cell pixel measurements, refreshed on
  resize, with an 8×16-pixel fallback and a minimum one-cell footprint. Retain
  the existing 64-column/32-row bounds. Inline CSS pixel dimensions take
  precedence over the corresponding HTML attribute; ignore unsupported units.
- Share downloaded data for repeated sources but calculate display size and
  placement separately for each HTML occurrence. Unreferenced attachments use
  natural dimensions and the same reading-pane bounds.
- Remove the per-image full-screen clear. Update content through normal
  rendering and upload only new images or changed placements. Preserve cursor
  state and cached graphics across ordinary repaints. Correct stale-line
  rendering without clearing the terminal; actual terminal clears still require
  restoring graphics.
- Remove the per-message remote-download approval gate. The existing saved
  setting enables automatic embedded and remote loading for every opened email.
  `i` remains a saved on/off toggle; disabling it cancels pending work and hides
  images. Navigation cancels previous-message work, and stale results are ignored.
- Keep the command-palette loading action as an enable/retry action. Update its
  wording, settings hints, help, README, and guide to explain automatic remote
  loading and that remote servers can observe requests. Preserve the separate
  full-screen preview flow.
- Retain input/decoded-pixel limits, tracking-image filtering, timeouts, and
  unsupported-terminal fallback. Keep the existing `image_previews` config key;
  no migration or additional setting is needed. Existing enabled settings now
  authorize automatic remote loading.

Internal interfaces will need to carry cell pixel dimensions and per-occurrence
display bounds instead of relying solely on cell aspect ratio. No public CLI,
database, or configuration schema changes are planned.

## Validation and acceptance

- Test small icons, large photos, HTML/CSS dimensions, repeated sources with
  different sizes, missing cell measurements, and resize behavior.
- Verify an already-enabled setting loads remote images on opening each message;
  one `i` press hides them and another shows them. Verify cancellation and stale
  result handling when disabling images or navigating during a download.
- Verify image completion emits no full-screen clear and does not re-upload
  unchanged images. Verify changed placements and actual clears still recover.
- Run `go test ./...`, `go test -race ./internal/imagepreview ./internal/ui`,
  and `git diff --check`.
- Extend the synthetic interactive fixture with small icons and multiple images
  completing gradually. Live-test scrolling, resizing, overlays, and toggling:
  no flashing, stale text, oversized icons, or misplaced images. Do not use real
  mail or save user settings for this test.

## Delivery

Implement on `feat/email-images`, update user documentation to the completed
behavior, remove the pending-fix notices, then commit and push after validation.
The documentation commit recording this plan does not implement the fixes.
