import { useCallback, useEffect, useState } from "react";
import {
  AlertTriangle, Check, HardDrive, Loader2, RefreshCw, Usb,
} from "lucide-react";
import { Button } from "./ui/button";
import { Checkbox } from "./ui/checkbox";

interface UsbDrive {
  rootPath: string;
  label: string;
  sizeBytes: number;
}

function formatBytes(n: number): string {
  if (!n || n <= 0) return "";
  const gb = n / (1024 ** 3);
  if (gb >= 1) return `${gb.toFixed(1)} GB`;
  const mb = n / (1024 ** 2);
  return `${mb.toFixed(0)} MB`;
}

interface OptionRowProps {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
  hint?: string;
  disabled?: boolean;
}

function OptionRow({ checked, onChange, label, hint, disabled }: OptionRowProps) {
  return (
    <label className={`flex items-start gap-2.5 text-[13px] select-none ${disabled ? "opacity-50 cursor-not-allowed" : "cursor-pointer"}`}>
      <Checkbox
        checked={checked}
        disabled={disabled}
        onCheckedChange={(v) => onChange(v === true)}
        className="mt-0.5"
      />
      <span>
        <span className="text-foreground">{label}</span>
        {hint && <span className="block text-[11px] text-muted-foreground mt-0.5">{hint}</span>}
      </span>
    </label>
  );
}

export default function BadAvatarUsbPage() {
  const [drives, setDrives] = useState<UsbDrive[]>([]);
  const [selectedDrive, setSelectedDrive] = useState("");
  const [drivesLoading, setDrivesLoading] = useState(false);
  const [isAdmin, setIsAdmin] = useState(true);
  const [platform, setPlatform] = useState("");

  const [formatDrive, setFormatDrive] = useState(true);
  const [overwriteExisting, setOverwriteExisting] = useState(true);
  const [installProto, setInstallProto] = useState(true);
  const [installFreestyle, setInstallFreestyle] = useState(true);
  const [installAurora, setInstallAurora] = useState(true);

  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const [percent, setPercent] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  const refreshDrives = useCallback(async () => {
    setDrivesLoading(true);
    try {
      const [driveRes, adminRes] = await Promise.all([
        window.godsendApi.toolsBadAvatarListDrives(),
        window.godsendApi.toolsBadAvatarIsAdmin(),
      ]);
      if (driveRes.ok && Array.isArray(driveRes.drives)) {
        setDrives(driveRes.drives);
        if (driveRes.drives.length > 0) {
          setSelectedDrive((prev) =>
            prev && driveRes.drives.some((d: UsbDrive) => d.rootPath === prev)
              ? prev
              : driveRes.drives[0].rootPath,
          );
        } else {
          setSelectedDrive("");
        }
      }
      if (adminRes.ok) {
        setIsAdmin(adminRes.isAdmin === true);
        setPlatform(adminRes.platform || "");
        if (adminRes.formatRequiresElevation && !adminRes.isAdmin) {
          setFormatDrive(false);
        }
      }
    } finally {
      setDrivesLoading(false);
    }
  }, []);

  useEffect(() => {
    refreshDrives();
  }, [refreshDrives]);

  useEffect(() => {
    const cleanup = window.godsendApi.onBadAvatarProgress((p: { status: string; percent: number }) => {
      setStatus(p.status);
      setPercent(p.percent);
    });
    return cleanup;
  }, []);

  async function handleCreate() {
    if (!selectedDrive || busy) return;

    if (formatDrive) {
      const ok = window.confirm(
        `Format and configure ${selectedDrive}?\n\nThis will erase all data on the device. Make sure you selected the correct USB drive.`,
      );
      if (!ok) return;
    }

    setBusy(true);
    setError(null);
    setDone(false);
    setStatus("Starting…");
    setPercent(0);

    try {
      const r = await window.godsendApi.toolsBadAvatarCreate({
        driveRoot: selectedDrive,
        formatDrive,
        overwriteExisting,
        installProto,
        installFreestyle,
        installAurora,
      });
      if (r.ok) {
        setDone(true);
      } else {
        setError(r.error || "Setup failed");
      }
    } catch (err: any) {
      setError(err.message || "Unknown error");
    } finally {
      setBusy(false);
    }
  }

  const canCreate = selectedDrive && !busy && drives.length > 0;

  return (
    <div className="flex flex-col h-full p-3 gap-3 max-w-2xl">
      <p className="text-[12px] text-muted-foreground leading-relaxed shrink-0">
        Create a BadAvatar USB for the Xbox 360 BadUpdate exploit. Packages are downloaded from{" "}
        <a
          href="https://github.com/LxcyDr0p/BadStick"
          className="text-primary hover:underline"
          target="_blank"
          rel="noreferrer"
        >
          BadStick
        </a>{" "}
        release assets and extracted to your USB drive. Formatting uses platform tools
        (fat32format on Windows, newfs_msdos/diskutil on macOS, mkfs.vfat on Linux) and
        supports drives larger than 32&nbsp;GB.
      </p>

      {/* Drive picker */}
      <section className="border border-border rounded-lg p-3 flex flex-col gap-2 shrink-0">
        <div className="flex items-center gap-2">
          <Usb className="h-4 w-4 text-yellow-400 shrink-0" />
          <span className="text-[13px] font-medium text-foreground">USB drive</span>
          <Button
            size="icon"
            className="ml-auto h-7 w-7"
            title="Refresh drives"
            disabled={drivesLoading || busy}
            onClick={refreshDrives}
          >
            {drivesLoading
              ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
              : <RefreshCw className="h-3.5 w-3.5" />}
          </Button>
        </div>

        {drives.length === 0 ? (
          <div className="flex items-center gap-2 text-[12px] text-amber-400/90 py-1">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
            No USB drives detected. Insert a USB stick (any filesystem — it can be formatted below).
          </div>
        ) : (
          <select
            className="w-full bg-surface border border-border rounded-md px-2.5 py-1.5 text-[13px] text-foreground"
            value={selectedDrive}
            disabled={busy}
            onChange={(e) => setSelectedDrive(e.target.value)}
          >
            {drives.map((d) => (
              <option key={d.rootPath} value={d.rootPath}>
                {d.rootPath} ({d.label}{formatBytes(d.sizeBytes) ? ` — ${formatBytes(d.sizeBytes)}` : ""})
              </option>
            ))}
          </select>
        )}

        {!isAdmin && (
          <p className="text-[11px] text-amber-400/90">
            {platform === "linux"
              ? "Formatting on Linux requires root — run GODsend with sudo, or uncheck format and use an existing FAT32 stick."
              : platform === "win32"
                ? "Not running as Administrator — USB formatting is disabled. Install packages only if the drive is already FAT32, or restart GODsend as Administrator."
                : null}
          </p>
        )}
      </section>

      {/* Options */}
      <section className="border border-border rounded-lg p-3 flex flex-col gap-3 shrink-0">
        <span className="text-[13px] font-medium text-foreground flex items-center gap-2">
          <HardDrive className="h-4 w-4 text-blue-400" />
          Setup options
        </span>
        <div className="grid gap-2.5 pl-1">
          <OptionRow
            checked={formatDrive}
            onChange={setFormatDrive}
            disabled={!isAdmin || busy}
            label="Format USB to FAT32 before install"
            hint="Erases the entire drive. Works on any size USB. Windows: Administrator + bundled fat32format. Linux: root + mkfs.vfat. macOS: no admin needed for typical USB sticks."
          />
          <OptionRow
            checked={overwriteExisting}
            onChange={setOverwriteExisting}
            disabled={busy}
            label="Overwrite existing files"
            hint="Re-download cached packages and replace files already on the USB."
          />
        </div>
      </section>

      <section className="border border-border rounded-lg p-3 flex flex-col gap-3 shrink-0">
        <span className="text-[13px] font-medium text-foreground">Optional packages</span>
        <p className="text-[11px] text-muted-foreground -mt-1">
          BadAvatar payload and XeXMenu are always included.
        </p>
        <div className="grid gap-2.5 pl-1">
          <OptionRow checked={installProto} onChange={setInstallProto} disabled={busy} label="Proto" />
          <OptionRow checked={installFreestyle} onChange={setInstallFreestyle} disabled={busy} label="FreestyleDash" />
          <OptionRow checked={installAurora} onChange={setInstallAurora} disabled={busy} label="Aurora (XeUnshackle build)" />
        </div>
      </section>

      {/* Progress */}
      {(busy || status || done || error) && (
        <section className="border border-border rounded-lg p-3 flex flex-col gap-2 shrink-0">
          <div className="h-2 bg-muted rounded-full overflow-hidden">
            <div
              className="h-full bg-primary transition-all duration-300"
              style={{ width: `${Math.min(100, Math.max(0, percent))}%` }}
            />
          </div>
          <div className="flex items-center gap-2 text-[12px]">
            {busy && <Loader2 className="h-3.5 w-3.5 animate-spin text-primary shrink-0" />}
            {done && !busy && <Check className="h-3.5 w-3.5 text-green-400 shrink-0" />}
            {error && !busy && <AlertTriangle className="h-3.5 w-3.5 text-red-400 shrink-0" />}
            <span className={error && !busy ? "text-red-400" : "text-muted-foreground"}>
              {error && !busy ? error : status}
            </span>
          </div>
        </section>
      )}

      <footer className="shrink-0">
        <Button variant="primary" disabled={!canCreate} onClick={handleCreate}>
          {busy
            ? <><Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />Creating USB…</>
            : "Create BadAvatar USB"}
        </Button>
      </footer>
    </div>
  );
}
