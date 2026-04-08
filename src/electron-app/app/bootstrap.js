const { app, BrowserWindow, ipcMain, dialog } = require("electron");
const {
  getLogInfo,
  openLogsFolder,
  appendAppEvent,
} = require("../infrastructure/serverLog");
const http = require("http");
const os = require("os");
const path = require("path");
const fs = require("fs");
const ftp = require("basic-ftp");

const {
  getFirstValidIconPath,
  getWritableRuntimeRoot,
  getAuroraScriptsPath,
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
  getConfiguredServerPort,
  getConfiguredXboxIP,
  getConfiguredFtpUser,
  getConfiguredFtpPassword,
  getDefaultFtpScriptsPath,
  getConfiguredFtpScriptsPath,
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

// Returns this machine's first non-loopback IPv4 address, or null if not found.
function getLocalIPAddress() {
  const ifaces = os.networkInterfaces();
  for (const name of Object.keys(ifaces)) {
    for (const iface of ifaces[name]) {
      if (iface.family === "IPv4" && !iface.internal) {
        return iface.address;
      }
    }
  }
  return null;
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

  ipcMain.handle("logs:get-info", () => getLogInfo());
  ipcMain.handle("logs:open-folder", () => openLogsFolder());

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
    appendAppEvent(
      "CONFIG",
      `transferFolder set to ${f ? path.resolve(f) : "(default runtime/Transfer)"}; restarting backend`
    );
    restartGodsendIfRunning();
    return getConfiguredTransferFolder();
  });

  ipcMain.handle("config:get-server-port", () => getConfiguredServerPort());

  ipcMain.handle("config:set-server-port", (_event, value) => {
    const n = parseInt(value, 10);
    const port = Number.isInteger(n) && n >= 1 && n <= 65535 ? n : 8080;
    writeConfig({ serverPort: port });
    appendAppEvent("CONFIG", `serverPort=${port}`);
    restartGodsendIfRunning();
    return port;
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
      appendAppEvent("IA_LOGIN", `ok email=${email}`);
      return { ok: true, screenname, email };
    } catch (err) {
      const msg = err && err.message ? err.message : String(err);
      appendAppEvent("IA_LOGIN", `failed: ${msg}`);
      return {
        ok: false,
        error: msg,
      };
    }
  });

  ipcMain.handle("config:ia-logout", () => {
    writeConfig({ iaCookie: "", iaAuthorization: "", iaScreenname: "" });
    appendAppEvent("IA_LOGIN", "logout; session cleared");
    restartGodsendIfRunning();
    return true;
  });

  ipcMain.handle("config:get-ia-concurrency", () =>
    getConfiguredIAConcurrency()
  );

  ipcMain.handle("config:set-ia-concurrency", (_event, value) => {
    const n = Math.max(1, Math.min(7, parseInt(value, 10) || 4));
    writeConfig({ iaConcurrency: n });
    appendAppEvent("CONFIG", `iaConcurrency=${n}`);
    restartGodsendIfRunning();
    return n;
  });

  ipcMain.handle("config:get-rom-path", () =>
    getConfiguredROMPath() || getDefaultROMPath()
  );

  ipcMain.handle("config:set-rom-path", (_event, value) => {
    const v = typeof value === "string" ? value.trim() : "";
    writeConfig({ romPath: v });
    appendAppEvent("CONFIG", `romPath=${v || "(default)"}`);
    restartGodsendIfRunning();
    return getConfiguredROMPath();
  });

  ipcMain.handle("config:cache-refresh", (_event, platform) => {
    const p = typeof platform === "string" && platform ? platform : "all";
    appendAppEvent("CACHE", `refresh requested platform=${p}`);
    return new Promise((resolve) => {
      const req = http.get(
        `http://localhost:${getConfiguredServerPort()}/cache-refresh?platform=${encodeURIComponent(p)}`,
        (res) => {
          let data = "";
          res.on("data", (chunk) => { data += chunk; });
          res.on("end", () => {
            appendAppEvent(
              "CACHE",
              `refresh http status=${res.statusCode} bodyLen=${data.length}`
            );
            resolve({ ok: true, data });
          });
        }
      );
      req.on("error", (err) => {
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

  ipcMain.handle("config:choose-transfer-folder", async () => {
    const win = BrowserWindow.getFocusedWindow() || mainWindow;
    const r = await dialog.showOpenDialog(win || undefined, {
      properties: ["openDirectory", "createDirectory"],
    });
    if (r.canceled || !r.filePaths[0]) return null;
    return r.filePaths[0];
  });

  ipcMain.handle("config:get-xbox-connection", () => ({
    xboxIp:         getConfiguredXboxIP(),
    ftpUser:        getConfiguredFtpUser(),
    ftpPassword:    getConfiguredFtpPassword(),
    ftpScriptsPath: getConfiguredFtpScriptsPath(),
  }));

  ipcMain.handle("config:set-xbox-connection", (_event, payload) => {
    const p = payload || {};
    writeConfig({
      xboxIp:         typeof p.xboxIp         === "string" ? p.xboxIp.trim()        : getConfiguredXboxIP(),
      ftpUser:        typeof p.ftpUser         === "string" ? p.ftpUser.trim()       : getConfiguredFtpUser(),
      ftpPassword:    typeof p.ftpPassword     === "string" ? p.ftpPassword          : getConfiguredFtpPassword(),
      ftpScriptsPath: typeof p.ftpScriptsPath  === "string" ? p.ftpScriptsPath.trim(): getConfiguredFtpScriptsPath(),
    });
    return true;
  });

  ipcMain.handle("config:get-ftp-scripts-path-default", () => getDefaultFtpScriptsPath());

  ipcMain.handle("xbox:ftp-scripts", async (_event, payload) => {
    const p = payload || {};
    const xboxIp    = (typeof p.xboxIp  === "string" ? p.xboxIp.trim()  : "") || getConfiguredXboxIP();
    const ftpUser   = (typeof p.ftpUser === "string" ? p.ftpUser.trim() : "") || getConfiguredFtpUser();
    const ftpPass   = (typeof p.ftpPassword === "string" ? p.ftpPassword : "") || getConfiguredFtpPassword();
    const remotePath = (typeof p.ftpScriptsPath === "string" && p.ftpScriptsPath.trim())
      ? p.ftpScriptsPath.trim()
      : getConfiguredFtpScriptsPath();

    // Send live status updates to the renderer window.
    const sendProgress = (msg) => {
      appendAppEvent("FTP", msg);
      if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send("godsend-ftp-progress", msg);
      }
    };

    let iniTempPath = null;
    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 20000;
    try {
      if (!xboxIp) return { ok: false, error: "Xbox IP address is required." };

      const scriptsDir = getAuroraScriptsPath();
      if (!fs.existsSync(scriptsDir)) {
        return { ok: false, error: `Aurora scripts folder not found at: ${scriptsDir}` };
      }

      // Auto-detect this PC's local IP and patch it into GODSend.ini.
      // Write the temp file to os.tmpdir() so it's always writable.
      const pcIp = getLocalIPAddress();
      const serverPort = getConfiguredServerPort();
      const iniSrc = path.join(scriptsDir, "GODSend.ini");
      if (!pcIp) {
        return { ok: false, error: "Could not detect this PC's local IPv4 address for script patching." };
      }
      if (fs.existsSync(iniSrc)) {
        const originalIni = fs.readFileSync(iniSrc, "utf8");
        const patchIniValue = (text, keyRegex, value) => {
          if (keyRegex.test(text)) {
            return text.replace(keyRegex, (_m, prefix) => `${prefix}${value}`);
          }
          return text;
        };
        let patched = originalIni;
        patched = patchIniValue(patched, /^(BrainAddress\s*=\s*).*$\r?$/im, pcIp);
        patched = patchIniValue(patched, /^(ip\s*=\s*).*$\r?$/im, pcIp);
        patched = patchIniValue(patched, /^(BrainPort\s*=\s*).*$\r?$/im, String(serverPort));
        patched = patchIniValue(patched, /^(port\s*=\s*).*$\r?$/im, String(serverPort));
        if (patched === originalIni) {
          patched += `\n[Config]\nip=${pcIp}\nport=${serverPort}\n`;
        }
        iniTempPath = path.join(os.tmpdir(), "GODSend.ini.upload-tmp");
        fs.writeFileSync(iniTempPath, patched, "utf8");
      }

      sendProgress("Connecting to " + xboxIp + "...");
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      sendProgress("Connected. Preparing destination folder...");
      await client.ensureDir(remotePath);

      // Upload all entries — basic-ftp STOR overwrites existing files by default.
      const entries = fs.readdirSync(scriptsDir, { withFileTypes: true });
      let done = 0;
      for (const entry of entries) {
        sendProgress(`Uploading ${entry.name} (${done}/${entries.length})...`);
        if (entry.isDirectory()) {
          await client.uploadFromDir(
            path.join(scriptsDir, entry.name),
            `${remotePath}/${entry.name}`
          );
        } else {
          const localFile = (iniTempPath && entry.name === "GODSend.ini")
            ? iniTempPath
            : path.join(scriptsDir, entry.name);
          await client.uploadFrom(localFile, `${remotePath}/${entry.name}`);
        }
        done++;
      }
      appendAppEvent("FTP", `upload complete host=${xboxIp} path=${remotePath}`);
      return { ok: true, remotePath };
    } catch (err) {
      appendAppEvent("FTP", `error: ${err.message || String(err)}`);
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
      if (iniTempPath) try { fs.unlinkSync(iniTempPath); } catch { /* ignore */ }
    }
  });
}

function bootstrapApp() {
  app.whenReady().then(() => {
    app.setAppUserModelId("com.abbu.godsend");
    appendAppEvent(
      "LIFECYCLE",
      `app ready userData=${app.getPath("userData")} logDir=${getLogInfo().logsDirectory}`
    );

    createMainWindow();
    createTray(mainWindow, {
      onQuit: () => {
        isQuitting = true;
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
    appendAppEvent("LIFECYCLE", "application before-quit");
    stopGodsend();
  });

  app.on("window-all-closed", (event) => {
    event.preventDefault();
  });

  registerIpcHandlers();
}

module.exports = { bootstrapApp };
