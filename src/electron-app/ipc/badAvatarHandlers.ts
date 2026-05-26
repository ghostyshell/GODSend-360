/**
 * IPC handlers for the BadAvatar USB toolbox tool.
 */

import { IpcMain } from "electron";
import { resolveFat32FormatExe } from "../infrastructure/fat32Format";
import {
  createBadAvatarUsb,
  formatRequiresElevation,
  isRunningAsAdmin,
  listFat32UsbDrives,
  type BadAvatarCreateOptions,
} from "../services/badAvatarUsbService";
import { getWebContentsForPush } from "../app/window";

export function register(ipcMain: IpcMain): void {
  ipcMain.handle("tools:badavatar-list-drives", async () => {
    try {
      const drives = await listFat32UsbDrives();
      return { ok: true, drives };
    } catch (err: any) {
      return { ok: false, drives: [], error: err.message || String(err) };
    }
  });

  ipcMain.handle("tools:badavatar-is-admin", async () => {
    return {
      ok: true,
      isAdmin: await isRunningAsAdmin(),
      platform: process.platform,
      formatRequiresElevation: formatRequiresElevation(),
      hasFat32FormatExe: process.platform === "win32" && resolveFat32FormatExe() != null,
    };
  });

  ipcMain.handle("tools:badavatar-create", async (_event, opts: BadAvatarCreateOptions) => {
    if (!opts?.driveRoot) {
      return { ok: false, error: "No USB drive selected." };
    }

    const wc = getWebContentsForPush();
    const emit = (progress: { status: string; percent: number; detail?: string }) => {
      if (wc && !wc.isDestroyed()) {
        wc.send("tools:badavatar-progress", progress);
      }
    };

    try {
      await createBadAvatarUsb(opts, emit);
      return { ok: true };
    } catch (err: any) {
      emit({ status: `Error: ${err.message || err}`, percent: 0 });
      return { ok: false, error: err.message || String(err) };
    }
  });
}
