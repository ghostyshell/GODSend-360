import { useEffect, useRef, useState } from "react";
import { RefreshCw, Settings, Gamepad2, Loader2, RotateCcw, ListOrdered, Store, Wrench, Disc, FolderOpen, HardDrive } from "lucide-react";
import { Button } from "./ui/button";
import { cn } from "../lib/utils";

interface LogInfo {
  logsDirectory?: string;
  currentLogFile?: string;
}

// FTP status dot + label
function FtpIndicator({ status }: { status: string }) {
  const dotClass =
    status === "connected"    ? "bg-green-500"
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
      <div className={cn("w-2 h-2 rounded-full shrink-0 transition-colors", dotClass)} />
      <span className="text-[10px] font-medium text-muted-foreground leading-none tracking-wide">
        FTP
      </span>
    </div>
  );
}

// Toolbox dropdown menu
interface ToolboxDropdownProps {
  onIso2God: () => void;
  onIso2Xex: () => void;
  onFtpManager: () => void;
}

function ToolboxDropdown({ onIso2God, onIso2Xex, onFtpManager }: ToolboxDropdownProps) {
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
      <Button size="icon" title="Toolbox" onClick={() => setOpen(!open)}>
        <Wrench className="h-4 w-4" />
      </Button>
      {open && (
        <div className="absolute right-0 top-full mt-1 z-50 w-44 bg-surface border border-border rounded-lg shadow-lg overflow-hidden">
          <button
            className="flex items-center gap-2 w-full px-3 py-2 text-left text-[12px] text-foreground hover:bg-accent transition-colors"
            onClick={() => { setOpen(false); onIso2God(); }}
          >
            <Disc className="h-3.5 w-3.5 text-blue-400" />
            ISO to GOD
          </button>
          <button
            className="flex items-center gap-2 w-full px-3 py-2 text-left text-[12px] text-foreground hover:bg-accent transition-colors"
            onClick={() => { setOpen(false); onIso2Xex(); }}
          >
            <FolderOpen className="h-3.5 w-3.5 text-green-400" />
            ISO to XEX
          </button>
          <button
            className="flex items-center gap-2 w-full px-3 py-2 text-left text-[12px] text-foreground hover:bg-accent transition-colors"
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

interface HomePageProps {
  outputLines: string[];
  logInfo: LogInfo | null;
  ftpStatus: string;
  onNavigateSettings: () => void;
  onNavigateQueue: () => void;
  onNavigateBrowse: () => void;
  onNavigateIso2God: () => void;
  onNavigateIso2Xex: () => void;
  onNavigateFtpManager: () => void;
  onLibraryToggle: () => void;
  onReconnect: () => void;
  libraryLoading: boolean;
  onAppendLine: (line: string) => void;
  queueJobs: any[];
}

export default function HomePage({
  outputLines,
  logInfo,
  ftpStatus,
  onNavigateSettings,
  onNavigateQueue,
  onNavigateBrowse,
  onNavigateIso2God,
  onNavigateIso2Xex,
  onNavigateFtpManager,
  onLibraryToggle,
  onReconnect,
  libraryLoading,
  onAppendLine,
  queueJobs,
}: HomePageProps) {
  const outputRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [outputLines]);

  async function handleOpenLogs() {
    const r = await window.godsendApi.openLogsFolder();
    if (r && !r.ok && r.error) {
      onAppendLine(`[ERROR] Could not open logs folder: ${r.error}`);
    }
  }

  const ftpConnected    = ftpStatus === "connected";
  const ftpChecking     = ftpStatus === "checking";
  const showLibraryBtn  = ftpConnected || libraryLoading; // hide when disconnected
  const hasQueueJobs    = Array.isArray(queueJobs) && queueJobs.length > 0;

  return (
    <div className="flex flex-col h-screen p-3 gap-2.5">
      {/* Top bar — all controls right-aligned */}
      <header className="flex justify-end items-center gap-1.5 shrink-0">

        {/* FTP status indicator */}
        <FtpIndicator status={ftpStatus} />

        {/* Reconnect button */}
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

        {/* Xbox Library toggle — hidden while FTP is disconnected */}
        {showLibraryBtn && (
          <Button
            size="icon"
            title={libraryLoading ? "Connecting to Xbox…" : "Xbox Library"}
            disabled={libraryLoading}
            onClick={onLibraryToggle}
          >
            {libraryLoading
              ? <Loader2 className="h-4 w-4 animate-spin" />
              : <Gamepad2 className="h-4 w-4" />}
          </Button>
        )}

        {/* Queue viewer — visible when there are active jobs */}
        {hasQueueJobs && (
          <Button
            size="icon"
            title={`Server queue (${queueJobs.length} job${queueJobs.length !== 1 ? "s" : ""})`}
            onClick={onNavigateQueue}
            className="relative"
          >
            <ListOrdered className="h-4 w-4" />
            <span className="absolute -top-1 -right-1 h-4 min-w-[16px] px-0.5 rounded-full bg-primary text-[9px] font-bold text-primary-foreground flex items-center justify-center leading-none">
              {queueJobs.length > 9 ? "9+" : queueJobs.length}
            </span>
          </Button>
        )}

        {/* Browse library */}
        <Button size="icon" title="Browse & Download" onClick={onNavigateBrowse}>
          <Store className="h-4 w-4" />
        </Button>

        {/* Toolbox dropdown */}
        <ToolboxDropdown
          onIso2God={onNavigateIso2God}
          onIso2Xex={onNavigateIso2Xex}
          onFtpManager={onNavigateFtpManager}
        />

        {/* Restart backend */}
        <Button
          size="icon"
          title="Restart backend"
          onClick={() => window.godsendApi.restartProcess()}
        >
          <RefreshCw className="h-4 w-4" />
        </Button>

        {/* Settings */}
        <Button size="icon" title="Settings" onClick={onNavigateSettings}>
          <Settings className="h-4 w-4" />
        </Button>
      </header>

      {/* Terminal output */}
      <pre
        ref={outputRef}
        className="flex-1 min-h-0 m-0 p-2.5 bg-surface border border-border rounded-lg overflow-auto whitespace-pre-wrap break-words font-mono text-[13px] leading-[1.4] select-text cursor-text"
      >
        {outputLines.join("\n")}
      </pre>

      {/* Footer */}
      <footer className="flex justify-between items-center gap-2.5 shrink-0 text-[11px] text-muted-foreground">
        <span
          className="overflow-hidden text-ellipsis whitespace-nowrap min-w-0"
          title={logInfo?.logsDirectory ?? ""}
        >
          {logInfo?.currentLogFile ? `Log: ${logInfo.currentLogFile}` : ""}
        </span>
        <Button size="sm" className="shrink-0" onClick={handleOpenLogs}>
          Open logs folder
        </Button>
      </footer>
    </div>
  );
}
