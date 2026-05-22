import { app } from "electron";
import path from "path";
import fs from "fs";
import { ensureDirectory } from "../infrastructure/fileSystem";

export interface GodsendConfig {
  appDataDir?: string;
  storagePath?: string;
  transferFolder?: string;
  iaCookie?: string;
  iaAuthorization?: string;
  serverPort?: number | string;
  iaEmail?: string;
  iaScreenname?: string;
  romPath?: string;
  xboxIp?: string;
  ftpUser?: string;
  ftpPassword?: string;
  ftpScriptsPath?: string;
  defaultXboxDrive?: string;
  customGodPath?: string;
  customXexPath?: string;
  aria2ListenPort?: string | number;
  aria2DhtPort?: string | number;
}

export function configFilePath(): string {
  return path.join(app.getPath("userData"), "config.json");
}

export function readConfig(): GodsendConfig {
  try {
    return JSON.parse(fs.readFileSync(configFilePath(), "utf8"));
  } catch {
    return {};
  }
}

export function writeConfig(partial: Partial<GodsendConfig>): GodsendConfig {
  const next = { ...readConfig(), ...partial };
  ensureDirectory(path.dirname(configFilePath()));
  fs.writeFileSync(configFilePath(), JSON.stringify(next, null, 2), "utf8");
  return next;
}

export function getConfiguredStoragePath(): string {
  const v = readConfig().storagePath;
  return typeof v === "string" ? v.trim() : "";
}

export function getConfiguredTransferFolder(): string {
  const v = readConfig().transferFolder;
  return typeof v === "string" ? v.trim() : "";
}

export function getDefaultTransferFolder(writableRoot: string): string {
  return path.join(writableRoot, "Transfer");
}

export function getConfiguredIACookie(): string {
  const v = readConfig().iaCookie;
  return typeof v === "string" ? v.trim() : "";
}

export function getConfiguredIAAuthorization(): string {
  const v = readConfig().iaAuthorization;
  return typeof v === "string" ? v.trim() : "";
}

export function getConfiguredServerPort(): number {
  const v = readConfig().serverPort;
  const n = parseInt(String(v), 10);
  if (isNaN(n) || n < 1 || n > 65535) return 8080;
  return n;
}

export function getConfiguredIAEmail(): string {
  const v = readConfig().iaEmail;
  return typeof v === "string" ? v.trim() : "";
}

export function getConfiguredIAScreenname(): string {
  const v = readConfig().iaScreenname;
  return typeof v === "string" ? v.trim() : "";
}

export function getConfiguredROMPath(): string {
  const v = readConfig().romPath;
  return typeof v === "string" ? v.trim() : "";
}

export function getDefaultROMPath(): string {
  return "Emulators\\RetroArch\\roms";
}

export function getConfiguredXboxIP(): string {
  const v = readConfig().xboxIp;
  return typeof v === "string" ? v.trim() : "";
}

export function getConfiguredFtpUser(): string {
  const v = readConfig().ftpUser;
  return typeof v === "string" && v.trim() !== "" ? v.trim() : "xboxftp";
}

export function getConfiguredFtpPassword(): string {
  const v = readConfig().ftpPassword;
  return typeof v === "string" ? v : "xboxftp";
}

export function getDefaultFtpScriptsPath(): string {
  return "/Hdd1/Aurora/User/Scripts/Utility/GODSend";
}

export function getConfiguredFtpScriptsPath(): string {
  const v = readConfig().ftpScriptsPath;
  return typeof v === "string" && v.trim() ? v.trim() : getDefaultFtpScriptsPath();
}

export function getConfiguredDefaultXboxDrive(): string {
  const v = readConfig().defaultXboxDrive;
  return typeof v === "string" ? v.trim() : "";
}

export function getConfiguredAria2ListenPort(): string {
  const v = readConfig().aria2ListenPort;
  const n = parseInt(String(v), 10);
  return isNaN(n) || n < 1 || n > 65535 ? "" : String(n);
}

export function getConfiguredAria2DhtPort(): string {
  const v = readConfig().aria2DhtPort;
  const n = parseInt(String(v), 10);
  return isNaN(n) || n < 1 || n > 65535 ? "" : String(n);
}

export function getConfiguredCustomGodPath(): string {
  const v = readConfig().customGodPath;
  return typeof v === "string" ? v.trim() : "";
}

export function getConfiguredCustomXexPath(): string {
  const v = readConfig().customXexPath;
  return typeof v === "string" ? v.trim() : "";
}

export function buildGodsendEnv(writableRoot: string): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { ...process.env, GODSEND_HOME: writableRoot };
  const custom = getConfiguredTransferFolder();
  if (custom) env.GODSEND_TRANSFER = path.resolve(custom);
  const iaCookie = getConfiguredIACookie();
  if (iaCookie) env.GODSEND_IA_COOKIE = iaCookie;
  const iaAuth = getConfiguredIAAuthorization();
  if (iaAuth) env.GODSEND_IA_AUTHORIZATION = iaAuth;
  const romPath = getConfiguredROMPath();
  if (romPath) env.GODSEND_ROM_PATH = romPath;
  env.GODSEND_PORT = String(getConfiguredServerPort());
  env.GODSEND_FTP_USER = getConfiguredFtpUser();
  env.GODSEND_FTP_PASS = getConfiguredFtpPassword();
  const defaultDrive = getConfiguredDefaultXboxDrive();
  if (defaultDrive) env.GODSEND_DEFAULT_DRIVE = defaultDrive;
  const aria2Listen = getConfiguredAria2ListenPort();
  if (aria2Listen) env.GODSEND_ARIA2_LISTEN_PORT = aria2Listen;
  const aria2Dht = getConfiguredAria2DhtPort();
  if (aria2Dht) env.GODSEND_ARIA2_DHT_PORT = aria2Dht;
  const customGodPath = getConfiguredCustomGodPath();
  if (customGodPath) env.GODSEND_CUSTOM_GOD_PATH = customGodPath;
  const customXexPath = getConfiguredCustomXexPath();
  if (customXexPath) env.GODSEND_CUSTOM_XEX_PATH = customXexPath;
  return env;
}
