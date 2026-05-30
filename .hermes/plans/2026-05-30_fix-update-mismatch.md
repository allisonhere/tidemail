# Plan: Fix Update Asset Name Mismatch Properly

## Context

Commit a54a822 renamed the repo from `tide` to `tidemail`. The release workflow was updated to build `tidemail-*` binary names, but the updater's `assetName()` was never updated — it still returns `"tide-" + goos + "-" + arch`.

So every version since the rename has shipped with a broken update checker:
- v0.1.8: workflow built `tidemail-linux-x86_64.tar.gz`, updater looked for `tide-linux-x86_64.tar.gz` ✗
- v0.2.0: same bug, but I temporarily fixed it by reverting workflow to `tide-*` and re-releasing

The hacky fix (change workflow back to `tide-*`) works for now but leaves the codebase inconsistent — the rest of the project is called `tidemail`, and future release builders will wonder why assets are named `tide-*`.

## Proper fix

Two parts:

### Part 1: Revert workflow back to `tidemail-*`

Change `.github/workflows/release.yml` back to building `tidemail-linux-x86_64` etc. This is the right name for the app.

### Part 2: Update the updater to match

In `internal/update/update.go`, change `assetName()` from:
```go
return "tide-" + goos + "-" + arch
```
to:
```go
return "tidemail-" + goos + "-" + arch
```

## After the fix

Delete and re-create the v0.2.0 tag so the release action rebuilds with `tidemail-*` names. The release will end up with only `tidemail-*` assets (plus the orphaned `tide-*` from the previous run — GitHub doesn't delete old assets, but the updater won't look for them).

Users on any version (v0.1.7 looking for `tide-*`, or v0.1.8/v0.2.0 looking for `tidemail-*`) will get the correct assets. The only gap: v0.1.8 users won't be able to update to v0.2.0 because the updater in v0.1.8 expects `tide-*`. But since v0.1.8 has the bug, its updater has never worked — those users are already on v0.1.8, they'd need to install v0.2.0 manually once.

## File changes

| File | Change |
|------|--------|
| `.github/workflows/release.yml` | Revert target names to `tidemail-*` |
| `internal/update/update.go` | Change `"tide-"` to `"tidemail-"` in `assetName()` |

## Verification

```go
// Test both produce the right name
u := update.New()
name, _ := u.assetName()
// linux/amd64 → "tidemail-linux-x86_64"
```
