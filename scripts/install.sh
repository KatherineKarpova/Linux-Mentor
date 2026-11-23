#!/usr/bin/env bash
# One-time setup for Linux Mentor: builds the programs, installs them for the
# current user, and registers the plugin with agy. Safe to run again after updates.
set -euo pipefail

repo="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")/.." && pwd -P)"
bin_dir="${LINUX_MENTOR_BIN_DIR:-$HOME/.local/bin}"

say()  { printf 'linux-mentor: %s\n' "$*"; }
fail() { printf 'linux-mentor: %s\n' "$*" >&2; exit 1; }

# 1. tools needed to build
missing=()
for tool in go npm pkg-config; do
  command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
done
if ((${#missing[@]})); then
  fail "missing required tools: ${missing[*]}. Install them, then run this script again."
fi

# 2. GTK and WebKit development files, with a hint matched to the distribution
if ! pkg-config --exists gtk+-3.0 || ! { pkg-config --exists webkit2gtk-4.1 || pkg-config --exists webkit2gtk-4.0; }; then
  . /etc/os-release 2>/dev/null || true
  case "${ID:-} ${ID_LIKE:-}" in
    *debian*|*ubuntu*) hint="sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev" ;;
    *fedora*|*rhel*)   hint="sudo dnf install gtk3-devel webkit2gtk4.1-devel" ;;
    *arch*)            hint="sudo pacman -S gtk3 webkit2gtk-4.1" ;;
    *suse*)            hint="sudo zypper install gtk3-devel webkit2gtk3-devel (package names vary)" ;;
    *)                 hint="install the GTK 3 and WebKit2GTK development packages for your distribution" ;;
  esac
  fail "GTK 3 and WebKit2GTK development files are missing. Try: $hint"
fi

# 3. build
say "building the desktop window (this takes a minute the first time)"
"$repo/scripts/build-desktop.sh"

say "building the MCP server"
(cd "$repo" && go build -o "$repo/build/bin/linux-mentor-mcp" ./cmd/mcp-server)

# 4. install for this user
mkdir -p "$bin_dir"
install -m 0755 "$repo/build/bin/linux-mentor" "$bin_dir/linux-mentor-gui"
install -m 0755 "$repo/build/bin/linux-mentor-mcp" "$bin_dir/linux-mentor-mcp"
install -m 0755 "$repo/scripts/run-mentor.sh" "$bin_dir/run-mentor"

# 5. register the plugin with agy
if command -v agy >/dev/null 2>&1; then
  say "registering the plugin with agy"
  agy plugin install "$repo" || say "plugin install did not complete; if it is already installed, that is fine"
else
  say "agy was not found. After installing Antigravity CLI, run: agy plugin install $repo"
fi

case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) say "$bin_dir is not on your PATH. Add it (for example in ~/.bashrc), then open a new terminal." ;;
esac

say "done. Keep this folder in place: the plugin links back to it."
say "start agy and the window opens with each new conversation, or run: run-mentor"