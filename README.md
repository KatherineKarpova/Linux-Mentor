# Linux-Mentor

An Antigravity CLI plugin that aims to teach Linux troubleshooting and system concepts through Socratic guidance, initially targeting Ubuntu and Fedora.

The current Go MCP server is a connectivity skeleton. An enabled `SessionStart` hook launches the GUI executable at `build/bin/linux-mentor` (or the path in `LINUX_MENTOR_GUI`) and passes Antigravity's conversation ID and transcript path through environment variables.

The GUI and transcript reader are not implemented yet, so assistant responses are not currently displayed outside Antigravity. See [documentation/architecture.txt](documentation/architecture.txt) for the current design, completed decisions, and remaining work.
