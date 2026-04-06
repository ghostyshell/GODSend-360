const fs = require("fs");
const path = require("path");
const os = require("os");
const { app, shell } = require("electron");

function getAppVersion() {
  try {
    const pkgPath = path.join(__dirname, "..", "package.json");
    const v = JSON.parse(fs.readFileSync(pkgPath, "utf8")).version;
    return typeof v === "string" ? v : "?";
  } catch {
    return "?";
  }
}

function getPrimaryIPv4() {
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

function logsDirectory() {
  return path.join(app.getPath("userData"), "logs");
}

function currentLogFilePath() {
  const dateStr = new Date().toISOString().slice(0, 10);
  return path.join(logsDirectory(), `godsend-server-${dateStr}.log`);
}

function ensureLogDir() {
  fs.mkdirSync(logsDirectory(), { recursive: true });
}

/** Single-line log entry (ISO time, pid, source tag, message). */
function appendLine(sourceTag, message) {
  try {
    ensureLogDir();
    const ts = new Date().toISOString();
    const pid = process.pid;
    const safe =
      typeof message === "string"
        ? message.replace(/\r?\n/g, "\\n ")
        : String(message);
    fs.appendFileSync(
      currentLogFilePath(),
      `${ts}\tpid=${pid}\t${sourceTag}\t${safe}\n`,
      "utf8"
    );
  } catch (err) {
    console.error("serverLog.appendLine failed:", err.message);
  }
}

/** Redacted summary of GODSEND_* env passed to the child (no secrets). */
function formatEnvForLog(env) {
  const lines = [];
  const keys = Object.keys(env || {})
    .filter((k) => k.startsWith("GODSEND_"))
    .sort();
  for (const k of keys) {
    if (k === "GODSEND_IA_COOKIE" || k === "GODSEND_IA_AUTHORIZATION") {
      lines.push(`${k}=<redacted length=${String(env[k] || "").length}>`);
    } else {
      lines.push(`${k}=${env[k]}`);
    }
  }
  return lines;
}

/**
 * Written when the Go backend process is spawned. Includes host and path context
 * (no IA cookie / password values).
 */
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
      `platform: ${process.platform}  arch: ${process.arch}  os.release: ${os.release()}  hostname: ${os.hostname()}`,
      `packaged: ${app.isPackaged}  execPath: ${process.execPath}`,
      `primaryIPv4: ${meta.localIPv4 ?? getPrimaryIPv4() ?? "(none)"}`,
      `GODSEND_HOME (cwd): ${meta.writableRoot ?? ""}`,
      `backendExecutable: ${meta.godsendExePath ?? ""}`,
      `transferFolder (effective): ${meta.transferFolder ?? ""}`,
      "child environment (GODSEND_* only):",
      ...envLines.map((l) => `  ${l}`),
      "================================================================================",
      "",
    ].join("\n");
    fs.appendFileSync(file, block, "utf8");
  } catch (err) {
    console.error("serverLog.appendBackendSessionStart failed:", err.message);
  }
}

function appendBackendSessionEnd(reason, code, signal) {
  appendLine(
    "BACKEND_END",
    `reason=${reason} exitCode=${code === null ? "null" : code} signal=${signal || "none"}`
  );
  try {
    ensureLogDir();
    fs.appendFileSync(
      currentLogFilePath(),
      "--------------------------------------------------------------------------------\n",
      "utf8"
    );
  } catch {
    /* ignore */
  }
}

function appendBackendStdout(line) {
  appendLine("BACKEND_OUT", line);
}

function appendBackendStderr(line) {
  appendLine("BACKEND_ERR", line);
}

/** UI / Electron messages mirrored from the on-screen buffer ([INFO], [ERR], etc.). */
function appendAppLine(line) {
  appendLine("ELECTRON_UI", line);
}

/** Config changes, FTP helper, cache refresh, lifecycle (structured). */
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
    if (!fs.existsSync(f)) {
      fs.writeFileSync(f, "", "utf8");
    }
    shell.showItemInFolder(f);
    return { ok: true };
  } catch (err) {
    return { ok: false, error: err.message || String(err) };
  }
}

module.exports = {
  getAppVersion,
  getPrimaryIPv4,
  getLogInfo,
  openLogsFolder,
  appendBackendSessionStart,
  appendBackendSessionEnd,
  appendBackendStdout,
  appendBackendStderr,
  appendAppLine,
  appendAppEvent,
};
