#!/usr/bin/env bash
#
# Tide release console — one script for every Tide app.
#
# A full-screen TUI for cutting a release: version bump, release notes,
# verification, the GitHub release, and publishing the -bin package to the AUR.
#
# This file is identical in every Tide repository. What differs between the
# apps - name, repository, binaries, colours - lives in deploy.conf beside it.
# Change the console in one repository, then copy it to the others with
#
#     ./deploy.sh --sync ../tide ../tidemail ../tideftp ../tidesms ../tidedeck
#
# Deliberately not `set -e`: an interactive loop must survive a failed step and
# redraw, not exit mid-frame. Every step reports its own status instead.
set -uo pipefail

DEPLOY_SCRIPT_VERSION="2.0.0"

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SELF="$PROJECT_DIR/$(basename "${BASH_SOURCE[0]}")"
DIST_DIR="$PROJECT_DIR/dist"
CONF_FILE="$PROJECT_DIR/deploy.conf"

# ── Per-app configuration ────────────────────────────────────────────────────
# Defaults first; deploy.conf overrides them. See the comments in any
# repository's deploy.conf for what each one means.

APP_NAME=""            # shown in the banner: "TideSMS"
BINARY_NAME=""         # the main binary, and the prefix of every release asset
REPO=""                # owner/name on GitHub
AUR_PKGNAME=""         # usually "$BINARY_NAME-bin"
BINARIES=()            # "name:go-package" for each binary a release ships
CHECKSUMS_ASSET="SHA256SUMS"
GO_BUILD_FLAGS=(-trimpath)
VERSION_VAR="main.version"
# goos goarch asset-arch: the asset-arch is what the file names say.
BUILD_TARGETS=(
  "linux  amd64 x86_64"
  "linux  arm64 aarch64"
  "darwin amd64 x86_64"
  "darwin arm64 aarch64"
)
# "changelog" promotes CHANGELOG.md's Unreleased section, which the release
# workflow publishes; "generated" leaves the notes to GitHub; "auto" picks
# changelog when CHANGELOG.md exists.
RELEASE_NOTES="auto"
BUILD_NOTE=""          # a line shown by the local build, if the app needs one
APP_GLYPH="≋"
ACCENT_RGB="94;196;215"   # the banner's gradient runs from ACCENT_RGB…
ACCENT2_RGB="137;180;250" # …to ACCENT2_RGB
AUR_HOST="aur.archlinux.org"
GITHUB_FILE_LIMIT_BYTES=$((100 * 1024 * 1024))
# How long to wait for the release workflow to publish its assets.
ASSET_WAIT_SECONDS=900

load_config() {
  if [ ! -f "$CONF_FILE" ]; then
    echo "deploy.sh: no deploy.conf beside the script ($CONF_FILE)." >&2
    echo "Copy one from another Tide repository and change the app's details." >&2
    exit 2
  fi
  # shellcheck source=/dev/null
  source "$CONF_FILE"
  local missing=()
  [ -n "$APP_NAME" ] || missing+=(APP_NAME)
  [ -n "$BINARY_NAME" ] || missing+=(BINARY_NAME)
  [ -n "$REPO" ] || missing+=(REPO)
  [ "${#BINARIES[@]}" -gt 0 ] || missing+=(BINARIES)
  if [ "${#missing[@]}" -gt 0 ]; then
    echo "deploy.sh: deploy.conf does not set: ${missing[*]}" >&2
    exit 2
  fi
  [ -n "$AUR_PKGNAME" ] || AUR_PKGNAME="${BINARY_NAME}-bin"
  AUR_REMOTE="ssh://aur@${AUR_HOST}/${AUR_PKGNAME}.git"
  case "$RELEASE_NOTES" in
    auto) [ -f "$PROJECT_DIR/CHANGELOG.md" ] && RELEASE_NOTES=changelog || RELEASE_NOTES=generated ;;
    changelog | generated) ;;
    *) echo "deploy.sh: RELEASE_NOTES must be auto, changelog or generated" >&2; exit 2 ;;
  esac
}

notes_from_changelog() { [ "$RELEASE_NOTES" = "changelog" ]; }

# asset_name OS ARCH is the tarball a release carries for one target.
asset_name() { printf '%s-%s-%s.tar.gz' "$BINARY_NAME" "$1" "$2"; }

# ── Command line ─────────────────────────────────────────────────────────────

DRY_RUN=0
MODE="console"
SYNC_TARGETS=()

usage() {
  cat <<'USAGE'
Usage: ./deploy.sh [--dry-run]
       ./deploy.sh --check
       ./deploy.sh --sync DIR [DIR...]

  -n, --dry-run   Rehearse: no commit, push, tag, or file is written.
                  Toggle it from inside the console with `d`.
      --check     Print the release-readiness checklist and exit:
                  0 when nothing fails, 1 otherwise. No terminal needed.
      --sync      Copy this console into other Tide repositories, each of
                  which keeps its own deploy.conf.
  -V, --version   Print the console's version.
USAGE
}

parse_args() {
  while [ $# -gt 0 ]; do
    case "$1" in
      -n | --dry-run) DRY_RUN=1 ;;
      --check) MODE="check" ;;
      --sync)
        MODE="sync"
        shift
        while [ $# -gt 0 ] && [[ $1 != -* ]]; do SYNC_TARGETS+=("$1"); shift; done
        continue
        ;;
      -V | --version) echo "Tide release console $DEPLOY_SCRIPT_VERSION"; exit 0 ;;
      -h | --help) usage; exit 0 ;;
      *) echo "deploy.sh: unknown argument '$1'" >&2; usage >&2; exit 2 ;;
    esac
    shift
  done
}

# sync_into copies this script into other repositories. A repository without
# a deploy.conf is skipped, because the console would not know what it is.
sync_into() {
  if [ "${#SYNC_TARGETS[@]}" -eq 0 ]; then
    echo "deploy.sh --sync needs at least one repository directory" >&2
    exit 2
  fi
  local dir status=0
  for dir in "${SYNC_TARGETS[@]}"; do
    if [ ! -f "$dir/deploy.conf" ]; then
      echo "  ✗ $dir — no deploy.conf there; create one first"
      status=1
      continue
    fi
    if [ "$(cd "$dir" && pwd)" = "$PROJECT_DIR" ]; then
      echo "  · $dir — this repository"
      continue
    fi
    if cmp -s "$SELF" "$dir/deploy.sh"; then
      echo "  ✓ $dir — already current"
      continue
    fi
    cp "$SELF" "$dir/deploy.sh" && chmod +x "$dir/deploy.sh" && echo "  ✓ $dir — updated to $DEPLOY_SCRIPT_VERSION" || status=1
  done
  exit "$status"
}

# VERSION is the release being prepared — chosen by the bump step and sticky
# until the console exits. CURRENT_TAG is whatever git already has; keeping them
# apart is what stops a status refresh from silently undoing a version bump.
VERSION=""
CURRENT_TAG=""
NEXT_VERSION=""

# ── Colors ───────────────────────────────────────────────────────────────────

TRUECOLOR=0
setup_colors() {
  if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    RED=$'\033[0;31m';     GREEN=$'\033[0;32m';   YELLOW=$'\033[0;33m'
    BLUE=$'\033[0;34m';    CYAN=$'\033[0;36m';    MAGENTA=$'\033[0;35m'
    BOLD=$'\033[1m';       DIM=$'\033[2m';        NC=$'\033[0m'
    REV=$'\033[7m'
    case "${COLORTERM:-}" in truecolor | 24bit) TRUECOLOR=1 ;; esac
    if [ "$TRUECOLOR" -eq 1 ]; then
      ACCENT=$'\033[38;2;'"$ACCENT_RGB"'m'
    else
      ACCENT=$CYAN
    fi
  else
    RED=''; GREEN=''; YELLOW=''; BLUE=''; CYAN=''; MAGENTA=''; BOLD=''; DIM=''; NC=''
    REV=''; ACCENT=''
  fi
}

# gradient TEXT paints text from ACCENT_RGB to ACCENT2_RGB, one character at a
# time. Without truecolor it is simply the accent.
gradient() {
  local text=$1
  if [ "$TRUECOLOR" -ne 1 ] || [ -z "$NC" ]; then
    printf '%s' "${ACCENT}${BOLD}${text}${NC}"
    return
  fi
  local r1 g1 b1 r2 g2 b2
  IFS=';' read -r r1 g1 b1 <<<"$ACCENT_RGB"
  IFS=';' read -r r2 g2 b2 <<<"$ACCENT2_RGB"
  local n=${#text} i out="" span
  span=$((n > 1 ? n - 1 : 1))
  for ((i = 0; i < n; i++)); do
    out+=$'\033[1;38;2;'"$((r1 + (r2 - r1) * i / span));$((g1 + (g2 - g1) * i / span));$((b1 + (b2 - b1) * i / span))m${text:i:1}"
  done
  printf '%s' "${out}${NC}"
}

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

# ── Frame buffer ─────────────────────────────────────────────────────────────
# Lines are written with ESC[K (erase to end of line) so a shorter line never
# leaves the tail of the previous frame behind, and no padding math is needed
# except where a box border has to land on the right edge.

CLR_EOL=$'\033[K'

# pad_to WIDTH TEXT [nopad] fits a possibly-colored string to an exact
# printable width: padded with spaces when short, clipped when long, so a box
# border always lands on the same column. Escape sequences are copied through
# and never counted. With "nopad" it only clips, leaving a short line short.
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

# visible_width TEXT counts the printable characters, skipping escapes.
visible_width() {
  local text=$1
  text=$(printf '%s' "$text" | sed $'s/\033\\[[0-9;]*m//g')
  printf '%s' "${#text}"
}

# repeat_char CHAR N
repeat_char() {
  local ch=$1 n=$2 out=""
  while [ "$n" -gt 0 ]; do out+="$ch"; n=$((n - 1)); done
  printf '%s' "$out"
}

FRAME=""
FRAME_ROWS=0
PROMPT_TEXT=""
PROMPT_HINT=""
PROMPT_KEYS=""
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

ST_VERSION="" ST_NEXT="" ST_BRANCH="" ST_DIRTY="" ST_GO="" ST_GH="" ST_AUR="" ST_AHEAD=""
AUR_PROBE_FILE=""

read_current_tag() {
  CURRENT_TAG=$(git -C "$PROJECT_DIR" describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || echo "v0.0.0")
}

# semver_parts TAG sets MAJ MIN PAT, or fails for a tag that is not vX.Y.Z.
MAJ=0 MIN=0 PAT=0
semver_parts() {
  local clean=${1#v}
  [[ $clean =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] || return 1
  MAJ=${BASH_REMATCH[1]} MIN=${BASH_REMATCH[2]} PAT=${BASH_REMATCH[3]}
}

# semver_gt A B reports whether A is a later version than B.
semver_gt() {
  local a1 a2 a3
  semver_parts "$1" || return 1
  a1=$MAJ a2=$MIN a3=$PAT
  semver_parts "$2" || return 0
  [ "$a1" -gt "$MAJ" ] && return 0
  [ "$a1" -lt "$MAJ" ] && return 1
  [ "$a2" -gt "$MIN" ] && return 0
  [ "$a2" -lt "$MIN" ] && return 1
  [ "$a3" -gt "$PAT" ]
}

suggest_next_patch() {
  NEXT_VERSION=""
  if semver_parts "$CURRENT_TAG"; then
    NEXT_VERSION="v${MAJ}.${MIN}.$((PAT + 1))"
  fi
}

gh_is_authenticated() { gh auth status -h github.com >/dev/null 2>&1; }

# The AUR probe opens an SSH connection, so it runs once in the background and
# the result is read from a file on later redraws instead of stalling a frame.
aur_probe_start() {
  [ -n "$AUR_PROBE_FILE" ] && return 0
  AUR_PROBE_FILE=$(mktemp -u "${TMPDIR:-/tmp}/${BINARY_NAME}-aur-probe-XXXXXX")
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

# ahead_of_origin prints how many local commits the branch has that its
# upstream does not, as of the last fetch; nothing when there is no upstream.
ahead_of_origin() {
  git -C "$PROJECT_DIR" rev-list --count '@{u}..HEAD' 2>/dev/null
}

refresh_status() {
  read_current_tag
  suggest_next_patch
  ST_VERSION="$CURRENT_TAG"
  ST_NEXT="$NEXT_VERSION"
  ST_BRANCH=$(git -C "$PROJECT_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "?")
  ST_DIRTY=$(git -C "$PROJECT_DIR" status --porcelain 2>/dev/null | wc -l | tr -d ' ')
  ST_AHEAD=$(ahead_of_origin)
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
log_step()    { log_raw ""; log_raw "  ${ACCENT}▸${NC} ${BOLD}$1${NC}"; }
log_ok()      { log_raw "  ${GREEN}✓${NC} $1"; }
log_err()     { log_raw "  ${RED}✗${NC} $1"; }
log_warn()    { log_raw "  ${YELLOW}⚠${NC} $1"; }
log_info()    { log_raw "  ${BLUE}ℹ${NC} $1"; }
log_detail()  { log_raw "    ${DIM}$1${NC}"; }

format_time() {
  local seconds=$1
  if [ "$seconds" -ge 60 ]; then echo "$((seconds / 60))m $((seconds % 60))s"; else echo "${seconds}s"; fi
}

# log_box TITLE COLOR LINE... draws a small framed panel into the log, for the
# results worth seeing at a glance.
log_box() {
  local title=$1 color=$2; shift 2
  local width=${#title} line w
  for line in "$@"; do
    w=$(visible_width "$line")
    [ "$w" -gt "$width" ] && width=$w
  done
  width=$((width + 2))
  local top=$((width - ${#title} - 1))
  [ "$top" -lt 0 ] && top=0
  log_raw ""
  log_raw "  ${color}╭─${NC}${BOLD}${color}${title}${NC}${color}$(repeat_char '─' "$top")╮${NC}"
  for line in "$@"; do
    log_raw "  ${color}│${NC} $(pad_to $((width - 1)) "$line")${color}│${NC}"
  done
  log_raw "  ${color}╰$(repeat_char '─' "$width")╯${NC}"
}

# ── Running commands with live output ────────────────────────────────────────

SPIN_FRAMES='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
SPIN_I=0
BUSY_MSG=""
BUSY_START=0

set_busy() { BUSY_MSG=$1; BUSY_START=$(date +%s); }
clear_busy() { BUSY_MSG=""; BUSY_START=0; }

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
  fifo=$(mktemp -u "${TMPDIR:-/tmp}/${BINARY_NAME}-deploy-fifo-XXXXXX")
  rc_file="${fifo}.rc"
  mkfifo "$fifo" 2>/dev/null || { log_err "could not create pipe for: $desc"; return 1; }

  set_busy "$desc"
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
  clear_busy
  return "${status:-1}"
}

# mutate DESC CMD... — the single gate for anything that changes state outside
# this process. In a dry run it reports the command and returns success, so the
# rest of the flow still exercises.
mutate() {
  local desc=$1; shift
  if [ "$DRY_RUN" -eq 1 ]; then
    log_warn "would $desc"
    log_detail "$*"
    return 0
  fi
  run_quiet "$desc" "$@"
}

# mutate_streamed is mutate for a command whose output is worth watching.
mutate_streamed() {
  local desc=$1; shift
  if [ "$DRY_RUN" -eq 1 ]; then
    log_warn "would $desc"
    log_detail "$*"
    return 0
  fi
  run_streamed "$desc" "$@"
}

# log_result reports a completed action, or makes plain that a dry run only
# rehearsed it — a rehearsal saying "pushed" is how a dry run stops being dry
# in the reader's head.
log_result() {
  if [ "$DRY_RUN" -eq 1 ]; then
    log_detail "dry run — nothing changed"
  else
    log_ok "$1"
  fi
}

# dry_run_blocks reports true when an operation must be skipped entirely,
# for the cases that are not a single command.
dry_run_blocks() {
  [ "$DRY_RUN" -eq 1 ] || return 1
  log_warn "would $1"
  return 0
}

# run_quiet DESC CMD... — same, but only surfaces output on failure.
run_quiet() {
  local desc=$1; shift
  local out status
  set_busy "$desc"
  draw_frame
  out=$("$@" 2>&1)
  status=$?
  clear_busy
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

# ask PROMPT KEYS-HINT — puts a question on its own highlighted row and waits
# for a key, leaving it in READ_KEY_VALUE. EOF answers with Esc.
ask() {
  PROMPT_TEXT=$1
  PROMPT_HINT=$2
  draw_frame
  if ! read_key; then READ_KEY_VALUE=$'\033'; fi
}

# tui_confirm PROMPT [default-yes] asks a yes/no question.
#
# Only y, n, Enter and Esc answer it. Any other key used to take the default,
# so a stray j or an arrow key at "Publish to the AUR now? [Y/n]" published;
# now it is ignored and the question stays until it is actually answered.
tui_confirm() {
  local prompt=$1 default=${2:-n} hint answer=""
  if [ "$default" = "y" ]; then hint="Y/n"; else hint="y/N"; fi
  PROMPT_KEYS="y yes · n no · enter takes the capitalised default · esc no"
  while [ -z "$answer" ]; do
    ask "$prompt" "$hint"
    case "$READ_KEY_VALUE" in
      y | Y) answer=yes ;;
      n | N | $'\033') answer=no ;;
      "" | $'\n' | $'\r') [ "$default" = "y" ] && answer=yes || answer=no ;;
    esac
  done
  PROMPT_TEXT="" PROMPT_HINT="" PROMPT_KEYS=""
  if [ "$answer" = yes ]; then
    log_raw "  ${YELLOW}?${NC} ${prompt} ${GREEN}yes${NC}"; draw_frame; return 0
  fi
  log_raw "  ${YELLOW}?${NC} ${prompt} ${DIM}no${NC}"; draw_frame; return 1
}

# tui_input PROMPT [default] — reads a line with the cursor shown.
TUI_INPUT_VALUE=""
tui_input() {
  local prompt=$1 default=${2:-} value
  log_raw "  ${ACCENT}›${NC} ${BOLD}${prompt}${NC}${default:+ ${DIM}(${default})${NC}}"
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
  log_raw "  ${ACCENT}›${NC} ${prompt} ${GREEN}${value}${NC}"
  draw_frame
}

# ── Menu model ───────────────────────────────────────────────────────────────

MENU_KIND=()   # header | item
MENU_LABEL=()
MENU_ACTION=()
MENU_HINT=()
CURSOR=0
# RESULT holds how each action last ended, for the ✓/✗ beside it.
declare -A RESULT=()

menu_header() { MENU_KIND+=("header"); MENU_LABEL+=("$1"); MENU_ACTION+=(""); MENU_HINT+=(""); }
menu_item()   { MENU_KIND+=("item");   MENU_LABEL+=("$1"); MENU_ACTION+=("$2"); MENU_HINT+=("${3:-}"); }

build_menu() {
  MENU_KIND=(); MENU_LABEL=(); MENU_ACTION=(); MENU_HINT=()

  menu_header "Release"
  local pipeline="bump → verify → tag → wait for CI → AUR"
  notes_from_changelog && pipeline="bump → notes → verify → tag → wait for CI → AUR"
  menu_item   "Full release" "act_full_release" "$pipeline"
  menu_item   "Check readiness" "act_check" "everything a release needs, checked at once"
  menu_item   "Bump version" "act_bump" "patch, minor or major"
  if notes_from_changelog; then
    menu_item "Release notes" "act_notes" "promote Unreleased → \$VERSION in CHANGELOG.md"
  fi

  menu_header "Verify"
  menu_item   "Run tests" "act_test" "go test ./..."
  menu_item   "Run linter" "act_lint" "golangci-lint run"
  menu_item   "Build binaries locally" "act_build" "compile check; CI builds the real assets"

  menu_header "Publish"
  menu_item   "Commit changes" "act_commit" "review, then commit the working tree"
  menu_item   "Push $(git -C "$PROJECT_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || echo main)" "act_push" "push the current branch"
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

# ── Pipeline tracker ─────────────────────────────────────────────────────────
# A full release is a row of steps drawn under the banner, filling in as it
# goes, so where it is and where it stopped are never a matter of scrolling.

PIPE_ACTIVE=0
PIPE_NAMES=()
PIPE_STATE=()   # pending | running | done | failed | skipped

pipe_begin() { PIPE_NAMES=("$@"); PIPE_STATE=(); local _; for _ in "$@"; do PIPE_STATE+=(pending); done; PIPE_ACTIVE=1; }
pipe_set()   { PIPE_STATE[$1]=$2; draw_frame; }

draw_pipeline() {
  [ "$PIPE_ACTIVE" -eq 1 ] || return 0
  local i out=" " mark sep="${DIM} ─ ${NC}" name
  for ((i = 0; i < ${#PIPE_NAMES[@]}; i++)); do
    name=${PIPE_NAMES[$i]}
    case "${PIPE_STATE[$i]}" in
      done)    mark="${GREEN}●${NC} ${DIM}${name}${NC}" ;;
      running) mark="${ACCENT}${SPIN_FRAMES:$SPIN_I:1}${NC} ${BOLD}${name}${NC}" ;;
      failed)  mark="${RED}✗ ${name}${NC}" ;;
      skipped) mark="${DIM}◌ ${name}${NC}" ;;
      *)       mark="${DIM}○ ${name}${NC}" ;;
    esac
    [ "$i" -gt 0 ] && out+="$sep"
    out+="$mark"
  done
  # Too wide for the terminal: say where it is instead.
  if [ "$(visible_width "$out")" -gt "$COLS" ]; then
    local done_n=0 current="" state
    for ((i = 0; i < ${#PIPE_NAMES[@]}; i++)); do
      state=${PIPE_STATE[$i]}
      [ "$state" = done ] || [ "$state" = skipped ] && done_n=$((done_n + 1))
      [ "$state" = running ] || [ "$state" = failed ] && current="${PIPE_NAMES[$i]}"
    done
    out=" ${ACCENT}${done_n}/${#PIPE_NAMES[@]}${NC} ${BOLD}${current:-done}${NC}"
  fi
  frame_line "$out"
}

# ── Frame rendering ──────────────────────────────────────────────────────────

status_cell() {
  local label=$1 color=$2 value=$3
  printf '%s' "${DIM}${label}${NC} ${color}${value}${NC}"
}

draw_header() {
  local inner=$((COLS - 4))
  [ "$inner" -gt 96 ] && inner=96

  local title=" ${APP_GLYPH} ${APP_NAME} " subtitle=" release console " right=" v${DEPLOY_SCRIPT_VERSION} "
  local painted
  painted="$(gradient "$title")${DIM}${subtitle}${NC}"
  local used=$(( ${#title} + ${#subtitle} ))
  if [ "$DRY_RUN" -eq 1 ]; then
    painted+=" ${YELLOW}${REV}${BOLD} DRY RUN ${NC}"
    used=$((used + 10))
  fi
  local dashes=$((inner - used - ${#right} - 1))
  if [ "$dashes" -lt 1 ]; then right=""; dashes=$((inner - used - 1)); fi
  [ "$dashes" -lt 0 ] && dashes=0
  frame_line "${ACCENT}╭─${NC}${painted}${ACCENT}$(repeat_char '─' "$dashes")${NC}${DIM}${right}${NC}${ACCENT}╮${NC}"

  # Row 1: version and branch.
  local ver_cell branch_cell short_branch
  # A long feature-branch name would otherwise eat the whole status row.
  short_branch="$ST_BRANCH"
  if [ "${#short_branch}" -gt 18 ]; then short_branch="${short_branch:0:17}…"; fi
  ver_cell=$(status_cell "version" "$GREEN" "${ST_VERSION:-?}")
  if [ -n "$VERSION" ] && [ "$VERSION" != "$ST_VERSION" ]; then
    # A bump has been made: show what is actually going to be released, so a
    # pending version is never invisible on the way to the tag step.
    ver_cell+=" ${DIM}→${NC} ${BOLD}${ACCENT}releasing ${VERSION}${NC}"
  elif [ -n "$ST_NEXT" ]; then
    ver_cell+=" ${DIM}→${NC} ${ACCENT}${ST_NEXT}${NC}"
  fi
  if [ "${ST_DIRTY:-0}" -gt 0 ] 2>/dev/null; then
    branch_cell=$(status_cell "git" "$YELLOW" "${short_branch} ⚠ ${ST_DIRTY} dirty")
  elif [ "${ST_AHEAD:-0}" -gt 0 ] 2>/dev/null; then
    branch_cell=$(status_cell "git" "$YELLOW" "${short_branch} ↑${ST_AHEAD} unpushed")
  else
    branch_cell=$(status_cell "git" "$GREEN" "${short_branch} ✓ clean")
  fi
  frame_line "${ACCENT}│${NC} $(pad_to $((inner - 1)) "${ver_cell}   ${branch_cell}")${ACCENT}│${NC}"

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
    probing) aur_cell=$(status_cell "aur" "$DIM" "${SPIN_FRAMES:$SPIN_I:1}") ;;
    *)       aur_cell=$(status_cell "aur" "$YELLOW" "? unknown") ;;
  esac
  frame_line "${ACCENT}│${NC} $(pad_to $((inner - 1)) "${go_cell}   ${gh_cell}   ${aur_cell}   $(status_cell "pkg" "$ACCENT" "$AUR_PKGNAME")")${ACCENT}│${NC}"

  frame_line "${ACCENT}╰$(repeat_char '─' "$inner")╯${NC}"
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
      frame_line "${marker}${ACCENT}${DIM}${MENU_LABEL[$i]^^}${NC}"
      continue
    fi
    local badge=""
    case "${RESULT[${MENU_ACTION[$i]}]:-}" in
      ok)   badge=" ${GREEN}✓${NC}" ;;
      fail) badge=" ${RED}✗${NC}" ;;
    esac
    if [ "$i" -eq "$CURSOR" ]; then
      local hint=""
      [ -n "${MENU_HINT[$i]}" ] && hint="  ${DIM}${MENU_HINT[$i]}${NC}"
      frame_line "${marker}${ACCENT}▸${NC} ${BOLD}${MENU_LABEL[$i]}${NC}${badge}${hint}"
    else
      frame_line "${marker}  ${MENU_LABEL[$i]}${badge}"
    fi
  done
}

draw_log() {
  local height=$1
  [ "$height" -lt 1 ] && return 0
  frame_line " ${DIM}$(repeat_char '─' $((COLS - 2)))${NC}"

  if [ -n "$BUSY_MSG" ]; then
    local elapsed=""
    [ "$BUSY_START" -gt 0 ] && elapsed=" ${DIM}$(format_time $(($(date +%s) - BUSY_START)))${NC}"
    frame_line "  ${ACCENT}${SPIN_FRAMES:$SPIN_I:1}${NC} ${BUSY_MSG}…${elapsed}"
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

# draw_prompt renders the pending question on its own highlighted row, so it
# can never be mistaken for log output.
draw_prompt() {
  [ -n "$PROMPT_TEXT" ] || return 0
  frame_line " ${BOLD}${YELLOW}▶ ${PROMPT_TEXT}${NC}  ${DIM}[${PROMPT_HINT}]${NC}"
}

# draw_footer drops hints rather than letting the row be clipped, so the way
# out of the console stays visible however narrow the terminal is.
draw_footer() {
  if [ -n "$PROMPT_TEXT" ]; then
    frame_line " ${DIM}${PROMPT_KEYS}${NC}"
    return 0
  fi
  local hint=" ↑↓/jk move · enter run · d dry-run · r refresh · c clear · q quit"
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
  draw_pipeline
  # The header's rows, the tracker's, the log separator and the footer are
  # fixed overhead.
  local overhead=6
  [ "$PIPE_ACTIVE" -eq 1 ] && overhead=$((overhead + 1))
  [ -n "$PROMPT_TEXT" ] && overhead=$((overhead + 1))
  local available=$((ROWS - overhead))
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
  # Push the prompt and footer onto the last rows so they never float mid-screen.
  local tail_rows=1
  [ -n "$PROMPT_TEXT" ] && tail_rows=2
  while [ "$FRAME_ROWS" -lt $((ROWS - tail_rows)) ]; do frame_blank; done
  draw_prompt
  draw_footer
  frame_flush
}

# ── Readiness ────────────────────────────────────────────────────────────────
# Everything a release depends on, checked in one place. The console runs it
# as a menu item and at the start of a full release; --check prints it and
# exits, for a terminal-less run.

CHECK_FAILS=0
CHECK_WARNS=0
CHECK_SINK="log"   # log | stdout

check_line() {
  local level=$1 text=$2
  case "$level" in
    ok)   [ "$CHECK_SINK" = log ] && log_ok "$text"   || printf '  %s✓%s %s\n' "$GREEN" "$NC" "$text" ;;
    warn) CHECK_WARNS=$((CHECK_WARNS + 1))
          [ "$CHECK_SINK" = log ] && log_warn "$text" || printf '  %s⚠%s %s\n' "$YELLOW" "$NC" "$text" ;;
    fail) CHECK_FAILS=$((CHECK_FAILS + 1))
          [ "$CHECK_SINK" = log ] && log_err "$text"  || printf '  %s✗%s %s\n' "$RED" "$NC" "$text" ;;
  esac
}

readiness_checks() {
  CHECK_FAILS=0 CHECK_WARNS=0
  refresh_status

  if [ -n "$ST_GO" ]; then check_line ok "go $ST_GO"; else check_line fail "go is not installed"; fi
  case "$ST_GH" in
    auth)   check_line ok "gh is signed in to GitHub" ;;
    noauth) check_line fail "gh is not signed in — run 'gh auth login'" ;;
    *)      check_line fail "the GitHub CLI (gh) is not installed" ;;
  esac
  case "${ST_AUR%%:*}" in
    ok)      check_line ok "the AUR accepts your SSH key (${ST_AUR#ok:})" ;;
    denied)  check_line warn "the AUR rejected your SSH key — see 'AUR setup help'" ;;
    probing) check_line warn "the AUR key check has not answered yet" ;;
    *)       check_line warn "could not tell whether the AUR accepts your key" ;;
  esac
  if command -v makepkg >/dev/null 2>&1; then
    check_line ok "makepkg is available for the AUR's .SRCINFO"
  else
    check_line warn "makepkg is missing — the AUR step needs base-devel"
  fi
  if command -v golangci-lint >/dev/null 2>&1; then
    check_line ok "golangci-lint is available"
  else
    check_line warn "golangci-lint is missing — the lint step will be skipped"
  fi

  case "$ST_BRANCH" in
    main | master) check_line ok "on $ST_BRANCH" ;;
    *) check_line warn "on $ST_BRANCH — releases usually come from main" ;;
  esac
  if [ "${ST_DIRTY:-0}" -gt 0 ]; then
    check_line warn "$ST_DIRTY uncommitted change(s) — commit them before tagging"
  else
    check_line ok "the working tree is clean"
  fi
  if [ -z "$ST_AHEAD" ]; then
    check_line warn "$ST_BRANCH has no upstream to push to"
  elif [ "$ST_AHEAD" -gt 0 ]; then
    check_line warn "$ST_AHEAD commit(s) not pushed yet (as of the last fetch)"
  else
    check_line ok "$ST_BRANCH is pushed"
  fi

  if [ -s "$PROJECT_DIR/LICENSE" ]; then
    check_line ok "LICENSE is present — the AUR package installs it"
  else
    check_line fail "no LICENSE — the AUR package installs one from the tag"
  fi
  local workflow="$PROJECT_DIR/.github/workflows/release.yml"
  if [ ! -f "$workflow" ]; then
    check_line fail "no .github/workflows/release.yml — nothing would build the release"
  elif ! grep -q "$CHECKSUMS_ASSET" "$workflow"; then
    check_line fail "release.yml never writes $CHECKSUMS_ASSET, which the AUR step reads"
  else
    check_line ok "the release workflow publishes $CHECKSUMS_ASSET"
  fi
  if [ -f "$PROJECT_DIR/packaging/aur/PKGBUILD.in" ] && [ -f "$PROJECT_DIR/packaging/aur/render-pkgbuild.sh" ]; then
    check_line ok "AUR packaging is in packaging/aur"
  else
    check_line fail "packaging/aur is missing PKGBUILD.in or render-pkgbuild.sh"
  fi
  local entry name pkg
  for entry in "${BINARIES[@]}"; do
    name=${entry%%:*} pkg=${entry#*:}
    if [ -d "$PROJECT_DIR/${pkg#./}" ] || [ "$pkg" = "." ]; then
      check_line ok "$name builds from $pkg"
    else
      check_line fail "$name's package $pkg does not exist"
    fi
  done

  if notes_from_changelog; then
    local pending
    pending=$(changelog_section "Unreleased")
    if [ -z "$(printf '%s' "$pending" | tr -d '[:space:]')" ]; then
      check_line warn "CHANGELOG.md's Unreleased section is empty"
    else
      check_line ok "CHANGELOG.md has $(printf '%s\n' "$pending" | grep -cE '^- ') unreleased entr$([ "$(printf '%s\n' "$pending" | grep -cE '^- ')" = 1 ] && echo y || echo ies)"
    fi
  else
    check_line ok "release notes are generated by GitHub from the commits"
  fi
}

act_check() {
  log_step "Check readiness"
  CHECK_SINK=log
  readiness_checks
  if [ "$CHECK_FAILS" -gt 0 ]; then
    log_err "$CHECK_FAILS problem(s) to fix before releasing"
    return 1
  fi
  if [ "$CHECK_WARNS" -gt 0 ]; then
    log_ok "ready, with $CHECK_WARNS warning(s)"
  else
    log_ok "ready to release"
  fi
}

# ── Actions: version and notes ───────────────────────────────────────────────

act_bump() {
  log_step "Bump version"
  read_current_tag
  log_info "current tag: ${GREEN}${CURRENT_TAG}${NC}"

  local patch="" minor="" major=""
  if semver_parts "$CURRENT_TAG"; then
    patch="v${MAJ}.${MIN}.$((PAT + 1))"
    minor="v${MAJ}.$((MIN + 1)).0"
    major="v$((MAJ + 1)).0.0"
  fi

  local choice=""
  if [ -n "$patch" ]; then
    PROMPT_KEYS="p patch · m minor · M major · c custom · enter patch · esc cancel"
    while [ -z "$choice" ]; do
      ask "Release which version?" "p $patch · m $minor · M $major · c custom"
      case "$READ_KEY_VALUE" in
        p | "" | $'\n' | $'\r') choice=$patch ;;
        m) choice=$minor ;;
        M) choice=$major ;;
        c | C) choice=custom ;;
        $'\033') choice=cancel ;;
      esac
    done
    PROMPT_TEXT="" PROMPT_HINT="" PROMPT_KEYS=""
  else
    choice=custom
  fi
  case "$choice" in
    cancel) log_info "version unchanged"; return 1 ;;
    custom) tui_input "Enter version (e.g. v1.2.3)" "$patch"; choice=$TUI_INPUT_VALUE ;;
  esac

  if [[ ! "$choice" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log_err "'$choice' is not a vMAJOR.MINOR.PATCH tag"
    return 1
  fi
  # A version that already exists would re-release it; one lower than the last
  # would make the AUR go backwards. Neither is what a bump means.
  if git -C "$PROJECT_DIR" rev-parse -q --verify "refs/tags/$choice" >/dev/null; then
    log_err "$choice is already tagged — pick a new version"
    return 1
  fi
  if [ "$CURRENT_TAG" != "v0.0.0" ] && ! semver_gt "$choice" "$CURRENT_TAG"; then
    log_warn "$choice is not later than $CURRENT_TAG"
    tui_confirm "Release $choice anyway?" n || return 1
  fi
  VERSION=$choice
  log_ok "version set to ${GREEN}${VERSION}${NC}"
  refresh_status
}

# ensure_version makes VERSION usable by steps run straight from the menu:
# publishing or checking what is already released means the latest tag.
ensure_version() {
  if [ -z "$VERSION" ] || [ "$VERSION" = "v0.0.0" ]; then
    read_current_tag
    VERSION="$CURRENT_TAG"
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
  tmp=$(mktemp "${TMPDIR:-/tmp}/${BINARY_NAME}-changelog-XXXXXX")
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
  if dry_run_blocks "rewrite CHANGELOG.md, promoting Unreleased to ## $version"; then
    rm -f "$tmp"
    return 0
  fi
  cat "$tmp" > "$PROJECT_DIR/CHANGELOG.md" || { rm -f "$tmp"; return 1; }
  rm -f "$tmp"
}

changelog_preview() {
  local body count
  body=$(changelog_section "$1")
  count=$(printf '%s\n' "$body" | grep -cE '^- ')
  log_info "the release workflow will publish $count entr$([ "$count" = 1 ] && echo y || echo ies) for $1:"
  local line
  while IFS= read -r line; do
    [ -n "$line" ] && log_detail "$line"
  done < <(printf '%s\n' "$body" | grep -E '^- |^### ' | head -10)
}

# act_notes prepares the notes for VERSION in CHANGELOG.md. The release
# workflow builds its notes from the "## <tag>" section, so promoting
# Unreleased here is what makes a tagged release come out documented.
act_notes() {
  log_step "Release notes"
  if ! notes_from_changelog; then
    log_info "release notes are generated by GitHub from the commits"
    return 0
  fi
  ensure_version || { log_err "no version selected — bump the version first"; return 1; }

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
    log_info "Unreleased holds $(printf '%s\n' "$pending" | grep -cE '^- ') entries"
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
  log_result "CHANGELOG.md: Unreleased → $VERSION"
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
  [ -n "$BUILD_NOTE" ] && log_detail "$BUILD_NOTE"

  rm -rf "$DIST_DIR"
  mkdir -p "$DIST_DIR"

  local failed=0 target goos goarch label entry name pkg out
  for target in "${BUILD_TARGETS[@]}"; do
    read -r goos goarch label <<<"$target"
    for entry in "${BINARIES[@]}"; do
      name=${entry%%:*} pkg=${entry#*:}
      out="$DIST_DIR/${name}-${goos}-${label}"
      if run_quiet "building $name for $goos/$goarch" env CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
          go build "${GO_BUILD_FLAGS[@]}" -ldflags="-s -w -X ${VERSION_VAR}=${VERSION:-dev}" -o "$out" "$pkg"; then
        log_ok "$(basename "$out") ${DIM}($(du -h "$out" | cut -f1))${NC}"
      else
        log_err "build failed: $name for $goos/$goarch"
        failed=1
      fi
    done
  done
  [ "$failed" -eq 0 ] && log_ok "every target compiled"
  return "$failed"
}

# ── Actions: git ─────────────────────────────────────────────────────────────

act_commit() {
  log_step "Commit changes"
  local changes
  changes=$(git -C "$PROJECT_DIR" status --porcelain)
  if [ -z "$changes" ]; then
    log_info "nothing to commit"
    return 0
  fi
  ensure_version
  local count
  count=$(printf '%s\n' "$changes" | wc -l | tr -d ' ')
  # Everything in the tree goes into one release commit, so it is shown first:
  # an unrelated half-finished change should be noticed here, not in the tag.
  log_info "$count change(s) would go into \"chore: release $VERSION\":"
  local line shown=0
  while IFS= read -r line; do
    [ "$shown" -ge 12 ] && { log_detail "… and $((count - shown)) more"; break; }
    log_detail "$line"
    shown=$((shown + 1))
  done <<<"$changes"

  if dry_run_blocks "stage and commit $count change(s) as \"chore: release $VERSION\""; then
    return 0
  fi
  tui_confirm "Commit these $count change(s)?" y || { log_info "nothing committed"; return 1; }
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

  if mutate "commit \"chore: release $VERSION\"" git -C "$PROJECT_DIR" commit -m "chore: release $VERSION"; then
    log_result "committed: chore: release $VERSION"
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
  if mutate_streamed "push $branch to origin" git -C "$PROJECT_DIR" push origin "$branch"; then
    log_result "pushed to origin/$branch"
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
  require_gh || return 1

  # Run straight from the menu, "Tag and release" used to fall back to the
  # latest tag and offer to delete and recreate it. Re-releasing is now
  # something to say yes to, never the default.
  if [ -z "$VERSION" ]; then
    read_current_tag
    if [ "$CURRENT_TAG" = "v0.0.0" ]; then
      log_err "no version selected — run 'Bump version' first"
      return 1
    fi
    log_warn "no version was bumped this session"
    tui_confirm "Re-release the existing $CURRENT_TAG?" n || { log_info "run 'Bump version' first"; return 1; }
    VERSION=$CURRENT_TAG
  fi

  # A tag is made from the last commit, so uncommitted work would be missing
  # from the release that claims to contain it.
  if [ -n "$(git -C "$PROJECT_DIR" status --porcelain)" ]; then
    if [ "$DRY_RUN" -eq 1 ]; then
      log_warn "the working tree has uncommitted changes, which the tag would leave out"
    else
      log_err "uncommitted changes would not be in $VERSION — run 'Commit changes' first"
      return 1
    fi
  fi
  local ahead
  ahead=$(ahead_of_origin)
  if [ -n "$ahead" ] && [ "$ahead" -gt 0 ]; then
    log_warn "$ahead commit(s) on this branch are not pushed"
    log_detail "pushing the tag carries them to GitHub anyway, but the branch would lag behind it"
    if tui_confirm "Push the branch first?" y; then
      act_push || return 1
    fi
  fi

  # The release workflow builds its notes from the "## <tag>" changelog
  # section. Tagging without one publishes the boilerplate description alone.
  if notes_from_changelog && ! changelog_has_section "$VERSION"; then
    log_warn "CHANGELOG.md has no '## $VERSION' section"
    log_detail "the release workflow would publish empty notes for $VERSION"
    log_detail "run 'Release notes' first to promote Unreleased"
    if ! tui_confirm "Tag $VERSION anyway?" n; then
      return 1
    fi
  fi

  if git -C "$PROJECT_DIR" rev-parse -q --verify "refs/tags/$VERSION" >/dev/null; then
    log_warn "tag $VERSION already exists"
    if ! tui_confirm "Delete and recreate tag $VERSION?" n; then
      log_info "keeping the existing tag"
      return 1
    fi
    if ! dry_run_blocks "delete tag $VERSION locally and on origin"; then
      git -C "$PROJECT_DIR" tag -d "$VERSION" >/dev/null 2>&1
      git -C "$PROJECT_DIR" push origin --delete "$VERSION" >/dev/null 2>&1
      log_ok "old tag removed"
    fi
  fi

  # Point of no return: pushing the tag is what makes the release public and
  # starts the build. Nothing before this leaves the machine irreversibly.
  if [ "$DRY_RUN" -eq 0 ]; then
    log_warn "${BOLD}this publishes ${VERSION} to ${REPO} and cannot be undone by this script${NC}"
    if ! tui_confirm "Push tag $VERSION and publish the release?" n; then
      log_info "nothing published"
      return 1
    fi
    git -C "$PROJECT_DIR" tag -a "$VERSION" -m "Release $VERSION"
  fi
  if ! mutate_streamed "push tag $VERSION to origin, publishing the release" \
      git -C "$PROJECT_DIR" push origin "$VERSION"; then
    log_err "tag push failed"
    return 1
  fi
  log_result "tag $VERSION pushed — the release workflow is building the assets"
  if notes_from_changelog; then
    log_detail "release notes come from CHANGELOG.md's ## $VERSION section"
  else
    log_detail "release notes are generated from commit history by the workflow"
  fi
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
  local required=("$CHECKSUMS_ASSET" "$(asset_name linux x86_64)" "$(asset_name linux aarch64)")
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

  # Nothing was tagged in a dry run, so the assets are never going to appear;
  # waiting out the timeout would just stall the rehearsal.
  if dry_run_blocks "wait for the release workflow to publish $VERSION"; then
    return 0
  fi

  log_info "polling for the release workflow to publish $VERSION"
  local deadline=$(( $(date +%s) + ASSET_WAIT_SECONDS ))
  set_busy "waiting for CI to publish $VERSION"
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if release_has_all_assets "$VERSION"; then
      clear_busy
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
  clear_busy
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
  dir=$(mktemp -d "${TMPDIR:-/tmp}/${BINARY_NAME}-aur-XXXXXX")

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
  read_current_tag
  if [ "${CURRENT_TAG#v}" = "$pkgver" ]; then
    log_ok "matches the latest tag $CURRENT_TAG"
  else
    log_warn "latest tag is $CURRENT_TAG — the AUR is behind"
  fi
  log_detail "https://${AUR_HOST}/packages/${AUR_PKGNAME}"
  rm -rf "$dir"
}

# aur_render_into writes PKGBUILD and .SRCINFO for one pkgrel into an AUR
# checkout. Both are produced together so they can never disagree.
aur_render_into() {
  local dir=$1 rel=$2 x86=$3 arm=$4 lic=$5
  if ! bash "$PROJECT_DIR/packaging/aur/render-pkgbuild.sh" \
      --version "$VERSION" --pkgrel "$rel" \
      --sha256-x86_64 "$x86" --sha256-aarch64 "$arm" \
      --sha256-license "$lic" \
      --output "$dir/PKGBUILD"; then
    log_err "rendering the PKGBUILD failed"
    return 1
  fi
  if ! command -v makepkg >/dev/null 2>&1; then
    log_err "makepkg is required to generate .SRCINFO (install base-devel)"
    return 1
  fi
  if ! ( cd "$dir" && makepkg --printsrcinfo > .SRCINFO 2>/dev/null ); then
    log_err "makepkg rejected the PKGBUILD"
    return 1
  fi
  return 0
}

# sha_for_asset pulls one checksum out of the release's checksums file.
sha_for_asset() {
  local sums_file=$1 asset=$2
  awk -v want="$asset" '$2 == want || $2 == "*" want { print $1; exit }' "$sums_file"
}

# license_sha256 hashes the LICENSE blob at the tag, which is exactly what the
# PKGBUILD's raw.githubusercontent source URL serves.
license_sha256() {
  local tag=$1 tmp hash
  tmp=$(mktemp "${TMPDIR:-/tmp}/license-XXXXXX")
  # Piping git straight into sha256sum hides a missing file: the pipeline still
  # succeeds and hashes empty input, yielding e3b0c442… — a digest that looks
  # real and would publish a PKGBUILD nobody can build. Capture, check, then hash.
  if ! git -C "$PROJECT_DIR" show "${tag}:LICENSE" > "$tmp" 2>/dev/null || [ ! -s "$tmp" ]; then
    rm -f "$tmp"
    return 1
  fi
  hash=$(sha256sum "$tmp" | cut -d' ' -f1)
  rm -f "$tmp"
  printf '%s' "$hash"
}

AUR_PUBLISHED=""

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

  # 1. The checksums must come from the assets users actually download. Right
  # after a tag push the workflow is usually still uploading, so wait for them
  # rather than failing a publish that would have worked a minute later.
  if ! release_has_all_assets "$VERSION"; then
    if [ "$DRY_RUN" -eq 1 ]; then
      log_warn "would wait for $VERSION's assets, then render and push the PKGBUILD"
      return 0
    fi
    log_info "release $VERSION has no Linux assets yet — waiting for the workflow"
    if ! act_wait_assets; then
      log_err "cannot publish $AUR_PKGNAME without the release assets"
      return 1
    fi
  fi

  local work
  work=$(mktemp -d "${TMPDIR:-/tmp}/${BINARY_NAME}-aur-XXXXXX")
  if ! run_quiet "downloading $CHECKSUMS_ASSET" \
      gh release download "$VERSION" --repo "$REPO" -p "$CHECKSUMS_ASSET" -O "$work/checksums"; then
    log_err "could not download $CHECKSUMS_ASSET for $VERSION"
    rm -rf "$work"
    return 1
  fi

  local sha_x86 sha_arm sha_license x86_asset arm_asset
  x86_asset=$(asset_name linux x86_64)
  arm_asset=$(asset_name linux aarch64)
  sha_x86=$(sha_for_asset "$work/checksums" "$x86_asset")
  sha_arm=$(sha_for_asset "$work/checksums" "$arm_asset")
  sha_license=$(license_sha256 "$VERSION")

  if [ -z "$sha_x86" ] || [ -z "$sha_arm" ]; then
    log_err "$CHECKSUMS_ASSET does not list both Linux tarballs"
    rm -rf "$work"
    return 1
  fi
  if [ -z "$sha_license" ]; then
    log_err "no LICENSE at $VERSION — the tag must carry one, and be fetched locally"
    log_detail "the PKGBUILD installs the licence from $VERSION, so it has to exist there"
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

  # Start at whatever pkgrel is published for this pkgver. Bumping up front
  # would guarantee a difference, so re-running the step could never report
  # "nothing to do" and would publish a pointless release every time.
  local pkgrel=1 old_ver old_rel
  old_ver=$(aur_current_pkgver "$repo_dir" 2>/dev/null)
  old_rel=$(aur_current_pkgrel "$repo_dir" 2>/dev/null)
  if [ -n "$old_ver" ] && [ "$old_ver" = "$pkgver" ]; then
    pkgrel=${old_rel:-1}
  elif [ -n "$old_ver" ]; then
    log_info "updating $old_ver-${old_rel:-1} → ${pkgver}-1"
  fi

  # 3. Render the PKGBUILD and its .SRCINFO.
  if ! aur_render_into "$repo_dir" "$pkgrel" "$sha_x86" "$sha_arm" "$sha_license"; then
    rm -rf "$work"
    return 1
  fi

  # Unchanged against what the AUR already has: nothing to publish.
  if [ -n "$old_ver" ] && [ "$old_ver" = "$pkgver" ]; then
    git -C "$repo_dir" add -N PKGBUILD .SRCINFO 2>/dev/null
    if git -C "$repo_dir" diff --quiet -- PKGBUILD .SRCINFO 2>/dev/null; then
      log_ok "the AUR already has ${AUR_PKGNAME} ${pkgver}-${pkgrel} — nothing to publish"
      AUR_PUBLISHED="${pkgver}-${pkgrel}"
      rm -rf "$work"
      return 0
    fi
    # Something genuinely differs at the same version, so this is a new build.
    pkgrel=$((pkgrel + 1))
    log_info "same pkgver, changed package — bumping pkgrel to $pkgrel"
    if ! aur_render_into "$repo_dir" "$pkgrel" "$sha_x86" "$sha_arm" "$sha_license"; then
      rm -rf "$work"
      return 1
    fi
  fi
  log_ok "PKGBUILD rendered for ${pkgver}-${pkgrel}"

  # 4. Show what would be published before anything leaves this machine.
  log_plain ""
  log_info "${BOLD}${AUR_PKGNAME} ${pkgver}-${pkgrel}${NC} would be published as:"
  log_detail "x86_64  $x86_asset  $sha_x86"
  log_detail "aarch64 $arm_asset $sha_arm"
  log_detail "LICENSE at tag $VERSION  $sha_license"

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
  if ! mutate "commit \"$msg\" in the AUR checkout" git -C "$repo_dir" commit -m "$msg"; then
    log_err "commit failed"
    rm -rf "$work"
    return 1
  fi

  # The AUR may prompt for an SSH key passphrase, which needs the real terminal.
  if dry_run_blocks "push ${AUR_PKGNAME} ${pkgver}-${pkgrel} to $AUR_REMOTE"; then
    log_detail "rendered package kept at $repo_dir"
    return 0
  fi
  tui_suspend
  git -C "$repo_dir" push origin HEAD:master
  local push_rc=$?
  tui_resume

  if [ "$push_rc" -ne 0 ]; then
    log_err "push to the AUR failed"
    log_detail "checkout kept at $repo_dir"
    return 1
  fi
  log_result "published ${AUR_PKGNAME} ${pkgver}-${pkgrel}"
  AUR_PUBLISHED="${pkgver}-${pkgrel}"
  log_detail "https://${AUR_HOST}/packages/${AUR_PKGNAME}"
  rm -rf "$work"
  refresh_status
}

# ── Full release pipeline ────────────────────────────────────────────────────

act_full_release() {
  local start=$(date +%s)
  log_raw ""
  log_raw "  ${BOLD}${ACCENT}━━ Full release of ${APP_NAME} ━━${NC}"

  local steps=(check bump) funcs=(act_check act_bump)
  if notes_from_changelog; then steps+=(notes); funcs+=(act_notes); fi
  steps+=(test lint build commit push tag assets aur)
  funcs+=(act_test act_lint act_build act_commit act_push act_release act_wait_assets act_aur_publish)
  pipe_begin "${steps[@]}"

  local i
  AUR_PUBLISHED=""
  for ((i = 0; i < ${#steps[@]}; i++)); do
    if [ "${steps[$i]}" = "aur" ]; then
      if ! tui_confirm "Publish $AUR_PKGNAME $VERSION to the AUR now?" y; then
        pipe_set "$i" skipped
        log_info "skipped the AUR — run 'Publish $AUR_PKGNAME' when ready"
        continue
      fi
    fi
    pipe_set "$i" running
    if "${funcs[$i]}"; then
      pipe_set "$i" done
    else
      pipe_set "$i" failed
      if [ "${steps[$i]}" = "aur" ]; then
        log_err "the GitHub release is live, but the AUR publish failed"
      else
        log_err "release stopped at: ${steps[$i]}"
      fi
      return 1
    fi
  done

  local took
  took=$(format_time $(($(date +%s) - start)))
  local title=" ✓ ${APP_NAME} ${VERSION} released "
  [ "$DRY_RUN" -eq 1 ] && title=" ✓ ${APP_NAME} ${VERSION} rehearsed "
  local lines=("${DIM}github${NC}  https://github.com/${REPO}/releases/tag/${VERSION}")
  if [ -n "$AUR_PUBLISHED" ]; then
    lines+=("${DIM}aur${NC}     ${AUR_PKGNAME} ${AUR_PUBLISHED}")
  fi
  lines+=("${DIM}took${NC}    ${took}")
  [ "$DRY_RUN" -eq 1 ] && lines+=("${YELLOW}dry run — nothing was committed, pushed or published${NC}")
  log_box "$title" "$GREEN" "${lines[@]}"
}

# ── Main loop ────────────────────────────────────────────────────────────────

run_action() {
  local action=$1
  [ -n "$action" ] || return 0
  # A tracker belongs to the release that drew it; any other action clears it.
  [ "$action" = "act_full_release" ] || PIPE_ACTIVE=0
  "$action"
  local rc=$?
  if [ "$rc" -eq 0 ]; then RESULT[$action]=ok; else RESULT[$action]=fail; fi
  clear_busy
  refresh_status
  draw_frame
  return $rc
}

main() {
  parse_args "$@"
  if [ "$MODE" = "sync" ]; then
    setup_colors
    echo "Syncing the Tide release console $DEPLOY_SCRIPT_VERSION:"
    sync_into
  fi
  load_config
  setup_colors
  cd "$PROJECT_DIR" || exit 1

  if [ "$MODE" = "check" ]; then
    printf '%s release readiness\n' "$(gradient " ${APP_GLYPH} ${APP_NAME} ")"
    CHECK_SINK=stdout
    aur_probe_start
    # Give the background AUR probe a moment, so the check can say something.
    local waited=0
    while [ "$waited" -lt 8 ] && { [ ! -f "$AUR_PROBE_FILE" ] || [ ! -s "$AUR_PROBE_FILE" ]; }; do
      sleep 1
      waited=$((waited + 1))
    done
    readiness_checks
    if [ "$CHECK_FAILS" -gt 0 ]; then
      printf '\n  %s%d problem(s)%s to fix before releasing\n' "$RED" "$CHECK_FAILS" "$NC"
      exit 1
    fi
    printf '\n  %sready%s%s\n' "$GREEN" "$NC" "$([ "$CHECK_WARNS" -gt 0 ] && printf ', with %d warning(s)' "$CHECK_WARNS")"
    exit 0
  fi

  if [ ! -t 0 ] || [ ! -t 1 ]; then
    echo "deploy.sh needs an interactive terminal (or use --check)." >&2
    exit 1
  fi

  trap cleanup EXIT
  trap 'cleanup; exit 130' INT
  trap 'RESIZE_PENDING=1' WINCH

  aur_probe_start
  refresh_status
  build_menu
  tui_enter

  log_raw "  ${DIM}${APP_NAME} release console — select an action and press enter.${NC}"
  [ "$DRY_RUN" -eq 1 ] && log_warn "dry run ${BOLD}on${NC} — nothing will be committed, pushed, or published"
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
        SPIN_I=$(((SPIN_I + 1) % 10))
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
      d | D)
        if [ "$DRY_RUN" -eq 1 ]; then
          DRY_RUN=0
          log_warn "dry run ${BOLD}off${NC} — actions now commit, push, and publish for real"
        else
          DRY_RUN=1
          log_ok "dry run ${BOLD}on${NC} — nothing will be committed, pushed, or published"
        fi
        ;;
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

RESIZE_PENDING=0
main "$@"
