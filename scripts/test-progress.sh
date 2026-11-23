#!/usr/bin/env bash
set -euo pipefail

plugin_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/linux-mentor-progress-test.XXXXXX")"
database_path="$test_dir/progress.db"

cd "$plugin_root"
printf 'Using isolated test database: %s\n' "$database_path"
if ! go run ./cmd/progress-test -db "$database_path"; then
  printf 'The test database was kept at %s\n' "$database_path" >&2
  exit 1
fi

if [[ ! -t 0 ]]; then
  printf 'Run this script in a terminal to remove the test database with a key press.\n'
  printf 'The database remains at %s\n' "$database_path"
  exit 1
fi

printf '\nPress any keyboard key to delete this temporary database. Clicking alone will not delete it.\n'
if IFS= read -r -n 1 -s _; then
  rm -f -- "$database_path" "$database_path-wal" "$database_path-shm" "$database_path-journal"
  rmdir -- "$test_dir"
  printf 'Temporary test database deleted.\n'
else
  printf '\nNo key was received; the database remains at %s\n' "$database_path"
fi
