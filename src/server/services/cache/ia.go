// ia.go — Internet Archive cache persistence, build, and lookup.
package cache

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"godsend/app"
	"godsend/models"
)

// IAService manages the Internet Archive game cache.
type IAService struct {
	App *app.App
}

// ==========================================
// CACHE — DISK PERSISTENCE
// ==========================================

func (s *IAService) cacheFilePath(platform string) string {
	return filepath.Join(s.App.ToolsDir, "cache", platform+".json")
}

func (s *IAService) SaveCacheToDisk(platform string, games []string, entries map[string]models.IAGameEntry) {
	pc := models.PlatformCache{
		Games:       games,
		GameEntries: entries,
		BuildTime:   time.Now(),
	}
	data, err := json.MarshalIndent(pc, "", "  ")
	if err != nil {
		s.App.Logf("CACHE SAVE ERROR %s: %v", platform, err)
		return
	}
	if err := os.WriteFile(s.cacheFilePath(platform), data, 0644); err != nil {
		s.App.Logf("CACHE SAVE ERROR %s: %v", platform, err)
		return
	}
	s.App.Logf("CACHE: Saved %s (%d games) to disk", platform, len(games))
}

// LoadCacheFromDisk returns true if a usable cache was loaded.
func (s *IAService) LoadCacheFromDisk(platform string) bool {
	data, err := os.ReadFile(s.cacheFilePath(platform))
	if err != nil {
		return false
	}
	var pc models.PlatformCache
	if err := json.Unmarshal(data, &pc); err != nil {
		return false
	}
	if len(pc.Games) == 0 {
		return false
	}

	s.App.IAGameCacheMu.Lock()
	s.App.IAGameCache[platform] = pc.Games
	s.App.IAGameCacheMu.Unlock()

	s.App.GameEntryMapMu.Lock()
	for k, v := range pc.GameEntries {
		s.App.GameEntryMap[k] = v
	}
	s.App.GameEntryMapMu.Unlock()

	s.SetBuildState(platform, "ready", int32(len(pc.Games)), int32(len(pc.Games)))
	return true
}

// ==========================================
// CACHE — BUILD PROGRESS
// ==========================================

func (s *IAService) GetBuildState(platform string) *models.BuildState {
	s.App.BuildStatesMu.Lock()
	st, ok := s.App.BuildStates[platform]
	if !ok {
		st = &models.BuildState{State: "idle"}
		s.App.BuildStates[platform] = st
	}
	s.App.BuildStatesMu.Unlock()
	return st
}

func (s *IAService) SetBuildState(platform, state string, loaded, total int32) {
	st := s.GetBuildState(platform)
	atomic.StoreInt32(&st.Loaded, loaded)
	atomic.StoreInt32(&st.Total, total)
	s.App.BuildStatesMu.Lock()
	st.State = state
	s.App.BuildStatesMu.Unlock()
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

// IAFetchSem is a global semaphore capping simultaneous archive.org metadata
// requests across ALL platform cache builds.
var IAFetchSem = make(chan struct{}, 6)

const (
	MaxIARetries  = 4
	IABaseTimeout = 60 * time.Second
)

// IARetryBackoff is the wait before each retry attempt (index 0 = first retry).
var IARetryBackoff = []time.Duration{3 * time.Second, 8 * time.Second, 20 * time.Second, 40 * time.Second}

// DoIAMetaFetch performs one HTTP GET of the IA metadata API and returns parsed entries.
func (s *IAService) DoIAMetaFetch(collectionID string) ([]models.IAGameEntry, error) {
	IAFetchSem <- struct{}{} // acquire slot
	defer func() { <-IAFetchSem }()

	apiURL := "https://archive.org/metadata/" + collectionID
	client := &http.Client{Timeout: IABaseTimeout}
	req, _ := http.NewRequest("GET", apiURL, nil)
	s.App.ApplyArchiveOrgHeaders(req)

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

	var entries []models.IAGameEntry
	for _, f := range meta.Files {
		if f.Source != "original" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if !archiveExts[ext] {
			continue
		}
		entries = append(entries, models.IAGameEntry{
			CollectionID: collectionID,
			FileName:     f.Name,
		})
	}
	return entries, nil
}

// FetchIACollectionEntries wraps DoIAMetaFetch with exponential-backoff retries.
func (s *IAService) FetchIACollectionEntries(collectionID string) ([]models.IAGameEntry, error) {
	entries, err := s.DoIAMetaFetch(collectionID)
	if err == nil {
		return entries, nil
	}
	for attempt := 0; attempt < len(IARetryBackoff); attempt++ {
		wait := IARetryBackoff[attempt]
		s.App.Logf("CACHE RETRY [%s] attempt %d/%d in %v: %v",
			collectionID, attempt+1, MaxIARetries-1, wait, err)
		time.Sleep(wait)
		entries, err = s.DoIAMetaFetch(collectionID)
		if err == nil {
			return entries, nil
		}
	}
	return nil, fmt.Errorf("%s: gave up after %d attempts: %w", collectionID, MaxIARetries, err)
}

// Build fetches all collections for a platform sequentially
// (controlled by the global semaphore) so archive.org isn't overwhelmed.
func (s *IAService) Build(platform string) {
	s.App.IACacheBuildMu.Lock()
	if s.App.IACacheBuilding[platform] {
		s.App.IACacheBuildMu.Unlock()
		return
	}
	s.App.IACacheBuilding[platform] = true
	s.App.IACacheBuildMu.Unlock()

	defer func() {
		s.App.IACacheBuildMu.Lock()
		s.App.IACacheBuilding[platform] = false
		s.App.IACacheBuildMu.Unlock()
	}()

	colls, ok := app.IACollections[platform]
	if !ok {
		return
	}

	total := int32(len(colls))
	s.SetBuildState(platform, "building", 0, total)
	s.App.Logf("CACHE: Building %s — %d collections...", platform, total)

	type result struct {
		entries      []models.IAGameEntry
		collectionID string
		err          error
	}
	ch := make(chan result, len(colls))

	for _, coll := range colls {
		go func(c string) {
			entries, err := s.FetchIACollectionEntries(c)
			ch <- result{entries, c, err}
		}(coll)
	}

	newEntries := map[string]models.IAGameEntry{}
	var allGames []string
	var loaded int32

	for range colls {
		r := <-ch
		loaded++
		s.SetBuildState(platform, "building", loaded, total)

		if r.err != nil {
			s.App.Logf("CACHE WARN [%s]: %v", platform, r.err)
			continue
		}
		for _, e := range r.entries {
			ext := filepath.Ext(e.FileName)
			name := strings.TrimSuffix(e.FileName, ext)
			lower := strings.ToLower(name)
			newEntries[lower] = e
			allGames = append(allGames, name)
		}
		s.App.Logf("CACHE [%s] %d/%d: %s (%d files)", platform, loaded, total, r.collectionID, len(r.entries))
	}

	sort.Strings(allGames)
	s.SetBuildState(platform, "ready", total, total)
	s.App.Logf("CACHE: %s complete — %d games", platform, len(allGames))

	s.App.IAGameCacheMu.Lock()
	s.App.IAGameCache[platform] = allGames
	s.App.IAGameCacheMu.Unlock()

	s.App.GameEntryMapMu.Lock()
	for k, v := range newEntries {
		s.App.GameEntryMap[k] = v
	}
	s.App.GameEntryMapMu.Unlock()

	s.SaveCacheToDisk(platform, allGames, newEntries)
}

// ==========================================
// CACHE — LOOKUP
// ==========================================

// FindEntry returns the IAGameEntry for a game, searching cached data.
// Falls back to a live per-letter search if cache is empty.
func (s *IAService) FindEntry(gameName, platform string) (models.IAGameEntry, error) {
	lower := strings.ToLower(gameName)

	s.App.GameEntryMapMu.RLock()
	entry, ok := s.App.GameEntryMap[lower]
	s.App.GameEntryMapMu.RUnlock()
	if ok {
		return entry, nil
	}

	// Fuzzy: game name contains the search term (handles region tags)
	s.App.GameEntryMapMu.RLock()
	for k, e := range s.App.GameEntryMap {
		baseName := strings.ToLower(strings.Split(k, " (")[0])
		searchBase := strings.ToLower(strings.Split(gameName, " (")[0])
		if strings.Contains(k, lower) || baseName == searchBase {
			s.App.GameEntryMapMu.RUnlock()
			return e, nil
		}
	}
	s.App.GameEntryMapMu.RUnlock()

	// Live fetch from the relevant collection page(s)
	entry, err := s.LiveSearchIA(gameName, platform)
	if err != nil {
		return models.IAGameEntry{}, fmt.Errorf("not found in Internet Archive: %s", gameName)
	}
	return entry, nil
}

// LiveSearchIA searches IA collections via the Metadata API when the cache is cold.
func (s *IAService) LiveSearchIA(gameName, platform string) (models.IAGameEntry, error) {
	colls, ok := app.IACollections[platform]
	if !ok {
		return models.IAGameEntry{}, fmt.Errorf("unknown platform: %s", platform)
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
		entries, err := s.DoIAMetaFetch(coll)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.FileName), lowerSearch) {
				return e, nil
			}
		}
	}
	return models.IAGameEntry{}, fmt.Errorf("no match for '%s'", gameName)
}
