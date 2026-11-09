#!/usr/bin/env bash
# =============================================================================
# AgentBoard installer
#
#   curl -fsSL https://agentboard.ayande.xyz/install.sh | bash
#
# Downloads the latest agentboard binary for this platform from GitHub
# Releases, verifies its SHA-256 against the release's checksums.txt, and
# installs it to ~/.local/bin (Termux: $PREFIX/bin). Installs tmux and git
# with the system package manager if they are missing. Re-run any time to
# update, or run `agentboard update`.
#
# Environment:
#   AGENTBOARD_VERSION=v0.2.0      install this tag instead of the latest
#   AGENTBOARD_INSTALL_DIR=<dir>   install somewhere other than ~/.local/bin
#   AGENTBOARD_NO_DEPS=1           do not install tmux/git
#   AGENTBOARD_NO_MODIFY_PATH=1    do not edit shell rc files
# =============================================================================
set -euo pipefail

REPO="ayandexyz/agent-board"
SITE="https://agentboard.ayande.xyz"

if [ -t 1 ]; then
  BOLD=$'\033[1m'; BLUE=$'\033[1;34m'; GREEN=$'\033[1;32m'; YELLOW=$'\033[1;33m'; RED=$'\033[1;31m'; RESET=$'\033[0m'
else
  BOLD=""; BLUE=""; GREEN=""; YELLOW=""; RED=""; RESET=""
fi
info() { printf '%s==>%s %s\n' "$BLUE" "$RESET" "$*"; }
ok()   { printf '%s ✓%s %s\n' "$GREEN" "$RESET" "$*"; }
warn() { printf '%swarning:%s %s\n' "$YELLOW" "$RESET" "$*" >&2; }
err()  { printf '%serror:%s %s\n' "$RED" "$RESET" "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

tmpdir=""
cleanup() { [ -z "$tmpdir" ] || rm -rf "$tmpdir"; }
trap cleanup EXIT

is_termux() { [ -n "${TERMUX_VERSION:-}" ] || [[ "${PREFIX:-}" == *com.termux* ]]; }

detect_asset() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64)  arch="x86_64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) err "unsupported architecture: $arch (agentboard ships x86_64 and arm64)" ;;
  esac
  case "$os" in
    Linux)
      if is_termux || [ "$(uname -o 2>/dev/null)" = "Android" ]; then
        [ "$arch" = "arm64" ] || err "on Android only arm64 is supported"
        echo "agentboard-android-arm64"
      else
        echo "agentboard-linux-$arch"
      fi ;;
    Darwin) echo "agentboard-darwin-$arch" ;;
    MINGW*|MSYS*|CYGWIN*) err "Windows is not supported natively; install inside WSL instead" ;;
    *) err "unsupported OS: $os" ;;
  esac
}

resolve_version() {
  if [ -n "${AGENTBOARD_VERSION:-}" ]; then
    case "$AGENTBOARD_VERSION" in v*) echo "$AGENTBOARD_VERSION" ;; *) echo "v$AGENTBOARD_VERSION" ;; esac
    return
  fi
  # Follow the /releases/latest redirect rather than calling the API, which
  # rate-limits anonymous callers to 60 requests an hour.
  local url
  url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")" \
    || err "could not reach GitHub to find the latest release"
  case "$url" in
    */releases/tag/*) echo "${url##*/}" ;;
    *) err "no published release found at https://github.com/$REPO/releases" ;;
  esac
}

sha256_of() {
  if have sha256sum; then sha256sum "$1" | cut -d' ' -f1
  elif have shasum; then shasum -a 256 "$1" | cut -d' ' -f1
  else err "neither sha256sum nor shasum is available to verify the download"
  fi
}

as_root() {
  if [ "$(id -u)" -eq 0 ]; then "$@"
  elif have sudo; then sudo "$@"
  else return 1
  fi
}

install_pkg() {
  local pkg="$1"
  if is_termux;          then pkg install -y "$pkg"
  elif have brew;        then brew install "$pkg"
  elif have apt-get;     then as_root apt-get install -y "$pkg"
  elif have dnf;         then as_root dnf install -y "$pkg"
  elif have pacman;      then as_root pacman -S --noconfirm --needed "$pkg"
  elif have zypper;      then as_root zypper install -y "$pkg"
  elif have apk;         then as_root apk add "$pkg"
  else return 1
  fi
}

ensure_dep() {
  local cmd="$1" why="$2"
  if have "$cmd"; then
    ok "$cmd found"
    return
  fi
  if [ "${AGENTBOARD_NO_DEPS:-}" = "1" ]; then
    warn "$cmd is not installed ($why); install it before running agentboard"
    return
  fi
  info "installing $cmd ($why)"
  if install_pkg "$cmd" && have "$cmd"; then
    ok "$cmd installed"
  else
    warn "could not install $cmd automatically; install it with your package manager ($why)"
  fi
}

add_to_path() {
  local dir="$1" line rc added=""
  case ":$PATH:" in *":$dir:"*) return ;; esac
  if [ "${AGENTBOARD_NO_MODIFY_PATH:-}" = "1" ] || is_termux; then
    warn "$dir is not on your PATH; add it with: export PATH=\"$dir:\$PATH\""
    return
  fi
  line="export PATH=\"$dir:\$PATH\""
  for rc in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
    case "$rc" in
      */.bashrc) have bash || [ -f "$rc" ] || continue ;;
      */.zshrc)  have zsh || [ -f "$rc" ] || [ "$(uname -s)" = "Darwin" ] || continue ;;
    esac
    if ! grep -qF "$dir" "$rc" 2>/dev/null; then
      printf '\n# Added by the agentboard installer\n%s\n' "$line" >> "$rc"
      added="$added $rc"
    fi
  done
  rc="${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
  if have fish && ! grep -qF "$dir" "$rc" 2>/dev/null; then
    mkdir -p "$(dirname "$rc")"
    printf '\n# Added by the agentboard installer\nfish_add_path -g "%s"\n' "$dir" >> "$rc"
    added="$added $rc"
  fi
  [ -z "$added" ] || ok "added $dir to PATH in:$added"
  PATH_CHANGED=1
}

main() {
  have curl || err "curl is required"
  printf '\n%sAgentBoard installer%s\n\n' "$BOLD" "$RESET"

  local asset version base install_dir target existing want got
  asset="$(detect_asset)"
  version="$(resolve_version)"
  base="https://github.com/$REPO/releases/download/$version"

  if is_termux; then
    install_dir="${AGENTBOARD_INSTALL_DIR:-${PREFIX:-/data/data/com.termux/files/usr}/bin}"
  else
    install_dir="${AGENTBOARD_INSTALL_DIR:-$HOME/.local/bin}"
  fi
  target="$install_dir/agentboard"

  existing=""
  [ -x "$target" ] && existing="$("$target" --version 2>/dev/null | awk '{print $2}' || true)"
  if [ -z "$existing" ]; then
    info "installing agentboard $version ($asset)"
  elif [ "$existing" = "$version" ]; then
    info "agentboard $version is already installed; reinstalling"
  else
    info "updating agentboard $existing -> $version"
  fi

  tmpdir="$(mktemp -d)"
  curl -fsSL "$base/checksums.txt" -o "$tmpdir/checksums.txt" \
    || err "release $version has no checksums.txt (is $version published?)"
  want="$(awk -v a="$asset" '$2 == a || $2 == "*"a {print $1}' "$tmpdir/checksums.txt")"
  [ -n "$want" ] || err "checksums.txt in $version lists no $asset"

  curl -fSL --progress-bar "$base/$asset" -o "$tmpdir/agentboard" \
    || err "download failed: $base/$asset"
  got="$(sha256_of "$tmpdir/agentboard")"
  [ "$got" = "$want" ] || err "checksum mismatch for $asset (got $got, want $want); aborting"
  ok "verified sha256 $got"

  mkdir -p "$install_dir"
  chmod 755 "$tmpdir/agentboard"
  if [ "$(uname -s)" = "Darwin" ]; then
    xattr -d com.apple.quarantine "$tmpdir/agentboard" 2>/dev/null || true
  fi
  cp "$tmpdir/agentboard" "$install_dir/.agentboard.new"
  mv -f "$install_dir/.agentboard.new" "$target"
  ok "installed $target"

  echo ""
  info "checking dependencies"
  ensure_dep tmux "agentboard runs every agent in a tmux window"
  ensure_dep git "tickets get their own git worktree"

  PATH_CHANGED=0
  add_to_path "$install_dir"

  local resolved
  resolved="$(command -v agentboard 2>/dev/null || true)"
  if [ -n "$resolved" ] && [ "$resolved" != "$target" ]; then
    warn "another agentboard at $resolved comes first on your PATH; remove it or it will shadow $target"
  fi

  printf '\n%sagentboard %s is installed.%s\n\n' "$GREEN" "$version" "$RESET"
  echo "  Get started — open any git repository and run:"
  echo ""
  if [ "$PATH_CHANGED" = "1" ]; then
    printf '    %sexport PATH="%s:$PATH"%s   # this shell only; new shells already have it\n' "$BOLD" "$install_dir" "$RESET"
  fi
  printf '    %scd your-project && agentboard%s\n\n' "$BOLD" "$RESET"
  echo "  Update later with:  agentboard update"
  echo "  Uninstall with:     curl -fsSL $SITE/uninstall.sh | bash"
  echo ""
}

main "$@"
