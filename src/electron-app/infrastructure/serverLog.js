"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.getAppVersion = getAppVersion;
exports.getPrimaryIPv4 = getPrimaryIPv4;
exports.appendBackendSessionStart = appendBackendSessionStart;
exports.appendBackendSessionEnd = appendBackendSessionEnd;
exports.appendBackendStdout = appendBackendStdout;
exports.appendBackendStderr = appendBackendStderr;
exports.appendAppLine = appendAppLine;
exports.appendAppEvent = appendAppEvent;
exports.getLogInfo = getLogInfo;
exports.openLogsFolder = openLogsFolder;
const fs_1 = __importDefault(require("fs"));
const path_1 = __importDefault(require("path"));
const os_1 = __importDefault(require("os"));
const electron_1 = require("electron");
function getAppVersion() {
    try {
        const pkgPath = path_1.default.join(__dirname, "..", "package.json");
        const v = JSON.parse(fs_1.default.readFileSync(pkgPath, "utf8")).version;
        return typeof v === "string" ? v : "?";
    }
    catch {
        return "?";
    }
}
function getPrimaryIPv4() {
    const ifaces = os_1.default.networkInterfaces();
    for (const name of Object.keys(ifaces)) {
        for (const iface of ifaces[name]) {
            if (iface.family === "IPv4" && !iface.internal) {
                return iface.address;
            }
        }
    }
    return null;
}
function logsDirectory() {
    return path_1.default.join(electron_1.app.getPath("userData"), "logs");
}
function currentLogFilePath() {
    const dateStr = new Date().toISOString().slice(0, 10);
    return path_1.default.join(logsDirectory(), `godsend-server-${dateStr}.log`);
}
function ensureLogDir() {
    fs_1.default.mkdirSync(logsDirectory(), { recursive: true });
}
function appendLine(sourceTag, message) {
    try {
        ensureLogDir();
        const ts = new Date().toISOString();
        const pid = process.pid;
        const safe = typeof message === "string"
            ? message.replace(/\r?\n/g, "\\n ")
            : String(message);
        fs_1.default.appendFileSync(currentLogFilePath(), `${ts}\tpid=${pid}\t${sourceTag}\t${safe}\n`, "utf8");
    }
    catch (err) {
        console.error("serverLog.appendLine failed:", err.message);
    }
}
function formatEnvForLog(env) {
    const lines = [];
    const keys = Object.keys(env || {})
        .filter((k) => k.startsWith("GODSEND_"))
        .sort();
    for (const k of keys) {
        if (k === "GODSEND_IA_COOKIE" || k === "GODSEND_IA_AUTHORIZATION") {
            lines.push(`${k}=<redacted length=${String(env[k] || "").length}>`);
        }
        else {
            lines.push(`${k}=${env[k]}`);
        }
    }
    return lines;
}
function appendBackendSessionStart(meta) {
    try {
        ensureLogDir();
        const file = currentLogFilePath();
        const envLines = formatEnvForLog(meta.env || {});
        const block = [
            "",
            "================================================================================",
            "GODsend backend session start",
            `timestamp (UTC): ${new Date().toISOString()}`,
            `electronAppVersion: ${meta.appVersion ?? getAppVersion()}`,
            `electron: ${process.versions.electron}  chrome: ${process.versions.chrome}  node: ${process.versions.node}`,
            `platform: ${process.platform}  arch: ${process.arch}  os.release: ${os_1.default.release()}  hostname: ${os_1.default.hostname()}`,
            `packaged: ${electron_1.app.isPackaged}  execPath: ${process.execPath}`,
            `primaryIPv4: ${meta.localIPv4 ?? getPrimaryIPv4() ?? "(none)"}`,
            `GODSEND_HOME (cwd): ${meta.writableRoot ?? ""}`,
            `backendExecutable: ${meta.godsendExePath ?? ""}`,
            `transferFolder (effective): ${meta.transferFolder ?? ""}`,
            "child environment (GODSEND_* only):",
            ...envLines.map((l) => `  ${l}`),
            "================================================================================",
            "",
        ].join("\n");
        fs_1.default.appendFileSync(file, block, "utf8");
    }
    catch (err) {
        console.error("serverLog.appendBackendSessionStart failed:", err.message);
    }
}
function appendBackendSessionEnd(reason, code, signal) {
    appendLine("BACKEND_END", `reason=${reason} exitCode=${code === null ? "null" : code} signal=${signal || "none"}`);
    try {
        ensureLogDir();
        fs_1.default.appendFileSync(currentLogFilePath(), "--------------------------------------------------------------------------------\n", "utf8");
    }
    catch {
        /* ignore */
    }
}
function appendBackendStdout(line) {
    appendLine("BACKEND_OUT", line);
}
function appendBackendStderr(line) {
    appendLine("BACKEND_ERR", line);
}
function appendAppLine(line) {
    appendLine("ELECTRON_UI", line);
}
function appendAppEvent(category, message) {
    appendLine(`APP_${String(category).toUpperCase()}`, message);
}
function getLogInfo() {
    ensureLogDir();
    return {
        logsDirectory: logsDirectory(),
        currentLogFile: currentLogFilePath(),
    };
}
function openLogsFolder() {
    try {
        ensureLogDir();
        const f = currentLogFilePath();
        if (!fs_1.default.existsSync(f)) {
            fs_1.default.writeFileSync(f, "", "utf8");
        }
        electron_1.shell.showItemInFolder(f);
        return { ok: true };
    }
    catch (err) {
        return { ok: false, error: err.message || String(err) };
    }
}
