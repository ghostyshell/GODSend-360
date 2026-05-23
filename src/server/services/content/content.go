// content.go — DLC / Title Update discovery and management service.
package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	"godsend/infrastructure/torrent"
	"godsend/models"
	"godsend/utils"

	goftp "github.com/jlaffaye/ftp"
)

// Service handles DLC/TU discovery, scanning, and queueing.
type Service struct {
	App     *app.App
	FTP     *ftp.Service
	Torrent *torrent.Service
}

// contentMarker is the JSON shape written to .godsend.json on the Xbox
// so the scanner can map an installed file back to its source metadata.
type contentMarker struct {
	FileName     string `json:"file_name"`
	DisplayName  string `json:"display_name"`
	Source       string `json:"source"`
	SourceURL    string `json:"source_url,omitempty"`
	Size         int64  `json:"size"`
	DownloadedAt string `json:"downloaded_at"`
}

const godsendMarkerName = ".godsend.json"

// ftpReadFile downloads a small remote file via an open FTP connection.
func (s *Service) ftpReadFile(conn *goftp.ServerConn, path string) ([]byte, error) {
	r, err := conn.Retr(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// writeContentMarker creates or updates the .godsend.json marker in the
// Xbox content directory so ScanInstalledContent can recover rich metadata.
func (s *Service) writeContentMarker(conn *goftp.ServerConn, basePath string, item models.ContentItem) error {
	markerPath := joinFtpPath(basePath, godsendMarkerName)
	var existing []contentMarker
	if data, err := s.ftpReadFile(conn, markerPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	// Replace any existing entry for the same file_name + source, otherwise append.
	found := false
	for i := range existing {
		if strings.EqualFold(existing[i].FileName, item.FileName) &&
			strings.EqualFold(existing[i].Source, item.Source) {
			existing[i].DisplayName = item.DisplayName
			existing[i].SourceURL = item.SourceURL
			existing[i].Size = item.Size
			existing[i].DownloadedAt = time.Now().UTC().Format(time.RFC3339)
			found = true
			break
		}
	}
	if !found {
		existing = append(existing, contentMarker{
			FileName:     item.FileName,
			DisplayName:  item.DisplayName,
			Source:       item.Source,
			SourceURL:    item.SourceURL,
			Size:         item.Size,
			DownloadedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}

	data, err := json.Marshal(existing)
	if err != nil {
		return err
	}
	return conn.Stor(markerPath, bytes.NewReader(data))
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
		var fileNames []string
		for _, sf := range subFiles {
			if sf.Type == goftp.EntryTypeFile {
				// Skip GODsend marker files; Xbox ignores them.
				if strings.HasPrefix(strings.ToLower(sf.Name), ".godsend") {
					continue
				}
				size += int64(sf.Size)
				fileNames = append(fileNames, sf.Name)
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
		if len(fileNames) > 0 {
			item.FileName = fileNames[0]
		}

		// Read .godsend.json markers and overlay richer metadata.
		markerPath := joinFtpPath(base, e.Name, godsendMarkerName)
		if data, err := s.ftpReadFile(conn, markerPath); err == nil {
			var markers []contentMarker
			if json.Unmarshal(data, &markers) == nil {
				for _, m := range markers {
					if item.FileName != "" && strings.EqualFold(item.FileName, m.FileName) {
						item.DisplayName = m.DisplayName
						item.Source = m.Source
						item.SourceURL = m.SourceURL
						item.Size = m.Size
					}
				}
			}
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
			if !s.containsTU(manifest.TitleUpdates, it) {
				manifest.TitleUpdates = append(manifest.TitleUpdates, it)
			}
		} else {
			if !s.containsDLC(manifest.DLCs, it) {
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

func (s *Service) containsTU(list []models.ContentItem, candidate models.ContentItem) bool {
	for _, it := range list {
		// Exact file-name match (works when both sides have a known filename).
		if candidate.FileName != "" && strings.EqualFold(it.FileName, candidate.FileName) {
			return true
		}
		// Display-name match (useful for TUs with versioned names).
		if candidate.DisplayName != "" && strings.EqualFold(it.DisplayName, candidate.DisplayName) {
			return true
		}
		// Weak dedup: if any TU of the same type is already installed for this
		// TitleID, treat the discovered item as a likely duplicate.
		if it.Installed && it.ContentType == candidate.ContentType && it.TitleID == candidate.TitleID {
			return true
		}
	}
	return false
}

func (s *Service) containsDLC(list []models.ContentItem, candidate models.ContentItem) bool {
	for _, it := range list {
		// Exact file-name match.
		if candidate.FileName != "" && strings.EqualFold(it.FileName, candidate.FileName) {
			return true
		}
		// Display-name match (rare for DLC because installed names are generic,
		// but catches cases where a custom INI or previous scan set a real name).
		if candidate.DisplayName != "" && strings.EqualFold(it.DisplayName, candidate.DisplayName) {
			return true
		}
		// Weak dedup: any installed DLC of the same content type for the same
		// TitleID is treated as a likely duplicate. Xbox 360 puts all DLC for a
		// title into a single 00000002 folder, so we cannot distinguish individual
		// DLCs once installed; prefer not offering a re-download.
		if it.Installed && it.ContentType == candidate.ContentType && it.TitleID == candidate.TitleID {
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
	queueKey := req.GameName + " — " + req.DisplayName
	safeName := helpers.SanitizeFilename(req.GameName + "_" + req.DisplayName)
	gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	// Minerva sources expose torrent paths (e.g. "./No-Intro/.../foo.zip"),
	// not HTTP URLs — route them through the aria2 torrent + extract flow.
	if strings.EqualFold(req.Source, "minerva") {
		s.App.LogStatus(queueKey, "Queued", fmt.Sprintf("Queued %s", req.DisplayName))
		s.App.Logf("CONTENT QUEUE (minerva): %s file=%s", req.DisplayName, req.FileName)
		return s.queueViaTorrent(req, xboxConn, queueKey, safeName, gameDir)
	}

	if req.SourceURL != "" {
		s.App.LogStatus(queueKey, "Queued", fmt.Sprintf("Queued %s", req.DisplayName))
		s.App.Logf("CONTENT QUEUE: %s from %s", req.DisplayName, req.SourceURL)
		fileName := filepath.Base(req.SourceURL)
		if fileName == "" {
			fileName = req.FileName
		}
		if fileName == "" {
			fileName = req.DisplayName + ".bin"
		}
		localPath := filepath.Join(gameDir, fileName)
		s.App.LogStatus(queueKey, "Processing", fmt.Sprintf("Downloading %s…", fileName))
		if err := s.downloadURLToFile(req.SourceURL, localPath); err != nil {
			s.App.LogStatus(queueKey, "Error", fmt.Sprintf("Download failed: %v", err))
			return err
		}

		if xboxConn != nil && xboxConn.Mode == "ftp" {
			drive := strings.TrimSuffix(xboxConn.Drive, ":")
			base := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", drive, strings.ToUpper(req.TitleID), strings.ToLower(req.ContentType))
			s.App.LogStatus(queueKey, "Processing", fmt.Sprintf("FTP uploading to %s…", base))
			fc, err := s.FTP.ConnectWithRetry(xboxConn.IP)
			if err != nil {
				s.App.LogStatus(queueKey, "Error", fmt.Sprintf("FTP connect: %v", err))
				return err
			}
			defer fc.Quit()
			ftp.MkdirAll(fc, base)
			info, _ := os.Stat(localPath)
			var xfer int64
			if err := s.FTP.UploadFile(fc, localPath, base+"/"+fileName, req.GameName, &xfer, info.Size(), 1, 1, time.Now(), new(float64)); err != nil {
				s.App.LogStatus(queueKey, "Error", fmt.Sprintf("FTP upload: %v", err))
				return err
			}
			_ = s.writeContentMarker(fc, base, models.ContentItem{
				TitleID:     req.TitleID,
				ContentType: req.ContentType,
				FileName:    fileName,
				DisplayName: req.DisplayName,
				Source:      req.Source,
				SourceURL:   req.SourceURL,
				Size:        info.Size(),
			})
			os.RemoveAll(gameDir)
			s.App.LogFTPComplete(queueKey, req.TitleID, xboxConn.IP)
		} else {
			relPath := fmt.Sprintf("Content\\0000000000000000\\%s\\%s\\", strings.ToUpper(req.TitleID), strings.ToLower(req.ContentType))
			s.updateContentINI(gameDir, req.GameName, req.TitleID, fileName, relPath)
			s.App.LogStatus(queueKey, "Ready", "Ready to Install")
		}
	} else {
		s.App.Logf("CONTENT QUEUE: %s has no direct URL — needs manual trigger from browse store", req.DisplayName)
	}
	return nil
}

// queueViaTorrent downloads a Minerva DLC/TU via aria2 + torrent, extracts the
// archive, locates the Xbox content file (LIVE/PIRS/CON header), and uploads it
// to the Xbox under Content/0000000000000000/<TitleID>/<ContentType>/.
func (s *Service) queueViaTorrent(req models.ContentQueueRequest, xboxConn *models.XboxConnection, queueKey, safeName, gameDir string) error {
	if s.Torrent == nil {
		s.App.LogStatus(queueKey, "Error", "Torrent service not configured")
		return fmt.Errorf("torrent service not configured")
	}
	if req.FileName == "" {
		s.App.LogStatus(queueKey, "Error", "Missing file_name for Minerva source")
		return fmt.Errorf("missing file_name for minerva source")
	}

	torrentDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_dlctorrent")
	os.MkdirAll(torrentDir, 0755)
	defer os.RemoveAll(torrentDir)

	s.App.LogStatus(queueKey, "Processing", fmt.Sprintf("Torrenting %s…", req.FileName))
	entry := models.MinervaEntry{FileName: req.FileName, PathParam: req.SourceURL}
	archivePath, err := s.Torrent.DownloadViaTorrent("dlc", torrentDir, queueKey, entry)
	if err != nil {
		s.App.LogStatus(queueKey, "Error", fmt.Sprintf("Torrent: %v", err))
		return err
	}

	s.App.LogStatus(queueKey, "Processing", "Extracting…")
	extDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_dlcext")
	os.RemoveAll(extDir)
	defer os.RemoveAll(extDir)
	if err := utils.ExtractArchive(archivePath, extDir); err != nil {
		s.App.LogStatus(queueKey, "Error", fmt.Sprintf("Extract: %v", err))
		return err
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
		tid, ct := helpers.ParseXboxHeader(p)
		if tid != "" {
			contentFile = p
			titleID = tid
			typeDir = fmt.Sprintf("%08X", ct)
			return io.EOF
		}
		return nil
	})
	if contentFile == "" {
		s.App.LogStatus(queueKey, "Error", "No valid Xbox content found in archive")
		return fmt.Errorf("no content file in archive")
	}

	finalName := filepath.Base(contentFile)
	if xboxConn != nil && xboxConn.Mode == "ftp" {
		drive := strings.TrimSuffix(xboxConn.Drive, ":")
		base := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", drive, titleID, typeDir)
		s.App.LogStatus(queueKey, "Processing", fmt.Sprintf("FTP uploading to %s…", base))
		fc, err := s.FTP.ConnectWithRetry(xboxConn.IP)
		if err != nil {
			s.App.LogStatus(queueKey, "Error", fmt.Sprintf("FTP connect: %v", err))
			return err
		}
		defer fc.Quit()
		ftp.MkdirAll(fc, base)
		info, _ := os.Stat(contentFile)
		var xfer int64
		if err := s.FTP.UploadFile(fc, contentFile, base+"/"+finalName, queueKey, &xfer, info.Size(), 1, 1, time.Now(), new(float64)); err != nil {
			s.App.LogStatus(queueKey, "Error", fmt.Sprintf("FTP upload: %v", err))
			return err
		}
		_ = s.writeContentMarker(fc, base, models.ContentItem{
			TitleID:     titleID,
			ContentType: typeDir,
			FileName:    finalName,
			DisplayName: req.DisplayName,
			Source:      req.Source,
			SourceURL:   req.SourceURL,
			Size:        info.Size(),
		})
		os.RemoveAll(gameDir)
		s.App.LogFTPComplete(queueKey, titleID, xboxConn.IP)
	} else {
		relPath := fmt.Sprintf("Content\\0000000000000000\\%s\\%s\\", titleID, typeDir)
		if err := helpers.CopyFileBuffered(contentFile, filepath.Join(gameDir, finalName)); err != nil {
			s.App.LogStatus(queueKey, "Error", fmt.Sprintf("Copy: %v", err))
			return err
		}
		s.updateContentINI(gameDir, req.GameName, titleID, finalName, relPath)
		s.App.LogStatus(queueKey, "Ready", "Ready to Install")
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
