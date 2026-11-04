# Linux-Mentor

An Antigravity CLI plugin that aims to teach Linux troubleshooting and system concepts through Socratic guidance, initially targeting Ubuntu and Fedora.

The desktop application is a Wails native window with a React and TypeScript interface. Build it on Linux with `./scripts/build-desktop.sh`; the script writes `build/bin/linux-mentor`, which the enabled `SessionStart` hook launches (or set `LINUX_MENTOR_GUI` to override the path). The hook passes Antigravity's conversation ID and transcript path through environment variables.

The GUI reads completed model responses from the active Antigravity transcript and does not display user messages. The small `index.html` in the frontend is only the embedded React entry point for Wails; it is not served as a separate website. See [documentation/architecture.txt](documentation/architecture.txt) for the current design and remaining work.
