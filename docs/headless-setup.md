# Running Without the Desktop App

The Go backend works as a standalone headless server - no Electron, no GUI, no display required. This is useful for always-on home servers, NAS boxes, Raspberry Pi, Docker containers, or any Linux/macOS/Windows machine you want to run unattended.

## 1. Get the server binary

**Option A - Download a prebuilt binary** *(GoFile is primary, file.kiwi is a backup mirror - pick whichever loads)*:

| Platform | GoFile | file.kiwi backup |
|----------|--------|--------|
| **Windows (x64)** | [`godsend.exe`](https://gofile.io/d/5feTCpn7) | [`godsend.exe`](https://file.kiwi/5936b4a0#s0KvMbeOhUC_-p45qOqZVA) |
| **Linux (x64 / amd64)** | [`godsend-linux-x64`](https://gofile.io/d/Xq0pnr2n) | [`godsend-linux-x64`](https://file.kiwi/36049e15#z58XadO-NhYxqp1FK-_GgQ) |
| **Linux (arm64)** | [`godsend-linux-arm64`](https://gofile.io/d/BkRG4JBJ) | [`godsend-linux-arm64`](https://file.kiwi/b93b487f#mtfpHPAa9QJuomPJr1tmcw) |
| **macOS (Apple Silicon)** | [`godsend-darwin-arm64`](https://gofile.io/d/fkOkufAP) | [`godsend-darwin-arm64`](https://file.kiwi/af018656#4D48fX7aHAno8cnfS3hwlA) |
| **macOS (Intel)** | [`godsend-darwin-amd64`](https://gofile.io/d/EgAXsjo8) | [`godsend-darwin-amd64`](https://file.kiwi/c39b8323#WXSYAryG9noKCa0_5KkcuA) |

For the **full desktop app** (tray UI + bundled backend), see the download table in the main [README](../readme.md#quick-installation).

On Linux / macOS, make the binary executable after downloading: `chmod +x godsend-*`

**Option B - Build from source** (requires **Go 1.21+**):

```bash
# Windows
go build -C src/server -o ../../dist/godsend.exe .

# Linux amd64
GOOS=linux GOARCH=amd64 go build -C src/server -o ../../dist/godsend-linux-x64 .

# Linux arm64 (Raspberry Pi 4/5, Oracle ARM, etc.)
GOOS=linux GOARCH=arm64 go build -C src/server -o ../../dist/godsend-linux-arm64 .

# macOS Apple Silicon
GOOS=darwin GOARCH=arm64 go build -C src/server -o ../../dist/godsend-darwin-arm64 .

# macOS Intel
GOOS=darwin GOARCH=amd64 go build -C src/server -o ../../dist/godsend-darwin-amd64 .
```

Or use the npm helper: `npm run build:server:all` (builds all platforms).

## 2. Configure via environment variables

The backend reads all its settings from environment variables - no config file needed. Set these before launching:

| Variable | Required | Description |
|----------|----------|-------------|
| `GODSEND_HOME` | Recommended | Root directory for `Transfer/`, `Ready/`, `Temp/`, `cache/`. Defaults to the binary's directory. |
| `GODSEND_TORRENT_TEMP` | No | aria2c Minerva torrent staging (default `$GODSEND_HOME/Temp/torrent-dl`). |
| `GODSEND_TRANSFER` | No | Override the Transfer folder independently (defaults to `$GODSEND_HOME/Transfer`). |
| `GODSEND_PORT` | No | HTTP listen port (default `8080`). |
| `GODSEND_IA_COOKIE` | For IA | `logged-in-user=…; logged-in-sig=…` session cookie from archive.org. |
| `GODSEND_IA_AUTHORIZATION` | For IA | Bearer token (alternative to cookie). |
| `GODSEND_IA_MAX_CONNECTIONS` | No | Max concurrent HTTP range requests per large download (default `16`, max `32`). Optional. Legacy: `GODSEND_IA_CONCURRENCY` is accepted as an alias. |
| `GODSEND_DEBRID_PROVIDER` | For Debrid | Active debrid provider: `none` (default), `realdebrid`, or `torbox`. When set, torrents (both providers) and Internet Archive downloads (TorBox only) first cache on the provider for up to 60s and pull the direct HTTP link, falling back to the native source on timeout/error. |
| `GODSEND_REALDEBRID_KEY` | For Debrid | Real-Debrid private token. Required when provider is `realdebrid`. |
| `GODSEND_TORBOX_KEY` | For Debrid | TorBox API key. Required when provider is `torbox`. |
| `GODSEND_FTP_USER` | For FTP | FTP username for the Xbox (default `xboxftp`). |
| `GODSEND_FTP_PASS` | For FTP | FTP password for the Xbox (default `xboxftp`). |
| `GODSEND_ROM_PATH` | No | Drive-relative ROM install path (default `Emulators\RetroArch\roms`). |

## 3. Run it

**Linux / macOS:**

```bash
export GODSEND_HOME="/opt/godsend"
export GODSEND_PORT="8080"
./godsend-linux-x64
```

**Windows (PowerShell):**

```powershell
$env:GODSEND_HOME="C:\godsend"
$env:GODSEND_PORT="8080"
.\godsend.exe
```

The server starts immediately and logs to stdout. It creates `Transfer/`, `Ready/`, `Temp/`, and `cache/` under `GODSEND_HOME` on first run.

## 4. Run as a system service (optional)

To keep the backend running after logout or reboots:

### systemd (Linux)

```ini
# /etc/systemd/system/godsend.service
[Unit]
Description=GODsend 360 backend
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=godsend
ExecStart=/opt/godsend/godsend-linux-x64
Environment=GODSEND_HOME=/opt/godsend
Environment=GODSEND_PORT=8080
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now godsend
```

Common systemctl commands:

```bash
# Check status and recent logs
sudo systemctl status godsend

# View live logs
sudo journalctl -u godsend -f

# Stop the service
sudo systemctl stop godsend

# Restart the service
sudo systemctl restart godsend

# Disable auto-start and stop the service
sudo systemctl disable --now godsend

# Remove the service entirely
sudo systemctl disable --now godsend
sudo rm /etc/systemd/system/godsend.service
sudo systemctl daemon-reload
```

### launchd (macOS)

```xml
<!-- ~/Library/LaunchAgents/com.godsend.server.plist -->
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.godsend.server</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/godsend/godsend-darwin-arm64</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>GODSEND_HOME</key>
    <string>/opt/godsend</string>
    <key>GODSEND_PORT</key>
    <string>8080</string>
  </dict>
  <key>KeepAlive</key>
  <true/>
  <key>RunAtLoad</key>
  <true/>
</dict>
</plist>
```

```bash
launchctl load ~/Library/LaunchAgents/com.godsend.server.plist
```

Common launchctl commands:

```bash
# Stop the service
launchctl unload ~/Library/LaunchAgents/com.godsend.server.plist

# Restart (unload + load)
launchctl unload ~/Library/LaunchAgents/com.godsend.server.plist
launchctl load ~/Library/LaunchAgents/com.godsend.server.plist

# Remove the service entirely
launchctl unload ~/Library/LaunchAgents/com.godsend.server.plist
rm ~/Library/LaunchAgents/com.godsend.server.plist
```

### Docker

Good fit for an always-on NAS. The repo root has a `Dockerfile` (Alpine, backend binary + `aria2c` - required for Minerva/BitTorrent downloads) and a `docker-compose.yml` you can copy and adjust.

The image runs as uid 1000, not root, so the bind-mounted data folder needs to be owned by that uid up front - otherwise the backend can't create `Transfer/`, `Ready/`, `Temp/`, `cache/` under it and exits on startup:

```bash
git clone https://github.com/ghostyshell/GODSend-360.git
cd GODSend-360
mkdir -p data && sudo chown -R 1000:1000 data
docker compose up -d --build
```

Or with plain `docker run`:

```bash
mkdir -p /volume1/godsend && sudo chown -R 1000:1000 /volume1/godsend
docker build -t godsend .
docker run -d --name godsend --restart unless-stopped \
  -p 8080:8080 \
  -v /volume1/godsend:/data \
  -e GODSEND_PORT=8080 \
  godsend
```

Notes:

- The container binds to `0.0.0.0:8080` internally, so only the `-p`/`ports:` mapping controls what's reachable - point `aurora-scripts/state.lua`'s `BRAIN_IP` at the **host's** LAN IP, and its `PORT` at the **host side** of the mapping (the first `8080` in `8080:8080`), not necessarily `GODSEND_PORT` itself if you remap it, e.g. `-p 9000:8080`.
- The published port isn't behind your NAS's own firewall app (Docker inserts its own iptables/nftables rules ahead of it) and the backend has no auth - bind it to a specific LAN interface if that matters to you, e.g. `-p 192.168.1.50:8080:8080` instead of `-p 8080:8080`.
- Bridge networking (the default) is enough for the FTP transfer to the Xbox - the container reaches it outbound through normal Docker NAT, no `network_mode: host` needed. Minerva/BitTorrent downloads work the same way but stay outbound-only (no inbound peer connections) unless you also publish `GODSEND_ARIA2_LISTEN_PORT`/`GODSEND_ARIA2_DHT_PORT`, which mostly affects download speed, not correctness.
- Set env vars from the [configuration table above](#2-configure-via-environment-variables) via `-e` / `environment:` instead of a config file. Prefer a gitignored `.env` file (`env_file: .env` in `docker-compose.yml`) over inline values for `GODSEND_REALDEBRID_KEY` / `GODSEND_TORBOX_KEY` / `GODSEND_IA_COOKIE`.
- Point `-v` straight at an existing ISO share (e.g. `-v /volume1/isos:/data/Transfer`) if you want GODsend to pick up files you already have, per [Local Transfer folder](features.md#local-transfer-folder-your-own-isos). That path needs the same uid 1000 ownership as `/data` above.
- No published image yet - `docker compose up -d --build` builds locally from source.

## 5. Point the Xbox at the server

Edit `aurora-scripts/state.lua` on the Xbox (or before copying the scripts over):

```lua
BRAIN_IP = "192.168.1.50"   -- IP of your headless server
PORT     = "8080"            -- must match GODSEND_PORT
```

Copy the `aurora-scripts/` folder to the Xbox at `Hdd1:\Aurora\User\Scripts\Utility\GODSend\` via FTP (this is the default Aurora path - yours may differ depending on where Aurora is installed on your Xbox, e.g. `Usb0:\Apps\Aurora\...` for USB setups), then launch GODsend from Aurora → Scripts.

## 6. Verify

Open `http://<server-ip>:8080/debug` in a browser to confirm the backend is running and see cache status, transfer folder contents, and active jobs.
