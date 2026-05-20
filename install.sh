#!/bin/sh
set -e

REPO="allisonhere/tide"
BINARY="tide"
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

printf "\n  ${BOLD}Tide Installer${NC}\n"
printf "  ${DIM}──────────────────────────${NC}\n"
info "Platform: ${OS}/${ARCH}"

# Get latest release version
info "Fetching latest release..."
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' \
  | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

[ -z "$LATEST" ] && error "Could not determine latest version"
info "Latest version: ${LATEST}"

# Download
URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET}.tar.gz"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

info "Downloading ${ASSET}.tar.gz..."
curl -fsSL "$URL" -o "${TMP}/${ASSET}.tar.gz" \
  || error "Download failed: ${URL}"

tar -xzf "${TMP}/${ASSET}.tar.gz" -C "$TMP"

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv "${TMP}/${ASSET}" "${INSTALL_DIR}/${BINARY}"
  chmod +x "${INSTALL_DIR}/${BINARY}"
else
  info "Need sudo to install to ${INSTALL_DIR}"
  sudo mv "${TMP}/${ASSET}" "${INSTALL_DIR}/${BINARY}"
  sudo chmod +x "${INSTALL_DIR}/${BINARY}"
fi

success "Installed ${BINARY} ${LATEST} to ${INSTALL_DIR}/${BINARY}"
printf "\n  Run ${BOLD}tide${NC} to get started.\n\n"
