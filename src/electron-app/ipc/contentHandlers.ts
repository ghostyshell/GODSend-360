/**
 * IPC handlers for DLC / Title Update management:
 *   content:discover, content:installed, content:queue, content:sources
 */

import http from "http";
import { IpcMain } from "electron";

import { getConfiguredXboxIP, getConfiguredServerPort, getConfiguredDefaultXboxDrive } from "../services/settingsService";
import { backendGet, backendPost } from "../infrastructure/backendHttp";

export function register(ipcMain: IpcMain): void {

  // ── Discover all DLC and TUs for a TitleID ────────────────────────────────
  ipcMain.handle("content:discover", async (_event, { titleId, gameName }) => {
    try {
      const xboxIp = getConfiguredXboxIP();
      const drive = getConfiguredDefaultXboxDrive() || "Hdd1:";
      const ipParam = xboxIp ? `&xbox_ip=${encodeURIComponent(xboxIp)}` : "";
      const drvParam = `&drive=${encodeURIComponent(drive)}`;
      const data = await backendGet(
        `/content/discover?title_id=${encodeURIComponent(titleId)}&game_name=${encodeURIComponent(gameName || "")}${ipParam}${drvParam}`
      );
      return { ok: true, ...JSON.parse(data) };
    } catch (err: any) {
      return { ok: false, error: err.message };
    }
  });

  // ── Scan Xbox Content directory for installed items ───────────────────
  ipcMain.handle("content:installed", async (_event, { titleId }) => {
    try {
      const xboxIp = getConfiguredXboxIP();
      if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };
      const drive = getConfiguredDefaultXboxDrive() || "Hdd1:";
      const data = await backendGet(
        `/content/installed?title_id=${encodeURIComponent(titleId)}&xbox_ip=${encodeURIComponent(xboxIp)}&drive=${encodeURIComponent(drive)}`
      );
      return { ok: true, ...JSON.parse(data) };
    } catch (err: any) {
      return { ok: false, error: err.message };
    }
  });

  // ── Queue a specific DLC or TU for download + FTP ───────────────────────
  ipcMain.handle("content:queue", async (_event, payload) => {
    try {
      const port = getConfiguredServerPort();
      const body = JSON.stringify(payload);
      return new Promise((resolve) => {
        const req = http.request(
          {
            hostname: "localhost",
            port,
            path: "/content/queue",
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "Content-Length": Buffer.byteLength(body),
            },
          },
          (res) => {
            let data = "";
            res.on("data", (chunk) => { data += chunk; });
            res.on("end", () => {
              try { resolve({ ok: true, ...JSON.parse(data) }); }
              catch { resolve({ ok: true, data }); }
            });
          }
        );
        req.on("error", (err: Error) => resolve({ ok: false, error: err.message }));
        req.write(body);
        req.end();
      });
    } catch (err: any) {
      return { ok: false, error: err.message };
    }
  });

  // ── Get available download sources for a content item ───────────────────
  ipcMain.handle("content:sources", async (_event, { titleId, gameName }) => {
    try {
      const data = await backendGet(
        `/content/sources?title_id=${encodeURIComponent(titleId)}&game_name=${encodeURIComponent(gameName || "")}`
      );
      return { ok: true, ...JSON.parse(data) };
    } catch (err: any) {
      return { ok: false, error: err.message };
    }
  });
}
