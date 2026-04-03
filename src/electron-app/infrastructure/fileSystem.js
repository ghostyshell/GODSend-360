const { app, nativeImage } = require("electron");
const path = require("path");
const fs = require("fs");

/** Install root: next to the .exe on Windows (extraFiles land here, not under resources). */
function getBundledRoot() {
  return app.isPackaged
    ? path.dirname(process.execPath)
    : path.resolve(__dirname, "../..");
}

/** Go binary: packaged as godsend-backend.exe so it never overwrites GODsend.exe on case-insensitive Windows. */
function getGodsendExePath() {
  const root = getBundledRoot();
  return app.isPackaged
    ? path.join(root, "godsend-backend.exe")
    : path.join(root, "godsend.exe");
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

  for (const fileName of ["iso2god.exe", "7za.exe", "7za.dll", "7zxa.dll"]) {
    copyFileIfMissing(
      path.join(bundledRoot, fileName),
      path.join(writableRoot, fileName)
    );
  }

  return writableRoot;
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
  getBundledRoot,
  getGodsendExePath,
  getWritableRuntimeRoot,
  ensureDirectory,
  copyFileIfMissing,
  copyDirectoryContentsIfMissing,
  prepareWritableRuntime,
  getIconCandidates,
  getFirstValidIconPath,
};
