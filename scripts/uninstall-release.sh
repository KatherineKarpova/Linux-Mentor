#!/usr/bin/env bash
# Removes Linux Mentor. Usage: uninstall.sh [--purge]
# --purge also deletes your learning progress and the documentation index.
set -euo pipefail

home_dir="${LINUX_MENTOR_HOME:-$HOME/.local/share/linux-mentor}"
bin_dir="${LINUX_MENTOR_BIN_DIR:-$HOME/.local/bin}"
state_dir="${XDG_STATE_HOME:-$HOME/.local/state}/linux-mentor"
data_dir="${XDG_DATA_HOME:-$HOME/.local/share}/linux-mentor"

say() { printf 'linux-mentor: %s\n' "$*"; }

if command -v agy >/dev/null 2>&1; then
  agy plugin uninstall linux-mentor >/dev/null 2>&1 && say "removed the plugin from agy" || true
fi

rm -f "$bin_dir/linux-mentor-gui" "$bin_dir/linux-mentor-mcp" "$bin_dir/run-mentor"
rm -rf "$home_dir/app"
# the PATH line the installer may have added stays: agy lives in the same folder.
say "removed the programs"

if [[ "${1:-}" == "--purge" ]]; then
  rm -rf "$state_dir" "$data_dir"
  say "removed your progress and the documentation index"
else
  say "your progress is kept in $state_dir (run with --purge to delete it too)"
fi
