# Plugin management: browse, install, update, remove from inside TideMail

## Context
Installing a plugin today is manual: build it with Go, copy the binary and `plugin.toml` into
`~/.config/tidemail/plugins/<id>/`, restart. The official collection (`github.com/allisonhere/tidemail-plugins`:
`smart`, `analytics`, `unsubscribe-plus`) has **no releases and no prebuilt binaries**, so most users can't
install anything. You chose: prebuilt downloads; browse + install + update + remove; and moving the new PGP
plugin into the plugins repo so it shows up in the same list.

Out of scope for v1: Windows plugin binaries (TideMail's Windows build is experimental), a CLI
`tidemail plugin install`, third-party catalogs, signature checking beyond SHA-256 (see Security).

## How it works
1. **Plugins repo cuts a release.** CI builds every plugin for linux/darwin x amd64/arm64 (CGO off), packs
   `<id>-<version>-<os>-<arch>.tar.gz` (manifest + binary + README), and generates `catalog.json` listing each
   plugin's id, name, description, version, API, permissions, and per-platform asset URL + SHA-256. Assets:
   tarballs, `catalog.json`, `SHA256SUMS`.
2. **TideMail fetches the catalog on demand** from
   `https://github.com/allisonhere/tidemail-plugins/releases/latest/download/catalog.json`, only when you open
   the Available view (never in the background).
3. **Install** downloads the tarball for your OS/arch, checks the SHA-256 from the catalog, extracts to a temp
   directory inside the plugins dir, validates it with the existing `plugin.LoadPlugin` (manifest valid, id and
   version match the catalog, command exists and is executable), then swaps it into
   `plugins/<id>/` atomically (old copy kept until the swap succeeds). The plugin list reloads live.
4. **Update** appears when the catalog version is newer than the installed manifest version; same path as install.
5. **Remove** deletes `plugins/<id>/` after a confirm. Stored annotations stay (existing "stored data from
   removed plugins" behavior); plugin settings stay in config.

## TideMail changes (branch `feat/plugin-catalog`)
- **New package `internal/plugincatalog`**
  - `catalog.go`: types, `Fetch(ctx, client)` with 1 MiB cap, strict decode and validation (`plugin.ValidateID`,
    https + trusted GitHub hosts, 64-hex SHA-256, permission names from a fixed set, no duplicate ids).
  - `install.go`: `Install(ctx, entry, pluginsDir)` and `Remove(id, pluginsDir)`. Safe tar extraction (regular files
    only, flat names, no symlinks/absolute/`..`, file count and total size caps, 0755 for the command, 0644
    otherwise), SHA-256 check before extraction, validation via `plugin.LoadPlugin`, atomic swap with rollback.
  - Tests with `httptest` servers and generated tarballs: checksum mismatch, path traversal, symlink entry, wrong
    id/version, oversize, missing platform, swap rollback, remove.
- **Reuse**: export `update.TrustedAssetHost` (currently the unexported `isTrustedAssetHost` in
  `internal/update/update.go`) and mirror its download pattern (User-Agent, timeouts, size limits,
  `sha256File`). Reuse `update.IsNewerVersion` for update detection. Reuse the permission checklist renderer
  already in `internal/ui/plugins.go` (~line 1100) for the install confirm.
- **UI** (`internal/ui/plugin_catalog.go`, plus edits in `plugins.go`, `settings_plugins.go`, `msgs.go`,
  `model.go`, `keys.go`, `help.go`):
  - Settings -> Plugins gets a `b` "browse available" view listing catalog entries (name, version, one-line
    description; badges: installed, update available). Loaded asynchronously with a status line; `r` refreshes.
  - `i` install / `u` update opens a confirm showing the permissions checklist and plain-words warning
    ("runs with your privileges; downloaded from GitHub, checksum verified"). Installing never enables automatic
    processing (still the existing separate opt-in).
  - `x` on an installed plugin removes it (confirm).
  - After success: `plugin.Discover(dir)` then the existing `SetPlugins` (already safe to call again: cancels
    workers, preserves annotation cache). Refuse install/remove while a plugin run is in flight
    (`m.plugins.running`), with a status message.
  - Network/catalog failures show a readable one-line error; the installed list is unaffected.
  - Check the plugin-list key handler (`handlePluginListKey`) for existing bindings before assigning `b/i/u/x`.
- **Docs in the same change** (AGENTS.md): `docs/guide.md` (Plugins), `README.md`, `STATUS.md`, `help.go`,
  `keybindings.csv`, `CHANGELOG.md` (Unreleased), `docs/plugins.md`, `docs/plugins/security.md` (install trust
  model), `docs/plugins/manifest.md` or a new `docs/plugins/distribution.md` (catalog format, release layout).

## Plugins repo changes (branch `feat/releases` in `~/Projects/tidemail-plugins`; nothing pushed without asking)
- Fix the stale CI ref (`ref: experiment/plugin-system`) to TideMail `main` for the developer CLI.
- **Move PGP in**: `plugins/pgp` (main.go, gpg.go, plugin.toml, README, real-gpg unit tests). The end-to-end test
  imports TideMail internals, so it stays in TideMail as `internal/pgpmail/e2e_test.go`, builds the plugin from
  `TIDEMAIL_PLUGINS_DIR` (skips if unset), and the plugins repo's CI (which already checks out TideMail) runs it.
  Remove `examples/plugins/pgp` from TideMail once the plugins repo has it; keep `docs/plans/pgp-plugin.md`.
- `.github/workflows/release.yml`: on tag `v*`, matrix build + test, tarballs, `scripts/make-catalog` (small Go
  program reading each `plugin.toml` + the built tarballs' hashes), upload assets.
- README: install instructions now "Settings -> Plugins -> b".

## Security
- Trust anchor is the plugins repo's release. Downloads only from github.com / githubusercontent.com over HTTPS;
  SHA-256 comes from the same release's catalog (protects against corruption and mismatched assets, not against
  a compromised repo; say so in the docs). No signature scheme in v1.
- Nothing runs during install (no `plugin.test`, no execution). Plugins are still not sandboxed; the confirm and
  docs say so, and permissions are shown before install.
- Extraction is allowlist-based (see above); the manifest is re-validated from disk, never trusted from the
  catalog; id and version must match the catalog entry.
- Replacing a manually installed plugin with the same id is allowed only through the confirm, which says so.

## Order of work
1. `internal/plugincatalog` + tests (no UI). 2. UI browse/install/update/remove + tests with an injected fake
catalog client. 3. Docs. 4. Plugins repo: release workflow + catalog generator, then PGP move. 5. Cut a first
plugins release (needs your go-ahead) and test the real flow end to end.

## Verification
- `go test -race ./...`, `gofmt`, `go vet`, `golangci-lint`, Windows cross-vet; `internal/plugin/docs_test.go`
  stays green.
- Unit tests above, plus UI tests: browse shows entries and badges, install confirm lists permissions, success
  reloads the list, failure leaves the old copy, running-plugin guard, remove.
- Manual after the first release: `go run .` -> Settings -> Plugins -> `b` -> install `unsubscribe-plus`,
  see it listed without a restart, run it on a message, remove it. Also tamper test: edit a SHA in a local copy
  of the catalog served from a test server and confirm install refuses.
