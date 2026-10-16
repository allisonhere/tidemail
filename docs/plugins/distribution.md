# Distributing plugins: the official catalog

TideMail's **Settings → Plugins → `b` (browse available)** lists the plugins
published in the [tidemail-plugins](https://github.com/allisonhere/tidemail-plugins)
repository and installs, updates and removes them. This page describes what that
repository's release must contain and what TideMail does with it. If you write a
plugin for yourself, none of this is needed: copy it into the plugins directory
as described in [manifest.md](manifest.md).

## The release

Every release of the plugins repository carries these assets:

| Asset | What it is |
| --- | --- |
| `catalog.json` | the list of plugins, described below |
| `<id>-<version>-<os>-<arch>.tar.gz` | one tarball per plugin and platform |
| `SHA256SUMS` | the same hashes as `catalog.json`, for people |

TideMail reads `catalog.json` from
`https://github.com/allisonhere/tidemail-plugins/releases/latest/download/catalog.json`,
and only when the user opens the available-plugins view.

## `catalog.json`

```json
{
  "schema": 1,
  "plugins": [
    {
      "id": "smart",
      "name": "TideMail Smart (JEV)",
      "description": "Tag mail that needs a reply, is urgent or important",
      "version": "0.2.0",
      "api": 1,
      "homepage": "https://github.com/allisonhere/tidemail-plugins/tree/main/plugins/smart",
      "permissions": {"message_metadata": true, "network": true, "annotations": true},
      "assets": {
        "linux-amd64": {"url": "https://github.com/…/smart-0.2.0-linux-amd64.tar.gz", "sha256": "<64 hex digits>"},
        "darwin-arm64": {"url": "…", "sha256": "…"}
      }
    }
  ]
}
```

- `schema` is `1`. A TideMail that doesn't understand the schema says so and
  shows nothing.
- `id`, `version` and `permissions` repeat the plugin's `plugin.toml`. The
  version is `MAJOR.MINOR.PATCH`; an update is offered when it is newer than the
  installed plugin's.
- `api` must be `1`; entries for other plugin APIs are skipped.
- `assets` is keyed by `<GOOS>-<GOARCH>`. A plugin with no entry for the
  user's platform is listed as not available there.
- Names and descriptions are plain text; control characters are rejected.

## The tarball

A flat set of regular files: `plugin.toml`, the executable the manifest names as
`command`, and optionally a README or licence. No directories, symlinks, hidden
files, or more than 16 files. The manifest inside must agree with the catalog on
`id`, `version`, `api` and every permission.

## What TideMail does

1. Fetches the catalog (HTTPS, GitHub hosts only, at most 1 MiB) and validates
   every entry.
2. On install or update, asks the user first, showing the permissions.
3. Downloads the tarball (GitHub hosts only, including redirects; at most 64
   MiB), and requires its SHA-256 to equal the catalog's.
4. Unpacks it into a hidden staging directory inside the plugins directory,
   makes only the manifest's `command` executable, and validates the manifest
   exactly as at startup.
5. Checks the manifest against the catalog entry (`id`, `version`, `api`,
   permissions). Any difference stops the install.
6. Moves the staged directory into place. When updating, the old directory is
   moved aside first and put back if the move fails.
7. Reloads the plugin list. **The plugin is never run during any of this.**

Removing a plugin deletes its directory. Its settings (kept in TideMail's
config) and the annotations it stored (kept in the database) stay, as they do
when you delete a plugin by hand.

## Trust

The checksum comes from the same release as the tarball, so it protects against
corrupt or mismatched downloads, not against someone who controls the plugins
repository. Plugins are ordinary programs that run with the user's privileges
([security.md](security.md)); the confirm step says so and lists what the plugin
asks for. There is no signature scheme yet.
