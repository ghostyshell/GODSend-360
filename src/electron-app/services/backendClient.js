const { spawn } = require("child_process");
const {
  getGodsendExePath,
  prepareWritableRuntime,
} = require("../infrastructure/fileSystem");
const {
  appendBackendSessionStart,
  appendBackendSessionEnd,
  appendBackendStdout,
  appendBackendStderr,
  appendAppLine,
  appendAppEvent,
  getPrimaryIPv4,
  getAppVersion,
  getLogInfo,
} = require("../infrastructure/serverLog");
const {
  getConfiguredTransferFolder,
  getDefaultTransferFolder,
  buildGodsendEnv,
} = require("./settingsService");

const IA_XAUTHN_URL = "https://archive.org/services/xauthn/?op=login";
const IA_LOGIN_UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36";

let outputBuffer = [];
const maxBufferLines = 3000;
let godsendProcess = null;
let mainWindowRef = null;

/** Must be called once the BrowserWindow is created so output can be forwarded to the renderer. */
function setMainWindowRef(win) {
  mainWindowRef = win;
}

/** Returns the live child_process handle (or null if not running). */
function getProcess() {
  return godsendProcess;
}

/**
 * @param {"ui"|"out"|"err"} stream - ui = Electron messages; out/err = raw backend streams (also file-logged).
 */
function addOutputLine(line, stream = "ui") {
  outputBuffer.push(line);
  if (outputBuffer.length > maxBufferLines) {
    outputBuffer = outputBuffer.slice(outputBuffer.length - maxBufferLines);
  }
  if (stream === "out") {
    appendBackendStdout(line);
  } else if (stream === "err") {
    appendBackendStderr(line);
  } else {
    appendAppLine(line);
  }
  if (mainWindowRef && !mainWindowRef.isDestroyed()) {
    mainWindowRef.webContents.send("godsend-output", line);
  }
}

function getOutputBuffer() {
  return outputBuffer;
}

function startGodsend() {
  if (godsendProcess) return;

  const writableRoot = prepareWritableRuntime();
  const godsendExePath = getGodsendExePath();
  const transferNote =
    getConfiguredTransferFolder() || getDefaultTransferFolder(writableRoot);

  const childEnv = buildGodsendEnv(writableRoot);
  addOutputLine(`[INFO] Starting: ${godsendExePath}`);
  addOutputLine(`[INFO] Data dir (GODSEND_HOME): ${writableRoot}`);
  addOutputLine(`[INFO] Local Transfer folder: ${transferNote}`);
  addOutputLine(`[INFO] Server logs: ${getLogInfo().logsDirectory}`);

  appendBackendSessionStart({
    appVersion: getAppVersion(),
    writableRoot,
    godsendExePath,
    transferFolder: transferNote,
    env: childEnv,
    localIPv4: getPrimaryIPv4(),
  });

  godsendProcess = spawn(godsendExePath, [], {
    cwd: writableRoot,
    windowsHide: true,
    env: childEnv,
  });

  appendAppEvent("BACKEND", `spawned pid=${godsendProcess.pid}`);

  let sessionEnded = false;
  const endBackendSession = (reason, code, signal) => {
    if (sessionEnded) return;
    sessionEnded = true;
    appendBackendSessionEnd(reason, code, signal);
  };

  godsendProcess.stdout.on("data", (data) => {
    data
      .toString()
      .split(/\r?\n/)
      .forEach((line) => {
        if (line.trim().length > 0) addOutputLine(line, "out");
      });
  });

  godsendProcess.stderr.on("data", (data) => {
    data
      .toString()
      .split(/\r?\n/)
      .forEach((line) => {
        if (line.trim().length > 0) addOutputLine(`[ERR] ${line}`, "err");
      });
  });

  godsendProcess.on("error", (error) => {
    endBackendSession("spawn_error", null, null);
    addOutputLine(`[ERROR] Failed to start process: ${error.message}`);
    godsendProcess = null;
  });

  godsendProcess.on("close", (code, signal) => {
    endBackendSession("process_exit", code, signal);
    addOutputLine(
      `[INFO] Process closed (code=${code}, signal=${signal || "none"})`
    );
    godsendProcess = null;
  });
}

function stopGodsend() {
  if (!godsendProcess) return;
  appendAppEvent("BACKEND", "stop requested (kill)");
  addOutputLine("[INFO] Stopping process...");
  godsendProcess.kill();
}

function restartGodsendIfRunning() {
  if (!godsendProcess) return;
  stopGodsend();
  setTimeout(() => startGodsend(), 400);
}

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
      Accept: "application/json",
    },
    body,
  });

  let j;
  try {
    j = await res.json();
  } catch {
    throw new Error(
      `Internet Archive login failed (HTTP ${res.status}, not JSON).`
    );
  }

  if (!j || j.success !== true) {
    let msg = "Login failed.";
    try {
      if (j.values && j.values.reason) msg = j.values.reason;
      else if (j.error) msg = j.error;
    } catch {
      /* keep default */
    }
    if (msg === "account_not_found") msg = "Account not found. Check your email.";
    else if (msg === "account_bad_password") msg = "Incorrect password.";
    throw new Error(msg);
  }

  const cookies = j.values && j.values.cookies;
  const u = cookies && cookies["logged-in-user"];
  const sig = cookies && cookies["logged-in-sig"];
  if (!u || !sig) {
    throw new Error(
      "Login succeeded but session cookies were missing. Try again or use archive.org in a browser."
    );
  }

  // IA's xauthn API sometimes returns full Set-Cookie strings as values
  // (e.g. "value; expires=...; domain=..."). Strip attributes, keep only the value.
  const cookieAttrs = new Set([
    "expires", "max-age", "path", "domain", "secure", "httponly", "samesite",
  ]);
  const extractCookieValue = (raw) => {
    if (!raw) return raw;
    const firstSemi = raw.indexOf(";");
    if (firstSemi === -1) return raw;
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

module.exports = {
  setMainWindowRef,
  getProcess,
  getOutputBuffer,
  addOutputLine,
  startGodsend,
  stopGodsend,
  restartGodsendIfRunning,
  loginInternetArchive,
};
