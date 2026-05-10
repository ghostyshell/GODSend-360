// app.go — central App struct holding all shared server state.
package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"godsend/models"
)

// App holds all shared state for the GODsend backend.
// Services and handlers receive a pointer to App instead of accessing globals.
type App struct {
	// ── Paths & config (set once at startup) ──────────────────────────
	ToolsDir         string
	GodsendExeDir    string // directory containing the godsend binary
	TransferDir      string // local ISO folder
	PendingFTPDir    string
	ServerIP         string
	ServerPort       string
	FTPUsername      string
	FTPPassword      string
	DefaultXboxDrive string // GODSEND_DEFAULT_DRIVE
	CustomGodPath    string // GODSEND_CUSTOM_GOD_PATH (optional override for GOD install dir)
	CustomXexPath    string // GODSEND_CUSTOM_XEX_PATH (optional override for XEX install dir)
	Aria2ListenPort  string // GODSEND_ARIA2_LISTEN_PORT
	Aria2DhtPort     string // GODSEND_ARIA2_DHT_PORT
	ROMRootPath      string // drive-relative path for ROM installs on Xbox

	// ── IA auth & download settings ───────────────────────────────────
	IACookieHeader        string
	IAAuthorizationHeader string
	IADownloadMaxParallel int
	IAHTTPClient          *http.Client
	EdgeEmuHTTPClient     *http.Client

	// ── Shared buffers ────────────────────────────────────────────────
	CopyBuffer []byte

	// ── IA cache state ────────────────────────────────────────────────
	IAGameCache   map[string][]string
	IAGameCacheMu sync.RWMutex

	GameEntryMap   map[string]models.IAGameEntry
	GameEntryMapMu sync.RWMutex

	BuildStates   map[string]*models.BuildState
	BuildStatesMu sync.Mutex

	IACacheBuilding map[string]bool
	IACacheBuildMu  sync.Mutex

	// ── Minerva cache state ───────────────────────────────────────────
	MinervaGameCache   map[string][]string
	MinervaGameCacheMu sync.RWMutex

	MinervaEntryMap   map[string]models.MinervaEntry
	MinervaEntryMapMu sync.RWMutex

	MinervaBuildStates   map[string]*models.BuildState
	MinervaBuildStatesMu sync.Mutex

	MinervaCacheBuilding map[string]bool
	MinervaCacheBuildMu  sync.Mutex

	// ── ROM cache state ───────────────────────────────────────────────
	ROMGameCache   map[string][]string
	ROMGameCacheMu sync.RWMutex

	ROMURLMap   map[string]string
	ROMURLMapMu sync.RWMutex

	// ── sync.Map state (concurrent without external mutex) ────────────
	JobQueue        sync.Map
	SuppressedJobs  sync.Map // games removed via /queue/remove
	GamePartsMap    sync.Map
	XboxConnections sync.Map
	InstallTypeMap  sync.Map
}

// NewApp creates an App with initialised maps.
func NewApp() *App {
	return &App{
		IAGameCache:          make(map[string][]string),
		GameEntryMap:         make(map[string]models.IAGameEntry),
		BuildStates:          make(map[string]*models.BuildState),
		IACacheBuilding:      make(map[string]bool),
		MinervaGameCache:     make(map[string][]string),
		MinervaEntryMap:      make(map[string]models.MinervaEntry),
		MinervaBuildStates:   make(map[string]*models.BuildState),
		MinervaCacheBuilding: make(map[string]bool),
		ROMGameCache:         make(map[string][]string),
		ROMURLMap:            make(map[string]string),
		EdgeEmuHTTPClient:    &http.Client{Timeout: 0},
	}
}

// ── Logging helpers ───────────────────────────────────────────────────

// Logf prints a timestamped log line to stdout (feeds Electron terminal).
func (a *App) Logf(format string, args ...interface{}) {
	fmt.Printf("[%s] "+format+"\n", append([]interface{}{time.Now().Format("15:04:05")}, args...)...)
}

// LogStatus updates the job queue entry for a game (feeds Lua progress polling).
func (a *App) LogStatus(game, state, msg string) {
	if _, suppressed := a.SuppressedJobs.Load(game); suppressed {
		return
	}
	a.JobQueue.Store(game, models.GameStatus{State: state, Message: msg})
}

// LogFTPComplete marks a game FTP transfer as complete and emits a structured
// event line so that the Electron main process can trigger Aurora asset upload
// and Xbox Library cache sync automatically.
func (a *App) LogFTPComplete(gameName, titleID, xboxIP string) {
	a.LogStatus(gameName, "Ready", "FTP Transfer Complete!")
	data, _ := json.Marshal(map[string]string{
		"game_name": gameName,
		"title_id":  titleID,
		"xbox_ip":   xboxIP,
	})
	fmt.Printf("GODSEND_FTP_COMPLETE:%s\n", data)
}

// LookupInstallType returns the install type for a game: "god", "content", or "xex".
func (a *App) LookupInstallType(gameName string) string {
	it := "god"
	if v, ok := a.InstallTypeMap.Load(gameName); ok {
		it = strings.ToLower(strings.TrimSpace(v.(string)))
	}
	if it != "god" && it != "content" && it != "xex" {
		return "god"
	}
	return it
}

// FmtDuration formats a duration in seconds as "1m23s" (or "45s" for < 60s).
func FmtDuration(secs float64) string {
	if secs < 0 {
		secs = 0
	}
	s := int(secs)
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm%02ds", s/60, s%60)
}
