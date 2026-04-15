import { useState } from "react";
import {
  ArrowLeft, FolderOpen, FileUp, Loader2, Check, AlertTriangle, X, Disc,
} from "lucide-react";
import { Button } from "./ui/button";

interface IsoFile {
  path: string;
  state: "pending" | "converting" | "done" | "error";
  info: any | null;
  error: string | null;
  outputDir: string | null;
}

interface IsoRowProps {
  file: IsoFile;
  onRemove: (path: string) => void;
}

function IsoRow({ file, onRemove }: IsoRowProps) {
  const name = file.path.split(/[\\/]/).pop();
  const stateIcon =
    file.state === "done"       ? <Check className="h-3.5 w-3.5 text-green-400" /> :
    file.state === "converting" ? <Loader2 className="h-3.5 w-3.5 animate-spin text-blue-400" /> :
    file.state === "error"      ? <AlertTriangle className="h-3.5 w-3.5 text-red-400" /> :
                                  <Disc className="h-3.5 w-3.5 text-muted-foreground" />;

  return (
    <div className="flex items-center gap-2 py-1.5 px-2 border-b border-[#1e242e] last:border-0">
      {stateIcon}
      <div className="flex-1 min-w-0">
        <p className="text-[13px] text-foreground truncate" title={file.path}>{name}</p>
        {file.info && (
          <p className="text-[11px] text-muted-foreground">
            {file.info.displayName || "Unknown"} — {file.info.titleId}
          </p>
        )}
        {file.error && (
          <p className="text-[11px] text-red-400">{file.error}</p>
        )}
        {file.outputDir && (
          <p className="text-[11px] text-green-400/80 truncate" title={file.outputDir}>
            Output: {file.outputDir}
          </p>
        )}
      </div>
      {file.state !== "converting" && (
        <Button size="icon" className="shrink-0 h-6 w-6" title="Remove" onClick={() => onRemove(file.path)}>
          <X className="h-3 w-3" />
        </Button>
      )}
    </div>
  );
}

interface ISO2GODPageProps {
  onBack: () => void;
}

export default function ISO2GODPage({ onBack }: ISO2GODPageProps) {
  const [files, setFiles]     = useState<IsoFile[]>([]);
  const [outDir, setOutDir]   = useState("");
  const [busy, setBusy]       = useState(false);

  async function handleSelectISOs() {
    const r = await window.godsendApi.toolsChooseIsoFiles();
    if (!r.ok) return;
    const existing = new Set(files.map(f => f.path));
    const newFiles: IsoFile[] = r.files
      .filter((p: string) => !existing.has(p))
      .map((p: string) => ({ path: p, state: "pending", info: null, error: null, outputDir: null }));
    if (!newFiles.length) return;

    setFiles(prev => [...prev, ...newFiles]);

    // Probe each ISO in background
    for (const nf of newFiles) {
      window.godsendApi.toolsProbeIso(nf.path).then((result: any) => {
        setFiles(prev => prev.map(f =>
          f.path === nf.path ? { ...f, info: result.ok ? result : null } : f
        ));
      });
    }
  }

  async function handleSelectOutDir() {
    const r = await window.godsendApi.toolsChooseOutputFolder();
    if (r.ok) setOutDir(r.folder);
  }

  function handleRemove(path: string) {
    setFiles(prev => prev.filter(f => f.path !== path));
  }

  async function handleConvert() {
    if (!outDir || !files.length) return;
    setBusy(true);

    for (const file of files) {
      if (file.state === "done") continue;
      setFiles(prev => prev.map(f =>
        f.path === file.path ? { ...f, state: "converting", error: null } : f
      ));
      try {
        const r = await window.godsendApi.toolsIso2God({ isoPath: file.path, outDir });
        if (r.ok) {
          setFiles(prev => prev.map(f =>
            f.path === file.path ? { ...f, state: "done", outputDir: r.outputDir, info: r } : f
          ));
        } else {
          setFiles(prev => prev.map(f =>
            f.path === file.path ? { ...f, state: "error", error: r.error || "Conversion failed" } : f
          ));
        }
      } catch (err: any) {
        setFiles(prev => prev.map(f =>
          f.path === file.path ? { ...f, state: "error", error: err.message || "Unknown error" } : f
        ));
      }
    }
    setBusy(false);
  }

  const pending = files.filter(f => f.state !== "done");
  const canConvert = outDir && pending.length > 0 && !busy;

  return (
    <div className="flex flex-col h-screen p-3 gap-2.5">
      <header className="flex items-center gap-2.5 shrink-0 pb-3 border-b border-border">
        <Button size="icon" title="Back" onClick={onBack}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <span className="text-[15px] font-semibold text-foreground flex-1">
          ISO to GOD
        </span>
      </header>

      {/* Controls */}
      <div className="flex items-center gap-2 shrink-0">
        <Button size="sm" onClick={handleSelectISOs} disabled={busy}>
          <FileUp className="h-3.5 w-3.5 mr-1" />
          Add ISOs
        </Button>
        <Button size="sm" onClick={handleSelectOutDir} disabled={busy}>
          <FolderOpen className="h-3.5 w-3.5 mr-1" />
          Output Folder
        </Button>
        {outDir && (
          <span className="text-[11px] text-muted-foreground truncate min-w-0 flex-1" title={outDir}>
            {outDir}
          </span>
        )}
      </div>

      {/* File list */}
      <div className="flex-1 min-h-0 overflow-auto border border-border rounded-lg">
        {files.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-full gap-1 text-muted-foreground">
            <Disc className="h-8 w-8 opacity-40" />
            <p className="text-[13px]">No ISO files selected</p>
            <p className="text-[11px]">Click "Add ISOs" to select Xbox 360 disc images</p>
          </div>
        ) : (
          <div className="pr-1">
            {files.map(f => (
              <IsoRow key={f.path} file={f} onRemove={handleRemove} />
            ))}
          </div>
        )}
      </div>

      {/* Footer */}
      <footer className="flex items-center gap-2 shrink-0">
        <Button
          variant="primary"
          disabled={!canConvert}
          onClick={handleConvert}
        >
          {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin mr-1" /> : null}
          {busy ? "Converting..." : `Convert ${pending.length} ISO${pending.length !== 1 ? "s" : ""} to GOD`}
        </Button>
        <span className="text-[11px] text-muted-foreground">
          Output: Title Name - TitleID
        </span>
      </footer>
    </div>
  );
}
