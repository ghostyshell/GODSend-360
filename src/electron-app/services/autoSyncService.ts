/**
 * Post-FTP automation helpers.
 *
 * autoUploadAuroraAssets — download Aurora assets (cover, bg, banner, icon)
 *   from Xbox Live CDN / XboxUnity and upload them to the console's Aurora
 *   Import folder after a successful game FTP transfer.
 *
 * doAuroraLibrarySync — re-download Aurora's content.db and settings.db from
 *   the console and update the local library cache, called after FTP transfers
 *   and game moves to keep the Xbox Library view in sync.
 */

import fs from "fs";
import { Writable, Readable } from "stream";
import * as ftp from "basic-ftp";
import { app } from "electron";

import {
  getConfiguredXboxIP,
  getConfiguredFtpUser,
  getConfiguredFtpPassword,
  getConfiguredFtpScriptsPath,
} from "./settingsService";
import { addOutputLine } from "./backendClient";
import { appendAppEvent } from "../infrastructure/serverLog";
import {
  setActiveAuroraCacheRoot,
  getAuroraLibraryCacheRoot,
  databasesDir,
  contentDbPath,
  settingsDbPath,
  writeMeta,
} from "../infrastructure/auroraLibraryCache";
import {
  xboxAuroraRoot,
  discoverAuroraRoot,
  setLastDiscoveredAuroraRoot,
} from "./auroraPathHelper";
import {
  readContentScanRowsFromBuffer,
  readScanRowsFromSettingsBuffer,
  probeScanPathDrives,
} from "./auroraLibraryService";
import { fetchHttpImage, imageExtFromMagic } from "../infrastructure/httpHelper";
import { ftpTryDownloadFile } from "./auroraVisualService";

/**
 * Download Aurora assets (cover, background, banner, icon) for a title from
 * Xbox Live CDN and XboxUnity, then upload them to the console's Aurora Import
 * folder via FTP.
 */
export async function autoUploadAuroraAssets(titleId: string, xboxIp: string): Promise<void> {
  if (!titleId || !/^[0-9A-F]{8}$/i.test(titleId) || !xboxIp) return;
  const tidUpper    = titleId.toUpperCase();
  const ftpUser     = getConfiguredFtpUser();
  const ftpPass     = getConfiguredFtpPassword();
  const scriptsPath = getConfiguredFtpScriptsPath();
  const auroraRoot  = xboxAuroraRoot(scriptsPath);
  const importDir   = `${auroraRoot}/User/Import/${tidUpper}`;

  addOutputLine(`[INFO] Auto-assets: fetching Aurora assets for ${tidUpper}…`);

  // ── Collect typed images from Xbox CDN catalog ────────────────────────────
  const catalogUrl =
    `http://catalog-cdn.xboxlive.com/Catalog/Catalog.asmx/Query` +
    `?methodName=FindGames&Names=Locale&Values=en-US&Names=LegalLocale&Values=en-US` +
    `&Names=Store&Values=1&Names=PageSize&Values=100&Names=PageNum&Values=1` +
    `&Names=DetailView&Values=5&Names=OfferFilterLevel&Values=1` +
    `&Names=MediaIds&Values=66acd000-77fe-1000-9115-d802${tidUpper}` +
    `&Names=UserTypes&Values=2&Names=MediaTypes&Values=1&Names=MediaTypes&Values=21` +
    `&Names=MediaTypes&Values=23&Names=MediaTypes&Values=37&Names=MediaTypes&Values=46`;

  const cdnImages: { background: string | null; banner: string | null; icon: string | null } = {
    background: null,
    banner:     null,
    icon:       null,
  };
  try {
    const xmlBuf = await fetchHttpImage(catalogUrl);
    if (xmlBuf && xmlBuf.length > 0) {
      const xml = xmlBuf.toString("utf8");
      for (const [, block] of xml.matchAll(/<live:image[^>]*>([\s\S]*?)<\/live:image>/gi)) {
        const urlM  = (block as string).match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
        const typeM = (block as string).match(/<live:relationshipType[^>]*>\s*(\d+)\s*<\/live:relationshipType>/i);
        if (!urlM) continue;
        const url  = urlM[1].trim();
        const type = typeM ? parseInt(typeM[1], 10) : -1;
        if ((type === 15 || type === 23) && !cdnImages.icon)      cdnImages.icon       = url;
        else if (type === 25             && !cdnImages.background) cdnImages.background = url;
        else if (type === 27             && !cdnImages.banner)     cdnImages.banner     = url;
      }
    }
  } catch { /* CDN catalog unavailable */ }

  // ── Cover: XboxUnity (preferred) → Xbox CDN fallback ─────────────────────
  let coverBuf: Buffer | null = null;
  try {
    const unityBuf = await fetchHttpImage(
      `http://xboxunity.net/api/Covers/${encodeURIComponent(tidUpper)}`
    );
    if (unityBuf && unityBuf.length > 0) {
      let items: any[] | null;
      try { items = JSON.parse(unityBuf.toString("utf8")); } catch { items = null; }
      if (Array.isArray(items)) {
        items.sort((a, b) => {
          if (!!b.official !== !!a.official) return a.official ? -1 : 1;
          return (b.rating || 0) - (a.rating || 0);
        });
        const first = items.find((r) => r.front || r.url);
        if (first) {
          const coverUrl: string = first.front || first.url;
          if (coverUrl && coverUrl.startsWith("http")) coverBuf = await fetchHttpImage(coverUrl);
        }
      }
    }
  } catch { /* XboxUnity unavailable */ }
  if (!coverBuf || coverBuf.length < 100) {
    try {
      coverBuf = await fetchHttpImage(
        `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${tidUpper}/en-US/1`
      );
    } catch { /* CDN cover unavailable */ }
  }

  // ── Build upload list ─────────────────────────────────────────────────────
  const uploads: { assetType: string; buf: Buffer }[] = [];
  if (coverBuf && coverBuf.length >= 100) uploads.push({ assetType: "cover", buf: coverBuf });
  for (const [slot, url] of Object.entries(cdnImages)) {
    if (!url) continue;
    try {
      const buf = await fetchHttpImage(url);
      if (buf && buf.length >= 100) uploads.push({ assetType: slot, buf });
    } catch { /* skip unreachable CDN asset */ }
  }

  if (uploads.length === 0) {
    addOutputLine(`[INFO] Auto-assets: no assets found for ${tidUpper}`);
    return;
  }

  // ── Upload via FTP ────────────────────────────────────────────────────────
  const client = new ftp.Client();
  client.ftp.verbose = false;
  (client.ftp as any).timeout = 30000;
  try {
    await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
    await client.ensureDir(importDir);
    for (const { assetType, buf } of uploads) {
      const ext        = imageExtFromMagic(buf);
      const remotePath = `${importDir}/${assetType}${ext}`;
      await client.uploadFrom(Readable.from([buf]), remotePath);
      addOutputLine(`[INFO] Auto-assets: uploaded ${assetType}${ext} for ${tidUpper}`);
    }
    appendAppEvent("AURORA_ASSET", `auto-uploaded ${uploads.length} asset(s) for ${tidUpper}`);
  } catch (err: any) {
    addOutputLine(`[WARN] Auto-assets: FTP upload error for ${tidUpper}: ${err.message || err}`);
  } finally {
    client.close();
  }
}

/**
 * Re-download Aurora's content.db and settings.db from the console and update
 * the local library cache.  Called automatically after FTP transfers and game
 * moves to keep the Xbox Library view in sync.
 */
export async function doAuroraLibrarySync(): Promise<void> {
  const xboxIp      = getConfiguredXboxIP();
  const ftpUser     = getConfiguredFtpUser();
  const ftpPass     = getConfiguredFtpPassword();
  const scriptsPath = getConfiguredFtpScriptsPath();
  if (!xboxIp) return;

  let auroraRoot = xboxAuroraRoot(scriptsPath);
  let dbDir      = `${auroraRoot}/Data/Databases`;
  let cacheRoot  = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);

  addOutputLine(`[INFO] Auto-sync: refreshing Aurora library cache…`);

  const client = new ftp.Client();
  client.ftp.verbose = false;
  (client.ftp as any).timeout = 30000;
  try {
    await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

    // Auto-discover Aurora root if databases not found at the configured path.
    let contentSz  = -1;
    let settingsSz = -1;
    try { contentSz  = await client.size(`${dbDir}/content.db`);  } catch {}
    try { settingsSz = await client.size(`${dbDir}/settings.db`); } catch {}
    if (contentSz < 0 || settingsSz < 0) {
      const discovered = await discoverAuroraRoot(client);
      if (discovered) {
        setLastDiscoveredAuroraRoot(discovered);
        auroraRoot = discovered;
        dbDir      = `${auroraRoot}/Data/Databases`;
        cacheRoot  = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);
        setActiveAuroraCacheRoot(cacheRoot);
        try { contentSz  = await client.size(`${dbDir}/content.db`);  } catch {}
        try { settingsSz = await client.size(`${dbDir}/settings.db`); } catch {}
      }
    }
    if (contentSz < 0 || settingsSz < 0) {
      addOutputLine(`[WARN] Auto-sync: Aurora DBs unreachable — skipping library sync`);
      return;
    }

    fs.mkdirSync(databasesDir(cacheRoot), { recursive: true });
    const contentChunks: Buffer[] = [];
    await client.downloadTo(
      new Writable({ write(c: Buffer, _: BufferEncoding, cb: () => void) { contentChunks.push(c); cb(); } }),
      `${dbDir}/content.db`
    );
    const settingsChunks: Buffer[] = [];
    await client.downloadTo(
      new Writable({ write(c: Buffer, _: BufferEncoding, cb: () => void) { settingsChunks.push(c); cb(); } }),
      `${dbDir}/settings.db`
    );
    const contentBuf  = Buffer.concat(contentChunks);
    const settingsBuf = Buffer.concat(settingsChunks);

    fs.writeFileSync(contentDbPath(cacheRoot), contentBuf);
    fs.writeFileSync(settingsDbPath(cacheRoot), settingsBuf);

    const contentRows  = await readContentScanRowsFromBuffer(contentBuf);
    const scanRows     = await readScanRowsFromSettingsBuffer(settingsBuf);
    const scanDriveMap = await probeScanPathDrives(client, scanRows, contentRows);

    writeMeta(cacheRoot, {
      xboxIp,
      auroraRoot,
      ftpScriptsPath:    scriptsPath,
      contentDbSize:     contentSz,
      settingsDbSize:    settingsSz,
      scanDriveMap:      Object.fromEntries([...scanDriveMap.entries()].map(([k, v]) => [String(k), v])),
      driveProbeVersion: 2,
      updatedAt:         Date.now(),
    });
    setActiveAuroraCacheRoot(cacheRoot);
    addOutputLine(`[INFO] Auto-sync: Aurora library cache updated.`);
  } catch (err: any) {
    addOutputLine(`[WARN] Auto-sync: library sync error: ${err.message || err}`);
  } finally {
    client.close();
  }
}
