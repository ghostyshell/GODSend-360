// pipeline.go — ISO processing pipelines (local and online Redump).
package pipeline

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"godsend/app"
	"godsend/infrastructure/download"
	"godsend/infrastructure/ftp"
	"godsend/infrastructure/helpers"
	"godsend/infrastructure/torrent"
	"godsend/models"
	"godsend/services"
	"godsend/services/cache"
	"godsend/utils"
)

// Service orchestrates all game processing pipelines.
type Service struct {
	App      *app.App
	IA       *cache.IAService
	Minerva  *cache.MinervaService
	ROM      *cache.ROMService
	Download *download.Service
	FTP      *ftp.Service
	Torrent  *torrent.Service
}

// ==========================================
// LOCAL ISO PROCESSING
// ==========================================

func (s *Service) ProcessLocalISO(gameName, isoPath string) {
	s.App.Logf("=== Local ISO: %s ===", gameName)
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

	installType := s.App.LookupInstallType(gameName)
	if installType == "xex" {
		xexDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_xex")
		os.RemoveAll(xexDir)
		s.App.LogStatus(gameName, "Processing", "Extracting XEX layout from ISO...")
		if err := utils.ExtractXEXFolderFromISO(isoPath, xexDir); err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("XEX from ISO: %v", err))
			return
		}
		defer os.RemoveAll(xexDir)

		gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
		os.MkdirAll(gameDir, 0755)
		folderName := safeName
		if xboxConn != nil && xboxConn.Mode == "ftp" {
			if err := s.FTP.TransferXEX(xexDir, folderName, xboxConn, gameName); err != nil {
				s.App.Logf("FTP: initial XEX transfer failed for %s: %v — scheduling for retry", gameName, err)
				job := ftp.PendingFTPJob{
					ID:         helpers.SanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
					GameName:   gameName,
					Type:       "xex",
					SourceDir:  xexDir,
					GameDir:    gameDir,
					XboxIP:     xboxConn.IP,
					Drive:      xboxConn.Drive,
					FolderName: folderName,
					CreatedAt:  time.Now(),
				}
				s.FTP.SchedulePendingFTP(job)
				return
			}
			os.RemoveAll(gameDir)
			s.App.LogFTPComplete(gameName, "", xboxConn.IP)
		} else {
			partName := fmt.Sprintf("%s_Part1.7z", safeName)
			if err := utils.CreateZipFromDir(xexDir, filepath.Join(gameDir, partName)); err != nil {
				s.App.LogStatus(gameName, "Error", fmt.Sprintf("Archive XEX: %v", err))
				return
			}
			s.App.GamePartsMap.Store(gameName, []string{partName})
			s.updateGameINI_XEX(gameDir, gameName, folderName, partName)
			s.App.LogStatus(gameName, "Ready", "Ready to Install")
		}
		if gs, ok := s.App.JobQueue.Load(gameName); ok && gs.(models.GameStatus).State == "Ready" {
			if err := os.Remove(isoPath); err == nil {
				s.App.Logf("Cleanup: deleted source ISO: %s", filepath.Base(isoPath))
			}
		}
		s.App.Logf("=== Complete (local XEX from ISO): %s ===", gameName)
		return
	}
	if installType == "content" {
		s.processContentInstallFromISO(gameName, safeName, isoPath, xboxConn)
		if gs, ok := s.App.JobQueue.Load(gameName); ok && gs.(models.GameStatus).State == "Ready" {
			if err := os.Remove(isoPath); err == nil {
				s.App.Logf("Cleanup: deleted source ISO: %s", filepath.Base(isoPath))
			}
		}
		return
	}

	gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	s.App.LogStatus(gameName, "Processing", "Converting ISO to GOD...")
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
	s.App.Logf("Local ISO: TitleID=%s MediaID=%s", titleID, mediaID)
	s.finalizeGOD(gameName, safeName, gameDir, godDir, titleID, mediaID, xboxConn)

	if gs, ok := s.App.JobQueue.Load(gameName); ok && gs.(models.GameStatus).State == "Ready" {
		if err := os.Remove(isoPath); err == nil {
			s.App.Logf("Cleanup: deleted source ISO: %s", filepath.Base(isoPath))
		} else {
			s.App.Logf("Cleanup WARN: could not delete source ISO %s: %v", filepath.Base(isoPath), err)
		}
	}
}

// ==========================================
// ONLINE ISO PROCESSING (Redump)
// ==========================================

func (s *Service) ProcessGame(gameName, platform string) {
	s.App.Logf("=== Online ISO: %s (%s) ===", gameName, platform)
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
		s.App.Logf("ERROR [%s]: IA search failed: %v", gameName, err)
		s.App.LogStatus(gameName, "Error", err.Error())
		return
	}
	downloadURL := app.IADownloadBase + entry.CollectionID + "/" + url.PathEscape(entry.FileName)
	s.App.Logf("IA Download: %s → %s", gameName, entry.FileName)

	archivePath := filepath.Join(s.App.ToolsDir, "Temp", safeName+filepath.Ext(entry.FileName))
	s.App.LogStatus(gameName, "Processing", "Downloading from Internet Archive...")
	if err := s.Download.DownloadWithProgress(downloadURL, archivePath, gameName, app.IADownloadBase); err != nil {
		s.App.Logf("ERROR [%s]: IA download failed: %v", gameName, err)
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Download: %v", err))
		return
	}

	installType := s.App.LookupInstallType(gameName)

	if installType == "xex" {
		extDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_ext")
		os.RemoveAll(extDir)
		s.App.LogStatus(gameName, "Processing", "Extracting archive for XEX...")
		if err := utils.ExtractArchive(archivePath, extDir); err != nil {
			os.Remove(archivePath)
			s.App.Logf("ERROR [%s]: XEX extract failed: %v", gameName, err)
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
			return
		}
		os.Remove(archivePath)
		defer os.RemoveAll(extDir)

		xexFolder := helpers.FindXEXFolder(extDir)
		if xexFolder == "" {
			s.App.LogStatus(gameName, "Error", "No default.xex in archive — XEX needs a loose folder rip. Use GOD or DLC for ISO-only Redump releases.")
			return
		}
		folderName := filepath.Base(xexFolder)
		s.App.LogStatus(gameName, "Processing", fmt.Sprintf("XEX folder: %s", folderName))
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
		s.App.Logf("=== Complete (Redump XEX): %s ===", gameName)
		return
	}

	s.App.LogStatus(gameName, "Processing", "Extracting ISO...")
	isoPath, err := utils.ExtractISO(archivePath, safeName, filepath.Join(s.App.ToolsDir, "Temp"))
	os.Remove(archivePath)
	if err != nil {
		s.App.Logf("ERROR [%s]: Extract failed: %v", gameName, err)
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	if installType == "content" {
		s.processContentInstallFromISO(gameName, safeName, isoPath, xboxConn)
		os.Remove(isoPath)
		return
	}

	s.App.LogStatus(gameName, "Processing", "Converting to GOD...")
	godDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_GOD")
	os.MkdirAll(godDir, 0755)
	if err := utils.RunIso2GodNative(isoPath, godDir, Iso2GodResolveDisplayTitle); err != nil {
		s.App.Logf("ERROR [%s]: iso2god failed: %v", gameName, err)
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
	s.App.Logf("Online ISO: TitleID=%s MediaID=%s", titleID, mediaID)
	s.finalizeGOD(gameName, safeName, gameDir, godDir, titleID, mediaID, xboxConn)
}

// finalizeGOD handles the FTP vs HTTP packaging step shared by local and online ISO flows.
func (s *Service) finalizeGOD(gameName, safeName, gameDir, godDir, titleID, mediaID string, xboxConn *models.XboxConnection) {
	s.App.LogStatus(gameName, "Processing", "Looking up title name...")
	resolvedName := services.LookupTitleName(titleID)

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		s.App.LogStatus(gameName, "Processing", "FTP Transfer starting...")
		if err := s.FTP.TransferGame(godDir, xboxConn, gameName, titleID, mediaID, resolvedName); err != nil {
			s.App.Logf("FTP: initial transfer failed for %s: %v — scheduling for retry", gameName, err)
			job := ftp.PendingFTPJob{
				ID:           helpers.SanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
				GameName:     gameName,
				Type:         "god",
				SourceDir:    godDir,
				GameDir:      gameDir,
				XboxIP:       xboxConn.IP,
				Drive:        xboxConn.Drive,
				TitleID:      titleID,
				MediaID:      mediaID,
				ResolvedName: resolvedName,
				CreatedAt:    time.Now(),
			}
			s.FTP.SchedulePendingFTP(job)
			return
		}
		os.RemoveAll(godDir)
		os.RemoveAll(gameDir)
		s.App.LogFTPComplete(gameName, titleID, xboxConn.IP)
	} else {
		s.App.LogStatus(gameName, "Processing", "Archiving for HTTP transfer...")
		titleID, mediaID, err := helpers.BucketAndZip(s.App, godDir, gameDir, gameName, safeName)
		if err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("Archive: %v", err))
			os.RemoveAll(godDir)
			return
		}
		os.RemoveAll(godDir)
		s.updateGameINI_Parts(gameDir, gameName, titleID, mediaID, resolvedName, nil)
		s.App.LogStatus(gameName, "Ready", "Ready to Install")
	}
	s.App.Logf("=== Complete: %s ===", gameName)
}
