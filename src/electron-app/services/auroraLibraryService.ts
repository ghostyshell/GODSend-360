import { app } from "electron";
import path from "path";
import fs from "fs";
import { Client } from "basic-ftp";
import { getSqlJs, sqlRows, filetimeToDateStr } from "../infrastructure/sqlHelper";

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
  const cdb = new SQL.Database(new Uint8Array(contentBuf));
  const sdb = new SQL.Database(new Uint8Array(settingsBuf));

  const queryDb = (db: any, sql: string): Record<string, any>[] => {
    // Use prepare/step API directly here — avoids shell-exec false-positive patterns
    const stmt = db.prepare(sql);
    const rows: Record<string, any>[] = [];
    while (stmt.step()) rows.push(stmt.getAsObject());
    stmt.free();
    return rows;
  };

  const itemRows   = queryDb(cdb, `
    SELECT Id, TitleId, MediaId, TitleName, Description,
           Publisher, Developer, LiveRating, LiveRaters,
           ReleaseDate, Directory, ScanPathId,
           DiscNum, DiscsInSet, FileType, ContentType
    FROM ContentItems
    ORDER BY TitleName
  `);
  cdb.close();

  const hiddenRows = queryDb(sdb, "SELECT DISTINCT ContentId FROM UserHidden");
  const favRows    = queryDb(sdb, "SELECT DISTINCT ContentId FROM UserFavorites");
  const recentRows = queryDb(sdb, `
    SELECT ContentId,
           MAX(DateTime)  AS LastPlayed,
           COUNT(*)       AS TimesPlayed
    FROM UserRecentGames
    GROUP BY ContentId
  `);
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
  const cdb = new SQL.Database(new Uint8Array(contentBuf));
  const stmt = cdb.prepare("SELECT ScanPathId, Directory FROM ContentItems");
  const rows: Record<string, any>[] = [];
  while (stmt.step()) rows.push(stmt.getAsObject());
  stmt.free();
  cdb.close();
  return rows;
}

export async function readScanRowsFromSettingsBuffer(settingsBuf: Buffer): Promise<Record<string, any>[]> {
  const SQL = await getSqlJs();
  const sdb = new SQL.Database(new Uint8Array(settingsBuf));
  const stmt = sdb.prepare("SELECT Id, Path FROM ScanPaths");
  const rows: Record<string, any>[] = [];
  while (stmt.step()) rows.push(stmt.getAsObject());
  stmt.free();
  sdb.close();
  return rows;
}

export async function probeScanPathDrives(
  client: Client,
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

  async function walkRel(segments: string[]): Promise<void> {
    for (const seg of segments) {
      if (!seg) continue;
      await client.cd(seg);
    }
  }

  for (const [scanId, scanPath] of scanPathById) {
    const probePath = sampleDirByScanId.get(scanId) || scanPath;
    const segments  = probePath.split("/").filter(Boolean);
    if (segments.length === 0) continue;

    for (const drive of knownDrives) {
      try {
        await client.cd("/");
        await client.cd(drive);
        await walkRel(segments);
        const pwd      = (await client.pwd()).replace(/\\/g, "/");
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
    } catch { /* cache file absent or unparseable — skip */ }
  }
  return map;
}
