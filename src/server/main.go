package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"syscall"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"os/exec"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/jlaffaye/ftp"

	"godsend/services"
	"godsend/utils"
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

	// Minerva Archive browse base
	MinervaBrowseBase = "https://minerva-archive.org/browse/"

	// Internet Archive / HTTP range downloads: fixed-size segment queue + worker pool
	// (dynamic work assignment like https://github.com/GopeedLab/gopeed — avoids one slow tail chunk).
	iaChunkRetries       = 5
	iaChunkRetryBase     = 6 * time.Second
	iaParallelThreshold  = 32 * 1024 * 1024 // below this size, use a single HTTP stream
	iaSegmentSize        = 4 * 1024 * 1024  // bytes per queued range job
	iaParallelMaxDefault = 16                // default concurrent range GETs
	iaParallelMaxCap     = 32                // upper bound for env-tuned parallelism
)

func clampIAParallel(c int) int {
	if c < 1 {
		return 1
	}
	if c > iaParallelMaxCap {
		return iaParallelMaxCap
	}
	return c
}

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
// MINERVA ARCHIVE COLLECTION MAP
// Source: https://minerva-archive.org/browse/
// Xbox 360 ISOs (Redump), OG Xbox (Redump), and digital content (No-Intro).
// Each entry is a single browse-page URL to scrape.
// ==========================================
var minervaPageURLs = map[string]string{
	"xbox360": MinervaBrowseBase + "Redump/Microsoft%20-%20Xbox%20360/",
	"xbox":    MinervaBrowseBase + "Redump/Microsoft%20-%20Xbox/",
	// digital, xbla, dlc, xblig all share the No-Intro Digital page; tag-filters applied at build time.
	"digital": MinervaBrowseBase + "No-Intro/Microsoft%20-%20Xbox%20360%20(Digital)/",
	"xbla":    MinervaBrowseBase + "No-Intro/Microsoft%20-%20Xbox%20360%20(Digital)/",
	"dlc":     MinervaBrowseBase + "No-Intro/Microsoft%20-%20Xbox%20360%20(Digital)/",
	"xblig":   MinervaBrowseBase + "No-Intro/Microsoft%20-%20Xbox%20360%20(Digital)/",
	"games":   MinervaBrowseBase + "No-Intro/Non-Redump%20-%20Microsoft%20-%20Xbox%20360/",
}

// minervaTagFilters: if non-empty, only filenames containing this substring are kept.
var minervaTagFilters = map[string]string{
	"xbla":  "(XBLA)",
	"dlc":   "(Addon)",
	"xblig": "(XBLIG)",
}

// minervaTorrentURLs: the collection-level .torrent file for each platform.
// digital/xbla/dlc/xblig all share the No-Intro Digital torrent.
var minervaTorrentURLs = map[string]string{
	"xbox360": "https://minerva-archive.org/assets/Minerva_Myrient_v0.3/Minerva_Myrient%20-%20Redump%20-%20Microsoft%20-%20Xbox%20360.torrent",
	"xbox":    "https://minerva-archive.org/assets/Minerva_Myrient_v0.3/Minerva_Myrient%20-%20Redump%20-%20Microsoft%20-%20Xbox.torrent",
	"digital": "https://minerva-archive.org/assets/Minerva_Myrient_v0.3/Minerva_Myrient%20-%20No-Intro%20-%20Microsoft%20-%20Xbox%20360%20(Digital).torrent",
	"xbla":    "https://minerva-archive.org/assets/Minerva_Myrient_v0.3/Minerva_Myrient%20-%20No-Intro%20-%20Microsoft%20-%20Xbox%20360%20(Digital).torrent",
	"dlc":     "https://minerva-archive.org/assets/Minerva_Myrient_v0.3/Minerva_Myrient%20-%20No-Intro%20-%20Microsoft%20-%20Xbox%20360%20(Digital).torrent",
	"xblig":   "https://minerva-archive.org/assets/Minerva_Myrient_v0.3/Minerva_Myrient%20-%20No-Intro%20-%20Microsoft%20-%20Xbox%20360%20(Digital).torrent",
	"games":   "https://minerva-archive.org/assets/Minerva_Myrient_v0.3/Minerva_Myrient%20-%20No-Intro%20-%20Non-Redump%20-%20Microsoft%20-%20Xbox%20360.torrent",
}

// minervaHrefRe extracts the value of href="/rom?name=…" from Minerva browse pages.
var minervaHrefRe = regexp.MustCompile(`href="(/rom\?name=[^"]+)"`)

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
	"a2600":    {Name: "Atari - 2600", BrowseURL: "https://edgeemu.net/browse/atari-2600", Folder: "A2600"},
	"a5200":    {Name: "Atari - 5200", BrowseURL: "https://edgeemu.net/browse/atari-5200", Folder: "A5200"},
	"a7800":    {Name: "Atari - 7800", BrowseURL: "https://edgeemu.net/browse/atari-7800", Folder: "A7800"},
	"jaguar":   {Name: "Atari - Jaguar", BrowseURL: "https://edgeemu.net/browse/atari-jaguar", Folder: "JAG"},
	"jaguarcd": {Name: "Atari - Jaguar CD", BrowseURL: "https://edgeemu.net/browse/atari-jaguar-cd", Folder: "JAGCD"},
	"lynx":     {Name: "Atari - Lynx", BrowseURL: "https://edgeemu.net/browse/atari-lynx", Folder: "LYNX"},
	"st":       {Name: "Atari - ST", BrowseURL: "https://edgeemu.net/browse/atari-st", Folder: "ST"},
	// Bandai
	"ws": {Name: "Bandai - WonderSwan", BrowseURL: "https://edgeemu.net/browse/bandai-wonderswan", Folder: "WS"},
	// Coleco
	"coleco": {Name: "Coleco - ColecoVision", BrowseURL: "https://edgeemu.net/browse/colecovision", Folder: "COLECO"},
	// Commodore
	"c64":       {Name: "Commodore - 64", BrowseURL: "https://edgeemu.net/browse/commodore-64", Folder: "C64"},
	"amiga":     {Name: "Commodore - Amiga", BrowseURL: "https://edgeemu.net/browse/commodore-amiga", Folder: "AMIGA"},
	"amigacd":   {Name: "Commodore - Amiga CD", BrowseURL: "https://edgeemu.net/browse/commodore-amiga-cd", Folder: "AMIGACD"},
	"amigacd32": {Name: "Commodore - Amiga CD32", BrowseURL: "https://edgeemu.net/browse/commodore-amiga-cd32", Folder: "AMIGACD32"},
	"plus4":     {Name: "Commodore - Plus/4", BrowseURL: "https://edgeemu.net/browse/commodore-plus-4", Folder: "PLUS4"},
	"vic20":     {Name: "Commodore - VIC-20", BrowseURL: "https://edgeemu.net/browse/commodore-vic-20", Folder: "VIC20"},
	// Fairchild
	"channelf": {Name: "Fairchild - Channel F", BrowseURL: "https://edgeemu.net/browse/fairchild-channel-f", Folder: "CHANNELF"},
	// GCE
	"vectrex": {Name: "GCE - Vectrex", BrowseURL: "https://edgeemu.net/browse/gce-vectrex", Folder: "VECTREX"},
	// Microsoft
	"msx": {Name: "Microsoft - MSX / MSX2", BrowseURL: "https://edgeemu.net/browse/microsoft-msx", Folder: "MSX"},
	// NEC
	"pcecd": {Name: "NEC - PC Engine CD / TurboGrafx CD", BrowseURL: "https://edgeemu.net/browse/nec-pc-engine-cd-turbografx-cd", Folder: "PCECD"},
	"sgx":   {Name: "NEC - PC Engine SuperGrafx", BrowseURL: "https://edgeemu.net/browse/nec-pc-engine-supergrafx", Folder: "SGX"},
	"pce":   {Name: "NEC - PC Engine / TurboGrafx 16", BrowseURL: "https://edgeemu.net/browse/nec-pc-engine-turbografx-16", Folder: "PCE"},
	// Nintendo
	"nds":  {Name: "Nintendo - DS", BrowseURL: "https://edgeemu.net/browse/nintendo-ds", Folder: "NDS"},
	"fds":  {Name: "Nintendo - Famicom Disk System", BrowseURL: "https://edgeemu.net/browse/nintendo-fds", Folder: "FDS"},
	"gb":   {Name: "Nintendo - Game Boy", BrowseURL: "https://edgeemu.net/browse/nintendo-gameboy", Folder: "GB"},
	"gba":  {Name: "Nintendo - Game Boy Advance", BrowseURL: "https://edgeemu.net/browse/nintendo-gameboy-advance", Folder: "GBA"},
	"gbc":  {Name: "Nintendo - Game Boy Color", BrowseURL: "https://edgeemu.net/browse/nintendo-gameboy-color", Folder: "GBC"},
	"gc":   {Name: "Nintendo - GameCube", BrowseURL: "https://edgeemu.net/browse/nintendo-gamecube", Folder: "GC"},
	"n64":  {Name: "Nintendo - 64", BrowseURL: "https://edgeemu.net/browse/nintendo-64", Folder: "N64"},
	"nes":  {Name: "Nintendo - NES", BrowseURL: "https://edgeemu.net/browse/nintendo-nes", Folder: "NES"},
	"sat":  {Name: "Nintendo - Satellaview", BrowseURL: "https://edgeemu.net/browse/nintendo-satellaview", Folder: "SAT"},
	"vb":   {Name: "Nintendo - Virtual Boy", BrowseURL: "https://edgeemu.net/browse/nintendo-virtualboy", Folder: "VB"},
	"snes": {Name: "Nintendo - SNES", BrowseURL: "https://edgeemu.net/browse/nintendo-snes", Folder: "SNES"},
	// Panasonic
	"3do": {Name: "Panasonic - 3DO", BrowseURL: "https://edgeemu.net/browse/panasonic-3do", Folder: "3DO"},
	// Philips
	"cdi": {Name: "Philips - CDi", BrowseURL: "https://edgeemu.net/browse/philips-cdi", Folder: "CDI"},
	// RCA
	"studioii": {Name: "RCA - Studio II", BrowseURL: "https://edgeemu.net/browse/rca-studioii", Folder: "STUDIOII"},
	// Sega
	"32x":     {Name: "Sega - 32X", BrowseURL: "https://edgeemu.net/browse/sega-32x", Folder: "32X"},
	"dc":      {Name: "Sega - Dreamcast", BrowseURL: "https://edgeemu.net/browse/sega-dreamcast", Folder: "DC"},
	"gg":      {Name: "Sega - Game Gear", BrowseURL: "https://edgeemu.net/browse/sega-gamegear", Folder: "GG"},
	"sms":     {Name: "Sega - Master System / Mark III", BrowseURL: "https://edgeemu.net/browse/sega-sms", Folder: "SMS"},
	"scd":     {Name: "Sega - Mega-CD / Sega CD", BrowseURL: "https://edgeemu.net/browse/sega-cd", Folder: "SCD"},
	"genesis": {Name: "Sega - Mega Drive / Genesis", BrowseURL: "https://edgeemu.net/browse/sega-genesis", Folder: "MD"},
	"pico":    {Name: "Sega - PICO", BrowseURL: "https://edgeemu.net/browse/sega-pico", Folder: "PICO"},
	"saturn":  {Name: "Sega - Saturn", BrowseURL: "https://edgeemu.net/browse/sega-saturn", Folder: "SATURN"},
	"sg1000":  {Name: "Sega - SG-1000", BrowseURL: "https://edgeemu.net/browse/sega-sg1000", Folder: "SG1000"},
	// Sinclair
	"zx": {Name: "Sinclair - ZX Spectrum +3", BrowseURL: "https://edgeemu.net/browse/sinclair-zx-spectrum-3", Folder: "ZX"},
	// SNK
	"ngcd": {Name: "SNK - Neo Geo CD", BrowseURL: "https://edgeemu.net/browse/snk-neo-geo-cd", Folder: "NGCD"},
	"ngpc": {Name: "SNK - Neo Geo Pocket Color", BrowseURL: "https://edgeemu.net/browse/snk-ngpc", Folder: "NGPC"},
	// Watara
	"supervision": {Name: "Watara - Supervision", BrowseURL: "https://edgeemu.net/browse/watara-supervision", Folder: "SUPERVISION"},
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

// MinervaEntry links a display name to its Minerva download path.
type MinervaEntry struct {
	FileName  string `json:"filename"`   // e.g. "007 - Blood Stone (USA, Europe).zip"
	PathParam string `json:"path_param"` // URL-encoded path for /rom?name= query param
}

// MinervaPlatformCache is persisted to disk per platform.
type MinervaPlatformCache struct {
	Games     []string                `json:"games"`
	Entries   map[string]MinervaEntry `json:"entries"` // lower(basename-no-ext) -> entry
	BuildTime time.Time               `json:"build_time"`
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

	// in-memory Minerva game lists per platform
	minervaGameCache   = map[string][]string{}
	minervaGameCacheMu sync.RWMutex

	// lower(basename-no-ext) -> MinervaEntry for fast Minerva download lookup
	minervaEntryMap   = map[string]MinervaEntry{}
	minervaEntryMapMu sync.RWMutex

	// live build progress for Minerva platform caches
	minervaBuildStates   = map[string]*buildState{}
	minervaBuildStatesMu sync.Mutex

	// prevent double-building the same Minerva platform cache
	minervaCacheBuilding = map[string]bool{}
	minervaCacheBuildMu  sync.Mutex
)

// ==========================================
// SERVER STATE
// ==========================================

var (
	toolsDir              string
	godsendExeDir         string // directory containing the godsend binary (bundled cache/ lives here when shipped)
	transferDir           string // local ISO folder (default toolsDir/Transfer, or GODSEND_TRANSFER)
	pendingFTPDir         string
	defaultXboxDrive      string // GODSEND_DEFAULT_DRIVE
	aria2ListenPort       string // GODSEND_ARIA2_LISTEN_PORT
	aria2DhtPort          string // GODSEND_ARIA2_DHT_PORT
	jobQueue              sync.Map
	suppressedJobs        sync.Map // games removed via /queue/remove — ignore logStatus until next /trigger
	iaCookieHeader        string   // GODSEND_IA_COOKIE — browser session for archive.org
	iaAuthorizationHeader string   // GODSEND_IA_AUTHORIZATION — optional Bearer/basic
	iaDownloadMaxParallel int      // max concurrent range GETs (GODSEND_IA_MAX_CONNECTIONS or legacy GODSEND_IA_CONCURRENCY)
	iaHTTPClient          *http.Client
	serverIP              string
	serverPort            string
	ftpUsername           string // GODSEND_FTP_USER — Aurora FTP username (default xboxftp)
	ftpPassword           string // GODSEND_FTP_PASS — Aurora FTP password (default xboxftp)
	gamePartsMap          sync.Map
	copyBuffer            []byte
	xboxConnections       sync.Map
	// installTypeMap stores the user-selected install type per game: "god", "content", or "xex"
	installTypeMap sync.Map
)

func lookupInstallType(gameName string) string {
	it := "god"
	if v, ok := installTypeMap.Load(gameName); ok {
		it = strings.ToLower(strings.TrimSpace(v.(string)))
	}
	if it != "god" && it != "content" && it != "xex" {
		return "god"
	}
	return it
}

// ==========================================
// MULTI-DISC COMPAT TABLE
// ==========================================

// discCompatRec holds the recommended install method for a known multi-disc title.
type discCompatRec struct {
	installType string // "god" or "content"
	notes       string
}

// discCompatTable maps TitleID → recommendation for Disc 2+ of known titles.
// Sourced from docs/reference/multi-disc-compatibility.md.
var discCompatTable = map[uint32]discCompatRec{
	0x4D5308AB: {installType: "content", notes: "Disc 2 is bonus content loaded by Disc 1"},
	0x555307DC: {installType: "content", notes: "Disc 2 is bonus content"},
	0x5345082C: {installType: "content", notes: "Disc 2 is bonus content loaded by Disc 1"},
	0x53450833: {installType: "content", notes: "Disc 2 is bonus content loaded by Disc 1"},
	0x545407E7: {installType: "content", notes: "GOTY / multi-disc: Disc 2 is DLC content (Borderlands)"},
	0x5454087C: {installType: "content", notes: "GOTY / multi-disc: Disc 2 is DLC content (Borderlands 2)"},
	0x4541082F: {installType: "content", notes: "Disc 2 is bonus content"},
	0x41560855: {installType: "content", notes: "Disc 2 is multiplayer/zombies content"},
	0x41560817: {installType: "content", notes: "Disc 2 is spec ops content"},
	0x41560882: {installType: "content", notes: "Disc 2 is spec ops content"},
	0x41560812: {installType: "content", notes: "Disc 2 is multiplayer content"},
	0x4541085F: {installType: "content", notes: "Disc 2 is bonus content"},
	0x45410850: {installType: "content", notes: "Disc 2 is bonus content"},
	0x45410889: {installType: "content", notes: "Disc 2 is bonus content"},
	0x524B4005: {installType: "content", notes: "Disc 2/3 are bonus content"},
	0x4541082E: {installType: "content", notes: "Disc 2 is bonus content"},
	0x4541097C: {installType: "content", notes: "Disc 2 is bonus content"},
	0x5254082A: {installType: "content", notes: "Disc 2 is multiplayer content"},
	0x5553083E: {installType: "content", notes: "Disc 2 continues the game as content"},
	0x5454082B: {installType: "content", notes: "Disc 2 (Undead Nightmare) is content"},
	0x5553081A: {installType: "content", notes: "Disc 2 is bonus content"},
	0x4541091B: {installType: "content", notes: "Disc 2 is bonus content"},
	0x5454086B: {installType: "content", notes: "Disc 2 is high-res texture pack"},
	0x5553088F: {installType: "content", notes: "Disc 2 is bonus content"},
	0x4541089C: {installType: "content", notes: "Disc 2 is bonus content"},
	0x0B4607F2: {installType: "god", notes: "Disc 2 is game continuation"},
	0x4D5307E6: {installType: "god", notes: "Disc 2 is game continuation"},
	0x4D5307F1: {installType: "god", notes: "Disc 2 is game continuation"},
	0x4D53082D: {installType: "god", notes: "Disc 2 contains car/track data"},
	0x4D53087F: {installType: "god", notes: "Disc 2 contains car/track data"},
	0x5345200A: {installType: "god", notes: "Disc 2 is game continuation"},
	0x4D530877: {installType: "god", notes: "Disc 2 is multiplayer disc"},
	0x4D530830: {installType: "god", notes: "Multi-disc RPG — all discs are GOD"},
	0x5345082D: {installType: "god", notes: "Disc 2 is game continuation"},
	0x4D530810: {installType: "god", notes: "Disc 2 is game continuation"},
}

// discCompatRec returns the compat recommendation for a given TitleID and disc number.
func discCompat(titleID uint32, discNumber byte) discCompatRec {
	if discNumber <= 1 {
		return discCompatRec{installType: "god"}
	}
	if rec, ok := discCompatTable[titleID]; ok {
		return rec
	}
	return discCompatRec{installType: "content", notes: "Default: Disc 2+ is typically content"}
}

// Redump-style names often use [DVD2] instead of "Disc 2"; Lua menu uses the same idea.
var multiDiscNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bdisc\s*[2-9]\b`),
	regexp.MustCompile(`(?i)\bdisk\s*[2-9]\b`),
	regexp.MustCompile(`(?i)\bcd\s*[2-9]\b`),
	regexp.MustCompile(`(?i)\(disc\s*[2-9]\)`),
	regexp.MustCompile(`(?i)\(disk\s*[2-9]\)`),
	regexp.MustCompile(`(?i)\(cd\s*[2-9]\)`),
	regexp.MustCompile(`(?i)\[dvd\s*[2-9]\]`),
	regexp.MustCompile(`(?i)\[dvd[2-9]\]`),
	regexp.MustCompile(`(?i)\bdvd\s*[2-9]\b`),
	regexp.MustCompile(`(?i)\[cd\s*[2-9]\]`),
}

func isMultiDiscGameName(name string) bool {
	for _, re := range multiDiscNamePatterns {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// guessTitleIDFromMultiDiscName maps common IA/Redump strings to Title IDs for /disc-info
// when there is no ISO in Transfer yet (filename-only hint).
func guessTitleIDFromMultiDiscName(name string) uint32 {
	l := strings.ToLower(name)
	if strings.Contains(l, "borderlands 2") && (strings.Contains(l, "goty") || strings.Contains(l, "game of the year") || strings.Contains(l, "triple pack")) {
		return 0x5454087C
	}
	if strings.Contains(l, "borderlands") && strings.Contains(l, "pre-sequel") {
		return 0
	}
	// "Add-On Content Disc" releases for Borderlands GOTY use placeholder XEX TitleID FFED2000;
	// the content belongs under the main game's TitleID 545407E7.
	if strings.Contains(l, "borderlands") && (strings.Contains(l, "goty") || strings.Contains(l, "game of the year") || strings.Contains(l, "triple pack") || strings.Contains(l, "add-on content")) {
		return 0x545407E7
	}
	return 0
}

// isContentDiscPlaceholderTitleID returns true when the title ID read from a
// content disc's XEX is a known publisher placeholder rather than the parent
// game's real Title ID. In these cases the correct destination Title ID must be
// derived from the game name instead.
func isContentDiscPlaceholderTitleID(tid uint32) bool {
	switch tid {
	case 0xFFED2000: // Borderlands GOTY Add-On Content Disc (2K Games placeholder)
		return true
	}
	return false
}

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

func isTCPAddrInUse(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		if errno, ok := opErr.Err.(syscall.Errno); ok {
			if errno == syscall.EADDRINUSE {
				return true
			}
			if runtime.GOOS == "windows" && int(errno) == 10048 { // WSAEADDRINUSE
				return true
			}
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "only one usage of each socket address") ||
		strings.Contains(msg, "wsaeaddrinuse")
}

// listenOnAvailablePort binds to start, then start+1, … until success or a non–address-in-use error.
func listenOnAvailablePort(start int) (net.Listener, int, error) {
	if start < 1 || start > 65535 {
		return nil, 0, fmt.Errorf("invalid start port %d", start)
	}
	for p := start; p <= 65535; p++ {
		addr := fmt.Sprintf(":%d", p)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, p, nil
		}
		if !isTCPAddrInUse(err) {
			return nil, 0, fmt.Errorf("listen %s: %w", addr, err)
		}
		logf("[WARN] TCP port %d in use, trying %d", p, p+1)
	}
	return nil, 0, fmt.Errorf("no free TCP port from %d through 65535", start)
}

// ==========================================
// MAIN & SETUP
// ==========================================

func main() {
	if err := setupPaths(); err != nil {
		fmt.Printf("[FATAL] Setup failed: %v\n", err)
		os.Exit(1)
	}
	if err := ensureAria2cDarwinAtStartup(); err != nil {
		logf("[WARN] Could not ensure aria2c on macOS: %v — Minerva torrents need aria2 (install Homebrew + brew install aria2, or set GODSEND_SKIP_ARIA2_BOOTSTRAP=1 if you use IA only)", err)
	}
	loadIAAuthFromEnv()
	serverIP = getOutboundIP()
	if serverIP == "" {
		serverIP = "0.0.0.0"
	}
	copyBuffer = make([]byte, CopyBufferSize)

	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║    GODSend Backend Server v2.7.4         ║")
	fmt.Println("║  ISO + XEX + XBLA + DLC + ROMs (EdgeEmu) ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Printf("[INFO] Copy Buffer: %d MB | Serve Buffer: %d KB | FTP Buffer: %d MB\n",
		CopyBufferSize/1024/1024, ServeBufferSize/1024, FTPBufferSize/1024/1024)
	fmt.Printf("[INFO] Transfer folder (local ISOs): %s\n", transferDir)
	fmt.Printf("[INFO] ROM install path (on Xbox): [Drive]\\%s\\[System]\\\n", romRootPath)

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

	// Load Minerva caches from disk; build any that are missing in background.
	minervaPlatforms := []string{"xbox360", "xbox", "digital", "xbla", "dlc", "xblig", "games"}
	var minervaDelay time.Duration
	for _, mp := range minervaPlatforms {
		if loadMinervaCacheFromDisk(mp) {
			logf("MINERVA CACHE: Loaded %s from disk", mp)
		} else {
			go func(p string, d time.Duration) {
				if d > 0 {
					time.Sleep(d)
				}
				buildMinervaCache(p)
			}(mp, minervaDelay)
			minervaDelay += 1200 * time.Millisecond
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
	http.HandleFunc("/disc-info", recoverMiddleware(handleDiscInfo))
	http.HandleFunc("/files/", recoverMiddleware(handleFileServe))
	http.HandleFunc("/data/status", recoverMiddleware(handleDataStatus))
	http.HandleFunc("/data/clear", recoverMiddleware(handleDataClear))
	http.HandleFunc("/config", recoverMiddleware(handleServerConfig))

	// Resume any pending FTP jobs from previous sessions
	go func() {
		for _, job := range loadAllPendingFTPJobs() {
			logf("FTP PENDING: Resuming job for %s (from previous session)", job.GameName)
			logStatus(job.GameName, "Pending FTP", "Resumed from previous session — waiting for Xbox FTP...")
			go retryFTPJobForever(job)
		}
	}()

	requestedPort, err := strconv.Atoi(serverPort)
	if err != nil {
		fmt.Printf("[FATAL] invalid server port %q\n", serverPort)
		os.Exit(1)
	}
	listener, chosenPort, err := listenOnAvailablePort(requestedPort)
	if err != nil {
		fmt.Printf("[FATAL] %v\n", err)
		os.Exit(1)
	}
	if chosenPort != requestedPort {
		logf("[INFO] Port %d was in use; listening on %d instead", requestedPort, chosenPort)
	}
	serverPort = strconv.Itoa(chosenPort)
	fmt.Printf("\n[INFO] Server IP: %s:%s\n", serverIP, serverPort)
	logf("[INFO] GODSEND_LISTEN_PORT=%s", serverPort)

	server := &http.Server{
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
	logf("Starting server on port %s... Server started. Please start the script on the xbox", serverPort)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	godsendExeDir = exDir
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
	serverPort = Port
	if v := strings.TrimSpace(os.Getenv("GODSEND_PORT")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("GODSEND_PORT must be an integer between 1 and 65535")
		}
		serverPort = strconv.Itoa(n)
		logf("[INFO] Server port (GODSEND_PORT): %s", serverPort)
	}
	ftpUsername = "xboxftp"
	ftpPassword = "xboxftp"
	if v := strings.TrimSpace(os.Getenv("GODSEND_FTP_USER")); v != "" {
		ftpUsername = v
	}
	if v := os.Getenv("GODSEND_FTP_PASS"); v != "" {
		ftpPassword = v
	}
	pendingFTPDir = filepath.Join(toolsDir, "pending_ftp")
	os.MkdirAll(pendingFTPDir, 0755)

	defaultXboxDrive = strings.TrimSpace(os.Getenv("GODSEND_DEFAULT_DRIVE"))
	aria2ListenPort = strings.TrimSpace(os.Getenv("GODSEND_ARIA2_LISTEN_PORT"))
	aria2DhtPort = strings.TrimSpace(os.Getenv("GODSEND_ARIA2_DHT_PORT"))

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

	iaDownloadMaxParallel = iaParallelMaxDefault
	if v := strings.TrimSpace(os.Getenv("GODSEND_IA_MAX_CONNECTIONS")); v != "" {
		if c, err := strconv.Atoi(v); err == nil {
			iaDownloadMaxParallel = clampIAParallel(c)
		}
	} else if v := strings.TrimSpace(os.Getenv("GODSEND_IA_CONCURRENCY")); v != "" {
		// Legacy desktop / docs: same meaning as max parallel range requests (wider clamp than old 1–7).
		if c, err := strconv.Atoi(v); err == nil {
			iaDownloadMaxParallel = clampIAParallel(c)
		}
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
	logf("[INFO] Internet Archive: chunked HTTP downloads (max %d parallel range requests)", iaDownloadMaxParallel)
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
// MINERVA CACHE — DISK PERSISTENCE
// ==========================================

func minervaCacheFilePath(platform string) string {
	return filepath.Join(toolsDir, "cache", "minerva_"+platform+".json")
}

func saveMinervaCacheToDisk(platform string, games []string, entries map[string]MinervaEntry) {
	mc := MinervaPlatformCache{
		Games:     games,
		Entries:   entries,
		BuildTime: time.Now(),
	}
	data, err := json.MarshalIndent(mc, "", "  ")
	if err != nil {
		logf("MINERVA CACHE SAVE ERROR %s: %v", platform, err)
		return
	}
	if err := os.WriteFile(minervaCacheFilePath(platform), data, 0644); err != nil {
		logf("MINERVA CACHE SAVE ERROR %s: %v", platform, err)
		return
	}
	logf("MINERVA CACHE: Saved %s (%d games) to disk", platform, len(games))
}

func loadMinervaCacheFromDisk(platform string) bool {
	data, err := os.ReadFile(minervaCacheFilePath(platform))
	if err != nil {
		return false
	}
	var mc MinervaPlatformCache
	if err := json.Unmarshal(data, &mc); err != nil {
		return false
	}
	if len(mc.Games) == 0 {
		return false
	}

	minervaGameCacheMu.Lock()
	minervaGameCache[platform] = mc.Games
	minervaGameCacheMu.Unlock()

	minervaEntryMapMu.Lock()
	for k, v := range mc.Entries {
		minervaEntryMap[k] = v
		if dk := strings.ToLower(decodeMinervaName(k)); dk != k {
			if _, taken := minervaEntryMap[dk]; !taken {
				minervaEntryMap[dk] = v
			}
		}
	}
	minervaEntryMapMu.Unlock()

	setMinervaBuildState(platform, "ready", int32(len(mc.Games)), int32(len(mc.Games)))
	return true
}

// ==========================================
// MINERVA CACHE — BUILD STATE
// ==========================================

func getMinervaBuildState(platform string) *buildState {
	minervaBuildStatesMu.Lock()
	s, ok := minervaBuildStates[platform]
	if !ok {
		s = &buildState{state: "idle"}
		minervaBuildStates[platform] = s
	}
	minervaBuildStatesMu.Unlock()
	return s
}

func setMinervaBuildState(platform, state string, loaded, total int32) {
	s := getMinervaBuildState(platform)
	atomic.StoreInt32(&s.loaded, loaded)
	atomic.StoreInt32(&s.total, total)
	minervaBuildStatesMu.Lock()
	s.state = state
	minervaBuildStatesMu.Unlock()
}

// ==========================================
// MINERVA CACHE — SCRAPE + BUILD
// ==========================================

// scrapeMinervaPage fetches one Minerva browse URL and returns file entries.
// tagFilter, if non-empty, restricts results to filenames containing that substring.
func scrapeMinervaPage(browseURL, tagFilter string) ([]MinervaEntry, error) {
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("GET", browseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", browseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("fetch %s: HTTP %d", browseURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", browseURL, err)
	}

	matches := minervaHrefRe.FindAllSubmatch(body, -1)
	var entries []MinervaEntry
	for _, m := range matches {
		hrefVal := string(m[1]) // e.g. "/rom?name=.%2FRedump%2F...%2FGame.zip"
		const prefix = "/rom?name="
		if !strings.HasPrefix(hrefVal, prefix) {
			continue
		}
		pathParam := hrefVal[len(prefix):]
		decoded, err := url.PathUnescape(pathParam)
		if err != nil {
			continue
		}
		ext := strings.ToLower(filepath.Ext(decoded))
		if ext != ".zip" && ext != ".7z" && ext != ".rar" {
			continue
		}
		fileName := filepath.Base(decoded)
		if tagFilter != "" && !strings.Contains(fileName, tagFilter) {
			continue
		}
		entries = append(entries, MinervaEntry{
			FileName:  fileName,
			PathParam: pathParam,
		})
	}
	return entries, nil
}

// buildMinervaCache scrapes the Minerva browse page for one platform and caches results.
// Safe to call multiple times — deduplicates via minervaCacheBuilding guard.
func buildMinervaCache(platform string) {
	minervaCacheBuildMu.Lock()
	if minervaCacheBuilding[platform] {
		minervaCacheBuildMu.Unlock()
		return
	}
	minervaCacheBuilding[platform] = true
	minervaCacheBuildMu.Unlock()

	defer func() {
		minervaCacheBuildMu.Lock()
		minervaCacheBuilding[platform] = false
		minervaCacheBuildMu.Unlock()
	}()

	browseURL, ok := minervaPageURLs[platform]
	if !ok {
		return
	}
	tagFilter := minervaTagFilters[platform]

	setMinervaBuildState(platform, "building", 0, 1)
	logf("MINERVA CACHE: Building %s ...", platform)

	entries, err := scrapeMinervaPage(browseURL, tagFilter)
	if err != nil {
		logf("MINERVA CACHE ERROR [%s]: %v", platform, err)
		setMinervaBuildState(platform, "error", 0, 1)
		return
	}

	newEntries := make(map[string]MinervaEntry, len(entries)*2)
	var allGames []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.FileName, filepath.Ext(e.FileName))
		lower := strings.ToLower(name)
		if _, dup := newEntries[lower]; dup {
			continue
		}
		me := MinervaEntry{FileName: e.FileName, PathParam: e.PathParam}
		newEntries[lower] = me
		if dec := strings.ToLower(decodeMinervaName(name)); dec != lower {
			if _, taken := newEntries[dec]; !taken {
				newEntries[dec] = MinervaEntry{FileName: me.FileName, PathParam: me.PathParam}
			}
		}
		allGames = append(allGames, name)
	}
	sort.Strings(allGames)
	setMinervaBuildState(platform, "ready", 1, 1)
	logf("MINERVA CACHE: %s complete — %d games", platform, len(allGames))

	minervaGameCacheMu.Lock()
	minervaGameCache[platform] = allGames
	minervaGameCacheMu.Unlock()

	minervaEntryMapMu.Lock()
	for k, v := range newEntries {
		minervaEntryMap[k] = v
	}
	minervaEntryMapMu.Unlock()

	saveMinervaCacheToDisk(platform, allGames, newEntries)
}

// ==========================================
// MINERVA CACHE — LOOKUP
// ==========================================

// findMinervaEntry looks up a game in the Minerva cache.
// Returns the entry and true if found, or false if not found.
// Triggers a background cache build if the cache is empty for this platform.
func findMinervaEntry(gameName, platform string) (MinervaEntry, bool) {
	keys := minervaLookupKeys(gameName)

	minervaEntryMapMu.RLock()
	for _, key := range keys {
		if key == "" {
			continue
		}
		if e, ok := minervaEntryMap[key]; ok {
			minervaEntryMapMu.RUnlock()
			return e, true
		}
	}
	// Fuzzy: strip region tags and compare base names (decode entities for comparison)
	decName := decodeMinervaName(gameName)
	lowerDec := strings.ToLower(decName)
	baseName := strings.ToLower(strings.SplitN(decName, " (", 2)[0])
	for k, e := range minervaEntryMap {
		kDec := decodeMinervaName(k)
		if strings.Contains(strings.ToLower(kDec), lowerDec) {
			minervaEntryMapMu.RUnlock()
			return e, true
		}
		kBase := strings.ToLower(strings.SplitN(kDec, " (", 2)[0])
		if kBase == baseName {
			minervaEntryMapMu.RUnlock()
			return e, true
		}
	}
	minervaEntryMapMu.RUnlock()

	// Trigger a background build if the cache is empty for this platform
	minervaGameCacheMu.RLock()
	isEmpty := len(minervaGameCache[platform]) == 0
	minervaGameCacheMu.RUnlock()
	if isEmpty {
		go buildMinervaCache(platform)
	}
	return MinervaEntry{}, false
}

// ==========================================
// MINERVA TORRENT DOWNLOAD
// ==========================================

// fetchMinervaTorrent downloads the collection .torrent file for the given platform from Minerva.
func fetchMinervaTorrent(platform string) ([]byte, error) {
	torrentURL, ok := minervaTorrentURLs[platform]
	if !ok {
		return nil, fmt.Errorf("no torrent URL for platform %q", platform)
	}
	logf("TORRENT: Fetching collection torrent for %s...", platform)
	req, err := http.NewRequest("GET", torrentURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("download torrent: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("torrent HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// aria2cResolved caches the result of aria2cBinary so we don't probe `--version`
// on every download. Cleared if a cached binary later fails.
var (
	aria2cResolvedMu   sync.Mutex
	aria2cResolvedPath string
)

// aria2cWorks runs `<path> --version` with a short timeout and reports whether
// the binary launches cleanly. Used to detect broken bundled binaries (e.g. the
// mac aria2c extracted from a Homebrew bottle, whose dylib references resolve
// to "Symbol not found: _sqlite3_close" on a fresh machine).
func aria2cWorks(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		if snippet == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, snippet)
	}
	return nil
}

// probeWorkingAria2c finds a usable aria2c (bundled next to the server binary, PATH,
// then macOS Homebrew locations). Not cached — used at startup and by aria2cBinary.
func probeWorkingAria2c() (string, error) {
	name := "aria2c"
	if runtime.GOOS == "windows" {
		name = "aria2c.exe"
	}
	var lastErr error
	tried := map[string]bool{}

	try := func(p string, label string) (string, bool) {
		if p == "" || tried[p] {
			return "", false
		}
		tried[p] = true
		werr := aria2cWorks(p)
		if werr == nil {
			return p, true
		}
		lastErr = fmt.Errorf("%s (%s) unusable: %v", label, p, werr)
		return "", false
	}

	bundled := filepath.Join(godsendExeDir, name)
	if _, err := os.Stat(bundled); err == nil {
		if p, ok := try(bundled, "bundled aria2c"); ok {
			return p, nil
		}
		if lastErr != nil {
			logf("[WARN] %v — trying PATH / Homebrew locations", lastErr)
		}
	}

	if lp, err := exec.LookPath("aria2c"); err == nil {
		if p, ok := try(lp, "aria2c on PATH"); ok {
			return p, nil
		}
	}

	for _, cand := range darwinAria2cExtraCandidates() {
		if _, err := os.Stat(cand); err != nil {
			continue
		}
		if p, ok := try(cand, "aria2c"); ok {
			return p, nil
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("aria2c not usable — %v", lastErr)
	}
	return "", fmt.Errorf("aria2c not found — bundled binary missing and not in PATH")
}

// aria2cBinary returns the path to a working aria2c executable.
// Tries the bundled binary first (next to the server binary), validates it with
// `--version`, then PATH and macOS Homebrew paths. Result is cached.
func aria2cBinary() (string, error) {
	aria2cResolvedMu.Lock()
	defer aria2cResolvedMu.Unlock()
	if aria2cResolvedPath != "" {
		return aria2cResolvedPath, nil
	}
	p, err := probeWorkingAria2c()
	if err != nil {
		if runtime.GOOS == "darwin" {
			return "", fmt.Errorf("%w. On macOS the backend normally installs Homebrew aria2 at startup; fix the error above or set GODSEND_SKIP_ARIA2_BOOTSTRAP=1 and install aria2 yourself", err)
		}
		return "", fmt.Errorf("%w. Install aria2 and restart the backend", err)
	}
	aria2cResolvedPath = p
	bundledName := "aria2c"
	if runtime.GOOS == "windows" {
		bundledName = "aria2c.exe"
	}
	bundled := filepath.Join(godsendExeDir, bundledName)
	if p != bundled {
		logf("[INFO] Using aria2c: %s", p)
	}
	return p, nil
}

// torrentBasenameMatches reports whether a path inside the .torrent matches the Minerva entry
// filename, including when one side uses HTML entities (e.g. &#39;) and the other uses a literal apostrophe.
func torrentBasenameMatches(torrentBase, entryFileName string) bool {
	if strings.EqualFold(torrentBase, entryFileName) {
		return true
	}
	a := decodeMinervaName(torrentBase)
	b := decodeMinervaName(entryFileName)
	if strings.EqualFold(a, b) {
		return true
	}
	if strings.EqualFold(a, entryFileName) || strings.EqualFold(torrentBase, b) {
		return true
	}
	return false
}

// downloadViaTorrent uses aria2c to download a single file from the Minerva collection torrent.
// It fetches the .torrent from Minerva's URL, finds the target file's 1-based index, then
// shells out to aria2c with --select-file so only that file is downloaded.
func downloadViaTorrent(platform, destDir, gameName string, entry MinervaEntry) (string, error) {
	aria2c, err := aria2cBinary()
	if err != nil {
		return "", err
	}

	torrentURL, ok := minervaTorrentURLs[platform]
	if !ok {
		return "", fmt.Errorf("no torrent URL for platform %q", platform)
	}

	// Fetch torrent to find the 1-based file index aria2c needs.
	torrentData, err := fetchMinervaTorrent(platform)
	if err != nil {
		return "", fmt.Errorf("fetch torrent: %w", err)
	}
	mi, err := metainfo.Load(bytes.NewReader(torrentData))
	if err != nil {
		return "", fmt.Errorf("parse .torrent: %w", err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return "", fmt.Errorf("torrent info: %w", err)
	}

	fileIndex := -1
	var fileSize int64
	for i, f := range info.UpvertedFiles() {
		torrentBase := filepath.Base(filepath.Join(f.Path...))
		if torrentBasenameMatches(torrentBase, entry.FileName) {
			fileIndex = i + 1 // aria2c uses 1-based index
			fileSize = f.Length
			break
		}
	}
	if fileIndex < 0 {
		return "", fmt.Errorf("file %q not found in torrent", entry.FileName)
	}

	logf("TORRENT [%s]: aria2c downloading %s (%.0f MB) file-index=%d", gameName, entry.FileName, float64(fileSize)/1048576, fileIndex)
	logStatus(gameName, "Processing", fmt.Sprintf("Torrenting (Minerva): starting... (%.0f MB)", float64(fileSize)/1048576))

	// Write torrent to a temp file so aria2c doesn't need to re-fetch it via HTTPS.
	// (aria2c on Windows has SSL issues fetching HTTPS URLs; Go has none.)
	tf, err := os.CreateTemp("", "godsend-*.torrent")
	if err != nil {
		return "", fmt.Errorf("create temp torrent: %w", err)
	}
	torrentFile := tf.Name()
	defer os.Remove(torrentFile)
	if _, err := tf.Write(torrentData); err != nil {
		tf.Close()
		return "", fmt.Errorf("write temp torrent: %w", err)
	}
	tf.Close()

	// aria2c nests output under <torrent-name>/path/… so the full path can exceed
	// Windows MAX_PATH (260 chars) when destDir + torrent subdirs + filename are combined.
	// Use a short-named OS temp dir as the aria2c working directory; move the finished
	// file to destDir afterwards.
	aria2cDir, err := os.MkdirTemp("", "gd-dl-*")
	if err != nil {
		return "", fmt.Errorf("create aria2c temp dir: %w", err)
	}
	defer os.RemoveAll(aria2cDir)

	args := []string{
		"--dir=" + aria2cDir,
		"--select-file=" + strconv.Itoa(fileIndex),
		"--seed-time=0",                    // stop seeding immediately after download
		"--bt-remove-unselected-file=true", // don't keep unselected files
		"--bt-max-peers=100",
		"--follow-torrent=false", // torrent file is our input, don't re-fetch
		"--file-allocation=none", // skip pre-allocation — avoids spurious ENOSPC on large files
		"--console-log-level=warn",
		"--summary-interval=3", // print progress every 3 s
		"--human-readable=true",
		torrentFile,
	}
	if aria2ListenPort != "" {
		args = append(args, "--listen-port="+aria2ListenPort)
		args = append(args, "--dht-listen-port="+aria2ListenPort)
	}
	if aria2DhtPort != "" {
		args = append(args, "--dht-listen-port="+aria2DhtPort)
	}
	_ = torrentURL // URL was used to fetch; aria2c gets the temp file

	cmd := exec.Command(aria2c, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("aria2c pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // merge stderr into the same pipe

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("aria2c start: %w", err)
	}

	// aria2c summary lines look like:
	//   [#abc123 195MiB/6504MiB(3%) CN:67 DL:9.9MiB ETA:31m]
	summaryRe := regexp.MustCompile(`\[#\S+\s+([\d.]+\S+)/([\d.]+\S+)\((\d+)%\)[^\]]*DL:([\d.]+\S+)[^\]]*ETA:(\S+)\]`)

	// Drain aria2c output in a goroutine so the pipe never fills and deadlocks cmd.Wait().
	// aria2c uses bare \r (carriage return) to redraw inline progress — bufio.ScanLines
	// only splits on \n/\r\n, so \r-only sequences accumulate until the buffer limit is
	// hit, Scan() returns false, and cmd.Wait() deadlocks (aria2c blocked on pipe write).
	// Custom split handles both \r and \n; 1 MB buffer covers any bursts between summaries.
	//
	// Non-progress lines (warnings, errors, abort messages) are kept in a small ring buffer
	// so they can be surfaced if aria2c exits non-zero — otherwise the only signal would be
	// "signal: abort trap" with no context. Bound the buffer so a chatty aria2c can't blow
	// memory on long-running downloads.
	const tailMax = 50
	var (
		tailMu   sync.Mutex
		tailBuf  []string
	)
	appendTail := func(line string) {
		tailMu.Lock()
		defer tailMu.Unlock()
		if len(tailBuf) >= tailMax {
			tailBuf = tailBuf[1:]
		}
		tailBuf = append(tailBuf, line)
	}

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		sc.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			for i, b := range data {
				if b == '\n' || b == '\r' {
					adv := i + 1
					if b == '\r' && adv < len(data) && data[adv] == '\n' {
						adv++ // consume \r\n as one unit
					}
					return adv, data[:i], nil
				}
			}
			if atEOF && len(data) > 0 {
				return len(data), data, nil
			}
			return 0, nil, nil
		})
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), " \t")
			if line == "" {
				continue
			}
			if m := summaryRe.FindStringSubmatch(line); m != nil {
				pct, dl, eta := m[3], m[4], m[5]
				msg := fmt.Sprintf("Torrenting (Minerva): %s%% @ %s/s ETA %s", pct, dl, eta)
				logf("TORRENT [%s]: %s", gameName, msg)
				logStatus(gameName, "Processing", msg)
				continue
			}
			// Keep non-progress lines for post-mortem and forward them to the server log
			// so users can see warnings/errors as they happen.
			appendTail(line)
			logf("TORRENT [%s]: aria2c: %s", gameName, line)
		}
	}()

	waitErr := cmd.Wait()
	<-doneCh // ensure pipe is fully drained before proceeding
	if waitErr != nil {
		tailMu.Lock()
		tail := strings.Join(tailBuf, " | ")
		tailMu.Unlock()
		if tail == "" {
			tail = "(no output captured)"
		}
		return "", fmt.Errorf("aria2c: %w — last output: %s", waitErr, tail)
	}

	// Walk the short temp dir to find the downloaded file.
	var foundPath string
	_ = filepath.Walk(aria2cDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Base(path), entry.FileName) {
			foundPath = path
			return filepath.SkipAll
		}
		return nil
	})
	if foundPath == "" {
		return "", fmt.Errorf("aria2c finished but %q not found under %s", entry.FileName, aria2cDir)
	}

	// Move the file to destDir (caller manages destDir lifetime).
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("create dest dir: %w", err)
	}
	destFile := filepath.Join(destDir, filepath.Base(foundPath))
	if err := os.Rename(foundPath, destFile); err != nil {
		return "", fmt.Errorf("move downloaded file to dest: %w", err)
	}

	logf("TORRENT [%s]: Download complete (%.0f MB)", gameName, float64(fileSize)/1048576)
	return destFile, nil
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

var (
	// Aurora host buffer reuse can concatenate a browse URL onto a title, e.g.
	// "Open Season (USA)228:8080/browse?platform=local" or with full host prefix.
	browseURLLeakPattern = regexp.MustCompile(
		`https?://[\d.]+:\d+/browse\?platform=[a-zA-Z0-9_]+|` +
			`\d{1,3}(?:\.\d{1,3}){3}:\d+/browse\?platform=[a-zA-Z0-9_]+|` +
			`\d+:\d+/browse\?platform=[a-zA-Z0-9_]+`, // leftmost of these wins; covers "228:8080/..." after title
	)
	// Aurora letter-jump can leave one ASCII letter after ")", e.g. "Open Season (USA)q"
	trailingParenJumpLetter = regexp.MustCompile(`\)([a-zA-Z])$`)
	// Some Aurora menu buffers can append tiny prompt tails to the game title, e.g.
	// "...(Add-On Content Disc)in" or "...(Add-On Content Disc)our PC".
	localQueryTailLeakPattern = regexp.MustCompile(`^[A-Za-z0-9 ]{1,24}$`)
)

// normalizeClientGameName strips junk Aurora sometimes sends on the `game` query param:
// NUL-terminated buffer tail, C0 controls (e.g. 0x08), invalid UTF-8 bytes, and accidental
// GODsend browse URL tails from Http buffer reuse so local ISO basenames still match.
func normalizeClientGameName(s string) string {
	s = strings.TrimSpace(s)
	if loc := browseURLLeakPattern.FindStringIndex(s); loc != nil {
		s = strings.TrimSpace(s[:loc[0]])
	}
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.ToValidUTF8(s, "")
	s = strings.TrimFunc(s, unicode.IsControl)
	for len(s) > 0 {
		r, sz := utf8.DecodeLastRuneInString(s)
		if r == utf8.RuneError && sz == 1 {
			s = s[:len(s)-1]
			continue
		}
		if unicode.IsControl(r) || r == '\uFFFD' {
			s = s[:len(s)-sz]
			continue
		}
		break
	}
	s = strings.ReplaceAll(s, "\u00A0", " ")
	s = strings.TrimSpace(s)
	for i := 0; i < 8 && trailingParenJumpLetter.MatchString(s); i++ {
		s = strings.TrimSpace(trailingParenJumpLetter.ReplaceAllString(s, ")"))
	}
	return s
}

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

func normalizeLocalBasename(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\uFF0E", "."))
	s = strings.ReplaceAll(s, "\u00A0", " ")
	return s
}

// findLocalISOExact matches the ISO basename (no extension) case-insensitively.
func findLocalISOExact(gameName string) string {
	entries, err := os.ReadDir(transferDir)
	if err != nil {
		return ""
	}
	want := normalizeLocalBasename(gameName)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
			continue
		}
		base := normalizeLocalBasename(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		if strings.EqualFold(base, want) {
			return filepath.Join(transferDir, e.Name())
		}
	}
	return ""
}

func findLocalISO(gameName string) string {
	gameName = strings.TrimSpace(gameName)
	if gameName == "" {
		return ""
	}
	if p := findLocalISOExact(gameName); p != "" {
		return p
	}
	if strings.Contains(gameName, " ") {
		if p := findLocalISOExact(strings.ReplaceAll(gameName, " ", "+")); p != "" {
			logf("LOCAL ISO: matched %q using space→+ fallback (query '+' vs filename)", gameName)
			return p
		}
	}
	// Fallback: tolerate short leaked alpha tails appended after an otherwise exact ISO basename.
	// This keeps local matching robust when Aurora sends e.g. "...Disc)in" or "...Disc)our PC".
	entries, _ := os.ReadDir(transferDir)
	var tailMatched string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
			continue
		}
		base := normalizeLocalBasename(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		if len(gameName) < len(base) || !strings.EqualFold(gameName[:len(base)], base) {
			continue
		}
		suffix := strings.TrimSpace(gameName[len(base):])
		if suffix == "" || !localQueryTailLeakPattern.MatchString(suffix) {
			continue
		}
		if tailMatched != "" {
			// Ambiguous fallback; avoid guessing if more than one basename could match.
			tailMatched = ""
			break
		}
		tailMatched = filepath.Join(transferDir, e.Name())
	}
	if tailMatched != "" {
		logf("LOCAL ISO: matched %q by trimming short leaked title suffix", gameName)
		return tailMatched
	}
	// Prefix fallback: take the first 60% of the query and look for an ISO whose
	// basename starts with that prefix.  Handles Lua buffer corruption that appends
	// arbitrary data (hex digits, random bytes) after the real title.
	prefixLen := len(gameName) * 60 / 100
	if prefixLen > 4 {
		prefix := strings.ToLower(normalizeLocalBasename(gameName[:prefixLen]))
		var prefixMatch string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
				continue
			}
			base := strings.ToLower(normalizeLocalBasename(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))))
			if strings.HasPrefix(base, prefix) {
				if prefixMatch != "" {
					prefixMatch = ""
					break
				}
				prefixMatch = filepath.Join(transferDir, e.Name())
			}
		}
		if prefixMatch != "" {
			logf("LOCAL ISO: matched %q using 60%% prefix fallback (%d chars)", gameName, prefixLen)
			return prefixMatch
		}
	}
	var isoNames []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
			continue
		}
		isoNames = append(isoNames, e.Name())
		if len(isoNames) >= 24 {
			break
		}
	}
	logf("LOCAL ISO miss: query=%q transferDir=%s isoFiles=%v", gameName, transferDir, isoNames)
	return ""
}

func isGameReadyLocally(gameName string) bool {
	_, err := os.Stat(filepath.Join(toolsDir, "Ready", sanitizeFilename(gameName), "godsend.ini"))
	return err == nil
}

// ==========================================
// HTTP HANDLERS
// ==========================================

// decodeMinervaName decodes HTML entities that appear in Minerva No-Intro filenames
// (e.g. &#39; → ', &amp; → &) so the display name is clean.
func decodeMinervaName(s string) string {
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	return s
}

// minervaLookupKeys returns distinct lowercased index keys for a Minerva display/file base name.
// Cached scrape data often keeps HTML entities in the raw string while /browse decodes them for
// display; indexing both forms keeps trigger lookup consistent with the UI.
func minervaLookupKeys(name string) []string {
	name = strings.TrimSpace(name)
	raw := strings.ToLower(name)
	dec := strings.ToLower(decodeMinervaName(name))
	if raw == dec {
		return []string{raw}
	}
	return []string{raw, dec}
}

func handleBrowse(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	source := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source"))) // "minerva", "ia", or "" (merged)
	logf("BROWSE: platform=%s source=%s", platform, source)

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

	// Source-specific browse — return only the requested source's list.
	minervaGameCacheMu.RLock()
	minervaCached := minervaGameCache[platform]
	minervaGameCacheMu.RUnlock()

	iaGameCacheMu.RLock()
	iaCached := iaGameCache[platform]
	iaGameCacheMu.RUnlock()

	if source == "minerva" {
		if len(minervaCached) > 0 {
			decoded := make([]string, len(minervaCached))
			for i, g := range minervaCached {
				decoded[i] = decodeMinervaName(g)
			}
			logf("BROWSE: Serving %d Minerva games for %s", len(decoded), platform)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(strings.Join(decoded, "|")))
			return
		}
		go buildMinervaCache(platform)
		logf("BROWSE: Minerva cache building for %s", platform)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "__IA_LOADING__:0/1")
		return
	}

	if source == "ia" {
		if len(iaCached) > 0 {
			logf("BROWSE: Serving %d IA games for %s", len(iaCached), platform)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(strings.Join(iaCached, "|")))
			return
		}
		go buildIAGameCache(platform)
		s := getBuildState(platform)
		loaded := atomic.LoadInt32(&s.loaded)
		total := atomic.LoadInt32(&s.total)
		if total == 0 {
			total = int32(len(iaCollections[platform]))
		}
		logf("BROWSE: IA cache building for %s %d/%d", platform, loaded, total)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "__IA_LOADING__:%d/%d", loaded, total)
		return
	}

	// No source specified — merged fallback (backward compat).
	if len(minervaCached) > 0 || len(iaCached) > 0 {
		seen := make(map[string]bool, len(minervaCached)+len(iaCached))
		merged := make([]string, 0, len(minervaCached)+len(iaCached))
		for _, g := range minervaCached {
			key := strings.ToLower(decodeMinervaName(g))
			if !seen[key] {
				seen[key] = true
				merged = append(merged, decodeMinervaName(g))
			}
		}
		for _, g := range iaCached {
			key := strings.ToLower(g)
			if !seen[key] {
				seen[key] = true
				merged = append(merged, g)
			}
		}
		logf("BROWSE: Serving %d merged games for %s (%d Minerva, %d IA)", len(merged), platform, len(minervaCached), len(iaCached))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(strings.Join(merged, "|")))
		return
	}

	// Nothing ready yet — trigger both builds and return a loading marker.
	go buildIAGameCache(platform)
	go buildMinervaCache(platform)

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

// handleCacheRefresh triggers a fresh rebuild for one platform or all platforms.
// ?platform=all     — rebuild all IA + Minerva platforms
// ?platform=xbox360 — rebuild a single IA + Minerva platform
// ?platform=minerva_xbox360 — rebuild only the Minerva cache for one platform
// ?platform=rom_nes — rebuild the ROM cache for one system
// Returns immediately; the build runs in the background.
func handleCacheRefresh(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")

	if platform == "" || platform == "all" {
		logf("CACHE REFRESH: all IA + Minerva platforms requested")
		for p := range iaCollections {
			go buildIAGameCache(p)
		}
		for p := range minervaPageURLs {
			go buildMinervaCache(p)
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

	if strings.HasPrefix(platform, "minerva_") {
		p := strings.TrimPrefix(platform, "minerva_")
		if _, ok := minervaPageURLs[p]; !ok {
			jsonError(w, 400, "Unknown Minerva platform: "+p)
			return
		}
		logf("CACHE REFRESH: Minerva %s", p)
		go buildMinervaCache(p)
		jsonSuccess(w, map[string]string{"status": "refreshing", "platform": platform})
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
	logf("CACHE REFRESH: %s (IA + Minerva)", platform)
	go buildIAGameCache(platform)
	go buildMinervaCache(platform)
	jsonSuccess(w, map[string]string{"status": "refreshing", "platform": platform})
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	gameName := normalizeClientGameName(r.URL.Query().Get("game"))
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
	installType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("install_type")))
	if installType == "" {
		installType = "god"
	}
	if installType != "god" && installType != "content" && installType != "xex" {
		installType = "god"
	}
	installTypeMap.Store(gameName, installType)
	xboxConnections.Store(gameName, XboxConnection{
		IP: xboxIP, Drive: drive, GameName: gameName,
		Platform: platform, Mode: mode, Timestamp: time.Now(),
	})
	logf("REGISTER: Xbox %s for %s (mode=%s drive=%s install=%s)", xboxIP, gameName, mode, drive, installType)
	jsonSuccess(w, map[string]string{"status": "registered", "mode": mode, "ip": xboxIP, "drive": drive})
}

func handleTrigger(w http.ResponseWriter, r *http.Request) {
	gameName := normalizeClientGameName(r.URL.Query().Get("game"))
	platform := r.URL.Query().Get("platform")
	source := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source"))) // "minerva", "ia", or ""
	if gameName == "" {
		jsonError(w, 400, "Missing game parameter")
		return
	}
	if platform == "" {
		platform = "xbox360"
	}
	installType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("install_type")))
	if installType == "" {
		installType = "god"
	}
	if installType != "god" && installType != "content" && installType != "xex" {
		installType = "god"
	}
	installTypeMap.Store(gameName, installType)
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
			logf("LOCAL UNAVAILABLE: no .iso match for %q in %s (check URL encoding for & + # in filenames)", gameName, transferDir)
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

	// Minerva — check before IA (source priority: local → Minerva → Internet Archive)
	// Skipped when source=="ia" (user explicitly chose Internet Archive).
	if source != "ia" {
		if _, hasMinervaPage := minervaPageURLs[platform]; hasMinervaPage {
			if mEntry, ok := findMinervaEntry(gameName, platform); ok {
				logf("TRIGGER: Minerva source for '%s' (%s)", gameName, platform)
				switch platform {
				case "digital", "xbla", "dlc", "xblig":
					launcher(func() { processMinervaDigital(gameName, mEntry, platform) })
				case "games":
					launcher(func() { processMinervaGenericGame(gameName, mEntry) })
				default: // xbox360, xbox
					launcher(func() { processMinervaGame(gameName, mEntry, platform) })
				}
				jsonSuccess(w, map[string]string{"status": "triggered", "source": "minerva"})
				return
			}
			if source == "minerva" {
				logStatus(gameName, "Error", "Not found in Minerva Archive")
				jsonSuccess(w, map[string]string{"status": "minerva_unavailable", "message": "Game not found in Minerva Archive."})
				return
			}
		}
	}

	// source=="minerva" but platform has no Minerva page — treat as not found
	if source == "minerva" {
		logStatus(gameName, "Error", "Not found in Minerva Archive")
		jsonSuccess(w, map[string]string{"status": "minerva_unavailable", "message": "Game not found in Minerva Archive."})
		return
	}

	// Internet Archive — fallback when Minerva has no match, or source=="ia"
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
	gameName := normalizeClientGameName(r.URL.Query().Get("game"))
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

// handleDiscInfo probes a local ISO in the Transfer folder and returns disc
// metadata along with a compat-table install recommendation.
func handleDiscInfo(w http.ResponseWriter, r *http.Request) {
	gameName := normalizeClientGameName(r.URL.Query().Get("game"))
	if gameName == "" {
		jsonError(w, 400, "Missing game parameter")
		return
	}
	iso := findLocalISO(gameName)
	if iso != "" {
		info, err := utils.ProbeISODiscInfo(iso)
		if err != nil {
			jsonError(w, 500, fmt.Sprintf("Disc probe failed: %v", err))
			return
		}
		rec := discCompat(info.TitleID, info.DiscNumber)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"disc_number":    info.DiscNumber,
			"disc_count":     info.DiscCount,
			"title_id":       fmt.Sprintf("%08X", info.TitleID),
			"recommendation": rec.installType,
			"notes":          rec.notes,
			"probed":         true,
		})
		return
	}
	// No Transfer-folder ISO yet (typical for IA-only installs) — filename-based hint for Disc 2+.
	if !isMultiDiscGameName(gameName) {
		jsonError(w, 404, "No local ISO found for this game")
		return
	}
	tid := guessTitleIDFromMultiDiscName(gameName)
	rec := discCompat(tid, 2)
	note := rec.notes
	if tid == 0 {
		note = note + " (Title ID unknown from name — optional: copy ISO to PC Transfer for an exact probe)"
	} else {
		note = note + " (Title ID guessed from game name)"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"disc_number":    2,
		"disc_count":     0,
		"title_id":       fmt.Sprintf("%08X", tid),
		"recommendation": rec.installType,
		"notes":          note,
		"probed":         false,
	})
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
	game := normalizeClientGameName(r.URL.Query().Get("game"))
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
	// Also cancel any pending FTP job for this game
	for _, job := range loadAllPendingFTPJobs() {
		if job.GameName == game {
			deletePendingFTPJob(job.ID)
			go func(j PendingFTPJob) {
				time.Sleep(3 * time.Second)
				os.RemoveAll(j.SourceDir)
				if j.GameDir != "" {
					os.RemoveAll(j.GameDir)
				}
			}(job)
		}
	}
	logf("QUEUE: removed job %q", game)
	jsonSuccess(w, map[string]string{"status": "removed", "game": game})
}

func handleDataStatus(w http.ResponseWriter, r *http.Request) {
	var activeJobs int
	jobQueue.Range(func(k, v interface{}) bool {
		gs := v.(GameStatus)
		if gs.State == "Processing" || gs.State == "Pending FTP" {
			activeJobs++
		}
		return true
	})
	pendingJobs := loadAllPendingFTPJobs()
	pendingFTPJobs := len(pendingJobs)

	// Calculate local data size (Ready/ + Temp/ directories)
	var localDataBytes int64
	for _, dir := range []string{"Ready", "Temp"} {
		filepath.Walk(filepath.Join(toolsDir, dir), func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				localDataBytes += info.Size()
			}
			return nil
		})
	}
	// Also count pending_ftp source dirs
	for _, job := range pendingJobs {
		filepath.Walk(job.SourceDir, func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				localDataBytes += info.Size()
			}
			return nil
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"active_jobs":      activeJobs,
		"pending_ftp_jobs": pendingFTPJobs,
		"local_data_bytes": localDataBytes,
		"local_data_mb":    localDataBytes / 1048576,
	})
}

func handleDataClear(w http.ResponseWriter, r *http.Request) {
	// Clear all job statuses
	jobQueue.Range(func(k, v interface{}) bool {
		suppressedJobs.Store(k, true)
		jobQueue.Delete(k)
		return true
	})
	// Clear pending FTP jobs (goroutines will detect suppression and exit)
	pendingJobs := loadAllPendingFTPJobs()
	for _, job := range pendingJobs {
		suppressedJobs.Store(job.GameName, true)
		deletePendingFTPJob(job.ID)
		go func(j PendingFTPJob) {
			time.Sleep(2 * time.Second)
			os.RemoveAll(j.SourceDir)
			if j.GameDir != "" {
				os.RemoveAll(j.GameDir)
			}
		}(job)
	}
	// Clear Ready/ and Temp/ directories
	os.RemoveAll(filepath.Join(toolsDir, "Ready"))
	os.RemoveAll(filepath.Join(toolsDir, "Temp"))
	os.MkdirAll(filepath.Join(toolsDir, "Ready"), 0755)
	os.MkdirAll(filepath.Join(toolsDir, "Temp"), 0755)

	jsonSuccess(w, map[string]string{"status": "cleared"})
}

func handleServerConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"default_drive": defaultXboxDrive,
	})
}

func handleDebug(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h2>GODSend Debug v7.0-IA</h2><p>Server: %s:%s</p>", serverIP, serverPort)
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
	if err = c.Login(ftpUsername, ftpPassword); err != nil {
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

	installType := lookupInstallType(gameName)
	if installType == "xex" {
		xexDir := filepath.Join(toolsDir, "Temp", safeName+"_xex")
		os.RemoveAll(xexDir)
		logStatus(gameName, "Processing", "Extracting XEX layout from ISO...")
		if err := utils.ExtractXEXFolderFromISO(isoPath, xexDir); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("XEX from ISO: %v", err))
			return
		}
		defer os.RemoveAll(xexDir)

		gameDir := filepath.Join(toolsDir, "Ready", safeName)
		os.MkdirAll(gameDir, 0755)
		folderName := safeName
		if xboxConn != nil && xboxConn.Mode == "ftp" {
			if err := ftpTransferXEX(xexDir, folderName, xboxConn, gameName); err != nil {
				logf("FTP: initial XEX transfer failed for %s: %v — scheduling for retry", gameName, err)
				job := PendingFTPJob{
					ID:         sanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
					GameName:   gameName,
					Type:       "xex",
					SourceDir:  xexDir,
					GameDir:    gameDir,
					XboxIP:     xboxConn.IP,
					Drive:      xboxConn.Drive,
					FolderName: folderName,
					CreatedAt:  time.Now(),
				}
				schedulePendingFTP(job)
				return
			}
			os.RemoveAll(gameDir)
			logStatus(gameName, "Ready", "FTP Transfer Complete!")
		} else {
			partName := fmt.Sprintf("%s_Part1.7z", safeName)
			if err := utils.CreateZipFromDir(xexDir, filepath.Join(gameDir, partName)); err != nil {
				logStatus(gameName, "Error", fmt.Sprintf("Archive XEX: %v", err))
				return
			}
			gamePartsMap.Store(gameName, []string{partName})
			updateGameINI_XEX(gameDir, gameName, folderName, partName)
			logStatus(gameName, "Ready", "Ready to Install")
		}
		if gs, ok := jobQueue.Load(gameName); ok && gs.(GameStatus).State == "Ready" {
			if err := os.Remove(isoPath); err == nil {
				logf("Cleanup: deleted source ISO: %s", filepath.Base(isoPath))
			}
		}
		logf("=== Complete (local XEX from ISO): %s ===", gameName)
		return
	}
	if installType == "content" {
		processContentInstallFromISO(gameName, safeName, isoPath, xboxConn)
		if gs, ok := jobQueue.Load(gameName); ok && gs.(GameStatus).State == "Ready" {
			if err := os.Remove(isoPath); err == nil {
				logf("Cleanup: deleted source ISO: %s", filepath.Base(isoPath))
			}
		}
		return
	}

	gameDir := filepath.Join(toolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	logStatus(gameName, "Processing", "Converting ISO to GOD...")
	godDir := filepath.Join(toolsDir, "Temp", safeName+"_GOD")
	os.MkdirAll(godDir, 0755)
	if err := utils.RunIso2GodNative(isoPath, godDir, iso2GodResolveDisplayTitle); err != nil {
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

	installType := lookupInstallType(gameName)

	// XEX: full archive extract, find folder containing default.xex (same idea as Games Archive).
	if installType == "xex" {
		extDir := filepath.Join(toolsDir, "Temp", safeName+"_ext")
		os.RemoveAll(extDir)
		logStatus(gameName, "Processing", "Extracting archive for XEX...")
		if err := utils.ExtractArchive(archivePath, extDir); err != nil {
			os.Remove(archivePath)
			logf("ERROR [%s]: XEX extract failed: %v", gameName, err)
			logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
			return
		}
		os.Remove(archivePath)
		defer os.RemoveAll(extDir)

		xexFolder := findXEXFolder(extDir)
		if xexFolder == "" {
			logStatus(gameName, "Error", "No default.xex in archive — XEX needs a loose folder rip. Use GOD or DLC for ISO-only Redump releases.")
			return
		}
		folderName := filepath.Base(xexFolder)
		logStatus(gameName, "Processing", fmt.Sprintf("XEX folder: %s", folderName))
		if xboxConn != nil && xboxConn.Mode == "ftp" {
			if err := ftpTransferXEX(xexFolder, folderName, xboxConn, gameName); err != nil {
				logf("FTP: initial XEX transfer failed for %s: %v — scheduling for retry", gameName, err)
				job := PendingFTPJob{
					ID:         sanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
					GameName:   gameName,
					Type:       "xex",
					SourceDir:  xexFolder,
					GameDir:    gameDir,
					XboxIP:     xboxConn.IP,
					Drive:      xboxConn.Drive,
					FolderName: folderName,
					CreatedAt:  time.Now(),
				}
				schedulePendingFTP(job)
			} else {
				os.RemoveAll(gameDir)
				logStatus(gameName, "Ready", "FTP Transfer Complete!")
			}
		} else {
			partName := fmt.Sprintf("%s_Part1.7z", safeName)
			if err := utils.CreateZipFromDir(xexFolder, filepath.Join(gameDir, partName)); err != nil {
				logStatus(gameName, "Error", fmt.Sprintf("Archive XEX: %v", err))
				return
			}
			gamePartsMap.Store(gameName, []string{partName})
			updateGameINI_XEX(gameDir, gameName, folderName, partName)
			logStatus(gameName, "Ready", "Ready to Install")
		}
		logf("=== Complete (Redump XEX): %s ===", gameName)
		return
	}

	logStatus(gameName, "Processing", "Extracting ISO...")
	isoPath, err := utils.ExtractISO(archivePath, safeName, filepath.Join(toolsDir, "Temp"))
	os.Remove(archivePath)
	if err != nil {
		logf("ERROR [%s]: Extract failed: %v", gameName, err)
		logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	if installType == "content" {
		processContentInstallFromISO(gameName, safeName, isoPath, xboxConn)
		os.Remove(isoPath)
		return
	}

	logStatus(gameName, "Processing", "Converting to GOD...")
	godDir := filepath.Join(toolsDir, "Temp", safeName+"_GOD")
	os.MkdirAll(godDir, 0755)
	if err := utils.RunIso2GodNative(isoPath, godDir, iso2GodResolveDisplayTitle); err != nil {
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
	resolvedName := services.LookupTitleName(titleID) // may be empty — callers fall back gracefully

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		logStatus(gameName, "Processing", "FTP Transfer starting...")
		if err := ftpTransferGame(godDir, xboxConn, gameName, titleID, mediaID, resolvedName); err != nil {
			logf("FTP: initial transfer failed for %s: %v — scheduling for retry", gameName, err)
			job := PendingFTPJob{
				ID:           sanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
				GameName:     gameName,
				Type:         "god",
				SourceDir:    godDir,
				GameDir:      gameDir,
				XboxIP:       xboxConn.IP,
				Drive:        xboxConn.Drive,
				TitleID:      titleID,
				MediaID:      mediaID,
				ResolvedName: resolvedName,
				CreatedAt:    time.Now(),
			}
			schedulePendingFTP(job)
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
// MINERVA PROCESSING FUNCTIONS
// ==========================================

// processMinervaGame downloads and processes an Xbox 360 / Xbox disc ISO from Minerva.
// The Minerva file is a .zip wrapping the Redump disc image; pipeline is identical to processGame.
func processMinervaGame(gameName string, entry MinervaEntry, platform string) {
	logf("=== Minerva ISO: %s (%s) ===", gameName, platform)
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

	torrentDir := filepath.Join(toolsDir, "Temp", safeName+"_torrent")
	os.MkdirAll(torrentDir, 0755)
	defer os.RemoveAll(torrentDir)
	logf("Minerva Torrent: %s → %s", gameName, entry.FileName)
	logStatus(gameName, "Processing", "Starting Minerva torrent download...")
	archivePath, err := downloadViaTorrent(platform, torrentDir, gameName, entry)
	if err != nil {
		logf("ERROR [%s]: Minerva torrent failed: %v", gameName, err)
		logStatus(gameName, "Error", fmt.Sprintf("Minerva torrent: %v", err))
		return
	}

	installType := lookupInstallType(gameName)

	if installType == "xex" {
		extDir := filepath.Join(toolsDir, "Temp", safeName+"_mext")
		os.RemoveAll(extDir)
		logStatus(gameName, "Processing", "Extracting archive for XEX...")
		if err := utils.ExtractArchive(archivePath, extDir); err != nil {
			os.Remove(archivePath)
			logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
			return
		}
		os.Remove(archivePath)
		defer os.RemoveAll(extDir)
		xexFolder := findXEXFolder(extDir)
		if xexFolder == "" {
			logStatus(gameName, "Error", "No default.xex found in Minerva archive")
			return
		}
		folderName := filepath.Base(xexFolder)
		if xboxConn != nil && xboxConn.Mode == "ftp" {
			if err := ftpTransferXEX(xexFolder, folderName, xboxConn, gameName); err != nil {
				logf("FTP: initial XEX transfer failed for %s: %v — scheduling for retry", gameName, err)
				job := PendingFTPJob{
					ID:         sanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
					GameName:   gameName,
					Type:       "xex",
					SourceDir:  xexFolder,
					GameDir:    gameDir,
					XboxIP:     xboxConn.IP,
					Drive:      xboxConn.Drive,
					FolderName: folderName,
					CreatedAt:  time.Now(),
				}
				schedulePendingFTP(job)
			} else {
				os.RemoveAll(gameDir)
				logStatus(gameName, "Ready", "FTP Transfer Complete!")
			}
		} else {
			partName := fmt.Sprintf("%s_Part1.7z", safeName)
			if err := utils.CreateZipFromDir(xexFolder, filepath.Join(gameDir, partName)); err != nil {
				logStatus(gameName, "Error", fmt.Sprintf("Archive XEX: %v", err))
				return
			}
			gamePartsMap.Store(gameName, []string{partName})
			updateGameINI_XEX(gameDir, gameName, folderName, partName)
			logStatus(gameName, "Ready", "Ready to Install")
		}
		logf("=== Complete (Minerva XEX): %s ===", gameName)
		return
	}

	logStatus(gameName, "Processing", "Extracting ISO...")
	isoPath, err := utils.ExtractISO(archivePath, safeName, filepath.Join(toolsDir, "Temp"))
	os.Remove(archivePath)
	if err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	if installType == "content" {
		processContentInstallFromISO(gameName, safeName, isoPath, xboxConn)
		os.Remove(isoPath)
		return
	}

	logStatus(gameName, "Processing", "Converting to GOD...")
	godDir := filepath.Join(toolsDir, "Temp", safeName+"_MGOD")
	os.MkdirAll(godDir, 0755)
	if err := utils.RunIso2GodNative(isoPath, godDir, iso2GodResolveDisplayTitle); err != nil {
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
	logf("Minerva ISO: TitleID=%s MediaID=%s", titleID, mediaID)
	finalizeGOD(gameName, safeName, gameDir, godDir, titleID, mediaID, xboxConn)
}

// processMinervaGenericGame handles the "games" platform from Minerva (Non-Redump mixed archives).
func processMinervaGenericGame(gameName string, entry MinervaEntry) {
	logf("=== Minerva Generic: %s ===", gameName)
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

	torrentDir := filepath.Join(toolsDir, "Temp", safeName+"_torrent")
	os.MkdirAll(torrentDir, 0755)
	defer os.RemoveAll(torrentDir)
	logStatus(gameName, "Processing", "Starting Minerva torrent download...")
	archivePath, err := downloadViaTorrent("games", torrentDir, gameName, entry)
	if err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Minerva torrent: %v", err))
		return
	}

	logStatus(gameName, "Processing", "Extracting archive...")
	extDir := filepath.Join(toolsDir, "Temp", safeName+"_mgext")
	os.RemoveAll(extDir)
	defer os.RemoveAll(extDir)
	if err := utils.ExtractArchive(archivePath, extDir); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	// Try ISO pipeline first
	isoPath := findFileByExt(extDir, ".iso")
	if isoPath != "" {
		logStatus(gameName, "Processing", "Converting to GOD...")
		godDir := filepath.Join(toolsDir, "Temp", safeName+"_MGGOD")
		os.MkdirAll(godDir, 0755)
		if err := utils.RunIso2GodNative(isoPath, godDir, iso2GodResolveDisplayTitle); err != nil {
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
		logf("=== Complete (Minerva Generic/ISO): %s ===", gameName)
		return
	}

	// Fallback: look for a XEX folder
	xexFolder := findXEXFolder(extDir)
	if xexFolder == "" {
		logStatus(gameName, "Error", "No ISO or XEX found in Minerva archive")
		return
	}
	folderName := filepath.Base(xexFolder)
	if xboxConn != nil && xboxConn.Mode == "ftp" {
		if err := ftpTransferXEX(xexFolder, folderName, xboxConn, gameName); err != nil {
			logf("FTP: initial XEX transfer failed for %s: %v — scheduling for retry", gameName, err)
			job := PendingFTPJob{
				ID:         sanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
				GameName:   gameName,
				Type:       "xex",
				SourceDir:  xexFolder,
				GameDir:    gameDir,
				XboxIP:     xboxConn.IP,
				Drive:      xboxConn.Drive,
				FolderName: folderName,
				CreatedAt:  time.Now(),
			}
			schedulePendingFTP(job)
		} else {
			os.RemoveAll(gameDir)
			logStatus(gameName, "Ready", "FTP Transfer Complete!")
		}
	} else {
		partName := fmt.Sprintf("%s_Part1.7z", safeName)
		if err := utils.CreateZipFromDir(xexFolder, filepath.Join(gameDir, partName)); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("Archive XEX: %v", err))
			return
		}
		gamePartsMap.Store(gameName, []string{partName})
		updateGameINI_XEX(gameDir, gameName, folderName, partName)
		logStatus(gameName, "Ready", "Ready to Install")
	}
	logf("=== Complete (Minerva Generic/XEX): %s ===", gameName)
}

// processMinervaDigital handles XBLA / DLC / XBLIG content from Minerva No-Intro Digital.
func processMinervaDigital(gameName string, entry MinervaEntry, platform string) {
	logf("=== Minerva Digital: %s (%s) ===", gameName, platform)
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

	torrentDir := filepath.Join(toolsDir, "Temp", safeName+"_torrent")
	os.MkdirAll(torrentDir, 0755)
	defer os.RemoveAll(torrentDir)
	logStatus(gameName, "Processing", "Starting Minerva torrent download...")
	archivePath, err := downloadViaTorrent(platform, torrentDir, gameName, entry)
	if err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Minerva torrent: %v", err))
		return
	}

	logStatus(gameName, "Processing", "Extracting...")
	extDir := filepath.Join(toolsDir, "Temp", safeName+"_mdext")
	os.RemoveAll(extDir)
	defer os.RemoveAll(extDir)
	if err := utils.ExtractArchive(archivePath, extDir); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	var contentFile, titleID, typeDir string
	filepath.Walk(extDir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() || i.Size() < 0x368 {
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
		logStatus(gameName, "Error", "No valid Xbox content found in Minerva archive")
		return
	}
	logf("Minerva Digital: TitleID=%s Type=%s", titleID, typeDir)
	finalName := filepath.Base(contentFile)

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		drive := strings.TrimSuffix(xboxConn.Drive, ":")
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
			os.RemoveAll(gameDir)
			logStatus(gameName, "Ready", "FTP Transfer Complete!")
		}
	} else {
		relPath := fmt.Sprintf("Content\\0000000000000000\\%s\\%s\\", titleID, typeDir)
		if err := copyFileBuffered(contentFile, filepath.Join(gameDir, finalName)); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("Copy: %v", err))
		} else {
			updateGameINI_Raw(gameDir, gameName, finalName, relPath, "")
			logStatus(gameName, "Ready", "Ready to Install")
		}
	}
	logf("=== Complete (Minerva Digital): %s ===", gameName)
}

// ==========================================
// CONTENT INSTALL (Disc 2+ DLC path)
// ==========================================

// processContentInstallFromISO extracts the secondary-disc content from an ISO
// and either FTPs it to the Xbox or packages it for HTTP delivery.
// Install path on Xbox: {Drive}\Content\0000000000000000\{TitleID}\00000002\
func processContentInstallFromISO(gameName, safeName, isoPath string, xboxConn *XboxConnection) {
	logf("=== Content install: %s ===", gameName)

	logStatus(gameName, "Processing", "Reading disc info...")
	info, err := utils.ProbeISODiscInfo(isoPath)
	if err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Disc probe: %v", err))
		return
	}
	titleID := fmt.Sprintf("%08X", info.TitleID)
	if isContentDiscPlaceholderTitleID(info.TitleID) {
		// Probe the content packages embedded in the disc for the real Title ID.
		// STFS/CON files carry the parent game's Title ID at header offset 0x0360,
		// which is always correct regardless of game name.
		if probed, err := utils.ProbeContentPackageTitleID(isoPath, info); err == nil && probed != 0 {
			logf("Content install: placeholder TitleID %s resolved to %08X from content packages", titleID, probed)
			titleID = fmt.Sprintf("%08X", probed)
		} else if guessed := guessTitleIDFromMultiDiscName(gameName); guessed != 0 {
			logf("Content install: placeholder TitleID %s overridden to %08X from game name", titleID, guessed)
			titleID = fmt.Sprintf("%08X", guessed)
		} else {
			logf("Content install: WARNING — TitleID %s is a known placeholder; could not resolve parent title from content packages or game name %q — content may install to wrong folder", titleID, gameName)
		}
	}
	logf("Content install: TitleID=%s disc=%d/%d", titleID, info.DiscNumber, info.DiscCount)

	logStatus(gameName, "Processing", "Extracting content files from ISO...")
	contentDir := filepath.Join(toolsDir, "Temp", safeName+"_content")
	os.RemoveAll(contentDir)
	os.MkdirAll(contentDir, 0755)
	if err := utils.ExtractXDVDFSContentToDir(isoPath, contentDir, info); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Content extract: %v", err))
		os.RemoveAll(contentDir)
		return
	}

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		logStatus(gameName, "Processing", "FTP Transfer starting...")
		if err := ftpTransferContent(contentDir, xboxConn, gameName, titleID); err != nil {
			logf("FTP: initial content transfer failed for %s: %v — scheduling for retry", gameName, err)
			gameDir := filepath.Join(toolsDir, "Ready", safeName)
			job := PendingFTPJob{
				ID:        sanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
				GameName:  gameName,
				Type:      "content",
				SourceDir: contentDir,
				GameDir:   gameDir,
				XboxIP:    xboxConn.IP,
				Drive:     xboxConn.Drive,
				TitleID:   titleID,
				CreatedAt: time.Now(),
			}
			schedulePendingFTP(job)
			return
		}
		os.RemoveAll(contentDir)
		logStatus(gameName, "Ready", "FTP Transfer Complete!")
	} else {
		gameDir := filepath.Join(toolsDir, "Ready", safeName)
		os.MkdirAll(gameDir, 0755)

		logStatus(gameName, "Processing", "Packaging content for transfer...")
		partName := safeName + "_Part1.7z"
		if err := utils.CreateZipFromDir(contentDir, filepath.Join(gameDir, partName)); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("Archive: %v", err))
			os.RemoveAll(contentDir)
			return
		}
		os.RemoveAll(contentDir)
		gamePartsMap.Store(gameName, []string{partName})
		relPath := fmt.Sprintf("Content\\0000000000000000\\%s\\00000002\\", titleID)
		updateGameINI_Content(gameDir, gameName, titleID, partName, relPath)
		logStatus(gameName, "Ready", "Ready to Install")
	}
	logf("=== Complete (Content): %s ===", gameName)
}

// ftpTransferContent FTPs extracted content files to
// {Drive}/Content/0000000000000000/{titleID}/00000002/ on the Xbox.
func ftpTransferContent(contentDir string, conn *XboxConnection, gameName, titleID string) error {
	fc, err := connectWithRetry(conn.IP)
	if err != nil {
		return err
	}
	defer fc.Quit()

	drive := strings.TrimSuffix(conn.Drive, ":")
	base := fmt.Sprintf("/%s/Content/0000000000000000/%s/00000002", drive, titleID)
	logf("FTP Content Dest: %s", base)
	ftpMkdirAll(fc, base)

	var totalFiles int
	var totalSize int64
	filepath.Walk(contentDir, func(p string, i os.FileInfo, e error) error {
		if e == nil && !i.IsDir() {
			totalFiles++
			totalSize += i.Size()
		}
		return nil
	})

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

// updateGameINI_Content writes a manifest for secondary-disc content installs.
func updateGameINI_Content(gameDir, gameName, titleID, partFile, relPath string) {
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
	fmt.Fprintf(w, "[%s]\ntype=content\ntitleid=%s\npath=%s\ndataurl=%s\n",
		gameName, titleID, relPath, enc(partFile))
	w.Flush()
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
	if err := utils.ExtractArchive(archivePath, extDir); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	installType := lookupInstallType(gameName)

	isoPath := findFileByExt(extDir, ".iso")
	xexFolder := findXEXFolder(extDir)

	// XEX: loose-folder Game Archives layout (user must pick this when the RAR has no ISO).
	if installType == "xex" {
		if xexFolder == "" {
			logStatus(gameName, "Error", "XEX install needs a loose game folder in the archive. Try GOD (ISO) or DLC (Disc 2 content ISO).")
			return
		}
		folderName := filepath.Base(xexFolder)
		logStatus(gameName, "Processing", fmt.Sprintf("XEX folder: %s", folderName))
		if xboxConn != nil && xboxConn.Mode == "ftp" {
			if err := ftpTransferXEX(xexFolder, folderName, xboxConn, gameName); err != nil {
				logf("FTP: initial XEX transfer failed for %s: %v — scheduling for retry", gameName, err)
				job := PendingFTPJob{
					ID:         sanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
					GameName:   gameName,
					Type:       "xex",
					SourceDir:  xexFolder,
					GameDir:    gameDir,
					XboxIP:     xboxConn.IP,
					Drive:      xboxConn.Drive,
					FolderName: folderName,
					CreatedAt:  time.Now(),
				}
				schedulePendingFTP(job)
			} else {
				os.RemoveAll(gameDir)
				logStatus(gameName, "Ready", "FTP Transfer Complete!")
			}
		} else {
			partName := fmt.Sprintf("%s_Part1.7z", safeName)
			if err := utils.CreateZipFromDir(xexFolder, filepath.Join(gameDir, partName)); err != nil {
				logStatus(gameName, "Error", fmt.Sprintf("Archive XEX: %v", err))
				return
			}
			gamePartsMap.Store(gameName, []string{partName})
			updateGameINI_XEX(gameDir, gameName, folderName, partName)
			logStatus(gameName, "Ready", "Ready to Install")
		}
		return
	}

	// DLC / Content: XDVDFS content tree → Content\...\00000002\ (same as Redump online flow).
	if installType == "content" {
		if isoPath == "" {
			logStatus(gameName, "Error", "DLC/content install needs an ISO. Pick XEX if this release is a loose-folder rip.")
			return
		}
		processContentInstallFromISO(gameName, safeName, isoPath, xboxConn)
		return
	}

	// GOD (default): ISO → Games on Demand.
	if isoPath != "" {
		logStatus(gameName, "Processing", "ISO detected, converting to GOD...")
		godDir := filepath.Join(toolsDir, "Temp", safeName+"_GOD")
		os.MkdirAll(godDir, 0755)
		if err := utils.RunIso2GodNative(isoPath, godDir, iso2GodResolveDisplayTitle); err != nil {
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
		return
	}

	if xexFolder != "" {
		logStatus(gameName, "Error", "No ISO in archive. Choose Install method: XEX for this folder layout, or use a Redump-style ISO release.")
		return
	}
	logStatus(gameName, "Error", "No ISO or XEX content found in archive")
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
// All platforms respect the user's drive selection.
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
	if err := utils.ExtractArchive(archivePath, extDir); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	var contentFile, titleID, typeDir string
	filepath.Walk(extDir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() || i.Size() < 0x368 {
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

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		drive := strings.TrimSuffix(xboxConn.Drive, ":")
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
			updateGameINI_Raw(gameDir, gameName, finalName, relPath, "")
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
	base := fmt.Sprintf("/%s/GOD/%s - %s", drive, folderID, titleID)
	logf("FTP GOD Dest: %s", base)
	ftpMkdirAll(fc, base)

	contentDir := filepath.Join(godDir, titleID)
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
// PENDING FTP QUEUE
// ==========================================

// PendingFTPJob describes a game transfer that should be retried indefinitely.
type PendingFTPJob struct {
	ID           string    `json:"id"`
	GameName     string    `json:"game_name"`
	Type         string    `json:"type"`         // "god", "xex", "content"
	SourceDir    string    `json:"source_dir"`   // directory with files to upload
	GameDir      string    `json:"game_dir"`     // Ready/ dir to remove on success (may be "")
	XboxIP       string    `json:"xbox_ip"`
	Drive        string    `json:"drive"`
	TitleID      string    `json:"title_id,omitempty"`
	MediaID      string    `json:"media_id,omitempty"`
	ResolvedName string    `json:"resolved_name,omitempty"`
	FolderName   string    `json:"folder_name,omitempty"` // xex only
	CreatedAt    time.Time `json:"created_at"`
}

func pendingFTPJobPath(id string) string {
	return filepath.Join(pendingFTPDir, id+".json")
}

func savePendingFTPJob(job PendingFTPJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return os.WriteFile(pendingFTPJobPath(job.ID), data, 0644)
}

func deletePendingFTPJob(id string) {
	os.Remove(pendingFTPJobPath(id))
}

func loadAllPendingFTPJobs() []PendingFTPJob {
	entries, err := os.ReadDir(pendingFTPDir)
	if err != nil {
		return nil
	}
	var jobs []PendingFTPJob
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pendingFTPDir, e.Name()))
		if err != nil {
			continue
		}
		var job PendingFTPJob
		if err := json.Unmarshal(data, &job); err != nil {
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs
}

// executePendingFTPJob runs the actual FTP transfer for a pending job.
// Returns nil on success. On success, removes source files.
func executePendingFTPJob(job PendingFTPJob) error {
	conn := &XboxConnection{IP: job.XboxIP, Drive: job.Drive}
	switch job.Type {
	case "god":
		if err := ftpTransferGame(job.SourceDir, conn, job.GameName, job.TitleID, job.MediaID, job.ResolvedName); err != nil {
			return err
		}
	case "xex":
		if err := ftpTransferXEX(job.SourceDir, job.FolderName, conn, job.GameName); err != nil {
			return err
		}
	case "content":
		if err := ftpTransferContent(job.SourceDir, conn, job.GameName, job.TitleID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown pending FTP job type: %s", job.Type)
	}
	// Clean up on success
	os.RemoveAll(job.SourceDir)
	if job.GameDir != "" {
		os.RemoveAll(job.GameDir)
	}
	return nil
}

// retryFTPJobForever retries a pending FTP job indefinitely until it succeeds or is cancelled.
func retryFTPJobForever(job PendingFTPJob) {
	backoff := 30 * time.Second
	const maxBackoff = 5 * time.Minute
	logf("FTP PENDING: %s — will retry every %s", job.GameName, backoff)
	logStatus(job.GameName, "Pending FTP", "Xbox unreachable — will retry automatically when FTP comes back online")

	for {
		time.Sleep(backoff)

		// Stop if job was removed from queue
		if _, suppressed := suppressedJobs.Load(job.GameName); suppressed {
			logf("FTP PENDING: %s — cancelled, removing", job.GameName)
			deletePendingFTPJob(job.ID)
			os.RemoveAll(job.SourceDir)
			if job.GameDir != "" {
				os.RemoveAll(job.GameDir)
			}
			return
		}

		logf("FTP PENDING: Retrying %s...", job.GameName)
		logStatus(job.GameName, "Processing", "FTP retry: reconnecting to Xbox...")
		if err := executePendingFTPJob(job); err != nil {
			logf("FTP PENDING: Retry failed for %s: %v", job.GameName, err)
			logStatus(job.GameName, "Pending FTP", fmt.Sprintf("FTP retry failed — will try again: %v", err))
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// Success
		deletePendingFTPJob(job.ID)
		logStatus(job.GameName, "Ready", "FTP Transfer Complete!")
		logf("=== FTP PENDING Complete: %s ===", job.GameName)
		return
	}
}

// schedulePendingFTP saves the job to disk and starts retrying in the background.
// Call this when an initial FTP transfer attempt has failed.
func schedulePendingFTP(job PendingFTPJob) {
	if err := savePendingFTPJob(job); err != nil {
		logf("FTP PENDING: Failed to save job for %s: %v", job.GameName, err)
	}
	go retryFTPJobForever(job)
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

// iso2GodResolveDisplayTitle maps Title ID → display string for the LIVE CON
// header UTF-16 title slots (same chain as services.LookupTitleName).
func iso2GodResolveDisplayTitle(titleID uint32) string {
	return services.LookupTitleName(fmt.Sprintf("%08X", titleID))
}

// godFolderName returns the directory name to use inside the GOD folder.
// Format: "<TitleName> - <TitleID>" if services.LookupTitleName resolves the name,
// otherwise falls back to "Title - <TitleID>" to preserve old behaviour.
func godFolderName(titleID string) string {
	if name := services.LookupTitleName(titleID); name != "" {
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
	contentDir := filepath.Join(src, titleID)
	err = filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(contentDir, path)
		if curSize+info.Size() > MaxPartSize && curSize > 0 {
			pname := fmt.Sprintf("%s_Part%d.7z", safeName, pn)
			if err := utils.CreateZipFromDir(cpd, filepath.Join(dest, pname)); err != nil {
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
		if err := utils.CreateZipFromDir(cpd, filepath.Join(dest, pname)); err != nil {
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
	// New layout (iso2god-rs): godDir/{TitleID}/{00007000|00005000}/{CON name} + {CON}.data/Data*
	// Legacy: godDir/{TitleID}/Data* + CON file flat in TitleID folder.
	entries, err := os.ReadDir(godDir)
	if err != nil {
		return "", "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		titleID := e.Name()
		titlePath := filepath.Join(godDir, titleID)
		subs, err := os.ReadDir(titlePath)
		if err != nil {
			continue
		}
		for _, s := range subs {
			if !s.IsDir() {
				continue
			}
			ct := s.Name()
			if len(ct) != 8 || !isHexString(ct) {
				continue
			}
			ctPath := filepath.Join(titlePath, ct)
			ctEntries, err := os.ReadDir(ctPath)
			if err != nil {
				continue
			}
			for _, f := range ctEntries {
				if f.IsDir() {
					continue
				}
				n := f.Name()
				if strings.HasPrefix(strings.ToUpper(n), "DATA") {
					continue
				}
				return titleID, n, nil
			}
		}
		for _, s := range subs {
			if s.IsDir() {
				continue
			}
			n := s.Name()
			if strings.HasPrefix(strings.ToUpper(n), "DATA") {
				continue
			}
			return titleID, n, nil
		}
	}
	return "", "", fmt.Errorf("GOD structure not found")
}

func isHexString(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'A' && c <= 'F':
		case c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
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

// downloadWithProgress downloads urlStr to dest. For Internet Archive URLs it uses a
// Gopeed-style segment queue (fixed-size ranges, worker pool) when Range is supported.
func downloadWithProgress(urlStr, dest, name, ref string) error {
	isIA := strings.Contains(strings.ToLower(urlStr), "archive.org")
	if isIA && iaDownloadMaxParallel > 1 {
		size, rangeOK, err := iaProbeDownload(urlStr, ref)
		if err != nil {
			logf("WARN [%s]: probe failed (%v), using single stream", name, err)
		} else if rangeOK && size >= iaParallelThreshold {
			nSeg := (size + iaSegmentSize - 1) / iaSegmentSize
			logf("[%s] Chunked download: %.0f MB, %d segments (~%d MiB each), up to %d parallel HTTP",
				name, float64(size)/1048576, nSeg, iaSegmentSize/(1024*1024), iaDownloadMaxParallel)
			return iaDownloadChunkedParallel(urlStr, dest, name, ref, size)
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

// iaDownloadChunkedParallel downloads the file into a single pre-sized destination using a
// queue of fixed-size byte ranges and a bounded worker pool (Gopeed-style work stealing
// across many small segments instead of one range per worker).
func iaDownloadChunkedParallel(urlStr, dest, name, ref string, totalSize int64) error {
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	if err := out.Truncate(totalSize); err != nil {
		out.Close()
		os.Remove(dest)
		return fmt.Errorf("truncate: %w", err)
	}

	type seg struct {
		start, end int64
	}
	var segments []seg
	for off := int64(0); off < totalSize; off += iaSegmentSize {
		end := off + iaSegmentSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		segments = append(segments, seg{off, end})
	}

	jobs := make(chan seg, len(segments))
	for _, s := range segments {
		jobs <- s
	}
	close(jobs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
				wMB := float64(w) / 1048576
				tMB := float64(totalSize) / 1048576
				etaStr := "..."
				if speedMBs > 0 && pct < 100 {
					etaSecs := float64(totalSize-w) / (speedMBs * 1048576)
					etaStr = "~" + fmtDuration(etaSecs) + " left"
				}
				logStatus(name, "Processing",
					fmt.Sprintf("Downloading: %.0f%% (%.0f/%.0f MB) @ %.1f MB/s | %s",
						pct, wMB, tMB, speedMBs, etaStr))
				if now.Sub(lastConsole) > 15*time.Second {
					logf("Download [%s]: %.1f%% (%.1f/%.1f MB) @ %.1f MB/s (chunked HTTP)",
						name, pct, wMB, tMB, speedMBs)
					lastConsole = now
				}
			}
		}
	}()

	workers := iaDownloadMaxParallel
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				if ctx.Err() != nil {
					return
				}
				if err := iaDownloadRange(ctx, urlStr, ref, out, s.start, s.end, &written); err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					errMu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	close(progressDone)
	out.Close()

	if firstErr != nil {
		os.Remove(dest)
		return firstErr
	}
	return nil
}

// iaDownloadRange downloads the inclusive byte range [start,end] into out at the same file offsets.
func iaDownloadRange(ctx context.Context, urlStr, ref string, out *os.File, start, end int64, writtenAtomic *int64) error {
	expect := end - start + 1
	var lastErr error
	for attempt := 0; attempt <= iaChunkRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * iaChunkRetryBase
			logf("RETRY chunk bytes=%d-%d (attempt %d/%d): %v — waiting %s",
				start, end, attempt, iaChunkRetries, lastErr, wait)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
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
			select {
			case <-ctx.Done():
				resp.Body.Close()
				atomic.AddInt64(writtenAtomic, -chunkWritten)
				return ctx.Err()
			default:
			}
			var n int
			n, readErr = resp.Body.Read(buf)
			if n > 0 {
				off := start + chunkWritten
				if _, wErr := out.WriteAt(buf[:n], off); wErr != nil {
					resp.Body.Close()
					atomic.AddInt64(writtenAtomic, -chunkWritten)
					lastErr = fmt.Errorf("write at +%d: %w", chunkWritten, wErr)
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
		if chunkWritten != expect {
			atomic.AddInt64(writtenAtomic, -chunkWritten)
			lastErr = fmt.Errorf("range incomplete: got %d want %d bytes", chunkWritten, expect)
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
			fullPath := m[1] // e.g. /download/nintendo-gameboy-advance/Game%20Name%20(USA).zip
			encoded := m[2]  // e.g. Game%20Name%20(USA).zip

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
	if err := utils.ExtractArchive(zipPath, extDir); err != nil {
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
		if err := utils.CompressROMFile(romFile, archiveDest); err != nil {
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
				if rangeOK && size >= iaParallelThreshold && iaDownloadMaxParallel > 1 {
					nSeg := (size + iaSegmentSize - 1) / iaSegmentSize
					logf("[%s] Chunked ROM download: %.0f MB, %d segments (~%d MiB each), up to %d parallel HTTP",
						name, float64(size)/1048576, nSeg, iaSegmentSize/(1024*1024), iaDownloadMaxParallel)
					return downloadEdgeEmuChunkedParallel(urlStr, dest, name, size)
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

// downloadEdgeEmuChunkedParallel is the edgeemu.net counterpart to iaDownloadChunkedParallel.
func downloadEdgeEmuChunkedParallel(urlStr, dest, name string, totalSize int64) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if err := out.Truncate(totalSize); err != nil {
		out.Close()
		os.Remove(dest)
		return err
	}

	type seg struct {
		start, end int64
	}
	var segments []seg
	for off := int64(0); off < totalSize; off += iaSegmentSize {
		end := off + iaSegmentSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		segments = append(segments, seg{off, end})
	}

	jobs := make(chan seg, len(segments))
	for _, s := range segments {
		jobs <- s
	}
	close(jobs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
					fmt.Sprintf("Downloading: %.0f%% (%.0f/%.0f MB) @ %.1f MB/s | %s",
						pct, float64(w)/1048576, float64(totalSize)/1048576, speedMBs, etaStr))
				if now.Sub(lastConsole) > 15*time.Second {
					logf("ROM Download [%s]: %.1f%% @ %.1f MB/s (chunked HTTP)", name, pct, speedMBs)
					lastConsole = now
				}
			}
		}
	}()

	workers := iaDownloadMaxParallel
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				if ctx.Err() != nil {
					return
				}
				if err := edgeEmuDownloadRange(ctx, urlStr, out, s.start, s.end, &written); err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					errMu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	close(progressDone)
	out.Close()

	if firstErr != nil {
		os.Remove(dest)
		return firstErr
	}
	return nil
}

func edgeEmuDownloadRange(ctx context.Context, urlStr string, out *os.File, start, end int64, writtenAtomic *int64) error {
	expect := end - start + 1
	var lastErr error
	for attempt := 0; attempt <= iaChunkRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * iaChunkRetryBase
			logf("RETRY ROM chunk bytes=%d-%d attempt %d: %v — waiting %s", start, end, attempt, lastErr, wait)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
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
			select {
			case <-ctx.Done():
				resp.Body.Close()
				atomic.AddInt64(writtenAtomic, -chunkWritten)
				return ctx.Err()
			default:
			}
			var n int
			n, readErr = resp.Body.Read(buf)
			if n > 0 {
				off := start + chunkWritten
				if _, wErr := out.WriteAt(buf[:n], off); wErr != nil {
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
		if chunkWritten != expect {
			atomic.AddInt64(writtenAtomic, -chunkWritten)
			lastErr = fmt.Errorf("range incomplete: got %d want %d bytes", chunkWritten, expect)
			continue
		}
		return nil
	nextAttempt:
	}
	return lastErr
}
