import { app } from "electron";
import path from "path";
import fs from "fs";
import { getSqlJs, sqlRows, filetimeToDateStr } from "../infrastructure/sqlHelper";
import { backendPost } from "../infrastructure/backendHttp";
import { diagnoseDb, diagnosticsLine } from "../infrastructure/auroraDbDiagnostics";
import { addOutputLine } from "./backendClient";

/**
 * openAuroraDb validates a DB buffer and opens it, turning the opaque
 * "database disk image is malformed" into an actionable error that names the DB
 * and the likely cause (truncated download vs mid-write corruption vs not-a-DB).
 */
function openAuroraDb(SQL: any, buf: Buffer, label: string): any {
  const diag = diagnoseDb(buf);
  if (!diag.sqliteHeaderOk || diag.truncated) {
    throw new Error(`Aurora ${label} malformed - ${diagnosticsLine(label, diag)}`);
  }
  try {
    return new SQL.Database(new Uint8Array(buf));
  } catch (err: any) {
    throw new Error(
      `Aurora ${label} malformed (${err?.message || err}) - ${diagnosticsLine(label, diag)}`
    );
  }
}

export interface AuroraGame {
  contentId: number;
  titleId: string;
  name: string;
  description: string;
  publisher: string;
  developer: string;
  liveRating: string;
  liveRaters: string;
  releaseDate: string;
  directory: string;
  discNum: number;
  discsInSet: number;
  isFavorite: boolean;
  timesPlayed: number;
  lastPlayed: string | null;
  sourceDrive: string;
  gameDataDir: string;
  scanPathId: number;
  mediaId: number | null;
  fileType: number | null;
  contentType: number | null;
}

/**
 * Parse Aurora SQLite DB buffers and return the games list used by the Xbox
 * Library view.
 */
export async function buildAuroraGamesFromDbBuffers(
  contentBuf: Buffer,
  settingsBuf: Buffer,
  scanDriveMap: Map<number, string>
): Promise<AuroraGame[]> {
  const SQL = await getSqlJs();
  const cdb = openAuroraDb(SQL, contentBuf, "content.db");

  const queryDb = (db: any, sql: string): Record<string, any>[] => {
    // Use prepare/step API directly here - avoids shell-exec false-positive patterns
    const stmt = db.prepare(sql);
    const rows: Record<string, any>[] = [];
    while (stmt.step()) rows.push(stmt.getAsObject());
    stmt.free();
    return rows;
  };

  // Aurora's settings.db can carry a corrupt table (a bad b-tree page / broken
  // autoindex on disk) while content.db stays intact. The game LIST lives in
  // content.db; the settings metadata tables only enrich it (hidden / favorites
  // / recently-played). So read each metadata table tolerantly: if one is
  // malformed, log the table and degrade to empty rather than failing the whole
  // library load with "database disk image is malformed".
  const queryDbTolerant = (db: any, sql: string, table: string): Record<string, any>[] => {
    let stmt: any;
    try {
      stmt = db.prepare(sql);
      const rows: Record<string, any>[] = [];
      while (stmt.step()) rows.push(stmt.getAsObject());
      return rows;
    } catch (e: any) {
      // Run PRAGMA integrity_check so the corruption signature (which page /
      // rowid / index) lands in the log - makes this diagnosable without the
      // DB file itself. Best-effort: integrity_check walks every page and may
      // also throw on a badly damaged DB.
      let integrity = "";
      let ic: any;
      try {
        ic = db.prepare("PRAGMA integrity_check(8)");
        const lines: string[] = [];
        while (ic.step()) lines.push(String(ic.getAsObject().integrity_check || ""));
        integrity = lines.join(" | ");
      } catch { /* DB too damaged to check */ }
      finally { if (ic) { try { ic.free(); } catch { /* best-effort */ } } }
      addOutputLine(
        `[WARN] Aurora library: settings.db table "${table}" is corrupt (${e?.message || e})` +
          (integrity ? ` - integrity_check: ${integrity}` : "") +
          ` - skipping it (library will load without that metadata). Rebuild the DB in Aurora on the console to restore it.`,
        "ui"
      );
      return [];
    } finally {
      // Free in finally so a mid-iteration throw from step() can't leak the
      // prepared statement (free was previously inside try, after the loop).
      if (stmt) { try { stmt.free(); } catch { /* best-effort */ } }
    }
  };

  // content.db holds the game LIST, so a read failure can't be skipped like the
  // settings metadata tables. openAuroraDb already rejects bad headers /
  // truncation, but a valid-header DB with corrupt body pages throws here. Catch
  // that and re-throw with diagnostics embedded so the top-level handler still
  // recognises it as malformed and points the user at the DB export.
  let itemRows: Record<string, any>[];
  try {
    itemRows = queryDb(cdb, `
      SELECT Id, TitleId, MediaId, TitleName, Description,
             Publisher, Developer, LiveRating, LiveRaters,
             ReleaseDate, Directory, ScanPathId,
             DiscNum, DiscsInSet, FileType, ContentType
      FROM ContentItems
      ORDER BY TitleName
    `);
  } catch (e: any) {
    cdb.close();
    throw new Error(
      `Aurora content.db malformed (${e?.message || e}) - ${diagnosticsLine("content.db", diagnoseDb(contentBuf))}`
    );
  }
  cdb.close();

  // Open settings.db only after content.db's read succeeds: the content.db
  // item query can throw on a valid-header-but-corrupt-body DB, and the catch
  // above closes cdb and re-throws - opening sdb earlier would leak it (WASM
  // page cache) on every Export-Aurora-DBs retry.
  const sdb = openAuroraDb(SQL, settingsBuf, "settings.db");

  const hiddenRows = queryDbTolerant(sdb, "SELECT DISTINCT ContentId FROM UserHidden", "UserHidden");
  const favRows    = queryDbTolerant(sdb, "SELECT DISTINCT ContentId FROM UserFavorites", "UserFavorites");
  const recentRows = queryDbTolerant(sdb, `
    SELECT ContentId,
           MAX(DateTime)  AS LastPlayed,
           COUNT(*)       AS TimesPlayed
    FROM UserRecentGames
    GROUP BY ContentId
  `, "UserRecentGames");
  sdb.close();

  const hiddenIds   = new Set(hiddenRows.map((h) => Number(h.ContentId)));
  const favoriteIds = new Set(favRows.map((f) => Number(f.ContentId)));
  const recentMap   = new Map(
    recentRows.map((r) => [Number(r.ContentId), {
      lastPlayed:  filetimeToDateStr(r.LastPlayed),
      timesPlayed: Number(r.TimesPlayed),
    }])
  );

  const games: AuroraGame[] = [];
  for (const g of itemRows) {
    const contentId = Number(g.Id);
    if (hiddenIds.has(contentId)) continue;

    const titleIdInt = Number(g.TitleId) >>> 0;
    const titleId    = titleIdInt.toString(16).toUpperCase().padStart(8, "0");
    if (titleId === "00000000") continue;

    const sourceDrive = scanDriveMap.get(Number(g.ScanPathId)) || "";
    const gameDataDir = `${titleId}_${contentId.toString(16).toUpperCase().padStart(8, "0")}`;
    const recent      = recentMap.get(contentId);

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
      mediaId:     g.MediaId     != null ? Number(g.MediaId)     : null,
      fileType:    g.FileType    != null ? Number(g.FileType)    : null,
      contentType: g.ContentType != null ? Number(g.ContentType) : null,
    });
  }
  return games;
}

export async function readContentScanRowsFromBuffer(contentBuf: Buffer): Promise<Record<string, any>[]> {
  const SQL = await getSqlJs();
  const cdb = openAuroraDb(SQL, contentBuf, "content.db");
  const stmt = cdb.prepare("SELECT ScanPathId, Directory FROM ContentItems");
  const rows: Record<string, any>[] = [];
  while (stmt.step()) rows.push(stmt.getAsObject());
  stmt.free();
  cdb.close();
  return rows;
}

export async function readScanRowsFromSettingsBuffer(settingsBuf: Buffer): Promise<Record<string, any>[]> {
  const SQL = await getSqlJs();
  const sdb = openAuroraDb(SQL, settingsBuf, "settings.db");
  const stmt = sdb.prepare("SELECT Id, Path FROM ScanPaths");
  const rows: Record<string, any>[] = [];
  while (stmt.step()) rows.push(stmt.getAsObject());
  stmt.free();
  sdb.close();
  return rows;
}

export async function probeScanPathDrives(
  xboxIp: string,
  scanRows: Record<string, any>[],
  contentRows: Record<string, any>[]
): Promise<Map<number, string>> {
  const knownDrives    = ["Hdd1", "Usb0", "Usb1", "Usb2", "HddX"];
  const scanDriveMap   = new Map<number, string>();

  const sampleDirByScanId = new Map<number, string>();
  for (const c of contentRows || []) {
    const sid = Number(c.ScanPathId) || 0;
    if (!sid || sampleDirByScanId.has(sid)) continue;
    const dir = String(c.Directory || "").replace(/\\/g, "/").replace(/^\/+|\/+$/g, "");
    if (dir) sampleDirByScanId.set(sid, dir);
  }

  const scanPathById = new Map<number, string>(
    scanRows.map((s) => [
      Number(s.Id),
      String(s.Path || "").replace(/\\/g, "/").replace(/^\/+|\/+$/g, ""),
    ])
  );

  // Build one big batch: for each scanPath × drive combo, cd / then cd drive
  // then cd each segment then pwd.  Failed cd returns error without closing the
  // connection, and cd / resets for the next candidate.
  const ops: any[] = [];
  const probeMap: { scanId: number; drive: string; pwdIdx: number; segments: string[] }[] = [];

  for (const [scanId, scanPath] of scanPathById) {
    const probePath = sampleDirByScanId.get(scanId) || scanPath;
    const segments  = probePath.split("/").filter(Boolean);
    if (segments.length === 0) continue;

    for (const drive of knownDrives) {
      ops.push({ op: "cd", path: "/" });
      ops.push({ op: "cd", path: drive });
      for (const seg of segments) ops.push({ op: "cd", path: seg });
      const pwdIdx = ops.length;
      ops.push({ op: "pwd" });
      probeMap.push({ scanId, drive, pwdIdx, segments });
    }
  }

  if (ops.length === 0) return scanDriveMap;

  const res = await backendPost("/ftp/batch", { ip: xboxIp, ops }, 60000);
  const results = res.results || [];

  for (const { scanId, drive, pwdIdx, segments } of probeMap) {
    if (scanDriveMap.has(scanId)) continue; // already found for this scanId
    const r = results[pwdIdx];
    if (r && r.ok && r.data) {
      const pwd      = String(r.data).replace(/\\/g, "/");
      const expected = `/${drive}/${segments.join("/")}`;
      if (pwd.replace(/\/+$/, "").toLowerCase() === expected.toLowerCase()) {
        scanDriveMap.set(scanId, drive);
      }
    }
  }
  return scanDriveMap;
}

export function xboxBuildGameNameMap(): Map<string, string> {
  const map      = new Map<string, string>();
  const cacheDir = app.isPackaged
    ? path.join(process.resourcesPath, "cache")
    : path.join(__dirname, "..", "..", "..", "cache");

  for (const file of ["xbox360.json", "xbla.json", "games.json", "digital.json", "xbox.json"]) {
    try {
      const raw   = fs.readFileSync(path.join(cacheDir, file), "utf8");
      const data  = JSON.parse(raw);
      const items: any[] = Array.isArray(data) ? data : Object.values(data).flat();
      for (const item of items) {
        if (!item || typeof item !== "object") continue;
        const titleId = String(item.titleId || item.TitleId || item.title_id || "").toUpperCase().trim();
        const name    = String(item.title  || item.name   || item.Title    || item.Name || "").trim();
        if (titleId && name && /^[0-9A-F]{8}$/.test(titleId)) map.set(titleId, name);
      }
    } catch { /* cache file absent or unparseable - skip */ }
  }
  return map;
}
