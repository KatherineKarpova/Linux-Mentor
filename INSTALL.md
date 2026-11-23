# Installing Linux Mentor

Linux Mentor is a plugin for the Antigravity CLI (`agy`). It opens a small window next to your terminal and helps you learn Linux by asking guiding questions instead of handing you answers.

These steps are for Ubuntu, Fedora, and similar 64-bit Linux systems with a graphical desktop.

## 1. Install the Antigravity CLI

```bash
curl -fsSL https://antigravity.google/cli/install.sh | bash
```

Open a new terminal afterwards so the `agy` command is found. Check it with:

```bash
agy --version
```

## 2. Sign in

Run `agy` once and follow the sign-in prompts in your browser. When you are signed in, quit `agy` (type `/quit` or press Ctrl+D).

## 3. Download Linux Mentor

```bash
mkdir -p ~/.local/share/linux-mentor/current
cd ~/.local/share/linux-mentor
curl -fLO https://github.com/KatherineKarpova/Linux-Mentor/releases/latest/download/linux-mentor-linux-amd64.tar.gz
curl -fLO https://github.com/KatherineKarpova/Linux-Mentor/releases/latest/download/linux-mentor-linux-amd64.tar.gz.sha256
```

On an ARM computer (for example a Raspberry Pi 5), replace `amd64` with `arm64` in both links and in the commands below.

## 4. Check the download

```bash
sha256sum -c linux-mentor-linux-amd64.tar.gz.sha256
```

You should see `linux-mentor-linux-amd64.tar.gz: OK`. If you see `FAILED`, delete both files and download them again.

## 5. Unpack and install

```bash
tar -xzf linux-mentor-linux-amd64.tar.gz -C current --strip-components=1
cd current
./install.sh
```

The installer checks that your system has what the window needs, copies the programs to `~/.local/bin`, and registers the plugin with `agy`. If it tells you a library is missing, it prints the command to install it; run that, then run `./install.sh` again.

Keep the `~/.local/share/linux-mentor/current` folder: the plugin is loaded from there.

## 6. Start it

```bash
agy
```

The Linux Mentor window opens when a new conversation starts. To open the window yourself, run `run-mentor`.

## Updating

Download the new release and install it over the old one:

```bash
cd ~/.local/share/linux-mentor
rm -f linux-mentor-linux-amd64.tar.gz linux-mentor-linux-amd64.tar.gz.sha256
curl -fLO https://github.com/KatherineKarpova/Linux-Mentor/releases/latest/download/linux-mentor-linux-amd64.tar.gz
curl -fLO https://github.com/KatherineKarpova/Linux-Mentor/releases/latest/download/linux-mentor-linux-amd64.tar.gz.sha256
sha256sum -c linux-mentor-linux-amd64.tar.gz.sha256
rm -rf current && mkdir current
tar -xzf linux-mentor-linux-amd64.tar.gz -C current --strip-components=1
cd current && ./install.sh
```

Your progress is kept in `~/.local/state/linux-mentor` and is not touched by updating.

## Uninstalling

```bash
agy plugin uninstall linux-mentor
rm -f ~/.local/bin/linux-mentor-gui ~/.local/bin/linux-mentor-mcp ~/.local/bin/run-mentor
rm -rf ~/.local/share/linux-mentor
```

To also delete your progress and logs:

```bash
rm -rf ~/.local/state/linux-mentor
```

## Troubleshooting

**`agy: command not found`**: open a new terminal. If it still is not found, add `~/.local/bin` to your PATH by adding `export PATH="$HOME/.local/bin:$PATH"` to `~/.bashrc`, then open a new terminal.

**`linux-mentor-mcp` or `run-mentor` not found**: the same fix: make sure `~/.local/bin` is on your PATH.

**The window does not open**: it needs a graphical desktop. Over SSH, on a server, or in WSL without graphics it is skipped on purpose. Check the logs in `~/.local/state/linux-mentor/hook.log` and `gui.log`.

**`python3 is required`**: install it with `sudo apt install python3` (Ubuntu) or `sudo dnf install python3` (Fedora), then run `./install.sh` again.

**`missing libraries`**: run the install command the installer printed (it installs GTK 3 and WebKit2GTK), then run `./install.sh` again.

**`this package is for amd64, but this computer is arm64`** (or the other way round): you downloaded the wrong file. Download the release that matches your computer.

**The plugin does not show up in `agy`**: from `~/.local/share/linux-mentor/current`, run `agy plugin install .` and check it with `agy plugin list`.
