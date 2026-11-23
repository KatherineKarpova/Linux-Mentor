#!/usr/bin/env bash
# Builds a release archive. Usage: scripts/package-release.sh v0.2.0
set -euo pipefail

repo="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")/.." && pwd -P)"
version="${1:-$(git -C "$repo" describe --tags --always --dirty)}"

case "$(uname -m)" in
  x86_64)  arch=amd64 ;;
  aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

name="linux-mentor-${version}-linux-${arch}"
out="$repo/dist/$name"
rm -rf "$out"
mkdir -p "$out/bin" "$out/scripts" "$out/rules"

# the desktop window (needs cgo, so it must be built natively for each architecture)
"$repo/scripts/build-desktop.sh"
cp "$repo/build/bin/linux-mentor" "$out/bin/linux-mentor-gui"

# the MCP server (needs cgo for github.com/mattn/go-sqlite3)
(cd "$repo" && CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o "$out/bin/linux-mentor-mcp" ./cmd/mcp-server)

# plugin files
cp "$repo/plugin.json" "$repo/hooks.json" "$repo/mcp_config.json" "$repo/LINUXMENTOR.md" "$repo/README.md" "$repo/INSTALL.md" "$out/"
cp "$repo/rules/AGENTS.md" "$out/rules/"
[ -f "$repo/LICENSE" ] && cp "$repo/LICENSE" "$out/"
cp "$repo/scripts/on-session-start.sh" "$repo/scripts/run-mentor.sh" "$out/scripts/"
[ -f "$repo/scripts/allow-tools.sh" ] && cp "$repo/scripts/allow-tools.sh" "$out/scripts/"
cp "$repo/scripts/install-release.sh" "$out/install.sh"

echo "$arch" > "$out/ARCH"
echo "$version" > "$out/VERSION"
chmod +x "$out"/bin/* "$out"/scripts/*.sh "$out/install.sh"

(cd "$repo/dist" && tar -czf "$name.tar.gz" "$name" && sha256sum "$name.tar.gz" > "$name.tar.gz.sha256")

# a copy without the version in its name, so releases/latest/download/... links keep working
latest="linux-mentor-linux-${arch}.tar.gz"
(cd "$repo/dist" && cp "$name.tar.gz" "$latest" && sha256sum "$latest" > "$latest.sha256")
echo "built: $repo/dist/$name.tar.gz"
echo "built: $repo/dist/$latest"