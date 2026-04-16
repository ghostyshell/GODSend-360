"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.getRepoRoot = getRepoRoot;
exports.getBundledRoot = getBundledRoot;
exports.getBundledResourcesRoot = getBundledResourcesRoot;
exports.getGodsendExePath = getGodsendExePath;
exports.getWritableRuntimeRoot = getWritableRuntimeRoot;
exports.ensureDirectory = ensureDirectory;
exports.copyFileIfMissing = copyFileIfMissing;
exports.copyDirectoryContentsIfMissing = copyDirectoryContentsIfMissing;
exports.prepareWritableRuntime = prepareWritableRuntime;
exports.getAuroraScriptsPath = getAuroraScriptsPath;
exports.getIconCandidates = getIconCandidates;
exports.getFirstValidIconPath = getFirstValidIconPath;
const electron_1 = require("electron");
const path_1 = __importDefault(require("path"));
const fs_1 = __importDefault(require("fs"));
/**
 * Monorepo root (directory containing `cache/`, `dist/`, `tools/`).
 */
function getRepoRoot() {
    return path_1.default.resolve(__dirname, "../../..");
}
/** Install root: next to the .exe on Windows (extraFiles land here, not under resources). */
function getBundledRoot() {
    return electron_1.app.isPackaged
        ? path_1.default.dirname(process.execPath)
        : getRepoRoot();
}
/**
 * Resource root for bundled data files (cache, assets, aurora-scripts).
 * On packaged macOS this is `Contents/Resources/`; on Windows/Linux it's
 * `<install>/resources/`. Code-signing on macOS forbids non-Mach-O files
 * inside `Contents/MacOS/`, so data must live under Resources.
 */
function getBundledResourcesRoot() {
    return electron_1.app.isPackaged ? process.resourcesPath : getRepoRoot();
}
/** Go binary: packaged next to the app executable; name varies by OS. */
function getGodsendExePath() {
    const isWin = process.platform === "win32";
    const isArm64 = process.arch === "arm64";
    if (electron_1.app.isPackaged) {
        const name = isWin ? "godsend-backend.exe" : "godsend-backend";
        return path_1.default.join(path_1.default.dirname(process.execPath), name);
    }
    const repo = getRepoRoot();
    const devCandidates = isWin
        ? ["dist/godsend.exe"]
        : process.platform === "darwin"
            ? ["dist/godsend-mac"]
            : isArm64
                ? ["dist/godsend-linux-arm64", "dist/godsend-linux-x64"]
                : ["dist/godsend-linux-x64", "dist/godsend-linux-arm64"];
    for (const rel of devCandidates) {
        const p = path_1.default.join(repo, rel);
        if (fs_1.default.existsSync(p))
            return p;
    }
    return path_1.default.join(repo, devCandidates[0]);
}
function getWritableRuntimeRoot() {
    return electron_1.app.isPackaged
        ? path_1.default.join(electron_1.app.getPath("userData"), "runtime")
        : getBundledRoot();
}
function ensureDirectory(dirPath) {
    fs_1.default.mkdirSync(dirPath, { recursive: true });
}
function copyFileIfMissing(sourcePath, targetPath) {
    if (!fs_1.default.existsSync(sourcePath) || fs_1.default.existsSync(targetPath))
        return;
    ensureDirectory(path_1.default.dirname(targetPath));
    fs_1.default.copyFileSync(sourcePath, targetPath);
}
function copyDirectoryContentsIfMissing(sourceDir, targetDir) {
    if (!fs_1.default.existsSync(sourceDir))
        return;
    ensureDirectory(targetDir);
    const entries = fs_1.default.readdirSync(sourceDir, { withFileTypes: true });
    for (const entry of entries) {
        const src = path_1.default.join(sourceDir, entry.name);
        const dst = path_1.default.join(targetDir, entry.name);
        if (entry.isDirectory()) {
            copyDirectoryContentsIfMissing(src, dst);
        }
        else {
            copyFileIfMissing(src, dst);
        }
    }
}
function prepareWritableRuntime() {
    const bundledRoot = getBundledResourcesRoot();
    const writableRoot = getWritableRuntimeRoot();
    ensureDirectory(writableRoot);
    ensureDirectory(path_1.default.join(writableRoot, "cache"));
    ensureDirectory(path_1.default.join(writableRoot, "Temp"));
    ensureDirectory(path_1.default.join(writableRoot, "Transfer"));
    ensureDirectory(path_1.default.join(writableRoot, "Ready"));
    copyDirectoryContentsIfMissing(path_1.default.join(bundledRoot, "cache"), path_1.default.join(writableRoot, "cache"));
    if (!electron_1.app.isPackaged) {
        const legacyCache = path_1.default.join(getRepoRoot(), "src", "cache");
        copyDirectoryContentsIfMissing(legacyCache, path_1.default.join(writableRoot, "cache"));
    }
    return writableRoot;
}
/** Aurora scripts bundled with the installer (extraFiles → aurora-scripts/). */
function getAuroraScriptsPath() {
    return path_1.default.join(getBundledResourcesRoot(), "aurora-scripts");
}
function getIconCandidates() {
    const bundledRoot = getBundledResourcesRoot();
    const assetsDev = path_1.default.join(__dirname, "..", "assets");
    return [
        path_1.default.join(bundledRoot, "assets", "icon.ico"),
        path_1.default.join(bundledRoot, "assets", "icon.png"),
        path_1.default.join(assetsDev, "icon.ico"),
        path_1.default.join(assetsDev, "icon.png"),
        path_1.default.join(bundledRoot, "assets", "tray.ico"),
        path_1.default.join(bundledRoot, "assets", "tray.png"),
        path_1.default.join(assetsDev, "tray.ico"),
        path_1.default.join(assetsDev, "tray.png"),
    ];
}
function getFirstValidIconPath() {
    for (const iconPath of getIconCandidates()) {
        if (!fs_1.default.existsSync(iconPath))
            continue;
        const icon = electron_1.nativeImage.createFromPath(iconPath);
        if (!icon.isEmpty())
            return iconPath;
    }
    return null;
}
