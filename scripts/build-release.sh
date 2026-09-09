#!/usr/bin/env bash
set -euo pipefail

# Desktop client credentials are supplied by maintainers, never by end users.
# They identify a public installed app and are extractable from the binary.
: "${TIDEMAIL_GOOGLE_CLIENT_ID:?Google Desktop client ID is required for a release build}"
: "${TIDEMAIL_GOOGLE_CLIENT_SECRET:?Google Desktop client secret is required for a release build}"
version="${1:?Usage: bash scripts/build-release.sh VERSION OUTPUT}"
output="${2:?Usage: bash scripts/build-release.sh VERSION OUTPUT}"

# Keep values safe for Go's linker-flag parser without echoing credentials.
for value in "$TIDEMAIL_GOOGLE_CLIENT_ID" "$TIDEMAIL_GOOGLE_CLIENT_SECRET" "$version"; do
  if [[ ! "$value" =~ ^[a-zA-Z0-9._+-]+$ ]]; then
    echo "Invalid release credential or version format" >&2
    exit 1
  fi
done

package=github.com/allisonhere/tidemail/internal/config
CGO_ENABLED=0 go build \
  -ldflags="-s -w -X main.version=$version -X $package.DefaultGoogleClientID=$TIDEMAIL_GOOGLE_CLIENT_ID -X $package.DefaultGoogleClientSecret=$TIDEMAIL_GOOGLE_CLIENT_SECRET" \
  -o "$output" .
