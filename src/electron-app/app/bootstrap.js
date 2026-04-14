const { app, BrowserWindow, ipcMain, dialog, protocol } = require("electron");
/** Electron's net (for `fetch`); do not shadow Node's `require("net")` TCP module. */
const electronNet = require("electron").net;
const { pathToFileURL } = require("url");
const {
  getLogInfo,
  openLogsFolder,
  appendAppEvent,
} = require("../infrastructure/serverLog");
const http = require("http");
const https = require("https");
const net = require("net");
const os = require("os");
const path = require("path");
const fs = require("fs");
const ftp = require("basic-ftp");
const { Writable, Readable } = require("stream");

const {
  getFirstValidIconPath,
  getWritableRuntimeRoot,
  getAuroraScriptsPath,
} = require("../infrastructure/fileSystem");
const { createTray } = require("../infrastructure/electronTray");
const {
  getConfiguredTransferFolder,
  getDefaultTransferFolder,
  getConfiguredROMPath,
  getDefaultROMPath,
  getConfiguredIAEmail,
  getConfiguredIAScreenname,
  getConfiguredIACookie,
  getConfiguredServerPort,
  getConfiguredXboxIP,
  getConfiguredFtpUser,
  getConfiguredFtpPassword,
  getDefaultFtpScriptsPath,
  getConfiguredFtpScriptsPath,
  getConfiguredDefaultXboxDrive,
  getConfiguredAria2ListenPort,
  getConfiguredAria2DhtPort,
  getConfiguredAuroraLibrarySources,
  writeConfig,
} = require("../services/settingsService");
const {
  setMainWindowRef,
  getProcess,
  getOutputBuffer,
  addOutputLine,
  startGodsend,
  stopGodsend,
  restartGodsendIfRunning,
  loginInternetArchive,
} = require("../services/backendClient");
const {
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
} = require("../infrastructure/auroraLibraryCache");

let mainWindow = null;
let isQuitting = false;

/** Prefer the tray BrowserWindow; fall back if ref is stale (e.g. during teardown). */
function getWebContentsForPush() {
  if (mainWindow && !mainWindow.isDestroyed()) return mainWindow.webContents;
  for (const w of BrowserWindow.getAllWindows()) {
    if (!w.isDestroyed() && w.webContents) return w.webContents;
  }
  return null;
}

function createMainWindow() {
  const windowIconPath = getFirstValidIconPath();
  mainWindow = new BrowserWindow({
    width: 900,
    height: 600,
    show: true,
    autoHideMenuBar: true,
    icon: windowIconPath || undefined,
    webPreferences: {
      preload: path.join(__dirname, "..", "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  if (process.env.VITE_DEV_SERVER_URL) {
    mainWindow.loadURL(process.env.VITE_DEV_SERVER_URL);
  } else {
    mainWindow.loadFile(path.join(__dirname, "..", "renderer-dist", "index.html"));
  }

  mainWindow.on("minimize", (event) => {
    event.preventDefault();
    mainWindow.hide();
  });

  mainWindow.on("close", (event) => {
    if (!isQuitting) {
      event.preventDefault();
      mainWindow.hide();
    }
  });

  setMainWindowRef(mainWindow);
}

// Returns this machine's first non-loopback IPv4 address, or null if not found.
function getLocalIPAddress() {
  const ifaces = os.networkInterfaces();
  for (const name of Object.keys(ifaces)) {
    for (const iface of ifaces[name]) {
      if (iface.family === "IPv4" && !iface.internal) {
        return iface.address;
      }
    }
  }
  return null;
}

/**
 * Parse Aurora SQLite DB buffers and build the library list (same shape as before).
 * @param {Buffer} contentBuf
 * @param {Buffer} settingsBuf
 * @param {Map<number, string>} scanDriveMap
 */
async function buildAuroraGamesFromDbBuffers(contentBuf, settingsBuf, scanDriveMap) {
  const SQL = await getSqlJs();
  const cdb = new SQL.Database(new Uint8Array(contentBuf));
  const sdb = new SQL.Database(new Uint8Array(settingsBuf));

  const itemRows = sqlRows(cdb.exec(`
        SELECT Id, TitleId, MediaId, TitleName, Description,
               Publisher, Developer, LiveRating, LiveRaters,
               ReleaseDate, Directory, ScanPathId,
               DiscNum, DiscsInSet, FileType, ContentType
        FROM ContentItems
        ORDER BY TitleName
      `));
  cdb.close();

  const hiddenRows  = sqlRows(sdb.exec("SELECT DISTINCT ContentId FROM UserHidden"));
  const favRows     = sqlRows(sdb.exec("SELECT DISTINCT ContentId FROM UserFavorites"));
  const recentRows  = sqlRows(sdb.exec(`
        SELECT ContentId,
               MAX(DateTime)  AS LastPlayed,
               COUNT(*)       AS TimesPlayed
        FROM UserRecentGames
        GROUP BY ContentId
      `));
  sdb.close();

  const hiddenIds   = new Set(hiddenRows.map((h) => Number(h.ContentId)));
  const favoriteIds = new Set(favRows.map((f) => Number(f.ContentId)));
  const recentMap   = new Map(
    recentRows.map((r) => [Number(r.ContentId), {
      lastPlayed:  filetimeToDateStr(r.LastPlayed),
      timesPlayed: Number(r.TimesPlayed),
    }])
  );

  const games = [];
  for (const g of itemRows) {
    const contentId = Number(g.Id);
    if (hiddenIds.has(contentId)) continue;

    const titleIdInt = Number(g.TitleId) >>> 0;   // unsigned 32-bit (handles signed DB values like 0xC0DE9999)
    const titleId    = titleIdInt.toString(16).toUpperCase().padStart(8, "0");
    if (titleId === "00000000") continue;

    const sourceDrive = scanDriveMap.get(Number(g.ScanPathId)) || "";
    const gameDataDir = `${titleId}_${contentId.toString(16).toUpperCase().padStart(8, "0")}`;
    const recent = recentMap.get(contentId);

    games.push({
      contentId,
      titleId,
      name:        String(g.TitleName   || titleId),
      description: String(g.Description || ""),
      publisher:   String(g.Publisher   || ""),
      developer:   String(g.Developer   || ""),
      liveRating:  g.LiveRating  != null ? Number(g.LiveRating).toFixed(1)              : "",
      liveRaters:  g.LiveRaters  != null ? Number(g.LiveRaters).toLocaleString("en-US") : "",
      releaseDate: String(g.ReleaseDate  || ""),
      directory:   String(g.Directory    || ""),
      discNum:     Number(g.DiscNum      || 1),
      discsInSet:  Number(g.DiscsInSet   || 1),
      isFavorite:  favoriteIds.has(contentId),
      timesPlayed: recent?.timesPlayed ?? 0,
      lastPlayed:  recent?.lastPlayed  ?? null,
      sourceDrive,
      gameDataDir,
      scanPathId:  Number(g.ScanPathId) || 0,
      mediaId:     g.MediaId != null ? Number(g.MediaId) : null,
      fileType:    g.FileType != null ? Number(g.FileType) : null,
      contentType: g.ContentType != null ? Number(g.ContentType) : null,
    });
  }
  return games;
}

/**
 * Aurora's FTP server returns the root listing for any absolute path that
 * does not exist (instead of failing), so `cd /Hdd1/foo` always "succeeds"
 * even when `/Hdd1/foo` is not real. The only reliable way to verify a path
 * is to walk it segment-by-segment with relative `cd` and check `pwd` after.
 *
 * @returns {Promise<Map<number, string>>}
 */
async function probeScanPathDrives(client, scanRows, contentRows) {
  const knownDrives = ["Hdd1", "Usb0", "Usb1", "Usb2", "HddX"];
  const scanDriveMap = new Map();

  const sampleDirByScanId = new Map();
  for (const c of contentRows || []) {
    const sid = Number(c.ScanPathId) || 0;
    if (!sid || sampleDirByScanId.has(sid)) continue;
    const dir = String(c.Directory || "").replace(/\\/g, "/").replace(/^\/+|\/+$/g, "");
    if (dir) sampleDirByScanId.set(sid, dir);
  }

  const scanPathById = new Map(
    scanRows.map((s) => [
      Number(s.Id),
      String(s.Path || "").replace(/\\/g, "/").replace(/^\/+|\/+$/g, ""),
    ])
  );

  async function walkRel(segments) {
    for (const seg of segments) {
      if (!seg) continue;
      await client.cd(seg);
    }
  }

  for (const [scanId, scanPath] of scanPathById) {
    const probePath = sampleDirByScanId.get(scanId) || scanPath;
    const segments = probePath.split("/").filter(Boolean);
    if (segments.length === 0) continue;

    for (const drive of knownDrives) {
      try {
        await client.cd("/");
        await client.cd(drive);
        await walkRel(segments);
        const pwd = (await client.pwd()).replace(/\\/g, "/");
        const expected = `/${drive}/${segments.join("/")}`;
        if (pwd.replace(/\/+$/, "").toLowerCase() === expected.toLowerCase()) {
          scanDriveMap.set(scanId, drive);
          break;
        }
      } catch { /* try next drive */ }
    }
  }
  return scanDriveMap;
}

async function readContentScanRowsFromBuffer(contentBuf) {
  const SQL = await getSqlJs();
  const cdb = new SQL.Database(new Uint8Array(contentBuf));
  const rows = sqlRows(cdb.exec("SELECT ScanPathId, Directory FROM ContentItems"));
  cdb.close();
  return rows;
}

async function readScanRowsFromSettingsBuffer(settingsBuf) {
  const SQL = await getSqlJs();
  const sdb = new SQL.Database(new Uint8Array(settingsBuf));
  const scanRows = sqlRows(sdb.exec("SELECT Id, Path FROM ScanPaths"));
  sdb.close();
  return scanRows;
}

function imageExtFromMagic(buf) {
  if (!buf || buf.length < 4) return ".jpg";
  if (buf[0] === 0xFF && buf[1] === 0xD8) return ".jpg";
  if (buf[0] === 0x89 && buf[1] === 0x50 && buf[2] === 0x4E && buf[3] === 0x47) return ".png";
  if (buf[0] === 0x47 && buf[1] === 0x49 && buf[2] === 0x46) return ".gif";
  return ".jpg";
}

function auroraCdnUrl(relUnix) {
  const r = String(relUnix || "").replace(/\\/g, "/").replace(/^\/+/, "");
  return `godsend-aurora://cdn/${r}`;
}

/** Download a single remote file; returns null if missing or empty. */
async function ftpTryDownloadFile(client, remotePath) {
  try {
    const chunks = [];
    await client.downloadTo(
      new Writable({ write(c, _, cb) { chunks.push(c); cb(); } }),
      remotePath
    );
    const buf = Buffer.concat(chunks);
    return buf.length > 0 ? buf : null;
  } catch {
    return null;
  }
}

function safeVisualLocalName(name) {
  return String(name || "").replace(/[^a-zA-Z0-9._-]+/g, "_").slice(0, 180) || "file";
}

/**
 * Aurora flat Media filenames: {TitleId}{SUFFIX}.ext — e.g. GC cover, BK background.
 * See also User/Import/{TitleId}/ layout (banner.jpg, …) in ConsoleMods wiki.
 */
function classifyFlatMediaSuffix(titleId, filename) {
  const base = path.basename(filename);
  const tid  = titleId.toUpperCase();
  const nu   = base.toUpperCase();
  if (!nu.startsWith(tid)) return "other";
  const dot = nu.lastIndexOf(".");
  const stem = dot >= tid.length ? nu.slice(tid.length, dot) : nu.slice(tid.length);
  if (!stem) return "other";
  if (stem === "GC") return "cover";
  if (stem === "BK" || stem === "BG") return "background";
  if (stem === "BN" || stem === "BA") return "banner";
  if (stem === "IC" || stem === "IL" || stem === "IS") return "icon";
  if (/^SS\d*$/i.test(stem) || /^SC\d*$/i.test(stem)) return "screenshot";
  return "other";
}

function emptyTitleVisualsPayload() {
  return {
    cover:         null,
    background:    null,
    banner:        null,
    icon:          null,
    screenshots:   [],
    other:         [],
  };
}

function emitAuroraTitleVisualEvents(titleId, gameDataDir, cacheRoot) {
  const wc = getWebContentsForPush();
  if (!wc) return;
  const manifestPath = path.join(gameCacheDir(cacheRoot, gameDataDir), "visual-manifest.json");
  if (!fs.existsSync(manifestPath)) {
    wc.send("xbox-title-visuals", {
      titleId,
      visuals: emptyTitleVisualsPayload(),
    });
    return;
  }
  let m;
  try {
    m = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  } catch {
    wc.send("xbox-title-visuals", {
      titleId,
      visuals: emptyTitleVisualsPayload(),
    });
    return;
  }
  const toAsset = (o) =>
    o && o.rel ? { src: auroraCdnUrl(o.rel), ext: o.ext || "" } : null;
  wc.send("xbox-title-visuals", {
    titleId,
    visuals: {
      coverIsBooklet: Boolean(m.importCover && m.importCover.rel),
      cover:        toAsset(m.importCover || m.mediaCover),
      background:   toAsset(m.background),
      banner:       toAsset(m.banner),
      icon:         toAsset(m.icon),
      screenshots:  Array.isArray(m.screenshots)
        ? m.screenshots.map((s) => ({
          src:  s.rel ? auroraCdnUrl(s.rel) : "",
          ext:  s.ext || "",
          name: s.name || "",
        })).filter((s) => s.src)
        : [],
      other:        Array.isArray(m.other)
        ? m.other.map((o) => ({
          src:  o.rel ? auroraCdnUrl(o.rel) : "",
          ext:  o.ext || "",
          name: o.name || "",
        })).filter((o) => o.src)
        : [],
    },
  });
}

/**
 * Sync banner / background / icon / cover / screenshots for one title.
 *
 * Priority order (highest wins per slot):
 *   1. User/Import/{TitleId}/ — files placed there for Aurora to import.
 *   2. GameAssetInfo.bin XML  — Xbox Live CDN URLs (download.xbox.com).
 *   3. GameCoverInfo.bin JSON — XboxUnity cover entry (mediaCover fallback).
 *
 * Aurora's .asset files use the RXEA GPU-texture format (big-endian DXT)
 * and cannot be decoded without a platform DXT decompressor; they are ignored.
 */
async function syncAuroraTitleVisualAssets(
  client,
  auroraRoot,
  titleId,
  gameDataDir,
  cacheRoot,
  force
) {
  const gdir         = gameCacheDir(cacheRoot, gameDataDir);
  const vdir         = path.join(gdir, "visual");
  const importBase   = `${auroraRoot}/User/Import/${titleId}`;
  const gameDataPath = `${auroraRoot}/Data/GameData/${gameDataDir}`;
  fs.mkdirSync(vdir, { recursive: true });

  const m = {
    importCover: null, mediaCover:  null,
    background:  null, banner:      null,
    icon:        null, screenshots: [], other: [],
  };
  const screenshotSort = [];

  function assetFor(localFileName) {
    return {
      rel: `games/${gameDataDir}/visual/${localFileName}`,
      ext: path.extname(localFileName).toLowerCase(),
    };
  }

  // ── 1. LIST Import/{titleId}/ once; build stem → entry map ──────────────────
  let importEntries = [];
  try { importEntries = await client.list(importBase); } catch { /* no import folder */ }

  const importByStem = new Map();
  for (const e of importEntries) {
    if (!e || !e.name || e.type === 2) continue;
    const dot  = e.name.lastIndexOf(".");
    const stem = (dot >= 0 ? e.name.slice(0, dot) : e.name).toLowerCase();
    const ext  = dot >= 0 ? e.name.slice(dot).toLowerCase() : "";
    if (!importByStem.has(stem)) importByStem.set(stem, { name: e.name, extWithDot: ext });
  }

  async function pullImportFile(slotKey, ...stems) {
    for (const stem of stems) {
      const entry = importByStem.get(stem.toLowerCase());
      if (!entry) continue;
      const localName = `import-${safeVisualLocalName(entry.name)}`;
      const lp        = path.join(vdir, localName);
      if (!force && fs.existsSync(lp)) { m[slotKey] = assetFor(localName); return; }
      const buf = await ftpTryDownloadFile(client, `${importBase}/${entry.name}`);
      if (!buf || buf.length < 16) continue;
      fs.writeFileSync(lp, buf);
      m[slotKey] = assetFor(localName);
      return;
    }
  }

  await pullImportFile("background",  "background", "Background", "BoxArtBack",  "boxartback");
  await pullImportFile("banner",      "banner",     "Banner");
  await pullImportFile("icon",        "icon",       "Icon");
  await pullImportFile("importCover", "cover",      "Cover",      "BoxArtFront", "boxartfront");

  // Screenshots from Import (screenshot1…10, with or without zero-padding).
  for (let i = 1; i <= 10; i++) {
    const stems = [`screenshot${i}`, `screenshot${String(i).padStart(2, "0")}`];
    for (const stem of stems) {
      const entry = importByStem.get(stem);
      if (!entry) continue;
      const localName = `import-${safeVisualLocalName(entry.name)}`;
      const lp        = path.join(vdir, localName);
      const info = {
        sortKey: `${String(i).padStart(3, "0")}-import`,
        rel: assetFor(localName).rel, ext: assetFor(localName).ext, name: entry.name,
      };
      if (!force && fs.existsSync(lp)) { screenshotSort.push(info); break; }
      const buf = await ftpTryDownloadFile(client, `${importBase}/${entry.name}`);
      if (!buf || buf.length < 16) continue;
      fs.writeFileSync(lp, buf);
      screenshotSort.push(info);
      break;
    }
  }

  // ── 2. Data/GameData/{dir}/*.asset — RXEA decode via Go server ──────────────
  //    Aurora's .asset files (BK/GC/GL/SS{TitleId}.asset) contain the textures
  //    that are actually displayed on the console.  If a user has uploaded custom
  //    art (Import folder → Aurora processed it) those files reflect the custom
  //    images.  We decode them via the Go RXEA codec and cache as PNG.
  const goPort = getConfiguredServerPort();

  /** FTP download one .asset file and POST it to the Go RXEA decoder.
   *  Returns a map of slot → PNG Buffer, or {} on any failure. */
  async function decodeAssetFile(assetName) {
    const assetBuf = await ftpTryDownloadFile(client, `${gameDataPath}/${assetName}`);
    if (!assetBuf || assetBuf.length < 2048) return {};
    const decoded = await new Promise((res) => {
      const req = http.request(
        { host: "127.0.0.1", port: goPort, path: "/rxea/decode", method: "POST",
          headers: { "Content-Type": "application/octet-stream", "Content-Length": assetBuf.length } },
        (httpRes) => {
          const chunks = [];
          httpRes.on("data", (c) => chunks.push(c));
          httpRes.on("end", () => {
            try { res(JSON.parse(Buffer.concat(chunks).toString("utf8"))); }
            catch (e) {
              addOutputLine(`[WARN] RXEA sync ${titleId}/${assetName}: JSON parse error: ${e.message}`);
              res(null);
            }
          });
        }
      );
      req.on("error", (e) => {
        addOutputLine(`[WARN] RXEA sync ${titleId}/${assetName}: Go server error: ${e.message}`);
        res(null);
      });
      req.end(assetBuf);
    });
    if (!decoded || !Array.isArray(decoded.slots)) {
      const goErr = decoded?.error ? ` — ${decoded.error}` : "";
      addOutputLine(`[WARN] RXEA sync ${titleId}/${assetName}: decoder returned no slots${goErr}.`);
      if (Array.isArray(decoded?.diags) && decoded.diags.length > 0) {
        for (const d of decoded.diags) {
          addOutputLine(`[DIAG] slot${d.slot}: off=${d.offset} sz=${d.size} fmt=${d.gpu_fmt} w=${d.width} h=${d.height} tiled=${d.tiled} endian=${d.endian}${d.error ? ` err="${d.error}"` : ""}`);
        }
      }
      return {};
    }
    const result = {};
    for (const s of decoded.slots) {
      const pngBuf = Buffer.isBuffer(s.png) ? s.png : Buffer.from(s.png, "base64");
      if (pngBuf.length >= 100) result[s.slot] = pngBuf;
    }
    addOutputLine(`[INFO] RXEA sync ${titleId}/${assetName}: decoded ${Object.keys(result).length} slot(s).`);
    return result;
  }

  async function cacheDecodedSlot(slotKey, pngBuf, localBase) {
    if (m[slotKey]) return; // already filled by Import
    const localName = `rxea-${localBase}.png`;
    const lp = path.join(vdir, localName);
    if (!force && fs.existsSync(lp)) { m[slotKey] = assetFor(localName); return; }
    fs.writeFileSync(lp, pngBuf);
    m[slotKey] = assetFor(localName);
  }

  // BK{TitleId}.asset → slot 4 = background
  {
    const decoded = await decodeAssetFile(`BK${titleId}.asset`);
    if (decoded[4]) await cacheDecodedSlot("background", decoded[4], "bk-background");
  }
  // GC{TitleId}.asset → slot 2 = cover
  {
    const decoded = await decodeAssetFile(`GC${titleId}.asset`);
    if (decoded[2]) await cacheDecodedSlot("importCover", decoded[2], "gc-cover");
  }
  // GL{TitleId}.asset → slot 0 = icon, slot 1 = banner
  {
    const decoded = await decodeAssetFile(`GL${titleId}.asset`);
    if (decoded[0]) await cacheDecodedSlot("icon",   decoded[0], "gl-icon");
    if (decoded[1]) await cacheDecodedSlot("banner", decoded[1], "gl-banner");
  }
  // SS{TitleId}.asset → slots 5–24 = screenshots (only if Import had none)
  if (screenshotSort.length === 0) {
    const decoded = await decodeAssetFile(`SS${titleId}.asset`);
    for (let si = 5; si <= 24; si++) {
      if (!decoded[si]) continue;
      const localName = `rxea-screenshot${si - 4}.png`;
      const lp = path.join(vdir, localName);
      if (!force && fs.existsSync(lp)) {
        screenshotSort.push({ sortKey: `${String(si - 4).padStart(3, "0")}-rxea`, rel: assetFor(localName).rel, ext: ".png", name: localName });
        continue;
      }
      fs.writeFileSync(lp, decoded[si]);
      screenshotSort.push({ sortKey: `${String(si - 4).padStart(3, "0")}-rxea`, rel: assetFor(localName).rel, ext: ".png", name: localName });
    }
  }

  // ── 3. GameAssetInfo.bin — Xbox Live CDN image URLs (fallback) ───────────────
  //    Aurora stores an Atom XML feed per title; all URLs go to download.xbox.com.
  let assetInfo = { background: null, banner: null, icon: null, cover: null, screenshots: [] };
  try {
    const chunks = [];
    await client.downloadTo(
      new Writable({ write(c, _, cb) { chunks.push(c); cb(); } }),
      `${gameDataPath}/GameAssetInfo.bin`
    );
    assetInfo = parseGameAssetInfoXml(Buffer.concat(chunks).toString("utf8"));
  } catch { /* GameAssetInfo.bin absent — no CDN URLs available */ }

  /** Download a CDN URL and cache it; fills m[slotKey] if not already set. */
  async function pullCdnImage(slotKey, url, localBase) {
    if (m[slotKey] || !url) return;
    const rawExt    = path.extname(url).toLowerCase();
    const safeExt   = [".jpg", ".jpeg", ".png", ".gif"].includes(rawExt) ? rawExt : ".jpg";
    const localName = `cdnasset-${localBase}${safeExt}`;
    const lp        = path.join(vdir, localName);
    if (!force && fs.existsSync(lp)) { m[slotKey] = assetFor(localName); return; }
    const buf = await fetchHttpImage(url);
    if (!buf || buf.length < 100) return;
    const realExt  = imageExtFromMagic(buf);
    const finalName = `cdnasset-${localBase}${realExt}`;
    fs.writeFileSync(path.join(vdir, finalName), buf);
    m[slotKey] = assetFor(finalName);
  }

  await pullCdnImage("background",  assetInfo.background, "background");
  await pullCdnImage("banner",      assetInfo.banner,     "banner");
  await pullCdnImage("icon",        assetInfo.icon,       "icon");
  await pullCdnImage("importCover", assetInfo.cover,      "cover");

  // Screenshots from CDN (only when Import provided none).
  if (screenshotSort.length === 0) {
    for (let i = 0; i < assetInfo.screenshots.length; i++) {
      const url = assetInfo.screenshots[i];
      if (!url) continue;
      const rawExt    = path.extname(url).toLowerCase();
      const safeExt   = [".jpg", ".jpeg", ".png", ".gif"].includes(rawExt) ? rawExt : ".jpg";
      const localName = `cdnasset-screenshot${i + 1}${safeExt}`;
      const lp        = path.join(vdir, localName);
      const info = {
        sortKey: `${String(i + 1).padStart(3, "0")}-cdn`,
        rel: assetFor(localName).rel, ext: safeExt, name: `Screenshot${i + 1}${safeExt}`,
      };
      if (!force && fs.existsSync(lp)) { screenshotSort.push(info); continue; }
      const buf = await fetchHttpImage(url);
      if (!buf || buf.length < 100) continue;
      const realExt   = imageExtFromMagic(buf);
      const finalName = `cdnasset-screenshot${i + 1}${realExt}`;
      fs.writeFileSync(path.join(vdir, finalName), buf);
      screenshotSort.push({ ...info, rel: assetFor(finalName).rel, ext: realExt });
    }
  }

  // ── 4. GameCoverInfo.bin — XboxUnity cover (mediaCover fallback) ─────────────
  try {
    const chunks = [];
    await client.downloadTo(
      new Writable({ write(c, _, cb) { chunks.push(c); cb(); } }),
      `${gameDataPath}/GameCoverInfo.bin`
    );
    let entries;
    try { entries = JSON.parse(Buffer.concat(chunks).toString("utf8")); } catch { entries = null; }
    if (Array.isArray(entries) && entries.length > 0) {
      // Prefer official entries, then highest-rated.
      const sorted = [...entries].sort((a, b) => {
        if (!!a.official !== !!b.official) return a.official ? -1 : 1;
        return (Number(b.rating) || 0) - (Number(a.rating) || 0);
      });
      for (const row of sorted) {
        const url = (row.front || row.thumbnail || row.url || "").trim();
        if (!url) continue;
        const rawExt    = path.extname(url).toLowerCase();
        const safeExt   = [".jpg", ".jpeg", ".png", ".gif"].includes(rawExt) ? rawExt : ".jpg";
        const localName = `cdnasset-xboxunity-cover${safeExt}`;
        const lp        = path.join(vdir, localName);
        if (!force && fs.existsSync(lp)) { m.mediaCover = assetFor(localName); break; }
        const buf = await fetchHttpImage(url);
        if (!buf || buf.length < 100) continue;
        const realExt   = imageExtFromMagic(buf);
        const finalName = `cdnasset-xboxunity-cover${realExt}`;
        fs.writeFileSync(path.join(vdir, finalName), buf);
        m.mediaCover = assetFor(finalName);
        break;
      }
    }
  } catch { /* GameCoverInfo.bin absent */ }

  screenshotSort.sort((a, b) => a.sortKey.localeCompare(b.sortKey));
  m.screenshots = screenshotSort.map((s) => ({ rel: s.rel, ext: s.ext, name: s.name }));

  fs.writeFileSync(path.join(gdir, "visual-manifest.json"), JSON.stringify(m, null, 2), "utf8");
}

function emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot) {
  const wc = getWebContentsForPush();
  if (!wc) return;
  const gdir = gameCacheDir(cacheRoot, gameDataDir);
  const metaP = path.join(gdir, "cover-files.json");
  let primarySrc = null;
  if (fs.existsSync(metaP)) {
    try {
      const meta = JSON.parse(fs.readFileSync(metaP, "utf8"));
      if (meta.primaryFile && fs.existsSync(path.join(gdir, meta.primaryFile))) {
        primarySrc = auroraCdnUrl(`games/${gameDataDir}/${meta.primaryFile}`);
      }
    } catch { /* ignore */ }
  }
  if (!primarySrc && fs.existsSync(gdir)) {
    for (const name of fs.readdirSync(gdir)) {
      if (name.startsWith("cover-primary.")) {
        primarySrc = auroraCdnUrl(`games/${gameDataDir}/${name}`);
        break;
      }
    }
  }
  // Always emit an event so the renderer transitions from "loading" to either
  // a valid cover or the "no cover" state.  Without this, cards whose covers
  // were never found stay in the animated-pulse loading state forever.
  wc.send("xbox-cover", { titleId, src: primarySrc });
}

/**
 * Summarize GameCoverInfo.bin JSON for the inspector (no image downloads).
 */
function summarizeGameCoverInfoJson(text) {
  if (!text || typeof text !== "string") return { entryCount: 0, preview: [] };
  let arr;
  try {
    arr = JSON.parse(text);
  } catch {
    return { entryCount: 0, preview: [], parseError: true };
  }
  if (!Array.isArray(arr)) return { entryCount: 0, preview: [] };
  const preview = arr.slice(0, 12).map((e, i) => ({
    index:        i,
    official:     !!e?.official,
    rating:       e?.rating != null ? Number(e.rating) : null,
    hasFront:     !!(e?.front && String(e.front).trim()),
    hasThumbnail: !!(e?.thumbnail && String(e.thumbnail).trim()),
    hasUrl:       !!(e?.url && String(e.url).trim()),
  }));
  return { entryCount: arr.length, preview };
}

/**
 * Cache GameCoverInfo.bin, download only the single best cover image (+ Media fallback).
 * Alternate cover URLs stay in the .bin on-console for a future picker.
 */
async function syncAuroraGameCoverAssets(client, auroraRoot, mediaDir, titleId, gameDataDir, cacheRoot, force) {
  const gdir = gameCacheDir(cacheRoot, gameDataDir);
  fs.mkdirSync(gdir, { recursive: true });
  const remoteBin = `${auroraRoot}/Data/GameData/${gameDataDir}/GameCoverInfo.bin`;
  let remoteSz = -1;
  try {
    remoteSz = await client.size(remoteBin);
  } catch { /* bin missing */ }

  const binPath = path.join(gdir, "GameCoverInfo.bin");
  let needBin = force;
  if (remoteSz >= 0) {
    if (!fs.existsSync(binPath)) needBin = true;
    else if (fs.statSync(binPath).size !== remoteSz) needBin = true;
  }

  if (needBin && remoteSz >= 0) {
    const chunks = [];
    await client.downloadTo(
      new Writable({ write(c, _, cb) { chunks.push(c); cb(); } }),
      remoteBin
    );
    fs.writeFileSync(binPath, Buffer.concat(chunks));
  }

  let entries = [];
  if (fs.existsSync(binPath)) {
    try {
      const parsed = JSON.parse(fs.readFileSync(binPath, "utf8"));
      if (Array.isArray(parsed)) entries = parsed;
    } catch { entries = []; }
  }

  const tryMediaFallback = async () => {
    const gcExts = ["jpg", "jpeg", "png", "dds"];
    for (const x of gcExts) {
      const mediaRemote = `${mediaDir}/${titleId}GC.${x}`;
      const buf = await ftpTryDownloadFile(client, mediaRemote);
      if (!buf || buf.length < 100) continue;
      const ext = imageExtFromMagic(buf);
      const primaryName = `cover-primary${ext}`;
      fs.writeFileSync(path.join(gdir, primaryName), buf);
      fs.writeFileSync(
        path.join(gdir, "cover-files.json"),
        JSON.stringify({
          primaryFile: primaryName,
          bestUrl:     `aurora:MediaGC.${x}`,
          gameCoverInfoEntryCount: entries.length,
        }, null, 2),
        "utf8"
      );
      return true;
    }
    return false;
  };

  const withUrl = entries.filter((e) => e && (e.front || e.thumbnail || e.url));
  if (withUrl.length === 0) {
    if (await tryMediaFallback()) emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
    return;
  }

  const best = withUrl.reduce((prev, curr) => {
    if (curr.official && !prev.official) return curr;
    if (!curr.official && prev.official) return prev;
    return (curr.rating || 0) >= (prev.rating || 0) ? curr : prev;
  });

  const bestUrl = best.front || best.thumbnail || best.url;
  if (!bestUrl || typeof bestUrl !== "string") {
    if (await tryMediaFallback()) emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
    return;
  }

  let prevMeta = {};
  try {
    prevMeta = JSON.parse(fs.readFileSync(path.join(gdir, "cover-files.json"), "utf8"));
  } catch { /* none */ }
  if (
    !force &&
    prevMeta.bestUrl === bestUrl &&
    prevMeta.primaryFile &&
    fs.existsSync(path.join(gdir, prevMeta.primaryFile))
  ) {
    emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
    return;
  }

  const buf = await fetchHttpImage(bestUrl);
  if (!buf || buf.length < 100) {
    if (await tryMediaFallback()) emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
    return;
  }

  const ext = imageExtFromMagic(buf);
  const primaryName = `cover-primary${ext}`;
  fs.writeFileSync(path.join(gdir, primaryName), buf);
  fs.writeFileSync(
    path.join(gdir, "cover-files.json"),
    JSON.stringify({
      primaryFile: primaryName,
      bestUrl,
      gameCoverInfoEntryCount: entries.length,
    }, null, 2),
    "utf8"
  );

  emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
}

/** File kind hint for Aurora assets (.asset) vs images. */
function classifyAuroraFileKind(name) {
  const lower = String(name || "").toLowerCase();
  if (lower.endsWith(".asset")) return "asset";
  if (lower.endsWith(".bin")) return "bin";
  if (/\.(jpg|jpeg|png|gif|bmp|dds)$/i.test(lower)) return "image";
  return "other";
}

/**
 * Parse an Xbox Live GameAssetInfo.bin Atom XML feed.
 * Returns { background, banner, icon, cover, screenshots[] } — each a URL string or null.
 *
 * relationshipType mapping (from Xbox Live Atom spec):
 *   23 = tile/icon,  25 = background,  27 = banner,  33 = cover/boxartlg
 * Screenshots are listed inside <live:slideShow> elements.
 */
function parseGameAssetInfoXml(xmlText) {
  const result = { background: null, banner: null, icon: null, cover: null, screenshots: [] };
  if (!xmlText || typeof xmlText !== "string") return result;

  // Each <live:asset> block contains a <live:fileUrl> + <live:relationshipType>.
  for (const [, block] of xmlText.matchAll(/<live:asset[^>]*>([\s\S]*?)<\/live:asset>/gi)) {
    const urlM  = block.match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
    const typeM = block.match(/<live:relationshipType[^>]*>\s*(\d+)\s*<\/live:relationshipType>/i);
    if (!urlM || !typeM) continue;
    const url  = urlM[1].trim();
    const type = parseInt(typeM[1], 10);
    if      (type === 25 && !result.background) result.background = url;
    else if (type === 27 && !result.banner)     result.banner     = url;
    else if (type === 23 && !result.icon)       result.icon       = url;
    else if (type === 33 && !result.cover)      result.cover      = url;
  }

  // Screenshots live in <live:slideShow> elements.
  for (const [, block] of xmlText.matchAll(/<live:slideShow[^>]*>([\s\S]*?)<\/live:slideShow>/gi)) {
    const urlM = block.match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
    if (urlM) result.screenshots.push(urlM[1].trim());
  }

  return result;
}

function registerIpcHandlers() {
  ipcMain.handle("startup:get", () => {
    const settings = app.getLoginItemSettings();
    return !!settings.openAtLogin;
  });

  ipcMain.handle("startup:set", (_event, enabled) => {
    const shouldEnable = Boolean(enabled);
    app.setLoginItemSettings({ openAtLogin: shouldEnable, openAsHidden: true });
    return shouldEnable;
  });

  ipcMain.handle("logs:get-info", () => getLogInfo());
  ipcMain.handle("logs:open-folder", () => openLogsFolder());

  ipcMain.handle("godsend:get-buffer", () => getOutputBuffer());
  ipcMain.handle("godsend:start", () => { startGodsend(); return true; });
  ipcMain.handle("godsend:stop", () => { stopGodsend(); return true; });
  ipcMain.handle("godsend:restart", () => {
    if (getProcess()) restartGodsendIfRunning();
    else startGodsend();
    return true;
  });

  ipcMain.handle("config:get-transfer-folder", () =>
    getConfiguredTransferFolder()
  );

  ipcMain.handle("config:get-effective-transfer-folder", () => {
    const writableRoot = getWritableRuntimeRoot();
    const custom = getConfiguredTransferFolder();
    return custom
      ? path.resolve(custom)
      : getDefaultTransferFolder(writableRoot);
  });

  ipcMain.handle("config:set-transfer-folder", (_event, folder) => {
    const f = typeof folder === "string" ? folder.trim() : "";
    writeConfig({ transferFolder: f });
    appendAppEvent(
      "CONFIG",
      `transferFolder set to ${f ? path.resolve(f) : "(default runtime/Transfer)"}; restarting backend`
    );
    restartGodsendIfRunning();
    return getConfiguredTransferFolder();
  });

  ipcMain.handle("config:get-server-port", () => getConfiguredServerPort());

  ipcMain.handle("config:set-server-port", (_event, value) => {
    const n = parseInt(value, 10);
    const port = Number.isInteger(n) && n >= 1 && n <= 65535 ? n : 8080;
    writeConfig({ serverPort: port });
    appendAppEvent("CONFIG", `serverPort=${port}`);
    restartGodsendIfRunning();
    return port;
  });

  ipcMain.handle("config:get-archive-auth", () => ({
    iaEmail: getConfiguredIAEmail(),
    iaScreenname: getConfiguredIAScreenname(),
    hasSession: Boolean(getConfiguredIACookie()),
  }));

  ipcMain.handle("config:ia-login", async (_event, payload) => {
    const p = payload || {};
    try {
      const { cookieHeader, screenname, email } = await loginInternetArchive(
        p.email,
        p.password
      );
      writeConfig({
        iaCookie: cookieHeader,
        iaEmail: email,
        iaScreenname: screenname,
        iaAuthorization: "",
      });
      restartGodsendIfRunning();
      appendAppEvent("IA_LOGIN", `ok email=${email}`);
      return { ok: true, screenname, email };
    } catch (err) {
      const msg = err && err.message ? err.message : String(err);
      appendAppEvent("IA_LOGIN", `failed: ${msg}`);
      return {
        ok: false,
        error: msg,
      };
    }
  });

  ipcMain.handle("config:ia-logout", () => {
    writeConfig({ iaCookie: "", iaAuthorization: "", iaScreenname: "" });
    appendAppEvent("IA_LOGIN", "logout; session cleared");
    restartGodsendIfRunning();
    return true;
  });

  ipcMain.handle("config:get-rom-path", () =>
    getConfiguredROMPath() || getDefaultROMPath()
  );

  ipcMain.handle("config:set-rom-path", (_event, value) => {
    const v = typeof value === "string" ? value.trim() : "";
    writeConfig({ romPath: v });
    appendAppEvent("CONFIG", `romPath=${v || "(default)"}`);
    restartGodsendIfRunning();
    return getConfiguredROMPath();
  });

  ipcMain.handle("config:cache-refresh", (_event, platform) => {
    const p = typeof platform === "string" && platform ? platform : "all";
    appendAppEvent("CACHE", `refresh requested platform=${p}`);
    return new Promise((resolve) => {
      const req = http.get(
        `http://localhost:${getConfiguredServerPort()}/cache-refresh?platform=${encodeURIComponent(p)}`,
        (res) => {
          let data = "";
          res.on("data", (chunk) => { data += chunk; });
          res.on("end", () => {
            appendAppEvent(
              "CACHE",
              `refresh http status=${res.statusCode} bodyLen=${data.length}`
            );
            resolve({ ok: true, data });
          });
        }
      );
      req.on("error", (err) => {
        appendAppEvent("CACHE", `refresh error: ${err.message}`);
        resolve({ ok: false, error: err.message });
      });
      req.setTimeout(5000, () => {
        req.destroy();
        appendAppEvent("CACHE", "refresh error: timeout");
        resolve({ ok: false, error: "timeout" });
      });
    });
  });

  ipcMain.handle("config:choose-transfer-folder", async () => {
    const win = BrowserWindow.getFocusedWindow() || mainWindow;
    const r = await dialog.showOpenDialog(win || undefined, {
      properties: ["openDirectory", "createDirectory"],
    });
    if (r.canceled || !r.filePaths[0]) return null;
    return r.filePaths[0];
  });

  ipcMain.handle("config:get-xbox-connection", () => ({
    xboxIp:         getConfiguredXboxIP(),
    ftpUser:        getConfiguredFtpUser(),
    ftpPassword:    getConfiguredFtpPassword(),
    ftpScriptsPath: getConfiguredFtpScriptsPath(),
  }));

  ipcMain.handle("config:set-xbox-connection", (_event, payload) => {
    const p = payload || {};
    writeConfig({
      xboxIp:         typeof p.xboxIp         === "string" ? p.xboxIp.trim()        : getConfiguredXboxIP(),
      ftpUser:        typeof p.ftpUser         === "string" ? p.ftpUser.trim()       : getConfiguredFtpUser(),
      ftpPassword:    typeof p.ftpPassword     === "string" ? p.ftpPassword          : getConfiguredFtpPassword(),
      ftpScriptsPath: typeof p.ftpScriptsPath  === "string" ? p.ftpScriptsPath.trim(): getConfiguredFtpScriptsPath(),
    });
    appendAppEvent("CONFIG", `xboxConnection saved (ftpUser=${getConfiguredFtpUser()})`);
    // Restart so the backend re-reads GODSEND_FTP_USER / GODSEND_FTP_PASS from env.
    restartGodsendIfRunning();
    return true;
  });

  ipcMain.handle("config:get-ftp-scripts-path-default", () => getDefaultFtpScriptsPath());

  ipcMain.handle("xbox:ftp-scripts", async (_event, payload) => {
    const p = payload || {};
    const xboxIp    = (typeof p.xboxIp  === "string" ? p.xboxIp.trim()  : "") || getConfiguredXboxIP();
    const ftpUser   = (typeof p.ftpUser === "string" ? p.ftpUser.trim() : "") || getConfiguredFtpUser();
    const ftpPass   = (typeof p.ftpPassword === "string" ? p.ftpPassword : "") || getConfiguredFtpPassword();
    const remotePath = (typeof p.ftpScriptsPath === "string" && p.ftpScriptsPath.trim())
      ? p.ftpScriptsPath.trim()
      : getConfiguredFtpScriptsPath();

    // Send live status updates to the renderer window.
    const sendProgress = (msg) => {
      appendAppEvent("FTP", msg);
      if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send("godsend-ftp-progress", msg);
      }
    };

    let stateTempPath = null;
    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 20000;
    try {
      if (!xboxIp) return { ok: false, error: "Xbox IP address is required." };

      const scriptsDir = getAuroraScriptsPath();
      if (!fs.existsSync(scriptsDir)) {
        return { ok: false, error: `Aurora scripts folder not found at: ${scriptsDir}` };
      }

      // Auto-detect this PC's local IP and patch it directly into state.lua.
      // Write the temp file to os.tmpdir() so it's always writable.
      const pcIp = getLocalIPAddress();
      const serverPort = getConfiguredServerPort();
      if (!pcIp) {
        return { ok: false, error: "Could not detect this PC's local IPv4 address for state.lua patching." };
      }
      const stateSrc = path.join(scriptsDir, "state.lua");
      if (fs.existsSync(stateSrc)) {
        const originalState = fs.readFileSync(stateSrc, "utf8");
        let patchedState = originalState;
        patchedState = patchedState.replace(
          /^(BRAIN_IP\s*=\s*)["'][^"']*["']\s*$/m,
          `$1"${pcIp}"`
        );
        patchedState = patchedState.replace(
          /^(PORT\s*=\s*)["'][^"']*["']\s*$/m,
          `$1"${serverPort}"`
        );
        stateTempPath = path.join(os.tmpdir(), "state.lua.upload-tmp");
        fs.writeFileSync(stateTempPath, patchedState, "utf8");
      }

      sendProgress("Connecting to " + xboxIp + "...");
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      sendProgress("Connected. Preparing destination folder...");
      await client.ensureDir(remotePath);

      // Upload all entries — basic-ftp STOR overwrites existing files by default.
      const entries = fs.readdirSync(scriptsDir, { withFileTypes: true });
      let done = 0;
      for (const entry of entries) {
        sendProgress(`Uploading ${entry.name} (${done}/${entries.length})...`);
        if (entry.isDirectory()) {
          await client.uploadFromDir(
            path.join(scriptsDir, entry.name),
            `${remotePath}/${entry.name}`
          );
        } else {
          const localFile = (stateTempPath && entry.name === "state.lua")
            ? stateTempPath
            : path.join(scriptsDir, entry.name);
          await client.uploadFrom(localFile, `${remotePath}/${entry.name}`);
        }
        done++;
      }
      appendAppEvent("FTP", `upload complete host=${xboxIp} path=${remotePath}`);
      return { ok: true, remotePath };
    } catch (err) {
      appendAppEvent("FTP", `error: ${err.message || String(err)}`);
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
      if (stateTempPath) try { fs.unlinkSync(stateTempPath); } catch { /* ignore */ }
    }
  });

  // ── FTP Ping (lightweight connectivity check) ─────────────────────────────
  ipcMain.handle("xbox:ping", async () => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 5000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── FTP Debug: Test Connection ──
  ipcMain.handle("xbox:ftp-test", async (_event, payload) => {
    const p = payload || {};
    const xboxIp  = (typeof p.xboxIp  === "string" ? p.xboxIp.trim()  : "") || getConfiguredXboxIP();
    const ftpUser = (typeof p.ftpUser === "string" ? p.ftpUser.trim() : "") || getConfiguredFtpUser();
    const ftpPass = (typeof p.ftpPassword === "string" ? p.ftpPassword : "") || getConfiguredFtpPassword();

    const sendDebug = (line) => {
      if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send("godsend-ftp-debug", line);
      }
    };

    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = true;
    client.ftp.timeout = 15000;
    client.ftp.log = (msg) => sendDebug(msg);

    try {
      sendDebug(`[TEST] Connecting to ${xboxIp}:21 as ${ftpUser || "(anonymous)"}...`);
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      sendDebug(`[TEST] Login successful.`);

      sendDebug(`[TEST] Sending PWD...`);
      const pwd = await client.pwd();
      sendDebug(`[TEST] Working directory: ${pwd}`);

      sendDebug(`[TEST] Listing root directory...`);
      const list = await client.list("/");
      for (const item of list) {
        sendDebug(`  ${item.type === 2 ? "DIR " : "FILE"} ${item.name}  (${item.size || 0} bytes)`);
      }

      sendDebug(`[TEST] Connection test PASSED.`);
      return { ok: true };
    } catch (err) {
      sendDebug(`[TEST] FAILED: ${err.message || String(err)}`);
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── FTP Debug: Port Scanner ──
  ipcMain.handle("xbox:ftp-scan", async (_event, subnet) => {
    if (typeof subnet !== "string" || !/^\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(subnet.trim())) {
      return { ok: false, error: "Invalid subnet. Use format like 192.168.1" };
    }
    subnet = subnet.trim();

    const sendDebug = (line) => {
      if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send("godsend-ftp-debug", line);
      }
    };

    sendDebug(`[SCAN] Scanning ${subnet}.1 - ${subnet}.254 on port 21...`);

    const found = [];
    const BATCH = 25;
    const TIMEOUT = 2000;

    for (let batchStart = 1; batchStart <= 254; batchStart += BATCH) {
      const batchEnd = Math.min(batchStart + BATCH - 1, 254);
      sendDebug(`[SCAN] Probing ${subnet}.${batchStart} - ${subnet}.${batchEnd}...`);

      const promises = [];
      for (let i = batchStart; i <= batchEnd; i++) {
        const ip = `${subnet}.${i}`;
        promises.push(new Promise((resolve) => {
          const sock = new net.Socket();
          sock.setTimeout(TIMEOUT);
          sock.once("connect", () => {
            sock.destroy();
            resolve(ip);
          });
          sock.once("timeout", () => { sock.destroy(); resolve(null); });
          sock.once("error", () => { sock.destroy(); resolve(null); });
          sock.connect(21, ip);
        }));
      }

      const results = await Promise.all(promises);
      for (const ip of results) {
        if (ip) {
          found.push(ip);
          sendDebug(`[SCAN] FOUND: ${ip}:21 is open (FTP)`);
        }
      }
    }

    if (found.length === 0) {
      sendDebug(`[SCAN] No FTP servers found on ${subnet}.0/24.`);
    } else {
      sendDebug(`[SCAN] Done. Found ${found.length} host(s) with FTP: ${found.join(", ")}`);
    }
    return { ok: true, hosts: found };
  });

  // ── Xbox Game Library ──────────────────────────────────────────────────────

  ipcMain.handle("xbox:list-games", async () => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();

    if (!xboxIp) return { ok: false, error: "No Xbox IP configured. Set it in Settings." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 20000;

    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

      const nameMap  = xboxBuildGameNameMap();
      const mediaDir = xboxAuroraMediaDir(getConfiguredFtpScriptsPath());
      // Use Map to deduplicate by TitleID across all scan locations.
      const games = new Map();

      function addGame(titleId, fallbackName, location) {
        const id = titleId.toUpperCase();
        if (!games.has(id)) {
          games.set(id, {
            titleId: id,
            name: nameMap.get(id) || fallbackName || id,
            location,
            coverFtpPath: `${mediaDir}/${id}GC.jpg`,
          });
        }
      }

      // 1. Scan /Hdd1/Content/<profile>/<TitleID> (covers all profile IDs)
      try {
        const profileDirs = await client.list("/Hdd1/Content");
        for (const profileDir of profileDirs) {
          if (profileDir.type !== 2) continue;
          try {
            const titleDirs = await client.list(`/Hdd1/Content/${profileDir.name}`);
            for (const titleDir of titleDirs) {
              if (titleDir.type !== 2) continue;
              const id = titleDir.name.toUpperCase();
              if (!/^[0-9A-F]{8}$/.test(id)) continue;
              addGame(id, null, `/Hdd1/Content/${profileDir.name}/${titleDir.name}`);
            }
          } catch { /* unreadable profile dir — skip */ }
        }
      } catch { /* /Hdd1/Content not present */ }

      // 2. Scan /Hdd1/Games/<GameName>/Content/<TitleID>
      try {
        const gameDirs = await client.list("/Hdd1/Games");
        for (const gameDir of gameDirs) {
          if (gameDir.type !== 2) continue;
          try {
            const contentEntries = await client.list(`/Hdd1/Games/${gameDir.name}/Content`);
            for (const entry of contentEntries) {
              if (entry.type !== 2) continue;
              const id = entry.name.toUpperCase();
              if (!/^[0-9A-F]{8}$/.test(id)) continue;
              addGame(id, gameDir.name, `/Hdd1/Games/${gameDir.name}`);
            }
          } catch {
            // No Content subdir — still expose the folder with folder name as title.
            addGame(gameDir.name.toUpperCase().padEnd(8, "0").slice(0, 8), gameDir.name, `/Hdd1/Games/${gameDir.name}`);
          }
        }
      } catch { /* /Hdd1/Games not present */ }

      const gameList = Array.from(games.values()).sort((a, b) =>
        a.name.localeCompare(b.name)
      );
      return { ok: true, games: gameList, connectedTo: xboxIp };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // Streams covers back one-by-one over a single FTP session so the renderer
  // can render them progressively without making N round-trip IPC calls.
  ipcMain.handle("xbox:fetch-covers", async (_event, coverRequests) => {
    if (!Array.isArray(coverRequests) || coverRequests.length === 0) return { ok: true };

    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 10000;

    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

      for (const { titleId, ftpPath } of coverRequests) {
        let dataUrl = null;
        try {
          const chunks = [];
          const writable = new Writable({
            write(chunk, _enc, cb) { chunks.push(chunk); cb(); },
          });
          await client.downloadTo(writable, ftpPath);
          const buf  = Buffer.concat(chunks);
          const mime = (buf[0] === 0xFF && buf[1] === 0xD8) ? "image/jpeg"
                     : (buf[0] === 0x89 && buf[1] === 0x50) ? "image/png"
                     : "image/jpeg";
          dataUrl = `data:${mime};base64,${buf.toString("base64")}`;
        } catch { /* cover file absent — leave null */ }

        if (mainWindow && !mainWindow.isDestroyed()) {
          mainWindow.webContents.send("xbox-cover", { titleId, dataUrl });
        }
      }
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── Queue IPC ─────────────────────────────────────────────────────────────
  ipcMain.handle("xbox:get-queue", async () => {
    return new Promise((resolve) => {
      const port = getConfiguredServerPort();
      const req = http.get(`http://localhost:${port}/queue`, (res) => {
        let data = "";
        res.on("data", chunk => { data += chunk; });
        res.on("end", () => {
          try { resolve({ ok: true, jobs: JSON.parse(data) }); }
          catch { resolve({ ok: true, jobs: [] }); }
        });
      });
      req.on("error", (err) => resolve({ ok: false, jobs: [], error: err.message }));
      req.setTimeout(5000, () => { req.destroy(); resolve({ ok: false, jobs: [] }); });
    });
  });

  ipcMain.handle("xbox:remove-queue-item", async (_event, game) => {
    return new Promise((resolve) => {
      const port = getConfiguredServerPort();
      const enc = encodeURIComponent(game);
      const req = http.get(`http://localhost:${port}/queue/remove?game=${enc}`, (res) => {
        let data = "";
        res.on("data", chunk => { data += chunk; });
        res.on("end", () => resolve({ ok: true, data }));
      });
      req.on("error", (err) => resolve({ ok: false, error: err.message }));
      req.setTimeout(5000, () => { req.destroy(); resolve({ ok: false }); });
    });
  });

  // ── Browse: helpers ───────────────────────────────────────────────────────

  /** Fire a GET to the local Go backend and return the raw text body. */
  function backendGet(path) {
    const port = getConfiguredServerPort();
    return new Promise((resolve, reject) => {
      const req = http.get(`http://localhost:${port}${path}`, (res) => {
        let data = "";
        res.on("data", (c) => { data += c; });
        res.on("end", () => resolve(data));
      });
      req.on("error", reject);
      req.setTimeout(30000, () => { req.destroy(); reject(new Error("Timeout")); });
    });
  }

  /** Clean a Redump/No-Intro game name for a cover-art search query. */
  function cleanTitleForSearch(raw) {
    return raw
      .replace(/\s*\((?:USA|EUR|PAL|NTSC|Japan|UK|EU|US|En|Fr|De|Es|Pt|Rev\s*\d+|v\d[^)]*|[A-Z]{2,3})\)/gi, "")
      .replace(/\s*\[.*?\]/g, "")
      .replace(/\s+/g, " ")
      .trim();
  }

  // ── Browse: get game list from Go backend ─────────────────────────────────
  ipcMain.handle("browse:get-games", async (_event, { platform, source }) => {
    try {
      const src = source ? `&source=${encodeURIComponent(source)}` : "";
      const data = await backendGet(
        `/browse?platform=${encodeURIComponent(platform)}${src}`
      );
      if (data.startsWith("__IA_LOADING__")) {
        const m = data.match(/__IA_LOADING__:(\d+)\/(\d+)/);
        return { ok: true, loading: true,
          loaded: m ? m[1] : "?", total: m ? m[2] : "?", games: [] };
      }
      const games = data.split("|").map((s) => s.trim()).filter(Boolean);
      return { ok: true, loading: false, games };
    } catch (err) {
      return { ok: false, error: err.message, games: [] };
    }
  });

  // ── Browse: queue a game (register → trigger on Go backend) ──────────────
  ipcMain.handle("browse:queue-game", async (_event, { game, platform, source, drive, installType }) => {
    const xboxIp = getConfiguredXboxIP();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured. Check Settings → Xbox connection." };

    const enc   = encodeURIComponent(game);
    const drv   = encodeURIComponent(drive || "Hdd1:");
    const inst  = encodeURIComponent(installType || "god");
    const plat  = encodeURIComponent(platform || "xbox360");
    const src   = source ? `&source=${encodeURIComponent(source)}` : "";

    try {
      // 1. Register Xbox FTP destination for this game
      const regData = await backendGet(
        `/register?game=${enc}&ip=${encodeURIComponent(xboxIp)}&drive=${drv}&platform=${plat}&mode=ftp&install_type=${inst}`
      );
      let reg;
      try { reg = JSON.parse(regData); } catch { reg = {}; }
      if (reg.error) return { ok: false, error: `Register: ${reg.error}` };

      // 2. Trigger download + processing
      const trigData = await backendGet(
        `/trigger?game=${enc}&platform=${plat}&install_type=${inst}${src}`
      );
      let trig;
      try { trig = JSON.parse(trigData); } catch { trig = {}; }
      if (trig.error) return { ok: false, error: `Trigger: ${trig.error}` };

      const status = trig.status || "triggered";
      // already_ready / already_processing are fine outcomes
      return { ok: true, status };
    } catch (err) {
      return { ok: false, error: err.message };
    }
  });

  // ── Browse: disc-info recommendation (install type hint) ─────────────────
  ipcMain.handle("browse:get-disc-info", async (_event, game) => {
    try {
      const data = await backendGet(`/disc-info?game=${encodeURIComponent(game)}`);
      return { ok: true, ...JSON.parse(data) };
    } catch {
      return { ok: false };
    }
  });

  // ── Browse: fetch cover art for a game name ──────────────────────────────
  // Strategy (in order):
  //   1. XboxUnity /api/Covers/{name} — official/highest-rated; use row titleid → CDN when helpful
  //   2. TitleList.php (+ series-stripped retry) → CDN
  //   3. Microsoft Store autosuggest + product JSON → legacy Title ID → CDN
  //   4. Wikipedia REST API summary thumbnail as last resort
  // Results are cached by base title so all regional variants of the same
  // game share one cache entry and one network round-trip.
  const browseCoverCache = new Map(); // base-title → { ok, dataUrl? }

  /** Strip ALL parenthetical/bracketed suffixes to get a bare base title. */
  function baseTitleForCover(raw) {
    return raw
      .replace(/\s*\(.*?\)/g, "")
      .replace(/\s*\[.*?\]/g, "")
      .replace(/\s+/g, " ")
      .trim();
  }

  /** Pick best cover row (official first); returns { buf, titleId } or null. */
  async function fetchXboxUnityCoverWithMeta(searchTerm) {
    const url = `http://xboxunity.net/api/Covers/${encodeURIComponent(searchTerm)}`;
    const jsonBuf = await fetchHttpImage(url);
    if (!jsonBuf || jsonBuf.length === 0) return null;
    let items;
    try { items = JSON.parse(jsonBuf.toString("utf8")); } catch { return null; }
    if (!Array.isArray(items) || items.length === 0) return null;

    const sorted = [...items].sort((a, b) => {
      if (b.official && !a.official) return 1;
      if (a.official && !b.official) return -1;
      return (b.rating || 0) - (a.rating || 0);
    });
    const row = sorted[0];
    const coverUrl = row.front || row.thumbnail || row.url;
    if (!coverUrl) return null;
    const tidRaw = row.titleid ?? row.TitleID;
    const ts = String(tidRaw ?? "").trim();
    const titleId = /^[0-9A-F]{8}$/i.test(ts) ? ts.toUpperCase() : "";

    const buf = await fetchHttpImage(coverUrl);
    if (!buf || buf.length < 100) return null;
    return { buf, titleId };
  }

  async function fetchXboxUnityCover(searchTerm) {
    const r = await fetchXboxUnityCoverWithMeta(searchTerm);
    return r ? r.buf : null;
  }

  const invalidStoreTitleHex = new Set(["00000000", "FFFFFFFF"]);

  function normalizeKeyStore(s) {
    return String(s)
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "")
      .trim();
  }

  function titleRankStore(query, title) {
    const q = normalizeKeyStore(query);
    const t = normalizeKeyStore(title);
    if (!q || !t) return 99;
    if (q === t) return 0;
    if (t.includes(q) || q.includes(t)) return 1;
    return 2;
  }

  /** Parse legacy 8-hex Title ID from Store product JSON (e.g. ProductGroupName "… (4D5307E6)"). */
  function extractTitleIdFromStoreProductJsonStr(jsonStr) {
    const m = jsonStr.match(/ProductGroupName"\s*:\s*"[^"]*\(([0-9A-F]{8})\)/i);
    if (m) {
      const h = m[1].toUpperCase();
      if (!invalidStoreTitleHex.has(h)) return h;
    }
    const re = /\(([0-9A-F]{8})\)/gi;
    let mm;
    while ((mm = re.exec(jsonStr)) !== null) {
      const h = mm[1].toUpperCase();
      if (!invalidStoreTitleHex.has(h)) return h;
    }
    return "";
  }

  /** Microsoft Store Display Catalog (xbox.com backend) → legacy Title ID. */
  async function fetchMicrosoftStoreTitleIdForBrowse(searchTerm) {
    const p = new URLSearchParams({
      languages: "en-us",
      market: "US",
      platformdependencyname: "Windows.Xbox",
      productFamilyNames: "Games",
      query: searchTerm,
      topProducts: "10",
    });
    let res;
    try {
      res = await fetch(
        `https://displaycatalog.mp.microsoft.com/v7.0/productFamilies/autosuggest?${p}`,
        { headers: { "User-Agent": "Mozilla/5.0 GODsend-browse-cover" } },
      );
    } catch {
      return "";
    }
    if (!res.ok) return "";
    let asj;
    try {
      asj = await res.json();
    } catch {
      return "";
    }
    const candidates = [];
    for (const fam of asj.Results || []) {
      for (const pr of fam.Products || []) {
        if (pr?.ProductId && pr?.Title) {
          candidates.push({
            productId: pr.ProductId,
            title: String(pr.Title),
            type: pr.Type || "",
          });
        }
      }
    }
    candidates.sort((a, b) => {
      const g = (t) => (t === "Game" ? 0 : 1);
      const tg = g(a.type) - g(b.type);
      if (tg !== 0) return tg;
      return titleRankStore(searchTerm, a.title) - titleRankStore(searchTerm, b.title);
    });
    for (let i = 0; i < Math.min(2, candidates.length); i++) {
      const q2 = new URLSearchParams({
        bigIds: candidates[i].productId,
        market: "US",
        languages: "en-us",
        fieldsTemplate: "details",
      });
      let pr;
      try {
        pr = await fetch(`https://displaycatalog.mp.microsoft.com/v7.0/products?${q2}`, {
          headers: { "User-Agent": "Mozilla/5.0 GODsend-browse-cover" },
        });
      } catch {
        continue;
      }
      if (!pr.ok) continue;
      let pj;
      try {
        pj = await pr.json();
      } catch {
        continue;
      }
      const hex = extractTitleIdFromStoreProductJsonStr(JSON.stringify(pj));
      if (/^[0-9A-F]{8}$/.test(hex)) return hex;
    }
    return "";
  }

  async function tryXboxCdnFromMicrosoftStoreSearch(searchTerm) {
    const hex = await fetchMicrosoftStoreTitleIdForBrowse(searchTerm);
    if (!hex) return null;
    const xboxUrl = `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${hex}/en-US/1`;
    const xboxBuf = await fetchHttpImage(xboxUrl);
    return xboxBuf && xboxBuf.length >= 100 ? xboxBuf : null;
  }

  /**
   * Fetch a cover from the Wikipedia REST API page summary thumbnail.
   * Tries the given article title; returns a Buffer or null.
   */
  async function fetchWikipediaCover(articleTitle) {
    const url = `https://en.wikipedia.org/api/rest_v1/page/summary/${encodeURIComponent(articleTitle)}`;
    const jsonBuf = await fetchHttpImage(url);
    if (!jsonBuf) return null;
    let parsed;
    try { parsed = JSON.parse(jsonBuf.toString("utf8")); } catch { return null; }
    if (parsed.type !== "standard") return null;
    const imgUrl = parsed.originalimage?.source || parsed.thumbnail?.source;
    if (!imgUrl) return null;
    const buf = await fetchHttpImage(imgUrl);
    return buf && buf.length >= 100 ? buf : null;
  }

  ipcMain.handle("browse:fetch-cover", async (_event, gameName) => {
    try {
      const base = baseTitleForCover(gameName);

      if (browseCoverCache.has(base)) {
        return browseCoverCache.get(base);
      }

      let imgBuf = null;

      // ── 1. XboxUnity Covers API (image + optional titleid on same payload) ─
      if (!imgBuf) {
        let meta = await fetchXboxUnityCoverWithMeta(base);
        if (!meta) {
          const m = base.match(/^.+?\s+-\s+(.+)$/);
          if (m) meta = await fetchXboxUnityCoverWithMeta(m[1].trim());
        }
        if (meta) {
          imgBuf = meta.buf;
          // Prefer CDN tile when Covers gave us a Title ID (often works when TitleList misses).
          if (meta.titleId) {
            const xboxUrl = `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${meta.titleId}/en-US/1`;
            const xboxBuf = await fetchHttpImage(xboxUrl);
            if (xboxBuf && xboxBuf.length >= 100) imgBuf = xboxBuf;
          }
        }
      }

      // ── 2. Xbox.com catalog via TitleList (when Covers had no usable titleid) ─
      if (!imgBuf) {
        const tlBuf = await fetchHttpImage(
          `http://xboxunity.net/Resources/Lib/TitleList.php?search=${encodeURIComponent(base)}`
        );
        if (tlBuf) {
          try {
            const tlJson = JSON.parse(tlBuf.toString("utf8"));
            const tlTid = tlJson?.Items?.[0]?.TitleID;
            if (tlTid) {
              const xboxUrl = `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${String(tlTid).toUpperCase()}/en-US/1`;
              const xboxBuf = await fetchHttpImage(xboxUrl);
              if (xboxBuf && xboxBuf.length >= 100) imgBuf = xboxBuf;
            }
          } catch { /* ignore */ }
        }
      }

      // TitleList sub-title retry (series strip) if still no CDN hit
      if (!imgBuf) {
        const m = base.match(/^.+?\s+-\s+(.+)$/);
        if (m) {
          const tlBuf = await fetchHttpImage(
            `http://xboxunity.net/Resources/Lib/TitleList.php?search=${encodeURIComponent(m[1].trim())}`
          );
          if (tlBuf) {
            try {
              const tlJson = JSON.parse(tlBuf.toString("utf8"));
              const tlTid = tlJson?.Items?.[0]?.TitleID;
              if (tlTid) {
                const xboxUrl = `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${String(tlTid).toUpperCase()}/en-US/1`;
                const xboxBuf = await fetchHttpImage(xboxUrl);
                if (xboxBuf && xboxBuf.length >= 100) imgBuf = xboxBuf;
              }
            } catch { /* ignore */ }
          }
        }
      }

      // ── 3. Microsoft Store (xbox.com) → Title ID in product metadata → CDN ──
      if (!imgBuf) {
        imgBuf = await tryXboxCdnFromMicrosoftStoreSearch(base);
        if (!imgBuf) {
          const sm = base.match(/^.+?\s+-\s+(.+)$/);
          if (sm) imgBuf = await tryXboxCdnFromMicrosoftStoreSearch(sm[1].trim());
        }
      }

      // ── 4. Wikipedia REST API ─────────────────────────────────────────────
      if (!imgBuf) {
        const wikiTitles = [`${base} (video game)`, base];
        const m = base.match(/^.+?\s+-\s+(.+)$/);
        if (m) wikiTitles.push(`${m[1].trim()} (video game)`, m[1].trim());

        for (const title of wikiTitles) {
          imgBuf = await fetchWikipediaCover(title);
          if (imgBuf) break;
        }
      }

      if (!imgBuf) {
        const result = { ok: false };
        browseCoverCache.set(base, result);
        return result;
      }

      const mime =
        (imgBuf[0] === 0xFF && imgBuf[1] === 0xD8) ? "image/jpeg" :
        (imgBuf[0] === 0x89 && imgBuf[1] === 0x50) ? "image/png"  : "image/jpeg";
      const result = { ok: true, dataUrl: `data:${mime};base64,${imgBuf.toString("base64")}` };
      browseCoverCache.set(base, result);
      return result;
    } catch {
      return { ok: false };
    }
  });

  // ── Data status / clear IPC ───────────────────────────────────────────────
  ipcMain.handle("data:status", async () => {
    return new Promise((resolve) => {
      const port = getConfiguredServerPort();
      const req = http.get(`http://localhost:${port}/data/status`, (res) => {
        let data = "";
        res.on("data", chunk => { data += chunk; });
        res.on("end", () => {
          try { resolve({ ok: true, ...JSON.parse(data) }); }
          catch { resolve({ ok: false, error: "parse error" }); }
        });
      });
      req.on("error", (err) => resolve({ ok: false, error: err.message }));
      req.setTimeout(5000, () => { req.destroy(); resolve({ ok: false, error: "timeout" }); });
    });
  });

  ipcMain.handle("data:clear", async () => {
    return new Promise((resolve) => {
      const port = getConfiguredServerPort();
      const req = http.get(`http://localhost:${port}/data/clear`, (res) => {
        let data = "";
        res.on("data", chunk => { data += chunk; });
        res.on("end", () => resolve({ ok: true }));
      });
      req.on("error", (err) => resolve({ ok: false, error: err.message }));
      req.setTimeout(10000, () => { req.destroy(); resolve({ ok: false, error: "timeout" }); });
    });
  });

  // ── Aria2 port settings ───────────────────────────────────────────────────
  ipcMain.handle("config:get-aria2-listen-port", () => getConfiguredAria2ListenPort());
  ipcMain.handle("config:set-aria2-listen-port", (_event, value) => {
    const n = parseInt(value, 10);
    const v = (Number.isInteger(n) && n >= 1 && n <= 65535) ? String(n) : "";
    writeConfig({ aria2ListenPort: v });
    restartGodsendIfRunning();
    return v;
  });
  ipcMain.handle("config:get-aria2-dht-port", () => getConfiguredAria2DhtPort());
  ipcMain.handle("config:set-aria2-dht-port", (_event, value) => {
    const n = parseInt(value, 10);
    const v = (Number.isInteger(n) && n >= 1 && n <= 65535) ? String(n) : "";
    writeConfig({ aria2DhtPort: v });
    restartGodsendIfRunning();
    return v;
  });

  // ── Default Xbox drive ────────────────────────────────────────────────────
  ipcMain.handle("config:get-default-xbox-drive", () => getConfiguredDefaultXboxDrive());

  ipcMain.handle("config:set-default-xbox-drive", (_event, value) => {
    const v = typeof value === "string" ? value.trim() : "";
    writeConfig({ defaultXboxDrive: v });
    restartGodsendIfRunning();
    return v;
  });

  // Fetch available drives from Xbox via FTP (list root dirs)
  ipcMain.handle("xbox:list-drives", async () => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 10000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      await client.cd("/");
      const entries = await client.list();
      // Xbox drives appear as directories at the root: Hdd1, Usb0, etc.
      const drives = entries
        .filter(e => e.type === 2)
        .map(e => e.name + ":")
        .filter(d => /^[A-Za-z][A-Za-z0-9]*:$/.test(d));
      const known = ["Hdd1:", "Usb0:", "Usb1:", "Usb2:"];
      const all = [...new Set([...drives, ...known])];
      return { ok: true, drives: all };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── Aurora Library sources config ─────────────────────────────────────────
  ipcMain.handle("config:get-aurora-library-sources", () =>
    getConfiguredAuroraLibrarySources()
  );

  ipcMain.handle("config:set-aurora-library-sources", (_event, sources) => {
    const arr = Array.isArray(sources)
      ? sources.map(String).filter(Boolean)
      : ["Hdd1"];
    writeConfig({ auroraLibrarySources: arr });
    return arr;
  });

  // ── Aurora Library: cached DBs + FTP size fingerprint sync ────────────────
  ipcMain.handle("xbox:list-aurora-library", async (_event, opts) => {
    const force = opts && opts.force === true;
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    const scriptsPath = getConfiguredFtpScriptsPath();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured. Set it in Settings." };

    let auroraRoot = xboxAuroraRoot(scriptsPath);
    let dbDir      = `${auroraRoot}/Data/Databases`;
    let cacheRoot  = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);
    setActiveAuroraCacheRoot(cacheRoot);

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 30000;

    try {
      addOutputLine(
        `[INFO] Aurora library: ${force ? "refresh (forced)" : "loading"} — FTP ${xboxIp}…`
      );
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

      let contentSz = -1;
      let settingsSz = -1;
      try {
        contentSz = await client.size(`${dbDir}/content.db`);
      } catch { /* missing */ }
      try {
        settingsSz = await client.size(`${dbDir}/settings.db`);
      } catch { /* missing */ }

      // If the configured Aurora root has no databases, auto-discover the
      // real install location (XEXMenu / Apps/Aurora layouts, USB installs).
      if (contentSz < 0 || settingsSz < 0) {
        addOutputLine(
          `[INFO] Aurora library: ${auroraRoot}/Data/Databases not found — auto-discovering Aurora install…`
        );
        const discovered = await discoverAuroraRoot(client);
        if (discovered) {
          _lastDiscoveredAuroraRoot = discovered;
          auroraRoot = discovered;
          dbDir      = `${auroraRoot}/Data/Databases`;
          cacheRoot  = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);
          setActiveAuroraCacheRoot(cacheRoot);
          addOutputLine(`[INFO] Aurora library: discovered Aurora at ${auroraRoot}`);
          try { contentSz = await client.size(`${dbDir}/content.db`); } catch {}
          try { settingsSz = await client.size(`${dbDir}/settings.db`); } catch {}
        } else {
          addOutputLine(
            `[ERROR] Aurora library: could not find an Aurora install on the console.`
          );
        }
      }

      const meta = readMeta(cacheRoot);
      const fingerprintMatch =
        !force &&
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
        fs.existsSync(contentDbPath(cacheRoot)) &&
        fs.existsSync(settingsDbPath(cacheRoot));

      if (fingerprintMatch) {
        const contentBuf  = fs.readFileSync(contentDbPath(cacheRoot));
        const settingsBuf = fs.readFileSync(settingsDbPath(cacheRoot));
        const scanDriveMap = new Map(
          Object.entries(meta.scanDriveMap).map(([k, v]) => [Number(k), String(v)])
        );
        const games = await buildAuroraGamesFromDbBuffers(contentBuf, settingsBuf, scanDriveMap);
        addOutputLine(
          `[INFO] Aurora library: using local DB cache (${games.length} games, console DB unchanged).`
        );
        return {
          ok: true,
          games,
          connectedTo: xboxIp,
          auroraRoot,
          libraryUnchanged: true,
          fromCache: true,
        };
      }

      addOutputLine("[INFO] Aurora library: downloading content.db and settings.db…");
      fs.mkdirSync(databasesDir(cacheRoot), { recursive: true });

      const contentChunks = [];
      await client.downloadTo(
        new Writable({ write(c, _, cb) { contentChunks.push(c); cb(); } }),
        `${dbDir}/content.db`
      );
      const settingsChunks = [];
      await client.downloadTo(
        new Writable({ write(c, _, cb) { settingsChunks.push(c); cb(); } }),
        `${dbDir}/settings.db`
      );

      const contentBuf  = Buffer.concat(contentChunks);
      const settingsBuf = Buffer.concat(settingsChunks);
      fs.writeFileSync(contentDbPath(cacheRoot), contentBuf);
      fs.writeFileSync(settingsDbPath(cacheRoot), settingsBuf);

      const scanRows = await readScanRowsFromSettingsBuffer(settingsBuf);
      const contentScanRows = await readContentScanRowsFromBuffer(contentBuf);
      addOutputLine(
        `[INFO] Aurora library: probing ${scanRows.length} scan path(s) for drive letters…`
      );
      const prevFtpTimeout = client.ftp.timeout;
      client.ftp.timeout = 8000;
      let scanDriveMap;
      try {
        scanDriveMap = await probeScanPathDrives(client, scanRows, contentScanRows);
      } finally {
        client.ftp.timeout = prevFtpTimeout;
      }

      writeMeta(cacheRoot, {
        xboxIp,
        auroraRoot,
        ftpScriptsPath: scriptsPath,
        contentDbSize:    contentSz,
        settingsDbSize:   settingsSz,
        scanDriveMap:     Object.fromEntries(scanDriveMap),
        driveProbeVersion: 2,
        updatedAt:        Date.now(),
      });

      const games = await buildAuroraGamesFromDbBuffers(contentBuf, settingsBuf, scanDriveMap);

      addOutputLine(
        `[INFO] Aurora library: ready (${games.length} games, DB saved to app cache).`
      );
      return {
        ok: true,
        games,
        connectedTo: xboxIp,
        auroraRoot,
        libraryUnchanged: false,
        fromCache: false,
      };
    } catch (err) {
      const msg = err.message || String(err);
      addOutputLine(`[ERROR] Aurora library: ${msg}`);
      appendAppEvent("AURORA_LIB", `error: ${msg}`);
      return { ok: false, error: msg };
    } finally {
      client.close();
    }
  });

  // ── Aurora covers: single primary image + cached GameCoverInfo.bin ─────────
  // gameList: [{ titleId, contentId, gameDataDir }]
  ipcMain.handle("xbox:fetch-aurora-covers", async (_event, gameList, opts) => {
    if (!Array.isArray(gameList) || gameList.length === 0) return { ok: true };

    const force        = opts && opts.force === true;
    const fromDiskOnly = opts && opts.fromDiskOnly === true;

    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
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
    client.ftp.timeout = 20000;

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
          await syncAuroraGameCoverAssets(
            client, auroraRoot, mediaDir, titleId, gameDataDir, cacheRoot, force
          );
          await syncAuroraTitleVisualAssets(
            client,
            auroraRoot,
            titleId,
            gameDataDir,
            cacheRoot,
            force
          );
        } catch (err) {
          const em = err?.message || String(err);
          addOutputLine(`[WARN] Aurora sync ${titleId}: ${em}`);
          appendAppEvent("AURORA_SYNC", `${titleId}: ${em}`);
        } finally {
          emitAuroraCoverEvents(titleId, gameDataDir, cacheRoot);
          emitAuroraTitleVisualEvents(titleId, gameDataDir, cacheRoot);
        }
        lastProcessed = gi;
        if (progressEvery > 0 && (gi + 1) % progressEvery === 0) {
          addOutputLine(
            `[INFO] Aurora covers + artwork: progress ${gi + 1}/${gameList.length} titles…`
          );
        }
        if ((gi & 3) === 3) {
          await new Promise((resolve) => setImmediate(resolve));
        }
      }

      addOutputLine(
        `[INFO] Aurora covers + artwork: finished ${gameList.length} title(s).`
      );
      return { ok: true };
    } catch (err) {
      const msg = err.message || String(err);
      addOutputLine(`[ERROR] Aurora covers + artwork: ${msg}`);
      appendAppEvent("AURORA_SYNC", `fatal: ${msg}`);

      // Emit cached cover/visual events for any games the loop never reached
      // so the renderer transitions them out of the "loading" state.
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

  /** Re-read `visual-manifest.json` from disk and push `xbox-title-visuals` (no FTP). */
  ipcMain.handle("xbox:refresh-title-visuals-cache", async (_event, payload) => {
    const p = payload || {};
    const titleId =
      typeof p.titleId === "string" ? p.titleId.trim().toUpperCase() : "";
    const gameDataDir =
      typeof p.gameDataDir === "string" ? p.gameDataDir.trim() : "";
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

  // ── Xbox CDN Catalog query for type-specific assets ─────────────────────────
  // Mirrors AuroraAssetEditor's XboxAssetDownloader: queries the Xbox Live
  // catalog XML to obtain background, icon, banner, and screenshot URLs.
  // Returns { background[], banner[], icon[], screenshot[], cover[] } — each
  // an array of URL strings.
  async function fetchXboxCdnAssets(titleIdHex, locale = "en-US") {
    const result = { background: [], banner: [], icon: [], screenshot: [], cover: [] };
    if (!titleIdHex || !/^[0-9A-F]{8}$/i.test(titleIdHex)) return result;

    const catalogUrl =
      `http://catalog-cdn.xboxlive.com/Catalog/Catalog.asmx/Query` +
      `?methodName=FindGames` +
      `&Names=Locale&Values=${locale}` +
      `&Names=LegalLocale&Values=${locale}` +
      `&Names=Store&Values=1` +
      `&Names=PageSize&Values=100` +
      `&Names=PageNum&Values=1` +
      `&Names=DetailView&Values=5` +
      `&Names=OfferFilterLevel&Values=1` +
      `&Names=MediaIds&Values=66acd000-77fe-1000-9115-d802${titleIdHex.toUpperCase()}` +
      `&Names=UserTypes&Values=2` +
      `&Names=MediaTypes&Values=1&Names=MediaTypes&Values=21` +
      `&Names=MediaTypes&Values=23&Names=MediaTypes&Values=37` +
      `&Names=MediaTypes&Values=46`;

    try {
      const xmlBuf = await fetchHttpImage(catalogUrl);
      if (!xmlBuf || xmlBuf.length === 0) return result;
      const xml = xmlBuf.toString("utf8");

      // Parse <live:image> blocks for typed assets (icon/background/banner).
      for (const [, block] of xml.matchAll(/<live:image[^>]*>([\s\S]*?)<\/live:image>/gi)) {
        const urlM  = block.match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
        const typeM = block.match(/<live:relationshipType[^>]*>\s*(\d+)\s*<\/live:relationshipType>/i);
        if (!urlM) continue;
        const url  = urlM[1].trim();
        const type = typeM ? parseInt(typeM[1], 10) : -1;
        if      (type === 15 || type === 23) result.icon.push(url);
        else if (type === 25)                result.background.push(url);
        else if (type === 27)                result.banner.push(url);
      }

      // Parse <live:slideShow> blocks for screenshots.
      for (const [, block] of xml.matchAll(/<live:slideShow[^>]*>([\s\S]*?)<\/live:slideShow>/gi)) {
        const urlM = block.match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
        if (urlM) result.screenshot.push(urlM[1].trim());
      }

      // Also try the simple CoverArt endpoint for cover.
      const coverUrl = `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${titleIdHex.toUpperCase()}/${locale}/1`;
      const coverBuf = await fetchHttpImage(coverUrl);
      if (coverBuf && coverBuf.length >= 100) {
        const mime = (coverBuf[0] === 0xFF && coverBuf[1] === 0xD8) ? "image/jpeg" : "image/png";
        result.cover.push(`data:${mime};base64,${coverBuf.toString("base64")}`);
      }
    } catch { /* ignore catalog errors */ }
    return result;
  }

  // ── Aurora asset search (multi-source: XboxUnity covers + Xbox CDN catalog) ──
  // Payload: { query, titleId, assetType? }
  // assetType: "cover"|"background"|"icon"|"banner"|"screenshot" (defaults to "cover")
  ipcMain.handle("xbox:search-assets", async (_event, payload) => {
    const { query, titleId, assetType: rawAssetType } = payload || {};
    const assetType = (typeof rawAssetType === "string" && rawAssetType.trim())
      ? rawAssetType.trim().toLowerCase().replace(/\d+$/, "")   // "screenshot3" → "screenshot"
      : "cover";
    const searchTerm = (titleId && /^[0-9A-F]{8}$/i.test(String(titleId).trim()))
      ? titleId.trim().toUpperCase()
      : (typeof query === "string" ? query.trim() : "");
    if (!searchTerm) return { ok: true, results: [] };

    // ── Helper: XboxUnity cover search ────────────────────────────────────────
    async function searchXboxUnityCovers(term) {
      const url = `http://xboxunity.net/api/Covers/${encodeURIComponent(term)}`;
      const jsonBuf = await fetchHttpImage(url);
      if (!jsonBuf || jsonBuf.length === 0) return [];
      let items;
      try { items = JSON.parse(jsonBuf.toString("utf8")); } catch { return []; }
      if (!Array.isArray(items)) return [];
      return items
        .map((item) => ({
          titleId:   String(item.titleid || item.TitleID || "").toUpperCase(),
          front:     item.front     || null,
          thumbnail: item.thumbnail || null,
          url:       item.url       || null,
          official:  !!item.official,
          rating:    item.rating != null ? Number(item.rating) : null,
        }))
        .filter((r) => r.front || r.thumbnail || r.url)
        .sort((a, b) => {
          if (b.official !== a.official) return a.official ? -1 : 1;
          return (b.rating || 0) - (a.rating || 0);
        });
    }

    // ── Helper: resolve a titleId hex from XboxUnity results or the search term ─
    async function resolveTitleIdHex(term) {
      if (/^[0-9A-F]{8}$/i.test(term)) return term.toUpperCase();
      // Try XboxUnity to discover the titleId from a game name.
      const covers = await searchXboxUnityCovers(term);
      const first  = covers.find((c) => c.titleId && /^[0-9A-F]{8}$/.test(c.titleId));
      return first ? first.titleId : null;
    }

    // ── Cover search: XboxUnity + CDN cover (existing behaviour) ──────────────
    if (assetType === "cover") {
      let results = await searchXboxUnityCovers(searchTerm);
      // Name-based fallback if titleId search yielded nothing.
      if (results.length === 0 && titleId && typeof query === "string" && query.trim() && searchTerm !== query.trim()) {
        results = await searchXboxUnityCovers(query.trim());
      }
      // Prepend Xbox CDN high-res cover if we have a titleId.
      if (results.length > 0 && results[0].titleId && /^[0-9A-F]{8}$/.test(results[0].titleId)) {
        const cdnUrl = `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${results[0].titleId}/en-US/1`;
        const cdnBuf = await fetchHttpImage(cdnUrl);
        if (cdnBuf && cdnBuf.length >= 100) {
          const mime = (cdnBuf[0] === 0xFF && cdnBuf[1] === 0xD8) ? "image/jpeg" : "image/png";
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

    // ── Non-cover search: query Xbox CDN Catalog for type-specific assets ─────
    const tidHex = await resolveTitleIdHex(searchTerm);
    if (!tidHex) {
      // Could not resolve a titleId — no Xbox CDN results possible.
      return { ok: true, results: [] };
    }

    const cdnAssets = await fetchXboxCdnAssets(tidHex);
    const typeKey   = assetType === "background" ? "background"
                    : assetType === "banner"     ? "banner"
                    : assetType === "icon"        ? "icon"
                    : assetType === "screenshot"  ? "screenshot"
                    : "cover";

    const urls = cdnAssets[typeKey] || [];
    if (urls.length === 0) return { ok: true, results: [] };

    // Fetch each URL and convert to base64 data URLs for preview.
    const results = [];
    for (const u of urls) {
      if (u.startsWith("data:")) {
        // Already a data URL (e.g. cover from the helper).
        results.push({
          titleId:   tidHex,
          front:     u,
          thumbnail: u,
          url:       null,
          official:  true,
          rating:    null,
          source:    "xbox-cdn",
          assetType: typeKey,
        });
        continue;
      }
      const buf = await fetchHttpImage(u);
      if (buf && buf.length >= 100) {
        const mime = (buf[0] === 0xFF && buf[1] === 0xD8) ? "image/jpeg" : "image/png";
        const dataUrl = `data:${mime};base64,${buf.toString("base64")}`;
        results.push({
          titleId:   tidHex,
          front:     dataUrl,
          thumbnail: dataUrl,
          url:       u,
          official:  true,
          rating:    null,
          source:    "xbox-cdn",
          assetType: typeKey,
        });
      }
    }

    return { ok: true, results };
  });

  // ── Fetch a remote image URL and return as base64 data URL (for preview) ─────
  ipcMain.handle("xbox:fetch-url-image", async (_event, url) => {
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

  // ── Open a native file picker and return the selected image as data URL ───────
  ipcMain.handle("xbox:choose-image-file", async () => {
    const win = BrowserWindow.getFocusedWindow() || mainWindow;
    const r   = await dialog.showOpenDialog(win || undefined, {
      title:   "Choose image",
      filters: [{ name: "Images", extensions: ["jpg", "jpeg", "png", "bmp", "gif"] }],
      properties: ["openFile"],
    });
    if (r.canceled || !r.filePaths[0]) return { ok: false };
    const filePath = r.filePaths[0];
    const buf = fs.readFileSync(filePath);
    const ext = path.extname(filePath).toLowerCase();
    const mime = (ext === ".png") ? "image/png" : (ext === ".gif") ? "image/gif" : "image/jpeg";
    return { ok: true, dataUrl: `data:${mime};base64,${buf.toString("base64")}`, ext, filePath };
  });

  // ── Upload an asset image to the console via FTP → User/Import/{TitleId}/ ────
  // Payload: { titleId, assetType, imageBase64?, imageUrl?, ext? }
  // assetType is the import folder filename stem: "background", "banner", "icon",
  // "cover", "screenshot1" … "screenshot5".
  ipcMain.handle("xbox:upload-asset-to-console", async (_event, payload) => {
    const p = payload || {};
    const titleId   = typeof p.titleId   === "string" ? p.titleId.trim().toUpperCase()  : "";
    const assetType = typeof p.assetType === "string" ? p.assetType.trim().toLowerCase() : "";
    if (!titleId || !/^[0-9A-F]{8}$/.test(titleId)) {
      return { ok: false, error: "Invalid or missing titleId." };
    }
    const allowedTypes = new Set([
      "background", "banner", "icon", "cover",
      ...Array.from({ length: 10 }, (_, i) => `screenshot${i + 1}`),
    ]);
    if (!allowedTypes.has(assetType)) {
      return { ok: false, error: `Unknown assetType: ${assetType}` };
    }

    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    // Resolve image buffer from base64 payload or by fetching a URL.
    let imgBuf = null;
    if (typeof p.imageBase64 === "string" && p.imageBase64.length > 0) {
      imgBuf = Buffer.from(p.imageBase64, "base64");
    } else if (typeof p.imageUrl === "string" && p.imageUrl.startsWith("http")) {
      imgBuf = await fetchHttpImage(p.imageUrl);
    }
    if (!imgBuf || imgBuf.length < 100) {
      return { ok: false, error: "No valid image data provided." };
    }

    const ext        = (typeof p.ext === "string" && p.ext.startsWith(".")) ? p.ext : imageExtFromMagic(imgBuf);
    const scriptsPath = getConfiguredFtpScriptsPath();
    const auroraRoot  = xboxAuroraRoot(scriptsPath);
    const importDir   = `${auroraRoot}/User/Import/${titleId}`;
    const remotePath  = `${importDir}/${assetType}${ext}`;

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 30000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      await client.ensureDir(importDir);
      const stream = Readable.from([imgBuf]);
      await client.uploadFrom(stream, remotePath);
      appendAppEvent("AURORA_ASSET", `uploaded ${assetType}${ext} for ${titleId} → ${remotePath}`);

      // Bump the cache fingerprint so the next library load re-syncs visuals.
      const cacheRoot = getAuroraLibraryCacheRoot(app, xboxIp, auroraRoot);
      const metaObj   = readMeta(cacheRoot);
      if (metaObj) writeMeta(cacheRoot, { ...metaObj, updatedAt: Date.now() });

      return { ok: true, remotePath };
    } catch (err) {
      const msg = err.message || String(err);
      appendAppEvent("AURORA_ASSET", `upload error ${titleId}/${assetType}: ${msg}`);
      return { ok: false, error: msg };
    } finally {
      client.close();
    }
  });

  // ── Decode an existing .asset file from the console (RXEA → PNG) ────────────
  // Payload: { titleId, gameDataDir }
  // Returns: { ok, slots: [ { slot, width, height, key, dataUrl } ] }
  //   key is the visual-manifest field: "background"|"banner"|"icon"|"cover"|"screenshot{N}"
  ipcMain.handle("xbox:decode-asset", async (_event, payload) => {
    const p = payload || {};
    const titleId    = typeof p.titleId    === "string" ? p.titleId.trim().toUpperCase()  : "";
    const gameDataDir = typeof p.gameDataDir === "string" ? p.gameDataDir.trim() : "";
    if (!titleId || !gameDataDir) return { ok: false, error: "titleId and gameDataDir required." };

    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const scriptsPath  = getConfiguredFtpScriptsPath();
    const auroraRoot   = xboxAuroraRoot(scriptsPath);
    const gameDataPath = `${auroraRoot}/Data/GameData/${gameDataDir}`;
    const port         = getConfiguredServerPort();

    // Asset files Aurora creates for this title (prefix+TitleId naming).
    const assetFiles = [
      { name: `BK${titleId}.asset`, slotKeys: { 4: "background" } },
      { name: `GC${titleId}.asset`, slotKeys: { 2: "cover"      } },
      { name: `GL${titleId}.asset`, slotKeys: { 0: "icon", 1: "banner" } },
      { name: `SS${titleId}.asset`, slotKeys: null /* screenshots */ },
    ];

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 25000;
    const allSlots = [];

    try {
      addOutputLine(`[INFO] RXEA decode ${titleId}: connecting to ${xboxIp}…`);
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

      for (const { name, slotKeys } of assetFiles) {
        const assetBuf = await ftpTryDownloadFile(client, `${gameDataPath}/${name}`);
        if (!assetBuf || assetBuf.length < 2048) {
          addOutputLine(`[INFO] RXEA decode ${titleId}: ${name} not found or too small, skipping.`);
          continue;
        }

        addOutputLine(`[INFO] RXEA decode ${titleId}: decoding ${name} (${assetBuf.length} bytes)…`);

        // POST raw RXEA bytes to Go server for decode.
        const decoded = await new Promise((resolve) => {
          const req = http.request(
            { host: "127.0.0.1", port, path: "/rxea/decode", method: "POST",
              headers: { "Content-Type": "application/octet-stream", "Content-Length": assetBuf.length } },
            (res) => {
              const chunks = [];
              res.on("data", (c) => chunks.push(c));
              res.on("end", () => {
                try { resolve(JSON.parse(Buffer.concat(chunks).toString("utf8"))); }
                catch (e) {
                  addOutputLine(`[WARN] RXEA decode ${titleId}: JSON parse error for ${name}: ${e.message}`);
                  resolve(null);
                }
              });
            }
          );
          req.on("error", (e) => {
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
              addOutputLine(`[DIAG] slot${d.slot}: off=${d.offset} sz=${d.size} fmt=${d.gpu_fmt} w=${d.width} h=${d.height} tiled=${d.tiled} endian=${d.endian}${d.error ? ` err="${d.error}"` : ""}`);
            }
          }
          continue;
        }

        // Even when slots decoded successfully, log any per-slot errors from diags.
        if (Array.isArray(decoded.diags)) {
          for (const d of decoded.diags.filter(x => x.error)) {
            addOutputLine(`[DIAG] slot${d.slot} error: fmt=${d.gpu_fmt} w=${d.width} h=${d.height} — ${d.error}`);
          }
        }

        const decodedCount = decoded.slots.length;
        addOutputLine(`[INFO] RXEA decode ${titleId}: ${name} → ${decodedCount} slot(s).`);

        for (const s of decoded.slots) {
          let key = null;
          if (slotKeys && slotKeys[s.slot] !== undefined) {
            key = slotKeys[s.slot];
          } else if (s.slot >= 5 && s.slot <= 24) {
            key = `screenshot${s.slot - 4}`;
          }
          if (!key) continue;

          // s.png is base64 (Go json.Marshal encodes []byte as base64).
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
    } catch (err) {
      const msg = err.message || String(err);
      addOutputLine(`[WARN] RXEA decode ${titleId}: ${msg}`);
      return { ok: false, error: msg };
    } finally {
      client.close();
    }
  });

  // ── Encode a PNG and upload it as an RXEA .asset directly to the console ────
  // Payload: { titleId, gameDataDir, slot, imageBase64?, imageUrl?, ext? }
  // slot: 0=icon, 1=banner, 2=cover, 4=background, 5-24=screenshots
  // Uploads to Data/GameData/{dir}/{PREFIX}{TitleId}.asset
  ipcMain.handle("xbox:encode-asset", async (_event, payload) => {
    const p = payload || {};
    const titleId     = typeof p.titleId     === "string" ? p.titleId.trim().toUpperCase() : "";
    const gameDataDir = typeof p.gameDataDir === "string" ? p.gameDataDir.trim() : "";
    const slotNum     = typeof p.slot        === "number" ? p.slot : parseInt(p.slot, 10);
    if (!titleId || !gameDataDir || isNaN(slotNum) || slotNum < 0 || slotNum > 24) {
      return { ok: false, error: "titleId, gameDataDir, and slot (0–24) required." };
    }

    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    // Resolve image bytes.
    let imgBuf = null;
    if (typeof p.imageBase64 === "string" && p.imageBase64.length > 0) {
      imgBuf = Buffer.from(p.imageBase64, "base64");
    } else if (typeof p.imageUrl === "string" && p.imageUrl.startsWith("http")) {
      imgBuf = await fetchHttpImage(p.imageUrl);
    }
    if (!imgBuf || imgBuf.length < 100) return { ok: false, error: "No valid image data." };

    // Convert to PNG if not already (the Go encoder expects PNG).
    const isPng = imgBuf[0] === 0x89 && imgBuf[1] === 0x50;
    let pngBuf = imgBuf;
    if (!isPng) {
      // We can only pass PNG to the Go encoder; if JPEG, flag an error for now.
      // (JPEG→PNG conversion would require sharp/jimp which we avoid.)
      return { ok: false, error: "Image must be PNG format for RXEA encoding." };
    }

    const port = getConfiguredServerPort();

    // POST PNG to Go server → receive RXEA bytes.
    const rxeaBuf = await new Promise((resolve) => {
      const req = http.request(
        { host: "127.0.0.1", port, path: `/rxea/encode?slot=${slotNum}`, method: "POST",
          headers: { "Content-Type": "image/png", "Content-Length": pngBuf.length } },
        (res) => {
          const chunks = [];
          res.on("data", (c) => chunks.push(c));
          res.on("end", () => resolve(res.statusCode === 200 ? Buffer.concat(chunks) : null));
        }
      );
      req.on("error", () => resolve(null));
      req.end(pngBuf);
    });

    if (!rxeaBuf || rxeaBuf.length < 2048) {
      return { ok: false, error: "RXEA encoding failed — Go server returned no data." };
    }

    // Determine the .asset filename prefix for this slot.
    const prefixMap = { 0: "GL", 1: "GL", 2: "GC", 3: "GC", 4: "BK" };
    const prefix = slotNum >= 5 ? "SS" : (prefixMap[slotNum] || "GC");
    const assetName = `${prefix}${titleId}.asset`;

    const scriptsPath  = getConfiguredFtpScriptsPath();
    const auroraRoot   = xboxAuroraRoot(scriptsPath);
    const remotePath   = `${auroraRoot}/Data/GameData/${gameDataDir}/${assetName}`;

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 30000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      const stream = Readable.from([rxeaBuf]);
      await client.uploadFrom(stream, remotePath);
      appendAppEvent("AURORA_ASSET", `encoded+uploaded slot${slotNum} → ${remotePath} (${rxeaBuf.length} B)`);
      return { ok: true, remotePath, rxeaSize: rxeaBuf.length };
    } catch (err) {
      const msg = err.message || String(err);
      appendAppEvent("AURORA_ASSET", `encode-upload error: ${msg}`);
      return { ok: false, error: msg };
    } finally {
      client.close();
    }
  });

  // ── Aurora inspector: GameData + Media inventory, GameCoverInfo summary ───
  ipcMain.handle("xbox:inspect-aurora-game", async (_event, payload) => {
    const p = payload || {};
    const titleId = typeof p.titleId === "string" ? p.titleId.trim().toUpperCase() : "";
    const gameDataDir = typeof p.gameDataDir === "string" ? p.gameDataDir.trim() : "";
    if (!titleId || !gameDataDir) {
      return { ok: false, error: "titleId and gameDataDir are required." };
    }

    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const scriptsPath = getConfiguredFtpScriptsPath();
    const auroraRoot  = xboxAuroraRoot(scriptsPath);
    const mediaDir    = xboxAuroraMediaDir(scriptsPath);
    const gameDataPath = `${auroraRoot}/Data/GameData/${gameDataDir}`;

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 25000;

    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

      let gameDataFiles = [];
      try {
        const list = await client.list(gameDataPath);
        gameDataFiles = list
          .filter((e) => e && e.name && e.name !== "." && e.name !== "..")
          .map((e) => ({
            name:  e.name,
            size:  e.size != null ? Number(e.size) : null,
            isDir: e.type === 2,
            kind:  classifyAuroraFileKind(e.name),
          }))
          .sort((a, b) => a.name.localeCompare(b.name));
      } catch { /* missing folder */ }

      let mediaFiles = [];
      try {
        const mlist = await client.list(mediaDir);
        mediaFiles = mlist
          .filter((e) => e && e.name && e.name.startsWith(titleId))
          .map((e) => ({
            name:    e.name,
            size:    e.size != null ? Number(e.size) : null,
            ftpPath: `${mediaDir}/${e.name}`,
            kind:    classifyAuroraFileKind(e.name),
          }))
          .sort((a, b) => a.name.localeCompare(b.name));
      } catch { /* no Media */ }

      let gameCoverInfoText = null;
      try {
        const chunks = [];
        await client.downloadTo(
          new Writable({ write(c, _, cb) { chunks.push(c); cb(); } }),
          `${gameDataPath}/GameCoverInfo.bin`
        );
        gameCoverInfoText = Buffer.concat(chunks).toString("utf8");
      } catch { /* no GameCoverInfo */ }

      const gameCoverInfo = summarizeGameCoverInfoJson(gameCoverInfoText);

      return {
        ok: true,
        auroraRoot,
        gameDataPath,
        mediaDir,
        gameDataFiles,
        mediaFiles,
        gameCoverInfo,
      };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── Toolbox: backend POST helper ──────────────────────────────────────────
  function backendPost(urlPath, body, timeoutMs = 600000) {
    const port = getConfiguredServerPort();
    const payload = JSON.stringify(body);
    return new Promise((resolve, reject) => {
      const opts = {
        hostname: "localhost",
        port,
        path: urlPath,
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Content-Length": Buffer.byteLength(payload),
        },
      };
      const req = http.request(opts, (res) => {
        let data = "";
        res.on("data", (c) => { data += c; });
        res.on("end", () => {
          try { resolve(JSON.parse(data)); }
          catch { resolve({ error: data }); }
        });
      });
      req.on("error", reject);
      req.setTimeout(timeoutMs, () => { req.destroy(); reject(new Error("Timeout")); });
      req.write(payload);
      req.end();
    });
  }

  // ── Toolbox: choose ISO files ─────────────────────────────────────────────
  ipcMain.handle("tools:choose-iso-files", async () => {
    const result = await dialog.showOpenDialog(mainWindow, {
      title: "Select Xbox 360 ISO files",
      filters: [{ name: "ISO images", extensions: ["iso"] }],
      properties: ["openFile", "multiSelections"],
    });
    if (result.canceled || !result.filePaths.length) return { ok: false };
    return { ok: true, files: result.filePaths };
  });

  // ── Toolbox: choose output folder ─────────────────────────────────────────
  ipcMain.handle("tools:choose-output-folder", async () => {
    const result = await dialog.showOpenDialog(mainWindow, {
      title: "Select output folder",
      properties: ["openDirectory", "createDirectory"],
    });
    if (result.canceled || !result.filePaths.length) return { ok: false };
    return { ok: true, folder: result.filePaths[0] };
  });

  // ── Toolbox: probe ISO ────────────────────────────────────────────────────
  ipcMain.handle("tools:probe-iso", async (_event, isoPath) => {
    try {
      const r = await backendPost("/tools/probe-iso", { isoPath });
      if (r.error && !r.titleId) return { ok: false, error: r.error };
      return { ok: true, ...r };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  // ── Toolbox: ISO → GOD ────────────────────────────────────────────────────
  ipcMain.handle("tools:iso2god", async (_event, { isoPath, outDir }) => {
    try {
      const r = await backendPost("/tools/iso2god", { isoPath, outDir });
      if (r.error && !r.ok) return { ok: false, error: r.error };
      return { ok: true, ...r };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  // ── Toolbox: ISO → XEX ────────────────────────────────────────────────────
  ipcMain.handle("tools:iso2xex", async (_event, { isoPath, outDir }) => {
    try {
      const r = await backendPost("/tools/iso2xex", { isoPath, outDir });
      if (r.error && !r.ok) return { ok: false, error: r.error };
      return { ok: true, ...r };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  // ── Toolbox: FTP Manager — list directory ─────────────────────────────────
  ipcMain.handle("tools:ftp-list", async (_event, remotePath) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 15000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      await client.cd(remotePath || "/");
      const list = await client.list();
      const entries = list.map(e => ({
        name: e.name,
        type: e.type === 2 ? "dir" : "file",
        size: e.size || 0,
      }));
      return { ok: true, entries, cwd: remotePath || "/" };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── Toolbox: FTP Manager — choose local files to upload ───────────────────
  ipcMain.handle("tools:ftp-choose-files", async () => {
    const result = await dialog.showOpenDialog(mainWindow, {
      title: "Select files to upload to Xbox",
      properties: ["openFile", "multiSelections"],
    });
    if (result.canceled || !result.filePaths.length) return { ok: false };
    return { ok: true, files: result.filePaths };
  });

  // ── Toolbox: FTP Manager — choose local folder to upload ──────────────────
  ipcMain.handle("tools:ftp-choose-folder", async () => {
    const result = await dialog.showOpenDialog(mainWindow, {
      title: "Select folder to upload to Xbox",
      properties: ["openDirectory"],
    });
    if (result.canceled || !result.filePaths.length) return { ok: false };
    return { ok: true, folder: result.filePaths[0] };
  });

  // ── Toolbox: FTP Manager — upload files (adds to queue) ───────────────────
  let _ftpUploadId = 0;
  const _ftpUploadJobs = new Map(); // id → { id, name, state, progress, error }

  ipcMain.handle("tools:ftp-upload", async (_event, { localPaths, remotePath }) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const jobs = [];
    for (const lp of localPaths) {
      const id = ++_ftpUploadId;
      const name = path.basename(lp);
      const job = { id, name, localPath: lp, remotePath: `${remotePath}/${name}`, state: "Queued", progress: 0, error: null };
      _ftpUploadJobs.set(id, job);
      jobs.push({ id, name });

      // Start upload in background
      (async () => {
        const client = new ftp.Client();
        client.ftp.verbose = false;
        client.ftp.timeout = 30000;
        try {
          job.state = "Processing";
          await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

          const stat = fs.statSync(lp);
          if (stat.isDirectory()) {
            await client.uploadFromDir(lp, `${remotePath}/${name}`);
          } else {
            client.trackProgress((info) => {
              if (stat.size > 0) job.progress = Math.round((info.bytes / stat.size) * 100);
            });
            await client.uploadFrom(lp, `${remotePath}/${name}`);
          }
          job.state = "Ready";
          job.progress = 100;
        } catch (err) {
          job.state = "Error";
          job.error = err.message || String(err);
        } finally {
          client.close();
        }
      })();
    }
    return { ok: true, jobs };
  });

  // ── Toolbox: FTP Manager — get upload queue status ────────────────────────
  ipcMain.handle("tools:ftp-upload-status", async () => {
    const jobs = [];
    for (const [, job] of _ftpUploadJobs) {
      jobs.push({ id: job.id, name: job.name, state: job.state, progress: job.progress, error: job.error, remotePath: job.remotePath });
    }
    return { ok: true, jobs };
  });

  // ── Toolbox: FTP Manager — remove completed/errored upload ────────────────
  ipcMain.handle("tools:ftp-upload-remove", async (_event, id) => {
    _ftpUploadJobs.delete(id);
    return { ok: true };
  });

  // ── Toolbox: FTP Manager — delete remote file/folder ──────────────────────
  ipcMain.handle("tools:ftp-delete", async (_event, remotePath) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 15000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      try {
        await client.remove(remotePath);
      } catch {
        await client.removeDir(remotePath);
      }
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── Toolbox: FTP Manager — create remote directory ────────────────────────
  ipcMain.handle("tools:ftp-mkdir", async (_event, remotePath) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 15000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      await client.ensureDir(remotePath);
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── Toolbox: FTP Manager — rename / move a remote file or directory ───────
  ipcMain.handle("tools:ftp-rename", async (_event, { from, to }) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 30000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      await client.rename(from, to);
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── Toolbox: FTP Manager — copy a remote file or directory (download+reupload) ─
  // Xbox 360 FTP servers typically don't support server-side copy, so we
  // download to a temp file and re-upload. For directories we recurse.
  ipcMain.handle("tools:ftp-copy", async (_event, { src, dst, isDir }) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const tmpDir = path.join(require("os").tmpdir(), "godsend-ftp-copy-" + Date.now());
    fs.mkdirSync(tmpDir, { recursive: true });

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 60000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

      if (isDir) {
        const localDir = path.join(tmpDir, path.basename(src));
        await client.downloadToDir(localDir, src);
        await client.uploadFromDir(localDir, dst);
      } else {
        const localFile = path.join(tmpDir, path.basename(src));
        await client.downloadTo(localFile, src);
        await client.uploadFrom(localFile, dst);
      }
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
      // Clean up temp files
      fs.rm(tmpDir, { recursive: true, force: true }, () => {});
    }
  });

  // ── Move game to a different Xbox drive ───────────────────────────────────
  // Queues an FTP job: rename each item from source to destination drive.
  // Aurora game paths follow: /{Drive}/Content/... or /{Drive}/GOD/... etc.
  ipcMain.handle("xbox:move-game", async (_event, { game, targetDrive }) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };
    if (!game || !targetDrive) return { ok: false, error: "Missing game or target drive." };

    const gameName = game.name || game.titleId || "Unknown";
    const srcDrive = game.sourceDrive;
    const gameDir  = game.directory;
    if (!srcDrive || !gameDir) return { ok: false, error: "Game has no source drive or directory info." };
    if (srcDrive + ":" === targetDrive || srcDrive === targetDrive) {
      return { ok: false, error: "Source and destination drive are the same." };
    }

    // Build source and destination FTP paths.
    // game.directory is typically like "Content\\0000000000000000\\DEADBEEF\\00000002"
    // or "GOD\\GameName - TITLEID" etc. The sourceDrive is e.g. "Hdd1".
    const dirNorm = gameDir.replace(/\\/g, "/");
    const srcPath = "/" + srcDrive + "/" + dirNorm;
    const dstDriveClean = targetDrive.replace(/:$/, "");
    const dstPath = "/" + dstDriveClean + "/" + dirNorm;

    // Create the job entry in the FTP upload queue for visibility.
    const id = ++_ftpUploadId;
    const job = {
      id,
      name: `Move: ${gameName} → ${dstDriveClean}`,
      localPath: null,
      remotePath: dstPath,
      state: "Queued",
      progress: 0,
      error: null,
    };
    _ftpUploadJobs.set(id, job);

    // Perform the move in the background
    (async () => {
      const moveClient = new ftp.Client();
      moveClient.ftp.verbose = false;
      moveClient.ftp.timeout = 60000;
      try {
        job.state = "Processing";
        await moveClient.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

        // Ensure parent directories exist on destination
        const dstParent = dstPath.split("/").slice(0, -1).join("/") || "/";
        await moveClient.ensureDir(dstParent);
        // Reset CWD after ensureDir
        await moveClient.cd("/");

        // Try RNFR/RNTO (rename = move) first — works across drives on some FTP servers
        try {
          await moveClient.rename(srcPath, dstPath);
          job.state = "Ready";
          job.progress = 100;
          return;
        } catch {
          // Rename across drives not supported; fall back to download + reupload + delete
        }

        // Fallback: download to temp, upload to dest, delete source
        const tmpDir = path.join(require("os").tmpdir(), "godsend-move-" + Date.now());
        fs.mkdirSync(tmpDir, { recursive: true });

        job.progress = 10;
        const localDir = path.join(tmpDir, path.basename(srcPath));
        await moveClient.downloadToDir(localDir, srcPath);

        job.progress = 50;
        await moveClient.uploadFromDir(localDir, dstPath);

        job.progress = 90;
        // Remove source directory
        await moveClient.removeDir(srcPath);

        job.state = "Ready";
        job.progress = 100;

        // Clean up temp
        fs.rm(tmpDir, { recursive: true, force: true }, () => {});
      } catch (err) {
        job.state = "Error";
        job.error = err.message || String(err);
      } finally {
        moveClient.close();
      }
    })();

    return { ok: true, jobId: id, message: `Queued move of ${gameName} to ${dstDriveClean}` };
  });
}

// ── Game library helpers ───────────────────────────────────────────────────────

// Derive the Aurora root from the configured FTP scripts path.
//   /Hdd1/Aurora/User/Scripts/Utility/GODSend  →  /Hdd1/Aurora
//   /Usb0/Apps/Aurora/User/Scripts/Utility/…   →  /Usb0/Apps/Aurora
let _lastDiscoveredAuroraRoot = null;
function xboxAuroraRoot(ftpScriptsPath) {
  if (ftpScriptsPath) {
    const parts = ftpScriptsPath.replace(/\\/g, "/").split("/").filter(Boolean);
    const idx   = parts.findIndex((p) => p.toLowerCase() === "aurora");
    if (idx !== -1) return "/" + parts.slice(0, idx + 1).join("/");
  }
  if (_lastDiscoveredAuroraRoot) return _lastDiscoveredAuroraRoot;
  return "/Hdd1/Aurora";
}

/**
 * Auto-discover the Aurora install path by probing common locations on the
 * console's FTP. Aurora's FTP server returns the root listing for any
 * non-existent absolute path, so we walk paths segment-by-segment and
 * verify with `pwd`. Returns the first location whose `Data/Databases`
 * subfolder exists.
 */
async function discoverAuroraRoot(client) {
  const candidates = [
    ["Hdd1", "Aurora"],
    ["Usb0", "Apps", "Aurora"],
    ["Hdd1", "Apps", "Aurora"],
    ["Usb0", "Aurora"],
    ["Usb1", "Apps", "Aurora"],
    ["Usb1", "Aurora"],
    ["HddX", "Aurora"],
  ];
  for (const segs of candidates) {
    try {
      await client.cd("/");
      for (const s of segs) await client.cd(s);
      await client.cd("Data");
      await client.cd("Databases");
      const pwd = (await client.pwd()).replace(/\\/g, "/").replace(/\/+$/, "");
      const expected = "/" + segs.join("/") + "/Data/Databases";
      if (pwd.toLowerCase() === expected.toLowerCase()) {
        return "/" + segs.join("/");
      }
    } catch { /* try next */ }
  }
  return null;
}

function xboxAuroraMediaDir(ftpScriptsPath) {
  return xboxAuroraRoot(ftpScriptsPath) + "/Media";
}

// ── sql.js singleton ──────────────────────────────────────────────────────────
let _SQL = null;
async function getSqlJs() {
  if (_SQL) return _SQL;
  const initSqlJs = require("sql.js");
  // Locate the WASM file both in dev (node_modules) and packaged (asar.unpacked).
  const wasmPath = app.isPackaged
    ? path.join(
        process.resourcesPath,
        "app.asar.unpacked",
        "node_modules",
        "sql.js",
        "dist",
        "sql-wasm.wasm"
      )
    : path.join(__dirname, "..", "node_modules", "sql.js", "dist", "sql-wasm.wasm");

  const opts = {};
  if (fs.existsSync(wasmPath)) {
    opts.wasmBinary = fs.readFileSync(wasmPath);
  } else {
    opts.locateFile = () => wasmPath;
  }
  _SQL = await initSqlJs(opts);
  return _SQL;
}

// ── FILETIME helper ───────────────────────────────────────────────────────────
// Aurora stores dates as Windows FILETIME (100-ns ticks since 1601-01-01).
function filetimeToDateStr(ft) {
  if (!ft || ft === 0) return null;
  try {
    // Use BigInt to avoid floating-point precision loss.
    const ms = Number(BigInt(Math.round(Number(ft))) / 10000n) - 11644473600000;
    if (ms < 0 || ms > 9999999999999) return null;
    return new Date(ms).toISOString().split("T")[0]; // "YYYY-MM-DD"
  } catch { return null; }
}

// ── HTTP image fetch helper ───────────────────────────────────────────────────
// Fetches an image URL (http or https), follows one redirect, returns Buffer.
function fetchHttpImage(url, redirectCount = 0) {
  return new Promise((resolve) => {
    if (!url || redirectCount > 3) { resolve(null); return; }
    const mod = url.startsWith("https") ? https : http;
    const req = mod.get(
      url,
      { headers: { "User-Agent": "Aurora/0.7b GODsend" }, timeout: 12000 },
      (res) => {
        if (res.statusCode === 301 || res.statusCode === 302 || res.statusCode === 307) {
          const loc = res.headers.location;
          res.resume();
          fetchHttpImage(loc, redirectCount + 1).then(resolve);
          return;
        }
        if (res.statusCode !== 200) { res.resume(); resolve(null); return; }
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end",  () => resolve(Buffer.concat(chunks)));
        res.on("error", () => resolve(null));
      }
    );
    req.on("error",   () => resolve(null));
    req.on("timeout", () => { req.destroy(); resolve(null); });
  });
}

// Parse a rows/columns sql.js result into an array of plain objects.
function sqlRows(result) {
  if (!result || !result[0]) return [];
  const { columns, values } = result[0];
  return values.map((row) => {
    const obj = {};
    columns.forEach((c, i) => { obj[c] = row[i]; });
    return obj;
  });
}

function xboxBuildGameNameMap() {
  const map = new Map();
  const cacheDir = app.isPackaged
    ? path.join(process.resourcesPath, "cache")
    : path.join(__dirname, "..", "..", "..", "cache");

  for (const file of ["xbox360.json", "xbla.json", "games.json", "digital.json", "xbox.json"]) {
    try {
      const raw   = fs.readFileSync(path.join(cacheDir, file), "utf8");
      const data  = JSON.parse(raw);
      const items = Array.isArray(data) ? data : Object.values(data).flat();
      for (const item of items) {
        if (!item || typeof item !== "object") continue;
        const titleId = String(item.titleId || item.TitleId || item.title_id || "").toUpperCase().trim();
        const name    = String(item.title  || item.name   || item.Title    || item.Name || "").trim();
        if (titleId && name && /^[0-9A-F]{8}$/.test(titleId)) map.set(titleId, name);
      }
    } catch { /* cache file absent or unparseable — skip */ }
  }
  return map;
}

function bootstrapApp() {
  app.whenReady().then(() => {
    protocol.handle("godsend-aurora", (request) => {
      const root = getActiveAuroraCacheRoot();
      if (!root) return new Response(null, { status: 404 });
      let u;
      try {
        u = new URL(request.url);
      } catch {
        return new Response(null, { status: 400 });
      }
      if (u.hostname !== "cdn") return new Response(null, { status: 404 });
      const rel = (u.pathname || "").replace(/^\/+/, "");
      const full = safeFileUnderRoot(root, rel);
      if (!full || !fs.existsSync(full)) return new Response(null, { status: 404 });
      try {
        return electronNet.fetch(pathToFileURL(full).href);
      } catch {
        return new Response(null, { status: 500 });
      }
    });

    app.setAppUserModelId("com.abbu.godsend");
    appendAppEvent(
      "LIFECYCLE",
      `app ready userData=${app.getPath("userData")} logDir=${getLogInfo().logsDirectory}`
    );

    createMainWindow();
    createTray(mainWindow, {
      onQuit: () => {
        isQuitting = true;
        app.quit();
      },
    });

    // Start backend after UI loads so the window is visible first.
    mainWindow.webContents.once("did-finish-load", () => {
      startGodsend();
    });
  });

  app.on("before-quit", () => {
    isQuitting = true;
    appendAppEvent("LIFECYCLE", "application before-quit");
    stopGodsend();
  });

  app.on("window-all-closed", (event) => {
    event.preventDefault();
  });

  registerIpcHandlers();
}

module.exports = { bootstrapApp };
