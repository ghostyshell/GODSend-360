#!/usr/bin/env node
/**
 * Download the official FlareSolverr bundle for this platform into
 * vendor/flaresolverr/ so the desktop build can ship (and auto-start) it.
 *
 * Official assets exist for Linux x64 and Windows x64 only. On macOS (or arm64
 * hosts) this exits 0 with a note: point Settings at a LAN FlareSolverr/Byparr
 * instead. Any download failure is non-fatal - the app works without a solver.
 */
const https = require("https");
const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");
const crypto = require("crypto");

const VERSION = "v3.5.2";
const root = path.resolve(__dirname, "..", "vendor", "flaresolverr");
fs.mkdirSync(root, { recursive: true });

const platform = process.platform;
const arch = process.arch;
let asset;
let binaryName;
if (platform === "linux" && arch === "x64") {
  asset = "flaresolverr_linux_x64.tar.gz";
  binaryName = "flaresolverr";
} else if (platform === "win32" && arch === "x64") {
  asset = "flaresolverr_windows_x64.zip";
  binaryName = "flaresolverr.exe";
} else {
  console.log(
    `[flare-solver] No official FlareSolverr build for ${platform}-${arch}. ` +
      "Set the solver URL in Settings (a LAN Byparr/FlareSolverr works) or leave it unset."
  );
  process.exit(0);
}

const url = `https://github.com/FlareSolverr/FlareSolverr/releases/download/${VERSION}/${asset}`;
const dest = path.join(root, asset);

function download(url, out, redirects = 0) {
  return new Promise((resolve, reject) => {
    https
      .get(url, { headers: { "User-Agent": "godsend-build" } }, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          if (redirects > 5) return reject(new Error("too many redirects"));
          res.resume();
          return download(res.headers.location, out, redirects + 1).then(resolve, reject);
        }
        if (res.statusCode !== 200) return reject(new Error(`HTTP ${res.statusCode}`));
        const file = fs.createWriteStream(out);
        res.pipe(file);
        file.on("finish", () => file.close(resolve));
        file.on("error", reject);
      })
      .on("error", reject);
  });
}

function findBinary(dir, name, depth = 0) {
  if (depth > 2) return null;
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isFile() && e.name === name) return p;
    if (e.isDirectory()) {
      const found = findBinary(p, name, depth + 1);
      if (found) return found;
    }
  }
  return null;
}

(async () => {
  const versionFile = path.join(root, "VERSION");
  const existing = findBinary(root, binaryName);
  if (existing && fs.existsSync(versionFile) && fs.readFileSync(versionFile, "utf8").trim() === VERSION) {
    console.log(`[flare-solver] already present: ${existing}`);
    return;
  }
  console.log(`[flare-solver] downloading ${url}`);
  await download(url, dest);

  // Optional integrity pin: export FLARESOLVERR_SHA256 to fail the build if the
  // upstream asset ever changes unexpectedly.
  const expected = (process.env.FLARESOLVERR_SHA256 || "").trim().toLowerCase();
  if (expected) {
    const got = crypto.createHash("sha256").update(fs.readFileSync(dest)).digest("hex");
    if (got !== expected) throw new Error(`sha256 mismatch (got ${got})`);
  }

  // Reject archive members that escape the target directory before extracting.
  // `tar` (bsdtar on Windows 10+) reads both .tar.gz and .zip.
  const entries = execFileSync("tar", ["-tf", dest], { encoding: "utf8", maxBuffer: 16 << 20 })
    .split("\n")
    .map((e) => e.trim())
    .filter(Boolean);
  for (const entry of entries) {
    const norm = entry.replace(/\\/g, "/");
    if (norm.startsWith("/") || norm.split("/").includes("..")) {
      throw new Error(`unsafe archive entry: ${entry}`);
    }
  }
  execFileSync("tar", ["-xf", dest, "-C", root], { stdio: "inherit" });
  fs.unlinkSync(dest);
  const bin = findBinary(root, binaryName);
  if (!bin) throw new Error("binary not found after extraction");
  fs.chmodSync(bin, 0o755);
  fs.writeFileSync(path.join(root, "VERSION"), VERSION + "\n");
  console.log(`[flare-solver] ready: ${bin}`);
})().catch((err) => {
  console.warn(`[flare-solver] skipped: ${err.message}`);
  process.exit(0);
});
