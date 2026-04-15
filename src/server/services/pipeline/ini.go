// ini.go — game manifest (INI) writing and GOD folder naming helpers.
package pipeline

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"godsend/infrastructure/helpers"
	"godsend/services"
)

func (s *Service) updateGameINI_Parts(gameDir, gameName, titleID, mediaID, resolvedName string, dlcList []string) {
	f, err := os.Create(filepath.Join(gameDir, "godsend.ini"))
	if err != nil {
		s.App.Logf("INI ERROR: %v", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := func(s string) string {
		s = strings.ReplaceAll(s, " ", "%20")
		s = strings.ReplaceAll(s, "(", "%28")
		s = strings.ReplaceAll(s, ")", "%29")
		return s
	}
	raw, ok := s.App.GamePartsMap.Load(gameName)
	if !ok {
		s.App.Logf("INI ERROR: no parts for %s", gameName)
		return
	}
	parts := raw.([]string)
	fmt.Fprintf(w, "[%s]\ntype=god\ntitleid=%s\nmediaid=%s\n", gameName, titleID, mediaID)
	if resolvedName != "" {
		fmt.Fprintf(w, "titlename=%s\n", resolvedName)
	}
	if len(parts) > 0 {
		fmt.Fprintf(w, "dataurl=%s\n", enc(parts[0]))
	}
	for i := 1; i < len(parts); i++ {
		fmt.Fprintf(w, "dataurlpart%d=%s\n", i+1, enc(parts[i]))
	}
	for i, d := range dlcList {
		fmt.Fprintf(w, "dlc_%d=%s\n", i+1, enc(d))
	}
	w.Flush()
}

// updateGameINI_Raw writes a manifest for digital/XBLA content.
func (s *Service) updateGameINI_Raw(gameDir, gameName, fileName, relPath, forcedDrive string) {
	f, err := os.Create(filepath.Join(gameDir, "godsend.ini"))
	if err != nil {
		s.App.Logf("INI ERROR: %v", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "[%s]\ntype=raw\nfilename=%s\npath=%s\n", gameName, fileName, relPath)
	if forcedDrive != "" {
		fmt.Fprintf(w, "drive=%s:\n", forcedDrive)
	}
	w.Flush()
}

// updateGameINI_XEX writes a manifest for XEX folder games.
func (s *Service) updateGameINI_XEX(gameDir, gameName, folderName, partFile string) {
	f, err := os.Create(filepath.Join(gameDir, "godsend.ini"))
	if err != nil {
		s.App.Logf("INI ERROR: %v", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, " ", "%20"), "(", "%28"), ")", "%29")
	}
	fmt.Fprintf(w, "[%s]\ntype=xex\nfoldername=%s\ndataurl=%s\n",
		gameName, folderName, enc(partFile))
	w.Flush()
}

// updateGameINI_Content writes a manifest for secondary-disc content installs.
func (s *Service) updateGameINI_Content(gameDir, gameName, titleID, partFile, relPath string) {
	f, err := os.Create(filepath.Join(gameDir, "godsend.ini"))
	if err != nil {
		s.App.Logf("INI ERROR: %v", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := func(s string) string {
		s = strings.ReplaceAll(s, " ", "%20")
		s = strings.ReplaceAll(s, "(", "%28")
		s = strings.ReplaceAll(s, ")", "%29")
		return s
	}
	fmt.Fprintf(w, "[%s]\ntype=content\ntitleid=%s\npath=%s\ndataurl=%s\n",
		gameName, titleID, relPath, enc(partFile))
	w.Flush()
}

// updateGameINI_ROM writes a godsend.ini manifest for a ROM install.
func (s *Service) updateGameINI_ROM(gameDir, gameName, archiveName, romPath string) {
	f, err := os.Create(filepath.Join(gameDir, "godsend.ini"))
	if err != nil {
		s.App.Logf("INI ERROR: %v", err)
		return
	}
	defer f.Close()
	enc := func(s string) string {
		s = strings.ReplaceAll(s, " ", "%20")
		s = strings.ReplaceAll(s, "(", "%28")
		s = strings.ReplaceAll(s, ")", "%29")
		return s
	}
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "[%s]\ntype=rom\ndataurl=%s\nrompath=%s\n", gameName, enc(archiveName), romPath)
	w.Flush()
}

// Iso2GodResolveDisplayTitle maps Title ID → display string for the LIVE CON header.
func Iso2GodResolveDisplayTitle(titleID uint32) string {
	return services.LookupTitleName(fmt.Sprintf("%08X", titleID))
}

// GodFolderName returns the directory name to use inside the GOD folder.
func GodFolderName(titleID string) string {
	if name := services.LookupTitleName(titleID); name != "" {
		return helpers.SanitizeFilename(name) + " - " + titleID
	}
	return "Title - " + titleID
}
