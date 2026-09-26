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
  - A docs commit adding this handover and `PLAN.md` and correcting the
    release number in `STATUS.md` to v1.0.27.

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

After `f6c6d85`, all repository checks passed:

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

- Live terminal image placement has not received a final visual pass after the
  cache fixes. Test it in Ghostty or Kitty before release.
- WezTerm uses text placeholders because this backend requires Kitty Unicode
  placeholders and virtual placements.
- Animated GIFs show their first frame. SVG and remote CSS backgrounds are not
  rendered.
- Newsletter presentation tables are flattened in DOM order. Side-by-side cards
  stack, and boundaries between adjacent cards can still be weak.
- Complex row-spanning data tables use the simpler text fallback.
- Image layout records height hints but currently sizes primarily from width;
  terminal cell geometry is detected only when the image store is created.

## Recommended next step

Implement Milestone 1 in `PLAN.md`: preserve newsletter content-group boundaries
through layout-table flattening. Start with normalization and rendering tests,
then change the DOM pass. Avoid coupling this work to `tideui`; it is specific to
email HTML and belongs in TideMail.

Before editing, confirm the working tree is clean. After implementation, run the full
quality gate above and manually inspect at least one narrow and one wide frame.

## Documentation debt

`STATUS.md` and `CONTRIBUTING.md` refer to a root `CLAUDE.md`, but that file is
not present on this branch. Restore an accurate package map or update both links
in a separate documentation change; do not treat the email-rendering handover as
a complete architecture guide for the rest of TideMail.
