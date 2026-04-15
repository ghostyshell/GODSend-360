import { useState, useEffect, useCallback } from "react";
import { ArrowLeft, RefreshCw, Loader2, X } from "lucide-react";
import { Button } from "./ui/button";
import { cn } from "../lib/utils";

interface Job {
  game: string;
  state: string;
  message?: string;
}

function stateColor(state: string) {
  if (state === "Ready")       return "text-green-400";
  if (state === "Error")       return "text-red-400";
  if (state === "Pending FTP") return "text-yellow-400";
  if (state === "Processing")  return "text-blue-400";
  return "text-muted-foreground";
}

function stateIcon(state: string) {
  if (state === "Ready")       return "OK";
  if (state === "Error")       return "!!";
  if (state === "Pending FTP") return "..";
  if (state === "Processing")  return ">>";
  return "?";
}

interface JobRowProps {
  job: Job;
  onRemove: (game: string) => void;
  removing: boolean;
}

function JobRow({ job, onRemove, removing }: JobRowProps) {
  const pct = (job.message || "").match(/\((\d+\.?\d*)%\)/) ||
              (job.message || "").match(/:\s*(\d+)%/);
  const pctNum = pct ? parseFloat(pct[1]) : null;

  return (
    <div className="flex items-start gap-2 py-2 border-b border-[#1e242e] last:border-0">
      <span className={cn("font-mono text-[11px] mt-0.5 shrink-0 w-6 text-center", stateColor(job.state))}>
        {stateIcon(job.state)}
      </span>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-[13px] font-medium text-foreground truncate">{job.game}</span>
          <span className={cn("text-[11px] shrink-0", stateColor(job.state))}>{job.state}</span>
          {pctNum !== null && (
            <span className="text-[11px] text-muted-foreground shrink-0">{pctNum.toFixed(0)}%</span>
          )}
        </div>
        {job.message && (
          <p className="text-[11px] text-muted-foreground mt-0.5 truncate" title={job.message}>
            {job.message}
          </p>
        )}
        {pctNum !== null && (
          <div className="mt-1 h-1 bg-muted rounded-full overflow-hidden max-w-[300px]">
            <div
              className="h-full bg-primary rounded-full transition-all duration-300"
              style={{ width: `${Math.min(100, pctNum)}%` }}
            />
          </div>
        )}
      </div>
      <Button
        size="icon"
        className="shrink-0 h-6 w-6"
        title="Remove from queue"
        disabled={removing}
        onClick={() => onRemove(job.game)}
      >
        {removing ? <Loader2 className="h-3 w-3 animate-spin" /> : <X className="h-3 w-3" />}
      </Button>
    </div>
  );
}

interface QueuePageProps {
  onBack: () => void;
}

export default function QueuePage({ onBack }: QueuePageProps) {
  const [jobs, setJobs]           = useState<Job[]>([]);
  const [loading, setLoading]     = useState(true);
  const [removing, setRemoving]   = useState<Record<string, boolean>>({});

  const fetchQueue = useCallback(async () => {
    const r = await window.godsendApi.getQueue().catch(() => ({ ok: false, jobs: [] }));
    if (r.ok) setJobs(Array.isArray(r.jobs) ? r.jobs : []);
    setLoading(false);
  }, []);

  // Initial load + auto-refresh every 3 s
  useEffect(() => {
    fetchQueue();
    const id = setInterval(fetchQueue, 3000);
    return () => clearInterval(id);
  }, [fetchQueue]);

  async function handleRemove(gameName: string) {
    setRemoving((prev) => ({ ...prev, [gameName]: true }));
    try {
      await window.godsendApi.removeFromQueue(gameName);
      await fetchQueue();
    } finally {
      setRemoving((prev) => ({ ...prev, [gameName]: false }));
    }
  }

  const activeCount  = jobs.filter(j => j.state === "Processing" || j.state === "Pending FTP").length;
  const pendingCount = jobs.filter(j => j.state === "Pending FTP").length;

  return (
    <div className="flex flex-col h-screen p-3 gap-2.5">
      {/* Header */}
      <header className="flex items-center gap-2.5 shrink-0 pb-3 border-b border-border">
        <Button size="icon" title="Back" onClick={onBack}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <span className="text-[15px] font-semibold text-foreground flex-1">
          Server Queue
          {!loading && jobs.length > 0 && (
            <span className="ml-2 text-[12px] font-normal text-muted-foreground">
              {jobs.length} job{jobs.length !== 1 ? "s" : ""}
              {activeCount > 0 ? `, ${activeCount} active` : ""}
              {pendingCount > 0 ? `, ${pendingCount} pending FTP` : ""}
            </span>
          )}
        </span>
        <Button size="icon" title="Refresh" onClick={fetchQueue} disabled={loading}>
          {loading
            ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
            : <RefreshCw className="h-3.5 w-3.5" />}
        </Button>
      </header>

      {/* Job list */}
      <div className="flex-1 min-h-0 overflow-auto">
        {loading && jobs.length === 0 ? (
          <div className="flex justify-center items-center h-24">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : jobs.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-24 gap-1">
            <p className="text-[13px] text-muted-foreground">No active jobs on the server.</p>
            <p className="text-[11px] text-muted-foreground">Start a download from the Aurora script to see progress here.</p>
          </div>
        ) : (
          <div className="pr-1">
            {jobs.map((job) => (
              <JobRow
                key={job.game}
                job={job}
                onRemove={handleRemove}
                removing={!!removing[job.game]}
              />
            ))}
          </div>
        )}
      </div>

      {/* Footer hint */}
      {pendingCount > 0 && (
        <footer className="shrink-0 px-1 py-1.5 border-t border-border">
          <p className="text-[11px] text-yellow-400/80">
            {pendingCount} job{pendingCount !== 1 ? "s are" : " is"} waiting for Xbox FTP.
            The server will retry automatically when the console is reachable.
          </p>
        </footer>
      )}
    </div>
  );
}
