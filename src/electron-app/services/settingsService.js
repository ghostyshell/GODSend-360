const { app } = require("electron");
const path = require("path");
const fs = require("fs");
const { ensureDirectory } = require("../infrastructure/fileSystem");

function configFilePath() {
  return path.join(app.getPath("userData"), "config.json");
}

function readConfig() {
  try {
    return JSON.parse(fs.readFileSync(configFilePath(), "utf8"));
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

function getConfiguredServerPort() {
  const v = readConfig().serverPort;
  const n = parseInt(v, 10);
  if (isNaN(n) || n < 1 || n > 65535) return 8080;
  return n;
}

function getConfiguredIAEmail() {
  const v = readConfig().iaEmail;
  return typeof v === "string" ? v.trim() : "";
}

function getConfiguredIAScreenname() {
  const v = readConfig().iaScreenname;
  return typeof v === "string" ? v.trim() : "";
}

function getConfiguredROMPath() {
  const v = readConfig().romPath;
  return typeof v === "string" ? v.trim() : "";
}

function getDefaultROMPath() {
  return "Emulators\\RetroArch\\roms";
}

function getConfiguredXboxIP() {
  const v = readConfig().xboxIp;
  return typeof v === "string" ? v.trim() : "";
}

function getConfiguredFtpUser() {
  const v = readConfig().ftpUser;
  return typeof v === "string" && v.trim() !== "" ? v.trim() : "xboxftp";
}

function getConfiguredFtpPassword() {
  const v = readConfig().ftpPassword;
  return typeof v === "string" ? v : "xboxftp";
}

function getDefaultFtpScriptsPath() {
  // Aurora expects `Scripts/Utility` (singular) on most setups; USB FTP often shows an extra `Apps` segment.
  return "/Hdd1/Aurora/User/Scripts/Utility/GODSend";
}

function getConfiguredFtpScriptsPath() {
  const v = readConfig().ftpScriptsPath;
  return typeof v === "string" && v.trim() ? v.trim() : getDefaultFtpScriptsPath();
}

function buildGodsendEnv(writableRoot) {
  const env = { ...process.env, GODSEND_HOME: writableRoot };
  const custom = getConfiguredTransferFolder();
  if (custom) env.GODSEND_TRANSFER = path.resolve(custom);
  const iaCookie = getConfiguredIACookie();
  if (iaCookie) env.GODSEND_IA_COOKIE = iaCookie;
  const iaAuth = getConfiguredIAAuthorization();
  if (iaAuth) env.GODSEND_IA_AUTHORIZATION = iaAuth;
  env.GODSEND_IA_CONCURRENCY = String(getConfiguredIAConcurrency());
  const romPath = getConfiguredROMPath();
  if (romPath) env.GODSEND_ROM_PATH = romPath;
  env.GODSEND_PORT = String(getConfiguredServerPort());
  env.GODSEND_FTP_USER = getConfiguredFtpUser();
  env.GODSEND_FTP_PASS = getConfiguredFtpPassword();
  return env;
}

module.exports = {
  readConfig,
  writeConfig,
  getConfiguredTransferFolder,
  getDefaultTransferFolder,
  getConfiguredROMPath,
  getDefaultROMPath,
  getConfiguredIACookie,
  getConfiguredIAAuthorization,
  getConfiguredIAConcurrency,
  getConfiguredServerPort,
  getConfiguredIAEmail,
  getConfiguredIAScreenname,
  getConfiguredXboxIP,
  getConfiguredFtpUser,
  getConfiguredFtpPassword,
  getDefaultFtpScriptsPath,
  getConfiguredFtpScriptsPath,
  buildGodsendEnv,
};
