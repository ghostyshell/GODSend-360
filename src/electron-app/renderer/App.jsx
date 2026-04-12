import { useState, useEffect, useCallback } from "react";
import { Loader2 } from "lucide-react";
import HomePage from "./components/HomePage";
import SettingsPage from "./components/SettingsPage";
import LibraryPage from "./components/LibraryPage";
import QueuePage from "./components/QueuePage";
import BrowsePage from "./components/BrowsePage";

export default function App() {
  // "loading" during the initial ping; then "home" | "settings" | "library" | "queue" | "browse"
  const [page, setPage] = useState("loading");

  // ── Console output ────────────────────────────────────────────────────────
  const [outputLines, setOutputLines] = useState([]);
  const [logInfo, setLogInfo]         = useState(null);

  // ── FTP connectivity status ───────────────────────────────────────────────
  const [ftpStatus, setFtpStatus] = useState("checking");

  // ── Queue state ───────────────────────────────────────────────────────────
  const [queueJobs, setQueueJobs] = useState([]);

  // ── Xbox library state ────────────────────────────────────────────────────
  const [libraryStatus, setLibraryStatus]           = useState("idle");
  const [libraryGames, setLibraryGames]             = useState([]);
  const [libraryConnectedTo, setLibraryConnectedTo] = useState("");
  const [covers, setCovers]                         = useState({});
  const [titleVisuals, setTitleVisuals]             = useState({});
  const [libraryLoading, setLibraryLoading]         = useState(false);
  const [libraryRefreshing, setLibraryRefreshing]  = useState(false);
  const [librarySources, setLibrarySources]         = useState(["Hdd1"]);

  async function loadAuroraLibrary(opts = {}) {
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
    setCovers({});
    setTitleVisuals({});
    setLibraryStatus(result.games.length === 0 ? "empty" : "ready");

    if (result.games.length > 0) {
      const fromDisk =
        result.fromCache === true && result.libraryUnchanged === true;
      window.godsendApi
        .fetchAuroraCovers(
          result.games.map((g) => ({
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
    window.godsendApi.getOutputBuffer().then((buf) => setOutputLines(buf));
    window.godsendApi.getLogsInfo().then((info) => setLogInfo(info));
    window.godsendApi.getAuroraLibrarySources().then((s) => {
      if (Array.isArray(s) && s.length > 0) setLibrarySources(s);
    }).catch(() => {});

    const cleanupOutput = window.godsendApi.onOutput((line) =>
      setOutputLines((prev) => [...prev, line])
    );
    const cleanupCover = window.godsendApi.onXboxCover(({ titleId, src, dataUrl }) =>
      setCovers((prev) => ({ ...prev, [titleId]: src || dataUrl }))
    );
    const cleanupVisuals = window.godsendApi.onXboxTitleVisuals(({ titleId, visuals }) =>
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
    (line) => setOutputLines((prev) => [...prev, line]),
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
      setCovers({});
      setTitleVisuals({});
      if (r.games.length > 0) {
        window.godsendApi
          .fetchAuroraCovers(
            r.games.map((g) => ({
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
    } catch (err) {
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
    } catch (err) {
      appendLine(`[ERROR] Xbox Library refresh: ${err.message || "Unknown error"}`);
    } finally {
      setLibraryRefreshing(false);
    }
  }

  // ── Library sources update (called from SettingsPage) ────────────────────
  function handleLibrarySourcesChanged(sources) {
    setLibrarySources(sources);
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
        onLibrarySourcesChanged={handleLibrarySourcesChanged}
      />
    );
  }

  if (page === "queue") {
    return <QueuePage onBack={() => setPage("home")} />;
  }

  if (page === "browse") {
    return <BrowsePage onBack={() => setPage("home")} />;
  }

  if (page === "library") {
    return (
      <LibraryPage
        status={libraryStatus}
        games={libraryGames}
        covers={covers}
        titleVisuals={titleVisuals}
        connectedTo={libraryConnectedTo}
        librarySources={librarySources}
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
      onLibraryToggle={handleLibraryToggle}
      onReconnect={pingFtp}
      libraryLoading={libraryLoading}
      onAppendLine={appendLine}
      queueJobs={queueJobs}
    />
  );
}
