import { fetchHttpImage } from "../infrastructure/httpHelper";

export interface CoverResult {
  ok: boolean;
  dataUrl?: string;
}

export interface XboxUnityCoverEntry {
  titleId: string;
  front: string | null;
  thumbnail: string | null;
  url: string | null;
  official: boolean;
  rating: number | null;
  source?: string;
  assetType?: string;
}

// In-memory cache: base-title → { ok, dataUrl? }
export const browseCoverCache = new Map<string, CoverResult>();

/** Strip ALL parenthetical/bracketed suffixes to get a bare base title. */
export function baseTitleForCover(raw: string): string {
  return raw
    .replace(/\s*\(.*?\)/g, "")
    .replace(/\s*\[.*?\]/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

/** Clean a Redump/No-Intro game name for a cover-art search query. */
export function cleanTitleForSearch(raw: string): string {
  return raw
    .replace(/\s*\((?:USA|EUR|PAL|NTSC|Japan|UK|EU|US|En|Fr|De|Es|Pt|Rev\s*\d+|v\d[^)]*|[A-Z]{2,3})\)/gi, "")
    .replace(/\s*\[.*?\]/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

export async function fetchXboxUnityCoverWithMeta(searchTerm: string): Promise<{ buf: Buffer; titleId: string } | null> {
  const url     = `http://xboxunity.net/api/Covers/${encodeURIComponent(searchTerm)}`;
  const jsonBuf = await fetchHttpImage(url);
  if (!jsonBuf || jsonBuf.length === 0) return null;
  let items: any[];
  try { items = JSON.parse(jsonBuf.toString("utf8")); } catch { return null; }
  if (!Array.isArray(items) || items.length === 0) return null;

  const sorted = [...items].sort((a, b) => {
    if (b.official && !a.official) return 1;
    if (a.official && !b.official) return -1;
    return (b.rating || 0) - (a.rating || 0);
  });
  const row      = sorted[0];
  const coverUrl = row.front || row.thumbnail || row.url;
  if (!coverUrl) return null;

  const tidRaw  = row.titleid ?? row.TitleID;
  const ts      = String(tidRaw ?? "").trim();
  const titleId = /^[0-9A-F]{8}$/i.test(ts) ? ts.toUpperCase() : "";

  const buf = await fetchHttpImage(coverUrl);
  if (!buf || buf.length < 100) return null;
  return { buf, titleId };
}

export async function searchXboxUnityCovers(searchTerm: string): Promise<XboxUnityCoverEntry[]> {
  const url     = `http://xboxunity.net/api/Covers/${encodeURIComponent(searchTerm)}`;
  const jsonBuf = await fetchHttpImage(url);
  if (!jsonBuf || jsonBuf.length === 0) return [];
  let items: any[];
  try { items = JSON.parse(jsonBuf.toString("utf8")); } catch { return []; }
  if (!Array.isArray(items)) return [];
  return items
    .map((item) => ({
      titleId:   String(item.titleid || item.TitleID || "").toUpperCase(),
      front:     item.front     || null,
      thumbnail: item.thumbnail || null,
      url:       item.url       || null,
      official:  !!item.official,
      rating:    item.rating != null ? Number(item.rating) : null,
    }))
    .filter((r) => r.front || r.thumbnail || r.url)
    .sort((a, b) => {
      if (b.official !== a.official) return a.official ? -1 : 1;
      return (b.rating || 0) - (a.rating || 0);
    });
}

const invalidStoreTitleHex = new Set(["00000000", "FFFFFFFF"]);

function normalizeKeyStore(s: string): string {
  return String(s).toLowerCase().replace(/[^a-z0-9]+/g, "").trim();
}

function titleRankStore(query: string, title: string): number {
  const q = normalizeKeyStore(query);
  const t = normalizeKeyStore(title);
  if (!q || !t) return 99;
  if (q === t) return 0;
  if (t.includes(q) || q.includes(t)) return 1;
  return 2;
}

function extractTitleIdFromStoreProductJsonStr(jsonStr: string): string {
  const primaryMatch = jsonStr.match(/ProductGroupName"\s*:\s*"[^"]*\(([0-9A-F]{8})\)/i);
  if (primaryMatch) {
    const h = primaryMatch[1].toUpperCase();
    if (!invalidStoreTitleHex.has(h)) return h;
  }
  for (const match of jsonStr.matchAll(/\(([0-9A-F]{8})\)/gi)) {
    const h = match[1].toUpperCase();
    if (!invalidStoreTitleHex.has(h)) return h;
  }
  return "";
}

export async function fetchMicrosoftStoreTitleIdForBrowse(searchTerm: string): Promise<string> {
  const p = new URLSearchParams({
    languages:              "en-us",
    market:                 "US",
    platformdependencyname: "Windows.Xbox",
    productFamilyNames:     "Games",
    query:                  searchTerm,
    topProducts:            "10",
  });
  let res: Response;
  try {
    res = await fetch(
      `https://displaycatalog.mp.microsoft.com/v7.0/productFamilies/autosuggest?${p}`,
      { headers: { "User-Agent": "Mozilla/5.0 GODsend-browse-cover" } }
    );
  } catch { return ""; }
  if (!res.ok) return "";
  let asj: any;
  try { asj = await res.json(); } catch { return ""; }

  const candidates: { productId: string; title: string; type: string }[] = [];
  for (const fam of asj.Results || []) {
    for (const pr of fam.Products || []) {
      if (pr?.ProductId && pr?.Title) {
        candidates.push({ productId: pr.ProductId, title: String(pr.Title), type: pr.Type || "" });
      }
    }
  }
  candidates.sort((a, b) => {
    const g  = (t: string) => (t === "Game" ? 0 : 1);
    const tg = g(a.type) - g(b.type);
    if (tg !== 0) return tg;
    return titleRankStore(searchTerm, a.title) - titleRankStore(searchTerm, b.title);
  });

  for (let i = 0; i < Math.min(2, candidates.length); i++) {
    const q2 = new URLSearchParams({
      bigIds:         candidates[i].productId,
      market:         "US",
      languages:      "en-us",
      fieldsTemplate: "details",
    });
    let pr: Response;
    try {
      pr = await fetch(`https://displaycatalog.mp.microsoft.com/v7.0/products?${q2}`, {
        headers: { "User-Agent": "Mozilla/5.0 GODsend-browse-cover" },
      });
    } catch { continue; }
    if (!pr.ok) continue;
    let pj: any;
    try { pj = await pr.json(); } catch { continue; }
    const hex = extractTitleIdFromStoreProductJsonStr(JSON.stringify(pj));
    if (/^[0-9A-F]{8}$/.test(hex)) return hex;
  }
  return "";
}

export async function tryXboxCdnFromMicrosoftStoreSearch(searchTerm: string): Promise<Buffer | null> {
  const hex = await fetchMicrosoftStoreTitleIdForBrowse(searchTerm);
  if (!hex) return null;
  const xboxUrl = `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${hex}/en-US/1`;
  const xboxBuf = await fetchHttpImage(xboxUrl);
  return xboxBuf && xboxBuf.length >= 100 ? xboxBuf : null;
}

export async function fetchWikipediaCover(articleTitle: string): Promise<Buffer | null> {
  const url     = `https://en.wikipedia.org/api/rest_v1/page/summary/${encodeURIComponent(articleTitle)}`;
  const jsonBuf = await fetchHttpImage(url);
  if (!jsonBuf) return null;
  let parsed: any;
  try { parsed = JSON.parse(jsonBuf.toString("utf8")); } catch { return null; }
  if (parsed.type !== "standard") return null;
  const imgUrl = parsed.originalimage?.source || parsed.thumbnail?.source;
  if (!imgUrl) return null;
  const buf = await fetchHttpImage(imgUrl);
  return buf && buf.length >= 100 ? buf : null;
}

export async function fetchXboxCdnAssets(
  titleIdHex: string,
  locale = "en-US"
): Promise<{ background: string[]; banner: string[]; icon: string[]; screenshot: string[]; cover: string[] }> {
  const result = {
    background: [] as string[], banner: [] as string[],
    icon: [] as string[], screenshot: [] as string[], cover: [] as string[],
  };
  if (!titleIdHex || !/^[0-9A-F]{8}$/i.test(titleIdHex)) return result;

  const catalogUrl =
    `http://catalog-cdn.xboxlive.com/Catalog/Catalog.asmx/Query` +
    `?methodName=FindGames` +
    `&Names=Locale&Values=${locale}` +
    `&Names=LegalLocale&Values=${locale}` +
    `&Names=Store&Values=1&Names=PageSize&Values=100&Names=PageNum&Values=1` +
    `&Names=DetailView&Values=5&Names=OfferFilterLevel&Values=1` +
    `&Names=MediaIds&Values=66acd000-77fe-1000-9115-d802${titleIdHex.toUpperCase()}` +
    `&Names=UserTypes&Values=2` +
    `&Names=MediaTypes&Values=1&Names=MediaTypes&Values=21` +
    `&Names=MediaTypes&Values=23&Names=MediaTypes&Values=37&Names=MediaTypes&Values=46`;

  try {
    const xmlBuf = await fetchHttpImage(catalogUrl);
    if (!xmlBuf || xmlBuf.length === 0) return result;
    const xml = xmlBuf.toString("utf8");

    for (const [, block] of xml.matchAll(/<live:image[^>]*>([\s\S]*?)<\/live:image>/gi)) {
      const urlM  = (block as string).match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
      const typeM = (block as string).match(/<live:relationshipType[^>]*>\s*(\d+)\s*<\/live:relationshipType>/i);
      if (!urlM) continue;
      const url  = urlM[1].trim();
      const type = typeM ? parseInt(typeM[1], 10) : -1;
      if (type === 15 || type === 23) result.icon.push(url);
      else if (type === 25)           result.background.push(url);
      else if (type === 27)           result.banner.push(url);
    }

    for (const [, block] of xml.matchAll(/<live:slideShow[^>]*>([\s\S]*?)<\/live:slideShow>/gi)) {
      const urlM = (block as string).match(/<live:fileUrl[^>]*>\s*(https?:\/\/[^\s<]+)\s*<\/live:fileUrl>/i);
      if (urlM) result.screenshot.push(urlM[1].trim());
    }

    const coverUrl = `http://catalog.xboxlive.com/Catalog/Product/CoverArt/${titleIdHex.toUpperCase()}/${locale}/1`;
    const coverBuf = await fetchHttpImage(coverUrl);
    if (coverBuf && coverBuf.length >= 100) {
      const mime = (coverBuf[0] === 0xFF && coverBuf[1] === 0xD8) ? "image/jpeg" : "image/png";
      result.cover.push(`data:${mime};base64,${coverBuf.toString("base64")}`);
    }
  } catch { /* ignore catalog errors */ }
  return result;
}

export async function resolveTitleIdHex(term: string): Promise<string | null> {
  if (/^[0-9A-F]{8}$/i.test(term)) return term.toUpperCase();
  const covers = await searchXboxUnityCovers(term);
  const first  = covers.find((c) => c.titleId && /^[0-9A-F]{8}$/.test(c.titleId));
  return first ? first.titleId : null;
}
