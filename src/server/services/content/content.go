// content.go — DLC / Title Update discovery and management service.
package content

import (
	"bytes"
	"encoding/binary"
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

// ftpReadHeader downloads only the first 1 KB of a remote file via FTP.
func (s *Service) ftpReadHeader(conn *goftp.ServerConn, path string) ([]byte, error) {
	r, err := conn.Retr(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	if n < 0x368 {
		return nil, fmt.Errorf("header too short")
	}
	return buf[:n], nil
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
	"00009000": "DLC / Add-on",
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

// parseXboxHeaderBytes reads a LIVE/PIRS/CON header from a byte slice and
// returns (TitleID hex, ContentType hex). Used when scanning over FTP.
func parseXboxHeaderBytes(h []byte) (string, string) {
	if len(h) < 0x368 {
		return "", ""
	}
	magic := string(h[0:4])
	if magic != "LIVE" && magic != "PIRS" && magic != "CON " {
		return "", ""
	}
	titleID := strings.ToUpper(fmt.Sprintf("%X", h[0x360:0x364]))
	ct := fmt.Sprintf("%08X", binary.BigEndian.Uint32(h[0x344:0x348]))
	return titleID, ct
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
	// Closure so a mid-scan reconnect (see below) still cleans up the
	// currently-held conn on return — `defer s.FTP.QuitConn(conn)` would
	// capture the original pointer and leak the replacement.
	defer func() { s.FTP.QuitConn(conn) }()

	driveClean := strings.TrimSuffix(drive, ":")
	base := fmt.Sprintf("/%s/Content/0000000000000000/%s", driveClean, strings.ToUpper(titleID))
	s.App.Logf("CONTENT SCAN: drive=%s base=%s", drive, base)

	// Xbox FTP servers may ignore absolute paths in List(); navigate first.
	if err := conn.ChangeDir(base); err != nil {
		s.App.Logf("CONTENT SCAN: ChangeDir error for %s: %v", base, err)
		return &models.InstalledContentReport{TitleID: strings.ToUpper(titleID)}, nil
	}
	entries, err := conn.List("")
	if err != nil {
		s.App.Logf("CONTENT SCAN: list error for %s: %v", base, err)
		return &models.InstalledContentReport{TitleID: strings.ToUpper(titleID)}, nil
	}
	s.App.Logf("CONTENT SCAN: found %d entries in %s", len(entries), base)

	var dlcs, tus []models.ContentItem
	for idx, e := range entries {
		// Aurora's FTP server has been observed to stall when consecutive
		// data-channel (PASV) opens happen too quickly. Give the server a
		// brief breather between subfolder lists.
		if idx > 0 {
			time.Sleep(150 * time.Millisecond)
		}
		s.App.Logf("CONTENT SCAN: processing entry %s", e.Name)
		if e.Type != goftp.EntryTypeFolder {
			s.App.Logf("CONTENT SCAN: skipping non-folder %s", e.Name)
			continue
		}
		ct := strings.ToLower(e.Name)
		if len(ct) != 8 || !helpers.IsHexString(ct) {
			s.App.Logf("CONTENT SCAN: skipping non-hex folder %s", e.Name)
			continue
		}
		if err := conn.ChangeDir(joinFtpPath(base, e.Name)); err != nil {
			s.App.Logf("CONTENT SCAN: ChangeDir error for %s: %v", joinFtpPath(base, e.Name), err)
			continue
		}
		// Bounded-time List so a stalled subfolder cannot hang the whole scan.
		subFiles, listErr := listWithTimeout(conn, 8*time.Second)
		if listErr != nil {
			// Aurora's FTP server intermittently stalls on consecutive PASV
			// data-channel opens within the same session. Drop the poisoned
			// conn and retry the subfolder once with a fresh connection — the
			// first List on a brand-new conn reliably succeeds.
			s.App.Logf("CONTENT SCAN: List timeout for %s — reconnecting and retrying", e.Name)
			s.FTP.QuitConn(conn)
			newConn, rerr := s.FTP.ConnectWithRetry(xboxIP)
			if rerr != nil {
				s.App.Logf("CONTENT SCAN: reconnect failed: %v", rerr)
				conn = nil
				break
			}
			conn = newConn
			if err := conn.ChangeDir(joinFtpPath(base, e.Name)); err != nil {
				s.App.Logf("CONTENT SCAN: ChangeDir after reconnect failed for %s: %v", e.Name, err)
				continue
			}
			subFiles, listErr = listWithTimeout(conn, 8*time.Second)
			if listErr != nil {
				s.App.Logf("CONTENT SCAN: List still failed after reconnect for %s: %v", e.Name, listErr)
				continue
			}
			s.App.Logf("CONTENT SCAN: reconnect recovered %s (%d entries)", e.Name, len(subFiles))
		}
		var size int64
		var fileNames []string
		var hasMarker bool
		for _, sf := range subFiles {
			if sf.Type == goftp.EntryTypeFile {
				// Skip GODsend marker files; Xbox ignores them.
				if strings.EqualFold(sf.Name, godsendMarkerName) {
					hasMarker = true
					continue
				}
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
		s.App.Logf("CONTENT SCAN: ct=%s file=%s count=%d", ct, item.FileName, len(fileNames))
		s.App.Logf("CONTENT SCAN: created item %s (%s)", item.DisplayName, item.ContentType)

		// Read .godsend.json markers only when the directory listing showed
		// the marker file — otherwise we'd pay an FTP RETR roundtrip per
		// subfolder just to learn the file is missing.
		markersByFile := map[string]contentMarker{}
		if hasMarker {
			markerPath := joinFtpPath(".", godsendMarkerName)
			if data, err := s.ftpReadFile(conn, markerPath); err == nil {
				var markers []contentMarker
				if json.Unmarshal(data, &markers) == nil {
					for _, m := range markers {
						markersByFile[strings.ToLower(m.FileName)] = m
					}
					if m, ok := markersByFile[strings.ToLower(item.FileName)]; ok && item.FileName != "" {
						item.DisplayName = m.DisplayName
						item.Source = m.Source
						item.SourceURL = m.SourceURL
						item.Size = m.Size
					}
				}
			}
		}

		// Change back to base directory for next iteration
		if err := conn.ChangeDir(base); err != nil {
			s.App.Logf("CONTENT SCAN: failed to change back to base: %v", err)
			// Continue anyway - next iteration will try to change dir and likely fail
		}

		// Determine true content type: trust folder name for known types, but
		// read file header when the folder name is an unknown / non-DLC/TU type.
		s.App.Logf("CONTENT SCAN: determining true CT for folder %s (ct=%s)", e.Name, ct)
		trueCT := ct
		if item.FileName != "" && ct != "00000002" && ct != "00000001" && ct != "00005000" && ct != "000b0000" {
			headerPath := joinFtpPath(base, e.Name, item.FileName)
			if h, err := s.ftpReadHeader(conn, headerPath); err == nil {
				_, headerCT := parseXboxHeaderBytes(h)
				if headerCT != "" {
					s.App.Logf("CONTENT SCAN: header says ct=%s for folder %s", headerCT, ct)
					trueCT = strings.ToLower(headerCT)
					item.ContentType = trueCT
					// Only overwrite generic display name; preserve marker-enriched name.
					if item.DisplayName == "" || item.DisplayName == contentTypeName(ct) {
						item.DisplayName = contentTypeName(trueCT)
					}
				}
			} else {
				s.App.Logf("CONTENT SCAN: header read error for %s: %v", headerPath, err)
			}
		} else {
			s.App.Logf("CONTENT SCAN: using folder name as CT: %s", trueCT)
		}

		if trueCT == "00005000" || trueCT == "000b0000" {
			// One row per TU file in the folder so the user can independently
			// activate / deactivate each one. A file ending in `.disabled` is
			// treated as installed-but-inactive — Aurora and the Xbox loader
			// ignore non-matching filenames, so renaming is a safe toggle.
			//
			// Display name + version come from the bare (without `.disabled`)
			// filename via tuMetaFromFiles so a single-file folder produces a
			// clean "Title Update vN" label.
			for _, sf := range subFiles {
				if sf.Type != goftp.EntryTypeFile {
					continue
				}
				if strings.EqualFold(sf.Name, godsendMarkerName) ||
					strings.HasPrefix(strings.ToLower(sf.Name), ".godsend") {
					continue
				}
				bare := strings.TrimSuffix(sf.Name, ".disabled")
				active := !strings.HasSuffix(strings.ToLower(sf.Name), ".disabled")
				display, _ := tuMetaFromFiles([]*goftp.Entry{{Name: bare, Type: goftp.EntryTypeFile, Size: sf.Size}})
				tuItem := models.ContentItem{
					TitleID:     strings.ToUpper(titleID),
					ContentType: trueCT,
					DisplayName: display,
					FileName:    sf.Name,
					Size:        int64(sf.Size),
					Installed:   true,
					Active:      active,
					Drive:       driveClean,
				}
				// Per-file marker enrichment (match by bare name so a
				// `.disabled` rename doesn't break the link to its source).
				if m, ok := markersByFile[strings.ToLower(bare)]; ok {
					if m.DisplayName != "" {
						tuItem.DisplayName = m.DisplayName
					}
					tuItem.Source = m.Source
					tuItem.SourceURL = m.SourceURL
					if m.Size > 0 {
						tuItem.Size = m.Size
					}
				}
				tus = append(tus, tuItem)
				s.App.Logf("CONTENT SCAN: added TU %s (%s, active=%v, file=%s)", tuItem.DisplayName, tuItem.ContentType, tuItem.Active, tuItem.FileName)
			}
		} else if trueCT == "00000002" || trueCT == "00000001" || trueCT == "00009000" {
			// One row per DLC file in the folder. Xbox 360 bundles every DLC
			// for a title into a single 00000002 directory, so picking just
			// fileNames[0] previously hid all but the first installed file
			// from the UI. Each row carries its own filename so the renderer
			// can target deletes / moves precisely.
			// 00009000 is a non-standard content type seen in some Minerva/
			// No-Intro DLC archives — the file header confirms it.
			for _, sf := range subFiles {
				if sf.Type != goftp.EntryTypeFile {
					continue
				}
				if strings.EqualFold(sf.Name, godsendMarkerName) ||
					strings.HasPrefix(strings.ToLower(sf.Name), ".godsend") {
					continue
				}
				dlcItem := models.ContentItem{
					TitleID:     strings.ToUpper(titleID),
					ContentType: trueCT,
					FileName:    sf.Name,
					Size:        int64(sf.Size),
					Installed:   true,
					Active:      false,
					Drive:       driveClean,
					DisplayName: sf.Name,
				}
				// Per-file marker enrichment so each bundled DLC keeps its
				// own display name / source / source-url. Without this, the
				// previous code reused fileNames[0]'s marker data for every
				// file in the folder.
				if m, ok := markersByFile[strings.ToLower(sf.Name)]; ok {
					if m.DisplayName != "" {
						dlcItem.DisplayName = m.DisplayName
					}
					dlcItem.Source = m.Source
					dlcItem.SourceURL = m.SourceURL
					if m.Size > 0 {
						dlcItem.Size = m.Size
					}
				}
				dlcs = append(dlcs, dlcItem)
				s.App.Logf("CONTENT SCAN: added DLC %s (%s, file=%s)", dlcItem.DisplayName, dlcItem.ContentType, dlcItem.FileName)
			}
		} else {
			s.App.Logf("CONTENT SCAN: ct=%s not DLC/TU, skipping", ct)
		}
	}

	return &models.InstalledContentReport{
		TitleID:      strings.ToUpper(titleID),
		DLCs:         dlcs,
		TitleUpdates: tus,
	}, nil
}

// SetTUActive activates or deactivates a single TU file under
// /{drive}/Content/0000000000000000/{TitleID}/{ContentType}/.
//
// Activation strategy: a bare filename is "active"; a `<name>.disabled`
// filename is "inactive". When activating, every other non-disabled sibling
// in the same folder is renamed to `.disabled` so only one TU is active.
func (s *Service) SetTUActive(xboxIP, drive, titleID, contentType, fileName string, setActive bool) error {
	conn, err := s.FTP.ConnectWithRetry(xboxIP)
	if err != nil {
		return err
	}
	defer s.FTP.QuitConn(conn)

	driveClean := strings.TrimSuffix(drive, ":")
	folder := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s",
		driveClean, strings.ToUpper(titleID), strings.ToLower(contentType))

	bare := strings.TrimSuffix(fileName, ".disabled")
	disabled := bare + ".disabled"
	bareRemote := folder + "/" + bare
	disabledRemote := folder + "/" + disabled

	if setActive {
		// Rename the target back to bare if it's currently disabled.
		// (Ignore "550" / file-not-found — the user may have clicked twice.)
		_ = conn.Rename(disabledRemote, bareRemote)

		// Deactivate every other active TU file in this folder.
		if err := conn.ChangeDir(folder); err != nil {
			s.App.Logf("CONTENT SET-ACTIVE: ChangeDir %s: %v", folder, err)
			return err
		}
		entries, err := listWithTimeout(conn, 8*time.Second)
		if err != nil {
			s.App.Logf("CONTENT SET-ACTIVE: list failed (%v) — reconnecting", err)
			s.FTP.QuitConn(conn)
			newConn, rerr := s.FTP.ConnectWithRetry(xboxIP)
			if rerr != nil {
				return rerr
			}
			conn = newConn
			if cerr := conn.ChangeDir(folder); cerr != nil {
				return cerr
			}
			entries, err = listWithTimeout(conn, 8*time.Second)
			if err != nil {
				return err
			}
		}
		for _, e := range entries {
			if e.Type != goftp.EntryTypeFile {
				continue
			}
			if strings.EqualFold(e.Name, bare) {
				continue
			}
			lower := strings.ToLower(e.Name)
			if strings.HasSuffix(lower, ".disabled") || strings.HasPrefix(lower, ".godsend") {
				continue
			}
			from := folder + "/" + e.Name
			to := folder + "/" + e.Name + ".disabled"
			if rerr := conn.Rename(from, to); rerr != nil {
				s.App.Logf("CONTENT SET-ACTIVE: failed to disable sibling %s: %v", e.Name, rerr)
			} else {
				s.App.Logf("CONTENT SET-ACTIVE: disabled sibling %s", e.Name)
			}
		}
		s.App.Logf("CONTENT SET-ACTIVE: activated %s in %s", bare, folder)
		return nil
	}

	// Deactivating: rename bare → .disabled.
	if err := conn.Rename(bareRemote, disabledRemote); err != nil {
		return fmt.Errorf("rename to disabled failed: %v", err)
	}
	s.App.Logf("CONTENT SET-ACTIVE: deactivated %s in %s", bare, folder)
	return nil
}

// listWithTimeout runs conn.List("") under a hard deadline so a stalled
// PASV data-channel open (a known Aurora FTP failure mode after several
// consecutive listings) can't hang the whole scan indefinitely.
//
// On timeout the goroutine running List may continue to leak in the
// background until the FTP server eventually responds or the connection is
// closed by the caller's QuitConn — both are bounded by the deferred Quit.
func listWithTimeout(conn *goftp.ServerConn, d time.Duration) ([]*goftp.Entry, error) {
	type result struct {
		entries []*goftp.Entry
		err     error
	}
	done := make(chan result, 1)
	go func() {
		e, err := conn.List("")
		done <- result{e, err}
	}()
	select {
	case r := <-done:
		return r.entries, r.err
	case <-time.After(d):
		return nil, fmt.Errorf("List timed out after %s", d)
	}
}

// tuMetaFromFiles derives a TU display name (from the highest TUxx file) and
// whether the TU is "active" (any file present) from an already-fetched FTP
// listing. Avoids a second ChangeDir/List on the same folder, which Aurora's
// FTP server has been observed to stall on.
func tuMetaFromFiles(entries []*goftp.Entry) (string, bool) {
	var bestVer int
	var fileCount int
	for _, e := range entries {
		if e.Type != goftp.EntryTypeFile {
			continue
		}
		fileCount++
		name := e.Name
		if strings.HasPrefix(strings.ToLower(name), "tu") {
			verStr := name[2:]
			if idx := strings.IndexAny(verStr, "_."); idx > 0 {
				verStr = verStr[:idx]
			}
			if v, err := strconv.Atoi(verStr); err == nil && v > bestVer {
				bestVer = v
			}
		}
	}
	name := "Title Update"
	if bestVer > 0 {
		name = "Title Update v" + strconv.Itoa(bestVer)
	}
	return name, fileCount > 0
}

// ============================================================
// Discovery — separated into DLC (Minerva/IA) and TU (XboxUnity)
// ============================================================

// DiscoverDLC fetches available DLC for a TitleID from Minerva and IA,
// merging with installed items from Xbox. Does NOT call XboxUnity.
func (s *Service) DiscoverDLC(titleID, gameName string, xboxIP, drive string) (*models.ContentManifest, error) {
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
			s.App.Logf("CONTENT DISCOVER: installed scan found %d DLC, %d TU", len(installed.DLCs), len(installed.TitleUpdates))
		} else {
			s.App.Logf("CONTENT DISCOVER: installed scan error: %v", err)
		}
	}

	// Search IA + Minerva for DLC only (no XboxUnity).
	s.App.Logf("CONTENT DISCOVER: starting Minerva scan for %s", gameName)
	candidates := s.discoverFromMinerva(gameName, titleID)
	s.App.Logf("CONTENT DISCOVER: Minerva returned %d items", len(candidates))
	candidates = append(candidates, s.discoverFromIA(gameName, titleID)...)
	s.App.Logf("CONTENT DISCOVER: %d candidates from Minerva+IA", len(candidates))

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
	s.App.Logf("CONTENT DISCOVER: final manifest %d DLC, %d TU", len(manifest.DLCs), len(manifest.TitleUpdates))

	// Keep the highest TU version information for display.
	s.normalizeTUVersions(manifest.TitleUpdates)

	return manifest, nil
}

// DiscoverTitleUpdates fetches available Title Updates for a TitleID from XboxUnity.
// Returns only TUs, not DLC.
func (s *Service) DiscoverTitleUpdates(titleID string) []models.ContentItem {
	titleID = strings.ToUpper(titleID)
	var tus []models.ContentItem

	// XboxUnity API call (can be slow, so it’s separate from DLC loading)
	tus = append(tus, s.discoverFromXboxUnity(titleID)...)

	// Also check Minerva/IA for any TU entries they might have
	// (some games have TUs in the dlc/digital collections)
	tus = append(tus, s.discoverFromMinerva("", titleID)...)
	tus = append(tus, s.discoverFromIA("", titleID)...)

	// Deduplicate
	deduped := make([]models.ContentItem, 0)
	seen := make(map[string]bool)
	for _, tu := range tus {
		key := tu.FileName
		if key == "" {
			key = tu.DisplayName + ":" + tu.SourceURL
		}
		if !seen[key] {
			seen[key] = true
			deduped = append(deduped, tu)
		}
	}

	s.App.Logf("CONTENT TU: %d title updates for %s", len(deduped), titleID)
	return deduped
}

// ============================================================
// Source helpers
// ============================================================

// isTUContentType returns true for any recognized Title Update content type.
func isTUContentType(ct string) bool {
	l := strings.ToLower(ct)
	return l == "00005000" || l == "000b0000"
}

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
		// Source URL match — strongest identifier when both sides know the origin.
		if candidate.SourceURL != "" && strings.EqualFold(it.SourceURL, candidate.SourceURL) {
			return true
		}
		// Weak dedup: if the same TU version is already installed for this
		// TitleID, treat the discovered item as a likely duplicate.
		// We compare using isTUContentType so 00005000 and 000b0000 are treated
		// as equivalent — the file header may report one while the candidate guesses the other.
		if it.Installed && isTUContentType(it.ContentType) && isTUContentType(candidate.ContentType) && it.TitleID == candidate.TitleID && it.Version == candidate.Version {
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
		// Source URL match — strongest identifier when both sides know the origin.
		if candidate.SourceURL != "" && strings.EqualFold(it.SourceURL, candidate.SourceURL) {
			return true
		}
		// (Per-content-type weak dedup intentionally removed: Xbox 360 bundles
		// every DLC for a title into one 00000002 folder, so the old rule
		// "any installed DLC of the same content type = duplicate" hid every
		// Minerva/IA candidate the moment a single DLC was installed. Each
		// installed file now gets its own row from the scan, so accurate
		// dedup happens at the filename / display-name / source-url level.)
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

	// Copy data under lock to avoid holding locks during iteration
	s.App.MinervaGameCacheMu.RLock()
	minervaCache := make(map[string][]string)
	for k, v := range s.App.MinervaGameCache {
		minervaCache[k] = append([]string(nil), v...)
	}
	s.App.MinervaGameCacheMu.RUnlock()

	s.App.MinervaEntryMapMu.RLock()
	entryMap := make(map[string]models.MinervaEntry)
	for k, v := range s.App.MinervaEntryMap {
		entryMap[k] = v
	}
	s.App.MinervaEntryMapMu.RUnlock()

	for _, platform := range []string{"dlc", "xbla", "digital", "xblig", "games"} {
		games, ok := minervaCache[platform]
		if !ok {
			continue
		}
		for _, g := range games {
			lower := strings.ToLower(g)
			if !s.matchesGameName(lower, searchLower, titleID) {
				continue
			}
			entry, ok := entryMap[lower]
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

	// Copy data under lock, then iterate without holding locks
	s.App.IAGameCacheMu.RLock()
	iaCache := make(map[string][]string)
	for k, v := range s.App.IAGameCache {
		iaCache[k] = append([]string(nil), v...)
	}
	s.App.IAGameCacheMu.RUnlock()

	s.App.GameEntryMapMu.RLock()
	entryMap := make(map[string]models.IAGameEntry)
	for k, v := range s.App.GameEntryMap {
		entryMap[k] = v
	}
	s.App.GameEntryMapMu.RUnlock()

	for _, platform := range []string{"dlc", "xbla", "digital", "xblig", "games"} {
		games, ok := iaCache[platform]
		if !ok {
			continue
		}
		for _, g := range games {
			lower := strings.ToLower(g)
			if !s.matchesGameName(lower, searchLower, titleID) {
				continue
			}
			entry, ok := entryMap[lower]
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
//  XboxUnity Title Update discovery
// ============================================================

func (s *Service) discoverFromXboxUnity(titleID string) []models.ContentItem {
	var items []models.ContentItem
	apiURL := "http://xboxunity.net/Resources/Lib/TitleUpdateInfo.php?titleid=" + strings.ToUpper(titleID)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		s.App.Logf("XboxUnity TU: request build error: %v", err)
		return items
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		s.App.Logf("XboxUnity TU: request failed for %s: %v", titleID, err)
		return items
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		s.App.Logf("XboxUnity TU: HTTP %d for %s", resp.StatusCode, titleID)
		return items
	}

	var result struct {
		Type     int `json:"Type"`
		MediaIDS []struct {
			MediaID string `json:"MediaID"`
			Updates []struct {
				TitleUpdateID string `json:"TitleUpdateID"`
				Version       string `json:"Version"`
				Hash          string `json:"hash"`
				Size          string `json:"Size"`
				UploadDate    string `json:"UploadDate"`
				Name          string `json:"Name"`
				BaseVersion   string `json:"BaseVersion"`
			} `json:"Updates"`
			Count int `json:"Count"`
		} `json:"MediaIDS"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		s.App.Logf("XboxUnity TU: JSON decode error for %s: %v", titleID, err)
		return items
	}

	for _, media := range result.MediaIDS {
		for _, u := range media.Updates {
			ver, _ := strconv.Atoi(u.Version)
			size, _ := strconv.ParseInt(u.Size, 10, 64)
			size *= 1024 // Size is in KB
			items = append(items, models.ContentItem{
				TitleID:     strings.ToUpper(titleID),
				ContentType: "00005000",
				DisplayName: fmt.Sprintf("Title Update v%s", u.Version),
				FileName:    fmt.Sprintf("%s_TU%s_%s.bin", strings.ToUpper(titleID), u.Version, media.MediaID),
				Source:      "xboxunity",
				SourceURL:   fmt.Sprintf("http://xboxunity.net/Resources/Lib/TitleUpdate.php?tuid=%s", u.TitleUpdateID),
				Size:        size,
				Version:     ver,
				Installed:   false,
			})
		}
	}
	s.App.Logf("XboxUnity TU: found %d updates for %s", len(items), titleID)
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
		// Prefer req.FileName (already sanitized by the discovery layer),
		// then fall back to a safe basename derived from the URL.
		fileName := req.FileName
		if fileName == "" {
			fileName = filepath.Base(req.SourceURL)
			// URLs like http://xboxunity.net/TitleUpdate.php?tuid=123 produce
			// a basename with query params — strip them.
			if idx := strings.IndexAny(fileName, "?=&"); idx > 0 {
				fileName = fileName[:idx]
			}
		}
		if fileName == "" {
			fileName = helpers.SanitizeFilename(req.DisplayName) + ".bin"
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
			defer s.FTP.QuitConn(fc)
			ftp.MkdirAll(fc, base)
			info, _ := os.Stat(localPath)
			// Pre-upload marker (see queueViaTorrent for rationale): leaves a
			// dedup breadcrumb if the upload stalls partway.
			_ = s.writeContentMarker(fc, base, models.ContentItem{
				TitleID:     req.TitleID,
				ContentType: req.ContentType,
				FileName:    fileName,
				DisplayName: req.DisplayName,
				Source:      req.Source,
				SourceURL:   req.SourceURL,
				Size:        info.Size(),
			})
			var xfer int64
			if err := s.FTP.UploadFile(fc, localPath, base+"/"+fileName, req.GameName, &xfer, info.Size(), 1, 1, time.Now(), new(float64)); err != nil {
				s.App.LogStatus(queueKey, "Error", fmt.Sprintf("FTP upload: %v", err))
				return err
			}
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

	var contentFile, headerTitleID, typeDir string
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
			headerTitleID = tid
			typeDir = fmt.Sprintf("%08X", ct)
			return io.EOF
		}
		return nil
	})
	if contentFile == "" {
		s.App.LogStatus(queueKey, "Error", "No valid Xbox content found in archive")
		return fmt.Errorf("no content file in archive")
	}

	// Use the game's TitleID from the request for the upload path so the
	// scan on the game's page finds it. The Xbox reads the file header anyway.
	destTitleID := strings.ToUpper(req.TitleID)
	if headerTitleID != "" && !strings.EqualFold(headerTitleID, destTitleID) {
		s.App.Logf("CONTENT QUEUE: header TitleID %s differs from request %s — uploading to request folder", headerTitleID, destTitleID)
	}

	finalName := filepath.Base(contentFile)
	if xboxConn != nil && xboxConn.Mode == "ftp" {
		drive := strings.TrimSuffix(xboxConn.Drive, ":")
		base := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", drive, destTitleID, typeDir)
		s.App.LogStatus(queueKey, "Processing", fmt.Sprintf("FTP uploading to %s…", base))
		fc, err := s.FTP.ConnectWithRetry(xboxConn.IP)
		if err != nil {
			s.App.LogStatus(queueKey, "Error", fmt.Sprintf("FTP connect: %v", err))
			return err
		}
		defer s.FTP.QuitConn(fc)
		ftp.MkdirAll(fc, base)
		info, _ := os.Stat(contentFile)
		// Pre-upload marker — written BEFORE the bytes go up so a stalled or
		// aborted upload still leaves a breadcrumb tying the partial file on
		// the Xbox to its Minerva source. Without it, a half-installed DLC
		// shows on the library page as a hash-named row that can't be
		// matched to its catalog entry. Size will be overwritten with the
		// real value once the upload completes.
		_ = s.writeContentMarker(fc, base, models.ContentItem{
			TitleID:     destTitleID,
			ContentType: typeDir,
			FileName:    finalName,
			DisplayName: req.DisplayName,
			Source:      req.Source,
			SourceURL:   req.SourceURL,
			Size:        info.Size(),
		})
		var xfer int64
		if err := s.FTP.UploadFile(fc, contentFile, base+"/"+finalName, queueKey, &xfer, info.Size(), 1, 1, time.Now(), new(float64)); err != nil {
			s.App.LogStatus(queueKey, "Error", fmt.Sprintf("FTP upload: %v", err))
			return err
		}
		_ = s.writeContentMarker(fc, base, models.ContentItem{
			TitleID:     destTitleID,
			ContentType: typeDir,
			FileName:    finalName,
			DisplayName: req.DisplayName,
			Source:      req.Source,
			SourceURL:   req.SourceURL,
			Size:        info.Size(),
		})
		os.RemoveAll(gameDir)
		s.App.LogFTPComplete(queueKey, destTitleID, xboxConn.IP)
	} else {
		relPath := fmt.Sprintf("Content\\0000000000000000\\%s\\%s\\", destTitleID, typeDir)
		if err := helpers.CopyFileBuffered(contentFile, filepath.Join(gameDir, finalName)); err != nil {
			s.App.LogStatus(queueKey, "Error", fmt.Sprintf("Copy: %v", err))
			return err
		}
		s.updateContentINI(gameDir, req.GameName, destTitleID, finalName, relPath)
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
