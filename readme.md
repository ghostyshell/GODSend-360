# GODsend

GODsend is a local-network game management system for Xbox 360 consoles running the Aurora dashboard. It consists of three parts:

- **Go backend** — HTTP server running on your PC that fetches games from Internet Archive, converts ISOs to GOD format, and transfers them to the Xbox via FTP
- **Electron app** — Windows desktop tray application wrapping the backend with a live terminal and settings UI
- **Aurora Lua script** — runs on the Xbox and talks to the backend to browse, trigger downloads, and track progress

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

1. Copy the entire `aurora-scripts/` folder to the Xbox at `HDD1:\Aurora\User\Scripts\Utilities\GODsend\` (or any Aurora scripts path). The script is now split into multiple `.lua` files; all of them must be present in the same directory alongside `main.lua`
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

- Windows PC (backend + Electron app)
- Xbox 360 running Aurora with FTP server enabled
- Both devices on the same local network
- Free archive.org account for Internet Archive downloads
