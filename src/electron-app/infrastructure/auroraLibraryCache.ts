import path from "path";
import fs from "fs";
import { App } from "electron";

export interface AuroraLibraryMeta {
  xboxIp: string;
  auroraRoot: string;
  ftpScriptsPath: string;
  contentDbSize: number;
  settingsDbSize: number;
  scanDriveMap: Record<string, string>;
  driveProbeVersion: number;
  updatedAt: number;
}

let activeCacheRoot: string | null = null;

export function setActiveAuroraCacheRoot(root: string | null): void {
  activeCacheRoot = root && typeof root === "string" ? root : null;
}

export function getActiveAuroraCacheRoot(): string | null {
  return activeCacheRoot;
}

export function slugPart(s: any): string {
  return String(s || "")
    .replace(/\\/g, "/")
    .replace(/[^a-zA-Z0-9._-]+/g, "_")
    .replace(/_+/g, "_")
    .slice(0, 120) || "default";
}

export function getAuroraLibraryCacheRoot(app: App, xboxIp: string, auroraRoot: string): string {
  const base = path.join(app.getPath("userData"), "aurora-library-cache");
  const key = `${slugPart(xboxIp)}__${slugPart(auroraRoot)}`;
  return path.join(base, key);
}

export function metaPath(cacheRoot: string): string {
  return path.join(cacheRoot, "meta.json");
}

export function databasesDir(cacheRoot: string): string {
  return path.join(cacheRoot, "databases");
}

export function gameCacheDir(cacheRoot: string, gameDataDir: string): string {
  return path.join(cacheRoot, "games", gameDataDir);
}

export function readMeta(cacheRoot: string): AuroraLibraryMeta | null {
  try {
    const p = metaPath(cacheRoot);
    if (!fs.existsSync(p)) return null;
    return JSON.parse(fs.readFileSync(p, "utf8"));
  } catch {
    return null;
  }
}

export function writeMeta(cacheRoot: string, obj: Partial<AuroraLibraryMeta>): void {
  fs.mkdirSync(cacheRoot, { recursive: true });
  fs.writeFileSync(metaPath(cacheRoot), JSON.stringify(obj, null, 2), "utf8");
}

export function safeFileUnderRoot(root: string, relUnix: string): string | null {
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

export function contentDbPath(cacheRoot: string): string {
  return path.join(databasesDir(cacheRoot), "content.db");
}

export function settingsDbPath(cacheRoot: string): string {
  return path.join(databasesDir(cacheRoot), "settings.db");
}
