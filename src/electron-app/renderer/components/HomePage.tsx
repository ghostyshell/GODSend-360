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
      <header className="flex justify-end items-center shrink-0">
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

      <pre
        ref={outputRef}
        className="flex-1 min-h-0 m-0 p-2.5 bg-surface border border-border rounded-lg overflow-auto whitespace-pre-wrap break-words font-mono text-[13px] leading-[1.4] select-text cursor-text"
      >
        {outputLines.join("\n")}
      </pre>

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
