/**
 * Resolves where the app stores ALL user data — config.json, logs, caches,
 * runtime/Ready/Temp/Transfer. Determined once at boot via getAppDataDir(),
 * which is then locked in by app.setPath("userData", …) in main.ts so every
 * subsequent app.getPath("userData") call resolves there.
 *
 * Default per platform:
 *   - Portable Windows (PORTABLE_EXECUTABLE_DIR set): <portable-dir>/godsend-data
 *   - Anything else: Electron's default userData (OS Application Support / AppData)
 *
 * Override: a small marker JSON next to the platform default, written by the
 * Settings page. We keep the marker at the *default* location so we can find
 * the override on the next boot without already knowing where it points.
 */

import { app } from "electron";
import path from "path";
import fs from "fs";
import os from "os";

const MARKER_FILENAME = "appdata-override.json";

interface OverrideMarker {
  appDataDir: string;
}

/**
 * The location Electron *would* use for userData if we never touched it.
 * Computed without calling app.getPath("userData") because that may have
 * already been overridden — we need the unaltered platform default.
 */
function getPlatformDefaultUserData(): string {
  const productName = app.getName?.() || "godsend-electron";
  switch (process.platform) {
    case "darwin":
      return path.join(os.homedir(), "Library", "Application Support", productName);
    case "win32":
      return path.join(process.env.APPDATA || path.join(os.homedir(), "AppData", "Roaming"), productName);
    default:
      return path.join(process.env.XDG_CONFIG_HOME || path.join(os.homedir(), ".config"), productName);
  }
}

/**
 * Where the portable .exe lives, or null if not running as a portable bundle.
 * electron-builder sets PORTABLE_EXECUTABLE_DIR when launching the portable .exe.
 */
function getPortableRoot(): string | null {
  const env = process.env.PORTABLE_EXECUTABLE_DIR;
  return env && env.trim() ? env.trim() : null;
}

/** Default app-data dir before any user override. */
export function getDefaultAppDataDir(): string {
  const portable = getPortableRoot();
  if (portable) return path.join(portable, "godsend-data");
  return getPlatformDefaultUserData();
}

/**
 * Path to the marker file in a *stable* location so we can find the override
 * before knowing it. For portable we keep the marker next to the .exe (the
 * data dir itself may have been moved); for everything else we keep it in the
 * default data dir.
 */
function getMarkerPath(): string {
  const portable = getPortableRoot();
  if (portable) return path.join(portable, MARKER_FILENAME);
  return path.join(getDefaultAppDataDir(), MARKER_FILENAME);
}

function readMarker(): OverrideMarker | null {
  try {
    const raw = fs.readFileSync(getMarkerPath(), "utf8");
    const parsed = JSON.parse(raw);
    if (typeof parsed?.appDataDir === "string" && parsed.appDataDir.trim()) {
      return { appDataDir: parsed.appDataDir.trim() };
    }
  } catch { /* ignore */ }
  return null;
}

function writeMarker(appDataDir: string): void {
  const markerPath = getMarkerPath();
  fs.mkdirSync(path.dirname(markerPath), { recursive: true });
  const marker: OverrideMarker = { appDataDir };
  fs.writeFileSync(markerPath, JSON.stringify(marker, null, 2), "utf8");
}

function clearMarker(): void {
  try { fs.unlinkSync(getMarkerPath()); } catch { /* ignore */ }
}

/**
 * The effective app data directory: override marker → default. Resolved once
 * at boot in main.ts; do not re-call after app.setPath("userData", …) unless
 * you specifically need to bypass the override (you don't).
 */
export function getAppDataDir(): string {
  const marker = readMarker();
  if (marker) {
    const candidate = path.resolve(marker.appDataDir);
    try {
      fs.mkdirSync(candidate, { recursive: true });
      return candidate;
    } catch {
      // Override path is invalid (drive unmounted, permission denied, …).
      // Fall back to the default so the app still launches.
    }
  }
  const def = getDefaultAppDataDir();
  fs.mkdirSync(def, { recursive: true });
  return def;
}

/** True when the user is running the portable Windows build. */
export function isPortable(): boolean {
  return getPortableRoot() !== null;
}

/** Recursively copy contents of src into dst. Skips files already at dst. */
function copyDirRecursive(src: string, dst: string): void {
  if (!fs.existsSync(src)) return;
  fs.mkdirSync(dst, { recursive: true });
  for (const entry of fs.readdirSync(src, { withFileTypes: true })) {
    const s = path.join(src, entry.name);
    const d = path.join(dst, entry.name);
    if (entry.isDirectory()) {
      copyDirRecursive(s, d);
    } else if (entry.isFile()) {
      if (!fs.existsSync(d)) {
        fs.copyFileSync(s, d);
      }
    }
  }
}

/**
 * Move all current app data from `from` into `to`. Best-effort: copies
 * everything (excluding the marker file itself), then attempts to remove the
 * source. Returns true if migration succeeded enough to switch over.
 */
export function migrateAppData(from: string, to: string): { ok: boolean; error?: string } {
  try {
    if (path.resolve(from) === path.resolve(to)) return { ok: true };
    fs.mkdirSync(to, { recursive: true });
    for (const entry of fs.readdirSync(from, { withFileTypes: true })) {
      if (entry.name === MARKER_FILENAME) continue;
      const s = path.join(from, entry.name);
      const d = path.join(to, entry.name);
      if (entry.isDirectory()) {
        copyDirRecursive(s, d);
        try { fs.rmSync(s, { recursive: true, force: true }); } catch { /* ignore */ }
      } else if (entry.isFile()) {
        if (!fs.existsSync(d)) fs.copyFileSync(s, d);
        try { fs.unlinkSync(s); } catch { /* ignore */ }
      }
    }
    return { ok: true };
  } catch (err: any) {
    return { ok: false, error: err.message || String(err) };
  }
}

/**
 * Persist a new app-data directory choice. Pass an empty string to clear the
 * override and fall back to the platform default. Caller is responsible for
 * migrating data first (via migrateAppData) and restarting the app afterwards.
 */
export function setAppDataDirOverride(newPath: string): void {
  const trimmed = (newPath || "").trim();
  if (!trimmed || path.resolve(trimmed) === path.resolve(getDefaultAppDataDir())) {
    clearMarker();
    return;
  }
  writeMarker(path.resolve(trimmed));
}
