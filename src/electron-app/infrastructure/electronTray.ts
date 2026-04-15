import { Tray, Menu, nativeImage, BrowserWindow } from "electron";
import fs from "fs";
import { getIconCandidates } from "./fileSystem";

export interface TrayOptions {
  onQuit: () => void;
}

export function createTray(mainWindow: BrowserWindow, { onQuit }: TrayOptions): Tray {
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
