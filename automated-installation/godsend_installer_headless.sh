#!/usr/bin/env bash
# ==========================================
# GODSend Installer - Headless/Server Edition
# - Optimized for headless Linux servers
# - Includes systemd service generation
# - Non-interactive installation
# ==========================================

set -euo pipefail

# ==========================================
# Configuration
# ==========================================

FORCE_CLEAN=false
INSTALL_SYSTEMD=false
INSTALL_USER="${SUDO_USER:-$USER}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${SCRIPT_DIR}/godsend"
VERBOSE=false
REPO_URL="https://gitgud.io/Nesquin/godsend-homelab-edition.git"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --force-clean|-f)
            FORCE_CLEAN=true
            shift
            ;;
        --systemd|-s)
            INSTALL_SYSTEMD=true
            shift
            ;;
        --install-dir|-d)
            INSTALL_DIR="$2"
            shift 2
            ;;
        --user|-u)
            INSTALL_USER="$2"
            shift 2
            ;;
        --verbose|-v)
            VERBOSE=true
            shift
            ;;
        --help|-h)
            cat << EOF
GODSend Installer - Headless/Server Edition

Usage: $0 [OPTIONS]

Options:
  --force-clean, -f        Remove existing installation before installing
  --systemd, -s            Install systemd service file (requires root)
  --install-dir, -d DIR    Custom installation directory (default: ./godsend)
  --user, -u USER          User to run service as (default: current user)
  --verbose, -v            Verbose output
  --help, -h               Show this help message

Examples:
  # Basic installation
  ./godsend_installer_headless.sh

  # Install with systemd service
  sudo ./godsend_installer_headless.sh --systemd

  # Custom directory with service
  sudo ./godsend_installer_headless.sh --install-dir /opt/godsend --systemd --user godsend

  # Clean reinstall
  ./godsend_installer_headless.sh --force-clean

EOF
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# ==========================================
# Helper Functions
# ==========================================

log_info() { echo "[INFO] $1"; }
log_ok() { echo "[OK] $1"; }
log_warn() { echo "[WARN] $1"; }
log_error() { echo "[ERROR] $1" >&2; }
log_debug() { [[ "$VERBOSE" == "true" ]] && echo "[DEBUG] $1" || true; }

ensure_dir() {
    [[ ! -d "$1" ]] && mkdir -p "$1"
}

detect_platform() {
    local os arch
    
    case "$(uname -s)" in
        Linux*)     os="linux" ;;
        Darwin*)    os="macos" ;;
        *)          log_error "Unsupported OS: $(uname -s)"; exit 1 ;;
    esac
    
    case "$(uname -m)" in
        x86_64|amd64)   arch="x86_64" ;;
        arm64|aarch64)  arch="aarch64" ;;
        armv7l)         arch="armv7" ;;
        *)              arch="x86_64" ;;
    esac
    
    echo "${os}:${arch}"
}

download_file() {
    local url="$1"
    local outfile="$2"
    local retries="${3:-3}"
    local min_size="${4:-100}"
    
    ensure_dir "$(dirname "$outfile")"
    
    for ((i=1; i<=retries; i++)); do
        log_debug "Download attempt $i: $(basename "$outfile")"
        
        if command -v curl &>/dev/null; then
            if curl -fsSL --retry 2 -o "$outfile" "$url" 2>/dev/null; then
                local filesize
                filesize=$(stat -c%s "$outfile" 2>/dev/null || stat -f%z "$outfile" 2>/dev/null || echo 0)
                if [[ "$filesize" -ge "$min_size" ]]; then
                    log_debug "Downloaded: $filesize bytes"
                    return 0
                fi
            fi
        elif command -v wget &>/dev/null; then
            if wget -q -O "$outfile" "$url" 2>/dev/null; then
                local filesize
                filesize=$(stat -c%s "$outfile" 2>/dev/null || stat -f%z "$outfile" 2>/dev/null || echo 0)
                if [[ "$filesize" -ge "$min_size" ]]; then
                    log_debug "Downloaded: $filesize bytes"
                    return 0
                fi
            fi
        else
            log_error "Neither curl nor wget available"
            exit 1
        fi
        
        [[ $i -lt $retries ]] && sleep $((i * 2))
    done
    
    return 1
}

# ==========================================
# Main Installation
# ==========================================

log_info "GODSend Headless Installer starting..."

PLATFORM_INFO=$(detect_platform)
OS="${PLATFORM_INFO%%:*}"
ARCH="${PLATFORM_INFO##*:}"
log_info "Platform: $OS ($ARCH)"

# Check dependencies
for cmd in tar git; do
    if ! command -v $cmd &>/dev/null; then
        log_error "Required command not found: $cmd"
        exit 1
    fi
done

if ! command -v curl &>/dev/null && ! command -v wget &>/dev/null; then
    log_error "curl or wget required"
    exit 1
fi

# Handle force clean
if [[ "$FORCE_CLEAN" == "true" ]] && [[ -d "$INSTALL_DIR" ]]; then
    log_info "Removing existing installation..."
    rm -rf "$INSTALL_DIR"
fi

# Create directories FIRST
ensure_dir "$INSTALL_DIR"

READY_DIR="${INSTALL_DIR}/Ready"
TEMP_DIR="${INSTALL_DIR}/Temp"
XBOX_DIR="${INSTALL_DIR}/MOVE_THESE_FILES_TO_XBOX"
REPO_CLONE_DIR="${TEMP_DIR}/repo-clone"

ensure_dir "$READY_DIR"
ensure_dir "$TEMP_DIR"
ensure_dir "$XBOX_DIR"

log_ok "Directory structure created"

# Track results
declare -a INSTALLED=()
declare -a FAILED=()

# ----------------------------
# Clone Repository
# ----------------------------
log_info "Cloning GODSend repository..."

if [[ -d "$REPO_CLONE_DIR" ]]; then
    rm -rf "$REPO_CLONE_DIR"
fi

if git clone --depth 1 "$REPO_URL" "$REPO_CLONE_DIR" 2>/dev/null; then
    log_ok "Repository cloned"
    INSTALLED+=("Repository")
else
    log_error "Failed to clone repository"
    FAILED+=("Repository")
fi

# ----------------------------
# 1) 7-Zip
# ----------------------------
log_info "Installing 7-Zip..."
SEVEN_ZIP_EXE="${INSTALL_DIR}/7zz"

if [[ -f "$SEVEN_ZIP_EXE" ]]; then
    log_ok "7-Zip already installed"
    INSTALLED+=("7-Zip")
else
    case "$OS:$ARCH" in
        linux:x86_64)   SEVEN_ZIP_URL="https://7-zip.org/a/7z2301-linux-x64.tar.xz" ;;
        linux:aarch64)  SEVEN_ZIP_URL="https://7-zip.org/a/7z2301-linux-arm64.tar.xz" ;;
        linux:armv7)    SEVEN_ZIP_URL="https://7-zip.org/a/7z2301-linux-arm.tar.xz" ;;
        macos:*)        SEVEN_ZIP_URL="https://7-zip.org/a/7z2301-mac.tar.xz" ;;
        *)              SEVEN_ZIP_URL="" ;;
    esac
    
    if [[ -n "$SEVEN_ZIP_URL" ]]; then
        SEVEN_ZIP_ARCHIVE="${TEMP_DIR}/7z.tar.xz"
        if download_file "$SEVEN_ZIP_URL" "$SEVEN_ZIP_ARCHIVE" 3 10000; then
            tar -xf "$SEVEN_ZIP_ARCHIVE" -C "$INSTALL_DIR" 2>/dev/null
            if [[ -f "$SEVEN_ZIP_EXE" ]]; then
                chmod +x "$SEVEN_ZIP_EXE"
                log_ok "7-Zip installed"
                INSTALLED+=("7-Zip")
            else
                FAILED+=("7-Zip")
            fi
        else
            FAILED+=("7-Zip")
        fi
    elif command -v 7z &>/dev/null; then
        ln -sf "$(which 7z)" "$SEVEN_ZIP_EXE"
        log_ok "7-Zip (system) linked"
        INSTALLED+=("7-Zip")
    else
        log_warn "7-Zip not available for this platform"
        FAILED+=("7-Zip")
    fi
fi

# ----------------------------
# 2) iso2god-rs
# ----------------------------
log_info "Installing iso2god-rs..."
ISO2GOD_PATH="${INSTALL_DIR}/iso2god"

if [[ -f "$ISO2GOD_PATH" ]]; then
    log_ok "iso2god-rs already installed"
    INSTALLED+=("iso2god-rs")
else
    case "$OS:$ARCH" in
        linux:x86_64)   ISO2GOD_URL="https://github.com/iliazeus/iso2god-rs/releases/download/v1.8.1/iso2god-x86_64-linux" ;;
        linux:aarch64)  ISO2GOD_URL="https://github.com/iliazeus/iso2god-rs/releases/download/v1.8.1/iso2god-aarch64-linux" ;;
        macos:x86_64)   ISO2GOD_URL="https://github.com/iliazeus/iso2god-rs/releases/download/v1.8.1/iso2god-x86_64-macos" ;;
        macos:aarch64)  ISO2GOD_URL="https://github.com/iliazeus/iso2god-rs/releases/download/v1.8.1/iso2god-aarch64-macos" ;;
        *)              ISO2GOD_URL="" ;;
    esac
    
    if [[ -n "$ISO2GOD_URL" ]]; then
        if download_file "$ISO2GOD_URL" "$ISO2GOD_PATH" 3 10000; then
            chmod +x "$ISO2GOD_PATH"
            log_ok "iso2god-rs installed"
            INSTALLED+=("iso2god-rs")
        else
            FAILED+=("iso2god-rs")
        fi
    else
        log_warn "iso2god-rs not available for $OS:$ARCH"
        FAILED+=("iso2god-rs")
    fi
fi

# ----------------------------
# 3) GODSend backend (from cloned repo)
# ----------------------------
log_info "Installing GODSend backend..."
GODSEND_EXE="${INSTALL_DIR}/godsend"

if [[ -f "$GODSEND_EXE" ]]; then
    log_ok "GODSend backend already installed"
    INSTALLED+=("GODSend")
else
    case "$OS" in
        linux)  GODSEND_SRC="${REPO_CLONE_DIR}/source-control/godsend_linux" ;;
        macos)  GODSEND_SRC="${REPO_CLONE_DIR}/source-control/godsend_mac" ;;
        *)      GODSEND_SRC="" ;;
    esac
    
    if [[ -n "$GODSEND_SRC" ]] && [[ -f "$GODSEND_SRC" ]]; then
        cp "$GODSEND_SRC" "$GODSEND_EXE"
        chmod +x "$GODSEND_EXE"
        log_ok "GODSend backend installed"
        INSTALLED+=("GODSend")
    else
        log_warn "GODSend backend not found: $GODSEND_SRC"
        FAILED+=("GODSend")
    fi
fi

# ----------------------------
# 4) Xbox client files (from cloned repo)
# ----------------------------
log_info "Installing Xbox client files..."

CLIENT_SCRIPTS_DIR="${REPO_CLONE_DIR}/client-scripts"

if [[ -d "$CLIENT_SCRIPTS_DIR" ]]; then
    if cp -r "${CLIENT_SCRIPTS_DIR}/"* "$XBOX_DIR/" 2>/dev/null; then
        log_ok "Xbox client files installed"
        INSTALLED+=("Xbox Client Files")
    else
        log_warn "Failed to copy Xbox client files"
        FAILED+=("Xbox Client Files")
    fi
else
    log_warn "client-scripts directory not found: $CLIENT_SCRIPTS_DIR"
    FAILED+=("Xbox Client Files")
fi

# ----------------------------
# 5) Systemd service (optional)
# ----------------------------
if [[ "$INSTALL_SYSTEMD" == "true" ]]; then
    log_info "Installing systemd service..."
    
    if [[ $EUID -ne 0 ]]; then
        log_warn "Systemd installation requires root. Skipping."
    else
        SERVICE_FILE="/etc/systemd/system/godsend.service"
        
        cat > "$SERVICE_FILE" << EOF
[Unit]
Description=GODSend Xbox 360 Game Server
After=network.target

[Service]
Type=simple
User=${INSTALL_USER}
WorkingDirectory=${INSTALL_DIR}
ExecStart=${GODSEND_EXE}
Restart=on-failure
RestartSec=10

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=${INSTALL_DIR}
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
        
        systemctl daemon-reload
        log_ok "Systemd service installed: godsend.service"
        
        cat > "${INSTALL_DIR}/godsend-ctl" << 'CTLEOF'
#!/usr/bin/env bash
case "$1" in
    start)   sudo systemctl start godsend ;;
    stop)    sudo systemctl stop godsend ;;
    restart) sudo systemctl restart godsend ;;
    status)  systemctl status godsend ;;
    enable)  sudo systemctl enable godsend ;;
    disable) sudo systemctl disable godsend ;;
    logs)    journalctl -u godsend -f ;;
    *)
        echo "Usage: $0 {start|stop|restart|status|enable|disable|logs}"
        exit 1
        ;;
esac
CTLEOF
        chmod +x "${INSTALL_DIR}/godsend-ctl"
        INSTALLED+=("Systemd Service")
    fi
fi

# Cleanup - remove repo clone but keep Temp folder
log_info "Cleaning up..."
rm -rf "$REPO_CLONE_DIR" 2>/dev/null || true
rm -f "${TEMP_DIR}"/*.tar.xz 2>/dev/null || true
log_ok "Cleanup complete. Temp folder preserved for application use."

# ==========================================
# Summary
# ==========================================

echo ""
echo "========================================"
echo "  Installation Complete"
echo "========================================"
echo ""
echo "Directory: $INSTALL_DIR"
echo ""
echo "Installed: ${INSTALLED[*]:-none}"
[[ ${#FAILED[@]} -gt 0 ]] && echo "Failed: ${FAILED[*]}"
echo ""

if [[ -f "$GODSEND_EXE" ]]; then
    echo "Start server:"
    echo "  cd $INSTALL_DIR && ./godsend"
    echo ""
    if [[ "$INSTALL_SYSTEMD" == "true" ]] && [[ -f "/etc/systemd/system/godsend.service" ]]; then
        echo "Or use systemd:"
        echo "  sudo systemctl start godsend"
        echo "  sudo systemctl enable godsend  # auto-start on boot"
        echo "  ./godsend-ctl logs             # view logs"
        echo ""
    fi
fi

echo "Xbox client files: $XBOX_DIR"
echo ""

exit 0
