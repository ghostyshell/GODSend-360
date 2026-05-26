import { app, ipcMain, protocol, net } from "electron";
import { pathToFileURL } from "url";
import fs from "fs";

import { appendAppEvent, getLogInfo } from "../infrastructure/serverLog";
import { safeFileUnderRoot, getActiveAuroraCacheRoot } from "../infrastructure/auroraLibraryCache";
import { createTray } from "../infrastructure/electronTray";
import {
  getProcess,
  getOutputBuffer,
  startGodsend,
  stopGodsend,
  restartGodsendIfRunning,
  onFTPComplete,
  addOutputLine,
} from "../services/backendClient";
import { autoUploadAuroraAssets, doAuroraLibrarySync } from "../services/autoSyncService";
import { createMainWindow, setIsQuitting, getMainWindow } from "./window";

import * as configHandlers        from "../ipc/configHandlers";
import * as xboxFtpHandlers       from "../ipc/xboxFtpHandlers";
import * as auroraLibraryHandlers from "../ipc/auroraLibraryHandlers";
import * as auroraAssetHandlers   from "../ipc/auroraAssetHandlers";
import * as browseHandlers        from "../ipc/browseHandlers";
import * as toolsHandlers         from "../ipc/toolsHandlers";
import * as badAvatarHandlers     from "../ipc/badAvatarHandlers";
import * as contentHandlers       from "../ipc/contentHandlers";
import * as saveHandlers          from "../ipc/saveHandlers";

function registerIpcHandlers(): void {
  ipcMain.handle("godsend:get-buffer", () => getOutputBuffer());
  ipcMain.handle("godsend:start",   () => { startGodsend(); return true; });
  ipcMain.handle("godsend:stop",    () => { stopGodsend();  return true; });
  ipcMain.handle("godsend:restart", () => {
    if (getProcess()) {
      restartGodsendIfRunning();
    } else {
      startGodsend();
    }
    return true;
  });

  configHandlers.register(ipcMain);
  xboxFtpHandlers.register(ipcMain);
  auroraLibraryHandlers.register(ipcMain);
  auroraAssetHandlers.register(ipcMain);
  browseHandlers.register(ipcMain);
  toolsHandlers.register(ipcMain);
  badAvatarHandlers.register(ipcMain);
  contentHandlers.register(ipcMain);
  saveHandlers.register(ipcMain);
}

export function bootstrapApp(): void {
  app.whenReady().then(() => {
    protocol.handle("godsend-aurora", (request) => {
      const root = getActiveAuroraCacheRoot();
      if (!root) return new Response(null, { status: 404 });
      let u: URL;
      try {
        u = new URL(request.url);
      } catch {
        return new Response(null, { status: 400 });
      }
      if (u.hostname !== "cdn") return new Response(null, { status: 404 });
      const rel  = (u.pathname || "").replace(/^\/+/, "");
      const full = safeFileUnderRoot(root, rel);
      if (!full || !fs.existsSync(full)) return new Response(null, { status: 404 });
      try {
        return net.fetch(pathToFileURL(full).href);
      } catch {
        return new Response(null, { status: 500 });
      }
    });

    app.setAppUserModelId("com.abbu.godsend");
    appendAppEvent(
      "LIFECYCLE",
      `app ready userData=${app.getPath("userData")} logDir=${getLogInfo().logsDirectory}`
    );

    createMainWindow();
    createTray(getMainWindow()!, {
      onQuit: () => {
        setIsQuitting(true);
        app.quit();
      },
    });

    getMainWindow()!.webContents.once("did-finish-load", () => {
      startGodsend();
    });

    onFTPComplete(({ gameName, titleId, xboxIp }) => {
      (async () => {
        if (titleId) {
          try {
            await autoUploadAuroraAssets(titleId, xboxIp);
          } catch (err: any) {
            addOutputLine(`[WARN] Auto-assets failed for ${gameName}: ${err.message || err}`);
          }
        }
        try {
          await doAuroraLibrarySync();
        } catch (err: any) {
          addOutputLine(`[WARN] Auto-sync failed after ${gameName}: ${err.message || err}`);
        }
      })();
    });
  });

  app.on("before-quit", () => {
    setIsQuitting(true);
    appendAppEvent("LIFECYCLE", "application before-quit");
    stopGodsend();
  });

  app.on("window-all-closed", () => {
    // Intentionally do nothing — prevent default quit so tray keeps the app alive.
  });

  registerIpcHandlers();
}
