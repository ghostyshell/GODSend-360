#!/usr/bin/env node
/**
 * Verify that the Go binaries in dist/ have the correct executable
 * format for their target OS by inspecting magic bytes.
 *
 * This catches a class of build bug where someone forgets to set
 * GOOS=windows and the resulting Mach-O binary gets shipped as
 * `godsend.exe` — Windows refuses to load it with
 *   "the specified executable is not a valid application for this OS"
 * which surfaces in Electron as `spawn UNKNOWN`.
 *
 * Usage:
 *   node scripts/verify-go-binaries.js windows
 *   node scripts/verify-go-binaries.js darwin
 *   node scripts/verify-go-binaries.js darwin-amd64
 *   node scripts/verify-go-binaries.js darwin-arm64
 *   node scripts/verify-go-binaries.js linux
 *   node scripts/verify-go-binaries.js linux-amd64
 *   node scripts/verify-go-binaries.js linux-arm64
 *   node scripts/verify-go-binaries.js all
 *
 * Add --run to functionally smoke-test the Windows binary by launching it
 * under wine (if available) and confirming the banner prints + the HTTP
 * port comes up. Magic-byte check alone proves "this is a PE" — `--run`
 * proves "this actually executes on Windows".
 *
 *   node scripts/verify-go-binaries.js windows --run
 *   node scripts/verify-go-binaries.js all --run
 */

const fs = require("fs");
const path = require("path");
const { spawn, spawnSync } = require("child_process");

const DIST = path.resolve(__dirname, "..", "dist");

/**
 * Each target maps to { file: relative path in dist/, magic: expected first-N bytes, format: human-readable }.
 * - Windows PE/PE32+ files start with `MZ` (0x4D 0x5A).
 * - Mach-O 64-bit little-endian: 0xCF 0xFA 0xED 0xFE (Intel) or 0xCF 0xFA 0xED 0xFE (ARM64, same magic).
 *   We check the 64-bit magic only; arch is encoded later in the header.
 * - ELF: 0x7F 0x45 0x4C 0x46 ("\x7fELF").
 */
const TARGETS = {
  "windows":       { file: "godsend.exe",            magic: [0x4D, 0x5A],                   format: "Windows PE32+ (MZ)" },
  "windows-amd64": { file: "godsend.exe",            magic: [0x4D, 0x5A],                   format: "Windows PE32+ (MZ)" },
  "darwin-amd64":  { file: "godsend-darwin-amd64",   magic: [0xCF, 0xFA, 0xED, 0xFE],       format: "Mach-O 64-bit" },
  "darwin-arm64":  { file: "godsend-darwin-arm64",   magic: [0xCF, 0xFA, 0xED, 0xFE],       format: "Mach-O 64-bit" },
  "darwin":        { file: "godsend-mac",            magic: [0xCF, 0xFA, 0xED, 0xFE],       format: "Mach-O 64-bit" },
  "linux-amd64":   { file: "godsend-linux-x64",      magic: [0x7F, 0x45, 0x4C, 0x46],       format: "ELF" },
  "linux-arm64":   { file: "godsend-linux-arm64",    magic: [0x7F, 0x45, 0x4C, 0x46],       format: "ELF" },
  "linux":         null, // expanded to both at runtime
};

const ALL_KEYS = ["windows", "darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"];

function expand(arg) {
  if (arg === "all") return ALL_KEYS;
  if (arg === "linux") return ["linux-amd64", "linux-arm64"];
  if (arg === "darwin") return ["darwin-amd64", "darwin-arm64"];
  if (TARGETS[arg]) return [arg];
  throw new Error(`Unknown target: ${arg}. Use one of: ${[...Object.keys(TARGETS), "all"].join(", ")}`);
}

function checkOne(key) {
  const spec = TARGETS[key];
  if (!spec) throw new Error(`No spec for target ${key}`);
  const full = path.join(DIST, spec.file);
  if (!fs.existsSync(full)) {
    console.error(`[verify] ✗ ${spec.file} missing — expected at ${full}`);
    return false;
  }
  const stat = fs.statSync(full);
  if (stat.size < spec.magic.length + 32) {
    console.error(`[verify] ✗ ${spec.file} is too small (${stat.size} bytes) — likely a broken build`);
    return false;
  }
  const fd = fs.openSync(full, "r");
  const buf = Buffer.alloc(spec.magic.length);
  try {
    fs.readSync(fd, buf, 0, spec.magic.length, 0);
  } finally {
    fs.closeSync(fd);
  }
  const ok = spec.magic.every((b, i) => buf[i] === b);
  if (!ok) {
    const found = [...buf].map((b) => b.toString(16).padStart(2, "0")).join(" ");
    const expected = spec.magic.map((b) => b.toString(16).padStart(2, "0")).join(" ");
    console.error(`[verify] ✗ ${spec.file} has wrong magic bytes for ${spec.format}:`);
    console.error(`         expected:  ${expected}`);
    console.error(`         found:     ${found}`);
    console.error(`         This usually means the binary was built for the wrong GOOS.`);
    console.error(`         Re-run with explicit GOOS/GOARCH (see scripts/build-go-all.js).`);
    return false;
  }
  console.log(`[verify] ✓ ${spec.file} (${(stat.size / 1024 / 1024).toFixed(1)} MB) — ${spec.format}`);
  return true;
}

function hasWine() {
  const r = spawnSync("wine", ["--version"], { stdio: "ignore" });
  return r.status === 0;
}

/**
 * Launch godsend.exe under wine, wait up to `timeoutMs` for the banner +
 * GODSEND_LISTEN_PORT line, then kill the process. Returns true if both
 * markers were observed (proving the binary actually executes on Windows
 * and isn't just a "valid PE that crashes immediately").
 */
function smokeTestWindowsExe(timeoutMs = 15000) {
  if (!hasWine()) {
    console.warn(`[verify] ⚠ wine not installed — skipping functional smoke test`);
    console.warn(`         Install wine to enable: brew install --cask --no-quarantine wine-stable`);
    return true; // not a failure — opt-in
  }
  const exe = path.join(DIST, "godsend.exe");
  console.log(`[verify] · launching ${exe} under wine for ${timeoutMs / 1000}s…`);
  return new Promise((resolve) => {
    const child = spawn("wine", [exe], { stdio: ["ignore", "pipe", "pipe"] });
    let buf = "";
    let sawBanner = false;
    let sawPort = false;
    const settled = (ok, reason) => {
      try { child.kill("SIGTERM"); } catch (_) { /* ignore */ }
      setTimeout(() => { try { child.kill("SIGKILL"); } catch (_) { /* ignore */ } }, 1000);
      console.log(ok ? `[verify] ✓ wine smoke test passed (${reason})` : `[verify] ✗ wine smoke test FAILED (${reason})`);
      resolve(ok);
    };
    const onData = (chunk) => {
      buf += chunk.toString();
      if (!sawBanner && /GODSend Backend Server v/.test(buf)) {
        sawBanner = true;
      }
      if (!sawPort && /GODSEND_LISTEN_PORT=\d+/.test(buf)) {
        sawPort = true;
      }
      if (sawBanner && sawPort) settled(true, "banner + GODSEND_LISTEN_PORT seen");
    };
    child.stdout.on("data", onData);
    child.stderr.on("data", onData);
    child.on("error", (e) => settled(false, `wine spawn error: ${e.message}`));
    const timer = setTimeout(() => {
      if (sawBanner) settled(false, "banner printed but port never came up");
      else settled(false, "timeout — banner never printed (binary may have crashed at startup)");
    }, timeoutMs);
    child.on("exit", () => clearTimeout(timer));
  });
}

async function main() {
  const argv = process.argv.slice(2);
  const flags = new Set(argv.filter((a) => a.startsWith("--")));
  const targets = argv.filter((a) => !a.startsWith("--"));
  if (targets.length === 0) {
    console.error("Usage: verify-go-binaries.js <target> [--run]");
    console.error(`Targets: ${[...Object.keys(TARGETS).filter((k) => TARGETS[k]), "linux", "darwin", "all"].join(", ")}`);
    process.exit(2);
  }
  const keys = targets.flatMap((a) => expand(a));
  const results = keys.map((k) => checkOne(k));
  if (results.some((r) => !r)) process.exit(1);

  if (flags.has("--run") && (keys.includes("windows") || keys.includes("windows-amd64"))) {
    const ok = await smokeTestWindowsExe();
    if (!ok) process.exit(1);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
