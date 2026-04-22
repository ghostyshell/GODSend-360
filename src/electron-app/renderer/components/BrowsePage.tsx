import { useState, useEffect, useRef } from "react";
import {
  Search, Loader2, WifiOff, Gamepad2, Download,
  RefreshCw, ChevronDown, X, HardDrive,
} from "lucide-react";
import { Button } from "./ui/button";
import { ScrollArea } from "./ui/scroll-area";
import { cn } from "../lib/utils";

// ── Constants ─────────────────────────────────────────────────────────────────

const PLATFORMS = [
  { id: "xbox360", label: "Xbox 360",      methods: true  },
  { id: "xbox",    label: "Original Xbox", methods: true  },
  { id: "xbla",   label: "XBLA",          methods: false },
  { id: "digital", label: "Digital",       methods: false },
  { id: "dlc",    label: "DLC",           methods: false },
  { id: "xblig",  label: "Indie",         methods: false },
  { id: "games",  label: "Games Archive", methods: true  },
];

const SOURCES = [
  { id: "minerva", label: "Minerva" },
  { id: "ia",      label: "Internet Archive" },
  { id: "local",   label: "Local Library" },
];

const METHODS   = [
  { id: "god",     label: "GOD",     desc: "ISO → Games on Demand" },
  { id: "content", label: "Content (DLC/Multi-Disc)", desc: "Content folder (DLC tree)" },
  { id: "xex",     label: "XEX",     desc: "Loose folder (default.xex)" },
];

// ── Small helpers ─────────────────────────────────────────────────────────────

function CenteredOverlay({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex-1 flex flex-col items-center justify-center gap-3 text-muted-foreground min-h-0">
      {children}
    </div>
  );
}

// Pill-style toggle button (source / platform tabs)
interface PillBtnProps {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
  className?: string;
}

function PillBtn({ active, onClick, children, className }: PillBtnProps) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "shrink-0 px-3 py-1 text-[11px] rounded-full whitespace-nowrap transition-colors",
        "focus:outline-none focus-visible:ring-1 focus-visible:ring-ring",
        active
          ? "bg-primary text-primary-foreground font-semibold"
          : "text-muted-foreground hover:text-foreground bg-muted/50 hover:bg-muted",
        className
      )}
    >
      {children}
    </button>
  );
}

// ── Cover art placeholder / loader ────────────────────────────────────────────

interface CoverArtProps {
  dataUrl?: string | null;
  name: string;
  size?: number;
}

function CoverArt({ dataUrl, name, size = 100 }: CoverArtProps) {
  return (
    <div
      className="relative shrink-0 rounded-lg overflow-hidden border border-border bg-[#0d1117]"
      style={{ width: size, aspectRatio: "3/4" }}
    >
      {dataUrl === undefined ? (
        <div className="absolute inset-0 bg-gradient-to-r from-muted via-accent/30 to-muted animate-pulse" />
      ) : dataUrl === null ? (
        <div className="absolute inset-0 flex items-center justify-center">
          <Gamepad2 className="h-7 w-7 text-border" />
        </div>
      ) : (
        <img
          src={dataUrl}
          alt={name}
          className="absolute inset-0 w-full h-full object-cover"
          draggable={false}
        />
      )}
    </div>
  );
}

// ── Queue dialog (modal overlay) ──────────────────────────────────────────────

interface QueueDialogProps {
  game: string;
  platform: string;
  source: string;
  cover?: string | null;
  coverLoading?: boolean;
  defaultDrive: string;
  drives: string[];
  onClose: () => void;
  onQueue?: () => void;
}

function QueueDialog({
  game, platform, source,
  cover,
  defaultDrive,
  drives,
  onClose,
}: QueueDialogProps) {
  const hasMethods = source === "local" || (PLATFORMS.find((p) => p.id === platform)?.methods ?? false);
  const [drive,  setDrive]  = useState(defaultDrive || drives[0] || "Hdd1:");
  const [method, setMethod] = useState("god");
  const [queuing, setQueuing]   = useState(false);
  const [result,  setResult]    = useState<any>(null);
  const [discRec, setDiscRec]   = useState<string | null>(null);
  const usingDefault = defaultDrive && drive === defaultDrive;

  // Fetch disc-info recommendation for applicable platforms
  useEffect(() => {
    if (!hasMethods) return;
    window.godsendApi.browseGetDiscInfo(game).then((r: any) => {
      if (r.ok && r.recommendation) setDiscRec(r.recommendation);
    }).catch(() => {});
  }, [game, hasMethods]);

  async function handleQueue() {
    setQueuing(true);
    setResult(null);
    const r = await window.godsendApi.browseQueueGame({
      game,
      platform,
      source,
      drive,
      installType: hasMethods ? method : "god",
    });
    setQueuing(false);
    setResult(r);
  }

  const queued = result?.ok;

  return (
    <div className="absolute inset-0 z-20 flex items-center justify-center p-4"
      style={{ background: "rgba(0,0,0,0.75)", backdropFilter: "blur(4px)" }}
    >
      <div className="relative bg-background border border-border rounded-xl p-4 w-full max-w-[340px] flex flex-col gap-3 shadow-2xl">

        {/* Close */}
        <button
          onClick={onClose}
          className="absolute top-3 right-3 p-1 rounded text-muted-foreground hover:text-foreground"
        >
          <X className="h-3.5 w-3.5" />
        </button>

        {/* Cover + title */}
        <div className="flex gap-3 items-start pr-6">
          <CoverArt dataUrl={cover} name={game} size={80} />
          <div className="flex-1 min-w-0 pt-0.5">
            <p className="text-[13px] font-semibold text-foreground leading-snug break-words">
              {game}
            </p>
            <p className="text-[10px] text-muted-foreground mt-1">
              {source === "local"
                ? "Local Library"
                : `${PLATFORMS.find((p) => p.id === platform)?.label ?? platform} · ${SOURCES.find((s) => s.id === source)?.label ?? source}`}
            </p>
          </div>
        </div>

        {/* Drive selector */}
        {!queued && (
          <div className="flex flex-col gap-1">
            <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">
              Destination drive
            </label>
            <div className="relative">
              <select
                value={drive}
                onChange={(e) => setDrive(e.target.value)}
                className={cn(
                  "w-full appearance-none bg-muted border border-border rounded-md",
                  "px-2.5 pr-7 py-1.5 text-[12px] text-foreground focus:outline-none",
                  "focus-visible:ring-1 focus-visible:ring-ring"
                )}
              >
                {defaultDrive && (
                  <option value={defaultDrive}>
                    {defaultDrive} (default)
                  </option>
                )}
                {drives.filter((d) => d !== defaultDrive).map((d) => (
                  <option key={d} value={d}>{d}</option>
                ))}
              </select>
              <ChevronDown className="absolute right-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground pointer-events-none" />
            </div>
            {usingDefault && (
              <p className="text-[9.5px] text-muted-foreground/60">
                Using default from Settings → Default Xbox drive
              </p>
            )}
          </div>
        )}

        {/* Install method (GOD / Content / XEX) — only for applicable platforms */}
        {hasMethods && !queued && (
          <div className="flex flex-col gap-1">
            <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">
              Install method
            </label>
            <div className="flex gap-1.5">
              {METHODS.map((m) => {
                const recommended = discRec === m.id;
                return (
                  <button
                    key={m.id}
                    onClick={() => setMethod(m.id)}
                    title={m.desc + (recommended ? " · Recommended" : "")}
                    className={cn(
                      "flex-1 py-1 text-[11px] rounded-md border transition-colors",
                      method === m.id
                        ? "bg-primary/20 border-primary/50 text-primary font-semibold"
                        : "bg-muted border-border text-muted-foreground hover:text-foreground",
                      recommended && method !== m.id && "border-yellow-500/40"
                    )}
                  >
                    {m.label}
                    {recommended && (
                      <span className="block text-[8px] text-yellow-400/80 leading-none">
                        rec
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          </div>
        )}

        {/* Result message */}
        {result && (
          <p className={cn(
            "text-[11px] px-2 py-1.5 rounded-md text-center",
            result.ok
              ? "bg-green-500/10 text-green-400 border border-green-500/20"
              : "bg-red-500/10 text-red-400 border border-red-500/20"
          )}>
            {result.ok
              ? `Queued! Status: ${result.status}`
              : result.error || "Unknown error"}
          </p>
        )}

        {/* Queue button */}
        {!queued ? (
          <Button
            className="w-full"
            disabled={queuing}
            onClick={handleQueue}
          >
            {queuing
              ? <><Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />Queuing…</>
              : <><Download className="h-3.5 w-3.5 mr-1.5" />Queue for Download</>
            }
          </Button>
        ) : (
          <Button variant="outline" className="w-full" onClick={onClose}>
            Done
          </Button>
        )}
      </div>
    </div>
  );
}

// ── Local library game card ──────────────────────────────────────────────────

interface LocalGameCardProps {
  name: string;
  cover?: string | null;
  onClick: () => void;
}

function LocalGameCard({ name, cover, onClick }: LocalGameCardProps) {
  return (
    <button
      onClick={onClick}
      className="group flex flex-col gap-1 rounded-lg p-1.5 hover:bg-accent/40 active:bg-accent transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-ring"
    >
      <div
        className="relative w-full rounded-lg overflow-hidden border border-border bg-[#0d1117]"
        style={{ aspectRatio: "3/4" }}
      >
        {cover === undefined ? (
          <div className="absolute inset-0 bg-gradient-to-r from-muted via-accent/30 to-muted animate-pulse" />
        ) : cover ? (
          <img
            src={cover}
            alt={name}
            className="absolute inset-0 w-full h-full object-cover"
            draggable={false}
          />
        ) : (
          <div className="absolute inset-0 flex items-center justify-center">
            <Gamepad2 className="h-7 w-7 text-border" />
          </div>
        )}
        <div className="absolute inset-0 bg-gradient-to-t from-black/60 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity flex items-end justify-center pb-2">
          <Download className="h-4 w-4 text-white/80" />
        </div>
      </div>
      <span className="text-[10px] leading-tight text-foreground/80 group-hover:text-foreground text-center line-clamp-2 min-h-[2lh]">
        {name}
      </span>
    </button>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

interface BrowsePageProps {
}

export default function BrowsePage({}: BrowsePageProps) {
  const [source,   setSource]   = useState("minerva");
  const [platform, setPlatform] = useState("xbox360");
  const [status,   setStatus]   = useState("idle");  // idle|loading|cache-building|ready|empty|error
  const [games,    setGames]    = useState<string[]>([]);
  const [cacheProgress, setCacheProgress] = useState<{ loaded: number; total: number } | null>(null);
  const [filter,   setFilter]   = useState("");
  const [defaultDrive, setDefaultDrive] = useState("");

  const [selected,     setSelected]     = useState<string | null>(null);
  const [cover,        setCover]        = useState<string | null | undefined>(undefined);
  const [drives,       setDrives]       = useState<string[]>([]);
  const filterRef = useRef<HTMLInputElement>(null);

  const [localCovers, setLocalCovers] = useState<Record<string, string | null | undefined>>({});

  const isLocal = source === "local";

  // Load default drive and FTP drive list once
  useEffect(() => {
    window.godsendApi.getDefaultXboxDrive().then((d: string) => {
      if (d) setDefaultDrive(d);
    }).catch(() => {});
    window.godsendApi.listXboxDrives().then((r: any) => {
      if (r?.ok && Array.isArray(r.drives) && r.drives.length > 0) {
        setDrives(r.drives);
      }
    }).catch(() => {});
  }, []);

  // Auto-load when platform or source changes
  useEffect(() => {
    loadGames();
  }, [platform, source]);

  async function loadGames() {
    setStatus("loading");
    setGames([]);
    setFilter("");
    setCacheProgress(null);
    setLocalCovers({});
    const browsePayload = isLocal
      ? { platform: "local", source: "local" }
      : { platform, source };
    const r = await window.godsendApi.browseGetGames(browsePayload);
    if (!r.ok) {
      setStatus("error");
      return;
    }
    if (r.loading) {
      setCacheProgress({ loaded: r.loaded, total: r.total });
      setStatus("cache-building");
      return;
    }
    const list = Array.isArray(r.games) ? r.games : [];
    setGames(list);
    setStatus(list.length === 0 ? "empty" : "ready");
    setTimeout(() => filterRef.current?.focus(), 50);

    if (isLocal && list.length > 0) {
      fetchLocalCovers(list);
    }
  }

  function fetchLocalCovers(names: string[]) {
    const initCovers: Record<string, string | null | undefined> = {};
    for (const n of names) initCovers[n] = undefined;
    setLocalCovers(initCovers);

    for (const name of names) {
      window.godsendApi.browseFetchCover(name).then((r: any) => {
        setLocalCovers((prev) => ({ ...prev, [name]: r.ok ? r.dataUrl : null }));
      }).catch(() => {
        setLocalCovers((prev) => ({ ...prev, [name]: null }));
      });
    }
  }

  function openGame(name: string) {
    setSelected(name);
    if (isLocal && localCovers[name] !== undefined) {
      setCover(localCovers[name]);
    } else {
      setCover(undefined);
      window.godsendApi.browseFetchCover(name).then((r: any) => {
        setCover(r.ok ? r.dataUrl : null);
      }).catch(() => setCover(null));
    }
  }

  function closeDialog() {
    setSelected(null);
    setCover(undefined);
  }

  const filtered = filter.trim()
    ? games.filter((g) => g.toLowerCase().includes(filter.toLowerCase()))
    : games;

  const effectivePlatform = isLocal ? "local" : platform;

  return (
    <div className="relative flex flex-col h-full p-3 gap-2 overflow-hidden">

      {/* ── Header ── */}
      <header className="flex items-center gap-2 shrink-0">
        <div className="flex gap-1">
          {SOURCES.map((s) => (
            <PillBtn
              key={s.id}
              active={source === s.id}
              onClick={() => setSource(s.id)}
            >
              {s.id === "local" && <HardDrive className="inline h-3 w-3 mr-1 -mt-px" />}
              {s.label}
            </PillBtn>
          ))}
        </div>
      </header>

      {/* ── Platform tabs (hidden for local library) ── */}
      {!isLocal && (
        <div className="flex gap-1 overflow-x-auto shrink-0 pb-0.5 no-scrollbar">
          {PLATFORMS.map((p) => (
            <PillBtn
              key={p.id}
              active={platform === p.id}
              onClick={() => setPlatform(p.id)}
            >
              {p.label}
            </PillBtn>
          ))}
        </div>
      )}

      {/* ── Content area ── */}

      {status === "loading" && (
        <CenteredOverlay>
          <Loader2 className="h-7 w-7 animate-spin text-primary" />
          <p className="text-[13px]">
            {isLocal ? "Scanning Transfer folder…" : "Loading game list…"}
          </p>
        </CenteredOverlay>
      )}

      {status === "cache-building" && (
        <CenteredOverlay>
          <Loader2 className="h-7 w-7 animate-spin text-primary" />
          <p className="text-[13px]">Building cache…</p>
          {cacheProgress && (
            <p className="text-[11px] text-muted-foreground/70">
              {cacheProgress.loaded} / {cacheProgress.total} fetched
            </p>
          )}
          <p className="text-[11px] text-muted-foreground/60 max-w-[220px] text-center">
            First load takes a moment. Go back and try again shortly.
          </p>
          <Button size="sm" variant="outline" onClick={loadGames} className="mt-1">
            <RefreshCw className="h-3 w-3 mr-1.5" />
            Retry
          </Button>
        </CenteredOverlay>
      )}

      {status === "error" && (
        <CenteredOverlay>
          <WifiOff className="h-7 w-7 text-muted-foreground" />
          <p className="text-[13px]">Could not reach the server.</p>
          <p className="text-[11px] text-muted-foreground/60">
            Make sure the GODsend backend is running.
          </p>
          <Button size="sm" onClick={loadGames}>
            <RefreshCw className="h-3 w-3 mr-1.5" />
            Retry
          </Button>
        </CenteredOverlay>
      )}

      {status === "empty" && (
        <CenteredOverlay>
          {isLocal ? (
            <>
              <HardDrive className="h-7 w-7 text-muted-foreground" />
              <p className="text-[13px]">No ISOs found in Transfer folder.</p>
              <p className="text-[11px] text-muted-foreground/60 max-w-[260px] text-center">
                Place Xbox 360 ISO files in your Transfer folder to see them here.
                The folder path can be changed in Settings.
              </p>
            </>
          ) : (
            <>
              <Gamepad2 className="h-7 w-7 text-muted-foreground" />
              <p className="text-[13px]">No games found.</p>
              <p className="text-[11px] text-muted-foreground/60 max-w-[220px] text-center">
                The list may still be building. Try again in a moment.
              </p>
            </>
          )}
          <Button size="sm" onClick={loadGames}>
            <RefreshCw className="h-3 w-3 mr-1.5" />
            {isLocal ? "Rescan" : "Retry"}
          </Button>
        </CenteredOverlay>
      )}

      {status === "ready" && (
        <>
          {/* Search bar */}
          <div className="relative shrink-0">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground pointer-events-none" />
            <input
              ref={filterRef}
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder={`Filter ${games.length} title${games.length === 1 ? "" : "s"}…`}
              className={cn(
                "w-full pl-8 pr-3 py-1.5 text-[12px] rounded-md",
                "bg-muted border border-border text-foreground placeholder:text-muted-foreground",
                "focus:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              )}
            />
            {filter && (
              <button
                onClick={() => setFilter("")}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </div>

          {/* Result count hint */}
          {filter && (
            <p className="text-[10px] text-muted-foreground/60 shrink-0 -mt-1 px-0.5">
              {filtered.length} match{filtered.length !== 1 ? "es" : ""}
            </p>
          )}

          {/* Game grid (local library) or text list (store) */}
          <ScrollArea className="flex-1 min-h-0">
            {filtered.length === 0 ? (
              <p className="text-[12px] text-muted-foreground text-center py-8">
                No matches for &ldquo;{filter}&rdquo;
              </p>
            ) : isLocal ? (
              <div
                className="grid gap-2 pb-4 pr-1"
                style={{ gridTemplateColumns: "repeat(auto-fill, minmax(100px, 1fr))" }}
              >
                {filtered.map((name) => (
                  <LocalGameCard
                    key={name}
                    name={name}
                    cover={localCovers[name]}
                    onClick={() => openGame(name)}
                  />
                ))}
              </div>
            ) : (
              <div className="pr-1 pb-2">
                {filtered.map((name) => (
                    <button
                      key={name}
                      onClick={() => openGame(name)}
                      className={cn(
                        "w-full text-left px-2 py-1.5 rounded-md group",
                        "text-[12px] text-foreground/85 hover:text-foreground",
                        "hover:bg-accent/50 active:bg-accent transition-colors",
                        "focus:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                      )}
                    >
                      <span className="flex items-center justify-between gap-2">
                        <span className="truncate">{name}</span>
                        <Download className="h-3 w-3 shrink-0 text-muted-foreground/0 group-hover:text-muted-foreground/60 transition-colors" />
                      </span>
                    </button>
                ))}
              </div>
            )}
          </ScrollArea>
        </>
      )}

      {/* ── Queue dialog overlay ── */}
      {selected && (
        <QueueDialog
          game={selected}
          platform={effectivePlatform}
          source={source}
          cover={cover}
          defaultDrive={defaultDrive}
          drives={drives}
          onClose={closeDialog}
        />
      )}
    </div>
  );
}
