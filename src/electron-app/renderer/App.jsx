import { useState, useEffect, useCallback } from "react";
import { Loader2 } from "lucide-react";
import HomePage from "./components/HomePage";
import SettingsPage from "./components/SettingsPage";
import LibraryPage from "./components/LibraryPage";

export default function App() {
  // "loading" during the initial ping; then "home" | "settings" | "library"
  const [page, setPage] = useState("loading");

  // ── Console output ────────────────────────────────────────────────────────
  const [outputLines, setOutputLines] = useState([]);
  const [logInfo, setLogInfo]         = useState(null);

  // ── FTP connectivity status ───────────────────────────────────────────────
  // "checking" | "connected" | "disconnected"
  const [ftpStatus, setFtpStatus] = useState("checking");

  // ── Xbox library state ────────────────────────────────────────────────────
  const [libraryStatus, setLibraryStatus]           = useState("idle");
  const [libraryGames, setLibraryGames]             = useState([]);
  const [libraryConnectedTo, setLibraryConnectedTo] = useState("");
  const [covers, setCovers]                         = useState({});
  const [libraryLoading, setLibraryLoading]         = useState(false);

  // ── Startup ───────────────────────────────────────────────────────────────
  useEffect(() => {
    // Kick off output + log loading in parallel with the ping
    window.godsendApi.getOutputBuffer().then((buf) => setOutputLines(buf));
    window.godsendApi.getLogsInfo().then((info) => setLogInfo(info));

    const cleanupOutput = window.godsendApi.onOutput((line) =>
      setOutputLines((prev) => [...prev, line])
    );
    const cleanupCover = window.godsendApi.onXboxCover(({ titleId, dataUrl }) =>
      setCovers((prev) => ({ ...prev, [titleId]: dataUrl }))
    );

    // Ping → auto-open library on success, fall back to home on failure
    initApp();

    return () => {
      cleanupOutput();
      cleanupCover();
    };
  }, []);

  async function initApp() {
    const ping = await window.godsendApi.pingXbox();

    if (!ping.ok) {
      setFtpStatus("disconnected");
      setPage("home");
      return;
    }

    // Connected — go straight to the library
    setFtpStatus("connected");
    setLibraryStatus("connecting");
    setPage("library");

    const result = await window.godsendApi.listXboxGames();

    if (!result.ok) {
      // Reachable but scan failed — fall back to home and flag as disconnected
      setFtpStatus("disconnected");
      setPage("home");
      return;
    }

    setLibraryGames(result.games);
    setLibraryConnectedTo(result.connectedTo);
    setCovers({});
    setLibraryStatus(result.games.length === 0 ? "empty" : "ready");

    if (result.games.length > 0) {
      window.godsendApi
        .fetchXboxCovers(result.games.map((g) => ({ titleId: g.titleId, ftpPath: g.coverFtpPath })))
        .catch(() => {});
    }
  }

  // ── FTP ping (used by reconnect button) ───────────────────────────────────
  async function pingFtp() {
    setFtpStatus("checking");
    const result = await window.godsendApi.pingXbox();
    setFtpStatus(result.ok ? "connected" : "disconnected");
  }

  const appendLine = useCallback(
    (line) => setOutputLines((prev) => [...prev, line]),
    []
  );

  // ── Library toggle (manual open/close from home page) ─────────────────────
  async function handleLibraryToggle() {
    if (page === "library") {
      setPage("home");
      return;
    }

    setLibraryLoading(true);
    setLibraryStatus("connecting");

    try {
      const result = await window.godsendApi.listXboxGames();

      if (!result.ok) {
        appendLine(`[ERROR] Xbox Library: ${result.error}`);
        setFtpStatus("disconnected");
        setLibraryLoading(false);
        return;
      }

      setLibraryGames(result.games);
      setLibraryConnectedTo(result.connectedTo);
      setCovers({});
      setLibraryStatus(result.games.length === 0 ? "empty" : "ready");
      setPage("library");
      setLibraryLoading(false);

      if (result.games.length > 0) {
        window.godsendApi
          .fetchXboxCovers(result.games.map((g) => ({ titleId: g.titleId, ftpPath: g.coverFtpPath })))
          .catch(() => {});
      }
    } catch (err) {
      appendLine(`[ERROR] Xbox Library: ${err.message || "Unknown error"}`);
      setLibraryLoading(false);
    }
  }

  // ── Routing ───────────────────────────────────────────────────────────────

  // Brief spinner while the initial ping is in flight
  if (page === "loading") {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <Loader2 className="h-6 w-6 animate-spin text-primary" />
      </div>
    );
  }

  if (page === "settings") {
    return <SettingsPage onBack={() => setPage("home")} onAppendLine={appendLine} />;
  }

  if (page === "library") {
    return (
      <LibraryPage
        status={libraryStatus}
        games={libraryGames}
        covers={covers}
        connectedTo={libraryConnectedTo}
        onToggle={handleLibraryToggle}
      />
    );
  }

  return (
    <HomePage
      outputLines={outputLines}
      logInfo={logInfo}
      ftpStatus={ftpStatus}
      onNavigateSettings={() => setPage("settings")}
      onLibraryToggle={handleLibraryToggle}
      onReconnect={pingFtp}
      libraryLoading={libraryLoading}
      onAppendLine={appendLine}
    />
  );
}
