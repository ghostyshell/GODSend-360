"use strict";
/**
 * IPC handlers for the Toolbox and FTP Manager:
 *   tools:choose-iso-files, tools:choose-output-folder
 *   tools:probe-iso, tools:iso2god, tools:iso2xex
 *   tools:ftp-list, tools:ftp-choose-files, tools:ftp-choose-folder
 *   tools:ftp-upload, tools:ftp-upload-status, tools:ftp-upload-remove
 *   tools:ftp-delete, tools:ftp-mkdir, tools:ftp-rename, tools:ftp-copy
 *   xbox:move-game
 *
 * All FTP operations are proxied through the Go backend for centralised tracking.
 */
Object.defineProperty(exports, "__esModule", { value: true });
exports.register = register;
const electron_1 = require("electron");
const settingsService_1 = require("../services/settingsService");
const backendClient_1 = require("../services/backendClient");
const backendHttp_1 = require("../infrastructure/backendHttp");
const window_1 = require("../app/window");
const autoSyncService_1 = require("../services/autoSyncService");
function register(ipcMain) {
    // ── File pickers ────────────────────────────────────────────────────────────
    ipcMain.handle("tools:choose-iso-files", async () => {
        const win = electron_1.BrowserWindow.getFocusedWindow() || (0, window_1.getMainWindow)();
        const result = await electron_1.dialog.showOpenDialog(win || undefined, {
            title: "Select Xbox 360 ISO files",
            filters: [{ name: "ISO images", extensions: ["iso"] }],
            properties: ["openFile", "multiSelections"],
        });
        if (result.canceled || !result.filePaths.length)
            return { ok: false };
        return { ok: true, files: result.filePaths };
    });
    ipcMain.handle("tools:choose-output-folder", async () => {
        const win = electron_1.BrowserWindow.getFocusedWindow() || (0, window_1.getMainWindow)();
        const result = await electron_1.dialog.showOpenDialog(win || undefined, {
            title: "Select output folder",
            properties: ["openDirectory", "createDirectory"],
        });
        if (result.canceled || !result.filePaths.length)
            return { ok: false };
        return { ok: true, folder: result.filePaths[0] };
    });
    // ── ISO tools (proxied to Go backend) ──────────────────────────────────────
    ipcMain.handle("tools:probe-iso", async (_event, isoPath) => {
        try {
            const r = await (0, backendHttp_1.backendPost)("/tools/probe-iso", { isoPath });
            if (r.error && !r.titleId)
                return { ok: false, error: r.error };
            return { ok: true, ...r };
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    ipcMain.handle("tools:iso2god", async (_event, { isoPath, outDir }) => {
        try {
            const r = await (0, backendHttp_1.backendPost)("/tools/iso2god", { isoPath, outDir });
            if (r.error && !r.ok)
                return { ok: false, error: r.error };
            return { ok: true, ...r };
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    ipcMain.handle("tools:iso2xex", async (_event, { isoPath, outDir }) => {
        try {
            const r = await (0, backendHttp_1.backendPost)("/tools/iso2xex", { isoPath, outDir });
            if (r.error && !r.ok)
                return { ok: false, error: r.error };
            return { ok: true, ...r };
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── FTP Manager: list directory (proxied to Go backend) ────────────────────
    ipcMain.handle("tools:ftp-list", async (_event, remotePath) => {
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        try {
            const r = await (0, backendHttp_1.backendPost)("/ftp/list", { ip: xboxIp, path: remotePath || "/" });
            return r;
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── FTP Manager: local file / folder pickers ────────────────────────────────
    ipcMain.handle("tools:ftp-choose-files", async () => {
        const win = electron_1.BrowserWindow.getFocusedWindow() || (0, window_1.getMainWindow)();
        const result = await electron_1.dialog.showOpenDialog(win || undefined, {
            title: "Select files to upload to Xbox",
            properties: ["openFile", "multiSelections"],
        });
        if (result.canceled || !result.filePaths.length)
            return { ok: false };
        return { ok: true, files: result.filePaths };
    });
    ipcMain.handle("tools:ftp-choose-folder", async () => {
        const win = electron_1.BrowserWindow.getFocusedWindow() || (0, window_1.getMainWindow)();
        const result = await electron_1.dialog.showOpenDialog(win || undefined, {
            title: "Select folder to upload to Xbox",
            properties: ["openDirectory"],
        });
        if (result.canceled || !result.filePaths.length)
            return { ok: false };
        return { ok: true, folder: result.filePaths[0] };
    });
    // ── FTP Manager: upload files (proxied to Go backend — async tracked) ──────
    ipcMain.handle("tools:ftp-upload", async (_event, { localPaths, remotePath }) => {
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        try {
            const r = await (0, backendHttp_1.backendPost)("/ftp/upload", { ip: xboxIp, local_paths: localPaths, remote_path: remotePath });
            return r;
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── FTP Manager: queue status (from Go backend) ────────────────────────────
    ipcMain.handle("tools:ftp-upload-status", async () => {
        try {
            const raw = await (0, backendHttp_1.backendGet)("/ftp/jobs");
            const r = JSON.parse(raw);
            return r;
        }
        catch (err) {
            return { ok: false, jobs: [], error: err.message || String(err) };
        }
    });
    // ── FTP Manager: remove completed/failed job ──────────────────────────────
    ipcMain.handle("tools:ftp-upload-remove", async (_event, id) => {
        try {
            const raw = await (0, backendHttp_1.backendGet)(`/ftp/jobs/remove?id=${id}`);
            return JSON.parse(raw);
        }
        catch {
            return { ok: true };
        }
    });
    // ── FTP Manager: delete remote file/folder (proxied to Go backend) ─────────
    ipcMain.handle("tools:ftp-delete", async (_event, remotePath) => {
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        try {
            return await (0, backendHttp_1.backendPost)("/ftp/delete", { ip: xboxIp, path: remotePath });
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── FTP Manager: create remote directory (proxied to Go backend) ───────────
    ipcMain.handle("tools:ftp-mkdir", async (_event, remotePath) => {
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        try {
            return await (0, backendHttp_1.backendPost)("/ftp/mkdir", { ip: xboxIp, path: remotePath });
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── FTP Manager: rename / move (proxied to Go backend) ─────────────────────
    ipcMain.handle("tools:ftp-rename", async (_event, { from, to }) => {
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        try {
            return await (0, backendHttp_1.backendPost)("/ftp/rename", { ip: xboxIp, from, to });
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── FTP Manager: copy (proxied to Go backend — async tracked) ──────────────
    ipcMain.handle("tools:ftp-copy", async (_event, { src, dst, isDir }) => {
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        try {
            return await (0, backendHttp_1.backendPost)("/ftp/copy", { ip: xboxIp, src, dst, is_dir: isDir });
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
    // ── Move a game to a different Xbox drive (proxied to Go backend) ──────────
    ipcMain.handle("xbox:move-game", async (_event, { game, targetDrive }) => {
        const xboxIp = (0, settingsService_1.getConfiguredXboxIP)();
        if (!xboxIp)
            return { ok: false, error: "No Xbox IP configured." };
        if (!game || !targetDrive)
            return { ok: false, error: "Missing game or target drive." };
        const gameName = game.name || game.titleId || "Unknown";
        const srcDrive = game.sourceDrive;
        const gameDir = game.directory;
        if (!srcDrive || !gameDir)
            return { ok: false, error: "Game has no source drive or directory info." };
        if (srcDrive + ":" === targetDrive || srcDrive === targetDrive) {
            return { ok: false, error: "Source and destination drive are the same." };
        }
        try {
            const r = await (0, backendHttp_1.backendPost)("/ftp/move-game", {
                ip: xboxIp,
                game_name: gameName,
                src_drive: srcDrive,
                directory: gameDir,
                target_drive: targetDrive,
            });
            if (!r.ok)
                return r;
            (0, backendClient_1.addOutputLine)(`[INFO] Move ${gameName}: queued via backend (job ${r.id})`);
            // Trigger Aurora library sync once the move completes (poll briefly)
            (async () => {
                const jobId = r.id;
                for (let i = 0; i < 600; i++) { // poll up to ~10 minutes
                    await new Promise((res) => setTimeout(res, 1000));
                    try {
                        const raw = await (0, backendHttp_1.backendGet)("/ftp/jobs");
                        const data = JSON.parse(raw);
                        const job = (data.jobs || []).find((j) => j.id === jobId);
                        if (!job || job.state === "Ready") {
                            (0, backendClient_1.addOutputLine)(`[INFO] Move ${gameName}: complete.`);
                            (0, autoSyncService_1.doAuroraLibrarySync)().catch((e) => (0, backendClient_1.addOutputLine)(`[WARN] Auto-sync after move failed: ${e.message || e}`));
                            return;
                        }
                        if (job.state === "Error") {
                            (0, backendClient_1.addOutputLine)(`[ERROR] Move ${gameName}: ${job.error}`);
                            return;
                        }
                    }
                    catch { /* backend may be temporarily unreachable */ }
                }
            })();
            return { ok: true, jobId: r.id, message: r.message };
        }
        catch (err) {
            return { ok: false, error: err.message || String(err) };
        }
    });
}
