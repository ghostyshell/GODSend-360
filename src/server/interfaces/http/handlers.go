// handlers.go — HTTP request handlers for browse, cache, trigger, status, queue, debug, and file serving.
package http

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"godsend/app"
	"godsend/infrastructure/ftp"
	"godsend/infrastructure/helpers"
	"godsend/models"
	"godsend/services/local"
	"godsend/utils"
)

func (d *Deps) handleBrowse(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	platform := r.URL.Query().Get("platform")
	source := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source"))) // "minerva", "ia", or "" (merged)
	d.App.Logf("BROWSE: platform=%s source=%s", platform, source)

	// ROM platforms — served from edgeemu.net scrape cache
	if strings.HasPrefix(platform, "rom_") {
		sysid := strings.TrimPrefix(platform, "rom_")
		if _, ok := app.ROMSystems[sysid]; !ok {
			jsonError(w, 400, "Unknown ROM system: "+sysid)
			return
		}
		d.App.ROMGameCacheMu.RLock()
		cached := d.App.ROMGameCache[sysid]
		d.App.ROMGameCacheMu.RUnlock()
		if len(cached) > 0 {
			d.App.Logf("BROWSE: Serving %d cached ROMs for %s", len(cached), app.ROMSystems[sysid].Name)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(strings.Join(cached, "|")))
			return
		}
		go d.ROM.Build(sysid)
		s := d.IA.GetBuildState(platform)
		loaded := atomic.LoadInt32(&s.Loaded)
		total := atomic.LoadInt32(&s.Total)
		if total == 0 {
			total = 1
		}
		d.App.Logf("BROWSE: ROM cache building for %s", sysid)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "__IA_LOADING__:%d/%d", loaded, total)
		return
	}

	// Local — scan Transfer folder immediately, no IA needed
	if platform == "local" {
		games := d.Local.ScanTransferFolder()
		d.App.Logf("BROWSE: %d local ISOs found", len(games))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(strings.Join(games, "|")))
		return
	}

	// Source-specific browse — return only the requested source's list.
	d.App.MinervaGameCacheMu.RLock()
	minervaCached := d.App.MinervaGameCache[platform]
	d.App.MinervaGameCacheMu.RUnlock()

	d.App.IAGameCacheMu.RLock()
	iaCached := d.App.IAGameCache[platform]
	d.App.IAGameCacheMu.RUnlock()

	if source == "minerva" {
		if len(minervaCached) > 0 {
			decoded := make([]string, len(minervaCached))
			for i, g := range minervaCached {
				decoded[i] = helpers.DecodeMinervaName(g)
			}
			d.App.Logf("BROWSE: Serving %d Minerva games for %s", len(decoded), platform)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(strings.Join(decoded, "|")))
			return
		}
		go d.Minerva.Build(platform)
		d.App.Logf("BROWSE: Minerva cache building for %s", platform)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "__IA_LOADING__:0/1")
		return
	}

	if source == "ia" {
		if len(iaCached) > 0 {
			d.App.Logf("BROWSE: Serving %d IA games for %s", len(iaCached), platform)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(strings.Join(iaCached, "|")))
			return
		}
		go d.IA.Build(platform)
		s := d.IA.GetBuildState(platform)
		loaded := atomic.LoadInt32(&s.Loaded)
		total := atomic.LoadInt32(&s.Total)
		if total == 0 {
			total = int32(len(app.IACollections[platform]))
		}
		d.App.Logf("BROWSE: IA cache building for %s %d/%d", platform, loaded, total)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "__IA_LOADING__:%d/%d", loaded, total)
		return
	}

	// No source specified — merged fallback (backward compat).
	if len(minervaCached) > 0 || len(iaCached) > 0 {
		seen := make(map[string]bool, len(minervaCached)+len(iaCached))
		merged := make([]string, 0, len(minervaCached)+len(iaCached))
		for _, g := range minervaCached {
			key := strings.ToLower(helpers.DecodeMinervaName(g))
			if !seen[key] {
				seen[key] = true
				merged = append(merged, helpers.DecodeMinervaName(g))
			}
		}
		for _, g := range iaCached {
			key := strings.ToLower(g)
			if !seen[key] {
				seen[key] = true
				merged = append(merged, g)
			}
		}
		d.App.Logf("BROWSE: Serving %d merged games for %s (%d Minerva, %d IA)", len(merged), platform, len(minervaCached), len(iaCached))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(strings.Join(merged, "|")))
		return
	}

	// Nothing ready yet — trigger both builds and return a loading marker.
	go d.IA.Build(platform)
	go d.Minerva.Build(platform)

	s := d.IA.GetBuildState(platform)
	loaded := atomic.LoadInt32(&s.Loaded)
	total := atomic.LoadInt32(&s.Total)
	if total == 0 {
		total = int32(len(app.IACollections[platform]))
	}
	d.App.Logf("BROWSE: %s cache building %d/%d", platform, loaded, total)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "__IA_LOADING__:%d/%d", loaded, total)
}

func (d *Deps) handleCacheStatus(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	type platformStatus struct {
		State  string `json:"state"`
		Loaded int32  `json:"loaded"`
		Total  int32  `json:"total"`
		Games  int    `json:"games"`
	}
	result := map[string]platformStatus{}

	d.App.BuildStatesMu.Lock()
	for p, s := range d.App.BuildStates {
		d.App.IAGameCacheMu.RLock()
		count := len(d.App.IAGameCache[p])
		d.App.IAGameCacheMu.RUnlock()
		result[p] = platformStatus{
			State:  s.State,
			Loaded: atomic.LoadInt32(&s.Loaded),
			Total:  atomic.LoadInt32(&s.Total),
			Games:  count,
		}
	}
	d.App.BuildStatesMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleCacheRefresh triggers a fresh rebuild for one platform or all platforms.
func (d *Deps) handleCacheRefresh(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	platform := r.URL.Query().Get("platform")

	if platform == "" || platform == "all" {
		d.App.Logf("CACHE REFRESH: all IA + Minerva platforms requested")
		for p := range app.IACollections {
			go d.IA.Build(p)
		}
		for p := range app.MinervaPageURLs {
			go d.Minerva.Build(p)
		}
		// Also refresh any ROM system that already has a cache on disk
		var romRefreshed []string
		d.App.ROMGameCacheMu.RLock()
		for sysid := range app.ROMSystems {
			if len(d.App.ROMGameCache[sysid]) > 0 {
				romRefreshed = append(romRefreshed, sysid)
			}
		}
		d.App.ROMGameCacheMu.RUnlock()
		for _, sysid := range romRefreshed {
			go d.ROM.Build(sysid)
		}
		d.App.Logf("CACHE REFRESH: %d previously-used ROM systems queued", len(romRefreshed))
		jsonSuccess(w, map[string]string{"status": "refreshing", "platforms": "all"})
		return
	}

	if strings.HasPrefix(platform, "minerva_") {
		p := strings.TrimPrefix(platform, "minerva_")
		if _, ok := app.MinervaPageURLs[p]; !ok {
			jsonError(w, 400, "Unknown Minerva platform: "+p)
			return
		}
		d.App.Logf("CACHE REFRESH: Minerva %s", p)
		go d.Minerva.Build(p)
		jsonSuccess(w, map[string]string{"status": "refreshing", "platform": platform})
		return
	}

	if strings.HasPrefix(platform, "rom_") {
		sysid := strings.TrimPrefix(platform, "rom_")
		if _, ok := app.ROMSystems[sysid]; !ok {
			jsonError(w, 400, "Unknown ROM system: "+sysid)
			return
		}
		d.App.Logf("CACHE REFRESH: ROM system %s", sysid)
		go d.ROM.Build(sysid)
		jsonSuccess(w, map[string]string{"status": "refreshing", "platform": platform})
		return
	}

	if _, ok := app.IACollections[platform]; !ok {
		jsonError(w, 400, "Unknown platform: "+platform)
		return
	}
	d.App.Logf("CACHE REFRESH: %s (IA + Minerva)", platform)
	go d.IA.Build(platform)
	go d.Minerva.Build(platform)
	jsonSuccess(w, map[string]string{"status": "refreshing", "platform": platform})
}

func (d *Deps) handleRegister(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	gameName := local.NormalizeClientGameName(r.URL.Query().Get("game"))
	xboxIP := r.URL.Query().Get("ip")
	drive := r.URL.Query().Get("drive")
	platform := r.URL.Query().Get("platform")
	mode := r.URL.Query().Get("mode")
	if gameName == "" || xboxIP == "" {
		jsonError(w, 400, "Missing game or ip parameter")
		return
	}
	if net.ParseIP(xboxIP) == nil {
		jsonError(w, 400, "Invalid IP address format")
		return
	}
	if drive == "" {
		drive = "Hdd1:"
	}
	if mode == "" {
		mode = "http"
	}
	if platform == "" {
		platform = "xbox360"
	}
	installType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("install_type")))
	if installType == "" {
		installType = "god"
	}
	if installType != "god" && installType != "content" && installType != "xex" {
		installType = "god"
	}
	d.App.InstallTypeMap.Store(gameName, installType)
	d.App.XboxConnections.Store(gameName, models.XboxConnection{
		IP: xboxIP, Drive: drive, GameName: gameName,
		Platform: platform, Mode: mode, Timestamp: time.Now(),
	})
	d.App.Logf("REGISTER: Xbox %s for %s (mode=%s drive=%s install=%s)", xboxIP, gameName, mode, drive, installType)
	jsonSuccess(w, map[string]string{"status": "registered", "mode": mode, "ip": xboxIP, "drive": drive})
}

func (d *Deps) handleTrigger(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	gameName := local.NormalizeClientGameName(r.URL.Query().Get("game"))
	platform := r.URL.Query().Get("platform")
	source := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source"))) // "minerva", "ia", or ""
	if gameName == "" {
		jsonError(w, 400, "Missing game parameter")
		return
	}
	if platform == "" {
		platform = "xbox360"
	}
	installType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("install_type")))
	if installType == "" {
		installType = "god"
	}
	if installType != "god" && installType != "content" && installType != "xex" {
		installType = "god"
	}
	d.App.InstallTypeMap.Store(gameName, installType)
	d.App.SuppressedJobs.Delete(gameName)

	if status, exists := d.App.JobQueue.Load(gameName); exists {
		gs := status.(models.GameStatus)
		if gs.State == "Ready" {
			jsonSuccess(w, map[string]string{"status": "already_ready"})
			return
		}
		if gs.State == "Processing" {
			jsonSuccess(w, map[string]string{"status": "already_processing"})
			return
		}
	}

	launcher := func(fn func()) {
		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					d.App.Logf("PANIC processing %s: %v", gameName, rec)
					buf := make([]byte, 4096)
					n := runtime.Stack(buf, false)
					d.App.Logf("STACK: %s", string(buf[:n]))
					d.App.LogStatus(gameName, "Error", "Server crashed during processing")
				}
			}()
			fn()
		}()
	}

	// Local ISO in Transfer folder takes priority for disc-based platforms
	if platform == "xbox360" || platform == "xbox" || platform == "local" {
		if iso := d.Local.FindLocalISO(gameName); iso != "" {
			d.App.Logf("TRIGGER: Local ISO found for '%s'", gameName)
			launcher(func() { d.Pipeline.ProcessLocalISO(gameName, iso) })
			jsonSuccess(w, map[string]string{"status": "triggered", "source": "local"})
			return
		}
		if d.Local.IsGameReadyLocally(gameName) {
			d.App.LogStatus(gameName, "Ready", "Ready to Install")
			jsonSuccess(w, map[string]string{"status": "already_ready"})
			return
		}
		// Local Transfer list (platform=local): never use Internet Archive
		if platform == "local" {
			d.App.Logf("LOCAL UNAVAILABLE: no .iso match for %q in %s (check URL encoding for & + # in filenames)", gameName, d.App.TransferDir)
			d.App.LogStatus(gameName, "Error", "No ISO in Transfer folder for \""+gameName+"\"")
			jsonSuccess(w, map[string]string{
				"status":  "local_unavailable",
				"message": "Add the game ISO to your Transfer folder, then queue again.",
			})
			return
		}
	}

	// ROM platforms (edgeemu.net)
	if strings.HasPrefix(platform, "rom_") {
		sysid := strings.TrimPrefix(platform, "rom_")
		if _, ok := app.ROMSystems[sysid]; !ok {
			jsonError(w, 400, "Unknown ROM system: "+sysid)
			return
		}
		if d.Local.IsGameReadyLocally(gameName) {
			d.App.LogStatus(gameName, "Ready", "Ready to Install")
			jsonSuccess(w, map[string]string{"status": "already_ready"})
			return
		}
		launcher(func() { d.Pipeline.ProcessROM(gameName, sysid) })
		jsonSuccess(w, map[string]string{"status": "triggered", "source": "edgeemu"})
		return
	}

	// Minerva — check before IA (source priority: local → Minerva → Internet Archive)
	// Skipped when source=="ia" (user explicitly chose Internet Archive).
	if source != "ia" {
		if _, hasMinervaPage := app.MinervaPageURLs[platform]; hasMinervaPage {
			if mEntry, ok := d.Minerva.FindEntry(gameName, platform); ok {
				d.App.Logf("TRIGGER: Minerva source for '%s' (%s)", gameName, platform)
				switch platform {
				case "digital", "xbla", "dlc", "xblig":
					launcher(func() { d.Pipeline.ProcessMinervaDigital(gameName, mEntry, platform) })
				case "games":
					launcher(func() { d.Pipeline.ProcessMinervaGenericGame(gameName, mEntry) })
				default: // xbox360, xbox
					launcher(func() { d.Pipeline.ProcessMinervaGame(gameName, mEntry, platform) })
				}
				jsonSuccess(w, map[string]string{"status": "triggered", "source": "minerva"})
				return
			}
			if source == "minerva" {
				d.App.LogStatus(gameName, "Error", "Not found in Minerva Archive")
				jsonSuccess(w, map[string]string{"status": "minerva_unavailable", "message": "Game not found in Minerva Archive."})
				return
			}
		}
	}

	// source=="minerva" but platform has no Minerva page — treat as not found
	if source == "minerva" {
		d.App.LogStatus(gameName, "Error", "Not found in Minerva Archive")
		jsonSuccess(w, map[string]string{"status": "minerva_unavailable", "message": "Game not found in Minerva Archive."})
		return
	}

	// Internet Archive — fallback when Minerva has no match, or source=="ia"
	switch platform {
	case "digital", "xbla", "dlc", "xblig":
		launcher(func() { d.Pipeline.ProcessDigital(gameName, platform) })
	case "games":
		launcher(func() { d.Pipeline.ProcessGenericGame(gameName) })
	default: // xbox360, xbox
		launcher(func() { d.Pipeline.ProcessGame(gameName, platform) })
	}
	jsonSuccess(w, map[string]string{"status": "triggered", "source": "internet_archive"})
}

func (d *Deps) handleStatus(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	gameName := local.NormalizeClientGameName(r.URL.Query().Get("game"))
	if gameName == "" {
		jsonError(w, 400, "Missing game parameter")
		return
	}
	status := models.GameStatus{State: "Missing", Message: "Not Found"}
	if s, exists := d.App.JobQueue.Load(gameName); exists {
		status = s.(models.GameStatus)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleDiscInfo probes a local ISO in the Transfer folder and returns disc
// metadata along with a compat-table install recommendation.
func (d *Deps) handleDiscInfo(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	gameName := local.NormalizeClientGameName(r.URL.Query().Get("game"))
	if gameName == "" {
		jsonError(w, 400, "Missing game parameter")
		return
	}
	iso := d.Local.FindLocalISO(gameName)
	if iso != "" {
		info, err := utils.ProbeISODiscInfo(iso)
		if err != nil {
			jsonError(w, 500, fmt.Sprintf("Disc probe failed: %v", err))
			return
		}
		rec := models.DiscCompat(info.TitleID, info.DiscNumber)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"disc_number":    info.DiscNumber,
			"disc_count":     info.DiscCount,
			"title_id":       fmt.Sprintf("%08X", info.TitleID),
			"recommendation": rec.InstallType,
			"notes":          rec.Notes,
			"probed":         true,
		})
		return
	}
	// No Transfer-folder ISO yet (typical for IA-only installs) — filename-based hint for Disc 2+.
	if !models.IsMultiDiscGameName(gameName) {
		jsonError(w, 404, "No local ISO found for this game")
		return
	}
	tid := models.GuessTitleIDFromMultiDiscName(gameName)
	rec := models.DiscCompat(tid, 2)
	note := rec.Notes
	if tid == 0 {
		note = note + " (Title ID unknown from name — optional: copy ISO to PC Transfer for an exact probe)"
	} else {
		note = note + " (Title ID guessed from game name)"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"disc_number":    2,
		"disc_count":     0,
		"title_id":       fmt.Sprintf("%08X", tid),
		"recommendation": rec.InstallType,
		"notes":          note,
		"probed":         false,
	})
}

func (d *Deps) handleQueue(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	type JobEntry struct {
		Game    string `json:"game"`
		State   string `json:"state"`
		Message string `json:"message"`
	}
	var jobs []JobEntry
	d.App.JobQueue.Range(func(k, v interface{}) bool {
		gs := v.(models.GameStatus)
		jobs = append(jobs, JobEntry{
			Game:    k.(string),
			State:   gs.State,
			Message: gs.Message,
		})
		return true
	})
	// Sort: Processing first, then Ready, then Error, then others
	sort.Slice(jobs, func(i, j int) bool {
		order := map[string]int{"Processing": 0, "Ready": 1, "Error": 2}
		oi, iok := order[jobs[i].State]
		oj, jok := order[jobs[j].State]
		if !iok {
			oi = 3
		}
		if !jok {
			oj = 3
		}
		if oi != oj {
			return oi < oj
		}
		return jobs[i].Game < jobs[j].Game
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

// handleQueueRemove clears one job or the whole queue (POST /queue/remove?game=name or no game = all).
func (d *Deps) handleQueueRemove(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost && r.Method != stdhttp.MethodGet {
		jsonError(w, 405, "Use GET or POST /queue/remove?game=GameName (omit game to clear all)")
		return
	}
	game := local.NormalizeClientGameName(r.URL.Query().Get("game"))
	if game == "" {
		var keys []string
		d.App.JobQueue.Range(func(k, _ interface{}) bool {
			keys = append(keys, k.(string))
			return true
		})
		for _, k := range keys {
			d.App.JobQueue.Delete(k)
			d.App.SuppressedJobs.Store(k, struct{}{})
		}
		d.App.Logf("QUEUE: cleared %d job(s)", len(keys))
		jsonSuccess(w, map[string]string{"status": "cleared", "count": fmt.Sprintf("%d", len(keys))})
		return
	}
	d.App.JobQueue.Delete(game)
	d.App.SuppressedJobs.Store(game, struct{}{})
	// Also cancel any pending FTP job for this game
	for _, job := range d.FTP.LoadAllPendingFTPJobs() {
		if job.GameName == game {
			d.FTP.DeletePendingFTPJob(job.ID)
			go func(j ftp.PendingFTPJob) {
				time.Sleep(3 * time.Second)
				os.RemoveAll(j.SourceDir)
				if j.GameDir != "" {
					os.RemoveAll(j.GameDir)
				}
			}(job)
		}
	}
	d.App.Logf("QUEUE: removed job %q", game)
	jsonSuccess(w, map[string]string{"status": "removed", "game": game})
}

func (d *Deps) handleDataStatus(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var activeJobs int
	d.App.JobQueue.Range(func(k, v interface{}) bool {
		gs := v.(models.GameStatus)
		if gs.State == "Processing" || gs.State == "Pending FTP" {
			activeJobs++
		}
		return true
	})
	pendingJobs := d.FTP.LoadAllPendingFTPJobs()
	pendingFTPJobs := len(pendingJobs)

	// Calculate local data size (Ready/ + Temp/ directories)
	var localDataBytes int64
	for _, dir := range []string{"Ready", "Temp"} {
		filepath.Walk(filepath.Join(d.App.ToolsDir, dir), func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				localDataBytes += info.Size()
			}
			return nil
		})
	}
	// Also count pending_ftp source dirs
	for _, job := range pendingJobs {
		filepath.Walk(job.SourceDir, func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				localDataBytes += info.Size()
			}
			return nil
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"active_jobs":      activeJobs,
		"pending_ftp_jobs": pendingFTPJobs,
		"local_data_bytes": localDataBytes,
		"local_data_mb":    localDataBytes / 1048576,
	})
}

func (d *Deps) handleDataClear(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	// Clear all job statuses
	d.App.JobQueue.Range(func(k, v interface{}) bool {
		d.App.SuppressedJobs.Store(k, true)
		d.App.JobQueue.Delete(k)
		return true
	})
	// Clear pending FTP jobs (goroutines will detect suppression and exit)
	pendingJobs := d.FTP.LoadAllPendingFTPJobs()
	for _, job := range pendingJobs {
		d.App.SuppressedJobs.Store(job.GameName, true)
		d.FTP.DeletePendingFTPJob(job.ID)
		go func(j ftp.PendingFTPJob) {
			time.Sleep(2 * time.Second)
			os.RemoveAll(j.SourceDir)
			if j.GameDir != "" {
				os.RemoveAll(j.GameDir)
			}
		}(job)
	}
	// Clear Ready/ and Temp/ directories
	os.RemoveAll(filepath.Join(d.App.ToolsDir, "Ready"))
	os.RemoveAll(filepath.Join(d.App.ToolsDir, "Temp"))
	os.MkdirAll(filepath.Join(d.App.ToolsDir, "Ready"), 0755)
	os.MkdirAll(filepath.Join(d.App.ToolsDir, "Temp"), 0755)

	jsonSuccess(w, map[string]string{"status": "cleared"})
}

func (d *Deps) handleServerConfig(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"default_drive": d.App.DefaultXboxDrive,
	})
}

func (d *Deps) handleDebug(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h2>GODSend Debug v7.0-IA</h2><p>Server: %s:%s</p>", d.App.ServerIP, d.App.ServerPort)
	fmt.Fprintf(w, "<h3>Cache Status:</h3><ul>")
	d.App.BuildStatesMu.Lock()
	for p, s := range d.App.BuildStates {
		d.App.IAGameCacheMu.RLock()
		count := len(d.App.IAGameCache[p])
		d.App.IAGameCacheMu.RUnlock()
		fmt.Fprintf(w, "<li>%s: %s %d/%d (%d games)</li>",
			p, s.State, atomic.LoadInt32(&s.Loaded), atomic.LoadInt32(&s.Total), count)
	}
	d.App.BuildStatesMu.Unlock()
	fmt.Fprintf(w, "</ul><h3>Transfer (Local ISOs):</h3><ul>")
	for _, g := range d.Local.ScanTransferFolder() {
		fmt.Fprintf(w, "<li>%s</li>", g)
	}
	fmt.Fprintf(w, "</ul><h3>Ready Games:</h3><ul>")
	if files, err := os.ReadDir(filepath.Join(d.App.ToolsDir, "Ready")); err == nil {
		for _, f := range files {
			if f.IsDir() {
				fmt.Fprintf(w, "<li>%s</li>", f.Name())
			}
		}
	}
	fmt.Fprintf(w, "</ul><h3>Active Jobs:</h3><ul>")
	d.App.JobQueue.Range(func(k, v interface{}) bool {
		gs := v.(models.GameStatus)
		fmt.Fprintf(w, "<li>%s: [%s] %s</li>", k, gs.State, gs.Message)
		return true
	})
	fmt.Fprintf(w, "</ul><p><b>Queue:</b> GET or POST <code>/queue/remove?game=ExactName</code> to drop one job (omit <code>game</code> to clear all). Suppresses in-flight status updates until that game is triggered again.</p>")
	fmt.Fprintf(w, "<h3>Xbox Connections:</h3><ul>")
	d.App.XboxConnections.Range(func(k, v interface{}) bool {
		c := v.(models.XboxConnection)
		fmt.Fprintf(w, "<li>%s: IP=%s Mode=%s Drive=%s (%s ago)</li>",
			c.GameName, c.IP, c.Mode, c.Drive, time.Since(c.Timestamp).Round(time.Second))
		return true
	})
	fmt.Fprintf(w, "</ul>")
}

// ==========================================
// FILE SERVING
// ==========================================

func (d *Deps) handleFileServe(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/files/")
	if relPath == "" {
		jsonError(w, 404, "No file path specified")
		return
	}
	decodedPath, err := url.QueryUnescape(relPath)
	if err != nil {
		jsonError(w, 400, "Invalid file path encoding")
		return
	}
	fullPath := filepath.Join(d.App.ToolsDir, "Ready", decodedPath)

	absReady, _ := filepath.Abs(filepath.Join(d.App.ToolsDir, "Ready"))
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absReady) {
		jsonError(w, 403, "Access denied")
		return
	}

	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		jsonError(w, 404, fmt.Sprintf("File not found: %s", filepath.Base(decodedPath)))
		return
	}
	if err != nil {
		jsonError(w, 500, "Cannot access file")
		return
	}

	if info.IsDir() {
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			jsonError(w, 500, "Cannot list directory")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><body><h2>Index of /%s</h2><ul>", relPath)
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				name += "/"
			}
			fmt.Fprintf(w, "<li><a href=\"%s\">%s</a></li>", url.PathEscape(name), name)
		}
		fmt.Fprintf(w, "</ul></body></html>")
		return
	}

	file, err := os.Open(fullPath)
	if err != nil {
		jsonError(w, 500, "Cannot open file")
		return
	}
	defer file.Close()

	fileSize := info.Size()
	fileName := filepath.Base(fullPath)
	adviseFadvise(file, fileSize)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	if rh := r.Header.Get("Range"); rh != "" {
		start, end, err := parseRangeHeader(rh, fileSize)
		if err != nil {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
			w.WriteHeader(stdhttp.StatusRequestedRangeNotSatisfiable)
			return
		}
		cl := end - start + 1
		if _, err := file.Seek(start, 0); err != nil {
			jsonError(w, 500, "File seek error")
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
		w.Header().Set("Content-Length", strconv.FormatInt(cl, 10))
		w.WriteHeader(stdhttp.StatusPartialContent)

		startTime := time.Now()
		bw := bufio.NewWriterSize(w, app.ServeBufferSize)
		written, err := io.CopyN(bw, file, cl)
		if flushErr := bw.Flush(); flushErr != nil && err == nil {
			err = flushErr
		}
		elapsed := time.Since(startTime).Seconds()
		if elapsed < 0.001 {
			elapsed = 0.001
		}
		if err != nil {
			d.App.Logf("FILE WARN: Range xfer interrupted %s after %.2f MB @ %.1f MB/s: %v",
				fileName, float64(written)/1048576, float64(written)/elapsed/1048576, err)
		}
		return
	}

	d.App.Logf("FILE: Sending %s (%.2f MB)", fileName, float64(fileSize)/1048576)
	startTime := time.Now()
	stdhttp.ServeContent(w, r, fileName, info.ModTime(), file)
	elapsed := time.Since(startTime).Seconds()
	if elapsed < 0.001 {
		elapsed = 0.001
	}
	d.App.Logf("FILE: Done %s (%.2f MB) in %.1fs @ %.1f MB/s",
		fileName, float64(fileSize)/1048576, elapsed, float64(fileSize)/elapsed/1048576)
}

func parseRangeHeader(header string, fileSize int64) (int64, int64, error) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, fmt.Errorf("not a byte range: %s", header)
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.HasPrefix(spec, "-") {
		s, err := strconv.ParseInt(spec[1:], 10, 64)
		if err != nil || s <= 0 {
			return 0, 0, fmt.Errorf("bad suffix: %s", spec)
		}
		start := fileSize - s
		if start < 0 {
			start = 0
		}
		return start, fileSize - 1, nil
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("bad format: %s", spec)
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bad start: %s", parts[0])
	}
	var end int64
	if parts[1] == "" {
		end = fileSize - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("bad end: %s", parts[1])
		}
	}
	if start < 0 || start >= fileSize {
		return 0, 0, fmt.Errorf("start %d out of range (size %d)", start, fileSize)
	}
	if end < start {
		return 0, 0, fmt.Errorf("end %d < start %d", end, start)
	}
	if end >= fileSize {
		end = fileSize - 1
	}
	return start, end, nil
}

func adviseFadvise(f *os.File, size int64) {}
