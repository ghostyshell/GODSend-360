import { useState, useEffect, useCallback, useRef } from "react";
import {
  Folder, File, Upload, Trash2, Loader2, RefreshCw,
  FolderPlus, ChevronRight, HardDrive, X, Check, AlertTriangle,
  Scissors, Copy, ClipboardPaste, Clipboard, ChevronDown,
} from "lucide-react";
import { Button } from "./ui/button";
import { cn } from "../lib/utils";

interface FtpEntry {
  name: string;
  type: "dir" | "file";
  size?: number;
}

interface UploadJob {
  id: number;
  name: string;
  state: "Queued" | "Processing" | "Ready" | "Error";
  progress?: number;
  error?: string;
}

interface ClipboardItem {
  name: string;
  type: "dir" | "file";
}

interface ClipboardState {
  mode: "cut" | "copy";
  sourceDir: string;
  items: ClipboardItem[];
}

interface ContextMenuState {
  x: number;
  y: number;
  entry: FtpEntry | null;
}

function formatSize(bytes: number | undefined) {
  if (!bytes || bytes <= 0) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function stateColor(state: string) {
  if (state === "Ready")      return "text-green-400";
  if (state === "Error")      return "text-red-400";
  if (state === "Processing") return "text-blue-400";
  if (state === "Queued")     return "text-yellow-400";
  return "text-muted-foreground";
}

interface UploadJobRowProps {
  job: UploadJob;
  onRemove: (id: number) => void;
}

function UploadJobRow({ job, onRemove }: UploadJobRowProps) {
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
          {job.state === "Processing" && job.progress !== undefined && job.progress > 0 && (
            <span className="text-[10px] text-muted-foreground shrink-0">{job.progress}%</span>
          )}
        </div>
        {job.error && <p className="text-[10px] text-red-400 truncate">{job.error}</p>}
        {job.state === "Processing" && job.progress !== undefined && job.progress > 0 && (
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

interface FTPManagerPageProps {
}

export default function FTPManagerPage({}: FTPManagerPageProps) {
  const [cwd, setCwd]             = useState("/");
  const [entries, setEntries]     = useState<FtpEntry[]>([]);
  const [loading, setLoading]     = useState(true);
  const [error, setError]         = useState<string | null>(null);
  const [uploads, setUploads]     = useState<UploadJob[]>([]);
  const [mkdirName, setMkdirName] = useState("");
  const [showMkdir, setShowMkdir] = useState(false);

  // ── Selection state ─────────────────────────────────────────────────────
  const [selected, setSelected]   = useState<Set<string>>(new Set());

  // ── Clipboard state ─────────────────────────────────────────────────────
  const [clipboard, setClipboard] = useState<ClipboardState | null>(null);
  const [showClipboard, setShowClipboard] = useState(false);
  const clipboardRef = useRef<HTMLDivElement>(null);
  const [pasting, setPasting]     = useState(false);

  // ── Context menu state ──────────────────────────────────────────────────
  const [ctxMenu, setCtxMenu]     = useState<ContextMenuState | null>(null);
  const ctxRef                    = useRef<HTMLDivElement>(null);

  // Close dropdowns / context menu on outside click
  useEffect(() => {
    function onPointerDown(e: PointerEvent) {
      if (ctxMenu && ctxRef.current && !ctxRef.current.contains(e.target as Node)) setCtxMenu(null);
      if (showClipboard && clipboardRef.current && !clipboardRef.current.contains(e.target as Node)) setShowClipboard(false);
    }
    document.addEventListener("pointerdown", onPointerDown, true);
    return () => document.removeEventListener("pointerdown", onPointerDown, true);
  }, [ctxMenu, showClipboard]);

  const fetchDir = useCallback(async (dir: string) => {
    setLoading(true);
    setError(null);
    setSelected(new Set());
    const r = await window.godsendApi.toolsFtpList(dir).catch((err: any) => ({ ok: false, error: err.message }));
    if (r.ok) {
      setEntries(r.entries.sort((a: FtpEntry, b: FtpEntry) => {
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

  function navigateTo(dir: string) {
    let target: string;
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

  async function handleDelete(entry: FtpEntry) {
    const target = cwd.replace(/\/+$/, "") + "/" + entry.name;
    const r = await window.godsendApi.toolsFtpDelete(target);
    if (r.ok) fetchDir(cwd);
  }

  async function handleDeleteSelected() {
    for (const name of selected) {
      const target = cwd.replace(/\/+$/, "") + "/" + name;
      await window.godsendApi.toolsFtpDelete(target);
    }
    setSelected(new Set());
    fetchDir(cwd);
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

  async function handleRemoveUpload(id: number) {
    await window.godsendApi.toolsFtpUploadRemove(id);
    setUploads(prev => prev.filter(j => j.id !== id));
  }

  // ── Selection helpers ───────────────────────────────────────────────────
  function toggleSelect(name: string, e: React.MouseEvent) {
    setSelected(prev => {
      const next = new Set(prev);
      if (e && e.shiftKey && prev.size > 0) {
        // Range select: from last selected to this one
        const names = entries.map(en => en.name);
        const lastSelected = [...prev].pop()!;
        const from = names.indexOf(lastSelected);
        const to = names.indexOf(name);
        if (from !== -1 && to !== -1) {
          const [lo, hi] = from < to ? [from, to] : [to, from];
          for (let i = lo; i <= hi; i++) next.add(names[i]);
        }
      } else if (e && (e.ctrlKey || e.metaKey)) {
        if (next.has(name)) next.delete(name); else next.add(name);
      } else {
        if (next.has(name) && next.size === 1) {
          next.delete(name);
        } else {
          next.clear();
          next.add(name);
        }
      }
      return next;
    });
  }

  function selectAll() {
    setSelected(new Set(entries.map(e => e.name)));
  }

  // ── Clipboard operations ────────────────────────────────────────────────
  function getSelectedItems(): ClipboardItem[] {
    return entries.filter(e => selected.has(e.name)).map(e => ({ name: e.name, type: e.type }));
  }

  function handleCut() {
    const items = getSelectedItems();
    if (items.length === 0) return;
    setClipboard({ mode: "cut", sourceDir: cwd, items });
    setCtxMenu(null);
  }

  function handleCopy() {
    const items = getSelectedItems();
    if (items.length === 0) return;
    setClipboard({ mode: "copy", sourceDir: cwd, items });
    setCtxMenu(null);
  }

  async function handlePaste() {
    if (!clipboard || clipboard.items.length === 0) return;
    setCtxMenu(null);
    setPasting(true);
    try {
      const srcDir = clipboard.sourceDir.replace(/\/+$/, "");
      const dstDir = cwd.replace(/\/+$/, "");

      for (const item of clipboard.items) {
        const src = srcDir + "/" + item.name;
        const dst = dstDir + "/" + item.name;
        if (clipboard.mode === "cut") {
          await window.godsendApi.toolsFtpRename({ from: src, to: dst });
        } else {
          await window.godsendApi.toolsFtpCopy({ src, dst, isDir: item.type === "dir" });
        }
      }
      // Clear clipboard after cut (move); keep after copy
      if (clipboard.mode === "cut") setClipboard(null);
      fetchDir(cwd);
    } finally {
      setPasting(false);
    }
  }

  function clearClipboard() {
    setClipboard(null);
    setShowClipboard(false);
  }

  // ── Context menu helpers ────────────────────────────────────────────────
  function handleContextMenu(e: React.MouseEvent, entry: FtpEntry) {
    e.preventDefault();
    e.stopPropagation();
    // If right-clicking an unselected entry, select only that one
    if (entry && !selected.has(entry.name)) {
      setSelected(new Set([entry.name]));
    }
    setCtxMenu({ x: e.clientX, y: e.clientY, entry });
  }

  function handleBgContextMenu(e: React.MouseEvent) {
    e.preventDefault();
    setCtxMenu({ x: e.clientX, y: e.clientY, entry: null });
  }

  // Items that are being cut (shown dimmed)
  const cutNames = clipboard?.mode === "cut" && clipboard.sourceDir === cwd
    ? new Set(clipboard.items.map(i => i.name))
    : new Set<string>();

  const canPaste = clipboard && clipboard.items.length > 0 &&
    clipboard.sourceDir !== cwd; // don't paste into same directory

  const breadcrumbs = cwd.split("/").filter(Boolean);
  const activeUploads = uploads.filter(j => j.state === "Processing" || j.state === "Queued");

  return (
    <div className="flex flex-col h-full p-3 gap-2.5">
      <header className="flex items-center gap-2.5 shrink-0">
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

        {/* Separator */}
        <div className="w-px h-5 bg-border mx-0.5" />

        {/* Cut / Copy / Paste buttons */}
        <Button
          size="sm"
          onClick={handleCut}
          disabled={selected.size === 0}
          title="Cut selected (Ctrl+X)"
        >
          <Scissors className="h-3 w-3 mr-1" />
          Cut
        </Button>
        <Button
          size="sm"
          onClick={handleCopy}
          disabled={selected.size === 0}
          title="Copy selected (Ctrl+C)"
        >
          <Copy className="h-3 w-3 mr-1" />
          Copy
        </Button>
        <Button
          size="sm"
          onClick={handlePaste}
          disabled={!canPaste || pasting}
          title="Paste here (Ctrl+V)"
        >
          {pasting
            ? <Loader2 className="h-3 w-3 mr-1 animate-spin" />
            : <ClipboardPaste className="h-3 w-3 mr-1" />}
          Paste
        </Button>

        {/* Clipboard dropdown */}
        <div className="relative" ref={clipboardRef}>
          <Button
            size="sm"
            variant={clipboard ? "secondary" : "ghost"}
            className={cn(
              "h-7 px-1.5 gap-1",
              clipboard && "ring-1 ring-primary/50"
            )}
            title="Clipboard contents"
            disabled={!clipboard}
            onClick={() => setShowClipboard(!showClipboard)}
          >
            <Clipboard className="h-3 w-3" />
            {clipboard && (
              <span className="min-w-[14px] h-[14px] flex items-center justify-center rounded-full bg-primary text-[9px] font-bold text-primary-foreground leading-none">
                {clipboard.items.length}
              </span>
            )}
            <ChevronDown className="h-2.5 w-2.5" />
          </Button>
          {showClipboard && clipboard && (
            <div className="absolute right-0 top-full mt-1 z-50 min-w-[240px] max-w-[320px] rounded-md border border-border bg-popover shadow-lg">
              <div className="flex items-center justify-between px-2.5 py-1.5 border-b border-border">
                <span className="text-[11px] font-semibold text-foreground">
                  {clipboard.mode === "cut" ? "Cut" : "Copied"} — {clipboard.items.length} item{clipboard.items.length !== 1 ? "s" : ""}
                </span>
                <button
                  className="text-[10px] text-muted-foreground hover:text-foreground transition-colors"
                  onClick={clearClipboard}
                >
                  Clear
                </button>
              </div>
              <div className="text-[10px] text-muted-foreground px-2.5 py-1 border-b border-border/50">
                From: {clipboard.sourceDir}
              </div>
              <div className="max-h-[200px] overflow-auto py-1">
                {clipboard.items.map((item) => (
                  <div key={item.name} className="flex items-center gap-1.5 px-2.5 py-0.5">
                    {item.type === "dir"
                      ? <Folder className="h-3 w-3 text-yellow-400/70 shrink-0" />
                      : <File className="h-3 w-3 text-muted-foreground shrink-0" />}
                    <span className="text-[11px] text-foreground truncate">{item.name}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Select All / Deselect when items are selected */}
        {selected.size > 0 && (
          <>
            <div className="w-px h-5 bg-border mx-0.5" />
            <span className="text-[11px] text-muted-foreground">
              {selected.size} selected
            </span>
            <Button size="sm" variant="ghost" className="h-7 text-[11px] px-1.5" onClick={selectAll}>
              All
            </Button>
            <Button size="sm" variant="ghost" className="h-7 text-[11px] px-1.5" onClick={() => setSelected(new Set())}>
              None
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 text-[11px] px-1.5 text-red-400 hover:text-red-300"
              onClick={handleDeleteSelected}
            >
              <Trash2 className="h-3 w-3 mr-0.5" />
              Delete
            </Button>
          </>
        )}
      </div>

      {/* File browser + upload queue side by side */}
      <div className="flex-1 min-h-0 flex gap-2.5">
        {/* Directory listing */}
        <div
          className="flex-1 min-w-0 overflow-auto border border-border rounded-lg"
          onContextMenu={handleBgContextMenu}
        >
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
              {entries.map(entry => {
                const isSel = selected.has(entry.name);
                const isCut = cutNames.has(entry.name);
                return (
                  <div
                    key={entry.name}
                    className={cn(
                      "flex items-center gap-2 px-2 py-1.5 transition-colors border-b border-[#1e242e] last:border-0 group cursor-default",
                      isSel ? "bg-primary/15" : "hover:bg-accent/50",
                      isCut && "opacity-50",
                    )}
                    onClick={(e) => toggleSelect(entry.name, e)}
                    onContextMenu={(e) => handleContextMenu(e, entry)}
                  >
                    {entry.type === "dir" ? (
                      <button
                        className="flex items-center gap-2 flex-1 min-w-0 text-left"
                        onClick={(e) => { e.stopPropagation(); navigateTo(entry.name); }}
                        onDoubleClick={(e) => e.stopPropagation()}
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
                      onClick={(e) => { e.stopPropagation(); handleDelete(entry); }}
                    >
                      <Trash2 className="h-2.5 w-2.5" />
                    </Button>
                  </div>
                );
              })}
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

      {/* ── Context menu ────────────────────────────────────────────────── */}
      {ctxMenu && (
        <div
          ref={ctxRef}
          className="fixed z-[100] min-w-[160px] rounded-md border border-border bg-popover shadow-xl py-1"
          style={{ left: ctxMenu.x, top: ctxMenu.y }}
        >
          {ctxMenu.entry && (
            <>
              <button
                className="flex items-center gap-2 w-full px-3 py-1.5 text-[12px] text-foreground hover:bg-accent/60 transition-colors text-left"
                onClick={() => { handleCut(); setCtxMenu(null); }}
              >
                <Scissors className="h-3 w-3" /> Cut
                <span className="ml-auto text-[10px] text-muted-foreground">Ctrl+X</span>
              </button>
              <button
                className="flex items-center gap-2 w-full px-3 py-1.5 text-[12px] text-foreground hover:bg-accent/60 transition-colors text-left"
                onClick={() => { handleCopy(); setCtxMenu(null); }}
              >
                <Copy className="h-3 w-3" /> Copy
                <span className="ml-auto text-[10px] text-muted-foreground">Ctrl+C</span>
              </button>
              <div className="my-1 border-t border-border/50" />
            </>
          )}
          <button
            className={cn(
              "flex items-center gap-2 w-full px-3 py-1.5 text-[12px] text-left transition-colors",
              canPaste ? "text-foreground hover:bg-accent/60" : "text-muted-foreground/50 cursor-not-allowed",
            )}
            disabled={!canPaste}
            onClick={() => { handlePaste(); setCtxMenu(null); }}
          >
            <ClipboardPaste className="h-3 w-3" /> Paste
            {clipboard && (
              <span className="ml-1 text-[10px] text-muted-foreground">
                ({clipboard.items.length} {clipboard.mode === "cut" ? "cut" : "copied"})
              </span>
            )}
            <span className="ml-auto text-[10px] text-muted-foreground">Ctrl+V</span>
          </button>
          {ctxMenu.entry && (
            <>
              <div className="my-1 border-t border-border/50" />
              <button
                className="flex items-center gap-2 w-full px-3 py-1.5 text-[12px] text-red-400 hover:bg-accent/60 transition-colors text-left"
                onClick={() => {
                  if (selected.size > 1) handleDeleteSelected();
                  else if (ctxMenu.entry) handleDelete(ctxMenu.entry);
                  setCtxMenu(null);
                }}
              >
                <Trash2 className="h-3 w-3" />
                Delete{selected.size > 1 ? ` (${selected.size})` : ""}
              </button>
            </>
          )}
          {selected.size > 0 && (
            <>
              <div className="my-1 border-t border-border/50" />
              <button
                className="flex items-center gap-2 w-full px-3 py-1.5 text-[12px] text-foreground hover:bg-accent/60 transition-colors text-left"
                onClick={() => { selectAll(); setCtxMenu(null); }}
              >
                Select All
                <span className="ml-auto text-[10px] text-muted-foreground">Ctrl+A</span>
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
