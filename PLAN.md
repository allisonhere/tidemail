# TideMail email rendering plan

Updated: 2026-09-26

## Goal

Make HTML mail read like a coherent document in a terminal while keeping the
renderer private by default, responsive during navigation, and bounded for
untrusted input. Preserve semantic relationships first; reproduce browser
styling only where it improves comprehension.

## Current baseline

- Inline CID, data-URI, and Content-Location images render through Kitty
  Unicode placeholders on supported terminals. Remote images require `i`.
- Image payloads, decoded images, and MIME-part indexes have bounded caches.
- Receipts preserve item, quantity, amount, and merged-total relationships.
  Narrow panes use labelled records instead of squeezing columns.
- The existing `reading_width` setting already caps prose width. No replacement
  setting is planned.

## Milestone 1: newsletter content groups

Status: implemented in `internal/ui/email_groups.go` with tests in
`email_groups_test.go`. Checked in a live terminal on 2026-09-26.

Keep an image, heading, description, and call-to-action together when flattening
presentation tables.

Implementation:

1. Extend HTML normalization with a newsletter-group pass before
   `normalizeEmailTables`. Recognize a row or cell as a content group only when
   it contains at least two semantic elements from: a non-tracking image, a
   heading, prose, and a link/button label. Exclude nested semantic data tables,
   quoted replies, signatures, and cells containing only navigation links.
2. Mark recognized group and row boundaries with TideMail-owned data attributes.
   Preserve these boundaries when layout tables become `div` elements, keeping
   DOM order. Do not express a group boundary as extra blank lines or `<hr>`:
   `html-to-markdown` collapses repeated `<br>`/blank lines to one, glamour
   collapses repeated blank lines again, and a styled `<hr>` renders as nothing
   (plain mode shows `--------`). Instead, have a converter rule emit a
   private-use sentinel line for each boundary, following the existing
   `imageMarker` pattern (`…` in `rich_images.go`), and replace it
   after `renderMarkdown` with a themed separator: a dimmed thin rule sized to
   the rendered width in styled mode, and a blank line plus a short ASCII rule
   in plain mode. Content inside a group keeps normal paragraph spacing. Strip
   any sentinel that survives on the plain-text, link-filter, and search paths.
3. Keep wide two-column newsletter rows readable without attempting browser
   layout: render each cell as one complete group, left to right, then top to
   bottom. Do not place two raster images beside each other in the first pass.
4. Add fixtures for a hero card, repeated product cards, image-only CTAs, nested
   layout tables, forwarded mail, and a narrow pane. Assert content order,
   single occurrence, link availability, bounded line width, and adjacency of
   each image label/placeholder with its heading and CTA.

Acceptance criteria:

- Repeated newsletter cards never interleave text from neighboring cells.
- Images and their captions/headings remain adjacent after resize.
- Group boundaries are visible in both plain and styled rendering, and no
  sentinel characters appear in rendered, copied, or searched text.
- Receipt detection and current quote, link, and image tests remain unchanged.

## Milestone 2: image geometry

Status: implemented. `richmail.Layout` applies one scale factor to both axes.
Rather than rebuilding the backend (the Kitty backend never reads the cell
size), the image store keeps the current geometry and `refreshGeometry` runs on
every `tea.WindowSizeMsg`. Checked in a live terminal on 2026-09-26.

Honor height hints and refresh terminal cell geometry after a resize.

Implementation:

1. In `richmail.Layout`, derive one scale factor from intrinsic dimensions,
   optional width and height hints, CSS max-width, available pixel width, and
   the row cap. Apply that single factor to both axes before converting to cell
   rows and columns. Never enlarge an image beyond its decoded size.
2. Treat width-only and height-only hints as proportional constraints. When both
   are present, fit within the hinted box without stretching. Keep the current
   one-cell minimum and 512-row safety limit.
3. On `tea.WindowSizeMsg`, re-read terminal pixel geometry. If a valid geometry
   changed, delete active terminal images, rebuild the backend with the same
   protocol and new cell metrics, bump the image generation, clear body and
   viewport caches, and render the current message again. Retain the last valid
   geometry when the terminal reports zero pixel dimensions.
4. Test portrait, landscape, conflicting hints, extreme aspect ratios, unknown
   geometry, and a resize that changes cell dimensions. Assert proportional
   output within one cell of rounding error and no stale placements.

Acceptance criteria:

- Sender height hints affect layout without image distortion.
- Resizing or changing terminal scale produces placements based on current cell
  dimensions and releases the previous terminal image data.

## Milestone 3: visual verification and release

Status: fixtures, automated width checks, and the live-terminal pass are done.

Create a small, synthetic fixture set under `internal/ui/testdata/` containing a
receipt, newsletter, reply thread, image-heavy message, malformed HTML, and long
international text. Fixtures must contain no private mail or live tracking URLs.
The set now exists in `internal/ui/testdata/mail/` (plus a forwarded
newsletter), and `TestMailFixturesRenderBounded` covers every file in it.

For each milestone, verify plain and styled rendering at 24, 40, 80, and 120
columns; run `gofmt -w .`, `go build ./...`, `go vet ./...`,
`go test ./... -race`, and `golangci-lint run`. Before release, manually inspect
Ghostty or Kitty for image placement and an unsupported terminal for the text
fallback. Update `README.md`, `STATUS.md`, `docs/guide.md`, and the changelog when
behavior changes.

## Out of scope

- Full CSS layout, arbitrary rowspans, animation, SVG, JavaScript, and automatic
  remote-image loading.
- Adding another image protocol until the Kitty path is visually verified and
  stable.
- Replacing the explicit reading-width preference. A later UX pass may propose
  an `auto` default, but that requires a separate product decision.
