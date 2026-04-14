import { useState, useEffect, useRef, useCallback } from "react";
import {
  ArrowLeft, Gamepad2, Loader2, WifiOff,
  Star, Disc3, RefreshCw, Upload, Search, X, Check, ChevronLeft, ChevronRight,
  ArrowUpDown, Filter,
} from "lucide-react";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { ScrollArea } from "./ui/scroll-area";
import { cn } from "../lib/utils";
import XboxBoxCover from "./XboxBoxCover";

// ── Helpers ───────────────────────────────────────────────────────────────────

function isWebImageExt(ext) {
  const e = String(ext || "").toLowerCase();
  return e === ".jpg" || e === ".jpeg" || e === ".png" || e === ".gif" || e === ".bmp" || e === ".webp";
}

function isDataUrl(s) {
  return typeof s === "string" && s.startsWith("data:");
}

function MetaRow({ label, value }) {
  if (value === undefined || value === null || value === "") return null;
  return (
    <div className="flex gap-2 min-w-0">
      <span className="text-[11px] text-muted-foreground shrink-0 w-[88px]">{label}</span>
      <span className="text-[11px] text-foreground break-words min-w-0">{String(value)}</span>
    </div>
  );
}

function StarRating({ rating, raters }) {
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

function ImageLightbox({ images, startIndex, onClose }) {
  const [idx, setIdx] = useState(startIndex ?? 0);
  const total = images.length;
  const prev = useCallback(() => setIdx((i) => (i - 1 + total) % total), [total]);
  const next = useCallback(() => setIdx((i) => (i + 1) % total), [total]);

  useEffect(() => {
    function onKey(e) {
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

function AssetSlotCard({
  slotKey, label, aspect, currentAsset, pendingAsset,
  onSearch, onUpload, onClearPending, onImageClick,
  objectFit = "cover", objectPosition = "center",
}) {
  const hasPending = !!pendingAsset;
  const displaySrc  = hasPending
    ? (pendingAsset.dataUrl || pendingAsset.previewUrl || pendingAsset.url || null)
    : (currentAsset?.src || null);
  const displayIsWeb = hasPending
    ? (pendingAsset.dataUrl ? true : isWebImageExt(".jpg"))
    : (currentAsset ? isWebImageExt(currentAsset.ext) : false);

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

function AssetSearchPanel({ game, targetSlot, onSelect, onClose }) {
  const [query, setQuery]       = useState(game.name);
  const [results, setResults]   = useState([]);
  const [loading, setLoading]   = useState(false);
  const [searched, setSearched] = useState(false);
  const inputRef = useRef(null);

  useEffect(() => {
    inputRef.current?.focus();
    // Auto-search by titleId on open.
    runSearch(game.name, game.titleId);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function runSearch(q, titleId) {
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

  function handleKeyDown(e) {
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
  const thumbWidth  = isWideSearch ? 120 : targetSlot === "icon" ? 68 : 68;

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
          {results.map((r, idx) => {
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

// Per-slot display overrides: { objectFit, objectPosition }
const SLOT_DISPLAY = {
  banner:  { objectFit: "contain", objectPosition: "center" },
  cover:   { objectFit: "cover",   objectPosition: "right center" },
};

function AssetEditorSection({ game, titleVisuals, rxeaSlots, rxeaLoading, onRefresh }) {
  const [pending, setPending]       = useState({});
  const [searchSlot, setSearchSlot] = useState(null);
  const [saving, setSaving]         = useState(false);
  const [saveMsg, setSaveMsg]       = useState("");
  const [saveMsgKind, setSaveMsgKind] = useState("ok"); // "ok" | "error"
  const [lightbox, setLightbox]     = useState(null); // { images: string[], idx: number }

  const hasPending = Object.keys(pending).length > 0;

  function currentAsset(slotKey) {
    // RXEA-decoded assets from the console take priority over CDN/manifest assets.
    if (rxeaSlots && rxeaSlots[slotKey]) return rxeaSlots[slotKey];
    if (!titleVisuals) return null;
    if (slotKey.startsWith("screenshot")) {
      const idx = parseInt(slotKey.replace("screenshot", ""), 10) - 1;
      return titleVisuals.screenshots?.[idx] || null;
    }
    return titleVisuals[slotKey] || null;
  }

  async function openSearch(slotKey) {
    setSaveMsg("");
    setSearchSlot(slotKey);
  }

  function handleSearchSelect(slotKey, result) {
    const url = result.front || result.thumbnail || result.url;
    if (!url) return;
    const previewUrl = result.thumbnail || result.front || url;

    if (isDataUrl(url)) {
      // Already a data URL (e.g. Xbox CDN results) — use directly at full resolution.
      setPending((prev) => ({ ...prev, [slotKey]: { url: null, dataUrl: url, previewUrl: url, ext: ".jpg" } }));
    } else {
      // HTTP URL — show the thumbnail immediately, then fetch the full image in background.
      setPending((prev) => ({ ...prev, [slotKey]: { url, dataUrl: null, previewUrl, ext: ".jpg" } }));
      (async () => {
        const fullUrl = result.front || result.url || url;
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

  async function handleUploadFile(slotKey) {
    setSaveMsg("");
    const r = await window.godsendApi.chooseAssetImageFile();
    if (!r || !r.ok) return;
    setPending((prev) => ({ ...prev, [slotKey]: { dataUrl: r.dataUrl, ext: r.ext || ".jpg" } }));
  }

  function clearPending(slotKey) {
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
      setSaveMsg(`${Object.keys(pending).length} asset(s) saved. Aurora will apply on next library scan.`);
      setSaveMsgKind("ok");
      setPending({});
      // Trigger a fresh FTP sync so the cached visuals update.
      setTimeout(() => onRefresh?.(), 400);
    }
  }

  // Current screenshot count (from RXEA decode, existing visuals, or pending).
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

  function slotSrc(key) {
    const p = pending[key];
    if (p) return p.dataUrl || p.previewUrl || p.url || null;
    return currentAsset(key)?.src || null;
  }

  // Build ordered screenshot image list for lightbox navigation.
  const screenshotSrcs = screenshotSlots.map((s) => slotSrc(s.key)).filter(Boolean);

  function openLightbox(images, idx) {
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
          Images are uploaded to{" "}
          <span className="font-mono">Aurora/User/Import/{game.titleId}/</span>{" "}
          via FTP. Aurora processes them on next library scan.
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

      {/* Empty state — only show when RXEA decode is done and nothing was found */}
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

function GameDetail({
  game,
  coverDataUrl,
  titleVisuals,
  isOnSource,
  onBack,
  onRefresh,
  refreshBusy,
  onRxeaCover,
}) {
  const [rxeaSlots, setRxeaSlots]     = useState(null);
  const [rxeaLoading, setRxeaLoading] = useState(false);
  const [coverFlipped, setCoverFlipped] = useState(false);

  useEffect(() => {
    // Re-read manifest from disk (no FTP).
    window.godsendApi
      .refreshTitleVisualsFromCache({
        titleId:     game.titleId,
        gameDataDir: game.gameDataDir,
      })
      .catch(() => {});

    // On-demand RXEA decode: FTP-fetch .asset files and decode them via the Go codec.
    // This runs regardless of whether the library loaded from cache or live FTP,
    // so decoded console art is always visible in game detail.
    if (!game.gameDataDir) return;
    setRxeaSlots(null);
    setRxeaLoading(true);
    window.godsendApi
      .decodeAsset({ titleId: game.titleId, gameDataDir: game.gameDataDir })
      .then((r) => {
        if (r?.ok && Array.isArray(r.slots) && r.slots.length > 0) {
          const map = {};
          for (const s of r.slots) {
            map[s.key] = { src: s.dataUrl, ext: ".png" };
          }
          setRxeaSlots(map);
          const coverSlot = r.slots.find((s) => s.key === "cover");
          if (coverSlot?.dataUrl) onRxeaCover?.(game.titleId, coverSlot.dataUrl);
        } else {
          setRxeaSlots({});
        }
      })
      .catch(() => setRxeaSlots({}))
      .finally(() => setRxeaLoading(false));
  }, [game.titleId, game.gameDataDir]);

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
          <RefreshCw className={cn("h-4 w-4", refreshBusy && "animate-spin")} />
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
              const activeSrc   = rxeaCoverSrc || coverDataUrl;
              const isRxea      = !!rxeaCoverSrc;
              return (
                <div
                  className="shrink-0"
                  onMouseEnter={() => setCoverFlipped(true)}
                  onMouseLeave={() => setCoverFlipped(false)}
                >
                  <XboxBoxCover
                    bookletSrc={isRxea ? activeSrc : null}
                    frontSrc={!isRxea ? activeSrc : null}
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
                {game.discsInSet > 1 && (
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
                <MetaRow
                  label="Asset path"
                  value={`Aurora/User/Import/${game.titleId}/`}
                />
                {game.timesPlayed > 0 && (
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

          {/* ── Asset editor (replaces WIP read-only section) ── */}
          <AssetEditorSection
            game={game}
            titleVisuals={titleVisuals}
            rxeaSlots={rxeaSlots}
            rxeaLoading={rxeaLoading}
            onRefresh={onRefresh}
          />

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

function GameCard({ game, coverDataUrl, rxeaCover, isOnSource, onClick }) {
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
          {game.discsInSet > 1 && (
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

function CenteredOverlay({ children }) {
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

function sortAndFilterGames(games, query, sortKey, filterKey) {
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
    list = list.filter((g) => g.discsInSet > 1);
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
      sorted.sort((a, b) => (parseFloat(b.liveRating) || 0) - (parseFloat(a.liveRating) || 0));
      break;
    case "rating-asc":
      sorted.sort((a, b) => (parseFloat(a.liveRating) || 0) - (parseFloat(b.liveRating) || 0));
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

export default function LibraryPage({
  status,
  games,
  covers,
  titleVisuals = {},
  connectedTo,
  librarySources,
  onToggle,
  onRefresh,
  refreshBusy = false,
}) {
  const [selectedGame, setSelectedGame] = useState(null);
  const [rxeaCovers, setRxeaCovers] = useState({});

  // ── Sort / filter state ──
  const [searchQuery, setSearchQuery] = useState("");
  const [sortKey, setSortKey]         = useState("name-asc");
  const [filterKey, setFilterKey]     = useState("all");
  const [showSortMenu, setShowSortMenu]     = useState(false);
  const [showFilterMenu, setShowFilterMenu] = useState(false);
  const sortRef   = useRef(null);
  const filterRef = useRef(null);

  // Close dropdowns on outside click
  useEffect(() => {
    function onPointerDown(e) {
      if (showSortMenu && sortRef.current && !sortRef.current.contains(e.target)) setShowSortMenu(false);
      if (showFilterMenu && filterRef.current && !filterRef.current.contains(e.target)) setShowFilterMenu(false);
    }
    document.addEventListener("pointerdown", onPointerDown, true);
    return () => document.removeEventListener("pointerdown", onPointerDown, true);
  }, [showSortMenu, showFilterMenu]);

  const filteredGames = status === "ready"
    ? sortAndFilterGames(games, searchQuery, sortKey, filterKey)
    : games;

  function handleRxeaCover(titleId, src) {
    setRxeaCovers((prev) => (prev[titleId] === src ? prev : { ...prev, [titleId]: src }));
  }

  function isOnSource(game) {
    return Boolean(game.sourceDrive);
  }

  if (selectedGame) {
    return (
      <div className="flex flex-col h-screen p-3 gap-2.5">
        <GameDetail
          game={selectedGame}
          coverDataUrl={covers[selectedGame.titleId]}
          titleVisuals={titleVisuals[selectedGame.titleId]}
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
  const drives = [...new Set(games.map((g) => g.sourceDrive).filter(Boolean))].sort();

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
          <div className="flex items-center gap-1 shrink-0">
            <Button
              size="icon"
              variant="ghost"
              title="Refresh library cache from Xbox"
              disabled={refreshBusy || status !== "ready" || typeof onRefresh !== "function"}
              onClick={() => { setRxeaCovers({}); onRefresh?.(); }}
            >
              <RefreshCw className={cn("h-4 w-4", refreshBusy && "animate-spin")} />
            </Button>
            <Button size="icon" title="Back to console" onClick={onToggle}>
              <ArrowLeft className="h-4 w-4" />
            </Button>
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
                    { value: "multi-disc", label: "Multi-disc",  count: games.filter((g) => g.discsInSet > 1).length },
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
              const tv = titleVisuals[game.titleId];
              const visualCover = tv?.cover?.src || null;
              const isBooklet = tv?.coverIsBooklet === true;
              return (
                <GameCard
                  key={`${game.titleId}-${game.contentId}`}
                  game={game}
                  coverDataUrl={
                    isBooklet ? covers[game.titleId] : (visualCover || covers[game.titleId])
                  }
                  rxeaCover={
                    rxeaCovers[game.titleId] ||
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
