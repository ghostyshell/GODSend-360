"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.createTray = createTray;
const electron_1 = require("electron");
const fs_1 = __importDefault(require("fs"));
const fileSystem_1 = require("./fileSystem");
function createTray(mainWindow, { onQuit }) {
    let trayIcon = electron_1.nativeImage.createEmpty();
    for (const iconPath of (0, fileSystem_1.getIconCandidates)()) {
        if (!fs_1.default.existsSync(iconPath))
            continue;
        const candidate = electron_1.nativeImage.createFromPath(iconPath);
        if (!candidate.isEmpty()) {
            trayIcon = candidate.resize({ width: 16, height: 16 });
            break;
        }
    }
    const tray = new electron_1.Tray(trayIcon);
    tray.setToolTip("GODsend");
    tray.on("double-click", () => {
        if (mainWindow) {
            mainWindow.show();
            mainWindow.focus();
        }
    });
    const menu = electron_1.Menu.buildFromTemplate([
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
