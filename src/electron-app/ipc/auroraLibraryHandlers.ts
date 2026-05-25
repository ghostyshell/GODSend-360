/**
 * IPC handlers for Aurora library operations:
 *   xbox:list-aurora-library
 *   xbox:fetch-aurora-covers
 *   xbox:refresh-title-visuals-cache
 *
 * All FTP operations are proxied through the Go backend for centralised tracking.
 */

import { app, BrowserWindow, dialog, IpcMain } from "electron";
import fs from "fs";
import path from "path";

import {
  getConfiguredXboxIP,
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
import { backendPost } from "../infrastructure/backendHttp";

export function register(ipcMain: IpcMain): void {

  // ── List Aurora library (DB fingerprint sync + game list) ──────────────────
  ipcMain.handle("xbox:list-aurora-library", async (_event, opts) => {
    const force       = opts && opts.force === true;
    const xboxIp      = getConfiguredXboxIP();
    const scriptsPath = getConfiguredFtpScriptsPath();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured. Set it in Settings." };

    let auroraRoot = xboxAuroraRoot(scriptsPath);
    let dbDir      = `${auroraRoot}/Data/Databases`;
    let cacheRoot  = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);
    setActiveAuroraCacheRoot(cacheRoot);

    try {
      addOutputLine(`[INFO] Aurora library: ${force ? "refresh (forced)" : "loading"} — FTP ${xboxIp}…`);

      // Check DB sizes via Go backend batch (1 FTP connection).
      // For the background (unforced) poll, give up quickly if the FTP
      // lock is busy — a long-running upload shouldn't make the poll
      // hang for tens of seconds. Forced refresh waits as long as needed.
      const lockWaitMs = force ? 0 : 5000;
      let batchRes = await backendPost("/ftp/batch", { ip: xboxIp, ops: [
        { op: "size", path: `${dbDir}/content.db` },
        { op: "size", path: `${dbDir}/settings.db` },
      ], lock_wait_ms: lockWaitMs });
      if (batchRes && batchRes.busy) {
        addOutputLine(`[INFO] Aurora library: Xbox FTP busy — serving last known state.`);
        const cachedMeta = readMeta(cacheRoot);
        if (cachedMeta && fs.existsSync(contentDbPath(cacheRoot)) && fs.existsSync(settingsDbPath(cacheRoot))) {
          const contentBuf  = fs.readFileSync(contentDbPath(cacheRoot));
          const settingsBuf = fs.readFileSync(settingsDbPath(cacheRoot));
          const scanDriveMap = new Map<number, string>(
            Object.entries(cachedMeta.scanDriveMap || {}).map(([k, v]) => [Number(k), String(v)])
          );
          const games = await buildAuroraGamesFromDbBuffers(contentBuf, settingsBuf, scanDriveMap);
          return { ok: true, games, connectedTo: xboxIp, auroraRoot, libraryUnchanged: true, fromCache: true, ftpBusy: true };
        }
        return { ok: false, error: "Xbox FTP busy and no cached Aurora library available." };
      }
      let results = batchRes.results || [];
      let contentSz  = results[0] && results[0].ok ? Number(results[0].data) : -1;
      let settingsSz = results[1] && results[1].ok ? Number(results[1].data) : -1;

      if (contentSz < 0 || settingsSz < 0) {
        addOutputLine(
          `[INFO] Aurora library: ${auroraRoot}/Data/Databases not found — auto-discovering Aurora install…`
        );
        const discovered = await discoverAuroraRoot(xboxIp);
        if (discovered) {
          setLastDiscoveredAuroraRoot(discovered);
          auroraRoot = discovered;
          dbDir      = `${auroraRoot}/Data/Databases`;
          cacheRoot  = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);
          setActiveAuroraCacheRoot(cacheRoot);
          addOutputLine(`[INFO] Aurora library: discovered Aurora at ${auroraRoot}`);
          // Re-check sizes at new path
          batchRes = await backendPost("/ftp/batch", { ip: xboxIp, ops: [
            { op: "size", path: `${dbDir}/content.db` },
            { op: "size", path: `${dbDir}/settings.db` },
          ]});
          results = batchRes.results || [];
          contentSz  = results[0] && results[0].ok ? Number(results[0].data) : -1;
          settingsSz = results[1] && results[1].ok ? Number(results[1].data) : -1;
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

      // Download databases via Go backend batch (download to local cache paths)
      addOutputLine("[INFO] Aurora library: downloading content.db and settings.db…");
      fs.mkdirSync(databasesDir(cacheRoot), { recursive: true });

      const dlBatchRes = await backendPost("/ftp/batch", { ip: xboxIp, ops: [
        { op: "download", path: `${dbDir}/content.db`,  local_path: contentDbPath(cacheRoot) },
        { op: "download", path: `${dbDir}/settings.db`, local_path: settingsDbPath(cacheRoot) },
      ]});
      const dlResults = dlBatchRes.results || [];
      if (dlResults[0] && !dlResults[0].ok) throw new Error(`Download content.db failed: ${dlResults[0].error}`);
      if (dlResults[1] && !dlResults[1].ok) throw new Error(`Download settings.db failed: ${dlResults[1].error}`);

      const contentBuf  = fs.readFileSync(contentDbPath(cacheRoot));
      const settingsBuf = fs.readFileSync(settingsDbPath(cacheRoot));

      const scanRows     = await readScanRowsFromSettingsBuffer(settingsBuf);
      const contentRows  = await readContentScanRowsFromBuffer(contentBuf);
      addOutputLine(`[INFO] Aurora library: probing ${scanRows.length} scan path(s) for drive letters…`);

      const scanDriveMap = await probeScanPathDrives(xboxIp, scanRows, contentRows);

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
    }
  });

  // ── Fetch Aurora covers + visual assets ─────────────────────────────────────
  ipcMain.handle("xbox:fetch-aurora-covers", async (_event, gameList, opts) => {
    if (!Array.isArray(gameList) || gameList.length === 0) return { ok: true };

    const force        = opts && opts.force        === true;
    const fromDiskOnly = opts && opts.fromDiskOnly  === true;

    const xboxIp      = getConfiguredXboxIP();
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

    let lastProcessed = -1;
    try {
      addOutputLine(
        `[INFO] Aurora covers + artwork: FTP sync starting for ${gameList.length} title(s)${force ? " (force refresh with hash verification)" : ""}…`
      );

      const syncStartTime = Date.now();
      const progressEvery = 25;
      for (let gi = 0; gi < gameList.length; gi += 1) {
        const { titleId, gameDataDir } = gameList[gi];
        try {
          await syncAuroraGameCoverAssets(xboxIp, auroraRoot, mediaDir, titleId, gameDataDir, cacheRoot, force);
          await syncAuroraTitleVisualAssets(xboxIp, auroraRoot, titleId, gameDataDir, cacheRoot, force);
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

      const elapsed = ((Date.now() - syncStartTime) / 1000).toFixed(1);
      addOutputLine(`[INFO] Aurora covers + artwork: finished ${gameList.length} title(s) in ${elapsed}s.`);
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

  // ── Export Aurora DBs for debugging ────────────────────────────────────────
  ipcMain.handle("xbox:export-aurora-db", async () => {
    const xboxIp      = getConfiguredXboxIP();
    const scriptsPath = getConfiguredFtpScriptsPath();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    let auroraRoot = xboxAuroraRoot(scriptsPath);
    let dbDir      = `${auroraRoot}/Data/Databases`;

    // Quick probe to discover Aurora root if default path is wrong
    try {
      const probeRes = await backendPost("/ftp/batch", { ip: xboxIp, ops: [
        { op: "size", path: `${dbDir}/content.db` },
      ]});
      const r = (probeRes.results || [])[0];
      if (!r || !r.ok) {
        const discovered = await discoverAuroraRoot(xboxIp);
        if (discovered) {
          auroraRoot = discovered;
          dbDir = `${auroraRoot}/Data/Databases`;
        }
      }
    } catch { /* ignore probe errors, fall back to default */ }

    // Let user pick a destination folder
    const win = BrowserWindow.getFocusedWindow();
    const dlg = await dialog.showOpenDialog(win || undefined, {
      title: "Select folder to save Aurora databases",
      properties: ["openDirectory", "createDirectory"],
    });
    if (dlg.canceled || !dlg.filePaths[0]) {
      return { ok: false, error: "No destination folder selected." };
    }
    const destDir = dlg.filePaths[0];

    addOutputLine("[INFO] Exporting Aurora DBs from console…");
    try {
      const dlRes = await backendPost("/ftp/batch", { ip: xboxIp, ops: [
        { op: "download", path: `${dbDir}/content.db`,  local_path: path.join(destDir, "content.db") },
        { op: "download", path: `${dbDir}/settings.db`, local_path: path.join(destDir, "settings.db") },
      ]});
      const results = dlRes.results || [];
      const contentOk  = results[0] && results[0].ok;
      const settingsOk = results[1] && results[1].ok;

      if (!contentOk || !settingsOk) {
        const err = (!contentOk ? results[0]?.error : "") + " " + (!settingsOk ? results[1]?.error : "");
        addOutputLine(`[ERROR] Aurora DB export failed: ${err.trim()}`);
        return { ok: false, error: err.trim() || "FTP download failed." };
      }

      addOutputLine(`[INFO] Aurora DBs exported to ${destDir}`);
      return { ok: true, files: [path.join(destDir, "content.db"), path.join(destDir, "settings.db")] };
    } catch (err: any) {
      const msg = err.message || String(err);
      addOutputLine(`[ERROR] Aurora DB export: ${msg}`);
      return { ok: false, error: msg };
    }
  });
}
