# Postmortem: The Update System Mess

## Timeline

### Before the repo rename — everything worked

The app was called `tide`. The workflow built `tide-linux-x86_64.tar.gz`. The updater looked for `tide-linux-x86_64.tar.gz`. They matched. v0.1.0 through v0.1.7 shipped with a working update system.

### Commit a54a822 — the repo rename

The repo was renamed from `allisonhere/tide` to `allisonhere/tidemail`. The release workflow was updated to build `tidemail-linux-x86_64.tar.gz`. But the updater's `assetName()` in `internal/update/update.go` was **never updated** — it still returned `"tide-" + goos + "-" + arch`. 

So v0.1.8 shipped with a broken update checker. It looks for `tide-linux-x86_64.tar.gz` but the workflow builds `tidemail-linux-x86_64.tar.gz`. Every v0.1.8 user who checks for updates gets "does not have asset".

### The v0.2.0 release — three rebuilds, three asset sets

We pushed v0.2.0 with the OAuth2 features. The workflow initially built `tidemail-*` assets. The updater in the source (HEAD) still looked for `tide-*`. Three iterations followed:

1. **Tag push #1**: workflow builds `tidemail-*`. Release gets `tidemail-*` assets. v0.1.8 users can't find them. I "fixed" the workflow by changing names to `tide-*`.

2. **Tag push #2**: workflow builds `tide-*`. Release gets `tide-*` added (plus the old `tidemail-*`). v0.1.8 users can now find assets. But the fix was wrong — the updater was unchanged, still looking for `tide-*`, which only matched because we changed the workflow to match the bug.

3. **Tag push #3**: reverted workflow back to `tidemail-*` AND updated the updater to return `tidemail-*`. This is the correct fix. The release now has BOTH `tide-*` and `tidemail-*` assets from the accumulated rebuilds.

### The dismiss trap

v0.1.8 users who checked for updates during this mess hit this sequence:
1. First check: succeeds (finds `tide-*` assets from rebuild #2) — shows "v0.2.0 available"
2. User presses `i` to dismiss → `dismissed_version = "v0.2.0"` saved to config
3. Every subsequent check: `updateDismissed` is true (version matches dismissed) → shows "dismissed", no install possible even from Settings

This is a separate code bug fixed in commit `2c1c5f3`: the Settings check button should override the dismiss flag. But that fix only ships in the NEXT binary. Stuck v0.1.8 users can't benefit from it.

## Current state

| Component | What it expects | Status |
|-----------|----------------|--------|
| v0.1.8 updater | `tide-linux-x86_64.tar.gz` | ✓ Found in release (from rebuild #2) |
| HEAD updater | `tidemail-linux-x86_64.tar.gz` | ✓ Found in release (from rebuild #3) |
| release workflow | builds `tidemail-linux-x86_64.tar.gz` | ✓ Correct |
| install.sh | downloads `tidemail-linux-x86_64.tar.gz` | ✓ Correct |
| deploy.sh | builds `tidemail-linux-x86_64.tar.gz` | ✓ Correct |

Everything is consistent now. The only problem is v0.1.8 users who dismissed v0.2.0 — they're stuck in the dismiss trap until they either clear their config or install via curl.

## What should have happened

The repo rename commit (a54a822) should have updated BOTH:
```
- workflow: tide-* → tidemail-* ✓
- updater:  "tide-" → "tidemail-" ✗ (missed)
```

One-line change in `internal/update/update.go`, line 369:
```go
- return "tide-" + goos + "-" + arch, nil
+ return "tidemail-" + goos + "-" + arch, nil
```

## Recommendations

1. **No more re-tags for v0.2.0** — the release already has both `tide-*` and `tidemail-*` assets, which covers both old and new updaters. Stop touching the tag.

2. **Bump to v0.2.1** for the dismiss-override fix. The accumulated patch-level changes (dismiss override in Settings, updater name fix, sync timer fix, collapse persistence, etc.) deserve a fresh version that doesn't carry the baggage of v0.2.0's three rebuilds.

3. **Add a test** that verifies `assetName()` matches the workflow target names. A simple unit test or a CI step that grep-checks `release.yml` against `assetName()`.

4. **Add a "Clear dismissed" button** in Settings → Updates so users can un-dismiss a version without editing config manually.
