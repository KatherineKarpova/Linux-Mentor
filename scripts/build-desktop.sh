#!/usr/bin/env bash
set -euo pipefail

plugin_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
frontend_dir="$plugin_root/cmd/desktop/frontend"
output="$plugin_root/build/bin/linux-mentor"

if ! command -v go >/dev/null 2>&1; then
  printf 'linux-mentor: go is required to build the desktop application\n' >&2
  exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
  printf 'linux-mentor: npm is required to build the desktop application\n' >&2
  exit 1
fi

cd "$frontend_dir"
npm ci
npm run build

mkdir -p "$(dirname "$output")"
cd "$plugin_root"

# wails needs these build tags; a plain `go build` produces a binary that won't start.
tags="desktop,production"
if pkg-config --exists webkit2gtk-4.1 && ! pkg-config --exists webkit2gtk-4.0; then
  tags="$tags,webkit2_41"
fi
go build -tags "$tags" -o "$output" ./cmd/desktop
printf 'linux-mentor: desktop application built at %s (tags: %s)\n' "$output" "$tags"
