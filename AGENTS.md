# Contributor guidance

## Keep documentation current

When changing user-facing TideMail behavior, update the relevant help text and
documentation in the same change. This includes keyboard shortcuts, settings,
commands and flags, account/authentication flows, search behavior, and layout
or status-line changes.

The documentation site lives at `~/Projects/tide-help`. Update its Markdown
source under `content/tidemail/` when the change needs a user-facing guide or
reference update, then run `npm run build` there so generated files stay in
sync. If no documentation change is needed, briefly note why in the change
summary.

## At release time

Three things name the current version and drift apart silently, because
`deploy.sh` updates none of them. Do all three in the release commit:

1. **`CHANGELOG.md`** — retitle the `## Unreleased` block to the version being
   tagged, and open a fresh empty `## Unreleased` above it. Leaving shipped
   entries under `Unreleased` means the next release announces them a second
   time while its own work goes unannounced.
2. **`README.md`** — rewrite the `## What's new in vX.Y.Z` section so its
   heading and its bullets match the release just tagged. It describes what
   someone gets from `install.sh`, so it must never list unreleased work.
3. **`STATUS.md`** — update the `Current release:` line.

The version in the tag is the source of truth; the other three follow it.
