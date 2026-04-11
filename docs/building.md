# Building

Requires **Go 1.21+** and **Node.js 18+**. No third-party tool binaries are needed — ISO conversion and archive extraction are handled by the Go backend natively. **Packaged desktop builds** place the Go backend next to the app executable as **`godsend-backend`** (`.exe` on Windows) and ship the Aurora Lua scripts under **`resources/`**; Electron starts the backend when the app launches.

From the repository root:

```
npm install
npm run build
```

`npm install` pulls in Electron app dependencies (`postinstall` runs `npm install` under `src/electron-app`). `npm run build` cross-compiles Go for Windows, Linux, and macOS (`dist/godsend.exe`, `dist/godsend-linux-x64`, `dist/godsend-linux-arm64`, `dist/godsend-darwin-*`, plus `dist/godsend-mac`), then builds the **Electron installer for the machine you run on**: **NSIS** on Windows, **AppImage** on Linux, and on **macOS** an AppImage plus **arm64 and x64 DMGs**. AppImage is omitted on Windows (electron-builder needs symlink privileges there). Use `npm run build:win` for Windows-only (faster). All artifacts land under the root `dist/` folder.

Backend only (all platforms): `npm run build:server:all`.
- Windows: `go build -C src/server -o ../../dist/godsend.exe .`
- Linux amd64 (`x64`): `npm run build:server:linux:amd64`
- Linux arm64: `npm run build:server:linux:arm64`

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

src/electron-app/        Electron desktop app (Windows / macOS / Linux)
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
