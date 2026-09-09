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
