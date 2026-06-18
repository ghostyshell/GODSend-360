# Features

Each item is a **high-level capability**, **how you use it**, and **how it works** under the hood.

## Desktop app (Electron) + backend

- **What:** Run the Go HTTP server with a full desktop UI — **Windows** (NSIS installer), **macOS** (DMG), or **Linux** (AppImage). Includes Xbox Library, FTP Manager, Aurora Asset Editor, ISO conversion tools, unified job queue, and one-click Aurora script deployment.
- **How:** Launch GODsend from the **Start menu** (Windows), **Applications** (macOS), or your **app launcher** (Linux; tray icon support depends on the desktop environment). Use the tray icon to open the window. Tools open as overlay panels on top of the current page (close with X, no back-navigation needed). Restart the backend from the home screen; optional **Launch at login** in Settings. Set **Backend server port** first (if needed), then under **Xbox connection** enter your Xbox IP and click **FTP Aurora Scripts to Xbox** — your computer’s LAN IP + selected backend port are patched into `state.lua` automatically and upload progress is shown file-by-file.
- **How it works:** Electron (TypeScript main process) spawns the Go backend (`godsend-backend` / `godsend-backend.exe`) with a writable runtime (`Transfer`, `Ready`, `Temp`, `cache`) and injects `GODSEND_*` environment variables from your settings. All FTP operations are centralised through the Go backend’s `ftp.Manager` (no npm FTP dependency). The React/Vite renderer communicates via typed IPC channels exposed through `preload.ts`.

## Minerva Archive (BitTorrent — no account needed)

- **What:** Xbox 360, OG Xbox, XBLA, DLC, XBLIG, and Game Archive libraries sourced from [minerva-archive.org](https://minerva-archive.org) — no account or login required. Works out of the box.
- **How:** When browsing any game library, select **Minerva Archive** as the source. Game lists are bundled in the installer so browsing is instant.
- **How it works:** The backend fetches the Minerva collection torrent, finds the requested file's index, and uses `aria2c` to download only that file via BitTorrent (`--select-file`). Pieces land in **torrent download temp** (`GODSEND_HOME/Temp/torrent-dl` by default, or `GODSEND_TORRENT_TEMP` / Settings → **Torrent download temp**), then move into processing `Temp/` for extraction/conversion. **Windows** and **Linux** desktop builds ship a bundled `aria2c` next to the backend; the **Windows NSIS** installer can add OS firewall rules for `aria2c` so Windows does not prompt on first torrent use. **macOS** does not bundle `aria2c` — the backend prepends Homebrew to `PATH` and, if needed, tries a non-interactive Homebrew install plus `brew install aria2` at startup; if `sudo` is unavailable (typical when launched from the GUI), it sets **`SUDO_ASKPASS`** so the installer runs as your user and macOS shows the password dialog when Homebrew needs `sudo` (the installer cannot run as root). Progress is reported to the Aurora queue display every 3 seconds.

## Internet Archive account & parallel downloads (optional fallback)

- **What:** Authenticated downloads from archive.org collections — useful for titles not available on Minerva.
- **How:** Settings → **Internet Archive account** → **Log in**. Select **Internet Archive** as the source when browsing. Large files use automatic chunked parallel HTTP downloads (no connection slider).
- **How it works:** The app stores session cookies locally (not your password), passes them to the backend, which fetches items with multiple range-request workers for faster ISO/archive retrieval.

## Local Transfer folder (your own ISOs)

- **What:** Install disc games from `.iso` files you already have, without re-downloading from IA.
- **How:** Settings → set **Local Transfer folder** (or use the default runtime `Transfer` folder). Drop ISOs there. On the Xbox, open **Local Library** or trigger a title that matches a filename in that folder.
- **How it works:** For Xbox 360 / original Xbox / `local` browse, the backend prefers a matching ISO under `Transfer` over Internet Archive and runs the same conversion pipeline locally.

## Storage paths and temporary directories

- **What:** Control where GODsend stores working files — separate from Windows `%TEMP%`.
- **How:** Settings → **App data directory** (config, logs, caches), **Local storage path** (`GODSEND_HOME`: `Temp/`, `Ready/`, `Transfer/`, `cache/`), and **Temporary directories** (read-only **Processing temp** path plus configurable **Torrent download temp**).
- **How it works:** Electron writes `storagePath` / `torrentTempPath` to `config.json` and passes `GODSEND_HOME` / `GODSEND_TORRENT_TEMP` to the Go backend on spawn. Processing temp holds extraction, ISO→GOD, FTP staging, and post-torrent job folders; torrent temp is where aria2c writes active Minerva downloads (`gd-dl-*`) before they move into processing temp.

## Library metadata caches

- **What:** Faster startup after the first run; optional forced refresh when collections change.
- **How:** First launch may take a minute while lists build. In Settings, **Refresh Cache** rebuilds IA/ROM indexes in the background. On the console, **Server Queue & Status** shows aggregate cache readiness and per-platform detail.
- **How it works:** The server persists lists under `cache/` and serves `/browse` from memory; `/cache-refresh` and the Electron button trigger rebuilds without blocking the HTTP server indefinitely.

## Server queue, status, and job cleanup

- **What:** See everything the backend is processing or has finished; clear stuck or old jobs.
- **How:** Aurora → **Server Queue & Status** — refresh the list, open **Cache** for build state, **Clear ALL server jobs** or remove one job from its submenu.
- **How it works:** The script polls `/queue` and `/cache-status`. Removals call `/queue/remove` (GET/POST), which deletes entries and suppresses stray status updates for cleared games.

## Browse & install: Xbox 360 / original Xbox disc libraries

- **What:** Redump-style ISO libraries from Minerva Archive or Internet Archive, converted for Aurora. The script offers **GOD**, **DLC** (content install), or **XEX** for every title — pick the install layout that matches the disc.
- **How:** Main menu → **Xbox 360 Redump ISOs** or **Original Xbox Redump ISOs** → pick source (Minerva / Internet Archive) → letter folder → title → destination drive → install type → confirm. The backend downloads, converts, and pushes over FTP automatically. A **[Recommended]** label appears when the server can determine the correct layout from the disc.
- **How it works:** Backend downloads the ISO from Minerva (via BitTorrent) or Internet Archive (chunked parallel HTTP), or uses your local copy. Converts to GOD format natively (no external tools required), then pushes directly to the Xbox over FTP. If the Xbox is unreachable (e.g. a game was launched), the transfer is saved and retried automatically — no data is lost. Title names are resolved from XboxUnity → XboxDB → an embedded title list and used to name the GOD folder on the Xbox (e.g. `Open Season - 5454082A`) so Aurora shows the correct title.

## Multi-disc game support

- **What:** Correct handling of multi-disc games where Disc 2 (or later) is DLC or bonus content rather than a standalone game — the server recommends the right install method per disc.
- **How:** When triggering a Disc 2+ title the Aurora menu shows **GOD** or **Content** options with a **[Recommended]** label. Select the recommended option; for content discs the files land in `Content\0000000000000000\{TitleID}\00000002\` on the chosen drive. Disc 1 is always installed as GOD in the normal flow.
- **How it works:** `/disc-info` checks the disc against a 40+ title compatibility table (`discCompatTable`) that maps each game's Title ID to the correct install method. Content discs often carry a generic placeholder Title ID (`FFED2000`) in their `default.xex`; the server automatically reads the real Title ID from the STFS/CON packages on the disc (header offset `0x0360`) so the content lands in the right folder even without manual input. Covered games include Borderlands (GOTY), Borderlands 2 (GOTY), Call of Duty, Mass Effect, Red Dead Redemption, Skyrim, L.A. Noire, and many more.

## Browse & install: XBLA, digital (No-Intro), DLC, Xbox Live Indie Games, 360 game archives

- **What:** Non-disc content (arcade packages, digital titles, DLC, indie games, pre-packed game archives) from Minerva Archive or Internet Archive.
- **How:** Choose the matching main-menu entry → pick source (Minerva / Internet Archive) → same browse flow. All content types show the drive picker so you can install to any drive.
- **How it works:** Backend downloads from Minerva (BitTorrent) or Internet Archive and unpacks archives natively (no external tools required), then FTPs content directly to the Xbox. Install type may be **GOD** (content tree to `[drive]\GOD\`), **raw** (package to its content path), or **xex** (loose folder to `[drive]\XEX\`).

## Install layouts: GOD, XEX folder, content (DLC), raw, ROMs

- **What:** Different on-disk layouts depending on title type. The backend handles all conversion and packaging natively — no external tools required.
- **How:** After selecting a game and drive, the script asks **every** **Xbox 360 / Original / Local / Games Archive** title for **GOD / DLC / XEX**. Follow prompts until success, then **Settings → Content → Scan** in Aurora (or launch RetroArch for ROMs).
- **How it works:**
  - **GOD** — ISO is converted to Games on Demand format natively; the backend FTPs the content tree directly to `[drive]\GOD\{Name} - {TitleID}\` on the Xbox. If the Xbox goes offline mid-transfer (e.g. a game was launched), the job is saved and retried automatically — no re-download needed.
  - **DLC (Content)** — Content files are extracted from the ISO and FTP'd to `Content\0000000000000000\{TitleID}\00000002\` on the target drive. The correct Title ID is resolved from the disc's content packages automatically (see Multi-disc game support above).
  - **XEX** — The backend walks the XDVDFS filesystem for `default.xex`/`default.xbe`, extracts that game root, and FTPs it to `[drive]\XEX\{folderName}\`.
  - **Raw** — Package is FTP'd directly to the appropriate content path on the target drive.
  - **ROM** — Archive is extracted and the ROM file is FTP'd to `[drive]\<ROM root>\<system folder>\` (configurable via Settings).

## Retro ROMs (EdgeEmu, many systems)

- **What:** Browse and install classic ROM sets scraped from EdgeEmu-compatible metadata (dozens of consoles/handhelds).
- **How:** **Retro ROMs** → pick system → folder → title → drive → same wait/install flow as other libraries.
- **How it works:** Backend fetches/builds per-system ROM lists (`rom_*` platforms), downloads archives when triggered, and emits a **rom**-type manifest with a drive-relative `rompath`. The script extracts under `[drive]\<ROM root>\<system folder>\`, where **ROM install path** in Settings sets the root (default `Emulators\RetroArch\roms`, passed to the backend as `GODSEND_ROM_PATH`).

## Persistent server logs

- **What:** Daily rotating log files that capture all backend activity — useful for diagnosing failed installs, FTP errors, IA download issues, or anything else that goes wrong.
- **How:** Logs are written automatically under **`logs/`** in Electron’s user-data directory (e.g. **`%APPDATA%\GODsend\logs\godsend-server-YYYY-MM-DD.log`** on Windows, **`~/Library/Application Support/GODsend/logs/`** on macOS — on Linux, use **Open logs folder** to see the exact path). On the home screen, click **Open logs folder** to open that directory in the system file manager.
- **How it works:** Each session opens with a banner that records app/Electron versions, OS, hostname, primary IPv4, `GODSEND_HOME`, backend executable path, effective Transfer folder, and all `GODSEND_*` environment variables (IA secrets redacted). Backend stdout/stderr are tagged `BACKEND_OUT`/`BACKEND_ERR`; UI events (FTP upload steps, cache refresh triggers, config saves, IA login) are tagged separately. Lines use ISO 8601 timestamps with PID so multi-process output is unambiguous.

## Xbox Library

- **What:** Live view of every game installed on your Aurora console — cover art, metadata, sorting, filtering, and drive management.
- **How:** Open **Xbox Library** from the home page. The first load downloads Aurora's content.db and settings.db via FTP and syncs cover art from RXEA assets, User/Import images, and online sources (Xbox CDN, XboxUnity). Subsequent refreshes use fingerprint-based caching (FTP SIZE checks and SHA-256 content hashes) to skip unchanged games — near-instant after the first sync. Use the **search bar** to filter by name, title ID, publisher, or developer. Sort by name, rating, last played, most played, drive, or favorites. Filter to favorites, on-drive, or multi-disc titles.
- **How it works:** `auroraLibraryService.ts` parses Aurora's SQLite databases into `AuroraGame[]` and probes FTP drives. `auroraVisualService.ts` syncs visual assets with a priority chain: User/Import files → RXEA `.asset` decode → GameAssetInfo.bin CDN URLs → GameCoverInfo.bin XboxUnity cover. Per-game fingerprints are stored in `visual-manifest.json` so unchanged assets are skipped on refresh. Covers render as CSS 3D box art (replacing the earlier WebGL approach that hit Chromium's ~16 context limit). Games with title IDs above `0x7FFFFFFF` (homebrew/unsigned) use unsigned 32-bit conversion to avoid negative hex strings. Per-game detail panels (Library Database, Asset Editor, Move to Drive, DLC & Title Updates, Save Games) are collapsible and lazy-load on first expand to keep initial page weight low.

## UI design system

- **What:** Cohesive OLED-friendly visual design across the desktop app — deep-space background, neon-green accents, monospaced body type, and consistent interaction states.
- **How it works:** The app uses a `#020617` OLED background with a `#22C55E` green accent, Orbitron headings + JetBrains Mono body via Google Fonts, smooth CSS transition tokens, and shared focus / hover states (cursor-pointer, focus-visible rings). The main nav highlights the active page with a green accent ring.

## FTP Manager

- **What:** Full file browser for your Xbox's filesystem — navigate, upload, download, cut/copy/paste, delete, rename, and create directories.
- **How:** Open **FTP Manager** from the home page (opens as an overlay panel). Browse the Xbox FTP root; right-click files or folders for Cut, Copy, Paste, Delete. Multi-select via Ctrl+Click and Shift+Click with a selection toolbar. The Clipboard dropdown shows pending items. Upload files via the upload button; transfers show streaming progress with speed and current filename.
- **How it works:** All FTP operations go through the Go backend's `ftp.Manager` (the `basic-ftp` npm dependency was fully removed in 2.8.4). 17 HTTP endpoints under `/ftp/*` handle list, upload, delete, mkdir, rename, copy, move, batch operations, and tracked async jobs. Cut uses FTP `RNFR`/`RNTO`; copy downloads to a temp file and re-uploads. The batch endpoint (`POST /ftp/batch`) executes multiple operations over a single FTP connection for efficiency.

## Aurora Asset Editor

- **What:** Search, preview, and upload cover, background, banner, icon, and screenshot artwork for any game on the console — using XboxUnity, Xbox CDN, or local image files.
- **How:** Open a game in the Xbox Library and scroll to the asset editor. Each slot (Background, Banner, Icon, Cover, Screenshots) shows the current image. Click **Search** to query XboxUnity by title name and ID, or **File** to pick a local image (JPEG/PNG/BMP/GIF). Staged uploads show a blue dot. Click **Save to Console** to push all staged images.
- **How it works:** The Go backend's `/rxea/encode` endpoint encodes images as RXEA `.asset` files (DXT5-compressed Xbox 360 Xenos GPU textures) and uploads them directly to `Aurora/Data/GameData/{dir}/` for immediate visibility without an Aurora rescan. `/rxea/decode` converts RXEA assets back to PNG for display. The backend accepts any Go-supported image format. XboxUnity search (`xbox:search-assets` IPC) queries the API by title ID and name, prepending Xbox CDN high-res covers when available.

## ISO to GOD and ISO to XEX tools

- **What:** Convert local `.iso` files to Games on Demand or XEX folder format without downloading anything — useful for ISOs you already have.
- **How:** Open **ISO to GOD** or **ISO to XEX** from the home page toolbox. Select an ISO file, choose a destination, and the backend converts and optionally transfers to the Xbox. Title names are resolved from XboxUnity → XboxDB → embedded title list → cleaned ISO filename (stripping region tags like "(USA)") as a final fallback.
- **How it works:** Uses the pure-Go ISO converter (`src/server/utils/iso2god.go`) via `/tools/iso2god`, `/tools/iso2xex`, and `/tools/probe-iso` HTTP endpoints. The probe endpoint reads disc metadata (title ID, media ID, disc number) without converting.

## DLC & Title Updates management

- **What:** Browse, install, activate, and remove DLC and Title Updates for any game on your Xbox — sourced from Minerva Archive, Internet Archive, and XboxUnity, with full awareness of what is already installed on the console.
- **How:** Open a game in the Xbox Library and expand the **DLC & Title Updates** section. Installed and candidate rows are merged into a single list per content type. Click **Install** to queue a download + FTP transfer; click the **Active / Inactive** toggle on an installed Title Update to switch versions (other TUs in the same folder are auto-disabled by renaming to `.disabled`). Use **Delete** / **Move** to remove or relocate content.
- **How it works:** The renderer issues `/content/discover` (DLC scan + Minerva / IA candidates) and `/content/tu` (XboxUnity Title Updates) independently so each list streams in as soon as its source responds. Installs go through `/content/queue` — the Minerva path downloads via `aria2c` torrent and the direct-URL path streams from XboxUnity / Internet Archive. The `.godsend.json` marker is written **before** upload so half-transferred files can still be matched back to their catalog entry on rescan. `/content/set-active` activates a single TU by renaming siblings to `.disabled`. The Aurora library background poll uses `lock_wait_ms` on `/ftp/batch` so a long upload no longer blocks the UI — when the FTP lock is busy, the renderer serves the last cached `content.db` / `settings.db` instead of spinning.

## Save game management & profile backup

- **What:** Browse, back up, restore, and delete Xbox 360 profile packages and per-game saves directly from the desktop app. One-click **Save Game Backup** pulls every profile and every save for every profile on the connected console into a local archive folder.
- **How:** Open a game in the Xbox Library and expand the **Save Games** section to list per-profile saves for that title — download a single save to the local backup folder, delete it from the console, or copy it between profiles. To back up everything at once, open **Settings → Save Game Backup** and click **Back up all profiles**. Bulk backups are organised by gamertag.
- **How it works:** The Go backend's `saves.Service` walks `/Content/<XUID>/...` over FTP, reading each profile's gamertag from the embedded `Account` blob — the file table is parsed with proper L1/L2 hash-table padding, the `Account` payload is decrypted with `RC4(HMAC-SHA1(RETAIL_KEY, file[0:0x10]))`, and the UTF-16BE gamertag is read from decrypted offset `0x10`. Both retail and devkit keys are tried (matches Velocity / py360 behaviour); the ASCII-scan heuristic is kept only as a last resort. Per-title display names are resolved through the existing XboxUnity → XboxDB → embedded list chain. Files land at `<localDir>/Saves/<gamertag> (<XUID>)/<gameName> - <titleID>/<files>` with the profile package at `<gamertag> (<XUID>)/Profile/<XUID>`; filesystem-unsafe characters in gamertags are sanitised. Endpoints: `/saves/discover`, `/saves/list`, `/saves/download`, `/saves/delete`, `/saves/copy`, `/saves/backup-all`, `/saves/keyvault-status`.

## BadAvatar USB tool (BadUpdate exploit builder)

- **What:** Build a bootable Xbox 360 USB stick that triggers the **BadUpdate** payload — a hardware-free RGH-style exploit that lets a stock console run unsigned code. Combines a FAT32 format step with the BadStick payload installer in a single Toolbox flow. Optional Proto, FreestyleDash, and Aurora (XeUnshackle build) are included by default; **Overwrite existing** and **Format USB** are also on by default.
- **How:** Plug in a USB stick, open **Toolbox → BadAvatar USB**, pick the drive, choose your payload options, and click **Build**. Progress streams file-by-file. On Windows the formatter handles large drives without the 32 GB limit; on macOS and Linux the formatter is built-in.
- **How it works:** The Electron service `badAvatarUsbService.ts` orchestrates the workflow over `tools:badavatar-*` IPC channels handled in `badAvatarHandlers.ts`. Formatting uses platform-native tools — `infrastructure/fat32Format.ts` invokes Ridgecrop `fat32format.exe` on Windows (bundled into the installer via `scripts/download-fat32format.js` → `dist/tools/`), `newfs_msdos` / `diskutil` on macOS, and `mkfs.vfat` / `mkfs.fat` on Linux. Payload files are pulled from the [BadStick](https://github.com/LxcyDr0p/BadStick) release packages.

## Move Game to Drive

- **What:** Move a game from one Xbox drive to another directly from the Library page.
- **How:** Open a game's detail view in the Xbox Library, select a target drive from "Move to Drive" (the current drive is excluded), and click Move. Progress, transfer speed, and current file are shown in real time. The job persists across page navigation.
- **How it works:** The `xbox:move-game` IPC handler queues an FTP job through the Go backend. Uses FTP rename (`RNFR`/`RNTO`) when supported (fast, same-drive moves); falls back to download-reupload-delete for cross-drive moves. FTP timeout is 120 seconds for large transfers. Double-slash path bugs (from Aurora's leading-slash DB entries) are stripped. The local Aurora library cache auto-syncs after completion.

## Unified Job Queue

- **What:** Single view of all active work — game pipeline jobs and FTP Manager jobs merged together.
- **How:** Open **Job Queue** from the home page. Each job shows its source (Store vs FTP), state, progress bar, transfer speed, and current file detail. Remove completed or stuck jobs individually.
- **How it works:** The Queue page merges jobs from `/queue` (game pipeline) and `/ftp/jobs` (FTP Manager tracked jobs) into one unified list. Progress bars and percentage hide for completed/errored jobs.

## Auto Aurora sync

- **What:** After a game is downloaded and transferred to the Xbox, cover art and the local library cache update automatically — no manual refresh needed.
- **How:** Automatic; no user action required. After any successful game FTP transfer or drive move, the app fetches cover/background/banner/icon from XboxUnity and Xbox CDN, uploads them to `Aurora/User/Import/{TitleId}/`, and re-downloads content.db + settings.db.
- **How it works:** `autoSyncService.ts` listens for backend FTP completion events. `autoUploadAuroraAssets` fetches artwork from multiple CDN sources and uploads via FTP. `doAuroraLibrarySync` re-downloads Aurora databases to keep the Library page current.

## Overlay navigation

- **What:** Settings, Job Queue, Browse & Download, ISO to GOD, ISO to XEX, and FTP Manager open as overlay panels on top of the current page instead of navigating away.
- **How:** Click any of those buttons — the panel slides in over the current view. Close with the X button. No back-navigation required; the page underneath is preserved.
- **How it works:** React overlay components mount on top of the existing route, keeping Library or Home state intact while tools are used.

## Developer / diagnostics

- **What:** Quick HTML snapshot of cache, transfer folder, ready games, and jobs.
- **How:** From a browser on the same machine as the backend, open `http://<host-ip>:<port>/debug` while the server is running.
- **How it works:** The server renders live in-memory and filesystem state for troubleshooting.
