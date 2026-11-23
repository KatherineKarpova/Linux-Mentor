# Installing Linux Mentor

Linux Mentor is a plugin for the Antigravity CLI (`agy`). It opens a small window next to your terminal and helps you learn Linux by asking guiding questions instead of handing you answers.

## What you need

- 64-bit Linux with a desktop: **Ubuntu 22.04 or newer**, Linux Mint 21+, Debian 12+, Fedora 36+, Arch, or similar.
- Works in VirtualBox or VMware, and in WSL on Windows 11.
- Intel/AMD computers use the **amd64** download. ARM computers (Apple Silicon Macs running a Linux VM, Raspberry Pi 5) use **arm64**. Not sure? Run `uname -m`: `x86_64` means amd64, `aarch64` means arm64.

### Not supported

- **Older releases**: Ubuntu 20.04, Debian 11, RHEL/Rocky/Alma 8 and 9. Their system libraries are too old.
- **Alpine, Void (musl), and other musl-based distros**: the window needs glibc.
- **NixOS**: downloaded programs don't run without extra setup (for example `nix-ld`).
- **Immutable distros** (Fedora Silverblue, Bazzite, and similar): the installer can't add the libraries the window needs. Use a Distrobox container running Ubuntu or Fedora, or layer the library yourself with `rpm-ostree install webkit2gtk4.1` and reboot.
- **32-bit systems and other processors** (32-bit Raspberry Pi OS, RISC-V): only amd64 and arm64 are built.
- **No desktop** (servers, SSH sessions): the mentor still works inside `agy`, but the window can't open.

## Install

**1. Install the Antigravity CLI and sign in.**

```bash
curl -fsSL https://antigravity.google/cli/install.sh | bash
```

Open a new terminal, run `agy`, follow the sign-in steps in your browser, then quit `agy` (Ctrl+D).

**2. Download Linux Mentor** from the [latest release](https://github.com/KatherineKarpova/Linux-Mentor/releases/latest) (`linux-mentor-linux-amd64.zip`) and unzip it. Double-clicking it in your file manager works, or in a terminal:

```bash
cd ~/Downloads
unzip linux-mentor-linux-amd64.zip
```

**3. Run the installer.**

```bash
bash linux-mentor/install.sh
```

If the window's libraries (GTK and WebKit) are missing, the installer offers to install them, which asks for your password. If you skipped step 1, it offers to install `agy` too.

**4. Use it.** Open a new terminal and run `agy`. The mentor window opens with each new conversation. To open the window yourself at any time, run:

```bash
run-mentor
```

You can delete the downloaded zip and folder now. Linux Mentor is installed in `~/.local/share/linux-mentor`.

## Updating

Download and unzip the new release, then run its installer again. Your progress is kept.

```bash
bash linux-mentor/install.sh
```

## Uninstalling

```bash
~/.local/share/linux-mentor/app/uninstall.sh
```

Add `--purge` to also delete your learning progress and the documentation index.

## Checking the download (optional)

Each download has a `.sha256` file next to it on the release page. Save it in the same folder, then run:

```bash
sha256sum -c linux-mentor-linux-amd64.zip.sha256
```

It should print `OK`.

## Troubleshooting

**`run-mentor: command not found` or `agy: command not found`**: open a new terminal. The installer adds `~/.local/bin` to your PATH in `~/.bashrc`, and only new terminals see it.

**`unzip: command not found`**: run `sudo apt install unzip` (Ubuntu) or `sudo dnf install unzip` (Fedora), or unzip it from your file manager instead.

**The window is blank or white (common in VirtualBox)**: `run-mentor` and the session hook already switch WebKit to a safe renderer in virtual machines. If the window is still blank, turn off **3D acceleration** in the VM's Display settings, or run:

```bash
WEBKIT_DISABLE_DMABUF_RENDERER=1 run-mentor
```

**The window does not open at all**: it needs a graphical desktop. Over SSH or on a server it is skipped on purpose. On Windows, use WSL on Windows 11, which includes graphics support. The logs are in `~/.local/state/linux-mentor/` (`hook.log` and `gui.log`).

**`this package is for amd64, but this computer is arm64`** (or the other way round): download the other zip.

**`GLIBC_2.xx not found` or a library error on start**: your Linux is older than Ubuntu 22.04. Upgrade, or use a newer distribution in a VM.

**The mentor does not show up in `agy`**: run the installer again, then check with `agy plugin list`.

## Building from source

For working on Linux Mentor itself. You need `go`, `npm`, `pkg-config`, `python3` and the GTK 3 and WebKit2GTK 4.1 development packages (`sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev` on Ubuntu).

```bash
git clone https://github.com/KatherineKarpova/Linux-Mentor.git
cd Linux-Mentor
scripts/install-from-source.sh
```

This registers the plugin straight from the checkout, so keep the folder.
