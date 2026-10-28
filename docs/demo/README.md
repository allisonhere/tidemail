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

When the GIF looks right, commit it and add this under the tagline in the README:

```markdown
![TideMail demo](docs/demo/demo.gif)
```
