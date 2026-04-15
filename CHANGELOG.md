# Changelog

All notable changes to GODsend-360 are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Changed
- **Xbox Library: fingerprint-based asset caching** — Aurora visual asset sync (`syncAuroraTitleVisualAssets`) now collects lightweight FTP SIZE fingerprints for all source files (.asset, .bin, Import directory listing) and stores them in `visual-manifest.json`. On subsequent refreshes, remote sizes are compared to cached fingerprints; if all match for a game, the entire visual sync is skipped. This avoids redundant FTP downloads and RXEA decodes for unchanged games, dramatically speeding up library refreshes when only a few games have new or updated assets.

### Added
- **Store: drive selector fetches from FTP** — the destination drive `<select>` in the Browse / Store queue dialog now fetches the actual drives present on the connected Xbox via FTP on page load (same call as "Fetch drives from Xbox" in Settings) instead of showing a hardcoded static list. Only drives that exist on the console and match valid Xbox drive patterns (`Hdd1`, `Usb0`, `Usb1`, `Usb2`, …) are offered.

### Changed
- **Drive listing restricted to valid Xbox drives** — `xbox:list-drives` (used by Default Xbox drive in Settings, the Library "Move to Drive" panel, and the Browse queue dialog) now returns only directories actually present at the FTP root that match the Xbox drive naming convention (`Hdd\d*` / `Usb\d+`). The previous behaviour of always merging a hardcoded fallback list (`Hdd1:`, `Usb0:`, `Usb1:`, `Usb2:`) regardless of what the console reported has been removed.
- **Store: Content install method renamed** — the "Content" button in the Browse queue dialog install-method selector is now labelled "Content (DLC/Multi-Disc)" to better describe its purpose.

### Removed
- **Xbox Library sources setting removed** — the "Xbox Library sources" section (drive checkboxes in Settings, associated IPC channels `config:get-aurora-library-sources` / `config:set-aurora-library-sources`, and the `auroraLibrarySources` config field) has been removed. All drives are now scanned automatically; game covers are shown in full colour based on whether a source drive was detected, with no manual filtering.

### Changed
- **Electron main process migrated to TypeScript** — all source files under `src/electron-app/` (entry points, app layer, services, infrastructure, IPC handlers, preload) converted from JavaScript to TypeScript. `tsconfig.json` compiles in-place (`commonjs`, no `outDir`); packaged builds exclude `.ts` source files. Type-checks clean with `strict: false`.
- **Electron build cleanup for compiled JS** — packaging scripts now run `clean:compiled-js` after `electron-builder` finishes, removing in-place TypeScript output files (`main.js`, `preload.js`, and generated `.js` mirrors under `app/`, `services/`, `infrastructure/`, and `ipc/`) once they are no longer needed.
- **RXEA encode accepts any image format** — the Go backend's `/rxea/encode` endpoint now accepts JPEG, PNG, and any Go-supported image format (previously PNG-only). Images are decoded generically before DXT5 encoding.

### Added
- **Auto Aurora assets on download** — after a game FTP transfer completes the backend emits a structured event; the Electron app automatically fetches cover, background, banner, and icon for the title from XboxUnity and Xbox Live CDN and uploads them to `Aurora/User/Import/{TitleId}/` on the console so Aurora displays artwork immediately without manual asset management.
- **Auto Xbox Library sync** — the local Aurora library cache (content.db + settings.db) is automatically re-downloaded and updated after every successful game FTP transfer and after every game drive move, keeping the Library page in sync with the console without a manual refresh.
- **FTP Manager: Cut/Copy/Paste** — right-click context menu on files and folders with Cut, Copy, Paste, and Delete actions. Cut uses FTP `RNFR`/`RNTO` (rename/move); Copy downloads to a temp file and re-uploads. Multi-select supported via Ctrl+Click and Shift+Click with a selection toolbar showing count, Select All, Deselect, and bulk Delete.
- **FTP Manager: Clipboard dropdown** — a Clipboard button in the toolbar shows a badge with the number of items in the clipboard and a dropdown listing the cut/copied items with their source directory. The button is highlighted when the clipboard is non-empty and greyed out when empty.
- **Library: Move Game to Drive** — the game details page now includes a "Move to Drive" section that lists available Xbox drives (excluding the current one). Selecting a target drive and clicking Move queues an FTP transfer job visible in the FTP Manager's transfer panel, which moves the entire game directory to the chosen drive. Uses rename (fast) when supported, falling back to download-reupload-delete.
- **FTP rename/copy IPC** — new `tools:ftp-rename` and `tools:ftp-copy` IPC handlers for FTP Manager clipboard operations; new `xbox:move-game` handler for game drive transfers.
- **Library sorting & filtering** — the Xbox Library page now includes a search bar (filters by name, title ID, publisher, or developer), a sort dropdown (Name A-Z/Z-A, Rating, Last played, Most played, Drive, Favorites first), and a filter dropdown (All, Favorites, On-drive, Multi-disc). The header badge updates to show filtered/total count. A "no results" empty state with a clear-filters button is shown when search or filters exclude all games.
- **Toolbox dropdown** — new wrench icon button in the HomePage header bar, between Browse and Restart, with three tools:

### Fixed
- **Library covers fail past ~16 games** — replaced the Three.js WebGL `<Canvas>` per game card in `XboxBoxCover` with pure CSS 3D transforms. Chromium limits concurrent WebGL contexts to ~16; with a large library the oldest contexts were forcibly evicted, causing covers that initially loaded to go blank/broken. CSS 3D has no such limit and is lighter on GPU memory.
- **Homebrew / non-standard title IDs broken** — games with title IDs above `0x7FFFFFFF` (e.g. XeXMenu `C0DE9999`) were stored as negative signed integers in Aurora's SQLite DB. The JS conversion (`Number(titleId).toString(16)`) produced invalid hex like `-3F216667`, breaking FTP asset paths, RXEA file names, Import folder paths, and the upload validation regex (`/^[0-9A-F]{8}$/`). Fixed by using unsigned 32-bit conversion (`>>> 0`) so the title ID is always the correct 8-char hex.
- **Asset search selects thumbnail instead of full image** — when selecting a cover from XboxUnity search results, the pending asset stored only the thumbnail URL for display and lightbox preview. Now the full-resolution image is fetched in the background via `fetchUrlImage` and replaces the thumbnail once loaded, so the lightbox shows the full image and uploads use full resolution.
- **Covers stuck in loading state when no cover exists** — `emitAuroraCoverEvents` now always sends an `xbox-cover` event (with `src: null` when no cover file is cached) so the renderer transitions from the animated-pulse loading state to the "no cover" state instead of loading forever.
- **FTP failure leaves remaining covers in loading state** — when the outer FTP connection error fires during the cover sync loop, events are now emitted for all remaining unprocessed games from the disk cache so they show their cached cover or "no cover" instead of pulsing indefinitely.
- **Library covers flash to empty** — `loadAuroraLibrary()` and the 2-minute poll no longer wipe `covers` / `titleVisuals` state before async re-fetch; cover push events now update incrementally so the grid never shows blank cards. State is only cleared on an explicit force refresh.
- **3D box cover clipped in grid and detail view** — moved the Three.js camera back from z=3.2 to z=3.8 so the full Xbox 360 case geometry (1.5 x 2.0) fits within the viewport with margin instead of being cropped on all edges.
- **Cover slot in asset editor shows only front crop** — the cover slot card used `object-fit: cover` with `right center` position, cropping the image to only show the front portion. Changed to `object-fit: contain` so the full image is visible in the thumbnail and lightbox.
- **Saved cover art not visible on console after upload** — "Save to Console" previously uploaded flat images to `Aurora/User/Import/{titleId}/` which required Aurora to process them on next library scan. Now encodes images as RXEA `.asset` files and uploads directly to `Aurora/Data/GameData/{dir}/`, making artwork immediately visible on the console without a scan. The local visual cache is also invalidated so the next library refresh picks up the new image.

### Changed
- **Version** — **2.8.1** (root + Electron `package.json`, lockfiles, backend banner, Aurora script `scriptVersion`).
- **Go backend DDD refactor** — restructured the Go backend from 20 flat `package main` files into a proper DDD-style package layout with dependency injection. New package structure: `models/` (pure domain types), `app/` (central `App` struct holding all shared state, config, logging), `infrastructure/` (helpers, download, ftp, torrent), `services/` (cache/ia, cache/minerva, cache/rom, local, pipeline), `interfaces/http/` (Deps struct, handlers, router). All services receive `*app.App` via constructor injection; no global mutable state remains in `package main`. `main.go` (~180 lines) is wiring only. All HTTP endpoints, behaviour, and external contracts are unchanged.

## [2.7.5] — 2026-04-14

### Changed
- **Version** — **2.7.5** (root + Electron `package.json`, lockfiles, backend banner); Aurora script **11.2.2**.

### Added
- **Pure-Go RXEA codec** (`src/server/utils/rxea.go`) — bidirectional Aurora `.asset` file converter. Decode: RXEA bytes → `[]image.NRGBA` PNG images. Encode: `image.Image` → RXEA bytes. Implements the full RXEA container (25-slot entry table, 2048-byte aligned data section), Xbox 360 Xenos GPU texture un-tiling (8×8 DXT-block Morton-curve macro-tiles), 8-in-32 endian swap for DXT data, and DXT1/DXT3/DXT5/8888 pixel decompression. DXT5 compression uses bounding-box colour quantisation. Two new Go HTTP endpoints: `POST /rxea/decode` → JSON `{slots:[{slot,width,height,png}]}` and `POST /rxea/encode?slot=N` → raw RXEA bytes.
- **`xbox:decode-asset` IPC** — FTP-downloads `BK/GC/GL/SS{TitleId}.asset` files, posts each to `/rxea/decode`, and returns slot→PNG data-URL map so the renderer can show exactly what textures Aurora is displaying on the console. Used by `syncAuroraTitleVisualAssets` as the second-highest priority source (after User/Import, before CDN fallback).
- **`xbox:encode-asset` IPC** — takes a PNG + slot index, posts to `/rxea/encode`, and FTPs the resulting RXEA bytes to `Data/GameData/{dir}/{PREFIX}{TitleId}.asset` — replacing the asset in-place immediately without requiring an Aurora library rescan.
- **`decodeAsset` / `encodeAsset` renderer APIs** — exposed via `preload.js` as `window.godsendApi.decodeAsset` and `window.godsendApi.encodeAsset`.
- **Game Details: Aurora Asset Editor** — the read-only "Aurora files on Xbox" WIP section is replaced with a full per-slot asset editor. Each slot (Background, Banner, Icon, Cover, Screenshots) shows the currently cached image, a **Search** button that queries XboxUnity by title name and TitleID, and a **File** button that opens a native file picker. Selecting an asset stages it as a pending upload (shown with a blue dot). **Save to Console** uploads all staged images to `Aurora/User/Import/{TitleID}/` via FTP; Aurora processes them on the next library scan.
- **XboxUnity asset search** — new IPC `xbox:search-assets` queries `xboxunity.net/api/Covers/` by TitleID then by name, sorts results by official/rating, and prepends an Xbox CDN high-res cover when a titleid is returned. Exposed as `window.godsendApi.searchAssets`.
- **FTP asset upload to console** — new IPC `xbox:upload-asset-to-console` accepts an image as base64 or a URL (fetched server-side), uploads to `User/Import/{TitleID}/{assetType}.{ext}` via FTP using a `Readable` stream, then bumps the cache fingerprint so the next library load re-syncs. Exposed as `window.godsendApi.uploadAssetToConsole`.
- **Native image file picker** — new IPC `xbox:choose-image-file` opens a dialog filtered to JPEG/PNG/BMP/GIF and returns a data URL for in-app preview. Exposed as `window.godsendApi.chooseAssetImageFile`.

### Fixed
- **Aurora asset sync: all slots showing "—"** — `syncAuroraTitleVisualAssets` now fetches real displayable images via `GameAssetInfo.bin` (Xbox Live Atom XML stored per-title in `Data/GameData/{dir}/`). The XML contains `download.xbox.com` CDN URLs indexed by `<live:relationshipType>` (23=icon, 25=background, 27=banner, 33=cover) and screenshot URLs in `<live:slideShows>`. All URLs are confirmed live (HTTP 200). New `parseGameAssetInfoXml()` helper extracts them via regex.
- **Aurora `.asset` binary files removed from sync** — Aurora's `BK/GC/GL/SS{TitleId}.asset` files use the RXEA GPU-texture format (big-endian DXT-compressed Xenos textures) and are **not decodable** as JPEG/PNG without a platform DXT decompressor. The previous magic-byte scan produced false positives from DXT data. These files are now ignored in favour of CDN images.
- **Aurora asset sync priority order** corrected: `User/Import/{TitleID}/` (highest, user-placed files) → `GameAssetInfo.bin` CDN URLs (Xbox Live images) → `GameCoverInfo.bin` XboxUnity cover (`mediaCover` fallback).
- **Media/ directory no longer scanned per-title** — `Aurora/Media/` contains Aurora's UI system assets (Fonts, Layouts, Scripts, Effects), not game artwork. The per-title Media scan and the shared `mediaDirSnapshot` LIST are removed; this eliminates hundreds of wasted FTP calls per sync session.

### Changed
- **Electron: no visible logs during Aurora library / artwork sync** — Aurora FTP steps now call `addOutputLine`, so lines appear in the Home **Console** output and in `godsend-server-*.log` as `ELECTRON_UI` (start, cache hit, DB download, scan-path probe, artwork progress every 25 titles, per-title warnings, fatal errors). `addOutputLine` also falls back to any open `BrowserWindow` if the tray window ref is missing.
- **Electron: Xbox Library refresh / cover sync appearing stuck** — artwork sync no longer runs `LIST` on the entire Aurora `Media/` folder once per game (which multiplied FTP traffic by library size). The Media directory is listed **once** per `fetchAuroraCovers` session and reused; the main thread also yields every few titles. Scan-path drive probing after a DB download uses a shorter FTP timeout so a bad path cannot stall for minutes per attempt.
- **Electron: Synced artwork stuck on “loads with the library FTP sync…”** — per-title FTP sync errors no longer skip pushing `xbox-title-visuals`; the main process always emits visuals (empty manifest when needed) in a `finally` block, re-reads the cache when opening game detail (`xbox:refresh-title-visuals-cache`), and falls back to any live `BrowserWindow` when pushing IPC if the tray window ref is stale.
- **Electron main process startup** — removed duplicate `net` binding (`require("net")` vs Electron’s `net`); Aurora cache protocol handler now uses `electron.net` explicitly so the packaged app loads `bootstrap.js` without a syntax error.

### Added
- **Electron: Aurora library disk cache** — Xbox Library caches Aurora `content.db` / `settings.db` and a **single primary** cover per title under `%APPDATA%` (per console IP + Aurora root). FTP `SIZE` checks detect DB changes; while unchanged, the UI reads local SQLite and serves the primary image via `godsend-aurora://`. **Refresh** forces a full re-sync; the library page **polls every 2 minutes** for DB changes. **`inspectAuroraGame` IPC** lists each title’s `Data/GameData/…` files (including `.asset`), `Media/{TitleID}*` matches, and a **read-only summary** of `GameCoverInfo.bin` (entry count + flags — no bulk cover downloads). Game detail shows **library DB fields** (scan path, media ID, file/content types, directory) and links to [Aurora Asset Editor](https://github.com/XboxUnity/AuroraAssetEditor) for future in-app editing.
- **Electron: Aurora artwork sync** — After each title’s primary cover, the app pulls **background, banner, icon, screenshots** from `Aurora/User/Import/<TitleID>/` ([ConsoleMods import layout](https://consolemods.org/wiki/Xbox_360:Aurora_Import_Format)) and image files under flat `Media/<TitleID>*` (suffix heuristics: **GC** cover, **BK/BG** background, **BN/BA** banner, **IC/IL/IS** icon, **SS/SC** screenshots). Files are cached under `aurora-library-cache/.../visual/` and surfaced in game detail via **`xbox-title-visuals`** (`godsend-aurora://` URLs). DDS and other non-web formats are stored but only show a “cached” placeholder in the UI.

### Removed
- **Browse `title_id` in cache JSON + `/browse?format=json`** — IA and Minerva cache files no longer store optional `title_id` on entries; `/browse` returns only the pipe-separated title list (optional `source=` unchanged). Removed `npm run enrich:cache-title-ids` and `scripts/enrich-cache-title-ids.mjs`.

### Changed
- **Electron: Xbox Library cover UX** — Removed downloading every `GameCoverInfo` URL and the multi-image gallery; only the best-rated/official primary cover is cached for the grid/detail (alternate entries remain on-console for a future picker / editor).
- **Electron: Browse Library + cover fetch** — Loads browse lists via plain `/browse` text again. Cover resolution: XboxUnity Covers (and CDN when the row has `titleid`), TitleList (+ series-stripped retry), Microsoft Store Display Catalog → legacy Title ID → CDN, then Wikipedia.

---

## [2.7.3] — 2026-04-12

### Fixed
- **Minerva lookup vs browse list** — titles scraped with HTML entities in the filename (e.g. `&#39;` for apostrophes) were shown decoded in the Minerva browse UI but failed `/trigger` with “Not found in Minerva Archive”. The backend now indexes and resolves both encoded and decoded forms, and torrent file matching tolerates the same mismatch.

### Added
- **Browse cache `title_id` + JSON list** — IA and Minerva cache entries may store optional `title_id` (8-char hex). `GET /browse?format=json` returns `[{"name","title_id"},…]` alongside the existing pipe-separated form (default). The Electron Browse Library uses JSON, passes `title_id` into cover fetch to hit the Xbox catalog CDN without an extra TitleList round-trip when the id is known. Script `npm run enrich:cache-title-ids` fills `title_id` using local bulk data first (embedded `iso2god_titles.jsonl`, cached [AdrianCassar gist](https://gist.github.com/AdrianCassar/c0d05a14608168259232b3ed8c77f28c) JSON, cached [XboxDB](https://xboxdb.altervista.org/browse/f) browse scrape), then XboxUnity Covers + TitleList (`cache/title_id_lookup_cache.json` for per-term resume). `--refresh-datasets` re-downloads gist + XboxDB. Cache rebuilds preserve existing `title_id` values when the same title keys are still present.
- **Electron: Aurora library view** — the Xbox Library panel now reads Aurora's `content.db` and `settings.db` directly via FTP instead of scanning FTP directories. Respects Aurora's hidden-game flag (`UserHidden`), surfaces favorites (`UserFavorites`), play counts and last-played dates (`UserRecentGames`), and full metadata (publisher, developer, description, star rating, release date, disc set). Clicking a game card opens a detail view with cover art, full metadata, and footer TitleID/ContentID.
- **Electron: Aurora library sources setting** — new Settings section to select which Xbox drives (Hdd1, Usb0, Usb1, Usb2) count as active library sources; games whose drive is not in the selected set are shown greyed-out in the library grid.
- **Backend: FTP drive-probing for ScanPaths** — drive assignment for each game is determined by probing FTP (`cd /{drive}{Directory}`) per unique `ScanPathId` at library-load time, rather than via the unreliable `ScanPaths.DeviceId → MountedDevices.DeviceId` join (which references stale device configs).
- **Backend: Persistent pending FTP queue** — if the Xbox goes offline mid-transfer (e.g. a game is launched), downloaded and converted files are preserved on disk and the backend retries the FTP transfer indefinitely (30 s → 5 min exponential backoff) until the console is reachable again. Pending jobs survive backend/app restarts and are resumed automatically.
- **Electron: Queue viewer** — main window shows a **Queue** button (visible when jobs are active) that opens a live queue view with per-job status, progress, and remove controls; auto-refreshes every 3 seconds.
- **Electron: Clear local app data** — new Settings section to purge pending FTP jobs, Ready/ and Temp/ directories; warns when active/pending jobs exist and requires confirmation.
- **Electron: Aria2 port settings** — new Settings section to configure the aria2 listen port and DHT port used for Minerva/torrent downloads; lets users open specific firewall rules.
- **Electron: Default Xbox drive** — new Settings section to fetch available storage drives from the Xbox via FTP and set a default destination; when set the Aurora script skips the drive picker on every download.

### Changed
- **Internet Archive HTTP downloads** — Removed the Settings **Parallel download connections** slider and Electron `GODSEND_IA_CONCURRENCY` wiring. Large IA (and EdgeEmu ROM) files now use a download-manager style queue of fixed-size byte ranges with up to 16 parallel range requests by default (pattern inspired by [Gopeed](https://github.com/GopeedLab/gopeed)); optional env `GODSEND_IA_MAX_CONNECTIONS` (1–32) or legacy `GODSEND_IA_CONCURRENCY` for headless tuning. Progress logs no longer show a trailing `Nx` connection count.
- **Electron: Browse cover fetch** — Uses XboxUnity `/api/Covers` metadata (`titleid`) to prefer Microsoft catalog art when available, with TitleList + series-stripped TitleList retries as fallbacks.
- **Aurora script** — Background download prompts and the post-dismissal message now say to run **Aurora's Scan Content** after the FTP transfer finishes so the title shows up (replacing wording that implied it would appear automatically).
- **Aurora: FTP-only transfer mode** — the transfer method prompt (FTP vs HTTP) has been removed; all transfers now go directly via FTP. HTTP packaging is no longer presented as an option.
- **Version** — **2.7.3** (root + Electron `package.json`, lockfiles, backend banner); Aurora script **11.2.1**.

---

## [2.7.1] — 2026-04-11

### Added
- **Electron: React + Vite renderer** — new UI stack for the desktop app (`renderer/main.jsx`, page components, and shadcn-style primitives under `renderer/components/ui/`). Tailwind CSS and PostCSS config; production bundle written to `renderer-dist/`. Dev: `npm run renderer:dev --prefix src/electron-app` with `VITE_DEV_SERVER_URL` loaded by the main window.

### Changed
- **Electron shell** — `index.html` mounts the React app; window loads `renderer-dist` in production or the Vite dev server when configured.
- **Version** — **2.7.1** (root + Electron `package.json`, lockfiles, backend banner); Aurora script **11.1.0** (unchanged).

---

## [2.7.0] — 2026-04-11

### Added
- **Documentation** — `docs/api-reference.md`, `docs/building.md`, `docs/features.md`, and `docs/headless-setup.md`; Aurora and multi-disc guides live under `docs/reference/`.
- **Electron: FTP debugging tools** — collapsible **FTP Debugging Tools** on the Xbox connection settings page: **Test Connection** (login, PWD, root listing with verbose FTP log), **Scan Network Ports** (probes port 21 on a `/24` from a subnet like `192.168.1`), and a clearable debug console. New IPC: `xbox:ftp-test`, `xbox:ftp-scan`, and `godsend-ftp-debug` events (see `preload.js`).

### Changed
- **Backend: Internet Archive parallel chunk retries** — increased chunk retry count and base backoff (`iaChunkRetries` / `iaChunkRetryBase`) for more resilient large downloads.
- **Backend: local Transfer-folder ISO matching** — widened the leaked-title tail pattern (digits, slightly longer tails) and added a **60% prefix** fallback when exactly one ISO basename matches the prefix, for corrupted or truncated Aurora `game` query values.
- **Backend: digital / Minerva digital content discovery** — minimum candidate file size lowered from 1 MiB to **0x368** bytes so small legitimate packages are not skipped.
- **Backend & Aurora: DLC / XBLIG install drive** — FTP staging for DLC and XBLIG now uses the **same user-selected drive** as other platforms (no forced `Hdd1:`); Aurora always shows the drive picker, including for DLC.
- **Aurora: library browser UX** — shorter main-menu labels; browse titles no longer append redundant source suffixes; game list popups run in a loop with **`collectgarbage()`** before each list to reduce memory pressure on-console.
- **README** — table of contents; **Running Without the Desktop App** (headless backend, prebuilt binaries table, env-var configuration); quick-install links target the **v2.7.0** release (per-file GitGud project upload URLs added when assets were published).
- **Version** — **2.7.0** (root + Electron `package.json`, lockfiles, backend banner); Aurora script **11.1.0**.

---

## [2.6.0] — 2026-04-10

### Added
- **Automatic free port when the configured port is busy** — if the backend cannot bind to `GODSEND_PORT` (e.g. default 8080), it tries the next ports until one succeeds. The effective port is logged as `GODSEND_LISTEN_PORT=…`; the Electron app persists that value so the UI and next launch stay aligned.

### Changed
- **Version** — **2.6.0** (root + Electron `package.json`, lockfiles, backend banner); Aurora script **11.0.2**.

---

## [2.5.2] — 2026-04-10

### Fixed
- **aria2c failures now report the actual error** — the torrent download path only logged lines matching the progress regex, so warnings, errors and abort messages were silently dropped and users only saw `aria2c: signal: abort trap`. Non-progress aria2c output is now forwarded to the server log in real time and the last 50 lines are appended to the returned error.

### Changed
- **macOS Minerva torrents: no bundled aria2c; Homebrew bootstrap at backend startup** — the Electron mac app no longer ships `aria2c` / `aria2c-lib`. On launch, the Go backend prepends `/opt/homebrew/bin` and `/usr/local/bin` to `PATH`, and if no working `aria2c` is found it runs the official Homebrew installer with `NONINTERACTIVE=1` / `CI=1`, then `brew install aria2` (also non-interactive). If that fails (e.g. no TTY for `sudo`), it retries with **`SUDO_ASKPASS`**: a small helper uses **osascript** to show the standard graphical password dialog, and the Homebrew installer runs as the **current user** (Homebrew aborts if the install script is run as root). Set `GODSEND_SKIP_ARIA2_BOOTSTRAP=1` to skip automatic install (e.g. IA-only use); set `GODSEND_NO_GUI_ELEVATION=1` to skip the GUI askpass retry (e.g. CI). `scripts/download-aria2.js` now fetches Windows + Linux binaries only; mac builds no longer require dylibbundler or per-arch darwin bundles.
- **Version** — **2.5.2** (root + Electron `package.json`, lockfiles, backend banner).

---

## [2.5.1] — 2026-04-10

### Fixed
- **Backend FTP push now respects configured Aurora credentials** — `connectToXboxFTP` previously hardcoded `xboxftp`/`xboxftp`, so users who changed Aurora's FTP credentials could upload scripts from the Electron app but post-torrent FTP installs failed at login. The Electron settings (`ftpUser`/`ftpPassword`) are now exported as `GODSEND_FTP_USER` / `GODSEND_FTP_PASS` to the backend and used for every FTP login.

### Added
- **"Save connection" button in Xbox FTP settings** — explicitly persists the Xbox IP, FTP username/password, and scripts path, and restarts the backend so post-download FTP installs immediately pick up the new credentials. Saving also happens automatically when uploading Aurora scripts, but the dedicated button makes it possible to update credentials without re-uploading scripts.

### Changed
- **Version** — **2.5.1** (root + Electron `package.json`, lockfile root, backend banner).

---

## [2.5.0] — 2026-04-08

### Fixed
- **Windows desktop/taskbar icon resolution** — fixed the NSIS desktop shortcut to use the installed `GODsend.exe` icon instead of a non-existent `$INSTDIR\\assets\\tray.ico` path, prioritized `icon.*` over `tray.*` for window/taskbar icon lookup, and updated icon sync logic to stop overwriting `icon.ico` when it already exists.
- **Local ISO detection with leaked Aurora title suffixes** — backend local-file matching now tolerates short alphabetic tails accidentally appended to `game` query values (for example `...Disc)in` / `...Disc)our PC`), so exact ISO basenames in the Transfer folder are still resolved.
- **Aurora `game` query sanitization for trigger/register/status** — client-side title cleanup now strips short leaked prompt tails appended after a closing `)` (for example `...Disc)in` / `...Disc)our PC`) before URL encoding, preventing malformed local lookup requests.

### Changed
- **Aurora script deployment now patches `state.lua` directly** — FTP upload now writes the detected PC IP and selected backend port directly into `BRAIN_IP` and `PORT`, and startup uses those values without runtime INI override logic.
- **Documentation refresh for host/port setup** — README now documents `state.lua` as the source of truth (`BRAIN_IP`/`PORT`), the configurable backend port flow in Electron settings, and generic `http://<pc-ip>:<port>` troubleshooting guidance.
- **Version** — **2.5.0** (root + Electron `package.json`, lockfile root, backend banner) and Aurora script **11.0.1**.

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
