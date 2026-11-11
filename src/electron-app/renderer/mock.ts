/**
 * Development-only fixture for the Electron bridge.
 *
 * Vite serves the renderer in a plain browser with `?mock=1`; this module
 * installs a small `window.godsendApi` shim so the interface can be laid out,
 * clicked and screenshotted without a console, a backend or an Xbox. It is
 * loaded by a dynamic import behind `import.meta.env.DEV`, so it never ships
 * in a release bundle.
 *
 *   npm run renderer:dev   # then open http://localhost:1420/?mock=1
 */

type Handler = (args?: any) => unknown;

const listeners = new Map<string, Set<(payload: any) => void>>();

function on(event: string, cb: (payload: any) => void) {
  if (!listeners.has(event)) listeners.set(event, new Set());
  listeners.get(event)!.add(cb);
  return () => listeners.get(event)?.delete(cb);
}

function emit(event: string, payload: any) {
  listeners.get(event)?.forEach((cb) => cb(payload));
}

/* ── Cover art ───────────────────────────────────────────────────────────── */

function wrap(text: string, width: number, max: number): string[] {
  const words = text.replace(/\s*\(.*\)\s*/g, " ").trim().split(/\s+/);
  const lines: string[] = [];
  let line = "";
  for (const word of words) {
    if (line && (line + " " + word).length > width) {
      lines.push(line);
      line = word;
      if (lines.length === max - 1) break;
    } else {
      line = line ? `${line} ${word}` : word;
    }
  }
  if (line && lines.length < max) lines.push(line);
  return lines;
}

function esc(s: string) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

/** Procedural Xbox 360 box art: gradient field, initials, title, platform strip. */
function coverArt(name: string, tone: number): string {
  const title = wrap(name, 15, 3);
  const initials = title[0]
    .split(/\s+/)
    .slice(0, 2)
    .map((w) => w[0])
    .join("")
    .toUpperCase();
  const text = title
    .map(
      (line, i) =>
        `<text x="26" y="${300 + i * 26}" fill="#f4f7fc" font-family="Helvetica, Arial, sans-serif" font-size="21" font-weight="700" opacity=".94">${esc(line)}</text>`,
    )
    .join("");
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="400" viewBox="0 0 300 400">
    <defs>
      <linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stop-color="hsl(${tone} 62% 34%)"/>
        <stop offset=".55" stop-color="hsl(${tone + 28} 58% 15%)"/>
        <stop offset="1" stop-color="hsl(${tone + 54} 64% 8%)"/>
      </linearGradient>
      <linearGradient id="s" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stop-color="rgba(255,255,255,.16)"/>
        <stop offset="1" stop-color="rgba(255,255,255,0)"/>
      </linearGradient>
    </defs>
    <rect width="300" height="400" fill="url(#g)"/>
    <rect width="300" height="400" fill="url(#s)"/>
    <g opacity=".16" stroke="#fff" stroke-width="1">
      <path d="M0 92 H300 M0 152 H300 M0 212 H300 M60 0 V400 M150 0 V400 M240 0 V400"/>
    </g>
    <text x="24" y="86" fill="rgba(255,255,255,.5)" font-family="Helvetica, Arial, sans-serif"
      font-size="13" letter-spacing="4">XBOX 360</text>
    <text x="20" y="205" fill="rgba(255,255,255,.88)" font-family="Helvetica, Arial, sans-serif"
      font-size="86" font-weight="700" opacity=".16">${esc(initials)}</text>
    ${text}
    <rect x="0" y="372" width="300" height="28" fill="rgba(0,0,0,.45)"/>
    <circle cx="22" cy="386" r="5" fill="#34e37a"/>
    <text x="34" y="390" fill="rgba(255,255,255,.72)" font-family="Helvetica, Arial, sans-serif"
      font-size="10" letter-spacing="3">AURORA LIBRARY</text>
  </svg>`;
  // Base64 keeps parentheses and quotes in the SVG from breaking the CSS
  // `url(...)` that XboxBoxCover builds.
  return `data:image/svg+xml;base64,${btoa(unescape(encodeURIComponent(svg)))}`;
}

/* ── Fixtures ────────────────────────────────────────────────────────────── */

const GAMES: Array<[string, string, string, string, number]> = [
  ["Halo 3", "4D5307E6", "Bungie", "Microsoft Game Studios", 148],
  ["Halo: Reach", "4D53085B", "Bungie", "Microsoft Game Studios", 190],
  ["Gears of War 3", "4D5308AB", "Epic Games", "Microsoft Game Studios", 12],
  ["Forza Motorsport 4", "4D530910", "Turn 10 Studios", "Microsoft Game Studios", 205],
  ["Mass Effect 2", "454108CE", "BioWare", "Electronic Arts", 260],
  ["Red Dead Redemption", "5454082B", "Rockstar San Diego", "Rockstar Games", 18],
  ["Grand Theft Auto V", "545408A7", "Rockstar North", "Rockstar Games", 32],
  ["The Elder Scrolls V: Skyrim", "425307E6", "Bethesda Game Studios", "Bethesda Softworks", 280],
  ["Fallout: New Vegas", "425307E0", "Obsidian Entertainment", "Bethesda Softworks", 44],
  ["BioShock Infinite", "5454085D", "Irrational Games", "2K Games", 210],
  ["Batman: Arkham City", "57520802", "Rocksteady Studios", "Warner Bros.", 236],
  ["Portal 2", "454108D4", "Valve", "Valve", 300],
  ["Dark Souls", "4E4D083D", "FromSoftware", "Namco Bandai", 22],
  ["Dead Space 2", "454108DF", "Visceral Games", "Electronic Arts", 0],
  ["Alan Wake", "4D530805", "Remedy Entertainment", "Microsoft Game Studios", 168],
  ["Crackdown", "4D5307DC", "Realtime Worlds", "Microsoft Game Studios", 92],
  ["Fable II", "4D5307F1", "Lionhead Studios", "Microsoft Game Studios", 118],
  ["Lost Odyssey", "4D5307FA", "Mistwalker", "Microsoft Game Studios", 330],
  ["Halo 3: ODST", "4D530877", "Bungie", "Microsoft Game Studios", 176],
  ["Dishonored", "425307D1", "Arkane Studios", "Bethesda Softworks", 30],
  ["XCOM: Enemy Unknown", "54540882", "Firaxis Games", "2K Games", 64],
  ["Assassin's Creed II", "5553083B", "Ubisoft Montreal", "Ubisoft", 246],
  ["Call of Duty: Black Ops II", "415608C3", "Treyarch", "Activision", 40],
  ["Battlefield 3", "45410950", "DICE", "Electronic Arts", 150],
];

const games = GAMES.map(([name, titleId, developer, publisher, tone], i) => {
  // Real shape: contentId is a numeric row id, gameDataDir is
  // `<TITLEID>_<CONTENTID-hex>` as one path segment.
  const contentId = (i + 1).toString(16).toUpperCase().padStart(8, "0");
  return {
    titleId,
    contentId: String(i + 1),
    name,
    publisher,
    developer,
    releaseDate: "2007-09-25",
    gameDataDir: `${titleId}_${contentId}`,
    sourceDrive: i % 7 === 6 ? "Usb0" : "Hdd1",
    isFavorite: i % 5 === 0,
    discsInSet: name === "Lost Odyssey" ? 4 : name === "Mass Effect 2" ? 2 : 1,
    discNum: 1,
    timesPlayed: i * 3,
    lastPlayed: "2026-11-08",
    liveRating: (3.5 + (i % 4) * 0.4).toFixed(1),
    liveRaters: 400 + i * 137,
    __tone: tone,
  };
});

// The store listing uses the same titles plus regional and XBLA entries.
const STORE = [
  ...GAMES.map(([name]) => name),
  "Halo 3 (Japan)",
  "Halo 3 (USA, Europe) (En,Fr,De,Es,It)",
  "Forza Motorsport 4 (Europe) (En,Fr,De,Es,It)",
  "Gears of War 3 (Japan)",
  "Trials HD",
  "Shadow Complex",
  "Castle Crashers",
  "Geometry Wars: Retro Evolved 2",
  "Bastion",
  "Limbo",
  "Braid",
  "Splosion Man",
  "Toy Soldiers",
  "Mark of the Ninja",
  "Minecraft: Xbox 360 Edition",
  "Fez",
  "Super Meat Boy",
  "The Walking Dead",
  "Might & Magic: Clash of Heroes",
  "Peggle",
  "Pinball FX2",
  "Rez HD",
  "Ikaruga",
  "Radiant Silvergun",
  "Guardian Heroes",
  "Perfect Dark",
  "Banjo-Kazooie",
  "Jet Set Radio",
  "Sonic Adventure 2",
  "Skate 3",
  "SSX",
  "Fight Night Champion",
  "NBA Jam",
  "FIFA 12",
  "PES 2013",
  "WWE '13",
  "Ultimate Marvel vs. Capcom 3",
  "Street Fighter IV",
  "Mortal Kombat",
  "Injustice: Gods Among Us",
];

const OUTPUT = [
  "[INFO] Data directory (GODSEND HOME): /Users/you/GODsend",
  "[INFO] Local Transfer folder (GODSEND_TRANSFER): /Users/you/Downloads",
  "[INFO] Server port (GODSEND_PORT): 8081",
  "[17:45:44] [INFO] Server IP: 192.168.1.63:8081",
  "[17:45:44] [INFO] GODSEND_LISTEN_PORT=8081",
  "",
  "  ┌─────────────────────────────────────────────┐",
  "  │  GODsend Backend Server v2.14.1             │",
  "  │  ISO + XEX + XBLA + DLC + ROMs (EdgeEmu)    │",
  "  └─────────────────────────────────────────────┘",
  "",
  "[17:45:44] [INFO] Buffer pool: 4MB | 128KB | 1MB",
  "[17:45:44] CACHE: Loaded xbox360 from disk (4,446 titles)",
  "[17:45:44] CACHE: Loaded xbla from disk (612 titles)",
  "[17:45:44] CACHE: Loaded dlc from disk (1,204 titles)",
  "[17:45:44] MINERVA CACHE: Loaded xbox360 from disk (4,446 titles)",
  "[17:45:44] MINERVA CACHE: Loaded xbla from disk (612 titles)",
  "[17:45:44] Starting server on port 8081... Server started.",
  "[17:45:45] [INFO] Please start the script on the Xbox",
  "[17:45:45] FTP: FTP: connecting to 192.168.1.50:21",
  "[17:45:46] FTP: Connected to Aurora FTP server",
  "[17:45:46] FTP: Aurora library: 24 games on Hdd1:",
  "[17:45:47] [INFO] Library cache updated (24 games, 1 drive)",
];

const queueJob = {
  id: 12,
  name: "Halo 3",
  state: "Processing",
  progress: 62,
  detail: "Extracting ISO → GOD",
  speed: "84.2 MB/s",
  eta: "00:01:18",
  kind: "pipeline",
};

const handlers: Record<string, Handler> = {
  /* app shell */
  getOutputBuffer: () => OUTPUT,
  getLogsInfo: () => ({
    logsDirectory: "/Users/you/Library/Application Support/GODsend/logs",
    currentLogFile: "godsend-server-2026-11-11.log",
  }),
  onOutput: (cb: any) => on("output", cb),
  onXboxCover: (cb: any) => on("cover", cb),
  onXboxTitleVisuals: (cb: any) => on("visuals", cb),
  onFtpProgress: () => () => {},
  onFtpDebugLog: () => () => {},
  onDownloadCoversProgress: () => () => {},
  onBadAvatarProgress: () => () => {},

  /* console + queue */
  pingXbox: () => ({ ok: true, latencyMs: 4 }),
  getQueue: () => ({ ok: true, jobs: [queueJob] }),
  toolsFtpUploadStatus: () => ({
    ok: true,
    jobs: [
      {
        id: 31,
        name: "DLC: Halo 3 Mythic Map Pack",
        state: "Processing",
        progress: 38,
        detail: "00000002\\Content",
        speed: "12.4 MB/s",
      },
    ],
  }),
  restartProcess: () => ({ ok: true }),
  openExternal: () => ({ ok: true }),
  openLogsFolder: () => ({ ok: true }),

  /* Aurora library */
  listAuroraLibrary: () => ({
    ok: true,
    connectedTo: "192.168.1.50",
    fromCache: false,
    games: games.map(({ __tone, ...g }) => g),
  }),
  fetchAuroraCovers: (list: any) => {
    (list || []).forEach((entry: any, i: number) => {
      const game = games.find((g) => g.titleId === entry.titleId);
      setTimeout(() => {
        emit("cover", {
          titleId: entry.titleId,
          gameDataDir: entry.gameDataDir,
          src: coverArt(game?.name || entry.titleId, game?.__tone ?? 150),
          dataUrl: null,
        });
      }, 40 + i * 25);
    });
    return { ok: true };
  },
  refreshTitleVisualsFromCache: () => ({ ok: true }),
  decodeAsset: () => ({ ok: true, slots: [] }),
  listXboxDrives: () => ({ ok: true, drives: ["Hdd1:", "Usb0:", "Usb1:"] }),
  getDefaultXboxDrive: () => "Hdd1:",
  downloadAllCovers: () => ({ ok: true }),

  /* settings (paths are placeholders, never a real home directory) */
  getStartupEnabled: () => false,
  getEffectiveStoragePath: () => "/Users/you/GODsend",
  getDefaultStoragePath: () => "/Users/you/GODsend",
  getEffectiveBackendTempPath: () => "/tmp/godsend",
  getEffectiveTorrentTempPath: () => "/tmp/godsend/torrents",
  getDefaultTorrentTempPath: () => "/tmp/godsend/torrents",
  getAppDataDir: () => "/Users/you/Library/Application Support/GODsend",
  getDefaultAppDataDir: () => "/Users/you/Library/Application Support/GODsend",
  isPortable: () => false,
  getEffectiveTransferFolder: () => "/Users/you/Downloads",
  getEffectiveSaveBackupFolder: () => "/Users/you/Downloads/Saves",
  getServerPort: () => 8081,
  getROMPath: () => "[Drive]\\Emulators\\RetroArch\\roms\\[System]\\",
  getXboxConnection: () => ({
    xboxIp: "192.168.1.50",
    ftpUser: "xboxftp",
    ftpPassword: "",
    ftpScriptsPath: "Aurora",
  }),
  getAria2ListenPort: () => 6881,
  getAria2DhtPort: () => 6882,
  getCustomGodPath: () => "",
  getCustomXexPath: () => "",
  getMcp: () => ({ enabled: false, port: 8090 }),
  getScraperNetwork: () => ({ proxies: "", torControl: "", solverUrl: "" }),
  getProfileLabels: () => ({}),
  getDataStatus: () => ({ games: 0, covers: 0, saves: 0 }),

  /* store */
  browseGetGames: () => ({ ok: true, games: STORE }),
  browseGetDiscInfo: () => ({ ok: true, recommendation: "god" }),
  browseFetchCover: (name: string) => {
    const idx = STORE.indexOf(name);
    return { ok: true, dataUrl: coverArt(name, 140 + (idx >= 0 ? idx : 0) * 9) };
  },
  browseQueueGame: () => ({ ok: true, status: "Queued" }),
  getDebrid: () => ({ provider: "none" }),
  getArchiveAuth: () => ({ iaEmail: "", loggedIn: false }),

  /* FTP manager */
  toolsFtpList: () => ({
    ok: true,
    cwd: "Hdd1:\\",
    entries: [
      { name: "Content", type: "dir" },
      { name: "Games", type: "dir" },
      { name: "TitleUpdates", type: "dir" },
      { name: "Aurora", type: "dir" },
      { name: "launch.ini", type: "file", size: 1842 },
      { name: "default.xex", type: "file", size: 262144 },
    ],
  }),
};

/** Install the fixture bridge. Replaces the preload API entirely. */
export function installMock() {
  const api: Record<string, any> = {};
  for (const [name, fn] of Object.entries(handlers)) {
    // The preload bridge returns promises from invoke-style channels and a
    // cleanup function from event subscriptions. Keep both shapes.
    api[name] = (...args: any[]) => {
      const result = fn(...args);
      return typeof result === "function" ? result : Promise.resolve(result);
    };
  }

  const w = window as any;
  w.godsendApi = new Proxy(api, {
    get(target, prop: string) {
      if (prop in target) return target[prop];
      // Unknown channels degrade to a benign failure so pages render instead
      // of crashing on a fixture gap.
      return async () => ({ ok: false, error: `mock: no handler for ${String(prop)}` });
    },
  });
  // A tiny hook so Playwright can assert it is looking at the fixture.
  w.__GODSEND_MOCK__ = { games, store: STORE };
}
