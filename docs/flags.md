# Development flags

Run development flags after the package argument:

```sh
go run . --disable-google-oauth
```

| Flag | Purpose |
| --- | --- |
| `--disable-google-oauth` | Developer/test flag that additionally clears Gmail OAuth client credentials for this run. Gmail OAuth controls are already hidden while Google approval is pending. It does not rewrite `config.toml` or disable Outlook OAuth. |
| `--preview-manual-update` | Opens the update window a package-managed install gets — the one that explains why TideMail cannot replace its own binary and offers the command to run. |
| `--preview-update-progress` | Previews the in-app update progress state. |
| `--prototype-forms` | Runs the standalone form prototype instead of the mail client. |

While Google approval is pending, a Gmail account form omits the Auth selector
and Google sign-in button. It shows this guidance under Password:

```text
Google OAuth is waiting for Google's approval.
Use a Google App Password instead.
```

The flag also protects an existing Gmail OAuth account from conversion to
password authentication if you save its form during a preview.

## Seeing both update windows

There are two, and they need different setups.

`--preview-manual-update` stages an update a packaged install cannot apply and
opens that window straight away. It fakes a pacman-owned `tidemail-bin`, so the
wording an AUR user sees renders on any machine rather than the generic
"cannot write to its install location" fallback:

```text
Update available: v0.0.39

TideMail was installed by pacman as tidemail-bin, so it
cannot replace its own binary.

Run this outside TideMail:

  yay -S tidemail-bin

c copy   esc close
```

Press `esc` and open Settings > Updates to see the same command in its field
there; the preview leaves the real state behind, so both surfaces work.

The other window — the install confirmation a self-installed copy gets — needs a
real release newer than the running build, because it offers to download and
install one. Build with an old version and point it at a scratch profile:

```sh
go build -ldflags "-X main.version=v1.0.1" -o /tmp/tidemail-old .
XDG_CONFIG_HOME=/tmp/tm-cfg XDG_DATA_HOME=/tmp/tm-data /tmp/tidemail-old
```

Then press `U`. The binary sits in a writable directory that no package owns, so
it takes the self-install path. Check update-on-startup is enabled, or use
"Check now" in Settings > Updates.
