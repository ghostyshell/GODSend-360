# GODsend 360

GODsend 360 is a local-network game management system for Xbox 360 consoles running the Aurora dashboard. It consists of three parts:

- **Go backend** — HTTP server running on your PC that fetches games from Internet Archive, converts ISOs to GOD format, and transfers them to the Xbox via FTP
- **Electron app** — Windows desktop tray application wrapping the backend with a live terminal and settings UI
- **Aurora Lua script** — runs on the Xbox and talks to the backend to browse, trigger downloads, and track progress

---

## Quick Installation

### 1. Download the files

Go to the [GODsend 360 v2.0.0 snippet](https://gitgud.io/ghosty99/godsend-360/-/snippets/2658) and download both files:

- **`godsend-Setup-2.0.0.exe`** — Windows installer for the Electron tray app + backend
- **`aurora-scripts-v2.0.0.zip`** — Aurora Lua scripts to install on the Xbox

### 2. Install the Electron app

1. Run `godsend-Setup-2.0.0.exe` and follow the installer prompts.
2. Launch **GODsend** from the Start Menu — the tray icon appears in the system tray.

### 3. Configure Internet Archive (IA) in the app

1. Click the tray icon and open the app window, then click the **⚙ Settings** button.
2. Under **Internet Archive account**, click **Log in** and enter your [archive.org](https://archive.org) credentials. Your session cookie is stored locally — your password is never saved.
3. Set **Parallel download connections** to your preferred value (default **5**, range 1–7).
4. Optionally set a **Local Transfer folder** if you want to use pre-downloaded ISOs instead of fetching from IA.

### 4. Install Aurora scripts on the Xbox

1. Extract `aurora-scripts-v2.0.0.zip`.
2. Edit `GODSend.ini` inside the extracted folder — set `ip=` to your PC's local IPv4 address:
   ```ini
   [Config]
   ip=192.168.1.x
   ```
3. Copy the contents of the `aurora-scripts/` folder to the Xbox at:
   ```
   HDD1:\Aurora\User\Scripts\Utilities\GODsend\
   ```
4. Enable Aurora's FTP server: **Aurora → Settings → Network → Enable FTP**.
5. Launch **GODsend** from Aurora → Scripts.

The Xbox will now connect to the backend running on your PC. You can browse games, trigger downloads, and track progress directly from Aurora.

---

## How it works

```
[Xbox Aurora script] ──HTTP──▶ [Go backend on PC] ──FTP──▶ [Xbox HDD/USB]
                                      │
                               Internet Archive
                               (parallel download)
```

1. The Aurora script on the Xbox browses game libraries sourced from Internet Archive metadata
2. The user selects a title; the script sends a trigger request to the Go backend
3. The backend downloads the ISO from Internet Archive using parallel range requests (5 workers by default, 1–7 configurable), or picks it up from the local Transfer folder if already present
4. For disc ISOs the backend runs `iso2god.exe` to convert to Games on Demand format; XBLA/digital titles are extracted with `7za.exe`
5. The finished game files are transferred to the Xbox over FTP using Aurora's built-in FTP server
6. The Aurora script polls the backend for status and shows a live progress display; the game appears in Aurora when the transfer completes

---

## Repository structure

```
package.json             Root npm scripts: `npm install`, `npm run build` (Go + Windows installer)
dist/                    Build artifacts (godsend.exe, Windows installer, etc.) — created by npm run build
tools/                   Local-only helper binaries (iso2god.exe, 7za.exe, etc.) — ignored by git

src/server/              Go backend
  main.go                  Entry point: HTTP server wiring & startup banner
  models/                  Pure domain types and repository interfaces (Game, Platform, JobStatus…)
  services/                Application-layer service interfaces (GameService)
  infrastructure/          Infrastructure helpers (config loading, path resolution)
  interfaces/http/         HTTP router factory

src/electron-app/        Electron Windows UI
  main.js                  Entry point (requires app/bootstrap)
  app/bootstrap.js         App lifecycle, window creation, IPC handler registration
  services/
    settingsService.js     Config file read/write and all setting accessors
    backendClient.js       Backend process lifecycle, IA login, output buffer
  infrastructure/
    fileSystem.js          Path resolution, directory/file helpers, runtime preparation
    electronTray.js        System-tray icon and context menu

aurora-scripts/          Aurora Lua script + icons installed on the Xbox
  main.lua                 Entry point: script metadata, module loading, main() loop
  state.lua                Connection settings and mutable operation globals
  http_client.lua          HTTP helpers, error catalogue, progress callback
  services.lua             Server communication, wait loop, game installation
  menu.lua                 Queue viewer and library browser UI
  menu_system.lua          Simple menu helper used by main.lua/menu.lua

scripts/installation/automated/   Helper installer scripts (Linux/Windows)
scripts/installation/docker/      Docker compose + Dockerfile for headless Linux deployment
```

---

## Building

Requires **Go 1.21+** and **Node.js 18+**. The Windows installer bundles the Go backend as `godsend-backend.exe` plus optional helper binaries from `src/`; the backend launches when the app starts.

From the repository root:

```
npm install
npm run build
```

`npm install` pulls in Electron app dependencies (`postinstall` runs `npm install` under `src/electron-app`). `npm run build` compiles the server to `dist/godsend.exe`, then runs the NSIS target; all build artifacts (including the installer) appear under the root `dist/` folder.

Place these third-party tools in a `tools/` folder at the repository root before `npm run build` if you want them included in the installer (they are not shipped in this repo and `tools/` is ignored by git):

| File | Source |
|------|--------|
| `iso2god.exe` | [Iso2God by r4dius (Windows GUI)](https://github.com/r4dius/Iso2God/releases) — download latest `Iso2God.exe` and rename/copy as needed |
| `7za.exe` | [7-Zip official downloads](https://www.7-zip.org/) — install 7-Zip and copy `7za.exe` from the installation folder into `tools/` |
| `7za.dll` | [7-Zip official downloads](https://www.7-zip.org/) — from the same installation folder as `7za.exe` |
| `7zxa.dll` | [7-Zip official downloads](https://www.7-zip.org/) — from the same installation folder as `7za.exe` |

Backend only (no installer): `go build -C src/server -o ../../dist/godsend.exe .`

---

## Setup options

You can run GODsend in two main ways:

- **Full desktop experience (recommended)** — Electron tray app + bundled backend
  - Ensure `Go` and `Node.js` are installed.
  - From the repository root:
    - `npm install`
    - `npm run build`
  - Run the generated Windows installer from the `dist/` folder and launch GODsend from the Start Menu.
  - The Electron app:
    - Starts the backend with a writable runtime (`runtime/Temp`, `runtime/Transfer`, `runtime/Ready`, `runtime/cache`).
    - Sets `GODSEND_HOME`, `GODSEND_TRANSFER`, and `GODSEND_IA_*` environment variables based on the Settings UI.
    - Shows live backend logs and lets you control startup at login.

- **Backend-only (no Electron)**
  - Build the server:
    - `go build -C src/server -o ../../dist/godsend.exe .`
  - Run it from the project root (or wherever you place the binary):
    - `dist\godsend.exe`
  - Optionally set the same environment variables the Electron app would:
    - `GODSEND_HOME` – base directory for `Transfer/`, `Ready/`, `Temp/`, `cache/`.
    - `GODSEND_TRANSFER` – override the Transfer folder if you keep ISOs elsewhere.
    - `GODSEND_IA_COOKIE` / `GODSEND_IA_AUTHORIZATION` / `GODSEND_IA_CONCURRENCY` – Internet Archive auth and concurrency (see table below).

    Example (PowerShell, one line):

    ```powershell
    $env:GODSEND_HOME="C:\godsend"; $env:GODSEND_TRANSFER="C:\godsend\Transfer"; $env:GODSEND_IA_CONCURRENCY="5"; .\dist\godsend.exe
    ```
  - Make sure the backend is reachable at `http://<your-pc-ip>:8080` and that the Aurora script’s `GODSend.ini` `ip=` value matches this host.

In both modes, the Aurora script setup is the same: copy `aurora-scripts/` to the Xbox, point `ip=` in `GODSend.ini` at the PC running `godsend.exe`, and enable Aurora’s FTP server.

---

## Configuration

### Electron app settings

Open the settings page (⚙ button) to configure:

- **Start with Windows** — adds GODsend to Windows login items
- **Local Transfer folder** — directory the backend scans for pre-downloaded ISOs (defaults to `%APPDATA%\godsend-electron\runtime\Transfer`)
- **Internet Archive account** — log in with your archive.org credentials; session cookies are stored locally, your password is never saved
- **Parallel download connections** — concurrent range-request workers per IA download (1–7, default 5)

### Aurora script (`aurora-scripts/GODSend.ini`)

Edit before copying to the Xbox:

```ini
[Config]
ip=192.168.1.x        ; IP address of the PC running the backend
```

If the IP changes after installation, edit `godsend_config.ini` in the script directory via FTP and restart the script. The file is read on every launch.

---

## Installing on the Xbox

1. Copy all the contents of the `aurora-scripts/` folder to the Xbox at `HDD1:\Aurora\User\Scripts\Utilities\GODsend\` (or any Aurora scripts path)
2. Edit `GODSend.ini` — set `ip=` to your PC's local IP address
3. Enable Aurora's FTP server: Aurora → Settings → Network → Enable FTP
4. Launch GODsend from Aurora → Scripts

---

## Docker (headless Linux)

See [scripts/installation/docker/README-Docker.md](scripts/installation/docker/README-Docker.md). Run Compose from `scripts/installation/docker/` so build context and volume paths resolve correctly.

---

## Backend HTTP API

The backend listens on port 8080. Endpoints used by the Lua script:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/browse?platform=<p>&game=<name>` | Search game library; `platform` = `xbox360`, `xbox`, `xbla`, `digital`, `dlc`, `xblig`, `local` |
| GET | `/status?game=<name>` | Poll job state: `Idle`, `Processing`, `Ready`, `Error` |
| GET | `/queue` | List all active and completed jobs |
| POST | `/trigger?game=<name>&platform=<p>` | Start processing a game |
| POST | `/register?ip=<xbox-ip>` | Register the Xbox IP for FTP transfer |
| GET | `/files/<name>/...` | Serve finished GOD/archive files to the Xbox over HTTP |
| DELETE | `/queue/<name>` | Remove a completed job from the queue |

---

## Runtime folders

The backend creates these under its working directory (or `GODSEND_HOME` if set):

| Folder | Purpose |
|--------|---------|
| `Transfer/` | Drop ISOs here for local-library installs (used instead of downloading from IA) |
| `Ready/` | Finished GOD/archive files awaiting FTP transfer or HTTP serving |
| `Temp/` | Working directory for in-progress conversions |
| `cache/` | Cached Internet Archive game metadata (avoids re-fetching on each launch) |

---

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GODSEND_HOME` | binary directory | Root path for Transfer/Ready/Temp/cache |
| `GODSEND_TRANSFER` | `$GODSEND_HOME/Transfer` | Override Transfer folder path independently |
| `GODSEND_IA_COOKIE` | — | `logged-in-user=…; logged-in-sig=…` session cookie for IA auth |
| `GODSEND_IA_AUTHORIZATION` | — | Bearer token as an alternative to cookie auth |
| `GODSEND_IA_CONCURRENCY` | `5` | Parallel download workers (1–7) |

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

- A summary of how the legacy layout maps to this repo lives in `docs/legacy-installers-and-layout.md`.
- The original PDF guide is expected at `docs/godsend-windows-install-guide.pdf` (copy your local PDF there) and can be opened directly for the full walkthrough.

Useful external references (for additional background and prebuilt installers):

- Main repo (original): `https://gitgud.io/Nesquin/godsend-homelab-edition`
- Windows installer repo: `https://github.com/my573ry/GODSendEXE/releases`
- GitGud releases: `https://gitgud.io/Nesquin/godsend-homelab-edition/-/releases`

### Common issues (quick checklist)

- **Lua script not visible in Aurora**
  - Verify path: `Hdd1:\Aurora\User\Scripts\Utility\godsend\`
  - Ensure `main.lua`, `GODSend.ini`, `menu_system.lua`, and `Icon/` are all present.
  - Restart Aurora.

- **Xbox cannot reach backend**
  - Confirm backend is listening on `http://<pc-ip>:8080` (open in a browser from the PC).
  - Make sure `ip=` in `GODSend.ini` matches your PC’s IPv4 address (not the Xbox IP).
  - Confirm PC firewall allows inbound connections on port `8080`.
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
