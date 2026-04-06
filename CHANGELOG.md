# Changelog

All notable changes to GODsend-360 are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [2.2.0] — 2026-04-06

### Added
- **Multi-disc game support** — Aurora menu now detects disc 2+ games and offers a GOD vs. content-install picker; `/disc-info` endpoint; 40+ game compatibility table (`docs/multi-disc-compatibility.md`).
- **Multi-platform builds** — macOS (DMG, x64 + arm64) and Linux (AppImage, x64) targets added to both root and Electron build scripts; Go server cross-compiled via `GOOS`/`GOARCH`.
- **Configurable FTP scripts destination path** — Settings page now exposes a text input (with reset button) for the remote Xbox path scripts are uploaded to; saved alongside other Xbox connection credentials.
- **Aurora FTP live progress** — per-file upload status streamed to the Electron renderer via IPC (`godsend-ftp-progress` events) so the UI reflects actual progress rather than a static "Starting…" label.
- **Auto PC-IP injection** — Electron app detects the local IPv4 address at upload time and patches it into `GODSend.ini`; no manual PC-IP input required.
- **Configurable FTP credentials** — Xbox FTP username and password are now configurable in Settings (defaulting to `xboxftp`/`xboxftp`).
- **Aurora scripts FTP button** — one-click push of the full `aurora-scripts/` bundle from the Settings page directly to the Xbox.

### Fixed
- **FTP "stuck at Starting"** — temp `GODSend.ini` was written to the aurora-scripts folder (inside `Program Files`, requiring elevation); now written to `os.tmpdir()` which is always writable. Click handler also lacked `try/catch`, leaving the button permanently disabled on any error — fixed.
- **GOD folder structure** — output was incorrectly nested as `{TitleID}/{MediaID}/Data*`; corrected to `{TitleID}/Data*` with the CON header file flat alongside the data partitions.
- **STFS/CON header binary** — removed incorrect `version`/`baseVersion` writes at `0x0358`/`0x035C`; added required `buf[0x03AC] = 0x01` flag in `emptyLIVEHeader`.
- **Lua `goto` not supported in 5.1** — replaced `goto continue_library_loop` with a boolean flag pattern.

### Changed
- **Xbox connection section** moved to second position in the Settings page (immediately below the startup toggle) for faster access.
- **Pure Go ISO tooling** — `iso2god.exe` and `7z.exe` external binaries removed; ISO conversion and archive extraction live in `src/server/utils/` (`package utils`, `iso2god.go`), imported by `main` as `godsend/utils`.
- **`scripts/installation/` removed** — Docker and automated installer scripts removed from the repository; all documentation references updated.
- Go server binary renamed per platform: `godsend.exe` (Windows), `godsend-mac` (macOS), `godsend-linux` (Linux).
- `fileSystem.js` `getGodsendExePath()` is now platform-aware; packaged binary resolves without `.exe` on macOS/Linux.

### Removed
- Noisy backend startup logs (TCP/sendfile details and redundant “native ISO tooling” info line).

---

## [2.1.0] — 2026-04-04

### Added
- Disk-backed library caches — game lists persisted to `cache/` and loaded on startup, eliminating cold-start IA fetch delays.
- XBLA platform — Xbox Live Arcade titles browsable and installable from Internet Archive; user-selectable drive (USB or HDD).
- Cache refresh endpoint (`/cache-refresh?platform=`) and Settings button to manually re-fetch all caches.
- Aurora reference docs (`docs/aurora-reference.md`) covering supported Lua APIs, path rules, and known limits.

---

## [2.0.1] — 2026-04-04

### Fixed
- `extraFiles` paths in `electron-builder` config now resolve correctly from the repository root.

### Changed
- XBLA installs respect the user-selected drive rather than always targeting Hdd1.

---

## [2.0.0] — 2026-04-03

### Added
- Complete rewrite: Go backend (`src/server/`) replacing the previous Python server.
- Electron tray app (`src/electron-app/`) for Windows with IPC-driven settings, process lifecycle management, and Internet Archive login.
- Aurora Lua script bundle (`aurora-scripts/`) with library browser, queue viewer, and install orchestration.
- Internet Archive integration — parallel range-request downloads (1–7 workers, configurable).
- HTTP and FTP transfer modes.
- GOD, XEX, raw, DLC, and ROM install types.
- Retro ROMs via EdgeEmu metadata (62 systems).
