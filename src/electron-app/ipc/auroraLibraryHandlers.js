"use strict";
/**
 * IPC handlers for Aurora library operations:
 *   xbox:list-aurora-library
 *   xbox:fetch-aurora-covers
 *   xbox:refresh-title-visuals-cache
 *
 * All FTP operations are proxied through the Go backend for centralised tracking.
 */
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.register = register;
const electron_1 = require("electron");
const fs_1 = __importDefault(require("fs"));
const settingsService_1 = require("../services/settingsService");
const backendClient_1 = require("../services/backendClient");
const serverLog_1 = require("../infrastructure/serverLog");
const auroraLibraryCache_1 = require("../infrastructure/auroraLibraryCache");
const auroraPathHelper_1 = require("../services/auroraPathHelper");
const auroraLibraryService_1 = require("../services/auroraLibraryService");
const auroraVisualService_1 = require("../services/auroraVisualService");
const backendHttp_1 = require("../infrastructure/backendHttp");
function register(ipcMain) {
    // ── List Aurora library (DB fingerprint sync + game list) ──────────────────
    ipcMain.handle("xbox:list-aurora-library", async (_event, opts) => {
        const force = opts && opts.force === true;
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        const scriptsPath = (0, settingsService_1.getConfiguredFtpScriptsPath)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured. Set it in Settings." };
        let auroraRoot = (0, auroraPathHelper_1.xboxAuroraRoot)(scriptsPath);
        let dbDir = `${auroraRoot}/Data/Databases`;
        let cacheRoot = (0, auroraLibraryCache_1.getAuroraLibraryCacheRoot)(electron_1.app, xboxIp, auroraRoot);
        (0, auroraLibraryCache_1.setActiveAuroraCacheRoot)(cacheRoot);
        try {
            (0, backendClient_1.addOutputLine)(`[INFO] Aurora library: ${force ? "refresh (forced)" : "loading"} — FTP ${xboxIp}…`);
            // Check DB sizes via Go backend batch (1 FTP connection)
            let batchRes = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops: [
                    { op: "size", path: `${dbDir}/content.db` },
                    { op: "size", path: `${dbDir}/settings.db` },
                ] });
            let results = batchRes.results || [];
            let contentSz = results[0] && results[0].ok ? Number(results[0].data) : -1;
            let settingsSz = results[1] && results[1].ok ? Number(results[1].data) : -1;
            if (contentSz < 0 || settingsSz < 0) {
                (0, backendClient_1.addOutputLine)(`[INFO] Aurora library: ${auroraRoot}/Data/Databases not found — auto-discovering Aurora install…`);
                const discovered = await (0, auroraPathHelper_1.discoverAuroraRoot)(xboxIp);
                if (discovered) {
                    (0, auroraPathHelper_1.setLastDiscoveredAuroraRoot)(discovered);
                    auroraRoot = discovered;
                    dbDir = `${auroraRoot}/Data/Databases`;
                    cacheRoot = (0, auroraLibraryCache_1.getAuroraLibraryCacheRoot)(electron_1.app, xboxIp, auroraRoot);
                    (0, auroraLibraryCache_1.setActiveAuroraCacheRoot)(cacheRoot);
                    (0, backendClient_1.addOutputLine)(`[INFO] Aurora library: discovered Aurora at ${auroraRoot}`);
                    // Re-check sizes at new path
                    batchRes = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops: [
                            { op: "size", path: `${dbDir}/content.db` },
                            { op: "size", path: `${dbDir}/settings.db` },
                        ] });
                    results = batchRes.results || [];
                    contentSz = results[0] && results[0].ok ? Number(results[0].data) : -1;
                    settingsSz = results[1] && results[1].ok ? Number(results[1].data) : -1;
                }
                else {
                    (0, backendClient_1.addOutputLine)(`[ERROR] Aurora library: could not find an Aurora install on the console.`);
                }
            }
            const meta = (0, auroraLibraryCache_1.readMeta)(cacheRoot);
            const fingerprintMatch = !force &&
                meta &&
                meta.xboxIp === xboxIp &&
                meta.auroraRoot === auroraRoot &&
                meta.ftpScriptsPath === scriptsPath &&
                meta.contentDbSize === contentSz &&
                meta.settingsDbSize === settingsSz &&
                contentSz >= 0 &&
                settingsSz >= 0 &&
                meta.scanDriveMap &&
                meta.driveProbeVersion === 2 &&
                fs_1.default.existsSync((0, auroraLibraryCache_1.contentDbPath)(cacheRoot)) &&
                fs_1.default.existsSync((0, auroraLibraryCache_1.settingsDbPath)(cacheRoot));
            if (fingerprintMatch) {
                const contentBuf = fs_1.default.readFileSync((0, auroraLibraryCache_1.contentDbPath)(cacheRoot));
                const settingsBuf = fs_1.default.readFileSync((0, auroraLibraryCache_1.settingsDbPath)(cacheRoot));
                const scanDriveMap = new Map(Object.entries(meta.scanDriveMap).map(([k, v]) => [Number(k), String(v)]));
                const games = await (0, auroraLibraryService_1.buildAuroraGamesFromDbBuffers)(contentBuf, settingsBuf, scanDriveMap);
                (0, backendClient_1.addOutputLine)(`[INFO] Aurora library: using local DB cache (${games.length} games, console DB unchanged).`);
                return { ok: true, games, connectedTo: xboxIp, auroraRoot, libraryUnchanged: true, fromCache: true };
            }
            // Download databases via Go backend batch (download to local cache paths)
            (0, backendClient_1.addOutputLine)("[INFO] Aurora library: downloading content.db and settings.db…");
            fs_1.default.mkdirSync((0, auroraLibraryCache_1.databasesDir)(cacheRoot), { recursive: true });
            const dlBatchRes = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops: [
                    { op: "download", path: `${dbDir}/content.db`, local_path: (0, auroraLibraryCache_1.contentDbPath)(cacheRoot) },
                    { op: "download", path: `${dbDir}/settings.db`, local_path: (0, auroraLibraryCache_1.settingsDbPath)(cacheRoot) },
                ] });
            const dlResults = dlBatchRes.results || [];
            if (dlResults[0] && !dlResults[0].ok)
                throw new Error(`Download content.db failed: ${dlResults[0].error}`);
            if (dlResults[1] && !dlResults[1].ok)
                throw new Error(`Download settings.db failed: ${dlResults[1].error}`);
            const contentBuf = fs_1.default.readFileSync((0, auroraLibraryCache_1.contentDbPath)(cacheRoot));
            const settingsBuf = fs_1.default.readFileSync((0, auroraLibraryCache_1.settingsDbPath)(cacheRoot));
            const scanRows = await (0, auroraLibraryService_1.readScanRowsFromSettingsBuffer)(settingsBuf);
            const contentRows = await (0, auroraLibraryService_1.readContentScanRowsFromBuffer)(contentBuf);
            (0, backendClient_1.addOutputLine)(`[INFO] Aurora library: probing ${scanRows.length} scan path(s) for drive letters…`);
            const scanDriveMap = await (0, auroraLibraryService_1.probeScanPathDrives)(xboxIp, scanRows, contentRows);
            (0, auroraLibraryCache_1.writeMeta)(cacheRoot, {
                xboxIp,
                auroraRoot,
                ftpScriptsPath: scriptsPath,
                contentDbSize: contentSz,
                settingsDbSize: settingsSz,
                scanDriveMap: Object.fromEntries(scanDriveMap),
                driveProbeVersion: 2,
                updatedAt: Date.now(),
            });
            const games = await (0, auroraLibraryService_1.buildAuroraGamesFromDbBuffers)(contentBuf, settingsBuf, scanDriveMap);
            (0, backendClient_1.addOutputLine)(`[INFO] Aurora library: ready (${games.length} games, DB saved to app cache).`);
            return { ok: true, games, connectedTo: xboxIp, auroraRoot, libraryUnchanged: false, fromCache: false };
        }
        catch (err) {
            const msg = err.message || String(err);
            (0, backendClient_1.addOutputLine)(`[ERROR] Aurora library: ${msg}`);
            (0, serverLog_1.appendAppEvent)("AURORA_LIB", `error: ${msg}`);
            return { ok: false, error: msg };
        }
    });
    // ── Fetch Aurora covers + visual assets ─────────────────────────────────────
    ipcMain.handle("xbox:fetch-aurora-covers", async (_event, gameList, opts) => {
        if (!Array.isArray(gameList) || gameList.length === 0)
            return { ok: true };
        const force = opts && opts.force === true;
        const fromDiskOnly = opts && opts.fromDiskOnly === true;
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        const scriptsPath = (0, settingsService_1.getConfiguredFtpScriptsPath)();
        const auroraRoot = (0, auroraPathHelper_1.xboxAuroraRoot)(scriptsPath);
        const mediaDir = (0, auroraPathHelper_1.xboxAuroraMediaDir)(scriptsPath);
        const cacheRoot = (0, auroraLibraryCache_1.getAuroraLibraryCacheRoot)(electron_1.app, xboxIp, auroraRoot);
        (0, auroraLibraryCache_1.setActiveAuroraCacheRoot)(cacheRoot);
        if (fromDiskOnly) {
            (0, backendClient_1.addOutputLine)(`[INFO] Aurora covers + artwork: serving ${gameList.length} title(s) from disk cache (no FTP).`);
            for (const { titleId, gameDataDir } of gameList) {
                (0, auroraVisualService_1.emitAuroraCoverEvents)(titleId, gameDataDir, cacheRoot);
                (0, auroraVisualService_1.emitAuroraTitleVisualEvents)(titleId, gameDataDir, cacheRoot);
            }
            return { ok: true };
        }
        let lastProcessed = -1;
        try {
            (0, backendClient_1.addOutputLine)(`[INFO] Aurora covers + artwork: FTP sync starting for ${gameList.length} title(s)${force ? " (force refresh with hash verification)" : ""}…`);
            const syncStartTime = Date.now();
            const progressEvery = 25;
            for (let gi = 0; gi < gameList.length; gi += 1) {
                const { titleId, gameDataDir } = gameList[gi];
                try {
                    await (0, auroraVisualService_1.syncAuroraGameCoverAssets)(xboxIp, auroraRoot, mediaDir, titleId, gameDataDir, cacheRoot, force);
                    await (0, auroraVisualService_1.syncAuroraTitleVisualAssets)(xboxIp, auroraRoot, titleId, gameDataDir, cacheRoot, force);
                }
                catch (err) {
                    const em = err?.message || String(err);
                    (0, backendClient_1.addOutputLine)(`[WARN] Aurora sync ${titleId}: ${em}`);
                    (0, serverLog_1.appendAppEvent)("AURORA_SYNC", `${titleId}: ${em}`);
                }
                finally {
                    (0, auroraVisualService_1.emitAuroraCoverEvents)(titleId, gameDataDir, cacheRoot);
                    (0, auroraVisualService_1.emitAuroraTitleVisualEvents)(titleId, gameDataDir, cacheRoot);
                }
                lastProcessed = gi;
                if (progressEvery > 0 && (gi + 1) % progressEvery === 0) {
                    (0, backendClient_1.addOutputLine)(`[INFO] Aurora covers + artwork: progress ${gi + 1}/${gameList.length} titles…`);
                }
                if ((gi & 3) === 3) {
                    await new Promise((resolve) => setImmediate(resolve));
                }
            }
            const elapsed = ((Date.now() - syncStartTime) / 1000).toFixed(1);
            (0, backendClient_1.addOutputLine)(`[INFO] Aurora covers + artwork: finished ${gameList.length} title(s) in ${elapsed}s.`);
            return { ok: true };
        }
        catch (err) {
            const msg = err.message || String(err);
            (0, backendClient_1.addOutputLine)(`[ERROR] Aurora covers + artwork: ${msg}`);
            (0, serverLog_1.appendAppEvent)("AURORA_SYNC", `fatal: ${msg}`);
            for (let gi = lastProcessed + 1; gi < gameList.length; gi += 1) {
                const { titleId, gameDataDir } = gameList[gi];
                (0, auroraVisualService_1.emitAuroraCoverEvents)(titleId, gameDataDir, cacheRoot);
                (0, auroraVisualService_1.emitAuroraTitleVisualEvents)(titleId, gameDataDir, cacheRoot);
            }
            return { ok: false, error: msg };
        }
    });
    // ── Refresh title visuals from disk cache (no FTP) ─────────────────────────
    ipcMain.handle("xbox:refresh-title-visuals-cache", async (_event, payload) => {
        const p = payload || {};
        const titleId = typeof p.titleId === "string" ? p.titleId.trim().toUpperCase() : "";
        const gameDataDir = typeof p.gameDataDir === "string" ? p.gameDataDir.trim() : "";
        if (!titleId || !gameDataDir)
            return { ok: false, error: "titleId and gameDataDir required." };
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        const scriptsPath = (0, settingsService_1.getConfiguredFtpScriptsPath)();
        const auroraRoot = (0, auroraPathHelper_1.xboxAuroraRoot)(scriptsPath);
        const cacheRoot = (0, auroraLibraryCache_1.getAuroraLibraryCacheRoot)(electron_1.app, xboxIp, auroraRoot);
        (0, auroraLibraryCache_1.setActiveAuroraCacheRoot)(cacheRoot);
        (0, auroraVisualService_1.emitAuroraTitleVisualEvents)(titleId, gameDataDir, cacheRoot);
        return { ok: true };
    });
}
