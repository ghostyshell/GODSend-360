"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.onFTPComplete = onFTPComplete;
exports.setMainWindowRef = setMainWindowRef;
exports.getProcess = getProcess;
exports.addOutputLine = addOutputLine;
exports.getOutputBuffer = getOutputBuffer;
exports.startGodsend = startGodsend;
exports.stopGodsend = stopGodsend;
exports.restartGodsendIfRunning = restartGodsendIfRunning;
exports.loginInternetArchive = loginInternetArchive;
const child_process_1 = require("child_process");
const electron_1 = require("electron");
const fileSystem_1 = require("../infrastructure/fileSystem");
const serverLog_1 = require("../infrastructure/serverLog");
const settingsService_1 = require("./settingsService");
const GODSEND_LISTEN_PORT_RE = /GODSEND_LISTEN_PORT=(\d+)/;
const GODSEND_FTP_COMPLETE_PREFIX = "GODSEND_FTP_COMPLETE:";
const IA_XAUTHN_URL = "https://archive.org/services/xauthn/?op=login";
const IA_LOGIN_UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36";
let outputBuffer = [];
const maxBufferLines = 3000;
let godsendProcess = null;
let mainWindowRef = null;
let _ftpCompleteListeners = [];
function onFTPComplete(cb) {
    _ftpCompleteListeners.push(cb);
}
function setMainWindowRef(win) {
    mainWindowRef = win;
}
function getProcess() {
    return godsendProcess;
}
function addOutputLine(line, stream = "ui") {
    outputBuffer.push(line);
    if (outputBuffer.length > maxBufferLines) {
        outputBuffer = outputBuffer.slice(outputBuffer.length - maxBufferLines);
    }
    if (stream === "out") {
        (0, serverLog_1.appendBackendStdout)(line);
    }
    else if (stream === "err") {
        (0, serverLog_1.appendBackendStderr)(line);
    }
    else {
        (0, serverLog_1.appendAppLine)(line);
    }
    let wc = null;
    if (mainWindowRef && !mainWindowRef.isDestroyed())
        wc = mainWindowRef.webContents;
    else {
        for (const w of electron_1.BrowserWindow.getAllWindows()) {
            if (!w.isDestroyed() && w.webContents) {
                wc = w.webContents;
                break;
            }
        }
    }
    if (wc)
        wc.send("godsend-output", line);
}
function getOutputBuffer() {
    return outputBuffer;
}
function startGodsend() {
    if (godsendProcess)
        return;
    const writableRoot = (0, fileSystem_1.prepareWritableRuntime)();
    const godsendExePath = (0, fileSystem_1.getGodsendExePath)();
    const transferNote = (0, settingsService_1.getConfiguredTransferFolder)() || (0, settingsService_1.getDefaultTransferFolder)(writableRoot);
    const childEnv = (0, settingsService_1.buildGodsendEnv)(writableRoot);
    addOutputLine(`[INFO] Starting: ${godsendExePath}`);
    addOutputLine(`[INFO] Data dir (GODSEND_HOME): ${writableRoot}`);
    addOutputLine(`[INFO] Local Transfer folder: ${transferNote}`);
    addOutputLine(`[INFO] Server logs: ${(0, serverLog_1.getLogInfo)().logsDirectory}`);
    (0, serverLog_1.appendBackendSessionStart)({
        appVersion: (0, serverLog_1.getAppVersion)(),
        writableRoot,
        godsendExePath,
        transferFolder: transferNote,
        env: childEnv,
        localIPv4: (0, serverLog_1.getPrimaryIPv4)(),
    });
    godsendProcess = (0, child_process_1.spawn)(godsendExePath, [], {
        cwd: writableRoot,
        windowsHide: true,
        env: childEnv,
    });
    (0, serverLog_1.appendAppEvent)("BACKEND", `spawned pid=${godsendProcess.pid}`);
    let sessionEnded = false;
    const endBackendSession = (reason, code, signal) => {
        if (sessionEnded)
            return;
        sessionEnded = true;
        (0, serverLog_1.appendBackendSessionEnd)(reason, code, signal);
    };
    godsendProcess.stdout.on("data", (data) => {
        data
            .toString()
            .split(/\r?\n/)
            .forEach((line) => {
            if (line.trim().length === 0)
                return;
            const m = line.match(GODSEND_LISTEN_PORT_RE);
            if (m) {
                const p = parseInt(m[1], 10);
                if (!isNaN(p) && p >= 1 && p <= 65535 && p !== (0, settingsService_1.getConfiguredServerPort)()) {
                    (0, settingsService_1.writeConfig)({ serverPort: p });
                    (0, serverLog_1.appendAppEvent)("CONFIG", `serverPort=${p} (auto, requested port in use)`);
                }
            }
            if (line.startsWith(GODSEND_FTP_COMPLETE_PREFIX)) {
                const jsonStr = line.slice(GODSEND_FTP_COMPLETE_PREFIX.length);
                try {
                    const evt = JSON.parse(jsonStr);
                    const payload = {
                        gameName: evt.game_name || "",
                        titleId: (evt.title_id || "").toUpperCase(),
                        xboxIp: evt.xbox_ip || "",
                    };
                    for (const cb of _ftpCompleteListeners) {
                        try {
                            cb(payload);
                        }
                        catch { /* listener error must not crash stdout handler */ }
                    }
                }
                catch { /* malformed JSON — ignore */ }
            }
            addOutputLine(line, "out");
        });
    });
    godsendProcess.stderr.on("data", (data) => {
        data
            .toString()
            .split(/\r?\n/)
            .forEach((line) => {
            if (line.trim().length > 0)
                addOutputLine(`[ERR] ${line}`, "err");
        });
    });
    godsendProcess.on("error", (error) => {
        endBackendSession("spawn_error", null, null);
        addOutputLine(`[ERROR] Failed to start process: ${error.message}`);
        godsendProcess = null;
    });
    godsendProcess.on("close", (code, signal) => {
        endBackendSession("process_exit", code, signal);
        addOutputLine(`[INFO] Process closed (code=${code}, signal=${signal || "none"})`);
        godsendProcess = null;
    });
}
function stopGodsend() {
    if (!godsendProcess)
        return;
    (0, serverLog_1.appendAppEvent)("BACKEND", "stop requested (kill)");
    addOutputLine("[INFO] Stopping process...");
    godsendProcess.kill();
}
function restartGodsendIfRunning() {
    if (!godsendProcess)
        return;
    stopGodsend();
    setTimeout(() => startGodsend(), 400);
}
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
    }
    catch {
        throw new Error(`Internet Archive login failed (HTTP ${res.status}, not JSON).`);
    }
    if (!j || j.success !== true) {
        let msg = "Login failed.";
        try {
            if (j.values && j.values.reason)
                msg = j.values.reason;
            else if (j.error)
                msg = j.error;
        }
        catch { /* keep default */ }
        if (msg === "account_not_found")
            msg = "Account not found. Check your email.";
        else if (msg === "account_bad_password")
            msg = "Incorrect password.";
        throw new Error(msg);
    }
    const cookies = j.values && j.values.cookies;
    const u = cookies && cookies["logged-in-user"];
    const sig = cookies && cookies["logged-in-sig"];
    if (!u || !sig) {
        throw new Error("Login succeeded but session cookies were missing. Try again or use archive.org in a browser.");
    }
    const cookieAttrs = new Set([
        "expires", "max-age", "path", "domain", "secure", "httponly", "samesite",
    ]);
    const extractCookieValue = (raw) => {
        if (!raw)
            return raw;
        const firstSemi = raw.indexOf(";");
        if (firstSemi === -1)
            return raw;
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
