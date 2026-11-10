// types.go - domain types shared across all packages.
package models

import (
	"strings"
	"time"
)

// IAGameEntry links a display name to its Internet Archive download location.
type IAGameEntry struct {
	CollectionID string `json:"collection_id"`
	FileName     string `json:"filename"` // original filename with extension
	// AccessRestricted is true when the IA item's metadata sets
	// access-restricted-item (collection "loggedin"), so its files only download
	// with a logged-in archive.org cookie. TorBox's createwebdownload fetches the
	// URL unauthenticated and gets a 403, so Debrid can't proxy these - skip it
	// and use the direct IA path (which sends the user's cookie).
	AccessRestricted bool `json:"access_restricted,omitempty"`
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
	PathParam string `json:"path_param"` // percent-encoded ./Collection/file.zip (legacy /rom?name= shape)
}

// MinervaPlatformCache is persisted to disk per Minerva platform.
type MinervaPlatformCache struct {
	Schema    int                     `json:"schema,omitempty"` // app.MinervaCacheSchema at build time; 0 == legacy pre-v2.12.10
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

// ==========================================
// dlxbgame.com source
// ==========================================

// DlxbLink is one download link scraped from a dlxbgame.com post.
type DlxbLink struct {
	Host  string `json:"host"`  // canonical label, e.g. "1fichier", "mediafire", "adgate"
	URL   string `json:"url"`   // the link exactly as it appears in the post
	Label string `json:"label"` // anchor text, e.g. "1File", "Direct"
	// Status is the last liveness check: "alive", "dead", or "" (unknown).
	// Dead links are skipped by the resolver and never offered by the UI.
	Status    string    `json:"status,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitempty"`
}

// DlxbEntry is one scraped dlxbgame.com release: its links, archive password,
// and the post URL so the app can send the user back to the site.
type DlxbEntry struct {
	Title     string     `json:"title"`    // display title, tags stripped? no - kept as posted
	PageURL   string     `json:"page_url"` // https://dlxbgame.com/<slug>/
	Category  string     `json:"category"` // "iso" or "rgh"
	Password  string     `json:"password,omitempty"`
	Passwords []string   `json:"passwords,omitempty"` // every password the post mentions
	Region    string     `json:"region,omitempty"`
	Links     []DlxbLink `json:"links"`
}

// DlxbCache is the persisted scrape of the whole site.
type DlxbCache struct {
	Schema    int                  `json:"schema"`
	Site      string               `json:"site"`
	BuildTime time.Time            `json:"build_time"`
	Games     map[string][]string  `json:"games"`   // category -> display titles
	Entries   map[string]DlxbEntry `json:"entries"` // "<category>|<lower(title)>" -> entry
}

// AllPasswords returns the post password followed by any alternates, deduped.
func (e DlxbEntry) AllPasswords() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(e.Password)
	for _, p := range e.Passwords {
		add(p)
	}
	return out
}
