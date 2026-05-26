import { useEffect, useRef, useState } from "react";
import {
  RefreshCw, Settings, Gamepad2, Loader2, RotateCcw, ListOrdered,
  Store, Wrench, Disc, FolderOpen, HardDrive, Terminal, Usb,
} from "lucide-react";
import { Button } from "./ui/button";
import { cn } from "../lib/utils";

export function FtpIndicator({ status }: { status: string }) {
  const dotClass =
    status === "connected"   ? "bg-green-500"
    : status === "checking"  ? "bg-yellow-400 animate-pulse"
    :                          "bg-muted-foreground/40";

  const label =
    status === "connected"   ? "FTP connected"
    : status === "checking"  ? "Checking FTP…"
    :                          "FTP not reachable";

  return (
    <div
      className="flex items-center gap-1.5 h-8 px-1 select-none"
      title={label}
      aria-label={label}
    >
      <div className={cn("w-2.5 h-2.5 rounded-full shrink-0 transition-all duration-300", dotClass)} />
      <span className="text-[10px] font-medium text-muted-foreground leading-none tracking-wide">
        FTP
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
        variant={active ? "primary" : "default"}
        onClick={() => setOpen(!open)}
      >
        <Wrench className="h-4 w-4" />
      </Button>
      {open && (
        <div className="absolute right-0 top-full mt-1 z-50 w-48 bg-surface border border-border rounded-lg shadow-lg overflow-hidden animate-fade-in">
          <button
            className="flex items-center gap-2 w-full px-3 py-2 text-left text-[12px] text-foreground hover:bg-accent hover:text-accent-foreground transition-colors cursor-pointer"
            onClick={() => { setOpen(false); onIso2God(); }}
          >
            <Disc className="h-3.5 w-3.5 text-blue-400" />
            ISO to GOD
          </button>
          <button
            className="flex items-center gap-2 w-full px-3 py-2 text-left text-[12px] text-foreground hover:bg-accent hover:text-accent-foreground transition-colors cursor-pointer"
            onClick={() => { setOpen(false); onIso2Xex(); }}
          >
            <FolderOpen className="h-3.5 w-3.5 text-green-400" />
            ISO to XEX
          </button>
          <button
            className="flex items-center gap-2 w-full px-3 py-2 text-left text-[12px] text-foreground hover:bg-accent hover:text-accent-foreground transition-colors cursor-pointer"
            onClick={() => { setOpen(false); onBadAvatarUsb(); }}
          >
            <Usb className="h-3.5 w-3.5 text-orange-400" />
            BadAvatar USB
          </button>
          <button
            className="flex items-center gap-2 w-full px-3 py-2 text-left text-[12px] text-foreground hover:bg-accent hover:text-accent-foreground transition-colors cursor-pointer"
            onClick={() => { setOpen(false); onFtpManager(); }}
          >
            <HardDrive className="h-3.5 w-3.5 text-yellow-400" />
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

  const activeBtnClass = "ring-1 ring-accent ring-offset-1 ring-offset-background";

  return (
    <div className="flex items-center gap-1.5">
      <FtpIndicator status={ftpStatus} />

      <Button
        size="icon"
        title="Retry FTP connection"
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
        onClick={() => window.godsendApi.restartProcess()}
      >
        <RefreshCw className="h-4 w-4" />
      </Button>

      <Button
        size="icon"
        title="Settings"
        variant={currentPage === "settings" ? "primary" : "default"}
        onClick={onNavigateSettings}
      >
        <Settings className="h-4 w-4" />
      </Button>
    </div>
  );
}
