## Legacy Windows installers and folder layout

Older Windows installers and guides (for example, “GODSend Homelab Edition – Windows Installation Guide”) describe a layout like:

```text
godsend/
  godsend.exe
  iso2god.exe
  7za.exe, 7za.dll, 7zxa.dll
  MOVE_THESE_FILES_TO_XBOX/
    GODSend.ini
    main.lua
    MenuSystem.lua
    Icon/
  Ready/
  Temp/
```

### How this maps to the current repository

In this repository, the same pieces are organised as:

- Backend binary:
  - `dist/godsend.exe` — built from `src/server/` via the root `package.json` or a direct `go build`.
- External tools:
  - `tools/iso2god.exe`
  - `tools/7za.exe`, `tools/7za.dll`, `tools/7zxa.dll`
  - These are not committed; place the downloaded binaries in `tools/` before building.
- Xbox-side scripts (what legacy docs called `MOVE_THESE_FILES_TO_XBOX/`):
  - `aurora-scripts/`:
    - `GODSend.ini` — Xbox-side configuration (PC IP, etc.).
    - `main.lua` — entry point.
    - `menu_system.lua` — menu helper used by `main.lua`/`menu.lua`.
    - `state.lua`, `http_client.lua`, `services.lua`, `menu.lua` — modularised script logic.
    - `Icon/` — same icon assets expected by Aurora.
- Runtime data:
  - `Ready/`, `Temp/`, `Transfer/`, `cache/` — now created at runtime under `GODSEND_HOME` (or the Electron runtime directory), but conceptually equivalent to the legacy `Ready/` and `Temp/` folders.

### Following the spirit of the old guides

You can still follow the high-level steps from the legacy PDF:

1. **Install or build the backend**
   - Use the modern installer produced by `npm run build`, or build `dist/godsend.exe` directly with Go.
2. **Prepare the Xbox script bundle**
   - Copy all files from `aurora-scripts/` (including `Icon/`) to your Xbox:
     - Example path: `Hdd1:\Aurora\User\Scripts\Utility\godsend\`
3. **Point the script at your PC**
   - Edit `GODSend.ini` and set `ip=` to your PC’s IPv4 address (where `godsend.exe` runs).
4. **Enable FTP and content scanning in Aurora**
   - Enable the FTP server in Aurora settings so the backend can push content (FTP mode).
   - Configure content paths and run a content scan so GOD/XEX/DLC installs appear.

### PDF guide

Place the legacy PDF (for example, `godsend windows install guide.pdf`) in:

- `docs/godsend-windows-install-guide.pdf`

and open it directly when you need the full, illustrated Windows installer walkthrough and FAQ.

