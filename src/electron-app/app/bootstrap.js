const { app, BrowserWindow, ipcMain, dialog } = require("electron");
const {
  getLogInfo,
  openLogsFolder,
  appendAppEvent,
} = require("../infrastructure/serverLog");
const http = require("http");
const net = require("net");
const os = require("os");
const path = require("path");
const fs = require("fs");
const ftp = require("basic-ftp");
const { Writable } = require("stream");

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

  if (process.env.VITE_DEV_SERVER_URL) {
    mainWindow.loadURL(process.env.VITE_DEV_SERVER_URL);
  } else {
    mainWindow.loadFile(path.join(__dirname, "..", "renderer-dist", "index.html"));
  }

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
    appendAppEvent("CONFIG", `xboxConnection saved (ftpUser=${getConfiguredFtpUser()})`);
    // Restart so the backend re-reads GODSEND_FTP_USER / GODSEND_FTP_PASS from env.
    restartGodsendIfRunning();
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

    let stateTempPath = null;
    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 20000;
    try {
      if (!xboxIp) return { ok: false, error: "Xbox IP address is required." };

      const scriptsDir = getAuroraScriptsPath();
      if (!fs.existsSync(scriptsDir)) {
        return { ok: false, error: `Aurora scripts folder not found at: ${scriptsDir}` };
      }

      // Auto-detect this PC's local IP and patch it directly into state.lua.
      // Write the temp file to os.tmpdir() so it's always writable.
      const pcIp = getLocalIPAddress();
      const serverPort = getConfiguredServerPort();
      if (!pcIp) {
        return { ok: false, error: "Could not detect this PC's local IPv4 address for state.lua patching." };
      }
      const stateSrc = path.join(scriptsDir, "state.lua");
      if (fs.existsSync(stateSrc)) {
        const originalState = fs.readFileSync(stateSrc, "utf8");
        let patchedState = originalState;
        patchedState = patchedState.replace(
          /^(BRAIN_IP\s*=\s*)["'][^"']*["']\s*$/m,
          `$1"${pcIp}"`
        );
        patchedState = patchedState.replace(
          /^(PORT\s*=\s*)["'][^"']*["']\s*$/m,
          `$1"${serverPort}"`
        );
        stateTempPath = path.join(os.tmpdir(), "state.lua.upload-tmp");
        fs.writeFileSync(stateTempPath, patchedState, "utf8");
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
          const localFile = (stateTempPath && entry.name === "state.lua")
            ? stateTempPath
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
      if (stateTempPath) try { fs.unlinkSync(stateTempPath); } catch { /* ignore */ }
    }
  });

  // ── FTP Ping (lightweight connectivity check) ─────────────────────────────
  ipcMain.handle("xbox:ping", async () => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 5000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── FTP Debug: Test Connection ──
  ipcMain.handle("xbox:ftp-test", async (_event, payload) => {
    const p = payload || {};
    const xboxIp  = (typeof p.xboxIp  === "string" ? p.xboxIp.trim()  : "") || getConfiguredXboxIP();
    const ftpUser = (typeof p.ftpUser === "string" ? p.ftpUser.trim() : "") || getConfiguredFtpUser();
    const ftpPass = (typeof p.ftpPassword === "string" ? p.ftpPassword : "") || getConfiguredFtpPassword();

    const sendDebug = (line) => {
      if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send("godsend-ftp-debug", line);
      }
    };

    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = true;
    client.ftp.timeout = 15000;
    client.ftp.log = (msg) => sendDebug(msg);

    try {
      sendDebug(`[TEST] Connecting to ${xboxIp}:21 as ${ftpUser || "(anonymous)"}...`);
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      sendDebug(`[TEST] Login successful.`);

      sendDebug(`[TEST] Sending PWD...`);
      const pwd = await client.pwd();
      sendDebug(`[TEST] Working directory: ${pwd}`);

      sendDebug(`[TEST] Listing root directory...`);
      const list = await client.list("/");
      for (const item of list) {
        sendDebug(`  ${item.type === 2 ? "DIR " : "FILE"} ${item.name}  (${item.size || 0} bytes)`);
      }

      sendDebug(`[TEST] Connection test PASSED.`);
      return { ok: true };
    } catch (err) {
      sendDebug(`[TEST] FAILED: ${err.message || String(err)}`);
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── FTP Debug: Port Scanner ──
  ipcMain.handle("xbox:ftp-scan", async (_event, subnet) => {
    if (typeof subnet !== "string" || !/^\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(subnet.trim())) {
      return { ok: false, error: "Invalid subnet. Use format like 192.168.1" };
    }
    subnet = subnet.trim();

    const sendDebug = (line) => {
      if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.webContents.send("godsend-ftp-debug", line);
      }
    };

    sendDebug(`[SCAN] Scanning ${subnet}.1 - ${subnet}.254 on port 21...`);

    const found = [];
    const BATCH = 25;
    const TIMEOUT = 2000;

    for (let batchStart = 1; batchStart <= 254; batchStart += BATCH) {
      const batchEnd = Math.min(batchStart + BATCH - 1, 254);
      sendDebug(`[SCAN] Probing ${subnet}.${batchStart} - ${subnet}.${batchEnd}...`);

      const promises = [];
      for (let i = batchStart; i <= batchEnd; i++) {
        const ip = `${subnet}.${i}`;
        promises.push(new Promise((resolve) => {
          const sock = new net.Socket();
          sock.setTimeout(TIMEOUT);
          sock.once("connect", () => {
            sock.destroy();
            resolve(ip);
          });
          sock.once("timeout", () => { sock.destroy(); resolve(null); });
          sock.once("error", () => { sock.destroy(); resolve(null); });
          sock.connect(21, ip);
        }));
      }

      const results = await Promise.all(promises);
      for (const ip of results) {
        if (ip) {
          found.push(ip);
          sendDebug(`[SCAN] FOUND: ${ip}:21 is open (FTP)`);
        }
      }
    }

    if (found.length === 0) {
      sendDebug(`[SCAN] No FTP servers found on ${subnet}.0/24.`);
    } else {
      sendDebug(`[SCAN] Done. Found ${found.length} host(s) with FTP: ${found.join(", ")}`);
    }
    return { ok: true, hosts: found };
  });

  // ── Xbox Game Library ──────────────────────────────────────────────────────

  ipcMain.handle("xbox:list-games", async () => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();

    if (!xboxIp) return { ok: false, error: "No Xbox IP configured. Set it in Settings." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 20000;

    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

      const nameMap  = xboxBuildGameNameMap();
      const mediaDir = xboxAuroraMediaDir(getConfiguredFtpScriptsPath());
      // Use Map to deduplicate by TitleID across all scan locations.
      const games = new Map();

      function addGame(titleId, fallbackName, location) {
        const id = titleId.toUpperCase();
        if (!games.has(id)) {
          games.set(id, {
            titleId: id,
            name: nameMap.get(id) || fallbackName || id,
            location,
            coverFtpPath: `${mediaDir}/${id}GC.jpg`,
          });
        }
      }

      // 1. Scan /Hdd1/Content/<profile>/<TitleID> (covers all profile IDs)
      try {
        const profileDirs = await client.list("/Hdd1/Content");
        for (const profileDir of profileDirs) {
          if (profileDir.type !== 2) continue;
          try {
            const titleDirs = await client.list(`/Hdd1/Content/${profileDir.name}`);
            for (const titleDir of titleDirs) {
              if (titleDir.type !== 2) continue;
              const id = titleDir.name.toUpperCase();
              if (!/^[0-9A-F]{8}$/.test(id)) continue;
              addGame(id, null, `/Hdd1/Content/${profileDir.name}/${titleDir.name}`);
            }
          } catch { /* unreadable profile dir — skip */ }
        }
      } catch { /* /Hdd1/Content not present */ }

      // 2. Scan /Hdd1/Games/<GameName>/Content/<TitleID>
      try {
        const gameDirs = await client.list("/Hdd1/Games");
        for (const gameDir of gameDirs) {
          if (gameDir.type !== 2) continue;
          try {
            const contentEntries = await client.list(`/Hdd1/Games/${gameDir.name}/Content`);
            for (const entry of contentEntries) {
              if (entry.type !== 2) continue;
              const id = entry.name.toUpperCase();
              if (!/^[0-9A-F]{8}$/.test(id)) continue;
              addGame(id, gameDir.name, `/Hdd1/Games/${gameDir.name}`);
            }
          } catch {
            // No Content subdir — still expose the folder with folder name as title.
            addGame(gameDir.name.toUpperCase().padEnd(8, "0").slice(0, 8), gameDir.name, `/Hdd1/Games/${gameDir.name}`);
          }
        }
      } catch { /* /Hdd1/Games not present */ }

      const gameList = Array.from(games.values()).sort((a, b) =>
        a.name.localeCompare(b.name)
      );
      return { ok: true, games: gameList, connectedTo: xboxIp };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // Streams covers back one-by-one over a single FTP session so the renderer
  // can render them progressively without making N round-trip IPC calls.
  ipcMain.handle("xbox:fetch-covers", async (_event, coverRequests) => {
    if (!Array.isArray(coverRequests) || coverRequests.length === 0) return { ok: true };

    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    client.ftp.timeout = 10000;

    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

      for (const { titleId, ftpPath } of coverRequests) {
        let dataUrl = null;
        try {
          const chunks = [];
          const writable = new Writable({
            write(chunk, _enc, cb) { chunks.push(chunk); cb(); },
          });
          await client.downloadTo(writable, ftpPath);
          const buf  = Buffer.concat(chunks);
          const mime = (buf[0] === 0xFF && buf[1] === 0xD8) ? "image/jpeg"
                     : (buf[0] === 0x89 && buf[1] === 0x50) ? "image/png"
                     : "image/jpeg";
          dataUrl = `data:${mime};base64,${buf.toString("base64")}`;
        } catch { /* cover file absent — leave null */ }

        if (mainWindow && !mainWindow.isDestroyed()) {
          mainWindow.webContents.send("xbox-cover", { titleId, dataUrl });
        }
      }
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });
}

// ── Game library helpers ───────────────────────────────────────────────────────

// Derive the Aurora Media directory from the configured FTP scripts path so the
// cover path is correct for both HDD and USB Aurora installs.
//
//   /Hdd1/Aurora/User/Scripts/Utility/GODSend  →  /Hdd1/Aurora/Media
//   /Usb0/Apps/Aurora/User/Scripts/Utility/…   →  /Usb0/Apps/Aurora/Media
//
// Falls back to the canonical HDD path when the scripts path is unset or
// contains no "Aurora" segment.
function xboxAuroraMediaDir(ftpScriptsPath) {
  if (ftpScriptsPath) {
    const parts = ftpScriptsPath.replace(/\\/g, "/").split("/").filter(Boolean);
    const idx   = parts.findIndex((p) => p.toLowerCase() === "aurora");
    if (idx !== -1) {
      return "/" + parts.slice(0, idx + 1).join("/") + "/Media";
    }
  }
  return "/Hdd1/Aurora/Media";
}

function xboxBuildGameNameMap() {
  const map = new Map();
  const cacheDir = app.isPackaged
    ? path.join(process.resourcesPath, "cache")
    : path.join(__dirname, "..", "..", "..", "cache");

  for (const file of ["xbox360.json", "xbla.json", "games.json", "digital.json", "xbox.json"]) {
    try {
      const raw   = fs.readFileSync(path.join(cacheDir, file), "utf8");
      const data  = JSON.parse(raw);
      const items = Array.isArray(data) ? data : Object.values(data).flat();
      for (const item of items) {
        if (!item || typeof item !== "object") continue;
        const titleId = String(item.titleId || item.TitleId || item.title_id || "").toUpperCase().trim();
        const name    = String(item.title  || item.name   || item.Title    || item.Name || "").trim();
        if (titleId && name && /^[0-9A-F]{8}$/.test(titleId)) map.set(titleId, name);
      }
    } catch { /* cache file absent or unparseable — skip */ }
  }
  return map;
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
