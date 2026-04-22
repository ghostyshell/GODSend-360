/**
 * IPC handlers for application configuration, startup settings, logs,
 * Internet Archive auth, cache refresh, aria2 ports, and default Xbox drive.
 */

import { app, BrowserWindow, dialog, IpcMain } from "electron";
import http from "http";
import path from "path";

import {
  getConfiguredStoragePath,
  getConfiguredTransferFolder,
  getDefaultTransferFolder,
  getConfiguredROMPath,
  getDefaultROMPath,
  getConfiguredIAEmail,
  getConfiguredIAScreenname,
  getConfiguredIACookie,
  getConfiguredServerPort,
  getConfiguredXboxIP,
  getConfiguredFtpUser,
  getConfiguredFtpPassword,
  getConfiguredFtpScriptsPath,
  getConfiguredDefaultXboxDrive,
  getConfiguredAria2ListenPort,
  getConfiguredAria2DhtPort,
  getDefaultFtpScriptsPath,
  writeConfig,
} from "../services/settingsService";
import {
  getProcess,
  restartGodsendIfRunning,
  loginInternetArchive,
} from "../services/backendClient";
import { getLogInfo, openLogsFolder, appendAppEvent } from "../infrastructure/serverLog";
import { getWritableRuntimeRoot, getDefaultWritableRuntimeRoot } from "../infrastructure/fileSystem";
import { getMainWindow } from "../app/window";

export function register(ipcMain: IpcMain): void {

  // ── Startup / logs ──────────────────────────────────────────────────────────
  ipcMain.handle("startup:get", () => {
    const settings = app.getLoginItemSettings();
    return !!settings.openAtLogin;
  });

  ipcMain.handle("startup:set", (_event, enabled) => {
    const shouldEnable = Boolean(enabled);
    app.setLoginItemSettings({ openAtLogin: shouldEnable, openAsHidden: true });
    return shouldEnable;
  });

  ipcMain.handle("logs:get-info",    () => getLogInfo());
  ipcMain.handle("logs:open-folder", () => openLogsFolder());

  // ── Storage path (GODSEND_HOME) ─────────────────────────────────────────────
  ipcMain.handle("config:get-storage-path", () => getConfiguredStoragePath());

  ipcMain.handle("config:get-effective-storage-path", () => getWritableRuntimeRoot());

  ipcMain.handle("config:get-default-storage-path", () => getDefaultWritableRuntimeRoot());

  ipcMain.handle("config:set-storage-path", (_event, folder) => {
    const f = typeof folder === "string" ? folder.trim() : "";
    writeConfig({ storagePath: f });
    appendAppEvent(
      "CONFIG",
      `storagePath set to ${f ? path.resolve(f) : "(default)"}; restarting backend`
    );
    restartGodsendIfRunning();
    return getConfiguredStoragePath();
  });

  ipcMain.handle("config:choose-storage-path", async () => {
    const win = BrowserWindow.getFocusedWindow() || getMainWindow();
    const r   = await dialog.showOpenDialog(win || undefined, {
      properties: ["openDirectory", "createDirectory"],
    });
    if (r.canceled || !r.filePaths[0]) return null;
    return r.filePaths[0];
  });

  // ── Transfer folder ─────────────────────────────────────────────────────────
  ipcMain.handle("config:get-transfer-folder", () => getConfiguredTransferFolder());

  ipcMain.handle("config:get-effective-transfer-folder", () => {
    const writableRoot = getWritableRuntimeRoot();
    const custom       = getConfiguredTransferFolder();
    return custom
      ? path.resolve(custom)
      : getDefaultTransferFolder(writableRoot);
  });

  ipcMain.handle("config:set-transfer-folder", (_event, folder) => {
    const f = typeof folder === "string" ? folder.trim() : "";
    writeConfig({ transferFolder: f });
    appendAppEvent(
      "CONFIG",
      `transferFolder set to ${f ? path.resolve(f) : "(default runtime/Transfer)"}; restarting backend`
    );
    restartGodsendIfRunning();
    return getConfiguredTransferFolder();
  });

  ipcMain.handle("config:choose-transfer-folder", async () => {
    const win = BrowserWindow.getFocusedWindow() || getMainWindow();
    const r   = await dialog.showOpenDialog(win || undefined, {
      properties: ["openDirectory", "createDirectory"],
    });
    if (r.canceled || !r.filePaths[0]) return null;
    return r.filePaths[0];
  });

  // ── Server port ─────────────────────────────────────────────────────────────
  ipcMain.handle("config:get-server-port", () => getConfiguredServerPort());

  ipcMain.handle("config:set-server-port", (_event, value) => {
    const n    = parseInt(value, 10);
    const port = Number.isInteger(n) && n >= 1 && n <= 65535 ? n : 8080;
    writeConfig({ serverPort: port });
    appendAppEvent("CONFIG", `serverPort=${port}`);
    restartGodsendIfRunning();
    return port;
  });

  // ── Internet Archive auth ───────────────────────────────────────────────────
  ipcMain.handle("config:get-archive-auth", () => ({
    iaEmail:      getConfiguredIAEmail(),
    iaScreenname: getConfiguredIAScreenname(),
    hasSession:   Boolean(getConfiguredIACookie()),
  }));

  ipcMain.handle("config:ia-login", async (_event, payload) => {
    const p = payload || {};
    try {
      const { cookieHeader, screenname, email } = await loginInternetArchive(
        p.email,
        p.password
      );
      writeConfig({
        iaCookie:        cookieHeader,
        iaEmail:         email,
        iaScreenname:    screenname,
        iaAuthorization: "",
      });
      restartGodsendIfRunning();
      appendAppEvent("IA_LOGIN", `ok email=${email}`);
      return { ok: true, screenname, email };
    } catch (err: any) {
      const msg = err && err.message ? err.message : String(err);
      appendAppEvent("IA_LOGIN", `failed: ${msg}`);
      return { ok: false, error: msg };
    }
  });

  ipcMain.handle("config:ia-logout", () => {
    writeConfig({ iaCookie: "", iaAuthorization: "", iaScreenname: "" });
    appendAppEvent("IA_LOGIN", "logout; session cleared");
    restartGodsendIfRunning();
    return true;
  });

  // ── ROM path ────────────────────────────────────────────────────────────────
  ipcMain.handle("config:get-rom-path", () => getConfiguredROMPath() || getDefaultROMPath());

  ipcMain.handle("config:set-rom-path", (_event, value) => {
    const v = typeof value === "string" ? value.trim() : "";
    writeConfig({ romPath: v });
    appendAppEvent("CONFIG", `romPath=${v || "(default)"}`);
    restartGodsendIfRunning();
    return getConfiguredROMPath();
  });

  // ── Cache refresh ───────────────────────────────────────────────────────────
  ipcMain.handle("config:cache-refresh", (_event, platform) => {
    const p = typeof platform === "string" && platform ? platform : "all";
    appendAppEvent("CACHE", `refresh requested platform=${p}`);
    return new Promise((resolve) => {
      const req = http.get(
        `http://localhost:${getConfiguredServerPort()}/cache-refresh?platform=${encodeURIComponent(p)}`,
        (res) => {
          let data = "";
          res.on("data",  (chunk) => { data += chunk; });
          res.on("end", () => {
            appendAppEvent("CACHE", `refresh http status=${res.statusCode} bodyLen=${data.length}`);
            resolve({ ok: true, data });
          });
        }
      );
      req.on("error", (err: Error) => {
        appendAppEvent("CACHE", `refresh error: ${err.message}`);
        resolve({ ok: false, error: err.message });
      });
      req.setTimeout(5000, () => {
        req.destroy();
        appendAppEvent("CACHE", "refresh error: timeout");
        resolve({ ok: false, error: "timeout" });
      });
    });
  });

  // ── Xbox connection (IP, FTP credentials, scripts path) ────────────────────
  ipcMain.handle("config:get-xbox-connection", () => ({
    xboxIp:         getConfiguredXboxIP(),
    ftpUser:        getConfiguredFtpUser(),
    ftpPassword:    getConfiguredFtpPassword(),
    ftpScriptsPath: getConfiguredFtpScriptsPath(),
  }));

  ipcMain.handle("config:set-xbox-connection", (_event, payload) => {
    const p = payload || {};
    writeConfig({
      xboxIp:         typeof p.xboxIp         === "string" ? p.xboxIp.trim()         : getConfiguredXboxIP(),
      ftpUser:        typeof p.ftpUser         === "string" ? p.ftpUser.trim()        : getConfiguredFtpUser(),
      ftpPassword:    typeof p.ftpPassword     === "string" ? p.ftpPassword           : getConfiguredFtpPassword(),
      ftpScriptsPath: typeof p.ftpScriptsPath  === "string" ? p.ftpScriptsPath.trim() : getConfiguredFtpScriptsPath(),
    });
    appendAppEvent("CONFIG", `xboxConnection saved (ftpUser=${getConfiguredFtpUser()})`);
    if (!p.skipRestart) restartGodsendIfRunning();
    return true;
  });

  // ── Default Xbox drive ──────────────────────────────────────────────────────
  ipcMain.handle("config:get-default-xbox-drive", () => getConfiguredDefaultXboxDrive());

  ipcMain.handle("config:set-default-xbox-drive", (_event, value) => {
    const v = typeof value === "string" ? value.trim() : "";
    writeConfig({ defaultXboxDrive: v });
    restartGodsendIfRunning();
    return v;
  });

  // ── Aria2 ports ─────────────────────────────────────────────────────────────
  ipcMain.handle("config:get-aria2-listen-port", () => getConfiguredAria2ListenPort());

  ipcMain.handle("config:set-aria2-listen-port", (_event, value) => {
    const n = parseInt(value, 10);
    const v = (Number.isInteger(n) && n >= 1 && n <= 65535) ? String(n) : "";
    writeConfig({ aria2ListenPort: v });
    restartGodsendIfRunning();
    return v;
  });

  ipcMain.handle("config:get-aria2-dht-port", () => getConfiguredAria2DhtPort());

  ipcMain.handle("config:set-aria2-dht-port", (_event, value) => {
    const n = parseInt(value, 10);
    const v = (Number.isInteger(n) && n >= 1 && n <= 65535) ? String(n) : "";
    writeConfig({ aria2DhtPort: v });
    restartGodsendIfRunning();
    return v;
  });

  // ── Data status / clear ─────────────────────────────────────────────────────
  ipcMain.handle("data:status", () => {
    return new Promise((resolve) => {
      const port = getConfiguredServerPort();
      const req  = http.get(`http://localhost:${port}/data/status`, (res) => {
        let data = "";
        res.on("data",  (chunk) => { data += chunk; });
        res.on("end", () => {
          try { resolve({ ok: true, ...JSON.parse(data) }); }
          catch { resolve({ ok: false, error: "parse error" }); }
        });
      });
      req.on("error", (err: Error) => resolve({ ok: false, error: err.message }));
      req.setTimeout(5000, () => { req.destroy(); resolve({ ok: false, error: "timeout" }); });
    });
  });

  ipcMain.handle("data:clear", () => {
    return new Promise((resolve) => {
      const port = getConfiguredServerPort();
      const req  = http.get(`http://localhost:${port}/data/clear`, (res) => {
        let data = "";
        res.on("data",  (chunk) => { data += chunk; });
        res.on("end", () => resolve({ ok: true }));
      });
      req.on("error", (err: Error) => resolve({ ok: false, error: err.message }));
      req.setTimeout(10000, () => { req.destroy(); resolve({ ok: false, error: "timeout" }); });
    });
  });

  // ── FTP scripts path default ────────────────────────────────────────────────
  ipcMain.handle("config:get-ftp-scripts-path-default", () => getDefaultFtpScriptsPath());
}
