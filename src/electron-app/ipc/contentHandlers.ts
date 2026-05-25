/**
 * IPC handlers for DLC / Title Update management:
 *   content:discover, content:installed, content:queue, content:sources
 */

import http from "http";
import { IpcMain } from "electron";

import { getConfiguredXboxIP, getConfiguredServerPort, getConfiguredDefaultXboxDrive } from "../services/settingsService";
import { backendGet, backendPost } from "../infrastructure/backendHttp";

export function register(ipcMain: IpcMain): void {

  // ── Discover DLC for a TitleID (Minerva + IA only, fast) ───────────────────
  ipcMain.handle("content:discover", async (_event, { titleId, gameName, drive: reqDrive }) => {
    try {
      const xboxIp = getConfiguredXboxIP();
      const drive = reqDrive || getConfiguredDefaultXboxDrive() || "Hdd1:";
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

  // ── Discover Title Updates for a TitleID (XboxUnity, separate endpoint) ───
  ipcMain.handle("content:title-updates", async (_event, { titleId }) => {
    try {
      const data = await backendGet(`/content/tu?title_id=${encodeURIComponent(titleId)}`);
      return { ok: true, ...JSON.parse(data) };
    } catch (err: any) {
      return { ok: false, error: err.message };
    }
  });

  // ── Scan Xbox Content directory for installed items ───────────────────
  ipcMain.handle("content:installed", async (_event, { titleId, drive: reqDrive }) => {
    try {
      const xboxIp = getConfiguredXboxIP();
      if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };
      const drive = reqDrive || getConfiguredDefaultXboxDrive() || "Hdd1:";
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
      const enriched = {
        xbox_ip: getConfiguredXboxIP(),
        drive: getConfiguredDefaultXboxDrive() || "Hdd1:",
        ...payload,
      };
      const body = JSON.stringify(enriched);
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
              const ok = (res.statusCode ?? 0) >= 200 && (res.statusCode ?? 0) < 300;
              try {
                const parsed = JSON.parse(data);
                const error = ok ? undefined : (parsed?.message || parsed?.error || `HTTP ${res.statusCode}`);
                resolve({ ok, ...parsed, ...(error ? { error } : {}) });
              } catch {
                resolve({ ok, data, ...(ok ? {} : { error: `HTTP ${res.statusCode}` }) });
              }
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

  // ── Activate / deactivate a Title Update file on the Xbox ────────────
  ipcMain.handle("content:set-active", async (_event, { titleId, contentType, fileName, drive, setActive }) => {
    try {
      const xboxIp = getConfiguredXboxIP();
      if (!xboxIp) return { ok: false, error: "No Xbox IP configured." };
      const resolvedDrive = drive || getConfiguredDefaultXboxDrive() || "Hdd1:";
      const body = JSON.stringify({
        title_id: titleId,
        content_type: contentType,
        file_name: fileName,
        drive: resolvedDrive,
        xbox_ip: xboxIp,
        set_active: !!setActive,
      });
      const port = getConfiguredServerPort();
      return await new Promise((resolve) => {
        const req = http.request(
          { hostname: "localhost", port, path: "/content/set-active", method: "POST",
            headers: { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(body) } },
          (res) => {
            let data = "";
            res.on("data", (chunk) => { data += chunk; });
            res.on("end", () => {
              const ok = (res.statusCode ?? 0) >= 200 && (res.statusCode ?? 0) < 300;
              try {
                const parsed = JSON.parse(data);
                resolve({ ok, ...parsed, ...(ok ? {} : { error: parsed?.message || parsed?.error || `HTTP ${res.statusCode}` }) });
              } catch {
                resolve({ ok, ...(ok ? {} : { error: `HTTP ${res.statusCode}` }) });
              }
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
