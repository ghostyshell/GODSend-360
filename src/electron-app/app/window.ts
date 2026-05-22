import { app, BrowserWindow } from "electron";
import path from "path";

import { getFirstValidIconPath } from "../infrastructure/fileSystem";
import { setMainWindowRef } from "../services/backendClient";

let mainWindow: BrowserWindow | null = null;
let isQuitting = false;

/**
 * Get the webContents to use for pushing events to the renderer.
 * Prefers the main window; falls back to any non-destroyed BrowserWindow.
 */
export function getWebContentsForPush(): Electron.WebContents | null {
  if (mainWindow && !mainWindow.isDestroyed()) return mainWindow.webContents;
  for (const w of BrowserWindow.getAllWindows()) {
    if (!w.isDestroyed() && w.webContents) return w.webContents;
  }
  return null;
}

/** Return the main BrowserWindow reference (may be null during startup/teardown). */
export function getMainWindow(): BrowserWindow | null {
  return mainWindow;
}

/** Return whether the app is in the process of quitting. */
export function getIsQuitting(): boolean {
  return isQuitting;
}

/** Mark the app as quitting so close events allow the window to close. */
export function setIsQuitting(value: boolean): void {
  isQuitting = Boolean(value);
}

/**
 * Create the main BrowserWindow and hook up minimize-to-tray / close-to-tray
 * behaviour.
 */
export function createMainWindow(): void {
  const windowIconPath = getFirstValidIconPath();
  mainWindow = new BrowserWindow({
    width:           900,
    height:          600,
    show:            true,
    autoHideMenuBar: true,
    icon:            windowIconPath || undefined,
    webPreferences: {
      preload:          path.join(__dirname, "..", "preload.js"),
      contextIsolation: true,
      nodeIntegration:  false,
    },
  });

  if (process.env.VITE_DEV_SERVER_URL) {
    mainWindow.loadURL(process.env.VITE_DEV_SERVER_URL);
  } else {
    mainWindow.loadFile(path.join(__dirname, "..", "renderer-dist", "index.html"), {
      query: { nocache: String(Date.now()) },
    });
  }

  // Force clear renderer cache on every startup so vite-dist changes
  // are always picked up (Electron keeps compiled JS in renderer caches).
  mainWindow.webContents.on("did-finish-load", () => {
    if (!mainWindow) return;
    mainWindow.webContents.session.clearCache().catch(() => {});
  });

  mainWindow.on("minimize", () => {
    mainWindow!.hide();
  });

  mainWindow.on("close", (event: Electron.Event) => {
    if (!isQuitting) {
      event.preventDefault();
      mainWindow!.hide();
    }
  });

  setMainWindowRef(mainWindow);
}
