#!/bin/sh
# Run this branch with the Omarchy theme, against a throwaway copy of your
# config (your real config is not touched).
set -e
cd "$(dirname "$0")"
go build -o /tmp/tidemail-omarchy .
d=$(mktemp -d)
mkdir -p "$d/tidemail"
sed 's/^theme = .*/theme = "omarchy"/' "$HOME/.config/tidemail/config.toml" > "$d/tidemail/config.toml"
XDG_CONFIG_HOME=$d /tmp/tidemail-omarchy
rm -rf "$d"
