import { useState, useEffect, useCallback } from "react";
import { RefreshCw, Loader2, X, Upload, HardDrive } from "lucide-react";
import { Button } from "./ui/button";
import { cn } from "../lib/utils";

// ── Unified job type ──────────────────────────────────────────────────────────

interface UnifiedJob {
  /** Unique key for React (game name for pipeline, "ftp-{id}" for FTP jobs) */
  key: string;
  /** Display name */
  name: string;
  /** Normalised state */
  state: string;
  /** Optional status message */
  message?: string;
  /** 0-100 progress (null = indeterminate) */
  progress: number | null;
  /** Transfer speed string (e.g. "2.4 MB/s") */
  speed?: string;
  /** "pipeline" | "ftp" — which source this job came from */
  source: "pipeline" | "ftp";
  /** Original FTP job id (for remove) */
  ftpJobId?: number;
}

// ── State helpers ─────────────────────────────────────────────────────────────

function stateColor(state: string) {
  if (state === "Ready")       return "text-green-400";
  if (state === "Error")       return "text-red-400";
  if (state === "Pending FTP") return "text-yellow-400";
  if (state === "Queued")      return "text-yellow-400";
  if (state === "Processing")  return "text-blue-400";
  return "text-muted-foreground";
}

function stateIcon(state: string) {
  if (state === "Ready")       return "OK";
  if (state === "Error")       return "!!";
  if (state === "Pending FTP") return "..";
  if (state === "Queued")      return "..";
  if (state === "Processing")  return ">>";
  return "?";
}

function sourceLabel(source: "pipeline" | "ftp") {
  return source === "pipeline" ? "Store" : "FTP";
}

function sourceIcon(source: "pipeline" | "ftp") {
  return source === "pipeline"
    ? <HardDrive className="h-2.5 w-2.5" />
    : <Upload className="h-2.5 w-2.5" />;
}

// ── Job row ───────────────────────────────────────────────────────────────────

interface JobRowProps {
  job: UnifiedJob;
  onRemove: (job: UnifiedJob) => void;
  removing: boolean;
}

function JobRow({ job, onRemove, removing }: JobRowProps) {
  const pct = job.progress;
  const isFinished = job.state === "Ready" || job.state === "Error";

  return (
    <div className="flex items-start gap-2 py-2 border-b border-[#1e242e] last:border-0">
      <span className={cn("font-mono text-[11px] mt-0.5 shrink-0 w-6 text-center", stateColor(job.state))}>
        {stateIcon(job.state)}
      </span>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-[13px] font-medium text-foreground truncate">{job.name}</span>
          <span className={cn("text-[11px] shrink-0", stateColor(job.state))}>{job.state}</span>
          {!isFinished && pct !== null && pct > 0 && (
            <span className="text-[11px] text-muted-foreground shrink-0">{pct}%</span>
          )}
          {!isFinished && job.speed && (
            <span className="text-[10px] text-muted-foreground/70 font-mono shrink-0">{job.speed}</span>
          )}
          <span className="flex items-center gap-0.5 text-[9px] text-muted-foreground/60 shrink-0 ml-auto border border-border/40 rounded px-1 py-0.5">
            {sourceIcon(job.source)}
            {sourceLabel(job.source)}
          </span>
        </div>
        {job.message && (
          <p className="text-[11px] text-muted-foreground mt-0.5 truncate" title={job.message}>
            {job.message}
          </p>
        )}
        {!isFinished && pct !== null && pct > 0 && (
          <div className="mt-1 h-1 bg-muted rounded-full overflow-hidden max-w-[300px]">
            <div
              className="h-full bg-primary rounded-full transition-all duration-300"
              style={{ width: `${Math.min(100, pct)}%` }}
            />
          </div>
        )}
      </div>
      <Button
        size="icon"
        className="shrink-0 h-6 w-6"
        title="Remove from queue"
        disabled={removing}
        onClick={() => onRemove(job)}
      >
        {removing ? <Loader2 className="h-3 w-3 animate-spin" /> : <X className="h-3 w-3" />}
      </Button>
    </div>
  );
}

// ── QueuePage ─────────────────────────────────────────────────────────────────

export default function QueuePage() {
  const [jobs, setJobs]           = useState<UnifiedJob[]>([]);
  const [loading, setLoading]     = useState(true);
  const [removing, setRemoving]   = useState<Record<string, boolean>>({});

  const fetchAll = useCallback(async () => {
    // Fetch both sources in parallel
    const [pipelineRes, ftpRes] = await Promise.all([
      window.godsendApi.getQueue().catch(() => ({ ok: false, jobs: [] })),
      window.godsendApi.toolsFtpUploadStatus().catch(() => ({ ok: false, jobs: [] })),
    ]);

    const unified: UnifiedJob[] = [];

    // Pipeline jobs (game downloads from Aurora Store)
    if (pipelineRes.ok && Array.isArray(pipelineRes.jobs)) {
      for (const j of pipelineRes.jobs) {
        // Extract percentage from message if present
        const pctMatch = (j.message || "").match(/\((\d+\.?\d*)%\)/) ||
                         (j.message || "").match(/:\s*(\d+)%/);
        const pctNum = pctMatch ? Math.round(parseFloat(pctMatch[1])) : null;

        unified.push({
          key:      `pipeline-${j.game}`,
          name:     j.game,
          state:    j.state,
          message:  j.message || undefined,
          progress: pctNum,
          source:   "pipeline",
        });
      }
    }

    // FTP Manager jobs (uploads, copies, moves, script uploads)
    if (ftpRes.ok && Array.isArray(ftpRes.jobs)) {
      for (const j of ftpRes.jobs) {
        // Build message: prefer detail over remote path; show remote path for completed/errored jobs
        const isActive = j.state === "Processing" || j.state === "Queued";
        const detailMsg = isActive && j.detail ? j.detail : undefined;
        const fallbackMsg = j.error || (j.remotePath ? `→ ${j.remotePath}` : undefined);

        unified.push({
          key:       `ftp-${j.id}`,
          name:      j.name,
          state:     j.state,
          message:   detailMsg || fallbackMsg,
          progress:  typeof j.progress === "number" ? j.progress : null,
          speed:     isActive && j.speed ? j.speed : undefined,
          source:    "ftp",
          ftpJobId:  j.id,
        });
      }
    }

    setJobs(unified);
    setLoading(false);
  }, []);

  // Initial load + auto-refresh every 3 s
  useEffect(() => {
    fetchAll();
    const id = setInterval(fetchAll, 3000);
    return () => clearInterval(id);
  }, [fetchAll]);

  async function handleRemove(job: UnifiedJob) {
    setRemoving((prev) => ({ ...prev, [job.key]: true }));
    try {
      if (job.source === "pipeline") {
        await window.godsendApi.removeFromQueue(job.name);
      } else if (job.ftpJobId != null) {
        await window.godsendApi.toolsFtpUploadRemove(job.ftpJobId);
      }
      await fetchAll();
    } finally {
      setRemoving((prev) => ({ ...prev, [job.key]: false }));
    }
  }

  const pipelineJobs   = jobs.filter(j => j.source === "pipeline");
  const ftpJobs        = jobs.filter(j => j.source === "ftp");
  const activeCount    = jobs.filter(j => j.state === "Processing" || j.state === "Queued").length;
  const pendingCount   = pipelineJobs.filter(j => j.state === "Pending FTP").length;
  const ftpActiveCount = ftpJobs.filter(j => j.state === "Processing" || j.state === "Queued").length;

  return (
    <div className="flex flex-col h-full p-3 gap-2.5">
      {/* Header — refresh + summary */}
      <header className="flex items-center gap-2.5 shrink-0">
        <span className="text-[13px] text-muted-foreground flex-1">
          {!loading && jobs.length > 0 && (
            <>
              {jobs.length} job{jobs.length !== 1 ? "s" : ""}
              {activeCount > 0 ? `, ${activeCount} active` : ""}
              {pendingCount > 0 ? `, ${pendingCount} pending FTP` : ""}
              {ftpActiveCount > 0 ? `, ${ftpActiveCount} FTP transfer${ftpActiveCount !== 1 ? "s" : ""}` : ""}
            </>
          )}
        </span>
        <Button size="icon" title="Refresh" onClick={fetchAll} disabled={loading}>
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
            <p className="text-[13px] text-muted-foreground">No active jobs.</p>
            <p className="text-[11px] text-muted-foreground">
              Game downloads, FTP uploads, copies, and moves will appear here.
            </p>
          </div>
        ) : (
          <div className="pr-1">
            {jobs.map((job) => (
              <JobRow
                key={job.key}
                job={job}
                onRemove={handleRemove}
                removing={!!removing[job.key]}
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
