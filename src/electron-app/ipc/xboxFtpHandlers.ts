/**
 * IPC handlers for direct Xbox FTP operations:
 *   xbox:ping, xbox:ftp-test, xbox:ftp-scan, xbox:ftp-scripts
 *   xbox:list-drives, xbox:list-games, xbox:fetch-covers
 */

import os from "os";
import path from "path";
import fs from "fs";
import net from "net";
import * as ftp from "basic-ftp";
import { Writable } from "stream";
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
import { xboxAuroraRoot, xboxAuroraMediaDir } from "../services/auroraPathHelper";
import { xboxBuildGameNameMap } from "../services/auroraLibraryService";

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

  // ── Ping (lightweight connectivity check) ──────────────────────────────────
  ipcMain.handle("xbox:ping", async () => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 5000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      return { ok: true };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── FTP Test (verbose connection diagnostics) ──────────────────────────────
  ipcMain.handle("xbox:ftp-test", async (_event, payload) => {
    const p       = payload || {};
    const xboxIp  = (typeof p.xboxIp  === "string" ? p.xboxIp.trim()  : "") || getConfiguredXboxIP();
    const ftpUser = (typeof p.ftpUser  === "string" ? p.ftpUser.trim() : "") || getConfiguredFtpUser();
    const ftpPass = (typeof p.ftpPassword === "string" ? p.ftpPassword  : "") || getConfiguredFtpPassword();

    const sendDebug = (line: string) => {
      const win = getMainWindow();
      if (win && !win.isDestroyed()) win.webContents.send("godsend-ftp-debug", line);
    };

    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = true;
    (client.ftp as any).timeout = 15000;
    client.ftp.log     = (msg: string) => sendDebug(msg);

    try {
      sendDebug(`[TEST] Connecting to ${xboxIp}:21 as ${ftpUser || "(anonymous)"}...`);
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      sendDebug(`[TEST] Login successful.`);

      sendDebug(`[TEST] Sending PWD...`);
      const pwd = await client.pwd();
      sendDebug(`[TEST] Working directory: ${pwd}`);

      sendDebug(`[TEST] Listing root directory...`);
      const list = await client.list("/");
      for (const item of list) {
        sendDebug(`  ${item.type === 2 ? "DIR " : "FILE"} ${item.name}  (${item.size || 0} bytes)`);
      }

      sendDebug(`[TEST] Connection test PASSED.`);
      return { ok: true };
    } catch (err: any) {
      sendDebug(`[TEST] FAILED: ${err.message || String(err)}`);
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── FTP Port Scanner ────────────────────────────────────────────────────────
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

  // ── Upload Aurora scripts ───────────────────────────────────────────────────
  ipcMain.handle("xbox:ftp-scripts", async (_event, payload) => {
    const p           = payload || {};
    const xboxIp      = (typeof p.xboxIp        === "string" ? p.xboxIp.trim()        : "") || getConfiguredXboxIP();
    const ftpUser     = (typeof p.ftpUser        === "string" ? p.ftpUser.trim()       : "") || getConfiguredFtpUser();
    const ftpPass     = (typeof p.ftpPassword    === "string" ? p.ftpPassword          : "") || getConfiguredFtpPassword();
    const remotePath  = (typeof p.ftpScriptsPath === "string" && p.ftpScriptsPath.trim())
      ? p.ftpScriptsPath.trim()
      : getConfiguredFtpScriptsPath();

    const sendProgress = (msg: string) => {
      appendAppEvent("FTP", msg);
      const win = getMainWindow();
      if (win && !win.isDestroyed()) win.webContents.send("godsend-ftp-progress", msg);
    };

    let stateTempPath: string | null = null;
    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 20000;
    try {
      if (!xboxIp) return { ok: false, error: "Xbox IP address is required." };

      const scriptsDir = getAuroraScriptsPath();
      if (!fs.existsSync(scriptsDir)) {
        return { ok: false, error: `Aurora scripts folder not found at: ${scriptsDir}` };
      }

      const pcIp       = getLocalIPAddress();
      const serverPort = getConfiguredServerPort();
      if (!pcIp) {
        return { ok: false, error: "Could not detect this PC's local IPv4 address for state.lua patching." };
      }

      const stateSrc = path.join(scriptsDir, "state.lua");
      if (fs.existsSync(stateSrc)) {
        let patchedState = fs.readFileSync(stateSrc, "utf8");
        patchedState = patchedState.replace(
          /^(BRAIN_IP\s*=\s*)["'][^"']*["']\s*$/m,
          `$1"${pcIp}"`
        );
        patchedState = patchedState.replace(
          /^(PORT\s*=\s*)["'][^"']*["']\s*$/m,
          `$1"${serverPort}"`
        );
        stateTempPath = path.join(os.tmpdir(), "state.lua.upload-tmp");
        fs.writeFileSync(stateTempPath, patchedState, "utf8");
      }

      sendProgress("Connecting to " + xboxIp + "...");
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      sendProgress("Connected. Preparing destination folder...");
      await client.ensureDir(remotePath);

      const entries = fs.readdirSync(scriptsDir, { withFileTypes: true });
      let done = 0;
      for (const entry of entries) {
        sendProgress(`Uploading ${entry.name} (${done}/${entries.length})...`);
        if (entry.isDirectory()) {
          await client.uploadFromDir(
            path.join(scriptsDir, entry.name),
            `${remotePath}/${entry.name}`
          );
        } else {
          const localFile = (stateTempPath && entry.name === "state.lua")
            ? stateTempPath
            : path.join(scriptsDir, entry.name);
          await client.uploadFrom(localFile, `${remotePath}/${entry.name}`);
        }
        done++;
      }
      appendAppEvent("FTP", `upload complete host=${xboxIp} path=${remotePath}`);
      return { ok: true, remotePath };
    } catch (err: any) {
      appendAppEvent("FTP", `error: ${err.message || String(err)}`);
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
      if (stateTempPath) try { fs.unlinkSync(stateTempPath); } catch { /* ignore */ }
    }
  });

  // ── List Xbox drives ────────────────────────────────────────────────────────
  ipcMain.handle("xbox:list-drives", async () => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 10000;
    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });
      await client.cd("/");
      const entries = await client.list();
      const drives  = entries
        .filter((e) => e.type === 2)
        .map((e) => e.name + ":")
        .filter((d) => /^(Hdd\d*|Usb\d+):$/.test(d));
      return { ok: true, drives };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── List games (simple FTP scan of game folders) ───────────────────────────
  ipcMain.handle("xbox:list-games", async () => {
    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured. Set it in Settings." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 20000;

    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

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

      // 1. Scan /Hdd1/Content/<profile>/<TitleID>
      try {
        const profileDirs = await client.list("/Hdd1/Content");
        for (const profileDir of profileDirs) {
          if (profileDir.type !== 2) continue;
          try {
            const titleDirs = await client.list(`/Hdd1/Content/${profileDir.name}`);
            for (const titleDir of titleDirs) {
              if (titleDir.type !== 2) continue;
              const id = titleDir.name.toUpperCase();
              if (!/^[0-9A-F]{8}$/.test(id)) continue;
              addGame(id, null, `/Hdd1/Content/${profileDir.name}/${titleDir.name}`);
            }
          } catch { /* unreadable profile dir — skip */ }
        }
      } catch { /* /Hdd1/Content not present */ }

      // 2. Scan /Hdd1/Games/<GameName>/Content/<TitleID>
      try {
        const gameDirs = await client.list("/Hdd1/Games");
        for (const gameDir of gameDirs) {
          if (gameDir.type !== 2) continue;
          try {
            const contentEntries = await client.list(`/Hdd1/Games/${gameDir.name}/Content`);
            for (const entry of contentEntries) {
              if (entry.type !== 2) continue;
              const id = entry.name.toUpperCase();
              if (!/^[0-9A-F]{8}$/.test(id)) continue;
              addGame(id, gameDir.name, `/Hdd1/Games/${gameDir.name}`);
            }
          } catch {
            addGame(
              gameDir.name.toUpperCase().padEnd(8, "0").slice(0, 8),
              gameDir.name,
              `/Hdd1/Games/${gameDir.name}`
            );
          }
        }
      } catch { /* /Hdd1/Games not present */ }

      const gameList = Array.from(games.values()).sort((a, b) => a.name.localeCompare(b.name));
      return { ok: true, games: gameList, connectedTo: xboxIp };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });

  // ── Fetch covers (streams back one-by-one over a single FTP session) ───────
  ipcMain.handle("xbox:fetch-covers", async (_event, coverRequests) => {
    if (!Array.isArray(coverRequests) || coverRequests.length === 0) return { ok: true };

    const xboxIp  = getConfiguredXboxIP();
    const ftpUser = getConfiguredFtpUser();
    const ftpPass = getConfiguredFtpPassword();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };

    const client = new ftp.Client();
    client.ftp.verbose = false;
    (client.ftp as any).timeout = 10000;

    try {
      await client.access({ host: xboxIp, port: 21, user: ftpUser, password: ftpPass, secure: false });

      for (const { titleId, ftpPath } of coverRequests) {
        let dataUrl: string | null = null;
        try {
          const chunks: Buffer[] = [];
          const writable = new Writable({
            write(chunk: Buffer, _enc: BufferEncoding, cb: () => void) { chunks.push(chunk); cb(); },
          });
          await client.downloadTo(writable, ftpPath);
          const buf  = Buffer.concat(chunks);
          const mime = (buf[0] === 0xFF && buf[1] === 0xD8) ? "image/jpeg"
                     : (buf[0] === 0x89 && buf[1] === 0x50) ? "image/png"
                     : "image/jpeg";
          dataUrl = `data:${mime};base64,${buf.toString("base64")}`;
        } catch { /* cover file absent — leave null */ }

        const win = getMainWindow();
        if (win && !win.isDestroyed()) win.webContents.send("xbox-cover", { titleId, dataUrl });
      }
      return { ok: true };
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    } finally {
      client.close();
    }
  });
}
