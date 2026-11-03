# Recording the README demo

`docs/demo/demo.gif` is made from made-up mail, never from a real inbox.

```bash
# one-time setup (Arch shown; use your package manager)
yay -S vhs ttyd ffmpeg     # or: go install github.com/charmbracelet/vhs@latest, plus ttyd and ffmpeg

docs/demo/record.sh
```

The script builds TideMail, runs `cmd/demoseed` to fill a throwaway sandbox in
`/tmp/tidemail-demo` (two accounts, ten messages, no server), and records
`demo.tape` against it. Edit `demo.tape` to change what the GIF shows. Keep the
result under about 5 MB; if it is larger, lower `Width`/`Height` or `FontSize`
in the tape, or trim sleeps.

The README already embeds the GIF, so after re-recording just commit the new file.

## Trying a one-account setup

`demoseed -single` seeds only the first account, which is the quickest way to
check behavior that differs for someone with exactly one (for example, compose
opens on To rather than From). It never touches your real profile. The seeder
needs an empty sandbox, so clear it first when re-running:

```bash
rm -rf /tmp/tm-single && mkdir -p /tmp/tm-single/bin
go build -o /tmp/tm-single/bin/tidemail .
go run ./cmd/demoseed -single /tmp/tm-single
XDG_CONFIG_HOME=/tmp/tm-single/config XDG_DATA_HOME=/tmp/tm-single/data /tmp/tm-single/bin/tidemail
```
