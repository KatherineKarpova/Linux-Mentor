# Linux-Mentor

An Antigravity CLI plugin that teaches Linux troubleshooting and system concepts through Socratic guidance, using the active distribution's installed documentation where available.

The desktop application is a Wails native window with a React and TypeScript interface. Build it on Linux with `./scripts/build-desktop.sh`; the script writes `build/bin/linux-mentor`, which the enabled `SessionStart` hook launches (or set `LINUX_MENTOR_GUI` to override the path). The hook passes Antigravity's conversation ID and transcript path through environment variables.

The GUI reads completed model responses from the active Antigravity transcript and does not display user messages. The small `index.html` in the frontend is only the embedded React entry point for Wails; it is not served as a separate website. See [documentation/architecture.txt](documentation/architecture.txt) for the current design and remaining work.

Learning progress is stored locally in SQLite at `${XDG_STATE_HOME:-~/.local/state}/linux-mentor/progress.db`. The desktop app and MCP server create or update the schema on first launch and share the same per-user database. It stores session metadata, concept progress and attempt history, and interaction preferences; it does not store transcript contents or collect hardware identifiers.

The MCP server also creates `${XDG_DATA_HOME:-~/.local/share}/linux-mentor/docs.db`. On first launch it reads the exact distro ID from `/etc/os-release` and indexes installed man pages and selected text documentation from standard system directories. RAG searches only that distro's indexed section and returns short, source-labeled excerpts; coverage depends on which local documentation is installed.

Run `./scripts/test-progress.sh` in a terminal to exercise scoring and SQLite persistence using an isolated temporary database. It stays available while the script waits; only a keyboard key triggers its deletion, not a mouse click.
