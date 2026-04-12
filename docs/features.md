# Features

Each item is a **high-level capability**, **how you use it**, and **how it works** under the hood.

## Desktop app (Electron) + backend

- **What:** Run the Go HTTP server with a small UI, live log output, and settings — **Windows** (NSIS installer), **macOS** (DMG), or **Linux** (AppImage). One-click upload of Aurora scripts to the Xbox via FTP with live per-file progress.
- **How:** Launch GODsend from the **Start menu** (Windows), **Applications** (macOS), or your **app launcher** (Linux; tray icon support depends on the desktop environment). Use the tray icon to open the window. Restart the backend from the home screen; optional **Launch at login** in Settings. Set **Backend server port** first (if needed), then under **Xbox connection** enter your Xbox IP and click **FTP Aurora Scripts to Xbox** — your computer’s LAN IP + selected backend port are patched into `state.lua` automatically and upload progress is shown file-by-file.
- **How it works:** Electron spawns the Go backend (`godsend-backend` / `godsend-backend.exe`) with a writable runtime (`Transfer`, `Ready`, `Temp`, `cache`) and injects `GODSEND_*` environment variables from your settings. The FTP upload streams `godsend-ftp-progress` IPC events to the renderer so the button reflects real progress rather than a static label.

## Minerva Archive (BitTorrent — no account needed)

- **What:** Xbox 360, OG Xbox, XBLA, DLC, XBLIG, and Game Archive libraries sourced from [minerva-archive.org](https://minerva-archive.org) — no account or login required. Works out of the box.
- **How:** When browsing any game library, select **Minerva Archive** as the source. Game lists are bundled in the installer so browsing is instant.
- **How it works:** The backend fetches the Minerva collection torrent, finds the requested file's index, and uses `aria2c` to download only that file via BitTorrent (`--select-file`). **Windows** and **Linux** desktop builds ship a bundled `aria2c` next to the backend; the **Windows NSIS** installer can add OS firewall rules for `aria2c` so Windows does not prompt on first torrent use. **macOS** does not bundle `aria2c` — the backend prepends Homebrew to `PATH` and, if needed, tries a non-interactive Homebrew install plus `brew install aria2` at startup; if `sudo` is unavailable (typical when launched from the GUI), it sets **`SUDO_ASKPASS`** so the installer runs as your user and macOS shows the password dialog when Homebrew needs `sudo` (the installer cannot run as root). Progress is reported to the Aurora queue display every 3 seconds.

## Internet Archive account & parallel downloads (optional fallback)

- **What:** Authenticated downloads from archive.org collections — useful for titles not available on Minerva.
- **How:** Settings → **Internet Archive account** → **Log in**. Select **Internet Archive** as the source when browsing. Large files use automatic chunked parallel HTTP downloads (no connection slider).
- **How it works:** The app stores session cookies locally (not your password), passes them to the backend, which fetches items with multiple range-request workers for faster ISO/archive retrieval.

## Local Transfer folder (your own ISOs)

- **What:** Install disc games from `.iso` files you already have, without re-downloading from IA.
- **How:** Settings → set **Local Transfer folder** (or use the default runtime `Transfer` folder). Drop ISOs there. On the Xbox, open **Local Library** or trigger a title that matches a filename in that folder.
- **How it works:** For Xbox 360 / original Xbox / `local` browse, the backend prefers a matching ISO under `Transfer` over Internet Archive and runs the same conversion pipeline locally.

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

## Developer / diagnostics

- **What:** Quick HTML snapshot of cache, transfer folder, ready games, and jobs.
- **How:** From a browser on the same machine as the backend, open `http://<host-ip>:<port>/debug` while the server is running.
- **How it works:** The server renders live in-memory and filesystem state for troubleshooting.
