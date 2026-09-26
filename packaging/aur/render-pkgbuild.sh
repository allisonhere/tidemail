#!/usr/bin/env bash
# Render packaging/aur/PKGBUILD.in into a concrete PKGBUILD for one release.
#
# Every checksum must come from the SHA256SUMS asset that the release workflow
# published alongside the tarballs — the binaries in a local `go build` are not
# the ones users download, because release builds embed OAuth credentials.
set -euo pipefail

TEMPLATE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="$TEMPLATE_DIR/PKGBUILD.in"

usage() {
  cat >&2 <<'USAGE'
Usage: render-pkgbuild.sh --version VERSION --pkgrel N \
         --sha256-x86_64 HASH --sha256-aarch64 HASH --sha256-license HASH \
         --sha256-icon HASH --sha256-desktop HASH \
         [--template PATH] --output PATH

VERSION accepts either v1.2.3 or 1.2.3; the rendered pkgver never has the "v".
USAGE
  exit 2
}

version="" pkgrel="" sha_x86="" sha_arm="" sha_license="" sha_icon="" sha_desktop="" output=""
while [ $# -gt 0 ]; do
  case "$1" in
    --version)         version="${2:?--version needs a value}"; shift 2 ;;
    --pkgrel)          pkgrel="${2:?--pkgrel needs a value}"; shift 2 ;;
    --sha256-x86_64)   sha_x86="${2:?--sha256-x86_64 needs a value}"; shift 2 ;;
    --sha256-aarch64)  sha_arm="${2:?--sha256-aarch64 needs a value}"; shift 2 ;;
    --sha256-license)  sha_license="${2:?--sha256-license needs a value}"; shift 2 ;;
    --sha256-icon)     sha_icon="${2:?--sha256-icon needs a value}"; shift 2 ;;
    --sha256-desktop)  sha_desktop="${2:?--sha256-desktop needs a value}"; shift 2 ;;
    --template)        TEMPLATE="${2:?--template needs a value}"; shift 2 ;;
    --output)          output="${2:?--output needs a value}"; shift 2 ;;
    -h|--help)         usage ;;
    *) echo "render-pkgbuild.sh: unknown argument '$1'" >&2; usage ;;
  esac
done

for required in version pkgrel sha_x86 sha_arm sha_license sha_icon sha_desktop output; do
  if [ -z "${!required}" ]; then
    echo "render-pkgbuild.sh: missing required value for $required" >&2
    usage
  fi
done

# pacman's pkgver must not contain a leading "v" or a hyphen.
pkgver="${version#v}"
if [[ ! "$pkgver" =~ ^[0-9]+(\.[0-9]+)*$ ]]; then
  echo "render-pkgbuild.sh: '$version' is not a release version (expected v1.2.3)" >&2
  exit 1
fi
if [[ ! "$pkgrel" =~ ^[1-9][0-9]*$ ]]; then
  echo "render-pkgbuild.sh: pkgrel '$pkgrel' must be a positive integer" >&2
  exit 1
fi
for name in sha_x86 sha_arm sha_license sha_icon sha_desktop; do
  if [[ ! "${!name}" =~ ^[0-9a-f]{64}$ ]]; then
    echo "render-pkgbuild.sh: $name is not a sha256 digest: '${!name}'" >&2
    exit 1
  fi
done

if [ ! -f "$TEMPLATE" ]; then
  echo "render-pkgbuild.sh: template not found at $TEMPLATE" >&2
  exit 1
fi

rendered=$(
  sed \
    -e "s|@PKGVER@|$pkgver|g" \
    -e "s|@PKGREL@|$pkgrel|g" \
    -e "s|@SHA256_X86_64@|$sha_x86|g" \
    -e "s|@SHA256_AARCH64@|$sha_arm|g" \
    -e "s|@SHA256_LICENSE@|$sha_license|g" \
    -e "s|@SHA256_ICON@|$sha_icon|g" \
    -e "s|@SHA256_DESKTOP@|$sha_desktop|g" \
    "$TEMPLATE"
)

# A leftover placeholder would publish a PKGBUILD that cannot build.
if printf '%s' "$rendered" | grep -q '@[A-Z0-9_]\+@'; then
  echo "render-pkgbuild.sh: unresolved placeholders remain:" >&2
  printf '%s' "$rendered" | grep -o '@[A-Z0-9_]\+@' | sort -u >&2
  exit 1
fi

mkdir -p "$(dirname "$output")"
printf '%s\n' "$rendered" > "$output"
