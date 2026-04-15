/**
 * IPC handlers for the Browse view and download queue:
 *   browse:get-games, browse:queue-game, browse:get-disc-info, browse:fetch-cover
 *   xbox:get-queue, xbox:remove-queue-item
 */

import http from "http";
import { IpcMain } from "electron";

import { getConfiguredXboxIP, getConfiguredServerPort } from "../services/settingsService";
import { backendGet } from "../infrastructure/backendHttp";
import { fetchHttpImage } from "../infrastructure/httpHelper";
import {
  browseCoverCache,
  baseTitleForCover,
  fetchXboxUnityCoverWithMeta,
  tryXboxCdnFromMicrosoftStoreSearch,
  fetchWikipediaCover,
} from "../services/coverArtService";

export function register(ipcMain: IpcMain): void {

  // ── Get game list from Go backend ──────────────────────────────────────────
  ipcMain.handle("browse:get-games", async (_event, { platform, source }) => {
    try {
      const src  = source ? `&source=${encodeURIComponent(source)}` : "";
      const data = await backendGet(
        `/browse?platform=${encodeURIComponent(platform)}${src}`
      );
      if (data.startsWith("__IA_LOADING__")) {
        const m = data.match(/__IA_LOADING__:(\d+)\/(\d+)/);
        return { ok: true, loading: true, loaded: m ? m[1] : "?", total: m ? m[2] : "?", games: [] };
      }
      const games = data.split("|").map((s: string) => s.trim()).filter(Boolean);
      return { ok: true, loading: false, games };
    } catch (err: any) {
      return { ok: false, error: err.message, games: [] };
    }
  });

  // ── Queue a game (register → trigger on Go backend) ───────────────────────
  ipcMain.handle("browse:queue-game", async (_event, { game, platform, source, drive, installType }) => {
    const xboxIp = getConfiguredXboxIP();
    if (!xboxIp) return { ok: false, error: "No Xbox IP configured. Check Settings → Xbox connection." };

    const enc  = encodeURIComponent(game);
    const drv  = encodeURIComponent(drive || "Hdd1:");
    const inst = encodeURIComponent(installType || "god");
    const plat = encodeURIComponent(platform || "xbox360");
    const src  = source ? `&source=${encodeURIComponent(source)}` : "";

    try {
      const regData = await backendGet(
        `/register?game=${enc}&ip=${encodeURIComponent(xboxIp)}&drive=${drv}&platform=${plat}&mode=ftp&install_type=${inst}`
      );
      let reg: any;
      try { reg = JSON.parse(regData); } catch { reg = {}; }
      if (reg.error) return { ok: false, error: `Register: ${reg.error}` };

      const trigData = await backendGet(
        `/trigger?game=${enc}&platform=${plat}&install_type=${inst}${src}`
      );
      let trig: any;
      try { trig = JSON.parse(trigData); } catch { trig = {}; }
      if (trig.error) return { ok: false, error: `Trigger: ${trig.error}` };

      const status = trig.status || "triggered";
      return { ok: true, status };
    } catch (err: any) {
      return { ok: false, error: err.message };
    }
  });

  // ── Disc-info recommendation (install type hint) ───────────────────────────
  ipcMain.handle("browse:get-disc-info", async (_event, game) => {
    try {
      const data = await backendGet(`/disc-info?game=${encodeURIComponent(game)}`);
      return { ok: true, ...JSON.parse(data) };
    } catch {
      return { ok: false };
    }
  });

  // ── Fetch cover art for a game name (multi-source cascade, memory-cached) ──
  ipcMain.handle("browse:fetch-cover", async (_event, gameName: string) => {
    try {
      const base = baseTitleForCover(gameName);

      if (browseCoverCache.has(base)) return browseCoverCache.get(base);

      let imgBuf: Buffer | null = null;

      // ── 1. XboxUnity Covers API ─────────────────────────────────────────────
      if (!imgBuf) {
        let meta = await fetchXboxUnityCoverWithMeta(base);
        if (!meta) {
          const m = base.match(/^.+?\s+-\s+(.+)$/);
          if (m) meta = await fetchXboxUnityCoverWithMeta(m[1].trim());
        }
        if (meta) {
          imgBuf = meta.buf;
          if (meta.titleId) {
            const xboxUrl = `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${meta.titleId}/en-US/1`;
            const xboxBuf = await fetchHttpImage(xboxUrl);
            if (xboxBuf && xboxBuf.length >= 100) imgBuf = xboxBuf;
          }
        }
      }

      // ── 2. Xbox.com catalog via TitleList ───────────────────────────────────
      if (!imgBuf) {
        const tlBuf = await fetchHttpImage(
          `http://xboxunity.net/Resources/Lib/TitleList.php?search=${encodeURIComponent(base)}`
        );
        if (tlBuf) {
          try {
            const tlJson = JSON.parse(tlBuf.toString("utf8"));
            const tlTid  = tlJson?.Items?.[0]?.TitleID;
            if (tlTid) {
              const xboxBuf = await fetchHttpImage(
                `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${String(tlTid).toUpperCase()}/en-US/1`
              );
              if (xboxBuf && xboxBuf.length >= 100) imgBuf = xboxBuf;
            }
          } catch { /* ignore */ }
        }
      }

      if (!imgBuf) {
        const m = base.match(/^.+?\s+-\s+(.+)$/);
        if (m) {
          const tlBuf = await fetchHttpImage(
            `http://xboxunity.net/Resources/Lib/TitleList.php?search=${encodeURIComponent(m[1].trim())}`
          );
          if (tlBuf) {
            try {
              const tlJson = JSON.parse(tlBuf.toString("utf8"));
              const tlTid  = tlJson?.Items?.[0]?.TitleID;
              if (tlTid) {
                const xboxBuf = await fetchHttpImage(
                  `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${String(tlTid).toUpperCase()}/en-US/1`
                );
                if (xboxBuf && xboxBuf.length >= 100) imgBuf = xboxBuf;
              }
            } catch { /* ignore */ }
          }
        }
      }

      // ── 3. Microsoft Store → Title ID in product metadata → CDN ────────────
      if (!imgBuf) {
        imgBuf = await tryXboxCdnFromMicrosoftStoreSearch(base);
        if (!imgBuf) {
          const sm = base.match(/^.+?\s+-\s+(.+)$/);
          if (sm) imgBuf = await tryXboxCdnFromMicrosoftStoreSearch(sm[1].trim());
        }
      }

      // ── 4. Wikipedia REST API ───────────────────────────────────────────────
      if (!imgBuf) {
        const wikiTitles = [`${base} (video game)`, base];
        const m = base.match(/^.+?\s+-\s+(.+)$/);
        if (m) wikiTitles.push(`${m[1].trim()} (video game)`, m[1].trim());
        for (const title of wikiTitles) {
          imgBuf = await fetchWikipediaCover(title);
          if (imgBuf) break;
        }
      }

      if (!imgBuf) {
        const result = { ok: false };
        browseCoverCache.set(base, result);
        return result;
      }

      const mime =
        (imgBuf[0] === 0xFF && imgBuf[1] === 0xD8) ? "image/jpeg" :
        (imgBuf[0] === 0x89 && imgBuf[1] === 0x50) ? "image/png"  : "image/jpeg";
      const result = { ok: true, dataUrl: `data:${mime};base64,${imgBuf.toString("base64")}` };
      browseCoverCache.set(base, result);
      return result;
    } catch {
      return { ok: false };
    }
  });

  // ── Download queue ──────────────────────────────────────────────────────────
  ipcMain.handle("xbox:get-queue", () => {
    return new Promise((resolve) => {
      const port = getConfiguredServerPort();
      const req  = http.get(`http://localhost:${port}/queue`, (res) => {
        let data = "";
        res.on("data",  (chunk) => { data += chunk; });
        res.on("end", () => {
          try { resolve({ ok: true, jobs: JSON.parse(data) }); }
          catch { resolve({ ok: true, jobs: [] }); }
        });
      });
      req.on("error", (err: Error) => resolve({ ok: false, jobs: [], error: err.message }));
      req.setTimeout(5000, () => { req.destroy(); resolve({ ok: false, jobs: [] }); });
    });
  });

  ipcMain.handle("xbox:remove-queue-item", (_event, game: string) => {
    return new Promise((resolve) => {
      const port = getConfiguredServerPort();
      const enc  = encodeURIComponent(game);
      const req  = http.get(`http://localhost:${port}/queue/remove?game=${enc}`, (res) => {
        let data = "";
        res.on("data",  (chunk) => { data += chunk; });
        res.on("end", () => resolve({ ok: true, data }));
      });
      req.on("error", (err: Error) => resolve({ ok: false, error: err.message }));
      req.setTimeout(5000, () => { req.destroy(); resolve({ ok: false }); });
    });
  });
}
