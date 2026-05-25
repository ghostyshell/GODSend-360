// minerva.go — Minerva Archive download and processing pipelines.
package pipeline

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	archivePath, err := s.Torrent.DownloadViaTorrent(platform, torrentDir, gameName, entry)
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
				s.App.Logf("FTP: initial XEX transfer failed for %s: %v — scheduling for retry", gameName, err)
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
	archivePath, err := s.Torrent.DownloadViaTorrent("games", torrentDir, gameName, entry)
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
			s.App.Logf("FTP: initial XEX transfer failed for %s: %v — scheduling for retry", gameName, err)
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
	archivePath, err := s.Torrent.DownloadViaTorrent(platform, torrentDir, gameName, entry)
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
		s.App.LogStatus(gameName, "Error", "No valid Xbox content found in Minerva archive")
		return
	}
	s.App.Logf("Minerva Digital: TitleID=%s Type=%s", titleID, typeDir)
	finalName := filepath.Base(contentFile)

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		drive := strings.TrimSuffix(xboxConn.Drive, ":")
		base := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", drive, titleID, typeDir)
		fc, err := s.FTP.ConnectWithRetry(xboxConn.IP)
		if err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("FTP: %v", err))
			return
		}
		defer s.FTP.QuitConn(fc)
		ftp.MkdirAll(fc, base)
		info, _ := os.Stat(contentFile)
		var xfer int64
		if err := s.FTP.UploadFile(fc, contentFile, base+"/"+finalName, gameName, &xfer, info.Size(), 1, 1, time.Now(), new(float64)); err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("FTP upload: %v", err))
		} else {
			os.RemoveAll(gameDir)
			s.App.LogFTPComplete(gameName, titleID, xboxConn.IP)
		}
	} else {
		relPath := fmt.Sprintf("Content\\0000000000000000\\%s\\%s\\", titleID, typeDir)
		if err := helpers.CopyFileBuffered(contentFile, filepath.Join(gameDir, finalName)); err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("Copy: %v", err))
		} else {
			s.updateGameINI_Raw(gameDir, gameName, finalName, relPath, "")
			s.App.LogStatus(gameName, "Ready", "Ready to Install")
		}
	}
	s.App.Logf("=== Complete (Minerva Digital): %s ===", gameName)
}
