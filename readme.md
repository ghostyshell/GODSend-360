# GODsend 360

GODsend 360 is a local-network game management system for Xbox 360 consoles running the Aurora dashboard. It consists of three parts:

- **Go backend** — HTTP server running on your PC that fetches games from Internet Archive, converts ISOs to GOD format, and transfers them to the Xbox via FTP
- **Electron app** — Windows desktop tray application wrapping the backend with a live terminal and settings UI
- **Aurora Lua script** — runs on the Xbox and talks to the backend to browse, trigger downloads, and track progress

---

## Quick Installation

### 1. Download the installer

Go to the [GODsend 360 v2.4.2 release](https://gitgud.io/ghosty99/godsend-360/-/releases/v2.4.2) and download:

- **[`godsend-Setup-2.4.2.exe`](https://gitgud.io/api/v4/projects/ghosty99%2Fgodsend-360/packages/generic/installer/2.4.2/godsend-Setup-2.4.2.exe)** — Windows installer for the Electron tray app + backend (Aurora scripts are bundled)

### 2. Install the Electron app

1. Run `godsend-Setup-2.4.2.exe` and follow the installer prompts.
2. Launch **GODsend** from the Start Menu — the tray icon appears in the system tray.

### 3. Configure Internet Archive (IA) in the app

1. Click the tray icon and open the app window, then click the **⚙ Settings** button.
2. Under **Internet Archive account**, click **Log in** and enter your [archive.org](https://archive.org) credentials. Your session cookie is stored locally — your password is never saved.
3. Set **Parallel download connections** to your preferred value (default **5**, range 1–7).
4. Optionally set a **Local Transfer folder** if you want to use pre-downloaded ISOs instead of fetching from IA.

### 4. Install Aurora scripts on the Xbox

The Aurora scripts are bundled with the installer. The easiest way to install them is via the app:

1. Enable Aurora's FTP server: **Aurora → Settings → Network → Enable FTP**.
2. In the GODsend app, open **⚙ Settings** → scroll to **Xbox connection**.
3. Enter your **Xbox IP address** and your **PC's IP address**, then click **Save**.
4. Click **FTP Aurora Scripts to Xbox** — the scripts are uploaded to the path you set (default `Hdd1:\Aurora\User\Scripts\Utility\GODSend\`; on USB FTP often shows `Usb0:\Apps\Aurora\User\Scripts\Utility\GODSend\`), and `GODSend.ini` is patched with your PC's IP.
5. Launch **GODsend** from Aurora → Scripts.

Alternatively, copy the `aurora-scripts/` folder from the GODsend install directory to the Xbox manually via FTP, then edit `GODSend.ini` to set `ip=` to your PC's IP.

The Xbox will now connect to the backend running on your PC. You can browse games, trigger downloads, and track progress directly from Aurora.

### Features (what you can do)

Each item is a **high-level capability**, **how you use it**, and **how it works** under the hood.

#### Windows tray app (Electron) + backend

- **What:** Run the Go HTTP server from your PC with a small UI, live log output, and settings. One-click upload of Aurora scripts to the Xbox via FTP with live per-file progress.
- **How:** Open GODsend from the Start Menu; use the tray icon for the window. Restart the backend from the home screen; optional **Start with Windows** in Settings. In Settings → **Xbox connection**, enter your Xbox IP and click **FTP Aurora Scripts to Xbox** — your PC's IP is patched into `GODSend.ini` automatically and upload progress is shown file-by-file.
- **How it works:** Electron spawns `godsend` with a writable runtime (`Transfer`, `Ready`, `Temp`, `cache`) and injects `GODSEND_*` environment variables from your settings. The FTP upload streams `godsend-ftp-progress` IPC events to the renderer so the button reflects real progress rather than a static label.

#### Internet Archive account & parallel downloads

- **What:** Authenticated downloads from archive.org collections used for the built-in libraries.
- **How:** Settings → **Internet Archive account** → **Log in**; adjust **Parallel download connections** (1–7).
- **How it works:** The app stores session cookies locally (not your password), passes them to the backend, which fetches items with multiple range-request workers for faster ISO/archive retrieval.

#### Local Transfer folder (your own ISOs)

- **What:** Install disc games from `.iso` files you already have, without re-downloading from IA.
- **How:** Settings → set **Local Transfer folder** (or use the default runtime `Transfer` folder). Drop ISOs there. On the Xbox, open **Local Library** or trigger a title that matches a filename in that folder.
- **How it works:** For Xbox 360 / original Xbox / `local` browse, the backend prefers a matching ISO under `Transfer` over Internet Archive and runs the same conversion pipeline locally.

#### Library metadata caches

- **What:** Faster startup after the first run; optional forced refresh when collections change.
- **How:** First launch may take a minute while lists build. In Settings, **Refresh Cache** rebuilds IA/ROM indexes in the background. On the console, **Server Queue & Status** shows aggregate cache readiness and per-platform detail.
- **How it works:** The server persists lists under `cache/` and serves `/browse` from memory; `/cache-refresh` and the Electron button trigger rebuilds without blocking the HTTP server indefinitely.

#### Server queue, status, and job cleanup

- **What:** See everything the PC is processing or has finished; clear stuck or old jobs.
- **How:** Aurora → **Server Queue & Status** — refresh the list, open **Cache** for build state, **Clear ALL server jobs** or remove one job from its submenu.
- **How it works:** The script polls `/queue` and `/cache-status`. Removals call `/queue/remove` (GET/POST), which deletes entries and suppresses stray status updates for cleared games.

#### Browse & install: Xbox 360 / original Xbox disc libraries (Internet Archive)

- **What:** Redump-style ISO libraries converted for Aurora. After choosing transfer mode (HTTP/FTP) the script offers **GOD**, **DLC** (content install), or **XEX** for every title — pick the install layout that matches the disc.
- **How:** Main menu → **Xbox 360 Redump ISOs** or **Original Xbox Redump ISOs** → letter folder → title → destination drive → HTTP or FTP → install type → confirm → install when **Ready**. A **[Recommended]** label appears when the server can determine the correct layout from the disc.
- **How it works:** Backend downloads the ISO (or uses your local copy), converts it to GOD format natively (no external tools required), stages under `Ready/`, then either serves files over HTTP for the script to pull or pushes them over FTP to the paths you registered. Title names are resolved from XboxUnity → XboxDB → an embedded title list and used to name the GOD folder on the Xbox (e.g. `Open Season - 5454082A`) so Aurora shows the correct title.

#### Multi-disc game support

- **What:** Correct handling of multi-disc games where Disc 2 (or later) is DLC or bonus content rather than a standalone game — the server recommends the right install method per disc.
- **How:** When triggering a Disc 2+ title the Aurora menu shows **GOD** or **Content** options with a **[Recommended]** label. Select the recommended option; for content discs the files land in `Content\0000000000000000\{TitleID}\00000002\` on the chosen drive. Disc 1 is always installed as GOD in the normal flow.
- **How it works:** `/disc-info` checks the disc against a 40+ title compatibility table (`discCompatTable`) that maps each game's Title ID to the correct install method. Content discs often carry a generic placeholder Title ID (`FFED2000`) in their `default.xex`; the server automatically reads the real Title ID from the STFS/CON packages on the disc (header offset `0x0360`) so the content lands in the right folder even without manual input. Covered games include Borderlands (GOTY), Borderlands 2 (GOTY), Call of Duty, Mass Effect, Red Dead Redemption, Skyrim, L.A. Noire, and many more.

#### Browse & install: XBLA, digital (No-Intro), DLC, Xbox Live Indie Games, 360 game archives

- **What:** Non-disc content (arcade packages, digital titles, DLC, indie games, pre-packed game archives) from curated Internet Archive collections.
- **How:** Choose the matching main-menu entry → same browse flow; **DLC** skips drive pick and targets **Hdd1:** as required for that content.
- **How it works:** Backend downloads and unpacks archives natively (no external tools required), writes a small `godsend.ini` manifest and payloads the script understands. Install type may be **GOD** (multi-part `.7z`), **raw** (single file into a content path), or **xex** as defined by the manifest.

#### HTTP vs FTP transfer mode

- **What:** Two ways to move prepared content from the PC to the Xbox.
- **How:** After picking a game, choose **HTTP (Download & Extract)** or **FTP (Direct Transfer - More Reliable)**.
- **How it works:** **HTTP:** the console downloads from `/files/...` and Aurora’s script extracts/places files (ZIP/7z handling on-console where applicable). **FTP:** you register the console IP with `/register`; the server uploads directly to Aurora’s FTP server to the drive you selected, so the script only waits for completion.

#### Install layouts: GOD, XEX folder, content (DLC), raw, ROMs

- **What:** Different on-disk layouts depending on title type. The backend handles all conversion and packaging natively — no external tools required.
- **How:** After **HTTP vs FTP**, the script asks **every** **Xbox 360 / Original / Local / Games Archive** title for **GOD / DLC / XEX**. Follow prompts until success, then **Settings → Content → Scan** in Aurora (or launch RetroArch for ROMs).
- **How it works:**
  - **GOD** — ISO is converted to Games on Demand format; script downloads manifest + `.7z` parts into the `Games on Demand` folder structure (`GOD\{Name} - {TitleID}\{TitleID}\00007000\`).
  - **DLC (Content)** — Content files are extracted from the ISO and placed in `Content\0000000000000000\{TitleID}\00000002\` on the target drive. The correct Title ID is resolved from the disc's content packages automatically (see Multi-disc game support above).
  - **XEX** — The backend walks the XDVDFS filesystem for `default.xex`/`default.xbe` and extracts that game root to a loose folder under `[drive]\XEX\...`.
  - **Raw** — A `.bin`/package is downloaded directly into the path specified in the manifest (DLC-type content forces **Hdd1:**).
  - **ROM** — Archive is extracted under `[drive]\<ROM root>\<system folder>\` (configurable via Settings).

#### Retro ROMs (EdgeEmu, many systems)

- **What:** Browse and install classic ROM sets scraped from EdgeEmu-compatible metadata (dozens of consoles/handhelds).
- **How:** **Retro ROMs** → pick system → folder → title → drive → HTTP or FTP → same wait/install flow as other libraries.
- **How it works:** Backend fetches/builds per-system ROM lists (`rom_*` platforms), downloads archives when triggered, and emits a **rom**-type manifest with a drive-relative `rompath`. The script extracts under `[drive]\<ROM root>\<system folder>\`, where **ROM install path** in Settings sets the root (default `Emulators\RetroArch\roms`, passed to the backend as `GODSEND_ROM_PATH`).

#### Persistent server logs

- **What:** Daily rotating log files that capture all backend activity — useful for diagnosing failed installs, FTP errors, IA download issues, or anything else that goes wrong.
- **How:** Logs are written automatically to `%APPDATA%\GODsend\logs\godsend-server-YYYY-MM-DD.log`. On the home screen, click **Open logs folder** to jump straight to today's file in File Explorer.
- **How it works:** Each session opens with a banner that records app/Electron versions, OS, hostname, primary IPv4, `GODSEND_HOME`, backend executable path, effective Transfer folder, and all `GODSEND_*` environment variables (IA secrets redacted). Backend stdout/stderr are tagged `BACKEND_OUT`/`BACKEND_ERR`; UI events (FTP upload steps, cache refresh triggers, config saves, IA login) are tagged separately. Lines use ISO 8601 timestamps with PID so multi-process output is unambiguous.

#### Developer / diagnostics

- **What:** Quick HTML snapshot of cache, transfer folder, ready games, and jobs.
- **How:** From a browser on the PC, open `http://<pc-ip>:8080/debug` while the backend is running.
- **How it works:** The server renders live in-memory and filesystem state for troubleshooting.

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
4. For disc ISOs the backend converts to Games on Demand format using a pure Go implementation; XBLA/digital titles are extracted natively — no external tools required
5. The finished game files are transferred to the Xbox over FTP using Aurora's built-in FTP server
6. The Aurora script polls the backend for status and shows a live progress display; the game appears in Aurora when the transfer completes

---

## Repository structure

```
package.json             Root npm scripts: `npm install`, `npm run build` (all Go targets + OS-matched Electron installer; DMGs on macOS)
dist/                    Build artifacts (per-OS binaries and installers) — created by `npm run build`

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

```

---

## Building

Requires **Go 1.21+** and **Node.js 18+**. No third-party tool binaries are needed — ISO conversion and archive extraction are handled by the Go backend natively. The Windows installer bundles the Go backend as `godsend-backend.exe` and the Aurora Lua scripts; the backend launches when the app starts.

From the repository root:

```
npm install
npm run build
```

`npm install` pulls in Electron app dependencies (`postinstall` runs `npm install` under `src/electron-app`). `npm run build` cross-compiles Go for Windows, Linux, and macOS (`dist/godsend.exe`, `dist/godsend-linux`, `dist/godsend-darwin-*`, plus `dist/godsend-mac`), then builds the **Electron installer for the machine you run on**: **NSIS** on Windows, **AppImage** on Linux, and on **macOS** an AppImage plus **arm64 and x64 DMGs**. AppImage is omitted on Windows (electron-builder needs symlink privileges there). Use `npm run build:win` for Windows-only (faster). All artifacts land under the root `dist/` folder.

Backend only (all platforms): `npm run build:server:all`. Windows binary only: `go build -C src/server -o ../../dist/godsend.exe .`

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
- **Xbox connection** — enter your Xbox IP, FTP username, and password, then click **FTP Aurora Scripts to Xbox** to push the bundled Lua scripts directly to the console (requires Aurora's FTP server to be enabled); your PC's IP is detected automatically
- **Server log files** — the app appends to a daily file under `%APPDATA%\GODsend\logs\` (folder name may be `godsend-electron` on some builds): timestamped backend stdout/stderr, session banner (paths, `GODSEND_*` env summary with secrets redacted, host IP), and notable UI actions (FTP upload steps, cache refresh, config changes). On the home screen use **Open logs folder** to show today’s file in File Explorer.

### Aurora script (`aurora-scripts/GODSend.ini`)

The easiest way to configure and deploy the scripts is via **Settings → Xbox connection** in the app: enter the Xbox IP and click **FTP Aurora Scripts to Xbox**. The app detects your PC's IP automatically and patches it into `GODSend.ini` before uploading.

To configure manually before copying to the Xbox:

```ini
[Config]
ip=192.168.1.x        ; IP address of the PC running the backend
```

If the IP changes after installation, edit `godsend_config.ini` in the script directory via FTP and restart the script. The file is read on every launch.

---

## Installing on the Xbox

**Via the app (recommended):**

1. Enable Aurora's FTP server: Aurora → Settings → Network → Enable FTP
2. In GODsend Settings → **Xbox connection**, enter the Xbox IP and click **FTP Aurora Scripts to Xbox**
3. Launch GODsend from Aurora → Scripts

**Manually:**

1. Copy all the contents of the `aurora-scripts/` folder (from the GODsend install directory or repo) to the Xbox at `HDD1:\Aurora\User\Scripts\Utility\GODSend\` (or the same path under your USB device if Aurora runs from USB, often including an `Apps` segment in FTP paths)
2. Edit `GODSend.ini` — set `ip=` to your PC's local IP address
3. Enable Aurora's FTP server: Aurora → Settings → Network → Enable FTP
4. Launch GODsend from Aurora → Scripts

---

---

## Backend HTTP API

The backend listens on port 8080. Endpoints used by the Lua script:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/browse?platform=<p>` | Game list (pipe-separated); `platform` includes `xbox360`, `xbox`, `xbla`, `digital`, `dlc`, `xblig`, `games`, `local`, `rom_<sysid>` |
| GET | `/status?game=<name>` | Poll job state: `Idle`, `Processing`, `Ready`, `Error`, `Missing` |
| GET | `/queue` | List all active and completed jobs |
| GET | `/trigger?game=<name>&platform=<p>` | Start processing a game (Aurora uses GET) |
| GET | `/register?game=<name>&ip=<xbox-ip>&drive=...&platform=...&mode=...` | Register the console for FTP transfer (`mode` = `http` or `ftp`) |
| GET | `/files/<name>/...` | Serve finished GOD/archive files to the Xbox over HTTP |
| GET/POST | `/queue/remove?game=<name>` | Remove one job (`game` omitted clears all); Aurora uses GET |
| GET | `/cache-status` | Per-platform cache build state and counts |
| GET | `/cache-refresh?platform=<p>` | Trigger cache rebuild (`all`, an IA platform, or `rom_<sysid>`); Electron uses GET |

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

- A summary of how the legacy layout maps to this repo lives in [`docs/legacy/legacy-installers-and-layout.md`](docs/legacy/legacy-installers-and-layout.md).
- The full legacy Windows walkthrough (formerly a PDF) is in [`docs/legacy/godsend-windows-install-guide.md`](docs/legacy/godsend-windows-install-guide.md).

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
