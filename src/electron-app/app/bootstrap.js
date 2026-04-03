const { app, BrowserWindow, ipcMain, dialog } = require("electron");
const http = require("http");
const path = require("path");

const {
  getFirstValidIconPath,
  getWritableRuntimeRoot,
} = require("../infrastructure/fileSystem");
const { createTray } = require("../infrastructure/electronTray");
const {
  getConfiguredTransferFolder,
  getDefaultTransferFolder,
  getConfiguredROMPath,
  getDefaultROMPath,
  getConfiguredIAEmail,
  getConfiguredIAScreenname,
  getConfiguredIACookie,
  getConfiguredIAConcurrency,
  writeConfig,
} = require("../services/settingsService");
const {
  setMainWindowRef,
  getProcess,
  getOutputBuffer,
  startGodsend,
  stopGodsend,
  restartGodsendIfRunning,
  loginInternetArchive,
} = require("../services/backendClient");

let mainWindow = null;
let isQuitting = false;

function createMainWindow() {
  const windowIconPath = getFirstValidIconPath();
  mainWindow = new BrowserWindow({
    width: 900,
    height: 600,
    show: true,
    autoHideMenuBar: true,
    icon: windowIconPath || undefined,
    webPreferences: {
      preload: path.join(__dirname, "..", "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  mainWindow.loadFile(path.join(__dirname, "..", "index.html"));

  mainWindow.on("minimize", (event) => {
    event.preventDefault();
    mainWindow.hide();
  });

  mainWindow.on("close", (event) => {
    if (!isQuitting) {
      event.preventDefault();
      mainWindow.hide();
    }
  });

  setMainWindowRef(mainWindow);
}

function registerIpcHandlers() {
  ipcMain.handle("startup:get", () => {
    const settings = app.getLoginItemSettings();
    return !!settings.openAtLogin;
  });

  ipcMain.handle("startup:set", (_event, enabled) => {
    const shouldEnable = Boolean(enabled);
    app.setLoginItemSettings({ openAtLogin: shouldEnable, openAsHidden: true });
    return shouldEnable;
  });

  ipcMain.handle("godsend:get-buffer", () => getOutputBuffer());
  ipcMain.handle("godsend:start", () => { startGodsend(); return true; });
  ipcMain.handle("godsend:stop", () => { stopGodsend(); return true; });
  ipcMain.handle("godsend:restart", () => {
    if (getProcess()) restartGodsendIfRunning();
    else startGodsend();
    return true;
  });

  ipcMain.handle("config:get-transfer-folder", () =>
    getConfiguredTransferFolder()
  );

  ipcMain.handle("config:get-effective-transfer-folder", () => {
    const writableRoot = getWritableRuntimeRoot();
    const custom = getConfiguredTransferFolder();
    return custom
      ? path.resolve(custom)
      : getDefaultTransferFolder(writableRoot);
  });

  ipcMain.handle("config:set-transfer-folder", (_event, folder) => {
    const f = typeof folder === "string" ? folder.trim() : "";
    writeConfig({ transferFolder: f });
    restartGodsendIfRunning();
    return getConfiguredTransferFolder();
  });

  ipcMain.handle("config:get-archive-auth", () => ({
    iaEmail: getConfiguredIAEmail(),
    iaScreenname: getConfiguredIAScreenname(),
    hasSession: Boolean(getConfiguredIACookie()),
  }));

  ipcMain.handle("config:ia-login", async (_event, payload) => {
    const p = payload || {};
    try {
      const { cookieHeader, screenname, email } = await loginInternetArchive(
        p.email,
        p.password
      );
      writeConfig({
        iaCookie: cookieHeader,
        iaEmail: email,
        iaScreenname: screenname,
        iaAuthorization: "",
      });
      restartGodsendIfRunning();
      return { ok: true, screenname, email };
    } catch (err) {
      return {
        ok: false,
        error: err && err.message ? err.message : String(err),
      };
    }
  });

  ipcMain.handle("config:ia-logout", () => {
    writeConfig({ iaCookie: "", iaAuthorization: "", iaScreenname: "" });
    restartGodsendIfRunning();
    return true;
  });

  ipcMain.handle("config:get-ia-concurrency", () =>
    getConfiguredIAConcurrency()
  );

  ipcMain.handle("config:set-ia-concurrency", (_event, value) => {
    const n = Math.max(1, Math.min(7, parseInt(value, 10) || 4));
    writeConfig({ iaConcurrency: n });
    restartGodsendIfRunning();
    return n;
  });

  ipcMain.handle("config:get-rom-path", () =>
    getConfiguredROMPath() || getDefaultROMPath()
  );

  ipcMain.handle("config:set-rom-path", (_event, value) => {
    const v = typeof value === "string" ? value.trim() : "";
    writeConfig({ romPath: v });
    restartGodsendIfRunning();
    return getConfiguredROMPath();
  });

  ipcMain.handle("config:cache-refresh", (_event, platform) => {
    const p = typeof platform === "string" && platform ? platform : "all";
    return new Promise((resolve) => {
      const req = http.get(
        `http://localhost:8080/cache-refresh?platform=${encodeURIComponent(p)}`,
        (res) => {
          let data = "";
          res.on("data", (chunk) => { data += chunk; });
          res.on("end", () => resolve({ ok: true, data }));
        }
      );
      req.on("error", (err) => resolve({ ok: false, error: err.message }));
      req.setTimeout(5000, () => { req.destroy(); resolve({ ok: false, error: "timeout" }); });
    });
  });

  ipcMain.handle("config:choose-transfer-folder", async () => {
    const win = BrowserWindow.getFocusedWindow() || mainWindow;
    const r = await dialog.showOpenDialog(win || undefined, {
      properties: ["openDirectory", "createDirectory"],
    });
    if (r.canceled || !r.filePaths[0]) return null;
    return r.filePaths[0];
  });
}

function bootstrapApp() {
  app.whenReady().then(() => {
    app.setAppUserModelId("com.abbu.godsend");

    createMainWindow();
    createTray(mainWindow, {
      onQuit: () => {
        isQuitting = true;
        const proc = getProcess();
        if (proc) proc.kill();
        app.quit();
      },
    });

    // Start backend after UI loads so the window is visible first.
    mainWindow.webContents.once("did-finish-load", () => {
      startGodsend();
    });
  });

  app.on("before-quit", () => {
    isQuitting = true;
  });

  app.on("window-all-closed", (event) => {
    event.preventDefault();
  });

  registerIpcHandlers();
}

module.exports = { bootstrapApp };
