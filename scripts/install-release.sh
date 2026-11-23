#!/usr/bin/env bash
# Installs Linux Mentor from an unpacked release. Safe to run again (also used to update).
#
# Copies the package to ~/.local/share/linux-mentor/app, puts linux-mentor-gui,
# linux-mentor-mcp and run-mentor in ~/.local/bin, and registers the plugin with agy.
# The unpacked folder can be deleted afterwards.
set -euo pipefail

pkg="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd -P)"
home_dir="${LINUX_MENTOR_HOME:-$HOME/.local/share/linux-mentor}"
dest="$home_dir/app"
bin_dir="${LINUX_MENTOR_BIN_DIR:-$HOME/.local/bin}"

say()  { printf 'linux-mentor: %s\n' "$*"; }
fail() { printf 'linux-mentor: %s\n' "$*" >&2; exit 1; }

# ask a yes/no question; the default is yes. Without a terminal, the answer is no.
ask() {
  [[ -t 0 ]] || return 1
  local reply
  read -r -p "linux-mentor: $1 [Y/n] " reply || return 1
  [[ -z "$reply" || "$reply" =~ ^[Yy] ]]
}

# 1. basic checks
[[ "$(uname -s)" == "Linux" ]] || fail "Linux Mentor runs on Linux only."

want="$(cat "$pkg/ARCH" 2>/dev/null || echo unknown)"
case "$(uname -m)" in x86_64) have=amd64 ;; aarch64) have=arm64 ;; *) have=unknown ;; esac
[[ "$want" == "$have" ]] || fail "this package is for $want, but this computer is $have. Download linux-mentor-linux-$have.zip instead."

. /etc/os-release 2>/dev/null || true
distro="${ID:-} ${ID_LIKE:-}"

command -v python3 >/dev/null 2>&1 || {
  case "$distro" in
    *debian*|*ubuntu*) hint="sudo apt install python3" ;;
    *fedora*|*rhel*)   hint="sudo dnf install python3" ;;
    *arch*)            hint="sudo pacman -S python" ;;
    *)                 hint="install python3 with your package manager" ;;
  esac
  fail "python3 is required. Run: $hint   then run this installer again."
}

chmod +x "$pkg"/bin/* "$pkg"/scripts/*.sh

# 2. the window's runtime libraries (GTK 3 and WebKit2GTK 4.1)
missing_libs() { ldd "$pkg/bin/linux-mentor-gui" 2>/dev/null | grep 'not found' || true; }
missing="$(missing_libs)"
if [[ -n "$missing" ]]; then
  case "$distro" in
    *debian*|*ubuntu*) cmd="sudo apt-get install -y libgtk-3-0 libwebkit2gtk-4.1-0" ;;
    *fedora*|*rhel*)   cmd="sudo dnf install -y gtk3 webkit2gtk4.1" ;;
    *arch*)            cmd="sudo pacman -S --needed gtk3 webkit2gtk-4.1" ;;
    *suse*)            cmd="sudo zypper install -y libgtk-3-0 libwebkit2gtk-4_1-0" ;;
    *)                 cmd="" ;;
  esac
  say "the window needs GTK 3 and WebKit2GTK 4.1, which are not installed yet."
  if [[ -n "$cmd" ]] && ask "install them now with: $cmd ?"; then
    $cmd || fail "installing the libraries failed. Run '$cmd' yourself, then run this installer again."
    missing="$(missing_libs)"
  fi
  if [[ -n "$missing" ]]; then
    printf '%s\n' "$missing" >&2
    if [[ -n "$cmd" ]]; then
      fail "missing libraries (listed above). Run: $cmd   then run this installer again."
    fi
    fail "missing libraries (listed above). Install the GTK 3 and WebKit2GTK 4.1 packages for your distribution, then run this installer again."
  fi
fi

# 3. copy the package to a fixed place, so the download folder can be deleted
case "$pkg/" in
  "$dest/"*) ;;  # already running from the installed copy
  *)
    mkdir -p "$home_dir"
    rm -rf "$dest.new" "$dest.old"
    cp -a "$pkg" "$dest.new"
    [[ -e "$dest" ]] && mv "$dest" "$dest.old"
    mv "$dest.new" "$dest"
    rm -rf "$dest.old"
    ;;
esac

mkdir -p "$bin_dir"
install -m 0755 "$dest/bin/linux-mentor-gui" "$bin_dir/linux-mentor-gui"
install -m 0755 "$dest/bin/linux-mentor-mcp" "$bin_dir/linux-mentor-mcp"
install -m 0755 "$dest/scripts/run-mentor.sh" "$bin_dir/run-mentor"

# 4. make sure new terminals find run-mentor and agy
path_changed=""
case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *)
    line="export PATH=\"$bin_dir:\$PATH\"  # added by linux-mentor"
    for rc in "$HOME/.bashrc" "$HOME/.zshrc"; do
      [[ "$rc" == "$HOME/.bashrc" || -f "$rc" ]] || continue
      grep -qF "# added by linux-mentor" "$rc" 2>/dev/null || printf '\n%s\n' "$line" >> "$rc"
    done
    export PATH="$bin_dir:$PATH"
    path_changed=1
    ;;
esac

# 5. the Antigravity CLI
if ! command -v agy >/dev/null 2>&1; then
  say "the Antigravity CLI (agy) is not installed."
  if ask "install it now from https://antigravity.google/cli/install.sh ?"; then
    curl -fsSL https://antigravity.google/cli/install.sh | bash || say "the agy installer did not finish."
    hash -r
  fi
fi

if command -v agy >/dev/null 2>&1; then
  say "registering the plugin with agy"
  agy plugin uninstall linux-mentor >/dev/null 2>&1 || true
  agy plugin install "$dest" >/dev/null || fail "agy could not register the plugin. Try: agy plugin install $dest"
  registered=1
else
  registered=""
fi

# 6. done
echo
say "installed $(cat "$dest/VERSION" 2>/dev/null || echo) to $dest"
[[ -n "$path_changed" ]] && say "open a new terminal first (so the commands below are found)."
if [[ -z "$registered" ]]; then
  say "next: install agy (curl -fsSL https://antigravity.google/cli/install.sh | bash),"
  say "      then run this installer again."
else
  say "next: run 'agy' and sign in if you have not yet. The mentor window opens with each conversation."
  say "      To open the window yourself at any time, run: run-mentor"
fi
say "you can delete the downloaded folder now. To uninstall later: $dest/uninstall.sh"
if [[ -f "$dest/scripts/allow-tools.sh" ]]; then
  say "to skip tool-approval prompts, see $dest/scripts/allow-tools.sh"
fi
