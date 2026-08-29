// digital.go - content install, generic game, and digital/XBLA/DLC/XBLIG processing.
package pipeline

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"godsend/app"
	"godsend/infrastructure/ftp"
	"godsend/infrastructure/helpers"
	"godsend/models"
	"godsend/utils"
)

// ==========================================
// CONTENT INSTALL (Disc 2+ DLC path)
// ==========================================

func (s *Service) processContentInstallFromISO(gameName, safeName, isoPath string, xboxConn *models.XboxConnection) {
	s.App.Logf("=== Content install: %s ===", gameName)

	s.App.LogStatus(gameName, "Processing", "Reading disc info...")
	info, err := utils.ProbeISODiscInfo(isoPath)
	if err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Disc probe: %v", err))
		return
	}
	titleID := fmt.Sprintf("%08X", info.TitleID)
	if models.IsContentDiscPlaceholderTitleID(info.TitleID) {
		if probed, err := utils.ProbeContentPackageTitleID(isoPath, info); err == nil && probed != 0 {
			s.App.Logf("Content install: placeholder TitleID %s resolved to %08X from content packages", titleID, probed)
			titleID = fmt.Sprintf("%08X", probed)
		} else if guessed := models.GuessTitleIDFromMultiDiscName(gameName); guessed != 0 {
			s.App.Logf("Content install: placeholder TitleID %s overridden to %08X from game name", titleID, guessed)
			titleID = fmt.Sprintf("%08X", guessed)
		} else {
			s.App.Logf("Content install: WARNING - TitleID %s is a known placeholder; could not resolve parent title from content packages or game name %q - content may install to wrong folder", titleID, gameName)
		}
	}
	s.App.Logf("Content install: TitleID=%s disc=%d/%d", titleID, info.DiscNumber, info.DiscCount)

	s.App.LogStatus(gameName, "Processing", "Extracting content files from ISO...")
	contentDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_content")
	os.RemoveAll(contentDir)
	os.MkdirAll(contentDir, 0755)
	if err := utils.ExtractXDVDFSContentToDir(isoPath, contentDir, info); err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Content extract: %v", err))
		os.RemoveAll(contentDir)
		return
	}

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		s.App.LogStatus(gameName, "Processing", "FTP Transfer starting...")
		if err := s.FTP.TransferContent(contentDir, xboxConn, gameName, titleID); err != nil {
			s.App.Logf("FTP: initial content transfer failed for %s: %v - scheduling for retry", gameName, err)
			gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
			job := ftp.PendingFTPJob{
				ID:        helpers.SanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
				GameName:  gameName,
				Type:      "content",
				SourceDir: contentDir,
				GameDir:   gameDir,
				XboxIP:    xboxConn.IP,
				Drive:     xboxConn.Drive,
				TitleID:   titleID,
				CreatedAt: time.Now(),
			}
			s.FTP.SchedulePendingFTP(job)
			return
		}
		os.RemoveAll(contentDir)
		s.App.LogFTPComplete(gameName, titleID, xboxConn.IP)
	} else {
		gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
		os.MkdirAll(gameDir, 0755)

		s.App.LogStatus(gameName, "Processing", "Packaging content for transfer...")
		partName := safeName + "_Part1.7z"
		if err := utils.CreateZipFromDir(contentDir, filepath.Join(gameDir, partName)); err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("Archive: %v", err))
			os.RemoveAll(contentDir)
			return
		}
		os.RemoveAll(contentDir)
		s.App.GamePartsMap.Store(gameName, []string{partName})
		relPath := fmt.Sprintf("Content\\0000000000000000\\%s\\00000002\\", titleID)
		s.updateGameINI_Content(gameDir, gameName, titleID, partName, relPath)
		s.App.LogStatus(gameName, "Ready", "Ready to Install")
	}
	s.App.Logf("=== Complete (Content): %s ===", gameName)
}

// ==========================================
// GENERIC GAME PROCESSING (XBOX_360_* collections)
// ==========================================

func (s *Service) ProcessGenericGame(gameName string) {
	s.App.Logf("=== Generic Game: %s ===", gameName)
	safeName := helpers.SanitizeFilename(gameName)
	if safeName == "" {
		s.App.LogStatus(gameName, "Error", "Invalid game name")
		return
	}
	var xboxConn *models.XboxConnection
	if c, ok := s.App.XboxConnections.Load(gameName); ok {
		cc := c.(models.XboxConnection)
		xboxConn = &cc
	}
	gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	s.App.LogStatus(gameName, "Processing", "Searching Internet Archive (Games)...")
	entry, err := s.IA.FindEntry(gameName, "games")
	if err != nil {
		s.App.Logf("ERROR [%s]: IA search failed: %v", gameName, err)
		s.App.LogStatus(gameName, "Error", err.Error())
		return
	}
	downloadURL := app.IADownloadBase + entry.CollectionID + "/" + url.PathEscape(entry.FileName)
	s.App.Logf("IA Download: %s → %s", gameName, entry.FileName)

	archivePath := filepath.Join(s.App.ToolsDir, "Temp", safeName+filepath.Ext(entry.FileName))
	s.App.LogStatus(gameName, "Processing", "Downloading from Internet Archive...")
	if err := s.downloadIAOrDebrid(downloadURL, archivePath, gameName, entry.AccessRestricted); err != nil {
		s.App.Logf("ERROR [%s]: IA download failed: %v", gameName, err)
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Download: %v", err))
		return
	}
	defer os.Remove(archivePath)

	s.App.LogStatus(gameName, "Processing", "Extracting archive...")
	extDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_ext")
	os.RemoveAll(extDir)
	defer os.RemoveAll(extDir)
	if err := utils.ExtractArchive(archivePath, extDir); err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	installType := s.App.LookupInstallType(gameName)

	isoPath := helpers.FindFileByExt(extDir, ".iso")
	xexFolder := helpers.FindXEXFolder(extDir)

	if installType == "xex" {
		if xexFolder == "" {
			s.App.LogStatus(gameName, "Error", "XEX install needs a loose game folder in the archive. Try GOD (ISO) or DLC (Disc 2 content ISO).")
			return
		}
		folderName := filepath.Base(xexFolder)
		s.App.LogStatus(gameName, "Processing", fmt.Sprintf("XEX folder: %s", folderName))
		if xboxConn != nil && xboxConn.Mode == "ftp" {
			if err := s.FTP.TransferXEX(xexFolder, folderName, xboxConn, gameName); err != nil {
				s.App.Logf("FTP: initial XEX transfer failed for %s: %v - scheduling for retry", gameName, err)
				job := ftp.PendingFTPJob{
					ID:         helpers.SanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
					GameName:   gameName,
					Type:       "xex",
					SourceDir:  xexFolder,
					GameDir:    gameDir,
					XboxIP:     xboxConn.IP,
					Drive:      xboxConn.Drive,
					FolderName: folderName,
					CreatedAt:  time.Now(),
				}
				s.FTP.SchedulePendingFTP(job)
			} else {
				os.RemoveAll(gameDir)
				s.App.LogFTPComplete(gameName, "", xboxConn.IP)
			}
		} else {
			partName := fmt.Sprintf("%s_Part1.7z", safeName)
			if err := utils.CreateZipFromDir(xexFolder, filepath.Join(gameDir, partName)); err != nil {
				s.App.LogStatus(gameName, "Error", fmt.Sprintf("Archive XEX: %v", err))
				return
			}
			s.App.GamePartsMap.Store(gameName, []string{partName})
			s.updateGameINI_XEX(gameDir, gameName, folderName, partName)
			s.App.LogStatus(gameName, "Ready", "Ready to Install")
		}
		return
	}

	if installType == "content" {
		if isoPath == "" {
			s.App.LogStatus(gameName, "Error", "DLC/content install needs an ISO. Pick XEX if this release is a loose-folder rip.")
			return
		}
		s.processContentInstallFromISO(gameName, safeName, isoPath, xboxConn)
		return
	}

	// GOD (default): ISO → Games on Demand.
	if isoPath != "" {
		s.App.LogStatus(gameName, "Processing", "ISO detected, converting to GOD...")
		godDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_GOD")
		os.MkdirAll(godDir, 0755)
		if err := utils.RunIso2GodNative(isoPath, godDir, Iso2GodResolveDisplayTitle); err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("GOD convert: %v", err))
			os.RemoveAll(godDir)
			return
		}
		titleID, mediaID, err := helpers.DetectGodStructure(godDir)
		if err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("GOD detect: %v", err))
			os.RemoveAll(godDir)
			return
		}
		s.finalizeGOD(gameName, safeName, gameDir, godDir, titleID, mediaID, xboxConn)
		return
	}

	if xexFolder != "" {
		s.App.LogStatus(gameName, "Error", "No ISO in archive. Choose Install method: XEX for this folder layout, or use a Redump-style ISO release.")
		return
	}
	s.App.LogStatus(gameName, "Error", "No ISO or XEX content found in archive")
	s.App.Logf("=== Complete (Generic): %s ===", gameName)
}

// ==========================================
// DIGITAL / XBLA / DLC / XBLIG PROCESSING
// ==========================================

// digitalContentFile is one valid Xbox content package found inside a
// digital/XBLA/DLC archive, tagged with the titleID/content-type its own
// LIVE/PIRS/CON header carries.
type digitalContentFile struct {
	path    string
	size    int64
	titleID string
	typeDir string
}

// findDigitalContentFiles walks extDir and returns every file with a valid
// Xbox content header. An archive can bundle more than one package (base
// game + title update, or several DLC packs); every one must be returned or
// callers silently drop everything after the first. Shared by ProcessDigital
// and ProcessMinervaDigital - Internet Archive and Minerva feed the same
// archive layout into the same install logic.
func findDigitalContentFiles(extDir string) []digitalContentFile {
	var found []digitalContentFile
	filepath.Walk(extDir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() || i.Size() < 0x368 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".txt" || ext == ".nfo" || ext == ".jpg" {
			return nil
		}
		if tid, ct := helpers.ParseXboxHeader(p); tid != "" {
			found = append(found, digitalContentFile{path: p, size: i.Size(), titleID: tid, typeDir: fmt.Sprintf("%08X", ct)})
		}
		return nil
	})
	return found
}

// primaryDigitalContentFile picks the file to install when only one can be
// used (the non-FTP "type=raw" manifest supports a single file per game).
// Walk order is lexical, so contentFiles[0] can land on a title update or a
// DLC pack instead of the base game if it happens to sort first; the largest
// file is a much safer guess at "the game".
func primaryDigitalContentFile(contentFiles []digitalContentFile) digitalContentFile {
	best := contentFiles[0]
	for _, cf := range contentFiles[1:] {
		if cf.size > best.size {
			best = cf
		}
	}
	return best
}

// distinctTitleIDs returns the set of TitleIDs found across contentFiles, for
// a diagnostic warning when an archive's packages don't all agree.
func distinctTitleIDs(contentFiles []digitalContentFile) []string {
	seen := map[string]bool{}
	var ids []string
	for _, cf := range contentFiles {
		if !seen[cf.titleID] {
			seen[cf.titleID] = true
			ids = append(ids, cf.titleID)
		}
	}
	return ids
}

// transferDigitalContentFilesFTP uploads every discovered content package
// over one connection to its own
// /<drive>/Content/0000000000000000/<titleID>/<typeDir> folder, derived
// per-file from that file's own header since a bundle can mix content types.
// Progress is aggregated across all files.
func (s *Service) transferDigitalContentFilesFTP(contentFiles []digitalContentFile, xboxConn *models.XboxConnection, gameName string) error {
	drive := strings.TrimSuffix(xboxConn.Drive, ":")
	fc, err := s.FTP.ConnectWithRetry(xboxConn.IP)
	if err != nil {
		return fmt.Errorf("FTP: %w", err)
	}
	defer s.FTP.QuitConn(fc)

	// Create every destination folder up front, while fc is known good.
	// UploadWithRetry transparently reconnects on a mid-loop failure, and a
	// MkdirAll issued after that point would silently no-op against the dead
	// connection - MakeDir errors are intentionally ignored (folder may
	// already exist), so a missing folder would only surface later as a
	// confusing STOR failure.
	made := map[string]bool{}
	var totalSize int64
	for _, cf := range contentFiles {
		base := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", drive, cf.titleID, cf.typeDir)
		if !made[base] {
			ftp.MkdirAll(fc, base)
			made[base] = true
		}
		totalSize += cf.size
	}

	seenRemote := map[string]bool{}
	var xfer int64
	xferStart := time.Now()
	for i, cf := range contentFiles {
		base := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", drive, cf.titleID, cf.typeDir)
		remote := base + "/" + filepath.Base(cf.path)
		if seenRemote[remote] {
			s.App.Logf("Digital FTP: skipping %s - remote path %s already used by another package in this archive", cf.path, remote)
			continue
		}
		seenRemote[remote] = true
		s.App.Logf("Digital FTP [%d/%d]: TitleID=%s Type=%s %s (%.1f MB)",
			i+1, len(contentFiles), cf.titleID, cf.typeDir, filepath.Base(cf.path), float64(cf.size)/1048576)
		if err := s.FTP.UploadWithRetry(fc, xboxConn.IP, cf.path, remote, gameName, &xfer, totalSize, i+1, len(contentFiles), xferStart); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ProcessDigital(gameName, platform string) {
	s.App.Logf("=== Digital: %s (%s) ===", gameName, platform)
	safeName := helpers.SanitizeFilename(gameName)
	if safeName == "" {
		s.App.LogStatus(gameName, "Error", "Invalid game name")
		return
	}
	var xboxConn *models.XboxConnection
	if c, ok := s.App.XboxConnections.Load(gameName); ok {
		cc := c.(models.XboxConnection)
		xboxConn = &cc
	}
	gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	s.App.LogStatus(gameName, "Processing", "Searching Internet Archive...")
	entry, err := s.IA.FindEntry(gameName, platform)
	if err != nil {
		s.App.LogStatus(gameName, "Error", err.Error())
		return
	}
	downloadURL := app.IADownloadBase + entry.CollectionID + "/" + url.PathEscape(entry.FileName)

	archivePath := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_digi"+filepath.Ext(entry.FileName))
	if err := s.downloadIAOrDebrid(downloadURL, archivePath, gameName, entry.AccessRestricted); err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Download: %v", err))
		return
	}
	defer os.Remove(archivePath)

	s.App.LogStatus(gameName, "Processing", "Extracting...")
	extDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_ext")
	os.RemoveAll(extDir)
	defer os.RemoveAll(extDir)
	if err := utils.ExtractArchive(archivePath, extDir); err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	// Digital archives commonly bundle more than one Xbox content package
	// (e.g. base game + title update, or several DLC packs) - collect every
	// valid one instead of stopping at the first match, or later packages
	// are silently dropped (only the first ever gets transferred).
	contentFiles := findDigitalContentFiles(extDir)

	if len(contentFiles) == 0 {
		s.App.LogStatus(gameName, "Error", "No valid Xbox content found in archive")
		return
	}
	titleID := contentFiles[0].titleID
	s.App.Logf("Digital: TitleID=%s (%d content file(s))", titleID, len(contentFiles))
	if ids := distinctTitleIDs(contentFiles); len(ids) > 1 {
		s.App.Logf("Digital: WARNING - content files carry different TitleIDs: %v", ids)
	}

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		if err := s.transferDigitalContentFilesFTP(contentFiles, xboxConn, gameName); err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("FTP upload: %v", err))
		} else {
			os.RemoveAll(gameDir)
			s.App.LogFTPComplete(gameName, titleID, xboxConn.IP)
		}
	} else {
		// ponytail: local-download install only carries a single raw file/path
		// per manifest (aurora-scripts' "type=raw" installer reads one filename+
		// path pair). Extra content files are dropped here; upgrade path is a
		// multi-entry raw manifest + matching Aurora loop if non-FTP
		// multi-package installs are ever requested.
		cf := primaryDigitalContentFile(contentFiles)
		status := "Ready to Install"
		if len(contentFiles) > 1 {
			s.App.Logf("Digital: %d content files found but not using FTP - only %s will be installed", len(contentFiles), filepath.Base(cf.path))
			status = fmt.Sprintf("Ready to Install (1 of %d packages - FTP transfers all of them)", len(contentFiles))
		}
		finalName := filepath.Base(cf.path)
		relPath := fmt.Sprintf("Content\\0000000000000000\\%s\\%s\\", cf.titleID, cf.typeDir)
		if err := helpers.CopyFileBuffered(cf.path, filepath.Join(gameDir, finalName)); err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("Copy: %v", err))
		} else {
			s.updateGameINI_Raw(gameDir, gameName, finalName, relPath, "")
			s.App.LogStatus(gameName, "Ready", status)
		}
	}
	s.App.Logf("=== Complete (Digital): %s ===", gameName)
}
