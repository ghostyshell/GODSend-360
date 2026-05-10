// handlers_content.go — DLC / Title Update HTTP handlers.
package http

import (
	"encoding/json"
	"net/url"
	stdhttp "net/http"
	"strings"

	"godsend/models"
	"godsend/services/content"
)

// handleContentDiscover returns all known DLC and TUs for a given TitleID,
// merging installed items from the Xbox with available items from CDN.
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

	contentSvc := &content.Service{App: d.App, FTP: d.FTP}
	manifest, err := contentSvc.DiscoverContent(titleID, gameName, xboxIP, drive)
	if err != nil {
		d.App.Logf("CONTENT DISCOVER error for %s: %v", titleID, err)
		jsonError(w, 500, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(manifest)
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

	contentSvc := &content.Service{App: d.App, FTP: d.FTP}
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

	// Resolve Xbox connection
	var xboxConn *models.XboxConnection
	if c, ok := d.App.XboxConnections.Load(req.GameName); ok {
		cc := c.(models.XboxConnection)
		xboxConn = &cc
	}
	if xboxConn == nil {
		jsonError(w, 400, "No Xbox registered for this game. Browse and queue the game first.")
		return
	}

	contentSvc := &content.Service{App: d.App, FTP: d.FTP}
	go func() {
		if err := contentSvc.QueueContentDownload(req, xboxConn); err != nil {
			d.App.Logf("CONTENT QUEUE error for %s: %v", req.DisplayName, err)
		}
	}()

	jsonSuccess(w, map[string]string{"status": "queued", "item": req.DisplayName})
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
					"url":      url.QueryEscape(entry.CollectionID + "/" + entry.FileName),
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
					"url":      url.QueryEscape(entry.CollectionID + "/" + entry.FileName),
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
