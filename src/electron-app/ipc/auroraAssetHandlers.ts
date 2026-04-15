/**
 * IPC handlers for Aurora asset operations:
 *   xbox:search-assets
 *   xbox:fetch-url-image
 *   xbox:choose-image-file
 *   xbox:upload-asset-to-console
 *   xbox:decode-asset
 *   xbox:encode-asset
 *   xbox:inspect-aurora-game
 *
 * All FTP operations are proxied through the Go backend for centralised tracking.
 */

import { app, BrowserWindow, dialog, IpcMain } from "electron";
import http from "http";
import path from "path";
import fs from "fs";

import {
  getConfiguredXboxIP,
  getConfiguredFtpScriptsPath,
  getConfiguredServerPort,
} from "../services/settingsService";
import { addOutputLine } from "../services/backendClient";
import { appendAppEvent } from "../infrastructure/serverLog";
import {
  getAuroraLibraryCacheRoot,
  readMeta,
  writeMeta,
} from "../infrastructure/auroraLibraryCache";
import { xboxAuroraRoot, xboxAuroraMediaDir } from "../services/auroraPathHelper";
import { fetchHttpImage } from "../infrastructure/httpHelper";
import {
  classifyAuroraFileKind,
  summarizeGameCoverInfoJson,
} from "../services/auroraVisualService";
import {
  searchXboxUnityCovers,
  fetchXboxCdnAssets,
  resolveTitleIdHex,
} from "../services/coverArtService";
import { getMainWindow } from "../app/window";
import { backendPost } from "../infrastructure/backendHttp";

// ── FTP batch helper ──────────────────────────────────────────────────────────

async function batchFtp(xboxIp: string, ops: any[]): Promise<any[]> {
  const res = await backendPost("/ftp/batch", { ip: xboxIp, ops }, 120000);
  return res.results || [];
}

function bufFromBatchResult(results: any[], idx: number): Buffer | null {
  if (idx < 0 || !results[idx] || !results[idx].ok || !results[idx].data) return null;
  return Buffer.from(results[idx].data, "base64");
}

export function register(ipcMain: IpcMain): void {

  // ── Search assets (XboxUnity covers + Xbox CDN catalog) ────────────────────
  ipcMain.handle("xbox:search-assets", async (_event, payload) => {
    const { query, titleId, assetType: rawAssetType } = payload || {};
    const assetType  = (typeof rawAssetType === "string" && rawAssetType.trim())
      ? rawAssetType.trim().toLowerCase().replace(/\d+$/, "")
      : "cover";
    const searchTerm = (titleId && /^[0-9A-F]{8}$/i.test(String(titleId).trim()))
      ? String(titleId).trim().toUpperCase()
      : (typeof query === "string" ? query.trim() : "");
    if (!searchTerm) return { ok: true, results: [] };

    // ── Cover search: XboxUnity + CDN high-res cover ─────────────────────────
    if (assetType === "cover") {
      let results = await searchXboxUnityCovers(searchTerm);
      if (results.length === 0 && titleId && typeof query === "string" && query.trim() && searchTerm !== query.trim()) {
        results = await searchXboxUnityCovers(query.trim());
      }
      if (results.length > 0 && results[0].titleId && /^[0-9A-F]{8}$/.test(results[0].titleId)) {
        const cdnUrl = `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${results[0].titleId}/en-US/1`;
        const cdnBuf = await fetchHttpImage(cdnUrl);
        if (cdnBuf && cdnBuf.length >= 100) {
          const mime       = (cdnBuf[0] === 0xFF && cdnBuf[1] === 0xD8) ? "image/jpeg" : "image/png";
          const cdnDataUrl = `data:${mime};base64,${cdnBuf.toString("base64")}`;
          results.unshift({
            titleId:   results[0].titleId,
            front:     cdnDataUrl,
            thumbnail: cdnDataUrl,
            url:       cdnUrl,
            official:  true,
            rating:    null,
            source:    "xbox-cdn",
          });
        }
      }
      return { ok: true, results };
    }

    // ── Non-cover: resolve titleId then query Xbox CDN catalog ────────────────
    const tidHex = await resolveTitleIdHex(searchTerm);
    if (!tidHex) return { ok: true, results: [] };

    const cdnAssets = await fetchXboxCdnAssets(tidHex);
    const typeKey   = assetType === "background" ? "background"
                    : assetType === "banner"      ? "banner"
                    : assetType === "icon"         ? "icon"
                    : assetType === "screenshot"   ? "screenshot"
                    : "cover";

    const urls = cdnAssets[typeKey as keyof typeof cdnAssets] || [];
    if (urls.length === 0) return { ok: true, results: [] };

    const results: any[] = [];
    for (const u of urls) {
      if (u.startsWith("data:")) {
        results.push({ titleId: tidHex, front: u, thumbnail: u, url: null, official: true, rating: null, source: "xbox-cdn", assetType: typeKey });
        continue;
      }
      const buf = await fetchHttpImage(u);
      if (buf && buf.length >= 100) {
        const mime    = (buf[0] === 0xFF && buf[1] === 0xD8) ? "image/jpeg" : "image/png";
        const dataUrl = `data:${mime};base64,${buf.toString("base64")}`;
        results.push({ titleId: tidHex, front: dataUrl, thumbnail: dataUrl, url: u, official: true, rating: null, source: "xbox-cdn", assetType: typeKey });
      }
    }
    return { ok: true, results };
  });

  // ── Fetch a remote image URL → base64 data URL ─────────────────────────────
  ipcMain.handle("xbox:fetch-url-image", async (_event, url: string) => {
    if (typeof url !== "string" || !url.startsWith("http")) {
      return { ok: false, error: "Invalid URL." };
    }
    const buf = await fetchHttpImage(url);
    if (!buf || buf.length < 100) return { ok: false, error: "Could not fetch image." };
    const mime =
      (buf[0] === 0xFF && buf[1] === 0xD8) ? "image/jpeg" :
      (buf[0] === 0x89 && buf[1] === 0x50) ? "image/png"  : "image/jpeg";
    return { ok: true, dataUrl: `data:${mime};base64,${buf.toString("base64")}` };
  });

  // ── Native file picker → image data URL ────────────────────────────────────
  ipcMain.handle("xbox:choose-image-file", async () => {
    const win = BrowserWindow.getFocusedWindow() || getMainWindow();
    const r   = await dialog.showOpenDialog(win || undefined, {
      title:   "Choose image",
      filters: [{ name: "Images", extensions: ["jpg", "jpeg", "png", "bmp", "gif"] }],
      properties: ["openFile"],
    });
    if (r.canceled || !r.filePaths[0]) return { ok: false };
    const filePath = r.filePaths[0];
    const buf      = fs.readFileSync(filePath);
    const ext      = path.extname(filePath).toLowerCase();
    const mime     = (ext === ".png") ? "image/png" : (ext === ".gif") ? "image/gif" : "image/jpeg";
    return { ok: true, dataUrl: `data:${mime};base64,${buf.toString("base64")}`, ext, filePath };
  });

  // ── Upload an asset image to the console (User/Import/{TitleId}/) ──────────
  // ── Asset type → RXEA slot number and .asset file prefix ──────────────────
  const ASSET_SLOT_MAP: Record<string, { slot: number; prefix: string }> = {
    icon:       { slot: 0, prefix: "GL" },
    banner:     { slot: 1, prefix: "GL" },
    cover:      { slot: 2, prefix: "GC" },
    background: { slot: 4, prefix: "BK" },
  };
  for (let i = 1; i <= 10; i++) {
    ASSET_SLOT_MAP[`screenshot${i}`] = { slot: 4 + i, prefix: "SS" };
  }

  ipcMain.handle("xbox:upload-asset-to-console", async (_event, payload) => {
    const p           = payload || {};
    const titleId     = typeof p.titleId     === "string" ? p.titleId.trim().toUpperCase()  : "";
    const assetType   = typeof p.assetType   === "string" ? p.assetType.trim().toLowerCase() : "";
    const gameDataDir = typeof p.gameDataDir === "string" ? p.gameDataDir.trim()             : "";
    if (!titleId || !/^[0-9A-F]{8}$/.test(titleId)) {
      return { ok: false, error: "Invalid or missing titleId." };
    }
    if (!gameDataDir) {
      return { ok: false, error: "gameDataDir is required for asset upload." };
    }
    const slotInfo = ASSET_SLOT_MAP[assetType];
    if (!slotInfo) {
      return { ok: false, error: `Unknown assetType: ${assetType}` };
    }

    const xboxIp = getConfiguredXboxIP();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    let imgBuf: Buffer | null = null;
    if (typeof p.imageBase64 === "string" && p.imageBase64.length > 0) {
      imgBuf = Buffer.from(p.imageBase64, "base64");
    } else if (typeof p.imageUrl === "string" && p.imageUrl.startsWith("http")) {
      imgBuf = await fetchHttpImage(p.imageUrl);
    }
    if (!imgBuf || imgBuf.length < 100) return { ok: false, error: "No valid image data provided." };

    // Encode the image into an RXEA .asset via the Go backend.
    const port = getConfiguredServerPort();
    const finalImgBuf = imgBuf;
    const rxeaBuf: Buffer | null = await new Promise((resolve) => {
      const req = http.request(
        { host: "127.0.0.1", port, path: `/rxea/encode?slot=${slotInfo.slot}`, method: "POST",
          headers: { "Content-Type": "application/octet-stream", "Content-Length": finalImgBuf.length } },
        (res) => {
          const chunks: Buffer[] = [];
          res.on("data", (c: Buffer) => chunks.push(c));
          res.on("end",  () => resolve(res.statusCode === 200 ? Buffer.concat(chunks) : null));
        }
      );
      req.on("error", () => resolve(null));
      req.end(finalImgBuf);
    });

    if (!rxeaBuf || rxeaBuf.length < 2048) {
      return { ok: false, error: "RXEA encoding failed — backend returned no data." };
    }

    const assetName  = `${slotInfo.prefix}${titleId}.asset`;
    const scriptsPath = getConfiguredFtpScriptsPath();
    const auroraRoot  = xboxAuroraRoot(scriptsPath);
    const remoteDir   = `${auroraRoot}/Data/GameData/${gameDataDir}`;
    const remotePath  = `${remoteDir}/${assetName}`;

    try {
      const results = await batchFtp(xboxIp, [
        { op: "ensure_dir", path: remoteDir },
        { op: "upload_base64", path: remotePath, data: rxeaBuf.toString("base64") },
      ]);
      if (results[1] && !results[1].ok) throw new Error(results[1].error || "Upload failed");

      appendAppEvent("AURORA_ASSET", `encoded+uploaded ${assetType} (slot ${slotInfo.slot}) → ${remotePath} (${rxeaBuf.length} B)`);

      // Invalidate local visual cache so the next refresh re-downloads the new asset.
      const cacheRoot = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);
      const metaObj   = readMeta(cacheRoot);
      if (metaObj) writeMeta(cacheRoot, { ...metaObj, updatedAt: Date.now() });

      // Delete the cached visual file for this asset so the next sync re-decodes it.
      try {
        const { gameCacheDir: getCacheDir } = await import("../infrastructure/auroraLibraryCache");
        const gdir = getCacheDir(cacheRoot, gameDataDir);
        const visualDir = path.join(gdir, "visual");
        if (fs.existsSync(visualDir)) {
          for (const name of fs.readdirSync(visualDir)) {
            const lower = name.toLowerCase();
            if (
              (assetType === "cover"      && (lower.includes("cover") || lower.includes("gc-"))) ||
              (assetType === "background" && (lower.includes("background") || lower.includes("bk-"))) ||
              (assetType === "banner"     && (lower.includes("banner") || lower.includes("gl-banner"))) ||
              (assetType === "icon"       && (lower.includes("icon") || lower.includes("gl-icon"))) ||
              (assetType.startsWith("screenshot") && lower.includes(assetType))
            ) {
              try { fs.unlinkSync(path.join(visualDir, name)); } catch { /* ignore */ }
            }
          }
          const manifestPath = path.join(gdir, "visual-manifest.json");
          try { fs.unlinkSync(manifestPath); } catch { /* ignore */ }
        }
      } catch { /* cache cleanup is best-effort */ }

      return { ok: true, remotePath };
    } catch (err: any) {
      const msg = err.message || String(err);
      appendAppEvent("AURORA_ASSET", `upload error ${titleId}/${assetType}: ${msg}`);
      return { ok: false, error: msg };
    }
  });

  // ── Decode .asset files from the console (RXEA → PNG) ─────────────────────
  ipcMain.handle("xbox:decode-asset", async (_event, payload) => {
    const p           = payload || {};
    const titleId     = typeof p.titleId     === "string" ? p.titleId.trim().toUpperCase() : "";
    const gameDataDir = typeof p.gameDataDir === "string" ? p.gameDataDir.trim() : "";
    if (!titleId || !gameDataDir) return { ok: false, error: "titleId and gameDataDir required." };

    const xboxIp = getConfiguredXboxIP();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const scriptsPath  = getConfiguredFtpScriptsPath();
    const auroraRoot   = xboxAuroraRoot(scriptsPath);
    const gameDataPath = `${auroraRoot}/Data/GameData/${gameDataDir}`;
    const port         = getConfiguredServerPort();

    const assetFiles = [
      { name: `BK${titleId}.asset`, slotKeys: { 4: "background" } as Record<number, string> },
      { name: `GC${titleId}.asset`, slotKeys: { 2: "cover"      } as Record<number, string> },
      { name: `GL${titleId}.asset`, slotKeys: { 0: "icon", 1: "banner" } as Record<number, string> },
      { name: `SS${titleId}.asset`, slotKeys: null },
    ];

    const allSlots: any[] = [];

    try {
      addOutputLine(`[INFO] RXEA decode ${titleId}: downloading asset files from ${xboxIp}…`);

      // Download all .asset files in one batch
      const dlOps = assetFiles.map((af) => ({
        op: "download_base64", path: `${gameDataPath}/${af.name}`,
      }));
      const dlResults = await batchFtp(xboxIp, dlOps);

      for (let fi = 0; fi < assetFiles.length; fi++) {
        const { name, slotKeys } = assetFiles[fi];
        const assetBuf = bufFromBatchResult(dlResults, fi);
        if (!assetBuf || assetBuf.length < 2048) {
          addOutputLine(`[INFO] RXEA decode ${titleId}: ${name} not found or too small, skipping.`);
          continue;
        }

        addOutputLine(`[INFO] RXEA decode ${titleId}: decoding ${name} (${assetBuf.length} bytes)…`);

        const decoded: any = await new Promise((resolve) => {
          const req = http.request(
            { host: "127.0.0.1", port, path: "/rxea/decode", method: "POST",
              headers: { "Content-Type": "application/octet-stream", "Content-Length": assetBuf.length } },
            (res) => {
              const chunks: Buffer[] = [];
              res.on("data", (c: Buffer) => chunks.push(c));
              res.on("end", () => {
                try { resolve(JSON.parse(Buffer.concat(chunks).toString("utf8"))); }
                catch (e: any) {
                  addOutputLine(`[WARN] RXEA decode ${titleId}: JSON parse error for ${name}: ${e.message}`);
                  resolve(null);
                }
              });
            }
          );
          req.on("error", (e: Error) => {
            addOutputLine(`[WARN] RXEA decode ${titleId}: Go server error for ${name}: ${e.message}`);
            resolve(null);
          });
          req.end(assetBuf);
        });

        if (!decoded || !Array.isArray(decoded.slots)) {
          const goErr = decoded?.error ? ` — ${decoded.error}` : "";
          addOutputLine(`[WARN] RXEA decode ${titleId}: Go server returned no slots for ${name}${goErr}.`);
          if (Array.isArray(decoded?.diags) && decoded.diags.length > 0) {
            for (const d of decoded.diags) {
              addOutputLine(
                `[DIAG] slot${d.slot}: off=${d.offset} sz=${d.size} fmt=${d.gpu_fmt} ` +
                `w=${d.width} h=${d.height} tiled=${d.tiled} endian=${d.endian}` +
                `${d.error ? ` err="${d.error}"` : ""}`
              );
            }
          }
          continue;
        }

        if (Array.isArray(decoded.diags)) {
          for (const d of decoded.diags.filter((x: any) => x.error)) {
            addOutputLine(`[DIAG] slot${d.slot} error: fmt=${d.gpu_fmt} w=${d.width} h=${d.height} — ${d.error}`);
          }
        }

        addOutputLine(`[INFO] RXEA decode ${titleId}: ${name} → ${decoded.slots.length} slot(s).`);

        for (const s of decoded.slots) {
          let key: string | null = null;
          if (slotKeys && slotKeys[s.slot] !== undefined) {
            key = slotKeys[s.slot];
          } else if (s.slot >= 5 && s.slot <= 24) {
            key = `screenshot${s.slot - 4}`;
          }
          if (!key) continue;
          const pngBuf = Buffer.isBuffer(s.png) ? s.png : Buffer.from(s.png, "base64");
          allSlots.push({
            slot:    s.slot,
            key,
            width:   s.width,
            height:  s.height,
            dataUrl: `data:image/png;base64,${pngBuf.toString("base64")}`,
          });
        }
      }

      addOutputLine(`[INFO] RXEA decode ${titleId}: done — ${allSlots.length} total slot(s) decoded.`);
      return { ok: true, slots: allSlots };
    } catch (err: any) {
      const msg = err.message || String(err);
      addOutputLine(`[WARN] RXEA decode ${titleId}: ${msg}`);
      return { ok: false, error: msg };
    }
  });

  // ── Encode a PNG and upload it as an RXEA .asset to the console ────────────
  ipcMain.handle("xbox:encode-asset", async (_event, payload) => {
    const p           = payload || {};
    const titleId     = typeof p.titleId     === "string" ? p.titleId.trim().toUpperCase() : "";
    const gameDataDir = typeof p.gameDataDir === "string" ? p.gameDataDir.trim() : "";
    const slotNum     = typeof p.slot === "number" ? p.slot : parseInt(p.slot, 10);
    if (!titleId || !gameDataDir || isNaN(slotNum) || slotNum < 0 || slotNum > 24) {
      return { ok: false, error: "titleId, gameDataDir, and slot (0–24) required." };
    }

    const xboxIp = getConfiguredXboxIP();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    let imgBuf: Buffer | null = null;
    if (typeof p.imageBase64 === "string" && p.imageBase64.length > 0) {
      imgBuf = Buffer.from(p.imageBase64, "base64");
    } else if (typeof p.imageUrl === "string" && p.imageUrl.startsWith("http")) {
      imgBuf = await fetchHttpImage(p.imageUrl);
    }
    if (!imgBuf || imgBuf.length < 100) return { ok: false, error: "No valid image data." };

    const port = getConfiguredServerPort();
    const finalImgBuf = imgBuf;
    const rxeaBuf: Buffer | null = await new Promise((resolve) => {
      const req = http.request(
        { host: "127.0.0.1", port, path: `/rxea/encode?slot=${slotNum}`, method: "POST",
          headers: { "Content-Type": "image/png", "Content-Length": finalImgBuf.length } },
        (res) => {
          const chunks: Buffer[] = [];
          res.on("data", (c: Buffer) => chunks.push(c));
          res.on("end",  () => resolve(res.statusCode === 200 ? Buffer.concat(chunks) : null));
        }
      );
      req.on("error", () => resolve(null));
      req.end(finalImgBuf);
    });

    if (!rxeaBuf || rxeaBuf.length < 2048) {
      return { ok: false, error: "RXEA encoding failed — Go server returned no data." };
    }

    const prefixMap: Record<number, string> = { 0: "GL", 1: "GL", 2: "GC", 3: "GC", 4: "BK" };
    const prefix     = slotNum >= 5 ? "SS" : (prefixMap[slotNum] || "GC");
    const assetName  = `${prefix}${titleId}.asset`;

    const scriptsPath = getConfiguredFtpScriptsPath();
    const auroraRoot  = xboxAuroraRoot(scriptsPath);
    const remotePath  = `${auroraRoot}/Data/GameData/${gameDataDir}/${assetName}`;

    try {
      // Upload via Go backend batch
      const results = await batchFtp(xboxIp, [
        { op: "upload_base64", path: remotePath, data: rxeaBuf.toString("base64") },
      ]);
      if (results[0] && !results[0].ok) throw new Error(results[0].error || "Upload failed");

      appendAppEvent("AURORA_ASSET", `encoded+uploaded slot${slotNum} → ${remotePath} (${rxeaBuf.length} B)`);
      return { ok: true, remotePath, rxeaSize: rxeaBuf.length };
    } catch (err: any) {
      const msg = err.message || String(err);
      appendAppEvent("AURORA_ASSET", `encode-upload error: ${msg}`);
      return { ok: false, error: msg };
    }
  });

  // ── Aurora inspector: GameData + Media inventory, GameCoverInfo summary ─────
  ipcMain.handle("xbox:inspect-aurora-game", async (_event, payload) => {
    const p           = payload || {};
    const titleId     = typeof p.titleId     === "string" ? p.titleId.trim().toUpperCase() : "";
    const gameDataDir = typeof p.gameDataDir === "string" ? p.gameDataDir.trim() : "";
    if (!titleId || !gameDataDir) return { ok: false, error: "titleId and gameDataDir are required." };

    const xboxIp = getConfiguredXboxIP();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const scriptsPath  = getConfiguredFtpScriptsPath();
    const auroraRoot   = xboxAuroraRoot(scriptsPath);
    const mediaDir     = xboxAuroraMediaDir(scriptsPath);
    const gameDataPath = `${auroraRoot}/Data/GameData/${gameDataDir}`;

    try {
      // List GameData dir, list Media dir (filtered), download GameCoverInfo.bin — all in one batch
      const results = await batchFtp(xboxIp, [
        { op: "list", path: gameDataPath },
        { op: "list", path: mediaDir },
        { op: "download_base64", path: `${gameDataPath}/GameCoverInfo.bin` },
      ]);

      let gameDataFiles: any[] = [];
      if (results[0] && results[0].ok && Array.isArray(results[0].data)) {
        gameDataFiles = (results[0].data as any[])
          .filter((e) => e && e.name && e.name !== "." && e.name !== "..")
          .map((e) => ({
            name:  e.name,
            size:  e.size != null ? Number(e.size) : null,
            isDir: e.type === "dir",
            kind:  classifyAuroraFileKind(e.name),
          }))
          .sort((a, b) => a.name.localeCompare(b.name));
      }

      let mediaFiles: any[] = [];
      if (results[1] && results[1].ok && Array.isArray(results[1].data)) {
        mediaFiles = (results[1].data as any[])
          .filter((e) => e && e.name && e.name.startsWith(titleId))
          .map((e) => ({
            name:    e.name,
            size:    e.size != null ? Number(e.size) : null,
            ftpPath: `${mediaDir}/${e.name}`,
            kind:    classifyAuroraFileKind(e.name),
          }))
          .sort((a, b) => a.name.localeCompare(b.name));
      }

      let gameCoverInfoText: string | null = null;
      const gciBuf = bufFromBatchResult(results, 2);
      if (gciBuf && gciBuf.length > 0) {
        gameCoverInfoText = gciBuf.toString("utf8");
      }

      return {
        ok: true,
        auroraRoot,
        gameDataPath,
        mediaDir,
        gameDataFiles,
        mediaFiles,
        gameCoverInfo: summarizeGameCoverInfoJson(gameCoverInfoText),
      };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });
}
