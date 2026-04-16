"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.configFilePath = configFilePath;
exports.readConfig = readConfig;
exports.writeConfig = writeConfig;
exports.getConfiguredTransferFolder = getConfiguredTransferFolder;
exports.getDefaultTransferFolder = getDefaultTransferFolder;
exports.getConfiguredIACookie = getConfiguredIACookie;
exports.getConfiguredIAAuthorization = getConfiguredIAAuthorization;
exports.getConfiguredServerPort = getConfiguredServerPort;
exports.getConfiguredIAEmail = getConfiguredIAEmail;
exports.getConfiguredIAScreenname = getConfiguredIAScreenname;
exports.getConfiguredROMPath = getConfiguredROMPath;
exports.getDefaultROMPath = getDefaultROMPath;
exports.getConfiguredXboxIP = getConfiguredXboxIP;
exports.getConfiguredFtpUser = getConfiguredFtpUser;
exports.getConfiguredFtpPassword = getConfiguredFtpPassword;
exports.getDefaultFtpScriptsPath = getDefaultFtpScriptsPath;
exports.getConfiguredFtpScriptsPath = getConfiguredFtpScriptsPath;
exports.getConfiguredDefaultXboxDrive = getConfiguredDefaultXboxDrive;
exports.getConfiguredAria2ListenPort = getConfiguredAria2ListenPort;
exports.getConfiguredAria2DhtPort = getConfiguredAria2DhtPort;
exports.buildGodsendEnv = buildGodsendEnv;
const electron_1 = require("electron");
const path_1 = __importDefault(require("path"));
const fs_1 = __importDefault(require("fs"));
const fileSystem_1 = require("../infrastructure/fileSystem");
function configFilePath() {
    return path_1.default.join(electron_1.app.getPath("userData"), "config.json");
}
function readConfig() {
    try {
        return JSON.parse(fs_1.default.readFileSync(configFilePath(), "utf8"));
    }
    catch {
        return {};
    }
}
function writeConfig(partial) {
    const next = { ...readConfig(), ...partial };
    (0, fileSystem_1.ensureDirectory)(path_1.default.dirname(configFilePath()));
    fs_1.default.writeFileSync(configFilePath(), JSON.stringify(next, null, 2), "utf8");
    return next;
}
function getConfiguredTransferFolder() {
    const v = readConfig().transferFolder;
    return typeof v === "string" ? v.trim() : "";
}
function getDefaultTransferFolder(writableRoot) {
    return path_1.default.join(writableRoot, "Transfer");
}
function getConfiguredIACookie() {
    const v = readConfig().iaCookie;
    return typeof v === "string" ? v.trim() : "";
}
function getConfiguredIAAuthorization() {
    const v = readConfig().iaAuthorization;
    return typeof v === "string" ? v.trim() : "";
}
function getConfiguredServerPort() {
    const v = readConfig().serverPort;
    const n = parseInt(String(v), 10);
    if (isNaN(n) || n < 1 || n > 65535)
        return 8080;
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
    return "/Hdd1/Aurora/User/Scripts/Utility/GODSend";
}
function getConfiguredFtpScriptsPath() {
    const v = readConfig().ftpScriptsPath;
    return typeof v === "string" && v.trim() ? v.trim() : getDefaultFtpScriptsPath();
}
function getConfiguredDefaultXboxDrive() {
    const v = readConfig().defaultXboxDrive;
    return typeof v === "string" ? v.trim() : "";
}
function getConfiguredAria2ListenPort() {
    const v = readConfig().aria2ListenPort;
    const n = parseInt(String(v), 10);
    return isNaN(n) || n < 1 || n > 65535 ? "" : String(n);
}
function getConfiguredAria2DhtPort() {
    const v = readConfig().aria2DhtPort;
    const n = parseInt(String(v), 10);
    return isNaN(n) || n < 1 || n > 65535 ? "" : String(n);
}
function buildGodsendEnv(writableRoot) {
    const env = { ...process.env, GODSEND_HOME: writableRoot };
    const custom = getConfiguredTransferFolder();
    if (custom)
        env.GODSEND_TRANSFER = path_1.default.resolve(custom);
    const iaCookie = getConfiguredIACookie();
    if (iaCookie)
        env.GODSEND_IA_COOKIE = iaCookie;
    const iaAuth = getConfiguredIAAuthorization();
    if (iaAuth)
        env.GODSEND_IA_AUTHORIZATION = iaAuth;
    const romPath = getConfiguredROMPath();
    if (romPath)
        env.GODSEND_ROM_PATH = romPath;
    env.GODSEND_PORT = String(getConfiguredServerPort());
    env.GODSEND_FTP_USER = getConfiguredFtpUser();
    env.GODSEND_FTP_PASS = getConfiguredFtpPassword();
    const defaultDrive = getConfiguredDefaultXboxDrive();
    if (defaultDrive)
        env.GODSEND_DEFAULT_DRIVE = defaultDrive;
    const aria2Listen = getConfiguredAria2ListenPort();
    if (aria2Listen)
        env.GODSEND_ARIA2_LISTEN_PORT = aria2Listen;
    const aria2Dht = getConfiguredAria2DhtPort();
    if (aria2Dht)
        env.GODSEND_ARIA2_DHT_PORT = aria2Dht;
    return env;
}
