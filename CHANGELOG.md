# Changelog

All notable changes to GODsend-360 are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

---

## [2.2.4] — 2026-04-06

### Fixed
- **Electron — FTP Aurora scripts** — Default/hint paths aligned with Aurora’s real layout (`Scripts/Utility`, not `Utilities`). Settings copy explains that **USB** FTP paths often include **`/Usb0/Apps/...`** so uploads are not sent to a different tree than the one Aurora opens. Success text now includes the **exact remote path** used for the upload.

### Changed
- **Version** — **2.2.4** (root + Electron `package.json`, lockfile roots, backend banner `GODSend Backend Server v2.2.4`). Aurora script bundle **8.2.3** (`main.lua` / menu title; no Lua logic changes).

---

## [2.2.3] — 2026-04-06

### Changed
- **Version** — **2.2.3** (root + Electron `package.json`, lockfile roots, backend banner `GODSend Backend Server v2.2.3`). Aurora script bundle **8.2.2** (`main.lua` / menu title).
- **Go layout** — Title ID resolution lives in **`services/title_lookup.go`** (`services.LookupTitleName`); embedded **`data/iso2god_titles.jsonl`** is wired from **`embed_titles.go`** in `main`. Removed root **`title_lookup.go`**. **`services/game_service.go`** import path corrected to **`godsend/models`**.
- **Aurora (v8.2.1)** — **Xbox 360 / Original / Local / Games Archive**: after HTTP/FTP, **every** title gets **GOD / DLC / XEX** (no multi-disc name filter). Optional **[Recommended]** still comes from **`/disc-info`** when the server can probe or hint.
- **Aurora (v8.2.0)** — Library flow: **Transfer method (HTTP/FTP) first**, then **install method**. Multi-disc detection on the server includes Redump-style **`[DVD2]`** and related filename hints for **`/disc-info`**.
- **`/disc-info`** — If there is no ISO in **Transfer**, but the name looks like Disc 2+, returns a **filename-based** recommendation (with optional Borderlands GOTY Title ID guess) instead of 404 so the picker works for IA-only installs.
- **`processGame` (Redump xbox360/xbox)** — Honors **`install_type`**: **GOD**, **content** (`processContentInstallFromISO`), **XEX** (full archive extract + `default.xex` folder, same FTP/HTTP packaging as Games Archive). **`lookupInstallType`** centralizes normalization.
- **Local Transfer + XEX** — **`utils.ExtractXEXFolderFromISO`** walks XDVDFS for **`default.xex`** / **`default.xbe`**, extracts that game root to temp, then same FTP / 7z+manifest path as loose XEX; source **`.iso`** is removed on success like other local jobs.
- **`processGenericGame` (`games`)** — Same **`install_type`** behavior via **`lookupInstallType`**. **`register` / `trigger`** accept only `god` \| `content` \| `xex`.
- **Docs in code** — `iso2god.go` package comment notes alignment with Xbox game-partition / XDVDFS handling as in [XboxDev/extract-xiso](https://github.com/XboxDev/extract-xiso) (extract-xiso is not bundled; all ISO I/O stays in Go).

---

## [2.2.2] — 2026-04-06

### Changed
- **Version** — **2.2.2** (root + Electron `package.json`, `package-lock.json` roots, backend startup banner `GODSend Backend Server v2.2.2`).
- **Docs** — Replaced `docs/godsend-windows-install-guide.pdf` with `docs/legacy/godsend-windows-install-guide.md`; README links to the Markdown guide. `docs/legacy-installers-and-layout.md` moved to `docs/legacy/legacy-installers-and-layout.md` with references updated.
- **Pure-Go ISO→GOD** — LIVE CON header now fills UTF-16 display title at **0x411** and **0x1691** (same layout as iso2god-rs `with_game_title`) when a name is resolved. `RunIso2GodNative` takes an optional `resolveDisplayTitle func(uint32) string` (server passes `iso2GodResolveDisplayTitle`); nil keeps title slots zero. CON template is the vendored **empty_live.bin** from [iso2god-rs `src/god`](https://github.com/iliazeus/iso2god-rs/blob/master/src/god/empty_live.bin) (`utils/data/empty_live.bin`, `go:embed`); finalize clears **0x35B**, **0x35F**, **0x391** and hashes **0x0344..0x0b000** like RS `ConHeaderBuilder::finalize`. Thumbnail length fields at **0x1712** / **0x1716** are left from the template (RS only overwrites them in `with_game_icon`). With the same ISO, title string, and `--trim=none`, **CON SHA-256 matches iso2god-rs v1.8.1** (verified on *Open Season*).
- **Title ID → display name** — `lookupTitleName` order is **XboxUnity** → **XboxDB** (`GET https://xboxdb.altervista.org/api/{title_id}` when the response is JSON) → **embedded** copy of [iso2god-rs `titles.jsonl`](https://github.com/iliazeus/iso2god-rs/blob/master/src/game_list/titles.jsonl) (MIT). Same chain for LIVE CON title, FTP `GOD\…` folder naming, `godsend.ini` `titlename`, and `godFolderName`.
- **Electron build scripts** — invoke `electron-builder` via `node --disable-warning=DEP0190 ./node_modules/electron-builder/cli.js …` so Node’s DEP0190 (`shell: true` + args inside electron-builder) does not spam the build log. Requires Node **18.3+** (same as root `engines`).

### Added
- **`src/server/data/iso2god_titles.jsonl`** — vendored Title ID / name list from iso2god-rs (update by replacing the file from upstream `src/game_list/titles.jsonl`); embedded at build time for offline fallback.
- **Electron — persistent server logs** — daily files under `%APPDATA%\GODsend\logs\` (`godsend-server-YYYY-MM-DD.log`): ISO timestamps, process id, tagged lines (`BACKEND_OUT` / `BACKEND_ERR` / `ELECTRON_UI` / `APP_*`), session banner (app/Electron versions, OS, hostname, primary IPv4, `GODSEND_HOME`, backend exe, effective transfer folder, `GODSEND_*` env with IA secrets redacted), backend lifecycle, plus config/cache/FTP/IA login events from the main process. Home screen **Open logs folder** opens Explorer on the current log file.

### Fixed
- **Pure-Go ISO→GOD** — Output tree now matches [iso2god-rs `file_layout`](https://github.com/iliazeus/iso2god-rs/blob/master/src/god/file_layout.rs): `{TitleID}/00007000/{MediaID}.data/Data*` plus `{TitleID}/00007000/{MediaID}` CON (Original Xbox: `00005000` / `{TitleID}.data`). The previous flat `{TitleID}/Data*` + CON layout did not match what Aurora/FSD expect, so converted games could fail to launch. `detectGodStructure` accepts both the new layout and the old flat layout for existing archives.
- **Local Transfer / FTP** — (1) Aurora sometimes left NUL tails, C0 controls (e.g. `0x08`), or invalid UTF-8 after titles from `Http.Get` / `ShowPopupList`, so the PC could not match the Transfer-folder `.iso` (e.g. `Open Season (USA)` + garbage). Go now normalizes every `game` query via `normalizeClientGameName`; Aurora **8.1.3+** truncates browse bodies at NUL, strips controls from parsed titles, and sanitizes selected names before URL encode. (2) Query `+` vs filename `+` and related encoding: space→`+` ISO fallback on the server; `encodeGameQueryParam` for literal `+` when there are no spaces. (3) **8.1.4** — `httpGet` deep-copies response bodies so titles are not aliased to reused host buffers; strip accidental `…228:8080/browse?platform=local` (or full `http://…`) suffixes in Lua and Go. (4) **8.1.6** — Trailing-dot handling is **Lua-only**: `sanitizeGameNameFromHost` deep-copies strings (`string.sub`), collapses duplicate trailing `.` / fullwidth `．` (Aurora UI often adds an extra `.` after Redump `… (Region).`), and no longer strips a single `).` (that removed the legitimate period before `.iso`). Go `findLocalISO` again uses only exact + space→`+` match (keeps `normalizeLocalBasename` / `EqualFold` for NBSP / fullwidth dot vs ASCII).
- **Aurora 8.1.7 — install / manifest strings** — `IniFile.ReadValue` and the section key could carry NUL tails or control bytes, producing bad **paths and filenames** (GOD folder `titlename`, raw `filename`, `dataurl` / part URLs, XEX `foldername`, etc.). **`sanitizeManifestValue`** (NUL cut, strip `%c`, trim, optional UTF-8 BOM) and **`sanitizeIniTitleName`** (plus trailing-dot collapse) wrap every manifest read; **`titleid` / `mediaid`** are hex-filtered. HTTP **`installGame`** re-sanitizes **`gameName`** before loading `godsend.ini`.
- **Aurora 8.1.8 / local ISO `game` query** — Letter-jump / quick-search can append one ASCII letter after `)`, e.g. `Open Season (USA)q`, so the PC could not match `Open Season (USA).iso`. **`sanitizeGameNameFromHost`** strips repeated `)X` tails (up to 8); **`normalizeClientGameName`** does the same so URL-encoded triggers still resolve.
- **Aurora 8.1.9 — browse list display** — The game picker could still show titles like `Open Season (USA)228:8080/browse?platform=local` even though the server saw a clean name: Lua had stripped the URL for logic, but **`ShowPopupList`** could render from host memory aliased to the next request. **`sanitizeGameNameFromHost`** now removes **`https?://…/browse?platform=…`** (not only `http://`) via `gsub`, and returns a **byte-rebuilt** string (`detachHostString`) so row labels are Lua-owned.

---

## [2.2.1] — 2026-04-06

### Added
- **`scripts/build-go-all.js`** — cross-compiles the Go server for Windows, Linux, and macOS (amd64/arm64) into `dist/`; uses `cwd` + `shell: false` so paths with spaces work; copies darwin/arm64 → `godsend-mac` for Electron/mac defaults.
- **`scripts/build-all.js`** — full pipeline: Go all targets, `sync-assets-icon`, then OS-specific Electron (see Changed).
- **`npm run build:server:all`** (root) — Go-only all-platform binaries.
- **Electron** `build:nsis` script — Windows NSIS without re-running icon sync (used by `build-all.js`).
- Multi-disc compatibility: **Borderlands** and **Borderlands 2** (incl. GOTY) Title IDs **545407E7** / **5454087C** recommend **Content** install for Disc 2 (`docs/multi-disc-compatibility.md`, `discCompatTable` in `main.go`). XboxDB / marketplace Title ID references noted in docs next to Xbox Unity.

### Changed
- **`npm run build`** now runs `node scripts/build-all.js`: all Go targets, then Electron for the **host OS** — **NSIS** on Windows, **AppImage** on Linux, **AppImage** plus **arm64 and x64 DMGs** on macOS. **`npm run build:win`** is unchanged (Windows Go + NSIS only). **`AGENTS.md`** and **`readme.md`** describe the split.
- **macOS DMG** artifact names include **`${arch}`** so arm64 and x64 builds do not overwrite each other.
- Aurora script bundle version **8.1.1** (`main.lua`).

### Fixed
- **AppImage on Windows** — unified build skips Linux AppImage on Windows (electron-builder symlink step needs Developer Mode / admin); build AppImage on Linux or macOS, or run `npm run build:wl` in `src/electron-app` if symlink creation is enabled.
- **`npm run build` on Windows** — `build-all.js` no longer spawns `npm.cmd` with `shell: false` (Node cannot execute `.cmd` that way, so the Electron step exited immediately with code 1). It now runs **`npm-cli.js` via the same `node` binary**; falls back to `npm` on Unix if that path is missing.
- **NSIS / 7-Zip flake** — before Windows NSIS, `build-all.js` removes **`dist/win-unpacked`** and **`dist/*.nsis.7z`** so electron-builder does not archive a half-written or stale tree (fixes intermittent “cannot find GODsend.exe / godsend-backend.exe” from `7za`).

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
