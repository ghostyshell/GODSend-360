#!/usr/bin/env node
/**
 * Downloads aria2c binaries for all target platforms into dist/tools/.
 *
 * Sources:
 *   Windows x64  — official GitHub release zip
 *   Linux x64    — Homebrew Linuxbrew bottle (GHCR OCI blob)
 *   macOS arm64  — Homebrew bottle (GHCR OCI blob)
 *   macOS x64    — Homebrew bottle (GHCR OCI blob)
 *
 * Output files:
 *   dist/tools/aria2c.exe              (Windows)
 *   dist/tools/aria2c-linux            (Linux x64)
 *   dist/tools/aria2c-darwin-arm64     (macOS Apple Silicon)
 *   dist/tools/aria2c-darwin-amd64     (macOS Intel)
 */

const https = require("https");
const http = require("http");
const fs = require("fs");
const path = require("path");
const zlib = require("zlib");
const { execSync } = require("child_process");
const os = require("os");

const ROOT = path.resolve(__dirname, "..");
const DIST_TOOLS = path.join(ROOT, "dist", "tools");
fs.mkdirSync(DIST_TOOLS, { recursive: true });

const ARIA2_VERSION = "1.37.0";

// ── helpers ──────────────────────────────────────────────────────────────────

function get(url, headers = {}) {
  return new Promise((resolve, reject) => {
    const mod = url.startsWith("https") ? https : http;
    mod.get(url, { headers }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return get(res.headers.location, headers).then(resolve).catch(reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
      }
      const chunks = [];
      res.on("data", (c) => chunks.push(c));
      res.on("end", () => resolve(Buffer.concat(chunks)));
      res.on("error", reject);
    }).on("error", reject);
  });
}

function getJSON(url) {
  return get(url, { Accept: "application/json", "User-Agent": "godsend-build/1" })
    .then((b) => JSON.parse(b.toString()));
}

async function ghcrToken(repo) {
  const d = await getJSON(
    `https://ghcr.io/token?scope=repository:${repo}:pull&service=ghcr.io`
  );
  return d.token;
}

/** Download an OCI blob from GHCR and return the raw bytes (a .tar.gz layer). */
async function ghcrBlob(repo, digest) {
  const token = await ghcrToken(repo);
  return get(`https://ghcr.io/v2/${repo}/blobs/${digest}`, {
    Authorization: `Bearer ${token}`,
    Accept: "application/octet-stream",
    "User-Agent": "godsend-build/1",
  });
}

/**
 * Extract a single file from an in-memory .tar.gz buffer.
 * Walks entries until it finds one whose basename matches `targetName`.
 */
function extractFromTarGz(buf, targetName) {
  return new Promise((resolve, reject) => {
    const gunzip = zlib.createGunzip();
    const chunks = [];
    let found = false;

    // Minimal tar parser — fixed 512-byte blocks, POSIX ustar header.
    const BLOCK = 512;
    let pos = 0;
    let skipBlocks = 0;
    let collecting = false;
    let remaining = 0;
    let collected = [];

    function process(data) {
      while (pos < data.length) {
        if (skipBlocks > 0) {
          const skip = Math.min(skipBlocks * BLOCK, data.length - pos);
          pos += skip;
          skipBlocks -= Math.ceil(skip / BLOCK);
          continue;
        }
        if (collecting) {
          const take = Math.min(remaining, data.length - pos);
          collected.push(data.slice(pos, pos + take));
          remaining -= take;
          pos += take;
          if (remaining <= 0) {
            if (found) {
              resolve(Buffer.concat(collected));
              return;
            }
            collecting = false;
            collected = [];
            // Align to next block boundary
            const extra = BLOCK - (pos % BLOCK);
            if (extra < BLOCK) pos += extra;
          }
          continue;
        }
        if (pos + BLOCK > data.length) break; // need more data
        const header = data.slice(pos, pos + BLOCK);
        pos += BLOCK;
        // Check for end-of-archive (two zero blocks)
        if (header.every((b) => b === 0)) {
          reject(new Error(`${targetName} not found in tar`));
          return;
        }
        const nameBytes = header.slice(0, 100);
        const nameEnd = nameBytes.indexOf(0);
        const name = nameBytes.slice(0, nameEnd === -1 ? 100 : nameEnd).toString();
        const sizeStr = header.slice(124, 136).toString().replace(/\0/g, "").trim();
        const size = parseInt(sizeStr, 8) || 0;
        const typeflag = String.fromCharCode(header[156]);

        if (typeflag === "0" || typeflag === "\0") {
          // Regular file
          const base = path.basename(name);
          if (base === targetName) {
            found = true;
            collecting = true;
            remaining = size;
          } else {
            skipBlocks = Math.ceil(size / BLOCK);
          }
        }
        // Directories / links / etc — skip
      }
    }

    const allChunks = [];
    gunzip.on("data", (c) => {
      allChunks.push(c);
      process(Buffer.concat(allChunks));
    });
    gunzip.on("end", () => {
      if (!found) reject(new Error(`${targetName} not found in tar`));
    });
    gunzip.on("error", reject);
    gunzip.end(buf);
  });
}

/** Get aria2c from a Homebrew GHCR bottle. */
async function downloadHomebrew(repo, digest, binaryName, destPath) {
  if (fs.existsSync(destPath)) {
    console.log(`  skip (exists): ${path.basename(destPath)}`);
    return;
  }
  console.log(`  downloading Homebrew bottle for ${path.basename(destPath)}...`);
  const buf = await ghcrBlob(repo, digest);
  console.log(`  downloaded ${(buf.length / 1024 / 1024).toFixed(1)} MB, extracting aria2c...`);
  const binary = await extractFromTarGz(buf, binaryName);
  fs.writeFileSync(destPath, binary, { mode: 0o755 });
  console.log(`  written ${(binary.length / 1024).toFixed(0)} KB → ${destPath}`);
}

/** Get aria2c.exe from the official GitHub release zip. */
async function downloadWindowsZip(destPath) {
  if (fs.existsSync(destPath)) {
    console.log(`  skip (exists): ${path.basename(destPath)}`);
    return;
  }
  const url = `https://github.com/aria2/aria2/releases/download/release-${ARIA2_VERSION}/aria2-${ARIA2_VERSION}-win-64bit-build1.zip`;
  console.log(`  downloading Windows aria2c from ${url}...`);
  const buf = await get(url, { "User-Agent": "godsend-build/1" });
  console.log(`  downloaded ${(buf.length / 1024 / 1024).toFixed(1)} MB, extracting aria2c.exe...`);

  // Simple ZIP parser — find aria2c.exe (central directory or local file headers).
  const sig = Buffer.from([0x50, 0x4b, 0x03, 0x04]);
  let offset = 0;
  let found = null;
  while (offset < buf.length - 30) {
    if (buf.slice(offset, offset + 4).equals(sig)) {
      const nameLen = buf.readUInt16LE(offset + 26);
      const extraLen = buf.readUInt16LE(offset + 28);
      const compSize = buf.readUInt32LE(offset + 18);
      const uncomp = buf.readUInt32LE(offset + 22);
      const comprMethod = buf.readUInt16LE(offset + 8);
      const name = buf.slice(offset + 30, offset + 30 + nameLen).toString();
      const dataStart = offset + 30 + nameLen + extraLen;
      if (path.basename(name).toLowerCase() === "aria2c.exe") {
        if (comprMethod === 0) {
          found = buf.slice(dataStart, dataStart + uncomp);
        } else if (comprMethod === 8) {
          found = zlib.inflateRawSync(buf.slice(dataStart, dataStart + compSize));
        }
        break;
      }
      offset = dataStart + compSize;
    } else {
      offset++;
    }
  }
  if (!found) throw new Error("aria2c.exe not found in zip");
  fs.writeFileSync(destPath, found);
  console.log(`  written ${(found.length / 1024).toFixed(0)} KB → ${destPath}`);
}

// ── Homebrew bottle digests from: https://formulae.brew.sh/api/formula/aria2.json ──
// Run `node scripts/download-aria2.js --refresh` to update these.
const HOMEBREW_REPO = "homebrew/core/aria2";
const BOTTLES = {
  "linux-amd64":   "sha256:ce15dc949ff077b3ded7d07bb45964a17a44a603e97a6be66ead70e9682f1d96",
  "darwin-arm64":  "sha256:8253bf83d39fcdb91b7a251b2d38f0e32f21a0352f2e3798f5a376ba21ae68e9",
  "darwin-amd64":  "sha256:08007898a6dc4b162547081eb85329457345688279d6dce42f98d601e19ad799",
};

// ── main ─────────────────────────────────────────────────────────────────────

async function main() {
  // Optionally refresh bottle digests from the Homebrew API
  if (process.argv.includes("--refresh")) {
    console.log("Refreshing bottle digests from Homebrew API...");
    const d = await getJSON("https://formulae.brew.sh/api/formula/aria2.json");
    const files = d.bottle.stable.files;
    console.log("  x86_64_linux  :", files.x86_64_linux?.url);
    console.log("  arm64_tahoe   :", files.arm64_tahoe?.url);
    console.log("  sonoma (x64)  :", files.sonoma?.url);
    // Extract digests from URLs (sha256:<hex>)
    for (const [k, v] of Object.entries(files)) {
      const m = v.url.match(/sha256:([0-9a-f]+)/);
      if (m) console.log(`  ${k}: sha256:${m[1]}`);
    }
    return;
  }

  console.log(`\nDownloading aria2 ${ARIA2_VERSION} binaries → dist/tools/\n`);

  // Windows
  console.log("[1/4] Windows x64:");
  await downloadWindowsZip(path.join(DIST_TOOLS, "aria2c.exe"));

  // Linux x64
  console.log("[2/4] Linux x64:");
  await downloadHomebrew(
    HOMEBREW_REPO,
    BOTTLES["linux-amd64"],
    "aria2c",
    path.join(DIST_TOOLS, "aria2c-linux")
  );

  // macOS arm64
  console.log("[3/4] macOS arm64:");
  await downloadHomebrew(
    HOMEBREW_REPO,
    BOTTLES["darwin-arm64"],
    "aria2c",
    path.join(DIST_TOOLS, "aria2c-darwin-arm64")
  );

  // macOS x64
  console.log("[4/4] macOS x64:");
  await downloadHomebrew(
    HOMEBREW_REPO,
    BOTTLES["darwin-amd64"],
    "aria2c",
    path.join(DIST_TOOLS, "aria2c-darwin-amd64")
  );

  console.log("\nAll done.\n");
}

main().catch((e) => { console.error("ERROR:", e.message); process.exit(1); });
