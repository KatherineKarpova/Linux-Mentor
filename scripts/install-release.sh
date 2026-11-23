#!/usr/bin/env bash
# Installs Linux Mentor from an unpacked release. Safe to run again.
set -euo pipefail

pkg="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd -P)"
bin_dir="${LINUX_MENTOR_BIN_DIR:-$HOME/.local/bin}"

say()  { printf 'linux-mentor: %s\n' "$*"; }
fail() { printf 'linux-mentor: %s\n' "$*" >&2; exit 1; }

[[ "$(uname -s)" == "Linux" ]] || fail "Linux Mentor runs on Linux only."
command -v python3 >/dev/null 2>&1 || fail "python3 is required (the session hook uses it). Install it, for example: sudo apt install python3 (Ubuntu) or sudo dnf install python3 (Fedora), then run this again."

want="$(cat "$pkg/ARCH" 2>/dev/null || echo unknown)"
case "$(uname -m)" in x86_64) have=amd64 ;; aarch64) have=arm64 ;; *) have=unknown ;; esac
[[ "$want" == "$have" ]] || fail "this package is for $want, but this computer is $have. Download the matching release."

chmod +x "$pkg"/bin/* "$pkg"/scripts/*.sh

# check that the window's runtime libraries exist
missing="$(ldd "$pkg/bin/linux-mentor-gui" 2>/dev/null | grep 'not found' || true)"
if [[ -n "$missing" ]]; then
  . /etc/os-release 2>/dev/null || true
  case "${ID:-} ${ID_LIKE:-}" in
    *debian*|*ubuntu*) hint="sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0" ;;
    *fedora*|*rhel*)   hint="sudo dnf install gtk3 webkit2gtk4.1" ;;
    *arch*)            hint="sudo pacman -S gtk3 webkit2gtk-4.1" ;;
    *)                 hint="install the GTK 3 and WebKit2GTK (4.1) runtime packages for your distribution" ;;
  esac
  printf '%s\n' "$missing" >&2
  fail "missing libraries (listed above). Try: $hint"
fi

mkdir -p "$bin_dir"
install -m 0755 "$pkg/bin/linux-mentor-gui" "$bin_dir/linux-mentor-gui"
install -m 0755 "$pkg/bin/linux-mentor-mcp" "$bin_dir/linux-mentor-mcp"
install -m 0755 "$pkg/scripts/run-mentor.sh" "$bin_dir/run-mentor"

case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) say "$bin_dir is not on your PATH. Add it (for example in ~/.bashrc), then open a new terminal." ;;
esac

if command -v agy >/dev/null 2>&1; then
  say "registering the plugin with agy"
  agy plugin install "$pkg" || say "plugin install did not complete; if it is already installed, that is fine"
else
  say "agy was not found. After installing Antigravity CLI, run: agy plugin install $pkg"
fi

say "done. Keep this folder in place: the plugin links back to it."
say "start agy and the window opens with each new conversation, or run: run-mentor"
if [[ -f "$pkg/scripts/allow-tools.sh" ]]; then
  say "to skip tool-approval prompts, see $pkg/scripts/allow-tools.sh"
fi