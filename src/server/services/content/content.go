// content.go — DLC / Title Update discovery and management service.
package content

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"godsend/app"
	"godsend/infrastructure/ftp"
	"godsend/infrastructure/helpers"
	"godsend/models"

	goftp "github.com/jlaffaye/ftp"
)

// Service handles DLC/TU discovery, scanning, and queueing.
type Service struct {
	App *app.App
	FTP *ftp.Service
}

// ContentTypeNames maps Xbox content type hex to human-readable names.
var ContentTypeNames = map[string]string{
	"00000000": "Game",
	"00000001": "Game DLC",
	"00000002": "DLC / Add-on",
	"00000003": "Avatar Item",
	"00000005": "Game Demo",
	"00000009": "Video",
	"00000010": "XNA Game",
	"00001000": "XBLA Title",
	"00002000": "XNA / XBLIG",
	"00005000": "Title Update",
	"000B0000": "Title Update",
	"00040000": "System Update",
	"00030000": "Gamer Picture",
	"00020000": "Theme",
}

func contentTypeName(ct string) string {
	if n, ok := ContentTypeNames[strings.ToLower(ct)]; ok {
		return n
	}
	return "Content (" + ct + ")"
}

func joinFtpPath(parts ...string) string {
	return strings.Join(parts, "/")
}

// ScanInstalledContent lists all DLC and TUs already present on the Xbox
// under /{drive}/Content/0000000000000000/{titleID}/.
func (s *Service) ScanInstalledContent(xboxIP, drive, titleID string) (*models.InstalledContentReport, error) {
	conn, err := s.FTP.ConnectWithRetry(xboxIP)
	if err != nil {
		return nil, err
	}
	defer conn.Quit()

	driveClean := strings.TrimSuffix(drive, ":")
	base := fmt.Sprintf("/%s/Content/0000000000000000/%s", driveClean, strings.ToUpper(titleID))

	entries, err := conn.List(base)
	if err != nil {
		return &models.InstalledContentReport{TitleID: strings.ToUpper(titleID)}, nil
	}

	var dlcs, tus []models.ContentItem
	for _, e := range entries {
		if e.Type != goftp.EntryTypeFolder {
			continue
		}
		ct := strings.ToLower(e.Name)
		if len(ct) != 8 || !helpers.IsHexString(ct) {
			continue
		}
		subFiles, _ := conn.List(joinFtpPath(base, e.Name))
		var size int64
		for _, sf := range subFiles {
			if sf.Type == goftp.EntryTypeFile {
				size += int64(sf.Size)
			}
		}
		item := models.ContentItem{
			TitleID:     strings.ToUpper(titleID),
			ContentType: ct,
			DisplayName: contentTypeName(ct),
			Size:        size,
			Installed:   true,
			Active:      false,
		}
		if ct == "00005000" || ct == "000b0000" {
			item.DisplayName = s.resolveTUName(conn, base, ct)
			item.Active = s.isTUActive(conn, base, ct)
			tus = append(tus, item)
		} else if ct == "00000002" || ct == "00000001" {
			dlcs = append(dlcs, item)
		}
	}

	return &models.InstalledContentReport{
		TitleID:      strings.ToUpper(titleID),
		DLCs:         dlcs,
		TitleUpdates: tus,
	}, nil
}

func (s *Service) resolveTUName(conn *goftp.ServerConn, base, ct string) string {
	entries, err := conn.List(joinFtpPath(base, ct))
	if err != nil {
		return "Title Update"
	}
	var best string
	var bestVer int
	for _, e := range entries {
		if e.Type != goftp.EntryTypeFile {
			continue
		}
		name := e.Name
		if strings.HasPrefix(strings.ToLower(name), "tu") {
			verStr := name[2:]
			if idx := strings.IndexAny(verStr, "_."); idx > 0 {
				verStr = verStr[:idx]
			}
			if v, err := strconv.Atoi(verStr); err == nil && v > bestVer {
				bestVer = v
				best = name
			}
		}
	}
	if best != "" {
		return "Title Update v" + strconv.Itoa(bestVer)
	}
	return "Title Update"
}

func (s *Service) isTUActive(conn *goftp.ServerConn, base, ct string) bool {
	entries, err := conn.List(joinFtpPath(base, ct))
	if err != nil {
		return false
	}
	fileCount := 0
	for _, e := range entries {
		if e.Type == goftp.EntryTypeFile {
			fileCount++
		}
	}
	return fileCount > 0
}

// ============================================================
// Discovery — Minerva + Internet Archive only.
// Xbox CDN (xboxunity TitleList) is intentionally removed because
// it returns inaccurate metadata rather than actual downloadable
// content.
// ============================================================

// DiscoverContent fetches available DLC/TU for a TitleID from Minerva
// and IA caches, merging them with what’s already installed on Xbox.
func (s *Service) DiscoverContent(titleID, gameName string, xboxIP, drive string) (*models.ContentManifest, error) {
	titleID = strings.ToUpper(titleID)
	manifest := &models.ContentManifest{
		TitleID:      titleID,
		GameName:     gameName,
		DLCs:         []models.ContentItem{},
		TitleUpdates: []models.ContentItem{},
	}

	if xboxIP != "" && drive != "" {
		installed, err := s.ScanInstalledContent(xboxIP, drive, titleID)
		if err == nil {
			manifest.DLCs = append(manifest.DLCs, installed.DLCs...)
			manifest.TitleUpdates = append(manifest.TitleUpdates, installed.TitleUpdates...)
		}
	}

	// Search IA + Minerva for this game's DLC / TU.
	// We search the dlc, xbla and digital caches — title updates for many
	// games are stored in the "dlc" or "digital" collections alongside DLC.
	candidates := s.discoverFromMinerva(gameName, titleID)
	candidates = append(candidates, s.discoverFromIA(gameName, titleID)...)

	for _, it := range candidates {
		if isTitleUpdateEntry(it.DisplayName) {
			if !s.containsTU(manifest.TitleUpdates, it.FileName) {
				manifest.TitleUpdates = append(manifest.TitleUpdates, it)
			}
		} else {
			if !s.containsDLC(manifest.DLCs, it.FileName) {
				manifest.DLCs = append(manifest.DLCs, it)
			}
		}
	}

	// Keep the highest TU version information for display.
	s.normalizeTUVersions(manifest.TitleUpdates)

	return manifest, nil
}

// ============================================================
// Source helpers
// ============================================================

func (s *Service) containsTU(list []models.ContentItem, fileName string) bool {
	for _, it := range list {
		if strings.EqualFold(it.FileName, fileName) {
			return true
		}
	}
	return false
}

func (s *Service) containsDLC(list []models.ContentItem, fileName string) bool {
	for _, it := range list {
		if strings.EqualFold(it.FileName, fileName) {
			return true
		}
	}
	return false
}

// isTitleUpdateEntry returns true when the raw title name looks like a TU.
func isTitleUpdateEntry(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "title update") || strings.Contains(lower, "tu ") || strings.Contains(lower, " (tu)") || strings.Contains(lower, "(title update)") || strings.Contains(lower, "title-update") || strings.Contains(lower, "data for tu") || strings.Contains(lower, "title update-ban")
}

// normalizeTUVersions detects version numbers from TU filenames and
// populates the Version / DisplayName fields for sorting.
func (s *Service) normalizeTUVersions(tus []models.ContentItem) {
	type verInfo struct {
		idx     int
		version int
	}
	var versions []verInfo
	for i, tu := range tus {
		v := extractTUVersion(tu.DisplayName)
		versions = append(versions, verInfo{idx: i, version: v})
		tus[i].Version = v
		if v > 0 {
			tus[i].DisplayName = fmt.Sprintf("Title Update v%d", v)
		}
	}
	// Mark the highest TU version as active (matching Aurora behaviour).
	var highestVer = -1
	var highestIdx = -1
	for _, vi := range versions {
		if vi.version > highestVer {
			highestVer = vi.version
			highestIdx = vi.idx
		}
	}
	if highestIdx >= 0 {
		tus[highestIdx].Active = true
	}
}

func extractTUVersion(name string) int {
	lower := strings.ToLower(name)
	// "Title Update v1", "TU 2", "(v3)", "v1.4", etc.
	patterns := []string{
		`\(v(\d+)\)`,
		`\bv(\d+)\b`,
		` \(v(\d+)\)`,
		`tu\s+(\d+)`,
		`title\s+update\s+(\d+)`,
		`title\s+update\s+v(\d+)`,
	}
	best := math.MaxInt
	bestVer := 0
	for _, pat := range patterns {
		// naive manual extraction without regex for speed
		_ = pat
	}
	// Simple string scan for patterns that actually appear in filenames
	if idx := strings.Index(lower, "title update v"); idx >= 0 {
		start := idx + len("title update v")
		numStr := ""
		for i := start; i < len(lower) && (lower[i] >= '0' && lower[i] <= '9'); i++ {
			numStr += string(lower[i])
		}
		if v, err := strconv.Atoi(numStr); err == nil && v > 0 && v < best {
			best = v
			bestVer = v
		}
	}
	if idx := strings.Index(lower, "(v"); idx >= 0 {
		start := idx + 2
		numStr := ""
		for i := start; i < len(lower) && (lower[i] >= '0' && lower[i] <= '9'); i++ {
			numStr += string(lower[i])
		}
		if v, err := strconv.Atoi(numStr); err == nil && v > 0 && v < best {
			best = v
			bestVer = v
		}
	}
	if idx := strings.Index(lower, "tu "); idx >= 0 {
		start := idx + 3
		numStr := ""
		for i := start; i < len(lower) && (lower[i] >= '0' && lower[i] <= '9'); i++ {
			numStr += string(lower[i])
		}
		if v, err := strconv.Atoi(numStr); err == nil && v > 0 && v < best {
			best = v
			bestVer = v
		}
	}
	return bestVer
}

// ============================================================
//  Minerva Archive discovery
// ============================================================

func (s *Service) discoverFromMinerva(gameName, titleID string) []models.ContentItem {
	var items []models.ContentItem
	searchLower := strings.ToLower(gameName)

	s.App.MinervaEntryMapMu.RLock()
	defer s.App.MinervaEntryMapMu.RUnlock()

	for _, platform := range []string{"dlc", "xbla", "digital", "xblig", "games"} {
		games, ok := s.App.MinervaGameCache[platform]
		if !ok {
			continue
		}
		for _, g := range games {
			lower := strings.ToLower(g)
			if !s.matchesGameName(lower, searchLower, titleID) {
				continue
			}
			entry, ok := s.App.MinervaEntryMap[lower]
			if !ok {
				continue
			}
			items = append(items, models.ContentItem{
				TitleID:     strings.ToUpper(titleID),
				ContentType: guessContentTypeFromName(g),
				DisplayName: g,
				FileName:    entry.FileName,
				Source:      "minerva",
				SourceURL:   entry.PathParam,
				Installed:   false,
			})
		}
	}
	return items
}

// ============================================================
//  Internet Archive discovery
// ============================================================

func (s *Service) discoverFromIA(gameName, titleID string) []models.ContentItem {
	var items []models.ContentItem
	searchLower := strings.ToLower(gameName)

	s.App.GameEntryMapMu.RLock()
	defer s.App.GameEntryMapMu.RUnlock()

	for _, platform := range []string{"dlc", "xbla", "digital", "xblig", "games"} {
		games, ok := s.App.IAGameCache[platform]
		if !ok {
			continue
		}
		for _, g := range games {
			lower := strings.ToLower(g)
			if !s.matchesGameName(lower, searchLower, titleID) {
				continue
			}
			entry, ok := s.App.GameEntryMap[lower]
			if !ok {
				continue
			}
			items = append(items, models.ContentItem{
				TitleID:     strings.ToUpper(titleID),
				ContentType: guessContentTypeFromName(g),
				DisplayName: g,
				FileName:    entry.FileName,
				Source:      "ia",
				SourceURL:   app.IADownloadBase + entry.CollectionID + "/" + entry.FileName,
				Installed:   false,
			})
		}
	}
	return items
}

// ============================================================
//  Matching helpers
// ============================================================

func (s *Service) matchesGameName(candidateNameLower, searchNameLower, titleID string) bool {
	if searchNameLower != "" {
		if strings.Contains(candidateNameLower, searchNameLower) {
			return true
		}
		// Fuzzy: strip parenthetical suffixes and compare bases
		candidateBase := strings.ToLower(strings.Split(candidateNameLower, " (")[0])
		searchBase := strings.ToLower(strings.Split(searchNameLower, " (")[0])
		if candidateBase == searchBase {
			return true
		}
	}
	// Also allow matching by TitleID if it appears in the filename.
	// This is rarer but useful for games with very different naming.
	if titleID != "" && strings.Contains(strings.ToUpper(candidateNameLower), titleID) {
		return true
	}
	return false
}

func guessContentTypeFromName(name string) string {
	lower := strings.ToLower(name)
	if isTitleUpdateEntry(name) {
		return "00005000"
	}
	if strings.Contains(lower, "(xbla)") || strings.Contains(lower, "arcade") {
		return "00001000"
	}
	if strings.Contains(lower, "(xblig)") || strings.Contains(lower, "indie") {
		return "00002000"
	}
	if strings.Contains(lower, "(digital)") || strings.Contains(lower, "world) (v") {
		return "00000002"
	}
	return "00000002" // default DLC / add-on
}

// QueueContentDownload prepares a content file for download & FTP.
func (s *Service) QueueContentDownload(req models.ContentQueueRequest, xboxConn *models.XboxConnection) error {
	safeName := helpers.SanitizeFilename(req.GameName + "_" + req.DisplayName)
	gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	if req.SourceURL != "" {
		s.App.Logf("CONTENT QUEUE: %s from %s", req.DisplayName, req.SourceURL)
		fileName := filepath.Base(req.SourceURL)
		if fileName == "" {
			fileName = req.FileName
		}
		if fileName == "" {
			fileName = req.DisplayName + ".bin"
		}
		localPath := filepath.Join(gameDir, fileName)
		if err := s.downloadURLToFile(req.SourceURL, localPath); err != nil {
			s.App.LogStatus(req.GameName, "Error", fmt.Sprintf("Content download failed: %v", err))
			return err
		}

		if xboxConn != nil && xboxConn.Mode == "ftp" {
			drive := strings.TrimSuffix(xboxConn.Drive, ":")
			base := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", drive, strings.ToUpper(req.TitleID), strings.ToLower(req.ContentType))
			fc, err := s.FTP.ConnectWithRetry(xboxConn.IP)
			if err != nil {
				return err
			}
			defer fc.Quit()
			ftp.MkdirAll(fc, base)
			info, _ := os.Stat(localPath)
			var xfer int64
			if err := s.FTP.UploadFile(fc, localPath, base+"/"+fileName, req.GameName, &xfer, info.Size(), 1, 1, time.Now(), new(float64)); err != nil {
				return err
			}
			os.RemoveAll(gameDir)
			s.App.LogFTPComplete(req.GameName, req.TitleID, xboxConn.IP)
		} else {
			relPath := fmt.Sprintf("Content\\0000000000000000\\%s\\%s\\", strings.ToUpper(req.TitleID), strings.ToLower(req.ContentType))
			s.updateContentINI(gameDir, req.GameName, req.TitleID, fileName, relPath)
			s.App.LogStatus(req.GameName, "Ready", "Ready to Install")
		}
	} else {
		s.App.Logf("CONTENT QUEUE: %s has no direct URL — needs manual trigger from browse store", req.DisplayName)
	}
	return nil
}

func (s *Service) downloadURLToFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.ReadFrom(resp.Body)
	return err
}

func (s *Service) updateContentINI(dir, gameName, titleID, fileName, relPath string) {
	iniPath := filepath.Join(dir, ".game.ini")
	lines := []string{
		"[Content]",
		fmt.Sprintf("name=%s", gameName),
		fmt.Sprintf("titleid=%s", strings.ToUpper(titleID)),
		fmt.Sprintf("filename=%s", fileName),
		fmt.Sprintf("path=%s", relPath),
		"type=raw",
	}
	os.WriteFile(iniPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}
