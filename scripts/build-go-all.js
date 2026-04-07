#!/usr/bin/env node
/**
 * Cross-compile the Go server for Windows, Linux, and macOS (Intel + Apple Silicon).
 * Writes: godsend.exe, godsend-linux, godsend-darwin-amd64, godsend-darwin-arm64,
 * and copies arm64 -> godsend-mac (name expected by electron-builder mac + dev).
 */
const { spawnSync } = require("child_process");
const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const dist = path.join(root, "dist");
const serverDir = path.join(root, "src", "server");

fs.mkdirSync(dist, { recursive: true });

// Download aria2c binaries for all platforms (skips if already present).
{
  const dl = path.join(__dirname, "download-aria2.js");
  const r = spawnSync(process.execPath, [dl], { stdio: "inherit", cwd: root, env: process.env });
  if (r.status !== 0) process.exit(r.status ?? 1);
}

const targets = [
  ["windows", "amd64", "godsend.exe"],
  ["linux", "amd64", "godsend-linux"],
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

