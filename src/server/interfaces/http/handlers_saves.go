// handlers_saves.go — HTTP handlers for save-game management.
package http

import (
	"encoding/json"
	"net/http"

	"godsend/services/saves"
)

// handleSavesDiscover lists profiles that have save data on the Xbox.
// GET /saves/discover?ip=X&drive=auto&title_id=XXXXXXXX
func (d *Deps) handleSavesDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ip := r.URL.Query().Get("ip")
	drive := r.URL.Query().Get("drive")
	titleID := r.URL.Query().Get("title_id")
	if ip == "" {
		jsonError(w, http.StatusBadRequest, "Missing ip parameter")
		return
	}

	svc := &saves.Service{App: d.App, FTPMgr: d.FTPMgr}
	var profiles []saves.ProfileSaves
	var err error
	if titleID == "" {
		profiles, err = svc.ListAllProfiles(ip, drive)
	} else {
		profiles, err = svc.DiscoverSaves(ip, drive, titleID)
	}
	if err != nil {
		d.App.Logf("SAVES: discover error: %v", err)
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"profiles": profiles,
	})
}

// handleSavesList lists save files for a specific title+profile combination.
// GET /saves/list?ip=X&drive=auto&title_id=X&profile_id=X
func (d *Deps) handleSavesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ip := r.URL.Query().Get("ip")
	drive := r.URL.Query().Get("drive")
	titleID := r.URL.Query().Get("title_id")
	profileID := r.URL.Query().Get("profile_id")
	if ip == "" || titleID == "" || profileID == "" {
		jsonError(w, http.StatusBadRequest, "Missing required parameter (ip, title_id, profile_id)")
		return
	}

	svc := &saves.Service{App: d.App, FTPMgr: d.FTPMgr}
	entries, err := svc.ListSaveFiles(ip, drive, titleID, profileID)
	if err != nil {
		d.App.Logf("SAVES: list error: %v", err)
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"entries": entries,
	})
}

// handleSavesDownload downloads save data from Xbox to local backup.
// POST /saves/download
// Body: { "ip": "...", "drive": "", "title_id": "...", "profile_id": "..." }
func (d *Deps) handleSavesDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		IP        string `json:"ip"`
		Drive     string `json:"drive"`
		TitleID   string `json:"title_id"`
		ProfileID string `json:"profile_id"`
		GameName  string `json:"game_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if req.IP == "" || req.TitleID == "" || req.ProfileID == "" {
		jsonError(w, http.StatusBadRequest, "Missing required field (ip, title_id, profile_id)")
		return
	}

	localDir := d.App.SaveBackupDir
	if localDir == "" {
		localDir = d.App.TransferDir
	}

	svc := &saves.Service{App: d.App, FTPMgr: d.FTPMgr}
	if err := svc.DownloadSave(req.IP, req.Drive, req.TitleID, req.ProfileID, localDir, req.GameName); err != nil {
		d.App.Logf("SAVES: download error: %v", err)
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	d.App.Logf("SAVES: downloaded saves for %s/%s", req.TitleID, req.ProfileID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true,
	})
}

// handleSavesBackupAll backs up every profile package + every save on the
// console into <SaveBackupDir>/Saves/<gamertag> (<XUID>)/...
// POST /saves/backup-all
// Body: { "ip": "...", "drive": "" }
func (d *Deps) handleSavesBackupAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		IP    string `json:"ip"`
		Drive string `json:"drive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if req.IP == "" {
		jsonError(w, http.StatusBadRequest, "Missing ip field")
		return
	}

	localDir := d.App.SaveBackupDir
	if localDir == "" {
		localDir = d.App.TransferDir
	}

	svc := &saves.Service{App: d.App, FTPMgr: d.FTPMgr}
	result, err := svc.BackupAllProfiles(req.IP, req.Drive, localDir)
	if err != nil {
		d.App.Logf("SAVES: backup-all error: %v", err)
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"result": result,
	})
}

// handleSavesDelete deletes save data from Xbox.
// POST /saves/delete
func (d *Deps) handleSavesDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		IP        string `json:"ip"`
		Drive     string `json:"drive"`
		TitleID   string `json:"title_id"`
		ProfileID string `json:"profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if req.IP == "" || req.TitleID == "" || req.ProfileID == "" {
		jsonError(w, http.StatusBadRequest, "Missing required field (ip, title_id, profile_id)")
		return
	}

	svc := &saves.Service{App: d.App, FTPMgr: d.FTPMgr}
	if err := svc.DeleteSave(req.IP, req.Drive, req.TitleID, req.ProfileID); err != nil {
		d.App.Logf("SAVES: delete error: %v", err)
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	d.App.Logf("SAVES: deleted save data for %s/%s", req.TitleID, req.ProfileID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true,
	})
}

// handleSavesCopy copies saves from one profile to another with optional re-signing.
// POST /saves/copy
// Body: { "ip": "...", "drive": "", "title_id": "...", "src_profile": "...", "dst_profile": "...", "use_keyvault": true }
func (d *Deps) handleSavesCopy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		IP         string `json:"ip"`
		Drive      string `json:"drive"`
		TitleID    string `json:"title_id"`
		SrcProfile string `json:"src_profile"`
		DstProfile string `json:"dst_profile"`
		UseKV      bool   `json:"use_keyvault"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if req.IP == "" || req.TitleID == "" || req.SrcProfile == "" || req.DstProfile == "" {
		jsonError(w, http.StatusBadRequest, "Missing required field")
		return
	}

	localDir := d.App.SaveBackupDir
	if localDir == "" {
		localDir = d.App.TransferDir
	}

	svc := &saves.Service{App: d.App, FTPMgr: d.FTPMgr}

	var kv *saves.KeyVault
	if req.UseKV {
		var err error
		kv, err = svc.TryFindKeyVaultOnConsole(req.IP)
		if err != nil {
			d.App.Logf("SAVES: keyvault not found: %v — copying raw", err)
			// Continue without keyvault; raw copy may still work same-console
		}
	}

	result, err := svc.CopySaveToProfile(req.IP, req.Drive, req.TitleID,
		req.SrcProfile, req.DstProfile, localDir, kv)
	if err != nil {
		d.App.Logf("SAVES: copy error: %v", err)
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	d.App.Logf("SAVES: copied %d files from %s to %s (resigned=%v)",
		result.FilesCopied, req.SrcProfile, req.DstProfile, result.Resigned)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"result": result,
	})
}

// handleSavesKeyvault checks if a keyvault is available on the console.
// GET /saves/keyvault-status?ip=X
func (d *Deps) handleSavesKeyvaultStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ip := r.URL.Query().Get("ip")
	if ip == "" {
		jsonError(w, http.StatusBadRequest, "Missing ip parameter")
		return
	}

	svc := &saves.Service{App: d.App, FTPMgr: d.FTPMgr}
	kv, err := svc.TryFindKeyVaultOnConsole(ip)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"found":   err == nil && kv != nil,
		"message": func() string { if err != nil { return err.Error() }; return "keyvault available" }(),
	})
}
