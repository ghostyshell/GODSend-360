"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.buildAuroraGamesFromDbBuffers = buildAuroraGamesFromDbBuffers;
exports.readContentScanRowsFromBuffer = readContentScanRowsFromBuffer;
exports.readScanRowsFromSettingsBuffer = readScanRowsFromSettingsBuffer;
exports.probeScanPathDrives = probeScanPathDrives;
exports.xboxBuildGameNameMap = xboxBuildGameNameMap;
const electron_1 = require("electron");
const path_1 = __importDefault(require("path"));
const fs_1 = __importDefault(require("fs"));
const sqlHelper_1 = require("../infrastructure/sqlHelper");
const backendHttp_1 = require("../infrastructure/backendHttp");
/**
 * Parse Aurora SQLite DB buffers and return the games list used by the Xbox
 * Library view.
 */
async function buildAuroraGamesFromDbBuffers(contentBuf, settingsBuf, scanDriveMap) {
    const SQL = await (0, sqlHelper_1.getSqlJs)();
    const cdb = new SQL.Database(new Uint8Array(contentBuf));
    const sdb = new SQL.Database(new Uint8Array(settingsBuf));
    const queryDb = (db, sql) => {
        // Use prepare/step API directly here — avoids shell-exec false-positive patterns
        const stmt = db.prepare(sql);
        const rows = [];
        while (stmt.step())
            rows.push(stmt.getAsObject());
        stmt.free();
        return rows;
    };
    const itemRows = queryDb(cdb, `
    SELECT Id, TitleId, MediaId, TitleName, Description,
           Publisher, Developer, LiveRating, LiveRaters,
           ReleaseDate, Directory, ScanPathId,
           DiscNum, DiscsInSet, FileType, ContentType
    FROM ContentItems
    ORDER BY TitleName
  `);
    cdb.close();
    const hiddenRows = queryDb(sdb, "SELECT DISTINCT ContentId FROM UserHidden");
    const favRows = queryDb(sdb, "SELECT DISTINCT ContentId FROM UserFavorites");
    const recentRows = queryDb(sdb, `
    SELECT ContentId,
           MAX(DateTime)  AS LastPlayed,
           COUNT(*)       AS TimesPlayed
    FROM UserRecentGames
    GROUP BY ContentId
  `);
    sdb.close();
    const hiddenIds = new Set(hiddenRows.map((h) => Number(h.ContentId)));
    const favoriteIds = new Set(favRows.map((f) => Number(f.ContentId)));
    const recentMap = new Map(recentRows.map((r) => [Number(r.ContentId), {
            lastPlayed: (0, sqlHelper_1.filetimeToDateStr)(r.LastPlayed),
            timesPlayed: Number(r.TimesPlayed),
        }]));
    const games = [];
    for (const g of itemRows) {
        const contentId = Number(g.Id);
        if (hiddenIds.has(contentId))
            continue;
        const titleIdInt = Number(g.TitleId) >>> 0;
        const titleId = titleIdInt.toString(16).toUpperCase().padStart(8, "0");
        if (titleId === "00000000")
            continue;
        const sourceDrive = scanDriveMap.get(Number(g.ScanPathId)) || "";
        const gameDataDir = `${titleId}_${contentId.toString(16).toUpperCase().padStart(8, "0")}`;
        const recent = recentMap.get(contentId);
        games.push({
            contentId,
            titleId,
            name: String(g.TitleName || titleId),
            description: String(g.Description || ""),
            publisher: String(g.Publisher || ""),
            developer: String(g.Developer || ""),
            liveRating: g.LiveRating != null ? Number(g.LiveRating).toFixed(1) : "",
            liveRaters: g.LiveRaters != null ? Number(g.LiveRaters).toLocaleString("en-US") : "",
            releaseDate: String(g.ReleaseDate || ""),
            directory: String(g.Directory || ""),
            discNum: Number(g.DiscNum || 1),
            discsInSet: Number(g.DiscsInSet || 1),
            isFavorite: favoriteIds.has(contentId),
            timesPlayed: recent?.timesPlayed ?? 0,
            lastPlayed: recent?.lastPlayed ?? null,
            sourceDrive,
            gameDataDir,
            scanPathId: Number(g.ScanPathId) || 0,
            mediaId: g.MediaId != null ? Number(g.MediaId) : null,
            fileType: g.FileType != null ? Number(g.FileType) : null,
            contentType: g.ContentType != null ? Number(g.ContentType) : null,
        });
    }
    return games;
}
async function readContentScanRowsFromBuffer(contentBuf) {
    const SQL = await (0, sqlHelper_1.getSqlJs)();
    const cdb = new SQL.Database(new Uint8Array(contentBuf));
    const stmt = cdb.prepare("SELECT ScanPathId, Directory FROM ContentItems");
    const rows = [];
    while (stmt.step())
        rows.push(stmt.getAsObject());
    stmt.free();
    cdb.close();
    return rows;
}
async function readScanRowsFromSettingsBuffer(settingsBuf) {
    const SQL = await (0, sqlHelper_1.getSqlJs)();
    const sdb = new SQL.Database(new Uint8Array(settingsBuf));
    const stmt = sdb.prepare("SELECT Id, Path FROM ScanPaths");
    const rows = [];
    while (stmt.step())
        rows.push(stmt.getAsObject());
    stmt.free();
    sdb.close();
    return rows;
}
async function probeScanPathDrives(xboxIp, scanRows, contentRows) {
    const knownDrives = ["Hdd1", "Usb0", "Usb1", "Usb2", "HddX"];
    const scanDriveMap = new Map();
    const sampleDirByScanId = new Map();
    for (const c of contentRows || []) {
        const sid = Number(c.ScanPathId) || 0;
        if (!sid || sampleDirByScanId.has(sid))
            continue;
        const dir = String(c.Directory || "").replace(/\\/g, "/").replace(/^\/+|\/+$/g, "");
        if (dir)
            sampleDirByScanId.set(sid, dir);
    }
    const scanPathById = new Map(scanRows.map((s) => [
        Number(s.Id),
        String(s.Path || "").replace(/\\/g, "/").replace(/^\/+|\/+$/g, ""),
    ]));
    // Build one big batch: for each scanPath × drive combo, cd / then cd drive
    // then cd each segment then pwd.  Failed cd returns error without closing the
    // connection, and cd / resets for the next candidate.
    const ops = [];
    const probeMap = [];
    for (const [scanId, scanPath] of scanPathById) {
        const probePath = sampleDirByScanId.get(scanId) || scanPath;
        const segments = probePath.split("/").filter(Boolean);
        if (segments.length === 0)
            continue;
        for (const drive of knownDrives) {
            ops.push({ op: "cd", path: "/" });
            ops.push({ op: "cd", path: drive });
            for (const seg of segments)
                ops.push({ op: "cd", path: seg });
            const pwdIdx = ops.length;
            ops.push({ op: "pwd" });
            probeMap.push({ scanId, drive, pwdIdx, segments });
        }
    }
    if (ops.length === 0)
        return scanDriveMap;
    const res = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops }, 60000);
    const results = res.results || [];
    for (const { scanId, drive, pwdIdx, segments } of probeMap) {
        if (scanDriveMap.has(scanId))
            continue; // already found for this scanId
        const r = results[pwdIdx];
        if (r && r.ok && r.data) {
            const pwd = String(r.data).replace(/\\/g, "/");
            const expected = `/${drive}/${segments.join("/")}`;
            if (pwd.replace(/\/+$/, "").toLowerCase() === expected.toLowerCase()) {
                scanDriveMap.set(scanId, drive);
            }
        }
    }
    return scanDriveMap;
}
function xboxBuildGameNameMap() {
    const map = new Map();
    const cacheDir = electron_1.app.isPackaged
        ? path_1.default.join(process.resourcesPath, "cache")
        : path_1.default.join(__dirname, "..", "..", "..", "cache");
    for (const file of ["xbox360.json", "xbla.json", "games.json", "digital.json", "xbox.json"]) {
        try {
            const raw = fs_1.default.readFileSync(path_1.default.join(cacheDir, file), "utf8");
            const data = JSON.parse(raw);
            const items = Array.isArray(data) ? data : Object.values(data).flat();
            for (const item of items) {
                if (!item || typeof item !== "object")
                    continue;
                const titleId = String(item.titleId || item.TitleId || item.title_id || "").toUpperCase().trim();
                const name = String(item.title || item.name || item.Title || item.Name || "").trim();
                if (titleId && name && /^[0-9A-F]{8}$/.test(titleId))
                    map.set(titleId, name);
            }
        }
        catch { /* cache file absent or unparseable — skip */ }
    }
    return map;
}
