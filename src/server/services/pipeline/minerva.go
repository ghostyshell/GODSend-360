// minerva.go - Minerva Archive download and processing pipelines.
package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"godsend/infrastructure/ftp"
	"godsend/infrastructure/helpers"
	"godsend/models"
	"godsend/utils"
)

// ==========================================
// MINERVA PROCESSING FUNCTIONS
// ==========================================

// ProcessMinervaGame downloads and processes an Xbox 360 / Xbox disc ISO from Minerva.
func (s *Service) ProcessMinervaGame(gameName string, entry models.MinervaEntry, platform string) {
	s.App.Logf("=== Minerva ISO: %s (%s) ===", gameName, platform)
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

	torrentDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_torrent")
	os.MkdirAll(torrentDir, 0755)
	defer os.RemoveAll(torrentDir)
	s.App.Logf("Minerva Torrent: %s → %s", gameName, entry.FileName)
	s.App.LogStatus(gameName, "Processing", "Starting Minerva torrent download...")
	archivePath, err := s.Torrent.DownloadViaTorrent(platform, torrentDir, gameName, entry, s.debridTorrentDownloader(gameName))
	if err != nil {
		s.App.Logf("ERROR [%s]: Minerva torrent failed: %v", gameName, err)
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Minerva torrent: %v", err))
		return
	}

	installType := s.App.LookupInstallType(gameName)

	if installType == "xex" {
		extDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_mext")
		os.RemoveAll(extDir)
		s.App.LogStatus(gameName, "Processing", "Extracting archive for XEX...")
		if err := utils.ExtractArchive(archivePath, extDir); err != nil {
			os.Remove(archivePath)
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
			return
		}
		os.Remove(archivePath)
		defer os.RemoveAll(extDir)
		xexFolder := helpers.FindXEXFolder(extDir)
		if xexFolder == "" {
			s.App.LogStatus(gameName, "Error", "No default.xex found in Minerva archive")
			return
		}
		folderName := filepath.Base(xexFolder)
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
		s.App.Logf("=== Complete (Minerva XEX): %s ===", gameName)
		return
	}

	s.App.LogStatus(gameName, "Processing", "Extracting ISO...")
	isoPath, err := utils.ExtractISO(archivePath, safeName, filepath.Join(s.App.ToolsDir, "Temp"))
	os.Remove(archivePath)
	if err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	if installType == "content" {
		s.processContentInstallFromISO(gameName, safeName, isoPath, xboxConn)
		os.Remove(isoPath)
		return
	}

	s.App.LogStatus(gameName, "Processing", "Converting to GOD...")
	godDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_MGOD")
	os.MkdirAll(godDir, 0755)
	if err := utils.RunIso2GodNative(isoPath, godDir, Iso2GodResolveDisplayTitle); err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("GOD convert: %v", err))
		os.Remove(isoPath)
		os.RemoveAll(godDir)
		return
	}
	os.Remove(isoPath)

	titleID, mediaID, err := helpers.DetectGodStructure(godDir)
	if err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("GOD detect: %v", err))
		os.RemoveAll(godDir)
		return
	}
	s.App.Logf("Minerva ISO: TitleID=%s MediaID=%s", titleID, mediaID)
	s.finalizeGOD(gameName, safeName, gameDir, godDir, titleID, mediaID, xboxConn)
}

// ProcessMinervaGenericGame handles the "games" platform from Minerva (Non-Redump mixed archives).
func (s *Service) ProcessMinervaGenericGame(gameName string, entry models.MinervaEntry) {
	s.App.Logf("=== Minerva Generic: %s ===", gameName)
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

	torrentDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_torrent")
	os.MkdirAll(torrentDir, 0755)
	defer os.RemoveAll(torrentDir)
	s.App.LogStatus(gameName, "Processing", "Starting Minerva torrent download...")
	archivePath, err := s.Torrent.DownloadViaTorrent("games", torrentDir, gameName, entry, s.debridTorrentDownloader(gameName))
	if err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Minerva torrent: %v", err))
		return
	}

	s.App.LogStatus(gameName, "Processing", "Extracting archive...")
	extDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_mgext")
	os.RemoveAll(extDir)
	defer os.RemoveAll(extDir)
	if err := utils.ExtractArchive(archivePath, extDir); err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	// Try ISO pipeline first
	isoPath := helpers.FindFileByExt(extDir, ".iso")
	if isoPath != "" {
		s.App.LogStatus(gameName, "Processing", "Converting to GOD...")
		godDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_MGGOD")
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
		s.App.Logf("=== Complete (Minerva Generic/ISO): %s ===", gameName)
		return
	}

	// Fallback: look for a XEX folder
	xexFolder := helpers.FindXEXFolder(extDir)
	if xexFolder == "" {
		s.App.LogStatus(gameName, "Error", "No ISO or XEX found in Minerva archive")
		return
	}
	folderName := filepath.Base(xexFolder)
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
	s.App.Logf("=== Complete (Minerva Generic/XEX): %s ===", gameName)
}

// ProcessMinervaDigital handles XBLA / DLC / XBIG content from Minerva No-Intro Digital.
func (s *Service) ProcessMinervaDigital(gameName string, entry models.MinervaEntry, platform string) {
	s.App.Logf("=== Minerva Digital: %s (%s) ===", gameName, platform)
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

	torrentDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_torrent")
	os.MkdirAll(torrentDir, 0755)
	defer os.RemoveAll(torrentDir)
	s.App.LogStatus(gameName, "Processing", "Starting Minerva torrent download...")
	archivePath, err := s.Torrent.DownloadViaTorrent(platform, torrentDir, gameName, entry, s.debridTorrentDownloader(gameName))
	if err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Minerva torrent: %v", err))
		return
	}

	s.App.LogStatus(gameName, "Processing", "Extracting...")
	extDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_mdext")
	os.RemoveAll(extDir)
	defer os.RemoveAll(extDir)
	if err := utils.ExtractArchive(archivePath, extDir); err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	// See findDigitalContentFiles (digital.go): a Minerva digital release can
	// bundle more than one Xbox content package (base game + title update,
	// or several DLC packs) - collect every valid one instead of stopping at
	// the first match.
	contentFiles := findDigitalContentFiles(extDir)
	if len(contentFiles) == 0 {
		s.App.LogStatus(gameName, "Error", "No valid Xbox content found in Minerva archive")
		return
	}
	titleID := contentFiles[0].titleID
	s.App.Logf("Minerva Digital: TitleID=%s (%d content file(s))", titleID, len(contentFiles))
	if ids := distinctTitleIDs(contentFiles); len(ids) > 1 {
		s.App.Logf("Minerva Digital: WARNING - content files carry different TitleIDs: %v", ids)
	}

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		if err := s.transferDigitalContentFilesFTP(contentFiles, xboxConn, gameName); err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("FTP upload: %v", err))
		} else {
			os.RemoveAll(gameDir)
			s.App.LogFTPComplete(gameName, titleID, xboxConn.IP)
		}
	} else {
		cf := primaryDigitalContentFile(contentFiles)
		status := "Ready to Install"
		if len(contentFiles) > 1 {
			s.App.Logf("Minerva Digital: %d content files found but not using FTP - only %s will be installed", len(contentFiles), filepath.Base(cf.path))
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
	s.App.Logf("=== Complete (Minerva Digital): %s ===", gameName)
}
