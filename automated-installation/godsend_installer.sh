#!/usr/bin/env bash
# ==========================================
# GODSend Installer (Linux/macOS)
# - Creates a "godsend" folder next to this script
# - Clones repo and downloads dependencies
# ==========================================

set -euo pipefail

# ==========================================
# Configuration
# ==========================================

FORCE_CLEAN=false
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${SCRIPT_DIR}/godsend"
REPO_URL="https://gitgud.io/Nesquin/godsend-homelab-edition.git"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --force-clean|-f)
            FORCE_CLEAN=true
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --force-clean, -f    Remove existing installation before installing"
            echo "  --help, -h           Show this help message"
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

# Colors (with fallback for non-interactive terminals)
if [[ -t 1 ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    CYAN='\033[0;36m'
    GRAY='\033[0;90m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    CYAN=''
    GRAY=''
    NC=''
fi

write_info() { echo -e "${CYAN}[*]${NC} $1"; }
write_ok() { echo -e "${GREEN}[+]${NC} $1"; }
write_warn() { echo -e "${YELLOW}[!]${NC} $1"; }
write_fail() { echo -e "${RED}[x]${NC} $1"; }

ensure_dir() {
    local path="$1"
    if [[ ! -d "$path" ]]; then
        mkdir -p "$path"
    fi
}

# Detect OS and architecture
detect_platform() {
    local os arch
    
    case "$(uname -s)" in
        Linux*)     os="linux" ;;
        Darwin*)    os="macos" ;;
        CYGWIN*|MINGW*|MSYS*) 
            write_fail "This script is for Linux/macOS. Use the PowerShell script for Windows."
            exit 1
            ;;
        *)          
            write_fail "Unsupported operating system: $(uname -s)"
            exit 1
            ;;
    esac
    
    case "$(uname -m)" in
        x86_64|amd64)   arch="x86_64" ;;
        arm64|aarch64)  arch="aarch64" ;;
        armv7l)         arch="armv7" ;;
        *)              
            write_warn "Unusual architecture: $(uname -m), assuming x86_64"
            arch="x86_64"
            ;;
    esac
    
    echo "${os}:${arch}"
}

# Download with retries
download_file() {
    local url="$1"
    local outfile="$2"
    local retries="${3:-3}"
    local min_size="${4:-100}"
    
    ensure_dir "$(dirname "$outfile")"
    
    local filename
    filename="$(basename "$outfile")"
    
    for ((i=1; i<=retries; i++)); do
        write_info "Downloading: $filename"
        echo -e "${GRAY}    From: $url${NC}"
        
        if command -v curl &>/dev/null; then
            if curl -fsSL --retry 2 -o "$outfile" "$url"; then
                if [[ -f "$outfile" ]]; then
                    local filesize
                    filesize=$(stat -f%z "$outfile" 2>/dev/null || stat -c%s "$outfile" 2>/dev/null || echo 0)
                    if [[ "$filesize" -ge "$min_size" ]]; then
                        echo -e "${GRAY}    Downloaded: $((filesize / 1024)) KB${NC}"
                        return 0
                    fi
                fi
            fi
        elif command -v wget &>/dev/null; then
            if wget -q -O "$outfile" "$url"; then
                if [[ -f "$outfile" ]]; then
                    local filesize
                    filesize=$(stat -f%z "$outfile" 2>/dev/null || stat -c%s "$outfile" 2>/dev/null || echo 0)
                    if [[ "$filesize" -ge "$min_size" ]]; then
                        echo -e "${GRAY}    Downloaded: $((filesize / 1024)) KB${NC}"
                        return 0
                    fi
                fi
            fi
        else
            write_fail "Neither curl nor wget is available."
            exit 1
        fi
        
        if [[ $i -lt $retries ]]; then
            write_warn "Download failed (attempt $i/$retries). Retrying..."
            sleep $((i * 2))
        fi
    done
    
    write_fail "Download failed after $retries attempts: $url"
    return 1
}

# Check for required tools
check_dependencies() {
    local missing=()
    
    if ! command -v git &>/dev/null; then
        missing+=("git")
    fi
    
    if ! command -v curl &>/dev/null && ! command -v wget &>/dev/null; then
        missing+=("curl or wget")
    fi
    
    if ! command -v tar &>/dev/null; then
        missing+=("tar")
    fi
    
    if [[ ${#missing[@]} -gt 0 ]]; then
        write_fail "Missing required dependencies: ${missing[*]}"
        echo ""
        echo "Please install them using your package manager:"
        echo "  Ubuntu/Debian: sudo apt install git curl tar"
        echo "  Fedora/RHEL:   sudo dnf install git curl tar"
        echo "  macOS:         brew install git curl"
        echo "  Arch:          sudo pacman -S git curl tar"
        exit 1
    fi
}

# ==========================================
# Installation Setup
# ==========================================

echo ""
echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}  GODSend Installation Script${NC}"
echo -e "${CYAN}  Platform: $(uname -s) $(uname -m)${NC}"
echo -e "${CYAN}========================================${NC}"
echo ""

check_dependencies

PLATFORM_INFO=$(detect_platform)
OS="${PLATFORM_INFO%%:*}"
ARCH="${PLATFORM_INFO##*:}"

write_info "Detected: $OS ($ARCH)"
write_info "Installation directory: $INSTALL_DIR"

# Handle ForceClean
if [[ "$FORCE_CLEAN" == "true" ]] && [[ -d "$INSTALL_DIR" ]]; then
    write_warn "ForceClean enabled: removing existing installation..."
    rm -rf "$INSTALL_DIR"
    write_ok "Previous installation removed."
fi

# Create directory structure FIRST
ensure_dir "$INSTALL_DIR"

READY_DIR="${INSTALL_DIR}/Ready"
TEMP_DIR="${INSTALL_DIR}/Temp"
XBOX_DIR="${INSTALL_DIR}/MOVE_THESE_FILES_TO_XBOX"
REPO_CLONE_DIR="${TEMP_DIR}/repo-clone"

ensure_dir "$READY_DIR"
ensure_dir "$TEMP_DIR"
ensure_dir "$XBOX_DIR"

write_ok "Directory structure created."
echo ""

# Track installation errors
declare -a INSTALL_ERRORS=()

# ==========================================
# Clone Repository
# ==========================================

echo -e "${YELLOW}[0/4] Cloning GODSend Repository${NC}"
echo -e "${GRAY}----------------------------------------${NC}"

write_info "Cloning repository from $REPO_URL..."

if [[ -d "$REPO_CLONE_DIR" ]]; then
    rm -rf "$REPO_CLONE_DIR"
fi

if git clone --depth 1 "$REPO_URL" "$REPO_CLONE_DIR" 2>/dev/null; then
    write_ok "Repository cloned successfully."
else
    write_fail "Failed to clone repository"
    INSTALL_ERRORS+=("Repository Clone")
fi

echo ""

# ==========================================
# Component Installation
# ==========================================

# ----------------------------
# 1) 7-Zip
# ----------------------------
echo -e "${YELLOW}[1/4] 7-Zip Installation${NC}"
echo -e "${GRAY}----------------------------------------${NC}"

SEVEN_ZIP_EXE="${INSTALL_DIR}/7zz"

if [[ -f "$SEVEN_ZIP_EXE" ]]; then
    write_ok "7-Zip already installed (skipping)."
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
            write_info "Extracting 7-Zip..."
            tar -xf "$SEVEN_ZIP_ARCHIVE" -C "$INSTALL_DIR"
            
            if [[ -f "$SEVEN_ZIP_EXE" ]]; then
                chmod +x "$SEVEN_ZIP_EXE"
                write_ok "7-Zip installed successfully."
            else
                write_fail "7zz not found after extraction"
                INSTALL_ERRORS+=("7-Zip")
            fi
        else
            INSTALL_ERRORS+=("7-Zip")
        fi
    elif command -v 7z &>/dev/null; then
        ln -sf "$(which 7z)" "$SEVEN_ZIP_EXE"
        write_ok "Using system 7z"
    elif command -v 7zz &>/dev/null; then
        ln -sf "$(which 7zz)" "$SEVEN_ZIP_EXE"
        write_ok "Using system 7zz"
    else
        write_fail "No 7-Zip available"
        INSTALL_ERRORS+=("7-Zip")
    fi
fi

echo ""

# ----------------------------
# 2) iso2god-rs
# ----------------------------
echo -e "${YELLOW}[2/4] iso2god-rs Installation${NC}"
echo -e "${GRAY}----------------------------------------${NC}"

ISO2GOD_PATH="${INSTALL_DIR}/iso2god"

if [[ -f "$ISO2GOD_PATH" ]]; then
    write_ok "iso2god-rs already installed (skipping)."
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
            write_ok "iso2god-rs installed successfully."
        else
            INSTALL_ERRORS+=("iso2god-rs")
        fi
    else
        write_fail "No iso2god-rs build available for $OS:$ARCH"
        INSTALL_ERRORS+=("iso2god-rs")
    fi
fi

echo ""

# ----------------------------
# 3) GODSend backend (from cloned repo)
# ----------------------------
echo -e "${YELLOW}[3/4] GODSend Backend Installation${NC}"
echo -e "${GRAY}----------------------------------------${NC}"

GODSEND_EXE="${INSTALL_DIR}/godsend"

if [[ -f "$GODSEND_EXE" ]]; then
    write_ok "GODSend backend already installed (skipping)."
else
    write_info "Installing GODSend backend from repository..."
    
    case "$OS" in
        linux)  GODSEND_SRC="${REPO_CLONE_DIR}/source-control/godsend_linux" ;;
        macos)  GODSEND_SRC="${REPO_CLONE_DIR}/source-control/godsend_mac" ;;
        *)      GODSEND_SRC="" ;;
    esac
    
    if [[ -n "$GODSEND_SRC" ]] && [[ -f "$GODSEND_SRC" ]]; then
        cp "$GODSEND_SRC" "$GODSEND_EXE"
        chmod +x "$GODSEND_EXE"
        write_ok "GODSend backend installed successfully."
    else
        write_fail "GODSend binary not found: $GODSEND_SRC"
        INSTALL_ERRORS+=("GODSend Backend")
    fi
fi

echo ""

# ----------------------------
# 4) Xbox client files (from cloned repo)
# ----------------------------
echo -e "${YELLOW}[4/4] Xbox Client Files Installation${NC}"
echo -e "${GRAY}----------------------------------------${NC}"

CLIENT_SCRIPTS_DIR="${REPO_CLONE_DIR}/client-scripts"

if [[ -d "$CLIENT_SCRIPTS_DIR" ]]; then
    write_info "Copying Xbox client files..."
    if cp -r "${CLIENT_SCRIPTS_DIR}/"* "$XBOX_DIR/" 2>/dev/null; then
        write_ok "Xbox client files installed successfully."
    else
        write_fail "Failed to copy Xbox client files"
        INSTALL_ERRORS+=("Xbox Client Files")
    fi
else
    write_fail "client-scripts directory not found: $CLIENT_SCRIPTS_DIR"
    INSTALL_ERRORS+=("Xbox Client Files")
fi

echo ""

# ==========================================
# Cleanup - remove repo clone but keep Temp folder
# ==========================================

write_info "Cleaning up..."
rm -rf "$REPO_CLONE_DIR" 2>/dev/null || true
rm -f "${TEMP_DIR}"/*.tar.xz 2>/dev/null || true
write_ok "Cleanup complete. Temp folder preserved for application use."

# ==========================================
# Installation Summary
# ==========================================

echo ""
echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}  Installation Summary${NC}"
echo -e "${CYAN}========================================${NC}"
echo ""

if [[ ${#INSTALL_ERRORS[@]} -eq 0 ]]; then
    echo -e "${GREEN}[+] All components installed successfully!${NC}"
else
    echo -e "${YELLOW}[!] Installation completed with warnings:${NC}"
    for error in "${INSTALL_ERRORS[@]}"; do
        echo -e "${YELLOW}    - $error${NC}"
    done
fi

echo ""
echo -e "${CYAN}Installation Location:${NC}"
echo "  $INSTALL_DIR"
echo ""

# Check what's installed
INSTALLED_COMPONENTS=()
[[ -f "$SEVEN_ZIP_EXE" ]] && INSTALLED_COMPONENTS+=("7-Zip")
[[ -f "$ISO2GOD_PATH" ]] && INSTALLED_COMPONENTS+=("iso2god-rs")
[[ -f "$GODSEND_EXE" ]] && INSTALLED_COMPONENTS+=("GODSend Backend")
[[ $(find "$XBOX_DIR" -type f 2>/dev/null | wc -l) -gt 0 ]] && INSTALLED_COMPONENTS+=("Xbox Client Files")

echo -e "${CYAN}Installed Components:${NC}"
for component in "${INSTALLED_COMPONENTS[@]}"; do
    echo -e "  ${GREEN}[+]${NC} $component"
done

echo ""
echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}  Next Steps${NC}"
echo -e "${CYAN}========================================${NC}"
echo ""

if [[ -f "$GODSEND_EXE" ]]; then
    echo -e "${YELLOW}1. Start the GODSend server:${NC}"
    echo "   cd \"$INSTALL_DIR\""
    echo "   ./godsend"
    echo ""
fi

if [[ $(find "$XBOX_DIR" -type f 2>/dev/null | wc -l) -gt 0 ]]; then
    echo -e "${YELLOW}2. Copy Xbox client files to your Xbox:${NC}"
    echo "   From: \"$XBOX_DIR\""
    echo "   To: Your Aurora/Scripts folder on Xbox"
    echo ""
fi

echo -e "${CYAN}Tips:${NC}"
echo -e "${GRAY}  - Re-run with --force-clean to completely reinstall${NC}"
echo -e "${GRAY}  - Check the 'Ready' folder for converted games${NC}"
echo ""

exit 0
