#!/usr/bin/env node
/**
 * Downloads Ridgecrop fat32format for Windows into dist/tools/.
 * Used by BadAvatar USB to format drives larger than 32 GB as FAT32.
 *
 * @see http://ridgecrop.co.uk/fat32format.htm
 *
 * Output: dist/tools/fat32format.exe
 */

const https = require("https");
const http = require("http");
const fs = require("fs");
const path = require("path");

const ROOT = path.resolve(__dirname, "..");
const OUT = path.join(ROOT, "dist", "tools", "fat32format.exe");

// Primary: community mirror of Ridgecrop fat32format (see tools/fat32format/README.md)
const EXE_URLS = [
  "https://raw.githubusercontent.com/Seabreg/fat32format/master/fat32format.exe",
];

const ZIP_URLS = [
  "http://ridgecrop.co.uk/files/fat32format.zip",
];

function get(url) {
  return new Promise((resolve, reject) => {
    const mod = url.startsWith("https") ? https : http;
    mod
      .get(url, { headers: { "User-Agent": "godsend-build/1" } }, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          return get(res.headers.location).then(resolve).catch(reject);
        }
        if (res.statusCode !== 200) {
          return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
        }
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      })
      .on("error", reject);
  });
}

function extractExeFromZip(buf) {
  // Local file header: PK\x03\x04 ... filename ... compressed data
  let off = 0;
  while (off < buf.length - 30) {
    if (buf[off] !== 0x50 || buf[off + 1] !== 0x4b || buf[off + 2] !== 0x03 || buf[off + 3] !== 0x04) {
      break;
    }
    const compMethod = buf.readUInt16LE(off + 8);
    const compSize = buf.readUInt32LE(off + 18);
    const nameLen = buf.readUInt16LE(off + 26);
    const extraLen = buf.readUInt16LE(off + 28);
    const nameStart = off + 30;
    const name = buf.slice(nameStart, nameStart + nameLen).toString("utf8");
    const dataStart = nameStart + nameLen + extraLen;
    const dataEnd = dataStart + compSize;
    if (/fat32format\.exe$/i.test(name)) {
      const payload = buf.slice(dataStart, dataEnd);
      if (compMethod === 0) return payload;
      throw new Error(`fat32format.exe is compressed (method ${compMethod}); extract manually`);
    }
    off = dataEnd;
  }
  throw new Error("fat32format.exe not found in zip");
}

async function main() {
  fs.mkdirSync(path.dirname(OUT), { recursive: true });
  let lastErr;

  for (const url of EXE_URLS) {
    try {
      console.log(`Fetching ${url}…`);
      const exe = await get(url);
      if (exe.length < 10000) throw new Error("file too small");
      fs.writeFileSync(OUT, exe);
      console.log(`Wrote ${OUT} (${exe.length} bytes)`);
      return;
    } catch (e) {
      lastErr = e;
      console.warn(`  failed: ${e.message}`);
    }
  }

  for (const url of ZIP_URLS) {
    try {
      console.log(`Fetching ${url}…`);
      const zip = await get(url);
      const exe = extractExeFromZip(zip);
      fs.writeFileSync(OUT, exe);
      console.log(`Wrote ${OUT} (${exe.length} bytes)`);
      return;
    } catch (e) {
      lastErr = e;
      console.warn(`  failed: ${e.message}`);
    }
  }
  console.error("Could not download fat32format. Place fat32format.exe manually at dist/tools/fat32format.exe");
  process.exit(lastErr ? 1 : 0);
}

main();
