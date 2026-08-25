// minerva.go - Minerva Archive cache persistence, scraping, build, and lookup.
package cache

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"godsend/app"
	"godsend/infrastructure/helpers"
	"godsend/models"
)

// MinervaService manages the Minerva Archive game cache.
type MinervaService struct {
	App *app.App
}

// ==========================================
// MINERVA CACHE - DISK PERSISTENCE
// ==========================================

func (s *MinervaService) cacheFilePath(platform string) string {
	return filepath.Join(s.App.ToolsDir, "cache", "minerva_"+platform+".json")
}

func (s *MinervaService) SaveCacheToDisk(platform string, games []string, entries map[string]models.MinervaEntry) {
	mc := models.MinervaPlatformCache{
		Schema:    app.MinervaCacheSchema,
		Games:     games,
		Entries:   entries,
		BuildTime: time.Now(),
	}
	data, err := json.MarshalIndent(mc, "", "  ")
	if err != nil {
		s.App.Logf("MINERVA CACHE SAVE ERROR %s: %v", platform, err)
		return
	}
	if err := os.WriteFile(s.cacheFilePath(platform), data, 0644); err != nil {
		s.App.Logf("MINERVA CACHE SAVE ERROR %s: %v", platform, err)
		return
	}
	s.App.Logf("MINERVA CACHE: Saved %s (%d games) to disk", platform, len(games))
}

func (s *MinervaService) LoadCacheFromDisk(platform string) bool {
	data, err := os.ReadFile(s.cacheFilePath(platform))
	if err != nil {
		return false
	}
	var mc models.MinervaPlatformCache
	if err := json.Unmarshal(data, &mc); err != nil {
		return false
	}
	if len(mc.Games) == 0 {
		return false
	}
	// Reject caches built with an older filter scheme so the next startup
	// rebuilds them and surfaces previously-filtered entries (e.g. `(DLC)`
	// tags in the No-Intro Digital collection).
	if mc.Schema < app.MinervaCacheSchema {
		s.App.Logf("MINERVA CACHE: %s schema=%d < %d - rebuilding", platform, mc.Schema, app.MinervaCacheSchema)
		return false
	}

	s.App.MinervaGameCacheMu.Lock()
	s.App.MinervaGameCache[platform] = mc.Games
	s.App.MinervaGameCacheMu.Unlock()

	s.App.MinervaEntryMapMu.Lock()
	for k, v := range mc.Entries {
		s.App.MinervaEntryMap[k] = v
		if dk := strings.ToLower(helpers.DecodeMinervaName(k)); dk != k {
			if _, taken := s.App.MinervaEntryMap[dk]; !taken {
				s.App.MinervaEntryMap[dk] = v
			}
		}
	}
	s.App.MinervaEntryMapMu.Unlock()

	s.SetBuildState(platform, "ready", int32(len(mc.Games)), int32(len(mc.Games)))
	return true
}

// ==========================================
// MINERVA CACHE - BUILD STATE
// ==========================================

func (s *MinervaService) GetBuildState(platform string) *models.BuildState {
	s.App.MinervaBuildStatesMu.Lock()
	st, ok := s.App.MinervaBuildStates[platform]
	if !ok {
		st = &models.BuildState{State: "idle"}
		s.App.MinervaBuildStates[platform] = st
	}
	s.App.MinervaBuildStatesMu.Unlock()
	return st
}

func (s *MinervaService) SetBuildState(platform, state string, loaded, total int32) {
	st := s.GetBuildState(platform)
	atomic.StoreInt32(&st.Loaded, loaded)
	atomic.StoreInt32(&st.Total, total)
	s.App.MinervaBuildStatesMu.Lock()
	st.State = state
	s.App.MinervaBuildStatesMu.Unlock()
}

// ==========================================
// MINERVA CACHE - SCRAPE + BUILD
// ==========================================

// ScrapeMinervaPage fetches one Minerva browse URL and returns file entries.
// tagFilters, if non-empty, restricts results to filenames containing AT LEAST
// ONE of the listed substrings (any-match).
//
// Current Minerva pages list files as data-name="file.zip" with href="/rom?id=…".
// Older pages used href="/rom?name=./Collection/file.zip"; both shapes are accepted.
func (s *MinervaService) ScrapeMinervaPage(browseURL string, tagFilters []string) ([]models.MinervaEntry, error) {
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

	// Relative torrent path prefix from the browse URL (…/browse/<dir>/).
	pathPrefix := minervaPathPrefixFromBrowseURL(browseURL)

	seen := make(map[string]struct{})
	var entries []models.MinervaEntry
	add := func(fileName, pathParam string) {
		ext := strings.ToLower(filepath.Ext(fileName))
		if ext != ".zip" && ext != ".7z" && ext != ".rar" {
			return
		}
		if len(tagFilters) > 0 {
			match := false
			for _, t := range tagFilters {
				if strings.Contains(fileName, t) {
					match = true
					break
				}
			}
			if !match {
				return
			}
		}
		key := strings.ToLower(fileName)
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		entries = append(entries, models.MinervaEntry{
			FileName:  fileName,
			PathParam: pathParam,
		})
	}

	for _, m := range app.MinervaRomIDLinkRe.FindAllSubmatch(body, -1) {
		fileName := filepath.Base(strings.TrimSpace(string(m[1])))
		rel := "./" + fileName
		if pathPrefix != "" {
			rel = "./" + pathPrefix + "/" + fileName
		}
		add(fileName, url.PathEscape(rel))
	}

	// data-name is lowercased on current Minerva pages; only use when rom?id text was absent.
	for _, m := range app.MinervaDataNameRe.FindAllSubmatch(body, -1) {
		fileName := filepath.Base(string(m[1]))
		rel := "./" + fileName
		if pathPrefix != "" {
			rel = "./" + pathPrefix + "/" + fileName
		}
		add(fileName, url.PathEscape(rel))
	}

	// Legacy /rom?name=./Collection/file.zip pages (pre data-name).
	for _, m := range app.MinervaHrefRe.FindAllSubmatch(body, -1) {
		hrefVal := string(m[1])
		const prefix = "/rom?name="
		if !strings.HasPrefix(hrefVal, prefix) {
			continue
		}
		pathParam := hrefVal[len(prefix):]
		decoded, err := url.PathUnescape(pathParam)
		if err != nil {
			continue
		}
		add(filepath.Base(decoded), pathParam)
	}
	return entries, nil
}

// minervaPathPrefixFromBrowseURL returns the decoded directory under /browse/
// (e.g. "Redump/Microsoft - Xbox 360"), or "" if the URL shape is unexpected.
func minervaPathPrefixFromBrowseURL(browseURL string) string {
	u, err := url.Parse(browseURL)
	if err != nil {
		return ""
	}
	const marker = "/browse/"
	idx := strings.Index(u.Path, marker)
	if idx < 0 {
		return ""
	}
	p := strings.Trim(u.Path[idx+len(marker):], "/")
	if p == "" {
		return ""
	}
	decoded, err := url.PathUnescape(p)
	if err != nil {
		return p
	}
	return decoded
}

// Build scrapes the Minerva browse page for one platform and caches results.
func (s *MinervaService) Build(platform string) {
	s.App.MinervaCacheBuildMu.Lock()
	if s.App.MinervaCacheBuilding[platform] {
		s.App.MinervaCacheBuildMu.Unlock()
		return
	}
	s.App.MinervaCacheBuilding[platform] = true
	s.App.MinervaCacheBuildMu.Unlock()

	defer func() {
		s.App.MinervaCacheBuildMu.Lock()
		s.App.MinervaCacheBuilding[platform] = false
		s.App.MinervaCacheBuildMu.Unlock()
	}()

	browseURL, ok := app.MinervaPageURLs[platform]
	if !ok {
		return
	}
	tagFilters := app.MinervaTagFilters[platform]

	s.SetBuildState(platform, "building", 0, 1)
	s.App.Logf("MINERVA CACHE: Building %s (filters=%v) ...", platform, tagFilters)

	entries, err := s.ScrapeMinervaPage(browseURL, tagFilters)
	if err != nil {
		s.App.Logf("MINERVA CACHE ERROR [%s]: %v", platform, err)
		s.SetBuildState(platform, "error", 0, 1)
		return
	}

	newEntries := make(map[string]models.MinervaEntry, len(entries)*2)
	var allGames []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.FileName, filepath.Ext(e.FileName))
		lower := strings.ToLower(name)
		if _, dup := newEntries[lower]; dup {
			continue
		}
		me := models.MinervaEntry{FileName: e.FileName, PathParam: e.PathParam}
		newEntries[lower] = me
		if dec := strings.ToLower(helpers.DecodeMinervaName(name)); dec != lower {
			if _, taken := newEntries[dec]; !taken {
				newEntries[dec] = models.MinervaEntry{FileName: me.FileName, PathParam: me.PathParam}
			}
		}
		allGames = append(allGames, name)
	}
	sort.Strings(allGames)
	s.SetBuildState(platform, "ready", 1, 1)
	s.App.Logf("MINERVA CACHE: %s complete - %d games", platform, len(allGames))

	s.App.MinervaGameCacheMu.Lock()
	s.App.MinervaGameCache[platform] = allGames
	s.App.MinervaGameCacheMu.Unlock()

	s.App.MinervaEntryMapMu.Lock()
	for k, v := range newEntries {
		s.App.MinervaEntryMap[k] = v
	}
	s.App.MinervaEntryMapMu.Unlock()

	s.SaveCacheToDisk(platform, allGames, newEntries)
}

// ==========================================
// MINERVA CACHE - LOOKUP
// ==========================================

// FindEntry looks up a game in the Minerva cache.
// Returns the entry and true if found, or false if not found.
func (s *MinervaService) FindEntry(gameName, platform string) (models.MinervaEntry, bool) {
	keys := MinervaLookupKeys(gameName)

	s.App.MinervaEntryMapMu.RLock()
	for _, key := range keys {
		if key == "" {
			continue
		}
		if e, ok := s.App.MinervaEntryMap[key]; ok {
			s.App.MinervaEntryMapMu.RUnlock()
			return e, true
		}
	}
	// Fuzzy: strip region tags and compare base names
	decName := helpers.DecodeMinervaName(gameName)
	lowerDec := strings.ToLower(decName)
	baseName := strings.ToLower(strings.SplitN(decName, " (", 2)[0])
	for k, e := range s.App.MinervaEntryMap {
		kDec := helpers.DecodeMinervaName(k)
		if strings.Contains(strings.ToLower(kDec), lowerDec) {
			s.App.MinervaEntryMapMu.RUnlock()
			return e, true
		}
		kBase := strings.ToLower(strings.SplitN(kDec, " (", 2)[0])
		if kBase == baseName {
			s.App.MinervaEntryMapMu.RUnlock()
			return e, true
		}
	}
	s.App.MinervaEntryMapMu.RUnlock()

	// Trigger a background build if the cache is empty for this platform
	s.App.MinervaGameCacheMu.RLock()
	isEmpty := len(s.App.MinervaGameCache[platform]) == 0
	s.App.MinervaGameCacheMu.RUnlock()
	if isEmpty {
		go s.Build(platform)
	}
	return models.MinervaEntry{}, false
}

// ==========================================
// MINERVA LOOKUP HELPERS
// ==========================================

// MinervaLookupKeys returns distinct lowercased index keys for a Minerva display/file base name.
func MinervaLookupKeys(name string) []string {
	name = strings.TrimSpace(name)
	raw := strings.ToLower(name)
	dec := strings.ToLower(helpers.DecodeMinervaName(name))
	if raw == dec {
		return []string{raw}
	}
	return []string{raw, dec}
}
