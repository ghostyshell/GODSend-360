# Running Without the Desktop App

The Go backend works as a standalone headless server — no Electron, no GUI, no display required. This is useful for always-on home servers, NAS boxes, Raspberry Pi, Docker containers, or any Linux/macOS/Windows machine you want to run unattended.

## 1. Get the server binary

**Option A — Download a prebuilt binary** from the [v2.9.2 release](https://gitgud.io/ghosty99/godsend-360/-/releases/v2.9.2) ([all releases](https://gitgud.io/ghosty99/godsend-360/-/releases)):

| Platform | Binary |
|----------|--------|
| **Windows (x64)** | [`godsend.exe`](https://gitgud.io/-/project/46780/uploads/339fbf3fa02cf3ae9e0a013fb5a84ccc/godsend.exe) |
| **Linux (x64 / amd64)** | [`godsend-linux-x64`](https://gitgud.io/-/project/46780/uploads/05a7c699c101852f6482af9c4581f0b0/godsend-linux-x64) |
| **Linux (arm64)** | [`godsend-linux-arm64`](https://gitgud.io/-/project/46780/uploads/ef695b9aeb116f7bab5a8b9f1c2d790d/godsend-linux-arm64) |
| **macOS (Apple Silicon)** | [`godsend-darwin-arm64`](https://gitgud.io/-/project/46780/uploads/41003612f40ead5e8e3588275770f6a4/godsend-darwin-arm64) |
| **macOS (Intel)** | [`godsend-darwin-amd64`](https://gitgud.io/-/project/46780/uploads/32687bb8e6869fd0759ff6d8278bc82e/godsend-darwin-amd64) |

For the **full desktop app** (tray UI + bundled backend), use the **NSIS** (Windows), **DMG** (macOS), or **AppImage** (Linux) assets on the same [release page](https://gitgud.io/ghosty99/godsend-360/-/releases/v2.9.2) instead of the bare binaries above.

On Linux / macOS, make the binary executable after downloading: `chmod +x godsend-*`

**Option B — Build from source** (requires **Go 1.21+**):

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

The backend reads all its settings from environment variables — no config file needed. Set these before launching:

| Variable | Required | Description |
|----------|----------|-------------|
| `GODSEND_HOME` | Recommended | Root directory for `Transfer/`, `Ready/`, `Temp/`, `cache/`. Defaults to the binary's directory. |
| `GODSEND_TRANSFER` | No | Override the Transfer folder independently (defaults to `$GODSEND_HOME/Transfer`). |
| `GODSEND_PORT` | No | HTTP listen port (default `8080`). |
| `GODSEND_IA_COOKIE` | For IA | `logged-in-user=…; logged-in-sig=…` session cookie from archive.org. |
| `GODSEND_IA_AUTHORIZATION` | For IA | Bearer token (alternative to cookie). |
| `GODSEND_IA_MAX_CONNECTIONS` | No | Max concurrent HTTP range requests per large download (default `16`, max `32`). Optional. Legacy: `GODSEND_IA_CONCURRENCY` is accepted as an alias. |
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

## 5. Point the Xbox at the server

Edit `aurora-scripts/state.lua` on the Xbox (or before copying the scripts over):

```lua
BRAIN_IP = "192.168.1.50"   -- IP of your headless server
PORT     = "8080"            -- must match GODSEND_PORT
```

Copy the `aurora-scripts/` folder to the Xbox at `Hdd1:\Aurora\User\Scripts\Utility\GODSend\` via FTP (this is the default Aurora path — yours may differ depending on where Aurora is installed on your Xbox, e.g. `Usb0:\Apps\Aurora\...` for USB setups), then launch GODsend from Aurora → Scripts.

## 6. Verify

Open `http://<server-ip>:8080/debug` in a browser to confirm the backend is running and see cache status, transfer folder contents, and active jobs.
