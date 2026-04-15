// types.go — domain types shared across all packages.
package models

import "time"

// IAGameEntry links a display name to its Internet Archive download location.
type IAGameEntry struct {
	CollectionID string `json:"collection_id"`
	FileName     string `json:"filename"` // original filename with extension
}

// PlatformCache is what gets persisted to disk per IA platform.
type PlatformCache struct {
	Games       []string               `json:"games"`
	GameEntries map[string]IAGameEntry `json:"game_entries"` // lower(name) -> entry
	BuildTime   time.Time              `json:"build_time"`
}

// BuildState tracks live progress of a cache build.
type BuildState struct {
	Total  int32
	Loaded int32
	State  string // "idle" "building" "ready" "error"
}

// MinervaEntry links a display name to its Minerva download path.
type MinervaEntry struct {
	FileName  string `json:"filename"`   // e.g. "007 - Blood Stone (USA, Europe).zip"
	PathParam string `json:"path_param"` // URL-encoded path for /rom?name= query param
}

// MinervaPlatformCache is persisted to disk per Minerva platform.
type MinervaPlatformCache struct {
	Games     []string                `json:"games"`
	Entries   map[string]MinervaEntry `json:"entries"` // lower(basename-no-ext) -> entry
	BuildTime time.Time               `json:"build_time"`
}

// XboxConnection holds Xbox console registration data.
type XboxConnection struct {
	IP        string `json:"ip"`
	Drive     string `json:"drive"`
	GameName  string `json:"game"`
	Platform  string `json:"platform"`
	Mode      string `json:"mode"`
	Timestamp time.Time
}

// GameStatus represents the current state/message for a queued game.
type GameStatus struct {
	State   string `json:"state"`
	Message string `json:"message"`
}

// ROMSystem describes one retro system served by EdgeEmu.
type ROMSystem struct {
	Name      string // Display name shown in Aurora menu
	BrowseURL string // edgeemu.net browse page URL
	Folder    string // RetroArch roms subfolder (e.g. "NES")
}

// PendingFTPJob represents a persisted FTP transfer that can be retried.
type PendingFTPJob struct {
	ID          string    `json:"id"`
	GameName    string    `json:"game_name"`
	Platform    string    `json:"platform"`
	XboxIP      string    `json:"xbox_ip"`
	Drive       string    `json:"drive"`
	Mode        string    `json:"mode"`
	SourceDir   string    `json:"source_dir"`
	TitleID     string    `json:"title_id,omitempty"`
	MediaID     string    `json:"media_id,omitempty"`
	ContentType string    `json:"content_type,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
