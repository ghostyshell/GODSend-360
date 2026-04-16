"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.getWebContentsForPush = getWebContentsForPush;
exports.getMainWindow = getMainWindow;
exports.getIsQuitting = getIsQuitting;
exports.setIsQuitting = setIsQuitting;
exports.createMainWindow = createMainWindow;
const electron_1 = require("electron");
const path_1 = __importDefault(require("path"));
const fileSystem_1 = require("../infrastructure/fileSystem");
const backendClient_1 = require("../services/backendClient");
let mainWindow = null;
let isQuitting = false;
/**
 * Get the webContents to use for pushing events to the renderer.
 * Prefers the main window; falls back to any non-destroyed BrowserWindow.
 */
function getWebContentsForPush() {
    if (mainWindow && !mainWindow.isDestroyed())
        return mainWindow.webContents;
    for (const w of electron_1.BrowserWindow.getAllWindows()) {
        if (!w.isDestroyed() && w.webContents)
            return w.webContents;
    }
    return null;
}
/** Return the main BrowserWindow reference (may be null during startup/teardown). */
function getMainWindow() {
    return mainWindow;
}
/** Return whether the app is in the process of quitting. */
function getIsQuitting() {
    return isQuitting;
}
/** Mark the app as quitting so close events allow the window to close. */
function setIsQuitting(value) {
    isQuitting = Boolean(value);
}
/**
 * Create the main BrowserWindow and hook up minimize-to-tray / close-to-tray
 * behaviour.
 */
function createMainWindow() {
    const windowIconPath = (0, fileSystem_1.getFirstValidIconPath)();
    mainWindow = new electron_1.BrowserWindow({
        width: 900,
        height: 600,
        show: true,
        autoHideMenuBar: true,
        icon: windowIconPath || undefined,
        webPreferences: {
            preload: path_1.default.join(__dirname, "..", "preload.js"),
            contextIsolation: true,
            nodeIntegration: false,
        },
    });
    if (process.env.VITE_DEV_SERVER_URL) {
        mainWindow.loadURL(process.env.VITE_DEV_SERVER_URL);
    }
    else {
        mainWindow.loadFile(path_1.default.join(__dirname, "..", "renderer-dist", "index.html"));
    }
    mainWindow.on("minimize", () => {
        mainWindow.hide();
    });
    mainWindow.on("close", (event) => {
        if (!isQuitting) {
            event.preventDefault();
            mainWindow.hide();
        }
    });
    (0, backendClient_1.setMainWindowRef)(mainWindow);
}
