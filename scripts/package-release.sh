#!/usr/bin/env bash
# Builds the release archives for this computer's architecture.
# Usage: scripts/package-release.sh v0.2.0
#
# Writes dist/linux-mentor-linux-<arch>.zip and .tar.gz, each with a .sha256 file.
# Both unpack to a single linux-mentor/ folder. Build on the oldest distro you want
# to support (the release workflow uses Ubuntu 22.04): the window links against the
# build machine's GTK and WebKit, and runs only where those are the same or newer.
set -euo pipefail

repo="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")/.." && pwd -P)"
version="${1:-$(git -C "$repo" describe --tags --always --dirty)}"

case "$(uname -m)" in
  x86_64)  arch=amd64 ;;
  aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

command -v python3 >/dev/null 2>&1 || { echo "python3 is required to build the zip" >&2; exit 1; }

base="linux-mentor-linux-${arch}"
stage="$repo/dist/stage-$arch"
out="$stage/linux-mentor"
rm -rf "$stage"
mkdir -p "$out/bin" "$out/scripts" "$out/rules"

# the desktop window (needs cgo, so it must be built natively for each architecture)
"$repo/scripts/build-desktop.sh"
cp "$repo/build/bin/linux-mentor" "$out/bin/linux-mentor-gui"

# the MCP server (pure Go, including SQLite, so a static binary that runs anywhere)
(cd "$repo" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$out/bin/linux-mentor-mcp" ./cmd/mcp-server)

# plugin files
cp "$repo/plugin.json" "$repo/hooks.json" "$repo/mcp_config.json" "$repo/LINUXMENTOR.md" "$repo/README.md" "$repo/INSTALL.md" "$out/"
cp "$repo/rules/AGENTS.md" "$out/rules/"
[ -f "$repo/LICENSE" ] && cp "$repo/LICENSE" "$out/"
cp "$repo/scripts/on-session-start.sh" "$repo/scripts/run-mentor.sh" "$out/scripts/"
[ -f "$repo/scripts/allow-tools.sh" ] && cp "$repo/scripts/allow-tools.sh" "$out/scripts/"
cp "$repo/scripts/install-release.sh" "$out/install.sh"
cp "$repo/scripts/uninstall-release.sh" "$out/uninstall.sh"

echo "$arch" > "$out/ARCH"
echo "$version" > "$out/VERSION"
chmod +x "$out"/bin/* "$out"/scripts/*.sh "$out/install.sh" "$out/uninstall.sh"

cd "$repo/dist"
rm -f "$base".zip "$base".tar.gz "$base".zip.sha256 "$base".tar.gz.sha256
tar -C "$stage" -czf "$base.tar.gz" linux-mentor

# zip with unix permissions kept, so ./install.sh stays executable after unzipping
python3 -I - "$stage" "$base.zip" <<'PY'
import os, sys, zipfile
root, dest = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(dest, "w", zipfile.ZIP_DEFLATED) as z:
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames.sort()
        for name in sorted(filenames):
            path = os.path.join(dirpath, name)
            info = zipfile.ZipInfo.from_file(path, os.path.relpath(path, root))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.create_system = 3  # unix, so unzip restores the permission bits
            with open(path, "rb") as f:
                z.writestr(info, f.read())
PY

sha256sum "$base.zip" > "$base.zip.sha256"
sha256sum "$base.tar.gz" > "$base.tar.gz.sha256"
rm -rf "$stage"
echo "built: $repo/dist/$base.zip"
echo "built: $repo/dist/$base.tar.gz"
