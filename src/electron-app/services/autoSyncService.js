"use strict";
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
 *
 * All FTP operations are proxied through the Go backend for centralised tracking.
 */
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.autoUploadAuroraAssets = autoUploadAuroraAssets;
exports.doAuroraLibrarySync = doAuroraLibrarySync;
const fs_1 = __importDefault(require("fs"));
const electron_1 = require("electron");
const settingsService_1 = require("./settingsService");
const backendClient_1 = require("./backendClient");
const serverLog_1 = require("../infrastructure/serverLog");
const auroraLibraryCache_1 = require("../infrastructure/auroraLibraryCache");
const auroraPathHelper_1 = require("./auroraPathHelper");
const auroraLibraryService_1 = require("./auroraLibraryService");
const httpHelper_1 = require("../infrastructure/httpHelper");
const backendHttp_1 = require("../infrastructure/backendHttp");
// ── FTP batch helper ──────────────────────────────────────────────────────────
async function batchFtp(xboxIp, ops) {
    const res = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops }, 120000);
    return res.results || [];
}
/**
 * Download Aurora assets (cover, background, banner, icon) for a title from
 * Xbox Live CDN and XboxUnity, then upload them to the console's Aurora Import
 * folder via the Go backend FTP batch endpoint.
 */
async function autoUploadAuroraAssets(titleId, xboxIp) {
    if (!titleId || !/^[0-9A-F]{8}$/i.test(titleId) || !xboxIp)
        return;
    const tidUpper = titleId.toUpperCase();
    const scriptsPath = (0, settingsService_1.getConfiguredFtpScriptsPath)();
    const auroraRoot = (0, auroraPathHelper_1.xboxAuroraRoot)(scriptsPath);
    const importDir = `${auroraRoot}/User/Import/${tidUpper}`;
    (0, backendClient_1.addOutputLine)(`[INFO] Auto-assets: fetching Aurora assets for ${tidUpper}…`);
    // ── Collect typed images from Xbox CDN catalog ────────────────────────────
    const catalogUrl = `http://catalog-cdn.xboxlive.com/Catalog/Catalog.asmx/Query` +
        `?methodName=FindGames&Names=Locale&Values=en-US&Names=LegalLocale&Values=en-US` +
        `&Names=Store&Values=1&Names=PageSize&Values=100&Names=PageNum&Values=1` +
        `&Names=DetailView&Values=5&Names=OfferFilterLevel&Values=1` +
        `&Names=MediaIds&Values=66acd000-77fe-1000-9115-d802${tidUpper}` +
        `&Names=UserTypes&Values=2&Names=MediaTypes&Values=1&Names=MediaTypes&Values=21` +
        `&Names=MediaTypes&Values=23&Names=MediaTypes&Values=37&Names=MediaTypes&Values=46`;
    const cdnImages = {
        background: null,
        banner: null,
        icon: null,
    };
    try {
        const xmlBuf = await (0, httpHelper_1.fetchHttpImage)(catalogUrl);
        if (xmlBuf && xmlBuf.length > 0) {
            const xml = xmlBuf.toString("utf8");
            for (const [, block] of xml.matchAll(/<live:image[^>]*>([\s\S]*?)<\/live:image>/gi)) {
                const urlM = block.match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
                const typeM = block.match(/<live:relationshipType[^>]*>\s*(\d+)\s*<\/live:relationshipType>/i);
                if (!urlM)
                    continue;
                const url = urlM[1].trim();
                const type = typeM ? parseInt(typeM[1], 10) : -1;
                if ((type === 15 || type === 23) && !cdnImages.icon)
                    cdnImages.icon = url;
                else if (type === 25 && !cdnImages.background)
                    cdnImages.background = url;
                else if (type === 27 && !cdnImages.banner)
                    cdnImages.banner = url;
            }
        }
    }
    catch { /* CDN catalog unavailable */ }
    // ── Cover: XboxUnity (preferred) → Xbox CDN fallback ─────────────────────
    let coverBuf = null;
    try {
        const unityBuf = await (0, httpHelper_1.fetchHttpImage)(`http://xboxunity.net/api/Covers/${encodeURIComponent(tidUpper)}`);
        if (unityBuf && unityBuf.length > 0) {
            let items;
            try {
                items = JSON.parse(unityBuf.toString("utf8"));
            }
            catch {
                items = null;
            }
            if (Array.isArray(items)) {
                items.sort((a, b) => {
                    if (!!b.official !== !!a.official)
                        return a.official ? -1 : 1;
                    return (b.rating || 0) - (a.rating || 0);
                });
                const first = items.find((r) => r.front || r.url);
                if (first) {
                    const coverUrl = first.front || first.url;
                    if (coverUrl && coverUrl.startsWith("http"))
                        coverBuf = await (0, httpHelper_1.fetchHttpImage)(coverUrl);
                }
            }
        }
    }
    catch { /* XboxUnity unavailable */ }
    if (!coverBuf || coverBuf.length < 100) {
        try {
            coverBuf = await (0, httpHelper_1.fetchHttpImage)(`http://catalog.xboxlive.com/Catalog/Product/CoverArt/${tidUpper}/en-US/1`);
        }
        catch { /* CDN cover unavailable */ }
    }
    // ── Build upload list ─────────────────────────────────────────────────────
    const uploads = [];
    if (coverBuf && coverBuf.length >= 100)
        uploads.push({ assetType: "cover", buf: coverBuf });
    for (const [slot, url] of Object.entries(cdnImages)) {
        if (!url)
            continue;
        try {
            const buf = await (0, httpHelper_1.fetchHttpImage)(url);
            if (buf && buf.length >= 100)
                uploads.push({ assetType: slot, buf });
        }
        catch { /* skip unreachable CDN asset */ }
    }
    if (uploads.length === 0) {
        (0, backendClient_1.addOutputLine)(`[INFO] Auto-assets: no assets found for ${tidUpper}`);
        return;
    }
    // ── Upload via Go backend FTP batch (single connection) ───────────────────
    try {
        const ops = [{ op: "ensure_dir", path: importDir }];
        for (const { assetType, buf } of uploads) {
            const ext = (0, httpHelper_1.imageExtFromMagic)(buf);
            const remotePath = `${importDir}/${assetType}${ext}`;
            ops.push({ op: "upload_base64", path: remotePath, data: buf.toString("base64") });
        }
        const results = await batchFtp(xboxIp, ops);
        // Log results (skip ensure_dir at index 0)
        for (let i = 1; i < results.length; i++) {
            const { assetType, buf } = uploads[i - 1];
            const ext = (0, httpHelper_1.imageExtFromMagic)(buf);
            if (results[i] && results[i].ok) {
                (0, backendClient_1.addOutputLine)(`[INFO] Auto-assets: uploaded ${assetType}${ext} for ${tidUpper}`);
            }
            else {
                (0, backendClient_1.addOutputLine)(`[WARN] Auto-assets: failed ${assetType}${ext} for ${tidUpper}: ${results[i]?.error || "unknown"}`);
            }
        }
        (0, serverLog_1.appendAppEvent)("AURORA_ASSET", `auto-uploaded ${uploads.length} asset(s) for ${tidUpper}`);
    }
    catch (err) {
        (0, backendClient_1.addOutputLine)(`[WARN] Auto-assets: FTP upload error for ${tidUpper}: ${err.message || err}`);
    }
}
/**
 * Re-download Aurora's content.db and settings.db from the console and update
 * the local library cache.  Called automatically after FTP transfers and game
 * moves to keep the Xbox Library view in sync.
 * All FTP operations go through the Go backend for centralised tracking.
 */
async function doAuroraLibrarySync() {
    const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
    const scriptsPath = (0, settingsService_1.getConfiguredFtpScriptsPath)();
    if (!xboxIp)
        return;
    let auroraRoot = (0, auroraPathHelper_1.xboxAuroraRoot)(scriptsPath);
    let dbDir = `${auroraRoot}/Data/Databases`;
    let cacheRoot = (0, auroraLibraryCache_1.getAuroraLibraryCacheRoot)(electron_1.app, xboxIp, auroraRoot);
    (0, backendClient_1.addOutputLine)(`[INFO] Auto-sync: refreshing Aurora library cache…`);
    try {
        // Check DB sizes via Go backend batch
        let batchRes = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops: [
                { op: "size", path: `${dbDir}/content.db` },
                { op: "size", path: `${dbDir}/settings.db` },
            ] });
        let results = batchRes.results || [];
        let contentSz = results[0] && results[0].ok ? Number(results[0].data) : -1;
        let settingsSz = results[1] && results[1].ok ? Number(results[1].data) : -1;
        // Auto-discover Aurora root if databases not found at the configured path.
        if (contentSz < 0 || settingsSz < 0) {
            const discovered = await (0, auroraPathHelper_1.discoverAuroraRoot)(xboxIp);
            if (discovered) {
                (0, auroraPathHelper_1.setLastDiscoveredAuroraRoot)(discovered);
                auroraRoot = discovered;
                dbDir = `${auroraRoot}/Data/Databases`;
                cacheRoot = (0, auroraLibraryCache_1.getAuroraLibraryCacheRoot)(electron_1.app, xboxIp, auroraRoot);
                (0, auroraLibraryCache_1.setActiveAuroraCacheRoot)(cacheRoot);
                // Re-check sizes at discovered path
                batchRes = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops: [
                        { op: "size", path: `${dbDir}/content.db` },
                        { op: "size", path: `${dbDir}/settings.db` },
                    ] });
                results = batchRes.results || [];
                contentSz = results[0] && results[0].ok ? Number(results[0].data) : -1;
                settingsSz = results[1] && results[1].ok ? Number(results[1].data) : -1;
            }
        }
        if (contentSz < 0 || settingsSz < 0) {
            (0, backendClient_1.addOutputLine)(`[WARN] Auto-sync: Aurora DBs unreachable — skipping library sync`);
            return;
        }
        // Download databases via Go backend batch (download to local cache paths)
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
        const contentRows = await (0, auroraLibraryService_1.readContentScanRowsFromBuffer)(contentBuf);
        const scanRows = await (0, auroraLibraryService_1.readScanRowsFromSettingsBuffer)(settingsBuf);
        const scanDriveMap = await (0, auroraLibraryService_1.probeScanPathDrives)(xboxIp, scanRows, contentRows);
        (0, auroraLibraryCache_1.writeMeta)(cacheRoot, {
            xboxIp,
            auroraRoot,
            ftpScriptsPath: scriptsPath,
            contentDbSize: contentSz,
            settingsDbSize: settingsSz,
            scanDriveMap: Object.fromEntries([...scanDriveMap.entries()].map(([k, v]) => [String(k), v])),
            driveProbeVersion: 2,
            updatedAt: Date.now(),
        });
        (0, auroraLibraryCache_1.setActiveAuroraCacheRoot)(cacheRoot);
        (0, backendClient_1.addOutputLine)(`[INFO] Auto-sync: Aurora library cache updated.`);
    }
    catch (err) {
        (0, backendClient_1.addOutputLine)(`[WARN] Auto-sync: library sync error: ${err.message || err}`);
    }
}
