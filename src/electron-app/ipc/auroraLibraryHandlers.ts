/**
 * IPC handlers for Aurora library operations:
 *   xbox:list-aurora-library
 *   xbox:fetch-aurora-covers
 *   xbox:refresh-title-visuals-cache
 */

import { app, IpcMain } from "electron";
import fs from "fs";
import * as ftp from "basic-ftp";
import { Writable } from "stream";

import {
  getConfiguredXboxIP,
  getConfiguredFtpUser,
  getConfiguredFtpPassword,
  getConfiguredFtpScriptsPath,
} from "../services/settingsService";
import { addOutputLine } from "../services/backendClient";
import { appendAppEvent } from "../infrastructure/serverLog";
import {
  setActiveAuroraCacheRoot,
  getAuroraLibraryCacheRoot,
  databasesDir,
  readMeta,
  writeMeta,
  contentDbPath,
  settingsDbPath,
} from "../infrastructure/auroraLibraryCache";
import {
  xboxAuroraRoot,
  xboxAuroraMediaDir,
  discoverAuroraRoot,
  setLastDiscoveredAuroraRoot,
} from "../services/auroraPathHelper";
import {
  buildAuroraGamesFromDbBuffers,
  readContentScanRowsFromBuffer,
  readScanRowsFromSettingsBuffer,
  probeScanPathDrives,
} from "../services/auroraLibraryService";
import {
  syncAuroraGameCoverAssets,
  syncAuroraTitleVisualAssets,
  emitAuroraCoverEvents,
  emitAuroraTitleVisualEvents,
} from "../services/auroraVisualService";

export function register(ipcMain: IpcMain): void {

  // ── List Aurora library (DB fingerprint sync + game list) ──────────────────
  ipcMain.handle("xbox:list-aurora-library", async (_event, opts) => {
    const force       = opts && opts.force === true;
    const xboxIp      = getConfiguredXboxIP();
    const ftpUser     = getConfiguredFtpUser();
    const ftpPass     = getConfiguredFtpPassword();
    const scriptsPath = getConfiguredFtpScriptsPath();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured. Set it in Settings." };

    let auroraRoot = xboxAuroraRoot(scriptsPath);
    let dbDir      = `${auroraRoot}/Data/Databases`;
    let cacheRoot  = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);
    setActiveAuroraCacheRoot(cacheRoot);

    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 30000;

    try {
      addOutputLine(`[INFO] Aurora library: ${force ? "refresh (forced)" : "loading"} — FTP ${xboxIp}…`);
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

      let contentSz  = -1;
      let settingsSz = -1;
      try { contentSz  = await client.size(`${dbDir}/content.db`);  } catch { /* missing */ }
      try { settingsSz = await client.size(`${dbDir}/settings.db`); } catch { /* missing */ }

      if (contentSz < 0 || settingsSz < 0) {
        addOutputLine(
          `[INFO] Aurora library: ${auroraRoot}/Data/Databases not found — auto-discovering Aurora install…`
        );
        const discovered = await discoverAuroraRoot(client);
        if (discovered) {
          setLastDiscoveredAuroraRoot(discovered);
          auroraRoot = discovered;
          dbDir      = `${auroraRoot}/Data/Databases`;
          cacheRoot  = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);
          setActiveAuroraCacheRoot(cacheRoot);
          addOutputLine(`[INFO] Aurora library: discovered Aurora at ${auroraRoot}`);
          try { contentSz  = await client.size(`${dbDir}/content.db`);  } catch {}
          try { settingsSz = await client.size(`${dbDir}/settings.db`); } catch {}
        } else {
          addOutputLine(`[ERROR] Aurora library: could not find an Aurora install on the console.`);
        }
      }

      const meta             = readMeta(cacheRoot);
      const fingerprintMatch =
        !force &&
        meta &&
        meta.xboxIp           === xboxIp      &&
        meta.auroraRoot       === auroraRoot   &&
        meta.ftpScriptsPath   === scriptsPath  &&
        meta.contentDbSize    === contentSz    &&
        meta.settingsDbSize   === settingsSz   &&
        contentSz  >= 0 &&
        settingsSz >= 0 &&
        meta.scanDriveMap &&
        meta.driveProbeVersion === 2 &&
        fs.existsSync(contentDbPath(cacheRoot)) &&
        fs.existsSync(settingsDbPath(cacheRoot));

      if (fingerprintMatch) {
        const contentBuf   = fs.readFileSync(contentDbPath(cacheRoot));
        const settingsBuf  = fs.readFileSync(settingsDbPath(cacheRoot));
        const scanDriveMap = new Map<number, string>(
          Object.entries(meta.scanDriveMap).map(([k, v]) => [Number(k), String(v)])
        );
        const games = await buildAuroraGamesFromDbBuffers(contentBuf, settingsBuf, scanDriveMap);
        addOutputLine(
          `[INFO] Aurora library: using local DB cache (${games.length} games, console DB unchanged).`
        );
        return { ok: true, games, connectedTo: xboxIp, auroraRoot, libraryUnchanged: true, fromCache: true };
      }

      addOutputLine("[INFO] Aurora library: downloading content.db and settings.db…");
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

      const scanRows     = await readScanRowsFromSettingsBuffer(settingsBuf);
      const contentRows  = await readContentScanRowsFromBuffer(contentBuf);
      addOutputLine(`[INFO] Aurora library: probing ${scanRows.length} scan path(s) for drive letters…`);

      const prevFtpTimeout = (client.ftp as any).timeout;
      (client.ftp as any).timeout   = 8000;
      let scanDriveMap: Map<number, string>;
      try {
        scanDriveMap = await probeScanPathDrives(client, scanRows, contentRows);
      } finally {
        (client.ftp as any).timeout = prevFtpTimeout;
      }

      writeMeta(cacheRoot, {
        xboxIp,
        auroraRoot,
        ftpScriptsPath:    scriptsPath,
        contentDbSize:     contentSz,
        settingsDbSize:    settingsSz,
        scanDriveMap:      Object.fromEntries(scanDriveMap),
        driveProbeVersion: 2,
        updatedAt:         Date.now(),
      });

      const games = await buildAuroraGamesFromDbBuffers(contentBuf, settingsBuf, scanDriveMap);
      addOutputLine(`[INFO] Aurora library: ready (${games.length} games, DB saved to app cache).`);
      return { ok: true, games, connectedTo: xboxIp, auroraRoot, libraryUnchanged: false, fromCache: false };
    } catch (err: any) {
      const msg = err.message || String(err);
      addOutputLine(`[ERROR] Aurora library: ${msg}`);
      appendAppEvent("AURORA_LIB", `error: ${msg}`);
      return { ok: false, error: msg };
    } finally {
      client.close();
    }
  });

  // ── Fetch Aurora covers + visual assets ─────────────────────────────────────
  ipcMain.handle("xbox:fetch-aurora-covers", async (_event, gameList, opts) => {
    if (!Array.isArray(gameList) || gameList.length === 0) return { ok: true };

    const force        = opts && opts.force        === true;
    const fromDiskOnly = opts && opts.fromDiskOnly  === true;

    const xboxIp      = getConfiguredXboxIP();
    const ftpUser     = getConfiguredFtpUser();
    const ftpPass     = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const scriptsPath = getConfiguredFtpScriptsPath();
    const auroraRoot  = xboxAuroraRoot(scriptsPath);
    const mediaDir    = xboxAuroraMediaDir(scriptsPath);
    const cacheRoot   = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);
    setActiveAuroraCacheRoot(cacheRoot);

    if (fromDiskOnly) {
      addOutputLine(
        `[INFO] Aurora covers + artwork: serving ${gameList.length} title(s) from disk cache (no FTP).`
      );
      for (const { titleId, gameDataDir } of gameList) {
        emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
        emitAuroraTitleVisualEvents(titleId, gameDataDir, cacheRoot);
      }
      return { ok: true };
    }

    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 20000;

    let lastProcessed = -1;
    try {
      addOutputLine(
        `[INFO] Aurora covers + artwork: FTP sync starting for ${gameList.length} title(s)…`
      );
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      addOutputLine(
        `[INFO] Aurora covers + artwork: syncing ${gameList.length} title(s) via CDN…`
      );

      const progressEvery = 25;
      for (let gi = 0; gi < gameList.length; gi += 1) {
        const { titleId, gameDataDir } = gameList[gi];
        try {
          await syncAuroraGameCoverAssets(client, auroraRoot, mediaDir, titleId, gameDataDir, cacheRoot, force);
          await syncAuroraTitleVisualAssets(client, auroraRoot, titleId, gameDataDir, cacheRoot, force);
        } catch (err: any) {
          const em = err?.message || String(err);
          addOutputLine(`[WARN] Aurora sync ${titleId}: ${em}`);
          appendAppEvent("AURORA_SYNC", `${titleId}: ${em}`);
        } finally {
          emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
          emitAuroraTitleVisualEvents(titleId, gameDataDir, cacheRoot);
        }
        lastProcessed = gi;
        if (progressEvery > 0 && (gi + 1) % progressEvery === 0) {
          addOutputLine(`[INFO] Aurora covers + artwork: progress ${gi + 1}/${gameList.length} titles…`);
        }
        if ((gi & 3) === 3) {
          await new Promise((resolve) => setImmediate(resolve));
        }
      }

      addOutputLine(`[INFO] Aurora covers + artwork: finished ${gameList.length} title(s).`);
      return { ok: true };
    } catch (err: any) {
      const msg = err.message || String(err);
      addOutputLine(`[ERROR] Aurora covers + artwork: ${msg}`);
      appendAppEvent("AURORA_SYNC", `fatal: ${msg}`);

      for (let gi = lastProcessed + 1; gi < gameList.length; gi += 1) {
        const { titleId, gameDataDir } = gameList[gi];
        emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
        emitAuroraTitleVisualEvents(titleId, gameDataDir, cacheRoot);
      }

      return { ok: false, error: msg };
    } finally {
      client.close();
    }
  });

  // ── Refresh title visuals from disk cache (no FTP) ─────────────────────────
  ipcMain.handle("xbox:refresh-title-visuals-cache", async (_event, payload) => {
    const p           = payload || {};
    const titleId     = typeof p.titleId     === "string" ? p.titleId.trim().toUpperCase() : "";
    const gameDataDir = typeof p.gameDataDir === "string" ? p.gameDataDir.trim() : "";
    if (!titleId || !gameDataDir) return { ok: false, error: "titleId and gameDataDir required." };

    const xboxIp = getConfiguredXboxIP();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const scriptsPath = getConfiguredFtpScriptsPath();
    const auroraRoot  = xboxAuroraRoot(scriptsPath);
    const cacheRoot   = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);
    setActiveAuroraCacheRoot(cacheRoot);
    emitAuroraTitleVisualEvents(titleId, gameDataDir, cacheRoot);
    return { ok: true };
  });
}
