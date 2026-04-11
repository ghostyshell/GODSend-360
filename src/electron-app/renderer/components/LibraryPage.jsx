import { useEffect } from "react";
import { Gamepad2, Loader2, WifiOff } from "lucide-react";
import { Button } from "./ui/button";
import { ScrollArea } from "./ui/scroll-area";

// ── Game card ─────────────────────────────────────────────────────────────────

function GameCard({ game, coverDataUrl }) {
  // coverDataUrl:
  //   undefined  → still loading (shimmer)
  //   null       → no cover found (placeholder icon)
  //   string     → base64 data URL

  return (
    <div className="flex flex-col gap-1.5 select-none">
      {/* Cover art */}
      <div className="relative w-full bg-[#0d1117] rounded-lg overflow-hidden border border-border"
           style={{ aspectRatio: "3 / 4" }}>
        {coverDataUrl === undefined ? (
          /* Loading shimmer */
          <div className="absolute inset-0 overflow-hidden rounded-lg">
            <div className="absolute inset-0 bg-gradient-to-r from-muted via-accent/30 to-muted animate-pulse" />
          </div>
        ) : coverDataUrl === null ? (
          /* No cover placeholder */
          <div className="absolute inset-0 flex items-center justify-center">
            <Gamepad2 className="h-8 w-8 text-border" />
          </div>
        ) : (
          <img
            src={coverDataUrl}
            alt={game.name}
            className="absolute inset-0 w-full h-full object-contain"
            draggable={false}
          />
        )}
      </div>

      {/* Metadata */}
      <div className="px-0.5 min-w-0">
        <p className="text-[11.5px] font-medium text-foreground leading-tight line-clamp-2 break-words">
          {game.name}
        </p>
        <p className="text-[10px] text-muted-foreground font-mono mt-0.5 tracking-wide">
          {game.titleId}
        </p>
      </div>
    </div>
  );
}

// ── Status overlays ───────────────────────────────────────────────────────────

function CenteredOverlay({ children }) {
  return (
    <div className="flex-1 flex flex-col items-center justify-center gap-3 text-muted-foreground">
      {children}
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export default function LibraryPage({ status, errorMsg, games, covers, connectedTo, onToggle }) {
  return (
    <div className="flex flex-col h-screen p-3 gap-2.5">

      {/* Header */}
      <header className="flex items-center justify-between shrink-0 pb-3 border-b border-border">
        <div className="flex items-center gap-2.5 min-w-0">
          <Gamepad2 className="h-[18px] w-[18px] text-primary shrink-0" />
          <span className="text-[15px] font-semibold text-foreground">Xbox Library</span>
          {status === "ready" && (
            <span className="text-[12px] text-muted-foreground truncate">
              {games.length} game{games.length !== 1 ? "s" : ""} &mdash; {connectedTo}
            </span>
          )}
        </div>
        <Button size="icon" title="Back to console" onClick={onToggle}>
          {/* Re-use the same Gamepad icon but rotated/styled to hint "back" */}
          <Gamepad2 className="h-4 w-4" />
        </Button>
      </header>

      {/* Body */}
      {status === "connecting" && (
        <CenteredOverlay>
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
          <p className="text-[13px]">Connecting to Xbox&hellip;</p>
        </CenteredOverlay>
      )}

      {status === "scanning" && (
        <CenteredOverlay>
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
          <p className="text-[13px]">Scanning game library&hellip;</p>
        </CenteredOverlay>
      )}

      {status === "error" && (
        <CenteredOverlay>
          <WifiOff className="h-8 w-8 text-muted-foreground" />
          <p className="text-[13px] text-center max-w-xs leading-snug">{errorMsg}</p>
          <Button size="sm" onClick={onToggle}>Back to console</Button>
        </CenteredOverlay>
      )}

      {status === "empty" && (
        <CenteredOverlay>
          <Gamepad2 className="h-8 w-8 text-muted-foreground" />
          <p className="text-[13px]">No games found on this Xbox.</p>
          <Button size="sm" onClick={onToggle}>Back to console</Button>
        </CenteredOverlay>
      )}

      {status === "ready" && games.length > 0 && (
        <ScrollArea className="flex-1 min-h-0">
          <div
            className="grid gap-3 pb-4 pr-1"
            style={{ gridTemplateColumns: "repeat(auto-fill, minmax(128px, 1fr))" }}
          >
            {games.map((game) => (
              <GameCard
                key={game.titleId}
                game={game}
                coverDataUrl={covers[game.titleId]}
              />
            ))}
          </div>
        </ScrollArea>
      )}

    </div>
  );
}
