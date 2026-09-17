#!/usr/bin/env bash
#
# TideMail release console.
#
# A full-screen TUI for cutting a release: version bump, verification, GitHub
# release, and publishing tidemail-bin to the AUR.
#
# Deliberately not `set -e`: an interactive loop must survive a failed step and
# redraw, not exit mid-frame. Every step reports its own status instead.
set -uo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST_DIR="$PROJECT_DIR/dist"
BINARY_NAME="tidemail"
REPO="allisonhere/tidemail"
AUR_PKGNAME="tidemail-bin"
AUR_HOST="aur.archlinux.org"
AUR_REMOTE="ssh://aur@${AUR_HOST}/${AUR_PKGNAME}.git"
GITHUB_FILE_LIMIT_BYTES=$((100 * 1024 * 1024))

# How long to wait for the release workflow to publish its assets.
ASSET_WAIT_SECONDS=900

VERSION=""
NEXT_VERSION=""
# ── Colors ───────────────────────────────────────────────────────────────────
if [ -t 1 ]; then
  RED=$'\033[0;31m';     GREEN=$'\033[0;32m';   YELLOW=$'\033[0;33m'
  BLUE=$'\033[0;34m';    CYAN=$'\033[0;36m';    MAGENTA=$'\033[0;35m'
  BOLD=$'\033[1m';       DIM=$'\033[2m';        NC=$'\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; BLUE=''; CYAN=''; MAGENTA=''; BOLD=''; DIM=''; NC=''
fi

# ── Terminal state ───────────────────────────────────────────────────────────

ROWS=24
COLS=80
TUI_ACTIVE=0
SAVED_STTY=""

measure_term() {
  ROWS=$(tput lines 2>/dev/null || echo 24)
  COLS=$(tput cols 2>/dev/null || echo 80)
  # Never round these up: drawing more rows or columns than the terminal has
  # wraps and scrolls the frame instead of fitting inside it.
  [ "$ROWS" -ge 1 ] || ROWS=24
  [ "$COLS" -ge 20 ] || COLS=20
}

tui_enter() {
  [ "$TUI_ACTIVE" -eq 1 ] && return 0
  SAVED_STTY=$(stty -g 2>/dev/null || echo "")
  tput smcup 2>/dev/null
  tput civis 2>/dev/null
  stty -echo 2>/dev/null
  TUI_ACTIVE=1
  measure_term
}

tui_leave() {
  [ "$TUI_ACTIVE" -eq 0 ] && return 0
  TUI_ACTIVE=0
  tput cnorm 2>/dev/null
  tput rmcup 2>/dev/null
  [ -n "$SAVED_STTY" ] && stty "$SAVED_STTY" 2>/dev/null
}

# Drop to the normal screen so a child process (an editor, an ssh passphrase
# prompt) owns the terminal, then come back.
tui_suspend() { tui_leave; }
tui_resume()  { tui_enter; draw_frame; }

cleanup() {
  tui_leave
  return 0
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
RESIZE_PENDING=0
trap 'RESIZE_PENDING=1' WINCH

# ── Frame buffer ─────────────────────────────────────────────────────────────
# Lines are written with ESC[K (erase to end of line) so a shorter line never
# leaves the tail of the previous frame behind, and no padding math is needed
# except where a box border has to land on the right edge.

CLR_EOL=$'\033[K'

# pad_to fits a possibly-colored string to an exact printable width: padded
# with spaces when short, clipped when long, so a box border always lands on
# the same column. Escape sequences are copied through and never counted.
# pad_to WIDTH TEXT [nopad] — fits text to WIDTH printable columns. With
# "nopad" it only clips, leaving a short line short.
pad_to() {
  local width=${1-0} text=${2-} mode=${3-}
  local n i=0 shown=0 ch out=""
  # State as it was before the most recent printable character was appended.
  # Clipping rewinds to here, so the ellipsis replaces a whole character and
  # can never cut an escape sequence in half.
  local prev_out="" prev_shown=0
  n=${#text}
  while [ "$i" -lt "$n" ]; do
    ch=${text:i:1}
    if [ "$ch" = $'\033' ]; then
      while [ "$i" -lt "$n" ] && [[ ${text:i:1} != m ]]; do
        out+=${text:i:1}
        i=$((i + 1))
      done
      out+="m"
      i=$((i + 1))
      continue
    fi
    if [ "$shown" -ge "$width" ]; then
      # Ran out of room; the tail is dropped below.
      break
    fi
    prev_out="$out"
    prev_shown=$shown
    out+=$ch
    shown=$((shown + 1))
    i=$((i + 1))
  done
  if [ "$i" -lt "$n" ] && [ "$width" -gt 0 ]; then
    # Clipped: rewind one character and mark it, then reset colour so nothing
    # bleeds past the right edge.
    out="${prev_out}…${NC}"
    shown=$((prev_shown + 1))
  fi
  if [ "$shown" -lt "$width" ] && [ "$mode" != "nopad" ]; then
    printf '%s%*s' "$out" $((width - shown)) ""
  else
    printf '%s' "$out"
  fi
}

# repeat_char CHAR N
repeat_char() {
  local ch=$1 n=$2 out=""
  while [ "$n" -gt 0 ]; do out+="$ch"; n=$((n - 1)); done
  printf '%s' "$out"
}

FRAME=""
FRAME_ROWS=0
# frame_line is the one place the frame's bounds are enforced: never more rows
# than the terminal has, and never a row wider than it, since either one wraps
# the frame onto an extra line and scrolls the top away.
frame_line() {
  [ "$FRAME_ROWS" -ge "$ROWS" ] && return 0
  local text=$1
  # A tab moves the cursor without erasing, so whatever the previous frame left
  # in the skipped cells shows through; a carriage return would reset to column
  # zero and overwrite the row. Neither belongs in a painted frame.
  text=${text//$'\t'/    }
  text=${text//$'\r'/}
  # The cheap length test counts escape bytes too, so it can only over-trigger
  # the clip, never miss an overflowing line.
  if [ "${#text}" -gt "$COLS" ]; then
    text=$(pad_to "$COLS" "$text" nopad)
  fi
  FRAME+="$text${CLR_EOL}"$'\n'
  FRAME_ROWS=$((FRAME_ROWS + 1))
}
frame_blank() {
  [ "$FRAME_ROWS" -ge "$ROWS" ] && return 0
  FRAME+="${CLR_EOL}"$'\n'
  FRAME_ROWS=$((FRAME_ROWS + 1))
}

frame_flush() {
  tput cup 0 0 2>/dev/null
  # Strip the trailing newline: emitting one while on the last row scrolls the
  # terminal up, which pushes the top border of the frame off screen.
  printf '%s' "${FRAME%$'\n'}"
  # Erase any rows the new frame did not reach.
  printf '\033[J'
  FRAME=""
  FRAME_ROWS=0
}

# ── Status probes ────────────────────────────────────────────────────────────

ST_VERSION="" ST_NEXT="" ST_BRANCH="" ST_DIRTY="" ST_GO="" ST_GH="" ST_AUR=""
AUR_PROBE_FILE=""

read_version() {
  VERSION=$(git -C "$PROJECT_DIR" describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
}

suggest_next_patch() {
  NEXT_VERSION=""
  local clean=${VERSION#v}
  if [[ $clean =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    NEXT_VERSION="v${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((BASH_REMATCH[3] + 1))"
  fi
}

gh_is_authenticated() { gh auth status -h github.com >/dev/null 2>&1; }

# The AUR probe opens an SSH connection, so it runs once in the background and
# the result is read from a file on later redraws instead of stalling a frame.
aur_probe_start() {
  [ -n "$AUR_PROBE_FILE" ] && return 0
  AUR_PROBE_FILE=$(mktemp -u "${TMPDIR:-/tmp}/tidemail-aur-probe-XXXXXX")
  (
    if ! command -v ssh >/dev/null 2>&1; then
      echo "missing:ssh not installed" > "$AUR_PROBE_FILE"
      exit 0
    fi
    local out
    out=$(ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new \
             -o ConnectTimeout=6 -T "aur@${AUR_HOST}" 2>&1)
    # The AUR greets an authenticated key and then closes the shell.
    if printf '%s' "$out" | grep -qi "Interactive shell is disabled"; then
      local user
      user=$(printf '%s' "$out" | sed -n 's/.*Welcome to AUR, \([^!]*\)!.*/\1/p' | head -1)
      echo "ok:${user:-authenticated}" > "$AUR_PROBE_FILE"
    elif printf '%s' "$out" | grep -qi "permission denied"; then
      echo "denied:SSH key not registered on the AUR" > "$AUR_PROBE_FILE"
    else
      echo "unknown:$(printf '%s' "$out" | head -1)" > "$AUR_PROBE_FILE"
    fi
  ) >/dev/null 2>&1 &
  disown 2>/dev/null
}

aur_probe_read() {
  if [ -z "$AUR_PROBE_FILE" ] || [ ! -f "$AUR_PROBE_FILE" ]; then
    ST_AUR="probing"
    return 0
  fi
  ST_AUR=$(head -1 "$AUR_PROBE_FILE" 2>/dev/null || echo "probing")
}

refresh_status() {
  read_version
  suggest_next_patch
  ST_VERSION="$VERSION"
  ST_NEXT="$NEXT_VERSION"
  ST_BRANCH=$(git -C "$PROJECT_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "?")
  ST_DIRTY=$(git -C "$PROJECT_DIR" status --porcelain 2>/dev/null | wc -l | tr -d ' ')
  if command -v go >/dev/null 2>&1; then
    ST_GO=$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//')
  else
    ST_GO=""
  fi
  if command -v gh >/dev/null 2>&1; then
    gh_is_authenticated && ST_GH="auth" || ST_GH="noauth"
  else
    ST_GH="missing"
  fi
  aur_probe_read
}

# ── Log pane ─────────────────────────────────────────────────────────────────

LOG=()
LOG_MAX=500

log_raw() {
  LOG+=("$1")
  if [ "${#LOG[@]}" -gt "$LOG_MAX" ]; then
    LOG=("${LOG[@]: -$LOG_MAX}")
  fi
}

log_plain()   { log_raw "  $1"; }
log_step()    { log_raw ""; log_raw "  ${MAGENTA}▸${NC} ${BOLD}$1${NC}"; }
log_ok()      { log_raw "  ${GREEN}✓${NC} $1"; }
log_err()     { log_raw "  ${RED}✗${NC} $1"; }
log_warn()    { log_raw "  ${YELLOW}⚠${NC} $1"; }
log_info()    { log_raw "  ${BLUE}ℹ${NC} $1"; }
log_detail()  { log_raw "    ${DIM}$1${NC}"; }

format_time() {
  local seconds=$1
  if [ "$seconds" -ge 60 ]; then echo "$((seconds / 60))m $((seconds % 60))s"; else echo "${seconds}s"; fi
}

# ── Running commands with live output ────────────────────────────────────────

SPIN_FRAMES='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
SPIN_I=0
BUSY_MSG=""

spin_tick() {
  SPIN_I=$(((SPIN_I + 1) % 10))
  draw_frame
}

# run_streamed DESC CMD...
# Streams combined output into the log pane while animating the busy line.
# Returns the command's exit status.
run_streamed() {
  local desc=$1; shift
  local fifo rc_file line rc status
  fifo=$(mktemp -u "${TMPDIR:-/tmp}/tidemail-deploy-fifo-XXXXXX")
  rc_file="${fifo}.rc"
  mkfifo "$fifo" 2>/dev/null || { log_err "could not create pipe for: $desc"; return 1; }

  BUSY_MSG="$desc"
  ( "$@" >"$fifo" 2>&1; echo $? >"$rc_file" ) &
  local pid=$!

  exec 3<"$fifo"
  while :; do
    IFS= read -r -t 0.12 line <&3
    rc=$?
    if [ "$rc" -eq 0 ]; then
      log_raw "    ${DIM}${line}${NC}"
      spin_tick
    elif [ "$rc" -gt 128 ]; then
      spin_tick
    else
      break
    fi
  done
  exec 3<&-

  wait "$pid" 2>/dev/null
  status=$(cat "$rc_file" 2>/dev/null || echo 1)
  rm -f "$fifo" "$rc_file"
  BUSY_MSG=""
  return "${status:-1}"
}

# run_quiet DESC CMD... — same, but only surfaces output on failure.
run_quiet() {
  local desc=$1; shift
  local out status
  BUSY_MSG="$desc"
  draw_frame
  out=$("$@" 2>&1)
  status=$?
  BUSY_MSG=""
  if [ "$status" -ne 0 ]; then
    while IFS= read -r line; do log_raw "    ${DIM}${line}${NC}"; done <<<"$out"
  fi
  return "$status"
}

# ── Input ────────────────────────────────────────────────────────────────────

# read_key [TIMEOUT] leaves the keystroke in READ_KEY_VALUE and returns
# 0 on a key, 1 on EOF, 2 on timeout. A global avoids the command-substitution
# subshell, which would otherwise swallow the value on an empty (Enter) key.
READ_KEY_VALUE=""
read_key() {
  local k="" rest rc timeout=${1-}
  READ_KEY_VALUE=""
  if [ -n "$timeout" ]; then
    IFS= read -rsn1 -t "$timeout" k
    rc=$?
  else
    IFS= read -rsn1 k
    rc=$?
  fi
  [ "$rc" -gt 128 ] && return 2
  [ "$rc" -ne 0 ] && return 1
  if [ "$k" = $'\033' ]; then
    if IFS= read -rsn2 -t 0.05 rest 2>/dev/null; then
      k+="$rest"
    fi
  fi
  READ_KEY_VALUE="$k"
  return 0
}

# tui_confirm PROMPT [default-yes] — draws a prompt in the log pane.
tui_confirm() {
  local prompt=$1 default=${2:-n} key hint
  if [ "$default" = "y" ]; then hint="[Y/n]"; else hint="[y/N]"; fi
  log_raw "  ${YELLOW}?${NC} ${BOLD}${prompt}${NC} ${DIM}${hint}${NC}"
  draw_frame
  read_key
  key="$READ_KEY_VALUE"
  # Replace the question with the recorded answer.
  unset 'LOG[-1]'
  case "$key" in
    y|Y) log_raw "  ${YELLOW}?${NC} ${prompt} ${GREEN}yes${NC}"; draw_frame; return 0 ;;
    n|N) log_raw "  ${YELLOW}?${NC} ${prompt} ${DIM}no${NC}";   draw_frame; return 1 ;;
    "")
      if [ "$default" = "y" ]; then
        log_raw "  ${YELLOW}?${NC} ${prompt} ${GREEN}yes${NC}"; draw_frame; return 0
      fi
      log_raw "  ${YELLOW}?${NC} ${prompt} ${DIM}no${NC}"; draw_frame; return 1 ;;
    *)
      if [ "$default" = "y" ]; then
        log_raw "  ${YELLOW}?${NC} ${prompt} ${GREEN}yes${NC}"; draw_frame; return 0
      fi
      log_raw "  ${YELLOW}?${NC} ${prompt} ${DIM}no${NC}"; draw_frame; return 1 ;;
  esac
}

# tui_input PROMPT [default] — reads a line with the cursor shown.
TUI_INPUT_VALUE=""
tui_input() {
  local prompt=$1 default=${2:-} value
  log_raw "  ${CYAN}›${NC} ${BOLD}${prompt}${NC}${default:+ ${DIM}(${default})${NC}}"
  draw_frame
  tput cnorm 2>/dev/null
  stty echo 2>/dev/null
  printf '\n  > '
  IFS= read -r value
  stty -echo 2>/dev/null
  tput civis 2>/dev/null
  [ -z "$value" ] && value="$default"
  TUI_INPUT_VALUE="$value"
  unset 'LOG[-1]'
  log_raw "  ${CYAN}›${NC} ${prompt} ${GREEN}${value}${NC}"
  draw_frame
}

# ── Menu model ───────────────────────────────────────────────────────────────

MENU_KIND=()   # header | item
MENU_LABEL=()
MENU_ACTION=()
MENU_HINT=()
CURSOR=0

menu_header() { MENU_KIND+=("header"); MENU_LABEL+=("$1"); MENU_ACTION+=(""); MENU_HINT+=(""); }
menu_item()   { MENU_KIND+=("item");   MENU_LABEL+=("$1"); MENU_ACTION+=("$2"); MENU_HINT+=("${3:-}"); }

build_menu() {
  MENU_KIND=(); MENU_LABEL=(); MENU_ACTION=(); MENU_HINT=()

  menu_header "Release"
  menu_item   "Full release" "act_full_release" "bump → notes → verify → tag → wait for CI → AUR"
  menu_item   "Bump version" "act_bump" "choose the next tag"
  menu_item   "Release notes" "act_notes" "promote Unreleased → \$VERSION in CHANGELOG.md"

  menu_header "Verify"
  menu_item   "Run tests" "act_test" "go test ./..."
  menu_item   "Run linter" "act_lint" "golangci-lint run"
  menu_item   "Build binaries locally" "act_build" "smoke test only; CI builds the real assets"

  menu_header "Publish"
  menu_item   "Commit changes" "act_commit" "stage and commit the working tree"
  menu_item   "Push to $(git -C "$PROJECT_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || echo main)" "act_push" "push the current branch"
  menu_item   "Tag and release" "act_release" "push the tag; CI builds and publishes"
  menu_item   "Wait for release assets" "act_wait_assets" "poll until CI uploads the tarballs"

  menu_header "AUR"
  menu_item   "Publish $AUR_PKGNAME" "act_aur_publish" "render PKGBUILD from the published checksums"
  menu_item   "Show AUR package status" "act_aur_status" "compare AUR pkgver to the latest release"
  menu_item   "AUR setup help" "act_aur_help" "SSH key and account setup"

  # Park the cursor on the first selectable row.
  if [ "${MENU_KIND[$CURSOR]:-header}" = "header" ]; then
    move_cursor 1
  fi
}

move_cursor() {
  local dir=$1 n=${#MENU_KIND[@]} i="$CURSOR" guard=0
  while [ "$guard" -lt "$n" ]; do
    i=$(((i + dir + n) % n))
    guard=$((guard + 1))
    if [ "${MENU_KIND[$i]}" = "item" ]; then
      CURSOR=$i
      return 0
    fi
  done
}

# ── Frame rendering ──────────────────────────────────────────────────────────

status_cell() {
  local label=$1 color=$2 value=$3
  printf '%s' "${DIM}${label}${NC} ${color}${value}${NC}"
}

draw_header() {
  local inner=$((COLS - 4))
  [ "$inner" -gt 74 ] && inner=74
  local title=" TideMail Release "
  local title_len=${#title}
  local dashes=$((inner - title_len - 1))
  [ "$dashes" -lt 0 ] && dashes=0

  frame_line "${BLUE}╭─${NC}${BOLD}${CYAN}${title}${NC}${BLUE}$(repeat_char '─' "$dashes")╮${NC}"

  # Row 1: version and branch.
  local ver_cell branch_cell short_branch
  # A long feature-branch name would otherwise eat the whole status row.
  short_branch="$ST_BRANCH"
  if [ "${#short_branch}" -gt 18 ]; then short_branch="${short_branch:0:17}…"; fi
  ver_cell=$(status_cell "version" "$GREEN" "${ST_VERSION:-?}")
  if [ -n "$ST_NEXT" ]; then
    ver_cell+=" ${DIM}→${NC} ${CYAN}${ST_NEXT}${NC}"
  fi
  if [ "${ST_DIRTY:-0}" -gt 0 ] 2>/dev/null; then
    branch_cell=$(status_cell "git" "$YELLOW" "${short_branch} ⚠ ${ST_DIRTY} dirty")
  else
    branch_cell=$(status_cell "git" "$GREEN" "${short_branch} ✓ clean")
  fi
  frame_line "${BLUE}│${NC} $(pad_to $((inner - 1)) "${ver_cell}   ${branch_cell}")${BLUE}│${NC}"

  # Row 2: toolchain and credentials.
  local go_cell gh_cell aur_cell
  if [ -n "$ST_GO" ]; then go_cell=$(status_cell "go" "$GREEN" "$ST_GO"); else go_cell=$(status_cell "go" "$RED" "missing"); fi
  case "$ST_GH" in
    auth)    gh_cell=$(status_cell "gh" "$GREEN" "✓ auth") ;;
    noauth)  gh_cell=$(status_cell "gh" "$YELLOW" "⚠ no auth") ;;
    *)       gh_cell=$(status_cell "gh" "$RED" "✗ missing") ;;
  esac
  case "${ST_AUR%%:*}" in
    ok)      aur_cell=$(status_cell "aur" "$GREEN" "✓ ${ST_AUR#ok:}") ;;
    denied)  aur_cell=$(status_cell "aur" "$YELLOW" "⚠ key rejected") ;;
    probing) aur_cell=$(status_cell "aur" "$DIM" "…") ;;
    *)       aur_cell=$(status_cell "aur" "$YELLOW" "? unknown") ;;
  esac
  frame_line "${BLUE}│${NC} $(pad_to $((inner - 1)) "${go_cell}   ${gh_cell}   ${aur_cell}")${BLUE}│${NC}"

  frame_line "${BLUE}╰$(repeat_char '─' "$inner")╯${NC}"
}

MENU_OFFSET=0

# draw_menu renders at most HEIGHT rows, scrolling so the cursor stays visible.
# A short terminal must still leave room for the log pane underneath.
draw_menu() {
  local height=$1
  local n=${#MENU_KIND[@]} i

  if [ "$height" -ge "$n" ]; then
    MENU_OFFSET=0
  else
    # Keep one row of context around the cursor where possible.
    if [ "$CURSOR" -lt $((MENU_OFFSET + 1)) ]; then
      MENU_OFFSET=$((CURSOR - 1))
    elif [ "$CURSOR" -gt $((MENU_OFFSET + height - 2)) ]; then
      MENU_OFFSET=$((CURSOR - height + 2))
    fi
    [ "$MENU_OFFSET" -lt 0 ] && MENU_OFFSET=0
    [ "$MENU_OFFSET" -gt $((n - height)) ] && MENU_OFFSET=$((n - height))
  fi

  local last=$((MENU_OFFSET + height))
  [ "$last" -gt "$n" ] && last=$n

  for ((i = MENU_OFFSET; i < last; i++)); do
    local marker="  "
    # Show that the list continues past the visible window.
    if [ "$i" -eq "$MENU_OFFSET" ] && [ "$MENU_OFFSET" -gt 0 ]; then marker=" ${DIM}↑${NC}"; fi
    if [ "$i" -eq $((last - 1)) ] && [ "$last" -lt "$n" ]; then marker=" ${DIM}↓${NC}"; fi

    if [ "${MENU_KIND[$i]}" = "header" ]; then
      frame_line "${marker}${DIM}${MENU_LABEL[$i]}${NC}"
      continue
    fi
    if [ "$i" -eq "$CURSOR" ]; then
      local hint=""
      [ -n "${MENU_HINT[$i]}" ] && hint="  ${DIM}${MENU_HINT[$i]}${NC}"
      frame_line "${marker}${CYAN}▸${NC} ${BOLD}${MENU_LABEL[$i]}${NC}${hint}"
    else
      frame_line "${marker}  ${MENU_LABEL[$i]}"
    fi
  done
}

draw_log() {
  local height=$1
  [ "$height" -lt 1 ] && return 0
  frame_line " ${DIM}$(repeat_char '─' $((COLS - 2)))${NC}"

  if [ -n "$BUSY_MSG" ]; then
    frame_line "  ${CYAN}${SPIN_FRAMES:$SPIN_I:1}${NC} ${BUSY_MSG}…"
    height=$((height - 1))
  fi
  [ "$height" -lt 1 ] && return 0

  local total=${#LOG[@]} start=0
  if [ "$total" -gt "$height" ]; then
    start=$((total - height))
  fi
  local i
  for ((i = start; i < total; i++)); do
    frame_line "${LOG[$i]}"
  done
}

# draw_footer drops hints rather than letting the row be clipped, so the way
# out of the console stays visible however narrow the terminal is.
draw_footer() {
  local hint=" ↑↓/jk move · enter run · r refresh · c clear log · q quit"
  if [ "${#hint}" -gt "$COLS" ]; then
    hint=" ↑↓ move · enter run · q quit"
  fi
  if [ "${#hint}" -gt "$COLS" ]; then
    hint=" ↑↓ · enter · q"
  fi
  frame_line "${DIM}${hint}${NC}"
}

draw_frame() {
  [ "$TUI_ACTIVE" -eq 1 ] || return 0
  if [ "$RESIZE_PENDING" -eq 1 ]; then
    RESIZE_PENDING=0
    measure_term
  fi
  FRAME=""
  FRAME_ROWS=0
  draw_header
  # 4 header rows, the log separator, and the footer row are fixed overhead.
  local available=$((ROWS - 6))
  [ "$available" -lt 1 ] && available=1

  # Split the remaining rows: the menu never grows past its own length and
  # never squeezes the log pane below a usable height.
  local menu_rows=${#MENU_KIND[@]}
  local menu_height=$((available - 8))
  [ "$menu_height" -lt 5 ] && menu_height=5
  [ "$menu_height" -gt "$menu_rows" ] && menu_height=$menu_rows
  [ "$menu_height" -gt "$available" ] && menu_height=$available

  draw_menu "$menu_height"
  local log_height=$((available - menu_height))
  draw_log "$log_height"
  # Push the footer onto the last row so it never floats mid-screen.
  while [ "$FRAME_ROWS" -lt $((ROWS - 1)) ]; do frame_blank; done
  draw_footer
  frame_flush
}

# ── Actions: version and notes ───────────────────────────────────────────────

act_bump() {
  log_step "Bump version"
  read_version
  suggest_next_patch
  log_info "current tag: ${GREEN}${VERSION}${NC}"

  if [ -n "$NEXT_VERSION" ] && tui_confirm "Use $NEXT_VERSION?" y; then
    VERSION="$NEXT_VERSION"
  else
    tui_input "Enter version (e.g. v1.2.3)" "$NEXT_VERSION"
    VERSION="$TUI_INPUT_VALUE"
  fi

  if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log_err "'$VERSION' is not a vMAJOR.MINOR.PATCH tag"
    return 1
  fi
  log_ok "version set to ${GREEN}${VERSION}${NC}"
  refresh_status
}

# ensure_version makes VERSION usable by steps run straight from the menu.
ensure_version() {
  if [ -z "$VERSION" ] || [ "$VERSION" = "v0.0.0" ]; then
    read_version
  fi
  [ -n "$VERSION" ] && [ "$VERSION" != "v0.0.0" ]
}

# changelog_has_section reports whether CHANGELOG.md already documents a tag.
changelog_has_section() {
  grep -qx "## $1" "$PROJECT_DIR/CHANGELOG.md" 2>/dev/null
}

# changelog_section prints one section's body, using the same extraction the
# release workflow runs, so a preview here is exactly what CI will publish.
changelog_section() {
  awk -v ver="## $1" '
    $0 == ver { flag = 1; next }
    /^## / && flag { flag = 0 }
    flag { print }
  ' "$PROJECT_DIR/CHANGELOG.md" 2>/dev/null
}

# changelog_promote renames "## Unreleased" to "## VERSION" and opens a fresh
# empty Unreleased above it. Without this the release workflow finds no section
# for the tag and publishes nothing but the boilerplate description.
changelog_promote() {
  local version=$1 tmp
  if ! grep -qx "## Unreleased" "$PROJECT_DIR/CHANGELOG.md"; then
    log_err "CHANGELOG.md has no '## Unreleased' heading to promote"
    return 1
  fi
  tmp=$(mktemp "${TMPDIR:-/tmp}/tidemail-changelog-XXXXXX")
  awk -v ver="## $version" '
    !promoted && $0 == "## Unreleased" {
      print "## Unreleased"
      print ""
      print ver
      promoted = 1
      next
    }
    { print }
  ' "$PROJECT_DIR/CHANGELOG.md" > "$tmp" || { rm -f "$tmp"; return 1; }

  if ! grep -qx "## $version" "$tmp"; then
    log_err "promotion produced no '## $version' section — changelog left alone"
    rm -f "$tmp"
    return 1
  fi
  cat "$tmp" > "$PROJECT_DIR/CHANGELOG.md" || { rm -f "$tmp"; return 1; }
  rm -f "$tmp"
}

changelog_preview() {
  local body count
  body=$(changelog_section "$1")
  count=$(printf '%s\n' "$body" | grep -cE '^- \*\*')
  log_info "the release workflow will publish $count entr$([ "$count" = 1 ] && echo y || echo ies) for $1:"
  local line
  while IFS= read -r line; do
    [ -n "$line" ] && log_detail "$line"
  done < <(printf '%s\n' "$body" | grep -E '^- \*\*|^### ' | head -10)
}

# act_notes prepares the notes for VERSION in CHANGELOG.md. The release
# workflow builds its notes from the "## <tag>" section, so promoting
# Unreleased here is what makes a tagged release come out documented.
act_notes() {
  log_step "Release notes"
  ensure_version || { log_err "no version selected — bump the version first"; return 1; }

  if [ ! -f "$PROJECT_DIR/CHANGELOG.md" ]; then
    log_err "CHANGELOG.md not found"
    return 1
  fi

  if changelog_has_section "$VERSION"; then
    log_ok "CHANGELOG.md already documents $VERSION"
    changelog_preview "$VERSION"
    return 0
  fi

  local pending
  pending=$(changelog_section "Unreleased")
  if [ -z "$(printf '%s' "$pending" | tr -d '[:space:]')" ]; then
    log_warn "the Unreleased section is empty — $VERSION would publish empty notes"
  else
    log_info "Unreleased holds $(printf '%s\n' "$pending" | grep -cE '^- \*\*') entries"
  fi

  if tui_confirm "Edit CHANGELOG.md before promoting?" y; then
    local editor="${EDITOR:-${VISUAL:-nano}}"
    log_info "opening $editor"
    tui_suspend
    "$editor" "$PROJECT_DIR/CHANGELOG.md"
    tui_resume
  fi

  if ! tui_confirm "Promote '## Unreleased' to '## $VERSION'?" y; then
    log_info "changelog left unchanged — $VERSION will publish empty notes"
    return 1
  fi

  changelog_promote "$VERSION" || return 1
  log_ok "CHANGELOG.md: Unreleased → $VERSION"
  changelog_preview "$VERSION"
}

# ── Actions: verification ────────────────────────────────────────────────────

act_test() {
  log_step "Run tests"
  local start=$(date +%s)
  if run_streamed "go test ./..." go test ./...; then
    log_ok "tests passed ${DIM}($(format_time $(($(date +%s) - start))))${NC}"
    return 0
  fi
  log_err "tests failed"
  return 1
}

act_lint() {
  log_step "Run linter"
  if ! command -v golangci-lint >/dev/null 2>&1; then
    log_warn "golangci-lint not installed — skipping"
    return 0
  fi
  if run_streamed "golangci-lint run" golangci-lint run; then
    log_ok "lint clean"
    return 0
  fi
  log_err "lint reported problems"
  return 1
}

act_build() {
  log_step "Build binaries locally"
  ensure_version
  log_info "local builds are a compile check — release assets come from CI"
  log_detail "CI injects the Google OAuth client via scripts/build-release.sh"

  rm -rf "$DIST_DIR"
  mkdir -p "$DIST_DIR"

  local targets=(
    "linux   amd64 tidemail-linux-x86_64"
    "linux   arm64 tidemail-linux-aarch64"
    "darwin  amd64 tidemail-darwin-x86_64"
    "darwin  arm64 tidemail-darwin-aarch64"
  )
  local failed=0 entry goos goarch name
  for entry in "${targets[@]}"; do
    read -r goos goarch name <<<"$entry"
    if run_quiet "building $goos/$goarch" env CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        go build -ldflags="-s -w -X main.version=$VERSION" -o "$DIST_DIR/$name" "$PROJECT_DIR"; then
      log_ok "$name ${DIM}($(du -h "$DIST_DIR/$name" | cut -f1))${NC}"
    else
      log_err "build failed for $goos/$goarch"
      failed=1
    fi
  done
  [ "$failed" -eq 0 ] && log_ok "all targets compiled"
  return "$failed"
}

# ── Actions: git ─────────────────────────────────────────────────────────────

act_commit() {
  log_step "Commit changes"
  if [ -z "$(git -C "$PROJECT_DIR" status --porcelain)" ]; then
    log_info "nothing to commit"
    return 0
  fi

  git -C "$PROJECT_DIR" add -A
  local skipped=0 path size
  while IFS= read -r path; do
    [ -f "$PROJECT_DIR/$path" ] || continue
    size=$(wc -c <"$PROJECT_DIR/$path" | tr -d ' ')
    if [ "$size" -gt "$GITHUB_FILE_LIMIT_BYTES" ]; then
      git -C "$PROJECT_DIR" restore --staged -- "$path" 2>/dev/null ||
        git -C "$PROJECT_DIR" reset -q HEAD -- "$path"
      log_warn "skipped $path — GitHub rejects files over 100 MB"
      skipped=1
    fi
  done < <(git -C "$PROJECT_DIR" diff --cached --name-only)

  if git -C "$PROJECT_DIR" diff --cached --quiet; then
    log_info "nothing committable after skipping oversized files"
    return 0
  fi

  ensure_version
  if run_quiet "committing" git -C "$PROJECT_DIR" commit -m "chore: release $VERSION"; then
    log_ok "committed: chore: release $VERSION"
    [ "$skipped" -eq 1 ] && log_info "oversized files remain in your working tree"
    refresh_status
    return 0
  fi
  log_err "commit failed"
  return 1
}

act_push() {
  local branch
  branch=$(git -C "$PROJECT_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || echo main)
  log_step "Push $branch"
  if run_streamed "pushing $branch" git -C "$PROJECT_DIR" push origin "$branch"; then
    log_ok "pushed to origin/$branch"
    refresh_status
    return 0
  fi
  log_err "push failed"
  return 1
}

# ── Actions: GitHub release ──────────────────────────────────────────────────

require_gh() {
  if ! command -v gh >/dev/null 2>&1; then
    log_err "GitHub CLI (gh) is not installed"
    return 1
  fi
  if ! gh_is_authenticated; then
    log_err "gh is not authenticated — run 'gh auth login' or export GH_TOKEN"
    return 1
  fi
  return 0
}

act_release() {
  log_step "Tag and release"
  ensure_version || { log_err "no version selected — bump first"; return 1; }
  require_gh || return 1

  # The release workflow builds its notes from the "## <tag>" changelog
  # section. Tagging without one publishes the boilerplate description alone.
  if ! changelog_has_section "$VERSION"; then
    log_warn "CHANGELOG.md has no '## $VERSION' section"
    log_detail "the release workflow would publish empty notes for $VERSION"
    log_detail "run 'Release notes' first to promote Unreleased"
    if ! tui_confirm "Tag $VERSION anyway?" n; then
      return 1
    fi
  fi

  if git -C "$PROJECT_DIR" rev-parse "$VERSION" >/dev/null 2>&1; then
    log_warn "tag $VERSION already exists"
    if ! tui_confirm "Delete and recreate tag $VERSION?" n; then
      log_info "keeping the existing tag"
      return 1
    fi
    git -C "$PROJECT_DIR" tag -d "$VERSION" >/dev/null 2>&1
    git -C "$PROJECT_DIR" push origin --delete "$VERSION" >/dev/null 2>&1
    log_ok "old tag removed"
  fi

  git -C "$PROJECT_DIR" tag -a "$VERSION" -m "Release $VERSION"
  if ! run_streamed "pushing tag $VERSION" git -C "$PROJECT_DIR" push origin "$VERSION"; then
    log_err "tag push failed"
    return 1
  fi
  log_ok "tag $VERSION pushed — the release workflow is building the assets"
  log_detail "https://github.com/${REPO}/actions"
  refresh_status
}

# release_asset_names lists the assets currently attached to a release.
release_asset_names() {
  gh release view "$1" --repo "$REPO" --json assets -q '.assets[].name' 2>/dev/null
}

release_has_all_assets() {
  local names
  names=$(release_asset_names "$1") || return 1
  local required=(SHA256SUMS tidemail-linux-x86_64.tar.gz tidemail-linux-aarch64.tar.gz)
  local want
  for want in "${required[@]}"; do
    printf '%s\n' "$names" | grep -qx "$want" || return 1
  done
  return 0
}

act_wait_assets() {
  log_step "Wait for release assets"
  ensure_version || { log_err "no version selected"; return 1; }
  require_gh || return 1

  if release_has_all_assets "$VERSION"; then
    log_ok "release $VERSION already has every Linux asset"
    return 0
  fi

  log_info "polling for the release workflow to publish $VERSION"
  local deadline=$(( $(date +%s) + ASSET_WAIT_SECONDS ))
  BUSY_MSG="waiting for CI to publish $VERSION"
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if release_has_all_assets "$VERSION"; then
      BUSY_MSG=""
      log_ok "all Linux assets are published"
      return 0
    fi
    # Animate between API polls: ~8s of spinner per poll.
    local tick=0
    while [ "$tick" -lt 40 ]; do
      spin_tick
      sleep 0.2
      tick=$((tick + 1))
    done
  done
  BUSY_MSG=""
  log_err "assets did not appear within $(format_time $ASSET_WAIT_SECONDS)"
  log_detail "check https://github.com/${REPO}/actions"
  return 1
}

# ── Actions: AUR ─────────────────────────────────────────────────────────────

act_aur_help() {
  log_step "AUR setup"
  log_plain "Publishing $AUR_PKGNAME needs an AUR account with your SSH key:"
  log_plain ""
  log_plain "  1. Create an account at https://${AUR_HOST}/register"
  log_plain "  2. Add this public key under My Account → SSH Public Key:"
  if [ -f "$HOME/.ssh/id_ed25519.pub" ]; then
    log_detail "$(cat "$HOME/.ssh/id_ed25519.pub")"
  else
    log_detail "no ~/.ssh/id_ed25519.pub — create one with: ssh-keygen -t ed25519"
  fi
  log_plain "  3. Add this to ~/.ssh/config so git uses that key:"
  log_detail "Host ${AUR_HOST}"
  log_detail "  User aur"
  log_detail "  IdentityFile ~/.ssh/id_ed25519"
  log_plain ""
  log_info "the first push to $AUR_REMOTE creates the package"
  log_info "status line shows 'aur ✓' once the key is accepted"
}

# aur_clone checks out the AUR package repository into DIR. A package that does
# not exist yet cannot be cloned, so start an empty repo with the right remote —
# the AUR creates the package on first push.
aur_clone() {
  local dir=$1
  if git clone --quiet "$AUR_REMOTE" "$dir" 2>/dev/null; then
    if [ -f "$dir/PKGBUILD" ]; then
      log_ok "cloned $AUR_PKGNAME from the AUR"
    else
      # The AUR hands out an empty repo for an unclaimed name; the first
      # push is what actually creates the package.
      log_info "$AUR_PKGNAME is not on the AUR yet — this will create it"
    fi
    return 0
  fi
  log_warn "could not clone $AUR_PKGNAME — starting a fresh repository"
  git init --quiet "$dir" || return 1
  git -C "$dir" remote add origin "$AUR_REMOTE" || return 1
  return 0
}

# aur_current_pkgver reads pkgver from a checked-out PKGBUILD.
aur_current_pkgver() {
  [ -f "$1/PKGBUILD" ] || return 1
  grep -m1 '^pkgver=' "$1/PKGBUILD" | cut -d= -f2
}

aur_current_pkgrel() {
  [ -f "$1/PKGBUILD" ] || return 1
  grep -m1 '^pkgrel=' "$1/PKGBUILD" | cut -d= -f2
}

act_aur_status() {
  log_step "AUR package status"
  local dir
  dir=$(mktemp -d "${TMPDIR:-/tmp}/tidemail-aur-XXXXXX")

  if ! git clone --quiet "$AUR_REMOTE" "$dir/pkg" 2>/dev/null; then
    log_err "could not reach the AUR over SSH"
    log_detail "run 'AUR setup help' if the status line does not show 'aur ✓'"
    rm -rf "$dir"
    return 1
  fi

  # The AUR serves an empty repository for any package name, so a successful
  # clone proves nothing — only a committed PKGBUILD means it is published.
  local pkgver pkgrel
  pkgver=$(aur_current_pkgver "$dir/pkg" 2>/dev/null)
  if [ -z "$pkgver" ]; then
    log_warn "$AUR_PKGNAME is not published yet — the AUR repo is empty"
    log_info "'Publish $AUR_PKGNAME' will create it"
    rm -rf "$dir"
    return 0
  fi

  pkgrel=$(aur_current_pkgrel "$dir/pkg" 2>/dev/null || echo 1)
  log_ok "AUR has $AUR_PKGNAME ${GREEN}${pkgver}-${pkgrel}${NC}"
  read_version
  if [ "${VERSION#v}" = "$pkgver" ]; then
    log_ok "matches the latest tag $VERSION"
  else
    log_warn "latest tag is $VERSION — the AUR is behind"
  fi
  log_detail "https://${AUR_HOST}/packages/${AUR_PKGNAME}"
  rm -rf "$dir"
}

# sha_for_asset pulls one checksum out of the release's SHA256SUMS file.
sha_for_asset() {
  local sums_file=$1 asset=$2
  awk -v want="$asset" '$2 == want { print $1; exit }' "$sums_file"
}

# license_sha256 hashes the LICENSE blob at the tag, which is exactly what the
# PKGBUILD's raw.githubusercontent source URL serves.
license_sha256() {
  local tag=$1
  git -C "$PROJECT_DIR" show "${tag}:LICENSE" 2>/dev/null | sha256sum | cut -d' ' -f1
}

act_aur_publish() {
  log_step "Publish $AUR_PKGNAME to the AUR"
  ensure_version || { log_err "no version selected"; return 1; }
  require_gh || return 1

  if [ "${ST_AUR%%:*}" = "denied" ]; then
    log_err "the AUR rejected your SSH key — run 'AUR setup help'"
    return 1
  fi

  local pkgver="${VERSION#v}"
  if [[ ! "$pkgver" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log_err "$VERSION is not a release tag the AUR can package"
    return 1
  fi

  # 1. The checksums must come from the assets users actually download.
  if ! release_has_all_assets "$VERSION"; then
    log_err "release $VERSION is missing Linux assets"
    log_info "run 'Wait for release assets' first"
    return 1
  fi

  local work
  work=$(mktemp -d "${TMPDIR:-/tmp}/tidemail-aur-XXXXXX")
  if ! run_quiet "downloading SHA256SUMS" \
      gh release download "$VERSION" --repo "$REPO" -p SHA256SUMS -O "$work/SHA256SUMS"; then
    log_err "could not download SHA256SUMS for $VERSION"
    rm -rf "$work"
    return 1
  fi

  local sha_x86 sha_arm sha_license
  sha_x86=$(sha_for_asset "$work/SHA256SUMS" "tidemail-linux-x86_64.tar.gz")
  sha_arm=$(sha_for_asset "$work/SHA256SUMS" "tidemail-linux-aarch64.tar.gz")
  sha_license=$(license_sha256 "$VERSION")

  if [ -z "$sha_x86" ] || [ -z "$sha_arm" ]; then
    log_err "SHA256SUMS does not list both Linux tarballs"
    rm -rf "$work"
    return 1
  fi
  if [ -z "$sha_license" ]; then
    log_err "could not hash LICENSE at $VERSION — is the tag fetched locally?"
    rm -rf "$work"
    return 1
  fi
  log_ok "checksums read from the published release"
  log_detail "x86_64  ${sha_x86:0:16}…"
  log_detail "aarch64 ${sha_arm:0:16}…"

  # 2. Check out the AUR package and decide the pkgrel.
  local repo_dir="$work/aur"
  if ! aur_clone "$repo_dir"; then
    log_err "could not prepare the AUR checkout"
    rm -rf "$work"
    return 1
  fi

  local pkgrel=1 old_ver old_rel
  old_ver=$(aur_current_pkgver "$repo_dir" 2>/dev/null)
  old_rel=$(aur_current_pkgrel "$repo_dir" 2>/dev/null)
  if [ -n "$old_ver" ] && [ "$old_ver" = "$pkgver" ]; then
    pkgrel=$(( ${old_rel:-1} + 1 ))
    log_info "same pkgver already published — bumping pkgrel to $pkgrel"
  elif [ -n "$old_ver" ]; then
    log_info "updating $old_ver-${old_rel:-1} → ${pkgver}-1"
  fi

  # 3. Render the PKGBUILD and its .SRCINFO.
  if ! bash "$PROJECT_DIR/packaging/aur/render-pkgbuild.sh" \
      --version "$VERSION" --pkgrel "$pkgrel" \
      --sha256-x86_64 "$sha_x86" --sha256-aarch64 "$sha_arm" \
      --sha256-license "$sha_license" \
      --output "$repo_dir/PKGBUILD"; then
    log_err "rendering the PKGBUILD failed"
    rm -rf "$work"
    return 1
  fi
  log_ok "PKGBUILD rendered for ${pkgver}-${pkgrel}"

  if ! command -v makepkg >/dev/null 2>&1; then
    log_err "makepkg is required to generate .SRCINFO (install base-devel)"
    rm -rf "$work"
    return 1
  fi
  if ! ( cd "$repo_dir" && makepkg --printsrcinfo > .SRCINFO 2>/dev/null ); then
    log_err "makepkg rejected the PKGBUILD"
    rm -rf "$work"
    return 1
  fi
  log_ok ".SRCINFO generated"

  # 4. Show what would be published before anything leaves this machine.
  log_plain ""
  log_info "${BOLD}${AUR_PKGNAME} ${pkgver}-${pkgrel}${NC} would be published as:"
  log_detail "x86_64  tidemail-linux-x86_64.tar.gz  $sha_x86"
  log_detail "aarch64 tidemail-linux-aarch64.tar.gz $sha_arm"
  log_detail "LICENSE at tag $VERSION               $sha_license"

  # `git add -N` registers the new files so `git diff` also renders the very
  # first publish, where both files are still untracked.
  git -C "$repo_dir" add -N PKGBUILD .SRCINFO 2>/dev/null
  local line diff_lines=0
  while IFS= read -r line; do
    log_raw "    ${DIM}${line}${NC}"
    diff_lines=$((diff_lines + 1))
  done < <(git -C "$repo_dir" diff --no-color -- PKGBUILD .SRCINFO 2>/dev/null |
             grep -E '^[+-]' | grep -vE '^[+-]{3}' | head -24)
  if [ "$diff_lines" -eq 0 ]; then
    log_info "no change against what the AUR already has"
  fi
  if [ -z "$old_ver" ]; then
    log_info "this is the first publish of $AUR_PKGNAME"
  fi

  if ! tui_confirm "Push ${AUR_PKGNAME} ${pkgver}-${pkgrel} to the AUR?" n; then
    log_info "not pushed — the rendered package is at $repo_dir"
    log_detail "inspect it, then delete the directory when done"
    return 1
  fi

  # 5. Commit and push.
  git -C "$repo_dir" add PKGBUILD .SRCINFO
  if git -C "$repo_dir" diff --cached --quiet 2>/dev/null; then
    log_info "the AUR already has this exact package — nothing to push"
    rm -rf "$work"
    return 0
  fi

  local msg="Update to ${pkgver}-${pkgrel}"
  [ -z "$old_ver" ] && msg="Add ${AUR_PKGNAME} ${pkgver}-${pkgrel}"
  if ! run_quiet "committing" git -C "$repo_dir" commit -m "$msg"; then
    log_err "commit failed"
    rm -rf "$work"
    return 1
  fi

  # The AUR may prompt for an SSH key passphrase, which needs the real terminal.
  tui_suspend
  git -C "$repo_dir" push origin HEAD:master
  local push_rc=$?
  tui_resume

  if [ "$push_rc" -ne 0 ]; then
    log_err "push to the AUR failed"
    log_detail "checkout kept at $repo_dir"
    return 1
  fi
  log_ok "published ${AUR_PKGNAME} ${pkgver}-${pkgrel}"
  log_detail "https://${AUR_HOST}/packages/${AUR_PKGNAME}"
  rm -rf "$work"
  refresh_status
}

# ── Full release pipeline ────────────────────────────────────────────────────

act_full_release() {
  local start=$(date +%s)
  log_raw ""
  log_raw "  ${BOLD}${CYAN}━━ Full release ━━${NC}"

  act_bump           || { log_err "release aborted at: version bump"; return 1; }
  act_notes          || { log_err "release aborted at: release notes"; return 1; }
  act_test           || { log_err "release aborted at: tests"; return 1; }
  act_lint           || { log_err "release aborted at: lint"; return 1; }
  act_build          || { log_err "release aborted at: build"; return 1; }
  act_commit         || { log_err "release aborted at: commit"; return 1; }
  act_push           || { log_err "release aborted at: push"; return 1; }
  act_release        || { log_err "release aborted at: tag"; return 1; }
  act_wait_assets    || { log_err "release aborted at: waiting for assets"; return 1; }

  if tui_confirm "Publish $AUR_PKGNAME $VERSION to the AUR now?" y; then
    act_aur_publish  || { log_err "GitHub release is live, but the AUR publish failed"; return 1; }
  else
    log_info "skipped the AUR — run 'Publish $AUR_PKGNAME' when ready"
  fi

  log_raw ""
  log_raw "  ${BOLD}${GREEN}✓ Release $VERSION complete${NC} ${DIM}($(format_time $(($(date +%s) - start))))${NC}"
  log_detail "https://github.com/${REPO}/releases/tag/$VERSION"
}

# ── Main loop ────────────────────────────────────────────────────────────────

run_action() {
  local action=$1
  [ -n "$action" ] || return 0
  "$action"
  local rc=$?
  BUSY_MSG=""
  refresh_status
  draw_frame
  return $rc
}

main() {
  if [ ! -t 0 ] || [ ! -t 1 ]; then
    echo "deploy.sh needs an interactive terminal." >&2
    exit 1
  fi
  cd "$PROJECT_DIR" || exit 1

  aur_probe_start
  refresh_status
  build_menu
  tui_enter

  log_raw "  ${DIM}TideMail release console — select an action and press enter.${NC}"
  draw_frame

  local key
  while :; do
    # Pick up the background AUR probe without blocking the loop. While it is
    # still running, wake up every second so the result appears on its own
    # instead of waiting for the next keystroke.
    aur_probe_read
    local rc
    read_key 1
    rc=$?
    if [ "$rc" -eq 1 ]; then
      # stdin closed (piped input exhausted) — leave cleanly rather than spin.
      tui_leave
      exit 0
    fi
    if [ "$rc" -eq 2 ]; then
      # Idle tick: redraw only when something actually changed, so a quiet
      # console costs nothing.
      if [ "$RESIZE_PENDING" -eq 1 ] || [ "${ST_AUR%%:*}" = "probing" ]; then
        aur_probe_read
        draw_frame
      fi
      continue
    fi
    key="$READ_KEY_VALUE"
    case "$key" in
      $'\033[A' | k) move_cursor -1 ;;
      $'\033[B' | j) move_cursor 1 ;;
      $'\033[5~')    move_cursor -1 ;;
      $'\033[6~')    move_cursor 1 ;;
      "" | $'\n' | $'\r' | $'\033[C' | l)
        run_action "${MENU_ACTION[$CURSOR]}"
        ;;
      r | R) refresh_status; log_info "status refreshed" ;;
      c | C) LOG=(); log_raw "  ${DIM}log cleared${NC}" ;;
      g)     CURSOR=0; move_cursor 1 ;;
      G)     CURSOR=$((${#MENU_KIND[@]} - 1)); [ "${MENU_KIND[$CURSOR]}" = "header" ] && move_cursor -1 ;;
      q | Q | $'\033')
        tui_leave
        echo
        echo "  Bye."
        echo
        exit 0
        ;;
    esac
    draw_frame
  done
}

main "$@"
