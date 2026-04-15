import fs from "fs";
import path from "path";
import os from "os";
import { app, shell } from "electron";

export interface BackendSessionMeta {
  appVersion?: string;
  writableRoot?: string;
  godsendExePath?: string;
  transferFolder?: string;
  env?: NodeJS.ProcessEnv;
  localIPv4?: string | null;
}

export function getAppVersion(): string {
  try {
    const pkgPath = path.join(__dirname, "..", "package.json");
    const v = JSON.parse(fs.readFileSync(pkgPath, "utf8")).version;
    return typeof v === "string" ? v : "?";
  } catch {
    return "?";
  }
}

export function getPrimaryIPv4(): string | null {
  const ifaces = os.networkInterfaces();
  for (const name of Object.keys(ifaces)) {
    for (const iface of ifaces[name]!) {
      if (iface.family === "IPv4" && !iface.internal) {
        return iface.address;
      }
    }
  }
  return null;
}

function logsDirectory(): string {
  return path.join(app.getPath("userData"), "logs");
}

function currentLogFilePath(): string {
  const dateStr = new Date().toISOString().slice(0, 10);
  return path.join(logsDirectory(), `godsend-server-${dateStr}.log`);
}

function ensureLogDir(): void {
  fs.mkdirSync(logsDirectory(), { recursive: true });
}

function appendLine(sourceTag: string, message: string): void {
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
  } catch (err: any) {
    console.error("serverLog.appendLine failed:", err.message);
  }
}

function formatEnvForLog(env: NodeJS.ProcessEnv): string[] {
  const lines: string[] = [];
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

export function appendBackendSessionStart(meta: BackendSessionMeta): void {
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
  } catch (err: any) {
    console.error("serverLog.appendBackendSessionStart failed:", err.message);
  }
}

export function appendBackendSessionEnd(reason: string, code: number | null, signal: string | null): void {
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

export function appendBackendStdout(line: string): void {
  appendLine("BACKEND_OUT", line);
}

export function appendBackendStderr(line: string): void {
  appendLine("BACKEND_ERR", line);
}

export function appendAppLine(line: string): void {
  appendLine("ELECTRON_UI", line);
}

export function appendAppEvent(category: string, message: string): void {
  appendLine(`APP_${String(category).toUpperCase()}`, message);
}

export function getLogInfo(): { logsDirectory: string; currentLogFile: string } {
  ensureLogDir();
  return {
    logsDirectory: logsDirectory(),
    currentLogFile: currentLogFilePath(),
  };
}

export function openLogsFolder(): { ok: boolean; error?: string } {
  try {
    ensureLogDir();
    const f = currentLogFilePath();
    if (!fs.existsSync(f)) {
      fs.writeFileSync(f, "", "utf8");
    }
    shell.showItemInFolder(f);
    return { ok: true };
  } catch (err: any) {
    return { ok: false, error: err.message || String(err) };
  }
}
