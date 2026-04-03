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
- **Domain & services**
  - `models/`: pure domain types and repository-like interfaces (e.g. `Game`, `Platform`, `JobStatus`, `GameRepository`, `QueueRepository`).
  - `services/`: service interfaces coordinating application use-cases (e.g. `GameService`). Future concrete implementations should live here or under `infrastructure/` as appropriate.
- **Infrastructure & interfaces**
  - `infrastructure/`: environment/config resolution, filesystem paths, external process integration, FTP/IA clients, ISO/GOD conversion helpers.
  - `interfaces/http/`: HTTP router construction and request handlers that adapt the domain/services to concrete HTTP endpoints.

**Pattern**: treat `models` as pure domain, `services` as application layer, `infrastructure` for side effects, and `interfaces/http` as the delivery mechanism. Keep `main.go` thin: wiring only, no complicated logic.

### Electron app (`src/electron-app/`)

- **Entrypoint & app shell**
  - `main.js`: minimal bootstrap that calls `app/bootstrap.js`.
  - `app/bootstrap.js`: creates the BrowserWindow and tray, registers all IPC handlers, and coordinates startup/shutdown of the backend child process via services.
- **Services (application behaviour)**
  - `services/settingsService.js`: reads/writes JSON config under `app.getPath("userData")`, exposes getters/setters for transfer folder and IA settings, and builds the child process environment (`GODSEND_*` variables).
  - `services/backendClient.js`: owns the backend `spawn` lifecycle, output buffering and broadcast, restart semantics, and Internet Archive login flow.
- **Infrastructure (platform concerns)**
  - `infrastructure/fileSystem.js`: canonical install/runtime root detection, directory creation, cache/Temp/Transfer/Ready layout, helper binary copying, and icon path resolution.
  - `infrastructure/electronTray.js`: tray icon creation, context menu wiring, simple open/quit callbacks.
- **Renderer bridge**
  - `preload.js`: exposes a narrow, typed IPC surface to the renderer (`window.godsendApi.*`).
  - `renderer.js`: DOM-only UI and interaction logic, built on the preload API; no direct Node/Electron imports.
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

### Installers, Docker, and tooling

- `scripts/installation/automated/`: PowerShell and shell installers for Windows/Linux, responsible for placing binaries and Lua scripts on disk.
- `scripts/installation/docker/`: Dockerfile and `docker-compose` YAMLs for headless backend deployment.
- Root `package.json`: unified build entrypoint:
  - `npm install` – installs root and Electron dependencies.
  - `npm run build` / `npm run build:server` / `npm run build:electron` – Go backend + Windows installer, outputs into `dist/`.
- `dist/`: consolidated build artifacts (`godsend.exe`, installer, etc.).
- `tools/`: ignored directory for third-party executables (`iso2god.exe`, `7za.exe`, `7za.dll`, `7zxa.dll`).

---

## Build, run, and test commands

### Electron + backend (Windows)

- **Install dependencies (root + Electron)**:
  - `npm install`
- **Full build (Go backend + Windows installer)**:
  - `npm run build`
- **Backend-only build (no installer)**:
  - `go build -C src/server -o ../../dist/godsend.exe .`
- **Run Electron app in dev mode**:
  - `npm start --prefix src/electron-app`

When making changes to Electron or the backend, prefer:

- Run `npm run build` at least once after structural refactors.
- Start the built app and verify:
  - Backend starts successfully.
  - Settings page works (transfer folder, IA login, concurrency).
  - Lua script can still talk to the backend using the API described in `README.md`.

### Docker / headless backend

- From `scripts/installation/docker/`, follow `README-Docker.md`:
  - Build & run: `docker compose up --build` (or the documented equivalent).

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
- Prefer defensive parsing for HTTP responses (`jsonField`, `validateResponse`) over brittle patterns.
- When adding new functionality:
  - Add low-level helpers to `http_client.lua` or `services.lua`.
  - Add menu flows to `menu.lua`, calling into those helpers instead of duplicating HTTP logic.

---

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

