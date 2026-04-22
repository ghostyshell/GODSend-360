import { useState, useEffect, useCallback } from "react";
import HomePage from "./components/HomePage";
import SettingsPage from "./components/SettingsPage";
import LibraryPage from "./components/LibraryPage";
import QueuePage from "./components/QueuePage";
import BrowsePage from "./components/BrowsePage";
import ISO2GODPage from "./components/ISO2GODPage";
import ISO2XEXPage from "./components/ISO2XEXPage";
import FTPManagerPage from "./components/FTPManagerPage";
import MainNav from "./components/MainNav";

type PageId = "home" | "library" | "settings" | "queue" | "browse" | "iso2god" | "iso2xex" | "ftpmanager";

const PAGE_TITLES: Record<string, string> = {
  settings:   "Settings",
  queue:      "Job Queue",
  browse:     "Browse & Download",
  iso2god:    "ISO to GOD",
  iso2xex:    "ISO to XEX",
  ftpmanager: "FTP Manager",
};

export default function App() {
  const [page, setPage] = useState<PageId>("home");

  // ── Console output ────────────────────────────────────────────────────────
  const [outputLines, setOutputLines] = useState<string[]>([]);
  const [logInfo, setLogInfo]         = useState<any>(null);

  // ── FTP connectivity status ───────────────────────────────────────────────
  const [ftpStatus, setFtpStatus] = useState("checking");

  // ── Queue state ───────────────────────────────────────────────────────────
  const [queueJobs, setQueueJobs] = useState<any[]>([]);

  // ── Xbox library state ────────────────────────────────────────────────────
  const [libraryStatus, setLibraryStatus]           = useState("idle");
  const [libraryGames, setLibraryGames]             = useState<any[]>([]);
  const [libraryConnectedTo, setLibraryConnectedTo] = useState("");
  const [covers, setCovers]                         = useState<Record<string, string>>({});
  const [titleVisuals, setTitleVisuals]             = useState<Record<string, any>>({});
  const [libraryLoading, setLibraryLoading]         = useState(false);
  const [libraryRefreshing, setLibraryRefreshing]  = useState(false);

  async function loadAuroraLibrary(opts: { force?: boolean; navigateOnLibraryError?: boolean } = {}) {
    const force = opts.force === true;
    const navigateOnLibraryError = opts.navigateOnLibraryError !== false;

    const result = await window.godsendApi.listAuroraLibrary(force ? { force: true } : {});

    if (!result.ok) {
      if (navigateOnLibraryError) {
        setFtpStatus("disconnected");
        setLibraryStatus("error");
        setPage("home");
      }
      return result;
    }

    setLibraryGames(result.games);
    setLibraryConnectedTo(result.connectedTo);
    // Only wipe cover caches on an explicit force refresh so that
    // incremental push-event updates don't flash empty.
    if (force) {
      setCovers({});
      setTitleVisuals({});
    }
    setLibraryStatus(result.games.length === 0 ? "empty" : "ready");

    if (result.games.length > 0) {
      const fromDisk =
        result.fromCache === true && result.libraryUnchanged === true;
      window.godsendApi
        .fetchAuroraCovers(
          result.games.map((g: any) => ({
            titleId:     g.titleId,
            contentId:   g.contentId,
            gameDataDir: g.gameDataDir,
          })),
          fromDisk ? { fromDiskOnly: true } : { force },
        )
        .catch(() => {});
    }

    return result;
  }

  // ── Startup ───────────────────────────────────────────────────────────────
  useEffect(() => {
    window.godsendApi.getOutputBuffer().then((buf: string[]) => setOutputLines(buf));
    window.godsendApi.getLogsInfo().then((info: any) => setLogInfo(info));

    const cleanupOutput = window.godsendApi.onOutput((line: string) =>
      setOutputLines((prev) => [...prev, line])
    );
    const cleanupCover = window.godsendApi.onXboxCover(({ titleId, gameDataDir, src, dataUrl }: any) => {
      const key = gameDataDir || titleId;
      setCovers((prev) => ({ ...prev, [key]: src || dataUrl }));
    });
    const cleanupVisuals = window.godsendApi.onXboxTitleVisuals(({ titleId, gameDataDir, visuals }: any) => {
      const key = gameDataDir || titleId;
      setTitleVisuals((prev) => ({ ...prev, [key]: visuals }));
    });

    initApp();

    const queueInterval = setInterval(async () => {
      const [pipelineRes, ftpRes] = await Promise.all([
        window.godsendApi.getQueue().catch(() => ({ ok: false, jobs: [] })),
        window.godsendApi.toolsFtpUploadStatus().catch(() => ({ ok: false, jobs: [] })),
      ]);
      const pJobs = pipelineRes.ok && Array.isArray(pipelineRes.jobs) ? pipelineRes.jobs : [];
      const fJobs = ftpRes.ok && Array.isArray(ftpRes.jobs) ? ftpRes.jobs : [];
      setQueueJobs([...pJobs, ...fJobs]);
    }, 5000);

    return () => {
      cleanupOutput();
      cleanupCover();
      cleanupVisuals();
      clearInterval(queueInterval);
    };
  }, []);

  async function initApp() {
    let ping: any = { ok: false };
    for (let attempt = 0; attempt < 10; attempt++) {
      ping = await window.godsendApi.pingXbox().catch(() => ({ ok: false }));
      if (ping.ok) break;
      await new Promise((r) => setTimeout(r, 500));
    }

    if (!ping.ok) {
      setFtpStatus("disconnected");
      return;
    }

    setFtpStatus("connected");
    setLibraryStatus("connecting");

    setPage((current) => (current === "home" ? "library" : current));

    const result = await loadAuroraLibrary({ navigateOnLibraryError: false });
    if (!result || !result.ok) {
      setFtpStatus("disconnected");
      setLibraryStatus("error");
    }
  }

  // ── FTP ping (reconnect button) ───────────────────────────────────────────
  async function pingFtp() {
    setFtpStatus("checking");
    const result = await window.godsendApi.pingXbox();
    setFtpStatus(result.ok ? "connected" : "disconnected");
  }

  const appendLine = useCallback(
    (line: string) => setOutputLines((prev) => [...prev, line]),
    []
  );

  // ── Aurora library: poll Xbox DB fingerprint every 2 minutes ─────────────
  useEffect(() => {
    if (page !== "library") return undefined;

    const pollMs = 120000;
    const id = setInterval(async () => {
      const r = await window.godsendApi.listAuroraLibrary({}).catch(() => ({ ok: false }));
      if (!r.ok || !Array.isArray(r.games)) return;
      if (r.libraryUnchanged === true) return;

      setLibraryGames(r.games);
      setLibraryConnectedTo(r.connectedTo || "");
      setLibraryStatus(r.games.length === 0 ? "empty" : "ready");
      // Don't clear covers/titleVisuals here — let push events update
      // them incrementally to avoid flashing empty covers on the grid.
      if (r.games.length > 0) {
        window.godsendApi
          .fetchAuroraCovers(
            r.games.map((g: any) => ({
              titleId:     g.titleId,
              contentId:   g.contentId,
              gameDataDir: g.gameDataDir,
            })),
            { force: false },
          )
          .catch(() => {});
      }
    }, pollMs);

    return () => clearInterval(id);
  }, [page]);

  // ── Library toggle ────────────────────────────────────────────────────────
  async function handleLibraryToggle() {
    if (page === "library") {
      setPage("home");
      return;
    }

    setLibraryLoading(true);
    setLibraryStatus("connecting");

    try {
      const result = await loadAuroraLibrary({ navigateOnLibraryError: false });

      if (!result.ok) {
        appendLine(`[ERROR] Xbox Library: ${result.error}`);
        setFtpStatus("disconnected");
        setLibraryLoading(false);
        return;
      }

      setPage("library");
    } catch (err: any) {
      appendLine(`[ERROR] Xbox Library: ${err.message || "Unknown error"}`);
    } finally {
      setLibraryLoading(false);
    }
  }

  async function handleLibraryRefresh() {
    setLibraryRefreshing(true);
    try {
      const r = await loadAuroraLibrary({
        force: true,
        navigateOnLibraryError: false,
      });
      if (!r.ok) {
        appendLine(`[ERROR] Xbox Library refresh: ${r.error}`);
        setFtpStatus("disconnected");
      }
    } catch (err: any) {
      appendLine(`[ERROR] Xbox Library refresh: ${err.message || "Unknown error"}`);
    } finally {
      setLibraryRefreshing(false);
    }
  }

  const navigateTo = useCallback((id: PageId) => setPage(id), []);

  // ── Shared nav props ───────────────────────────────────────────────────────
  const navProps = {
    ftpStatus,
    currentPage: page as any,
    libraryAvailable: ftpStatus === "connected",
    libraryLoading,
    queueJobs,
    onReconnect: pingFtp,
    onLibraryToggle: handleLibraryToggle,
    onNavigateHome:       () => navigateTo("home"),
    onNavigateQueue:      () => navigateTo("queue"),
    onNavigateBrowse:     () => navigateTo("browse"),
    onNavigateSettings:   () => navigateTo("settings"),
    onNavigateIso2God:    () => navigateTo("iso2god"),
    onNavigateIso2Xex:    () => navigateTo("iso2xex"),
    onNavigateFtpManager: () => navigateTo("ftpmanager"),
  };

  // ── Routing ───────────────────────────────────────────────────────────────
  if (page === "library") {
    return (
      <LibraryPage
        status={libraryStatus}
        games={libraryGames}
        covers={covers}
        titleVisuals={titleVisuals}
        connectedTo={libraryConnectedTo}
        onToggle={handleLibraryToggle}
        onRefresh={handleLibraryRefresh}
        refreshBusy={libraryRefreshing}
        ftpStatus={ftpStatus}
        libraryLoading={libraryLoading}
        queueJobs={queueJobs}
        onReconnect={pingFtp}
        onNavigateQueue={navProps.onNavigateQueue}
        onNavigateBrowse={navProps.onNavigateBrowse}
        onNavigateSettings={navProps.onNavigateSettings}
        onNavigateIso2God={navProps.onNavigateIso2God}
        onNavigateIso2Xex={navProps.onNavigateIso2Xex}
        onNavigateFtpManager={navProps.onNavigateFtpManager}
      />
    );
  }

  if (page === "home") {
    return (
      <HomePage
        outputLines={outputLines}
        logInfo={logInfo}
        ftpStatus={ftpStatus}
        onNavigateSettings={navProps.onNavigateSettings}
        onNavigateQueue={navProps.onNavigateQueue}
        onNavigateBrowse={navProps.onNavigateBrowse}
        onNavigateIso2God={navProps.onNavigateIso2God}
        onNavigateIso2Xex={navProps.onNavigateIso2Xex}
        onNavigateFtpManager={navProps.onNavigateFtpManager}
        onLibraryToggle={handleLibraryToggle}
        onReconnect={pingFtp}
        libraryLoading={libraryLoading}
        onAppendLine={appendLine}
        queueJobs={queueJobs}
      />
    );
  }

  // ── Tool / utility pages (settings, queue, browse, tools) ─────────────────
  let pageContent: React.ReactNode = null;
  if (page === "settings") {
    pageContent = <SettingsPage onAppendLine={appendLine} />;
  } else if (page === "queue") {
    pageContent = <QueuePage />;
  } else if (page === "browse") {
    pageContent = <BrowsePage />;
  } else if (page === "iso2god") {
    pageContent = <ISO2GODPage />;
  } else if (page === "iso2xex") {
    pageContent = <ISO2XEXPage />;
  } else if (page === "ftpmanager") {
    pageContent = <FTPManagerPage />;
  }

  return (
    <div className="flex flex-col h-screen p-3 gap-2.5">
      <header className="flex items-center shrink-0">
        <span className="text-[15px] font-semibold text-foreground flex-1">
          {PAGE_TITLES[page] || page}
        </span>
        <MainNav {...navProps} />
      </header>
      <div className="flex-1 min-h-0 overflow-auto">
        {pageContent}
      </div>
    </div>
  );
}
