#Requires -Version 5.1
<#
.SYNOPSIS
    GODSend Installer (Windows)
.DESCRIPTION
    Creates a "godsend" folder next to this script
    Clones repo and downloads dependencies
.PARAMETER ForceClean
    Remove existing installation before installing
.EXAMPLE
    .\install_godsend.ps1
.EXAMPLE
    .\install_godsend.ps1 -ForceClean
#>
[CmdletBinding()]
param(
    [switch]$ForceClean
)

# ==========================================
# Administrator Check and Auto-Elevation
# ==========================================

$isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

if (-not $isAdmin) {
    Write-Host "This script requires Administrator privileges." -ForegroundColor Yellow
    Write-Host "Attempting to restart as Administrator..." -ForegroundColor Cyan
    
    try {
        $scriptPath = $MyInvocation.MyCommand.Path
        $arguments = ""
        if ($ForceClean) {
            $arguments = "-ForceClean"
        }
        
        Start-Process powershell.exe -Verb RunAs -ArgumentList "-ExecutionPolicy Bypass -File `"$scriptPath`" $arguments"
        exit
    } catch {
        Write-Host "Failed to elevate privileges. Please run this script as Administrator manually." -ForegroundColor Red
        Pause
        exit 1
    }
}

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
$ProgressPreference = "SilentlyContinue"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# ==========================================
# Configuration
# ==========================================

$RepoUrl = "https://gitgud.io/Nesquin/godsend-homelab-edition.git"

# ==========================================
# Helper Functions
# ==========================================

function Write-Info($msg)  { Write-Host "[*] $msg" -ForegroundColor Cyan }
function Write-Ok($msg)    { Write-Host "[+] $msg" -ForegroundColor Green }
function Write-Warn($msg)  { Write-Host "[!] $msg" -ForegroundColor Yellow }
function Write-Fail($msg)  { Write-Host "[x] $msg" -ForegroundColor Red }

function Ensure-Dir([string]$Path) {
    if (!(Test-Path -LiteralPath $Path)) {
        New-Item -ItemType Directory -Force -Path $Path | Out-Null
    }
}

function Download-File([string]$Url, [string]$OutFile, [int]$Retries = 3, [int]$MinSizeBytes = 100) {
    Ensure-Dir (Split-Path -Parent $OutFile)

    for ($i = 1; $i -le $Retries; $i++) {
        try {
            Write-Info "Downloading: $(Split-Path -Leaf $OutFile)"
            Write-Host "    From: $Url" -ForegroundColor DarkGray
            
            Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing
            
            if (!(Test-Path -LiteralPath $OutFile)) {
                throw "Downloaded file not found: $OutFile"
            }
            
            $fileSize = (Get-Item -LiteralPath $OutFile).Length
            if ($fileSize -lt $MinSizeBytes) {
                throw "Downloaded file too small ($fileSize bytes, expected at least $MinSizeBytes): $OutFile"
            }
            
            Write-Host "    Downloaded: $([math]::Round($fileSize / 1KB, 2)) KB" -ForegroundColor DarkGray
            return $true
        } catch {
            if ($i -eq $Retries) { 
                Write-Fail "Download failed after $Retries attempts: $($_.Exception.Message)"
                throw 
            }
            Write-Warn "Download failed (attempt $i/$Retries). Retrying in $($i * 2) seconds..."
            Start-Sleep -Seconds (2 * $i)
        }
    }
}

function Test-GitInstalled {
    try {
        $null = Get-Command git -ErrorAction Stop
        return $true
    } catch {
        return $false
    }
}

# ==========================================
# Installation Setup
# ==========================================

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  GODSend Installation Script" -ForegroundColor Cyan
Write-Host "  Running as Administrator" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Check for Git
if (-not (Test-GitInstalled)) {
    Write-Fail "Git is not installed or not in PATH."
    Write-Host ""
    Write-Host "Please install Git from: https://git-scm.com/download/win" -ForegroundColor Yellow
    Write-Host "Or install via winget: winget install Git.Git" -ForegroundColor Yellow
    Write-Host ""
    Pause
    exit 1
}

Write-Ok "Git is installed."

# Determine script location
if ($PSScriptRoot) {
    $ScriptDir = $PSScriptRoot
} else {
    $ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
}

if ([string]::IsNullOrWhiteSpace($ScriptDir)) {
    $ScriptDir = Get-Location
}

$InstallDir = Join-Path $ScriptDir "godsend"

Write-Info "Installation directory: $InstallDir"

# Handle ForceClean
if ($ForceClean -and (Test-Path -LiteralPath $InstallDir)) {
    Write-Warn "ForceClean enabled: removing existing installation..."
    try {
        Remove-Item -LiteralPath $InstallDir -Recurse -Force
        Write-Ok "Previous installation removed."
    } catch {
        Write-Fail "Could not remove existing directory: $($_.Exception.Message)"
        exit 1
    }
}

# Create directory structure FIRST
Ensure-Dir $InstallDir

$ReadyDir  = Join-Path $InstallDir "Ready"
$TempDir   = Join-Path $InstallDir "Temp"
$XboxDir   = Join-Path $InstallDir "MOVE_THESE_FILES_TO_XBOX"
$RepoCloneDir = Join-Path $TempDir "repo-clone"

Ensure-Dir $ReadyDir
Ensure-Dir $TempDir
Ensure-Dir $XboxDir

Write-Ok "Directory structure created."
Write-Host ""

$installErrors = @()

# ==========================================
# Clone Repository
# ==========================================

Write-Host "[0/4] Cloning GODSend Repository" -ForegroundColor Yellow
Write-Host "----------------------------------------" -ForegroundColor DarkGray

Write-Info "Cloning repository from $RepoUrl..."
try {
    if (Test-Path -LiteralPath $RepoCloneDir) {
        Remove-Item -LiteralPath $RepoCloneDir -Recurse -Force
    }
    
    $gitResult = git clone --depth 1 $RepoUrl $RepoCloneDir 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "Git clone failed: $gitResult"
    }
    Write-Ok "Repository cloned successfully."
} catch {
    Write-Fail "Failed to clone repository: $($_.Exception.Message)"
    $installErrors += "Repository Clone"
}

Write-Host ""

# ==========================================
# Component Installation
# ==========================================

# ----------------------------
# 1) 7-Zip Extra (Windows)
# ----------------------------
Write-Host "[1/4] 7-Zip Installation" -ForegroundColor Yellow
Write-Host "----------------------------------------" -ForegroundColor DarkGray

$SevenZipExe = Join-Path $InstallDir "7za.exe"

if (Test-Path -LiteralPath $SevenZipExe) {
    Write-Ok "7-Zip already installed (skipping)."
} else {
    try {
        Write-Info "Downloading 7-Zip Extra package..."
        
        $7zExtraArchive = Join-Path $TempDir "7z1900-extra.7z"
        
        Download-File "https://7-zip.org/a/7z1900-extra.7z" $7zExtraArchive -MinSizeBytes 100KB
        
        # Download 7zr.exe (standalone extractor) to extract the archive
        $7zrExe = Join-Path $TempDir "7zr.exe"
        Write-Info "Downloading 7zr.exe to extract the package..."
        Download-File "https://www.7-zip.org/a/7zr.exe" $7zrExe -MinSizeBytes 100KB
        
        # Extract all files from x64 folder directly to install directory
        Write-Info "Extracting 7-Zip files to install directory..."
        $extractArgs = "e `"$7zExtraArchive`" -o`"$InstallDir`" x64/*.* -y"
        $extractProcess = Start-Process -FilePath $7zrExe -ArgumentList $extractArgs -Wait -NoNewWindow -PassThru
        
        if ($extractProcess.ExitCode -ne 0) {
            throw "Extraction failed with exit code: $($extractProcess.ExitCode)"
        }
        
        # Verify 7za.exe was extracted
        if (!(Test-Path -LiteralPath $SevenZipExe)) {
            throw "7za.exe not found after extraction"
        }
        
        Write-Ok "7-Zip installed successfully."
    } catch {
        Write-Fail "7-Zip installation failed: $($_.Exception.Message)"
        $installErrors += "7-Zip"
    }
}

Write-Host ""

# ----------------------------
# 2) iso2god-rs
# ----------------------------
Write-Host "[2/4] iso2god-rs Installation" -ForegroundColor Yellow
Write-Host "----------------------------------------" -ForegroundColor DarkGray

$Iso2GodPath = Join-Path $InstallDir "iso2god.exe"

if (Test-Path -LiteralPath $Iso2GodPath) {
    Write-Ok "iso2god-rs already installed (skipping)."
} else {
    try {
        Write-Info "Downloading iso2god-rs v1.8.1..."
        Download-File "https://github.com/iliazeus/iso2god-rs/releases/download/v1.8.1/iso2god-x86_64-windows.exe" $Iso2GodPath -MinSizeBytes 100KB
        Write-Ok "iso2god-rs installed successfully."
    } catch {
        Write-Fail "iso2god-rs installation failed: $($_.Exception.Message)"
        $installErrors += "iso2god-rs"
    }
}

Write-Host ""

# ----------------------------
# 3) GODSend backend (from cloned repo)
# ----------------------------
Write-Host "[3/4] GODSend Backend Installation" -ForegroundColor Yellow
Write-Host "----------------------------------------" -ForegroundColor DarkGray

$GodsendExe = Join-Path $InstallDir "godsend.exe"

if (Test-Path -LiteralPath $GodsendExe) {
    Write-Ok "GODSend backend already installed (skipping)."
} else {
    try {
        Write-Info "Installing GODSend backend from repository..."
        
        $GodsendSrc = Join-Path $RepoCloneDir "source-control\godsend_windows.exe"
        
        if (Test-Path -LiteralPath $GodsendSrc) {
            Copy-Item -LiteralPath $GodsendSrc -Destination $GodsendExe -Force
            Write-Ok "GODSend backend installed successfully."
        } else {
            throw "GODSend binary not found: $GodsendSrc"
        }
    } catch {
        Write-Fail "GODSend backend installation failed: $($_.Exception.Message)"
        $installErrors += "GODSend Backend"
    }
}

Write-Host ""

# ----------------------------
# 4) Xbox client files (from cloned repo)
# ----------------------------
Write-Host "[4/4] Xbox Client Files Installation" -ForegroundColor Yellow
Write-Host "----------------------------------------" -ForegroundColor DarkGray

$ClientScriptsDir = Join-Path $RepoCloneDir "client-scripts"

if (Test-Path -LiteralPath $ClientScriptsDir) {
    Write-Info "Copying Xbox client files..."
    try {
        Copy-Item -Path "$ClientScriptsDir\*" -Destination $XboxDir -Recurse -Force
        Write-Ok "Xbox client files installed successfully."
    } catch {
        Write-Fail "Failed to copy Xbox client files: $($_.Exception.Message)"
        $installErrors += "Xbox Client Files"
    }
} else {
    Write-Fail "client-scripts directory not found: $ClientScriptsDir"
    $installErrors += "Xbox Client Files"
}

Write-Host ""

# ==========================================
# Cleanup - remove repo clone but keep Temp folder
# ==========================================

Write-Info "Cleaning up..."
if (Test-Path -LiteralPath $RepoCloneDir) {
    Remove-Item -LiteralPath $RepoCloneDir -Recurse -Force -ErrorAction SilentlyContinue
}
# Remove downloaded archives from Temp
Get-ChildItem -LiteralPath $TempDir -File -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

Write-Ok "Cleanup complete. Temp folder preserved for application use."

# ==========================================
# Installation Summary
# ==========================================

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Installation Summary" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

if ($installErrors.Count -eq 0) {
    Write-Host "[+] All components installed successfully!" -ForegroundColor Green
} else {
    Write-Host "[!] Installation completed with warnings:" -ForegroundColor Yellow
    foreach ($err in $installErrors) {
        Write-Host "    - $err" -ForegroundColor Yellow
    }
}

Write-Host ""
Write-Host "Installation Location:" -ForegroundColor Cyan
Write-Host "  $InstallDir" -ForegroundColor White
Write-Host ""

# Check what's installed
$installedComponents = @()
if (Test-Path -LiteralPath $SevenZipExe) { $installedComponents += "7-Zip" }
if (Test-Path -LiteralPath $Iso2GodPath) { $installedComponents += "iso2god-rs" }
if (Test-Path -LiteralPath $GodsendExe) { $installedComponents += "GODSend Backend" }
if ((Get-ChildItem -LiteralPath $XboxDir -Recurse -File -ErrorAction SilentlyContinue).Count -gt 0) { $installedComponents += "Xbox Client Files" }

Write-Host "Installed Components:" -ForegroundColor Cyan
foreach ($component in $installedComponents) {
    Write-Host "  [+] $component" -ForegroundColor Green
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Next Steps" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

if (Test-Path -LiteralPath $GodsendExe) {
    Write-Host "1. Start the GODSend server:" -ForegroundColor Yellow
    Write-Host "   cd `"$InstallDir`"" -ForegroundColor White
    Write-Host "   .\godsend.exe" -ForegroundColor White
    Write-Host ""
}

if ((Get-ChildItem -LiteralPath $XboxDir -Recurse -File -ErrorAction SilentlyContinue).Count -gt 0) {
    Write-Host "2. Copy Xbox client files to your Xbox:" -ForegroundColor Yellow
    Write-Host "   From: `"$XboxDir`"" -ForegroundColor White
    Write-Host "   To: Your Aurora/Scripts folder on Xbox" -ForegroundColor White
    Write-Host ""
}

Write-Host "Tips:" -ForegroundColor Cyan
Write-Host "  - Re-run with -ForceClean to completely reinstall" -ForegroundColor DarkGray
Write-Host "  - Check the 'Ready' folder for converted games" -ForegroundColor DarkGray
Write-Host ""

Pause
