const fs = require("fs");
const path = require("path");
const pngToIco = require("png-to-ico");

const assetsDir = path.join(__dirname, "..", "assets");
const trayIco = path.join(assetsDir, "tray.ico");
const trayPng = path.join(assetsDir, "tray.png");
const iconIco = path.join(assetsDir, "icon.ico");
const legacyIconPng = path.join(assetsDir, "icon.png");

/**
 * Canonical branding: assets/tray.ico (or tray.png).
 * Duplicates to icon.ico for tools that expect that name (rcedit, shortcuts, etc.).
 */
async function main() {
  // Prefer icon.ico when present; only mirror between files if the counterpart is missing.
  if (fs.existsSync(iconIco)) {
    if (!fs.existsSync(trayIco)) fs.copyFileSync(iconIco, trayIco);
    return;
  }
  if (fs.existsSync(trayIco)) {
    if (!fs.existsSync(iconIco)) fs.copyFileSync(trayIco, iconIco);
    return;
  }
  if (fs.existsSync(trayPng)) {
    const buf = await pngToIco(fs.readFileSync(trayPng));
    fs.writeFileSync(trayIco, buf);
    fs.writeFileSync(iconIco, buf);
    return;
  }
  if (fs.existsSync(legacyIconPng)) {
    const buf = await pngToIco(fs.readFileSync(legacyIconPng));
    fs.writeFileSync(trayIco, buf);
    fs.writeFileSync(iconIco, buf);
    console.warn(
      "sync-assets-icon: using icon.png — prefer assets/tray.ico or tray.png as the canonical logo"
    );
    return;
  }
  console.warn(
    "sync-assets-icon: add assets/tray.ico (or tray.png) for app / window / tray icons"
  );
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
