#!/usr/bin/env bash
# Opens the Linux Mentor window in the background.
set -euo pipefail

here="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd -P)"
gui="${LINUX_MENTOR_GUI:-}"
if [[ -z "$gui" ]]; then
  if command -v linux-mentor-gui >/dev/null 2>&1; then
    gui="$(command -v linux-mentor-gui)"
  else
    for candidate in \
      "$here/linux-mentor-gui" \
      "$here/../build/bin/linux-mentor"; do
      if [[ -x "$candidate" ]]; then
        gui="$candidate"
        break
      fi
    done
  fi
fi
if [[ -z "$gui" || ! -x "$gui" ]]; then
  echo "linux-mentor: the window program was not found. Run install.sh from the release (or scripts/build-desktop.sh in a source checkout)." >&2
  exit 1
fi

if [[ -z "${DISPLAY:-}" && -z "${WAYLAND_DISPLAY:-}" ]]; then
  echo "linux-mentor: no graphical display found, so the window cannot open here." >&2
  exit 1
fi

state_dir="${XDG_STATE_HOME:-$HOME/.local/state}/linux-mentor"
mkdir -p "$state_dir"
# WebKit often draws a blank window inside virtual machines (VirtualBox, VMware);
# its DMA-BUF renderer needs GPU features those virtual graphics cards lack.
if [[ -z "${WEBKIT_DISABLE_DMABUF_RENDERER:-}" ]] && systemd-detect-virt --vm --quiet 2>/dev/null; then
  export WEBKIT_DISABLE_DMABUF_RENDERER=1
fi

nohup "$gui" >>"$state_dir/gui.log" 2>&1 </dev/null &
echo "linux-mentor: window started (log: $state_dir/gui.log)"