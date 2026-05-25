// rom.go — ROM download, extraction, and delivery pipeline.
package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"godsend/app"
	ftppkg "godsend/infrastructure/ftp"
	"godsend/infrastructure/helpers"
	"godsend/models"
	"godsend/utils"
)

// ProcessROM downloads a ROM from edgeemu.net using parallel range requests,
// extracts it, then delivers it via FTP or HTTP.
func (s *Service) ProcessROM(gameName, sysid string) {
	s.App.Logf("=== ROM: %s (%s) ===", gameName, sysid)
	sys, ok := app.ROMSystems[sysid]
	if !ok {
		s.App.LogStatus(gameName, "Error", "Unknown ROM system: "+sysid)
		return
	}
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

	// Resolve download URL from cache
	s.App.LogStatus(gameName, "Processing", "Looking up ROM on EdgeEmu...")
	downloadURL := s.ROM.FindDownloadURL(gameName, sysid)
	if downloadURL == "" {
		// Cache might be cold — try building it now and retry
		s.ROM.Build(sysid)
		downloadURL = s.ROM.FindDownloadURL(gameName, sysid)
	}
	if downloadURL == "" {
		s.App.LogStatus(gameName, "Error", "ROM not found: "+gameName)
		return
	}
	s.App.Logf("ROM Download: %s → %s", gameName, downloadURL)

	// Download the ZIP using parallel range requests
	zipPath := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_rom.zip")
	s.App.LogStatus(gameName, "Processing", "Downloading from EdgeEmu...")
	if err := s.Download.DownloadEdgeEmuWithProgress(downloadURL, zipPath, gameName); err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Download: %v", err))
		os.Remove(zipPath)
		return
	}
	defer os.Remove(zipPath)

	// Extract ZIP
	s.App.LogStatus(gameName, "Processing", "Extracting ROM...")
	extDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_rom_ext")
	os.RemoveAll(extDir)
	defer os.RemoveAll(extDir)
	if err := utils.ExtractArchive(zipPath, extDir); err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
		return
	}

	// Find the ROM file
	romFiles := findROMFiles(extDir)
	if len(romFiles) == 0 {
		s.App.LogStatus(gameName, "Error", "No ROM file found after extraction")
		return
	}
	romFile := romFiles[0]
	romFileName := filepath.Base(romFile)

	// Xbox install path: [Drive]\[romRootPath]\[SystemFolder]\
	xboxROMPath := s.App.ROMRootPath + "\\" + sys.Folder + "\\"

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		s.App.LogStatus(gameName, "Processing", "FTP transfer starting...")
		drive := strings.TrimSuffix(xboxConn.Drive, ":")
		remotePath := "/" + drive + "/" + strings.ReplaceAll(xboxROMPath, "\\", "/")

		fc, err := s.FTP.ConnectWithRetry(xboxConn.IP)
		if err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("FTP: %v", err))
			return
		}
		defer s.FTP.QuitConn(fc)
		ftppkg.MkdirAll(fc, strings.TrimSuffix(remotePath, "/"))

		info, _ := os.Stat(romFile)
		var xfer int64
		if err := s.FTP.UploadFile(fc, romFile, remotePath+romFileName, gameName,
			&xfer, info.Size(), 1, 1, time.Now(), new(float64)); err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("FTP upload: %v", err))
		} else {
			os.RemoveAll(gameDir)
			s.App.LogStatus(gameName, "Ready", "FTP Transfer Complete!")
		}
	} else {
		// HTTP mode: compress ROM to .7z and serve from Ready/
		s.App.LogStatus(gameName, "Processing", "Archiving for HTTP transfer...")
		archiveName := safeName + ".7z"
		archiveDest := filepath.Join(gameDir, archiveName)
		if err := utils.CompressROMFile(romFile, archiveDest); err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("Compress: %v", err))
			return
		}
		s.updateGameINI_ROM(gameDir, gameName, archiveName, xboxROMPath)
		s.App.LogStatus(gameName, "Ready", "Ready to Install")
	}
	s.App.Logf("=== Complete (ROM): %s ===", gameName)
}

// findROMFiles walks a directory and returns all non-metadata files (the actual ROMs).
func findROMFiles(dir string) []string {
	skipExts := map[string]bool{
		".txt": true, ".nfo": true, ".jpg": true, ".jpeg": true,
		".png": true, ".xml": true, ".dat": true, ".md": true,
	}
	var files []string
	filepath.Walk(dir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() || i.Size() == 0 {
			return nil
		}
		if !skipExts[strings.ToLower(filepath.Ext(p))] {
			files = append(files, p)
		}
		return nil
	})
	return files
}
