import { useState, useEffect, useRef, useCallback } from "react";
import {
  ArrowLeft, Gamepad2, Loader2, WifiOff,
  Star, Disc3, RotateCw, Upload, Search, X, Check, ChevronLeft, ChevronRight,
  ArrowUpDown, Filter, HardDrive, ArrowRightLeft, Download,
  Puzzle, PackageOpen, Trash2,
} from "lucide-react";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { ScrollArea } from "./ui/scroll-area";
import { cn } from "../lib/utils";
import XboxBoxCover from "./XboxBoxCover";
import MainNav from "./MainNav";

// ── Types ─────────────────────────────────────────────────────────────────────

interface Game {
  titleId: string;
  contentId?: string;
  name: string;
  gameDataDir?: string;
  sourceDrive?: string;
  directory?: string;
  publisher?: string;
  developer?: string;
  releaseDate?: string;
  description?: string;
  isFavorite?: boolean;
  discsInSet?: number;
  discNum?: number;
  timesPlayed?: number;
  lastPlayed?: string;
  liveRating?: string | number;
  liveRaters?: string | number;
  scanPathId?: number | null;
  mediaId?: number | null;
  fileType?: number | null;
  contentType?: number | null;
}

interface AssetSlot {
  src?: string;
  ext?: string;
}

interface TitleVisuals {
  background?: AssetSlot;
  banner?: AssetSlot;
  icon?: AssetSlot;
  cover?: AssetSlot;
  coverIsBooklet?: boolean;
  importCover?: AssetSlot;
  screenshots?: AssetSlot[];
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function isWebImageExt(ext: string) {
  const e = String(ext || "").toLowerCase();
  return e === ".jpg" || e === ".jpeg" || e === ".png" || e === ".gif" || e === ".bmp" || e === ".webp";
}

function isDataUrl(s: unknown): s is string {
  return typeof s === "string" && s.startsWith("data:");
}

function MetaRow({ label, value }: { label: string; value?: string | null }) {
  if (value === undefined || value === null || value === "") return null;
  return (
    <div className="flex gap-2 min-w-0">
      <span className="text-[11px] text-muted-foreground shrink-0 w-[88px]">{label}</span>
      <span className="text-[11px] text-foreground break-words min-w-0">{String(value)}</span>
    </div>
  );
}

function StarRating({ rating, raters }: { rating?: string | number; raters?: string | number }) {
  if (!rating) return null;
  const stars = Math.round(Number(rating));
  return (
    <div className="flex items-center gap-1.5 flex-wrap">
      <div className="flex">
        {[1,2,3,4,5].map((n) => (
          <Star
            key={n}
            className={cn("h-3 w-3", n <= stars ? "text-yellow-400 fill-yellow-400" : "text-muted-foreground/30")}
          />
        ))}
      </div>
      <span className="text-[10px] text-muted-foreground">{rating}</span>
      {raters && <span className="text-[10px] text-muted-foreground/60">({raters})</span>}
    </div>
  );
}

// ── Lightbox viewer ───────────────────────────────────────────────────────────

interface ImageLightboxProps {
  images: string[];
  startIndex?: number;
  onClose: () => void;
}

function ImageLightbox({ images, startIndex, onClose }: ImageLightboxProps) {
  const [idx, setIdx] = useState(startIndex ?? 0);
  const total = images.length;
  const prev = useCallback(() => setIdx((i) => (i - 1 + total) % total), [total]);
  const next = useCallback(() => setIdx((i) => (i + 1) % total), [total]);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape")     onClose();
      if (e.key === "ArrowLeft")  prev();
      if (e.key === "ArrowRight") next();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, prev, next]);

  const src = images[idx];
  if (!src) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/90"
      onClick={onClose}
    >
      <button
        className="absolute top-3 right-3 text-white/60 hover:text-white z-10"
        onClick={onClose}
      >
        <X className="h-6 w-6" />
      </button>
      {total > 1 && (
        <>
          <button
            className="absolute left-3 top-1/2 -translate-y-1/2 text-white/60 hover:text-white z-10 p-1"
            onClick={(e) => { e.stopPropagation(); prev(); }}
          >
            <ChevronLeft className="h-8 w-8" />
          </button>
          <button
            className="absolute right-3 top-1/2 -translate-y-1/2 text-white/60 hover:text-white z-10 p-1"
            onClick={(e) => { e.stopPropagation(); next(); }}
          >
            <ChevronRight className="h-8 w-8" />
          </button>
          <span className="absolute bottom-3 left-1/2 -translate-x-1/2 text-[11px] text-white/50">
            {idx + 1} / {total}
          </span>
        </>
      )}
      <img
        src={src}
        alt=""
        className="max-w-[90vw] max-h-[90vh] object-contain rounded shadow-2xl"
        draggable={false}
        onClick={(e) => e.stopPropagation()}
      />
    </div>
  );
}

// ── Asset slot definitions ────────────────────────────────────────────────────

const MAIN_SLOTS = [
  { key: "background", label: "Background", aspect: "video",    importName: "background" },
  { key: "banner",     label: "Banner",     aspect: "wide",     importName: "banner"     },
  { key: "icon",       label: "Icon",       aspect: "square",   importName: "icon"       },
  { key: "cover",      label: "Cover",      aspect: "portrait", importName: "cover"      },
];

const SCREENSHOT_COUNT = 10;

// ── Single asset slot card ────────────────────────────────────────────────────

interface AssetSlotCardProps {
  slotKey: string;
  label: string;
  aspect: string;
  currentAsset?: AssetSlot | null;
  pendingAsset?: any | null;
  onSearch: () => void;
  onUpload: () => void;
  onClearPending: () => void;
  onImageClick?: () => void;
  objectFit?: React.CSSProperties["objectFit"];
  objectPosition?: string;
}

function AssetSlotCard({
  slotKey, label, aspect, currentAsset, pendingAsset,
  onSearch, onUpload, onClearPending, onImageClick,
  objectFit = "cover", objectPosition = "center",
}: AssetSlotCardProps) {
  const hasPending = !!pendingAsset;
  const displaySrc  = hasPending
    ? (pendingAsset.dataUrl || pendingAsset.previewUrl || pendingAsset.url || null)
    : (currentAsset?.src || null);
  const displayIsWeb = hasPending
    ? (pendingAsset.dataUrl ? true : isWebImageExt(".jpg"))
    : (currentAsset ? isWebImageExt(currentAsset.ext || "") : false);

  const canClick = !!(displaySrc && displayIsWeb && onImageClick);

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between min-w-0">
        <p className="text-[9px] text-muted-foreground truncate">{label}</p>
        {hasPending && (
          <button
            className="text-[8px] text-muted-foreground hover:text-foreground ml-1 shrink-0"
            onClick={onClearPending}
            title="Discard pending change"
          >
            <X className="h-2.5 w-2.5" />
          </button>
        )}
      </div>
      <div
        className={cn(
          "relative rounded-md border bg-[#0d1117] overflow-hidden flex items-center justify-center",
          hasPending ? "border-primary/50 ring-1 ring-primary/30" : "border-border",
          canClick ? "cursor-zoom-in hover:opacity-90 transition-opacity" : "cursor-default",
          aspect === "video"    && "aspect-video w-full",
          aspect === "wide"     && "aspect-[4/1] w-full",
          aspect === "square"   && "aspect-square w-[68px]",
          aspect === "portrait" && "aspect-[3/4] w-[60px]",
        )}
        onClick={canClick ? onImageClick : undefined}
      >
        {displaySrc && displayIsWeb ? (
          <img
            src={displaySrc}
            alt=""
            className="w-full h-full"
            style={{ objectFit, objectPosition }}
            draggable={false}
          />
        ) : displaySrc ? (
          <span className="text-[7px] text-muted-foreground text-center px-1 leading-tight">
            {(currentAsset?.ext || "").toUpperCase()}<br />cached
          </span>
        ) : (
          <span className="text-[7px] text-muted-foreground">—</span>
        )}
        {hasPending && (
          <div
            className="absolute top-0.5 right-0.5 w-2 h-2 rounded-full bg-primary flex items-center justify-center"
            title="Pending — not yet saved"
          >
            <div className="w-1 h-1 rounded-full bg-primary-foreground" />
          </div>
        )}
      </div>
      <div className="flex gap-1">
        <button
          className="flex-1 text-[8px] py-0.5 rounded border border-border text-muted-foreground hover:text-foreground hover:border-primary/40 transition-colors flex items-center justify-center gap-0.5"
          onClick={onSearch}
          title="Search XboxUnity online"
        >
          <Search className="h-2 w-2" />Search
        </button>
        <button
          className="flex-1 text-[8px] py-0.5 rounded border border-border text-muted-foreground hover:text-foreground hover:border-primary/40 transition-colors flex items-center justify-center gap-0.5"
          onClick={onUpload}
          title="Upload a local image file"
        >
          <Upload className="h-2 w-2" />File
        </button>
      </div>
    </div>
  );
}

// ── Asset search panel ────────────────────────────────────────────────────────

interface AssetSearchPanelProps {
  game: Game;
  targetSlot: string;
  onSelect: (result: any) => void;
  onClose: () => void;
}

function AssetSearchPanel({ game, targetSlot, onSelect, onClose }: AssetSearchPanelProps) {
  const [query, setQuery]       = useState(game.name);
  const [results, setResults]   = useState<any[]>([]);
  const [loading, setLoading]   = useState(false);
  const [searched, setSearched] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
    // Auto-search by titleId on open.
    runSearch(game.name, game.titleId);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function runSearch(q: string, titleId?: string) {
    setLoading(true);
    setSearched(false);
    setResults([]);
    try {
      const r = await window.godsendApi.searchAssets({
        query:     q || game.name,
        titleId:   titleId || game.titleId,
        assetType: targetSlot,
      });
      setResults(r.results || []);
    } finally {
      setLoading(false);
      setSearched(true);
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter") runSearch(query);
    if (e.key === "Escape") onClose();
  }

  const slotLabel = [...MAIN_SLOTS, ...Array.from({ length: SCREENSHOT_COUNT }, (_, i) => ({
    key: `screenshot${i + 1}`, label: `Screenshot ${i + 1}`,
  }))].find((s) => s.key === targetSlot)?.label || targetSlot;

  const isCoverSearch = !targetSlot || targetSlot === "cover";
  const isScreenshotSearch = targetSlot?.startsWith("screenshot");
  const isWideSearch = isScreenshotSearch || targetSlot === "background";
  const thumbAspect = isWideSearch ? "aspect-video" : targetSlot === "banner" ? "aspect-[4/1]" : targetSlot === "icon" ? "aspect-square" : "aspect-[3/4]";
  const thumbWidth  = isWideSearch ? 120 : 68;

  return (
    <div className="rounded-md border border-border/80 bg-[#0d1117] p-2.5 space-y-2">
      <div className="flex items-center justify-between gap-2">
        <p className="text-[10px] font-semibold text-foreground truncate">
          Search for: <span className="text-primary">{slotLabel}</span>
        </p>
        <button
          className="text-muted-foreground hover:text-foreground shrink-0"
          onClick={onClose}
          title="Close"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="flex gap-1.5">
        <input
          ref={inputRef}
          className="flex-1 text-[10px] h-6 px-2 rounded border border-border bg-background text-foreground min-w-0 outline-none focus:border-primary/50"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Game title or TitleID…"
        />
        <button
          className="text-[9px] px-2 py-0.5 rounded border border-border text-muted-foreground hover:text-foreground hover:border-primary/40 disabled:opacity-40 transition-colors"
          onClick={() => runSearch(query)}
          disabled={loading || !query.trim()}
        >
          {loading ? <Loader2 className="h-3 w-3 animate-spin" /> : "Go"}
        </button>
      </div>

      {loading && (
        <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          {isCoverSearch ? "Searching XboxUnity…" : "Searching Xbox CDN catalog…"}
        </div>
      )}

      {searched && !loading && results.length === 0 && (
        <p className="text-[10px] text-muted-foreground">
          No {slotLabel.toLowerCase()} results found.{" "}
          {!isCoverSearch && "The Xbox CDN may not have assets for this title. "}
          Try a different query or use <span className="text-muted-foreground/70">File</span> to upload a local image.
        </p>
      )}

      {results.length > 0 && (
        <div className="flex flex-wrap gap-2 max-h-[200px] overflow-y-auto pt-0.5">
          {results.map((r: any, idx: number) => {
            const thumb = r.thumbnail || r.front || r.url;
            return (
              <button
                key={`${r.titleId || "r"}-${idx}`}
                className="flex flex-col rounded border border-border hover:border-primary/60 bg-[#0d1117] overflow-hidden transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                style={{ width: thumbWidth }}
                onClick={() => onSelect(r)}
                title={[r.official && "Official", r.rating != null && `★${r.rating}`, r.assetType].filter(Boolean).join(" · ")}
              >
                <div className={`w-full ${thumbAspect} overflow-hidden bg-muted/20`}>
                  {thumb ? (
                    <img src={thumb} alt="" className="w-full h-full object-cover" draggable={false} />
                  ) : (
                    <div className="w-full h-full flex items-center justify-center text-[7px] text-muted-foreground">
                      No image
                    </div>
                  )}
                </div>
                <div className="px-0.5 py-0.5 flex gap-0.5 flex-wrap">
                  {r.official && <span className="text-[6px] text-yellow-400">Official</span>}
                  {r.rating != null && <span className="text-[6px] text-muted-foreground">★{r.rating}</span>}
                  {r.source === "xbox-cdn" && <span className="text-[6px] text-blue-400">Xbox CDN</span>}
                  {r.titleId && (
                    <p className="text-[6px] text-muted-foreground/60 font-mono truncate w-full">{r.titleId}</p>
                  )}
                </div>
              </button>
            );
          })}
        </div>
      )}

      <p className="text-[8px] text-muted-foreground/50 leading-snug">
        {isCoverSearch
          ? <>Covers from <span className="text-muted-foreground/70">XboxUnity</span> and <span className="text-muted-foreground/70">Xbox CDN</span>. Use <span className="text-muted-foreground/70">File</span> to upload a custom image.</>
          : <>Results from <span className="text-muted-foreground/70">Xbox CDN catalog</span> for <span className="text-muted-foreground/70">{slotLabel.toLowerCase()}</span> assets. Use <span className="text-muted-foreground/70">File</span> to upload a custom image.</>
        }
      </p>
    </div>
  );
}

// ── Asset editor section ──────────────────────────────────────────────────────

// Per-slot display overrides
const SLOT_DISPLAY: Record<string, { objectFit?: React.CSSProperties["objectFit"]; objectPosition?: string }> = {
  banner:  { objectFit: "contain", objectPosition: "center" },
  cover:   { objectFit: "contain", objectPosition: "center" },
};

interface AssetEditorSectionProps {
  game: Game;
  titleVisuals?: TitleVisuals | null;
  rxeaSlots?: Record<string, AssetSlot> | null;
  rxeaLoading?: boolean;
  onRefresh?: () => void;
}

function AssetEditorSection({ game, titleVisuals, rxeaSlots, rxeaLoading, onRefresh }: AssetEditorSectionProps) {
  const [pending, setPending]       = useState<Record<string, any>>({});
  const [searchSlot, setSearchSlot] = useState<string | null>(null);
  const [saving, setSaving]         = useState(false);
  const [saveMsg, setSaveMsg]       = useState("");
  const [saveMsgKind, setSaveMsgKind] = useState<"ok" | "error">("ok");
  const [lightbox, setLightbox]     = useState<{ images: string[]; idx: number } | null>(null);

  const hasPending = Object.keys(pending).length > 0;

  function currentAsset(slotKey: string): AssetSlot | null {
    // RXEA-decoded assets from the console take priority over CDN/manifest assets.
    if (rxeaSlots && rxeaSlots[slotKey]) return rxeaSlots[slotKey];
    if (!titleVisuals) return null;
    if (slotKey.startsWith("screenshot")) {
      const idx = parseInt(slotKey.replace("screenshot", ""), 10) - 1;
      return titleVisuals.screenshots?.[idx] || null;
    }
    return (titleVisuals as any)[slotKey] || null;
  }

  async function openSearch(slotKey: string) {
    setSaveMsg("");
    setSearchSlot(slotKey);
  }

  function handleSearchSelect(slotKey: string, result: any) {
    // For cover art, prefer the full image (front+back) over the front-only crop
    // so Aurora receives the complete cover instead of a stretched front-only image.
    const isCover = slotKey === "cover";
    const url = isCover
      ? (result.url || result.front || result.thumbnail)
      : (result.front || result.thumbnail || result.url);
    if (!url) return;
    const previewUrl = result.thumbnail || result.front || url;

    if (isDataUrl(url)) {
      // Already a data URL (e.g. Xbox CDN results) — use directly at full resolution.
      setPending((prev) => ({ ...prev, [slotKey]: { url: null, dataUrl: url, previewUrl: url, ext: ".jpg" } }));
    } else {
      // HTTP URL — show the thumbnail immediately, then fetch the full image in background.
      setPending((prev) => ({ ...prev, [slotKey]: { url, dataUrl: null, previewUrl, ext: ".jpg" } }));
      (async () => {
        const fullUrl = isCover
          ? (result.url || result.front || url)
          : (result.front || result.url || url);
        const r = await window.godsendApi.fetchUrlImage(fullUrl);
        if (r && r.ok && r.dataUrl) {
          setPending((prev) => {
            if (!prev[slotKey]) return prev;
            return { ...prev, [slotKey]: { ...prev[slotKey], dataUrl: r.dataUrl } };
          });
        }
      })();
    }
    setSearchSlot(null);
    setSaveMsg("");
  }

  async function handleUploadFile(slotKey: string) {
    setSaveMsg("");
    const r = await window.godsendApi.chooseAssetImageFile();
    if (!r || !r.ok) return;
    setPending((prev) => ({ ...prev, [slotKey]: { dataUrl: r.dataUrl, ext: r.ext || ".jpg" } }));
  }

  function clearPending(slotKey: string) {
    setPending((prev) => { const n = { ...prev }; delete n[slotKey]; return n; });
  }

  async function saveToConsole() {
    setSaving(true);
    setSaveMsg("");
    let errorMsg = "";

    for (const [slotKey, p] of Object.entries(pending)) {
      // Extract raw base64 from data URL if present.
      const imageBase64 = p.dataUrl
        ? (p.dataUrl.includes(",") ? p.dataUrl.split(",")[1] : p.dataUrl)
        : null;

      const r = await window.godsendApi.uploadAssetToConsole({
        titleId:     game.titleId,
        gameDataDir: game.gameDataDir,
        assetType:   slotKey,
        imageBase64: imageBase64 || null,
        imageUrl:    (!imageBase64 && p.url) ? p.url : null,
        ext:         p.ext || ".jpg",
      });

      if (!r || !r.ok) {
        errorMsg = `Upload failed for ${slotKey}: ${r?.error || "Unknown error"}`;
        break;
      }
    }

    setSaving(false);
    if (errorMsg) {
      setSaveMsg(errorMsg);
      setSaveMsgKind("error");
    } else {
      setSaveMsg(`${Object.keys(pending).length} asset(s) saved as RXEA to console. Visible immediately after refresh.`);
      setSaveMsgKind("ok");
      setPending({});
      // Trigger a fresh FTP sync so the cached visuals update.
      setTimeout(() => onRefresh?.(), 400);
    }
  }

  // Current screenshot count
  const rxeaSsCount = rxeaSlots
    ? Object.keys(rxeaSlots)
        .filter((k) => k.startsWith("screenshot"))
        .map((k) => parseInt(k.replace("screenshot", ""), 10))
        .reduce((a, b) => Math.max(a, b), 0)
    : 0;
  const ssCount = Math.max(
    titleVisuals?.screenshots?.length || 0,
    rxeaSsCount,
    ...Object.keys(pending).filter((k) => k.startsWith("screenshot")).map((k) => parseInt(k.replace("screenshot", ""), 10)),
    0,
  );
  const screenshotSlots = Array.from({ length: Math.max(ssCount, SCREENSHOT_COUNT) }, (_, i) => ({
    key: `screenshot${i + 1}`, label: `Screenshot ${i + 1}`,
  }));

  function slotSrc(key: string): string | null {
    const p = pending[key];
    if (p) return p.dataUrl || p.previewUrl || p.url || null;
    return currentAsset(key)?.src || null;
  }

  // Build ordered screenshot image list for lightbox navigation.
  const screenshotSrcs = screenshotSlots.map((s) => slotSrc(s.key)).filter((s): s is string => Boolean(s));

  function openLightbox(images: string[], idx: number) {
    setLightbox({ images, idx });
  }

  return (
    <>
    {lightbox && (
      <ImageLightbox
        images={lightbox.images}
        startIndex={lightbox.idx}
        onClose={() => setLightbox(null)}
      />
    )}
    <div className="space-y-3">
      {/* Header row */}
      <div className="flex items-center justify-between gap-2">
        <p className="text-[10.5px] font-semibold text-muted-foreground uppercase tracking-wider">
          Aurora Assets
        </p>
        <div className="flex items-center gap-1.5 shrink-0">
          {rxeaLoading && (
            <span className="flex items-center gap-1 text-[9px] text-muted-foreground">
              <Loader2 className="h-2.5 w-2.5 animate-spin" />Decoding…
            </span>
          )}
          {hasPending && (
            <Button
              size="sm"
              disabled={saving}
              onClick={saveToConsole}
              className="h-6 text-[9px] px-2 gap-1"
            >
              {saving
                ? <Loader2 className="h-3 w-3 animate-spin" />
                : <Check className="h-3 w-3" />}
              Save to Console ({Object.keys(pending).length})
            </Button>
          )}
        </div>
      </div>

      {/* Status message */}
      {saveMsg && (
        <p className={cn(
          "text-[10.5px] leading-snug px-2 py-1.5 rounded",
          saveMsgKind === "error"
            ? "bg-destructive/20 text-destructive"
            : "bg-emerald-500/15 text-emerald-300",
        )}>
          {saveMsg}
        </p>
      )}

      {/* Main slots grid */}
      <div className="rounded-md border border-border/60 bg-muted/10 p-2.5">
        <p className="text-[9px] text-muted-foreground mb-2 leading-snug">
          Images are RXEA-encoded and uploaded as{" "}
          <span className="font-mono">.asset</span>{" "}
          files to the game's Aurora data directory via FTP.
        </p>
        <div className="grid gap-2.5" style={{ gridTemplateColumns: "1fr 1fr" }}>
          {MAIN_SLOTS.map(({ key, label, aspect }) => {
            const src = slotSrc(key);
            const disp = SLOT_DISPLAY[key] || {};
            return (
              <AssetSlotCard
                key={key}
                slotKey={key}
                label={label}
                aspect={aspect}
                currentAsset={currentAsset(key)}
                pendingAsset={pending[key] || null}
                onSearch={() => openSearch(key)}
                onUpload={() => handleUploadFile(key)}
                onClearPending={() => clearPending(key)}
                onImageClick={src ? () => openLightbox([src], 0) : undefined}
                objectFit={disp.objectFit}
                objectPosition={disp.objectPosition}
              />
            );
          })}
        </div>
      </div>

      {/* Screenshots */}
      {screenshotSlots.length > 0 && (
        <div className="rounded-md border border-border/60 bg-muted/10 p-2.5">
          <p className="text-[9px] text-muted-foreground mb-2">
            Screenshots{screenshotSlots.length > 1 && <span className="text-muted-foreground/50"> · scroll →</span>}
          </p>
          <div className="flex gap-2 overflow-x-auto pb-2" style={{ scrollbarWidth: "thin" }}>
            {screenshotSlots.map(({ key, label }, ssIdx) => {
              const src = slotSrc(key);
              return (
                <div key={key} className="shrink-0" style={{ width: 140 }}>
                  <AssetSlotCard
                    slotKey={key}
                    label={label}
                    aspect="video"
                    currentAsset={currentAsset(key)}
                    pendingAsset={pending[key] || null}
                    onSearch={() => openSearch(key)}
                    onUpload={() => handleUploadFile(key)}
                    onClearPending={() => clearPending(key)}
                    onImageClick={src ? () => openLightbox(screenshotSrcs, screenshotSrcs.indexOf(src)) : undefined}
                  />
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Empty state */}
      {!rxeaLoading
        && titleVisuals !== undefined
        && !titleVisuals?.background && !titleVisuals?.banner
        && !titleVisuals?.icon     && !titleVisuals?.cover
        && !titleVisuals?.importCover
        && !(titleVisuals?.screenshots?.length)
        && !rxeaSlots?.background && !rxeaSlots?.banner
        && !rxeaSlots?.icon && !rxeaSlots?.cover
        && !rxeaSsCount
        && !hasPending && (
        <div className="rounded-md border border-border/40 bg-muted/10 px-3 py-3 text-center">
          <p className="text-[11px] text-muted-foreground">
            No artwork found on console or in cache. Run a library refresh, or upload
            your own using Search / File above.
          </p>
        </div>
      )}

      {/* Inline search panel */}
      {searchSlot && (
        <AssetSearchPanel
          game={game}
          targetSlot={searchSlot}
          onSelect={(result) => handleSearchSelect(searchSlot, result)}
          onClose={() => setSearchSlot(null)}
        />
      )}
    </div>
    </>
  );
}

// ── Content / DLC / Title Update section ────────────────────────────────────

interface ContentItem {
  title_id: string;
  content_type: string;
  display_name: string;
  file_name?: string;
  size?: number;
  version?: number;
  source: string;
  source_url?: string;
  installed: boolean;
  active: boolean;
  offer_id?: string;
  drive?: string;
}

interface ContentSectionProps {
  game: Game;
}

function ContentSection({ game }: ContentSectionProps) {
  const [manifest, setManifest] = useState<{ dlcs: ContentItem[]; title_updates: ContentItem[] } | null>(null);
  const [loadingDlc, setLoadingDlc] = useState(false);
  const [loadingTu, setLoadingTu] = useState(false);
  const loading = loadingDlc || loadingTu;
  const [error, setError] = useState<string | null>(null);
  const [queuing, setQueuing] = useState<Record<string, boolean>>({});
  const [queueStatus, setQueueStatus] = useState<string | null>(null);
  const [queueStateMap, setQueueStateMap] = useState<Record<string, { state: string; message?: string }>>({});
  const refreshedForReady = useRef<Set<string>>(new Set());
  const [drives, setDrives] = useState<string[]>([]);
  const [defaultDrive, setDefaultDrive] = useState<string>("");
  const [showMoveFor, setShowMoveFor] = useState<string | null>(null);
  const [moving, setMoving] = useState<Record<string, boolean>>({});
  const [deleting, setDeleting] = useState<Record<string, boolean>>({});

  // Poll backend queue so buttons reflect Downloading / Ready state
  useEffect(() => {
    if (!manifest) return;
    let cancelled = false;
    const tick = async () => {
      try {
        const r: any = await window.godsendApi.getQueue();
        if (!r?.ok || !Array.isArray(r.jobs) || cancelled) return;
        const map: Record<string, { state: string; message?: string }> = {};
        for (const j of r.jobs) {
          const key: string = j.game || "";
          if (key) map[key] = { state: j.state, message: j.message };
        }
        setQueueStateMap(map);
      } catch { /* ignore */ }
    };
    tick();
    const id = setInterval(tick, 2000);
    return () => { cancelled = true; clearInterval(id); };
  }, [manifest, game.titleId]);

  // Auto-refresh installed content when a queued DLC/TU job reaches Ready
  useEffect(() => {
    if (!manifest) return;
    let shouldRefresh = false;
    for (const [k, v] of Object.entries(queueStateMap)) {
      if (v.state === "Ready" && !refreshedForReady.current.has(k)) {
        refreshedForReady.current.add(k);
        shouldRefresh = true;
      }
    }
    if (shouldRefresh) {
      const t = setTimeout(() => loadContent(), 1500);
      return () => clearTimeout(t);
    }
  }, [queueStateMap, manifest]);

  // Clear the Ready-tracking set when switching to a different game
  useEffect(() => {
    refreshedForReady.current.clear();
  }, [game.titleId]);

  // Fetch drives and default drive for move/delete operations
  useEffect(() => {
    window.godsendApi.getDefaultXboxDrive().then((r: any) => {
      if (r?.drive) setDefaultDrive(r.drive.replace(/:$/, ""));
    }).catch(() => {});
    window.godsendApi.listXboxDrives().then((r: any) => {
      if (r?.ok && Array.isArray(r.drives)) setDrives(r.drives.map((d: string) => d.replace(/:$/, "")));
    }).catch(() => {});
  }, []);

  function itemQueueState(item: ContentItem) {
    // If already installed on Xbox, always show Installed/Active —
    // do not let a stale "Ready" queue job render as "Downloaded".
    if (item.installed) {
      return item.active ? "Active" : "Installed";
    }
    const displayKey = `${item.display_name}`;
    const fileKey = item.file_name || "";
    for (const [k, v] of Object.entries(queueStateMap)) {
      if (k.includes(displayKey) || (fileKey && k.includes(fileKey))) {
        return v.state;
      }
    }
    return null;
  }

  useEffect(() => {
    setManifest(null);
    setError(null);
    loadContent();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [game.titleId]);

  // mergeTUs combines TU lists from /content/discover (installed) and
  // /content/tu (XboxUnity candidates), with installed entries taking
  // precedence. Without this merge the candidate list would mask a freshly
  // uploaded TU and the row would stay "Not installed".
  function mergeTUs(installed: ContentItem[], candidates: ContentItem[]): ContentItem[] {
    const result = [...installed];
    for (const c of candidates) {
      const dup = result.some((i) =>
        (!!i.file_name && !!c.file_name && i.file_name.toLowerCase() === c.file_name.toLowerCase()) ||
        (!!i.version && !!c.version && i.version === c.version && i.title_id === c.title_id)
      );
      if (!dup) result.push(c);
    }
    return result;
  }

  async function loadContent() {
    setLoadingDlc(true);
    setLoadingTu(true);
    setError(null);
    // Seed the manifest so each section can render independently as its
    // endpoint resolves — otherwise a slow FTP scan blocks the TU list.
    setManifest({ dlcs: [], title_updates: [] });

    // Track each half so a late-arriving response can merge against the other.
    let installedTus: ContentItem[] = [];
    let candidateTus: ContentItem[] = [];

    window.godsendApi
      .contentDiscover({ titleId: game.titleId, gameName: game.name, drive: game.sourceDrive })
      .then((dlcRes: any) => {
        const dlcs = (dlcRes?.ok ? dlcRes.dlcs : []) || [];
        installedTus = (dlcRes?.ok ? dlcRes.title_updates : []) || [];
        setManifest({ dlcs, title_updates: mergeTUs(installedTus, candidateTus) });
        if (!dlcRes?.ok && dlcRes?.error) setError(dlcRes.error);
      })
      .catch((err: any) => setError(err?.message || "Failed to load DLC"))
      .finally(() => setLoadingDlc(false));

    window.godsendApi
      .contentTitleUpdates({ titleId: game.titleId })
      .then((tuRes: any) => {
        candidateTus = (tuRes?.ok ? tuRes.title_updates : []) || [];
        setManifest((prev) => ({ dlcs: prev?.dlcs ?? [], title_updates: mergeTUs(installedTus, candidateTus) }));
      })
      .catch(() => { /* TU failures are non-fatal; DLC error already surfaces */ })
      .finally(() => setLoadingTu(false));
  }

  const [togglingActive, setTogglingActive] = useState<Record<string, boolean>>({});

  async function handleToggleActive(item: ContentItem, setActive: boolean) {
    const key = `${item.content_type}-${item.display_name}-${item.file_name}`;
    const drive = item.drive || game.sourceDrive || defaultDrive;
    if (!item.installed || !item.file_name || !drive) return;
    setTogglingActive((prev) => ({ ...prev, [key]: true }));
    try {
      const r = await window.godsendApi.contentSetActive({
        titleId: game.titleId,
        contentType: item.content_type,
        fileName: item.file_name,
        drive: `${drive.replace(/:$/, "")}:`,
        setActive,
      });
      if (r?.ok) {
        loadContent();
      } else {
        setQueueStatus(`Activate failed: ${r?.error || "Unknown error"}`);
      }
    } catch (err: any) {
      setQueueStatus(`Activate error: ${err.message}`);
    } finally {
      setTogglingActive((prev) => ({ ...prev, [key]: false }));
    }
  }

  async function handleDelete(item: ContentItem) {
    const key = `${item.content_type}-${item.display_name}`;
    // Prefer the drive the scan actually found the item on. Falling back
    // to defaultDrive first (the old behaviour) sent the FTP DELE to the
    // wrong drive whenever the game lived on Usb0 but the default was Hdd1.
    const drive = item.drive || game.sourceDrive || defaultDrive;
    if (!item.installed || !item.file_name || !drive) return;
    setDeleting((prev) => ({ ...prev, [key]: true }));
    try {
      const remotePath = `/${drive}/Content/0000000000000000/${game.titleId}/${item.content_type}/${item.file_name}`;
      const r = await window.godsendApi.toolsFtpDelete(remotePath);
      if (r?.ok) {
        loadContent();
      } else {
        setQueueStatus(`Delete failed: ${r?.error || "Unknown error"}`);
      }
    } catch (err: any) {
      setQueueStatus(`Delete error: ${err.message}`);
    } finally {
      setDeleting((prev) => ({ ...prev, [key]: false }));
    }
  }

  async function handleMove(item: ContentItem, targetDrive: string) {
    const key = `${item.content_type}-${item.display_name}`;
    // Same drive-selection as handleDelete: trust the scan-recorded drive first.
    const srcDrive = item.drive || game.sourceDrive || defaultDrive;
    if (!item.installed || !item.file_name || !srcDrive || targetDrive === srcDrive) return;
    setMoving((prev) => ({ ...prev, [key]: true }));
    try {
      const from = `/${srcDrive}/Content/0000000000000000/${game.titleId}/${item.content_type}/${item.file_name}`;
      const to = `/${targetDrive}/Content/0000000000000000/${game.titleId}/${item.content_type}/${item.file_name}`;
      await window.godsendApi.toolsFtpMkdir(`/${targetDrive}/Content/0000000000000000/${game.titleId}/${item.content_type}`);
      const r = await window.godsendApi.toolsFtpRename({ from, to });
      if (r?.ok) {
        setShowMoveFor(null);
        loadContent();
      } else {
        setQueueStatus(`Move failed: ${r?.error || "Unknown error"}`);
      }
    } catch (err: any) {
      setQueueStatus(`Move error: ${err.message}`);
    } finally {
      setMoving((prev) => ({ ...prev, [key]: false }));
    }
  }

  async function handleQueue(item: ContentItem, source?: string) {
    const key = `${item.content_type}-${item.display_name}`;
    setQueuing((prev) => ({ ...prev, [key]: true }));
    setQueueStatus(null);
    try {
      const payload = {
        game_name: game.name,
        title_id: game.titleId,
        content_type: item.content_type,
        display_name: item.display_name,
        file_name: item.file_name || item.display_name,
        source: source || item.source,
        source_url: item.source_url,
        drive: game.sourceDrive ? `${game.sourceDrive}:` : undefined,
      };
      const r = await window.godsendApi.contentQueue(payload);
      if (r?.ok) {
        setQueueStatus(`${item.display_name} queued for download.`);
        setTimeout(() => setQueueStatus(null), 3000);
      } else {
        setQueueStatus(`Queue failed: ${r?.error || "Unknown error"}`);
      }
    } catch (err: any) {
      setQueueStatus(`Queue error: ${err.message}`);
    } finally {
      setQueuing((prev) => ({ ...prev, [key]: false }));
    }
  }

  function handleQueueWithSourcePicker(item: ContentItem) {
    // If already installed, nothing to do
    if (item.installed) return;
    // If source has a direct URL, queue immediately
    if (item.source_url) {
      handleQueue(item);
      return;
    }
    // Otherwise open a source picker (simplified inline flow)
    (async () => {
      const key = `${item.content_type}-${item.display_name}`;
      setQueuing((prev) => ({ ...prev, [key]: true }));
      try {
        const s = await window.godsendApi.contentSources({ titleId: game.titleId, gameName: game.name });
        if (s?.ok && Array.isArray(s.sources) && s.sources.length > 0) {
          // For simplicity, queue the first source found
          const src = s.sources[0];
          await handleQueue(item, src.source);
        } else {
          setQueueStatus(`No download sources found for ${item.display_name}`);
        }
      } catch (err: any) {
        setQueueStatus(`Source lookup failed: ${err.message}`);
      } finally {
        setQueuing((prev) => ({ ...prev, [key]: false }));
      }
    })();
  }

  const hasContent = manifest && (manifest.dlcs.length > 0 || manifest.title_updates.length > 0);

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <p className="text-[10.5px] font-semibold text-muted-foreground uppercase tracking-wider">
          DLC & Title Updates
        </p>
        <button
          className="text-muted-foreground hover:text-foreground shrink-0"
          onClick={loadContent}
          disabled={loading}
          title="Refresh content list"
        >
          <RotateCw className={cn("h-3 w-3", loading && "animate-spin")} />
        </button>
      </div>

      {error && (
        <p className="text-[10px] text-red-400">{error}</p>
      )}

      {queueStatus && (
        <p className={cn(
          "text-[10px] px-2 py-1 rounded",
          queueStatus.startsWith("Queue failed") || queueStatus.startsWith("Queue error") || queueStatus.startsWith("No download sources")
            ? "bg-destructive/20 text-destructive"
            : "bg-emerald-500/15 text-emerald-300"
        )}>
          {queueStatus}
        </p>
      )}

      {!loading && !error && !hasContent && (
        <div className="rounded-md border border-border/40 bg-muted/10 px-3 py-3 text-center">
          <p className="text-[11px] text-muted-foreground">
            No DLC or Title Updates found for this title.
          </p>
        </div>
      )}

      {/* Title Updates */}
      {(loadingTu || (manifest && manifest.title_updates.length > 0)) && (
        <div className="rounded-md border border-border/60 bg-muted/20 px-2.5 py-2 space-y-2">
          <div className="flex items-center gap-1.5">
            <PackageOpen className="h-3 w-3 text-muted-foreground" />
            <p className="text-[10px] font-semibold text-muted-foreground">Title Updates</p>
            {loadingTu && <Loader2 className="h-2.5 w-2.5 animate-spin text-muted-foreground" />}
          </div>
          {loadingTu && (!manifest || manifest.title_updates.length === 0) && (
            <p className="text-[10px] text-muted-foreground">Checking XboxUnity…</p>
          )}
          <div className="flex flex-col gap-1.5">
            {manifest.title_updates.map((tu) => {
              const key = `${tu.content_type}-${tu.display_name}`;
              const isQueuing = queuing[key];
              return (
                <div
                  key={key}
                  className={cn(
                    "flex items-center justify-between rounded border px-2 py-1.5",
                    tu.installed ? "border-border/40 bg-background" : "border-border/60 bg-[#0d1117]"
                  )}
                >
                  <div className="flex flex-col min-w-0">
                    <span className="text-[11px] text-foreground truncate">{tu.display_name}</span>
                    <span className="text-[9px] text-muted-foreground font-mono">
                      {tu.installed ? (tu.active ? "Active" : "Installed") : "Not installed"}
                      {tu.size ? ` · ${(tu.size / 1048576).toFixed(1)} MB` : ""}
                      {tu.source ? ` · ${tu.source}` : ""}
                    </span>
                  </div>
                  <div className="shrink-0 ml-2">
                    {(() => {
                      const qs = itemQueueState(tu);
                      if (tu.installed) {
                        const isDeleting = deleting[key];
                        const isMoving = moving[key];
                        return (
                          <div className="flex items-center gap-1">
                            {(() => {
                              const tKey = `${tu.content_type}-${tu.display_name}-${tu.file_name}`;
                              const isToggling = togglingActive[tKey];
                              return tu.active ? (
                                <button
                                  className="text-[9px] px-1.5 py-0.5 rounded bg-primary/20 text-primary hover:bg-primary/30 transition-colors"
                                  onClick={() => handleToggleActive(tu, false)}
                                  disabled={isToggling || isDeleting || isMoving}
                                  title="Click to deactivate"
                                >
                                  {isToggling ? "…" : "Active"}
                                </button>
                              ) : (
                                <button
                                  className="text-[9px] px-1.5 py-0.5 rounded border border-border text-muted-foreground hover:text-foreground hover:border-primary/40 transition-colors"
                                  onClick={() => handleToggleActive(tu, true)}
                                  disabled={isToggling || isDeleting || isMoving}
                                >
                                  {isToggling ? "…" : "Make Active"}
                                </button>
                              );
                            })()}
                            <button
                              className="text-muted-foreground hover:text-destructive transition-colors disabled:opacity-40"
                              onClick={() => handleDelete(tu)}
                              disabled={isDeleting || isMoving}
                              title="Delete"
                            >
                              <Trash2 className="h-3 w-3" />
                            </button>
                            {showMoveFor === key ? (
                              <select
                                className="text-[9px] bg-background border border-border rounded px-1 py-0.5"
                                value=""
                                onChange={(e) => {
                                  if (e.target.value) handleMove(tu, e.target.value);
                                  e.target.value = "";
                                }}
                                disabled={isMoving}
                              >
                                <option value="">To…</option>
                                {drives.filter((d) => d !== (defaultDrive || game.sourceDrive)).map((d) => (
                                  <option key={d} value={d}>{d}</option>
                                ))}
                              </select>
                            ) : (
                              <button
                                className="text-muted-foreground hover:text-foreground transition-colors disabled:opacity-40"
                                onClick={() => setShowMoveFor(key)}
                                disabled={isDeleting || isMoving}
                                title="Move"
                              >
                                <ArrowRightLeft className="h-3 w-3" />
                              </button>
                            )}
                          </div>
                        );
                      }
                      if (qs === "Ready" || qs === "Error") {
                        return (
                          <span className={cn(
                            "text-[9px] px-1.5 py-0.5 rounded",
                            qs === "Error" ? "bg-destructive/20 text-destructive" : "bg-emerald-500/15 text-emerald-300"
                          )}>
                            {qs === "Error" ? "Error" : "Downloaded"}
                          </span>
                        );
                      }
                      return (
                        <button
                          className="text-[9px] px-1.5 py-0.5 rounded border border-border text-muted-foreground hover:text-foreground hover:border-primary/40 transition-colors flex items-center gap-0.5"
                          onClick={() => handleQueueWithSourcePicker(tu)}
                          disabled={isQueuing || qs === "Queued" || qs === "Processing"}
                        >
                          {isQueuing || qs === "Queued" || qs === "Processing" ? (
                            <Loader2 className="h-2.5 w-2.5 animate-spin" />
                          ) : (
                            <Download className="h-2.5 w-2.5" />
                          )}
                          {qs === "Queued" || qs === "Processing" ? "Downloading…" : "Queue"}
                        </button>
                      );
                    })()}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* DLCs */}
      {(loadingDlc || (manifest && manifest.dlcs.length > 0)) && (
        <div className="rounded-md border border-border/60 bg-muted/20 px-2.5 py-2 space-y-2">
          <div className="flex items-center gap-1.5">
            <Puzzle className="h-3 w-3 text-muted-foreground" />
            <p className="text-[10px] font-semibold text-muted-foreground">DLC</p>
            {loadingDlc && <Loader2 className="h-2.5 w-2.5 animate-spin text-muted-foreground" />}
          </div>
          {loadingDlc && (!manifest || manifest.dlcs.length === 0) && (
            <p className="text-[10px] text-muted-foreground">Scanning Xbox and Minerva/IA…</p>
          )}
          <div className="flex flex-col gap-1.5">
            {manifest.dlcs.map((dlc) => {
              const key = `${dlc.content_type}-${dlc.display_name}`;
              const isQueuing = queuing[key];
              return (
                <div
                  key={key}
                  className={cn(
                    "flex items-center justify-between rounded border px-2 py-1.5",
                    dlc.installed ? "border-border/40 bg-background" : "border-border/60 bg-[#0d1117]"
                  )}
                >
                  <div className="flex flex-col min-w-0">
                    <span className="text-[11px] text-foreground truncate">{dlc.display_name}</span>
                    <span className="text-[9px] text-muted-foreground font-mono">
                      {dlc.installed ? "Installed" : "Not installed"}
                      {dlc.size ? ` · ${(dlc.size / 1048576).toFixed(1)} MB` : ""}
                      {dlc.source ? ` · ${dlc.source}` : ""}
                    </span>
                  </div>
                  <div className="shrink-0 ml-2">
                    {(() => {
                      const qs = itemQueueState(dlc);
                      if (dlc.installed) {
                        const isDeleting = deleting[key];
                        const isMoving = moving[key];
                        return (
                          <div className="flex items-center gap-1">
                            <span className="text-[9px] px-1.5 py-0.5 rounded bg-emerald-500/15 text-emerald-300">Installed</span>
                            <button
                              className="text-muted-foreground hover:text-destructive transition-colors disabled:opacity-40"
                              onClick={() => handleDelete(dlc)}
                              disabled={isDeleting || isMoving}
                              title="Delete"
                            >
                              <Trash2 className="h-3 w-3" />
                            </button>
                            {showMoveFor === key ? (
                              <select
                                className="text-[9px] bg-background border border-border rounded px-1 py-0.5"
                                value=""
                                onChange={(e) => {
                                  if (e.target.value) handleMove(dlc, e.target.value);
                                  e.target.value = "";
                                }}
                                disabled={isMoving}
                              >
                                <option value="">To…</option>
                                {drives.filter((d) => d !== (defaultDrive || game.sourceDrive)).map((d) => (
                                  <option key={d} value={d}>{d}</option>
                                ))}
                              </select>
                            ) : (
                              <button
                                className="text-muted-foreground hover:text-foreground transition-colors disabled:opacity-40"
                                onClick={() => setShowMoveFor(key)}
                                disabled={isDeleting || isMoving}
                                title="Move"
                              >
                                <ArrowRightLeft className="h-3 w-3" />
                              </button>
                            )}
                          </div>
                        );
                      }
                      if (qs === "Ready" || qs === "Error") {
                        return (
                          <span className={cn(
                            "text-[9px] px-1.5 py-0.5 rounded",
                            qs === "Error" ? "bg-destructive/20 text-destructive" : "bg-emerald-500/15 text-emerald-300"
                          )}>
                            {qs === "Error" ? "Error" : "Downloaded"}
                          </span>
                        );
                      }
                      return (
                        <button
                          className="text-[9px] px-1.5 py-0.5 rounded border border-border text-muted-foreground hover:text-foreground hover:border-primary/40 transition-colors flex items-center gap-0.5"
                          onClick={() => handleQueueWithSourcePicker(dlc)}
                          disabled={isQueuing || qs === "Queued" || qs === "Processing"}
                        >
                          {isQueuing || qs === "Queued" || qs === "Processing" ? (
                            <Loader2 className="h-2.5 w-2.5 animate-spin" />
                          ) : (
                            <Download className="h-2.5 w-2.5" />
                          )}
                          {qs === "Queued" || qs === "Processing" ? "Downloading…" : "Queue"}
                        </button>
                      );
                    })()}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

// ── Disc face (shown on hover flip) ─────────────────────────────────────────

function DiscFace() {
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-[#07090d]">
      <div
        style={{
          width: "78%",
          aspectRatio: "1 / 1",
          borderRadius: "50%",
          position: "relative",
          flexShrink: 0,
          background:
            "radial-gradient(circle at 32% 28%, #f4f4f4, #bdbdbd 36%, #808080 68%, #484848 100%)",
          boxShadow: "0 6px 30px rgba(0,0,0,0.95), 0 2px 8px rgba(0,0,0,0.7)",
        }}
      >
        {/* Iridescent shimmer ring */}
        <div
          style={{
            position: "absolute", inset: "3.5%", borderRadius: "50%",
            background:
              "conic-gradient(from 195deg, rgba(255,80,80,.22), rgba(80,230,130,.22), rgba(80,120,255,.22), rgba(255,210,60,.22), rgba(255,80,200,.22), rgba(255,80,80,.22))",
          }}
        />
        {/* Green label area */}
        <div
          style={{
            position: "absolute", top: "50%", left: "50%",
            transform: "translate(-50%,-50%)",
            width: "66%", aspectRatio: "1 / 1", borderRadius: "50%",
            background:
              "radial-gradient(circle at 38% 34%, #247a44 0%, #0d4020 52%, #050d09 100%)",
            display: "flex", alignItems: "center", justifyContent: "center",
            flexDirection: "column", gap: "3px",
          }}
        >
          {/* Xbox sphere outline */}
          <svg width="22" height="22" viewBox="0 0 22 22" style={{ opacity: 0.65 }}>
            <circle cx="11" cy="11" r="9.5" fill="none" stroke="rgba(255,255,255,.28)" strokeWidth="0.8"/>
            <ellipse cx="11" cy="11" rx="6" ry="9.5" fill="none" stroke="rgba(255,255,255,.28)" strokeWidth="0.8"/>
            <ellipse cx="11" cy="11" rx="9.5" ry="3.8" fill="none" stroke="rgba(255,255,255,.28)" strokeWidth="0.8"/>
          </svg>
          <span style={{ fontSize: "3.8px", color: "rgba(255,255,255,.42)", letterSpacing: "1.6px", fontFamily: "sans-serif", textTransform: "uppercase" }}>
            Xbox 360
          </span>
        </div>
        {/* Centre hub */}
        <div
          style={{
            position: "absolute", top: "50%", left: "50%",
            transform: "translate(-50%,-50%)",
            width: "14%", aspectRatio: "1 / 1", borderRadius: "50%",
            background: "#111", border: "0.5px solid #3a3a3a", zIndex: 2,
          }}
        />
        {/* Gloss */}
        <div
          style={{
            position: "absolute", inset: 0, borderRadius: "50%",
            background: "linear-gradient(135deg, rgba(255,255,255,.3) 0%, rgba(255,255,255,.05) 40%, transparent 100%)",
            pointerEvents: "none",
          }}
        />
      </div>
    </div>
  );
}

// ── Detail view ───────────────────────────────────────────────────────────────

interface GameDetailProps {
  game: Game;
  coverDataUrl?: string;
  titleVisuals?: TitleVisuals | null;
  isOnSource: boolean;
  onBack: () => void;
  onRefresh?: () => void;
  refreshBusy?: boolean;
  onRxeaCover?: (gameDataDir: string | undefined, titleId: string, src: string) => void;
}

function GameDetail({
  game,
  coverDataUrl,
  titleVisuals,
  isOnSource,
  onBack,
  onRefresh,
  refreshBusy,
  onRxeaCover,
}: GameDetailProps) {
  const [rxeaSlots, setRxeaSlots]     = useState<Record<string, AssetSlot> | null>(null);
  const [rxeaLoading, setRxeaLoading] = useState(false);
  const [coverFlipped, setCoverFlipped] = useState(false);

  // ── Move to Drive state ─────────────────────────────────────────────────
  const [drives, setDrives]               = useState<string[]>([]);
  const [drivesLoading, setDrivesLoading] = useState(false);
  const [moveTarget, setMoveTarget]       = useState<string | null>(null);
  const [moveStatus, setMoveStatus]       = useState<null | "moving" | "done" | "error">(null);
  const [moveError, setMoveError]         = useState<string | null>(null);
  const [moveMessage, setMoveMessage]     = useState<string | null>(null);
  const [moveJobId, setMoveJobId]         = useState<number | null>(null);
  const [moveProgress, setMoveProgress]   = useState<number>(0);
  const [moveDetail, setMoveDetail]       = useState<string | null>(null);
  const [moveSpeed, setMoveSpeed]         = useState<string | null>(null);

  // Pending move = a successful move whose new location Aurora hasn't picked
  // up yet. Persisted so the notice survives navigation/reloads until Aurora
  // rescans content on the console and our next library load reflects it.
  const pendingMoveKey = `godsend.pendingMove.${game.titleId}.${game.mediaId ?? ""}`;
  const [pendingMoveDrive, setPendingMoveDrive] = useState<string | null>(() => {
    try { return localStorage.getItem(pendingMoveKey); } catch { return null; }
  });

  // Clear the pending notice once Aurora's reported drive matches the target.
  useEffect(() => {
    if (!pendingMoveDrive) return;
    if (game.sourceDrive && game.sourceDrive.toLowerCase() === pendingMoveDrive.toLowerCase()) {
      setPendingMoveDrive(null);
      try { localStorage.removeItem(pendingMoveKey); } catch {}
    }
  }, [pendingMoveDrive, game.sourceDrive, pendingMoveKey]);

  // Re-read pending state when switching games.
  useEffect(() => {
    try { setPendingMoveDrive(localStorage.getItem(pendingMoveKey)); } catch {}
  }, [pendingMoveKey]);

  useEffect(() => {
    // Re-read manifest from disk (no FTP).
    window.godsendApi
      .refreshTitleVisualsFromCache({
        titleId:     game.titleId,
        gameDataDir: game.gameDataDir,
      })
      .catch(() => {});

    // On-demand RXEA decode
    if (!game.gameDataDir) return;
    setRxeaSlots(null);
    setRxeaLoading(true);
    window.godsendApi
      .decodeAsset({ titleId: game.titleId, gameDataDir: game.gameDataDir })
      .then((r: any) => {
        if (r?.ok && Array.isArray(r.slots) && r.slots.length > 0) {
          const map: Record<string, AssetSlot> = {};
          for (const s of r.slots) {
            map[s.key] = { src: s.dataUrl, ext: ".png" };
          }
          setRxeaSlots(map);
          const coverSlot = r.slots.find((s: any) => s.key === "cover");
          if (coverSlot?.dataUrl) onRxeaCover?.(game.gameDataDir, game.titleId, coverSlot.dataUrl);
        } else {
          setRxeaSlots({});
        }
      })
      .catch(() => setRxeaSlots({}))
      .finally(() => setRxeaLoading(false));
  }, [game.titleId, game.gameDataDir]);

  // ── Restore in-progress move job from backend on mount ──────────────────
  useEffect(() => {
    window.godsendApi.toolsFtpUploadStatus().then((r: any) => {
      if (!r?.ok || !Array.isArray(r.jobs)) return;
      const movePrefix = `Move: ${game.name}`;
      const matchingJobs = r.jobs.filter((j: any) => j.name.startsWith(movePrefix));
      if (matchingJobs.length === 0) return;

      const activeJob = matchingJobs.find(
        (j: any) => j.state === "Queued" || j.state === "Processing"
      );
      if (activeJob) {
        setMoveJobId(activeJob.id);
        setMoveStatus("moving");
        setMoveProgress(activeJob.progress ?? 0);
        setMoveDetail(activeJob.detail || null);
        setMoveSpeed(activeJob.speed || null);
        setMoveMessage(`${activeJob.state}… ${activeJob.progress ?? 0}%`);
        return;
      }
      const errorJob = matchingJobs.find((j: any) => j.state === "Error");
      if (errorJob) {
        setMoveJobId(errorJob.id);
        setMoveStatus("error");
        setMoveError(errorJob.error || "Move failed.");
        return;
      }
      const doneJob = matchingJobs.find((j: any) => j.state === "Ready");
      if (doneJob) {
        setMoveJobId(doneJob.id);
        setMoveStatus("done");
        setMoveProgress(100);
        setMoveMessage("Move completed successfully.");
      }
    }).catch(() => {});
  }, [game.name]);

  // ── Load available Xbox drives ────────────────────────────────────────
  useEffect(() => {
    setDrivesLoading(true);
    window.godsendApi.listXboxDrives()
      .then((r: any) => {
        if (r?.ok && Array.isArray(r.drives)) {
          setDrives(r.drives);
        }
      })
      .catch(() => {})
      .finally(() => setDrivesLoading(false));
  }, []);

  async function handleMoveGame() {
    if (!moveTarget || moveStatus === "moving") return;
    setMoveStatus("moving");
    setMoveError(null);
    setMoveMessage(null);
    setMoveJobId(null);
    setMoveProgress(0);
    setMoveDetail(null);
    setMoveSpeed(null);
    try {
      const r = await window.godsendApi.moveGameToDrive({ game, targetDrive: moveTarget });
      if (r?.ok) {
        setMoveJobId(r.jobId ?? null);
        setMoveMessage(r.message || "Move queued successfully.");
        // Don't set "done" yet — let the polling effect handle final state.
        // If no jobId was returned, fall back to the old "done" behaviour.
        if (!r.jobId) setMoveStatus("done");
      } else {
        setMoveStatus("error");
        setMoveError(r?.error || "Failed to queue move.");
      }
    } catch (err: any) {
      setMoveStatus("error");
      setMoveError(err.message || "Unknown error");
    }
  }

  // Poll the FTP job status while a move is in progress
  useEffect(() => {
    if (moveJobId == null || moveStatus === "done" || moveStatus === "error") return;
    const id = setInterval(async () => {
      try {
        const r = await window.godsendApi.toolsFtpUploadStatus();
        if (!r?.ok) return;
        const job = (r.jobs || []).find((j: any) => j.id === moveJobId);
        if (!job) return;
        setMoveProgress(job.progress ?? 0);
        setMoveDetail(job.detail || null);
        setMoveSpeed(job.speed || null);
        if (job.state === "Ready") {
          setMoveStatus("done");
          setMoveMessage("Move completed successfully.");
          setMoveProgress(100);
          setMoveDetail(null);
          setMoveSpeed(null);
          if (moveTarget) {
            setPendingMoveDrive(moveTarget);
            try { localStorage.setItem(pendingMoveKey, moveTarget); } catch {}
          }
          clearInterval(id);
        } else if (job.state === "Error") {
          setMoveStatus("error");
          setMoveError(job.error || "Move failed.");
          setMoveDetail(null);
          setMoveSpeed(null);
          clearInterval(id);
        } else {
          // Still in progress — build a rich status message
          const pct = job.progress ?? 0;
          const parts: string[] = [];
          if (job.detail) parts.push(job.detail);
          parts.push(`${pct}%`);
          if (job.speed) parts.push(`(${job.speed})`);
          setMoveMessage(parts.join(" — "));
        }
      } catch { /* ignore poll errors */ }
    }, 1500);
    return () => clearInterval(id);
  }, [moveJobId, moveStatus]);

  return (
    <div className="flex flex-col h-full">
      <header className="flex items-center gap-2.5 shrink-0 pb-3 border-b border-border">
        <Button size="icon" onClick={onBack} title="Back to library">
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <Button
          size="icon"
          variant="ghost"
          disabled={refreshBusy || typeof onRefresh !== "function"}
          onClick={() => onRefresh?.()}
          title="Refresh library cache from Xbox"
        >
          <RotateCw className={cn("h-4 w-4", refreshBusy && "animate-spin")} />
        </Button>
        <span className="text-[14px] font-semibold text-foreground truncate flex-1 min-w-0">
          {game.name}
        </span>
        {game.isFavorite && (
          <Star className="h-3.5 w-3.5 shrink-0 text-yellow-400 fill-yellow-400" />
        )}
      </header>

      <div className="flex-1 min-h-0 mt-3 overflow-y-auto overflow-x-hidden" style={{ scrollbarWidth: "thin" }}>
        <div className="flex flex-col gap-4 pb-4 pr-1">

          {/* Cover + core info row */}
          <div className="flex gap-4">
            {(() => {
              const rxeaCoverSrc = rxeaSlots?.cover?.src || null;
              const tvCoverSrc   = titleVisuals?.cover?.src || null;
              const tvIsBooklet  = titleVisuals?.coverIsBooklet === true;
              // RXEA decoded > already-fetched titleVisuals cover > online cover
              const isBooklet    = !!rxeaCoverSrc || (tvIsBooklet && !!tvCoverSrc);
              const activeSrc    = rxeaCoverSrc || tvCoverSrc || coverDataUrl;
              return (
                <div
                  className="shrink-0"
                  onMouseEnter={() => setCoverFlipped(true)}
                  onMouseLeave={() => setCoverFlipped(false)}
                >
                  <XboxBoxCover
                    bookletSrc={isBooklet ? activeSrc : null}
                    frontSrc={!isBooklet ? activeSrc : null}
                    width={120}
                    height={160}
                    flipped={coverFlipped}
                    greyed={!isOnSource}
                  />
                </div>
              );
            })()}

            <div className="flex flex-col gap-1.5 min-w-0 flex-1">
              <p className="text-[14px] font-bold text-foreground leading-tight">{game.name}</p>
              <StarRating rating={game.liveRating} raters={game.liveRaters} />

              {!isOnSource && (
                <span className="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground self-start">
                  Not on selected drive
                </span>
              )}

              <div className="flex flex-col gap-1 mt-1">
                <MetaRow label="Publisher"  value={game.publisher} />
                <MetaRow label="Developer"  value={game.developer} />
                <MetaRow label="Released"   value={game.releaseDate} />
                {game.discsInSet !== undefined && game.discsInSet > 1 && (
                  <MetaRow label="Disc" value={`${game.discNum} of ${game.discsInSet}`} />
                )}
                <MetaRow
                  label="Game path"
                  value={
                    game.sourceDrive
                      ? `${game.sourceDrive}:${game.directory || ""}`
                      : game.directory || undefined
                  }
                />
                {pendingMoveDrive && (
                  <p className="text-[10px] text-amber-400 leading-snug -mt-0.5">
                    Moved to {pendingMoveDrive}: — path will refresh after Aurora rescans content on the console.
                  </p>
                )}
                <MetaRow
                  label="Asset path"
                  value={game.gameDataDir ? `Aurora/Data/GameData/${game.gameDataDir}/` : undefined}
                />
                {game.timesPlayed !== undefined && game.timesPlayed > 0 && (
                  <MetaRow label="Played" value={`${game.timesPlayed}×${game.lastPlayed ? ` · Last ${game.lastPlayed}` : ""}`} />
                )}
              </div>
            </div>
          </div>

          {/* Library database fields */}
          <div>
            <p className="text-[10.5px] font-semibold text-muted-foreground uppercase tracking-wider mb-1.5">
              Library database
            </p>
            <div className="flex flex-col gap-1 rounded-md border border-border/60 bg-muted/20 px-2.5 py-2">
              <MetaRow label="Scan path ID" value={game.scanPathId != null ? String(game.scanPathId) : undefined} />
              <MetaRow label="Media ID"     value={game.mediaId    != null ? String(game.mediaId)    : undefined} />
              <MetaRow label="File type"    value={game.fileType   != null ? String(game.fileType)   : undefined} />
              <MetaRow label="Content type" value={game.contentType != null ? String(game.contentType) : undefined} />
              <MetaRow label="Directory"    value={game.directory  || undefined} />
            </div>
          </div>

          {game.description && (
            <div>
              <p className="text-[10.5px] font-semibold text-muted-foreground uppercase tracking-wider mb-1.5">
                Description
              </p>
              <p className="text-[11.5px] text-foreground/85 leading-relaxed whitespace-pre-line">
                {game.description}
              </p>
            </div>
          )}

          {/* ── Asset editor ── */}
          <AssetEditorSection
            game={game}
            titleVisuals={titleVisuals}
            rxeaSlots={rxeaSlots}
            rxeaLoading={rxeaLoading}
            onRefresh={onRefresh}
          />

          {/* ── Move to Drive ── */}
          {game.sourceDrive && game.directory && (
            <div>
              <p className="text-[10.5px] font-semibold text-muted-foreground uppercase tracking-wider mb-1.5">
                Move to Drive
              </p>
              <div className="flex flex-col gap-2 rounded-md border border-border/60 bg-muted/20 px-2.5 py-2">
                <div className="flex items-center gap-2">
                  <HardDrive className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                  <span className="text-[11px] text-muted-foreground">
                    Currently on: <span className="text-foreground font-medium">{game.sourceDrive}</span>
                  </span>
                </div>
                <div className="flex items-center gap-1.5 flex-wrap">
                  {drivesLoading ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
                  ) : (
                    drives
                      .filter((d) => {
                        const clean = d.replace(/:$/, "");
                        return clean !== game.sourceDrive;
                      })
                      .map((d) => {
                        const clean = d.replace(/:$/, "");
                        const isSelected = moveTarget === d;
                        return (
                          <Button
                            key={d}
                            size="sm"
                            variant={isSelected ? "default" : "outline"}
                            className={cn(
                              "h-7 text-[11px] px-2.5",
                              isSelected && "ring-1 ring-primary"
                            )}
                            onClick={() => {
                              setMoveTarget(isSelected ? null : d);
                              setMoveStatus(null);
                              setMoveError(null);
                              setMoveMessage(null);
                            }}
                            disabled={moveStatus === "moving"}
                          >
                            <HardDrive className="h-3 w-3 mr-1" />
                            {clean}
                          </Button>
                        );
                      })
                  )}
                  {drives.filter((d) => d.replace(/:$/, "") !== game.sourceDrive).length === 0 && !drivesLoading && (
                    <span className="text-[11px] text-muted-foreground">No other drives found.</span>
                  )}
                </div>
                {!moveTarget && moveStatus === "moving" && (
                  <div className="flex flex-col gap-0.5 mt-0.5">
                    <div className="flex items-center gap-2">
                      <Loader2 className="h-3 w-3 animate-spin text-primary" />
                      <span className="text-[11px] text-foreground font-medium">
                        {moveProgress > 0 ? `Moving… ${moveProgress}%` : "Moving…"}
                        {moveSpeed ? ` — ${moveSpeed}` : ""}
                      </span>
                    </div>
                    {moveDetail && (
                      <span className="text-[10px] text-muted-foreground ml-5 truncate max-w-[260px]">
                        {moveDetail}
                      </span>
                    )}
                  </div>
                )}
                {moveTarget && (
                  <div className="flex flex-col gap-0.5 mt-0.5">
                    <div className="flex items-center gap-2">
                      <Button
                        size="sm"
                        onClick={handleMoveGame}
                        disabled={moveStatus === "moving" || moveStatus === "done"}
                        className="gap-1"
                      >
                        {moveStatus === "moving" ? (
                          <Loader2 className="h-3 w-3 animate-spin" />
                        ) : moveStatus === "done" ? (
                          <Check className="h-3 w-3" />
                        ) : (
                          <ArrowRightLeft className="h-3 w-3" />
                        )}
                        {moveStatus === "moving"
                          ? (moveProgress > 0 ? `Moving… ${moveProgress}%` : "Moving…")
                          : moveStatus === "done" ? "Done"
                          : `Move to ${moveTarget.replace(/:$/, "")}`}
                      </Button>
                      {moveStatus === "moving" && moveSpeed && (
                        <span className="text-[10px] text-muted-foreground font-mono">
                          {moveSpeed}
                        </span>
                      )}
                      {moveStatus !== "moving" && moveStatus !== "done" && (
                        <span className="text-[10px] text-muted-foreground">
                          This will queue an FTP transfer job.
                        </span>
                      )}
                    </div>
                    {moveStatus === "moving" && moveDetail && (
                      <span className="text-[10px] text-muted-foreground ml-0 truncate max-w-[300px]">
                        {moveDetail}
                      </span>
                    )}
                  </div>
                )}
                {moveStatus === "done" && moveMessage && (
                  <p className="text-[11px] text-green-400">{moveMessage}</p>
                )}
                {moveStatus === "error" && moveError && (
                  <p className="text-[11px] text-red-400">{moveError}</p>
                )}
              </div>
            </div>
          )}

  {/* ── Content / DLC / Title Update section ── */}
          {game.titleId && (
            <ContentSection game={game} />
          )}

          <p className="text-[9px] text-muted-foreground/40 font-mono">
            TitleID: {game.titleId}
            {game.contentId ? `  ·  ContentID: ${game.contentId}` : ""}
          </p>
        </div>
      </div>
    </div>
  );
}

// ── Game card ─────────────────────────────────────────────────────────────────

interface GameCardProps {
  game: Game;
  coverDataUrl?: string | null;
  rxeaCover?: string | null;
  isOnSource: boolean;
  onClick: () => void;
}

function GameCard({ game, coverDataUrl, rxeaCover, isOnSource, onClick }: GameCardProps) {
  const [flipped, setFlipped] = useState(false);

  return (
    <button
      className={cn(
        "flex flex-col gap-1 select-none text-left rounded-lg",
        "focus:outline-none focus-visible:ring-1 focus-visible:ring-ring"
      )}
      onClick={onClick}
      onMouseEnter={() => setFlipped(true)}
      onMouseLeave={() => setFlipped(false)}
    >
      <div className="relative w-full" style={{ aspectRatio: "3/4" }}>
        {coverDataUrl === undefined && !rxeaCover ? (
          <div className="absolute inset-0 rounded-lg overflow-hidden bg-gradient-to-r from-muted via-accent/30 to-muted animate-pulse" />
        ) : (
          <div className="absolute inset-0 flex items-center justify-center">
            <XboxBoxCover
              bookletSrc={rxeaCover || null}
              frontSrc={!rxeaCover ? coverDataUrl : null}
              width="100%"
              height="100%"
              flipped={flipped}
              greyed={!isOnSource}
            />
          </div>
        )}

        <div className="absolute top-1 left-1 flex flex-col gap-0.5 z-10 pointer-events-none">
          {game.isFavorite && (
            <span className="flex items-center justify-center w-4 h-4 rounded-full bg-black/60">
              <Star className="h-2.5 w-2.5 text-yellow-400 fill-yellow-400" />
            </span>
          )}
          {game.discsInSet !== undefined && game.discsInSet > 1 && (
            <span className="flex items-center justify-center w-4 h-4 rounded-full bg-black/60">
              <Disc3 className="h-2.5 w-2.5 text-blue-400" />
            </span>
          )}
        </div>

        {!isOnSource && (
          <div className="absolute inset-x-0 bottom-0 bg-black/70 py-0.5 px-1 z-10 pointer-events-none rounded-b-lg">
            <p className="text-[8px] text-white/70 text-center truncate">
              {game.sourceDrive || "Not found"}
            </p>
          </div>
        )}
      </div>

      <div className="px-0.5 min-w-0">
        <p className={cn(
          "text-[11px] font-medium leading-tight line-clamp-2 break-words",
          isOnSource ? "text-foreground" : "text-muted-foreground/60"
        )}>
          {game.name}
        </p>
        <p className="text-[9.5px] text-muted-foreground/50 font-mono mt-0.5 tracking-wide">
          {game.titleId}
        </p>
      </div>
    </button>
  );
}

function CenteredOverlay({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex-1 flex flex-col items-center justify-center gap-3 text-muted-foreground">
      {children}
    </div>
  );
}

// ── Sort/filter helpers ───────────────────────────────────────────────────────

const SORT_OPTIONS = [
  { value: "name-asc",        label: "Name A–Z" },
  { value: "name-desc",       label: "Name Z–A" },
  { value: "rating-desc",     label: "Rating (high–low)" },
  { value: "rating-asc",      label: "Rating (low–high)" },
  { value: "last-played",     label: "Last played" },
  { value: "most-played",     label: "Most played" },
  { value: "drive",           label: "Drive" },
  { value: "favorites-first", label: "Favorites first" },
];

function sortAndFilterGames(games: Game[], query: string, sortKey: string, filterKey: string): Game[] {
  let list = games;

  // ── text filter ──
  if (query) {
    const q = query.toLowerCase();
    list = list.filter(
      (g) =>
        g.name.toLowerCase().includes(q) ||
        g.titleId.toLowerCase().includes(q) ||
        (g.publisher && g.publisher.toLowerCase().includes(q)) ||
        (g.developer && g.developer.toLowerCase().includes(q))
    );
  }

  // ── category filter ──
  if (filterKey === "favorites") {
    list = list.filter((g) => g.isFavorite);
  } else if (filterKey === "on-source") {
    list = list.filter((g) => Boolean(g.sourceDrive));
  } else if (filterKey === "multi-disc") {
    list = list.filter((g) => (g.discsInSet || 0) > 1);
  }

  // ── sort ──
  const sorted = [...list];
  switch (sortKey) {
    case "name-asc":
      sorted.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
      break;
    case "name-desc":
      sorted.sort((a, b) => b.name.localeCompare(a.name, undefined, { sensitivity: "base" }));
      break;
    case "rating-desc":
      sorted.sort((a, b) => (parseFloat(String(b.liveRating)) || 0) - (parseFloat(String(a.liveRating)) || 0));
      break;
    case "rating-asc":
      sorted.sort((a, b) => (parseFloat(String(a.liveRating)) || 0) - (parseFloat(String(b.liveRating)) || 0));
      break;
    case "last-played":
      sorted.sort((a, b) => {
        const da = a.lastPlayed || "";
        const db = b.lastPlayed || "";
        if (!da && !db) return 0;
        if (!da) return 1;
        if (!db) return -1;
        return db.localeCompare(da);
      });
      break;
    case "most-played":
      sorted.sort((a, b) => (b.timesPlayed || 0) - (a.timesPlayed || 0));
      break;
    case "drive":
      sorted.sort((a, b) => (a.sourceDrive || "zzz").localeCompare(b.sourceDrive || "zzz"));
      break;
    case "favorites-first":
      sorted.sort((a, b) => {
        if (a.isFavorite !== b.isFavorite) return a.isFavorite ? -1 : 1;
        return a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
      });
      break;
    default:
      break;
  }
  return sorted;
}

// ── Main library page ─────────────────────────────────────────────────────────

interface LibraryPageProps {
  status: string;
  games: Game[];
  covers: Record<string, string>;
  titleVisuals?: Record<string, TitleVisuals>;
  connectedTo?: string;
  onToggle: () => void;
  onRefresh?: () => void;
  refreshBusy?: boolean;
  ftpStatus: string;
  libraryLoading: boolean;
  queueJobs: any[];
  onReconnect: () => void;
  onNavigateQueue: () => void;
  onNavigateBrowse: () => void;
  onNavigateSettings: () => void;
  onNavigateIso2God: () => void;
  onNavigateIso2Xex: () => void;
  onNavigateFtpManager: () => void;
}

export default function LibraryPage({
  status,
  games,
  covers,
  titleVisuals = {},
  connectedTo,
  onToggle,
  onRefresh,
  refreshBusy = false,
  ftpStatus,
  libraryLoading,
  queueJobs,
  onReconnect,
  onNavigateQueue,
  onNavigateBrowse,
  onNavigateSettings,
  onNavigateIso2God,
  onNavigateIso2Xex,
  onNavigateFtpManager,
}: LibraryPageProps) {
  const [selectedGame, setSelectedGame] = useState<Game | null>(null);
  const [rxeaCovers, setRxeaCovers] = useState<Record<string, string>>({});

  // ── Sort / filter state ──
  const [searchQuery, setSearchQuery] = useState("");
  const [sortKey, setSortKey]         = useState("name-asc");
  const [filterKey, setFilterKey]     = useState("all");
  const [showSortMenu, setShowSortMenu]     = useState(false);
  const [showFilterMenu, setShowFilterMenu] = useState(false);
  const sortRef   = useRef<HTMLDivElement>(null);
  const filterRef = useRef<HTMLDivElement>(null);

  // ── Download Covers state ──
  const [dlCoversBusy, setDlCoversBusy]         = useState(false);
  const [dlCoversProgress, setDlCoversProgress]  = useState<{ processed: number; total: number; current: string } | null>(null);
  const [dlCoversToast, setDlCoversToast]        = useState<string | null>(null);

  // Listen for progress events while download-covers is running
  useEffect(() => {
    const cleanup = (window as any).godsendApi.onDownloadCoversProgress?.((data: any) => {
      if (data.done) {
        setDlCoversProgress(null);
      } else {
        setDlCoversProgress({ processed: data.processed, total: data.total, current: data.current || "" });
      }
    });
    return () => cleanup?.();
  }, []);

  async function handleDownloadCovers() {
    if (dlCoversBusy || games.length === 0) return;
    setDlCoversBusy(true);
    setDlCoversProgress({ processed: 0, total: games.length, current: "" });
    setDlCoversToast(null);
    try {
      await (window as any).godsendApi.downloadAllCovers({
        games: games.map((g: Game) => ({
          titleId: g.titleId,
          gameDataDir: g.gameDataDir,
          name: g.name,
        })),
      });
    } catch { /* errors logged server-side */ }
    setDlCoversBusy(false);
    setDlCoversProgress(null);
    setDlCoversToast("Refresh data in Aurora for changes to reflect");
    setTimeout(() => setDlCoversToast(null), 3000);
    setRxeaCovers({});
    onRefresh?.();
  }

  // Close dropdowns on outside click
  useEffect(() => {
    function onPointerDown(e: PointerEvent) {
      if (showSortMenu && sortRef.current && !sortRef.current.contains(e.target as Node)) setShowSortMenu(false);
      if (showFilterMenu && filterRef.current && !filterRef.current.contains(e.target as Node)) setShowFilterMenu(false);
    }
    document.addEventListener("pointerdown", onPointerDown, true);
    return () => document.removeEventListener("pointerdown", onPointerDown, true);
  }, [showSortMenu, showFilterMenu]);

  const filteredGames = status === "ready"
    ? sortAndFilterGames(games, searchQuery, sortKey, filterKey)
    : games;

  function handleRxeaCover(gameDataDir: string | undefined, titleId: string, src: string) {
    const key = gameDataDir || titleId;
    setRxeaCovers((prev) => (prev[key] === src ? prev : { ...prev, [key]: src }));
  }

  function isOnSource(game: Game) {
    return Boolean(game.sourceDrive);
  }

  if (selectedGame) {
    return (
      <div className="flex flex-col h-screen p-3 gap-2.5">
        <GameDetail
          game={selectedGame}
          coverDataUrl={covers[selectedGame.gameDataDir || selectedGame.titleId]}
          titleVisuals={titleVisuals[selectedGame.gameDataDir || selectedGame.titleId]}
          isOnSource={isOnSource(selectedGame)}
          onBack={() => setSelectedGame(null)}
          onRefresh={onRefresh}
          refreshBusy={refreshBusy}
          onRxeaCover={handleRxeaCover}
        />
      </div>
    );
  }

  const onSourceCount  = games.filter(isOnSource).length;
  const offSourceCount = games.length - onSourceCount;
  const favCount       = games.filter((g) => g.isFavorite).length;

  // Build unique drive list for the filter badge.
  const drives = [...new Set(games.map((g) => g.sourceDrive).filter((d): d is string => Boolean(d)))].sort();

  return (
    <div className="flex flex-col h-screen p-3 gap-2.5">

      <header className="flex flex-col shrink-0 gap-2 pb-3 border-b border-border">
        {/* Top row: title + actions */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2.5 min-w-0">
            <Gamepad2 className="h-[18px] w-[18px] text-primary shrink-0" />
            <span className="text-[15px] font-semibold text-foreground">Xbox Library</span>
            {status === "ready" && (
              <span className="text-[11px] text-muted-foreground truncate">
                {filteredGames.length !== games.length
                  ? `${filteredGames.length} / ${games.length}`
                  : games.length}{" "}
                game{(filteredGames.length !== games.length ? filteredGames.length : games.length) !== 1 ? "s" : ""}
                {offSourceCount > 0 && (
                  <span className="text-muted-foreground/50">
                    {" "}&middot;{" "}{offSourceCount} off-drive
                  </span>
                )}
                {connectedTo && <span className="text-muted-foreground/50"> &middot; {connectedTo}</span>}
              </span>
            )}
          </div>
          <div className="flex items-center gap-1.5 shrink-0">
            <Button
              size="icon"
              variant="ghost"
              title="Refresh library cache from Xbox"
              disabled={refreshBusy || dlCoversBusy || status !== "ready" || typeof onRefresh !== "function"}
              onClick={() => { setRxeaCovers({}); onRefresh?.(); }}
            >
              <RotateCw className={cn("h-4 w-4", refreshBusy && "animate-spin")} />
            </Button>
            <Button
              size="icon"
              variant="ghost"
              title="Download covers for all games missing artwork"
              disabled={dlCoversBusy || refreshBusy || status !== "ready"}
              onClick={handleDownloadCovers}
            >
              <Download className={cn("h-4 w-4", dlCoversBusy && "animate-pulse")} />
            </Button>
            <MainNav
              ftpStatus={ftpStatus}
              currentPage="library"
              libraryAvailable={true}
              libraryLoading={libraryLoading}
              queueJobs={queueJobs}
              onReconnect={onReconnect}
              onLibraryToggle={onToggle}
              onNavigateQueue={onNavigateQueue}
              onNavigateBrowse={onNavigateBrowse}
              onNavigateSettings={onNavigateSettings}
              onNavigateIso2God={onNavigateIso2God}
              onNavigateIso2Xex={onNavigateIso2Xex}
              onNavigateFtpManager={onNavigateFtpManager}
            />
          </div>
        </div>

        {/* Search + sort/filter toolbar — only when library is ready */}
        {status === "ready" && games.length > 0 && (
          <div className="flex items-center gap-1.5">
            {/* Search */}
            <div className="relative flex-1 min-w-0">
              <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground pointer-events-none" />
              <Input
                type="text"
                placeholder="Search games..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="h-7 pl-7 pr-7 text-[12px]"
                spellCheck={false}
              />
              {searchQuery && (
                <button
                  className="absolute right-1.5 top-1/2 -translate-y-1/2 p-0.5 rounded hover:bg-accent"
                  onClick={() => setSearchQuery("")}
                >
                  <X className="h-3 w-3 text-muted-foreground" />
                </button>
              )}
            </div>

            {/* Sort dropdown */}
            <div className="relative" ref={sortRef}>
              <Button
                size="sm"
                variant={sortKey !== "name-asc" ? "secondary" : "ghost"}
                className="h-7 text-[11px] px-2 gap-1"
                title="Sort by"
                onClick={() => { setShowSortMenu(!showSortMenu); setShowFilterMenu(false); }}
              >
                <ArrowUpDown className="h-3 w-3" />
                <span className="hidden sm:inline">{SORT_OPTIONS.find((o) => o.value === sortKey)?.label || "Sort"}</span>
              </Button>
              {showSortMenu && (
                <div className="absolute right-0 top-full mt-1 z-50 min-w-[160px] rounded-md border border-border bg-popover shadow-lg py-1">
                  {SORT_OPTIONS.map((opt) => (
                    <button
                      key={opt.value}
                      className={cn(
                        "w-full text-left px-3 py-1.5 text-[11px] hover:bg-accent",
                        sortKey === opt.value && "bg-accent/60 font-medium"
                      )}
                      onClick={() => { setSortKey(opt.value); setShowSortMenu(false); }}
                    >
                      {opt.label}
                    </button>
                  ))}
                </div>
              )}
            </div>

            {/* Filter dropdown */}
            <div className="relative" ref={filterRef}>
              <Button
                size="sm"
                variant={filterKey !== "all" ? "secondary" : "ghost"}
                className="h-7 text-[11px] px-2 gap-1"
                title="Filter"
                onClick={() => { setShowFilterMenu(!showFilterMenu); setShowSortMenu(false); }}
              >
                <Filter className="h-3 w-3" />
                <span className="hidden sm:inline">
                  {filterKey === "all" ? "Filter" : filterKey === "favorites" ? "Favorites" : filterKey === "on-source" ? "On-drive" : "Multi-disc"}
                </span>
              </Button>
              {showFilterMenu && (
                <div className="absolute right-0 top-full mt-1 z-50 min-w-[150px] rounded-md border border-border bg-popover shadow-lg py-1">
                  {[
                    { value: "all",        label: "All games",   count: games.length },
                    { value: "favorites",  label: "Favorites",   count: favCount },
                    { value: "on-source",  label: "On-drive",    count: onSourceCount },
                    { value: "multi-disc", label: "Multi-disc",  count: games.filter((g) => (g.discsInSet || 0) > 1).length },
                  ].map((opt) => (
                    <button
                      key={opt.value}
                      className={cn(
                        "w-full text-left px-3 py-1.5 text-[11px] hover:bg-accent flex justify-between items-center",
                        filterKey === opt.value && "bg-accent/60 font-medium"
                      )}
                      onClick={() => { setFilterKey(opt.value); setShowFilterMenu(false); }}
                    >
                      <span>{opt.label}</span>
                      <span className="text-muted-foreground text-[10px] ml-2">{opt.count}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </header>

      {/* Download Covers progress bar */}
      {dlCoversBusy && dlCoversProgress && (
        <div className="shrink-0 flex items-center gap-2 px-1">
          <Loader2 className="h-3.5 w-3.5 animate-spin text-primary shrink-0" />
          <div className="flex-1 min-w-0">
            <div className="flex items-center justify-between text-[10px] text-muted-foreground mb-0.5">
              <span className="truncate max-w-[200px]">{dlCoversProgress.current}</span>
              <span className="shrink-0 ml-2">{dlCoversProgress.processed}/{dlCoversProgress.total}</span>
            </div>
            <div className="h-1 rounded-full bg-muted overflow-hidden">
              <div
                className="h-full rounded-full bg-primary transition-all duration-300"
                style={{ width: `${Math.round((dlCoversProgress.processed / Math.max(dlCoversProgress.total, 1)) * 100)}%` }}
              />
            </div>
          </div>
        </div>
      )}

      {/* Download Covers toast */}
      {dlCoversToast && (
        <div className="shrink-0 px-2 py-1.5 rounded bg-emerald-500/15 text-emerald-300 text-[11px] text-center">
          {dlCoversToast}
        </div>
      )}

      {status === "connecting" && (
        <CenteredOverlay>
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
          <p className="text-[13px]">Loading Aurora library...</p>
        </CenteredOverlay>
      )}

      {status === "error" && (
        <CenteredOverlay>
          <WifiOff className="h-8 w-8 text-muted-foreground" />
          <p className="text-[13px]">Could not load Aurora library.</p>
          <Button size="sm" onClick={onToggle}>Back to console</Button>
        </CenteredOverlay>
      )}

      {status === "empty" && (
        <CenteredOverlay>
          <Gamepad2 className="h-8 w-8 text-muted-foreground" />
          <p className="text-[13px]">No games found in Aurora's library.</p>
          <Button size="sm" onClick={onToggle}>Back to console</Button>
        </CenteredOverlay>
      )}

      {status === "ready" && games.length > 0 && filteredGames.length === 0 && (
        <CenteredOverlay>
          <Search className="h-8 w-8 text-muted-foreground" />
          <p className="text-[13px]">No games match your search or filter.</p>
          <Button size="sm" onClick={() => { setSearchQuery(""); setFilterKey("all"); }}>
            Clear filters
          </Button>
        </CenteredOverlay>
      )}

      {status === "ready" && filteredGames.length > 0 && (
        <ScrollArea className="flex-1 min-h-0">
          <div
            className="grid gap-3 pb-4 pr-1"
            style={{ gridTemplateColumns: "repeat(auto-fill, minmax(110px, 1fr))" }}
          >
            {filteredGames.map((game) => {
              const coverKey = game.gameDataDir || game.titleId;
              const tv = titleVisuals[coverKey];
              const visualCover = tv?.cover?.src || null;
              const isBooklet = tv?.coverIsBooklet === true;
              return (
                <GameCard
                  key={`${game.titleId}-${game.contentId}`}
                  game={game}
                  coverDataUrl={
                    isBooklet ? covers[coverKey] : (visualCover || covers[coverKey])
                  }
                  rxeaCover={
                    rxeaCovers[coverKey] ||
                    (isBooklet ? visualCover : null)
                  }
                  isOnSource={isOnSource(game)}
                  onClick={() => setSelectedGame(game)}
                />
              );
            })}
          </div>
        </ScrollArea>
      )}
    </div>
  );
}
