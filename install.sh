#!/bin/sh
set -e

REPO="allisonhere/tidemail"
BINARY="tidemail"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

info()    { printf "  ${CYAN}→${NC} %s\n" "$1"; }
success() { printf "  ${GREEN}✓${NC} %s\n" "$1"; }
error()   { printf "  ${RED}✗${NC} %s\n" "$1" >&2; exit 1; }

# Detect OS
case "$(uname -s)" in
  Linux*)  OS="linux" ;;
  Darwin*) OS="darwin" ;;
  *)       error "Unsupported OS: $(uname -s)" ;;
esac

# Detect architecture
case "$(uname -m)" in
  x86_64|amd64)   ARCH="x86_64" ;;
  aarch64|arm64)  ARCH="aarch64" ;;
  *)              error "Unsupported architecture: $(uname -m)" ;;
esac

ASSET="${BINARY}-${OS}-${ARCH}"

printf "\n  ${BOLD}TideMail Installer${NC}\n"
printf "  ${DIM}──────────────────────────────${NC}\n"
info "Platform: ${OS}/${ARCH}"

# ── Download ──────────────────────────────────────────────────────────────
# Use GitHub's /latest/download/ redirect URL — no API call needed, so no
# rate limiting and no fragile grep/sed parsing of the releases JSON.
URL="https://github.com/${REPO}/releases/latest/download/${ASSET}.tar.gz"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

info "Downloading latest release..."
if ! curl -fsSL -o "${TMP}/${ASSET}.tar.gz" "$URL"; then
  rm -rf "$TMP"
  error "Download failed — no release asset at ${URL}\n         Check that a release exists: https://github.com/${REPO}/releases"
fi

# Verify we got a real gzip archive (not an HTML error page from a redirect).
if ! gzip -t "${TMP}/${ASSET}.tar.gz" 2>/dev/null; then
  rm -rf "$TMP"
  error "Downloaded file is not a valid archive. The latest release may be missing ${OS}/${ARCH} assets.\n         See: https://github.com/${REPO}/releases"
fi

# ── Extract ───────────────────────────────────────────────────────────────
info "Extracting..."
if ! tar -xzf "${TMP}/${ASSET}.tar.gz" -C "$TMP"; then
  rm -rf "$TMP"
  error "Failed to extract ${ASSET}.tar.gz — the archive may be corrupt."
fi

if [ ! -f "${TMP}/${ASSET}" ]; then
  contents=$(ls -A "$TMP" 2>/dev/null | tr '\n' ' ')
  rm -rf "$TMP"
  error "Archive does not contain expected binary '${ASSET}'.\n         Found instead: ${contents}"
fi

chmod +x "${TMP}/${ASSET}"

# ── Install ───────────────────────────────────────────────────────────────
if [ -w "$INSTALL_DIR" ]; then
  mv "${TMP}/${ASSET}" "${INSTALL_DIR}/${BINARY}"
else
  info "Need sudo to install to ${INSTALL_DIR}"
  sudo mv "${TMP}/${ASSET}" "${INSTALL_DIR}/${BINARY}"
fi

# ── Verify ────────────────────────────────────────────────────────────────
VERSION=$("${INSTALL_DIR}/${BINARY}" --version 2>/dev/null || echo "unknown")
success "Installed ${VERSION} to ${INSTALL_DIR}/${BINARY}"
printf "\n  Run ${BOLD}tidemail${NC} to get started.\n\n"
