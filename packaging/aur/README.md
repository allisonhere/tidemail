# AUR packaging

TideMail ships to the AUR as **`tidemail-bin`**, which repackages the release
tarball published by `.github/workflows/release.yml`.

## Why a `-bin` package and not a source build

Release binaries are linked with the Google Desktop OAuth client credentials
(`scripts/build-release.sh`, fed from repository secrets). A source build on a
user's machine has no credentials, so Gmail sign-in would fail until every user
registered their own Google app. Packaging the official binary keeps sign-in
working out of the box.

## Files

| File | Purpose |
| --- | --- |
| `PKGBUILD.in` | The template. Edit this, never a rendered `PKGBUILD`. |
| `render-pkgbuild.sh` | Substitutes version, `pkgrel`, and checksums into the template. |
| `../linux/tidemail.desktop` | Desktop entry the package installs to `/usr/share/applications`. |
| `../../images/tidemail-icon.svg` | Icon the package installs to `/usr/share/icons/hicolor/scalable/apps/tidemail.svg`. |

Placeholders: `@PKGVER@`, `@PKGREL@`, `@SHA256_X86_64@`, `@SHA256_AARCH64@`,
`@SHA256_LICENSE@`, `@SHA256_ICON@`, `@SHA256_DESKTOP@`. Rendering fails if any placeholder survives, so a typo can
never publish a PKGBUILD that cannot build.

## Publishing

Use `./deploy.sh` → **AUR → Publish tidemail-bin**. It:

1. Confirms the GitHub release has both Linux tarballs and `SHA256SUMS`.
2. Reads the checksums from that published `SHA256SUMS`. **Checksums must come
   from the release assets, not a local `go build`** — local builds lack the
   OAuth credentials and hash differently.
3. Hashes the `LICENSE`, icon, and desktop entry blobs at the tag, which is
   what the PKGBUILD's `raw.githubusercontent.com` source URLs serve. Tags
   older than the icon do not carry these files, so they can no longer be
   published.
4. Clones the AUR repo and picks `pkgrel`: `1` for a new `pkgver`, otherwise
   one more than what is published.
5. Renders the PKGBUILD, generates `.SRCINFO` with `makepkg --printsrcinfo`,
   shows the diff, and pushes only after you confirm.

The AUR serves an empty git repository for any package name, so a successful
clone does not mean the package exists — only a committed `PKGBUILD` does.

## First-time setup

Register an AUR account and add your SSH public key under **My Account → SSH
Public Key**, then:

```
Host aur.archlinux.org
  User aur
  IdentityFile ~/.ssh/id_ed25519
```

`./deploy.sh` shows `aur ✓ <username>` in its status bar once the key is
accepted, and **AUR → AUR setup help** prints these steps with your key.

## Rendering by hand

```bash
bash packaging/aur/render-pkgbuild.sh \
  --version v1.0.16 --pkgrel 1 \
  --sha256-x86_64 <hash> --sha256-aarch64 <hash> --sha256-license <hash> \
  --sha256-icon <hash> --sha256-desktop <hash> \
  --output /tmp/aur/PKGBUILD
cd /tmp/aur && makepkg --printsrcinfo > .SRCINFO && makepkg -f
```

`aur_pkgbuild_test.go` covers the renderer, including that `makepkg` accepts
its output.
