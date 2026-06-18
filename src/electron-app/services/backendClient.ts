import { spawn, ChildProcess } from "child_process";
import fs from "fs";
import path from "path";
import { BrowserWindow } from "electron";
import {
  getGodsendExePath,
  prepareWritableRuntime,
} from "../infrastructure/fileSystem";
import {
  appendBackendSessionStart,
  appendBackendSessionEnd,
  appendBackendStdout,
  appendBackendStderr,
  appendAppLine,
  appendAppEvent,
  getPrimaryIPv4,
  getAppVersion,
  getLogInfo,
} from "../infrastructure/serverLog";
import {
  getConfiguredTransferFolder,
  getDefaultTransferFolder,
  getEffectiveTorrentTempPath,
  getConfiguredServerPort,
  writeConfig,
  buildGodsendEnv,
} from "./settingsService";

const GODSEND_LISTEN_PORT_RE = /GODSEND_LISTEN_PORT=(\d+)/;
const GODSEND_FTP_COMPLETE_PREFIX = "GODSEND_FTP_COMPLETE:";

const IA_XAUTHN_URL = "https://archive.org/services/xauthn/?op=login";
const IA_LOGIN_UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36";

let outputBuffer: string[] = [];
const maxBufferLines = 3000;
let godsendProcess: ChildProcess | null = null;
let mainWindowRef: BrowserWindow | null = null;

export interface FTPCompletePayload {
  gameName: string;
  titleId: string;
  xboxIp: string;
}

type FTPCompleteCallback = (payload: FTPCompletePayload) => void;

let _ftpCompleteListeners: FTPCompleteCallback[] = [];

export function onFTPComplete(cb: FTPCompleteCallback): void {
  _ftpCompleteListeners.push(cb);
}

export function setMainWindowRef(win: BrowserWindow): void {
  mainWindowRef = win;
}

export function getProcess(): ChildProcess | null {
  return godsendProcess;
}

export function addOutputLine(line: string, stream: "ui" | "out" | "err" = "ui"): void {
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
  let wc: Electron.WebContents | null = null;
  if (mainWindowRef && !mainWindowRef.isDestroyed()) wc = mainWindowRef.webContents;
  else {
    for (const w of BrowserWindow.getAllWindows()) {
      if (!w.isDestroyed() && w.webContents) {
        wc = w.webContents;
        break;
      }
    }
  }
  if (wc) wc.send("godsend-output", line);
}

export function getOutputBuffer(): string[] {
  return outputBuffer;
}

export function startGodsend(): void {
  if (godsendProcess) return;

  const writableRoot    = prepareWritableRuntime();
  const godsendExePath  = getGodsendExePath();
  const transferNote    =
    getConfiguredTransferFolder() || getDefaultTransferFolder(writableRoot);
  const torrentTempNote = getEffectiveTorrentTempPath(writableRoot);

  const childEnv = buildGodsendEnv(writableRoot);
  addOutputLine(`[INFO] Starting: ${godsendExePath}`);
  addOutputLine(`[INFO] Data dir (GODSEND_HOME): ${writableRoot}`);
  addOutputLine(`[INFO] Backend Temp: ${path.join(writableRoot, "Temp")}`);
  addOutputLine(`[INFO] Torrent download temp: ${torrentTempNote}`);
  addOutputLine(`[INFO] Local Transfer folder: ${transferNote}`);
  addOutputLine(`[INFO] Server logs: ${getLogInfo().logsDirectory}`);

  appendBackendSessionStart({
    appVersion:     getAppVersion(),
    writableRoot,
    godsendExePath,
    transferFolder: transferNote,
    env:            childEnv,
    localIPv4:      getPrimaryIPv4(),
  });

  // Pre-flight: confirm the backend binary is actually there and readable
  // before we hand it to spawn(). On Windows, antivirus quarantine yields
  // `spawn UNKNOWN` (a *synchronous* throw) rather than an ENOENT-style
  // error event — which used to crash the main process with the generic
  // "A JavaScript error occurred…" dialog.
  if (!fs.existsSync(godsendExePath)) {
    const msg =
      `[ERROR] Backend binary not found at:\n  ${godsendExePath}\n` +
      `This usually means the installer didn't ship godsend-backend.exe ` +
      `next to GODsend.exe. Reinstall, or place the binary there manually.`;
    addOutputLine(msg);
    appendAppEvent("BACKEND", `start failed: missing binary at ${godsendExePath}`);
    appendBackendSessionEnd("missing_binary", null, null);
    return;
  }

  try {
    godsendProcess = spawn(godsendExePath, [], {
      cwd:         writableRoot,
      windowsHide: true,
      env:         childEnv,
    });
  } catch (err: any) {
    const code = err?.code || "UNKNOWN";
    const hint =
      process.platform === "win32" && (code === "UNKNOWN" || code === "EACCES")
        ? `\nThis usually means antivirus (Windows Defender, Avast, Norton, etc.) ` +
          `quarantined or blocked the backend executable. Whitelist:\n` +
          `  ${godsendExePath}\n` +
          `and restart GODsend. The Go binary is unsigned and sometimes ` +
          `flagged as a false positive.`
        : "";
    const msg = `[ERROR] Could not start backend (${code}): ${err?.message || err}${hint}`;
    addOutputLine(msg);
    appendAppEvent("BACKEND", `spawn threw ${code}: ${err?.message || err}`);
    appendBackendSessionEnd("spawn_throw", null, null);
    return;
  }

  appendAppEvent("BACKEND", `spawned pid=${godsendProcess.pid}`);

  let sessionEnded = false;
  const endBackendSession = (reason: string, code: number | null, signal: string | null) => {
    if (sessionEnded) return;
    sessionEnded = true;
    appendBackendSessionEnd(reason, code, signal);
  };

  godsendProcess.stdout!.on("data", (data: Buffer) => {
    data
      .toString()
      .split(/\r?\n/)
      .forEach((line) => {
        if (line.trim().length === 0) return;
        const m = line.match(GODSEND_LISTEN_PORT_RE);
        if (m) {
          const p = parseInt(m[1], 10);
          if (!isNaN(p) && p >= 1 && p <= 65535 && p !== getConfiguredServerPort()) {
            writeConfig({ serverPort: p });
            appendAppEvent("CONFIG", `serverPort=${p} (auto, requested port in use)`);
          }
        }
        if (line.startsWith(GODSEND_FTP_COMPLETE_PREFIX)) {
          const jsonStr = line.slice(GODSEND_FTP_COMPLETE_PREFIX.length);
          try {
            const evt = JSON.parse(jsonStr);
            const payload: FTPCompletePayload = {
              gameName: evt.game_name || "",
              titleId:  (evt.title_id || "").toUpperCase(),
              xboxIp:   evt.xbox_ip  || "",
            };
            for (const cb of _ftpCompleteListeners) {
              try { cb(payload); } catch { /* listener error must not crash stdout handler */ }
            }
          } catch { /* malformed JSON — ignore */ }
        }
        addOutputLine(line, "out");
      });
  });

  godsendProcess.stderr!.on("data", (data: Buffer) => {
    data
      .toString()
      .split(/\r?\n/)
      .forEach((line) => {
        if (line.trim().length > 0) addOutputLine(`[ERR] ${line}`, "err");
      });
  });

  godsendProcess.on("error", (error: Error) => {
    endBackendSession("spawn_error", null, null);
    addOutputLine(`[ERROR] Failed to start process: ${error.message}`);
    godsendProcess = null;
  });

  godsendProcess.on("close", (code: number | null, signal: NodeJS.Signals | null) => {
    endBackendSession("process_exit", code, signal);
    addOutputLine(
      `[INFO] Process closed (code=${code}, signal=${signal || "none"})`
    );
    godsendProcess = null;
  });
}

export function stopGodsend(): void {
  if (!godsendProcess) return;
  appendAppEvent("BACKEND", "stop requested (kill)");
  addOutputLine("[INFO] Stopping process...");
  godsendProcess.kill();
}

export function restartGodsendIfRunning(): void {
  if (!godsendProcess) return;
  stopGodsend();
  setTimeout(() => startGodsend(), 400);
}

export interface IALoginResult {
  cookieHeader: string;
  screenname: string;
  email: string;
}

export async function loginInternetArchive(email: string, password: string): Promise<IALoginResult> {
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

  let j: any;
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
    } catch { /* keep default */ }
    if (msg === "account_not_found") msg = "Account not found. Check your email.";
    else if (msg === "account_bad_password") msg = "Incorrect password.";
    throw new Error(msg);
  }

  const cookies = j.values && j.values.cookies;
  const u   = cookies && cookies["logged-in-user"];
  const sig = cookies && cookies["logged-in-sig"];
  if (!u || !sig) {
    throw new Error(
      "Login succeeded but session cookies were missing. Try again or use archive.org in a browser."
    );
  }

  const cookieAttrs = new Set([
    "expires", "max-age", "path", "domain", "secure", "httponly", "samesite",
  ]);
  const extractCookieValue = (raw: string): string => {
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
  const screenname   = (j.values && j.values.screenname) || "";
  return { cookieHeader, screenname, email: trimmed };
}
