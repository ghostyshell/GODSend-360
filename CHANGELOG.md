# Changelog

All notable changes to GODsend-360 are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

---

## [2.5.0] — 2026-04-08

### Changed
- **Aurora script deployment now patches `state.lua` directly** — FTP upload now writes the detected PC IP and selected backend port directly into `BRAIN_IP` and `PORT`, and startup uses those values without runtime INI override logic.
- **Documentation refresh for host/port setup** — README now documents `state.lua` as the source of truth (`BRAIN_IP`/`PORT`), the configurable backend port flow in Electron settings, and generic `http://<pc-ip>:<port>` troubleshooting guidance.
- **Version** — **2.5.0** (root + Electron `package.json`, lockfile root, backend banner) and Aurora script **11.0.0**.

---

## [2.4.9] — 2026-04-08

### Changed
- **Electron settings layout** — moved the **Backend server port** section above **Xbox connection** for a more natural setup flow before FTP configuration.
- **Version** — **2.4.9** (root + Electron `package.json`, lockfile root, backend banner) and Aurora script **10.0.3**.

---

## [2.4.8] — 2026-04-08

### Fixed
- **Aurora connection fallback after FTP patching** — script config loading now reads both `godsend_config.ini` and `GODSend.ini`, supports both `[Config] ip/port` and `[Settings] BrainAddress/BrainPort`, and falls back per-field so custom backend ports patched during FTP are used reliably.

### Changed
- **Version** — **2.4.8** (root + Electron `package.json`, lockfile root, backend banner) and Aurora script **10.0.2**.

---

## [2.4.7] — 2026-04-08

### Added
- **Configurable backend port in Electron settings** — added a persisted `serverPort` setting in the desktop app and wired it through IPC/UI so changing the port restarts the backend and updates runtime calls that target the local server.

### Fixed
- **FTP Aurora script patching now includes both host and port** — when using "FTP Aurora Scripts to Xbox", the app now requires a detected local IPv4 and patches both IP and backend port into `GODSend.ini` before upload (`BrainAddress`/`BrainPort` plus legacy `ip`/`port` keys when present).
- **Aurora config load now supports saved port** — `aurora-scripts` now reads `port` from `godsend_config.ini` along with `ip`, so the script can connect to non-8080 backend ports after FTP patching.

### Changed
- **Version** — **2.4.7** (root + Electron `package.json`, lockfile root, backend banner) and Aurora script **10.0.1**.

### Fixed
- **macOS app failed to launch / Linux missing bundled data** — the platform-specific `extraFiles` blocks in `src/electron-app/package.json` were overriding (not merging with) the top-level `extraFiles`, so cache, assets, and Aurora scripts were silently dropped from the mac and linux builds. On macOS the Go backend was also placed under `Contents/` instead of `Contents/MacOS/`, causing `spawn ENOENT` on launch. Fixed by moving shared data into `extraResources` (which lands in `Contents/Resources/` on mac and `<install>/resources/` on win/linux), pointing the mac backend at `MacOS/godsend-backend`, and adding `getBundledResourcesRoot()` (= `process.resourcesPath` when packaged) so cache/assets/aurora-scripts lookups resolve to the correct location across platforms. Resource files must live under `Resources/` on macOS or code-signing rejects the bundle.
- **Linux arch build/run docs + dev binary lookup** — documented Linux `amd64` (`x64`) and `arm64` build/run commands plus distro-specific AppImage runtime dependencies in `readme.md`, added `build:server:linux:amd64` alias, and fixed Electron dev backend lookup to use `dist/godsend-linux-x64` / `dist/godsend-linux-arm64` instead of the old `dist/godsend-linux` path.

---

## [2.4.6] — 2026-04-08

### Fixed
- **Minerva torrent progress stops at 98–99%** — aria2c's inline progress uses bare `\r` (carriage return), which `bufio.ScanLines` does not split on. The `\r`-terminated bytes accumulate until the scanner's 64 KB buffer limit is hit, `Scan()` returns false, the read loop exits early, and no further progress is logged — leaving the Aurora UI showing stale status for the remaining download time. Fixed by running output reading in a goroutine (pipe is always drained), a custom split function treating both `\r` and `\n` as line boundaries, and a 1 MB scanner buffer.

### Changed
- **Version** — **2.4.6** (root + Electron `package.json`, backend banner).

---

## [2.4.5] — 2026-04-07

### Changed
- **Aurora script v10.0.0** — renamed the "Abort" button in the Background/Back modal to "Back" to better reflect that the server keeps running and nothing is cancelled.
- **Version** — **2.4.5** (root + Electron `package.json`, backend banner, Aurora script `10.0.0`).

---

## [2.4.4] — 2026-04-07

### Fixed
- **Minerva torrent exit code 16** — aria2c's working directory is now a short OS temp path (`%TEMP%\gd-dl-*`) instead of the long install-relative path, avoiding Windows MAX_PATH failures on deep torrent subdirectories. File is moved to the job temp dir after download. Added `--file-allocation=none` to skip pre-allocation on large files.
- **aria2c Windows Firewall prompt** — installer now adds `netsh` firewall rules for `aria2c.exe` (inbound + outbound) so Windows does not prompt for network access on first torrent download. Rules are removed on uninstall.
- **Torrent progress not shown in backend terminal** — `logf` was missing from the aria2c progress scanner loop; terminal now shows `TORRENT [game]: x% @ y/s ETA z` every 3 s alongside the Aurora queue updates.

### Changed
- **Version** — **2.4.4** (root + Electron `package.json`, backend banner).

---

## [2.4.3] — 2026-04-07

### Changed
- **Minerva downloads via aria2c** — replaced the `anacrolix/torrent` BitTorrent client with bundled `aria2c` binaries. `scripts/download-aria2.js` fetches aria2c 1.37.0 for Windows (GitHub release), Linux x64, macOS arm64, and macOS x64 (Homebrew GHCR bottles) into `dist/tools/`. The installer bundles the platform binary next to `godsend-backend`. At runtime the backend fetches the collection `.torrent` via Go HTTPS, writes it to a temp file, and shells out to aria2c with `--select-file=<index>` — avoiding aria2c's Windows SSL issues while getting full peer connectivity.
- **`build:server` script** — now runs `download-aria2.js` then builds the Go binary; old `ensure-minerva-torrent-zips.js` / `sync-minerva-torrent-zips-to-dist.js` calls removed.
- **Version** — **2.4.3** (root + Electron `package.json`, backend banner).

### Removed
- **Torrent zip cache** — `cache/minerva_*.zip` files, `scripts/ensure-minerva-torrent-zips.js`, `scripts/fetch-minerva-torrent-zips.js`, `scripts/sync-minerva-torrent-zips-to-dist.js`, and the `jszip` dev dependency are all gone. The collection `.torrent` is fetched fresh on each download.
- **`cmd/torrent-test`** — internal test tool removed now that the flow is validated.

---

## [2.4.2] — 2026-04-07

### Fixed
- **Minerva collection torrent cache** — torrents are stored on disk as zip archives (`minerva_xbox360.zip`, `minerva_xbox.zip`, `minerva_digital_torrent.zip`, `minerva_games_torrent.zip`) with a single inner entry `torrent` to reduce AV false positives on `.torrent` files. `downloadViaTorrent` now unpacks that zip before `metainfo.Load` (previously the bencode parser received raw PK zip bytes and failed). Added the missing `archive/zip` import.
- **Upgrades from 2.4.1** — plain `minerva_*.torrent` files left in `cache/` are migrated into the zip layout on first use instead of re-downloading.

### Changed
- **Aurora script bundle** — `scriptVersion` **9.1.2** (main-menu title shows `GODsend 360 v9.1.2`).
- **Version** — **2.4.2** (root + Electron `package.json`, lockfile roots, backend banner `GODSend Backend Server v2.4.2`, README install links). `AGENTS.md` Minerva torrent notes updated for the zip cache.

---

## [2.4.1] — 2026-04-07

### Fixed
- **Minerva torrent download** — replaced the broken `/rom?name=` per-file approach (which returned HTML, never a `.torrent`) with collection-torrent + selective-file download:
  - Four collection `.torrent` files (`minerva_xbox360.torrent`, `minerva_xbox.torrent`, `minerva_digital.torrent`, `minerva_games.torrent`) are pre-bundled in `cache/` and included in the installer via the existing `extraFiles` glob.
  - At runtime, `downloadViaTorrent` loads the cached collection torrent, locates the requested file by basename (`entry.FileName`), calls `file.Download()` on only that file (all other pieces remain at `PriorityNone`), and monitors per-file byte completion.
  - If a torrent file is absent at runtime (manual install / upgrade), `ensureMinervaTorrent` downloads it from Minerva on demand.
  - Each download runs in its own `Temp/<name>_torrent/` directory; the full tree is removed via `defer os.RemoveAll` after processing, avoiding conflicts between concurrent downloads.
  - Removed dead `fetchTorrentFile`, `MinervaDownloadBase` constant, and stale `torrentURL` / `referer` wiring from the three `processMinerva*` functions.

### Changed
- **Aurora script bundle** — `scriptVersion` **9.1.1** (main-menu title shows `GODsend 360 v9.1.1`).
- **Version** — **2.4.1** (root + Electron `package.json`, lockfile roots, backend banner `GODSend Backend Server v2.4.1`).

---

## [2.4.0] — 2026-04-07

### Fixed
- **Minerva torrent fetch** — `fetchTorrentFile` now sends a `Referer` header pointing to the platform's Minerva browse page; without it the server returned an HTML error page instead of the `.torrent` file, causing `bencode: syntax error (offset: 0): unknown value type '<'`.
- **Minerva HTML entities** — Minerva No-Intro filenames containing `&#39;`, `&amp;`, etc. are now decoded to plain characters before being sent to clients (`decodeMinervaName`), so names like `'Splosion Man` display correctly.

### Changed
- **Separate source browse lists** — Minerva Archive and Internet Archive are now independently browsable.  Selecting any library from the main menu first shows a **"Download Source"** popup (`Minerva Archive` / `Internet Archive`); the chosen source controls both the game list fetched from `/browse` and the `source=` parameter forwarded to `/trigger`.  The previously merged/deduped combined list is retained as a backward-compat fallback when `source` is omitted.
  - `/browse` now accepts `?source=minerva` or `?source=ia` to return each source's list independently.
  - Source selection is scoped to the browse session — no additional prompt appears at download time.
  - Main menu labels updated from `(Minerva | Internet Archive)` to `(Pick Source)`.
  - `source=minerva` on `/trigger` returns `minerva_unavailable` instead of silently falling back to IA when the game is not in Minerva's index.
- **Aurora script bundle** — `scriptVersion` **9.1.0** (main-menu title shows `GODsend 360 v9.1.0`).
- **Version** — **2.4.0** (root + Electron `package.json`, lockfile roots, backend banner `GODSend Backend Server v2.4.0`).

---

## [2.3.1] — 2026-04-07

### Changed
- **Aurora script name** — `scriptTitle` and the main-menu title now use **GODsend 360** (replacing "GODSend Store") to match the project branding.
- **Aurora script bundle** — `scriptVersion` **9.0.0** (main-menu title shows `GODsend 360 v9.0.0`).
- **Version** — **2.3.1** (root + Electron `package.json`, lockfile roots, backend banner `GODSend Backend Server v2.3.1`).

---

## [2.3.0] — 2026-04-07

### Added
- **Minerva Archive source** — Xbox 360 ISOs, OG Xbox ISOs, XBLA, DLC (Addon), XBLIG, and Games are now sourced from [minerva-archive.org](https://minerva-archive.org/browse/) in addition to Internet Archive.  Download priority is: **local Transfer folder → Minerva → Internet Archive**.  The browse list merges Minerva entries first then IA (deduplicated).
  - New `MinervaEntry` / `MinervaPlatformCache` types and a full cache stack (`buildMinervaCache`, `loadMinervaCacheFromDisk`, `saveMinervaCacheToDisk`, `findMinervaEntry`) mirroring the IA cache system.
  - **BitTorrent download** — Minerva's `/rom?name=` endpoint serves `.torrent` files rather than direct downloads.  `downloadViaTorrent` (backed by `github.com/anacrolix/torrent`) fetches the `.torrent` file, then downloads the actual content via BitTorrent.  Progress is reported to the Lua polling loop via the existing `logStatus` mechanism.
  - Three new processing functions: `processMinervaGame` (Redump ISO → GOD pipeline), `processMinervaGenericGame` (mixed archive pipeline), and `processMinervaDigital` (XBLA/DLC/XBLIG content pipeline).
  - `handleCacheRefresh` now rebuilds Minerva caches alongside IA for both `?platform=all` and per-platform requests; `minerva_<platform>` prefix refreshes Minerva only.
- **Pre-scrape script** (`scripts/scrape-minerva-cache.js`) — fetches all Minerva Xbox browse pages once and writes `cache/minerva_<platform>.json` files to the repo root.  Run with `npm run scrape:minerva` before building the installer so bundled caches are shipped day-one.  The existing `extraFiles` config in `electron-builder` already includes `cache/**/*`, so no packaging changes are needed.
- **Lua menu labels updated** — all Xbox/XBLA/DLC/Games menu items now read "(Minerva | Internet Archive)" to reflect the dual-source backend.

### Changed
- **Version** — **2.3.0** (root + Electron `package.json`, lockfile roots, backend banner `GODSend Backend Server v2.3.0`). Aurora script bundle **8.3.0** (`main.lua` / menu title).

---

## [2.2.5] — 2026-04-06

### Fixed
- **Content disc placeholder TitleID (`FFED2000`)** — Many publishers ship Add-On Content Discs whose `default.xex` carries a generic placeholder Title ID (`0xFFED2000`) instead of the parent game's real Title ID. The server now probes the STFS/CON content packages embedded in the disc (`content/0000000000000000/…/00000002/`): each package's STFS header at offset `0x0360` contains the correct parent Title ID. If the probe finds a valid non-placeholder Title ID it is used as the install destination; if the probe returns nothing the server falls back to game-name heuristics and logs a warning. Previously content always installed to the placeholder folder `FFED2000/00000002`, which Aurora/FSD would never associate with the parent game. (`utils.ProbeContentPackageTitleID`, `isContentDiscPlaceholderTitleID`, `guessTitleIDFromMultiDiscName` updated in `main.go` / `utils/iso2god.go`)
- **`guessTitleIDFromMultiDiscName`** — now matches "add-on content" in the game name for Borderlands GOTY, so the name-based fallback also works when the disc is titled "Borderlands - Game of the Year Edition (USA) (Add-On Content Disc)".

### Changed
- **Version** — **2.2.5** (root + Electron `package.json`, backend banner `GODSend Backend Server v2.2.5`). Aurora script bundle **8.2.4** (`main.lua` / menu title; no Lua logic changes).

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
