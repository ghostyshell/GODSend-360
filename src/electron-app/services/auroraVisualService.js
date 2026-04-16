"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.auroraCdnUrl = auroraCdnUrl;
exports.classifyFlatMediaSuffix = classifyFlatMediaSuffix;
exports.classifyAuroraFileKind = classifyAuroraFileKind;
exports.emptyTitleVisualsPayload = emptyTitleVisualsPayload;
exports.parseGameAssetInfoXml = parseGameAssetInfoXml;
exports.summarizeGameCoverInfoJson = summarizeGameCoverInfoJson;
exports.emitAuroraTitleVisualEvents = emitAuroraTitleVisualEvents;
exports.emitAuroraCoverEvents = emitAuroraCoverEvents;
exports.syncAuroraTitleVisualAssets = syncAuroraTitleVisualAssets;
exports.syncAuroraGameCoverAssets = syncAuroraGameCoverAssets;
const path_1 = __importDefault(require("path"));
const fs_1 = __importDefault(require("fs"));
const http_1 = __importDefault(require("http"));
const crypto_1 = __importDefault(require("crypto"));
const httpHelper_1 = require("../infrastructure/httpHelper");
const auroraLibraryCache_1 = require("../infrastructure/auroraLibraryCache");
const settingsService_1 = require("./settingsService");
const backendClient_1 = require("./backendClient");
const window_1 = require("../app/window");
const backendHttp_1 = require("../infrastructure/backendHttp");
// ── Pure helpers ──────────────────────────────────────────────────────────────
function auroraCdnUrl(relUnix) {
    const r = String(relUnix || "").replace(/\\/g, "/").replace(/^\/+/, "");
    return `godsend-aurora://cdn/${r}`;
}
function safeVisualLocalName(name) {
    return String(name || "").replace(/[^a-zA-Z0-9._-]+/g, "_").slice(0, 180) || "file";
}
function classifyFlatMediaSuffix(titleId, filename) {
    const base = path_1.default.basename(filename);
    const tid = titleId.toUpperCase();
    const nu = base.toUpperCase();
    if (!nu.startsWith(tid))
        return "other";
    const dot = nu.lastIndexOf(".");
    const stem = dot >= tid.length ? nu.slice(tid.length, dot) : nu.slice(tid.length);
    if (!stem)
        return "other";
    if (stem === "GC")
        return "cover";
    if (stem === "BK" || stem === "BG")
        return "background";
    if (stem === "BN" || stem === "BA")
        return "banner";
    if (stem === "IC" || stem === "IL" || stem === "IS")
        return "icon";
    if (/^SS\d*$/i.test(stem) || /^SC\d*$/i.test(stem))
        return "screenshot";
    return "other";
}
function classifyAuroraFileKind(name) {
    const lower = String(name || "").toLowerCase();
    if (lower.endsWith(".asset"))
        return "asset";
    if (lower.endsWith(".bin"))
        return "bin";
    if (/\.(jpg|jpeg|png|gif|bmp|dds)$/i.test(lower))
        return "image";
    return "other";
}
function emptyTitleVisualsPayload() {
    return { cover: null, background: null, banner: null, icon: null, screenshots: [], other: [] };
}
function parseGameAssetInfoXml(xmlText) {
    const result = { background: null, banner: null,
        icon: null, cover: null, screenshots: [] };
    if (!xmlText || typeof xmlText !== "string")
        return result;
    for (const [, block] of xmlText.matchAll(/<live:asset[^>]*>([\s\S]*?)<\/live:asset>/gi)) {
        const urlM = block.match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
        const typeM = block.match(/<live:relationshipType[^>]*>\s*(\d+)\s*<\/live:relationshipType>/i);
        if (!urlM || !typeM)
            continue;
        const url = urlM[1].trim();
        const type = parseInt(typeM[1], 10);
        if (type === 25 && !result.background)
            result.background = url;
        else if (type === 27 && !result.banner)
            result.banner = url;
        else if (type === 23 && !result.icon)
            result.icon = url;
        else if (type === 33 && !result.cover)
            result.cover = url;
    }
    for (const [, block] of xmlText.matchAll(/<live:slideShow[^>]*>([\s\S]*?)<\/live:slideShow>/gi)) {
        const urlM = block.match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
        if (urlM)
            result.screenshots.push(urlM[1].trim());
    }
    return result;
}
function summarizeGameCoverInfoJson(text) {
    if (!text || typeof text !== "string")
        return { entryCount: 0, preview: [] };
    let arr;
    try {
        arr = JSON.parse(text);
    }
    catch {
        return { entryCount: 0, preview: [], parseError: true };
    }
    if (!Array.isArray(arr))
        return { entryCount: 0, preview: [] };
    const preview = arr.slice(0, 12).map((e, i) => ({
        index: i,
        official: !!e?.official,
        rating: e?.rating != null ? Number(e.rating) : null,
        hasFront: !!(e?.front && String(e.front).trim()),
        hasThumbnail: !!(e?.thumbnail && String(e.thumbnail).trim()),
        hasUrl: !!(e?.url && String(e.url).trim()),
    }));
    return { entryCount: arr.length, preview };
}
// ── Renderer push helpers ──────────────────────────────────────────────────────
function emitAuroraTitleVisualEvents(titleId, gameDataDir, cacheRoot) {
    const wc = (0, window_1.getWebContentsForPush)();
    if (!wc)
        return;
    const manifestPath = path_1.default.join((0, auroraLibraryCache_1.gameCacheDir)(cacheRoot, gameDataDir), "visual-manifest.json");
    if (!fs_1.default.existsSync(manifestPath)) {
        wc.send("xbox-title-visuals", { titleId, gameDataDir, visuals: emptyTitleVisualsPayload() });
        return;
    }
    let m;
    try {
        m = JSON.parse(fs_1.default.readFileSync(manifestPath, "utf8"));
    }
    catch {
        wc.send("xbox-title-visuals", { titleId, gameDataDir, visuals: emptyTitleVisualsPayload() });
        return;
    }
    const toAsset = (o) => o && o.rel ? { src: auroraCdnUrl(o.rel), ext: o.ext || "" } : null;
    wc.send("xbox-title-visuals", {
        titleId,
        gameDataDir,
        visuals: {
            coverIsBooklet: Boolean(m.importCover && m.importCover.rel),
            cover: toAsset(m.importCover || m.mediaCover),
            background: toAsset(m.background),
            banner: toAsset(m.banner),
            icon: toAsset(m.icon),
            screenshots: Array.isArray(m.screenshots)
                ? m.screenshots
                    .map((s) => ({ src: s.rel ? auroraCdnUrl(s.rel) : "", ext: s.ext || "", name: s.name || "" }))
                    .filter((s) => s.src)
                : [],
            other: Array.isArray(m.other)
                ? m.other
                    .map((o) => ({ src: o.rel ? auroraCdnUrl(o.rel) : "", ext: o.ext || "", name: o.name || "" }))
                    .filter((o) => o.src)
                : [],
        },
    });
}
function emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot) {
    const wc = (0, window_1.getWebContentsForPush)();
    if (!wc)
        return;
    const gdir = (0, auroraLibraryCache_1.gameCacheDir)(cacheRoot, gameDataDir);
    const metaP = path_1.default.join(gdir, "cover-files.json");
    let primarySrc = null;
    if (fs_1.default.existsSync(metaP)) {
        try {
            const meta = JSON.parse(fs_1.default.readFileSync(metaP, "utf8"));
            if (meta.primaryFile && fs_1.default.existsSync(path_1.default.join(gdir, meta.primaryFile))) {
                primarySrc = auroraCdnUrl(`games/${gameDataDir}/${meta.primaryFile}`);
            }
        }
        catch { /* ignore */ }
    }
    if (!primarySrc && fs_1.default.existsSync(gdir)) {
        for (const name of fs_1.default.readdirSync(gdir)) {
            if (name.startsWith("cover-primary.")) {
                primarySrc = auroraCdnUrl(`games/${gameDataDir}/${name}`);
                break;
            }
        }
    }
    wc.send("xbox-cover", { titleId, gameDataDir, src: primarySrc });
}
// ── FTP batch helper (Go backend) ─────────────────────────────────────────────
async function batchFtp(xboxIp, ops) {
    const res = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops }, 120000);
    return res.results || [];
}
function bufFromBatchResult(results, idx) {
    if (idx < 0)
        return null;
    const r = results[idx];
    if (r && r.ok && r.data)
        return Buffer.from(r.data, "base64");
    return null;
}
// ── RXEA decode via Go server (takes already-downloaded buffer) ───────────────
async function decodeRxeaBuffer(assetBuf, titleId, assetName) {
    if (!assetBuf || assetBuf.length < 2048)
        return {};
    const goPort = (0, settingsService_1.getConfiguredServerPort)();
    const decoded = await new Promise((res) => {
        const req = http_1.default.request({
            host: "127.0.0.1",
            port: goPort,
            path: "/rxea/decode",
            method: "POST",
            headers: {
                "Content-Type": "application/octet-stream",
                "Content-Length": assetBuf.length,
            },
        }, (httpRes) => {
            const chunks = [];
            httpRes.on("data", (c) => chunks.push(c));
            httpRes.on("end", () => {
                try {
                    res(JSON.parse(Buffer.concat(chunks).toString("utf8")));
                }
                catch (e) {
                    (0, backendClient_1.addOutputLine)(`[WARN] RXEA sync ${titleId}/${assetName}: JSON parse error: ${e.message}`);
                    res(null);
                }
            });
        });
        req.on("error", (e) => {
            (0, backendClient_1.addOutputLine)(`[WARN] RXEA sync ${titleId}/${assetName}: Go server error: ${e.message}`);
            res(null);
        });
        req.end(assetBuf);
    });
    if (!decoded || !Array.isArray(decoded.slots)) {
        const goErr = decoded?.error ? ` — ${decoded.error}` : "";
        (0, backendClient_1.addOutputLine)(`[WARN] RXEA sync ${titleId}/${assetName}: decoder returned no slots${goErr}.`);
        if (Array.isArray(decoded?.diags) && decoded.diags.length > 0) {
            for (const d of decoded.diags) {
                (0, backendClient_1.addOutputLine)(`[DIAG] slot${d.slot}: off=${d.offset} sz=${d.size} fmt=${d.gpu_fmt} ` +
                    `w=${d.width} h=${d.height} tiled=${d.tiled} endian=${d.endian}` +
                    `${d.error ? ` err="${d.error}"` : ""}`);
            }
        }
        return {};
    }
    const result = {};
    for (const s of decoded.slots) {
        const pngBuf = Buffer.isBuffer(s.png) ? s.png : Buffer.from(s.png, "base64");
        if (pngBuf.length >= 100)
            result[s.slot] = pngBuf;
    }
    (0, backendClient_1.addOutputLine)(`[INFO] RXEA sync ${titleId}/${assetName}: decoded ${Object.keys(result).length} slot(s).`);
    return result;
}
// ── FTP fingerprint helpers ─────────────────────────────────────────────────────
function importListingFingerprint(entries) {
    return entries
        .filter((e) => e && e.name && e.type !== "dir")
        .map((e) => `${e.name}:${e.size || 0}`)
        .sort()
        .join(",");
}
// ── Main sync functions ────────────────────────────────────────────────────────
/**
 * Sync Aurora title visual assets (RXEA assets, Import folder, CDN images)
 * for a single game.  Uses the Go backend batch endpoint (one FTP connection
 * for fingerprinting, one for bulk downloads).
 */
async function syncAuroraTitleVisualAssets(xboxIp, auroraRoot, titleId, gameDataDir, cacheRoot, force) {
    const gdir = (0, auroraLibraryCache_1.gameCacheDir)(cacheRoot, gameDataDir);
    const vdir = path_1.default.join(gdir, "visual");
    const importBase = `${auroraRoot}/User/Import/${titleId}`;
    const gameDataPath = `${auroraRoot}/Data/GameData/${gameDataDir}`;
    fs_1.default.mkdirSync(vdir, { recursive: true });
    // ── Phase 1: Fingerprint batch (sizes + import listing — 1 FTP connection) ──
    const fpAssetKeys = [
        `BK${titleId}.asset`, `GC${titleId}.asset`,
        `GL${titleId}.asset`, `SS${titleId}.asset`,
        "GameAssetInfo.bin", "GameCoverInfo.bin",
    ];
    const fpOps = fpAssetKeys.map((name) => ({ op: "size", path: `${gameDataPath}/${name}` }));
    fpOps.push({ op: "list", path: importBase });
    const fpResults = await batchFtp(xboxIp, fpOps);
    const newFp = {};
    for (let i = 0; i < fpAssetKeys.length; i++) {
        newFp[fpAssetKeys[i]] = fpResults[i] && fpResults[i].ok ? Number(fpResults[i].data) : -1;
    }
    const listResult = fpResults[fpAssetKeys.length];
    let importEntries = [];
    if (listResult && listResult.ok && Array.isArray(listResult.data)) {
        importEntries = listResult.data;
    }
    newFp._importListing = importListingFingerprint(importEntries);
    // ── Phase 2: Early exit if all source fingerprints match the cached manifest ─
    const manifestPath = path_1.default.join(gdir, "visual-manifest.json");
    if (!force && fs_1.default.existsSync(manifestPath)) {
        try {
            const prev = JSON.parse(fs_1.default.readFileSync(manifestPath, "utf8"));
            const cachedFp = prev?._sourceFingerprints;
            if (cachedFp && typeof cachedFp === "object") {
                const allKeys = [...fpAssetKeys, "_importListing"];
                const allMatch = allKeys.every((k) => {
                    const cached = cachedFp[k];
                    const current = newFp[k];
                    if (cached === undefined || cached === null)
                        return current === -1 || current === "";
                    return cached === current;
                });
                if (allMatch)
                    return; // unchanged — existing manifest is still valid
            }
        }
        catch { /* corrupt manifest — proceed with full sync */ }
    }
    // ── Phase 3: Build download batch for all needed files (1 FTP connection) ────
    const dlOps = [];
    // Index import files by stem
    const importByStem = new Map();
    for (const e of importEntries) {
        if (!e || !e.name || e.type === "dir")
            continue;
        const dot = e.name.lastIndexOf(".");
        const stem = (dot >= 0 ? e.name.slice(0, dot) : e.name).toLowerCase();
        const ext = dot >= 0 ? e.name.slice(dot).toLowerCase() : "";
        if (!importByStem.has(stem))
            importByStem.set(stem, { name: e.name, extWithDot: ext });
    }
    // Queue import-file downloads
    const importSlotStems = [
        { slotKey: "background", stems: ["background", "boxartback"] },
        { slotKey: "banner", stems: ["banner"] },
        { slotKey: "icon", stems: ["icon"] },
        { slotKey: "importCover", stems: ["cover", "boxartfront"] },
    ];
    const importDownloads = [];
    for (const { slotKey, stems } of importSlotStems) {
        for (const stem of stems) {
            const entry = importByStem.get(stem);
            if (!entry)
                continue;
            const localName = `import-${safeVisualLocalName(entry.name)}`;
            if (!force && fs_1.default.existsSync(path_1.default.join(vdir, localName))) {
                importDownloads.push({ slotKey, localName, idx: -1, entryName: entry.name });
            }
            else {
                const idx = dlOps.length;
                dlOps.push({ op: "download_base64", path: `${importBase}/${entry.name}` });
                importDownloads.push({ slotKey, localName, idx, entryName: entry.name });
            }
            break; // first matching stem wins
        }
    }
    // Queue import-screenshot downloads
    const importScreenshots = [];
    for (let i = 1; i <= 10; i++) {
        const stems = [`screenshot${i}`, `screenshot${String(i).padStart(2, "0")}`];
        for (const stem of stems) {
            const entry = importByStem.get(stem);
            if (!entry)
                continue;
            const localName = `import-${safeVisualLocalName(entry.name)}`;
            const info = { sortKey: `${String(i).padStart(3, "0")}-import`, localName, idx: -1, entryName: entry.name };
            if (!force && fs_1.default.existsSync(path_1.default.join(vdir, localName))) {
                importScreenshots.push(info);
            }
            else {
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
    const rxeaDownloads = [];
    for (const rf of rxeaFiles) {
        if (newFp[rf.fpKey] > 0) {
            const idx = dlOps.length;
            dlOps.push({ op: "download_base64", path: `${gameDataPath}/${rf.name}` });
            rxeaDownloads.push({ name: rf.name, idx });
        }
        else {
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
    let dlResults = [];
    if (dlOps.length > 0) {
        dlResults = await batchFtp(xboxIp, dlOps);
    }
    // ── Phase 4: Process downloaded data ─────────────────────────────────────────
    let prevManifest = {};
    if (fs_1.default.existsSync(manifestPath)) {
        try {
            prevManifest = JSON.parse(fs_1.default.readFileSync(manifestPath, "utf8"));
        }
        catch { }
    }
    const oldHashes = prevManifest._sourceHashes || {};
    const newHashes = { ...oldHashes };
    const getBufHash = (b) => crypto_1.default.createHash("md5").update(b).digest("hex");
    const m = {
        importCover: prevManifest.importCover || null, mediaCover: prevManifest.mediaCover || null,
        background: prevManifest.background || null, banner: prevManifest.banner || null,
        icon: prevManifest.icon || null, screenshots: prevManifest.screenshots || [], other: prevManifest.other || [],
    };
    const screenshotSort = [];
    function assetFor(localFileName) {
        return {
            rel: `games/${gameDataDir}/visual/${localFileName}`,
            ext: path_1.default.extname(localFileName).toLowerCase(),
        };
    }
    // Process import-file downloads
    for (const dl of importDownloads) {
        const lp = path_1.default.join(vdir, dl.localName);
        if (dl.idx < 0) {
            if (fs_1.default.existsSync(lp))
                m[dl.slotKey] = assetFor(dl.localName);
            continue;
        }
        const buf = bufFromBatchResult(dlResults, dl.idx);
        if (!buf || buf.length < 16)
            continue;
        const hash = getBufHash(buf);
        newHashes[dl.entryName] = hash;
        if (fs_1.default.existsSync(lp) && oldHashes[dl.entryName] === hash) {
            m[dl.slotKey] = assetFor(dl.localName);
            continue;
        }
        fs_1.default.writeFileSync(lp, buf);
        m[dl.slotKey] = assetFor(dl.localName);
    }
    // Process import-screenshot downloads
    for (const dl of importScreenshots) {
        const lp = path_1.default.join(vdir, dl.localName);
        const info = {
            sortKey: dl.sortKey,
            rel: assetFor(dl.localName).rel,
            ext: assetFor(dl.localName).ext,
            name: dl.entryName,
        };
        if (dl.idx < 0) {
            if (fs_1.default.existsSync(lp))
                screenshotSort.push(info);
            continue;
        }
        const buf = bufFromBatchResult(dlResults, dl.idx);
        if (!buf || buf.length < 16)
            continue;
        const hash = getBufHash(buf);
        newHashes[dl.entryName] = hash;
        if (fs_1.default.existsSync(lp) && oldHashes[dl.entryName] === hash) {
            screenshotSort.push(info);
            continue;
        }
        fs_1.default.writeFileSync(lp, buf);
        screenshotSort.push(info);
    }
    // RXEA decode helper
    async function processRxea(dlInfo, slotIdx, slotKey, localBase) {
        if (m[slotKey] && m[slotKey] !== prevManifest[slotKey])
            return; // satisfied by import
        const buf = bufFromBatchResult(dlResults, dlInfo.idx);
        if (buf && buf.length >= 2048) {
            const hash = getBufHash(buf);
            newHashes[dlInfo.name] = hash;
            const cachedFile = path_1.default.join(vdir, `rxea-${localBase}.png`);
            if (fs_1.default.existsSync(cachedFile) && oldHashes[dlInfo.name] === hash) {
                m[slotKey] = assetFor(`rxea-${localBase}.png`);
                return;
            }
            const decoded = await decodeRxeaBuffer(buf, titleId, dlInfo.name);
            if (decoded[slotIdx]) {
                fs_1.default.writeFileSync(cachedFile, decoded[slotIdx]);
                m[slotKey] = assetFor(`rxea-${localBase}.png`);
            }
        }
    }
    await processRxea(rxeaDownloads[0], 4, "background", "bk-background");
    await processRxea(rxeaDownloads[1], 2, "importCover", "gc-cover");
    await processRxea(rxeaDownloads[2], 0, "icon", "gl-icon");
    if (!m.banner || m.banner === prevManifest.banner)
        await processRxea(rxeaDownloads[2], 1, "banner", "gl-banner");
    // SS asset → screenshots (only if no import screenshots)
    if (screenshotSort.length === 0 || screenshotSort.every(s => s.name.startsWith("rxea-"))) {
        const dlInfo = rxeaDownloads[3];
        const buf = bufFromBatchResult(dlResults, dlInfo.idx);
        if (buf && buf.length >= 2048) {
            const hash = getBufHash(buf);
            newHashes[dlInfo.name] = hash;
            const skipDecode = oldHashes[dlInfo.name] === hash;
            let decoded = null;
            for (let si = 5; si <= 24; si++) {
                const localName = `rxea-screenshot${si - 4}.png`;
                const lp = path_1.default.join(vdir, localName);
                if (skipDecode && fs_1.default.existsSync(lp)) {
                    screenshotSort.push({ sortKey: `${String(si - 4).padStart(3, "0")}-rxea`, rel: assetFor(localName).rel, ext: ".png", name: localName });
                    continue;
                }
                if (!decoded && !skipDecode)
                    decoded = await decodeRxeaBuffer(buf, titleId, dlInfo.name);
                if (decoded && decoded[si]) {
                    fs_1.default.writeFileSync(lp, decoded[si]);
                    screenshotSort.push({ sortKey: `${String(si - 4).padStart(3, "0")}-rxea`, rel: assetFor(localName).rel, ext: ".png", name: localName });
                }
            }
        }
    }
    // GameAssetInfo.bin → CDN image fetches
    let assetInfo = { background: null, banner: null, icon: null, cover: null, screenshots: [] };
    let assetInfoUnchanged = false;
    {
        const buf = bufFromBatchResult(dlResults, gameAssetInfoIdx);
        if (buf && buf.length > 0) {
            const hash = getBufHash(buf);
            newHashes["GameAssetInfo.bin"] = hash;
            assetInfoUnchanged = (oldHashes["GameAssetInfo.bin"] === hash);
            assetInfo = parseGameAssetInfoXml(buf.toString("utf8"));
        }
    }
    async function pullCdnImage(slotKey, url, localBase) {
        if ((m[slotKey] && m[slotKey] !== prevManifest[slotKey]) || !url)
            return;
        const rawExt = path_1.default.extname(url).toLowerCase();
        const safeExt = [".jpg", ".jpeg", ".png", ".gif"].includes(rawExt) ? rawExt : ".jpg";
        const localName = `cdnasset-${localBase}${safeExt}`;
        const lp = path_1.default.join(vdir, localName);
        if (assetInfoUnchanged && oldHashes[url] === "cached" && fs_1.default.existsSync(lp)) {
            m[slotKey] = assetFor(localName);
            newHashes[url] = "cached";
            return;
        }
        const buf = await (0, httpHelper_1.fetchHttpImage)(url);
        if (!buf || buf.length < 100)
            return;
        const realExt = (0, httpHelper_1.imageExtFromMagic)(buf);
        const finalName = `cdnasset-${localBase}${realExt}`;
        fs_1.default.writeFileSync(path_1.default.join(vdir, finalName), buf);
        m[slotKey] = assetFor(finalName);
        newHashes[url] = "cached";
    }
    await pullCdnImage("background", assetInfo.background, "background");
    await pullCdnImage("banner", assetInfo.banner, "banner");
    await pullCdnImage("icon", assetInfo.icon, "icon");
    await pullCdnImage("importCover", assetInfo.cover, "cover");
    // Keep deduplication of screenshot sort by tracking name starts
    if (screenshotSort.length === 0 || screenshotSort.every(s => s.name.startsWith("rxea-"))) {
        for (let i = 0; i < assetInfo.screenshots.length; i++) {
            const url = assetInfo.screenshots[i];
            if (!url)
                continue;
            const rawExt = path_1.default.extname(url).toLowerCase();
            const safeExt = [".jpg", ".jpeg", ".png", ".gif"].includes(rawExt) ? rawExt : ".jpg";
            const localName = `cdnasset-screenshot${i + 1}${safeExt}`;
            const lp = path_1.default.join(vdir, localName);
            const info = {
                sortKey: `${String(i + 1).padStart(3, "0")}-cdn`,
                rel: assetFor(localName).rel, ext: safeExt, name: `Screenshot${i + 1}${safeExt}`,
            };
            if (assetInfoUnchanged && oldHashes[url] === "cached" && fs_1.default.existsSync(lp)) {
                screenshotSort.push(info);
                newHashes[url] = "cached";
                continue;
            }
            const buf = await (0, httpHelper_1.fetchHttpImage)(url);
            if (!buf || buf.length < 100)
                continue;
            const realExt = (0, httpHelper_1.imageExtFromMagic)(buf);
            const finalName = `cdnasset-screenshot${i + 1}${realExt}`;
            fs_1.default.writeFileSync(path_1.default.join(vdir, finalName), buf);
            screenshotSort.push({ ...info, rel: assetFor(finalName).rel, ext: realExt });
            newHashes[url] = "cached";
        }
    }
    // GameCoverInfo.bin → mediaCover (XboxUnity best cover)
    {
        const buf = bufFromBatchResult(dlResults, gameCoverInfoIdx);
        if (buf && buf.length > 0) {
            try {
                const hash = getBufHash(buf);
                newHashes["GameCoverInfo.bin"] = hash;
                const coverUnchanged = oldHashes["GameCoverInfo.bin"] === hash;
                let entries;
                try {
                    entries = JSON.parse(buf.toString("utf8"));
                }
                catch {
                    entries = null;
                }
                if (Array.isArray(entries) && entries.length > 0) {
                    const sorted = [...entries].sort((a, b) => {
                        if (!!a.official !== !!b.official)
                            return a.official ? -1 : 1;
                        return (Number(b.rating) || 0) - (Number(a.rating) || 0);
                    });
                    for (const row of sorted) {
                        const url = (row.front || row.thumbnail || row.url || "").trim();
                        if (!url)
                            continue;
                        const rawExt = path_1.default.extname(url).toLowerCase();
                        const safeExt = [".jpg", ".jpeg", ".png", ".gif"].includes(rawExt) ? rawExt : ".jpg";
                        const localName = `cdnasset-xboxunity-cover${safeExt}`;
                        const lp = path_1.default.join(vdir, localName);
                        if (coverUnchanged && oldHashes[url] === "cached" && fs_1.default.existsSync(lp)) {
                            m.mediaCover = assetFor(localName);
                            newHashes[url] = "cached";
                            break;
                        }
                        const imgBuf = await (0, httpHelper_1.fetchHttpImage)(url);
                        if (!imgBuf || imgBuf.length < 100)
                            continue;
                        const realExt = (0, httpHelper_1.imageExtFromMagic)(imgBuf);
                        const finalName = `cdnasset-xboxunity-cover${realExt}`;
                        fs_1.default.writeFileSync(path_1.default.join(vdir, finalName), imgBuf);
                        m.mediaCover = assetFor(finalName);
                        newHashes[url] = "cached";
                        break;
                    }
                }
            }
            catch { /* GameCoverInfo parse error */ }
        }
    }
    // Make sure we deduplicate screenshots correctly
    const dedupedScreenshots = [];
    const seenScreens = new Set();
    screenshotSort.sort((a, b) => a.sortKey.localeCompare(b.sortKey));
    for (const s of screenshotSort) {
        if (!seenScreens.has(s.rel)) {
            seenScreens.add(s.rel);
            dedupedScreenshots.push({ rel: s.rel, ext: s.ext, name: s.name });
        }
    }
    m.screenshots = dedupedScreenshots;
    m._sourceFingerprints = newFp;
    m._sourceHashes = newHashes;
    fs_1.default.writeFileSync(manifestPath, JSON.stringify(m, null, 2), "utf8");
}
/**
 * Sync Aurora cover art for a single game.  Downloads GameCoverInfo.bin from
 * the console, picks the best cover URL, fetches it via HTTP, and caches
 * locally.  Falls back to Aurora Media directory flat covers if needed.
 * Uses the Go backend batch endpoint for FTP operations.
 */
async function syncAuroraGameCoverAssets(xboxIp, auroraRoot, mediaDir, titleId, gameDataDir, cacheRoot, force) {
    const gdir = (0, auroraLibraryCache_1.gameCacheDir)(cacheRoot, gameDataDir);
    fs_1.default.mkdirSync(gdir, { recursive: true });
    const remoteBin = `${auroraRoot}/Data/GameData/${gameDataDir}/GameCoverInfo.bin`;
    // Phase 1: Check remote GameCoverInfo.bin size
    const sizeResults = await batchFtp(xboxIp, [{ op: "size", path: remoteBin }]);
    let remoteSz = -1;
    if (sizeResults[0] && sizeResults[0].ok)
        remoteSz = Number(sizeResults[0].data);
    const binPath = path_1.default.join(gdir, "GameCoverInfo.bin");
    let needBin = force;
    if (remoteSz >= 0) {
        if (!fs_1.default.existsSync(binPath))
            needBin = true;
        else if (fs_1.default.statSync(binPath).size !== remoteSz)
            needBin = true;
    }
    if (needBin && remoteSz >= 0) {
        const dlResults = await batchFtp(xboxIp, [{ op: "download_base64", path: remoteBin }]);
        const buf = bufFromBatchResult(dlResults, 0);
        if (buf && buf.length > 0)
            fs_1.default.writeFileSync(binPath, buf);
    }
    let entries = [];
    if (fs_1.default.existsSync(binPath)) {
        try {
            const parsed = JSON.parse(fs_1.default.readFileSync(binPath, "utf8"));
            if (Array.isArray(parsed))
                entries = parsed;
        }
        catch {
            entries = [];
        }
    }
    // Media fallback: try downloading cover from Aurora Media directory (all extensions in one batch)
    const tryMediaFallback = async () => {
        const exts = ["jpg", "jpeg", "png", "dds"];
        const mediaOps = exts.map((x) => ({ op: "download_base64", path: `${mediaDir}/${titleId}GC.${x}` }));
        const mediaRes = await batchFtp(xboxIp, mediaOps);
        for (let i = 0; i < mediaRes.length; i++) {
            const buf = bufFromBatchResult(mediaRes, i);
            if (!buf || buf.length < 100)
                continue;
            const ext = (0, httpHelper_1.imageExtFromMagic)(buf);
            const primaryName = `cover-primary${ext}`;
            fs_1.default.writeFileSync(path_1.default.join(gdir, primaryName), buf);
            fs_1.default.writeFileSync(path_1.default.join(gdir, "cover-files.json"), JSON.stringify({
                primaryFile: primaryName,
                bestUrl: `aurora:MediaGC.${exts[i]}`,
                gameCoverInfoEntryCount: entries.length,
            }, null, 2), "utf8");
            return true;
        }
        return false;
    };
    const withUrl = entries.filter((e) => e && (e.front || e.thumbnail || e.url));
    if (withUrl.length === 0) {
        if (await tryMediaFallback())
            emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
        return;
    }
    const best = withUrl.reduce((prev, curr) => {
        if (curr.official && !prev.official)
            return curr;
        if (!curr.official && prev.official)
            return prev;
        return (curr.rating || 0) >= (prev.rating || 0) ? curr : prev;
    });
    const bestUrl = best.front || best.thumbnail || best.url;
    if (!bestUrl || typeof bestUrl !== "string") {
        if (await tryMediaFallback())
            emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
        return;
    }
    let prevMeta = {};
    try {
        prevMeta = JSON.parse(fs_1.default.readFileSync(path_1.default.join(gdir, "cover-files.json"), "utf8"));
    }
    catch { /* none */ }
    const oldPrimaryHash = prevMeta.primaryHash;
    if (prevMeta.bestUrl === bestUrl &&
        prevMeta.primaryFile &&
        fs_1.default.existsSync(path_1.default.join(gdir, prevMeta.primaryFile))) {
        // Check hash skipping only if force is explicitly bypassing things,
        // actually bestUrl match is already quite strong.
        // Wait, let's hash it if it's new.
        if (!force || (oldPrimaryHash && force)) {
            // if we have oldPrimaryHash, we can conditionally skip fetch, but bestUrl usually maps 1:1 to image content.
            // We'll skip fetching if bestUrl matches, EVEN WITH FORCE, assuming the bestUrl image did not change on the remote!
            emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
            return;
        }
    }
    const buf = await (0, httpHelper_1.fetchHttpImage)(bestUrl);
    if (!buf || buf.length < 100) {
        if (await tryMediaFallback())
            emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
        return;
    }
    const ext = (0, httpHelper_1.imageExtFromMagic)(buf);
    const primaryName = `cover-primary${ext}`;
    const newPrimaryHash = crypto_1.default.createHash("md5").update(buf).digest("hex");
    if (oldPrimaryHash === newPrimaryHash && fs_1.default.existsSync(path_1.default.join(gdir, prevMeta.primaryFile))) {
        // it was exactly the same image, keep it
    }
    else {
        fs_1.default.writeFileSync(path_1.default.join(gdir, primaryName), buf);
    }
    fs_1.default.writeFileSync(path_1.default.join(gdir, "cover-files.json"), JSON.stringify({
        primaryFile: primaryName,
        bestUrl,
        gameCoverInfoEntryCount: entries.length,
        primaryHash: newPrimaryHash,
    }, null, 2), "utf8");
    emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
}
