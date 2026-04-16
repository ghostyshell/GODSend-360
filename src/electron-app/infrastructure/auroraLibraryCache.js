"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.setActiveAuroraCacheRoot = setActiveAuroraCacheRoot;
exports.getActiveAuroraCacheRoot = getActiveAuroraCacheRoot;
exports.slugPart = slugPart;
exports.getAuroraLibraryCacheRoot = getAuroraLibraryCacheRoot;
exports.metaPath = metaPath;
exports.databasesDir = databasesDir;
exports.gameCacheDir = gameCacheDir;
exports.readMeta = readMeta;
exports.writeMeta = writeMeta;
exports.safeFileUnderRoot = safeFileUnderRoot;
exports.contentDbPath = contentDbPath;
exports.settingsDbPath = settingsDbPath;
const path_1 = __importDefault(require("path"));
const fs_1 = __importDefault(require("fs"));
let activeCacheRoot = null;
function setActiveAuroraCacheRoot(root) {
    activeCacheRoot = root && typeof root === "string" ? root : null;
}
function getActiveAuroraCacheRoot() {
    return activeCacheRoot;
}
function slugPart(s) {
    return String(s || "")
        .replace(/\\/g, "/")
        .replace(/[^a-zA-Z0-9._-]+/g, "_")
        .replace(/_+/g, "_")
        .slice(0, 120) || "default";
}
function getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot) {
    const base = path_1.default.join(app.getPath("userData"), "aurora-library-cache");
    const key = `${slugPart(xboxIp)}__${slugPart(auroraRoot)}`;
    return path_1.default.join(base, key);
}
function metaPath(cacheRoot) {
    return path_1.default.join(cacheRoot, "meta.json");
}
function databasesDir(cacheRoot) {
    return path_1.default.join(cacheRoot, "databases");
}
function gameCacheDir(cacheRoot, gameDataDir) {
    return path_1.default.join(cacheRoot, "games", gameDataDir);
}
function readMeta(cacheRoot) {
    try {
        const p = metaPath(cacheRoot);
        if (!fs_1.default.existsSync(p))
            return null;
        return JSON.parse(fs_1.default.readFileSync(p, "utf8"));
    }
    catch {
        return null;
    }
}
function writeMeta(cacheRoot, obj) {
    fs_1.default.mkdirSync(cacheRoot, { recursive: true });
    fs_1.default.writeFileSync(metaPath(cacheRoot), JSON.stringify(obj, null, 2), "utf8");
}
function safeFileUnderRoot(root, relUnix) {
    const raw = String(relUnix || "").replace(/\\/g, "/").replace(/^\/+/, "");
    const parts = raw.split("/").filter(Boolean);
    for (const p of parts) {
        if (p === "..")
            return null;
    }
    const full = path_1.default.resolve(root, ...parts);
    const base = path_1.default.resolve(root);
    const baseSep = base.endsWith(path_1.default.sep) ? base : base + path_1.default.sep;
    if (full !== base && !full.startsWith(baseSep)) {
        return null;
    }
    return full;
}
function contentDbPath(cacheRoot) {
    return path_1.default.join(databasesDir(cacheRoot), "content.db");
}
function settingsDbPath(cacheRoot) {
    return path_1.default.join(databasesDir(cacheRoot), "settings.db");
}
