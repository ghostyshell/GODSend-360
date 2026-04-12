import { useState, useEffect, useRef } from "react";
import { ArrowLeft, ChevronRight } from "lucide-react";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Checkbox } from "./ui/checkbox";
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from "./ui/collapsible";
import { ScrollArea } from "./ui/scroll-area";
import { cn } from "../lib/utils";

// ── Shared layout helpers ──────────────────────────────────────────────────────

function Section({ title, children }) {
  return (
    <div className="py-4 border-b border-[#1e242e] last:border-0">
      {title && (
        <span className="block text-[13px] font-semibold text-[#cad3dc] mb-2.5">
          {title}
        </span>
      )}
      {children}
    </div>
  );
}

function Hint({ children }) {
  return (
    <p className="mt-2 text-[11px] text-muted-foreground leading-[1.4]">{children}</p>
  );
}

function Status({ children, className }) {
  return (
    <p className={cn("text-[12px] text-[#a8b4c0]", className)} aria-live="polite">
      {children || null}
    </p>
  );
}

// ── Main component ─────────────────────────────────────────────────────────────

const COMMON_SOURCES = ["Hdd1", "Usb0", "Usb1", "Usb2"];

export default function SettingsPage({ onBack, onAppendLine, onLibrarySourcesChanged }) {
  // Form state
  const [startup, setStartup]                   = useState(false);
  const [serverPort, setServerPort]             = useState("8080");
  const [xboxIp, setXboxIp]                     = useState("");
  const [ftpUser, setFtpUser]                   = useState("");
  const [ftpPassword, setFtpPassword]           = useState("");
  const [ftpScriptsPath, setFtpScriptsPath]     = useState("");
  const [transferPath, setTransferPath]         = useState("");
  const [iaEmail, setIaEmail]                   = useState("");
  const [iaPassword, setIaPassword]             = useState("");
  const [romPath, setRomPath]                   = useState("");
  const [ftpScanSubnet, setFtpScanSubnet]       = useState("");

  // Aria2 port settings
  const [aria2ListenPort, setAria2ListenPort]   = useState("");
  const [aria2DhtPort, setAria2DhtPort]         = useState("");
  const [aria2Status, setAria2Status]           = useState("");

  // Default Xbox drive
  const [defaultDrive, setDefaultDrive]         = useState("");
  const [driveList, setDriveList]               = useState([]);
  const [driveStatus, setDriveStatus]           = useState("");
  const [driveLoading, setDriveLoading]         = useState(false);

  // Aurora library sources
  const [auroraLibrarySources, setAuroraLibrarySources] = useState(["Hdd1"]);
  const [librarySourcesStatus, setLibrarySourcesStatus] = useState("");

  // Local app data
  const [dataStatus, setDataStatus]             = useState(null);
  const [dataCheckLoading, setDataCheckLoading] = useState(false);
  const [dataClearLoading, setDataClearLoading] = useState(false);
  const [dataStatusMsg, setDataStatusMsg]       = useState("");

  // Status messages
  const [iaSessionStatus, setIaSessionStatus]           = useState("Not signed in.");
  const [cacheStatus, setCacheStatus]                   = useState("");
  const [xboxConnectionStatus, setXboxConnectionStatus] = useState("");
  const [ftpScriptsStatus, setFtpScriptsStatus]         = useState("");
  const [ftpDebugStatus, setFtpDebugStatus]             = useState("");
  const [ftpDebugLog, setFtpDebugLog]                   = useState("");

  // Loading flags
  const [iaLoginLoading, setIaLoginLoading]     = useState(false);
  const [xboxSaveLoading, setXboxSaveLoading]   = useState(false);
  const [ftpUploadLoading, setFtpUploadLoading] = useState(false);
  const [ftpTestLoading, setFtpTestLoading]     = useState(false);
  const [ftpScanLoading, setFtpScanLoading]     = useState(false);
  const [cacheLoading, setCacheLoading]         = useState(false);

  // Collapsible state
  const [ftpDebugOpen, setFtpDebugOpen] = useState(false);

  const ftpDebugLogRef = useRef(null);

  // ── Load saved values on mount ─────────────────────────────────────────────

  useEffect(() => {
    async function load() {
      setStartup(await window.godsendApi.getStartupEnabled());
      setTransferPath((await window.godsendApi.getEffectiveTransferFolder()) || "");
      setServerPort(String(await window.godsendApi.getServerPort()));

      const auth = await window.godsendApi.getArchiveAuth();
      setIaEmail(auth.iaEmail || "");
      applyIAStatus(auth);

      setRomPath(await window.godsendApi.getROMPath());

      const conn = await window.godsendApi.getXboxConnection();
      setXboxIp(conn.xboxIp || "");
      setFtpUser(conn.ftpUser || "");
      setFtpPassword(conn.ftpPassword || "");
      setFtpScriptsPath(conn.ftpScriptsPath || "");
      if (conn.xboxIp) {
        const parts = conn.xboxIp.split(".");
        if (parts.length === 4) setFtpScanSubnet(parts.slice(0, 3).join("."));
      }

      setAria2ListenPort(await window.godsendApi.getAria2ListenPort());
      setAria2DhtPort(await window.godsendApi.getAria2DhtPort());
      setDefaultDrive(await window.godsendApi.getDefaultXboxDrive());

      const sources = await window.godsendApi.getAuroraLibrarySources().catch(() => ["Hdd1"]);
      if (Array.isArray(sources) && sources.length > 0) setAuroraLibrarySources(sources);
    }
    load();

    const cleanupProgress  = window.godsendApi.onFtpProgress((msg) => setFtpScriptsStatus(msg));
    const cleanupDebugLog  = window.godsendApi.onFtpDebugLog((line) =>
      setFtpDebugLog((prev) => prev + line + "\n")
    );

    return () => {
      cleanupProgress();
      cleanupDebugLog();
    };
  }, []);

  // Auto-scroll FTP debug log
  useEffect(() => {
    if (ftpDebugLogRef.current) {
      ftpDebugLogRef.current.scrollTop = ftpDebugLogRef.current.scrollHeight;
    }
  }, [ftpDebugLog]);

  // ── Helpers ────────────────────────────────────────────────────────────────

  function applyIAStatus(auth) {
    setIaSessionStatus(
      auth?.hasSession && auth.iaScreenname
        ? `Signed in as ${auth.iaScreenname}.`
        : auth?.hasSession
        ? `Signed in (${auth.iaEmail || "session active"}).`
        : "Not signed in."
    );
  }

  // ── Handlers ───────────────────────────────────────────────────────────────

  async function handleStartupChange(checked) {
    const result = await window.godsendApi.setStartupEnabled(checked);
    setStartup(result);
  }

  async function handlePortSave() {
    const saved = await window.godsendApi.setServerPort(serverPort);
    setServerPort(String(saved));
    onAppendLine(`[INFO] Backend port set to ${saved}; backend restarted if running.`);
  }

  async function handlePortReset() {
    const saved = await window.godsendApi.setServerPort(8080);
    setServerPort(String(saved));
    onAppendLine("[INFO] Backend port reset to 8080; backend restarted if running.");
  }

  async function handleTransferBrowse() {
    const picked = await window.godsendApi.chooseTransferFolder();
    if (!picked) return;
    await window.godsendApi.setTransferFolder(picked);
    setTransferPath((await window.godsendApi.getEffectiveTransferFolder()) || "");
  }

  async function handleTransferReset() {
    await window.godsendApi.setTransferFolder("");
    setTransferPath((await window.godsendApi.getEffectiveTransferFolder()) || "");
  }

  async function handleIALogin() {
    setIaLoginLoading(true);
    try {
      const r = await window.godsendApi.loginInternetArchive({
        email: iaEmail,
        password: iaPassword,
      });
      setIaPassword("");
      if (r.ok) {
        onAppendLine("[INFO] Internet Archive: signed in; backend restarted.");
        applyIAStatus(await window.godsendApi.getArchiveAuth());
      } else {
        onAppendLine(`[ERROR] Internet Archive login: ${r.error || "Unknown error"}`);
      }
    } finally {
      setIaLoginLoading(false);
    }
  }

  async function handleIALogout() {
    await window.godsendApi.logoutInternetArchive();
    applyIAStatus(await window.godsendApi.getArchiveAuth());
    onAppendLine("[INFO] Internet Archive: signed out; backend restarted.");
  }

  async function handleRomPathSave() {
    await window.godsendApi.setROMPath(romPath);
  }

  async function handleRomPathReset() {
    await window.godsendApi.setROMPath("");
    setRomPath(await window.godsendApi.getROMPath());
  }

  async function handleCacheRefresh() {
    setCacheLoading(true);
    setCacheStatus("Requesting refresh...");
    const r = await window.godsendApi.refreshCache("all");
    setCacheStatus(
      r.ok
        ? "Refresh started — running in background. Check server log for progress."
        : `Failed: ${r.error || "unknown error"}`
    );
    setCacheLoading(false);
  }

  async function handleXboxSave() {
    setXboxSaveLoading(true);
    setXboxConnectionStatus("Saving\u2026");
    try {
      await window.godsendApi.setXboxConnection({
        xboxIp:         xboxIp.trim(),
        ftpUser:        ftpUser.trim(),
        ftpPassword,
        ftpScriptsPath: ftpScriptsPath.trim(),
      });
      setXboxConnectionStatus(
        "Saved. Backend restarted so post-download FTP installs use these credentials."
      );
      onAppendLine("[INFO] Xbox connection saved; backend restarted if running.");
    } catch (err) {
      setXboxConnectionStatus(`Failed to save: ${err.message || "unknown error"}`);
    } finally {
      setXboxSaveLoading(false);
    }
  }

  async function handleFtpUpload() {
    if (!xboxIp.trim()) {
      setFtpScriptsStatus("Enter the Xbox IP address first.");
      return;
    }
    setFtpUploadLoading(true);
    setFtpScriptsStatus("Starting\u2026");
    try {
      await window.godsendApi.setXboxConnection({
        xboxIp:         xboxIp.trim(),
        ftpUser:        ftpUser.trim(),
        ftpPassword,
        ftpScriptsPath: ftpScriptsPath.trim(),
      });
      const r = await window.godsendApi.ftpAuroraScripts({
        xboxIp:         xboxIp.trim(),
        ftpUser:        ftpUser.trim(),
        ftpPassword,
        ftpScriptsPath: ftpScriptsPath.trim(),
      });
      setFtpScriptsStatus(
        r.ok
          ? `Aurora scripts uploaded successfully to ${r.remotePath || "(path unknown)"}.`
          : `Failed: ${r.error || "unknown error"}`
      );
    } catch (err) {
      setFtpScriptsStatus(`Failed: ${err.message || "unknown error"}`);
    } finally {
      setFtpUploadLoading(false);
    }
  }

  async function handleFtpTest() {
    setFtpTestLoading(true);
    setFtpDebugStatus("Testing connection...");
    setFtpDebugLog("");
    try {
      const r = await window.godsendApi.ftpTestConnection({
        xboxIp:      xboxIp.trim(),
        ftpUser:     ftpUser.trim(),
        ftpPassword,
      });
      setFtpDebugStatus(
        r.ok ? "Connection test passed." : `Test failed: ${r.error}`
      );
    } catch (err) {
      setFtpDebugStatus(`Test failed: ${err.message || "unknown error"}`);
    } finally {
      setFtpTestLoading(false);
    }
  }

  async function handleFtpScan() {
    if (!ftpScanSubnet.trim()) {
      setFtpDebugStatus("Enter a subnet first (e.g. 192.168.1).");
      return;
    }
    setFtpScanLoading(true);
    setFtpDebugStatus("Scanning...");
    setFtpDebugLog("");
    try {
      const r = await window.godsendApi.ftpScanPorts(ftpScanSubnet.trim());
      if (r.ok) {
        setFtpDebugStatus(
          r.hosts.length
            ? `Found ${r.hosts.length} FTP host(s): ${r.hosts.join(", ")}`
            : "No FTP servers found on this subnet."
        );
      } else {
        setFtpDebugStatus(`Scan failed: ${r.error}`);
      }
    } catch (err) {
      setFtpDebugStatus(`Scan failed: ${err.message || "unknown error"}`);
    } finally {
      setFtpScanLoading(false);
    }
  }

  async function handleFtpScriptsPathReset() {
    setFtpScriptsPath(await window.godsendApi.getFtpScriptsPathDefault());
  }

  async function handleAria2Save() {
    await window.godsendApi.setAria2ListenPort(aria2ListenPort);
    await window.godsendApi.setAria2DhtPort(aria2DhtPort);
    setAria2Status("Saved. Backend restarted if running.");
  }

  async function handleFetchDrives() {
    setDriveLoading(true);
    setDriveStatus("Connecting to Xbox via FTP...");
    try {
      const r = await window.godsendApi.listXboxDrives();
      if (r.ok) {
        setDriveList(r.drives);
        setDriveStatus(`Found ${r.drives.length} drive(s).`);
      } else {
        setDriveStatus(`Failed: ${r.error || "unknown error"}`);
      }
    } catch (err) {
      setDriveStatus(`Failed: ${err.message || "unknown error"}`);
    } finally {
      setDriveLoading(false);
    }
  }

  async function handleDriveSave() {
    const saved = await window.godsendApi.setDefaultXboxDrive(defaultDrive);
    setDefaultDrive(saved);
    setDriveStatus(saved ? `Default drive set to ${saved}. Backend restarted.` : "Default drive cleared.");
  }

  async function handleLibrarySourceToggle(drive, checked) {
    const next = checked
      ? [...auroraLibrarySources, drive]
      : auroraLibrarySources.filter((d) => d !== drive);
    setAuroraLibrarySources(next);
    const saved = await window.godsendApi.setAuroraLibrarySources(next);
    setLibrarySourcesStatus(
      saved.length > 0
        ? `Saved: ${saved.join(", ")}`
        : "No drives selected — all games will appear active."
    );
    if (onLibrarySourcesChanged) onLibrarySourcesChanged(next);
  }

  async function handleDriveClear() {
    const saved = await window.godsendApi.setDefaultXboxDrive("");
    setDefaultDrive(saved);
    setDriveStatus("Default drive cleared — Aurora will prompt for drive on each download.");
  }

  async function handleDataCheck() {
    setDataCheckLoading(true);
    setDataStatusMsg("");
    try {
      const r = await window.godsendApi.getDataStatus();
      if (r.ok) {
        setDataStatus(r);
        setDataStatusMsg(
          `${r.active_jobs} active job(s), ${r.pending_ftp_jobs} pending FTP job(s), ${r.local_data_mb} MB local data`
        );
      } else {
        setDataStatusMsg(`Failed: ${r.error || "unknown error"}`);
      }
    } finally {
      setDataCheckLoading(false);
    }
  }

  async function handleDataClear() {
    const hasJobs = dataStatus && (dataStatus.active_jobs > 0 || dataStatus.pending_ftp_jobs > 0);
    const warn = hasJobs
      ? `WARNING: There are ${dataStatus.active_jobs} active job(s) and ${dataStatus.pending_ftp_jobs} pending FTP job(s).\n\nClearing will cancel all of them.\n\nContinue?`
      : "Clear all local data (Ready/ and Temp/ directories) and cancel pending FTP jobs?\n\nThis cannot be undone.";

    if (!window.confirm(warn)) return;

    setDataClearLoading(true);
    setDataStatusMsg("Clearing...");
    try {
      const r = await window.godsendApi.clearLocalData();
      setDataStatus(null);
      setDataStatusMsg(r.ok ? "Local data cleared." : `Failed: ${r.error || "unknown error"}`);
    } finally {
      setDataClearLoading(false);
    }
  }

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <div className="flex flex-col h-screen p-3 gap-2.5">

      {/* Header */}
      <header className="flex items-center gap-2.5 shrink-0 pb-3 border-b border-border">
        <Button size="icon" title="Back to terminal" onClick={onBack}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <span className="text-[15px] font-semibold text-foreground">Settings</span>
      </header>

      {/* Scrollable settings body */}
      <ScrollArea className="flex-1 min-h-0">
        <div className="flex flex-col pr-3">

          {/* ── Launch at login ── */}
          <Section>
            <label className="flex items-center gap-2.5 text-[13px] cursor-pointer select-none">
              <Checkbox checked={startup} onCheckedChange={handleStartupChange} />
              Launch GODsend at login
            </label>
          </Section>

          {/* ── Backend server port ── */}
          <Section title="Backend server port">
            <div className="flex flex-wrap gap-2 items-center">
              <Input
                type="number"
                min={1}
                max={65535}
                step={1}
                className="w-[110px]"
                placeholder="8080"
                value={serverPort}
                onChange={(e) => setServerPort(e.target.value)}
              />
              <Button onClick={handlePortSave}>Save</Button>
              <Button onClick={handlePortReset}>Use 8080</Button>
            </div>
            <Hint>
              Used by the local backend and patched into Aurora scripts during FTP
              upload. Changing this restarts the backend.
            </Hint>
          </Section>

          {/* ── Xbox connection ── */}
          <Section title="Xbox connection">
            <div className="space-y-3">
              <div>
                <Label htmlFor="xboxIp">Xbox IP address</Label>
                <Input
                  id="xboxIp"
                  type="text"
                  className="mt-1 max-w-[480px]"
                  spellCheck={false}
                  placeholder="e.g. 192.168.1.100"
                  value={xboxIp}
                  onChange={(e) => setXboxIp(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="ftpUser">FTP username</Label>
                <Input
                  id="ftpUser"
                  type="text"
                  className="mt-1 max-w-[480px]"
                  spellCheck={false}
                  placeholder="xboxftp"
                  autoComplete="username"
                  value={ftpUser}
                  onChange={(e) => setFtpUser(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="ftpPassword">FTP password</Label>
                <Input
                  id="ftpPassword"
                  type="password"
                  className="mt-1 max-w-[480px]"
                  spellCheck={false}
                  placeholder="xboxftp"
                  autoComplete="current-password"
                  value={ftpPassword}
                  onChange={(e) => setFtpPassword(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="ftpScriptsPath">Scripts destination path (on Xbox)</Label>
                <div className="flex flex-wrap gap-2 items-center mt-1">
                  <Input
                    id="ftpScriptsPath"
                    type="text"
                    className="flex-1 min-w-[180px] max-w-[480px]"
                    spellCheck={false}
                    placeholder="/Hdd1/Aurora/User/Scripts/Utility/GODSend"
                    value={ftpScriptsPath}
                    onChange={(e) => setFtpScriptsPath(e.target.value)}
                  />
                  <Button onClick={handleFtpScriptsPathReset}>Use default</Button>
                </div>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button disabled={xboxSaveLoading} onClick={handleXboxSave}>
                  Save connection
                </Button>
                <Button disabled={ftpUploadLoading} onClick={handleFtpUpload}>
                  FTP Aurora Scripts to Xbox
                </Button>
              </div>
              {xboxConnectionStatus && (
                <Status className="mb-0">{xboxConnectionStatus}</Status>
              )}
              {ftpScriptsStatus && (
                <Status className="mb-0">{ftpScriptsStatus}</Status>
              )}
            </div>

            <Hint>
              Click <strong>Save connection</strong> to persist the Xbox IP, FTP
              credentials, and scripts path; the backend will restart so post-download
              FTP installs use the same credentials. Enable FTP in Aurora (Settings
              &rarr; Network &rarr; Enable FTP) before using{" "}
              <strong>FTP Aurora Scripts to Xbox</strong>. Your PC IP and selected
              backend port are patched directly into <code>state.lua</code>{" "}
              automatically. The path must match the folder Aurora actually loads (copy
              it from your FTP client). On USB that is often{" "}
              <code>/Usb0/Apps/Aurora/User/Scripts/Utility/GODSend</code> &mdash; note{" "}
              <code>Apps</code> and <code>Utility</code> (not <code>Utilities</code>).
              On HDD it is often{" "}
              <code>/Hdd1/Aurora/User/Scripts/Utility/GODSend</code>.
            </Hint>

            {/* FTP Debugging (collapsible) */}
            <Collapsible
              open={ftpDebugOpen}
              onOpenChange={setFtpDebugOpen}
              className="mt-3 border border-[#1e242e] rounded-lg overflow-hidden"
            >
              <CollapsibleTrigger className="flex items-center gap-1.5 w-full px-3 py-2 text-[12px] font-semibold text-muted-foreground bg-muted hover:text-foreground hover:bg-accent transition-colors text-left select-none cursor-pointer">
                <ChevronRight
                  className={cn(
                    "h-3 w-3 transition-transform duration-150",
                    ftpDebugOpen && "rotate-90"
                  )}
                />
                FTP Debugging Tools
              </CollapsibleTrigger>
              <CollapsibleContent className="px-3 py-2.5 space-y-2">
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" disabled={ftpTestLoading} onClick={handleFtpTest}>
                    Test Connection
                  </Button>
                  <Button size="sm" disabled={ftpScanLoading} onClick={handleFtpScan}>
                    Scan Network Ports
                  </Button>
                  <Button
                    size="sm"
                    onClick={() => {
                      setFtpDebugLog("");
                      setFtpDebugStatus("");
                    }}
                  >
                    Clear Log
                  </Button>
                </div>
                {ftpDebugStatus && (
                  <Status className="mb-0">{ftpDebugStatus}</Status>
                )}
                <div>
                  <Label htmlFor="ftpScanSubnet">
                    Subnet to scan (e.g. 192.168.1)
                  </Label>
                  <div className="flex flex-wrap gap-2 items-center mt-1">
                    <Input
                      id="ftpScanSubnet"
                      type="text"
                      className="max-w-[200px]"
                      spellCheck={false}
                      placeholder="192.168.1"
                      value={ftpScanSubnet}
                      onChange={(e) => setFtpScanSubnet(e.target.value)}
                    />
                    <span className="text-[11px] text-muted-foreground">
                      Port 21 on .1 &ndash; .254
                    </span>
                  </div>
                </div>
                <pre
                  ref={ftpDebugLogRef}
                  className="p-2 bg-surface border border-border rounded text-[11px] leading-[1.4] text-[#c0c8d4] min-h-[80px] max-h-[220px] overflow-auto whitespace-pre-wrap break-words select-text cursor-text font-mono"
                >
                  {ftpDebugLog}
                </pre>
              </CollapsibleContent>
            </Collapsible>
          </Section>

          {/* ── Local Transfer folder ── */}
          <Section title="Local Transfer folder (ISOs)">
            <div className="flex flex-wrap gap-2 items-center">
              <Input
                type="text"
                readOnly
                className="flex-1 min-w-[180px]"
                placeholder="Default: data folder / Transfer"
                value={transferPath}
              />
              <Button onClick={handleTransferBrowse}>Browse&hellip;</Button>
              <Button onClick={handleTransferReset}>Use default</Button>
            </div>
            <Hint>
              Changing this restarts the backend. The Xbox script uses this folder for
              &ldquo;Local Library&rdquo;.
            </Hint>
          </Section>

          {/* ── Game cache ── */}
          <Section title="Game cache">
            {cacheStatus && (
              <Status className="mb-2">{cacheStatus}</Status>
            )}
            <Button disabled={cacheLoading} onClick={handleCacheRefresh}>
              Refresh all caches
            </Button>
            <Hint>
              Caches are loaded from disk on startup and never refreshed automatically.
              Click to re-fetch all Internet Archive game lists and any ROM system
              caches you have previously browsed.
            </Hint>
          </Section>

          {/* ── Internet Archive account ── */}
          <Section title="Internet Archive account">
            <div className="space-y-3">
              {iaSessionStatus && (
                <Status className="mb-0">{iaSessionStatus}</Status>
              )}
              <div>
                <Label htmlFor="iaEmail">Email</Label>
                <Input
                  id="iaEmail"
                  type="email"
                  className="mt-1 max-w-[480px]"
                  spellCheck={false}
                  autoComplete="username"
                  placeholder="Your archive.org email"
                  value={iaEmail}
                  onChange={(e) => setIaEmail(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="iaPassword">Password</Label>
                <Input
                  id="iaPassword"
                  type="password"
                  className="mt-1 max-w-[480px]"
                  spellCheck={false}
                  autoComplete="current-password"
                  placeholder="Not stored — only used to sign in"
                  value={iaPassword}
                  onChange={(e) => setIaPassword(e.target.value)}
                />
              </div>
              <div className="flex flex-wrap gap-2">
                <Button disabled={iaLoginLoading} onClick={handleIALogin}>
                  Log in &amp; restart backend
                </Button>
                <Button onClick={handleIALogout}>Sign out</Button>
              </div>
            </div>
            <Hint>
              Uses archive.org&rsquo;s official login API. Session cookies are saved
              locally; your password is never stored.
            </Hint>
          </Section>

          {/* ── ROM install path ── */}
          <Section title="ROM install path (on Xbox)">
            <div className="flex flex-wrap gap-2 items-center">
              <Input
                type="text"
                className="flex-1 min-w-[180px] max-w-[480px]"
                placeholder={String.raw`Default: Emulators\RetroArch\roms`}
                value={romPath}
                onChange={(e) => setRomPath(e.target.value)}
              />
              <Button onClick={handleRomPathSave}>Save</Button>
              <Button onClick={handleRomPathReset}>Use default</Button>
            </div>
            <Hint>
              Drive-relative path for ROM installs. Each system gets a subfolder
              (e.g.&nbsp;\NES\, \SNES\). Changing this restarts the backend.
            </Hint>
          </Section>

          {/* ── Aria2 / Minerva download ports ── */}
          <Section title="Aria2 / Minerva download ports">
            <div className="space-y-3">
              <div className="flex flex-wrap gap-3 items-end">
                <div>
                  <Label htmlFor="aria2Listen">Listen port</Label>
                  <Input
                    id="aria2Listen"
                    type="number"
                    min={1}
                    max={65535}
                    step={1}
                    className="mt-1 w-[110px]"
                    placeholder="(auto)"
                    value={aria2ListenPort}
                    onChange={(e) => setAria2ListenPort(e.target.value)}
                  />
                </div>
                <div>
                  <Label htmlFor="aria2Dht">DHT port</Label>
                  <Input
                    id="aria2Dht"
                    type="number"
                    min={1}
                    max={65535}
                    step={1}
                    className="mt-1 w-[110px]"
                    placeholder="(auto)"
                    value={aria2DhtPort}
                    onChange={(e) => setAria2DhtPort(e.target.value)}
                  />
                </div>
                <Button onClick={handleAria2Save}>Save</Button>
              </div>
              {aria2Status && <Status>{aria2Status}</Status>}
            </div>
            <Hint>
              Ports aria2 uses for BitTorrent traffic when downloading from Minerva
              Archive. Leave blank for automatic selection. Set these if you need to
              open specific firewall rules. Changing restarts the backend.
            </Hint>
          </Section>

          {/* ── Xbox Library sources ── */}
          <Section title="Xbox Library sources">
            <p className="text-[12px] text-muted-foreground mb-2.5">
              Select which drives are scanned for installed games. Games found on
              unselected drives appear greyed out in the library.
            </p>
            <div className="flex flex-wrap gap-x-5 gap-y-2">
              {COMMON_SOURCES.map((drive) => (
                <label
                  key={drive}
                  className="flex items-center gap-2 text-[13px] cursor-pointer select-none"
                >
                  <Checkbox
                    checked={auroraLibrarySources.includes(drive)}
                    onCheckedChange={(checked) => handleLibrarySourceToggle(drive, Boolean(checked))}
                  />
                  {drive}
                </label>
              ))}
            </div>
            {librarySourcesStatus && (
              <Status className="mt-2 mb-0">{librarySourcesStatus}</Status>
            )}
            <Hint>
              Corresponds to the Xbox drive names Aurora uses (Hdd1 = internal HDD,
              Usb0/Usb1 = USB storage). Covers show in full colour when the game is
              on a selected drive; greyed out otherwise.
            </Hint>
          </Section>

          {/* ── Default Xbox drive ── */}
          <Section title="Default Xbox drive">
            <div className="space-y-3">
              <div className="flex flex-wrap gap-2 items-center">
                <Button disabled={driveLoading} onClick={handleFetchDrives}>
                  {driveLoading ? "Fetching\u2026" : "Fetch drives from Xbox"}
                </Button>
              </div>
              {driveList.length > 0 && (
                <div className="flex flex-wrap gap-2 items-center">
                  <select
                    className="h-9 rounded-md border border-input bg-background px-3 py-1 text-[13px] text-foreground focus:outline-none focus:ring-1 focus:ring-ring"
                    value={defaultDrive}
                    onChange={(e) => setDefaultDrive(e.target.value)}
                  >
                    <option value="">(none — prompt each time)</option>
                    {driveList.map((d) => (
                      <option key={d} value={d}>{d}</option>
                    ))}
                  </select>
                </div>
              )}
              {driveList.length === 0 && defaultDrive && (
                <div className="flex flex-wrap gap-2 items-center">
                  <Input
                    type="text"
                    className="w-[140px]"
                    placeholder="e.g. Hdd1:"
                    value={defaultDrive}
                    onChange={(e) => setDefaultDrive(e.target.value)}
                  />
                </div>
              )}
              <div className="flex flex-wrap gap-2">
                <Button onClick={handleDriveSave}>Save</Button>
                <Button onClick={handleDriveClear}>Clear</Button>
              </div>
              {driveStatus && <Status>{driveStatus}</Status>}
            </div>
            <Hint>
              When set, the Aurora script skips the drive picker on every download
              and uses this drive automatically. Click{" "}
              <strong>Fetch drives from Xbox</strong> to list available storage
              devices from the console via FTP (Xbox IP must be configured). Click{" "}
              <strong>Clear</strong> to reset — Aurora will prompt for a drive each
              time.
            </Hint>
          </Section>

          {/* ── Local app data ── */}
          <Section title="Local app data">
            <div className="space-y-3">
              <div className="flex flex-wrap gap-2 items-center">
                <Button disabled={dataCheckLoading} onClick={handleDataCheck}>
                  {dataCheckLoading ? "Checking\u2026" : "Check status"}
                </Button>
                <Button disabled={dataClearLoading} onClick={handleDataClear}>
                  {dataClearLoading ? "Clearing\u2026" : "Clear local data"}
                </Button>
              </div>
              {dataStatusMsg && (
                <Status className={dataStatus && (dataStatus.active_jobs > 0 || dataStatus.pending_ftp_jobs > 0) ? "text-yellow-400" : ""}>
                  {dataStatusMsg}
                </Status>
              )}
            </div>
            <Hint>
              Shows active jobs, pending FTP retries, and total local data size
              (Ready/ and Temp/ directories). <strong>Clear local data</strong>{" "}
              cancels all pending FTP jobs, removes downloaded/converted game files,
              and resets the job queue. A confirmation prompt will warn if active or
              pending jobs exist.
            </Hint>
          </Section>

        </div>
      </ScrollArea>
    </div>
  );
}
