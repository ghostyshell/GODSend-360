const path = require("path");
const fs = require("fs");

/** @type {string|null} */
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

/**
 * Per-console Aurora library cache under userData.
 * @param {import("electron").App} app
 */
function getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot) {
  const base = path.join(app.getPath("userData"), "aurora-library-cache");
  const key = `${slugPart(xboxIp)}__${slugPart(auroraRoot)}`;
  return path.join(base, key);
}

function metaPath(cacheRoot) {
  return path.join(cacheRoot, "meta.json");
}

function databasesDir(cacheRoot) {
  return path.join(cacheRoot, "databases");
}

function gameCacheDir(cacheRoot, gameDataDir) {
  return path.join(cacheRoot, "games", gameDataDir);
}

function readMeta(cacheRoot) {
  try {
    const p = metaPath(cacheRoot);
    if (!fs.existsSync(p)) return null;
    return JSON.parse(fs.readFileSync(p, "utf8"));
  } catch {
    return null;
  }
}

function writeMeta(cacheRoot, obj) {
  fs.mkdirSync(cacheRoot, { recursive: true });
  fs.writeFileSync(metaPath(cacheRoot), JSON.stringify(obj, null, 2), "utf8");
}

function safeFileUnderRoot(root, relUnix) {
  const raw = String(relUnix || "").replace(/\\/g, "/").replace(/^\/+/, "");
  const parts = raw.split("/").filter(Boolean);
  for (const p of parts) {
    if (p === "..") return null;
  }
  const full = path.resolve(root, ...parts);
  const base = path.resolve(root);
  const baseSep = base.endsWith(path.sep) ? base : base + path.sep;
  if (full !== base && !full.startsWith(baseSep)) {
    return null;
  }
  return full;
}

function contentDbPath(cacheRoot) {
  return path.join(databasesDir(cacheRoot), "content.db");
}

function settingsDbPath(cacheRoot) {
  return path.join(databasesDir(cacheRoot), "settings.db");
}

module.exports = {
  setActiveAuroraCacheRoot,
  getActiveAuroraCacheRoot,
  getAuroraLibraryCacheRoot,
  readMeta,
  writeMeta,
  databasesDir,
  gameCacheDir,
  safeFileUnderRoot,
  contentDbPath,
  settingsDbPath,
};
