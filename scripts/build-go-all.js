#!/usr/bin/env node
/**
 * Cross-compile the Go server for Windows, Linux, and macOS (Intel + Apple Silicon).
 * Writes: godsend.exe, godsend-linux-x64, godsend-linux-arm64, godsend-darwin-amd64, godsend-darwin-arm64,
 * and copies arm64 -> godsend-mac (name expected by electron-builder mac + dev).
 */
const { spawnSync } = require("child_process");
const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const dist = path.join(root, "dist");
const serverDir = path.join(root, "src", "server");

fs.mkdirSync(dist, { recursive: true });

// Download aria2c for Windows + Linux (macOS uses Homebrew at runtime; skips if present).
{
  const dl = path.join(__dirname, "download-aria2.js");
  const r = spawnSync(process.execPath, [dl], { stdio: "inherit", cwd: root, env: process.env });
  if (r.status !== 0) process.exit(r.status ?? 1);
}

// fat32format.exe — bundled in Windows installer for BadAvatar USB (>32 GB FAT32).
{
  const dl = path.join(__dirname, "download-fat32format.js");
  const r = spawnSync(process.execPath, [dl], { stdio: "inherit", cwd: root, env: process.env });
  if (r.status !== 0) {
    console.warn(
      "[build-go-all] fat32format download failed — Windows NSIS/portable builds need dist/tools/fat32format.exe",
    );
  }
}

const targets = [
  ["windows", "amd64", "godsend.exe"],
  ["linux", "amd64", "godsend-linux-x64"],
  ["linux", "arm64", "godsend-linux-arm64"],
  ["darwin", "amd64", "godsend-darwin-amd64"],
  ["darwin", "arm64", "godsend-darwin-arm64"],
];

for (const [goos, goarch, name] of targets) {
  const out = path.join(dist, name);
  console.log(`\n[build-go-all] ${goos}/${goarch} -> dist/${name}`);
  const r = spawnSync("go", ["build", "-o", out, "."], {
    cwd: serverDir,
    stdio: "inherit",
    env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: "0" },
    // Never use shell: paths under "XBOX 360" break when the shell splits on spaces.
    shell: false,
  });
  if (r.status !== 0) {
    process.exit(r.status ?? 1);
  }
}

const arm64 = path.join(dist, "godsend-darwin-arm64");
const macDefault = path.join(dist, "godsend-mac");
fs.copyFileSync(arm64, macDefault);
if (process.platform !== "win32") {
  try {
    fs.chmodSync(macDefault, 0o755);
  } catch (_) {
    /* ignore */
  }
}
console.log("\n[build-go-all] dist/godsend-mac <- darwin/arm64 (use build:server:mac for Intel default)");

// Magic-byte verification: catch the "wrong GOOS" class of bug at build
// time instead of at user install time (spawn UNKNOWN on Windows).
{
  const verify = path.join(__dirname, "verify-go-binaries.js");
  const r = spawnSync(process.execPath, [verify, "all"], { stdio: "inherit", cwd: root, env: process.env });
  if (r.status !== 0) {
    console.error("[build-go-all] binary verification FAILED — refusing to ship a build with wrong-OS executables");
    process.exit(r.status ?? 1);
  }
}

