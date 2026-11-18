#!/usr/bin/env bash
set -euo pipefail

# Refresh vendorHash in flake.nix after go.mod or go.sum changes.
#
# Nix downloads Go modules in a sandbox and checks them against vendorHash, so
# any dependency change makes the flake build fail until the hash is updated.
# This builds the flake, takes the correct hash from the mismatch error, writes
# it into flake.nix, and rebuilds to confirm. It changes nothing when the hash
# is already current. CI runs it on main and opens a PR with the result, so
# maintainers normally never need to run it by hand.
#
# Requires Nix with flakes enabled. Exits nonzero if the build fails for any
# reason other than a stale vendorHash.

cd "$(dirname "$0")/.."

log=$(mktemp)
trap 'rm -f "$log"' EXIT

build() {
  # Keep the output visible (CI logs) while saving it for the hash lookup.
  nix build .#tidemail --no-link -L 2>&1 | tee "$log"
  return "${PIPESTATUS[0]}"
}

if build; then
  echo "vendorHash is current."
  exit 0
fi

got=$(sed -n 's/^[[:space:]]*got:[[:space:]]*\(sha256-[A-Za-z0-9+/=]*\).*/\1/p' "$log" | tail -n 1)
if [ -z "$got" ]; then
  echo "The Nix build failed for a reason other than vendorHash; see the output above." >&2
  exit 1
fi

# -i.bak works with both GNU and BSD (macOS) sed.
sed -i.bak -E "s|vendorHash = \"sha256-[A-Za-z0-9+/=]+\";|vendorHash = \"$got\";|" flake.nix
rm -f flake.nix.bak

if ! grep -qF "vendorHash = \"$got\";" flake.nix; then
  echo "Could not find the vendorHash line in flake.nix." >&2
  exit 1
fi

if ! build; then
  echo "The Nix build still fails after updating vendorHash; see the output above." >&2
  exit 1
fi
echo "Updated vendorHash to $got."
