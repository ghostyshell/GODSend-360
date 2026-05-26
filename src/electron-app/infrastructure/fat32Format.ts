/**
 * Cross-platform FAT32 formatting (including drives > 32 GB).
 *
 * - Windows: Ridgecrop fat32format.exe (bundled next to the app)
 * - macOS:   diskutil + newfs_msdos
 * - Linux:   umount + mkfs.vfat
 */

import fs from "fs";
import path from "path";
import { spawn } from "child_process";
import { getBundledRoot, getRepoRoot } from "./fileSystem";

export interface FormatProgress {
  status: string;
  percent: number;
}

export type FormatProgressCallback = (p: FormatProgress) => void;

function runCommand(
  command: string,
  args: string[],
  opts: { stdin?: string } = {},
): Promise<{ code: number; stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { windowsHide: true });
    let stdout = "";
    let stderr = "";
    child.stdout?.on("data", (d) => { stdout += d.toString(); });
    child.stderr?.on("data", (d) => { stderr += d.toString(); });
    if (opts.stdin != null) {
      child.stdin?.write(opts.stdin);
      child.stdin?.end();
    }
    child.on("error", reject);
    child.on("close", (code) => resolve({ code: code ?? 1, stdout, stderr }));
  });
}

function driveLetterFromRoot(driveRoot: string): string {
  const m = driveRoot.trim().match(/^([A-Za-z]):/);
  if (!m) throw new Error(`Invalid Windows drive path: ${driveRoot}`);
  return m[1].toUpperCase();
}

export function resolveFat32FormatExe(): string | null {
  if (process.platform !== "win32") return null;

  const candidates = [
    path.join(getBundledRoot(), "fat32format.exe"),
    path.join(getRepoRoot(), "dist", "tools", "fat32format.exe"),
    path.join(getRepoRoot(), "tools", "fat32format", "fat32format.exe"),
  ];
  for (const p of candidates) {
    if (fs.existsSync(p)) return p;
  }
  return null;
}

async function dismountWindowsVolume(letter: string): Promise<void> {
  const script = [
    `$l = '${letter}';`,
    `try {`,
    `  $p = Get-Partition -DriveLetter $l -ErrorAction Stop;`,
    `  $paths = @($p.AccessPaths) | Where-Object { $_ };`,
    `  foreach ($ap in $paths) { Remove-PartitionAccessPath -DiskNumber $p.DiskNumber -PartitionNumber $p.PartitionNumber -AccessPath $ap -ErrorAction SilentlyContinue };`,
    `} catch {}`,
  ].join(" ");
  await runCommand("powershell.exe", ["-NoProfile", "-Command", script]);
}

async function formatWindowsFat32(
  driveRoot: string,
  label: string,
  onProgress: FormatProgressCallback,
): Promise<void> {
  const letter = driveLetterFromRoot(driveRoot);
  const exe = resolveFat32FormatExe();

  onProgress({ status: "Preparing drive…", percent: 4 });
  await dismountWindowsVolume(letter);

  if (exe) {
    onProgress({ status: "Formatting to FAT32 (large-volume tool)…", percent: 8 });
    // fat32format prompts for confirmation; -y (some builds) or stdin Y. Try -f when supported.
    let result = await runCommand(exe, ["-y", `${letter}:`]);
    if (result.code !== 0) {
      result = await runCommand(exe, ["-f", `${letter}:`], { stdin: "Y\n" });
    }
    if (result.code !== 0) {
      result = await runCommand(exe, [`${letter}:`], { stdin: "Y\n" });
    }
    if (result.code !== 0) {
      const msg = (result.stderr || result.stdout).trim();
      throw new Error(msg || "fat32format failed. Close other apps using the drive and try again.");
    }
  } else {
    onProgress({ status: "Formatting to FAT32…", percent: 8 });
    const script = [
      `$v = Get-Volume -DriveLetter '${letter}' -ErrorAction Stop;`,
      `$v | Format-Volume -FileSystem FAT32 -NewFileSystemLabel '${label.replace(/'/g, "''")}' -Force -Confirm:$false;`,
    ].join(" ");
    const { code, stderr } = await runCommand("powershell.exe", ["-NoProfile", "-Command", script]);
    if (code !== 0) {
      const err = stderr.trim() || "Format failed.";
      if (/32\s*GB|34359738368/i.test(err)) {
        throw new Error(
          `${err} Run "node scripts/download-fat32format.js" and rebuild, or place fat32format.exe next to GODsend.`,
        );
      }
      throw new Error(err);
    }
  }

  onProgress({ status: "Remounting drive…", percent: 12 });
  await runCommand("powershell.exe", [
    "-NoProfile",
    "-Command",
    `Get-Volume -DriveLetter '${letter}' -ErrorAction SilentlyContinue | Out-Null;`,
  ]);
}

async function macPartitionDevice(mountPoint: string): Promise<string> {
  const { code, stdout } = await runCommand("diskutil", ["info", "-plist", mountPoint]);
  if (code !== 0) throw new Error("Could not read drive info for formatting.");

  const devMatch = stdout.match(/DeviceIdentifier<\/key>\s*<string>(disk\d+s\d+)<\/string>/);
  if (devMatch?.[1]) return devMatch[1];

  const devNode = await runCommand("diskutil", ["info", mountPoint]);
  const line = devNode.stdout.match(/Device Node:\s+(\S+)/);
  if (line?.[1]) return line[1].replace("/dev/", "");

  throw new Error("Could not determine partition device for formatting.");
}

async function formatDarwinFat32(
  driveRoot: string,
  label: string,
  onProgress: FormatProgressCallback,
): Promise<void> {
  const mountPoint = driveRoot.replace(/\/$/, "");
  const dev = await macPartitionDevice(mountPoint);
  const rawDev = dev.startsWith("/dev/") ? dev : `/dev/r${dev}`;

  onProgress({ status: "Unmounting drive…", percent: 5 });
  await runCommand("diskutil", ["unmount", mountPoint]);

  onProgress({ status: "Formatting to FAT32…", percent: 8 });
  // newfs_msdos supports large FAT32 volumes (FAT32B / -F 32).
  let result = await runCommand("newfs_msdos", ["-F", "32", "-v", label, rawDev]);
  if (result.code !== 0) {
    result = await runCommand("diskutil", ["eraseVolume", "MS-DOS FAT32", label, dev]);
  }
  if (result.code !== 0) {
    throw new Error((result.stderr || result.stdout).trim() || "Format failed.");
  }

  onProgress({ status: "Remounting drive…", percent: 12 });
  await runCommand("diskutil", ["mount", dev]);
}

async function linuxBlockDevice(mountPoint: string): Promise<string> {
  const r = await runCommand("findmnt", ["-n", "-o", "SOURCE", "--target", mountPoint]);
  if (r.code === 0 && r.stdout.trim()) {
    return r.stdout.trim();
  }

  const mounts = fs.readFileSync("/proc/mounts", "utf8");
  const norm = mountPoint.replace(/\/$/, "");
  for (const line of mounts.split("\n")) {
    const parts = line.split(" ");
    if (parts.length >= 2 && parts[1] === norm) {
      return parts[0];
    }
  }
  throw new Error("Could not determine block device for formatting.");
}

async function formatLinuxFat32(
  driveRoot: string,
  label: string,
  onProgress: FormatProgressCallback,
): Promise<void> {
  const mountPoint = driveRoot.replace(/\/$/, "");
  const dev = await linuxBlockDevice(mountPoint);

  onProgress({ status: "Unmounting drive…", percent: 5 });
  const umount = await runCommand("umount", [mountPoint]);
  if (umount.code !== 0) {
    const umountLazy = await runCommand("umount", ["-l", mountPoint]);
    if (umountLazy.code !== 0) {
      throw new Error(
        (umount.stderr || umount.stdout).trim() ||
          "Could not unmount drive. Close files using the USB and try again (may require root).",
      );
    }
  }

  onProgress({ status: "Formatting to FAT32…", percent: 8 });
  const mkfsCandidates: [string, string[]][] = [
    ["mkfs.vfat", ["-F", "32", "-n", label, dev]],
    ["mkfs.fat", ["-F", "32", "-n", label, dev]],
  ];

  let lastErr = "";
  for (const [cmd, args] of mkfsCandidates) {
    const result = await runCommand(cmd, args).catch(() => ({
      code: 1,
      stdout: "",
      stderr: `${cmd} not found`,
    }));
    if (result.code === 0) {
      onProgress({ status: "Remounting drive…", percent: 12 });
      await runCommand("mount", [dev, mountPoint]).catch(() => ({ code: 0, stdout: "", stderr: "" }));
      return;
    }
    lastErr = (result.stderr || result.stdout).trim();
  }

  throw new Error(
    lastErr ||
      "mkfs.vfat failed. Install dosfstools (mkfs.vfat) and run GODsend as root for formatting.",
  );
}

/** Format a mounted USB volume as FAT32 (any capacity supported on each OS). */
export async function formatVolumeFat32(
  driveRoot: string,
  onProgress: FormatProgressCallback,
  label = "BADAVATAR",
): Promise<void> {
  onProgress({ status: "Formatting drive to FAT32…", percent: 3 });

  if (process.platform === "win32") {
    await formatWindowsFat32(driveRoot, label, onProgress);
    return;
  }
  if (process.platform === "darwin") {
    await formatDarwinFat32(driveRoot, label, onProgress);
    return;
  }
  if (process.platform === "linux") {
    await formatLinuxFat32(driveRoot, label, onProgress);
    return;
  }

  throw new Error(`FAT32 formatting is not supported on ${process.platform}.`);
}

export function formatToolAvailable(): boolean {
  if (process.platform === "win32") {
    return resolveFat32FormatExe() != null || true; // PowerShell fallback
  }
  return process.platform === "darwin" || process.platform === "linux";
}
