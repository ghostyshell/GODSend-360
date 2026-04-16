"use strict";
/**
 * IPC handlers for direct Xbox FTP operations:
 *   xbox:ping, xbox:ftp-test, xbox:ftp-scan, xbox:ftp-scripts
 *   xbox:list-drives, xbox:list-games, xbox:fetch-covers
 *
 * All FTP operations are proxied through the Go backend for centralised tracking.
 */
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.register = register;
const os_1 = __importDefault(require("os"));
const net_1 = __importDefault(require("net"));
const settingsService_1 = require("../services/settingsService");
const serverLog_1 = require("../infrastructure/serverLog");
const fileSystem_1 = require("../infrastructure/fileSystem");
const window_1 = require("../app/window");
const auroraPathHelper_1 = require("../services/auroraPathHelper");
const auroraLibraryService_1 = require("../services/auroraLibraryService");
const backendHttp_1 = require("../infrastructure/backendHttp");
function getLocalIPAddress() {
    const ifaces = os_1.default.networkInterfaces();
    for (const name of Object.keys(ifaces)) {
        for (const iface of ifaces[name]) {
            if (iface.family === "IPv4" && !iface.internal)
                return iface.address;
        }
    }
    return null;
}
function register(ipcMain) {
    // ── Ping (proxied to Go backend) ──────────────────────────────────────────
    ipcMain.handle("xbox:ping", async () => {
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        try {
            return await (0, backendHttp_1.backendPost)("/ftp/ping", { ip: xboxIp });
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── FTP Test (verbose connection diagnostics — proxied to Go backend) ─────
    ipcMain.handle("xbox:ftp-test", async (_event, payload) => {
        const p = payload || {};
        const xboxIp = (typeof p.xboxIp === "string" ? p.xboxIp.trim() : "") || (0, settingsService_1.getConfiguredXboxIP)();
        const ftpUser = (typeof p.ftpUser === "string" ? p.ftpUser.trim() : "") || (0, settingsService_1.getConfiguredFtpUser)();
        const ftpPass = (typeof p.ftpPassword === "string" ? p.ftpPassword : "") || (0, settingsService_1.getConfiguredFtpPassword)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        const sendDebug = (line) => {
            const win = (0, window_1.getMainWindow)();
            if (win && !win.isDestroyed())
                win.webContents.send("godsend-ftp-debug", line);
        };
        try {
            const r = await (0, backendHttp_1.backendPost)("/ftp/test", { ip: xboxIp, user: ftpUser, password: ftpPass });
            // Replay log lines to the renderer
            if (Array.isArray(r.log)) {
                for (const line of r.log)
                    sendDebug(line);
            }
            return { ok: r.ok || false, error: r.ok ? undefined : "FTP test failed" };
        }
        catch (err) {
            sendDebug(`[TEST] FAILED: ${err.message || String(err)}`);
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── FTP Port Scanner (raw TCP — stays Electron-side, no FTP library needed) ─
    ipcMain.handle("xbox:ftp-scan", async (_event, subnet) => {
        if (typeof subnet !== "string" || !/^\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(subnet.trim())) {
            return { ok: false, error: "Invalid subnet. Use format like 192.168.1" };
        }
        subnet = subnet.trim();
        const sendDebug = (line) => {
            const win = (0, window_1.getMainWindow)();
            if (win && !win.isDestroyed())
                win.webContents.send("godsend-ftp-debug", line);
        };
        sendDebug(`[SCAN] Scanning ${subnet}.1 - ${subnet}.254 on port 21...`);
        const found = [];
        const BATCH = 25;
        const TIMEOUT = 2000;
        for (let batchStart = 1; batchStart <= 254; batchStart += BATCH) {
            const batchEnd = Math.min(batchStart + BATCH - 1, 254);
            sendDebug(`[SCAN] Probing ${subnet}.${batchStart} - ${subnet}.${batchEnd}...`);
            const promises = [];
            for (let i = batchStart; i <= batchEnd; i++) {
                const ip = `${subnet}.${i}`;
                promises.push(new Promise((resolve) => {
                    const sock = new net_1.default.Socket();
                    sock.setTimeout(TIMEOUT);
                    sock.once("connect", () => { sock.destroy(); resolve(ip); });
                    sock.once("timeout", () => { sock.destroy(); resolve(null); });
                    sock.once("error", () => { sock.destroy(); resolve(null); });
                    sock.connect(21, ip);
                }));
            }
            const results = await Promise.all(promises);
            for (const ip of results) {
                if (ip) {
                    found.push(ip);
                    sendDebug(`[SCAN] FOUND: ${ip}:21 is open (FTP)`);
                }
            }
        }
        if (found.length === 0) {
            sendDebug(`[SCAN] No FTP servers found on ${subnet}.0/24.`);
        }
        else {
            sendDebug(`[SCAN] Done. Found ${found.length} host(s) with FTP: ${found.join(", ")}`);
        }
        return { ok: true, hosts: found };
    });
    // ── Upload Aurora scripts (proxied to Go backend — async tracked) ──────────
    ipcMain.handle("xbox:ftp-scripts", async (_event, payload) => {
        const p = payload || {};
        const xboxIp = (typeof p.xboxIp === "string" ? p.xboxIp.trim() : "") || (0, settingsService_1.getConfiguredXboxIP)();
        const remotePath = (typeof p.ftpScriptsPath === "string" && p.ftpScriptsPath.trim())
            ? p.ftpScriptsPath.trim()
            : (0, settingsService_1.getConfiguredFtpScriptsPath)();
        const sendProgress = (msg) => {
            (0, serverLog_1.appendAppEvent)("FTP", msg);
            const win = (0, window_1.getMainWindow)();
            if (win && !win.isDestroyed())
                win.webContents.send("godsend-ftp-progress", msg);
        };
        if (!xboxIp)
            return { ok: false, error: "Xbox IP address is required." };
        const scriptsDir = (0, fileSystem_1.getAuroraScriptsPath)();
        const pcIp = getLocalIPAddress();
        const serverPort = (0, settingsService_1.getConfiguredServerPort)();
        if (!pcIp) {
            return { ok: false, error: "Could not detect this PC's local IPv4 address for state.lua patching." };
        }
        try {
            sendProgress("Uploading scripts via backend...");
            const r = await (0, backendHttp_1.backendPost)("/ftp/upload-scripts", {
                ip: xboxIp,
                scripts_dir: scriptsDir,
                remote_path: remotePath,
                server_ip: pcIp,
                server_port: serverPort,
            });
            if (r.ok) {
                sendProgress("Upload complete.");
                (0, serverLog_1.appendAppEvent)("FTP", `upload complete host=${xboxIp} path=${remotePath}`);
            }
            return { ok: r.ok, remotePath, error: r.ok ? undefined : (r.message || "Upload failed") };
        }
        catch (err) {
            (0, serverLog_1.appendAppEvent)("FTP", `error: ${err.message || String(err)}`);
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── List Xbox drives (proxied to Go backend) ──────────────────────────────
    ipcMain.handle("xbox:list-drives", async () => {
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        try {
            const raw = await (0, backendHttp_1.backendGet)(`/ftp/drives?ip=${encodeURIComponent(xboxIp)}`);
            return JSON.parse(raw);
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── List games (via Go backend /ftp/batch for multi-directory scan) ────────
    ipcMain.handle("xbox:list-games", async () => {
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured. Set it in Settings." };
        try {
            const nameMap = (0, auroraLibraryService_1.xboxBuildGameNameMap)();
            const mediaDir = (0, auroraPathHelper_1.xboxAuroraMediaDir)((0, settingsService_1.getConfiguredFtpScriptsPath)());
            const games = new Map();
            function addGame(titleId, fallbackName, location) {
                const id = titleId.toUpperCase();
                if (!games.has(id)) {
                    games.set(id, {
                        titleId: id,
                        name: nameMap.get(id) || fallbackName || id,
                        location,
                        coverFtpPath: `${mediaDir}/${id}GC.jpg`,
                    });
                }
            }
            // Step 1: List /Hdd1/Content profiles
            const batchOps = [
                { op: "list", path: "/Hdd1/Content" },
                { op: "list", path: "/Hdd1/Games" },
            ];
            const batchRes = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops: batchOps });
            const results = batchRes.results || [];
            // Process /Hdd1/Content/<profile>/<TitleID>
            if (results[0] && results[0].ok && Array.isArray(results[0].data)) {
                const profileDirs = results[0].data.filter((e) => e.type === "dir");
                // Build batch ops for all profile directories
                const profileOps = profileDirs.map((d) => ({ op: "list", path: `/Hdd1/Content/${d.name}` }));
                if (profileOps.length > 0) {
                    const profileRes = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops: profileOps });
                    const profResults = profileRes.results || [];
                    for (let pi = 0; pi < profileDirs.length; pi++) {
                        if (profResults[pi] && profResults[pi].ok && Array.isArray(profResults[pi].data)) {
                            for (const titleDir of profResults[pi].data) {
                                if (titleDir.type !== "dir")
                                    continue;
                                const id = titleDir.name.toUpperCase();
                                if (!/^[0-9A-F]{8}$/.test(id))
                                    continue;
                                addGame(id, null, `/Hdd1/Content/${profileDirs[pi].name}/${titleDir.name}`);
                            }
                        }
                    }
                }
            }
            // Process /Hdd1/Games/<GameName>/Content/<TitleID>
            if (results[1] && results[1].ok && Array.isArray(results[1].data)) {
                const gameDirs = results[1].data.filter((e) => e.type === "dir");
                const gameOps = gameDirs.map((d) => ({ op: "list", path: `/Hdd1/Games/${d.name}/Content` }));
                if (gameOps.length > 0) {
                    const gameRes = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops: gameOps });
                    const gameResults = gameRes.results || [];
                    for (let gi = 0; gi < gameDirs.length; gi++) {
                        if (gameResults[gi] && gameResults[gi].ok && Array.isArray(gameResults[gi].data)) {
                            for (const entry of gameResults[gi].data) {
                                if (entry.type !== "dir")
                                    continue;
                                const id = entry.name.toUpperCase();
                                if (!/^[0-9A-F]{8}$/.test(id))
                                    continue;
                                addGame(id, gameDirs[gi].name, `/Hdd1/Games/${gameDirs[gi].name}`);
                            }
                        }
                        else {
                            // Content dir unreadable — add with fallback name
                            addGame(gameDirs[gi].name.toUpperCase().padEnd(8, "0").slice(0, 8), gameDirs[gi].name, `/Hdd1/Games/${gameDirs[gi].name}`);
                        }
                    }
                }
            }
            const gameList = Array.from(games.values()).sort((a, b) => a.name.localeCompare(b.name));
            return { ok: true, games: gameList, connectedTo: xboxIp };
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── Fetch covers (via Go backend /ftp/batch download_base64) ──────────────
    ipcMain.handle("xbox:fetch-covers", async (_event, coverRequests) => {
        if (!Array.isArray(coverRequests) || coverRequests.length === 0)
            return { ok: true };
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        try {
            // Build batch ops to download each cover as base64
            const ops = coverRequests.map((cr) => ({
                op: "download_base64",
                path: cr.ftpPath,
            }));
            const batchRes = await (0, backendHttp_1.backendPost)("/ftp/batch", { ip: xboxIp, ops });
            const results = batchRes.results || [];
            for (let i = 0; i < coverRequests.length; i++) {
                const { titleId } = coverRequests[i];
                let dataUrl = null;
                if (results[i] && results[i].ok && results[i].data) {
                    const b64 = results[i].data;
                    // Detect MIME from first bytes of base64
                    const raw = Buffer.from(b64.slice(0, 8), "base64");
                    const mime = (raw[0] === 0xFF && raw[1] === 0xD8) ? "image/jpeg"
                        : (raw[0] === 0x89 && raw[1] === 0x50) ? "image/png"
                            : "image/jpeg";
                    dataUrl = `data:${mime};base64,${b64}`;
                }
                const win = (0, window_1.getMainWindow)();
                if (win && !win.isDestroyed())
                    win.webContents.send("xbox-cover", { titleId, dataUrl });
            }
            return { ok: true };
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
}
