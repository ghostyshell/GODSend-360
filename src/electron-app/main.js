const { app, BrowserWindow, Tray, Menu, ipcMain, nativeImage, dialog } = require("electron");
const path = require("path");
const fs = require("fs");
const { spawn } = require("child_process");

let mainWindow = null;
let tray = null;
let isQuitting = false;
let godsendProcess = null;
let outputBuffer = [];
const maxBufferLines = 3000;

function configFilePath() {
  return path.join(app.getPath("userData"), "config.json");
}

function readConfig() {
  try {
    const raw = fs.readFileSync(configFilePath(), "utf8");
    return JSON.parse(raw);
  } catch {
    return {};
  }
}

function writeConfig(partial) {
  const next = { ...readConfig(), ...partial };
  ensureDirectory(path.dirname(configFilePath()));
  fs.writeFileSync(configFilePath(), JSON.stringify(next, null, 2), "utf8");
  return next;
}

function getConfiguredTransferFolder() {
  const v = readConfig().transferFolder;
  return typeof v === "string" ? v.trim() : "";
}

function getDefaultTransferFolder(writableRoot) {
  return path.join(writableRoot, "Transfer");
}

function getConfiguredIACookie() {
  const v = readConfig().iaCookie;
  return typeof v === "string" ? v.trim() : "";
}

function getConfiguredIAAuthorization() {
  const v = readConfig().iaAuthorization;
  return typeof v === "string" ? v.trim() : "";
}

function getConfiguredIAConcurrency() {
  const v = readConfig().iaConcurrency;
  const n = parseInt(v, 10);
  if (isNaN(n) || n < 1) return 5;
  if (n > 7) return 7;
  return n;
}

function buildGodsendEnv(writableRoot) {
  const env = {
    ...process.env,
    GODSEND_HOME: writableRoot
  };
  const custom = getConfiguredTransferFolder();
  if (custom) {
    env.GODSEND_TRANSFER = path.resolve(custom);
  }
  const iaCookie = getConfiguredIACookie();
  if (iaCookie) {
    env.GODSEND_IA_COOKIE = iaCookie;
  }
  const iaAuth = getConfiguredIAAuthorization();
  if (iaAuth) {
    env.GODSEND_IA_AUTHORIZATION = iaAuth;
  }
  env.GODSEND_IA_CONCURRENCY = String(getConfiguredIAConcurrency());
  return env;
}

function restartGodsendIfRunning() {
  if (!godsendProcess) {
    return;
  }
  stopGodsend();
  setTimeout(() => startGodsend(), 400);
}

const IA_XAUTHN_URL = "https://archive.org/services/xauthn/?op=login";
const IA_LOGIN_UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36";

/**
 * Same flow as the official `internetarchive` Python library (xauthn login).
 * Returns a Cookie header value for logged-in-user + logged-in-sig.
 */
async function loginInternetArchive(email, password) {
  const trimmed = typeof email === "string" ? email.trim() : "";
  if (!trimmed || typeof password !== "string" || !password) {
    throw new Error("Email and password are required.");
  }

  const body = new URLSearchParams({ email: trimmed, password });
  const res = await fetch(IA_XAUTHN_URL, {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      "User-Agent": IA_LOGIN_UA,
      Accept: "application/json"
    },
    body
  });

  let j;
  try {
    j = await res.json();
  } catch {
    throw new Error(`Internet Archive login failed (HTTP ${res.status}, not JSON).`);
  }

  if (!j || j.success !== true) {
    let msg = "Login failed.";
    try {
      if (j.values && j.values.reason) {
        msg = j.values.reason;
      } else if (j.error) {
        msg = j.error;
      }
    } catch {
      /* keep default */
    }
    if (msg === "account_not_found") {
      msg = "Account not found. Check your email.";
    } else if (msg === "account_bad_password") {
      msg = "Incorrect password.";
    }
    throw new Error(msg);
  }

  const cookies = j.values && j.values.cookies;
  const u = cookies && cookies["logged-in-user"];
  const sig = cookies && cookies["logged-in-sig"];
  if (!u || !sig) {
    throw new Error("Login succeeded but session cookies were missing. Try again or use archive.org in a browser.");
  }

  // IA's xauthn API sometimes returns full Set-Cookie strings as values
  // (e.g. "value; expires=...; domain=..."). Strip attributes, keep only the value.
  const cookieAttrs = new Set(["expires","max-age","path","domain","secure","httponly","samesite"]);
  const extractCookieValue = (raw) => {
    if (!raw) return raw;
    const firstSemi = raw.indexOf(";");
    if (firstSemi === -1) return raw;
    // Check if this looks like a Set-Cookie string by detecting known attribute names
    const rest = raw.slice(firstSemi + 1).trim().toLowerCase();
    if (cookieAttrs.has(rest.split(/[=;]/)[0].trim())) {
      return raw.slice(0, firstSemi).trim();
    }
    return raw;
  };
  const cookieHeader = `logged-in-user=${extractCookieValue(u)}; logged-in-sig=${extractCookieValue(sig)}`;
  const screenname = (j.values && j.values.screenname) || "";
  return { cookieHeader, screenname, email: trimmed };
}

function getConfiguredIAEmail() {
  const v = readConfig().iaEmail;
  return typeof v === "string" ? v.trim() : "";
}

function getConfiguredIAScreenname() {
  const v = readConfig().iaScreenname;
  return typeof v === "string" ? v.trim() : "";
}

/** Install root: next to the .exe on Windows (extraFiles land here, not under resources). */
function getBundledRoot() {
  return app.isPackaged ? path.dirname(process.execPath) : path.resolve(__dirname, "..");
}

/** Go binary: packaged as godsend-backend.exe so it never overwrites GODsend.exe on case-insensitive Windows. */
function getGodsendExePath() {
  const root = getBundledRoot();
  if (app.isPackaged) {
    return path.join(root, "godsend-backend.exe");
  }
  return path.join(root, "godsend.exe");
}

function getWritableRuntimeRoot() {
  return app.isPackaged ? path.join(app.getPath("userData"), "runtime") : getBundledRoot();
}

function ensureDirectory(dirPath) {
  fs.mkdirSync(dirPath, { recursive: true });
}

function copyFileIfMissing(sourcePath, targetPath) {
  if (!fs.existsSync(sourcePath) || fs.existsSync(targetPath)) {
    return;
  }
  ensureDirectory(path.dirname(targetPath));
  fs.copyFileSync(sourcePath, targetPath);
}

function copyDirectoryContentsIfMissing(sourceDir, targetDir) {
  if (!fs.existsSync(sourceDir)) {
    return;
  }
  ensureDirectory(targetDir);

  const entries = fs.readdirSync(sourceDir, { withFileTypes: true });
  for (const entry of entries) {
    const sourcePath = path.join(sourceDir, entry.name);
    const targetPath = path.join(targetDir, entry.name);

    if (entry.isDirectory()) {
      copyDirectoryContentsIfMissing(sourcePath, targetPath);
      continue;
    }

    copyFileIfMissing(sourcePath, targetPath);
  }
}

function prepareWritableRuntime() {
  const bundledRoot = getBundledRoot();
  const writableRoot = getWritableRuntimeRoot();

  ensureDirectory(writableRoot);
  ensureDirectory(path.join(writableRoot, "cache"));
  ensureDirectory(path.join(writableRoot, "Temp"));
  ensureDirectory(path.join(writableRoot, "Transfer"));
  ensureDirectory(path.join(writableRoot, "Ready"));

  // Seed cache once from bundled resources if present.
  copyDirectoryContentsIfMissing(path.join(bundledRoot, "cache"), path.join(writableRoot, "cache"));

  // Helpers only — backend runs from install dir (separate name from GODsend.exe so Windows does not overwrite the Electron binary; paths are case-insensitive).
  const runtimeFiles = ["iso2god.exe", "7za.exe", "7za.dll", "7zxa.dll"];
  for (const fileName of runtimeFiles) {
    copyFileIfMissing(path.join(bundledRoot, fileName), path.join(writableRoot, fileName));
  }

  return writableRoot;
}

/** Window + tray: canonical tray logo; icon.ico is a duplicate from sync. */
function getIconCandidates() {
  const bundledRoot = getBundledRoot();
  return [
    path.join(bundledRoot, "assets", "tray.ico"),
    path.join(bundledRoot, "assets", "tray.png"),
    path.join(__dirname, "assets", "tray.ico"),
    path.join(__dirname, "assets", "tray.png"),
    path.join(bundledRoot, "assets", "icon.ico"),
    path.join(bundledRoot, "assets", "icon.png"),
    path.join(__dirname, "assets", "icon.ico"),
    path.join(__dirname, "assets", "icon.png")
  ];
}

function getFirstValidIconPath() {
  for (const iconPath of getIconCandidates()) {
    if (!fs.existsSync(iconPath)) {
      continue;
    }
    const icon = nativeImage.createFromPath(iconPath);
    if (!icon.isEmpty()) {
      return iconPath;
    }
  }
  return null;
}

function addOutputLine(line) {
  outputBuffer.push(line);
  if (outputBuffer.length > maxBufferLines) {
    outputBuffer = outputBuffer.slice(outputBuffer.length - maxBufferLines);
  }
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.webContents.send("godsend-output", line);
  }
}

function createMainWindow() {
  const windowIconPath = getFirstValidIconPath();
  mainWindow = new BrowserWindow({
    width: 900,
    height: 600,
    show: true,
    autoHideMenuBar: true,
    icon: windowIconPath || undefined,
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false
    }
  });

  mainWindow.loadFile(path.join(__dirname, "index.html"));

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
}

function createTray() {
  let trayIcon = nativeImage.createEmpty();
  for (const iconPath of getIconCandidates()) {
    if (!fs.existsSync(iconPath)) {
      continue;
    }
    const candidate = nativeImage.createFromPath(iconPath);
    if (!candidate.isEmpty()) {
      trayIcon = candidate.resize({ width: 16, height: 16 });
      break;
    }
  }

  tray = new Tray(trayIcon);
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
      }
    },
    {
      label: "Quit",
      click: () => {
        isQuitting = true;
        if (godsendProcess) {
          godsendProcess.kill();
        }
        app.quit();
      }
    }
  ]);
  tray.setContextMenu(menu);
}

function startGodsend() {
  if (godsendProcess) {
    return;
  }

  const writableRoot = prepareWritableRuntime();
  const godsendExePath = getGodsendExePath();
  const transferNote = getConfiguredTransferFolder() || getDefaultTransferFolder(writableRoot);

  addOutputLine(`[INFO] Starting: ${godsendExePath}`);
  addOutputLine(`[INFO] Data dir (GODSEND_HOME): ${writableRoot}`);
  addOutputLine(`[INFO] Local Transfer folder: ${transferNote}`);

  godsendProcess = spawn(godsendExePath, [], {
    cwd: writableRoot,
    windowsHide: true,
    env: buildGodsendEnv(writableRoot)
  });

  godsendProcess.stdout.on("data", (data) => {
    const text = data.toString();
    text.split(/\r?\n/).forEach((line) => {
      if (line.trim().length > 0) {
        addOutputLine(line);
      }
    });
  });

  godsendProcess.stderr.on("data", (data) => {
    const text = data.toString();
    text.split(/\r?\n/).forEach((line) => {
      if (line.trim().length > 0) {
        addOutputLine(`[ERR] ${line}`);
      }
    });
  });

  godsendProcess.on("error", (error) => {
    addOutputLine(`[ERROR] Failed to start process: ${error.message}`);
    godsendProcess = null;
  });

  godsendProcess.on("close", (code, signal) => {
    addOutputLine(`[INFO] Process closed (code=${code}, signal=${signal || "none"})`);
    godsendProcess = null;
  });
}

function stopGodsend() {
  if (!godsendProcess) {
    return;
  }
  addOutputLine("[INFO] Stopping process...");
  godsendProcess.kill();
}

app.whenReady().then(() => {
  app.setAppUserModelId("com.abbu.godsend");

  createMainWindow();
  createTray();
  // Start backend after the UI loads so the Electron window is visible first (and avoid racing the shell).
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

ipcMain.handle("startup:get", () => {
  const settings = app.getLoginItemSettings();
  return !!settings.openAtLogin;
});

ipcMain.handle("startup:set", (_event, enabled) => {
  const shouldEnable = Boolean(enabled);
  app.setLoginItemSettings({
    openAtLogin: shouldEnable,
    openAsHidden: true
  });
  return shouldEnable;
});

ipcMain.handle("godsend:get-buffer", () => outputBuffer);
ipcMain.handle("godsend:start", () => {
  startGodsend();
  return true;
});
ipcMain.handle("godsend:stop", () => {
  stopGodsend();
  return true;
});
ipcMain.handle("godsend:restart", () => {
  if (godsendProcess) {
    restartGodsendIfRunning();
  } else {
    startGodsend();
  }
  return true;
});

ipcMain.handle("config:get-transfer-folder", () => getConfiguredTransferFolder());

ipcMain.handle("config:get-effective-transfer-folder", () => {
  const writableRoot = getWritableRuntimeRoot();
  const custom = getConfiguredTransferFolder();
  return custom ? path.resolve(custom) : getDefaultTransferFolder(writableRoot);
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
  hasSession: Boolean(getConfiguredIACookie())
}));

ipcMain.handle("config:ia-login", async (_event, payload) => {
  const p = payload || {};
  try {
    const { cookieHeader, screenname, email } = await loginInternetArchive(p.email, p.password);
    writeConfig({
      iaCookie: cookieHeader,
      iaEmail: email,
      iaScreenname: screenname,
      iaAuthorization: ""
    });
    restartGodsendIfRunning();
    return { ok: true, screenname, email };
  } catch (err) {
    return { ok: false, error: err && err.message ? err.message : String(err) };
  }
});

ipcMain.handle("config:ia-logout", () => {
  writeConfig({
    iaCookie: "",
    iaAuthorization: "",
    iaScreenname: ""
  });
  restartGodsendIfRunning();
  return true;
});

ipcMain.handle("config:get-ia-concurrency", () => getConfiguredIAConcurrency());

ipcMain.handle("config:set-ia-concurrency", (_event, value) => {
  const n = Math.max(1, Math.min(7, parseInt(value, 10) || 4));
  writeConfig({ iaConcurrency: n });
  restartGodsendIfRunning();
  return n;
});

ipcMain.handle("config:choose-transfer-folder", async () => {
  const win = BrowserWindow.getFocusedWindow() || mainWindow;
  const r = await dialog.showOpenDialog(win || undefined, {
    properties: ["openDirectory", "createDirectory"]
  });
  if (r.canceled || !r.filePaths[0]) {
    return null;
  }
  return r.filePaths[0];
});
