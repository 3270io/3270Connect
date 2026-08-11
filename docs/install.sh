#!/usr/bin/env bash
#
# 3270Connect installer — https://3270connect.3270.io
#
#   curl -fsSL https://3270connect.3270.io/install.sh | bash
#
# Installs 3270Connect one of four ways: as a native binary, as a single Docker
# container running the operations console, as a Docker Compose stack, or as
# the lab — the console, the browser terminal and a sample TN3270 host to point
# them both at. Run it with no arguments and it asks; pass --method to skip
# straight through.
#
# The script is deliberately self-contained (no dependencies beyond curl and
# coreutils) and never writes outside the directories it reports.
#
# Copyright (c) 3270.io — MIT licensed.

set -euo pipefail

# ==========================================================================
# Constants
# ==========================================================================

REPO="3270io/3270Connect"
IMAGE="ghcr.io/3270io/3270connect"
WEB_IMAGE="ghcr.io/3270io/3270web"
DOCS_URL="https://3270connect.3270.io"
API="https://api.github.com/repos/${REPO}"
CONTAINER_NAME="3270connect"

# The release publishes one Linux binary and it is amd64. So is the image: the
# s3270 the emulator drives is embedded in the program as an x86-64 ELF, so
# there is no arm64 build to steer anybody towards, and saying so early beats
# failing on the first workflow.
BINARY_ASSET="3270Connect_linux"
COMMAND_NAME="3270connect"

# ==========================================================================
# Options (overridable by flags; see usage)
# ==========================================================================

METHOD=""                 # binary | docker | compose | lab
VERSION="latest"
PORT="9200"               # the operations console
BIND="127.0.0.1"
THEME="grn"
ASSUME_YES=0
USE_COLOR="auto"
SYSTEM_INSTALL="auto"     # auto | yes | no
COMPOSE_DIR="$PWD/3270connect"
BIN_PATH=""               # resolved during the binary install
DRY_RUN=0
EXISTING_STACK=0          # 1 when this run is updating an install already here

# The lab's other two ports. Only the console's is asked about, because it is
# the one somebody is likely to already have something on.
WEB_PORT="3270"
SAMPLE_PORT="3271"
SAMPLE_PORT_2="3272"

# Which options this run was actually given.
#
# "Settings you do not pass are carried over" is only implementable if the
# script can tell a value that was typed from a default that merely looks like
# one: --port 9200 and no --port at all produce the same PORT, and only the
# first should override what an existing install is already published on.
COMPOSE_DIR_EXPLICIT=0
PORT_EXPLICIT=0
BIND_EXPLICIT=0
VERSION_EXPLICIT=0

# Where /data comes from. Less catastrophic to get wrong here than in a program
# that keeps accounts — this folder holds run history, logs and workflow output
# — but pointing a re-run somewhere else still loses the record of every run,
# so it is carried forward rather than re-decided.
DATA_MOUNT="./data"       # left side of the ":/data" mapping, as written
DATA_DIR_PATH=""          # host directory to create and chown; empty for a volume
DATA_IS_VOLUME=0          # 1 when DATA_MOUNT names a Docker volume
DATA_IS_EXTERNAL=0        # 1 when that volume was not created by this stack
PUBLISH_SPEC=""           # host side of the port mapping, e.g. 127.0.0.1:9200

# ==========================================================================
# 1. Palette
#
# The four palettes are the GRN / AMB / ICE / DAY set from 3270.io, the
# MkDocs sites and the operations console itself, so an install reads as the
# same surface as the thing it installs. Truecolor when the terminal
# advertises it, a hand-picked 256-colour approximation otherwise, plain text
# when piped or when NO_COLOR is set.
# ==========================================================================

C_ACCENT=""; C_TEXT=""; C_DIM=""; C_FAINT=""
C_OK=""; C_INFO=""; C_WARN=""; C_DANGER=""
C_BOLD=""; C_RESET=""; C_LINE=""

setup_palette() {
  local mode="none"

  if [ "$USE_COLOR" = "always" ]; then
    mode="truecolor"
  elif [ "$USE_COLOR" = "never" ] || [ -n "${NO_COLOR:-}" ]; then
    mode="none"
  elif [ ! -t 1 ] || [ "${TERM:-dumb}" = "dumb" ]; then
    mode="none"
  elif [ "${COLORTERM:-}" = "truecolor" ] || [ "${COLORTERM:-}" = "24bit" ]; then
    mode="truecolor"
  else
    mode="256"
  fi

  [ "$mode" = "none" ] && return 0

  C_BOLD=$'\033[1m'
  C_RESET=$'\033[0m'

  local rgb_accent rgb_text rgb_dim rgb_faint rgb_ok rgb_info rgb_warn rgb_danger rgb_line
  local i_accent i_text i_dim i_faint i_ok i_info i_warn i_danger i_line

  case "$THEME" in
    amb|amber)
      rgb_accent="255;184;77";  i_accent=215
      rgb_text="255;243;220";   i_text=230
      rgb_dim="233;196;138";    i_dim=180
      rgb_faint="163;129;76";   i_faint=137
      rgb_ok="143;227;136";     i_ok=114
      rgb_info="124;200;255";   i_info=117
      rgb_warn="255;209;102";   i_warn=221
      rgb_danger="255;122;107"; i_danger=209
      rgb_line="122;83;24";     i_line=94
      ;;
    ice)
      rgb_accent="90;210;255";  i_accent=81
      rgb_text="234;245;255";   i_text=195
      rgb_dim="167;201;232";    i_dim=152
      rgb_faint="107;139;171";  i_faint=103
      rgb_ok="78;233;176";      i_ok=86
      rgb_info="90;210;255";    i_info=81
      rgb_warn="255;204;112";   i_warn=221
      rgb_danger="255;122;144"; i_danger=210
      rgb_line="42;80;118";     i_line=60
      ;;
    day|daylight)
      # The light palette. Only sensible on a light terminal background —
      # documented as such rather than silently guessing.
      rgb_accent="0;135;90";    i_accent=29
      rgb_text="8;32;25";       i_text=235
      rgb_dim="61;92;81";       i_dim=239
      rgb_faint="109;133;121";  i_faint=245
      rgb_ok="18;128;90";       i_ok=29
      rgb_info="11;111;164";    i_info=25
      rgb_warn="161;98;7";      i_warn=136
      rgb_danger="192;57;43";   i_danger=160
      rgb_line="164;186;176";   i_line=250
      ;;
    *)  # grn / phosphor — the default
      rgb_accent="78;255;179";  i_accent=86
      rgb_text="230;255;245";   i_text=195
      rgb_dim="159;230;200";    i_dim=151
      rgb_faint="95;158;134";   i_faint=72
      rgb_ok="78;255;179";      i_ok=86
      rgb_info="90;210;255";    i_info=81
      rgb_warn="247;195;107";   i_warn=215
      rgb_danger="255;111;130"; i_danger=204
      rgb_line="29;77;61";      i_line=22
      ;;
  esac

  if [ "$mode" = "truecolor" ]; then
    C_ACCENT=$'\033[38;2;'"${rgb_accent}"'m'
    C_TEXT=$'\033[38;2;'"${rgb_text}"'m'
    C_DIM=$'\033[38;2;'"${rgb_dim}"'m'
    C_FAINT=$'\033[38;2;'"${rgb_faint}"'m'
    C_OK=$'\033[38;2;'"${rgb_ok}"'m'
    C_INFO=$'\033[38;2;'"${rgb_info}"'m'
    C_WARN=$'\033[38;2;'"${rgb_warn}"'m'
    C_DANGER=$'\033[38;2;'"${rgb_danger}"'m'
    C_LINE=$'\033[38;2;'"${rgb_line}"'m'
  else
    C_ACCENT=$'\033[38;5;'"${i_accent}"'m'
    C_TEXT=$'\033[38;5;'"${i_text}"'m'
    C_DIM=$'\033[38;5;'"${i_dim}"'m'
    C_FAINT=$'\033[38;5;'"${i_faint}"'m'
    C_OK=$'\033[38;5;'"${i_ok}"'m'
    C_INFO=$'\033[38;5;'"${i_info}"'m'
    C_WARN=$'\033[38;5;'"${i_warn}"'m'
    C_DANGER=$'\033[38;5;'"${i_danger}"'m'
    C_LINE=$'\033[38;5;'"${i_line}"'m'
  fi
}

# ==========================================================================
# 2. Drawing primitives
#
# The vocabulary is the one on the docs home page: a terminal panel with a
# live dot in its head bar, `›` log lines with an aligned label, a value and
# a bracketed status tag, and mono uppercase eyebrows over each section.
# ==========================================================================

WIDTH=72
GLYPH_DOT="●"; GLYPH_ARROW="›"; GLYPH_BAR="▌"; GLYPH_TICK="✓"; GLYPH_CROSS="✕"
GLYPH_SEP="·"
BOX_TL="╭"; BOX_TR="╮"; BOX_BL="╰"; BOX_BR="╯"; BOX_H="─"; BOX_V="│"

setup_geometry() {
  local cols
  cols="${COLUMNS:-0}"
  if [ "$cols" -le 0 ] 2>/dev/null; then
    cols="$( (tput cols) 2>/dev/null || echo 80)"
  fi
  [ "$cols" -le 0 ] 2>/dev/null && cols=80
  WIDTH=$((cols - 4))
  [ "$WIDTH" -gt 84 ] && WIDTH=84
  [ "$WIDTH" -lt 52 ] && WIDTH=52

  # Fall back to ASCII when the locale cannot promise UTF-8, so the layout
  # degrades to something square rather than to mojibake.
  case "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" in
    *UTF-8*|*utf8*|*UTF8*|*utf-8*) : ;;
    *)
      GLYPH_DOT="*"; GLYPH_ARROW=">"; GLYPH_BAR="|"; GLYPH_TICK="+"; GLYPH_CROSS="x"
      GLYPH_SEP="-"
      BOX_TL="+"; BOX_TR="+"; BOX_BL="+"; BOX_BR="+"; BOX_H="-"; BOX_V="|"
      ;;
  esac
}

repeat() { # repeat <string> <count>
  local s="$1" n="$2" out=""
  while [ "$n" -gt 0 ]; do out="${out}${s}"; n=$((n - 1)); done
  printf '%s' "$out"
}

# Absolute paths are what the script acts on, but they wrap the card layout
# on a normal terminal. Display them the way a shell prompt would.
short_path() { # short_path <path>
  local p="$1"
  case "$p" in
    "$PWD"/*) printf '.%s' "${p#"$PWD"}"; return 0 ;;
    "$PWD")   printf '.';                 return 0 ;;
  esac
  case "$p" in
    "$HOME"/*) printf '~%s' "${p#"$HOME"}"; return 0 ;;
  esac
  printf '%s' "$p"
}

# Panel head bar: `╭─ ● left ──────── right ─╮` on one line, mirroring the
# .term-head component (live dot, label, right-aligned meta).
panel() { # panel <left> <right>
  local left="$1" right="${2:-}"
  local inner=$((WIDTH - 2))
  local text="  ${GLYPH_DOT} ${left}"
  local pad=$((inner - ${#text} - ${#right} - 2))
  [ "$pad" -lt 1 ] && pad=1
  printf '%s%s%s%s\n' \
    "$C_LINE" "$BOX_TL" "$(repeat "$BOX_H" "$inner")" "${BOX_TR}${C_RESET}"
  printf '%s%s%s  %s%s %s%s%s%s%s%s  %s%s%s\n' \
    "$C_LINE" "$BOX_V" "$C_RESET" \
    "$C_OK" "$GLYPH_DOT" \
    "$C_BOLD$C_TEXT" "$left" "$C_RESET" \
    "$(repeat ' ' "$pad")" \
    "$C_FAINT" "$right" \
    "$C_RESET$C_LINE" "$BOX_V" "$C_RESET"
  printf '%s%s%s%s\n' \
    "$C_LINE" "$BOX_BL" "$(repeat "$BOX_H" "$inner")" "${BOX_BR}${C_RESET}"
}

eyebrow() { # eyebrow <TITLE>
  printf '\n  %s%s%s %s%s%s\n\n' \
    "$C_ACCENT" "$GLYPH_BAR" "$C_RESET" "$C_DIM$C_BOLD" "$1" "$C_RESET"
}

rule() {
  printf '  %s%s%s\n' "$C_LINE" "$(repeat "$BOX_H" $((WIDTH - 2)))" "$C_RESET"
}

# `› label      value      [tag]` — the docs-home log line.
step() { # step <label> <value> [tag] [tagcolor]
  local label="$1" value="${2:-}" tag="${3:-}" tagcolor="${4:-$C_OK}"
  local pad=$((11 - ${#label}))
  [ "$pad" -lt 1 ] && pad=1
  printf '  %s%s%s %s%s%s%s%s%s' \
    "$C_ACCENT" "$GLYPH_ARROW" "$C_RESET" \
    "$C_DIM" "$label" "$C_RESET" "$(repeat ' ' "$pad")" "$C_TEXT" "$value"
  if [ -n "$tag" ]; then
    printf '  %s[%s]%s' "$tagcolor" "$tag" "$C_RESET"
  fi
  printf '%s\n' "$C_RESET"
}

note()  { printf '  %s%s%s\n' "$C_FAINT" "$1" "$C_RESET"; }
info()  { printf '  %s%s%s %s\n' "$C_INFO" "$GLYPH_ARROW" "$C_RESET" "$1"; }
good()  { printf '  %s%s%s %s%s%s\n' "$C_OK" "$GLYPH_TICK" "$C_RESET" "$C_TEXT" "$1" "$C_RESET"; }
warn()  { printf '  %s!%s %s%s%s\n' "$C_WARN" "$C_RESET" "$C_DIM" "$1" "$C_RESET" >&2; }

die() {
  printf '\n  %s%s%s %s%s%s\n\n' \
    "$C_DANGER" "$GLYPH_CROSS" "$C_RESET" "$C_TEXT" "$1" "$C_RESET" >&2
  exit "${2:-1}"
}

banner() {
  printf '\n'
  panel "3270Connect installer" "$(uname -s | tr '[:upper:]' '[:lower:]') ${GLYPH_SEP} $(uname -m)"
  printf '\n  %s%sMAINFRAME WORK, ON A SCHEDULE%s\n' "$C_BOLD" "$C_ACCENT" "$C_RESET"
  printf '  %sRecord a 3270 session once. Replay it a thousand times.%s\n\n' "$C_FAINT" "$C_RESET"
}

# A spinner that stays out of the way: only on a TTY, always cleaned up.
SPIN_PID=""
spin_start() { # spin_start <message>
  [ -t 1 ] || { printf '  %s%s%s %s...\n' "$C_ACCENT" "$GLYPH_ARROW" "$C_RESET" "$1"; return 0; }
  local msg="$1"
  (
    local frames='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏' i=0 n
    case "$GLYPH_DOT" in '*') frames='|/-\' ;; esac
    n=$(printf '%s' "$frames" | wc -m | tr -d ' ')
    while :; do
      i=$(((i % n) + 1))
      printf '\r  %s%s%s %s%s%s ' \
        "$C_ACCENT" "$(printf '%s' "$frames" | cut -c "$i" 2>/dev/null || printf '%s' "$frames" | head -c1)" \
        "$C_RESET" "$C_DIM" "$msg" "$C_RESET"
      sleep 0.1
    done
  ) 2>/dev/null &
  SPIN_PID=$!
}

spin_stop() { # spin_stop <label> <value> [tag]
  if [ -n "$SPIN_PID" ]; then
    kill "$SPIN_PID" 2>/dev/null || true
    wait "$SPIN_PID" 2>/dev/null || true
    SPIN_PID=""
    [ -t 1 ] && printf '\r%s\r' "$(repeat ' ' "$((WIDTH + 6))")"
  fi
  if [ "$#" -gt 0 ]; then
    step "$@"
  fi
  # Always succeed: stopping the spinner with no label is what every failure
  # path does, right before explaining itself, and under `set -e` a non-zero
  # return there would end the script before the explanation printed.
  return 0
}

cleanup() {
  local code=$?
  if [ -n "$SPIN_PID" ]; then
    kill "$SPIN_PID" 2>/dev/null || true
    SPIN_PID=""
    [ -t 1 ] && printf '\r%s\r' "$(repeat ' ' "$((WIDTH + 6))")"
  fi
  if [ "$code" -ne 0 ] && [ "$code" -ne 130 ]; then
    printf '\n  %s%s installation did not complete.%s\n' "$C_DANGER" "$GLYPH_CROSS" "$C_RESET" >&2
    printf '  %sTroubleshooting: %s/installation/%s\n\n' "$C_FAINT" "$DOCS_URL" "$C_RESET" >&2
  fi
}
trap cleanup EXIT
trap 'exit 130' INT

# ==========================================================================
# 3. Prompting
#
# Piped into bash, stdin is the script itself — every prompt must come from
# the controlling terminal or not at all.
# ==========================================================================

TTY_OK=0
setup_tty() {
  if [ -r /dev/tty ] && [ -t 1 ]; then TTY_OK=1; else TTY_OK=0; fi
}

ask() { # ask <prompt> <default>  → echoes the answer
  local prompt="$1" default="$2" reply=""
  if [ "$TTY_OK" -eq 0 ] || [ "$ASSUME_YES" -eq 1 ]; then
    printf '%s' "$default"
    return 0
  fi
  printf '  %s%s%s %s %s[%s]%s ' \
    "$C_ACCENT" "$GLYPH_ARROW" "$C_RESET" "$prompt" "$C_FAINT" "$default" "$C_RESET" >&2
  IFS= read -r reply < /dev/tty || reply=""
  [ -z "$reply" ] && reply="$default"
  printf '%s' "$reply"
}

confirm() { # confirm <question>  → 0 yes / 1 no
  local answer
  [ "$ASSUME_YES" -eq 1 ] && return 0
  [ "$TTY_OK" -eq 0 ] && return 0
  answer="$(ask "$1 (y/n)" "y")"
  case "$answer" in [yY]|[yY][eE][sS]) return 0 ;; *) return 1 ;; esac
}

# ==========================================================================
# 4. Environment probing
# ==========================================================================

have() { command -v "$1" >/dev/null 2>&1; }

ARCH=""
detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)             ARCH="$(uname -m)" ;;
  esac
}

SUDO=""
resolve_sudo() { # resolve_sudo <dir> → sets SUDO to "" or "sudo"
  local dir="$1"
  SUDO=""
  [ "$(id -u)" -eq 0 ] && return 0
  if [ -w "$dir" ]; then return 0; fi
  if have sudo; then SUDO="sudo"; return 0; fi
  return 1
}

DOCKER=""
DOCKER_COMPOSE=""
detect_docker() {
  DOCKER=""; DOCKER_COMPOSE=""
  have docker || return 0
  # `docker` on PATH is not the same as a reachable daemon.
  if docker info >/dev/null 2>&1 </dev/null; then
    DOCKER="docker"
  elif have sudo && sudo -n docker info >/dev/null 2>&1 </dev/null; then
    DOCKER="sudo docker"
  else
    DOCKER="docker"   # present but the daemon check failed; reported later
  fi
  # Compose has to talk to the daemon the same way `docker` does.
  #
  # `docker compose version` prints a version without going near the socket, so
  # it succeeds for a user who is not in the docker group -- and the plain
  # "docker compose" that check blessed then fails at `up -d` with a permission
  # error on /var/run/docker.sock, on a host where every other step worked
  # through sudo. Carry the prefix that was actually resolved above.
  local prefix=""
  case "$DOCKER" in
    "sudo "*) prefix="sudo " ;;
  esac
  if docker compose version >/dev/null 2>&1 </dev/null; then
    DOCKER_COMPOSE="${prefix}docker compose"
  elif have docker-compose; then
    DOCKER_COMPOSE="${prefix}docker-compose"
  fi
}

docker_ready() {
  [ -n "$DOCKER" ] || return 1
  $DOCKER info >/dev/null 2>&1 </dev/null
}

port_in_use() { # port_in_use <port>
  local port="$1"
  if have ss; then
    ss -ltn 2>/dev/null | grep -qE "[:.]${port}[[:space:]]" && return 0
  elif have netstat; then
    netstat -ltn 2>/dev/null | grep -qE "[:.]${port}[[:space:]]" && return 0
  fi
  return 1
}

# ==========================================================================
# 5. Release metadata
# ==========================================================================

RESOLVED_VERSION=""
DOWNLOAD_URL=""
EXPECTED_SHA=""

resolve_release() {
  local url json
  if [ "$VERSION" = "latest" ]; then
    url="${API}/releases/latest"
  else
    url="${API}/releases/tags/${VERSION}"
  fi

  json="$(curl -fsSL -H 'Accept: application/vnd.github+json' "$url" 2>/dev/null </dev/null || true)"

  if [ -n "$json" ]; then
    RESOLVED_VERSION="$(printf '%s' "$json" \
      | tr ',' '\n' | grep -m1 '"tag_name"' \
      | sed -e 's/.*"tag_name" *: *"//' -e 's/".*//' || true)"
    # The digest sits in the same object as the asset name, so walk the
    # asset list rather than grepping the whole document for a sha.
    EXPECTED_SHA="$(printf '%s' "$json" \
      | tr '{' '\n' \
      | grep '"name" *: *"'"${BINARY_ASSET}"'"' \
      | grep -m1 -o '"digest" *: *"sha256:[0-9a-f]*"' \
      | sed -e 's/.*sha256://' -e 's/"//' || true)"
  fi

  [ -z "$RESOLVED_VERSION" ] && RESOLVED_VERSION="$VERSION"

  if [ "$VERSION" = "latest" ]; then
    DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${BINARY_ASSET}"
  else
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY_ASSET}"
  fi
}

# ==========================================================================
# 6. Method: native binary
#
# 3270Connect keeps its run history in the user's config directory rather than
# beside the executable, so the binary is a single file on PATH and nothing
# else has to be arranged around it.
# ==========================================================================

resolve_binary_paths() {
  local system="$SYSTEM_INSTALL"
  if [ "$system" = "auto" ]; then
    if [ "$(id -u)" -eq 0 ]; then system="yes"; else system="no"; fi
  fi

  if [ "$system" = "yes" ]; then
    BIN_PATH="/usr/local/bin/${COMMAND_NAME}"
  else
    BIN_PATH="${HOME}/.local/bin/${COMMAND_NAME}"
  fi
}

install_binary() {
  eyebrow "INSTALL ${GLYPH_SEP} NATIVE BINARY"

  have curl || die "curl is required to download the release."

  if [ "$ARCH" != "amd64" ]; then
    warn "The published Linux binary is amd64 only; this host is ${ARCH}."
    note "The s3270 it drives is embedded as an x86-64 binary, so there is no"
    note "${ARCH} build to fall back to — not even the container image. Build"
    note "x3270 for this architecture and build from source:"
    note "  ${DOCS_URL}/installation/"
    die "No ${ARCH} build is published."
  fi

  resolve_binary_paths

  step "version" "$RESOLVED_VERSION"
  step "command" "$(short_path "$BIN_PATH")"
  step "state" "~/.config/3270Connect/  (run history the console reads)"
  printf '\n'

  if ! confirm "Install 3270Connect here?"; then
    die "Cancelled." 130
  fi
  printf '\n'

  if [ "$DRY_RUN" -eq 1 ]; then
    step "dry run" "nothing written" "skip" "$C_WARN"
    return 0
  fi

  local parent_bin
  parent_bin="$(dirname "$BIN_PATH")"

  resolve_sudo "$parent_bin" \
    || die "Cannot write to ${parent_bin} and sudo is unavailable. Re-run with --user."
  $SUDO mkdir -p "$parent_bin"

  local upgrade=0
  [ -e "$BIN_PATH" ] && upgrade=1

  local tmp
  tmp="$(mktemp -d "${TMPDIR:-/tmp}/3270connect-install.XXXXXX")"
  # shellcheck disable=SC2064  # $tmp is intentionally expanded now, not later
  trap "rm -rf '$tmp'; cleanup" EXIT

  spin_start "Downloading ${BINARY_ASSET} ${RESOLVED_VERSION}"
  if ! curl -fsSL --retry 3 --retry-delay 2 -o "${tmp}/${BINARY_ASSET}" "$DOWNLOAD_URL" </dev/null; then
    spin_stop
    die "Download failed: ${DOWNLOAD_URL}"
  fi
  spin_stop "download" "$(du -h "${tmp}/${BINARY_ASSET}" | cut -f1) from GitHub releases" "ok"

  if [ -n "$EXPECTED_SHA" ] && have sha256sum; then
    local actual
    actual="$(sha256sum "${tmp}/${BINARY_ASSET}" | cut -d' ' -f1)"
    if [ "$actual" != "$EXPECTED_SHA" ]; then
      die "Checksum mismatch — refusing to install.
     expected ${EXPECTED_SHA}
     actual   ${actual}"
    fi
    step "checksum" "sha256 verified" "ok"
  else
    step "checksum" "not published for this release" "skip" "$C_WARN"
  fi

  chmod +x "${tmp}/${BINARY_ASSET}"
  # Staged beside the target and renamed into place. Copying straight over it
  # fails with ETXTBSY when an upgrade runs while a load test is still going;
  # rename only swaps the directory entry, so a running process keeps its old
  # inode and the next start picks up the new build.
  $SUDO cp "${tmp}/${BINARY_ASSET}" "${BIN_PATH}.new"
  $SUDO chmod 0755 "${BIN_PATH}.new"
  $SUDO mv -f "${BIN_PATH}.new" "$BIN_PATH"
  step "installed" "$(short_path "$BIN_PATH")" "ok"

  if [ "$upgrade" -eq 1 ]; then
    step "upgraded" "restart anything still running the old build" "note" "$C_INFO"
  fi

  rm -rf "$tmp"
  trap cleanup EXIT

  step "s3270" "bundled in the binary" "ok"

  case ":${PATH}:" in
    *":${parent_bin}:"*) : ;;
    *)
      printf '\n'
      warn "${parent_bin} is not on your PATH."
      note "Add it for this shell:  export PATH=\"${parent_bin}:\$PATH\""
      ;;
  esac

  success_binary
}

success_binary() {
  printf '\n'
  panel "3270Connect installed" "${RESOLVED_VERSION} ${GLYPH_SEP} binary"
  printf '\n'
  step "console" "${COMMAND_NAME} -dashboard"
  step "open" "http://localhost:${PORT}/dashboard"
  step "run" "${COMMAND_NAME} -config workflow.json"
  step "load" "${COMMAND_NAME} -config workflow.json -concurrent 25 -runtime 600"
  step "try it" "${COMMAND_NAME} -runApp 1 -runApp-port 3270   (a 3270 host to aim at)"
  step "docs" "${DOCS_URL}/installation/"
  printf '\n'
}

# ==========================================================================
# 7. The data folder
# ==========================================================================

# The uid the container runs as. A bind-mounted host folder keeps the host's
# ownership — Docker adjusts a named volume's, but not a bind mount's — so the
# folder has to be handed to this user or the console cannot write to it.
readonly RUNTIME_UID=10001
readonly RUNTIME_GID=10001

# normalize_path <path> — an absolute path with any "." or ".." components
# resolved, so two spellings of one folder compare equal. Anything that is not
# an absolute path (a volume name, a path whose parent does not exist yet) is
# returned untouched.
normalize_path() {
  local p="$1" dir base
  case "$p" in
    /*) : ;;
    *)  printf '%s' "$p"; return 0 ;;
  esac
  dir="$(dirname "$p")"
  base="$(basename "$p")"
  if [ -d "$dir" ]; then
    printf '%s/%s' "$(cd "$dir" && pwd)" "$base"
  else
    printf '%s' "$p"
  fi
}

# resolve_data_mount — work out what DATA_MOUNT means on this host.
#
# A bind mount needs a directory created and handed to the runtime user; a
# named volume needs neither, and must not have a directory of that name
# invented for it beside the stack file.
resolve_data_mount() {
  DATA_IS_VOLUME=0
  case "$DATA_MOUNT" in
    /*)        DATA_DIR_PATH="$DATA_MOUNT" ;;
    "~"/*)     DATA_DIR_PATH="${HOME}/${DATA_MOUNT#"~"/}" ;;
    ./*|../*)  DATA_DIR_PATH="${COMPOSE_DIR}/${DATA_MOUNT#./}" ;;
    *)         DATA_IS_VOLUME=1; DATA_DIR_PATH="" ;;
  esac
}

# prepare_data_dir <dir> — create it, hand it to the runtime user, and say so.
prepare_data_dir() {
  local dir="$1"
  local existed=0
  [ -d "$dir" ] && existed=1

  mkdir -p "$dir" || die "Could not create ${dir}."

  # chown needs root unless the folder is already the right owner, which it is
  # on a re-run. Checked rather than assumed so a second install is quiet.
  local owner
  owner="$(stat -c '%u:%g' "$dir" 2>/dev/null || echo '')"
  if [ "$owner" != "${RUNTIME_UID}:${RUNTIME_GID}" ]; then
    local sudo_cmd=""
    [ "$(id -u)" -ne 0 ] && have sudo && sudo_cmd="sudo"
    if ! $sudo_cmd chown -R "${RUNTIME_UID}:${RUNTIME_GID}" "$dir" 2>/dev/null; then
      warn "Could not give ${dir} to uid ${RUNTIME_UID}."
      note "3270Connect runs unprivileged and cannot record runs without it. Run:"
      note "  sudo chown -R ${RUNTIME_UID}:${RUNTIME_GID} ${dir}"
      return 1
    fi
  fi

  if [ "$existed" -eq 1 ]; then
    step "data" "${dir} (kept)" "ok"
  else
    step "data" "${dir} (uid ${RUNTIME_UID})" "new"
  fi
  return 0
}

# ==========================================================================
# 8. Re-running over an install that is already here
# ==========================================================================

# read_data_mount <compose-file> — the left side of the ":/data" mapping.
#
# Scanned inside volumes: blocks only, and by string rather than by regex, so
# an absolute host path, a relative one and a named volume all come back as
# written.
read_data_mount() {
  [ -r "$1" ] || return 0
  awk '
    /^[[:space:]]*volumes:/ { invol = 1; next }
    invol && /^[[:space:]]*-/ {
      line = $0
      sub(/^[[:space:]]*-[[:space:]]*/, "", line)
      sub(/[[:space:]]*$/, "", line)
      gsub(/"/, "", line)
      gsub(/\x27/, "", line)
      n = index(line, ":/data")
      if (n > 0) {
        rest = substr(line, n)
        if (rest == ":/data" || substr(rest, 1, 7) == ":/data:") {
          print substr(line, 1, n - 1)
          exit
        }
      }
    }
  ' "$1"
}

# read_published <compose-file> — the host side of the port mapping, e.g.
# "127.0.0.1:9200" or "9200". The container side is always rewritten to 9200,
# so only the part somebody chose is carried forward.
read_published() {
  [ -r "$1" ] || return 0
  awk '
    /^[[:space:]]*ports:/ { inports = 1; next }
    inports && /^[[:space:]]*-/ {
      line = $0
      sub(/^[[:space:]]*-[[:space:]]*/, "", line)
      sub(/[[:space:]]*$/, "", line)
      gsub(/"/, "", line)
      gsub(/\x27/, "", line)
      if (line ~ /:[0-9]+$/) {
        sub(/:[0-9]+$/, "", line)
        print line
        exit
      }
    }
    inports && /^[[:space:]]*[A-Za-z_]+:/ { inports = 0 }
  ' "$1"
}

# read_image_tag <compose-file> — the tag an existing stack is pinned to.
read_image_tag() {
  [ -r "$1" ] || return 0
  sed -n "s|^[[:space:]]*image:[[:space:]]*${IMAGE}:\\([^[:space:]]*\\)[[:space:]]*\$|\\1|p" "$1" | tail -1
}

# apply_published <spec> — split a host-side mapping into BIND and PORT.
apply_published() {
  local spec="$1"
  [ -n "$spec" ] || return 0
  PUBLISH_SPEC="$spec"
  case "$spec" in
    *:*) BIND="${spec%:*}"; PORT="${spec##*:}" ;;
    *)   BIND=""; PORT="$spec" ;;
  esac
}

# container_label <label> — a label on the existing container, if there is one.
container_label() {
  [ -n "$DOCKER" ] || return 0
  local value
  value="$($DOCKER inspect --format "{{index .Config.Labels \"$1\"}}" "$CONTAINER_NAME" 2>/dev/null </dev/null || true)"
  case "$value" in
    "<no value>") value="" ;;
  esac
  printf '%s' "$value"
}

# container_data_mount — where the existing container gets /data from: a volume
# name, or the host path of a bind mount.
container_data_mount() {
  [ -n "$DOCKER" ] || return 0
  $DOCKER inspect --format \
    '{{range .Mounts}}{{if eq .Destination "/data"}}{{if eq .Type "volume"}}{{.Name}}{{else}}{{.Source}}{{end}}{{end}}{{end}}' \
    "$CONTAINER_NAME" 2>/dev/null </dev/null || true
}

# container_published — the host side of the existing container's mapping.
container_published() {
  [ -n "$DOCKER" ] || return 0
  local spec
  spec="$($DOCKER inspect --format \
    '{{range $p, $c := .HostConfig.PortBindings}}{{range $c}}{{.HostIp}}:{{.HostPort}}{{end}}{{end}}' \
    "$CONTAINER_NAME" 2>/dev/null </dev/null || true)"
  # An unset HostIp means every interface; keep it that way rather than
  # inventing a loopback bind an existing install did not ask for.
  case "$spec" in
    :*) spec="${spec#:}" ;;
  esac
  printf '%s' "$spec"
}

container_exists() {
  [ -n "$DOCKER" ] || return 1
  $DOCKER inspect "$CONTAINER_NAME" >/dev/null 2>&1 </dev/null
}

# locate_existing_stack — the directory of the Compose project already running
# 3270Connect, whether or not this run was started anywhere near it.
locate_existing_stack() {
  local dir
  dir="$(container_label com.docker.compose.project.working_dir)"
  [ -n "$dir" ] || return 0
  [ -e "${dir}/docker-compose.yml" ] || return 0
  printf '%s' "$dir"
}

# resolve_compose_dir — decide which directory this run updates.
#
# The installer defaults its project directory to ./3270connect *relative to
# the current directory*, and Compose derives its project name from that
# directory's basename. Re-running from somewhere else — which is the normal
# thing to do when the command came from a docs page — would otherwise land on
# a second, empty ./3270connect that Compose still considers the same project:
# it recreates the running container against a brand new data folder, and the
# console comes back up with no history of any run.
resolve_compose_dir() {
  local existing
  existing="$(locate_existing_stack)"
  [ -n "$existing" ] || return 0
  [ "$existing" = "$COMPOSE_DIR" ] && return 0

  if [ "$COMPOSE_DIR_EXPLICIT" -eq 0 ]; then
    COMPOSE_DIR="$existing"
    step "found" "an install already running from ${existing}" "ok"
    note "Updating that one. Pass --dir to work on a different stack."
    return 0
  fi

  warn "3270Connect is already installed at ${existing}."
  note "Bringing up a second stack in ${COMPOSE_DIR} would not run alongside"
  note "it. Compose recreates the container that is already running, against"
  note "this directory's data folder, and the run history in ${existing}/data"
  note "would be left behind on a console that no longer reads it."
  note ""
  note "To update the install that is here:"
  note "  --dir ${existing}"
  note "To move it, stop it and take its data folder with you:"
  note "  cd ${existing} && ${DOCKER_COMPOSE:-docker compose} down"
  note "  cp -a ${existing}/data ${COMPOSE_DIR}/data"
  die "Refusing to recreate the running instance against an empty data folder."
}

# adopt_existing <compose-file> — carry a previous run's choices forward, so
# re-running the installer updates an install rather than quietly
# reconfiguring it. Anything given explicitly on the command line still wins.
adopt_existing() {
  local file="$1"
  local from_file=0
  [ -e "$file" ] && from_file=1
  if [ "$from_file" -eq 0 ] && ! container_exists; then
    return 0
  fi
  EXISTING_STACK=1

  local previous_data previous_publish previous_tag
  previous_data=""
  previous_publish=""
  previous_tag=""

  if [ "$from_file" -eq 1 ]; then
    previous_data="$(read_data_mount "$file")"
    previous_publish="$(read_published "$file")"
    previous_tag="$(read_image_tag "$file")"
  fi

  # The running container is the fallback, and for an install made with
  # --method docker it is the only record there is. It is also the truth when
  # the stack file has drifted from what is actually running.
  [ -z "$previous_publish" ] && previous_publish="$(container_published)"
  if [ -z "$previous_data" ]; then
    previous_data="$(container_data_mount)"
    # A volume named by the container was not created by this stack file, so
    # Compose must be told to use that exact volume rather than create a
    # project-prefixed one of its own — which would come up empty.
    case "$previous_data" in
      ""|/*) : ;;
      *)     DATA_IS_EXTERNAL=1 ;;
    esac
  fi

  if [ -n "$previous_data" ] && [ "$previous_data" != "$DATA_MOUNT" ]; then
    DATA_MOUNT="$previous_data"
    step "kept" "the data folder this install already uses: ${DATA_MOUNT}" "ok"
  fi

  if [ "$PORT_EXPLICIT" -eq 0 ] && [ "$BIND_EXPLICIT" -eq 0 ] && [ -n "$previous_publish" ]; then
    apply_published "$previous_publish"
  fi

  if [ "$VERSION_EXPLICIT" -eq 0 ] && [ -n "$previous_tag" ] && [ "$previous_tag" != "latest" ]; then
    VERSION="$previous_tag"
    step "kept" "the version this install is pinned to: ${previous_tag}" "ok"
  fi
}

# assert_data_preserved — stop before a start that would read a different
# folder from the one holding this install's run history.
assert_data_preserved() {
  local running
  running="$(container_data_mount)"
  [ -n "$running" ] || return 0

  # A relative mount in a stack file and an absolute bind source on a container
  # are routinely the same folder, so compare what they resolve to rather than
  # how they are spelled. A volume name resolves to itself.
  local wanted="$DATA_MOUNT"
  [ "$DATA_IS_VOLUME" -eq 0 ] && [ -n "$DATA_DIR_PATH" ] && wanted="$DATA_DIR_PATH"
  [ "$(normalize_path "$running")" = "$(normalize_path "$wanted")" ] && return 0

  warn "This would change where 3270Connect keeps its runs."
  note "running now:  ${running}"
  note "would become: ${DATA_MOUNT}"
  note ""
  note "The run history, the console's logs and any workflow output are in the"
  note "first one. Starting against the second brings the console up empty."
  note ""
  note "Point this run at the folder that is in use, or copy it across first."
  die "Refusing to restart 3270Connect against a different data folder."
}

# The security note every published-port method prints. The console has no
# sign-in of its own, so where it is published is the whole of the decision.
step_exposure() {
  if [ -z "$BIND" ] || [ "$BIND" = "0.0.0.0" ]; then
    step "reachable" "from the network ${GLYPH_SEP} no sign-in" "open" "$C_WARN"
  else
    step "reachable" "from ${BIND} only" "ok"
  fi
}

# publish_entry <host port> <container port> — one "ports:" value for the bind
# this run resolved. An empty or all-interfaces bind is written as the bare
# "9200:9200" form, because ":9200:9200" is a spelling of the same thing that
# not every Compose version parses.
publish_entry() {
  if [ -z "$BIND" ] || [ "$BIND" = "0.0.0.0" ]; then
    printf '%s:%s' "$1" "$2"
  else
    printf '%s:%s:%s' "$BIND" "$1" "$2"
  fi
}

warn_if_public() {
  [ -z "$BIND" ] || [ "$BIND" = "0.0.0.0" ] || return 0
  warn "The console will be reachable from the network."
  note "It has no sign-in, and its start-process dialog launches a load run"
  note "for anybody who can open the page. Put an authenticating reverse proxy"
  note "in front of it, or re-run with --bind 127.0.0.1 and reach it over SSH:"
  note "  ssh -L ${PORT}:localhost:${PORT} user@$(uname -n)"
  printf '\n'
}

# ==========================================================================
# 9. Method: single Docker container
# ==========================================================================

require_docker() {
  detect_docker
  if [ -z "$DOCKER" ]; then
    warn "Docker is not installed on this host."
    note "Install it first:  curl -fsSL https://get.docker.com | sh"
    note "Then re-run this installer, or use --method binary instead."
    die "Docker is required for --method ${METHOD}."
  fi
  if ! docker_ready; then
    warn "Docker is installed but the daemon is not reachable as $(id -un)."
    note "Try:  sudo systemctl start docker"
    note "Or add yourself to the docker group:  sudo usermod -aG docker \$USER"
    die "Cannot talk to the Docker daemon."
  fi
}

require_compose() {
  require_docker
  if [ -z "$DOCKER_COMPOSE" ]; then
    warn "Docker Compose is not available."
    note "It ships with modern Docker as 'docker compose'."
    note "On older installs: https://docs.docker.com/compose/install/"
    die "Docker Compose is required for --method ${METHOD}."
  fi
}

install_docker() {
  eyebrow "INSTALL ${GLYPH_SEP} DOCKER"
  require_docker

  # Recreating the container is how this method upgrades, so everything the
  # running one was given has to be read off it first.
  DATA_MOUNT="${HOME}/.3270connect/data"
  adopt_existing ""
  resolve_data_mount
  assert_data_preserved

  local tag="latest"
  [ "$VERSION" != "latest" ] && tag="$VERSION"
  [ -z "$PUBLISH_SPEC" ] && PUBLISH_SPEC="${BIND}:${PORT}"

  step "image" "${IMAGE}:${tag}"
  step "name" "$CONTAINER_NAME"
  step "listen" "${PUBLISH_SPEC} → 9200"
  step "data" "${DATA_MOUNT} → /data"
  step_exposure
  printf '\n'
  warn_if_public

  # On an update the listener on that port is the container about to be
  # replaced, so saying it is taken would be the installer warning about
  # itself.
  if [ "$EXISTING_STACK" -eq 0 ] && port_in_use "$PORT"; then
    warn "Port ${PORT} already has a listener."
    confirm "Continue anyway?" || die "Cancelled." 130
    printf '\n'
  fi

  if ! confirm "Pull the image and start the console?"; then
    die "Cancelled." 130
  fi
  printf '\n'

  if [ "$DRY_RUN" -eq 1 ]; then
    step "dry run" "nothing pulled or started" "skip" "$C_WARN"
    return 0
  fi

  if $DOCKER ps -a --format '{{.Names}}' 2>/dev/null </dev/null | grep -qx "$CONTAINER_NAME"; then
    warn "A container named ${CONTAINER_NAME} already exists."
    note "Recreating it is how this method upgrades. Its settings above were"
    note "read off it, and ${DATA_MOUNT} — the run history — is mounted onto"
    note "the new container unchanged."
    if confirm "Remove and recreate it?"; then
      $DOCKER rm -f "$CONTAINER_NAME" >/dev/null </dev/null
      step "removed" "previous ${CONTAINER_NAME}" "ok"
    else
      die "Cancelled." 130
    fi
  fi

  spin_start "Pulling ${IMAGE}:${tag}"
  if ! $DOCKER pull "${IMAGE}:${tag}" >/dev/null 2>&1 </dev/null; then
    spin_stop
    die "Could not pull ${IMAGE}:${tag}."
  fi
  spin_stop "pulled" "${IMAGE}:${tag}" "ok"

  if [ "$DATA_IS_VOLUME" -eq 1 ]; then
    step "data" "docker volume ${DATA_MOUNT} (kept)" "ok"
  else
    prepare_data_dir "$DATA_DIR_PATH" || true
  fi

  # Redundant with the image default, but stated explicitly so the container's
  # listen address is visible in `docker inspect` next to the port mapping.
  $DOCKER run -d \
    --name "$CONTAINER_NAME" \
    --restart unless-stopped \
    -p "${PUBLISH_SPEC}:9200" \
    -e DASHBOARD_BIND=0.0.0.0 \
    -v "${DATA_MOUNT}:/data" \
    "${IMAGE}:${tag}" >/dev/null </dev/null
  step "started" "container ${CONTAINER_NAME}" "up"
  step "data" "${DATA_MOUNT} → /data" "ok"

  wait_for_health
  success_docker "$tag"
}

success_docker() {
  printf '\n'
  panel "3270Connect is running" "${1} ${GLYPH_SEP} docker"
  printf '\n'
  step "open" "http://localhost:${PORT}/dashboard"
  step "logs" "docker logs -f ${CONTAINER_NAME}"
  step "run" "docker exec ${CONTAINER_NAME} 3270Connect -config /data/workflow.json -headless"
  step "stop" "docker stop ${CONTAINER_NAME}"
  step "remove" "docker rm -f ${CONTAINER_NAME}"
  step "docs" "${DOCS_URL}/installation/"
  printf '\n'
}

# ==========================================================================
# 10. Method: Docker Compose
# ==========================================================================

install_compose() {
  eyebrow "INSTALL ${GLYPH_SEP} DOCKER COMPOSE"
  require_compose

  # Before anything is resolved against the current directory: an install that
  # is already running decides which directory this run updates.
  resolve_compose_dir

  local file="${COMPOSE_DIR}/docker-compose.yml"
  adopt_existing "$file"
  resolve_data_mount
  assert_data_preserved

  local tag="latest"
  [ "$VERSION" != "latest" ] && tag="$VERSION"
  [ -z "$PUBLISH_SPEC" ] && PUBLISH_SPEC="${BIND}:${PORT}"

  step "file" "$file"
  step "image" "${IMAGE}:${tag}"
  step "listen" "${PUBLISH_SPEC} → 9200"
  step "compose" "$DOCKER_COMPOSE"
  step "data" "${DATA_MOUNT} → /data"
  step_exposure
  printf '\n'
  warn_if_public

  # Re-running is the ordinary way to take a new image or change a setting, so
  # it explains what it is about to do rather than asking whether to overwrite
  # a file the person may not remember writing. Nothing in the data folder is
  # touched, and the previous file is kept beside the new one.
  if [ "$EXISTING_STACK" -eq 1 ]; then
    note "Updating the stack already in ${COMPOSE_DIR}."
    note "Settings not given on the command line are carried over, and"
    note "${DATA_MOUNT} — the run history — is left alone."
    printf '\n'
    confirm "Rewrite the stack file and restart?" || die "Cancelled." 130
    printf '\n'
  else
    if ! confirm "Write the stack and bring it up?"; then
      die "Cancelled." 130
    fi
    printf '\n'
  fi

  if [ "$DRY_RUN" -eq 1 ]; then
    step "dry run" "nothing written or started" "skip" "$C_WARN"
    return 0
  fi

  mkdir -p "$COMPOSE_DIR"
  if [ "$DATA_IS_VOLUME" -eq 1 ]; then
    step "data" "docker volume ${DATA_MOUNT} (kept)" "ok"
  else
    prepare_data_dir "$DATA_DIR_PATH" || true
  fi

  # A data folder that is a Docker volume has to be declared, and declared as
  # external when this stack did not create it: Compose otherwise prefixes the
  # name with the project's and mounts a new, empty volume of its own.
  local volume_decls=""
  if [ "$DATA_IS_VOLUME" -eq 1 ]; then
    if [ "$DATA_IS_EXTERNAL" -eq 1 ]; then
      volume_decls="

volumes:
  # The volume this install's run history is already in. External, so Compose
  # uses that exact volume rather than creating a project-prefixed one --
  # which would come up empty.
  ${DATA_MOUNT}:
    external: true"
    else
      volume_decls="

volumes:
  ${DATA_MOUNT}:"
    fi
  fi

  local ports_note
  if [ "$PUBLISH_SPEC" = "$PORT" ]; then
    ports_note="      # Published on every interface, and the console has no sign-in of its
      # own -- its start-process dialog launches a load run for anybody who
      # can open the page. Prefix a host address -- \"127.0.0.1:${PORT}:9200\" --
      # to keep it on this machine."
  else
    ports_note="      # Bound to ${PUBLISH_SPEC%:*}, because the console has no sign-in of its own
      # and its start-process dialog launches a load run for anybody who can
      # open the page. Change to \"${PORT}:9200\" to publish on every interface,
      # and put an authenticating reverse proxy in front of it if you do."
  fi

  # The file this run replaces is kept beside it. Rewriting somebody's stack
  # is a destructive edit to a file they may have added their own settings to,
  # and a copy costs nothing next to reconstructing one from memory.
  if [ -e "$file" ]; then
    cp -p "$file" "${file}.bak" 2>/dev/null || cp "$file" "${file}.bak" || true
    step "kept" "the previous stack at $(short_path "${file}.bak")" "ok"
  fi

  cat > "$file" <<YAML
# 3270Connect — ${DOCS_URL}
#
# Generated by the 3270Connect installer. Edit freely; re-run
# "${DOCKER_COMPOSE} up -d" to apply changes.

services:
  3270connect:
    image: ${IMAGE}:${tag}
    container_name: ${CONTAINER_NAME}
    ports:
${ports_note}
      - "${PUBLISH_SPEC}:9200"
    environment:
      # Listen on all interfaces *inside* the container. A published port
      # forwards to the container's external interface, so a loopback bind
      # here would be unreachable however the ports are mapped. What the
      # console is exposed to is decided by "ports:" above, not by this
      # line -- do not change it to 127.0.0.1 to keep the console private.
      - DASHBOARD_BIND=0.0.0.0
    volumes:
      # The metrics every run publishes, the console's logs, and whatever a
      # workflow writes. It has to outlive the container: recreating one
      # without this -- which is what pulling a new image does -- takes the
      # history of every run with it.
      #
      # 3270Connect runs unprivileged as uid ${RUNTIME_UID} and a bind mount keeps the
      # host's ownership, so the folder belongs to that user:
      #   sudo chown -R ${RUNTIME_UID}:${RUNTIME_GID} ${DATA_MOUNT}
      - ${DATA_MOUNT}:/data
    restart: unless-stopped${volume_decls}

# Running a workflow rather than the console -- the image takes any other set
# of flags as its command:
#
#   ${DOCKER_COMPOSE} run --rm 3270connect -config workflow.json -headless
#
# Both read /data, so the workflow file goes in ${DATA_MOUNT} beside the output.
YAML
  step "wrote" "$file" "ok"

  compose_pull_and_up
  wait_for_health
  success_compose "$tag"
}

# compose_pull_and_up — pull, then start, keeping the output when either fails.
#
# Pulled explicitly rather than left to `up -d`: compose only pulls an image it
# does not already have, so a re-run against an existing stack — the normal way
# to take a new build of a floating tag like :latest — would otherwise restart
# the same cached image under the appearance of an update.
compose_pull_and_up() {
  local log status

  log="$(mktemp "${TMPDIR:-/tmp}/3270connect-pull.XXXXXX")"
  status=0
  spin_start "Pulling images"
  (cd "$COMPOSE_DIR" && $DOCKER_COMPOSE pull >"$log" 2>&1 </dev/null) || status=$?
  if [ "$status" -ne 0 ]; then
    spin_stop
    printf '\n'
    warn "${DOCKER_COMPOSE} pull failed (exit ${status}). It said:"
    printf '\n'
    sed -e 's/^/      /' "$log" | tail -n 12 >&2
    printf '\n'
    rm -f "$log"
    die "Could not pull the images."
  fi
  rm -f "$log"
  spin_stop "pulled" "the images this stack runs" "ok"

  log="$(mktemp "${TMPDIR:-/tmp}/3270connect-compose.XXXXXX")"
  status=0
  spin_start "Starting the stack"
  (cd "$COMPOSE_DIR" && $DOCKER_COMPOSE up -d >"$log" 2>&1 </dev/null) || status=$?
  if [ "$status" -ne 0 ]; then
    spin_stop
    printf '\n'
    warn "${DOCKER_COMPOSE} up -d failed (exit ${status}). It said:"
    printf '\n'
    # The tail, because compose repeats its progress lines and the reason is
    # the last thing it prints. Indented so it reads as quoted output.
    sed -e 's/^/      /' "$log" | tail -n 12 >&2
    printf '\n'
    note "The stack file is written and unchanged; fix the cause and re-run:"
    note "  cd ${COMPOSE_DIR} && ${DOCKER_COMPOSE} up -d"
    rm -f "$log"
    die "Could not start the stack."
  fi
  rm -f "$log"
  spin_stop "started" "${DOCKER_COMPOSE} up -d" "up"
}

success_compose() {
  printf '\n'
  panel "3270Connect is running" "${1} ${GLYPH_SEP} compose"
  printf '\n'
  step "open" "http://localhost:${PORT}/dashboard"
  step "dir" "$COMPOSE_DIR"
  step "status" "${DOCKER_COMPOSE} ps"
  step "logs" "${DOCKER_COMPOSE} logs -f"
  step "run" "${DOCKER_COMPOSE} run --rm 3270connect -config workflow.json -headless"
  step "stop" "${DOCKER_COMPOSE} down"
  step "docs" "${DOCS_URL}/installation/"
  printf '\n'
}

# ==========================================================================
# 11. Method: the lab
#
# The console on its own has nothing to talk to, and the first question after
# installing it is always "against what?". This answers it: a TN3270 host, a
# browser terminal to explore it with, and the console to replay what you did
# there — three containers, one network, no mainframe required.
# ==========================================================================

install_lab() {
  eyebrow "INSTALL ${GLYPH_SEP} THE LAB"
  require_compose

  [ "$COMPOSE_DIR_EXPLICIT" -eq 0 ] && COMPOSE_DIR="$PWD/3270-lab"

  local file="${COMPOSE_DIR}/docker-compose.yml"
  local workflow="${COMPOSE_DIR}/workflow-sampleapp.json"

  step "dir" "$COMPOSE_DIR"
  step "console" "http://localhost:${PORT}/dashboard  ${GLYPH_SEP} ${IMAGE}"
  step "terminal" "http://localhost:${WEB_PORT}  ${GLYPH_SEP} ${WEB_IMAGE}"
  step "host" "sampleapps:${SAMPLE_PORT} (pet store), :${SAMPLE_PORT_2} (app1)"
  step_exposure
  printf '\n'
  note "Nothing here is a mainframe: the applications are demonstration"
  note "screens with no data behind them, which is what makes the lab safe"
  note "to point a load test at."
  printf '\n'
  # Neither page in the lab has a sign-in, so this is the one method where a
  # non-loopback bind exposes two of them at once.
  warn_if_public

  local port
  for port in "$PORT" "$WEB_PORT" "$SAMPLE_PORT" "$SAMPLE_PORT_2"; do
    if port_in_use "$port"; then
      warn "Port ${port} already has a listener; the lab wants all four free."
      confirm "Continue anyway?" || die "Cancelled." 130
      printf '\n'
      break
    fi
  done

  if ! confirm "Write the lab and bring it up?"; then
    die "Cancelled." 130
  fi
  printf '\n'

  if [ "$DRY_RUN" -eq 1 ]; then
    step "dry run" "nothing written or started" "skip" "$C_WARN"
    return 0
  fi

  mkdir -p "$COMPOSE_DIR"

  if [ -e "$file" ]; then
    cp -p "$file" "${file}.bak" 2>/dev/null || cp "$file" "${file}.bak" || true
    step "kept" "the previous stack at $(short_path "${file}.bak")" "ok"
  fi

  cat > "$workflow" <<'JSON'
{
  "Host": "sampleapps",
  "Port": 3272,
  "OutputFilePath": "output.html",
  "EveryStepDelay": { "Min": 0.1, "Max": 0.3 },
  "RampUpBatchSize": 50,
  "RampUpDelay": 1.5,
  "EndOfTaskDelay": { "Min": 0.5, "Max": 1.5 },
  "Steps": [
    { "Type": "Connect" },
    {
      "Type": "CheckValue",
      "Coordinates": { "Row": 1, "Column": 29, "Length": 24 },
      "Text": "3270 Example Application"
    },
    { "Type": "AsciiScreenGrab" },
    {
      "Type": "FillString",
      "Coordinates": { "Row": 5, "Column": 21 },
      "Text": "user1-firstname"
    },
    {
      "Type": "FillString",
      "Coordinates": { "Row": 6, "Column": 21 },
      "Text": "user1-lastname"
    },
    { "Type": "PressEnter" },
    {
      "Type": "CheckValue",
      "Coordinates": { "Row": 1, "Column": 29, "Length": 24 },
      "Text": "3270 Example Application"
    },
    { "Type": "AsciiScreenGrab" },
    { "Type": "Disconnect" }
  ]
}
JSON
  step "wrote" "$(short_path "$workflow")" "ok"

  local tag="latest"
  [ "$VERSION" != "latest" ] && tag="$VERSION"

  cat > "$file" <<YAML
# The 3270 lab — ${DOCS_URL}
#
# Generated by the 3270Connect installer. Three containers on one network: a
# TN3270 host to practise against, the browser terminal, and the automation
# console. Nothing here is a mainframe and nothing here has data behind it,
# which is what makes it safe to point a load test at.
#
#   http://localhost:${WEB_PORT}    the terminal. Connect to  sampleapps  port ${SAMPLE_PORT}
#   http://localhost:${PORT}    the console. Upload workflow-sampleapp.json

services:
  # The host to practise against: 3270Web's bundled sample applications,
  # served over TCP so anything on this network can dial them.
  sampleapps:
    image: ${WEB_IMAGE}:latest
    command: ["sampleapp", "--app", "petstore:${SAMPLE_PORT}", "--app", "app1:${SAMPLE_PORT_2}"]
    ports:
      # Published so a terminal on this machine that is not in this stack can
      # be pointed at them too. Containers here reach them as "sampleapps"
      # and do not need this.
      - "$(publish_entry "$SAMPLE_PORT" "$SAMPLE_PORT")"
      - "$(publish_entry "$SAMPLE_PORT_2" "$SAMPLE_PORT_2")"
    # The image's own healthcheck asks the web UI for /healthz, and this
    # service is not running the web UI. A TN3270 listener has nothing to
    # answer an HTTP probe with, so the check is what a client does: open the
    # port. bash explicitly, because /dev/tcp is a bash feature.
    healthcheck:
      test: ["CMD-SHELL", "bash -c 'exec 3<>/dev/tcp/127.0.0.1/${SAMPLE_PORT}' || exit 1"]
      interval: 10s
      timeout: 3s
      retries: 5
      start_period: 5s
    restart: unless-stopped

  # The terminal: drive the sample host by hand, and record what you did.
  3270web:
    image: ${WEB_IMAGE}:latest
    depends_on:
      sampleapps:
        condition: service_healthy
    ports:
      - "$(publish_entry "$WEB_PORT" 3270)"
    environment:
      - GIN_MODE=release
      - WEBUI_BIND=0.0.0.0
      # No accounts, deliberately: a lab you have to create an administrator
      # for before you can look at it is a lab nobody looks at. That is only
      # safe while the ports above are on loopback. For anything with more
      # than one person on it, set AUTH_MODE=local.
    volumes:
      # Named volumes, so the lab needs no chown before it starts and
      # "down -v" is enough to reset it.
      - 3270web-data:/data
    restart: unless-stopped

  # The console: replay that workflow, concurrently, and watch it happen.
  3270connect:
    image: ${IMAGE}:${tag}
    depends_on:
      sampleapps:
        condition: service_healthy
    ports:
      - "$(publish_entry "$PORT" 9200)"
    environment:
      - DASHBOARD_BIND=0.0.0.0
    volumes:
      - 3270connect-data:/data
      # A workflow already pointed at the sample host, for the command-line
      # runs below. The console's own dialog takes a workflow uploaded from
      # the browser rather than a path, so for that one you pick this same
      # file off your machine.
      - ./workflow-sampleapp.json:/data/workflow-sampleapp.json:ro
    restart: unless-stopped

volumes:
  3270web-data:
  3270connect-data:

# Running the workflow headlessly, the way a CI job would:
#
#   ${DOCKER_COMPOSE} run --rm 3270connect -config workflow-sampleapp.json -headless
#
# And as a load test. It stays up serving its own console when the run
# finishes, as a load run always does, so stop it with Ctrl+C:
#
#   ${DOCKER_COMPOSE} run --rm 3270connect -config workflow-sampleapp.json \\
#     -headless -concurrent 25 -runtime 120
#
# To take it all down, including the run history and the terminal's saved work:
#
#   ${DOCKER_COMPOSE} down -v
YAML
  step "wrote" "$file" "ok"

  compose_pull_and_up
  wait_for_health
  success_lab
}

success_lab() {
  printf '\n'
  panel "The lab is running" "3 services ${GLYPH_SEP} compose"
  printf '\n'
  step "terminal" "http://localhost:${WEB_PORT}"
  step "console" "http://localhost:${PORT}/dashboard"
  step "host" "sampleapps port ${SAMPLE_PORT} (pet store) and ${SAMPLE_PORT_2} (app1)"
  printf '\n'
  note "Start in the terminal: connect to host \"sampleapps\" port ${SAMPLE_PORT}, and"
  note "drive the pet store by hand. Then replay one headlessly:"
  printf '\n'
  step "run" "cd $(short_path "$COMPOSE_DIR") && ${DOCKER_COMPOSE} run --rm 3270connect -config workflow-sampleapp.json -headless"
  step "dir" "$COMPOSE_DIR"
  step "stop" "${DOCKER_COMPOSE} down -v"
  step "docs" "${DOCS_URL}/installation/"
  printf '\n'
}

# ==========================================================================
# 12. Health
# ==========================================================================

wait_for_health() {
  local url="http://127.0.0.1:${PORT}/healthz" i=0
  have curl || { step "health" "curl unavailable, skipped" "skip" "$C_WARN"; return 0; }
  spin_start "Waiting for ${url}"
  while [ "$i" -lt 45 ]; do
    if curl -fsS -m 2 "$url" >/dev/null 2>&1 </dev/null; then
      spin_stop "health" "GET /healthz" "ok"
      return 0
    fi
    i=$((i + 1))
    sleep 1
  done
  spin_stop "health" "no response after 45s" "slow" "$C_WARN"
  note "The container may still be starting. Check its logs if the page does not load."
}

# ==========================================================================
# 13. Method chooser — the "pick a door" card list from 3270.io
# ==========================================================================

card() { # card <key> <title> <blurb> <meta>
  printf '   %s%s[%s]%s  %s%s%-15s%s%s\n' \
    "$C_BOLD" "$C_ACCENT" "$1" "$C_RESET" \
    "$C_BOLD" "$C_TEXT" "$2" "$C_RESET" "$3"
  printf '         %s%s %s%s\n' "$C_FAINT" "$BOX_H" "$4" "$C_RESET"
}

choose_method() {
  eyebrow "PICK A DOOR"

  local docker_meta compose_meta
  if [ -n "$DOCKER" ]; then
    docker_meta="docker detected ${GLYPH_SEP} ${IMAGE}"
  else
    docker_meta="docker not found on this host"
  fi
  if [ -n "$DOCKER_COMPOSE" ]; then
    compose_meta="${DOCKER_COMPOSE} ${GLYPH_SEP} writes $(short_path "$COMPOSE_DIR")/docker-compose.yml"
  else
    compose_meta="docker compose not found on this host"
  fi

  local binary_meta
  resolve_binary_paths
  if [ "$ARCH" = "amd64" ]; then
    binary_meta="s3270 bundled ${GLYPH_SEP} installs to $(short_path "$BIN_PATH")"
  else
    binary_meta="no ${ARCH} build is published ${GLYPH_SEP} amd64 only"
  fi

  card 1 "Binary" \
    "$(printf '%sself-contained, no runtime deps%s' "$C_DIM" "$C_RESET")" \
    "$binary_meta"
  printf '\n'
  card 2 "Docker" \
    "$(printf '%sone container, the console%s' "$C_DIM" "$C_RESET")" \
    "$docker_meta"
  printf '\n'
  card 3 "Compose" \
    "$(printf '%sa stack you can edit and re-up%s' "$C_DIM" "$C_RESET")" \
    "$compose_meta"
  printf '\n'
  card 4 "Lab" \
    "$(printf '%s+ a terminal and a host to aim at%s' "$C_DIM" "$C_RESET")" \
    "$(if [ -n "$DOCKER_COMPOSE" ]; then printf 'three containers %s nothing to install on a mainframe' "$GLYPH_SEP"; else printf 'docker compose not found on this host'; fi)"
  printf '\n'

  if [ "$TTY_OK" -eq 0 ]; then
    # Non-interactive and no --method: pick the one most likely to work.
    if [ "$ARCH" = "amd64" ]; then METHOD="binary"; else METHOD="docker"; fi
    step "selected" "${METHOD} (non-interactive default)" "auto" "$C_INFO"
    return 0
  fi

  local default="1"
  [ "$ARCH" != "amd64" ] && default="2"

  while :; do
    local reply
    reply="$(ask "Select 1-4" "$default")"
    case "$reply" in
      1|binary)  METHOD="binary";  break ;;
      2|docker)  METHOD="docker";  break ;;
      3|compose) METHOD="compose"; break ;;
      4|lab)     METHOD="lab";     break ;;
      *) warn "Enter 1, 2, 3 or 4." ;;
    esac
  done
}

# ==========================================================================
# 14. Usage & arguments
# ==========================================================================

usage() {
  cat <<EOF
3270Connect installer

  curl -fsSL ${DOCS_URL}/install.sh | bash
  curl -fsSL ${DOCS_URL}/install.sh | bash -s -- --method lab --yes

Options
  --method <binary|docker|compose|lab>
                                    Installation method (default: ask).
                                    lab adds the browser terminal and a
                                    sample TN3270 host to point them at
  --version <tag>                   Release tag, e.g. v2.0 (default: latest)
  --port <port>                     Host port for the console (default: 9200)
  --bind <address>                  Host interface to bind (default: 127.0.0.1)
  --dir <path>                      Compose project directory
                                    (default: ./3270connect, ./3270-lab)
  --system                          Binary install to /usr/local/bin
  --user                            Binary install under \$HOME (default)
  --theme <grn|amb|ice|day>         Installer colour palette (default: grn)
  --no-color                        Disable colour output
  --color                           Force colour output
  --yes, -y                         Accept every prompt (non-interactive)
  --dry-run                         Show what would happen, change nothing
  --help, -h                        This text

Documentation
  ${DOCS_URL}/installation/
EOF
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --method)  METHOD="${2:-}"; shift 2 ;;
      --method=*) METHOD="${1#*=}"; shift ;;
      --version) VERSION="${2:-}"; VERSION_EXPLICIT=1; shift 2 ;;
      --version=*) VERSION="${1#*=}"; VERSION_EXPLICIT=1; shift ;;
      --port)    PORT="${2:-}"; PORT_EXPLICIT=1; shift 2 ;;
      --port=*)  PORT="${1#*=}"; PORT_EXPLICIT=1; shift ;;
      --bind)    BIND="${2:-}"; BIND_EXPLICIT=1; shift 2 ;;
      --bind=*)  BIND="${1#*=}"; BIND_EXPLICIT=1; shift ;;
      --dir)     COMPOSE_DIR="${2:-}"; COMPOSE_DIR_EXPLICIT=1; shift 2 ;;
      --dir=*)   COMPOSE_DIR="${1#*=}"; COMPOSE_DIR_EXPLICIT=1; shift ;;
      --theme)   THEME="${2:-}"; shift 2 ;;
      --theme=*) THEME="${1#*=}"; shift ;;
      --system)  SYSTEM_INSTALL="yes"; shift ;;
      --user)    SYSTEM_INSTALL="no"; shift ;;
      --no-color|--no-colour) USE_COLOR="never"; shift ;;
      --color|--colour)       USE_COLOR="always"; shift ;;
      -y|--yes)  ASSUME_YES=1; shift ;;
      --dry-run) DRY_RUN=1; shift ;;
      -h|--help) usage; exit 0 ;;
      *) printf 'Unknown option: %s\n\n' "$1" >&2; usage >&2; exit 2 ;;
    esac
  done

  case "$METHOD" in
    ""|binary|docker|compose|lab) : ;;
    *) printf 'Unknown --method: %s (want binary, docker, compose or lab)\n' "$METHOD" >&2; exit 2 ;;
  esac

  case "$PORT" in
    ''|*[!0-9]*) printf 'Invalid --port: %s\n' "$PORT" >&2; exit 2 ;;
  esac

  # Absolute from here on. The project directory is compared against the one
  # Docker reports for an install that is already running, and "3270connect"
  # and "/home/you/3270connect" have to be able to match.
  case "$COMPOSE_DIR" in
    /*) : ;;
    *)  COMPOSE_DIR="${PWD}/${COMPOSE_DIR#./}" ;;
  esac
}

# ==========================================================================
# 15. Main
# ==========================================================================

main() {
  parse_args "$@"
  setup_geometry
  setup_palette
  setup_tty
  detect_arch

  banner

  [ "$(uname -s)" = "Linux" ] || warn "This installer targets Linux; $(uname -s) is untested."

  eyebrow "PREFLIGHT"
  step "host" "$(uname -s) $(uname -m)" "ok"
  have curl && step "curl" "$(command -v curl)" "ok" || step "curl" "not found" "missing" "$C_WARN"

  detect_docker
  if [ -n "$DOCKER" ] && docker_ready; then
    step "docker" "$($DOCKER --version 2>/dev/null </dev/null | head -n1)" "ok"
  elif [ -n "$DOCKER" ]; then
    step "docker" "installed, daemon unreachable" "warn" "$C_WARN"
  else
    step "docker" "not installed" "n/a" "$C_FAINT"
  fi
  [ -n "$DOCKER_COMPOSE" ] \
    && step "compose" "$DOCKER_COMPOSE" "ok" \
    || step "compose" "not installed" "n/a" "$C_FAINT"

  if [ "$METHOD" = "binary" ] || [ -z "$METHOD" ]; then
    resolve_release
    step "release" "$RESOLVED_VERSION" "ok"
  fi

  [ -z "$METHOD" ] && choose_method

  # --method binary given directly still needs the release metadata.
  [ "$METHOD" = "binary" ] && [ -z "$RESOLVED_VERSION" ] && resolve_release

  case "$METHOD" in
    binary)  install_binary ;;
    docker)  install_docker ;;
    compose) install_compose ;;
    lab)     install_lab ;;
  esac
}

main "$@"
