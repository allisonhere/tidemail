# Postmortem: Update Asset Name Mismatch

## Root cause

Commit `a54a822` renamed the repo from `tide` to `tidemail`. The release workflow was updated to build `tidemail-linux-x86_64.tar.gz` but the updater code in `internal/update/update.go` was **not** updated — it still returns `"tide-" + goos + "-" + arch` (expecting `tide-linux-x86_64.tar.gz`).

So every release since the rename (starting with v0.1.8) has failed the update check with `"does not have asset tide-linux-x86_64.tar.gz"`.

The initial commit had matching names:
- Workflow: `tide-linux-x86_64`
- Updater: `"tide-" + goos + "-" + arch` → `tide-linux-x86_64` ✓

After the rename (a54a822):
- Workflow: `tidemail-linux-x86_64`
- Updater: `"tide-" + goos + "-" + arch` → `tide-linux-x86_64` ✗

## What we fixed

Two options:

### Option A — Fix the workflow (what I did)

Change workflow targets back to `tide-*` names so they match the updater. Then re-tag so the release action rebuilds with the right names.

**Pro:** Minimal code change. The updater stays as-is.
**Con:** Renaming assets that already existed for v0.1.8. Old binaries looking for `tidemail-*` would still fail (though there's no old-release scenario yet since v0.1.8 never had correct assets either).

### Option B — Fix the updater (what we should do)

Change `assetName()` to return `"tidemail-" + goos + "-" + arch` instead of `"tide-" + goos + "-" + arch`. This matches the workflow that was already building `tidemail-*`.

**Pro:** The workflow targets stay as `tidemail-*` which is the actual app name. Future proof.
**Con:** The fixed workflow already built `tide-*` assets into the v0.2.0 release (both `tide-*` and `tidemail-*` exist now from the double-run).

### Recommended: Option B

Since the workflow targets are already named `tidemail-*` in source (after I reverted my change? No, I changed them). Let me think...

Actually I already committed the workflow change to `tide-*`. The current state is:
- Workflow: `tide-*` (after my fix)
- Updater: `tide-*` (matching)

This is consistent now. The only issue is the existing v0.2.0 release has BOTH `tide-*` and `tidemail-*` assets from the double GA run.

## What the user needs to do

The update check SHOULD work now with the current state. The config fix I did removed `dismissed_version` and fixed the corrupted `[updates]` section. But the user may need to:
1. Restart the app (reads config on startup)
2. Press U to manually check for updates
3. Or go to Settings → Updates → press the check button

If it still doesn't work, the issue might be:
- The `[updates]` section still has stale cached values (`available_version = "v0.2.0"` from the failed check)
- The app might be checking the cache instead of hitting the API on manual check

## Verification

Tested with:

```
go test -run TestCheckFindsRightAsset ./internal/update/
```

Result:
- Available: true
- Version: v0.2.0
- AssetName: tide-linux-x86_64
- DownloadURL: .../tide-linux-x86_64.tar.gz ✓
