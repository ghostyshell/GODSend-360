import { IpcMain } from "electron";
import { backendGet, backendPost } from "../infrastructure/backendHttp";
import { getConfiguredXboxIP } from "../services/settingsService";

function xboxIP(): string | null {
  const ip = getConfiguredXboxIP();
  if (!ip) return null;
  return ip;
}

export function register(ipcMain: IpcMain): void {
  function tryParseJSON(raw: string): any {
    try {
      return JSON.parse(raw);
    } catch {
      return { ok: false, error: `Backend returned invalid response: ${raw.substring(0, 200)}` };
    }
  }

  ipcMain.handle("saves:discover", async (_event, payload?: { drive?: string; titleId?: string }) => {
    const ip = xboxIP();
    if (!ip) return { ok: false, error: "No Xbox IP configured." };
    const drive = payload?.drive || "";
    const tid = payload?.titleId ? `&title_id=${encodeURIComponent(payload.titleId)}` : "";
    try {
      const r = await backendGet(
        `/saves/discover?ip=${encodeURIComponent(ip)}&drive=${encodeURIComponent(drive)}${tid}`
      );
      return tryParseJSON(r);
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  ipcMain.handle("saves:list", async (_event, payload: { drive?: string; titleId: string; profileId: string }) => {
    const ip = xboxIP();
    if (!ip) return { ok: false, error: "No Xbox IP configured." };
    const drive = payload.drive || "";
    try {
      const r = await backendGet(
        `/saves/list?ip=${encodeURIComponent(ip)}&drive=${encodeURIComponent(drive)}&title_id=${encodeURIComponent(payload.titleId)}&profile_id=${encodeURIComponent(payload.profileId)}`
      );
      return tryParseJSON(r);
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  ipcMain.handle("saves:download", async (_event, payload: { drive?: string; titleId: string; profileId: string; gameName?: string }) => {
    const ip = xboxIP();
    if (!ip) return { ok: false, error: "No Xbox IP configured." };
    const drive = payload.drive || "";
    try {
      const r = await backendPost("/saves/download", {
        ip,
        drive,
        title_id: payload.titleId,
        profile_id: payload.profileId,
        game_name: payload.gameName || "",
      });
      return r;
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  ipcMain.handle("saves:delete", async (_event, payload: { drive?: string; titleId: string; profileId: string }) => {
    const ip = xboxIP();
    if (!ip) return { ok: false, error: "No Xbox IP configured." };
    const drive = payload.drive || "";
    try {
      const r = await backendPost("/saves/delete", {
        ip,
        drive,
        title_id: payload.titleId,
        profile_id: payload.profileId,
      });
      return r;
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  ipcMain.handle("saves:copy", async (_event, payload: {
    drive?: string; titleId: string; srcProfile: string; dstProfile: string; useKeyvault?: boolean;
  }) => {
    const ip = xboxIP();
    if (!ip) return { ok: false, error: "No Xbox IP configured." };
    try {
      const r = await backendPost("/saves/copy", {
        ip,
        drive: payload.drive || "",
        title_id: payload.titleId,
        src_profile: payload.srcProfile,
        dst_profile: payload.dstProfile,
        use_keyvault: payload.useKeyvault !== false,
      });
      return r;
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  ipcMain.handle("saves:backup-all", async (_event, payload?: { drive?: string }) => {
    const ip = xboxIP();
    if (!ip) return { ok: false, error: "No Xbox IP configured." };
    try {
      const r = await backendPost("/saves/backup-all", {
        ip,
        drive: payload?.drive || "",
      });
      return r;
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });

  ipcMain.handle("saves:keyvault-status", async () => {
    const ip = xboxIP();
    if (!ip) return { ok: false, error: "No Xbox IP configured." };
    try {
      const r = await backendGet(`/saves/keyvault-status?ip=${encodeURIComponent(ip)}`);
      return tryParseJSON(r);
    } catch (err: any) {
      return { ok: false, error: err.message || String(err) };
    }
  });
}
