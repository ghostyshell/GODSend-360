import { useEffect, useRef } from "react";
import { Button } from "./ui/button";
import MainNav from "./MainNav";

interface LogInfo {
  logsDirectory?: string;
  currentLogFile?: string;
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
  onNavigateBadAvatarUsb: () => void;
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
  onNavigateBadAvatarUsb,
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

  return (
    <div className="flex flex-col h-screen p-3 gap-2.5">
      <header className="flex items-center gap-3 shrink-0 pb-2.5 border-b border-white/[0.07]">
        <div className="flex flex-col min-w-0">
          <span className="eyebrow leading-none mb-1">GODsend 360</span>
          <span className="font-display text-[15px] font-bold text-foreground tracking-wide">
            Console
          </span>
        </div>
        <div className="flex-1" />
        <MainNav
          ftpStatus={ftpStatus}
          currentPage="home"
          libraryAvailable={ftpStatus === "connected"}
          libraryLoading={libraryLoading}
          queueJobs={queueJobs}
          onReconnect={onReconnect}
          onLibraryToggle={onLibraryToggle}
          onNavigateQueue={onNavigateQueue}
          onNavigateBrowse={onNavigateBrowse}
          onNavigateSettings={onNavigateSettings}
          onNavigateIso2God={onNavigateIso2God}
          onNavigateIso2Xex={onNavigateIso2Xex}
          onNavigateFtpManager={onNavigateFtpManager}
          onNavigateBadAvatarUsb={onNavigateBadAvatarUsb}
        />
      </header>

      <div className="panel flex-1 min-h-0 flex flex-col overflow-hidden">
        <div className="flex items-center gap-2 px-3 h-9 shrink-0 border-b border-white/[0.06] bg-white/[0.02]">
          <span className="w-1.5 h-1.5 rounded-full bg-[color:var(--laser)] shadow-[0_0_0_3px_rgba(52,227,122,0.14)]" />
          <span className="text-[10px] font-mono uppercase tracking-[0.18em] text-muted-foreground">
            Backend output
          </span>
          <span className="ml-auto text-[10px] font-mono text-[color:var(--text-faint)]">
            {outputLines.length} lines
          </span>
        </div>
        <pre
          ref={outputRef}
          className="flex-1 min-h-0 m-0 p-3 overflow-auto whitespace-pre-wrap break-words font-mono text-[12.5px] leading-[1.55] text-[color:var(--text-dim)] select-text cursor-text"
        >
          {outputLines.join("\n")}
        </pre>
      </div>

      <footer className="flex justify-between items-center gap-2.5 shrink-0 text-[11px] text-muted-foreground">
        <span
          className="overflow-hidden text-ellipsis whitespace-nowrap min-w-0 font-mono text-[10.5px]"
          title={logInfo?.logsDirectory ?? ""}
        >
          {logInfo?.currentLogFile ? `Log: ${logInfo.currentLogFile}` : ""}
        </span>
        <Button size="sm" variant="outline" className="shrink-0" onClick={handleOpenLogs}>
          Open logs folder
        </Button>
      </footer>
    </div>
  );
}
