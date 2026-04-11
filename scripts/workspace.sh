#!/usr/bin/env bash

set -euo pipefail

if [[ ${1:-} == "-h" || ${1:-} == "--help" ]]; then
  printf 'usage: %s [session-name]\n' "${0##*/}"
  printf 'env: AGENT_DECK_PROFILE=<profile>\n'
  exit 0
fi

repo_root=$(git rev-parse --show-toplevel 2>/dev/null)
session_name=${1:-$(basename "$repo_root")}
launcher=${AGENT_HUB_TMUX_BOOTSTRAP:-$HOME/.agents/scripts/bootstrap_tmux_workspace.sh}

if [[ ! -x "$launcher" ]]; then
  printf 'workspace launcher not found or not executable: %s\n' "$launcher" >&2
  printf 'expected from shared hub setup under ~/.agents/scripts\n' >&2
  exit 1
fi

exec "$launcher" "$repo_root" "$session_name"
