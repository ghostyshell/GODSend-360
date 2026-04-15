// handlers_ftp_manager.go — HTTP handlers for centralised FTP Manager operations.
package http

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"strconv"
	"strings"

	"godsend/infrastructure/ftp"
)

// ── Synchronous utility endpoints ─────────────────────────────────────

// POST /ftp/ping  { "ip": "..." }
func (d *Deps) handleFTPPing(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" {
		jsonError(w, 400, "ip required")
		return
	}
	if err := d.FTPMgr.Ping(req.IP); err != nil {
		jsonError(w, 502, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "state": "ok"})
}

// POST /ftp/list  { "ip": "...", "path": "/" }
func (d *Deps) handleFTPList(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP   string `json:"ip"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" {
		jsonError(w, 400, "ip required")
		return
	}
	if req.Path == "" {
		req.Path = "/"
	}
	entries, err := d.FTPMgr.List(req.IP, req.Path)
	if err != nil {
		jsonError(w, 502, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"entries": entries,
		"cwd":     req.Path,
	})
}

// POST /ftp/mkdir  { "ip": "...", "path": "/Hdd1/foo" }
func (d *Deps) handleFTPMkdir(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP   string `json:"ip"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" || req.Path == "" {
		jsonError(w, 400, "ip and path required")
		return
	}
	if err := d.FTPMgr.Mkdir(req.IP, req.Path); err != nil {
		jsonError(w, 502, err.Error())
		return
	}
	jsonSuccess(w, map[string]string{"state": "ok"})
}

// POST /ftp/delete  { "ip": "...", "path": "/Hdd1/foo" }
func (d *Deps) handleFTPDelete(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP   string `json:"ip"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" || req.Path == "" {
		jsonError(w, 400, "ip and path required")
		return
	}
	if err := d.FTPMgr.Delete(req.IP, req.Path); err != nil {
		jsonError(w, 502, err.Error())
		return
	}
	jsonSuccess(w, map[string]string{"state": "ok"})
}

// POST /ftp/rename  { "ip": "...", "from": "...", "to": "..." }
func (d *Deps) handleFTPRename(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP   string `json:"ip"`
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" || req.From == "" || req.To == "" {
		jsonError(w, 400, "ip, from, and to required")
		return
	}
	if err := d.FTPMgr.Rename(req.IP, req.From, req.To); err != nil {
		jsonError(w, 502, err.Error())
		return
	}
	jsonSuccess(w, map[string]string{"state": "ok"})
}

// POST /ftp/size  { "ip": "...", "path": "..." }
func (d *Deps) handleFTPSize(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP   string `json:"ip"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" || req.Path == "" {
		jsonError(w, 400, "ip and path required")
		return
	}
	sz, err := d.FTPMgr.Size(req.IP, req.Path)
	if err != nil {
		jsonError(w, 502, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "size": sz})
}

// POST /ftp/download-file  { "ip": "...", "remote_path": "...", "local_path": "..." }
func (d *Deps) handleFTPDownloadFile(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP         string `json:"ip"`
		RemotePath string `json:"remote_path"`
		LocalPath  string `json:"local_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" || req.RemotePath == "" || req.LocalPath == "" {
		jsonError(w, 400, "ip, remote_path, and local_path required")
		return
	}
	if err := d.FTPMgr.DownloadFile(req.IP, req.RemotePath, req.LocalPath); err != nil {
		jsonError(w, 502, err.Error())
		return
	}
	jsonSuccess(w, map[string]string{"state": "ok"})
}

// POST /ftp/upload-file  { "ip": "...", "local_path": "...", "remote_path": "..." }
func (d *Deps) handleFTPUploadFile(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP         string `json:"ip"`
		LocalPath  string `json:"local_path"`
		RemotePath string `json:"remote_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" || req.LocalPath == "" || req.RemotePath == "" {
		jsonError(w, 400, "ip, local_path, and remote_path required")
		return
	}
	if err := d.FTPMgr.UploadSingleFile(req.IP, req.LocalPath, req.RemotePath); err != nil {
		jsonError(w, 502, err.Error())
		return
	}
	jsonSuccess(w, map[string]string{"state": "ok"})
}

// POST /ftp/test  { "ip": "...", "user": "...", "password": "..." }
func (d *Deps) handleFTPTest(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP       string `json:"ip"`
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" {
		jsonError(w, 400, "ip required")
		return
	}
	result := d.FTPMgr.Test(req.IP, req.User, req.Password)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// GET /ftp/drives?ip=X
func (d *Deps) handleFTPDrives(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	ip := r.URL.Query().Get("ip")
	if ip == "" {
		jsonError(w, 400, "ip required")
		return
	}
	drives, err := d.FTPMgr.ListDrives(ip)
	if err != nil {
		jsonError(w, 502, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "drives": drives})
}

// POST /ftp/batch  { "ip": "...", "ops": [...] }
func (d *Deps) handleFTPBatch(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP  string        `json:"ip"`
		Ops []ftp.BatchOp `json:"ops"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" {
		jsonError(w, 400, "ip and ops required")
		return
	}
	results := d.FTPMgr.Batch(req.IP, req.Ops)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "results": results})
}

// ── Async trackable endpoints ─────────────────────────────────────────

// POST /ftp/upload  { "ip": "...", "local_paths": [...], "remote_path": "..." }
func (d *Deps) handleFTPUpload(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP         string   `json:"ip"`
		LocalPaths []string `json:"local_paths"`
		RemotePath string   `json:"remote_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" || len(req.LocalPaths) == 0 || req.RemotePath == "" {
		jsonError(w, 400, "ip, local_paths, and remote_path required")
		return
	}
	jobs := d.FTPMgr.Upload(req.IP, req.LocalPaths, req.RemotePath)
	type jobInfo struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	out := make([]jobInfo, len(jobs))
	for i, j := range jobs {
		out[i] = jobInfo{ID: j.ID, Name: j.Name}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "jobs": out})
}

// POST /ftp/copy  { "ip": "...", "src": "...", "dst": "...", "is_dir": false }
func (d *Deps) handleFTPCopy(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP    string `json:"ip"`
		Src   string `json:"src"`
		Dst   string `json:"dst"`
		IsDir bool   `json:"is_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" || req.Src == "" || req.Dst == "" {
		jsonError(w, 400, "ip, src, and dst required")
		return
	}
	j := d.FTPMgr.Copy(req.IP, req.Src, req.Dst, req.IsDir)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": j.ID, "name": j.Name})
}

// POST /ftp/move-game  { "ip": "...", "game_name": "...", "src_drive": "...", "directory": "...", "target_drive": "..." }
func (d *Deps) handleFTPMoveGame(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP          string `json:"ip"`
		GameName    string `json:"game_name"`
		SrcDrive    string `json:"src_drive"`
		Directory   string `json:"directory"`
		TargetDrive string `json:"target_drive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, 400, "invalid request body")
		return
	}
	if req.IP == "" || req.SrcDrive == "" || req.Directory == "" || req.TargetDrive == "" {
		jsonError(w, 400, "ip, src_drive, directory, and target_drive required")
		return
	}
	srcClean := strings.TrimSuffix(req.SrcDrive, ":")
	dstClean := strings.TrimSuffix(req.TargetDrive, ":")
	if srcClean == dstClean {
		jsonError(w, 400, "source and destination drive are the same")
		return
	}
	name := req.GameName
	if name == "" {
		name = "Unknown"
	}
	j := d.FTPMgr.MoveGame(req.IP, name, srcClean, req.Directory, req.TargetDrive)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"id":      j.ID,
		"name":    j.Name,
		"message": fmt.Sprintf("Queued move of %s to %s", name, dstClean),
	})
}

// POST /ftp/upload-scripts  { "ip": "...", "scripts_dir": "...", "remote_path": "...", "server_ip": "...", "server_port": "..." }
func (d *Deps) handleFTPUploadScripts(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		IP         string `json:"ip"`
		ScriptsDir string `json:"scripts_dir"`
		RemotePath string `json:"remote_path"`
		ServerIP   string `json:"server_ip"`
		ServerPort string `json:"server_port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" || req.ScriptsDir == "" || req.RemotePath == "" {
		jsonError(w, 400, "ip, scripts_dir, and remote_path required")
		return
	}
	j := d.FTPMgr.UploadScripts(req.IP, req.ScriptsDir, req.RemotePath, req.ServerIP, req.ServerPort)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": j.ID, "name": j.Name})
}

// ── Job management ────────────────────────────────────────────────────

// GET /ftp/jobs — returns all tracked FTP manager jobs
func (d *Deps) handleFTPJobs(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	jobs := d.FTPMgr.ListJobs()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "jobs": jobs})
}

// DELETE /ftp/jobs?id=N — remove a completed/failed job from tracking
func (d *Deps) handleFTPJobRemove(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, 400, "invalid id")
		return
	}
	if d.FTPMgr.RemoveJob(id) {
		jsonSuccess(w, map[string]string{"state": "ok"})
	} else {
		jsonError(w, 404, "job not found")
	}
}
