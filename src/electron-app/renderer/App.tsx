import { useState, useEffect, useCallback } from "react";
import { Loader2 } from "lucide-react";
import HomePage from "./components/HomePage";
import SettingsPage from "./components/SettingsPage";
import LibraryPage from "./components/LibraryPage";
import QueuePage from "./components/QueuePage";
import BrowsePage from "./components/BrowsePage";
import ISO2GODPage from "./components/ISO2GODPage";
import ISO2XEXPage from "./components/ISO2XEXPage";
import FTPManagerPage from "./components/FTPManagerPage";

export default function App() {
  // "loading" during the initial ping; then "home" | "settings" | "library" | "queue" | "browse" | "iso2god" | "iso2xex" | "ftpmanager"
  const [page, setPage] = useState("loading");

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
    const cleanupCover = window.godsendApi.onXboxCover(({ titleId, src, dataUrl }: any) =>
      setCovers((prev) => ({ ...prev, [titleId]: src || dataUrl }))
    );
    const cleanupVisuals = window.godsendApi.onXboxTitleVisuals(({ titleId, visuals }: any) =>
      setTitleVisuals((prev) => ({ ...prev, [titleId]: visuals }))
    );

    initApp();

    const queueInterval = setInterval(async () => {
      const r = await window.godsendApi.getQueue().catch(() => ({ ok: false, jobs: [] }));
      if (r.ok) setQueueJobs(Array.isArray(r.jobs) ? r.jobs : []);
    }, 5000);

    return () => {
      cleanupOutput();
      cleanupCover();
      cleanupVisuals();
      clearInterval(queueInterval);
    };
  }, []);

  async function initApp() {
    const ping = await window.godsendApi.pingXbox();

    if (!ping.ok) {
      setFtpStatus("disconnected");
      setPage("home");
      return;
    }

    setFtpStatus("connected");
    setLibraryStatus("connecting");
    setPage("library");

    await loadAuroraLibrary();
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

  // ── Routing ───────────────────────────────────────────────────────────────
  if (page === "loading") {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <Loader2 className="h-6 w-6 animate-spin text-primary" />
      </div>
    );
  }

  if (page === "settings") {
    return (
      <SettingsPage
        onBack={() => setPage("home")}
        onAppendLine={appendLine}
      />
    );
  }

  if (page === "queue") {
    return <QueuePage onBack={() => setPage("home")} />;
  }

  if (page === "browse") {
    return <BrowsePage onBack={() => setPage("home")} />;
  }

  if (page === "iso2god") {
    return <ISO2GODPage onBack={() => setPage("home")} />;
  }

  if (page === "iso2xex") {
    return <ISO2XEXPage onBack={() => setPage("home")} />;
  }

  if (page === "ftpmanager") {
    return <FTPManagerPage onBack={() => setPage("home")} />;
  }

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
      />
    );
  }

  return (
    <HomePage
      outputLines={outputLines}
      logInfo={logInfo}
      ftpStatus={ftpStatus}
      onNavigateSettings={() => setPage("settings")}
      onNavigateQueue={() => setPage("queue")}
      onNavigateBrowse={() => setPage("browse")}
      onNavigateIso2God={() => setPage("iso2god")}
      onNavigateIso2Xex={() => setPage("iso2xex")}
      onNavigateFtpManager={() => setPage("ftpmanager")}
      onLibraryToggle={handleLibraryToggle}
      onReconnect={pingFtp}
      libraryLoading={libraryLoading}
      onAppendLine={appendLine}
      queueJobs={queueJobs}
    />
  );
}
