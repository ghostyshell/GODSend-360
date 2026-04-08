## Project overview

GODsend-360 is a local-network game management system for Xbox 360 consoles running the Aurora dashboard. It has three main components:

- **Go backend (`src/server/`)**: HTTP server running on a PC, talking to Internet Archive and the local filesystem, converting ISOs to GOD / content packages, and coordinating queue state.
- **Electron desktop app (`src/electron-app/`)**: Windows tray UI that manages the backend process, exposes configuration (transfer folder, IA auth, concurrency), and ships an installer.
- **Aurora Lua scripts (`aurora-scripts/`)**: Script bundle running inside Aurora on the Xbox, providing the in-dashboard UI to browse libraries, trigger jobs, and monitor/perform installs.

High-level data flow:

- `aurora-scripts` ⇄ **HTTP** ⇄ Go backend ⇄ **Internet Archive / local Transfer folder**
- Go backend ⇄ **FTP / HTTP files** ⇄ Xbox content drives (via Aurora’s FTP and GOD/XEX/DLC layout)
- Electron app ⇄ **child process & IPC** ⇄ Go backend and local config

External behaviour, HTTP routes, and Lua-facing protocols are **stable contracts** – refactors must preserve them unless explicitly requested otherwise.

**Aurora Lua host (agents):** When editing `aurora-scripts/`, read [`docs/aurora-reference.md`](docs/aurora-reference.md) for supported APIs, path rules (relative vs absolute), known limits (Zip extraction, large downloads), and patterns that avoid crashes on-console.

---

## Repository layout & architectural patterns

### Go backend (`src/server/`)

- **Entry point**
  - `main.go`: process startup, HTTP handler registration (for now), environment/config wiring, banner printing.
  - `embed_titles.go` + `data/iso2god_titles.jsonl`: embeds the iso2god-rs title list and registers it with `services` at init.
- **`services/title_lookup.go`**: Title ID → display name for LIVE CON title, FTP GOD folder names, and INI (`services.LookupTitleName`: XboxUnity → XboxDB JSON API → embedded iso2god-rs title list).
- **Minerva source** — `main.go` contains a full Minerva Archive integration alongside the IA integration:
  - `minervaPageURLs` / `minervaTagFilters`: browse-page URL and filename-tag filter per platform (xbox360, xbox, digital, xbla, dlc, xblig, games).
  - `MinervaEntry` / `MinervaPlatformCache` types; cache files: `cache/minerva_<platform>.json`.
  - `scrapeMinervaPage`, `buildMinervaCache`, `loadMinervaCacheFromDisk`, `findMinervaEntry` mirror the IA cache system.
  - `downloadViaTorrent` — loads the platform's **collection** `.torrent` from `cache/` (zip-wrapped as `minerva_*.zip` to reduce AV noise; see `ensureMinervaTorrent` / `readMinervaCachedTorrentBytes`), matches `entry.FileName` in the metainfo, and pulls only that file via `github.com/anacrolix/torrent`. All three `processMinerva*` paths use it instead of `downloadWithProgress`.
  - **Bundled torrent zips** — commit `cache/minerva_xbox360.zip`, `minerva_xbox.zip`, `minerva_digital_torrent.zip`, `minerva_games_torrent.zip` in the repo root `cache/` (refresh with `npm run fetch:minerva-torrents`). Electron `extraFiles` ships `cache/` next to the app; on startup the backend seeds `GODSEND_HOME/cache` from that bundle when needed (`godsendExeDir`). `npm run build:server` / `build-go-all` run `ensure-minerva-torrent-zips.js` (fetch if missing) and `sync-minerva-torrent-zips-to-dist.js` so `dist/cache/` works for bare `godsend.exe`.
  - `processMinervaGame` (Redump ISO pipeline), `processMinervaGenericGame` (mixed archive), `processMinervaDigital` (XBLA/DLC/XBLIG).
  - Download priority in `handleTrigger`: **local → Minerva → Internet Archive**.
  - `handleBrowse` merges Minerva + IA lists (Minerva first, deduped).
  - Pre-scrape script: `scripts/scrape-minerva-cache.js` (`npm run scrape:minerva`) — run before a release build to pre-populate `cache/` for installer packaging.
- **`utils/`** (`package utils`)
  - `iso2god.go`: pure-Go ISO→GOD conversion, archive extract/create, and disc metadata probe (`ProbeISODiscInfo`). Imported by `main`. LIVE CON seed: `utils/data/empty_live.bin` (same file as iso2god-rs), embedded at build time.
- **Domain & services**
  - `models/`: pure domain types and repository-like interfaces (e.g. `Game`, `Platform`, `JobStatus`, `GameRepository`, `QueueRepository`).
  - `services/`: service interfaces and shared application helpers (e.g. `GameService`, `LookupTitleName` in `title_lookup.go`). Future concrete implementations should live here or under `infrastructure/` as appropriate.
- **Infrastructure & interfaces**
  - `infrastructure/`: environment/config resolution, filesystem paths, external process integration, FTP/IA clients.
  - `interfaces/http/`: HTTP router construction and request handlers that adapt the domain/services to concrete HTTP endpoints.

**Pattern**: treat `models` as pure domain, `services` as application layer, `infrastructure` for side effects, and `interfaces/http` as the delivery mechanism. Keep `main.go` thin: wiring only, no complicated logic.

### Electron app (`src/electron-app/`)

- **Entrypoint & app shell**
  - `main.js`: minimal bootstrap that calls `app/bootstrap.js`.
  - `app/bootstrap.js`: creates the BrowserWindow and tray, registers all IPC handlers, and coordinates startup/shutdown of the backend child process via services.
- **Services (application behaviour)**
- `services/settingsService.js`: reads/writes JSON config under `app.getPath("userData")`, exposes getters/setters for transfer folder, IA settings, and backend server port (`serverPort`), and builds the child process environment (`GODSEND_*` variables including `GODSEND_PORT`).
  - `services/backendClient.js`: owns the backend `spawn` lifecycle, output buffering and broadcast, restart semantics, and Internet Archive login flow.
- **Infrastructure (platform concerns)**
  - `infrastructure/fileSystem.js`: canonical install/runtime root detection, directory creation, cache/Temp/Transfer/Ready layout, helper binary copying, and icon path resolution.
  - `infrastructure/electronTray.js`: tray icon creation, context menu wiring, simple open/quit callbacks.
- **Renderer bridge**
- `preload.js`: exposes a narrow, typed IPC surface to the renderer (`window.godsendApi.*`), including persisted backend port config (`config:get-server-port`, `config:set-server-port`).
  - `renderer.js`: DOM-only UI and interaction logic, built on the preload API; no direct Node/Electron imports.
- **Server file logging** (`infrastructure/serverLog.js`): appends backend stdout/stderr and session/context lines to `%APPDATA%\<app>\logs\godsend-server-YYYY-MM-DD.log`; wired from `services/backendClient.js` and `app/bootstrap.js` (IPC `logs:get-info`, `logs:open-folder`).
- **Build scripts**
  - `scripts/sync-assets-icon.js`: pre-build script to normalise tray/icon artwork (`npm run build:win`).
  - `scripts/after-pack-win-icon.js`: `electron-builder` `afterPack` hook for embedding the icon into the Windows executable.

**Pattern**: keep Electron main process organised as:

- `app/` – lifecycle, IPC registration, top-level composition.
- `services/` – high-level behaviour, no direct knowledge of Electron window creation.
- `infrastructure/` – filesystem and OS-specific helpers, no business logic.
- `renderer.js + preload.js` – UI and IPC surface only.

### Aurora scripts (`aurora-scripts/`)

- `main.lua`: script metadata and orchestrator; wires modules and implements the main menu loop.
- `state.lua`: connection settings (`BRAIN_IP`, `PORT`, URL roots, paths) and all mutable state used across modules (`gAbortedOperation`, progress counters, install drive/mode, etc.), plus `initServerURL()` and `loadConfig()`.
- `http_client.lua`: HTTP helpers (`httpGet`, `jsonField`, `validateResponse`), time/size formatting, global `HttpProgressRoutine`, and a centralised error catalogue (`showError`).
- `services.lua`: backend-facing operations, including:
  - `getGameStatus`, `triggerDownload`, `registerForFTP`, `testServerConnection`
  - `waitForProcessing` loop
  - `installGame` for XEX, raw, and GOD installs + DLC handling.
- `menu.lua`: in-Aurora UI for:
  - Server queue/status viewer (`showQueue`) and cache status details.
  - Library browser (`browseLibrary`) with per-platform title lists, drive selection, transfer mode selection, and orchestration of trigger/wait/install.

**Pattern**: treat each `.lua` file as a module with globals shared intentionally:

- `state.lua` defines globals; other modules consume them.
- I/O-heavy functions (HTTP, filesystem, long loops) live in `http_client.lua` and `services.lua`.
- Menu/UX logic lives in `menu.lua` and calls into services instead of duplicating HTTP calls.

### Build tooling

- Root `package.json`: unified build entrypoint:
  - `npm install` – installs root and Electron dependencies.
  - `npm run build` – cross-compiles Go for Windows, Linux, and macOS; runs Electron for the **current OS** (Windows → **NSIS**; Linux → **AppImage**; macOS → **AppImage** then **arm64 + x64 DMGs**). AppImage is not built on Windows hosts by default (electron-builder needs symlink creation; use Linux/macOS CI or Windows Developer Mode if you must build it there).
  - `npm run build:win` – Windows-only (Go `godsend.exe` + NSIS), same as the former default full build.
  - `npm run build:server:all` – Go binaries only (all targets into `dist/`).
  - `npm run build:server` / `npm run build:electron` – single-platform server or Electron step.
- `dist/`: consolidated build artifacts (per-OS Go binaries, installers, etc.).
- `tools/`: ignored directory for third-party executables (`7za.exe`, `7za.dll`, `7zxa.dll`) when needed outside the bundled Go pipeline.

### GitGud release assets (upload + links)

- Preferred public asset URLs must use the project-scoped form:
  - `https://gitgud.io/-/project/46780/uploads/<upload-id>/<filename>`
  - Do **not** use `https://gitgud.io/uploads/...` directly (can require sign-in and break README/release links).
- If PAT credentials are embedded in `origin` (HTTPS URL with user:token), agents may use that token for GitGud API calls.
- Upload and attach a release asset via GitGud API:
  1. Extract PAT from `git remote get-url origin`.
  2. Upload file: `POST /api/v4/projects/:id/uploads` (multipart `file=@...`).
  3. Build public URL using the returned `upload.url` with `https://gitgud.io` prefix.
  4. Attach to release: `POST /api/v4/projects/:id/releases/:tag/assets/links` with `name` + `url`.
- Replacing an existing asset link:
  - List links: `GET /api/v4/projects/:id/releases/:tag/assets/links`
  - Update link URL: `PUT /api/v4/projects/:id/releases/:tag/assets/links/:link_id`
  - Or delete/recreate if needed.
- After any release asset change, verify:
  - README download links match the current upload URL.
  - Release page asset link resolves publicly (unauthenticated) and downloads the file.

---

## Build, run, and test commands

### Electron + backend (Windows)

- **Install dependencies (root + Electron)**:
  - `npm install`
- **Full build — all Go backends + installer for this OS**:
  - `npm run build` — cross-compiles **all Go backends**; **Windows**: NSIS; **Linux**: AppImage; **macOS**: AppImage + arm64/x64 DMGs. (Linux AppImage from a Windows PC is skipped — build on Linux or macOS for that artifact.)
- **Full build — Windows only (faster)**:
  - `npm run build:win`
- **Full build — macOS x64 (Go binary + DMG)**:
  - `npm run build:mac` *(run on macOS)*
- **Full build — macOS arm64 (Go binary + DMG)**:
  - `npm run build:mac:arm` *(run on macOS)*
- **Full build — Linux x64 (Go binary + AppImage)**:
  - `npm run build:linux` *(run on Linux or macOS)*
- **Backend-only builds**:
  - All targets: `npm run build:server:all`
  - Windows: `go build -C src/server -o ../../dist/godsend.exe .`
  - macOS x64: `npm run build:server:mac`
  - macOS arm64: `npm run build:server:mac:arm`
  - Linux x64: `npm run build:server:linux`
- **Run Electron app in dev mode**:
  - `npm start --prefix src/electron-app`
  - Dev binary resolved from `dist/godsend.exe` (Win), `dist/godsend-mac` (macOS), `dist/godsend-linux` (Linux).

When making changes to Electron or the backend, prefer:

- Run `npm run build` at least once after structural refactors.
- Start the built app and verify:
  - Backend starts successfully.
  - Settings page works (transfer folder, IA login, concurrency).
  - Lua script can still talk to the backend using the API described in `README.md`.

### Aurora scripts

- There is no automated test harness; manual checks are performed from Aurora:
  - Copy the contents of `aurora-scripts/` to the Aurora scripts directory.
  - Configure `GODSend.ini` as described in `README.md`.
  - Exercise:
    - Queue view.
    - Each library (xbox360/xbox/xbla/digital/dlc/xblig/local/games).
    - HTTP vs FTP modes.
    - Install paths (GOD/XEX/DLC).

---

## Code style & design guidelines

### General

- Prefer **small, focused modules** over large monoliths.
- Keep IO/infrastructure code separated from domain logic:
  - Domain types and invariants in `models/` (Go) or `state.lua` (Lua).
  - Application behaviour in `services/`.
  - Network, filesystem, external processes in `infrastructure/` or clearly named helpers.
- Backwards compatibility is important:
  - Preserve existing HTTP endpoints and query shapes.
  - Preserve Electron IPC channel names.
  - Preserve Lua-visible behaviour and user flows wherever possible.

### Go (`src/server/`)

- Follow standard `gofmt` formatting and idiomatic Go.
- Keep handlers thin:
  - Parse/validate HTTP input.
  - Delegate to `services`.
  - Translate domain outcomes to HTTP status codes + JSON.
- Avoid global mutable state:
  - Prefer structs with dependencies injected via constructors.
  - Use interfaces from `models/` for repositories/clients to keep the core testable.

### Electron (Node/JS)

- Use modern JS syntax (const/let, arrow functions where appropriate).
- Keep `main.js` minimal – all real behaviour flows through `app/bootstrap.js` + services.
- Do not import Electron directly from services unless absolutely necessary:
  - Services should be reusable and testable with minimal mocking.
  - Infrastructure modules can depend on Electron APIs (e.g. `app`, `nativeImage`) where needed.
- Keep `preload.js` as the **single bridge** between renderer and main; only expose stable, well-named methods on `window.godsendApi`.
- Avoid adding new global IPC channels without documenting them here and in `README.md`.

### Lua (Aurora scripts)

- Consult [`docs/aurora-reference.md`](docs/aurora-reference.md) for Aurora-specific Lua APIs, filesystem/HTTP quirks, and pre-deployment checks beyond this summary.
- The scripting environment is Lua 5.1 with limited libraries:
  - Avoid heavy allocations or deep recursion in hot paths.
  - Use `pcall` around operations that can throw from the host (e.g. `Http.*`, `IniFile`, `FileSystem`, `ZipFile`, `Script` UI calls).
- Keep cross-module state centralised in `state.lua`.
- Prefer defensive parsing for HTTP responses (`jsonField`, `validateResponse`) over brittle patterns; use **`sanitizeManifestValue`** / **`sanitizeIniTitleName`** on `IniFile.ReadValue` results that become paths, URLs, or filenames (NUL/control tails from Aurora).
- When adding new functionality:
  - Add low-level helpers to `http_client.lua` or `services.lua`.
  - Add menu flows to `menu.lua`, calling into those helpers instead of duplicating HTTP logic.

---

## Changelog and contributing rules

Every non-trivial change **must** include a `CHANGELOG.md` update. Read [`CONTRIBUTING.md`](CONTRIBUTING.md) for the full rules; the key points for agents are:

- Add a bullet under `[Unreleased]` at the top of `CHANGELOG.md` (create the section if absent) in the appropriate category (`Added`, `Fixed`, `Changed`, `Removed`).
- When releasing (cutting a new version), move `[Unreleased]` to the new version number + date and bump versions in **all four places** — see the version-bump table in `CONTRIBUTING.md`.

## When and how to update this file

Treat `AGENTS.md` as **living documentation for agents and automation**. Update it whenever you:

- Introduce a new subsystem or directory that encodes architectural decisions:
  - e.g. new `infrastructure/` subpackages, new `interfaces/*` targets, new script bundles.
- Change build or run commands:
  - e.g. add tests, change package manager, add makefiles, modify Docker entrypoints.
- Add or change conventions:
  - e.g. preferred logging style, error-handling patterns, IPC naming schemes, or directory naming standards.

When editing:

- Keep sections short and structured using the existing headings: **overview**, **layout**, **commands**, **style/guidelines**, **update rules**.
- Prefer describing **intent and constraints** over restating obvious code structure.
- If a new rule or pattern is **project-wide**, document it here and (if user-facing) in `README.md`.
- If a rule only applies to a subdirectory, briefly mention it here and link or reference any local docs.

If you are an agent performing a significant refactor:

- **First**: scan this file and the relevant section of `README.md` for constraints.
- **After changes**: update this file to reflect any new module layouts, entrypoints, or required commands before finishing your task.

