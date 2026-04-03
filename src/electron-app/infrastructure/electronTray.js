const { Tray, Menu, nativeImage } = require("electron");
const fs = require("fs");
const { getIconCandidates } = require("./fileSystem");

/**
 * Creates and returns the system-tray icon.
 * @param {Electron.BrowserWindow} mainWindow
 * @param {{ onQuit: () => void }} options
 */
function createTray(mainWindow, { onQuit }) {
  let trayIcon = nativeImage.createEmpty();
  for (const iconPath of getIconCandidates()) {
    if (!fs.existsSync(iconPath)) continue;
    const candidate = nativeImage.createFromPath(iconPath);
    if (!candidate.isEmpty()) {
      trayIcon = candidate.resize({ width: 16, height: 16 });
      break;
    }
  }

  const tray = new Tray(trayIcon);
  tray.setToolTip("GODsend");

  tray.on("double-click", () => {
    if (mainWindow) {
      mainWindow.show();
      mainWindow.focus();
    }
  });

  const menu = Menu.buildFromTemplate([
    {
      label: "Open",
      click: () => {
        if (mainWindow) {
          mainWindow.show();
          mainWindow.focus();
        }
      },
    },
    { label: "Quit", click: onQuit },
  ]);
  tray.setContextMenu(menu);

  return tray;
}

module.exports = { createTray };
