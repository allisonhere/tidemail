#!/bin/bash
set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
DIST_DIR="$PROJECT_DIR/dist"
BINARY_NAME="tidemail"
REPO="allisonhere/tidemail"
GITHUB_FILE_LIMIT_BYTES=$((100 * 1024 * 1024))

STEP_START=0
TOTAL_START=0
RELEASE_NOTES_FILE=""

# ============================================================================
# UTILITIES
# ============================================================================

print_header() {
  clear
  echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${BLUE}║${NC}            ${BOLD}${CYAN}TideMail Release Builder${NC}                         ${BLUE}║${NC}"
  echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
  echo ""
}

print_step() {
  local step=$1
  local total=$2
  local msg=$3
  STEP_START=$(date +%s)
  echo ""
  echo -e "${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BOLD}${CYAN}[$step/$total]${NC} ${BOLD}$msg${NC}"
  echo -e "${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

print_substep() { echo -e "  ${DIM}→${NC} $1"; }

print_success() {
  local elapsed=$(($(date +%s) - STEP_START))
  echo -e "  ${GREEN}✓${NC} $1 ${DIM}(${elapsed}s)${NC}"
}

print_error()   { echo -e "  ${RED}✗${NC} $1"; }
print_warning() { echo -e "  ${YELLOW}⚠${NC} $1"; }
print_info()    { echo -e "  ${BLUE}ℹ${NC} $1"; }

gh_is_authenticated() {
  gh auth status -h github.com >/dev/null 2>&1
}

print_file_size() {
  local file=$1
  if [ -f "$file" ]; then
    local size=$(du -h "$file" | cut -f1)
    local name=$(basename "$file")
    echo -e "  ${GREEN}✓${NC} ${name} ${DIM}(${size})${NC}"
  fi
}

format_time() {
  local seconds=$1
  if [ "$seconds" -ge 60 ]; then
    echo "$((seconds / 60))m $((seconds % 60))s"
  else
    echo "${seconds}s"
  fi
}

spinner() {
  local pid=$1
  local msg=$2
  local spin='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
  local i=0
  tput civis
  while kill -0 "$pid" 2>/dev/null; do
    i=$(((i + 1) % 10))
    printf "\r  ${CYAN}${spin:$i:1}${NC} %s" "$msg"
    sleep 0.1
  done
  tput cnorm
  printf "\r"
}

# ============================================================================
# VERSION MANAGEMENT
# ============================================================================

read_version() {
  VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
}

suggest_next_patch() {
  NEXT_VERSION=""
  local clean=${VERSION#v}
  if [[ $clean =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    NEXT_VERSION="v${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((BASH_REMATCH[3] + 1))"
  fi
}

bump_version() {
  read_version
  suggest_next_patch

  echo -e "\n  Current version: ${GREEN}$VERSION${NC}"
  if [ -n "$NEXT_VERSION" ]; then
    echo -e "  Suggested next:  ${CYAN}$NEXT_VERSION${NC}\n"
    read -p "  Use $NEXT_VERSION? [Y/n] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Nn]$ ]]; then
      VERSION="$NEXT_VERSION"
    else
      read -p "  Enter version (e.g. v1.2.3): " VERSION
    fi
  else
    read -p "  Enter version (e.g. v1.2.3): " VERSION
  fi
  print_success "Version set to $VERSION"
}

# ============================================================================
# RELEASE NOTES
# ============================================================================

collect_release_notes() {
  local tmp
  tmp=$(mktemp /tmp/tidemail-release-notes-XXXXXX.md)

  cat > "$tmp" <<TEMPLATE
## What's new in $VERSION

<!-- Write your release notes above this line. Lines starting with # are comments and will be removed. -->
TEMPLATE

  local editor="${EDITOR:-${VISUAL:-nano}}"
  "$editor" "$tmp"

  # Strip comment lines and trim trailing blank lines
  local cleaned
  cleaned=$(grep -v '^<!--' "$tmp" | grep -v '^-->' | sed '/^[[:space:]]*$/{ /./!d }')
  rm -f "$tmp"

  if [ -z "$(echo "$cleaned" | tr -d '[:space:]')" ]; then
    print_warning "No release notes written — using default install instructions"
    RELEASE_NOTES_FILE=""
    return 0
  fi

  RELEASE_NOTES_FILE=$(mktemp /tmp/tidemail-release-notes-final-XXXXXX.md)
  printf '%s\n' "$cleaned" > "$RELEASE_NOTES_FILE"

  print_success "Release notes saved"
  echo ""
  echo -e "${DIM}$(head -5 "$RELEASE_NOTES_FILE")${NC}"
  [ "$(wc -l < "$RELEASE_NOTES_FILE")" -gt 5 ] && echo -e "${DIM}  ...${NC}"
  echo ""
}

# ============================================================================
# BUILD
# ============================================================================

clean_dist() {
  print_substep "Removing old build artifacts..."
  rm -rf "$DIST_DIR"
  mkdir -p "$DIST_DIR"
  print_success "Cleaned dist folder"
}

build_all() {
  # GOOS | GOARCH | output name
  local targets=(
    "linux   amd64 tidemail-linux-x86_64"
    "linux   arm64 tidemail-linux-aarch64"
    "darwin  amd64 tidemail-darwin-x86_64"
    "darwin  arm64 tidemail-darwin-aarch64"
  )

  for entry in "${targets[@]}"; do
    read -r goos goarch name <<<"$entry"

    local bin_path="$DIST_DIR/$name"
    local archive="$DIST_DIR/$name.tar.gz"

    (
      cd "$PROJECT_DIR"
      CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build \
        -ldflags="-s -w -X main.version=$VERSION" \
        -o "$bin_path" \
        . 2>&1
    ) &

    local pid=$!
    spinner $pid "Building $name ($goos/$goarch)..."
    wait $pid || { print_error "Build failed for $goos/$goarch"; continue; }

    tar -czf "$archive" -C "$DIST_DIR" "$name"
    rm -f "$bin_path"
    print_file_size "$archive"
  done

  print_success "Build complete"
}

# ============================================================================
# GIT & RELEASE
# ============================================================================

commit_changes() {
  if [ -z "$(git status --porcelain)" ]; then
    print_info "No changes to commit"
  else
    print_substep "Staging and committing changes..."
    git add -A
    local skipped=0
    while IFS= read -r path; do
      if [ -f "$path" ]; then
        local size
        size=$(wc -c <"$path" | tr -d ' ')
        if [ "$size" -gt "$GITHUB_FILE_LIMIT_BYTES" ]; then
          git restore --staged -- "$path" 2>/dev/null || git reset -q HEAD -- "$path"
          print_warning "Skipped $path ($(du -h "$path" | cut -f1)); GitHub rejects files over 100 MB"
          skipped=1
        fi
      fi
    done < <(git diff --cached --name-only)
    if [ "$skipped" -eq 1 ]; then
      print_info "Skipped files remain in your working tree; commit them with Git LFS or shrink/remove them before release"
    fi
    if git diff --cached --quiet; then
      print_info "No committable changes after skipping oversized files"
      return 0
    fi
    git commit -m "chore: release $VERSION"
    print_success "Committed: release $VERSION"
  fi
}

push_to_origin() {
  print_substep "Pushing to origin..."
  if git push origin main; then
    print_success "Pushed to origin"
  else
    print_error "Push failed"
    return 1
  fi
}

create_release() {
  if ! command -v gh &>/dev/null; then
    print_error "GitHub CLI (gh) not installed"
    return 1
  fi

  if ! gh_is_authenticated; then
    print_error "GitHub CLI is not authenticated for GitHub API access"
    print_info "Run 'gh auth login' or export GH_TOKEN before creating releases"
    return 1
  fi

  if git rev-parse "$VERSION" &>/dev/null; then
    print_warning "Tag $VERSION already exists"
    read -p "  Delete and recreate? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
      git tag -d "$VERSION"
      git push origin --delete "$VERSION" 2>/dev/null || true
      print_success "Old tag deleted"
    else
      return 1
    fi
  fi

  print_substep "Creating and pushing tag $VERSION..."
  git tag -a "$VERSION" -m "Release $VERSION"
  git push origin "$VERSION"
  print_success "Tag $VERSION pushed — GitHub Action will build release binaries"

  if ! command -v gh &>/dev/null; then
    print_warning "GitHub CLI (gh) not installed — skipping release asset upload"
    print_info "The GitHub Action triggered by the tag push will build and publish the release"
    return 0
  fi

  if ! gh_is_authenticated; then
    print_warning "GitHub CLI is not authenticated — skipping release asset upload"
    print_info "The GitHub Action triggered by the tag push will build and publish the release"
    return 0
  fi

  print_substep "Creating GitHub release..."
  local default_notes="### Install

Download a binary below for your platform and add it to your PATH.

Or build from source:
\`\`\`bash
git clone https://github.com/${REPO}
cd tidemail
go build -o tidemail .
\`\`\`"

  local notes_args=()
  if [ -n "$RELEASE_NOTES_FILE" ] && [ -f "$RELEASE_NOTES_FILE" ]; then
    printf '\n\n%s\n' "$default_notes" >> "$RELEASE_NOTES_FILE"
    notes_args=(--notes-file "$RELEASE_NOTES_FILE")
  else
    notes_args=(--notes "## TideMail $VERSION

$default_notes")
  fi

  if ! gh release create "$VERSION" \
    --title "TideMail $VERSION" \
    "${notes_args[@]}" \
    --repo "$REPO"; then
    print_error "Release creation failed"
    [ -n "$RELEASE_NOTES_FILE" ] && rm -f "$RELEASE_NOTES_FILE"
    return 1
  fi
  [ -n "$RELEASE_NOTES_FILE" ] && rm -f "$RELEASE_NOTES_FILE" && RELEASE_NOTES_FILE=""
  print_success "Release created"

  print_substep "Uploading archives..."
  for f in "$DIST_DIR"/*.tar.gz; do
    if [ -f "$f" ]; then
      if ! gh release upload "$VERSION" "$f" --repo "$REPO"; then
        print_error "Failed to upload $(basename "$f")"
        return 1
      fi
      print_file_size "$f"
    fi
  done
  print_success "Assets uploaded"
  echo -e "  ${GREEN}→${NC} https://github.com/${REPO}/releases/tag/$VERSION"
}

full_release() {
  TOTAL_START=$(date +%s)
  local total_steps=6

  print_step 1 $total_steps "Version bump"
  bump_version

  print_step 2 $total_steps "Cleaning dist"
  clean_dist

  print_step 3 $total_steps "Building binaries"
  build_all

  print_step 4 $total_steps "Commit changes"
  commit_changes

  print_step 5 $total_steps "Push to origin"
  push_to_origin

  print_step 6 $total_steps "GitHub Release"
  create_release

  local total_time=$(($(date +%s) - TOTAL_START))
  echo -e "\n${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BOLD}${GREEN}  ✓ Release $VERSION complete!${NC} ${DIM}($(format_time $total_time))${NC}"
  echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"
}

# ============================================================================
# MAIN MENU
# ============================================================================

show_status() {
  read_version
  suggest_next_patch
  echo -e "  ${BOLD}Version:${NC}  ${GREEN}$VERSION${NC}"
  [ -n "$NEXT_VERSION" ] && echo -e "  ${BOLD}Next:${NC}     ${DIM}$NEXT_VERSION${NC}"

  local changes
  changes=$(git status --porcelain | wc -l)
  if [ "$changes" -gt 0 ]; then
    echo -e "  ${BOLD}Git:${NC}      ${YELLOW}$changes uncommitted change(s)${NC}"
  else
    echo -e "  ${BOLD}Git:${NC}      ${GREEN}clean${NC}"
  fi

  if command -v go &>/dev/null; then
    echo -e "  ${BOLD}Go:${NC}       ${GREEN}$(go version | awk '{print $3}')${NC}"
  else
    echo -e "  ${BOLD}Go:${NC}       ${RED}not found${NC}"
  fi

  if command -v gh &>/dev/null; then
    if gh_is_authenticated; then
      echo -e "  ${BOLD}gh:${NC}       ${GREEN}authenticated${NC}"
    else
      echo -e "  ${BOLD}gh:${NC}       ${YELLOW}installed, not authenticated${NC}"
    fi
  else
    echo -e "  ${BOLD}gh:${NC}       ${YELLOW}not found (releases won't work)${NC}"
  fi
  echo ""
}

main_menu() {
  while true; do
    print_header
    show_status

    echo -e "  ${BOLD}${CYAN}Actions${NC}"
    echo -e "  ${DIM}─────────────────────────────${NC}"
    echo "   1) Bump version"
    echo "   2) Write release notes (optional — run before full release)"
    echo "   3) Commit changes"
    echo "   4) Push to main"
    echo "   5) Clean dist"
    echo "   6) Build all binaries"
    echo ""
    echo -e "  ${BOLD}${CYAN}Release${NC}"
    echo -e "  ${DIM}─────────────────────────────${NC}"
    echo "   7) Create GitHub release only"
    echo -e "   8) ${GREEN}Full release (recommended)${NC}"
    echo ""
    echo "   0) Exit"
    echo ""

    read -p "  Choose [0-8]: " choice

    case $choice in
    1) bump_version || true ;;
    2) collect_release_notes || true ;;
    3) commit_changes || true ;;
    4) push_to_origin || true ;;
    5) clean_dist || true ;;
    6) build_all || true ;;
    7) create_release || true ;;
    8) full_release || true ;;
    0)
      echo -e "\n  ${DIM}Bye!${NC}\n"
      exit 0
      ;;
    *) print_error "Invalid choice" ;;
    esac

    echo ""
    read -p "  Press Enter to continue..." -r
  done
}

main_menu
