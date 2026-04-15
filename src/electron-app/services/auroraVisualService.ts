import path from "path";
import fs from "fs";
import http from "http";

import { imageExtFromMagic, fetchHttpImage } from "../infrastructure/httpHelper";
import { gameCacheDir } from "../infrastructure/auroraLibraryCache";
import { getConfiguredServerPort } from "./settingsService";
import { addOutputLine } from "./backendClient";
import { getWebContentsForPush } from "../app/window";
import { backendPost } from "../infrastructure/backendHttp";

// ── Pure helpers ──────────────────────────────────────────────────────────────

export function auroraCdnUrl(relUnix: string): string {
  const r = String(relUnix || "").replace(/\\/g, "/").replace(/^\/+/, "");
  return `godsend-aurora://cdn/${r}`;
}

function safeVisualLocalName(name: string): string {
  return String(name || "").replace(/[^a-zA-Z0-9._-]+/g, "_").slice(0, 180) || "file";
}

export function classifyFlatMediaSuffix(titleId: string, filename: string): string {
  const base = path.basename(filename);
  const tid  = titleId.toUpperCase();
  const nu   = base.toUpperCase();
  if (!nu.startsWith(tid)) return "other";
  const dot  = nu.lastIndexOf(".");
  const stem = dot >= tid.length ? nu.slice(tid.length, dot) : nu.slice(tid.length);
  if (!stem) return "other";
  if (stem === "GC") return "cover";
  if (stem === "BK" || stem === "BG") return "background";
  if (stem === "BN" || stem === "BA") return "banner";
  if (stem === "IC" || stem === "IL" || stem === "IS") return "icon";
  if (/^SS\d*$/i.test(stem) || /^SC\d*$/i.test(stem)) return "screenshot";
  return "other";
}

export function classifyAuroraFileKind(name: string): string {
  const lower = String(name || "").toLowerCase();
  if (lower.endsWith(".asset")) return "asset";
  if (lower.endsWith(".bin"))   return "bin";
  if (/\.(jpg|jpeg|png|gif|bmp|dds)$/i.test(lower)) return "image";
  return "other";
}

export function emptyTitleVisualsPayload() {
  return { cover: null, background: null, banner: null, icon: null, screenshots: [], other: [] };
}

export function parseGameAssetInfoXml(xmlText: string): {
  background: string | null; banner: string | null; icon: string | null;
  cover: string | null; screenshots: string[];
} {
  const result = { background: null as string | null, banner: null as string | null,
    icon: null as string | null, cover: null as string | null, screenshots: [] as string[] };
  if (!xmlText || typeof xmlText !== "string") return result;

  for (const [, block] of xmlText.matchAll(/<live:asset[^>]*>([\s\S]*?)<\/live:asset>/gi)) {
    const urlM  = (block as string).match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
    const typeM = (block as string).match(/<live:relationshipType[^>]*>\s*(\d+)\s*<\/live:relationshipType>/i);
    if (!urlM || !typeM) continue;
    const url  = urlM[1].trim();
    const type = parseInt(typeM[1], 10);
    if      (type === 25 && !result.background) result.background = url;
    else if (type === 27 && !result.banner)     result.banner     = url;
    else if (type === 23 && !result.icon)       result.icon       = url;
    else if (type === 33 && !result.cover)      result.cover      = url;
  }

  for (const [, block] of xmlText.matchAll(/<live:slideShow[^>]*>([\s\S]*?)<\/live:slideShow>/gi)) {
    const urlM = (block as string).match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
    if (urlM) result.screenshots.push(urlM[1].trim());
  }

  return result;
}

export function summarizeGameCoverInfoJson(text: string | null): {
  entryCount: number; preview: any[]; parseError?: boolean;
} {
  if (!text || typeof text !== "string") return { entryCount: 0, preview: [] };
  let arr: any[];
  try {
    arr = JSON.parse(text);
  } catch {
    return { entryCount: 0, preview: [], parseError: true };
  }
  if (!Array.isArray(arr)) return { entryCount: 0, preview: [] };
  const preview = arr.slice(0, 12).map((e, i) => ({
    index:        i,
    official:     !!e?.official,
    rating:       e?.rating != null ? Number(e.rating) : null,
    hasFront:     !!(e?.front     && String(e.front).trim()),
    hasThumbnail: !!(e?.thumbnail && String(e.thumbnail).trim()),
    hasUrl:       !!(e?.url       && String(e.url).trim()),
  }));
  return { entryCount: arr.length, preview };
}

// ── Renderer push helpers ──────────────────────────────────────────────────────

export function emitAuroraTitleVisualEvents(titleId: string, gameDataDir: string, cacheRoot: string): void {
  const wc = getWebContentsForPush();
  if (!wc) return;
  const manifestPath = path.join(gameCacheDir(cacheRoot, gameDataDir), "visual-manifest.json");
  if (!fs.existsSync(manifestPath)) {
    wc.send("xbox-title-visuals", { titleId, gameDataDir, visuals: emptyTitleVisualsPayload() });
    return;
  }
  let m: any;
  try {
    m = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  } catch {
    wc.send("xbox-title-visuals", { titleId, gameDataDir, visuals: emptyTitleVisualsPayload() });
    return;
  }
  const toAsset = (o: any) =>
    o && o.rel ? { src: auroraCdnUrl(o.rel), ext: o.ext || "" } : null;
  wc.send("xbox-title-visuals", {
    titleId,
    gameDataDir,
    visuals: {
      coverIsBooklet: Boolean(m.importCover && m.importCover.rel),
      cover:          toAsset(m.importCover || m.mediaCover),
      background:     toAsset(m.background),
      banner:         toAsset(m.banner),
      icon:           toAsset(m.icon),
      screenshots: Array.isArray(m.screenshots)
        ? m.screenshots
            .map((s: any) => ({ src: s.rel ? auroraCdnUrl(s.rel) : "", ext: s.ext || "", name: s.name || "" }))
            .filter((s: any) => s.src)
        : [],
      other: Array.isArray(m.other)
        ? m.other
            .map((o: any) => ({ src: o.rel ? auroraCdnUrl(o.rel) : "", ext: o.ext || "", name: o.name || "" }))
            .filter((o: any) => o.src)
        : [],
    },
  });
}

export function emitAuroraCoverEvents(titleId: string, gameDataDir: string, cacheRoot: string): void {
  const wc = getWebContentsForPush();
  if (!wc) return;
  const gdir     = gameCacheDir(cacheRoot, gameDataDir);
  const metaP    = path.join(gdir, "cover-files.json");
  let primarySrc: string | null = null;
  if (fs.existsSync(metaP)) {
    try {
      const meta = JSON.parse(fs.readFileSync(metaP, "utf8"));
      if (meta.primaryFile && fs.existsSync(path.join(gdir, meta.primaryFile))) {
        primarySrc = auroraCdnUrl(`games/${gameDataDir}/${meta.primaryFile}`);
      }
    } catch { /* ignore */ }
  }
  if (!primarySrc && fs.existsSync(gdir)) {
    for (const name of fs.readdirSync(gdir)) {
      if (name.startsWith("cover-primary.")) {
        primarySrc = auroraCdnUrl(`games/${gameDataDir}/${name}`);
        break;
      }
    }
  }
  wc.send("xbox-cover", { titleId, gameDataDir, src: primarySrc });
}

// ── FTP batch helper (Go backend) ─────────────────────────────────────────────

async function batchFtp(xboxIp: string, ops: any[]): Promise<any[]> {
  const res = await backendPost("/ftp/batch", { ip: xboxIp, ops }, 120000);
  return res.results || [];
}

function bufFromBatchResult(results: any[], idx: number): Buffer | null {
  if (idx < 0) return null;
  const r = results[idx];
  if (r && r.ok && r.data) return Buffer.from(r.data, "base64");
  return null;
}

// ── RXEA decode via Go server (takes already-downloaded buffer) ───────────────

async function decodeRxeaBuffer(
  assetBuf: Buffer,
  titleId: string,
  assetName: string
): Promise<Record<number, Buffer>> {
  if (!assetBuf || assetBuf.length < 2048) return {};

  const goPort = getConfiguredServerPort();
  const decoded: any = await new Promise((res) => {
    const req = http.request(
      {
        host:    "127.0.0.1",
        port:    goPort,
        path:    "/rxea/decode",
        method:  "POST",
        headers: {
          "Content-Type":   "application/octet-stream",
          "Content-Length": assetBuf.length,
        },
      },
      (httpRes) => {
        const chunks: Buffer[] = [];
        httpRes.on("data", (c: Buffer) => chunks.push(c));
        httpRes.on("end", () => {
          try { res(JSON.parse(Buffer.concat(chunks).toString("utf8"))); }
          catch (e: any) {
            addOutputLine(`[WARN] RXEA sync ${titleId}/${assetName}: JSON parse error: ${e.message}`);
            res(null);
          }
        });
      }
    );
    req.on("error", (e: Error) => {
      addOutputLine(`[WARN] RXEA sync ${titleId}/${assetName}: Go server error: ${e.message}`);
      res(null);
    });
    req.end(assetBuf);
  });

  if (!decoded || !Array.isArray(decoded.slots)) {
    const goErr = decoded?.error ? ` — ${decoded.error}` : "";
    addOutputLine(`[WARN] RXEA sync ${titleId}/${assetName}: decoder returned no slots${goErr}.`);
    if (Array.isArray(decoded?.diags) && decoded.diags.length > 0) {
      for (const d of decoded.diags) {
        addOutputLine(
          `[DIAG] slot${d.slot}: off=${d.offset} sz=${d.size} fmt=${d.gpu_fmt} ` +
          `w=${d.width} h=${d.height} tiled=${d.tiled} endian=${d.endian}` +
          `${d.error ? ` err="${d.error}"` : ""}`
        );
      }
    }
    return {};
  }

  const result: Record<number, Buffer> = {};
  for (const s of decoded.slots) {
    const pngBuf = Buffer.isBuffer(s.png) ? s.png : Buffer.from(s.png, "base64");
    if (pngBuf.length >= 100) result[s.slot] = pngBuf;
  }
  addOutputLine(`[INFO] RXEA sync ${titleId}/${assetName}: decoded ${Object.keys(result).length} slot(s).`);
  return result;
}

// ── FTP fingerprint helpers ─────────────────────────────────────────────────────

function importListingFingerprint(entries: any[]): string {
  return entries
    .filter((e: any) => e && e.name && e.type !== "dir")
    .map((e: any) => `${e.name}:${e.size || 0}`)
    .sort()
    .join(",");
}

// ── Main sync functions ────────────────────────────────────────────────────────

/**
 * Sync Aurora title visual assets (RXEA assets, Import folder, CDN images)
 * for a single game.  Uses the Go backend batch endpoint (one FTP connection
 * for fingerprinting, one for bulk downloads).
 */
export async function syncAuroraTitleVisualAssets(
  xboxIp: string,
  auroraRoot: string,
  titleId: string,
  gameDataDir: string,
  cacheRoot: string,
  force: boolean
): Promise<void> {
  const gdir         = gameCacheDir(cacheRoot, gameDataDir);
  const vdir         = path.join(gdir, "visual");
  const importBase   = `${auroraRoot}/User/Import/${titleId}`;
  const gameDataPath = `${auroraRoot}/Data/GameData/${gameDataDir}`;
  fs.mkdirSync(vdir, { recursive: true });

  // ── Phase 1: Fingerprint batch (sizes + import listing — 1 FTP connection) ──
  const fpAssetKeys = [
    `BK${titleId}.asset`, `GC${titleId}.asset`,
    `GL${titleId}.asset`, `SS${titleId}.asset`,
    "GameAssetInfo.bin", "GameCoverInfo.bin",
  ];
  const fpOps: any[] = fpAssetKeys.map((name) => ({ op: "size", path: `${gameDataPath}/${name}` }));
  fpOps.push({ op: "list", path: importBase });

  const fpResults = await batchFtp(xboxIp, fpOps);

  const newFp: Record<string, any> = {};
  for (let i = 0; i < fpAssetKeys.length; i++) {
    newFp[fpAssetKeys[i]] = fpResults[i] && fpResults[i].ok ? Number(fpResults[i].data) : -1;
  }
  const listResult = fpResults[fpAssetKeys.length];
  let importEntries: any[] = [];
  if (listResult && listResult.ok && Array.isArray(listResult.data)) {
    importEntries = listResult.data;
  }
  newFp._importListing = importListingFingerprint(importEntries);

  // ── Phase 2: Early exit if all source fingerprints match the cached manifest ─
  const manifestPath = path.join(gdir, "visual-manifest.json");
  if (!force && fs.existsSync(manifestPath)) {
    try {
      const prev     = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
      const cachedFp = prev?._sourceFingerprints;
      if (cachedFp && typeof cachedFp === "object") {
        const allKeys  = [...fpAssetKeys, "_importListing"];
        const allMatch = allKeys.every((k) => {
          const cached  = cachedFp[k];
          const current = newFp[k];
          if (cached === undefined || cached === null) return current === -1 || current === "";
          return cached === current;
        });
        if (allMatch) return;  // unchanged — existing manifest is still valid
      }
    } catch { /* corrupt manifest — proceed with full sync */ }
  }

  // ── Phase 3: Build download batch for all needed files (1 FTP connection) ────
  const dlOps: any[] = [];

  // Index import files by stem
  const importByStem = new Map<string, { name: string; extWithDot: string }>();
  for (const e of importEntries) {
    if (!e || !e.name || e.type === "dir") continue;
    const dot  = e.name.lastIndexOf(".");
    const stem = (dot >= 0 ? e.name.slice(0, dot) : e.name).toLowerCase();
    const ext  = dot >= 0 ? e.name.slice(dot).toLowerCase() : "";
    if (!importByStem.has(stem)) importByStem.set(stem, { name: e.name, extWithDot: ext });
  }

  // Queue import-file downloads
  const importSlotStems: { slotKey: string; stems: string[] }[] = [
    { slotKey: "background",  stems: ["background", "boxartback"] },
    { slotKey: "banner",      stems: ["banner"] },
    { slotKey: "icon",        stems: ["icon"] },
    { slotKey: "importCover", stems: ["cover", "boxartfront"] },
  ];
  const importDownloads: { slotKey: string; localName: string; idx: number; entryName: string }[] = [];
  for (const { slotKey, stems } of importSlotStems) {
    for (const stem of stems) {
      const entry = importByStem.get(stem);
      if (!entry) continue;
      const localName = `import-${safeVisualLocalName(entry.name)}`;
      if (!force && fs.existsSync(path.join(vdir, localName))) {
        importDownloads.push({ slotKey, localName, idx: -1, entryName: entry.name });
      } else {
        const idx = dlOps.length;
        dlOps.push({ op: "download_base64", path: `${importBase}/${entry.name}` });
        importDownloads.push({ slotKey, localName, idx, entryName: entry.name });
      }
      break; // first matching stem wins
    }
  }

  // Queue import-screenshot downloads
  const importScreenshots: { sortKey: string; localName: string; idx: number; entryName: string }[] = [];
  for (let i = 1; i <= 10; i++) {
    const stems = [`screenshot${i}`, `screenshot${String(i).padStart(2, "0")}`];
    for (const stem of stems) {
      const entry = importByStem.get(stem);
      if (!entry) continue;
      const localName = `import-${safeVisualLocalName(entry.name)}`;
      const info = { sortKey: `${String(i).padStart(3, "0")}-import`, localName, idx: -1, entryName: entry.name };
      if (!force && fs.existsSync(path.join(vdir, localName))) {
        importScreenshots.push(info);
      } else {
        info.idx = dlOps.length;
        dlOps.push({ op: "download_base64", path: `${importBase}/${entry.name}` });
        importScreenshots.push(info);
      }
      break;
    }
  }

  // Queue RXEA asset downloads (only if remote size > 0)
  const rxeaFiles = [
    { name: `BK${titleId}.asset`, fpKey: `BK${titleId}.asset` },
    { name: `GC${titleId}.asset`, fpKey: `GC${titleId}.asset` },
    { name: `GL${titleId}.asset`, fpKey: `GL${titleId}.asset` },
    { name: `SS${titleId}.asset`, fpKey: `SS${titleId}.asset` },
  ];
  const rxeaDownloads: { name: string; idx: number }[] = [];
  for (const rf of rxeaFiles) {
    if (newFp[rf.fpKey] > 0) {
      const idx = dlOps.length;
      dlOps.push({ op: "download_base64", path: `${gameDataPath}/${rf.name}` });
      rxeaDownloads.push({ name: rf.name, idx });
    } else {
      rxeaDownloads.push({ name: rf.name, idx: -1 });
    }
  }

  // Queue data-file downloads
  let gameAssetInfoIdx = -1;
  let gameCoverInfoIdx = -1;
  if (newFp["GameAssetInfo.bin"] > 0) {
    gameAssetInfoIdx = dlOps.length;
    dlOps.push({ op: "download_base64", path: `${gameDataPath}/GameAssetInfo.bin` });
  }
  if (newFp["GameCoverInfo.bin"] > 0) {
    gameCoverInfoIdx = dlOps.length;
    dlOps.push({ op: "download_base64", path: `${gameDataPath}/GameCoverInfo.bin` });
  }

  // Execute download batch
  let dlResults: any[] = [];
  if (dlOps.length > 0) {
    dlResults = await batchFtp(xboxIp, dlOps);
  }

  // ── Phase 4: Process downloaded data ─────────────────────────────────────────
  const m: Record<string, any> = {
    importCover: null, mediaCover: null,
    background:  null, banner:     null,
    icon:        null, screenshots: [], other: [],
  };
  const screenshotSort: { sortKey: string; rel: string; ext: string; name: string }[] = [];

  function assetFor(localFileName: string) {
    return {
      rel: `games/${gameDataDir}/visual/${localFileName}`,
      ext: path.extname(localFileName).toLowerCase(),
    };
  }

  // Process import-file downloads
  for (const dl of importDownloads) {
    const lp = path.join(vdir, dl.localName);
    if (dl.idx < 0) {
      if (fs.existsSync(lp)) m[dl.slotKey] = assetFor(dl.localName);
      continue;
    }
    const buf = bufFromBatchResult(dlResults, dl.idx);
    if (!buf || buf.length < 16) continue;
    fs.writeFileSync(lp, buf);
    m[dl.slotKey] = assetFor(dl.localName);
  }

  // Process import-screenshot downloads
  for (const dl of importScreenshots) {
    const lp   = path.join(vdir, dl.localName);
    const info = {
      sortKey: dl.sortKey,
      rel:     assetFor(dl.localName).rel,
      ext:     assetFor(dl.localName).ext,
      name:    dl.entryName,
    };
    if (dl.idx < 0) {
      if (fs.existsSync(lp)) screenshotSort.push(info);
      continue;
    }
    const buf = bufFromBatchResult(dlResults, dl.idx);
    if (!buf || buf.length < 16) continue;
    fs.writeFileSync(lp, buf);
    screenshotSort.push(info);
  }

  // RXEA decode helper
  async function cacheDecodedSlot(slotKey: string, pngBuf: Buffer, localBase: string): Promise<void> {
    if (m[slotKey]) return;
    const localName = `rxea-${localBase}.png`;
    const lp = path.join(vdir, localName);
    if (!force && fs.existsSync(lp)) { m[slotKey] = assetFor(localName); return; }
    fs.writeFileSync(lp, pngBuf);
    m[slotKey] = assetFor(localName);
  }

  // BK asset → background
  {
    const buf = bufFromBatchResult(dlResults, rxeaDownloads[0].idx);
    if (buf && buf.length >= 2048) {
      const decoded = await decodeRxeaBuffer(buf, titleId, rxeaDownloads[0].name);
      if (decoded[4]) await cacheDecodedSlot("background", decoded[4], "bk-background");
    }
  }
  // GC asset → cover
  {
    const buf = bufFromBatchResult(dlResults, rxeaDownloads[1].idx);
    if (buf && buf.length >= 2048) {
      const decoded = await decodeRxeaBuffer(buf, titleId, rxeaDownloads[1].name);
      if (decoded[2]) await cacheDecodedSlot("importCover", decoded[2], "gc-cover");
    }
  }
  // GL asset → icon + banner
  {
    const buf = bufFromBatchResult(dlResults, rxeaDownloads[2].idx);
    if (buf && buf.length >= 2048) {
      const decoded = await decodeRxeaBuffer(buf, titleId, rxeaDownloads[2].name);
      if (decoded[0]) await cacheDecodedSlot("icon",   decoded[0], "gl-icon");
      if (decoded[1]) await cacheDecodedSlot("banner", decoded[1], "gl-banner");
    }
  }
  // SS asset → screenshots (only if no import screenshots)
  if (screenshotSort.length === 0) {
    const buf = bufFromBatchResult(dlResults, rxeaDownloads[3].idx);
    if (buf && buf.length >= 2048) {
      const decoded = await decodeRxeaBuffer(buf, titleId, rxeaDownloads[3].name);
      for (let si = 5; si <= 24; si++) {
        if (!decoded[si]) continue;
        const localName = `rxea-screenshot${si - 4}.png`;
        const lp = path.join(vdir, localName);
        if (!force && fs.existsSync(lp)) {
          screenshotSort.push({ sortKey: `${String(si - 4).padStart(3, "0")}-rxea`, rel: assetFor(localName).rel, ext: ".png", name: localName });
          continue;
        }
        fs.writeFileSync(lp, decoded[si]);
        screenshotSort.push({ sortKey: `${String(si - 4).padStart(3, "0")}-rxea`, rel: assetFor(localName).rel, ext: ".png", name: localName });
      }
    }
  }

  // GameAssetInfo.bin → CDN image fetches
  let assetInfo = { background: null as string | null, banner: null as string | null, icon: null as string | null, cover: null as string | null, screenshots: [] as string[] };
  {
    const buf = bufFromBatchResult(dlResults, gameAssetInfoIdx);
    if (buf && buf.length > 0) {
      assetInfo = parseGameAssetInfoXml(buf.toString("utf8"));
    }
  }

  async function pullCdnImage(slotKey: string, url: string | null, localBase: string): Promise<void> {
    if (m[slotKey] || !url) return;
    const rawExt    = path.extname(url).toLowerCase();
    const safeExt   = [".jpg", ".jpeg", ".png", ".gif"].includes(rawExt) ? rawExt : ".jpg";
    const localName = `cdnasset-${localBase}${safeExt}`;
    const lp        = path.join(vdir, localName);
    if (!force && fs.existsSync(lp)) { m[slotKey] = assetFor(localName); return; }
    const buf = await fetchHttpImage(url);
    if (!buf || buf.length < 100) return;
    const realExt   = imageExtFromMagic(buf);
    const finalName = `cdnasset-${localBase}${realExt}`;
    fs.writeFileSync(path.join(vdir, finalName), buf);
    m[slotKey] = assetFor(finalName);
  }

  await pullCdnImage("background",  assetInfo.background, "background");
  await pullCdnImage("banner",      assetInfo.banner,     "banner");
  await pullCdnImage("icon",        assetInfo.icon,       "icon");
  await pullCdnImage("importCover", assetInfo.cover,      "cover");

  if (screenshotSort.length === 0) {
    for (let i = 0; i < assetInfo.screenshots.length; i++) {
      const url = assetInfo.screenshots[i];
      if (!url) continue;
      const rawExt    = path.extname(url).toLowerCase();
      const safeExt   = [".jpg", ".jpeg", ".png", ".gif"].includes(rawExt) ? rawExt : ".jpg";
      const localName = `cdnasset-screenshot${i + 1}${safeExt}`;
      const lp        = path.join(vdir, localName);
      const info = {
        sortKey: `${String(i + 1).padStart(3, "0")}-cdn`,
        rel: assetFor(localName).rel, ext: safeExt, name: `Screenshot${i + 1}${safeExt}`,
      };
      if (!force && fs.existsSync(lp)) { screenshotSort.push(info); continue; }
      const buf = await fetchHttpImage(url);
      if (!buf || buf.length < 100) continue;
      const realExt   = imageExtFromMagic(buf);
      const finalName = `cdnasset-screenshot${i + 1}${realExt}`;
      fs.writeFileSync(path.join(vdir, finalName), buf);
      screenshotSort.push({ ...info, rel: assetFor(finalName).rel, ext: realExt });
    }
  }

  // GameCoverInfo.bin → mediaCover (XboxUnity best cover)
  {
    const buf = bufFromBatchResult(dlResults, gameCoverInfoIdx);
    if (buf && buf.length > 0) {
      try {
        let entries: any[] | null;
        try { entries = JSON.parse(buf.toString("utf8")); } catch { entries = null; }
        if (Array.isArray(entries) && entries.length > 0) {
          const sorted = [...entries].sort((a, b) => {
            if (!!a.official !== !!b.official) return a.official ? -1 : 1;
            return (Number(b.rating) || 0) - (Number(a.rating) || 0);
          });
          for (const row of sorted) {
            const url = (row.front || row.thumbnail || row.url || "").trim();
            if (!url) continue;
            const rawExt    = path.extname(url).toLowerCase();
            const safeExt   = [".jpg", ".jpeg", ".png", ".gif"].includes(rawExt) ? rawExt : ".jpg";
            const localName = `cdnasset-xboxunity-cover${safeExt}`;
            const lp        = path.join(vdir, localName);
            if (!force && fs.existsSync(lp)) { m.mediaCover = assetFor(localName); break; }
            const imgBuf = await fetchHttpImage(url);
            if (!imgBuf || imgBuf.length < 100) continue;
            const realExt   = imageExtFromMagic(imgBuf);
            const finalName = `cdnasset-xboxunity-cover${realExt}`;
            fs.writeFileSync(path.join(vdir, finalName), imgBuf);
            m.mediaCover = assetFor(finalName);
            break;
          }
        }
      } catch { /* GameCoverInfo parse error */ }
    }
  }

  screenshotSort.sort((a, b) => a.sortKey.localeCompare(b.sortKey));
  m.screenshots = screenshotSort.map((s) => ({ rel: s.rel, ext: s.ext, name: s.name }));

  m._sourceFingerprints = newFp;
  fs.writeFileSync(manifestPath, JSON.stringify(m, null, 2), "utf8");
}

/**
 * Sync Aurora cover art for a single game.  Downloads GameCoverInfo.bin from
 * the console, picks the best cover URL, fetches it via HTTP, and caches
 * locally.  Falls back to Aurora Media directory flat covers if needed.
 * Uses the Go backend batch endpoint for FTP operations.
 */
export async function syncAuroraGameCoverAssets(
  xboxIp: string,
  auroraRoot: string,
  mediaDir: string,
  titleId: string,
  gameDataDir: string,
  cacheRoot: string,
  force: boolean
): Promise<void> {
  const gdir      = gameCacheDir(cacheRoot, gameDataDir);
  fs.mkdirSync(gdir, { recursive: true });

  const remoteBin = `${auroraRoot}/Data/GameData/${gameDataDir}/GameCoverInfo.bin`;

  // Phase 1: Check remote GameCoverInfo.bin size
  const sizeResults = await batchFtp(xboxIp, [{ op: "size", path: remoteBin }]);
  let remoteSz = -1;
  if (sizeResults[0] && sizeResults[0].ok) remoteSz = Number(sizeResults[0].data);

  const binPath = path.join(gdir, "GameCoverInfo.bin");
  let needBin   = force;
  if (remoteSz >= 0) {
    if (!fs.existsSync(binPath))                       needBin = true;
    else if (fs.statSync(binPath).size !== remoteSz)   needBin = true;
  }

  if (needBin && remoteSz >= 0) {
    const dlResults = await batchFtp(xboxIp, [{ op: "download_base64", path: remoteBin }]);
    const buf = bufFromBatchResult(dlResults, 0);
    if (buf && buf.length > 0) fs.writeFileSync(binPath, buf);
  }

  let entries: any[] = [];
  if (fs.existsSync(binPath)) {
    try {
      const parsed = JSON.parse(fs.readFileSync(binPath, "utf8"));
      if (Array.isArray(parsed)) entries = parsed;
    } catch { entries = []; }
  }

  // Media fallback: try downloading cover from Aurora Media directory (all extensions in one batch)
  const tryMediaFallback = async (): Promise<boolean> => {
    const exts      = ["jpg", "jpeg", "png", "dds"];
    const mediaOps  = exts.map((x) => ({ op: "download_base64", path: `${mediaDir}/${titleId}GC.${x}` }));
    const mediaRes  = await batchFtp(xboxIp, mediaOps);
    for (let i = 0; i < mediaRes.length; i++) {
      const buf = bufFromBatchResult(mediaRes, i);
      if (!buf || buf.length < 100) continue;
      const ext         = imageExtFromMagic(buf);
      const primaryName = `cover-primary${ext}`;
      fs.writeFileSync(path.join(gdir, primaryName), buf);
      fs.writeFileSync(
        path.join(gdir, "cover-files.json"),
        JSON.stringify({
          primaryFile: primaryName,
          bestUrl:     `aurora:MediaGC.${exts[i]}`,
          gameCoverInfoEntryCount: entries.length,
        }, null, 2),
        "utf8"
      );
      return true;
    }
    return false;
  };

  const withUrl = entries.filter((e) => e && (e.front || e.thumbnail || e.url));
  if (withUrl.length === 0) {
    if (await tryMediaFallback()) emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
    return;
  }

  const best = withUrl.reduce((prev: any, curr: any) => {
    if (curr.official && !prev.official) return curr;
    if (!curr.official && prev.official) return prev;
    return (curr.rating || 0) >= (prev.rating || 0) ? curr : prev;
  });

  const bestUrl = best.front || best.thumbnail || best.url;
  if (!bestUrl || typeof bestUrl !== "string") {
    if (await tryMediaFallback()) emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
    return;
  }

  let prevMeta: any = {};
  try {
    prevMeta = JSON.parse(fs.readFileSync(path.join(gdir, "cover-files.json"), "utf8"));
  } catch { /* none */ }
  if (
    !force &&
    prevMeta.bestUrl === bestUrl &&
    prevMeta.primaryFile &&
    fs.existsSync(path.join(gdir, prevMeta.primaryFile))
  ) {
    emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
    return;
  }

  const buf = await fetchHttpImage(bestUrl);
  if (!buf || buf.length < 100) {
    if (await tryMediaFallback()) emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
    return;
  }

  const ext         = imageExtFromMagic(buf);
  const primaryName = `cover-primary${ext}`;
  fs.writeFileSync(path.join(gdir, primaryName), buf);
  fs.writeFileSync(
    path.join(gdir, "cover-files.json"),
    JSON.stringify({
      primaryFile: primaryName,
      bestUrl,
      gameCoverInfoEntryCount: entries.length,
    }, null, 2),
    "utf8"
  );

  emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
}
