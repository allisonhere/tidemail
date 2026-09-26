# TideMail email rendering handover

Updated: 2026-09-26

## Repository state

- Branch: `main`
- Local `main` is ahead of `origin/main` and has not been pushed:
  - `c66b7c7` fixes image cache bounds, remote reload state,
    Content-Location resolution, and terminal image cleanup.
  - `f6c6d85` adds receipt detection, aligned numeric columns, merged totals,
    and narrow labelled rows.
  - `fa46050` separates `deploy.sh` output from its menu rows (unrelated to
    email rendering).
  - `c5d6bef` adds this handover and `PLAN.md` and corrects the release number
    in `STATUS.md` to v1.0.27.
  - `9d960f1` keeps newsletter cards together (Milestone 1) and adds the
    synthetic fixtures in `internal/ui/testdata/mail/`.
  - `4c25652` moves image uploads off the event loop and keeps uploaded images
    resident, fixing message-list lag on image-heavy mail.
  - `b5eb396` honours image height hints and refreshes cell geometry on resize
    (Milestone 2).

## What is working

The renderer normalizes untrusted HTML, converts it to Markdown, and renders it
with the active TideMail theme. It preserves headings, lists, quotes, preformatted
blocks, semantic tables, actionable links, and inline raster images. Unsupported
terminals and `[display] images = "off"` use text placeholders.

Image rendering is split across:

- `internal/richmail`: source parsing, MIME-part resolution, decode limits, SSRF
  policy, and cell layout.
- `internal/termimage`: terminal detection and Kitty Unicode-placeholder upload.
- `internal/ui/rich_images.go`: consent, caches, async remote loads, render plans,
  and viewport integration.

Receipt rendering lives in `internal/ui/email_tables.go`; the surrounding DOM
normalization and HTML-to-Markdown rules remain in `internal/ui/format.go`.

## Validation completed

After `b5eb396`, all repository checks passed:

```sh
gofmt -w .
go build ./...
go vet ./...
go test ./... -race
golangci-lint run
git diff --check
```

Receipt tests cover wide and narrow panes, semantic and headerless tables,
currency formats, discounts, merged totals, Unicode, malformed spans, and
newsletter false positives. Image regression tests cover cache eviction,
Content-Location privacy, duplicate fetch suppression, and terminal cleanup.

## Known limitations and risks

- Milestones 1 and 2 and the upload fix were checked by hand in a live
  terminal on 2026-09-26.
- WezTerm uses text placeholders because this backend requires Kitty Unicode
  placeholders and virtual placements.
- Animated GIFs show their first frame. SVG and remote CSS backgrounds are not
  rendered.
- Newsletter content groups (Milestone 1) are detected in
  `internal/ui/email_groups.go`. Cards made of several rows spread across
  separate layout tables are only grouped when a wrapping cell contains them;
  a card split across sibling rows of one table is not recognised.
- Complex row-spanning data tables use the simpler text fallback.
- Image uploads are asynchronous: `applyImages` only queues, `Model.Update`
  collects `takeUploadCmd`, and the command encodes after a 90 ms debounce.
  Uploaded images stay resident in the terminal under an LRU budget and are
  deleted by `ReleaseTerminalImages` on exit. A cell-geometry change calls
  `clearImages` before updating the store's geometry; the backend is never
  swapped, so in-flight upload commands stay safe.
- Image layout fits width and height hints with one scale factor. Cell
  geometry is re-read on each resize; terminals that report no pixel size keep
  the default or last valid geometry.

## Recommended next step

All three milestones in `PLAN.md` are complete. The shared fixtures live in
`internal/ui/testdata/mail/`, and `TestMailFixturesRenderBounded` renders every
fixture at 24, 40, 80, and 120 columns in plain and styled modes.

Candidates for further work, only if real mail shows the need:

- Group cards whose image, heading, and button sit in sibling rows of one
  table.
- Revisit height-hint handling if senders with mismatched width/height hints
  render noticeably smaller than in a browser.

Avoid coupling this work to `tideui`; it is specific to email HTML and belongs
in TideMail.

Before editing, confirm the working tree is clean. After implementation, run the full
quality gate above and manually inspect at least one narrow and one wide frame.

## Architecture reference

`docs/architecture.md` holds the package map and key flows for the whole
application. `CONTRIBUTING.md` and `STATUS.md` link to it.
