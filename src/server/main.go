package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jlaffaye/ftp"
)

// ==========================================
// CONFIGURATION
// ==========================================
const (
	Port            = "8080"
	MaxPartSize     = 1800000000
	MaxDLCSizeBytes = 349 * 1024 * 1024
	CopyBufferSize  = 4 * 1024 * 1024
	ServeBufferSize = 128 * 1024
	FTPPort         = 21
	FTPTimeout      = 30 * time.Second
	FTPBufferSize   = 1 * 1024 * 1024
	FTPMaxRetries   = 3
	FTPRetryDelay   = 2 * time.Second
	TCPSendBuffer   = 512 * 1024
	TCPKeepAlive    = 30 * time.Second

	IADownloadBase = "https://archive.org/download/"

	// Parallel IA download settings
	iaChunkRetries      = 3
	iaChunkRetryBase    = 4 * time.Second
	iaParallelThreshold = 32 * 1024 * 1024 // files smaller than 32 MB use single stream
)

// ==========================================
// INTERNET ARCHIVE COLLECTION MAP
// Sources: https://r-roms.github.io/Microsoft/microsoft-xbox360
// Redump sections = ISO disc images (main use case)
// XBOX_360_* sections = mixed GOD/XEX/ISO archives
// ==========================================
var iaCollections = map[string][]string{
	// Xbox 360 Redump disc ISOs — primary pipeline (iso2god)
	"xbox360": {
		"microsoft_xbox360_numberssymbols",
		"microsoft_xbox360_a_part1", "microsoft_xbox360_a_part2",
		"microsoft_xbox360_b_part1", "microsoft_xbox360_b_part2",
		"microsoft_xbox360_c_part1", "microsoft_xbox360_c_part2",
		"microsoft_xbox360_d_part1", "microsoft_xbox360_d_part2", "microsoft_xbox360_d_part3",
		"microsoft_xbox360_e",
		"microsoft_xbox360_f_part1", "microsoft_xbox360_f_part2",
		"microsoft_xbox360_g",
		"microsoft_xbox360_h",
		"microsoft_xbox360_i",
		"microsoft_xbox360_j",
		"microsoft_xbox360_k",
		"microsoft_xbox360_l",
		"microsoft_xbox360_m_part1", "microsoft_xbox360_m_part2",
		"microsoft_xbox360_n_part1", "microsoft_xbox360_n_part2",
		"microsoft_xbox360_o",
		"microsoft_xbox360_p",
		"microsoft_xbox360_q",
		"microsoft_xbox360_r",
		"microsoft_xbox360_s_part1", "microsoft_xbox360_s_part2",
		"microsoft_xbox360_t_part1", "microsoft_xbox360_t_part2",
		"microsoft_xbox360_u",
		"microsoft_xbox360_v",
		"microsoft_xbox360_w",
		"microsoft_xbox360_x_part1", "microsoft_xbox360_x_part2",
		"microsoft_xbox360_y",
		"microsoft_xbox360_z",
	},
	// Original Xbox Redump disc ISOs
	"xbox": {
		"microsoft_xbox_numberssymbols",
		"microsoft_xbox_a", "microsoft_xbox_b", "microsoft_xbox_c",
		"microsoft_xbox_d", "microsoft_xbox_e", "microsoft_xbox_f",
		"microsoft_xbox_g", "microsoft_xbox_h", "microsoft_xbox_i",
		"microsoft_xbox_j", "microsoft_xbox_k", "microsoft_xbox_l",
		"microsoft_xbox_m", "microsoft_xbox_n", "microsoft_xbox_o",
		"microsoft_xbox_p", "microsoft_xbox_q", "microsoft_xbox_r",
		"microsoft_xbox_s", "microsoft_xbox_t", "microsoft_xbox_u",
		"microsoft_xbox_v", "microsoft_xbox_w", "microsoft_xbox_x",
		"microsoft_xbox_y", "microsoft_xbox_z",
	},
	// Digital / No-Intro XBLA titles (parseXboxHeader pipeline)
	"digital": {
		"microsoft_xbox360_digital_part1",
		"microsoft_xbox360_digital_part2",
		"microsoft_xbox360_digital_part3",
		"microsoft_xbox360_digital_part4",
		"microsoft_xbox360_digital_part5",
		"microsoft_xbox360_digital_part6",
		"microsoft_xbox360_digital_part7",
	},
	// Xbox 360 XBLA Arcade titles
	"xbla": {
		"XBOX_360_XBLA",
	},
	// Xbox 360 DLC packages — always installed to Hdd1
	"dlc": {
		"XBOX_360_DLC_1", "XBOX_360_DLC_2", "XBOX_360_DLC_3",
		"XBOX_360_DLC_4", "XBOX_360_DLC_5", "XBOX_360_DLC_6",
		"XBOX_360_XBLA_DLC",
	},
	// Xbox Live Indie Games
	"xblig": {
		"XBOX_360_XBLIG_1", "XBOX_360_XBLIG_2",
		"XBOX_360_XBLIG_3", "XBOX_360_XBLIG_4",
	},
	// General Xbox 360 game archives — may be ISO, GOD, or XEX folder zips
	"games": {
		"XBOX_360_1", "XBOX_360_1_OTHER",
		"XBOX_360_2", "XBOX_360_3", "XBOX_360_4",
		"XBOX_360_5", "XBOX_360_6",
	},
}

// ==========================================
// ROM SYSTEMS (EdgeEmu — edgeemu.net)
// platform key = "rom_" + sysid  (e.g. "rom_nes", "rom_snes")
// ==========================================

// ROMSystem describes one retro system served by EdgeEmu.
type ROMSystem struct {
	Name      string // Display name shown in Aurora menu
	BrowseURL string // edgeemu.net browse page URL
	Folder    string // RetroArch roms subfolder (e.g. "NES")
}

var romSystems = map[string]ROMSystem{
	// Atari
	"a2600":      {Name: "Atari - 2600",                        BrowseURL: "https://edgeemu.net/browse/atari-2600",                      Folder: "A2600"},
	"a5200":      {Name: "Atari - 5200",                        BrowseURL: "https://edgeemu.net/browse/atari-5200",                      Folder: "A5200"},
	"a7800":      {Name: "Atari - 7800",                        BrowseURL: "https://edgeemu.net/browse/atari-7800",                      Folder: "A7800"},
	"jaguar":     {Name: "Atari - Jaguar",                      BrowseURL: "https://edgeemu.net/browse/atari-jaguar",                    Folder: "JAG"},
	"jaguarcd":   {Name: "Atari - Jaguar CD",                   BrowseURL: "https://edgeemu.net/browse/atari-jaguar-cd",                 Folder: "JAGCD"},
	"lynx":       {Name: "Atari - Lynx",                        BrowseURL: "https://edgeemu.net/browse/atari-lynx",                      Folder: "LYNX"},
	"st":         {Name: "Atari - ST",                          BrowseURL: "https://edgeemu.net/browse/atari-st",                        Folder: "ST"},
	// Bandai
	"ws":         {Name: "Bandai - WonderSwan",                 BrowseURL: "https://edgeemu.net/browse/bandai-wonderswan",               Folder: "WS"},
	// Coleco
	"coleco":     {Name: "Coleco - ColecoVision",               BrowseURL: "https://edgeemu.net/browse/colecovision",                    Folder: "COLECO"},
	// Commodore
	"c64":        {Name: "Commodore - 64",                      BrowseURL: "https://edgeemu.net/browse/commodore-64",                    Folder: "C64"},
	"amiga":      {Name: "Commodore - Amiga",                   BrowseURL: "https://edgeemu.net/browse/commodore-amiga",                 Folder: "AMIGA"},
	"amigacd":    {Name: "Commodore - Amiga CD",                BrowseURL: "https://edgeemu.net/browse/commodore-amiga-cd",              Folder: "AMIGACD"},
	"amigacd32":  {Name: "Commodore - Amiga CD32",              BrowseURL: "https://edgeemu.net/browse/commodore-amiga-cd32",            Folder: "AMIGACD32"},
	"plus4":      {Name: "Commodore - Plus/4",                  BrowseURL: "https://edgeemu.net/browse/commodore-plus-4",                Folder: "PLUS4"},
	"vic20":      {Name: "Commodore - VIC-20",                  BrowseURL: "https://edgeemu.net/browse/commodore-vic-20",                Folder: "VIC20"},
	// Fairchild
	"channelf":   {Name: "Fairchild - Channel F",               BrowseURL: "https://edgeemu.net/browse/fairchild-channel-f",            Folder: "CHANNELF"},
	// GCE
	"vectrex":    {Name: "GCE - Vectrex",                       BrowseURL: "https://edgeemu.net/browse/gce-vectrex",                     Folder: "VECTREX"},
	// Microsoft
	"msx":        {Name: "Microsoft - MSX / MSX2",              BrowseURL: "https://edgeemu.net/browse/microsoft-msx",                  Folder: "MSX"},
	// NEC
	"pcecd":      {Name: "NEC - PC Engine CD / TurboGrafx CD",  BrowseURL: "https://edgeemu.net/browse/nec-pc-engine-cd-turbografx-cd", Folder: "PCECD"},
	"sgx":        {Name: "NEC - PC Engine SuperGrafx",          BrowseURL: "https://edgeemu.net/browse/nec-pc-engine-supergrafx",        Folder: "SGX"},
	"pce":        {Name: "NEC - PC Engine / TurboGrafx 16",     BrowseURL: "https://edgeemu.net/browse/nec-pc-engine-turbografx-16",    Folder: "PCE"},
	// Nintendo
	"nds":        {Name: "Nintendo - DS",                       BrowseURL: "https://edgeemu.net/browse/nintendo-ds",                    Folder: "NDS"},
	"fds":        {Name: "Nintendo - Famicom Disk System",      BrowseURL: "https://edgeemu.net/browse/nintendo-fds",                   Folder: "FDS"},
	"gb":         {Name: "Nintendo - Game Boy",                 BrowseURL: "https://edgeemu.net/browse/nintendo-gameboy",               Folder: "GB"},
	"gba":        {Name: "Nintendo - Game Boy Advance",         BrowseURL: "https://edgeemu.net/browse/nintendo-gameboy-advance",       Folder: "GBA"},
	"gbc":        {Name: "Nintendo - Game Boy Color",           BrowseURL: "https://edgeemu.net/browse/nintendo-gameboy-color",         Folder: "GBC"},
	"gc":         {Name: "Nintendo - GameCube",                 BrowseURL: "https://edgeemu.net/browse/nintendo-gamecube",              Folder: "GC"},
	"n64":        {Name: "Nintendo - 64",                       BrowseURL: "https://edgeemu.net/browse/nintendo-64",                    Folder: "N64"},
	"nes":        {Name: "Nintendo - NES",                      BrowseURL: "https://edgeemu.net/browse/nintendo-nes",                   Folder: "NES"},
	"sat":        {Name: "Nintendo - Satellaview",              BrowseURL: "https://edgeemu.net/browse/nintendo-satellaview",           Folder: "SAT"},
	"vb":         {Name: "Nintendo - Virtual Boy",              BrowseURL: "https://edgeemu.net/browse/nintendo-virtualboy",            Folder: "VB"},
	"snes":       {Name: "Nintendo - SNES",                     BrowseURL: "https://edgeemu.net/browse/nintendo-snes",                  Folder: "SNES"},
	// Panasonic
	"3do":        {Name: "Panasonic - 3DO",                     BrowseURL: "https://edgeemu.net/browse/panasonic-3do",                  Folder: "3DO"},
	// Philips
	"cdi":        {Name: "Philips - CDi",                       BrowseURL: "https://edgeemu.net/browse/philips-cdi",                    Folder: "CDI"},
	// RCA
	"studioii":   {Name: "RCA - Studio II",                     BrowseURL: "https://edgeemu.net/browse/rca-studioii",                   Folder: "STUDIOII"},
	// Sega
	"32x":        {Name: "Sega - 32X",                          BrowseURL: "https://edgeemu.net/browse/sega-32x",                       Folder: "32X"},
	"dc":         {Name: "Sega - Dreamcast",                    BrowseURL: "https://edgeemu.net/browse/sega-dreamcast",                 Folder: "DC"},
	"gg":         {Name: "Sega - Game Gear",                    BrowseURL: "https://edgeemu.net/browse/sega-gamegear",                  Folder: "GG"},
	"sms":        {Name: "Sega - Master System / Mark III",     BrowseURL: "https://edgeemu.net/browse/sega-sms",                       Folder: "SMS"},
	"scd":        {Name: "Sega - Mega-CD / Sega CD",            BrowseURL: "https://edgeemu.net/browse/sega-cd",                        Folder: "SCD"},
	"genesis":    {Name: "Sega - Mega Drive / Genesis",         BrowseURL: "https://edgeemu.net/browse/sega-genesis",                   Folder: "MD"},
	"pico":       {Name: "Sega - PICO",                         BrowseURL: "https://edgeemu.net/browse/sega-pico",                      Folder: "PICO"},
	"saturn":     {Name: "Sega - Saturn",                       BrowseURL: "https://edgeemu.net/browse/sega-saturn",                    Folder: "SATURN"},
	"sg1000":     {Name: "Sega - SG-1000",                      BrowseURL: "https://edgeemu.net/browse/sega-sg1000",                    Folder: "SG1000"},
	// Sinclair
	"zx":         {Name: "Sinclair - ZX Spectrum +3",           BrowseURL: "https://edgeemu.net/browse/sinclair-zx-spectrum-3",         Folder: "ZX"},
	// SNK
	"ngcd":       {Name: "SNK - Neo Geo CD",                    BrowseURL: "https://edgeemu.net/browse/snk-neo-geo-cd",                 Folder: "NGCD"},
	"ngpc":       {Name: "SNK - Neo Geo Pocket Color",          BrowseURL: "https://edgeemu.net/browse/snk-ngpc",                       Folder: "NGPC"},
	// Watara
	"supervision": {Name: "Watara - Supervision",               BrowseURL: "https://edgeemu.net/browse/watara-supervision",             Folder: "SUPERVISION"},
}

var (
	// romGameCache maps sysid → sorted list of game names scraped from edgeemu.
	romGameCache   = map[string][]string{}
	romGameCacheMu sync.RWMutex

	// romURLMap maps "sysid\x00lower(gameName)" → direct ZIP download URL.
	romURLMap   = map[string]string{}
	romURLMapMu sync.RWMutex

	// romRootPath is the drive-relative path for ROM installs on Xbox (no drive, no trailing slash).
	// Default: "Emulators\RetroArch\roms". Overridden by GODSEND_ROM_PATH env var.
	romRootPath string

	// edgeEmuHTTPClient is a plain client for edgeemu.net downloads (no IA auth headers).
	edgeEmuHTTPClient = &http.Client{Timeout: 0}
)

// ==========================================
// CACHE TYPES
// ==========================================

// IAGameEntry links a display name to its download location
type IAGameEntry struct {
	CollectionID string `json:"collection_id"`
	FileName     string `json:"filename"` // original filename with extension
}

// PlatformCache is what gets persisted to disk per platform
type PlatformCache struct {
	Games       []string               `json:"games"`
	GameEntries map[string]IAGameEntry `json:"game_entries"` // lower(name) -> entry
	BuildTime   time.Time              `json:"build_time"`
}

// buildState tracks live progress of a cache build
type buildState struct {
	total  int32
	loaded int32
	state  string // "idle" "building" "ready" "error"
}

var (
	// in-memory game lists per platform
	iaGameCache   = map[string][]string{}
	iaGameCacheMu sync.RWMutex

	// lower(name) -> IAGameEntry for fast download-URL lookup
	gameEntryMap   = map[string]IAGameEntry{}
	gameEntryMapMu sync.RWMutex

	// live build progress per platform
	buildStates   = map[string]*buildState{}
	buildStatesMu sync.Mutex

	// prevent double-building the same platform
	iaCacheBuilding = map[string]bool{}
	iaCacheBuildMu  sync.Mutex
)

// ==========================================
// SERVER STATE
// ==========================================

var (
	toolsDir        string
	transferDir     string // local ISO folder (default toolsDir/Transfer, or GODSEND_TRANSFER)
	sevenZipBin     string
	isoGodBin       string
	jobQueue        sync.Map
	suppressedJobs  sync.Map // games removed via /queue/remove — ignore logStatus until next /trigger
	iaCookieHeader        string // GODSEND_IA_COOKIE — browser session for archive.org
	iaAuthorizationHeader string // GODSEND_IA_AUTHORIZATION — optional Bearer/basic
	iaDownloadConcurrency int    // GODSEND_IA_CONCURRENCY — parallel chunk workers (1-7, default 4)
	iaHTTPClient          *http.Client
	serverIP              string
	gamePartsMap    sync.Map
	copyBuffer      []byte
	xboxConnections sync.Map
)

type XboxConnection struct {
	IP        string `json:"ip"`
	Drive     string `json:"drive"`
	GameName  string `json:"game"`
	Platform  string `json:"platform"`
	Mode      string `json:"mode"`
	Timestamp time.Time
}
type GameStatus struct {
	State   string `json:"state"`
	Message string `json:"message"`
}
type ProgressWriter struct {
	Total       int64
	Written     int64
	GameName    string
	LastLog     time.Time // logStatus cadence (500 ms — feeds Lua progress)
	LastConsole time.Time // logf cadence (15 s — feeds Electron terminal)
	StartTime   time.Time
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.Written += int64(n)
	now := time.Now()
	if now.Sub(pw.LastLog) > 500*time.Millisecond || pw.Written == pw.Total {
		percent := float64(pw.Written) / float64(pw.Total) * 100
		elapsed := now.Sub(pw.StartTime).Seconds()
		if elapsed < 0.001 {
			elapsed = 0.001
		}
		speedMBs := float64(pw.Written) / elapsed / 1048576
		writtenMB := float64(pw.Written) / 1048576
		totalMB := float64(pw.Total) / 1048576
		elapsedStr := fmtDuration(elapsed)
		var etaStr string
		if speedMBs > 0 && percent < 100 {
			etaSecs := float64(pw.Total-pw.Written) / (speedMBs * 1048576)
			etaStr = "~" + fmtDuration(etaSecs) + " left"
		} else {
			etaStr = "done"
		}
		logStatus(pw.GameName, "Processing",
			fmt.Sprintf("Downloading: %.0f%% (%.0f/%.0f MB) @ %.1f MB/s | %s | %s",
				percent, writtenMB, totalMB, speedMBs, elapsedStr, etaStr))
		// Log to the Electron terminal only every 15 s to avoid spamming.
		if now.Sub(pw.LastConsole) > 15*time.Second || pw.Written == pw.Total {
			logf("Download [%s]: %.1f%% (%.1f/%.1f MB) @ %.1f MB/s | %s",
				pw.GameName, percent, writtenMB, totalMB, speedMBs, elapsedStr)
			pw.LastConsole = now
		}
		pw.LastLog = now
	}
	return n, nil
}

// ==========================================
// LOGGING / RESPONSE HELPERS
// ==========================================

func jsonError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"state": "Error", "message": message})
}
func jsonSuccess(w http.ResponseWriter, data map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
func recoverMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logf("PANIC: %s %s: %v", r.Method, r.URL.Path, err)
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				logf("STACK: %s", string(buf[:n]))
				jsonError(w, 500, "Internal server error")
			}
		}()
		next(w, r)
	}
}
func logf(format string, args ...interface{}) {
	fmt.Printf("[%s] "+format+"\n", append([]interface{}{time.Now().Format("15:04:05")}, args...)...)
}
func logStatus(game, state, msg string) {
	if _, suppressed := suppressedJobs.Load(game); suppressed {
		return
	}
	jobQueue.Store(game, GameStatus{State: state, Message: msg})
}

// fmtDuration formats a duration in seconds as "1m23s" (or "45s" for < 60s).
func fmtDuration(secs float64) string {
	if secs < 0 {
		secs = 0
	}
	s := int(secs)
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm%02ds", s/60, s%60)
}

// ==========================================
// MAIN & SETUP
// ==========================================

func main() {
	if err := setupPaths(); err != nil {
		fmt.Printf("[FATAL] Setup failed: %v\n", err)
		os.Exit(1)
	}
	loadIAAuthFromEnv()
	serverIP = getOutboundIP()
	if serverIP == "" {
		serverIP = "0.0.0.0"
	}
	copyBuffer = make([]byte, CopyBufferSize)

	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║    GODSend Backend Server v2.1.0         ║")
	fmt.Println("║  ISO + XEX + XBLA + DLC + ROMs (EdgeEmu) ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Printf("\n[INFO] Server IP: %s:%s\n", serverIP, Port)
	fmt.Printf("[INFO] Copy Buffer: %d MB | Serve Buffer: %d KB | FTP Buffer: %d MB\n",
		CopyBufferSize/1024/1024, ServeBufferSize/1024, FTPBufferSize/1024/1024)
	fmt.Printf("[INFO] TCP: NODELAY=on SNDBUF=%dKB KeepAlive=%s\n", TCPSendBuffer/1024, TCPKeepAlive)
	fmt.Printf("[INFO] File serving: sendfile() zero-copy via http.ServeContent\n")
	fmt.Printf("[INFO] Transfer folder (local ISOs): %s\n", transferDir)
	fmt.Printf("[INFO] ROM install path (on Xbox): [Drive]\\%s\\[System]\\\n", romRootPath)
	verifyTools()

	// Initialise build-state trackers for every platform
	buildStatesMu.Lock()
	for p := range iaCollections {
		buildStates[p] = &buildState{state: "idle"}
	}
	buildStatesMu.Unlock()

	// Load persisted caches from disk. If a cache exists it is used as-is —
	// no automatic background refresh. Use the "Refresh Cache" button in the
	// Electron settings (or /cache-refresh endpoint) to force a rebuild.
	// Platforms with no disk cache are built immediately in the background.
	platformOrder := []string{"xbox360", "digital", "xbla", "dlc", "xblig", "games", "xbox"}
	var delay time.Duration
	for _, platform := range platformOrder {
		loaded := loadCacheFromDisk(platform)
		if loaded {
			logf("CACHE: Loaded %s from disk", platform)
		} else {
			go func(p string, d time.Duration) {
				if d > 0 {
					time.Sleep(d)
				}
				buildIAGameCache(p)
			}(platform, delay)
			delay += 800 * time.Millisecond
		}
	}

	// Reload any previously scraped ROM caches from disk (lazy — won't block startup).
	go func() {
		for sysid := range romSystems {
			if loadROMCacheFromDisk(sysid) {
				logf("ROM CACHE: Loaded %s from disk", sysid)
			}
		}
	}()

	http.HandleFunc("/browse", recoverMiddleware(handleBrowse))
	http.HandleFunc("/cache-status", recoverMiddleware(handleCacheStatus))
	http.HandleFunc("/cache-refresh", recoverMiddleware(handleCacheRefresh))
	http.HandleFunc("/trigger", recoverMiddleware(handleTrigger))
	http.HandleFunc("/status", recoverMiddleware(handleStatus))
	http.HandleFunc("/queue", recoverMiddleware(handleQueue))
	http.HandleFunc("/queue/remove", recoverMiddleware(handleQueueRemove))
	http.HandleFunc("/debug", recoverMiddleware(handleDebug))
	http.HandleFunc("/register", recoverMiddleware(handleRegister))
	http.HandleFunc("/files/", recoverMiddleware(handleFileServe))

	server := &http.Server{
		Addr:              ":" + Port,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ConnState: func(conn net.Conn, state http.ConnState) {
			if state == http.StateNew {
				if tc, ok := conn.(*net.TCPConn); ok {
					tc.SetNoDelay(true)
					tc.SetKeepAlive(true)
					tc.SetKeepAlivePeriod(TCPKeepAlive)
					tc.SetWriteBuffer(TCPSendBuffer)
					tc.SetReadBuffer(TCPSendBuffer)
				}
			}
		},
	}
	logf("Starting server on port %s... Server started. Please start the script on the xbox", Port)
	if err := server.ListenAndServe(); err != nil {
		fmt.Printf("[FATAL] %v\n", err)
		os.Exit(1)
	}
}

func setupPaths() error {
	ex, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable path: %w", err)
	}
	exDir := filepath.Dir(ex)
	if v := strings.TrimSpace(os.Getenv("GODSEND_HOME")); v != "" {
		abs, err := filepath.Abs(v)
		if err != nil {
			return fmt.Errorf("GODSEND_HOME: %w", err)
		}
		toolsDir = abs
		logf("[INFO] Data directory (GODSEND_HOME): %s", toolsDir)
		logf("[INFO] Executable: %s", ex)
	} else {
		toolsDir = exDir
	}
	if runtime.GOOS == "windows" {
		sevenZipBin = "7z.exe"
		isoGodBin = "iso2god.exe"
	} else {
		sevenZipBin = "7zz"
		isoGodBin = "iso2god"
	}
	for _, dir := range []string{"Ready", "Temp", "cache"} {
		if err := os.MkdirAll(filepath.Join(toolsDir, dir), 0755); err != nil {
			return err
		}
	}
	// ROM install path (drive-relative, no drive letter, no trailing slash)
	romRootPath = "Emulators\\RetroArch\\roms"
	if v := strings.TrimSpace(os.Getenv("GODSEND_ROM_PATH")); v != "" {
		v = strings.ReplaceAll(v, "/", "\\")
		romRootPath = strings.TrimRight(v, "\\")
	}

	transferDir = filepath.Join(toolsDir, "Transfer")
	if v := strings.TrimSpace(os.Getenv("GODSEND_TRANSFER")); v != "" {
		abs, err := filepath.Abs(v)
		if err != nil {
			return fmt.Errorf("GODSEND_TRANSFER: %w", err)
		}
		transferDir = abs
		logf("[INFO] Local Transfer folder (GODSEND_TRANSFER): %s", transferDir)
	}
	if err := os.MkdirAll(transferDir, 0755); err != nil {
		return err
	}
	cleanupEmptyReadyDirs()
	return nil
}

// loadIAAuthFromEnv reads optional Internet Archive credentials and download settings.
func loadIAAuthFromEnv() {
	v := strings.TrimSpace(os.Getenv("GODSEND_IA_COOKIE"))
	if len(v) > 7 && strings.EqualFold(v[:7], "cookie:") {
		v = strings.TrimSpace(v[7:])
	}
	v = strings.ReplaceAll(strings.ReplaceAll(v, "\r", ""), "\n", "")
	iaCookieHeader = strings.TrimSpace(v)

	a := strings.TrimSpace(os.Getenv("GODSEND_IA_AUTHORIZATION"))
	if len(a) > 14 && strings.EqualFold(a[:14], "authorization:") {
		a = strings.TrimSpace(a[14:])
	}
	iaAuthorizationHeader = strings.TrimSpace(a)

	// Parallel download concurrency (1-7, default 4)
	iaDownloadConcurrency = 5
	if c, err := strconv.Atoi(strings.TrimSpace(os.Getenv("GODSEND_IA_CONCURRENCY"))); err == nil {
		if c < 1 {
			c = 1
		} else if c > 7 {
			c = 7
		}
		iaDownloadConcurrency = c
	}

	// Shared IA HTTP client: forwards auth headers across redirects (IA redirects to mirrors).
	iaHTTPClient = &http.Client{
		Timeout: 0,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			// Re-apply auth headers — Go strips them on cross-host redirects by default.
			if len(via) > 0 {
				for key, vals := range via[0].Header {
					if _, set := req.Header[key]; !set {
						req.Header[key] = vals
					}
				}
			}
			return nil
		},
	}

	if iaCookieHeader != "" {
		logf("[INFO] Internet Archive: Cookie header set (%d chars)", len(iaCookieHeader))
	}
	if iaAuthorizationHeader != "" {
		logf("[INFO] Internet Archive: Authorization header set (%d chars)", len(iaAuthorizationHeader))
	}
	logf("[INFO] Internet Archive: download concurrency = %d", iaDownloadConcurrency)
}

// applyArchiveOrgHeaders adds session/auth headers for archive.org HTTP requests.
func applyArchiveOrgHeaders(req *http.Request) {
	if iaCookieHeader != "" {
		req.Header.Set("Cookie", iaCookieHeader)
	}
	if iaAuthorizationHeader != "" {
		req.Header.Set("Authorization", iaAuthorizationHeader)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
}

// cleanupEmptyReadyDirs removes any subdirectory under Ready/ that contains no files.
// These are artifacts left by FTP-mode transfers (which never populate the Ready folder)
// or from sessions that crashed before completing.
func cleanupEmptyReadyDirs() {
	readyDir := filepath.Join(toolsDir, "Ready")
	entries, err := os.ReadDir(readyDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subDir := filepath.Join(readyDir, e.Name())
		hasFiles := false
		filepath.Walk(subDir, func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				hasFiles = true
				return filepath.SkipAll
			}
			return nil
		})
		if !hasFiles {
			logf("Cleanup: removing empty Ready dir: %s", e.Name())
			os.RemoveAll(subDir)
		}
	}
}

func verifyTools() {
	for _, t := range []struct{ n, p string }{
		{"7-Zip", filepath.Join(toolsDir, sevenZipBin)},
		{"iso2god", filepath.Join(toolsDir, isoGodBin)},
	} {
		if _, err := os.Stat(t.p); os.IsNotExist(err) {
			logf("WARNING: %s not found at %s", t.n, t.p)
		} else {
			logf("%s found: %s", t.n, t.p)
		}
	}
}

// ==========================================
// CACHE — DISK PERSISTENCE
// ==========================================

func cacheFilePath(platform string) string {
	return filepath.Join(toolsDir, "cache", platform+".json")
}

func saveCacheToDisk(platform string, games []string, entries map[string]IAGameEntry) {
	pc := PlatformCache{
		Games:       games,
		GameEntries: entries,
		BuildTime:   time.Now(),
	}
	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		logf("CACHE SAVE ERROR %s: %v", platform, err)
		return
	}
	if err := os.WriteFile(cacheFilePath(platform), data, 0644); err != nil {
		logf("CACHE SAVE ERROR %s: %v", platform, err)
		return
	}
	logf("CACHE: Saved %s (%d games) to disk", platform, len(games))
}

// loadCacheFromDisk returns true if a usable cache was loaded.
// Caches older than 7 days will be refreshed in background automatically.
func loadCacheFromDisk(platform string) bool {
	data, err := os.ReadFile(cacheFilePath(platform))
	if err != nil {
		return false
	}
	var pc PlatformCache
	if err := json.Unmarshal(data, &pc); err != nil {
		return false
	}
	if len(pc.Games) == 0 {
		return false
	}

	iaGameCacheMu.Lock()
	iaGameCache[platform] = pc.Games
	iaGameCacheMu.Unlock()

	gameEntryMapMu.Lock()
	for k, v := range pc.GameEntries {
		gameEntryMap[k] = v
	}
	gameEntryMapMu.Unlock()

	setBuildState(platform, "ready", int32(len(pc.Games)), int32(len(pc.Games)))
	return true
}

// ==========================================
// CACHE — BUILD PROGRESS
// ==========================================

func getBuildState(platform string) *buildState {
	buildStatesMu.Lock()
	s, ok := buildStates[platform]
	if !ok {
		s = &buildState{state: "idle"}
		buildStates[platform] = s
	}
	buildStatesMu.Unlock()
	return s
}

func setBuildState(platform, state string, loaded, total int32) {
	s := getBuildState(platform)
	atomic.StoreInt32(&s.loaded, loaded)
	atomic.StoreInt32(&s.total, total)
	buildStatesMu.Lock()
	s.state = state
	buildStatesMu.Unlock()
}

// ==========================================
// CACHE — BUILD (PARALLEL FETCH)
// ==========================================

// iaMetaResponse is the top-level shape of https://archive.org/metadata/<id>
type iaMetaResponse struct {
	Files []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
		Format string `json:"format"`
	} `json:"files"`
}

// archiveExts lists the file extensions we treat as downloadable game archives.
var archiveExts = map[string]bool{".zip": true, ".rar": true, ".7z": true}

// iaFetchSem is a global semaphore capping simultaneous archive.org metadata
// requests across ALL platform cache builds.  Without this, 90+ goroutines fire
// simultaneously and most time out due to IA rate-limiting.
var iaFetchSem = make(chan struct{}, 6)

const (
	maxIARetries  = 4
	iaBaseTimeout = 60 * time.Second
)

// iaRetryBackoff is the wait before each retry attempt (index 0 = first retry).
var iaRetryBackoff = []time.Duration{3 * time.Second, 8 * time.Second, 20 * time.Second, 40 * time.Second}

// doIAMetaFetch performs one HTTP GET of the IA metadata API and returns parsed
// entries.  The global semaphore slot must NOT be held by the caller.
func doIAMetaFetch(collectionID string) ([]IAGameEntry, error) {
	iaFetchSem <- struct{}{} // acquire slot
	defer func() { <-iaFetchSem }()

	apiURL := "https://archive.org/metadata/" + collectionID
	client := &http.Client{Timeout: iaBaseTimeout}
	req, _ := http.NewRequest("GET", apiURL, nil)
	applyArchiveOrgHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", collectionID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: HTTP %d", collectionID, resp.StatusCode)
	}

	var meta iaMetaResponse
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("%s: JSON decode: %w", collectionID, err)
	}

	var entries []IAGameEntry
	for _, f := range meta.Files {
		// Skip IA-generated derivatives (thumbnails, torrent, xml metadata…)
		if f.Source != "original" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if !archiveExts[ext] {
			continue
		}
		entries = append(entries, IAGameEntry{
			CollectionID: collectionID,
			FileName:     f.Name,
		})
	}
	return entries, nil
}

// fetchIACollectionEntries wraps doIAMetaFetch with exponential-backoff retries.
func fetchIACollectionEntries(collectionID string) ([]IAGameEntry, error) {
	entries, err := doIAMetaFetch(collectionID)
	if err == nil {
		return entries, nil
	}
	for attempt := 0; attempt < len(iaRetryBackoff); attempt++ {
		wait := iaRetryBackoff[attempt]
		logf("CACHE RETRY [%s] attempt %d/%d in %v: %v",
			collectionID, attempt+1, maxIARetries-1, wait, err)
		time.Sleep(wait)
		entries, err = doIAMetaFetch(collectionID)
		if err == nil {
			return entries, nil
		}
	}
	return nil, fmt.Errorf("%s: gave up after %d attempts: %w", collectionID, maxIARetries, err)
}

// buildIAGameCache fetches all collections for a platform sequentially
// (controlled by the global semaphore) so archive.org isn't overwhelmed.
// Safe to call multiple times — deduplicates via iaCacheBuilding guard.
func buildIAGameCache(platform string) {
	iaCacheBuildMu.Lock()
	if iaCacheBuilding[platform] {
		iaCacheBuildMu.Unlock()
		return
	}
	iaCacheBuilding[platform] = true
	iaCacheBuildMu.Unlock()

	defer func() {
		iaCacheBuildMu.Lock()
		iaCacheBuilding[platform] = false
		iaCacheBuildMu.Unlock()
	}()

	colls, ok := iaCollections[platform]
	if !ok {
		return
	}

	total := int32(len(colls))
	setBuildState(platform, "building", 0, total)
	logf("CACHE: Building %s — %d collections...", platform, total)

	type result struct {
		entries      []IAGameEntry
		collectionID string
		err          error
	}
	ch := make(chan result, len(colls))

	for _, coll := range colls {
		go func(c string) {
			entries, err := fetchIACollectionEntries(c)
			ch <- result{entries, c, err}
		}(coll)
	}

	newEntries := map[string]IAGameEntry{}
	var allGames []string
	var loaded int32

	for range colls {
		r := <-ch
		loaded++
		setBuildState(platform, "building", loaded, total)

		if r.err != nil {
			logf("CACHE WARN [%s]: %v", platform, r.err)
			continue
		}
		for _, e := range r.entries {
			ext := filepath.Ext(e.FileName)
			name := strings.TrimSuffix(e.FileName, ext)
			lower := strings.ToLower(name)
			newEntries[lower] = e
			allGames = append(allGames, name)
		}
		logf("CACHE [%s] %d/%d: %s (%d files)", platform, loaded, total, r.collectionID, len(r.entries))
	}

	sort.Strings(allGames)
	setBuildState(platform, "ready", total, total)
	logf("CACHE: %s complete — %d games", platform, len(allGames))

	iaGameCacheMu.Lock()
	iaGameCache[platform] = allGames
	iaGameCacheMu.Unlock()

	gameEntryMapMu.Lock()
	for k, v := range newEntries {
		gameEntryMap[k] = v
	}
	gameEntryMapMu.Unlock()

	saveCacheToDisk(platform, allGames, newEntries)
}

// ==========================================
// CACHE — LOOKUP
// ==========================================

// findIAEntry returns the IAGameEntry for a game, searching cached data.
// Falls back to a live per-letter search if cache is empty.
func findIAEntry(gameName, platform string) (IAGameEntry, error) {
	lower := strings.ToLower(gameName)

	gameEntryMapMu.RLock()
	entry, ok := gameEntryMap[lower]
	gameEntryMapMu.RUnlock()
	if ok {
		return entry, nil
	}

	// Fuzzy: game name contains the search term (handles region tags)
	gameEntryMapMu.RLock()
	for k, e := range gameEntryMap {
		baseName := strings.ToLower(strings.Split(k, " (")[0])
		searchBase := strings.ToLower(strings.Split(gameName, " (")[0])
		if strings.Contains(k, lower) || baseName == searchBase {
			gameEntryMapMu.RUnlock()
			return e, nil
		}
	}
	gameEntryMapMu.RUnlock()

	// Live fetch from the relevant collection page(s)
	entry, err := liveSearchIA(gameName, platform)
	if err != nil {
		return IAGameEntry{}, fmt.Errorf("not found in Internet Archive: %s", gameName)
	}
	return entry, nil
}

// liveSearchIA searches IA collections via the Metadata API when the cache is cold.
// It narrows candidates by first letter for letter-indexed collections (Redump),
// then falls back to all collections in the platform if nothing is found.
func liveSearchIA(gameName, platform string) (IAGameEntry, error) {
	colls, ok := iaCollections[platform]
	if !ok {
		return IAGameEntry{}, fmt.Errorf("unknown platform: %s", platform)
	}

	// Narrow by first letter for Redump-style collections
	candidates := colls
	if len(gameName) > 0 {
		firstLetter := strings.ToLower(string([]rune(gameName)[0]))
		var narrowed []string
		for _, c := range colls {
			lc := strings.ToLower(c)
			if strings.HasSuffix(lc, "_"+firstLetter) ||
				strings.Contains(lc, "_"+firstLetter+"_part") ||
				((firstLetter >= "0" && firstLetter <= "9") && strings.HasSuffix(lc, "_numberssymbols")) {
				narrowed = append(narrowed, c)
			}
		}
		if len(narrowed) > 0 {
			candidates = narrowed
		}
	}

	lowerSearch := strings.ToLower(gameName)

	for _, coll := range candidates {
		// Reuse doIAMetaFetch so live searches share the same semaphore + timeout
		entries, err := doIAMetaFetch(coll)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.FileName), lowerSearch) {
				return e, nil
			}
		}
	}
	return IAGameEntry{}, fmt.Errorf("no match for '%s'", gameName)
}

// ==========================================
// LOCAL TRANSFER FOLDER HELPERS
// ==========================================

func scanTransferFolder() []string {
	entries, err := os.ReadDir(transferDir)
	if err != nil {
		return nil
	}
	var games []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(strings.ToLower(n), ".iso") {
			games = append(games, strings.TrimSuffix(n, filepath.Ext(n)))
		}
	}
	sort.Strings(games)
	return games
}

func findLocalISO(gameName string) string {
	entries, err := os.ReadDir(transferDir)
	if err != nil {
		return ""
	}
	lower := strings.ToLower(gameName)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
			continue
		}
		if strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))) == lower {
			return filepath.Join(transferDir, e.Name())
		}
	}
	return ""
}

func isGameReadyLocally(gameName string) bool {
	_, err := os.Stat(filepath.Join(toolsDir, "Ready", sanitizeFilename(gameName), "godsend.ini"))
	return err == nil
}

// ==========================================
// HTTP HANDLERS
// ==========================================

func handleBrowse(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	logf("BROWSE: platform=%s", platform)

	// ROM platforms — served from edgeemu.net scrape cache
	if strings.HasPrefix(platform, "rom_") {
		sysid := strings.TrimPrefix(platform, "rom_")
		sys, ok := romSystems[sysid]
		if !ok {
			jsonError(w, 400, "Unknown ROM system: "+sysid)
			return
		}
		romGameCacheMu.RLock()
		cached, ok := romGameCache[sysid]
		romGameCacheMu.RUnlock()
		if ok && len(cached) > 0 {
			logf("BROWSE: Serving %d cached ROMs for %s", len(cached), sys.Name)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(strings.Join(cached, "|")))
			return
		}
		go buildROMGameCache(sysid)
		s := getBuildState(platform)
		loaded := atomic.LoadInt32(&s.loaded)
		total := atomic.LoadInt32(&s.total)
		if total == 0 {
			total = 1
		}
		logf("BROWSE: ROM cache building for %s", sysid)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "__IA_LOADING__:%d/%d", loaded, total)
		return
	}

	// Local — scan Transfer folder immediately, no IA needed
	if platform == "local" {
		games := scanTransferFolder()
		logf("BROWSE: %d local ISOs found", len(games))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(strings.Join(games, "|")))
		return
	}

	// Online — return cached list or loading marker with progress
	iaGameCacheMu.RLock()
	cached, ok := iaGameCache[platform]
	iaGameCacheMu.RUnlock()

	if ok && len(cached) > 0 {
		logf("BROWSE: Serving %d cached games for %s", len(cached), platform)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(strings.Join(cached, "|")))
		return
	}

	// Not ready — trigger build (safe no-op if already running) and return progress
	go buildIAGameCache(platform)

	s := getBuildState(platform)
	loaded := atomic.LoadInt32(&s.loaded)
	total := atomic.LoadInt32(&s.total)
	if total == 0 {
		total = int32(len(iaCollections[platform]))
	}
	logf("BROWSE: %s cache building %d/%d", platform, loaded, total)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "__IA_LOADING__:%d/%d", loaded, total)
}

func handleCacheStatus(w http.ResponseWriter, r *http.Request) {
	type platformStatus struct {
		State  string `json:"state"`
		Loaded int32  `json:"loaded"`
		Total  int32  `json:"total"`
		Games  int    `json:"games"`
	}
	result := map[string]platformStatus{}

	buildStatesMu.Lock()
	for p, s := range buildStates {
		iaGameCacheMu.RLock()
		count := len(iaGameCache[p])
		iaGameCacheMu.RUnlock()
		result[p] = platformStatus{
			State:  s.state,
			Loaded: atomic.LoadInt32(&s.loaded),
			Total:  atomic.LoadInt32(&s.total),
			Games:  count,
		}
	}
	buildStatesMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleCacheRefresh triggers a fresh rebuild for one platform or all IA platforms.
// ?platform=all  — rebuild all IA platforms (xbox360, digital, xbla, dlc, xblig, games, xbox)
// ?platform=xbox360 — rebuild a single IA platform
// ?platform=rom_nes — rebuild the ROM cache for one system
// Returns immediately; the build runs in the background.
func handleCacheRefresh(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")

	if platform == "" || platform == "all" {
		logf("CACHE REFRESH: all IA platforms requested")
		for p := range iaCollections {
			go buildIAGameCache(p)
		}
		// Also refresh any ROM system that already has a cache on disk
		var romRefreshed []string
		romGameCacheMu.RLock()
		for sysid := range romSystems {
			if len(romGameCache[sysid]) > 0 {
				romRefreshed = append(romRefreshed, sysid)
			}
		}
		romGameCacheMu.RUnlock()
		for _, sysid := range romRefreshed {
			go buildROMGameCache(sysid)
		}
		logf("CACHE REFRESH: %d previously-used ROM systems queued", len(romRefreshed))
		jsonSuccess(w, map[string]string{"status": "refreshing", "platforms": "all"})
		return
	}

	if strings.HasPrefix(platform, "rom_") {
		sysid := strings.TrimPrefix(platform, "rom_")
		if _, ok := romSystems[sysid]; !ok {
			jsonError(w, 400, "Unknown ROM system: "+sysid)
			return
		}
		logf("CACHE REFRESH: ROM system %s", sysid)
		go buildROMGameCache(sysid)
		jsonSuccess(w, map[string]string{"status": "refreshing", "platform": platform})
		return
	}

	if _, ok := iaCollections[platform]; !ok {
		jsonError(w, 400, "Unknown platform: "+platform)
		return
	}
	logf("CACHE REFRESH: %s", platform)
	go buildIAGameCache(platform)
	jsonSuccess(w, map[string]string{"status": "refreshing", "platform": platform})
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	gameName := r.URL.Query().Get("game")
	xboxIP := r.URL.Query().Get("ip")
	drive := r.URL.Query().Get("drive")
	platform := r.URL.Query().Get("platform")
	mode := r.URL.Query().Get("mode")
	if gameName == "" || xboxIP == "" {
		jsonError(w, 400, "Missing game or ip parameter")
		return
	}
	if net.ParseIP(xboxIP) == nil {
		jsonError(w, 400, "Invalid IP address format")
		return
	}
	if drive == "" {
		drive = "Hdd1:"
	}
	if mode == "" {
		mode = "http"
	}
	if platform == "" {
		platform = "xbox360"
	}
	xboxConnections.Store(gameName, XboxConnection{
		IP: xboxIP, Drive: drive, GameName: gameName,
		Platform: platform, Mode: mode, Timestamp: time.Now(),
	})
	logf("REGISTER: Xbox %s for %s (mode=%s drive=%s)", xboxIP, gameName, mode, drive)
	jsonSuccess(w, map[string]string{"status": "registered", "mode": mode, "ip": xboxIP, "drive": drive})
}

func handleTrigger(w http.ResponseWriter, r *http.Request) {
	gameName := r.URL.Query().Get("game")
	platform := r.URL.Query().Get("platform")
	if gameName == "" {
		jsonError(w, 400, "Missing game parameter")
		return
	}
	if platform == "" {
		platform = "xbox360"
	}
	suppressedJobs.Delete(gameName)

	if status, exists := jobQueue.Load(gameName); exists {
		gs := status.(GameStatus)
		if gs.State == "Ready" {
			jsonSuccess(w, map[string]string{"status": "already_ready"})
			return
		}
		if gs.State == "Processing" {
			jsonSuccess(w, map[string]string{"status": "already_processing"})
			return
		}
	}

	launcher := func(fn func()) {
		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					logf("PANIC processing %s: %v", gameName, rec)
					buf := make([]byte, 4096)
					n := runtime.Stack(buf, false)
					logf("STACK: %s", string(buf[:n]))
					logStatus(gameName, "Error", "Server crashed during processing")
				}
			}()
			fn()
		}()
	}

	// Local ISO in Transfer folder takes priority for disc-based platforms
	if platform == "xbox360" || platform == "xbox" || platform == "local" {
		if iso := findLocalISO(gameName); iso != "" {
			logf("TRIGGER: Local ISO found for '%s'", gameName)
			launcher(func() { processLocalISO(gameName, iso) })
			jsonSuccess(w, map[string]string{"status": "triggered", "source": "local"})
			return
		}
		if isGameReadyLocally(gameName) {
			logStatus(gameName, "Ready", "Ready to Install")
			jsonSuccess(w, map[string]string{"status": "already_ready"})
			return
		}
		// Local Transfer list (platform=local): never use Internet Archive
		if platform == "local" {
			logStatus(gameName, "Error", "No ISO in Transfer folder for \""+gameName+"\"")
			jsonSuccess(w, map[string]string{
				"status":  "local_unavailable",
				"message": "Add the game ISO to your Transfer folder, then queue again.",
			})
			return
		}
	}

	// ROM platforms (edgeemu.net)
	if strings.HasPrefix(platform, "rom_") {
		sysid := strings.TrimPrefix(platform, "rom_")
		if _, ok := romSystems[sysid]; !ok {
			jsonError(w, 400, "Unknown ROM system: "+sysid)
			return
		}
		if isGameReadyLocally(gameName) {
			logStatus(gameName, "Ready", "Ready to Install")
			jsonSuccess(w, map[string]string{"status": "already_ready"})
			return
		}
		launcher(func() { processROM(gameName, sysid) })
		jsonSuccess(w, map[string]string{"status": "triggered", "source": "edgeemu"})
		return
	}

	// Online — dispatch by platform
	switch platform {
	case "digital", "xbla", "dlc", "xblig":
		launcher(func() { processDigital(gameName, platform) })
	case "games":
		launcher(func() { processGenericGame(gameName) })
	default: // xbox360, xbox
		launcher(func() { processGame(gameName, platform) })
	}
	jsonSuccess(w, map[string]string{"status": "triggered", "source": "internet_archive"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	gameName := r.URL.Query().Get("game")
	if gameName == "" {
		jsonError(w, 400, "Missing game parameter")
		return
	}
	status := GameStatus{State: "Missing", Message: "Not Found"}
	if s, exists := jobQueue.Load(gameName); exists {
		status = s.(GameStatus)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func handleQueue(w http.ResponseWriter, r *http.Request) {
	type JobEntry struct {
		Game    string `json:"game"`
		State   string `json:"state"`
		Message string `json:"message"`
	}
	var jobs []JobEntry
	jobQueue.Range(func(k, v interface{}) bool {
		gs := v.(GameStatus)
		jobs = append(jobs, JobEntry{
			Game:    k.(string),
			State:   gs.State,
			Message: gs.Message,
		})
		return true
	})
	// Sort: Processing first, then Ready, then Error, then others
	sort.Slice(jobs, func(i, j int) bool {
		order := map[string]int{"Processing": 0, "Ready": 1, "Error": 2}
		oi, iok := order[jobs[i].State]
		oj, jok := order[jobs[j].State]
		if !iok {
			oi = 3
		}
		if !jok {
			oj = 3
		}
		if oi != oj {
			return oi < oj
		}
		return jobs[i].Game < jobs[j].Game
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

// handleQueueRemove clears one job or the whole queue (POST /queue/remove?game=name or no game = all).
// Suppresses further logStatus from in-flight workers for removed games so the queue stays cleared.
func handleQueueRemove(w http.ResponseWriter, r *http.Request) {
	// POST for tools; GET supported for Aurora (Http.Get only on console).
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		jsonError(w, 405, "Use GET or POST /queue/remove?game=GameName (omit game to clear all)")
		return
	}
	game := strings.TrimSpace(r.URL.Query().Get("game"))
	if game == "" {
		var keys []string
		jobQueue.Range(func(k, _ interface{}) bool {
			keys = append(keys, k.(string))
			return true
		})
		for _, k := range keys {
			jobQueue.Delete(k)
			suppressedJobs.Store(k, struct{}{})
		}
		logf("QUEUE: cleared %d job(s)", len(keys))
		jsonSuccess(w, map[string]string{"status": "cleared", "count": fmt.Sprintf("%d", len(keys))})
		return
	}
	jobQueue.Delete(game)
	suppressedJobs.Store(game, struct{}{})
	logf("QUEUE: removed job %q", game)
	jsonSuccess(w, map[string]string{"status": "removed", "game": game})
}

func handleDebug(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h2>GODSend Debug v7.0-IA</h2><p>Server: %s:%s</p>", serverIP, Port)
	fmt.Fprintf(w, "<h3>Cache Status:</h3><ul>")
	buildStatesMu.Lock()
	for p, s := range buildStates {
		iaGameCacheMu.RLock()
		count := len(iaGameCache[p])
		iaGameCacheMu.RUnlock()
		fmt.Fprintf(w, "<li>%s: %s %d/%d (%d games)</li>",
			p, s.state, atomic.LoadInt32(&s.loaded), atomic.LoadInt32(&s.total), count)
	}
	buildStatesMu.Unlock()
	fmt.Fprintf(w, "</ul><h3>Transfer (Local ISOs):</h3><ul>")
	for _, g := range scanTransferFolder() {
		fmt.Fprintf(w, "<li>%s</li>", g)
	}
	fmt.Fprintf(w, "</ul><h3>Ready Games:</h3><ul>")
	if files, err := os.ReadDir(filepath.Join(toolsDir, "Ready")); err == nil {
		for _, f := range files {
			if f.IsDir() {
				fmt.Fprintf(w, "<li>%s</li>", f.Name())
			}
		}
	}
	fmt.Fprintf(w, "</ul><h3>Active Jobs:</h3><ul>")
	jobQueue.Range(func(k, v interface{}) bool {
		gs := v.(GameStatus)
		fmt.Fprintf(w, "<li>%s: [%s] %s</li>", k, gs.State, gs.Message)
		return true
	})
	fmt.Fprintf(w, "</ul><p><b>Queue:</b> GET or POST <code>/queue/remove?game=ExactName</code> to drop one job (omit <code>game</code> to clear all). Suppresses in-flight status updates until that game is triggered again.</p>")
	fmt.Fprintf(w, "<h3>Xbox Connections:</h3><ul>")
	xboxConnections.Range(func(k, v interface{}) bool {
		c := v.(XboxConnection)
		fmt.Fprintf(w, "<li>%s: IP=%s Mode=%s Drive=%s (%s ago)</li>",
			c.GameName, c.IP, c.Mode, c.Drive, time.Since(c.Timestamp).Round(time.Second))
		return true
	})
	fmt.Fprintf(w, "</ul>")
}

// ==========================================
// FILE SERVING
// ==========================================

func handleFileServe(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/files/")
	if relPath == "" {
		jsonError(w, 404, "No file path specified")
		return
	}
	decodedPath, err := url.QueryUnescape(relPath)
	if err != nil {
		jsonError(w, 400, "Invalid file path encoding")
		return
	}
	fullPath := filepath.Join(toolsDir, "Ready", decodedPath)

	absReady, _ := filepath.Abs(filepath.Join(toolsDir, "Ready"))
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absReady) {
		jsonError(w, 403, "Access denied")
		return
	}

	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		jsonError(w, 404, fmt.Sprintf("File not found: %s", filepath.Base(decodedPath)))
		return
	}
	if err != nil {
		jsonError(w, 500, "Cannot access file")
		return
	}

	if info.IsDir() {
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			jsonError(w, 500, "Cannot list directory")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><body><h2>Index of /%s</h2><ul>", relPath)
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				name += "/"
			}
			fmt.Fprintf(w, "<li><a href=\"%s\">%s</a></li>", url.PathEscape(name), name)
		}
		fmt.Fprintf(w, "</ul></body></html>")
		return
	}

	file, err := os.Open(fullPath)
	if err != nil {
		jsonError(w, 500, "Cannot open file")
		return
	}
	defer file.Close()

	fileSize := info.Size()
	fileName := filepath.Base(fullPath)
	adviseFadvise(file, fileSize)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	if rh := r.Header.Get("Range"); rh != "" {
		start, end, err := parseRangeHeader(rh, fileSize)
		if err != nil {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		cl := end - start + 1
		if _, err := file.Seek(start, 0); err != nil {
			jsonError(w, 500, "File seek error")
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
		w.Header().Set("Content-Length", strconv.FormatInt(cl, 10))
		w.WriteHeader(http.StatusPartialContent)

		startTime := time.Now()
		bw := bufio.NewWriterSize(w, ServeBufferSize)
		written, err := io.CopyN(bw, file, cl)
		if flushErr := bw.Flush(); flushErr != nil && err == nil {
			err = flushErr
		}
		elapsed := time.Since(startTime).Seconds()
		if elapsed < 0.001 {
			elapsed = 0.001
		}
		if err != nil {
			logf("FILE WARN: Range xfer interrupted %s after %.2f MB @ %.1f MB/s: %v",
				fileName, float64(written)/1048576, float64(written)/elapsed/1048576, err)
		}
		return
	}

	logf("FILE: Sending %s (%.2f MB)", fileName, float64(fileSize)/1048576)
	startTime := time.Now()
	http.ServeContent(w, r, fileName, info.ModTime(), file)
	elapsed := time.Since(startTime).Seconds()
	if elapsed < 0.001 {
		elapsed = 0.001
	}
	logf("FILE: Done %s (%.2f MB) in %.1fs @ %.1f MB/s",
		fileName, float64(fileSize)/1048576, elapsed, float64(fileSize)/elapsed/1048576)
}

func parseRangeHeader(header string, fileSize int64) (int64, int64, error) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, fmt.Errorf("not a byte range: %s", header)
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.HasPrefix(spec, "-") {
		s, err := strconv.ParseInt(spec[1:], 10, 64)
		if err != nil || s <= 0 {
			return 0, 0, fmt.Errorf("bad suffix: %s", spec)
		}
		start := fileSize - s
		if start < 0 {
			start = 0
		}
		return start, fileSize - 1, nil
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("bad format: %s", spec)
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bad start: %s", parts[0])
	}
	var end int64
	if parts[1] == "" {
		end = fileSize - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("bad end: %s", parts[1])
		}
	}
	if start < 0 || start >= fileSize {
		return 0, 0, fmt.Errorf("start %d out of range (size %d)", start, fileSize)
	}
	if end < start {
		return 0, 0, fmt.Errorf("end %d < start %d", end, start)
	}
	if end >= fileSize {
		end = fileSize - 1
	}
	return start, end, nil
}

func adviseFadvise(f *os.File, size int64) {}

// ==========================================
// FTP HELPERS
// ==========================================

func connectToXboxFTP(ip string) (*ftp.ServerConn, error) {
	logf("FTP: Connecting to %s:%d...", ip, FTPPort)
	c, err := ftp.Dial(fmt.Sprintf("%s:%d", ip, FTPPort),
		ftp.DialWithTimeout(FTPTimeout), ftp.DialWithDisabledEPSV(true), ftp.DialWithDisabledUTF8(true))
	if err != nil {
		return nil, fmt.Errorf("FTP connect to %s failed: %v", ip, err)
	}
	if err = c.Login("xboxftp", "xboxftp"); err != nil {
		c.Quit()
		return nil, fmt.Errorf("FTP login failed: %v", err)
	}
	logf("FTP: Connected to %s", ip)
	return c, nil
}

func connectWithRetry(ip string) (*ftp.ServerConn, error) {
	var last error
	for i := 1; i <= FTPMaxRetries; i++ {
		c, err := connectToXboxFTP(ip)
		if err == nil {
			return c, nil
		}
		last = err
		if i < FTPMaxRetries {
			logf("FTP: Attempt %d/%d failed, retry...", i, FTPMaxRetries)
			time.Sleep(FTPRetryDelay)
		}
	}
	return nil, fmt.Errorf("FTP failed after %d attempts: %v", FTPMaxRetries, last)
}

func ftpMkdirAll(conn *ftp.ServerConn, path string) {
	cur := ""
	for _, p := range strings.Split(strings.Trim(path, "/"), "/") {
		cur += "/" + p
		conn.MakeDir(cur)
	}
}

// ftpUploadFile uploads one file via FTP.
// fileNum/totalFiles (1-based) are shown in live progress; pass 0/0 when unknown.
// overallStart is the time the entire multi-file transfer batch began.
// hwm is a shared high-water mark for overall%; it is preserved across retries so
// that a reset reader never makes the displayed progress go backwards.
func ftpUploadFile(conn *ftp.ServerConn, localPath, remotePath, gameName string,
	transferred *int64, totalSize int64, fileNum, totalFiles int,
	overallStart time.Time, hwm *float64) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open %s: %v", filepath.Base(localPath), err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %v", filepath.Base(localPath), err)
	}
	now := time.Now()
	fileMB := float64(info.Size()) / 1048576
	logf("FTP [%d/%d] Starting: %s (%.1f MB)", fileNum, totalFiles, filepath.Base(localPath), fileMB)
	rdr := &ftpProgressReader{
		reader:       f,
		total:        info.Size(),
		gameName:     gameName,
		fileName:     filepath.Base(localPath),
		lastLog:      now,
		startTime:    now,
		overallStart: overallStart,
		transferred:  transferred,
		totalSize:    totalSize,
		fileNum:      fileNum,
		totalFiles:   totalFiles,
		hwm:          hwm,
	}
	if err = conn.Stor(remotePath, rdr); err != nil {
		return fmt.Errorf("STOR %s: %v", filepath.Base(localPath), err)
	}
	*transferred += info.Size()
	logf("FTP [%d/%d] Done:     %s (%.1f MB)", fileNum, totalFiles, filepath.Base(localPath), fileMB)
	return nil
}

func ftpUploadWithRetry(conn *ftp.ServerConn, xboxIP, localPath, remotePath, gameName string,
	transferred *int64, totalSize int64, fileNum, totalFiles int, overallStart time.Time) error {
	// hwm is allocated once per file and shared between the initial attempt and any
	// retries, ensuring the displayed overall% never goes backwards on reconnect.
	var hwm float64
	if err := ftpUploadFile(conn, localPath, remotePath, gameName, transferred, totalSize, fileNum, totalFiles, overallStart, &hwm); err == nil {
		return nil
	}
	logf("FTP [%d/%d] Upload failed — reconnecting and retrying: %s", fileNum, totalFiles, filepath.Base(localPath))
	nc, err := connectToXboxFTP(xboxIP)
	if err != nil {
		return fmt.Errorf("reconnect failed: %v", err)
	}
	defer nc.Quit()
	return ftpUploadFile(nc, localPath, remotePath, gameName, transferred, totalSize, fileNum, totalFiles, overallStart, &hwm)
}

type ftpProgressReader struct {
	reader             io.Reader
	total, written     int64
	gameName, fileName string
	lastLog            time.Time
	startTime          time.Time // when this individual file upload started (per-file speed)
	overallStart       time.Time // when the entire FTP transfer batch started (elapsed/ETA)
	transferred        *int64    // bytes completed by all previous files in this batch
	totalSize          int64     // total bytes across all files in this batch
	fileNum            int       // 1-based index of current file
	totalFiles         int       // total file count
	hwm                *float64  // shared high-water mark for overallPct (survives retries)
	maxFilePct         float64   // high-water mark for this file's individual percentage
}

func (r *ftpProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.written += int64(n)

	if time.Since(r.lastLog) > 2*time.Second {
		// Per-file progress — only ever increases
		rawFilePct := float64(r.written) / float64(r.total) * 100
		if rawFilePct > r.maxFilePct {
			r.maxFilePct = rawFilePct
		}

		// Overall progress — clamped via shared hwm so retries can't roll it back
		overallDone := *r.transferred + r.written
		rawOverallPct := float64(overallDone) / float64(r.totalSize) * 100
		if rawOverallPct > *r.hwm {
			*r.hwm = rawOverallPct
		}
		overallPct := *r.hwm // never decreases, even across retries

		overallMB := float64(overallDone) / 1048576
		totalMB := float64(r.totalSize) / 1048576

		// Speed based on this file's elapsed time (most accurate for current link)
		fileElapsed := time.Since(r.startTime).Seconds()
		if fileElapsed < 0.001 {
			fileElapsed = 0.001
		}
		speedMBs := float64(r.written) / fileElapsed / 1048576

		// Elapsed/ETA based on overall batch start
		overallElapsed := time.Since(r.overallStart).Seconds()
		if overallElapsed < 0.001 {
			overallElapsed = 0.001
		}
		elapsedStr := fmtDuration(overallElapsed)
		var etaStr string
		if speedMBs > 0 && overallPct < 100 {
			remainingBytes := r.totalSize - overallDone
			if remainingBytes < 0 {
				remainingBytes = 0
			}
			etaSecs := float64(remainingBytes) / (speedMBs * 1048576)
			etaStr = "~" + fmtDuration(etaSecs) + " left"
		} else {
			etaStr = "finishing"
		}

		logf("FTP [%d/%d] %s  file:%.1f%%  overall:%.1f%% (%.0f/%.0f MB)  @ %.1f MB/s  %s  %s",
			r.fileNum, r.totalFiles, r.fileName,
			r.maxFilePct, overallPct, overallMB, totalMB,
			speedMBs, elapsedStr, etaStr)

		if r.fileNum > 0 {
			logStatus(r.gameName, "Processing",
				fmt.Sprintf("FTP: %d/%d (%.1f%%) @ %.1f MB/s | %s | %s",
					r.fileNum, r.totalFiles, overallPct, speedMBs, elapsedStr, etaStr))
		}
		r.lastLog = time.Now()
	}
	return n, err
}

// ==========================================
// LOCAL ISO PROCESSING
// ==========================================

func processLocalISO(gameName, isoPath string) {
	logf("=== Local ISO: %s ===", gameName)
	safeName := sanitizeFilename(gameName)
	if safeName == "" {
		logStatus(gameName, "Error", "Invalid game name")
		return
	}
	var xboxConn *XboxConnection
	if c, ok := xboxConnections.Load(gameName); ok {
		cc := c.(XboxConnection)
		xboxConn = &cc
	}
	gameDir := filepath.Join(toolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	logStatus(gameName, "Processing", "Converting ISO to GOD...")
	godDir := filepath.Join(toolsDir, "Temp", safeName+"_GOD")
	os.MkdirAll(godDir, 0755)
	if err := runIso2God(isoPath, godDir); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("GOD convert: %v", err))
		os.RemoveAll(godDir)
		return
	}
	titleID, mediaID, err := detectGodStructure(godDir)
	if err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("GOD detect: %v", err))
		os.RemoveAll(godDir)
		return
	}
	logf("Local ISO: TitleID=%s MediaID=%s", titleID, mediaID)
	finalizeGOD(gameName, safeName, gameDir, godDir, titleID, mediaID, xboxConn)

	// Delete the source ISO from Transfer/ once the job completes successfully.
	// finalizeGOD sets state to "Ready" on both FTP and HTTP success paths.
	if gs, ok := jobQueue.Load(gameName); ok && gs.(GameStatus).State == "Ready" {
		if err := os.Remove(isoPath); err == nil {
			logf("Cleanup: deleted source ISO: %s", filepath.Base(isoPath))
		} else {
			logf("Cleanup WARN: could not delete source ISO %s: %v", filepath.Base(isoPath), err)
		}
	}
}

// ==========================================
// ONLINE ISO PROCESSING (Redump)
// ==========================================

func processGame(gameName, platform string) {
	logf("=== Online ISO: %s (%s) ===", gameName, platform)
	safeName := sanitizeFilename(gameName)
	if safeName == "" {
		logStatus(gameName, "Error", "Invalid game name")
		return
	}
	var xboxConn *XboxConnection
	if c, ok := xboxConnections.Load(gameName); ok {
		cc := c.(XboxConnection)
		xboxConn = &cc
	}
	gameDir := filepath.Join(toolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	logStatus(gameName, "Processing", "Searching Internet Archive...")
	entry, err := findIAEntry(gameName, platform)
	if err != nil {
		logf("ERROR [%s]: IA search failed: %v", gameName, err)
		logStatus(gameName, "Error", err.Error())
		return
	}
	downloadURL := IADownloadBase + entry.CollectionID + "/" + url.PathEscape(entry.FileName)
	logf("IA Download: %s → %s", gameName, entry.FileName)

	archivePath := filepath.Join(toolsDir, "Temp", safeName+filepath.Ext(entry.FileName))
	logStatus(gameName, "Processing", "Downloading from Internet Archive...")
	if err := downloadWithProgress(downloadURL, archivePath, gameName, IADownloadBase); err != nil {
		logf("ERROR [%s]: IA download failed: %v", gameName, err)
		logStatus(gameName, "Error", fmt.Sprintf("Download: %v", err))
		return
	}

	logStatus(gameName, "Processing", "Extracting ISO...")
	isoPath, err := extractISO(archivePath, safeName)
	os.Remove(archivePath)
	if err != nil {
		logf("ERROR [%s]: Extract failed: %v", gameName, err)
		logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	logStatus(gameName, "Processing", "Converting to GOD...")
	godDir := filepath.Join(toolsDir, "Temp", safeName+"_GOD")
	os.MkdirAll(godDir, 0755)
	if err := runIso2God(isoPath, godDir); err != nil {
		logf("ERROR [%s]: iso2god failed: %v", gameName, err)
		logStatus(gameName, "Error", fmt.Sprintf("GOD convert: %v", err))
		os.Remove(isoPath)
		os.RemoveAll(godDir)
		return
	}
	os.Remove(isoPath)

	titleID, mediaID, err := detectGodStructure(godDir)
	if err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("GOD detect: %v", err))
		os.RemoveAll(godDir)
		return
	}
	logf("Online ISO: TitleID=%s MediaID=%s", titleID, mediaID)
	finalizeGOD(gameName, safeName, gameDir, godDir, titleID, mediaID, xboxConn)
}

// finalizeGOD handles the FTP vs HTTP packaging step shared by local and online ISO flows.
func finalizeGOD(gameName, safeName, gameDir, godDir, titleID, mediaID string, xboxConn *XboxConnection) {
	// Resolve the real title name from XboxUnity once — used for both FTP and HTTP paths.
	logStatus(gameName, "Processing", "Looking up title name...")
	resolvedName := lookupTitleName(titleID) // may be empty — callers fall back gracefully

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		logStatus(gameName, "Processing", "FTP Transfer starting...")
		if err := ftpTransferGame(godDir, xboxConn, gameName, titleID, mediaID, resolvedName); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("FTP: %v", err))
			os.RemoveAll(godDir)
			os.RemoveAll(gameDir) // always empty in FTP mode
			return
		}
		os.RemoveAll(godDir)
		os.RemoveAll(gameDir) // always empty in FTP mode — files went straight to Xbox
		logStatus(gameName, "Ready", "FTP Transfer Complete!")
	} else {
		logStatus(gameName, "Processing", "Archiving for HTTP transfer...")
		titleID, mediaID, err := bucketAndZip(godDir, gameDir, gameName, safeName)
		if err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("Archive: %v", err))
			os.RemoveAll(godDir)
			return
		}
		os.RemoveAll(godDir)
		updateGameINI_Parts(gameDir, gameName, titleID, mediaID, resolvedName, nil)
		logStatus(gameName, "Ready", "Ready to Install")
	}
	logf("=== Complete: %s ===", gameName)
}

// ==========================================
// GENERIC GAME PROCESSING (XBOX_360_* collections)
// Handles: zip/rar → ISO (iso2god) OR XEX folder
// ==========================================

func processGenericGame(gameName string) {
	logf("=== Generic Game: %s ===", gameName)
	safeName := sanitizeFilename(gameName)
	if safeName == "" {
		logStatus(gameName, "Error", "Invalid game name")
		return
	}
	var xboxConn *XboxConnection
	if c, ok := xboxConnections.Load(gameName); ok {
		cc := c.(XboxConnection)
		xboxConn = &cc
	}
	gameDir := filepath.Join(toolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	logStatus(gameName, "Processing", "Searching Internet Archive (Games)...")
	entry, err := findIAEntry(gameName, "games")
	if err != nil {
		logf("ERROR [%s]: IA search failed: %v", gameName, err)
		logStatus(gameName, "Error", err.Error())
		return
	}
	downloadURL := IADownloadBase + entry.CollectionID + "/" + url.PathEscape(entry.FileName)
	logf("IA Download: %s → %s", gameName, entry.FileName)

	archivePath := filepath.Join(toolsDir, "Temp", safeName+filepath.Ext(entry.FileName))
	logStatus(gameName, "Processing", "Downloading from Internet Archive...")
	if err := downloadWithProgress(downloadURL, archivePath, gameName, IADownloadBase); err != nil {
		logf("ERROR [%s]: IA download failed: %v", gameName, err)
		logStatus(gameName, "Error", fmt.Sprintf("Download: %v", err))
		return
	}
	defer os.Remove(archivePath)

	// Extract the archive (handles zip, rar, 7z)
	logStatus(gameName, "Processing", "Extracting archive...")
	extDir := filepath.Join(toolsDir, "Temp", safeName+"_ext")
	os.RemoveAll(extDir)
	defer os.RemoveAll(extDir)
	if err := extractArchive(archivePath, extDir); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	// Detect content type
	isoPath := findFileByExt(extDir, ".iso")
	xexFolder := findXEXFolder(extDir)

	switch {
	case isoPath != "":
		// ISO found → standard iso2god pipeline
		logStatus(gameName, "Processing", "ISO detected, converting to GOD...")
		godDir := filepath.Join(toolsDir, "Temp", safeName+"_GOD")
		os.MkdirAll(godDir, 0755)
		if err := runIso2God(isoPath, godDir); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("GOD convert: %v", err))
			os.RemoveAll(godDir)
			return
		}
		titleID, mediaID, err := detectGodStructure(godDir)
		if err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("GOD detect: %v", err))
			os.RemoveAll(godDir)
			return
		}
		finalizeGOD(gameName, safeName, gameDir, godDir, titleID, mediaID, xboxConn)

	case xexFolder != "":
		// XEX folder found → zip and serve as type=xex
		folderName := filepath.Base(xexFolder)
		logStatus(gameName, "Processing", fmt.Sprintf("XEX folder detected: %s", folderName))
		if xboxConn != nil && xboxConn.Mode == "ftp" {
			if err := ftpTransferXEX(xexFolder, folderName, xboxConn, gameName); err != nil {
				logStatus(gameName, "Error", fmt.Sprintf("FTP XEX: %v", err))
			} else {
				os.RemoveAll(gameDir) // always empty in FTP mode
				logStatus(gameName, "Ready", "FTP Transfer Complete!")
			}
		} else {
			// Package the XEX folder contents as a 7z archive
			partName := fmt.Sprintf("%s_Part1.7z", safeName)
			if err := createZipFromDir(xexFolder, filepath.Join(gameDir, partName)); err != nil {
				logStatus(gameName, "Error", fmt.Sprintf("Archive XEX: %v", err))
				return
			}
			gamePartsMap.Store(gameName, []string{partName})
			updateGameINI_XEX(gameDir, gameName, folderName, partName)
			logStatus(gameName, "Ready", "Ready to Install")
		}

	default:
		logStatus(gameName, "Error", "No ISO or XEX content found in archive")
	}
	logf("=== Complete (Generic): %s ===", gameName)
}

// findFileByExt walks dir and returns the first file with the given extension.
func findFileByExt(dir, ext string) string {
	var found string
	filepath.Walk(dir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(p), ext) {
			found = p
			return io.EOF // stop walking
		}
		return nil
	})
	return found
}

// findXEXFolder walks dir and returns the path of the folder directly
// containing a default.xex file.
func findXEXFolder(dir string) string {
	var xexFolder string
	filepath.Walk(dir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Base(p), "default.xex") {
			xexFolder = filepath.Dir(p)
			return io.EOF
		}
		return nil
	})
	return xexFolder
}

// ftpTransferXEX uploads the contents of a XEX folder to /<drive>/XEX/<folderName>/
func ftpTransferXEX(xexFolder, folderName string, conn *XboxConnection, gameName string) error {
	fc, err := connectWithRetry(conn.IP)
	if err != nil {
		return err
	}
	defer fc.Quit()

	drive := strings.TrimSuffix(conn.Drive, ":")
	base := fmt.Sprintf("/%s/XEX/%s", drive, folderName)
	logf("FTP XEX Dest: %s", base)
	ftpMkdirAll(fc, base)

	var totalSize int64
	var totalFiles int
	filepath.Walk(xexFolder, func(p string, i os.FileInfo, e error) error {
		if e == nil && !i.IsDir() {
			totalFiles++
			totalSize += i.Size()
		}
		return nil
	})

	var xferSize int64
	var xferred int
	xferStart := time.Now()
	return filepath.Walk(xexFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(xexFolder, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		remote := base + "/" + rel
		xferred++
		return ftpUploadWithRetry(fc, conn.IP, path, remote, gameName, &xferSize, totalSize, xferred, totalFiles, xferStart)
	})
}

// ==========================================
// DIGITAL / XBLA / DLC / XBLIG PROCESSING
// ==========================================

// processDigital handles digital content: XBLA, DLC, XBLIG (and the original No-Intro digital set).
// DLC/XBLIG always land on Hdd1; XBLA respects the user's drive selection.
func processDigital(gameName, platform string) {
	logf("=== Digital: %s (%s) ===", gameName, platform)
	safeName := sanitizeFilename(gameName)
	if safeName == "" {
		logStatus(gameName, "Error", "Invalid game name")
		return
	}
	var xboxConn *XboxConnection
	if c, ok := xboxConnections.Load(gameName); ok {
		cc := c.(XboxConnection)
		xboxConn = &cc
	}
	gameDir := filepath.Join(toolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	logStatus(gameName, "Processing", "Searching Internet Archive...")
	entry, err := findIAEntry(gameName, platform)
	if err != nil {
		logStatus(gameName, "Error", err.Error())
		return
	}
	downloadURL := IADownloadBase + entry.CollectionID + "/" + url.PathEscape(entry.FileName)

	archivePath := filepath.Join(toolsDir, "Temp", safeName+"_digi"+filepath.Ext(entry.FileName))
	if err := downloadWithProgress(downloadURL, archivePath, gameName, IADownloadBase); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Download: %v", err))
		return
	}
	defer os.Remove(archivePath)

	logStatus(gameName, "Processing", "Extracting...")
	extDir := filepath.Join(toolsDir, "Temp", safeName+"_ext")
	os.RemoveAll(extDir)
	defer os.RemoveAll(extDir)
	if err := extractArchive(archivePath, extDir); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	var contentFile, titleID, typeDir string
	filepath.Walk(extDir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() || i.Size() <= 1024*1024 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".txt" || ext == ".nfo" || ext == ".jpg" {
			return nil
		}
		tid, ct := parseXboxHeader(p)
		if tid != "" {
			contentFile = p
			titleID = tid
			typeDir = fmt.Sprintf("%08X", ct)
			return io.EOF
		}
		return nil
	})

	if contentFile == "" {
		logStatus(gameName, "Error", "No valid Xbox content found in archive")
		return
	}
	logf("Digital: TitleID=%s Type=%s", titleID, typeDir)
	finalName := filepath.Base(contentFile)

	// DLC/XBLIG always go to Hdd1; XBLA respects the user's drive selection.
	forcedDrive := ""
	switch platform {
	case "dlc", "xblig":
		forcedDrive = "Hdd1"
	}

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		drive := forcedDrive
		if drive == "" {
			drive = strings.TrimSuffix(xboxConn.Drive, ":")
		}
		base := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", drive, titleID, typeDir)
		fc, err := connectWithRetry(xboxConn.IP)
		if err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("FTP: %v", err))
			return
		}
		defer fc.Quit()
		ftpMkdirAll(fc, base)
		info, _ := os.Stat(contentFile)
		var xfer int64
		if err := ftpUploadFile(fc, contentFile, base+"/"+finalName, gameName, &xfer, info.Size(), 1, 1, time.Now(), new(float64)); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("FTP upload: %v", err))
		} else {
			os.RemoveAll(gameDir) // always empty in FTP mode
			logStatus(gameName, "Ready", "FTP Transfer Complete!")
		}
	} else {
		relPath := fmt.Sprintf("Content\\0000000000000000\\%s\\%s\\", titleID, typeDir)
		if err := copyFileBuffered(contentFile, filepath.Join(gameDir, finalName)); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("Copy: %v", err))
		} else {
			updateGameINI_Raw(gameDir, gameName, finalName, relPath, forcedDrive)
			logStatus(gameName, "Ready", "Ready to Install")
		}
	}
	logf("=== Complete (Digital): %s ===", gameName)
}

// ==========================================
// FTP TRANSFER (GOD)
// ==========================================

func ftpTransferGame(godDir string, conn *XboxConnection, gameName, titleID, mediaID, resolvedName string) error {
	fc, err := connectWithRetry(conn.IP)
	if err != nil {
		return err
	}
	defer fc.Quit()

	// Build folder name: "<ResolvedName> - <TitleID>" or fallback "Title - <TitleID>"
	folderID := resolvedName
	if folderID == "" {
		folderID = "Title"
	}
	folderID = sanitizeFilename(folderID)
	drive := strings.TrimSuffix(conn.Drive, ":")
	base := fmt.Sprintf("/%s/GOD/%s - %s/%s", drive, folderID, titleID, mediaID)
	logf("FTP GOD Dest: %s", base)
	ftpMkdirAll(fc, base)

	contentDir := filepath.Join(godDir, titleID, mediaID)
	if _, err := os.Stat(contentDir); os.IsNotExist(err) {
		return fmt.Errorf("GOD content not found: %s", contentDir)
	}

	var totalFiles int
	var totalSize int64
	filepath.Walk(contentDir, func(p string, i os.FileInfo, e error) error {
		if e == nil && !i.IsDir() {
			totalFiles++
			totalSize += i.Size()
		}
		return nil
	})
	if totalFiles == 0 {
		return fmt.Errorf("no files in GOD content")
	}
	logf("FTP GOD: %d files (%.2f GB)", totalFiles, float64(totalSize)/1073741824)

	var xferred int
	var xferSize int64
	xferStart := time.Now()
	return filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(contentDir, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		remote := base + "/" + rel
		if info.IsDir() {
			fc.MakeDir(remote)
			return nil
		}
		xferred++
		return ftpUploadWithRetry(fc, conn.IP, path, remote, gameName, &xferSize, totalSize, xferred, totalFiles, xferStart)
	})
}

// ==========================================
// INI MANAGEMENT
// ==========================================

func updateGameINI_Parts(gameDir, gameName, titleID, mediaID, resolvedName string, dlcList []string) {
	f, err := os.Create(filepath.Join(gameDir, "godsend.ini"))
	if err != nil {
		logf("INI ERROR: %v", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := func(s string) string {
		s = strings.ReplaceAll(s, " ", "%20")
		s = strings.ReplaceAll(s, "(", "%28")
		s = strings.ReplaceAll(s, ")", "%29")
		return s
	}
	raw, ok := gamePartsMap.Load(gameName)
	if !ok {
		logf("INI ERROR: no parts for %s", gameName)
		return
	}
	parts := raw.([]string)
	fmt.Fprintf(w, "[%s]\ntype=god\ntitleid=%s\nmediaid=%s\n", gameName, titleID, mediaID)
	if resolvedName != "" {
		fmt.Fprintf(w, "titlename=%s\n", resolvedName)
	}
	if len(parts) > 0 {
		fmt.Fprintf(w, "dataurl=%s\n", enc(parts[0]))
	}
	for i := 1; i < len(parts); i++ {
		fmt.Fprintf(w, "dataurlpart%d=%s\n", i+1, enc(parts[i]))
	}
	for i, d := range dlcList {
		fmt.Fprintf(w, "dlc_%d=%s\n", i+1, enc(d))
	}
	w.Flush()
}

// updateGameINI_Raw writes a manifest for digital/XBLA content.
// forcedDrive, if non-empty, tells the Lua to always install to that drive.
func updateGameINI_Raw(gameDir, gameName, fileName, relPath, forcedDrive string) {
	f, err := os.Create(filepath.Join(gameDir, "godsend.ini"))
	if err != nil {
		logf("INI ERROR: %v", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "[%s]\ntype=raw\nfilename=%s\npath=%s\n", gameName, fileName, relPath)
	if forcedDrive != "" {
		fmt.Fprintf(w, "drive=%s:\n", forcedDrive)
	}
	w.Flush()
}

// updateGameINI_XEX writes a manifest for XEX folder games.
func updateGameINI_XEX(gameDir, gameName, folderName, partFile string) {
	f, err := os.Create(filepath.Join(gameDir, "godsend.ini"))
	if err != nil {
		logf("INI ERROR: %v", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, " ", "%20"), "(", "%28"), ")", "%29")
	}
	fmt.Fprintf(w, "[%s]\ntype=xex\nfoldername=%s\ndataurl=%s\n",
		gameName, folderName, enc(partFile))
	w.Flush()
}

// ==========================================
// XBOXUNITY TITLE NAME LOOKUP
// API: http://xboxunity.net/Resources/Lib/TitleList.php?search=<TITLEID>
// Returns the real title name for a given TitleID hex string.
// Falls back to empty string on any error so callers degrade gracefully.
// ==========================================

func lookupTitleName(titleID string) string {
	apiURL := "http://xboxunity.net/Resources/Lib/TitleList.php?search=" + titleID
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		logf("XboxUnity: request build error: %v", err)
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		logf("XboxUnity: request failed for %s: %v", titleID, err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		logf("XboxUnity: HTTP %d for %s", resp.StatusCode, titleID)
		return ""
	}
	var result struct {
		Items []struct {
			Name string `json:"Name"`
		} `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logf("XboxUnity: JSON decode error for %s: %v", titleID, err)
		return ""
	}
	if len(result.Items) > 0 && result.Items[0].Name != "" {
		logf("XboxUnity: resolved %s → %s", titleID, result.Items[0].Name)
		return result.Items[0].Name
	}
	logf("XboxUnity: no result for %s", titleID)
	return ""
}

// godFolderName returns the directory name to use inside the GOD folder.
// Format: "<TitleName> - <TitleID>" if XboxUnity resolves the name,
// otherwise falls back to "Title - <TitleID>" to preserve old behaviour.
func godFolderName(titleID string) string {
	if name := lookupTitleName(titleID); name != "" {
		return sanitizeFilename(name) + " - " + titleID
	}
	return "Title - " + titleID
}

// ==========================================
// HELPERS
// ==========================================

func bucketAndZip(src, dest, gameName, safeName string) (string, string, error) {
	titleID, mediaID, err := detectGodStructure(src)
	if err != nil {
		return "", "", err
	}
	staging := filepath.Join(toolsDir, "Temp", safeName+"_staging")
	os.RemoveAll(staging)
	os.MkdirAll(staging, 0755)
	var parts []string
	var curSize int64
	pn := 1
	cpd := filepath.Join(staging, fmt.Sprintf("%s_Part%d", safeName, pn))
	os.MkdirAll(cpd, 0755)
	contentDir := filepath.Join(src, titleID, mediaID)
	err = filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(contentDir, path)
		if curSize+info.Size() > MaxPartSize && curSize > 0 {
			pname := fmt.Sprintf("%s_Part%d.7z", safeName, pn)
			if err := createZipFromDir(cpd, filepath.Join(dest, pname)); err != nil {
				return err
			}
			parts = append(parts, pname)
			pn++
			curSize = 0
			cpd = filepath.Join(staging, fmt.Sprintf("%s_Part%d", safeName, pn))
			os.MkdirAll(cpd, 0755)
		}
		dp := filepath.Join(cpd, rel)
		os.MkdirAll(filepath.Dir(dp), 0755)
		if err := copyFileBuffered(path, dp); err != nil {
			return err
		}
		curSize += info.Size()
		return nil
	})
	if err != nil {
		os.RemoveAll(staging)
		return "", "", err
	}
	if curSize > 0 {
		pname := fmt.Sprintf("%s_Part%d.7z", safeName, pn)
		if err := createZipFromDir(cpd, filepath.Join(dest, pname)); err != nil {
			os.RemoveAll(staging)
			return "", "", err
		}
		parts = append(parts, pname)
	}
	os.RemoveAll(staging)
	gamePartsMap.Store(gameName, parts)
	return titleID, mediaID, nil
}

func detectGodStructure(godDir string) (string, string, error) {
	entries, err := os.ReadDir(godDir)
	if err != nil {
		return "", "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subs, err := os.ReadDir(filepath.Join(godDir, e.Name()))
		if err != nil {
			continue
		}
		for _, s := range subs {
			if s.IsDir() {
				return e.Name(), s.Name(), nil
			}
		}
	}
	return "", "", fmt.Errorf("GOD structure not found")
}

func parseXboxHeader(path string) (string, uint32) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()
	h := make([]byte, 1024)
	n, err := f.Read(h)
	if err != nil || n < 0x368 {
		return "", 0
	}
	magic := string(h[0:4])
	if magic != "LIVE" && magic != "PIRS" && magic != "CON " {
		return "", 0
	}
	return strings.ToUpper(hex.EncodeToString(h[0x360:0x364])), binary.BigEndian.Uint32(h[0x344:0x348])
}

// downloadWithProgress downloads urlStr to dest, using parallel range-request workers
// for IA URLs when the server supports it. Falls back to a single stream otherwise.
func downloadWithProgress(urlStr, dest, name, ref string) error {
	isIA := strings.Contains(strings.ToLower(urlStr), "archive.org")
	if isIA && iaDownloadConcurrency > 1 {
		size, rangeOK, err := iaProbeDownload(urlStr, ref)
		if err != nil {
			logf("WARN [%s]: probe failed (%v), using single stream", name, err)
		} else if rangeOK && size >= iaParallelThreshold {
			logf("[%s] Parallel download: %d workers, %.0f MB", name, iaDownloadConcurrency, float64(size)/1048576)
			return iaDownloadParallel(urlStr, dest, name, ref, size, iaDownloadConcurrency)
		}
	}
	return iaDownloadSingle(urlStr, dest, name, ref)
}

// iaProbeDownload sends a HEAD request and returns (Content-Length, Accept-Ranges, error).
func iaProbeDownload(urlStr, ref string) (size int64, rangeOK bool, err error) {
	req, _ := http.NewRequest("HEAD", urlStr, nil)
	req.Header.Set("Referer", ref)
	applyArchiveOrgHeaders(req)
	resp, err := iaHTTPClient.Do(req)
	if err != nil {
		return 0, false, err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, false, fmt.Errorf("HEAD HTTP %d", resp.StatusCode)
	}
	size = resp.ContentLength
	rangeOK = strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes") && size > 0
	return size, rangeOK, nil
}

// iaDownloadParallel splits the file into `workers` equal chunks, downloads them
// concurrently with per-chunk retries, then joins them into dest.
func iaDownloadParallel(urlStr, dest, name, ref string, totalSize int64, workers int) error {
	chunkSize := (totalSize + int64(workers) - 1) / int64(workers)

	type chunkSpec struct {
		index int
		start int64
		end   int64
		path  string
	}
	chunks := make([]chunkSpec, 0, workers)
	for i := 0; i < workers; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		chunks = append(chunks, chunkSpec{
			index: i,
			start: start,
			end:   end,
			path:  dest + fmt.Sprintf(".part%d", i),
		})
	}

	// Shared atomic progress counter across all chunks.
	var written int64
	startTime := time.Now()

	// Progress reporter goroutine.
	// logStatus fires every 500 ms (feeds Lua); logf fires every 15 s (feeds Electron terminal).
	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		lastConsole := time.Time{}
		for {
			select {
			case <-progressDone:
				return
			case now := <-ticker.C:
				w := atomic.LoadInt64(&written)
				pct := float64(w) / float64(totalSize) * 100
				elapsed := now.Sub(startTime).Seconds()
				if elapsed < 0.001 {
					elapsed = 0.001
				}
				speedMBs := float64(w) / elapsed / 1048576
				wMB := float64(w) / 1048576
				tMB := float64(totalSize) / 1048576
				etaStr := "..."
				if speedMBs > 0 && pct < 100 {
					etaSecs := float64(totalSize-w) / (speedMBs * 1048576)
					etaStr = "~" + fmtDuration(etaSecs) + " left"
				}
				logStatus(name, "Processing",
					fmt.Sprintf("Downloading: %.0f%% (%.0f/%.0f MB) @ %.1f MB/s | %s | %dx",
						pct, wMB, tMB, speedMBs, etaStr, workers))
				if now.Sub(lastConsole) > 15*time.Second {
					logf("Download [%s]: %.1f%% (%.1f/%.1f MB) @ %.1f MB/s | %dx",
						name, pct, wMB, tMB, speedMBs, workers)
					lastConsole = now
				}
			}
		}
	}()

	// Download all chunks concurrently.
	var wg sync.WaitGroup
	errs := make([]error, len(chunks))
	for i, c := range chunks {
		wg.Add(1)
		go func(idx int, spec chunkSpec) {
			defer wg.Done()
			errs[idx] = iaDownloadChunk(urlStr, spec.path, ref, spec.start, spec.end, &written)
		}(i, c)
	}
	wg.Wait()
	close(progressDone)

	// Check chunk errors — clean up part files on any failure.
	for i, e := range errs {
		if e != nil {
			for _, c := range chunks {
				os.Remove(c.path)
			}
			return fmt.Errorf("chunk %d/%d failed: %w", i+1, len(chunks), e)
		}
	}

	// Join parts into the final destination file.
	logf("[%s] Joining %d parts...", name, len(chunks))
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	bw := bufio.NewWriterSize(out, CopyBufferSize)
	joinErr := func() error {
		for _, c := range chunks {
			f, err := os.Open(c.path)
			if err != nil {
				return fmt.Errorf("open part %d: %w", c.index, err)
			}
			_, err = io.Copy(bw, f)
			f.Close()
			os.Remove(c.path)
			if err != nil {
				return fmt.Errorf("join part %d: %w", c.index, err)
			}
		}
		return bw.Flush()
	}()
	out.Close()
	if joinErr != nil {
		os.Remove(dest)
		return fmt.Errorf("join: %w", joinErr)
	}
	return nil
}

// iaDownloadChunk downloads the byte range [start, end] for one parallel worker,
// writing to destPath using WriteAt (position-independent, safe to retry).
// writtenAtomic is updated in real-time; on retry any partial count is rolled back.
func iaDownloadChunk(urlStr, destPath, ref string, start, end int64, writtenAtomic *int64) error {
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create part: %w", err)
	}
	defer f.Close()

	var lastErr error
	for attempt := 0; attempt <= iaChunkRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * iaChunkRetryBase
			logf("RETRY chunk bytes=%d-%d (attempt %d/%d): %v — waiting %s",
				start, end, attempt, iaChunkRetries, lastErr, wait)
			time.Sleep(wait)
		}

		req, err := http.NewRequest("GET", urlStr, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		req.Header.Set("Referer", ref)
		applyArchiveOrgHeaders(req)

		resp, err := iaHTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request: %w", err)
			continue
		}
		if resp.StatusCode != 206 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d (expected 206 Partial Content)", resp.StatusCode)
			continue
		}

		var chunkWritten int64
		buf := make([]byte, 256*1024)
		var readErr error
		for {
			var n int
			n, readErr = resp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := f.WriteAt(buf[:n], chunkWritten); writeErr != nil {
					resp.Body.Close()
					atomic.AddInt64(writtenAtomic, -chunkWritten)
					lastErr = fmt.Errorf("write at +%d: %w", chunkWritten, writeErr)
					chunkWritten = 0
					goto nextAttempt
				}
				atomic.AddInt64(writtenAtomic, int64(n))
				chunkWritten += int64(n)
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				break
			}
		}
		resp.Body.Close()
		if readErr != nil && readErr != io.EOF {
			atomic.AddInt64(writtenAtomic, -chunkWritten)
			lastErr = fmt.Errorf("read after %d bytes: %w", chunkWritten, readErr)
			continue
		}
		return nil
	nextAttempt:
	}
	return lastErr
}

// iaDownloadSingle is a single-stream download with up to iaChunkRetries retries.
func iaDownloadSingle(urlStr, dest, name, ref string) error {
	isIA := strings.Contains(strings.ToLower(urlStr), "archive.org")
	var lastErr error
	for attempt := 0; attempt <= iaChunkRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * iaChunkRetryBase
			logf("RETRY download [%s] (attempt %d/%d): %v — waiting %s",
				name, attempt, iaChunkRetries, lastErr, wait)
			time.Sleep(wait)
		}
		lastErr = iaDownloadSingleAttempt(urlStr, dest, name, ref, isIA)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func iaDownloadSingleAttempt(urlStr, dest, name, ref string, isIA bool) error {
	client := iaHTTPClient
	if !isIA {
		client = &http.Client{Timeout: 0}
	}
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", ref)
	if isIA {
		applyArchiveOrgHeaders(req)
	} else {
		req.Header.Set("User-Agent", "Mozilla/5.0")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, urlStr)
	}
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()
	bw := bufio.NewWriterSize(out, CopyBufferSize)
	pw := &ProgressWriter{Total: resp.ContentLength, GameName: name, LastLog: time.Now(), StartTime: time.Now()}
	written, err := io.Copy(bw, io.TeeReader(resp.Body, pw))
	if err != nil {
		return fmt.Errorf("interrupted after %.2f MB: %w", float64(written)/1048576, err)
	}
	bw.Flush()
	if resp.ContentLength > 0 && written != resp.ContentLength {
		logf("WARN: Size mismatch %s: expected %d got %d", name, resp.ContentLength, written)
	}
	return nil
}

// extractArchive extracts any 7-Zip-supported archive (zip, rar, 7z) to destDir.
func extractArchive(archivePath, destDir string) error {
	os.MkdirAll(destDir, 0755)
	out, err := exec.Command(
		filepath.Join(toolsDir, sevenZipBin),
		"x", archivePath, "-o"+destDir, "-y",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("7z: %v | %s", err, string(out))
	}
	return nil
}

// extractISO extracts only the ISO file(s) from an archive.
func extractISO(archivePath, safeName string) (string, error) {
	dest := filepath.Join(toolsDir, "Temp", safeName+"_extracted")
	os.RemoveAll(dest)
	out, err := exec.Command(
		filepath.Join(toolsDir, sevenZipBin),
		"x", archivePath, "-o"+dest, "*.iso", "-r", "-y",
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("7z: %v | %s", err, string(out))
	}
	iso := findFileByExt(dest, ".iso")
	if iso == "" {
		return "", fmt.Errorf("no ISO found in archive")
	}
	return iso, nil
}

func runIso2God(iso, out string) error {
	o, err := exec.Command(filepath.Join(toolsDir, isoGodBin), iso, out).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iso2god: %v | %s", err, string(o))
	}
	return nil
}

func createZipFromDir(dir, out string) error {
	cmd := exec.Command(filepath.Join(toolsDir, sevenZipBin), "a", "-t7z", "-mx0", out, "*")
	cmd.Dir = dir
	o, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("7z: %v | %s", err, string(o))
	}
	return nil
}

func copyFileBuffered(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	bw := bufio.NewWriterSize(out, CopyBufferSize)
	if _, err = io.Copy(bw, bufio.NewReaderSize(in, CopyBufferSize)); err != nil {
		return err
	}
	return bw.Flush()
}

func getOutboundIP() string {
	c, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.String()
	}
	return ""
}

func sanitizeFilename(n string) string {
	if n == "" {
		return ""
	}
	return regexp.MustCompile(`[<>:"/\\|?*]`).ReplaceAllString(n, " -")
}

// ==========================================
// ROM — CACHE (EdgeEmu scraping)
// ==========================================

// buildROMGameCache fetches and caches the game list for one ROM system from edgeemu.net.
// Uses the same build-state infrastructure as IA caches, keyed by "rom_"+sysid.
func buildROMGameCache(sysid string) {
	platform := "rom_" + sysid
	iaCacheBuildMu.Lock()
	if iaCacheBuilding[platform] {
		iaCacheBuildMu.Unlock()
		return
	}
	iaCacheBuilding[platform] = true
	iaCacheBuildMu.Unlock()
	defer func() {
		iaCacheBuildMu.Lock()
		iaCacheBuilding[platform] = false
		iaCacheBuildMu.Unlock()
	}()

	sys, ok := romSystems[sysid]
	if !ok {
		return
	}
	setBuildState(platform, "building", 0, 1)
	logf("ROM CACHE: Building %s (%s)...", sysid, sys.Name)

	names, urlMap, err := fetchEdgeEmuGames(sys.BrowseURL)
	if err != nil {
		setBuildState(platform, "error", 0, 1)
		logf("ROM CACHE ERROR [%s]: %v", sysid, err)
		return
	}

	romGameCacheMu.Lock()
	romGameCache[sysid] = names
	romGameCacheMu.Unlock()

	romURLMapMu.Lock()
	for lower, dlURL := range urlMap {
		romURLMap[sysid+"\x00"+lower] = dlURL
	}
	romURLMapMu.Unlock()

	setBuildState(platform, "ready", 1, 1)
	logf("ROM CACHE: %s complete — %d games", sysid, len(names))

	// Persist using the existing PlatformCache format.
	// IAGameEntry.CollectionID = sysid, IAGameEntry.FileName = download URL.
	entries := map[string]IAGameEntry{}
	for lower, dlURL := range urlMap {
		entries[lower] = IAGameEntry{CollectionID: sysid, FileName: dlURL}
	}
	saveCacheToDisk(platform, names, entries)
}

// loadROMCacheFromDisk loads a previously scraped edgeemu game list.
func loadROMCacheFromDisk(sysid string) bool {
	platform := "rom_" + sysid
	data, err := os.ReadFile(cacheFilePath(platform))
	if err != nil {
		return false
	}
	var pc PlatformCache
	if err := json.Unmarshal(data, &pc); err != nil || len(pc.Games) == 0 {
		return false
	}

	romGameCacheMu.Lock()
	romGameCache[sysid] = pc.Games
	romGameCacheMu.Unlock()

	romURLMapMu.Lock()
	for lower, entry := range pc.GameEntries {
		// entry.FileName stores the download URL for ROM caches
		romURLMap[sysid+"\x00"+lower] = entry.FileName
	}
	romURLMapMu.Unlock()

	setBuildState(platform, "ready", 1, 1)
	return true
}

// fetchEdgeEmuGames scrapes an edgeemu.net browse page and returns all game names
// and their direct ZIP download URLs.
//
// edgeemu page structure (as of 2025):
//   - The base browse URL shows only ~10 random ROMs — not the full list.
//   - Full listings require ?alpha= letter pagination (A-Z plus "0" for numbers).
//   - Download links look like: href="/download/nintendo-gameboy-advance/Game%20Name.zip"
//   - The anchor text is the literal word "download", so game names are parsed
//     from the URL filename rather than the link text.
func fetchEdgeEmuGames(browseURL string) ([]string, map[string]string, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	urlMap := map[string]string{}
	var allNames []string
	seen := map[string]bool{}

	// Matches href="/download/[system]/[encoded-filename].zip"
	// Capture group 1 = full path, group 2 = encoded filename (with .zip)
	reDownload := regexp.MustCompile(`(?i)href="(/download/[^/"]+/([^"]+\.zip))"`)

	parsePage := func(pageURL string) int {
		req, err := http.NewRequest("GET", pageURL, nil)
		if err != nil {
			return 0
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := client.Do(req)
		if err != nil {
			return 0
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return 0
		}
		count := 0
		for _, m := range reDownload.FindAllStringSubmatch(string(body), -1) {
			fullPath := m[1]    // e.g. /download/nintendo-gameboy-advance/Game%20Name%20(USA).zip
			encoded := m[2]    // e.g. Game%20Name%20(USA).zip

			// URL-decode the filename and strip .zip to get the display name
			decoded, err := url.QueryUnescape(strings.ReplaceAll(encoded, "+", "%2B"))
			if err != nil {
				decoded = encoded
			}
			name := strings.TrimSuffix(decoded, ".zip")
			name = strings.TrimSuffix(name, ".ZIP")
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}

			lower := strings.ToLower(name)
			if !seen[lower] {
				seen[lower] = true
				urlMap[lower] = "https://edgeemu.net" + fullPath
				allNames = append(allNames, name)
				count++
			}
		}
		return count
	}

	// edgeemu letter pages use path segments: {browseURL}/a, {browseURL}/b, ...
	// Numbers are grouped under {browseURL}/0-9
	// The base page only shows ~10 random ROMs — always paginate all letters.
	base := strings.TrimRight(browseURL, "/")
	letters := []string{
		"0-9",
		"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m",
		"n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z",
	}
	for _, l := range letters {
		parsePage(base + "/" + l)
		time.Sleep(150 * time.Millisecond) // be polite to edgeemu.net
	}

	sort.Strings(allNames)
	if len(allNames) == 0 {
		return nil, nil, fmt.Errorf("no games found at %s", browseURL)
	}
	return allNames, urlMap, nil
}

// findROMDownloadURL looks up the cached download URL for a ROM, with fuzzy fallback.
func findROMDownloadURL(gameName, sysid string) string {
	lower := strings.ToLower(gameName)
	key := sysid + "\x00" + lower

	romURLMapMu.RLock()
	u, ok := romURLMap[key]
	romURLMapMu.RUnlock()
	if ok {
		return u
	}

	// Fuzzy: strip region tag "(USA)" etc. and try partial match
	baseSearch := strings.ToLower(strings.Split(gameName, " (")[0])
	prefix := sysid + "\x00"
	romURLMapMu.RLock()
	defer romURLMapMu.RUnlock()
	for k, v := range romURLMap {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		kGame := k[len(prefix):]
		kBase := strings.Split(kGame, " (")[0]
		if kGame == lower || strings.Contains(kGame, lower) || kBase == baseSearch {
			return v
		}
	}
	return ""
}

// ==========================================
// ROM — PROCESSING
// ==========================================

// processROM downloads a ROM from edgeemu.net using parallel range requests,
// extracts it, then delivers it via FTP or HTTP.
func processROM(gameName, sysid string) {
	logf("=== ROM: %s (%s) ===", gameName, sysid)
	sys, ok := romSystems[sysid]
	if !ok {
		logStatus(gameName, "Error", "Unknown ROM system: "+sysid)
		return
	}
	safeName := sanitizeFilename(gameName)
	if safeName == "" {
		logStatus(gameName, "Error", "Invalid game name")
		return
	}

	var xboxConn *XboxConnection
	if c, ok := xboxConnections.Load(gameName); ok {
		cc := c.(XboxConnection)
		xboxConn = &cc
	}
	gameDir := filepath.Join(toolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	// Resolve download URL from cache
	logStatus(gameName, "Processing", "Looking up ROM on EdgeEmu...")
	downloadURL := findROMDownloadURL(gameName, sysid)
	if downloadURL == "" {
		// Cache might be cold — try building it now and retry
		buildROMGameCache(sysid)
		downloadURL = findROMDownloadURL(gameName, sysid)
	}
	if downloadURL == "" {
		logStatus(gameName, "Error", "ROM not found: "+gameName)
		return
	}
	logf("ROM Download: %s → %s", gameName, downloadURL)

	// Download the ZIP using parallel range requests
	zipPath := filepath.Join(toolsDir, "Temp", safeName+"_rom.zip")
	logStatus(gameName, "Processing", "Downloading from EdgeEmu...")
	if err := downloadEdgeEmuWithProgress(downloadURL, zipPath, gameName); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Download: %v", err))
		os.Remove(zipPath)
		return
	}
	defer os.Remove(zipPath)

	// Extract ZIP
	logStatus(gameName, "Processing", "Extracting ROM...")
	extDir := filepath.Join(toolsDir, "Temp", safeName+"_rom_ext")
	os.RemoveAll(extDir)
	defer os.RemoveAll(extDir)
	if err := extractArchive(zipPath, extDir); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	// Find the ROM file
	romFiles := findROMFiles(extDir)
	if len(romFiles) == 0 {
		logStatus(gameName, "Error", "No ROM file found after extraction")
		return
	}
	romFile := romFiles[0]
	romFileName := filepath.Base(romFile)

	// Xbox install path: [Drive]\[romRootPath]\[SystemFolder]\
	xboxROMPath := romRootPath + "\\" + sys.Folder + "\\"

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		logStatus(gameName, "Processing", "FTP transfer starting...")
		drive := strings.TrimSuffix(xboxConn.Drive, ":")
		remotePath := "/" + drive + "/" + strings.ReplaceAll(xboxROMPath, "\\", "/")

		fc, err := connectWithRetry(xboxConn.IP)
		if err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("FTP: %v", err))
			return
		}
		defer fc.Quit()
		ftpMkdirAll(fc, strings.TrimSuffix(remotePath, "/"))

		info, _ := os.Stat(romFile)
		var xfer int64
		if err := ftpUploadFile(fc, romFile, remotePath+romFileName, gameName,
			&xfer, info.Size(), 1, 1, time.Now(), new(float64)); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("FTP upload: %v", err))
		} else {
			os.RemoveAll(gameDir)
			logStatus(gameName, "Ready", "FTP Transfer Complete!")
		}
	} else {
		// HTTP mode: compress ROM to .7z and serve from Ready/
		logStatus(gameName, "Processing", "Archiving for HTTP transfer...")
		archiveName := safeName + ".7z"
		archiveDest := filepath.Join(gameDir, archiveName)
		if err := compressROMFile(romFile, archiveDest, gameName); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("Compress: %v", err))
			return
		}
		updateGameINI_ROM(gameDir, gameName, archiveName, xboxROMPath)
		logStatus(gameName, "Ready", "Ready to Install")
	}
	logf("=== Complete (ROM): %s ===", gameName)
}

// findROMFiles walks a directory and returns all non-metadata files (the actual ROMs).
func findROMFiles(dir string) []string {
	skipExts := map[string]bool{
		".txt": true, ".nfo": true, ".jpg": true, ".jpeg": true,
		".png": true, ".xml": true, ".dat": true, ".md": true,
	}
	var files []string
	filepath.Walk(dir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() || i.Size() == 0 {
			return nil
		}
		if !skipExts[strings.ToLower(filepath.Ext(p))] {
			files = append(files, p)
		}
		return nil
	})
	return files
}

// compressROMFile compresses a single ROM file to a .7z archive.
// Runs 7z from the ROM's directory so the archive contains only the filename (no path).
func compressROMFile(romFile, destArchive, gameName string) error {
	logStatus(gameName, "Processing", "Compressing ROM...")
	cmd := exec.Command(
		filepath.Join(toolsDir, sevenZipBin),
		"a", "-mx=1", "-mmt=on", destArchive, filepath.Base(romFile),
	)
	cmd.Dir = filepath.Dir(romFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("7z: %v: %s", err, string(out))
	}
	return nil
}

// updateGameINI_ROM writes a godsend.ini manifest for a ROM install.
// romPath is the drive-relative path (e.g. "Emulators\RetroArch\roms\NES\").
func updateGameINI_ROM(gameDir, gameName, archiveName, romPath string) {
	f, err := os.Create(filepath.Join(gameDir, "godsend.ini"))
	if err != nil {
		logf("INI ERROR: %v", err)
		return
	}
	defer f.Close()
	enc := func(s string) string {
		s = strings.ReplaceAll(s, " ", "%20")
		s = strings.ReplaceAll(s, "(", "%28")
		s = strings.ReplaceAll(s, ")", "%29")
		return s
	}
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "[%s]\ntype=rom\ndataurl=%s\nrompath=%s\n", gameName, enc(archiveName), romPath)
	w.Flush()
}

// ==========================================
// ROM — PARALLEL DOWNLOAD (no IA auth)
// ==========================================

// downloadEdgeEmuWithProgress downloads from edgeemu.net using parallel range
// requests when supported, falling back to single-stream otherwise.
func downloadEdgeEmuWithProgress(urlStr, dest, name string) error {
	req, err := http.NewRequest("HEAD", urlStr, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0")
		if resp, err := edgeEmuHTTPClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				size := resp.ContentLength
				rangeOK := strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes") && size > 0
				if rangeOK && size >= iaParallelThreshold && iaDownloadConcurrency > 1 {
					logf("[%s] ROM parallel: %d workers, %.0f MB", name, iaDownloadConcurrency, float64(size)/1048576)
					return downloadEdgeEmuParallel(urlStr, dest, name, size, iaDownloadConcurrency)
				}
			}
		}
	}
	return downloadEdgeEmuSingle(urlStr, dest, name)
}

// downloadEdgeEmuSingle is a retrying single-stream download for edgeemu.net.
func downloadEdgeEmuSingle(urlStr, dest, name string) error {
	var lastErr error
	for attempt := 0; attempt <= iaChunkRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * iaChunkRetryBase
			logf("RETRY ROM [%s] attempt %d: %v — waiting %s", name, attempt, lastErr, wait)
			time.Sleep(wait)
		}
		req, err := http.NewRequest("GET", urlStr, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := edgeEmuHTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request: %w", err)
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		out, err := os.Create(dest)
		if err != nil {
			resp.Body.Close()
			return err
		}
		bw := bufio.NewWriterSize(out, CopyBufferSize)
		pw := &ProgressWriter{Total: resp.ContentLength, GameName: name, LastLog: time.Now(), StartTime: time.Now()}
		written, err := io.Copy(bw, io.TeeReader(resp.Body, pw))
		resp.Body.Close()
		bw.Flush()
		out.Close()
		if err != nil {
			os.Remove(dest)
			lastErr = fmt.Errorf("interrupted after %.2f MB: %w", float64(written)/1048576, err)
			continue
		}
		return nil
	}
	return lastErr
}

// downloadEdgeEmuParallel downloads a file from edgeemu.net using concurrent range requests.
func downloadEdgeEmuParallel(urlStr, dest, name string, totalSize int64, workers int) error {
	chunkSize := (totalSize + int64(workers) - 1) / int64(workers)
	type chunkSpec struct {
		index int
		start int64
		end   int64
		path  string
	}
	chunks := make([]chunkSpec, 0, workers)
	for i := 0; i < workers; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		chunks = append(chunks, chunkSpec{i, start, end, dest + fmt.Sprintf(".part%d", i)})
	}

	var written int64
	startTime := time.Now()
	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		lastConsole := time.Time{}
		for {
			select {
			case <-progressDone:
				return
			case now := <-ticker.C:
				w := atomic.LoadInt64(&written)
				pct := float64(w) / float64(totalSize) * 100
				elapsed := now.Sub(startTime).Seconds()
				if elapsed < 0.001 {
					elapsed = 0.001
				}
				speedMBs := float64(w) / elapsed / 1048576
				etaStr := "..."
				if speedMBs > 0 && pct < 100 {
					etaSecs := float64(totalSize-w) / (speedMBs * 1048576)
					etaStr = "~" + fmtDuration(etaSecs) + " left"
				}
				logStatus(name, "Processing",
					fmt.Sprintf("Downloading: %.0f%% (%.0f/%.0f MB) @ %.1f MB/s | %s | %dx",
						pct, float64(w)/1048576, float64(totalSize)/1048576, speedMBs, etaStr, workers))
				if now.Sub(lastConsole) > 15*time.Second {
					logf("ROM Download [%s]: %.1f%% @ %.1f MB/s | %dx", name, pct, speedMBs, workers)
					lastConsole = now
				}
			}
		}
	}()

	var wg sync.WaitGroup
	errs := make([]error, len(chunks))
	for i, c := range chunks {
		wg.Add(1)
		go func(idx int, spec chunkSpec) {
			defer wg.Done()
			errs[idx] = downloadEdgeEmuChunk(urlStr, spec.path, spec.start, spec.end, &written)
		}(i, c)
	}
	wg.Wait()
	close(progressDone)

	for i, e := range errs {
		if e != nil {
			for _, c := range chunks {
				os.Remove(c.path)
			}
			return fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), e)
		}
	}

	logf("[%s] Joining %d ROM parts...", name, len(chunks))
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(out, CopyBufferSize)
	joinErr := func() error {
		for _, c := range chunks {
			f, err := os.Open(c.path)
			if err != nil {
				return err
			}
			_, err = io.Copy(bw, f)
			f.Close()
			os.Remove(c.path)
			if err != nil {
				return err
			}
		}
		return bw.Flush()
	}()
	out.Close()
	if joinErr != nil {
		os.Remove(dest)
		return joinErr
	}
	return nil
}

// downloadEdgeEmuChunk downloads one byte-range chunk from edgeemu.net with retries.
func downloadEdgeEmuChunk(urlStr, destPath string, start, end int64, writtenAtomic *int64) error {
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create part: %w", err)
	}
	defer f.Close()

	var lastErr error
	for attempt := 0; attempt <= iaChunkRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * iaChunkRetryBase
			logf("RETRY ROM chunk bytes=%d-%d attempt %d: %v — waiting %s", start, end, attempt, lastErr, wait)
			time.Sleep(wait)
		}
		req, err := http.NewRequest("GET", urlStr, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := edgeEmuHTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request: %w", err)
			continue
		}
		if resp.StatusCode != 206 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d (expected 206)", resp.StatusCode)
			continue
		}
		var chunkWritten int64
		buf := make([]byte, 256*1024)
		var readErr error
		for {
			var n int
			n, readErr = resp.Body.Read(buf)
			if n > 0 {
				if _, wErr := f.WriteAt(buf[:n], chunkWritten); wErr != nil {
					resp.Body.Close()
					atomic.AddInt64(writtenAtomic, -chunkWritten)
					lastErr = fmt.Errorf("write: %w", wErr)
					chunkWritten = 0
					goto nextAttempt
				}
				atomic.AddInt64(writtenAtomic, int64(n))
				chunkWritten += int64(n)
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				break
			}
		}
		resp.Body.Close()
		if readErr != nil && readErr != io.EOF {
			atomic.AddInt64(writtenAtomic, -chunkWritten)
			lastErr = fmt.Errorf("read: %w", readErr)
			continue
		}
		return nil
	nextAttempt:
	}
	return lastErr
}
