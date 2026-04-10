# Running Without the Desktop App

The Go backend works as a standalone headless server — no Electron, no GUI, no display required. This is useful for always-on home servers, NAS boxes, Raspberry Pi, Docker containers, or any Linux/macOS/Windows machine you want to run unattended.

## 1. Get the server binary

**Option A — Download a prebuilt binary** from the [v2.7.0 release](https://gitgud.io/ghosty99/godsend-360/-/releases/v2.7.0) ([all releases](https://gitgud.io/ghosty99/godsend-360/-/releases)):

| Platform | Binary |
|----------|--------|
| **Windows (x64)** | [`godsend.exe`](https://gitgud.io/-/project/46780/uploads/7d70323bd36df2671077bca6488e8df2/godsend.exe) |
| **Linux (x64 / amd64)** | [`godsend-linux-x64`](https://gitgud.io/-/project/46780/uploads/6885b31cef1cf2702703d789c28ec00f/godsend-linux-x64) |
| **Linux (arm64)** | [`godsend-linux-arm64`](https://gitgud.io/-/project/46780/uploads/dcbcd980cd7920df4b04198df9e25144/godsend-linux-arm64) |
| **macOS (Apple Silicon)** | [`godsend-darwin-arm64`](https://gitgud.io/-/project/46780/uploads/16f8bb5a51f99ff0ec18c9886dfcaebf/godsend-darwin-arm64) |
| **macOS (Intel)** | [`godsend-darwin-amd64`](https://gitgud.io/-/project/46780/uploads/ab53d6906cb373054c3d0e4be5bc3e22/godsend-darwin-amd64) |

Other v2.7.0 artifacts (desktop AppImages/DMGs, `godsend-mac`): see the [release asset list](https://gitgud.io/ghosty99/godsend-360/-/releases/v2.7.0).

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
| `GODSEND_IA_CONCURRENCY` | No | Parallel download workers, 1–7 (default `5`). |
| `GODSEND_FTP_USER` | For FTP | FTP username for the Xbox (default `xboxftp`). |
| `GODSEND_FTP_PASS` | For FTP | FTP password for the Xbox (default `xboxftp`). |
| `GODSEND_ROM_PATH` | No | Drive-relative ROM install path (default `Emulators\RetroArch\roms`). |

## 3. Run it

**Linux / macOS:**

```bash
export GODSEND_HOME="/opt/godsend"
export GODSEND_PORT="8080"
export GODSEND_IA_CONCURRENCY="5"
./godsend-linux-x64
```

**Windows (PowerShell):**

```powershell
$env:GODSEND_HOME="C:\godsend"
$env:GODSEND_PORT="8080"
$env:GODSEND_IA_CONCURRENCY="5"
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
