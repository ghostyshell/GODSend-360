const { app, nativeImage } = require("electron");
const path = require("path");
const fs = require("fs");

/**
 * Monorepo root (directory containing `cache/`, `dist/`, `tools/`).
 * In dev this must match electron-builder `extraFiles` paths (`../../cache` from
 * `src/electron-app`), otherwise ROM caches land in `src/cache/` and are never
 * bundled into the installer.
 */
function getRepoRoot() {
  return path.resolve(__dirname, "../../..");
}

/** Install root: next to the .exe on Windows (extraFiles land here, not under resources). */
function getBundledRoot() {
  return app.isPackaged
    ? path.dirname(process.execPath)
    : getRepoRoot();
}

/** Go binary: packaged next to the app executable; name varies by OS. */
function getGodsendExePath() {
  const isWin = process.platform === "win32";
  if (app.isPackaged) {
    const name = isWin ? "godsend-backend.exe" : "godsend-backend";
    return path.join(path.dirname(process.execPath), name);
  }
  const repo = getRepoRoot();
  const devCandidates = isWin
    ? ["dist/godsend.exe"]
    : process.platform === "darwin"
    ? ["dist/godsend-mac"]
    : ["dist/godsend-linux"];
  for (const rel of devCandidates) {
    const p = path.join(repo, rel);
    if (fs.existsSync(p)) return p;
  }
  return path.join(repo, devCandidates[0]);
}

function getWritableRuntimeRoot() {
  return app.isPackaged
    ? path.join(app.getPath("userData"), "runtime")
    : getBundledRoot();
}

function ensureDirectory(dirPath) {
  fs.mkdirSync(dirPath, { recursive: true });
}

function copyFileIfMissing(sourcePath, targetPath) {
  if (!fs.existsSync(sourcePath) || fs.existsSync(targetPath)) return;
  ensureDirectory(path.dirname(targetPath));
  fs.copyFileSync(sourcePath, targetPath);
}

function copyDirectoryContentsIfMissing(sourceDir, targetDir) {
  if (!fs.existsSync(sourceDir)) return;
  ensureDirectory(targetDir);
  const entries = fs.readdirSync(sourceDir, { withFileTypes: true });
  for (const entry of entries) {
    const src = path.join(sourceDir, entry.name);
    const dst = path.join(targetDir, entry.name);
    if (entry.isDirectory()) {
      copyDirectoryContentsIfMissing(src, dst);
    } else {
      copyFileIfMissing(src, dst);
    }
  }
}

function prepareWritableRuntime() {
  const bundledRoot = getBundledRoot();
  const writableRoot = getWritableRuntimeRoot();

  ensureDirectory(writableRoot);
  ensureDirectory(path.join(writableRoot, "cache"));
  ensureDirectory(path.join(writableRoot, "Temp"));
  ensureDirectory(path.join(writableRoot, "Transfer"));
  ensureDirectory(path.join(writableRoot, "Ready"));

  copyDirectoryContentsIfMissing(
    path.join(bundledRoot, "cache"),
    path.join(writableRoot, "cache")
  );

  // Older dev builds used GODSEND_HOME under src/; pull any ROM caches forward once.
  if (!app.isPackaged) {
    const legacyCache = path.join(getRepoRoot(), "src", "cache");
    copyDirectoryContentsIfMissing(
      legacyCache,
      path.join(writableRoot, "cache")
    );
  }

  return writableRoot;
}

/** Aurora scripts bundled with the installer (extraFiles → aurora-scripts/). */
function getAuroraScriptsPath() {
  return path.join(getBundledRoot(), "aurora-scripts");
}

/** Window + tray: canonical tray logo; icon.ico is a duplicate from sync. */
function getIconCandidates() {
  const bundledRoot = getBundledRoot();
  const assetsDev = path.join(__dirname, "..", "assets");
  return [
    path.join(bundledRoot, "assets", "tray.ico"),
    path.join(bundledRoot, "assets", "tray.png"),
    path.join(assetsDev, "tray.ico"),
    path.join(assetsDev, "tray.png"),
    path.join(bundledRoot, "assets", "icon.ico"),
    path.join(bundledRoot, "assets", "icon.png"),
    path.join(assetsDev, "icon.ico"),
    path.join(assetsDev, "icon.png"),
  ];
}

function getFirstValidIconPath() {
  for (const iconPath of getIconCandidates()) {
    if (!fs.existsSync(iconPath)) continue;
    const icon = nativeImage.createFromPath(iconPath);
    if (!icon.isEmpty()) return iconPath;
  }
  return null;
}

module.exports = {
  getRepoRoot,
  getBundledRoot,
  getGodsendExePath,
  getWritableRuntimeRoot,
  getAuroraScriptsPath,
  ensureDirectory,
  copyFileIfMissing,
  copyDirectoryContentsIfMissing,
  prepareWritableRuntime,
  getIconCandidates,
  getFirstValidIconPath,
};
