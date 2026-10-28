#!/usr/bin/env bash
# Records docs/demo/demo.gif for the README using vhs (https://github.com/charmbracelet/vhs).
# Needs: vhs, ttyd, ffmpeg, Go. Run from the repository root:  docs/demo/record.sh
set -euo pipefail

SANDBOX=/tmp/tidemail-demo
rm -rf "$SANDBOX"
mkdir -p "$SANDBOX/bin"

for tool in vhs ttyd ffmpeg go; do
  command -v "$tool" >/dev/null || { echo "missing: $tool (see docs/demo/README.md)" >&2; exit 1; }
done

go build -o "$SANDBOX/bin/tidemail" .
go run ./cmd/demoseed "$SANDBOX"
vhs docs/demo/demo.tape
echo "wrote docs/demo/demo.gif ($(du -h docs/demo/demo.gif | cut -f1))"
