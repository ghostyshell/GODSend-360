"use strict";
/**
 * IPC handlers for application configuration, startup settings, logs,
 * Internet Archive auth, cache refresh, aria2 ports, and default Xbox drive.
 */
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.register = register;
const electron_1 = require("electron");
const http_1 = __importDefault(require("http"));
const path_1 = __importDefault(require("path"));
const settingsService_1 = require("../services/settingsService");
const backendClient_1 = require("../services/backendClient");
const serverLog_1 = require("../infrastructure/serverLog");
const fileSystem_1 = require("../infrastructure/fileSystem");
const window_1 = require("../app/window");
function register(ipcMain) {
    // ── Startup / logs ──────────────────────────────────────────────────────────
    ipcMain.handle("startup:get", () => {
        const settings = electron_1.app.getLoginItemSettings();
        return !!settings.openAtLogin;
    });
    ipcMain.handle("startup:set", (_event, enabled) => {
        const shouldEnable = Boolean(enabled);
        electron_1.app.setLoginItemSettings({ openAtLogin: shouldEnable, openAsHidden: true });
        return shouldEnable;
    });
    ipcMain.handle("logs:get-info", () => (0, serverLog_1.getLogInfo)());
    ipcMain.handle("logs:open-folder", () => (0, serverLog_1.openLogsFolder)());
    // ── Transfer folder ─────────────────────────────────────────────────────────
    ipcMain.handle("config:get-transfer-folder", () => (0, settingsService_1.getConfiguredTransferFolder)());
    ipcMain.handle("config:get-effective-transfer-folder", () => {
        const writableRoot = (0, fileSystem_1.getWritableRuntimeRoot)();
        const custom = (0, settingsService_1.getConfiguredTransferFolder)();
        return custom
            ? path_1.default.resolve(custom)
            : (0, settingsService_1.getDefaultTransferFolder)(writableRoot);
    });
    ipcMain.handle("config:set-transfer-folder", (_event, folder) => {
        const f = typeof folder === "string" ? folder.trim() : "";
        (0, settingsService_1.writeConfig)({ transferFolder: f });
        (0, serverLog_1.appendAppEvent)("CONFIG", `transferFolder set to ${f ? path_1.default.resolve(f) : "(default runtime/Transfer)"}; restarting backend`);
        (0, backendClient_1.restartGodsendIfRunning)();
        return (0, settingsService_1.getConfiguredTransferFolder)();
    });
    ipcMain.handle("config:choose-transfer-folder", async () => {
        const win = electron_1.BrowserWindow.getFocusedWindow() || (0, window_1.getMainWindow)();
        const r = await electron_1.dialog.showOpenDialog(win || undefined, {
            properties: ["openDirectory", "createDirectory"],
        });
        if (r.canceled || !r.filePaths[0])
            return null;
        return r.filePaths[0];
    });
    // ── Server port ─────────────────────────────────────────────────────────────
    ipcMain.handle("config:get-server-port", () => (0, settingsService_1.getConfiguredServerPort)());
    ipcMain.handle("config:set-server-port", (_event, value) => {
        const n = parseInt(value, 10);
        const port = Number.isInteger(n) && n >= 1 && n <= 65535 ? n : 8080;
        (0, settingsService_1.writeConfig)({ serverPort: port });
        (0, serverLog_1.appendAppEvent)("CONFIG", `serverPort=${port}`);
        (0, backendClient_1.restartGodsendIfRunning)();
        return port;
    });
    // ── Internet Archive auth ───────────────────────────────────────────────────
    ipcMain.handle("config:get-archive-auth", () => ({
        iaEmail: (0, settingsService_1.getConfiguredIAEmail)(),
        iaScreenname: (0, settingsService_1.getConfiguredIAScreenname)(),
        hasSession: Boolean((0, settingsService_1.getConfiguredIACookie)()),
    }));
    ipcMain.handle("config:ia-login", async (_event, payload) => {
        const p = payload || {};
        try {
            const { cookieHeader, screenname, email } = await (0, backendClient_1.loginInternetArchive)(p.email, p.password);
            (0, settingsService_1.writeConfig)({
                iaCookie: cookieHeader,
                iaEmail: email,
                iaScreenname: screenname,
                iaAuthorization: "",
            });
            (0, backendClient_1.restartGodsendIfRunning)();
            (0, serverLog_1.appendAppEvent)("IA_LOGIN", `ok email=${email}`);
            return { ok: true, screenname, email };
        }
        catch (err) {
            const msg = err && err.message ? err.message : String(err);
            (0, serverLog_1.appendAppEvent)("IA_LOGIN", `failed: ${msg}`);
            return { ok: false, error: msg };
        }
    });
    ipcMain.handle("config:ia-logout", () => {
        (0, settingsService_1.writeConfig)({ iaCookie: "", iaAuthorization: "", iaScreenname: "" });
        (0, serverLog_1.appendAppEvent)("IA_LOGIN", "logout; session cleared");
        (0, backendClient_1.restartGodsendIfRunning)();
        return true;
    });
    // ── ROM path ────────────────────────────────────────────────────────────────
    ipcMain.handle("config:get-rom-path", () => (0, settingsService_1.getConfiguredROMPath)() || (0, settingsService_1.getDefaultROMPath)());
    ipcMain.handle("config:set-rom-path", (_event, value) => {
        const v = typeof value === "string" ? value.trim() : "";
        (0, settingsService_1.writeConfig)({ romPath: v });
        (0, serverLog_1.appendAppEvent)("CONFIG", `romPath=${v || "(default)"}`);
        (0, backendClient_1.restartGodsendIfRunning)();
        return (0, settingsService_1.getConfiguredROMPath)();
    });
    // ── Cache refresh ───────────────────────────────────────────────────────────
    ipcMain.handle("config:cache-refresh", (_event, platform) => {
        const p = typeof platform === "string" && platform ? platform : "all";
        (0, serverLog_1.appendAppEvent)("CACHE", `refresh requested platform=${p}`);
        return new Promise((resolve) => {
            const req = http_1.default.get(`http://localhost:${(0, settingsService_1.getConfiguredServerPort)()}/cache-refresh?platform=${encodeURIComponent(p)}`, (res) => {
                let data = "";
                res.on("data", (chunk) => { data += chunk; });
                res.on("end", () => {
                    (0, serverLog_1.appendAppEvent)("CACHE", `refresh http status=${res.statusCode} bodyLen=${data.length}`);
                    resolve({ ok: true, data });
                });
            });
            req.on("error", (err) => {
                (0, serverLog_1.appendAppEvent)("CACHE", `refresh error: ${err.message}`);
                resolve({ ok: false, error: err.message });
            });
            req.setTimeout(5000, () => {
                req.destroy();
                (0, serverLog_1.appendAppEvent)("CACHE", "refresh error: timeout");
                resolve({ ok: false, error: "timeout" });
            });
        });
    });
    // ── Xbox connection (IP, FTP credentials, scripts path) ────────────────────
    ipcMain.handle("config:get-xbox-connection", () => ({
        xboxIp: (0, settingsService_1.getConfiguredXboxIP)(),
        ftpUser: (0, settingsService_1.getConfiguredFtpUser)(),
        ftpPassword: (0, settingsService_1.getConfiguredFtpPassword)(),
        ftpScriptsPath: (0, settingsService_1.getConfiguredFtpScriptsPath)(),
    }));
    ipcMain.handle("config:set-xbox-connection", (_event, payload) => {
        const p = payload || {};
        (0, settingsService_1.writeConfig)({
            xboxIp: typeof p.xboxIp === "string" ? p.xboxIp.trim() : (0, settingsService_1.getConfiguredXboxIP)(),
            ftpUser: typeof p.ftpUser === "string" ? p.ftpUser.trim() : (0, settingsService_1.getConfiguredFtpUser)(),
            ftpPassword: typeof p.ftpPassword === "string" ? p.ftpPassword : (0, settingsService_1.getConfiguredFtpPassword)(),
            ftpScriptsPath: typeof p.ftpScriptsPath === "string" ? p.ftpScriptsPath.trim() : (0, settingsService_1.getConfiguredFtpScriptsPath)(),
        });
        (0, serverLog_1.appendAppEvent)("CONFIG", `xboxConnection saved (ftpUser=${(0, settingsService_1.getConfiguredFtpUser)()})`);
        if (!p.skipRestart)
            (0, backendClient_1.restartGodsendIfRunning)();
        return true;
    });
    // ── Default Xbox drive ──────────────────────────────────────────────────────
    ipcMain.handle("config:get-default-xbox-drive", () => (0, settingsService_1.getConfiguredDefaultXboxDrive)());
    ipcMain.handle("config:set-default-xbox-drive", (_event, value) => {
        const v = typeof value === "string" ? value.trim() : "";
        (0, settingsService_1.writeConfig)({ defaultXboxDrive: v });
        (0, backendClient_1.restartGodsendIfRunning)();
        return v;
    });
    // ── Aria2 ports ─────────────────────────────────────────────────────────────
    ipcMain.handle("config:get-aria2-listen-port", () => (0, settingsService_1.getConfiguredAria2ListenPort)());
    ipcMain.handle("config:set-aria2-listen-port", (_event, value) => {
        const n = parseInt(value, 10);
        const v = (Number.isInteger(n) && n >= 1 && n <= 65535) ? String(n) : "";
        (0, settingsService_1.writeConfig)({ aria2ListenPort: v });
        (0, backendClient_1.restartGodsendIfRunning)();
        return v;
    });
    ipcMain.handle("config:get-aria2-dht-port", () => (0, settingsService_1.getConfiguredAria2DhtPort)());
    ipcMain.handle("config:set-aria2-dht-port", (_event, value) => {
        const n = parseInt(value, 10);
        const v = (Number.isInteger(n) && n >= 1 && n <= 65535) ? String(n) : "";
        (0, settingsService_1.writeConfig)({ aria2DhtPort: v });
        (0, backendClient_1.restartGodsendIfRunning)();
        return v;
    });
    // ── Data status / clear ─────────────────────────────────────────────────────
    ipcMain.handle("data:status", () => {
        return new Promise((resolve) => {
            const port = (0, settingsService_1.getConfiguredServerPort)();
            const req = http_1.default.get(`http://localhost:${port}/data/status`, (res) => {
                let data = "";
                res.on("data", (chunk) => { data += chunk; });
                res.on("end", () => {
                    try {
                        resolve({ ok: true, ...JSON.parse(data) });
                    }
                    catch {
                        resolve({ ok: false, error: "parse error" });
                    }
                });
            });
            req.on("error", (err) => resolve({ ok: false, error: err.message }));
            req.setTimeout(5000, () => { req.destroy(); resolve({ ok: false, error: "timeout" }); });
        });
    });
    ipcMain.handle("data:clear", () => {
        return new Promise((resolve) => {
            const port = (0, settingsService_1.getConfiguredServerPort)();
            const req = http_1.default.get(`http://localhost:${port}/data/clear`, (res) => {
                let data = "";
                res.on("data", (chunk) => { data += chunk; });
                res.on("end", () => resolve({ ok: true }));
            });
            req.on("error", (err) => resolve({ ok: false, error: err.message }));
            req.setTimeout(10000, () => { req.destroy(); resolve({ ok: false, error: "timeout" }); });
        });
    });
    // ── FTP scripts path default ────────────────────────────────────────────────
    ipcMain.handle("config:get-ftp-scripts-path-default", () => (0, settingsService_1.getDefaultFtpScriptsPath)());
}
