# GODsend 360

GODsend 360 is a local-network game management system for Xbox 360 consoles running the Aurora dashboard. It consists of three parts:

- **Go backend** — HTTP server running on your PC that fetches games from Minerva Archive (via BitTorrent) or Internet Archive, converts ISOs to GOD format, and transfers them to the Xbox via FTP
- **Electron app** — Windows/macOS/Linux desktop tray application wrapping the backend with a live terminal and settings UI
- **Aurora Lua script** — runs on the Xbox and talks to the backend to browse, trigger downloads, and track progress

Download priority for online libraries: **Local Transfer folder → Minerva Archive → Internet Archive**. An Internet Archive account is only needed if Minerva doesn't have the title you want.

---

## Table of Contents

- [Quick Installation](#quick-installation)
- [Running Without the Desktop App](#running-without-the-desktop-app)
- [Features](#features)
- [How it works](#how-it-works)
- [Building & repo structure](#building--repository-structure)
- [Setup options](#setup-options)
- [Configuration](#configuration)
- [Installing on the Xbox](#installing-on-the-xbox)
- [API, runtime folders & environment variables](#backend-http-api-runtime-folders--environment-variables)
- [Requirements](#requirements)
- [Additional documentation & troubleshooting](#additional-documentation--troubleshooting)

---

## Quick Installation

### 1. Download the installer

Go to the [GODsend 360 v2.7.0 release](https://gitgud.io/ghosty99/godsend-360/-/releases/v2.7.0) and download the build for your platform (direct links below match the release assets):

| Platform | File |
|---|---|
| **Windows (x64, backend binary)** | [`godsend.exe`](https://gitgud.io/-/project/46780/uploads/7d70323bd36df2671077bca6488e8df2/godsend.exe) |
| **macOS (Apple Silicon)** | [`godsend-2.7.0-arm64.dmg`](https://gitgud.io/-/project/46780/uploads/4f64c2ac45cebac3bf67762cfed9151c/godsend-2.7.0-arm64.dmg) |
| **macOS (Intel)** | [`godsend-2.7.0-x64.dmg`](https://gitgud.io/-/project/46780/uploads/17f09c725a049c7dbf48b56b5f8e130a/godsend-2.7.0-x64.dmg) |
| **Linux (x64 / amd64)** | [`godsend-2.7.0-x86_64.AppImage`](https://gitgud.io/-/project/46780/uploads/3820294f50767e4ceba2b5a4b77b26a0/godsend-2.7.0-x86_64.AppImage) |
| **Linux (arm64)** | [`godsend-2.7.0-arm64.AppImage`](https://gitgud.io/-/project/46780/uploads/6c8117f892126417905f45e419783620/godsend-2.7.0-arm64.AppImage) |

> **Windows:** the published artifact is the **standalone Go backend** only. For the Electron tray app on Windows, run `npm run build:win` locally to produce the NSIS installer.

### 2. Install and launch

1. **macOS:** open the `.dmg` and drag **GODsend** to Applications. **Linux:** `chmod +x` the `.AppImage` and run it. **Windows:** run `godsend.exe` from a folder of your choice (no installer in this release).
2. **macOS / Linux (desktop builds):** launch **GODsend** from Applications or your app launcher — the tray icon appears (Linux depends on desktop environment). **Windows (`godsend.exe`):** run the binary from a terminal or Explorer; keep the process running while you use Aurora, or run it as a background service (see [headless setup](docs/headless-setup.md)).

For Linux distro-specific run notes (Ubuntu/Debian/Fedora/Arch), see **Linux runtime notes** in the setup section below.

> **macOS first launch:** the app is not notarized with Apple, so Gatekeeper will block it the first time with *"Apple could not verify GODsend.app is free of malware"*. To allow it:
>
> 1. Click **Done** on the dialog (do **not** click "Move to Bin").
> 2. Open **System Settings → Privacy & Security** and scroll to the bottom.
> 3. You should see *"GODsend.app was blocked..."* — click **Open Anyway**.
> 4. Launch **GODsend** again and confirm at the prompt.
>
> You only need to do this once.

### 3. Configure download sources (optional)

Minerva Archive works out of the box with no account required — most games download immediately via BitTorrent.

If you want Internet Archive as a fallback (or for titles not on Minerva):

1. Click the tray icon and open the app window, then click the **⚙ Settings** button.
2. Under **Internet Archive account**, click **Log in** and enter your [archive.org](https://archive.org) credentials. Your session cookie is stored locally — your password is never saved.
3. Set **Parallel download connections** to your preferred value (default **5**, range 1–7).

You can also set a **Local Transfer folder** if you want to install from `.iso` files you already have on your PC.

### 4. Install Aurora scripts on the Xbox

The Aurora scripts are bundled with the installer. The easiest way to install them is via the app:

1. Enable Aurora's FTP server: **Aurora → Settings → Network → Enable FTP**.
2. In the GODsend app, open **⚙ Settings** → set **Backend server port** (if not using the default) and then scroll to **Xbox connection**.
3. Enter your **Xbox IP address** and click **FTP Aurora Scripts to Xbox**.
4. The scripts are uploaded to the path you set (default `Hdd1:\Aurora\User\Scripts\Utility\GODSend\`; on USB FTP often shows `Usb0:\Apps\Aurora\User\Scripts\Utility\GODSend\`), and `state.lua` is patched with your PC's IP + backend port.
5. Launch **GODsend** from Aurora → Scripts.

Alternatively, copy the `aurora-scripts/` folder from the GODsend install directory to the Xbox manually via FTP, then edit `state.lua` to set `BRAIN_IP` and `PORT`.

The Xbox will now connect to the backend running on your PC. You can browse games, trigger downloads, and track progress directly from Aurora.

---

## Running Without the Desktop App

The Go backend works as a standalone headless server — no Electron, no GUI, no display required. Useful for always-on home servers, NAS boxes, Raspberry Pi, or Docker containers.

Download a prebuilt binary from the [latest release](https://gitgud.io/ghosty99/godsend-360/-/releases), or build from source with Go 1.21+. Configure via environment variables, run the binary, and point your Xbox at it.

**[Full headless setup guide (build, configure, systemd/launchd service, Xbox pairing)](docs/headless-setup.md)**

---

## Features

Minerva Archive (BitTorrent, no account), Internet Archive (parallel HTTP, optional), local ISOs, XBLA, DLC, XBLIG, Game Archives, Retro ROMs (62 systems via EdgeEmu), multi-disc support, GOD/XEX/content install layouts, HTTP and FTP transfer modes, server queue management, and persistent logging.

**[Full feature list with details on each capability](docs/features.md)**

---

## How it works

```
[Xbox Aurora script] ──HTTP──▶ [Go backend on PC] ──FTP──▶ [Xbox HDD/USB]
                                      │
                          ┌───────────┴────────────┐
                    Minerva Archive           Internet Archive
                    (BitTorrent via           (parallel HTTP,
                     aria2c)                   optional account)
                          │
                   Local Transfer folder
                   (your own ISOs, highest priority)
```

1. The Aurora script on the Xbox browses game libraries — lists are sourced from Minerva Archive or Internet Archive metadata
2. The user selects a title and a source; the script sends a trigger request to the Go backend
3. The backend checks for a local ISO first, then downloads from Minerva Archive via BitTorrent (no account needed) or falls back to Internet Archive (parallel range requests, 1–7 workers, account required)
4. For disc ISOs the backend converts to Games on Demand format using a pure Go implementation; XBLA/digital titles are extracted natively — no external tools required
5. The finished game files are transferred to the Xbox over FTP using Aurora's built-in FTP server
6. The Aurora script polls the backend for status and shows a live progress display; the game appears in Aurora when the transfer completes

---

## Building & repository structure

Requires **Go 1.21+** and **Node.js 18+**. Quick start: `npm install && npm run build`. Backend only: `npm run build:server:all`.

**[Full build instructions, npm scripts, and repo layout](docs/building.md)**

---

## Setup options

You can run GODsend in two main ways:

- **Full desktop experience (recommended)** — Electron tray app + bundled backend. See [Quick Installation](#quick-installation) above.
- **Backend-only (headless)** — Run the Go server standalone on any machine with no GUI. See [Running Without the Desktop App](#running-without-the-desktop-app) above.

In both modes, the Aurora script setup is the same: copy `aurora-scripts/` to the Xbox, set `BRAIN_IP` and `PORT` in `state.lua` to the PC host/port running the backend, and enable Aurora’s FTP server.

### Linux runtime notes (different distros)

Use the AppImage that matches your CPU (`x64`/`amd64` or `arm64`):

```bash
chmod +x godsend-*.AppImage
./godsend-*.AppImage
```

If AppImage fails due to missing FUSE libraries, install distro-specific packages:

- **Ubuntu / Debian / Pop!_OS / Mint:** `sudo apt install libfuse2`
- **Fedora:** `sudo dnf install fuse fuse-libs`
- **Arch / EndeavourOS / Manjaro:** `sudo pacman -S fuse2`
- **openSUSE:** `sudo zypper install fuse libfuse2`

If your distro still blocks AppImage, extract and run without FUSE:

```bash
./godsend-*.AppImage --appimage-extract
./squashfs-root/AppRun
```

---

## Configuration

### Electron app settings

Open the settings page (⚙ button) to configure:

- **Start with Windows** — adds GODsend to Windows login items
- **Local Transfer folder** — directory the backend scans for pre-downloaded ISOs (defaults to `%APPDATA%\godsend-electron\runtime\Transfer`)
- **Internet Archive account** — log in with your archive.org credentials; session cookies are stored locally, your password is never saved
- **Parallel download connections** — concurrent range-request workers per IA download (1–7, default 5)
- **Backend server port** — choose the backend listen port used by both Electron and Aurora script patching
- **Xbox connection** — enter your Xbox IP, FTP username, and password, then click **FTP Aurora Scripts to Xbox** to push the bundled Lua scripts directly to the console (requires Aurora's FTP server to be enabled); your PC's IP and selected backend port are detected/applied automatically
- **Server log files** — the app appends to a daily file under `%APPDATA%\GODsend\logs\` (folder name may be `godsend-electron` on some builds): timestamped backend stdout/stderr, session banner (paths, `GODSEND_*` env summary with secrets redacted, host IP), and notable UI actions (FTP upload steps, cache refresh, config changes). On the home screen use **Open logs folder** to show today’s file in File Explorer.

### Aurora script (`aurora-scripts/state.lua`)

The easiest way to configure and deploy the scripts is via **Settings → Backend server port** + **Xbox connection** in the app: set the backend port, enter the Xbox IP, and click **FTP Aurora Scripts to Xbox**. The app patches `BRAIN_IP` and `PORT` directly in `state.lua` before uploading.

To configure manually before copying to the Xbox:

```lua
BRAIN_IP = "192.168.1.x"   -- IP address of the PC running the backend
PORT     = "8080"          -- backend server port
```

If the host IP or port changes after installation, edit `state.lua` in the script directory via FTP and restart the script.

---

## Installing on the Xbox

**Via the app (recommended):**

1. Enable Aurora's FTP server: Aurora → Settings → Network → Enable FTP
2. In GODsend Settings → **Xbox connection**, enter the Xbox IP and click **FTP Aurora Scripts to Xbox**
3. Launch GODsend from Aurora → Scripts

**Manually:**

1. Copy all the contents of the `aurora-scripts/` folder (from the GODsend install directory or repo) to the Xbox at `HDD1:\Aurora\User\Scripts\Utility\GODSend\` (or the same path under your USB device if Aurora runs from USB, often including an `Apps` segment in FTP paths)
2. Edit `state.lua` — set `BRAIN_IP` and `PORT` to your PC's backend host/port
3. Enable Aurora's FTP server: Aurora → Settings → Network → Enable FTP
4. Launch GODsend from Aurora → Scripts

---

## Backend HTTP API, runtime folders & environment variables

The backend listens on port `8080` by default. Key endpoints: `/browse`, `/trigger`, `/status`, `/queue`, `/register`, `/files/`, `/cache-status`, `/cache-refresh`.

**[Full API reference, runtime folder layout, and environment variable table](docs/api-reference.md)**

---

## Requirements

- Windows 10/11 64‑bit recommended (backend + Electron app)
- At least 500MB free for the app, plus **15–25GB** recommended for temp + ready game data
- Xbox 360 running Aurora (or another compatible dashboard) with FTP server enabled
- Both PC and Xbox on the same local network
- Free archive.org account for Internet Archive downloads

---

## Additional documentation & troubleshooting

### Legacy Windows installer docs
Older Windows installers and guides (for example, “GODSend Homelab Edition – Windows Installation Guide”) are still useful as historical context and screenshots.

- A summary of how the legacy layout maps to this repo lives in [`docs/legacy/legacy-installers-and-layout.md`](docs/legacy/legacy-installers-and-layout.md).
- The full legacy Windows walkthrough (formerly a PDF) is in [`docs/legacy/godsend-windows-install-guide.md`](docs/legacy/godsend-windows-install-guide.md).

Useful external references (for additional background and prebuilt installers):

- Main repo (original): `https://gitgud.io/Nesquin/godsend-homelab-edition`
- Windows installer repo: `https://github.com/my573ry/GODSendEXE/releases`
- GitGud releases: `https://gitgud.io/Nesquin/godsend-homelab-edition/-/releases`

### Common issues (quick checklist)

- **Lua script not visible in Aurora**
  - Verify path: `Hdd1:\Aurora\User\Scripts\Utility\godsend\`
  - Ensure `main.lua`, `state.lua`, `menu_system.lua`, and `Icon/` are all present.
  - Restart Aurora.

- **Xbox cannot reach backend**
  - Confirm backend is listening on `http://<pc-ip>:<port>` (open in a browser from the PC).
  - Make sure `BRAIN_IP` / `PORT` in `state.lua` on Xbox match your PC’s backend host/port.
  - Confirm PC firewall allows inbound connections on the configured backend port.
  - Ensure PC and Xbox are on the same subnet.

- **FTP transfer problems**
  - Enable FTP in Aurora settings and note the Xbox IP.
  - Use an FTP client (FileZilla/WinSCP) to test connection to port 21.
  - Check router/firewall rules that might block FTP.

- **Conversions/downloads fail or “Ready” is empty**
  - Verify there is enough free disk space (at least 2–3× the ISO size).
  - Check console logs from the backend (Electron window or terminal) for Myrient / IA errors.
  - If using your own ISOs, ensure they are in the Transfer folder and the backend is configured for local mode.

For deeper background on how earlier installers worked (and additional screenshots and FAQs), consult the legacy PDF guide or the original GitGud documentation linked above.
