/**
 * BadAvatar USB setup — downloads BadStick release packages and extracts them
 * to a FAT32 USB drive. Package URLs mirror LxcyDr0p/BadStick releases.
 * @see https://github.com/LxcyDr0p/BadStick
 */

import { app } from "electron";
import fs from "fs";
import path from "path";
import { spawn } from "child_process";
import https from "https";
import http from "http";
import { formatVolumeFat32 } from "../infrastructure/fat32Format";

const BADSTICK_PACKAGES_BASE =
  "https://github.com/LxcyDr0p/BadStick/releases/download/packages";

export interface UsbDriveInfo {
  rootPath: string;
  label: string;
  sizeBytes: number;
}

export interface BadAvatarPackage {
  id: string;
  fileName: string;
  downloadUrl: string;
  required?: boolean;
}

export const BADAVATAR_CORE_PACKAGES: BadAvatarPackage[] = [
  {
    id: "badavatar",
    fileName: "Payload-XeUnshackle.zip",
    downloadUrl: `${BADSTICK_PACKAGES_BASE}/Payload-XeUnshackle.zip`,
    required: true,
  },
  {
    id: "xexmenu",
    fileName: "XeXMenu.zip",
    downloadUrl: `${BADSTICK_PACKAGES_BASE}/XeXMenu.zip`,
    required: true,
  },
];

export const BADAVATAR_OPTIONAL_PACKAGES: BadAvatarPackage[] = [
  {
    id: "proto",
    fileName: "Proto.zip",
    downloadUrl: `${BADSTICK_PACKAGES_BASE}/Proto.zip`,
  },
  {
    id: "freestyle",
    fileName: "Freestyle.zip",
    downloadUrl: `${BADSTICK_PACKAGES_BASE}/Freestyle.zip`,
  },
  {
    id: "aurora",
    fileName: "Aurora.zip",
    downloadUrl: `${BADSTICK_PACKAGES_BASE}/Aurora.zip`,
  },
];

export interface BadAvatarCreateOptions {
  driveRoot: string;
  formatDrive: boolean;
  overwriteExisting: boolean;
  installProto: boolean;
  installFreestyle: boolean;
  installAurora: boolean;
}

export interface BadAvatarProgress {
  status: string;
  percent: number;
  detail?: string;
}

export type ProgressCallback = (progress: BadAvatarProgress) => void;

function cacheDir(): string {
  const dir = path.join(app.getPath("userData"), "badavatar-cache");
  if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true });
  return dir;
}

function normalizeRoot(driveRoot: string): string {
  let root = driveRoot.trim();
  if (process.platform === "win32") {
    if (!root.endsWith("\\")) root += "\\";
  } else if (!root.endsWith("/")) {
    root += "/";
  }
  return root;
}

function runCommand(
  command: string,
  args: string[],
): Promise<{ code: number; stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { windowsHide: true });
    let stdout = "";
    let stderr = "";
    child.stdout?.on("data", (d) => { stdout += d.toString(); });
    child.stderr?.on("data", (d) => { stderr += d.toString(); });
    child.on("error", reject);
    child.on("close", (code) => resolve({ code: code ?? 1, stdout, stderr }));
  });
}

export function isRunningAsAdmin(): Promise<boolean> {
  if (process.platform === "win32") {
    return runCommand("net", ["session"]).then((r) => r.code === 0);
  }
  if (process.platform === "linux") {
    return Promise.resolve(process.getuid?.() === 0);
  }
  // macOS: removable USB format via diskutil usually works without root
  return Promise.resolve(true);
}

/** True when elevation is required for formatting on this OS. */
export function formatRequiresElevation(): boolean {
  return process.platform === "win32" || process.platform === "linux";
}

async function listWindowsUsbDrives(): Promise<UsbDriveInfo[]> {
  const script = [
    "$usb = @(Get-Disk | Where-Object { $_.BusType -eq 'USB' } | Select-Object -ExpandProperty Number);",
    "Get-Partition | Where-Object {",
    "  $_.DriveLetter -and ($usb -contains $_.DiskNumber -or (Get-Disk -Number $_.DiskNumber).BusType -eq 'USB')",
    "} | ForEach-Object {",
    "  $v = Get-Volume -Partition $_ -ErrorAction SilentlyContinue;",
    "  [PSCustomObject]@{",
    "    RootPath = ($_.DriveLetter.ToString() + ':\\');",
    "    Label = if ($v.FileSystemLabel) { $v.FileSystemLabel } else { 'No Label' };",
    "    SizeBytes = [int64]($_.Size)",
    "  }",
    "} | ConvertTo-Json -Compress",
  ].join(" ");

  const { code, stdout } = await runCommand("powershell.exe", [
    "-NoProfile",
    "-Command",
    script,
  ]);
  if (code !== 0 || !stdout.trim()) {
    // Fallback: any removable volume with a drive letter
    const fallback = [
      "Get-Volume | Where-Object {",
      "  $_.DriveLetter -and $_.DriveType -eq 'Removable'",
      "} | ForEach-Object {",
      "  [PSCustomObject]@{",
      "    RootPath = ($_.DriveLetter.ToString() + ':\\');",
      "    Label = if ($_.FileSystemLabel) { $_.FileSystemLabel } else { 'No Label' };",
      "    SizeBytes = [int64]$_.Size",
      "  }",
      "} | ConvertTo-Json -Compress",
    ].join(" ");
    const fb = await runCommand("powershell.exe", ["-NoProfile", "-Command", fallback]);
    if (fb.code !== 0 || !fb.stdout.trim()) return [];
    return parseDriveJson(fb.stdout);
  }
  return parseDriveJson(stdout);
}

function parseDriveJson(stdout: string): UsbDriveInfo[] {
  try {
    const parsed = JSON.parse(stdout.trim());
    const rows = Array.isArray(parsed) ? parsed : [parsed];
    return rows
      .filter((r) => r && r.RootPath)
      .map((r) => ({
        rootPath: String(r.RootPath),
        label: String(r.Label || "No Label"),
        sizeBytes: Number(r.SizeBytes) || 0,
      }));
  } catch {
    return [];
  }
}

async function listDarwinUsbDrives(): Promise<UsbDriveInfo[]> {
  const roots: UsbDriveInfo[] = [];
  const volumesRoot = "/Volumes";
  if (!fs.existsSync(volumesRoot)) return roots;

  let entries: string[] = [];
  try {
    entries = fs.readdirSync(volumesRoot);
  } catch {
    return roots;
  }

  for (const entry of entries) {
    if (entry.startsWith(".")) continue;
    const full = path.join(volumesRoot, entry);
    try {
      const stat = fs.statSync(full);
      if (!stat.isDirectory()) continue;

      const info = await runCommand("diskutil", ["info", full]);
      if (info.code !== 0) continue;
      if (/Internal:\s+Yes/i.test(info.stdout)) continue;
      if (!/Protocol:\s+USB|Device Location:\s+External|Removable Media:\s+Removable/i.test(info.stdout)) {
        if (!/Internal:\s+No/i.test(info.stdout)) continue;
      }

      const sizeMatch = info.stdout.match(/Disk Size:\s+[\d.]+\s+\w+\s+\((\d+)\s+Bytes\)/);
      roots.push({
        rootPath: full.endsWith("/") ? full : `${full}/`,
        label: entry,
        sizeBytes: sizeMatch ? Number(sizeMatch[1]) : 0,
      });
    } catch {
      /* skip */
    }
  }
  return roots;
}

function walkMountDirs(dir: string, depth: number, roots: UsbDriveInfo[], seen: Set<string>): void {
  if (depth > 4 || !fs.existsSync(dir)) return;
  let entries: string[] = [];
  try {
    entries = fs.readdirSync(dir);
  } catch {
    return;
  }
  for (const entry of entries) {
    if (entry.startsWith(".")) continue;
    const full = path.join(dir, entry);
    addLinuxMount(full, roots, seen);
    try {
      if (fs.statSync(full).isDirectory()) {
        walkMountDirs(full, depth + 1, roots, seen);
      }
    } catch {
      /* skip */
    }
  }
}

async function listLinuxUsbDrives(): Promise<UsbDriveInfo[]> {
  const roots: UsbDriveInfo[] = [];
  const seen = new Set<string>();

  const lsblk = await runCommand("lsblk", [
    "-J",
    "-o",
    "NAME,SIZE,LABEL,MOUNTPOINT,RM,TYPE",
  ]).catch(() => ({ code: 1, stdout: "", stderr: "" }));

  if (lsblk.code === 0 && lsblk.stdout.trim()) {
    try {
      const tree = JSON.parse(lsblk.stdout);
      const devices = tree.blockdevices || [];
      const walkBlk = (nodes: any[]) => {
        for (const n of nodes) {
          if (n.rm === true || n.rm === "1" || n.rm === 1) {
            if (n.type === "part" && n.mountpoint) {
              const mp = String(n.mountpoint);
              const rootPath = mp.endsWith("/") ? mp : `${mp}/`;
              if (!seen.has(rootPath)) {
                seen.add(rootPath);
                roots.push({
                  rootPath,
                  label: n.label || path.basename(mp),
                  sizeBytes: Number(n.size) || 0,
                });
              }
            }
          }
          if (n.children) walkBlk(n.children);
        }
      };
      walkBlk(devices);
    } catch {
      /* fall through to mount walk */
    }
  }

  for (const mountRoot of ["/media", "/run/media", "/mnt"]) {
    walkMountDirs(mountRoot, 0, roots, seen);
  }
  return roots;
}

function addLinuxMount(full: string, roots: UsbDriveInfo[], seen: Set<string>): void {
  try {
    const stat = fs.statSync(full);
    if (!stat.isDirectory()) return;
    const rootPath = full.endsWith("/") ? full : `${full}/`;
    if (seen.has(rootPath)) return;
    if (full === "/media" || full === "/mnt") return;
    seen.add(rootPath);
    roots.push({
      rootPath,
      label: path.basename(full),
      sizeBytes: 0,
    });
  } catch {
    /* skip */
  }
}

/** USB / external drives (any filesystem) suitable for BadAvatar setup. */
export async function listFat32UsbDrives(): Promise<UsbDriveInfo[]> {
  if (process.platform === "win32") return listWindowsUsbDrives();
  if (process.platform === "darwin") return listDarwinUsbDrives();
  if (process.platform === "linux") return listLinuxUsbDrives();
  return [];
}

function isValidZip(filePath: string): boolean {
  try {
    const fd = fs.openSync(filePath, "r");
    const buf = Buffer.alloc(4);
    fs.readSync(fd, buf, 0, 4, 0);
    fs.closeSync(fd);
    return buf[0] === 0x50 && buf[1] === 0x4b;
  } catch {
    return false;
  }
}

function downloadFile(
  url: string,
  destPath: string,
  onProgress: (percent: number) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const follow = (fetchUrl: string, redirects = 0) => {
      if (redirects > 8) {
        reject(new Error("Too many redirects"));
        return;
      }
      const lib = fetchUrl.startsWith("https:") ? https : http;
      lib.get(fetchUrl, { headers: { "User-Agent": "GODsend-BadAvatar/1.0" } }, (res) => {
        if (res.statusCode && res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          follow(res.headers.location, redirects + 1);
          return;
        }
        if (res.statusCode && res.statusCode >= 400) {
          reject(new Error(`Download failed (${res.statusCode})`));
          return;
        }
        const total = Number(res.headers["content-length"]) || 0;
        let received = 0;
        const file = fs.createWriteStream(destPath);
        res.on("data", (chunk: Buffer) => {
          received += chunk.length;
          if (total > 0) onProgress(Math.min(99, Math.round((received / total) * 100)));
        });
        res.pipe(file);
        file.on("finish", () => {
          file.close(() => resolve());
        });
        file.on("error", (err) => {
          fs.unlink(destPath, () => reject(err));
        });
      }).on("error", reject);
    };
    follow(url);
  });
}

async function extractZip(
  zipPath: string,
  destPath: string,
  overwrite: boolean,
): Promise<void> {
  if (process.platform === "win32") {
    const script = overwrite
      ? `Expand-Archive -LiteralPath '${zipPath.replace(/'/g, "''")}' -DestinationPath '${destPath.replace(/'/g, "''")}' -Force`
      : [
          `$dest = '${destPath.replace(/'/g, "''")}';`,
          `Add-Type -AssemblyName System.IO.Compression.FileSystem;`,
          `[System.IO.Compression.ZipFile]::OpenRead('${zipPath.replace(/'/g, "''")}').Entries | ForEach-Object {`,
          `  $out = Join-Path $dest $_.FullName;`,
          `  if ($_.Name -and (Test-Path $out)) { return };`,
          `  $dir = Split-Path $out -Parent; if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null };`,
          `  if ($_.Name) { [System.IO.Compression.ZipFileExtensions]::ExtractToFile($_, $out, $false) }`,
          `}`,
        ].join(" ");
    const { code, stderr } = await runCommand("powershell.exe", [
      "-NoProfile",
      "-Command",
      script,
    ]);
    if (code !== 0) throw new Error(stderr.trim() || "Extraction failed");
    return;
  }

  const args = overwrite
    ? ["-o", zipPath, "-d", destPath]
    : ["-n", zipPath, "-d", destPath];
  const { code, stderr } = await runCommand("unzip", args);
  if (code !== 0) {
    throw new Error(stderr.trim() || "Extraction failed (is unzip installed?)");
  }
}

function buildPackageList(opts: BadAvatarCreateOptions): BadAvatarPackage[] {
  const list = [...BADAVATAR_CORE_PACKAGES];
  if (opts.installProto) {
    list.push(BADAVATAR_OPTIONAL_PACKAGES.find((p) => p.id === "proto")!);
  }
  if (opts.installFreestyle) {
    list.push(BADAVATAR_OPTIONAL_PACKAGES.find((p) => p.id === "freestyle")!);
  }
  if (opts.installAurora) {
    list.push(BADAVATAR_OPTIONAL_PACKAGES.find((p) => p.id === "aurora")!);
  }
  return list;
}

export async function createBadAvatarUsb(
  opts: BadAvatarCreateOptions,
  onProgress: ProgressCallback,
): Promise<void> {
  const usbRoot = normalizeRoot(opts.driveRoot);
  if (!fs.existsSync(usbRoot)) {
    throw new Error("Selected drive is not available.");
  }

  if (opts.formatDrive) {
    if (formatRequiresElevation()) {
      const admin = await isRunningAsAdmin();
      if (!admin) {
        const hint =
          process.platform === "linux"
            ? "Formatting on Linux requires root. Run GODsend with sudo or uncheck “Format USB”."
            : "Formatting requires Administrator privileges. Uncheck “Format USB” or restart GODsend as Administrator.";
        throw new Error(hint);
      }
    }
    await formatVolumeFat32(usbRoot, (p) =>
      onProgress({ status: p.status, percent: p.percent }),
    );
  } else {
    onProgress({ status: "Skipping format (per your settings)…", percent: 3 });
  }

  const packages = buildPackageList(opts);
  const tempDir = cacheDir();
  const total = packages.length;

  for (let i = 0; i < packages.length; i++) {
    const pkg = packages[i];
    const tempFile = path.join(tempDir, pkg.fileName);
    const basePercent = 10 + Math.round((i / total) * 80);

    const needsDownload =
      opts.overwriteExisting || !fs.existsSync(tempFile) || !isValidZip(tempFile);
    if (!needsDownload) {
      onProgress({
        status: `Using cached ${pkg.fileName}`,
        percent: basePercent,
        detail: `${i + 1}/${total}`,
      });
    } else {
      onProgress({
        status: `Downloading ${pkg.fileName}…`,
        percent: basePercent,
        detail: `${i + 1}/${total}`,
      });
      await downloadFile(pkg.downloadUrl, tempFile, (pct) => {
        onProgress({
          status: `Downloading ${pkg.fileName}…`,
          percent: basePercent + Math.round(pct * 0.4),
          detail: `${i + 1}/${total}`,
        });
      });
    }

    onProgress({
      status: `Extracting ${pkg.fileName}…`,
      percent: basePercent + 40,
      detail: `${i + 1}/${total}`,
    });
    await extractZip(tempFile, usbRoot, opts.overwriteExisting);
  }

  onProgress({ status: "Done! BadAvatar USB is ready.", percent: 100 });
}
