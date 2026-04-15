// rom.go — EdgeEmu ROM game cache scraping and lookup.
package cache

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"godsend/app"
	"godsend/models"
)

// ROMService manages the EdgeEmu ROM game cache.
type ROMService struct {
	App *app.App
	// IA is needed because ROM cache reuses IA's build state and cache file persistence.
	IA *IAService
}

// Build fetches and caches the game list for one ROM system from edgeemu.net.
func (s *ROMService) Build(sysid string) {
	platform := "rom_" + sysid
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

	sys, ok := app.ROMSystems[sysid]
	if !ok {
		return
	}
	s.IA.SetBuildState(platform, "building", 0, 1)
	s.App.Logf("ROM CACHE: Building %s (%s)...", sysid, sys.Name)

	names, urlMap, err := s.FetchEdgeEmuGames(sys.BrowseURL)
	if err != nil {
		s.IA.SetBuildState(platform, "error", 0, 1)
		s.App.Logf("ROM CACHE ERROR [%s]: %v", sysid, err)
		return
	}

	s.App.ROMGameCacheMu.Lock()
	s.App.ROMGameCache[sysid] = names
	s.App.ROMGameCacheMu.Unlock()

	s.App.ROMURLMapMu.Lock()
	for lower, dlURL := range urlMap {
		s.App.ROMURLMap[sysid+"\x00"+lower] = dlURL
	}
	s.App.ROMURLMapMu.Unlock()

	s.IA.SetBuildState(platform, "ready", 1, 1)
	s.App.Logf("ROM CACHE: %s complete — %d games", sysid, len(names))

	// Persist using the existing PlatformCache format.
	entries := map[string]models.IAGameEntry{}
	for lower, dlURL := range urlMap {
		entries[lower] = models.IAGameEntry{CollectionID: sysid, FileName: dlURL}
	}
	s.IA.SaveCacheToDisk(platform, names, entries)
}

// LoadFromDisk loads a previously scraped edgeemu game list.
func (s *ROMService) LoadFromDisk(sysid string) bool {
	platform := "rom_" + sysid
	data, err := os.ReadFile(s.IA.cacheFilePath(platform))
	if err != nil {
		return false
	}
	var pc models.PlatformCache
	if err := json.Unmarshal(data, &pc); err != nil || len(pc.Games) == 0 {
		return false
	}

	s.App.ROMGameCacheMu.Lock()
	s.App.ROMGameCache[sysid] = pc.Games
	s.App.ROMGameCacheMu.Unlock()

	s.App.ROMURLMapMu.Lock()
	for lower, entry := range pc.GameEntries {
		s.App.ROMURLMap[sysid+"\x00"+lower] = entry.FileName
	}
	s.App.ROMURLMapMu.Unlock()

	s.IA.SetBuildState(platform, "ready", 1, 1)
	return true
}

// FetchEdgeEmuGames scrapes an edgeemu.net browse page and returns all game names
// and their direct ZIP download URLs.
func (s *ROMService) FetchEdgeEmuGames(browseURL string) ([]string, map[string]string, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	urlMap := map[string]string{}
	var allNames []string
	seen := map[string]bool{}

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
			fullPath := m[1]
			encoded := m[2]

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

	base := strings.TrimRight(browseURL, "/")
	letters := []string{
		"0-9",
		"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m",
		"n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z",
	}
	for _, l := range letters {
		parsePage(base + "/" + l)
		time.Sleep(150 * time.Millisecond)
	}

	sort.Strings(allNames)
	if len(allNames) == 0 {
		return nil, nil, fmt.Errorf("no games found at %s", browseURL)
	}
	return allNames, urlMap, nil
}

// FindDownloadURL looks up the cached download URL for a ROM, with fuzzy fallback.
func (s *ROMService) FindDownloadURL(gameName, sysid string) string {
	lower := strings.ToLower(gameName)
	key := sysid + "\x00" + lower

	s.App.ROMURLMapMu.RLock()
	u, ok := s.App.ROMURLMap[key]
	s.App.ROMURLMapMu.RUnlock()
	if ok {
		return u
	}

	// Fuzzy: strip region tag "(USA)" etc. and try partial match
	baseSearch := strings.ToLower(strings.Split(gameName, " (")[0])
	prefix := sysid + "\x00"
	s.App.ROMURLMapMu.RLock()
	defer s.App.ROMURLMapMu.RUnlock()
	for k, v := range s.App.ROMURLMap {
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
