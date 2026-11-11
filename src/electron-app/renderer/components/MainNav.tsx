import { useEffect, useRef, useState } from "react";
import {
  RefreshCw, Settings, Gamepad2, Loader2, RotateCcw, ListOrdered,
  Store, Wrench, Disc, FolderOpen, HardDrive, Terminal, Usb,
} from "lucide-react";
import { Button } from "./ui/button";
import { cn } from "../lib/utils";

export function FtpIndicator({ status }: { status: string }) {
  const dotClass =
    status === "connected"   ? "bg-[color:var(--laser)] shadow-[0_0_0_3px_rgba(52,227,122,0.14)]"
    : status === "checking"  ? "bg-[color:var(--brass)] animate-pulse"
    :                          "bg-white/25";

  const label =
    status === "connected"   ? "Console online"
    : status === "checking"  ? "Checking console"
    :                          "Console offline";

  return (
    <div
      className="flex items-center gap-1.5 h-8 pl-2 pr-2.5 rounded-full border border-white/[0.09] bg-white/[0.03] select-none"
      title={label}
      role="status"
      aria-live="polite"
      aria-label={label}
    >
      <div className={cn("w-2 h-2 rounded-full shrink-0 transition-colors duration-300", dotClass)} />
      <span className="text-[10px] font-medium text-muted-foreground leading-none tracking-[0.08em] uppercase">
        {status === "connected" ? "Online" : status === "checking" ? "Checking" : "Offline"}
      </span>
    </div>
  );
}

interface ToolboxDropdownProps {
  onIso2God: () => void;
  onIso2Xex: () => void;
  onFtpManager: () => void;
  onBadAvatarUsb: () => void;
  active?: boolean;
}

export function ToolboxDropdown({ onIso2God, onIso2Xex, onFtpManager, onBadAvatarUsb, active }: ToolboxDropdownProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, [open]);

  return (
    <div className="relative" ref={ref}>
      <Button
        size="icon"
        title="Toolbox"
        aria-label="Toolbox"
        variant={active ? "primary" : "default"}
        onClick={() => setOpen(!open)}
      >
        <Wrench className="h-4 w-4" />
      </Button>
      {open && (
        <div className="absolute right-0 top-full mt-1.5 z-50 w-48 rounded-xl border border-white/[0.1] bg-popover/95 backdrop-blur-md shadow-[0_28px_64px_-40px_rgba(0,0,0,0.95)] overflow-hidden animate-toast">
          <button
            className="flex items-center gap-2 w-full px-3 py-2 text-left text-[12px] text-foreground hover:bg-accent/60 focus-visible:outline-none focus-visible:bg-accent/60 transition-colors cursor-pointer"
            onClick={() => { setOpen(false); onIso2God(); }}
          >
            <Disc className="h-3.5 w-3.5 text-[color:var(--laser-2)]" />
            ISO to GOD
          </button>
          <button
            className="flex items-center gap-2 w-full px-3 py-2 text-left text-[12px] text-foreground hover:bg-accent/60 focus-visible:outline-none focus-visible:bg-accent/60 transition-colors cursor-pointer"
            onClick={() => { setOpen(false); onIso2Xex(); }}
          >
            <FolderOpen className="h-3.5 w-3.5 text-[color:var(--laser)]" />
            ISO to XEX
          </button>
          <button
            className="flex items-center gap-2 w-full px-3 py-2 text-left text-[12px] text-foreground hover:bg-accent/60 focus-visible:outline-none focus-visible:bg-accent/60 transition-colors cursor-pointer"
            onClick={() => { setOpen(false); onBadAvatarUsb(); }}
          >
            <Usb className="h-3.5 w-3.5 text-[color:var(--brass)]" />
            BadAvatar USB
          </button>
          <button
            className="flex items-center gap-2 w-full px-3 py-2 text-left text-[12px] text-foreground hover:bg-accent/60 focus-visible:outline-none focus-visible:bg-accent/60 transition-colors cursor-pointer"
            onClick={() => { setOpen(false); onFtpManager(); }}
          >
            <HardDrive className="h-3.5 w-3.5 text-[color:var(--violet)]" />
            FTP Manager
          </button>
        </div>
      )}
    </div>
  );
}

export interface MainNavProps {
  ftpStatus: string;
  currentPage: "home" | "library" | "settings" | "queue" | "browse" | "iso2god" | "iso2xex" | "ftpmanager" | "badavatarusb";
  libraryAvailable?: boolean;
  libraryLoading: boolean;
  queueJobs: any[];
  onReconnect: () => void;
  onLibraryToggle: () => void;
  onNavigateHome?: () => void;
  onNavigateQueue: () => void;
  onNavigateBrowse: () => void;
  onNavigateSettings: () => void;
  onNavigateIso2God: () => void;
  onNavigateIso2Xex: () => void;
  onNavigateFtpManager: () => void;
  onNavigateBadAvatarUsb: () => void;
}

export default function MainNav({
  ftpStatus,
  currentPage,
  libraryAvailable,
  libraryLoading,
  queueJobs,
  onReconnect,
  onLibraryToggle,
  onNavigateHome,
  onNavigateQueue,
  onNavigateBrowse,
  onNavigateSettings,
  onNavigateIso2God,
  onNavigateIso2Xex,
  onNavigateFtpManager,
  onNavigateBadAvatarUsb,
}: MainNavProps) {
  const ftpChecking  = ftpStatus === "checking";
  const showLibBtn   = libraryAvailable || libraryLoading;
  const hasQueueJobs = Array.isArray(queueJobs) && queueJobs.length > 0;
  const onHome       = currentPage === "home";

  const activeBtnClass = "ring-2 ring-[color:var(--laser)] ring-offset-2 ring-offset-background";

  return (
    <div className="flex items-center gap-1.5">
      <FtpIndicator status={ftpStatus} />

      <Button
        size="icon"
        title="Retry FTP connection"
        aria-label="Retry FTP connection"
        disabled={ftpChecking}
        onClick={onReconnect}
      >
        {ftpChecking
          ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
          : <RotateCcw className="h-3.5 w-3.5" />}
      </Button>

      {!onHome && (
        <Button
          size="icon"
          title="Console"
          aria-label="Console"
          variant={currentPage === "home" ? "primary" : "default"}
          onClick={onNavigateHome || onLibraryToggle}
        >
          <Terminal className="h-4 w-4" />
        </Button>
      )}

      {showLibBtn && (
        <Button
          size="icon"
          title={libraryLoading ? "Connecting to Xbox…" : "Xbox Library"}
          aria-label="Xbox Library"
          aria-current={currentPage === "library" ? "page" : undefined}
          disabled={libraryLoading}
          variant={currentPage === "library" ? "primary" : "default"}
          onClick={onLibraryToggle}
        >
          {libraryLoading
            ? <Loader2 className="h-4 w-4 animate-spin" />
            : <Gamepad2 className="h-4 w-4" />}
        </Button>
      )}

      {hasQueueJobs && (
        <Button
          size="icon"
          title={`Server queue (${queueJobs.length} job${queueJobs.length !== 1 ? "s" : ""})`}
          aria-label={`Server queue, ${queueJobs.length} job${queueJobs.length !== 1 ? "s" : ""}`}
          aria-current={currentPage === "queue" ? "page" : undefined}
          onClick={onNavigateQueue}
          className={cn("relative", currentPage === "queue" && activeBtnClass)}
        >
          <ListOrdered className="h-4 w-4" />
          <span className="absolute -top-1 -right-1 h-4 min-w-[16px] px-0.5 rounded-full bg-primary text-[9px] font-bold text-primary-foreground flex items-center justify-center leading-none">
            {queueJobs.length > 9 ? "9+" : queueJobs.length}
          </span>
        </Button>
      )}

      <Button
        size="icon"
        title="Browse & Download"
        aria-label="Browse and Download"
        aria-current={currentPage === "browse" ? "page" : undefined}
        variant={currentPage === "browse" ? "primary" : "default"}
        onClick={onNavigateBrowse}
      >
        <Store className="h-4 w-4" />
      </Button>

      <ToolboxDropdown
        onIso2God={onNavigateIso2God}
        onIso2Xex={onNavigateIso2Xex}
        onFtpManager={onNavigateFtpManager}
        onBadAvatarUsb={onNavigateBadAvatarUsb}
        active={currentPage === "iso2god" || currentPage === "iso2xex" || currentPage === "ftpmanager" || currentPage === "badavatarusb"}
      />

      <Button
        size="icon"
        title="Restart backend"
        aria-label="Restart backend"
        onClick={() => window.godsendApi.restartProcess()}
      >
        <RefreshCw className="h-4 w-4" />
      </Button>

      <Button
        size="icon"
        title="Settings"
        aria-label="Settings"
        aria-current={currentPage === "settings" ? "page" : undefined}
        variant={currentPage === "settings" ? "primary" : "default"}
        onClick={onNavigateSettings}
      >
        <Settings className="h-4 w-4" />
      </Button>
    </div>
  );
}
