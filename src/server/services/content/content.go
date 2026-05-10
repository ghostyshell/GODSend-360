// content.go — DLC / Title Update discovery and management service.
package content

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

	sort.Slice(tus, func(i, j int) bool {
		return tus[i].Version > tus[j].Version
	})

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

// DiscoverContent fetches available DLC/TU for a TitleID from multiple sources.
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

	cdnItems, _ := s.discoverFromXboxCDN(titleID)
	for _, it := range cdnItems {
		if it.ContentType == "00005000" || it.ContentType == "000B0000" {
			if !s.containsTU(manifest.TitleUpdates, it.FileName) {
				manifest.TitleUpdates = append(manifest.TitleUpdates, it)
			}
		} else if it.ContentType == "00000002" || it.ContentType == "00000001" {
			if !s.containsDLC(manifest.DLCs, it.FileName) {
				manifest.DLCs = append(manifest.DLCs, it)
			}
		}
	}

	iaItems := s.discoverFromIA(titleID, gameName)
	for _, it := range iaItems {
		if it.ContentType == "00005000" || it.ContentType == "000B0000" {
			if !s.containsTU(manifest.TitleUpdates, it.FileName) {
				manifest.TitleUpdates = append(manifest.TitleUpdates, it)
			}
		} else if it.ContentType == "00000002" || it.ContentType == "00000001" {
			if !s.containsDLC(manifest.DLCs, it.FileName) {
				manifest.DLCs = append(manifest.DLCs, it)
			}
		}
	}

	return manifest, nil
}

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

func (s *Service) discoverFromXboxCDN(titleID string) ([]models.ContentItem, error) {
	url := fmt.Sprintf("http://xboxunity.net/Resources/Lib/TitleList.php?search=%s", titleID)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Items []struct {
			TitleID   string `json:"TitleID"`
			Name      string `json:"Name"`
			TitleType string `json:"TitleType"`
		} `json:"Items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var items []models.ContentItem
	for _, it := range result.Items {
		if !strings.EqualFold(it.TitleID, titleID) {
			continue
		}
		nameLower := strings.ToLower(it.Name)
		if strings.Contains(nameLower, "title update") || strings.Contains(nameLower, "tu") {
			items = append(items, models.ContentItem{
				TitleID:     titleID,
				ContentType: "00005000",
				DisplayName: it.Name,
				Source:      "xbox_cdn",
			})
		} else {
			items = append(items, models.ContentItem{
				TitleID:     titleID,
				ContentType: "00000002",
				DisplayName: it.Name,
				Source:      "xbox_cdn",
			})
		}
	}
	return items, nil
}

func (s *Service) discoverFromIA(titleID, gameName string) []models.ContentItem {
	var items []models.ContentItem
	_ = titleID
	_ = gameName
	return items
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
	_, err = io.Copy(f, resp.Body)
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
