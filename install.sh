#!/bin/sh
set -e

REPO="allisonhere/tidemail"
BINARY="tidemail"
if [ -z "${INSTALL_DIR:-}" ]; then
  if [ -z "${HOME:-}" ]; then
    INSTALL_DIR=""
  else
    INSTALL_DIR="${HOME}/.local/bin"
  fi
fi

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

info()    { printf "  ${CYAN}→${NC} %s\n" "$1"; }
success() { printf "  ${GREEN}✓${NC} %s\n" "$1"; }
warn()    { printf "  ${DIM}!${NC} %s\n" "$1"; }
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
INSTALL_TMP=""
cleanup() {
  rm -rf "$TMP"
  if [ -n "$INSTALL_TMP" ]; then
    rm -f "$INSTALL_TMP"
  fi
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

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
  error "Archive does not contain expected binary '${ASSET}'.\n         Found instead: ${contents}"
fi

chmod +x "${TMP}/${ASSET}"

# Verify the staged binary before touching any existing installation.
if ! "${TMP}/${ASSET}" --version >/dev/null 2>&1; then
  error "Downloaded binary failed its version check; existing installation left untouched."
fi

# ── Install ───────────────────────────────────────────────────────────────
if [ -z "$INSTALL_DIR" ]; then
  error "No install directory selected. Set INSTALL_DIR to a writable directory."
fi

if [ ! -d "$INSTALL_DIR" ]; then
  info "Creating ${INSTALL_DIR}"
  if ! mkdir -p "$INSTALL_DIR"; then
    error "Could not create ${INSTALL_DIR}. Set INSTALL_DIR to a writable directory."
  fi
fi

if [ ! -w "$INSTALL_DIR" ]; then
  error "${INSTALL_DIR} is not writable. Existing installation left untouched.\n         Try: INSTALL_DIR=\"\$HOME/.local/bin\" sh install.sh"
fi

INSTALL_TMP="${INSTALL_DIR}/.${BINARY}.tmp.$$"
info "Installing to ${INSTALL_DIR}/${BINARY}"
if ! install -m 0755 "${TMP}/${ASSET}" "$INSTALL_TMP"; then
  error "Could not stage binary in ${INSTALL_DIR}. Existing installation left untouched."
fi

if ! VERSION=$("$INSTALL_TMP" --version 2>/dev/null); then
  error "Staged binary failed its version check; existing installation left untouched."
fi

mv -f "$INSTALL_TMP" "${INSTALL_DIR}/${BINARY}"
INSTALL_TMP=""

# ── Verify ────────────────────────────────────────────────────────────────
success "Installed ${VERSION} to ${INSTALL_DIR}/${BINARY}"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) warn "${INSTALL_DIR} is not on PATH; add it before running ${BINARY} by name." ;;
esac
printf "\n  Run ${BOLD}tidemail${NC} to get started.\n\n"
