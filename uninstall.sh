#!/usr/bin/env bash
# =============================================================================
# AgentBoard uninstaller
#
#   curl -fsSL https://agentboard.ayande.xyz/uninstall.sh | bash
#
# Removes the agentboard binary. Your boards, config and ticket worktrees in
# ~/.agentboard are kept unless --purge is given.
#
# Flags: --purge (also delete ~/.agentboard), --dry-run
# =============================================================================
set -euo pipefail

if [ -t 1 ]; then
  BLUE=$'\033[1;34m'; YELLOW=$'\033[1;33m'; RED=$'\033[1;31m'; RESET=$'\033[0m'
else
  BLUE=""; YELLOW=""; RED=""; RESET=""
fi
info() { printf '%s==>%s %s\n' "$BLUE" "$RESET" "$*"; }
warn() { printf '%swarning:%s %s\n' "$YELLOW" "$RESET" "$*" >&2; }
err()  { printf '%serror:%s %s\n' "$RED" "$RESET" "$*" >&2; exit 1; }

main() {
  local purge=false dry_run=false arg
  for arg in "$@"; do
    case "$arg" in
      --purge)   purge=true ;;
      --dry-run) dry_run=true ;;
      *) err "unknown flag: $arg (supported: --purge, --dry-run)" ;;
    esac
  done

  local targets=() dir
  for dir in "${AGENTBOARD_INSTALL_DIR:-}" "$HOME/.local/bin" "${PREFIX:-/nonexistent}/bin"; do
    [ -n "$dir" ] && [ -f "$dir/agentboard" ] && targets+=("$dir/agentboard")
  done
  if [ "$purge" = true ] && [ -d "$HOME/.agentboard" ]; then
    targets+=("$HOME/.agentboard")
  fi

  if [ ${#targets[@]} -eq 0 ]; then
    info "nothing to uninstall: no agentboard installation found"
    exit 0
  fi

  info "removing:"
  printf '  - %s\n' "${targets[@]}"
  if [ "$purge" = false ] && [ -d "$HOME/.agentboard" ]; then
    warn "your boards and config in ~/.agentboard are kept; re-run with --purge to delete them"
  fi
  if [ "$dry_run" = true ]; then
    info "dry run: nothing deleted"
    exit 0
  fi

  rm -rf "${targets[@]}"
  info "agentboard uninstalled"
}

main "$@"
