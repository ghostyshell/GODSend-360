/**
 * IPC handlers for direct Xbox FTP operations:
 *   xbox:ping, xbox:ftp-test, xbox:ftp-scan, xbox:ftp-scripts
 *   xbox:list-drives, xbox:list-games, xbox:fetch-covers
 *
 * All FTP operations are proxied through the Go backend for centralised tracking.
 */

import os from "os";
import net from "net";
import { IpcMain } from "electron";

import {
  getConfiguredXboxIP,
  getConfiguredFtpUser,
  getConfiguredFtpPassword,
  getConfiguredFtpScriptsPath,
  getConfiguredServerPort,
} from "../services/settingsService";
import { appendAppEvent } from "../infrastructure/serverLog";
import { getAuroraScriptsPath } from "../infrastructure/fileSystem";
import { getMainWindow } from "../app/window";
import { xboxAuroraMediaDir } from "../services/auroraPathHelper";
import { xboxBuildGameNameMap } from "../services/auroraLibraryService";
import { backendGet, backendPost } from "../infrastructure/backendHttp";

function getLocalIPAddress(): string | null {
  const ifaces = os.networkInterfaces();
  for (const name of Object.keys(ifaces)) {
    for (const iface of ifaces[name]!) {
      if (iface.family === "IPv4" && !iface.internal) return iface.address;
    }
  }
  return null;
}

export function register(ipcMain: IpcMain): void {

  // ── Ping (proxied to Go backend) ──────────────────────────────────────────
  ipcMain.handle("xbox:ping", async () => {
    const xboxIp = getConfiguredXboxIP();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    try {
      return await backendPost("/ftp/ping", { ip: xboxIp });
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  // ── FTP Test (verbose connection diagnostics — proxied to Go backend) ─────
  ipcMain.handle("xbox:ftp-test", async (_event, payload) => {
    const p       = payload || {};
    const xboxIp  = (typeof p.xboxIp  === "string" ? p.xboxIp.trim()  : "") || getConfiguredXboxIP();
    const ftpUser = (typeof p.ftpUser  === "string" ? p.ftpUser.trim() : "") || getConfiguredFtpUser();
    const ftpPass = (typeof p.ftpPassword === "string" ? p.ftpPassword  : "") || getConfiguredFtpPassword();

    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const sendDebug = (line: string) => {
      const win = getMainWindow();
      if (win && !win.isDestroyed()) win.webContents.send("godsend-ftp-debug", line);
    };

    try {
      const r = await backendPost("/ftp/test", { ip: xboxIp, user: ftpUser, password: ftpPass });
      // Replay log lines to the renderer
      if (Array.isArray(r.log)) {
        for (const line of r.log) sendDebug(line);
      }
      return { ok: r.ok || false, error: r.ok ? undefined : "FTP test failed" };
    } catch (err: any) {
      sendDebug(`[TEST] FAILED: ${err.message || String(err)}`);
      return { ok: false, error: err.message || String(err) };
    }
  });

  // ── FTP Port Scanner (raw TCP — stays Electron-side, no FTP library needed) ─
  ipcMain.handle("xbox:ftp-scan", async (_event, subnet: string) => {
    if (typeof subnet !== "string" || !/^\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(subnet.trim())) {
      return { ok: false, error: "Invalid subnet. Use format like 192.168.1" };
    }
    subnet = subnet.trim();

    const sendDebug = (line: string) => {
      const win = getMainWindow();
      if (win && !win.isDestroyed()) win.webContents.send("godsend-ftp-debug", line);
    };

    sendDebug(`[SCAN] Scanning ${subnet}.1 - ${subnet}.254 on port 21...`);

    const found   = [];
    const BATCH   = 25;
    const TIMEOUT = 2000;

    for (let batchStart = 1; batchStart <= 254; batchStart += BATCH) {
      const batchEnd = Math.min(batchStart + BATCH - 1, 254);
      sendDebug(`[SCAN] Probing ${subnet}.${batchStart} - ${subnet}.${batchEnd}...`);

      const promises: Promise<string | null>[] = [];
      for (let i = batchStart; i <= batchEnd; i++) {
        const ip = `${subnet}.${i}`;
        promises.push(new Promise((resolve) => {
          const sock = new net.Socket();
          sock.setTimeout(TIMEOUT);
          sock.once("connect", () => { sock.destroy(); resolve(ip); });
          sock.once("timeout", () => { sock.destroy(); resolve(null); });
          sock.once("error",   () => { sock.destroy(); resolve(null); });
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
    } else {
      sendDebug(`[SCAN] Done. Found ${found.length} host(s) with FTP: ${found.join(", ")}`);
    }
    return { ok: true, hosts: found };
  });

  // ── Upload Aurora scripts (proxied to Go backend — async tracked) ──────────
  ipcMain.handle("xbox:ftp-scripts", async (_event, payload) => {
    const p           = payload || {};
    const xboxIp      = (typeof p.xboxIp        === "string" ? p.xboxIp.trim()        : "") || getConfiguredXboxIP();
    const remotePath  = (typeof p.ftpScriptsPath === "string" && p.ftpScriptsPath.trim())
      ? p.ftpScriptsPath.trim()
      : getConfiguredFtpScriptsPath();

    const sendProgress = (msg: string) => {
      appendAppEvent("FTP", msg);
      const win = getMainWindow();
      if (win && !win.isDestroyed()) win.webContents.send("godsend-ftp-progress", msg);
    };

    if (!xboxIp) return { ok: false, error: "Xbox IP address is required." };

    const scriptsDir = getAuroraScriptsPath();
    const pcIp       = getLocalIPAddress();
    const serverPort = getConfiguredServerPort();
    if (!pcIp) {
      return { ok: false, error: "Could not detect this PC's local IPv4 address for state.lua patching." };
    }

    try {
      sendProgress("Uploading scripts via backend...");
      const r = await backendPost("/ftp/upload-scripts", {
        ip:          xboxIp,
        scripts_dir: scriptsDir,
        remote_path: remotePath,
        server_ip:   pcIp,
        server_port: String(serverPort),
      });
      if (r.ok) {
        sendProgress("Upload complete.");
        appendAppEvent("FTP", `upload complete host=${xboxIp} path=${remotePath}`);
      }
      return { ok: r.ok, remotePath, error: r.ok ? undefined : (r.message || "Upload failed") };
    } catch (err: any) {
      appendAppEvent("FTP", `error: ${err.message || String(err)}`);
      return { ok: false, error: err.message || String(err) };
    }
  });

  // ── List Xbox drives (proxied to Go backend) ──────────────────────────────
  ipcMain.handle("xbox:list-drives", async () => {
    const xboxIp = getConfiguredXboxIP();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    try {
      const raw = await backendGet(`/ftp/drives?ip=${encodeURIComponent(xboxIp)}`);
      return JSON.parse(raw);
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  // ── List games (via Go backend /ftp/batch for multi-directory scan) ────────
  ipcMain.handle("xbox:list-games", async () => {
    const xboxIp = getConfiguredXboxIP();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured. Set it in Settings." };

    try {
      const nameMap  = xboxBuildGameNameMap();
      const mediaDir = xboxAuroraMediaDir(getConfiguredFtpScriptsPath());
      const games    = new Map<string, any>();

      function addGame(titleId: string, fallbackName: string | null, location: string) {
        const id = titleId.toUpperCase();
        if (!games.has(id)) {
          games.set(id, {
            titleId:      id,
            name:         nameMap.get(id) || fallbackName || id,
            location,
            coverFtpPath: `${mediaDir}/${id}GC.jpg`,
          });
        }
      }

      // Step 1: List /Hdd1/Content profiles
      const batchOps: any[] = [
        { op: "list", path: "/Hdd1/Content" },
        { op: "list", path: "/Hdd1/Games" },
      ];
      const batchRes = await backendPost("/ftp/batch", { ip: xboxIp, ops: batchOps });
      const results  = batchRes.results || [];

      // Process /Hdd1/Content/<profile>/<TitleID>
      if (results[0] && results[0].ok && Array.isArray(results[0].data)) {
        const profileDirs = (results[0].data as any[]).filter((e: any) => e.type === "dir");
        // Build batch ops for all profile directories
        const profileOps = profileDirs.map((d: any) => ({ op: "list", path: `/Hdd1/Content/${d.name}` }));
        if (profileOps.length > 0) {
          const profileRes = await backendPost("/ftp/batch", { ip: xboxIp, ops: profileOps });
          const profResults = profileRes.results || [];
          for (let pi = 0; pi < profileDirs.length; pi++) {
            if (profResults[pi] && profResults[pi].ok && Array.isArray(profResults[pi].data)) {
              for (const titleDir of profResults[pi].data as any[]) {
                if (titleDir.type !== "dir") continue;
                const id = titleDir.name.toUpperCase();
                if (!/^[0-9A-F]{8}$/.test(id)) continue;
                addGame(id, null, `/Hdd1/Content/${profileDirs[pi].name}/${titleDir.name}`);
              }
            }
          }
        }
      }

      // Process /Hdd1/Games/<GameName>/Content/<TitleID>
      if (results[1] && results[1].ok && Array.isArray(results[1].data)) {
        const gameDirs = (results[1].data as any[]).filter((e: any) => e.type === "dir");
        const gameOps  = gameDirs.map((d: any) => ({ op: "list", path: `/Hdd1/Games/${d.name}/Content` }));
        if (gameOps.length > 0) {
          const gameRes     = await backendPost("/ftp/batch", { ip: xboxIp, ops: gameOps });
          const gameResults = gameRes.results || [];
          for (let gi = 0; gi < gameDirs.length; gi++) {
            if (gameResults[gi] && gameResults[gi].ok && Array.isArray(gameResults[gi].data)) {
              for (const entry of gameResults[gi].data as any[]) {
                if (entry.type !== "dir") continue;
                const id = entry.name.toUpperCase();
                if (!/^[0-9A-F]{8}$/.test(id)) continue;
                addGame(id, gameDirs[gi].name, `/Hdd1/Games/${gameDirs[gi].name}`);
              }
            } else {
              // Content dir unreadable — add with fallback name
              addGame(
                gameDirs[gi].name.toUpperCase().padEnd(8, "0").slice(0, 8),
                gameDirs[gi].name,
                `/Hdd1/Games/${gameDirs[gi].name}`
              );
            }
          }
        }
      }

      const gameList = Array.from(games.values()).sort((a: any, b: any) => a.name.localeCompare(b.name));
      return { ok: true, games: gameList, connectedTo: xboxIp };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  // ── Fetch covers (via Go backend /ftp/batch download_base64) ──────────────
  ipcMain.handle("xbox:fetch-covers", async (_event, coverRequests) => {
    if (!Array.isArray(coverRequests) || coverRequests.length === 0) return { ok: true };

    const xboxIp = getConfiguredXboxIP();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    try {
      // Build batch ops to download each cover as base64
      const ops = coverRequests.map((cr: any) => ({
        op:   "download_base64",
        path: cr.ftpPath,
      }));

      const batchRes = await backendPost("/ftp/batch", { ip: xboxIp, ops });
      const results  = batchRes.results || [];

      for (let i = 0; i < coverRequests.length; i++) {
        const { titleId } = coverRequests[i];
        let dataUrl: string | null = null;

        if (results[i] && results[i].ok && results[i].data) {
          const b64 = results[i].data as string;
          // Detect MIME from first bytes of base64
          const raw = Buffer.from(b64.slice(0, 8), "base64");
          const mime = (raw[0] === 0xFF && raw[1] === 0xD8) ? "image/jpeg"
                     : (raw[0] === 0x89 && raw[1] === 0x50) ? "image/png"
                     : "image/jpeg";
          dataUrl = `data:${mime};base64,${b64}`;
        }

        const win = getMainWindow();
        if (win && !win.isDestroyed()) win.webContents.send("xbox-cover", { titleId, dataUrl });
      }
      return { ok: true };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });
}
