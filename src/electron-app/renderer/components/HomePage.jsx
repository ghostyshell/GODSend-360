import { useEffect, useRef } from "react";
import { RefreshCw, Settings, Gamepad2, Loader2, RotateCcw } from "lucide-react";
import { Button } from "./ui/button";
import { cn } from "../lib/utils";

// FTP status dot + label
function FtpIndicator({ status }) {
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

export default function HomePage({
  outputLines,
  logInfo,
  ftpStatus,
  onNavigateSettings,
  onLibraryToggle,
  onReconnect,
  libraryLoading,
  onAppendLine,
}) {
  const outputRef = useRef(null);

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
