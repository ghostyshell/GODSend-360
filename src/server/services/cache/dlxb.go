// dlxb.go - dlxbgame.com scrape persistence, build, and lookup.
package cache

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"godsend/app"
	"godsend/infrastructure/dlxb"
	"godsend/infrastructure/proxy"
	"godsend/models"
)

// DlxbService manages the dlxbgame.com game cache.
type DlxbService struct {
	App *app.App
}

// CacheFilePath is where the scrape lives inside GODSEND_HOME.
func (s *DlxbService) CacheFilePath() string {
	return filepath.Join(s.App.ToolsDir, "cache", "dlxb_games.json")
}

// ── persistence ──────────────────────────────────────────────────────────────

func (s *DlxbService) SaveCacheToDisk(c *models.DlxbCache) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	path := s.CacheFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		s.App.Logf("DLXB CACHE SAVE ERROR: %v", err)
		return err
	}
	s.App.Logf("DLXB CACHE: saved %d ISO + %d Jtag/RGH titles", len(c.Games["iso"]), len(c.Games["rgh"]))
	return nil
}

func (s *DlxbService) LoadCacheFromDisk() bool {
	data, err := os.ReadFile(s.CacheFilePath())
	if err != nil {
		return false
	}
	var c models.DlxbCache
	if err := json.Unmarshal(data, &c); err != nil {
		s.App.Logf("DLXB CACHE: unreadable (%v) - rebuilding", err)
		return false
	}
	if c.Schema < app.DlxbCacheSchema || len(c.Games) == 0 {
		s.App.Logf("DLXB CACHE: schema=%d < %d - rebuilding", c.Schema, app.DlxbCacheSchema)
		return false
	}
	s.apply(&c)
	s.SetBuildState("all", "ready", 1, 1)
	s.App.Logf("DLXB CACHE: loaded %d ISO + %d Jtag/RGH titles from disk", len(c.Games["iso"]), len(c.Games["rgh"]))
	return true
}

func (s *DlxbService) apply(c *models.DlxbCache) {
	s.App.DlxbMu.Lock()
	// Replace, don't merge: a refresh must be able to drop removed titles.
	s.App.DlxbGames = c.Games
	s.App.DlxbEntries = c.Entries
	if s.App.DlxbGames == nil {
		s.App.DlxbGames = map[string][]string{}
	}
	if s.App.DlxbEntries == nil {
		s.App.DlxbEntries = map[string]models.DlxbEntry{}
	}
	s.App.DlxbMu.Unlock()
}

// ── build state ──────────────────────────────────────────────────────────────

func (s *DlxbService) GetBuildState(key string) *models.BuildState {
	s.App.DlxbBuildStatesMu.Lock()
	st, ok := s.App.DlxbBuildStates[key]
	if !ok {
		st = &models.BuildState{State: "idle"}
		s.App.DlxbBuildStates[key] = st
	}
	s.App.DlxbBuildStatesMu.Unlock()
	return st
}

func (s *DlxbService) SetBuildState(key, state string, loaded, total int32) {
	st := s.GetBuildState(key)
	atomic.StoreInt32(&st.Loaded, loaded)
	atomic.StoreInt32(&st.Total, total)
	s.App.DlxbBuildStatesMu.Lock()
	st.State = state
	s.App.DlxbBuildStatesMu.Unlock()
}

// ── scrape ───────────────────────────────────────────────────────────────────

// Build scrapes the whole site and replaces the in-memory + on-disk cache.
// Concurrent calls collapse into one scrape.
func (s *DlxbService) Build() {
	s.App.DlxbBuildMu.Lock()
	if s.App.DlxbBuilding {
		s.App.DlxbBuildMu.Unlock()
		return
	}
	s.App.DlxbBuilding = true
	s.App.DlxbBuildMu.Unlock()
	defer func() {
		s.App.DlxbBuildMu.Lock()
		s.App.DlxbBuilding = false
		s.App.DlxbBuildMu.Unlock()
	}()

	s.SetBuildState("all", "building", 0, 1)
	s.App.Logf("DLXB CACHE: scraping %s ...", app.DlxbSiteURL)
	s.App.Logf("DLXB: %s", app.DlxbNotice)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cache, err := dlxb.Scrape(ctx, dlxb.Options{Logf: s.App.Logf, Proxy: proxy.FromEnv(s.App.Logf)})
	if err != nil {
		s.App.Logf("DLXB CACHE ERROR: %v", err)
		s.SetBuildState("all", "error", 0, 1)
		return
	}
	s.apply(cache)
	if err := s.SaveCacheToDisk(cache); err != nil {
		s.SetBuildState("all", "error", 0, 1)
		return
	}
	s.SetBuildState("all", "ready", 1, 1)
	s.App.Logf("DLXB CACHE: ready - %d ISO + %d Jtag/RGH titles",
		len(cache.Games["iso"]), len(cache.Games["rgh"]))
	// Mark dead links in the background so the browse lists stop offering
	// releases that can no longer download.
	if os.Getenv("GODSEND_DLXB_HEALTH") != "0" {
		go s.runHealthPass(cache)
	}
}

// runHealthPass checks the new links without blocking the refresh. It works on
// a copy and swaps it in under the lock, so handlers never see a partial write.
func (s *DlxbService) runHealthPass(cache *models.DlxbCache) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	r := dlxb.NewResolver()
	r.CFSolverURL = strings.TrimSpace(os.Getenv("GODSEND_CF_SOLVER_URL"))
	r.SetProxyPool(proxy.FromEnv(s.App.Logf))

	clone := &models.DlxbCache{
		Schema:    cache.Schema,
		Site:      cache.Site,
		BuildTime: cache.BuildTime,
		Games:     cache.Games,
		Entries:   make(map[string]models.DlxbEntry, len(cache.Entries)),
	}
	for k, e := range cache.Entries {
		e.Links = append([]models.DlxbLink(nil), e.Links...)
		clone.Entries[k] = e
	}
	// Per-link DEAD lines are useful to the CLI but would flood the app log;
	// keep the progress lines and drop the individual ones.
	logf := func(format string, args ...any) {
		if strings.HasPrefix(format, "DEAD ") {
			return
		}
		s.App.Logf(format, args...)
	}
	r.CheckAll(ctx, clone, 8, logf)

	s.App.DlxbMu.Lock()
	s.App.DlxbEntries = clone.Entries
	s.App.DlxbMu.Unlock()
	s.SaveCacheToDisk(clone)
}

// ── lookup ───────────────────────────────────────────────────────────────────

// Games returns the browsable titles for a platform key ("dlxb" or "dlxb_rgh").
// Titles whose every link is marked dead are hidden: the UI never offers a
// download that cannot start.
func (s *DlxbService) Games(platform string) []string {
	category, ok := app.DlxbPlatforms[platform]
	if !ok {
		return nil
	}
	s.App.DlxbMu.RLock()
	defer s.App.DlxbMu.RUnlock()
	games := s.App.DlxbGames[category]
	out := make([]string, 0, len(games))
	for _, title := range games {
		entry, ok := s.App.DlxbEntries[dlxb.EntryKey(category, title)]
		if !ok || hasLiveLink(entry) {
			out = append(out, title)
		}
	}
	return out
}

func hasLiveLink(e models.DlxbEntry) bool {
	if len(e.Links) == 0 {
		return false
	}
	for _, l := range e.Links {
		if l.Status != dlxb.StatusDead {
			return true
		}
	}
	return false
}

// FindEntry looks up a title on a platform by its display name.
func (s *DlxbService) FindEntry(game, platform string) (models.DlxbEntry, bool) {
	category, ok := app.DlxbPlatforms[platform]
	if !ok {
		return models.DlxbEntry{}, false
	}
	keys := []string{
		dlxb.EntryKey(category, game),
		dlxb.EntryKey(category, stripTags(game)),
	}
	s.App.DlxbMu.RLock()
	defer s.App.DlxbMu.RUnlock()
	for _, key := range keys {
		if e, ok := s.App.DlxbEntries[key]; ok {
			return e, true
		}
	}
	return models.DlxbEntry{}, false
}

// stripTags drops trailing release tags so "Game [USA][ISO]" also matches a
// stored "Game". Only the tag suffix is removed, not parenthesised subtitles.
func stripTags(title string) string {
	t := strings.TrimSpace(title)
	for {
		i := strings.LastIndex(t, "[")
		if i < 0 || !strings.HasSuffix(t, "]") {
			break
		}
		t = strings.TrimSpace(t[:i])
	}
	return t
}
