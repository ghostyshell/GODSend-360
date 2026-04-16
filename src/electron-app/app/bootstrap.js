"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.bootstrapApp = bootstrapApp;
const electron_1 = require("electron");
const url_1 = require("url");
const fs_1 = __importDefault(require("fs"));
const serverLog_1 = require("../infrastructure/serverLog");
const auroraLibraryCache_1 = require("../infrastructure/auroraLibraryCache");
const electronTray_1 = require("../infrastructure/electronTray");
const backendClient_1 = require("../services/backendClient");
const autoSyncService_1 = require("../services/autoSyncService");
const window_1 = require("./window");
const configHandlers = __importStar(require("../ipc/configHandlers"));
const xboxFtpHandlers = __importStar(require("../ipc/xboxFtpHandlers"));
const auroraLibraryHandlers = __importStar(require("../ipc/auroraLibraryHandlers"));
const auroraAssetHandlers = __importStar(require("../ipc/auroraAssetHandlers"));
const browseHandlers = __importStar(require("../ipc/browseHandlers"));
const toolsHandlers = __importStar(require("../ipc/toolsHandlers"));
function registerIpcHandlers() {
    electron_1.ipcMain.handle("godsend:get-buffer", () => (0, backendClient_1.getOutputBuffer)());
    electron_1.ipcMain.handle("godsend:start", () => { (0, backendClient_1.startGodsend)(); return true; });
    electron_1.ipcMain.handle("godsend:stop", () => { (0, backendClient_1.stopGodsend)(); return true; });
    electron_1.ipcMain.handle("godsend:restart", () => {
        if ((0, backendClient_1.getProcess)()) {
            (0, backendClient_1.restartGodsendIfRunning)();
        }
        else {
            (0, backendClient_1.startGodsend)();
        }
        return true;
    });
    configHandlers.register(electron_1.ipcMain);
    xboxFtpHandlers.register(electron_1.ipcMain);
    auroraLibraryHandlers.register(electron_1.ipcMain);
    auroraAssetHandlers.register(electron_1.ipcMain);
    browseHandlers.register(electron_1.ipcMain);
    toolsHandlers.register(electron_1.ipcMain);
}
function bootstrapApp() {
    electron_1.app.whenReady().then(() => {
        electron_1.protocol.handle("godsend-aurora", (request) => {
            const root = (0, auroraLibraryCache_1.getActiveAuroraCacheRoot)();
            if (!root)
                return new Response(null, { status: 404 });
            let u;
            try {
                u = new URL(request.url);
            }
            catch {
                return new Response(null, { status: 400 });
            }
            if (u.hostname !== "cdn")
                return new Response(null, { status: 404 });
            const rel = (u.pathname || "").replace(/^\/+/, "");
            const full = (0, auroraLibraryCache_1.safeFileUnderRoot)(root, rel);
            if (!full || !fs_1.default.existsSync(full))
                return new Response(null, { status: 404 });
            try {
                return electron_1.net.fetch((0, url_1.pathToFileURL)(full).href);
            }
            catch {
                return new Response(null, { status: 500 });
            }
        });
        electron_1.app.setAppUserModelId("com.abbu.godsend");
        (0, serverLog_1.appendAppEvent)("LIFECYCLE", `app ready userData=${electron_1.app.getPath("userData")} logDir=${(0, serverLog_1.getLogInfo)().logsDirectory}`);
        (0, window_1.createMainWindow)();
        (0, electronTray_1.createTray)((0, window_1.getMainWindow)(), {
            onQuit: () => {
                (0, window_1.setIsQuitting)(true);
                electron_1.app.quit();
            },
        });
        (0, window_1.getMainWindow)().webContents.once("did-finish-load", () => {
            (0, backendClient_1.startGodsend)();
        });
        (0, backendClient_1.onFTPComplete)(({ gameName, titleId, xboxIp }) => {
            (async () => {
                if (titleId) {
                    try {
                        await (0, autoSyncService_1.autoUploadAuroraAssets)(titleId, xboxIp);
                    }
                    catch (err) {
                        (0, backendClient_1.addOutputLine)(`[WARN] Auto-assets failed for ${gameName}: ${err.message || err}`);
                    }
                }
                try {
                    await (0, autoSyncService_1.doAuroraLibrarySync)();
                }
                catch (err) {
                    (0, backendClient_1.addOutputLine)(`[WARN] Auto-sync failed after ${gameName}: ${err.message || err}`);
                }
            })();
        });
    });
    electron_1.app.on("before-quit", () => {
        (0, window_1.setIsQuitting)(true);
        (0, serverLog_1.appendAppEvent)("LIFECYCLE", "application before-quit");
        (0, backendClient_1.stopGodsend)();
    });
    electron_1.app.on("window-all-closed", () => {
        // Intentionally do nothing — prevent default quit so tray keeps the app alive.
    });
    registerIpcHandlers();
}
