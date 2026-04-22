#!/usr/bin/env node
/**
 * Full monorepo build: all Go backends + Electron installers for the current OS.
 * - Windows: NSIS only (AppImage is skipped; electron-builder needs symlink privileges on Windows).
 * - Linux: AppImage.
 * - macOS: AppImage, then arm64 + x64 DMGs.
 */
const { spawnSync } = require("child_process");
const fs = require("fs");
const path = require("path");

const root = path.resolve(__dirname, "..");
const electronDir = path.join(root, "src", "electron-app");
const dist = path.join(root, "dist");
const node = process.execPath;

function runNodeScript(relPath, cwd = root) {
  const script = path.join(__dirname, relPath);
  console.log(`\n[build-all] node ${path.relative(root, script)}`);
  const r = spawnSync(node, [script], { stdio: "inherit", cwd });
  if (r.status !== 0) {
    process.exit(r.status ?? 1);
  }
}

function cleanWindowsPackagingArtifacts() {
  const winUnpacked = path.join(dist, "win-unpacked");
  if (fs.existsSync(winUnpacked)) {
    console.log("\n[build-all] Removing stale dist/win-unpacked (avoids flaky NSIS 7z step)");
    fs.rmSync(winUnpacked, { recursive: true, force: true });
  }
  for (const name of fs.existsSync(dist) ? fs.readdirSync(dist) : []) {
    if (name.endsWith(".nsis.7z")) {
      fs.unlinkSync(path.join(dist, name));
    }
  }
}

function npmRun(script) {
  console.log(`\n[build-all] npm run ${script} (in src/electron-app)`);
  // Windows: spawnSync cannot run .cmd without shell (silent failure / exit 1). Use npm-cli.js + node.
  const npmCli = path.join(
    path.dirname(process.execPath),
    "node_modules",
    "npm",
    "bin",
    "npm-cli.js"
  );
  const r = fs.existsSync(npmCli)
    ? spawnSync(node, [npmCli, "run", script], {
        stdio: "inherit",
        cwd: electronDir,
        env: process.env,
        windowsHide: process.platform === "win32",
      })
    : spawnSync("npm", ["run", script], {
        stdio: "inherit",
        cwd: electronDir,
        env: process.env,
        shell: false,
      });
  if (r.error) {
    console.error("[build-all] npm spawn failed:", r.error.message);
    process.exit(1);
  }
  if (r.status !== 0) {
    process.exit(r.status ?? 1);
  }
}

runNodeScript("build-go-all.js");

const syncIcon = path.join(electronDir, "scripts", "sync-assets-icon.js");
console.log(`\n[build-all] node ${path.relative(root, syncIcon)}`);
{
  const r = spawnSync(node, [syncIcon], { stdio: "inherit", cwd: electronDir });
  if (r.status !== 0) {
    process.exit(r.status ?? 1);
  }
}

{
  const platform = process.platform;
  if (platform === "win32") {
    console.log(
      "\n[build-all] Windows host: building NSIS only. Run `npm run build` on Linux or macOS to produce the AppImage, or enable Windows Developer Mode if you need AppImage locally."
    );
    cleanWindowsPackagingArtifacts();
    npmRun("build:nsis");
    npmRun("build:portable");
  } else if (platform === "linux") {
    npmRun("build:linux");
  } else if (platform === "darwin") {
    npmRun("build:linux");
    cleanWindowsPackagingArtifacts();
    npmRun("build:nsis:x64");
    npmRun("build:portable");
  } else {
    console.warn(`\n[build-all] Unsupported platform "${platform}" — no Electron target defined.`);
  }
}

if (process.platform === "darwin") {
  const arm = path.join(dist, "godsend-darwin-arm64");
  const intel = path.join(dist, "godsend-darwin-amd64");
  const mac = path.join(dist, "godsend-mac");
  fs.copyFileSync(arm, mac);
  try {
    fs.chmodSync(mac, 0o755);
  } catch (_) {
    /* ignore */
  }
  npmRun("build:mac:dmg:arm64");
  fs.copyFileSync(intel, mac);
  try {
    fs.chmodSync(mac, 0o755);
  } catch (_) {
    /* ignore */
  }
  npmRun("build:mac:dmg:x64");
} else {
  console.log(
    "\n[build-all] Skipping macOS DMGs (requires a macOS host). Go darwin binaries are in dist/."
  );
}

console.log("\n[build-all] Done.");
