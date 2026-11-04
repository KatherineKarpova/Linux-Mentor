#!/usr/bin/env bash
# sessionstart hook for linux-mentor.
set -euo pipefail

# resolve paths relative to this script instead of the host's current directory.
plugin_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
gui="${LINUX_MENTOR_GUI:-$plugin_root/build/bin/linux-mentor}"
if [[ "$gui" != /* ]]; then
  gui="$plugin_root/$gui"
fi

# read the hook payload once; stdout is reserved for the hook's json response.
hook_input="$(cat)"
if ! hook_context="$(python3 -c '
import json
import os
import sys

payload = json.load(sys.stdin)
conversation_id = payload.get("conversationId")
transcript_path = payload.get("transcriptPath")
if not isinstance(conversation_id, str) or not conversation_id:
    raise SystemExit("missing conversationId in SessionStart payload")
if not isinstance(transcript_path, str) or not transcript_path:
    raise SystemExit("missing transcriptPath in SessionStart payload")
transcript_path = os.path.abspath(os.path.expanduser(transcript_path))
if "\n" in conversation_id or "\n" in transcript_path:
    raise SystemExit("newline in SessionStart context is not supported")
print(conversation_id)
print(transcript_path)
' <<<"$hook_input")"; then
  printf 'linux-mentor: unable to read Antigravity SessionStart context\n' >&2
  printf '{}\n'
  exit 1
fi

# split the validated fields for the gui process environment.
mapfile -t hook_fields <<<"$hook_context"
conversation_id="${hook_fields[0]}"
transcript_path="${hook_fields[1]}"

# fail clearly instead of pretending the ui started when it has not been built.
if [[ ! -x "$gui" ]]; then
  printf 'linux-mentor: GUI executable not found: %s\n' "$gui" >&2
  printf '{}\n'
  exit 1
fi

# keep the detached ui's logs out of the hook's json output.
state_dir="${XDG_STATE_HOME:-$HOME/.local/state}/linux-mentor"
if ! mkdir -p "$state_dir"; then
  printf 'linux-mentor: cannot create state directory: %s\n' "$state_dir" >&2
  printf '{}\n'
  exit 1
fi

# pass read-only conversation context for the future transcript reader.
LINUX_MENTOR_CONVERSATION_ID="$conversation_id" \
LINUX_MENTOR_TRANSCRIPT_PATH="$transcript_path" \
  nohup "$gui" >>"$state_dir/gui.log" 2>&1 </dev/null &

printf '{}\n'
