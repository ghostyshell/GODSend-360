import { useState, useEffect, useRef } from "react";
import {
  ArrowLeft, Gamepad2, Loader2, WifiOff,
  Star, Disc3, RefreshCw, Upload, Search, X, Check,
} from "lucide-react";
import { Button } from "./ui/button";
import { ScrollArea } from "./ui/scroll-area";
import { cn } from "../lib/utils";

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

// ── Asset slot definitions ────────────────────────────────────────────────────

const MAIN_SLOTS = [
  { key: "background", label: "Background", aspect: "video",    importName: "background" },
  { key: "banner",     label: "Banner",     aspect: "wide",     importName: "banner"     },
  { key: "icon",       label: "Icon",       aspect: "square",   importName: "icon"       },
  { key: "cover",      label: "Cover",      aspect: "portrait", importName: "cover"      },
];

const SCREENSHOT_COUNT = 5;

// ── Single asset slot card ────────────────────────────────────────────────────

function AssetSlotCard({ slotKey, label, aspect, currentAsset, pendingAsset, onSearch, onUpload, onClearPending }) {
  const hasPending = !!pendingAsset;
  const displaySrc  = hasPending
    ? (pendingAsset.dataUrl || pendingAsset.previewUrl || pendingAsset.url || null)
    : (currentAsset?.src || null);
  const displayIsWeb = hasPending
    ? (pendingAsset.dataUrl ? true : isWebImageExt(".jpg"))
    : (currentAsset ? isWebImageExt(currentAsset.ext) : false);

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
          "relative rounded-md border bg-[#0d1117] overflow-hidden flex items-center justify-center cursor-default",
          hasPending ? "border-primary/50 ring-1 ring-primary/30" : "border-border",
          aspect === "video"    && "aspect-video w-full",
          aspect === "wide"     && "aspect-[3/1] w-full",
          aspect === "square"   && "aspect-square w-[68px]",
          aspect === "portrait" && "aspect-[3/4] w-[60px]",
        )}
      >
        {displaySrc && displayIsWeb ? (
          <img src={displaySrc} alt="" className="w-full h-full object-cover" draggable={false} />
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
        query:   q || game.name,
        titleId: titleId || game.titleId,
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
          <Loader2 className="h-3 w-3 animate-spin" /> Searching XboxUnity…
        </div>
      )}

      {searched && !loading && results.length === 0 && (
        <p className="text-[10px] text-muted-foreground">No results found. Try a different query.</p>
      )}

      {results.length > 0 && (
        <div className="flex flex-wrap gap-2 max-h-[200px] overflow-y-auto pt-0.5">
          {results.map((r, idx) => {
            const thumb = r.thumbnail || r.front || r.url;
            return (
              <button
                key={`${r.titleId || "r"}-${idx}`}
                className="flex flex-col rounded border border-border hover:border-primary/60 bg-[#0d1117] overflow-hidden transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                style={{ width: 68 }}
                onClick={() => onSelect(r)}
                title={[r.official && "Official", r.rating != null && `★${r.rating}`].filter(Boolean).join(" · ")}
              >
                <div className="w-full aspect-[3/4] overflow-hidden bg-muted/20">
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
        Results from XboxUnity. Select an image to stage it for upload.
      </p>
    </div>
  );
}

// ── Asset editor section ──────────────────────────────────────────────────────

function AssetEditorSection({ game, titleVisuals, onRefresh }) {
  const [pending, setPending]       = useState({});
  const [searchSlot, setSearchSlot] = useState(null);
  const [saving, setSaving]         = useState(false);
  const [saveMsg, setSaveMsg]       = useState("");
  const [saveMsgKind, setSaveMsgKind] = useState("ok"); // "ok" | "error"

  const hasPending = Object.keys(pending).length > 0;

  function currentAsset(slotKey) {
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
    const previewUrl = isDataUrl(url) ? url : (result.thumbnail || result.front || url);
    setPending((prev) => ({ ...prev, [slotKey]: { url: isDataUrl(url) ? null : url, dataUrl: isDataUrl(url) ? url : null, previewUrl, ext: ".jpg" } }));
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

  // Current screenshot count (from existing visuals or pending).
  const ssCount = Math.max(
    titleVisuals?.screenshots?.length || 0,
    ...Object.keys(pending).filter((k) => k.startsWith("screenshot")).map((k) => parseInt(k.replace("screenshot", ""), 10)),
    0,
  );
  const screenshotSlots = Array.from({ length: Math.max(ssCount, 0) }, (_, i) => ({
    key: `screenshot${i + 1}`, label: `Screenshot ${i + 1}`,
  }));

  return (
    <div className="space-y-3">
      {/* Header row */}
      <div className="flex items-center justify-between gap-2">
        <p className="text-[10.5px] font-semibold text-muted-foreground uppercase tracking-wider">
          Aurora Assets
        </p>
        <div className="flex items-center gap-1.5 shrink-0">
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
          {MAIN_SLOTS.map(({ key, label, aspect }) => (
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
            />
          ))}
        </div>
      </div>

      {/* Screenshots */}
      {screenshotSlots.length > 0 && (
        <div className="rounded-md border border-border/60 bg-muted/10 p-2.5">
          <p className="text-[9px] text-muted-foreground mb-2">Screenshots</p>
          <div className="flex gap-2 overflow-x-auto pb-1">
            {screenshotSlots.map(({ key, label }) => (
              <div key={key} className="shrink-0" style={{ width: 120 }}>
                <AssetSlotCard
                  slotKey={key}
                  label={label}
                  aspect="video"
                  currentAsset={currentAsset(key)}
                  pendingAsset={pending[key] || null}
                  onSearch={() => openSearch(key)}
                  onUpload={() => handleUploadFile(key)}
                  onClearPending={() => clearPending(key)}
                />
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Empty state */}
      {titleVisuals !== undefined
        && !titleVisuals?.background && !titleVisuals?.banner
        && !titleVisuals?.icon     && !titleVisuals?.cover
        && !titleVisuals?.importCover
        && !(titleVisuals?.screenshots?.length)
        && !hasPending && (
        <div className="rounded-md border border-border/40 bg-muted/10 px-3 py-3 text-center">
          <p className="text-[11px] text-muted-foreground">
            No artwork cached yet. Run a library refresh to pull assets from the console,
            or upload your own using Search / File above.
          </p>
        </div>
      )}
      {titleVisuals === undefined && (
        <p className="text-[11px] text-muted-foreground">
          Artwork loads after the library FTP sync…
        </p>
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
}) {
  useEffect(() => {
    window.godsendApi
      .refreshTitleVisualsFromCache({
        titleId:     game.titleId,
        gameDataDir: game.gameDataDir,
      })
      .catch(() => {});
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

      <ScrollArea className="flex-1 min-h-0 mt-3">
        <div className="flex flex-col gap-4 pb-4 pr-1">

          {/* Cover + core info row */}
          <div className="flex gap-4">
            <div
              className={cn(
                "relative shrink-0 rounded-lg overflow-hidden border border-border bg-[#0d1117]",
                !isOnSource && "opacity-50 grayscale"
              )}
              style={{ width: 110, aspectRatio: "3/4" }}
            >
              {coverDataUrl === undefined ? (
                <div className="absolute inset-0 bg-gradient-to-r from-muted via-accent/30 to-muted animate-pulse" />
              ) : coverDataUrl === null ? (
                <div className="absolute inset-0 flex items-center justify-center">
                  <Gamepad2 className="h-7 w-7 text-border" />
                </div>
              ) : (
                <img
                  src={coverDataUrl}
                  alt={game.name}
                  className="absolute inset-0 w-full h-full object-cover"
                  draggable={false}
                />
              )}
            </div>

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
                <MetaRow label="Drive" value={game.sourceDrive || "Unknown"} />
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
            onRefresh={onRefresh}
          />

          <p className="text-[9px] text-muted-foreground/40 font-mono">
            TitleID: {game.titleId}
            {game.contentId ? `  ·  ContentID: ${game.contentId}` : ""}
          </p>
        </div>
      </ScrollArea>
    </div>
  );
}

// ── Game card ─────────────────────────────────────────────────────────────────

function GameCard({ game, coverDataUrl, isOnSource, onClick }) {
  return (
    <button
      className={cn(
        "flex flex-col gap-1 select-none text-left rounded-lg",
        "focus:outline-none focus-visible:ring-1 focus-visible:ring-ring",
        "transition-opacity hover:opacity-80 active:opacity-60",
        !isOnSource && "opacity-50"
      )}
      onClick={onClick}
    >
      <div
        className="relative w-full bg-[#0d1117] rounded-lg overflow-hidden border border-border"
        style={{ aspectRatio: "3/4" }}
      >
        {coverDataUrl === undefined ? (
          <div className="absolute inset-0 overflow-hidden">
            <div className="absolute inset-0 bg-gradient-to-r from-muted via-accent/30 to-muted animate-pulse" />
          </div>
        ) : coverDataUrl === null ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <Gamepad2 className="h-7 w-7 text-border" />
          </div>
        ) : (
          <img
            src={coverDataUrl}
            alt={game.name}
            className={cn(
              "absolute inset-0 w-full h-full object-cover",
              !isOnSource && "grayscale"
            )}
            draggable={false}
          />
        )}

        <div className="absolute top-1 left-1 flex flex-col gap-0.5">
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
          <div className="absolute inset-x-0 bottom-0 bg-black/70 py-0.5 px-1">
            <p className="text-[8px] text-white/60 text-center truncate">
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

  function isOnSource(game) {
    if (!librarySources || librarySources.length === 0) return true;
    if (!game.sourceDrive) return false;
    return librarySources.includes(game.sourceDrive);
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
        />
      </div>
    );
  }

  const onSourceCount  = games.filter(isOnSource).length;
  const offSourceCount = games.length - onSourceCount;

  return (
    <div className="flex flex-col h-screen p-3 gap-2.5">

      <header className="flex items-center justify-between shrink-0 pb-3 border-b border-border">
        <div className="flex items-center gap-2.5 min-w-0">
          <Gamepad2 className="h-[18px] w-[18px] text-primary shrink-0" />
          <span className="text-[15px] font-semibold text-foreground">Xbox Library</span>
          {status === "ready" && (
            <span className="text-[11px] text-muted-foreground truncate">
              {games.length} game{games.length !== 1 ? "s" : ""}
              {offSourceCount > 0 && (
                <span className="text-muted-foreground/50">
                  {" "}·{" "}{offSourceCount} off-drive
                </span>
              )}
              {connectedTo && <span className="text-muted-foreground/50"> · {connectedTo}</span>}
            </span>
          )}
        </div>
        <div className="flex items-center gap-1 shrink-0">
          <Button
            size="icon"
            variant="ghost"
            title="Refresh library cache from Xbox"
            disabled={refreshBusy || status !== "ready" || typeof onRefresh !== "function"}
            onClick={() => onRefresh?.()}
          >
            <RefreshCw className={cn("h-4 w-4", refreshBusy && "animate-spin")} />
          </Button>
          <Button size="icon" title="Back to console" onClick={onToggle}>
            <ArrowLeft className="h-4 w-4" />
          </Button>
        </div>
      </header>

      {status === "connecting" && (
        <CenteredOverlay>
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
          <p className="text-[13px]">Loading Aurora library…</p>
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

      {status === "ready" && games.length > 0 && (
        <ScrollArea className="flex-1 min-h-0">
          <div
            className="grid gap-3 pb-4 pr-1"
            style={{ gridTemplateColumns: "repeat(auto-fill, minmax(110px, 1fr))" }}
          >
            {games.map((game) => (
              <GameCard
                key={`${game.titleId}-${game.contentId}`}
                game={game}
                coverDataUrl={covers[game.titleId]}
                isOnSource={isOnSource(game)}
                onClick={() => setSelectedGame(game)}
              />
            ))}
          </div>
        </ScrollArea>
      )}
    </div>
  );
}
