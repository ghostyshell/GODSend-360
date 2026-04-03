const fs = require("fs");
const path = require("path");

/**
 * Embeds the tray logo into the Windows app .exe (same artwork as tray.ico).
 */
module.exports = async function afterPack(context) {
  if (context.electronPlatformName !== "win32") {
    return;
  }
  const projectDir = context.packager.projectDir;
  const trayIco = path.join(projectDir, "assets", "tray.ico");
  const iconIco = path.join(projectDir, "assets", "icon.ico");
  const iconPath = fs.existsSync(trayIco) ? trayIco : iconIco;
  if (!fs.existsSync(iconPath)) {
    console.warn("after-pack-win-icon: assets/tray.ico (or icon.ico) missing, skipping");
    return;
  }
  const exeName = `${context.packager.appInfo.productFilename}.exe`;
  const exePath = path.join(context.appOutDir, exeName);
  if (!fs.existsSync(exePath)) {
    console.warn(`after-pack-win-icon: ${exeName} not found, skipping`);
    return;
  }
  const { rcedit } = await import("rcedit");
  const appInfo = context.packager.appInfo;
  await rcedit(exePath, {
    icon: iconPath,
    "version-string": {
      ProductName: appInfo.productName,
      FileDescription: appInfo.productName,
      LegalCopyright: appInfo.copyright || ""
    }
  });
};
