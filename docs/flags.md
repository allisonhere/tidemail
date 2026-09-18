# Flags

Flags go after the package argument:

```sh
go run . --disable-google-oauth
tidemail --open 4821
```

| Flag | Purpose |
| --- | --- |
| `--open <message-id>` | Starts on that message: the folder holding it becomes the selection, the cursor lands on it, and the reading pane shows it. The id is TideMail's own cache id for the message (`messages.id` in `mail.db`). An id this cache does not know still starts, and says so; a missing or malformed id is a startup error. Ignored with `--prototype-forms`, which never opens the database. |
| `--disable-google-oauth` | Hides Gmail OAuth controls and shows the App Password-only experience. The flag changes runtime state only; it does not rewrite `config.toml` or disable Outlook OAuth. |
| `--preview-manual-update` | Opens the update window a package-managed install gets — the one that explains why TideMail cannot replace its own binary and offers the command to run. |
| `--preview-update-progress` | Previews the in-app update progress state. |
| `--prototype-forms` | Runs the standalone form prototype instead of the mail client. |

## Opening a message from somewhere else

The TideDeck mail panel runs `tidemail --open <id>` for the message under its
cursor, so picking a message there lands here on that message:

```sh
tidemail --open 4821
```

The id is the row's `messages.id` in `~/.local/share/tidemail/mail.db`. Two
caveats worth knowing: if TideMail is **already running**, this is a second
TideMail over the same cache and nothing is handed over (there is no
single-instance channel yet); and an id is only meaningful against the cache it
came from, so a dashboard pointed at a different `mail.db` will name a
different message.

With `--disable-google-oauth`, a Gmail account form omits the Auth selector and
Google sign-in button. It shows this guidance under Password:

```text
Google OAuth is unavailable.
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
