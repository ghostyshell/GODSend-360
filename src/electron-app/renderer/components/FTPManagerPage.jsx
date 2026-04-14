import { useState, useEffect, useCallback } from "react";
import {
  ArrowLeft, Folder, File, Upload, Trash2, Loader2, RefreshCw,
  FolderPlus, ChevronRight, HardDrive, X, Check, AlertTriangle,
} from "lucide-react";
import { Button } from "./ui/button";
import { cn } from "../lib/utils";

function formatSize(bytes) {
  if (!bytes || bytes <= 0) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function stateColor(state) {
  if (state === "Ready")      return "text-green-400";
  if (state === "Error")      return "text-red-400";
  if (state === "Processing") return "text-blue-400";
  if (state === "Queued")     return "text-yellow-400";
  return "text-muted-foreground";
}

function UploadJobRow({ job, onRemove }) {
  const icon =
    job.state === "Ready"      ? <Check className="h-3 w-3 text-green-400" /> :
    job.state === "Error"      ? <AlertTriangle className="h-3 w-3 text-red-400" /> :
    job.state === "Processing" ? <Loader2 className="h-3 w-3 animate-spin text-blue-400" /> :
                                 <Upload className="h-3 w-3 text-yellow-400" />;

  return (
    <div className="flex items-center gap-2 py-1 px-2 border-b border-[#1e242e] last:border-0">
      {icon}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-[12px] text-foreground truncate">{job.name}</span>
          <span className={cn("text-[10px] shrink-0", stateColor(job.state))}>{job.state}</span>
          {job.state === "Processing" && job.progress > 0 && (
            <span className="text-[10px] text-muted-foreground shrink-0">{job.progress}%</span>
          )}
        </div>
        {job.error && <p className="text-[10px] text-red-400 truncate">{job.error}</p>}
        {job.state === "Processing" && job.progress > 0 && (
          <div className="mt-0.5 h-1 bg-muted rounded-full overflow-hidden max-w-[200px]">
            <div
              className="h-full bg-primary rounded-full transition-all duration-300"
              style={{ width: `${Math.min(100, job.progress)}%` }}
            />
          </div>
        )}
      </div>
      {(job.state === "Ready" || job.state === "Error") && (
        <Button size="icon" className="h-5 w-5 shrink-0" title="Remove" onClick={() => onRemove(job.id)}>
          <X className="h-2.5 w-2.5" />
        </Button>
      )}
    </div>
  );
}

export default function FTPManagerPage({ onBack }) {
  const [cwd, setCwd]             = useState("/");
  const [entries, setEntries]     = useState([]);
  const [loading, setLoading]     = useState(true);
  const [error, setError]         = useState(null);
  const [uploads, setUploads]     = useState([]);
  const [mkdirName, setMkdirName] = useState("");
  const [showMkdir, setShowMkdir] = useState(false);

  const fetchDir = useCallback(async (dir) => {
    setLoading(true);
    setError(null);
    const r = await window.godsendApi.toolsFtpList(dir).catch(err => ({ ok: false, error: err.message }));
    if (r.ok) {
      setEntries(r.entries.sort((a, b) => {
        if (a.type !== b.type) return a.type === "dir" ? -1 : 1;
        return a.name.localeCompare(b.name);
      }));
      setCwd(r.cwd);
    } else {
      setError(r.error || "Failed to list directory");
      setEntries([]);
    }
    setLoading(false);
  }, []);

  useEffect(() => {
    fetchDir("/");
  }, [fetchDir]);

  // Poll upload status
  useEffect(() => {
    const id = setInterval(async () => {
      const r = await window.godsendApi.toolsFtpUploadStatus().catch(() => ({ ok: false }));
      if (r.ok) setUploads(r.jobs || []);
    }, 2000);
    return () => clearInterval(id);
  }, []);

  function navigateTo(dir) {
    let target;
    if (dir === "..") {
      const parts = cwd.replace(/\/+$/, "").split("/").filter(Boolean);
      parts.pop();
      target = "/" + parts.join("/");
    } else {
      target = cwd.replace(/\/+$/, "") + "/" + dir;
    }
    fetchDir(target);
  }

  async function handleUploadFiles() {
    const r = await window.godsendApi.toolsFtpChooseFiles();
    if (!r.ok) return;
    await window.godsendApi.toolsFtpUpload({ localPaths: r.files, remotePath: cwd });
  }

  async function handleUploadFolder() {
    const r = await window.godsendApi.toolsFtpChooseFolder();
    if (!r.ok) return;
    await window.godsendApi.toolsFtpUpload({ localPaths: [r.folder], remotePath: cwd });
  }

  async function handleDelete(entry) {
    const target = cwd.replace(/\/+$/, "") + "/" + entry.name;
    const r = await window.godsendApi.toolsFtpDelete(target);
    if (r.ok) fetchDir(cwd);
  }

  async function handleMkdir() {
    if (!mkdirName.trim()) return;
    const target = cwd.replace(/\/+$/, "") + "/" + mkdirName.trim();
    const r = await window.godsendApi.toolsFtpMkdir(target);
    if (r.ok) {
      setMkdirName("");
      setShowMkdir(false);
      fetchDir(cwd);
    }
  }

  async function handleRemoveUpload(id) {
    await window.godsendApi.toolsFtpUploadRemove(id);
    setUploads(prev => prev.filter(j => j.id !== id));
  }

  const breadcrumbs = cwd.split("/").filter(Boolean);
  const activeUploads = uploads.filter(j => j.state === "Processing" || j.state === "Queued");

  return (
    <div className="flex flex-col h-screen p-3 gap-2.5">
      <header className="flex items-center gap-2.5 shrink-0 pb-3 border-b border-border">
        <Button size="icon" title="Back" onClick={onBack}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <span className="text-[15px] font-semibold text-foreground">
          FTP Manager
        </span>
        {activeUploads.length > 0 && (
          <span className="text-[11px] text-blue-400">
            {activeUploads.length} transfer{activeUploads.length !== 1 ? "s" : ""} active
          </span>
        )}
        <div className="flex-1" />
        <Button size="icon" title="Refresh" onClick={() => fetchDir(cwd)} disabled={loading}>
          {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
        </Button>
      </header>

      {/* Breadcrumb path bar */}
      <div className="flex items-center gap-1 shrink-0 text-[11px] text-muted-foreground overflow-x-auto">
        <button
          className="hover:text-foreground transition-colors shrink-0"
          onClick={() => fetchDir("/")}
        >
          <HardDrive className="h-3 w-3" />
        </button>
        {breadcrumbs.map((part, i) => (
          <span key={i} className="flex items-center gap-1 shrink-0">
            <ChevronRight className="h-3 w-3 opacity-40" />
            <button
              className="hover:text-foreground transition-colors"
              onClick={() => fetchDir("/" + breadcrumbs.slice(0, i + 1).join("/"))}
            >
              {part}
            </button>
          </span>
        ))}
      </div>

      {/* Toolbar */}
      <div className="flex items-center gap-1.5 shrink-0">
        <Button size="sm" onClick={handleUploadFiles}>
          <Upload className="h-3 w-3 mr-1" />
          Upload Files
        </Button>
        <Button size="sm" onClick={handleUploadFolder}>
          <Folder className="h-3 w-3 mr-1" />
          Upload Folder
        </Button>
        {showMkdir ? (
          <div className="flex items-center gap-1">
            <input
              type="text"
              className="h-7 px-2 text-[12px] bg-surface border border-border rounded-md text-foreground w-32 outline-none focus:border-primary"
              placeholder="Folder name"
              value={mkdirName}
              onChange={e => setMkdirName(e.target.value)}
              onKeyDown={e => { if (e.key === "Enter") handleMkdir(); if (e.key === "Escape") setShowMkdir(false); }}
              autoFocus
            />
            <Button size="sm" onClick={handleMkdir} disabled={!mkdirName.trim()}>Create</Button>
            <Button size="sm" onClick={() => setShowMkdir(false)}>Cancel</Button>
          </div>
        ) : (
          <Button size="sm" onClick={() => setShowMkdir(true)}>
            <FolderPlus className="h-3 w-3 mr-1" />
            New Folder
          </Button>
        )}
      </div>

      {/* File browser + upload queue side by side */}
      <div className="flex-1 min-h-0 flex gap-2.5">
        {/* Directory listing */}
        <div className="flex-1 min-w-0 overflow-auto border border-border rounded-lg">
          {error ? (
            <div className="flex items-center justify-center h-24 text-[13px] text-red-400">
              {error}
            </div>
          ) : loading && entries.length === 0 ? (
            <div className="flex items-center justify-center h-24">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <div>
              {/* Back row */}
              {cwd !== "/" && (
                <button
                  className="flex items-center gap-2 w-full px-2 py-1.5 hover:bg-accent/50 transition-colors text-left border-b border-[#1e242e]"
                  onClick={() => navigateTo("..")}
                >
                  <Folder className="h-3.5 w-3.5 text-yellow-400/70" />
                  <span className="text-[12px] text-muted-foreground">..</span>
                </button>
              )}
              {entries.map(entry => (
                <div
                  key={entry.name}
                  className="flex items-center gap-2 px-2 py-1.5 hover:bg-accent/50 transition-colors border-b border-[#1e242e] last:border-0 group"
                >
                  {entry.type === "dir" ? (
                    <button
                      className="flex items-center gap-2 flex-1 min-w-0 text-left"
                      onClick={() => navigateTo(entry.name)}
                    >
                      <Folder className="h-3.5 w-3.5 text-yellow-400/70 shrink-0" />
                      <span className="text-[12px] text-foreground truncate">{entry.name}</span>
                    </button>
                  ) : (
                    <div className="flex items-center gap-2 flex-1 min-w-0">
                      <File className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                      <span className="text-[12px] text-foreground truncate">{entry.name}</span>
                      <span className="text-[10px] text-muted-foreground shrink-0">{formatSize(entry.size)}</span>
                    </div>
                  )}
                  <Button
                    size="icon"
                    className="h-5 w-5 opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
                    title="Delete"
                    onClick={() => handleDelete(entry)}
                  >
                    <Trash2 className="h-2.5 w-2.5" />
                  </Button>
                </div>
              ))}
              {entries.length === 0 && !loading && (
                <div className="flex items-center justify-center h-24 text-[12px] text-muted-foreground">
                  Empty directory
                </div>
              )}
            </div>
          )}
        </div>

        {/* Upload queue panel */}
        {uploads.length > 0 && (
          <div className="w-[280px] shrink-0 border border-border rounded-lg overflow-auto">
            <div className="px-2 py-1.5 border-b border-border bg-surface">
              <span className="text-[11px] font-semibold text-foreground">Transfers</span>
              <span className="text-[10px] text-muted-foreground ml-1.5">{uploads.length}</span>
            </div>
            {uploads.map(job => (
              <UploadJobRow key={job.id} job={job} onRemove={handleRemoveUpload} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
