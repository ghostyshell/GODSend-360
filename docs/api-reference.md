# Backend HTTP API

The backend listens on port `8080` by default (configurable via Electron `Backend server port` or `GODSEND_PORT`). Endpoints used by the Lua script:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/browse?platform=<p>` | Game list (pipe-separated); `platform` includes `xbox360`, `xbox`, `xbla`, `digital`, `dlc`, `xblig`, `games`, `local`, `rom_<sysid>`. Optional `source=minerva\|ia` limits the list to one catalog when both are merged by default. |
| GET | `/status?game=<name>` | Poll job state: `Idle`, `Processing`, `Ready`, `Error`, `Missing` |
| GET | `/queue` | List all active and completed jobs |
| GET | `/trigger?game=<name>&platform=<p>` | Start processing a game (Aurora uses GET) |
| GET | `/register?game=<name>&ip=<xbox-ip>&drive=...&platform=...` | Register the console IP and drive for FTP transfer |
| GET | `/files/<name>/...` | Serve finished GOD/archive files to the Xbox over HTTP |
| GET/POST | `/queue/remove?game=<name>` | Remove one job (`game` omitted clears all); Aurora uses GET |
| GET | `/cache-status` | Per-platform cache build state and counts |
| GET | `/cache-refresh?platform=<p>` | Trigger cache rebuild (`all`, an IA platform, or `rom_<sysid>`); Electron uses GET |
| GET | `/data/status` | Returns `active_jobs`, `pending_ftp_jobs`, `local_data_mb` — used by Electron clear-data UI |
| GET | `/data/clear` | Cancels all jobs and pending FTP transfers, wipes `Ready/` and `Temp/` |
| GET | `/config` | Returns server-side config readable by Lua (currently `default_drive`) |

## Runtime folders

The backend creates these under its working directory (or `GODSEND_HOME` if set):

| Folder | Purpose |
|--------|---------|
| `Transfer/` | Drop ISOs here for local-library installs (used instead of downloading from IA) |
| `Ready/` | Staging area for GOD/archive files pending FTP transfer; also used by `pending_ftp/` job tracking |
| `Temp/` | Working directory for in-progress conversions |
| `cache/` | Cached Internet Archive game metadata (avoids re-fetching on each launch) |

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GODSEND_HOME` | binary directory | Root path for Transfer/Ready/Temp/cache |
| `GODSEND_TRANSFER` | `$GODSEND_HOME/Transfer` | Override Transfer folder path independently |
| `GODSEND_IA_COOKIE` | — | `logged-in-user=…; logged-in-sig=…` session cookie for IA auth |
| `GODSEND_IA_AUTHORIZATION` | — | Bearer token as an alternative to cookie auth |
| `GODSEND_IA_MAX_CONNECTIONS` | `16` | Max concurrent HTTP range requests per large IA / EdgeEmu download (1–32). Optional. |
| `GODSEND_IA_CONCURRENCY` | — | Legacy alias for `GODSEND_IA_MAX_CONNECTIONS` (same clamp 1–32). |
| `GODSEND_PORT` | `8080` | Backend listen port |
| `GODSEND_FTP_USER` | — | FTP username for the Xbox (default `xboxftp`) |
| `GODSEND_FTP_PASS` | — | FTP password for the Xbox (default `xboxftp`) |
| `GODSEND_ROM_PATH` | `Emulators\RetroArch\roms` | Drive-relative ROM install path |
| `GODSEND_DEFAULT_DRIVE` | — | Default Xbox destination drive (e.g. `Hdd1:`); when set, Aurora skips the drive picker |
| `GODSEND_ARIA2_LISTEN_PORT` | — | Override BitTorrent listen port used by aria2c (Minerva downloads) |
| `GODSEND_ARIA2_DHT_PORT` | — | Override DHT listen port used by aria2c |
