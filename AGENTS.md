## Project overview

GODsend-360 is a local-network game management system for Xbox 360 consoles running the Aurora dashboard. It has three main components:

- **Go backend (`src/server/`)**: HTTP server running on a PC, talking to Internet Archive and the local filesystem, converting ISOs to GOD / content packages, and coordinating queue state.
- **Electron desktop app (`src/electron-app/`)**: Windows tray UI that manages the backend process, exposes configuration (transfer folder, IA auth), and ships an installer.
- **Aurora Lua scripts (`aurora-scripts/`)**: Script bundle running inside Aurora on the Xbox, providing the in-dashboard UI to browse libraries, trigger jobs, and monitor/perform installs.

High-level data flow:

- `aurora-scripts` ⇄ **HTTP** ⇄ Go backend ⇄ **Internet Archive / local Transfer folder**
- Go backend ⇄ **FTP** ⇄ Xbox content drives (via Aurora’s FTP server; GOD/XEX/DLC layout)
- Electron app ⇄ **child process & IPC** ⇄ Go backend and local config

External behaviour, HTTP routes, and Lua-facing protocols are **stable contracts** – refactors must preserve them unless explicitly requested otherwise.

**Aurora Lua host (agents):** When editing `aurora-scripts/`, read [`docs/reference/aurora.md`](docs/reference/aurora.md) for supported APIs, path rules (relative vs absolute), known limits (Zip extraction, large downloads), and patterns that avoid crashes on-console.

---

## Repository layout & architectural patterns

### Go backend (`src/server/`)

The backend uses a **DDD-style package layout** with an `App` struct for dependency injection. All shared state lives in `*app.App`; services hold an `App` pointer and expose methods instead of free functions.

- **`main.go`** (~180 lines) — entry point only: constructs `*app.App`, all services, the HTTP `Deps` struct, wires the router, and starts the server. No business logic.
- **`embed_titles.go`** + `data/iso2god_titles.jsonl` — embeds the iso2god-rs title list and registers it with `services` at init.
- **`aria2c_darwin.go`** / **`aria2c_stub.go`** — build-tagged macOS aria2c bootstrap; accepts `*app.App` + `*torrent.Service` parameters.

#### `models/` — pure domain types (no dependencies)
  - `types.go`: all exported domain types (`IAGameEntry`, `PlatformCache`, `BuildState`, `MinervaEntry`, `MinervaPlatformCache`, `XboxConnection`, `GameStatus`, `PendingFTPJob`, `ROMSystemDef`, etc.).
  - `compat.go`: disc compatibility table and `DiscCompat()` lookup.
  - `game.go`: `Platform`, `JobStatus` enums; `Game`, `GameRepository`, `QueueRepository` interfaces.

#### `app/` — central App struct and configuration
  - `app.go`: `App` struct holding all shared state (config/paths, mutex-guarded caches, sync.Maps for job queue / connections / install types), `NewApp()` constructor, logging methods (`Logf`, `LogStatus`, `LogFTPComplete`), `LookupInstallType`, `FmtDuration`.
  - `config.go`: constants, IA/Minerva collection maps, ROM system definitions, `SetupPaths`, `LoadIAAuthFromEnv`, `ApplyArchiveOrgHeaders`, `CleanupEmptyReadyDirs`.
  - `listen.go`: `IsTCPAddrInUse`, `ListenOnAvailablePort` TCP helpers.

#### `infrastructure/` — side-effect adapters (filesystem, network, external processes)
  - `helpers/helpers.go`: utility functions (`GetOutboundIP`, `SanitizeFilename`, `CopyFileBuffered`, `DetectGodStructure`, `IsHexString`, `ParseXboxHeader`, `BucketAndZip`, `DecodeMinervaName`).
  - `download/ia.go`: IA download `Service` with chunked/parallel range-request support.
  - `download/edgeemu.go`: EdgeEmu download `Service` with chunked/parallel support.
  - `download/progress.go`: `ProgressWriter` for download progress tracking.
  - `ftp/client.go`: FTP `Service` — Xbox connection, upload, GOD/XEX/content transfer functions, pending-job persistence and retry, `MkdirAll` package-level helper.
  - `torrent/torrent.go`: torrent/aria2c `Service` — `DownloadViaTorrent`, aria2c detection, `DarwinCandidatesFn` injection point for macOS.

#### `services/` — application-layer logic
  - `cache/ia.go`: IA cache `IAService` — build, load, save, find, persistence.
  - `cache/minerva.go`: Minerva cache `MinervaService` — scrape, build, load, save, find.
  - `cache/rom.go`: ROM/EdgeEmu cache `ROMService` — build, load, find.
  - `local/scanner.go`: local Transfer-folder `Service` — ISO scanning, matching, `NormalizeClientGameName` (package-level function).
  - `pipeline/pipeline.go`: pipeline `Service` struct (holds references to all other services), `ProcessLocalISO`, `ProcessGame`, `FinalizeGOD`.
  - `pipeline/digital.go`: `ProcessContentInstallFromISO`, `ProcessGenericGame`, `ProcessDigital`, XEX/DLC transfer helpers.
  - `pipeline/minerva.go`: `ProcessMinervaGame`, `ProcessMinervaGenericGame`, `ProcessMinervaDigital`.
  - `pipeline/rom.go`: `ProcessROM`, `FindROMFiles`, `UpdateGameINI_ROM`.
  - `pipeline/ini.go`: `UpdateGameINI_Parts`, `UpdateGameINI_Raw`, `UpdateGameINI_XEX`, `Iso2GodResolveDisplayTitle`, `GodFolderName`.
  - `title_lookup.go`: `LookupTitleName` (XboxUnity → XboxDB → embedded iso2god-rs list).
  - `game_service.go`: `GameService` interface.

#### `interfaces/http/` — HTTP delivery layer
  - `middleware.go`: `Deps` struct (holds `*app.App` + service references), `jsonError`, `jsonSuccess`, `RecoverMiddleware`.
  - `handlers.go`: all main HTTP handlers as methods on `*Deps` (browse, cache, trigger, status, queue, register, debug, file serving, range parsing, etc.).
  - `handlers_rxea.go`: `/rxea/decode` and `/rxea/encode` handlers.
  - `handlers_tools.go`: `/tools/probe-iso`, `/tools/iso2god`, `/tools/iso2xex` handlers.
  - `router.go`: `NewRouter()` — registers all routes on `*http.ServeMux`.

#### `utils/` (`package utils`)
  - `iso2god.go`: pure-Go ISO→GOD conversion, archive extract/create, disc metadata probe (`ProbeISODiscInfo`). LIVE CON seed: `utils/data/empty_live.bin`.
  - `rxea.go`: pure-Go RXEA codec (Aurora `.asset` file encode/decode).

#### Dependency flow (no import cycles)
```
models → (nothing)
app → models
infrastructure/* → app, models
services/* → app, models, infrastructure/*
interfaces/http → app, models, services/*, infrastructure/*
main → everything (wiring only)
```

#### Key architectural notes
- **Pending FTP queue** (`infrastructure/ftp/`) — when an FTP transfer fails after retries (e.g. console launched a game), the backend persists the job to `GODSEND_HOME/pending_ftp/<id>.json` and retries indefinitely (30 s → 5 min backoff). Jobs survive restarts and are resumed at startup. Endpoints: `GET /data/status`, `GET /data/clear`, `GET /config`. Env vars: `GODSEND_DEFAULT_DRIVE`, `GODSEND_ARIA2_LISTEN_PORT`, `GODSEND_ARIA2_DHT_PORT`.
- **Minerva source** (`services/cache/minerva.go`, `infrastructure/torrent/`, `services/pipeline/minerva.go`) — Minerva Archive integration alongside IA. Download priority in `/trigger`: **local → Minerva → Internet Archive**. `/browse` merges Minerva + IA lists (Minerva first, deduped). Torrent download via `aria2c` (`--select-file`); macOS uses Homebrew bootstrap (`aria2c_darwin.go`).
- **Bundled torrent zips** — `cache/minerva_*.zip` in the repo root. Electron `extraFiles` ships `cache/` next to the app; backend seeds `GODSEND_HOME/cache` from that bundle. Pre-scrape: `npm run scrape:minerva`.

**Pattern**: treat `models` as pure domain, `app` as shared state container, `services` as application layer, `infrastructure` for side effects, and `interfaces/http` as the delivery mechanism. Keep `main.go` thin: wiring only, no complicated logic.

### Electron app (`src/electron-app/`)

The Electron main-process source is written in **TypeScript** (compiled in-place via `tsconfig.json`; no `outDir`). All source files are `.ts`; the compiled `.js` files are the build artefacts used at runtime.

- **Entrypoint & app shell**
  - `main.ts`: minimal bootstrap that registers the `godsend-aurora://` protocol scheme and calls `app/bootstrap.ts`.
  - `app/bootstrap.ts`: creates the BrowserWindow and tray, registers all IPC handlers, and coordinates startup/shutdown of the backend child process via services.
  - `app/window.ts`: BrowserWindow creation, minimize-to-tray / close-to-tray behaviour, `getMainWindow` / `getWebContentsForPush` helpers.
- **Services (application behaviour)**
  - `services/settingsService.ts`: reads/writes JSON config under `app.getPath("userData")`, exposes getters/setters for transfer folder, IA settings, backend server port (`serverPort`), default Xbox drive (`defaultXboxDrive`), and aria2 ports (`aria2ListenPort`, `aria2DhtPort`); builds the child process environment (`GODSEND_*` variables).
  - `services/backendClient.ts`: owns the backend `spawn` lifecycle, output buffering and broadcast, restart semantics, and Internet Archive login flow.
  - `services/auroraLibraryService.ts`: parses Aurora's SQLite databases (content.db / settings.db) into `AuroraGame[]`, probes FTP drive letters, and builds the local game-name cache from JSON title lists.
  - `services/auroraVisualService.ts`: syncs Aurora asset files (`.asset` RXEA, flat Media cover JPGs, `GameCoverInfo.bin`, `visual-manifest.json`) between the console and the local cache, emitting `xbox-cover` and `xbox-title-visuals` IPC events.
  - `services/auroraPathHelper.ts`: derives and caches the Aurora install root from the configured FTP scripts path; `discoverAuroraRoot` probes common locations.
  - `services/coverArtService.ts`: multi-source cover art fetching (XboxUnity, Xbox CDN, Microsoft Store autosuggest, Wikipedia); in-memory `browseCoverCache`.
  - `services/autoSyncService.ts`: post-FTP automation — `autoUploadAuroraAssets` and `doAuroraLibrarySync`.
- **IPC handlers (`ipc/`)**
  - `configHandlers.ts`: startup, logs, transfer folder, server port, IA auth, ROM path, cache refresh, Xbox connection, default drive, aria2 ports, Aurora library sources.
  - `xboxFtpHandlers.ts`: ping, verbose FTP test, port scanner, Aurora scripts upload, drive listing, game listing, cover fetch.
  - `auroraLibraryHandlers.ts`: Aurora library sync (DB fingerprint caching), cover + visual asset sync, disk-cache visual refresh.
  - `auroraAssetHandlers.ts`: asset search (XboxUnity + CDN), image fetch, file picker, console upload, RXEA decode/encode, Aurora game inspector.
  - `browseHandlers.ts`: game list, queue game, disc info, browse cover art, download queue.
  - `toolsHandlers.ts`: ISO probe/convert, FTP Manager (list, upload queue, delete, mkdir, rename, copy), game drive move.
- **Infrastructure (platform concerns)**
  - `infrastructure/fileSystem.ts`: canonical install/runtime root detection, directory creation, cache/Temp/Transfer/Ready layout, Aurora scripts path, icon resolution.
  - `infrastructure/electronTray.ts`: tray icon creation, context menu wiring.
  - `infrastructure/backendHttp.ts`: thin `backendGet` / `backendPost` helpers for the local Go server.
  - `infrastructure/httpHelper.ts`: redirect-following HTTP image fetch, magic-byte MIME detection.
  - `infrastructure/serverLog.ts`: session-structured log file appender for backend stdout/stderr and app events.
  - `infrastructure/auroraLibraryCache.ts`: local Aurora DB cache layout, meta read/write, safe path helper.
  - `infrastructure/sqlHelper.ts`: sql.js wrapper (`getSqlJs`, `sqlRows`, `filetimeToDateStr`).
- **Renderer bridge**
  - `preload.ts`: exposes a narrow, typed IPC surface to the renderer (`window.godsendApi.*`).
  - React renderer (`renderer/`): `App.jsx` (routing, queue polling every 5 s), page components `HomePage`, `SettingsPage`, `LibraryPage`, `QueuePage`.
- **Build scripts**
  - `scripts/sync-assets-icon.js`: pre-build script to normalise tray/icon artwork.
  - `scripts/after-pack-win-icon.js`: `electron-builder` `afterPack` hook for embedding the icon.
- **TypeScript compilation**
  - `tsconfig.json`: `"module": "commonjs"`, `"strict": false`, no `outDir` (in-place compilation).
  - Build commands (`npm run build:win` etc.) run `tsc` before electron-builder; `npm run tsc` compiles only.
  - Packaged `.asar` excludes `*.ts` and `tsconfig.json` via `build.files` in `package.json`.

**Pattern**: keep Electron main process organised as:

- `app/` – lifecycle, IPC registration, top-level composition.
- `services/` – high-level behaviour, no direct knowledge of Electron window creation.
- `infrastructure/` – filesystem and OS-specific helpers, no business logic.
- `preload.ts` – IPC surface only; no business logic.

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
  - Library browser (`browseLibrary`) with per-platform title lists, drive selection (skipped when `gDefaultDrive` is set), and orchestration of FTP trigger/wait/install.

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

- **README and headless-setup version strings** — Do **not** change `readme.md` or `docs/headless-setup.md` to reference a new version (GitGud `.../releases/vX.Y.Z` URLs, installer/AppImage/DMG filenames, or inline “use `godsend-Setup-X.Y.Z.exe`” text) until the **`vX.Y.Z` tag exists on the remote** (e.g. `git ls-remote --tags origin 'refs/tags/vX.Y.Z'` or the GitGud release page). Update those docs when attaching assets for that tag (see verify step below).
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
   - Settings page works (transfer folder, IA login).
  - Lua script can still talk to the backend using the API described in `README.md`.

### Aurora scripts

- There is no automated test harness; manual checks are performed from Aurora:
  - Copy the contents of `aurora-scripts/` to the Aurora scripts directory.
  - Configure `GODSend.ini` as described in `README.md`.
  - Exercise:
    - Queue view.
    - Each library (xbox360/xbox/xbla/digital/dlc/xblig/local/games).
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

- Consult [`docs/reference/aurora.md`](docs/reference/aurora.md) for Aurora-specific Lua APIs, filesystem/HTTP quirks, and pre-deployment checks beyond this summary.
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

