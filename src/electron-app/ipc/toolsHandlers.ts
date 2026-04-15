/**
 * IPC handlers for the Toolbox and FTP Manager:
 *   tools:choose-iso-files, tools:choose-output-folder
 *   tools:probe-iso, tools:iso2god, tools:iso2xex
 *   tools:ftp-list, tools:ftp-choose-files, tools:ftp-choose-folder
 *   tools:ftp-upload, tools:ftp-upload-status, tools:ftp-upload-remove
 *   tools:ftp-delete, tools:ftp-mkdir, tools:ftp-rename, tools:ftp-copy
 *   xbox:move-game
 */

import os from "os";
import path from "path";
import fs from "fs";
import { dialog, BrowserWindow, IpcMain } from "electron";
import * as ftp from "basic-ftp";

import {
  getConfiguredXboxIP,
  getConfiguredFtpUser,
  getConfiguredFtpPassword,
} from "../services/settingsService";
import { addOutputLine } from "../services/backendClient";
import { backendPost } from "../infrastructure/backendHttp";
import { getMainWindow } from "../app/window";
import { doAuroraLibrarySync } from "../services/autoSyncService";

// ── FTP upload queue state ─────────────────────────────────────────────────────
let _ftpUploadId = 0;
interface FtpUploadJob {
  id:         number;
  name:       string;
  localPath:  string | null;
  remotePath: string;
  state:      string;
  progress:   number;
  error:      string | null;
}
const _ftpUploadJobs = new Map<number, FtpUploadJob>();

export function register(ipcMain: IpcMain): void {

  // ── File pickers ────────────────────────────────────────────────────────────
  ipcMain.handle("tools:choose-iso-files", async () => {
    const win    = BrowserWindow.getFocusedWindow() || getMainWindow();
    const result = await dialog.showOpenDialog(win || undefined, {
      title:   "Select Xbox 360 ISO files",
      filters: [{ name: "ISO images", extensions: ["iso"] }],
      properties: ["openFile", "multiSelections"],
    });
    if (result.canceled || !result.filePaths.length) return { ok: false };
    return { ok: true, files: result.filePaths };
  });

  ipcMain.handle("tools:choose-output-folder", async () => {
    const win    = BrowserWindow.getFocusedWindow() || getMainWindow();
    const result = await dialog.showOpenDialog(win || undefined, {
      title:      "Select output folder",
      properties: ["openDirectory", "createDirectory"],
    });
    if (result.canceled || !result.filePaths.length) return { ok: false };
    return { ok: true, folder: result.filePaths[0] };
  });

  // ── ISO tools (proxied to Go backend) ──────────────────────────────────────
  ipcMain.handle("tools:probe-iso", async (_event, isoPath: string) => {
    try {
      const r = await backendPost("/tools/probe-iso", { isoPath });
      if (r.error && !r.titleId) return { ok: false, error: r.error };
      return { ok: true, ...r };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  ipcMain.handle("tools:iso2god", async (_event, { isoPath, outDir }) => {
    try {
      const r = await backendPost("/tools/iso2god", { isoPath, outDir });
      if (r.error && !r.ok) return { ok: false, error: r.error };
      return { ok: true, ...r };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  ipcMain.handle("tools:iso2xex", async (_event, { isoPath, outDir }) => {
    try {
      const r = await backendPost("/tools/iso2xex", { isoPath, outDir });
      if (r.error && !r.ok) return { ok: false, error: r.error };
      return { ok: true, ...r };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  // ── FTP Manager: list directory ─────────────────────────────────────────────
  ipcMain.handle("tools:ftp-list", async (_event, remotePath: string) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 15000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      await client.cd(remotePath || "/");
      const list    = await client.list();
      const entries = list.map((e) => ({
        name: e.name,
        type: e.type === 2 ? "dir" : "file",
        size: e.size || 0,
      }));
      return { ok: true, entries, cwd: remotePath || "/" };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── FTP Manager: local file / folder pickers ────────────────────────────────
  ipcMain.handle("tools:ftp-choose-files", async () => {
    const win    = BrowserWindow.getFocusedWindow() || getMainWindow();
    const result = await dialog.showOpenDialog(win || undefined, {
      title:      "Select files to upload to Xbox",
      properties: ["openFile", "multiSelections"],
    });
    if (result.canceled || !result.filePaths.length) return { ok: false };
    return { ok: true, files: result.filePaths };
  });

  ipcMain.handle("tools:ftp-choose-folder", async () => {
    const win    = BrowserWindow.getFocusedWindow() || getMainWindow();
    const result = await dialog.showOpenDialog(win || undefined, {
      title:      "Select folder to upload to Xbox",
      properties: ["openDirectory"],
    });
    if (result.canceled || !result.filePaths.length) return { ok: false };
    return { ok: true, folder: result.filePaths[0] };
  });

  // ── FTP Manager: upload files (adds to queue, runs in background) ──────────
  ipcMain.handle("tools:ftp-upload", async (_event, { localPaths, remotePath }: { localPaths: string[]; remotePath: string }) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const jobs: { id: number; name: string }[] = [];
    for (const lp of localPaths) {
      const id   = ++_ftpUploadId;
      const name = path.basename(lp);
      const job: FtpUploadJob = {
        id, name, localPath: lp,
        remotePath: `${remotePath}/${name}`,
        state: "Queued", progress: 0, error: null,
      };
      _ftpUploadJobs.set(id, job);
      jobs.push({ id, name });

      (async () => {
        const client = new ftp.Client();
        client.ftp.verbose = false;
        (client.ftp as any).timeout = 30000;
        try {
          job.state = "Processing";
          await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

          const stat = fs.statSync(lp);
          if (stat.isDirectory()) {
            await client.uploadFromDir(lp, `${remotePath}/${name}`);
          } else {
            client.trackProgress((info) => {
              if (stat.size > 0) job.progress = Math.round((info.bytes / stat.size) * 100);
            });
            await client.uploadFrom(lp, `${remotePath}/${name}`);
          }
          job.state    = "Ready";
          job.progress = 100;
        } catch (err: any) {
          job.state = "Error";
          job.error = err.message || String(err);
        } finally {
          client.close();
        }
      })();
    }
    return { ok: true, jobs };
  });

  // ── FTP Manager: queue status / remove ─────────────────────────────────────
  ipcMain.handle("tools:ftp-upload-status", () => {
    const jobs = [];
    for (const [, job] of _ftpUploadJobs) {
      jobs.push({
        id: job.id, name: job.name, state: job.state,
        progress: job.progress, error: job.error, remotePath: job.remotePath,
      });
    }
    return { ok: true, jobs };
  });

  ipcMain.handle("tools:ftp-upload-remove", (_event, id: number) => {
    _ftpUploadJobs.delete(id);
    return { ok: true };
  });

  // ── FTP Manager: delete remote file/folder ──────────────────────────────────
  ipcMain.handle("tools:ftp-delete", async (_event, remotePath: string) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 15000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      try {
        await client.remove(remotePath);
      } catch {
        await client.removeDir(remotePath);
      }
      return { ok: true };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── FTP Manager: create remote directory ────────────────────────────────────
  ipcMain.handle("tools:ftp-mkdir", async (_event, remotePath: string) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 15000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      await client.ensureDir(remotePath);
      return { ok: true };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── FTP Manager: rename / move a remote file or directory ───────────────────
  ipcMain.handle("tools:ftp-rename", async (_event, { from, to }: { from: string; to: string }) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 30000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      await client.rename(from, to);
      return { ok: true };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── FTP Manager: copy (download + reupload — Xbox FTP has no server-side copy) ─
  ipcMain.handle("tools:ftp-copy", async (_event, { src, dst, isDir }: { src: string; dst: string; isDir: boolean }) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const tmpDir = path.join(os.tmpdir(), "godsend-ftp-copy-" + Date.now());
    fs.mkdirSync(tmpDir, { recursive: true });

    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 60000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

      if (isDir) {
        const localDir = path.join(tmpDir, path.basename(src));
        await client.downloadToDir(localDir, src);
        await client.uploadFromDir(localDir, dst);
      } else {
        const localFile = path.join(tmpDir, path.basename(src));
        await client.downloadTo(localFile, src);
        await client.uploadFrom(localFile, dst);
      }
      return { ok: true };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
      fs.rm(tmpDir, { recursive: true, force: true }, () => {});
    }
  });

  // ── Move a game to a different Xbox drive ────────────────────────────────────
  ipcMain.handle("xbox:move-game", async (_event, { game, targetDrive }: { game: any; targetDrive: string }) => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };
    if (!game || !targetDrive) return { ok: false, error: "Missing game or target drive." };

    const gameName = game.name || game.titleId || "Unknown";
    const srcDrive = game.sourceDrive;
    const gameDir  = game.directory;
    if (!srcDrive || !gameDir) return { ok: false, error: "Game has no source drive or directory info." };
    if (srcDrive + ":" === targetDrive || srcDrive === targetDrive) {
      return { ok: false, error: "Source and destination drive are the same." };
    }

    const dirNorm       = gameDir.replace(/\\/g, "/");
    const srcPath       = "/" + srcDrive + "/" + dirNorm;
    const dstDriveClean = targetDrive.replace(/:$/, "");
    const dstPath       = "/" + dstDriveClean + "/" + dirNorm;

    const id  = ++_ftpUploadId;
    const job: FtpUploadJob = {
      id,
      name:       `Move: ${gameName} → ${dstDriveClean}`,
      localPath:  null,
      remotePath: dstPath,
      state:      "Queued",
      progress:   0,
      error:      null,
    };
    _ftpUploadJobs.set(id, job);

    (async () => {
      const moveClient = new ftp.Client();
      moveClient.ftp.verbose = false;
      (moveClient.ftp as any).timeout = 120000;
      try {
        job.state = "Processing";
        addOutputLine(`[INFO] Move ${gameName}: connecting to ${xboxIp}…`);
        await moveClient.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

        const dstParent = dstPath.split("/").slice(0, -1).join("/") || "/";
        await moveClient.ensureDir(dstParent);
        await moveClient.cd("/");

        try {
          addOutputLine(`[INFO] Move ${gameName}: attempting FTP rename ${srcPath} → ${dstPath}…`);
          await moveClient.rename(srcPath, dstPath);
          job.state    = "Ready";
          job.progress = 100;
          addOutputLine(`[INFO] Move ${gameName}: rename succeeded — done.`);
          doAuroraLibrarySync().catch((e: any) =>
            addOutputLine(`[WARN] Auto-sync after move failed: ${e.message || e}`)
          );
          return;
        } catch {
          addOutputLine(`[INFO] Move ${gameName}: rename not supported cross-drive, falling back to download + reupload.`);
        }

        const tmpDir   = path.join(os.tmpdir(), "godsend-move-" + Date.now());
        fs.mkdirSync(tmpDir, { recursive: true });

        job.progress = 10;
        addOutputLine(`[INFO] Move ${gameName}: downloading from ${srcPath} to temp…`);
        const localDir = path.join(tmpDir, path.basename(srcPath));
        await moveClient.downloadToDir(localDir, srcPath);

        job.progress = 50;
        addOutputLine(`[INFO] Move ${gameName}: uploading to ${dstPath}…`);
        await moveClient.uploadFromDir(localDir, dstPath);

        job.progress = 90;
        addOutputLine(`[INFO] Move ${gameName}: removing source ${srcPath}…`);
        await moveClient.removeDir(srcPath);

        job.state    = "Ready";
        job.progress = 100;
        addOutputLine(`[INFO] Move ${gameName}: complete.`);

        doAuroraLibrarySync().catch((e: any) =>
          addOutputLine(`[WARN] Auto-sync after move failed: ${e.message || e}`)
        );

        fs.rm(tmpDir, { recursive: true, force: true }, () => {});
      } catch (err: any) {
        job.state = "Error";
        job.error = err.message || String(err);
        addOutputLine(`[ERROR] Move ${gameName}: ${job.error}`);
      } finally {
        moveClient.close();
      }
    })();

    return { ok: true, jobId: id, message: `Queued move of ${gameName} to ${dstDriveClean}` };
  });
}
