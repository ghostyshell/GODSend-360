// handlers_content.go — DLC / Title Update HTTP handlers.
package http

import (
	"encoding/json"
	stdhttp "net/http"
	"strings"
	"time"

	"godsend/app"
	"godsend/models"
	"godsend/services/content"
)

// handleContentDiscover returns all known DLC for a given TitleID,
// merging installed items from the Xbox with available items from Minerva/IA.
// Title Updates are loaded separately via /content/tu to avoid blocking DLC.
func (d *Deps) handleContentDiscover(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	titleID := strings.TrimSpace(r.URL.Query().Get("title_id"))
	gameName := strings.TrimSpace(r.URL.Query().Get("game_name"))
	xboxIP := strings.TrimSpace(r.URL.Query().Get("xbox_ip"))
	drive := strings.TrimSpace(r.URL.Query().Get("drive"))

	if titleID == "" {
		jsonError(w, 400, "Missing title_id parameter")
		return
	}
	if drive == "" {
		drive = d.App.DefaultXboxDrive
	}
	if drive == "" {
		drive = "Hdd1:"
	}

	contentSvc := &content.Service{App: d.App, FTP: d.FTP, Torrent: d.Pipeline.Torrent}
	manifest, err := contentSvc.DiscoverDLC(titleID, gameName, xboxIP, drive)
	if err != nil {
		d.App.Logf("CONTENT DISCOVER error for %s: %v", titleID, err)
		jsonError(w, 500, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(manifest)
}

// handleContentTitleUpdates returns all known Title Updates for a given TitleID
// from XboxUnity (and Minerva/IA if they have TU entries).
func (d *Deps) handleContentTitleUpdates(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	titleID := strings.TrimSpace(r.URL.Query().Get("title_id"))

	if titleID == "" {
		jsonError(w, 400, "Missing title_id parameter")
		return
	}

	contentSvc := &content.Service{App: d.App}
	tus := contentSvc.DiscoverTitleUpdates(titleID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"title_id":      titleID,
		"title_updates": tus,
	})
}

// handleContentInstalled scans the Xbox Content directory and returns
// only what is already installed for a TitleID.
func (d *Deps) handleContentInstalled(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	titleID := strings.TrimSpace(r.URL.Query().Get("title_id"))
	xboxIP := strings.TrimSpace(r.URL.Query().Get("xbox_ip"))
	drive := strings.TrimSpace(r.URL.Query().Get("drive"))

	if titleID == "" {
		jsonError(w, 400, "Missing title_id parameter")
		return
	}
	if xboxIP == "" {
		jsonError(w, 400, "Missing xbox_ip parameter")
		return
	}
	if drive == "" {
		drive = d.App.DefaultXboxDrive
	}
	if drive == "" {
		drive = "Hdd1:"
	}

	contentSvc := &content.Service{App: d.App, FTP: d.FTP, Torrent: d.Pipeline.Torrent}
	report, err := contentSvc.ScanInstalledContent(xboxIP, drive, titleID)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}

// handleContentQueue accepts a POST with a ContentQueueRequest and starts
// the download + FTP transfer for that specific DLC/TU file.
func (d *Deps) handleContentQueue(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		jsonError(w, 405, "Use POST")
		return
	}

	var req models.ContentQueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "Invalid JSON body")
		return
	}

	if req.TitleID == "" || req.DisplayName == "" {
		jsonError(w, 400, "Missing title_id or display_name")
		return
	}

	// Resolve Xbox connection: prefer a prior registration, otherwise derive
	// one from request fields so library-page DLC/TU queues work without first
	// going through the Browse Store flow.
	var xboxConn *models.XboxConnection
	if c, ok := d.App.XboxConnections.Load(req.GameName); ok {
		cc := c.(models.XboxConnection)
		xboxConn = &cc
	}
	if xboxConn == nil {
		ip := strings.TrimSpace(req.XboxIP)
		drive := strings.TrimSpace(req.Drive)
		if ip == "" {
			jsonError(w, 400, "Missing xbox_ip and no Xbox registered for this game.")
			return
		}
		if drive == "" {
			drive = "Hdd1:"
		}
		derived := models.XboxConnection{
			IP: ip, Drive: drive, GameName: req.GameName,
			Platform: "xbox360", Mode: "ftp", Timestamp: time.Now(),
		}
		d.App.XboxConnections.Store(req.GameName, derived)
		xboxConn = &derived
	}

	contentSvc := &content.Service{App: d.App, FTP: d.FTP, Torrent: d.Pipeline.Torrent}
	go func() {
		if err := contentSvc.QueueContentDownload(req, xboxConn); err != nil {
			d.App.Logf("CONTENT QUEUE error for %s: %v", req.DisplayName, err)
		}
	}()

	jsonSuccess(w, map[string]string{"status": "queued", "item": req.DisplayName})
}

// handleContentSetActive activates or deactivates an installed Title Update by
// renaming its file on the Xbox FTP server. Activating one TU automatically
// deactivates other TUs in the same content-type folder (single-active model).
func (d *Deps) handleContentSetActive(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		jsonError(w, 405, "Use POST")
		return
	}
	var req struct {
		TitleID     string `json:"title_id"`
		Drive       string `json:"drive"`
		ContentType string `json:"content_type"`
		FileName    string `json:"file_name"`
		XboxIP      string `json:"xbox_ip"`
		SetActive   bool   `json:"set_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "Invalid JSON body")
		return
	}
	if req.TitleID == "" || req.FileName == "" || req.ContentType == "" || req.XboxIP == "" {
		jsonError(w, 400, "Missing title_id, content_type, file_name, or xbox_ip")
		return
	}
	if req.Drive == "" {
		req.Drive = d.App.DefaultXboxDrive
	}
	if req.Drive == "" {
		req.Drive = "Hdd1:"
	}

	contentSvc := &content.Service{App: d.App, FTP: d.FTP}
	if err := contentSvc.SetTUActive(req.XboxIP, req.Drive, req.TitleID, req.ContentType, req.FileName, req.SetActive); err != nil {
		d.App.Logf("CONTENT SET-ACTIVE error for %s: %v", req.FileName, err)
		jsonError(w, 500, err.Error())
		return
	}
	jsonSuccess(w, map[string]string{"status": "ok"})
}

// handleContentSources returns available download sources for a content item.
func (d *Deps) handleContentSources(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	titleID := strings.TrimSpace(r.URL.Query().Get("title_id"))
	gameName := strings.TrimSpace(r.URL.Query().Get("game_name"))
	if titleID == "" {
		jsonError(w, 400, "Missing title_id")
		return
	}

	var sources []map[string]string

	// Try IA: look for the game in DLC / XBLA DLC caches by name
	d.App.IAGameCacheMu.RLock()
	dlcGames := d.App.IAGameCache["dlc"]
	xblaDlcGames := d.App.IAGameCache["xbla"]
	d.App.IAGameCacheMu.RUnlock()

	searchLower := strings.ToLower(gameName)
	for _, g := range dlcGames {
		if strings.Contains(strings.ToLower(g), searchLower) || searchLower == "" {
			if entry, err := d.IA.FindEntry(g, "dlc"); err == nil {
				sources = append(sources, map[string]string{
					"source":   "ia",
					"name":     g,
					"url":      app.IADownloadBase + entry.CollectionID + "/" + entry.FileName,
					"platform": "dlc",
				})
			}
		}
	}
	for _, g := range xblaDlcGames {
		if strings.Contains(strings.ToLower(g), searchLower) || searchLower == "" {
			if entry, err := d.IA.FindEntry(g, "xbla"); err == nil {
				sources = append(sources, map[string]string{
					"source":   "ia",
					"name":     g,
					"url":      app.IADownloadBase + entry.CollectionID + "/" + entry.FileName,
					"platform": "xbla",
				})
			}
		}
	}

	// Try Minerva
	d.App.MinervaEntryMapMu.RLock()
	defer d.App.MinervaEntryMapMu.RUnlock()
	for _, g := range d.App.MinervaGameCache["dlc"] {
		if strings.Contains(strings.ToLower(g), searchLower) || searchLower == "" {
			if entry, ok := d.App.MinervaEntryMap[strings.ToLower(g)]; ok {
				sources = append(sources, map[string]string{
					"source":   "minerva",
					"name":     g,
					"url":      entry.PathParam,
					"platform": "dlc",
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"title_id": titleID,
		"sources":  sources,
	})
}
