#!/usr/bin/env bash
# SessionStart hook for linux-mentor.
# Runs once when a new Antigravity conversation session starts.
# Working directory is the plugin root (/home/katkarpova/projects/Linux-Mentor).
#
# Input on stdin: JSON with conversationId, workspacePaths, etc.
# Output on stdout: must be a valid JSON object (e.g. {} or {"injectSteps": [...]}).

# TODO: Add your launch command here (redirect its stdout/stderr so stdout stays valid JSON, e.g. my-app >/dev/null 2>&1 &)

printf '{}\n'
