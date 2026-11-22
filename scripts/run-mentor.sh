#!/usr/bin/env bash
# Opens the Linux Mentor window in the background.
set -euo pipefail

here="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd -P)"
gui="${LINUX_MENTOR_GUI:-}"
if [[ -z "$gui" ]]; then
  if command -v linux-mentor-gui >/dev/null 2>&1; then
    gui="$(command -v linux-mentor-gui)"
  elif [[ -x "$here/linux-mentor-gui" ]]; then
    gui="$here/linux-mentor-gui"
  fi
fi
if [[ -z "$gui" || ! -x "$gui" ]]; then
  echo "linux-mentor: the window program was not found. Run scripts/install.sh from the project folder first." >&2
  exit 1
fi

if [[ -z "${DISPLAY:-}" && -z "${WAYLAND_DISPLAY:-}" ]]; then
  echo "linux-mentor: no graphical display found, so the window cannot open here." >&2
  exit 1
fi

state_dir="${XDG_STATE_HOME:-$HOME/.local/state}/linux-mentor"
mkdir -p "$state_dir"
nohup "$gui" >>"$state_dir/gui.log" 2>&1 </dev/null &
echo "linux-mentor: window started (log: $state_dir/gui.log)"