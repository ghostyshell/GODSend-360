// manager.go — FTP Manager: centralised FTP operations for Electron-side tools.
//
// Provides both synchronous utility operations (list, mkdir, delete, rename, ping,
// drives, batch) and asynchronous trackable jobs (upload, copy, move-game,
// upload-scripts) with progress reporting via ManagerJob.
package ftp

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"godsend/app"

	goftp "github.com/jlaffaye/ftp"
)

// ── Manager tracks async FTP jobs ─────────────────────────────────────

// Manager provides FTP Manager operations (synchronous + async/tracked).
type Manager struct {
	App    *app.App
	FTP    *Service // underlying game-transfer FTP service (reuse connect helpers)
	nextID int64

	mu   sync.RWMutex
	jobs map[int64]*ManagerJob
}

// ManagerJobState is the state of an async FTP job.
type ManagerJobState string

const (
	JobQueued     ManagerJobState = "Queued"
	JobProcessing ManagerJobState = "Processing"
	JobReady      ManagerJobState = "Ready"
	JobError      ManagerJobState = "Error"
)

// ManagerJob represents an async FTP job tracked by the Manager.
type ManagerJob struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	RemotePath string          `json:"remotePath"`
	State      ManagerJobState `json:"state"`
	Progress   int             `json:"progress"` // 0-100
	Error      string          `json:"error,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
}

// NewManager creates a Manager.
func NewManager(a *app.App, ftpSvc *Service) *Manager {
	return &Manager{
		App:  a,
		FTP:  ftpSvc,
		jobs: make(map[int64]*ManagerJob),
	}
}

func (m *Manager) nextJobID() int64 {
	return atomic.AddInt64(&m.nextID, 1)
}

func (m *Manager) storeJob(j *ManagerJob) {
	m.mu.Lock()
	m.jobs[j.ID] = j
	m.mu.Unlock()
}

// ListJobs returns all tracked async jobs.
func (m *Manager) ListJobs() []*ManagerJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ManagerJob, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].ID < out[k].ID })
	return out
}

// RemoveJob removes a completed/failed job from tracking.
func (m *Manager) RemoveJob(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.jobs[id]; ok {
		delete(m.jobs, id)
		return true
	}
	return false
}

// ── FTP connection helper (short-lived for utility ops) ───────────────

func (m *Manager) connect(ip string) (*goftp.ServerConn, error) {
	return m.FTP.ConnectToXboxFTP(ip)
}

// ── Synchronous utility operations ────────────────────────────────────

// DirEntry describes one entry in an FTP directory listing.
type DirEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "dir" or "file"
	Size int64  `json:"size"`
}

// List returns the contents of a remote directory.
func (m *Manager) List(ip, remotePath string) ([]DirEntry, error) {
	c, err := m.connect(ip)
	if err != nil {
		return nil, err
	}
	defer c.Quit()

	entries, err := c.List(remotePath)
	if err != nil {
		return nil, fmt.Errorf("LIST %s: %v", remotePath, err)
	}
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		t := "file"
		if e.Type == goftp.EntryTypeFolder {
			t = "dir"
		}
		out = append(out, DirEntry{Name: e.Name, Type: t, Size: int64(e.Size)})
	}
	return out, nil
}

// Ping tests FTP connectivity to an Xbox.
func (m *Manager) Ping(ip string) error {
	c, err := m.connect(ip)
	if err != nil {
		return err
	}
	c.Quit()
	return nil
}

// Mkdir creates a directory (including parents) on the Xbox via FTP.
func (m *Manager) Mkdir(ip, remotePath string) error {
	c, err := m.connect(ip)
	if err != nil {
		return err
	}
	defer c.Quit()
	MkdirAll(c, remotePath)
	return nil
}

// Delete removes a remote file or directory (tries file first, then dir).
func (m *Manager) Delete(ip, remotePath string) error {
	c, err := m.connect(ip)
	if err != nil {
		return err
	}
	defer c.Quit()

	if err := c.Delete(remotePath); err != nil {
		// Maybe it's a directory
		return m.removeDirRecursive(c, remotePath)
	}
	return nil
}

// removeDirRecursive removes a directory and all its contents recursively.
func (m *Manager) removeDirRecursive(c *goftp.ServerConn, remotePath string) error {
	entries, err := c.List(remotePath)
	if err != nil {
		return c.RemoveDirRecur(remotePath)
	}
	for _, e := range entries {
		child := remotePath + "/" + e.Name
		if e.Type == goftp.EntryTypeFolder {
			if err := m.removeDirRecursive(c, child); err != nil {
				return err
			}
		} else {
			if err := c.Delete(child); err != nil {
				return err
			}
		}
	}
	return c.RemoveDir(remotePath)
}

// Rename renames (or moves) a remote file or directory.
func (m *Manager) Rename(ip, from, to string) error {
	c, err := m.connect(ip)
	if err != nil {
		return err
	}
	defer c.Quit()
	return c.Rename(from, to)
}

// Size returns the size of a remote file.
func (m *Manager) Size(ip, remotePath string) (int64, error) {
	c, err := m.connect(ip)
	if err != nil {
		return 0, err
	}
	defer c.Quit()
	return c.FileSize(remotePath)
}

// DownloadFile downloads a remote file to a local path.
func (m *Manager) DownloadFile(ip, remotePath, localPath string) error {
	c, err := m.connect(ip)
	if err != nil {
		return err
	}
	defer c.Quit()

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create local dir: %v", err)
	}
	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local file: %v", err)
	}
	defer f.Close()
	resp, err := c.Retr(remotePath)
	if err != nil {
		return fmt.Errorf("RETR %s: %v", remotePath, err)
	}
	defer resp.Close()
	_, err = io.Copy(f, resp)
	return err
}

// UploadSingleFile uploads a local file to a remote path.
func (m *Manager) UploadSingleFile(ip, localPath, remotePath string) error {
	c, err := m.connect(ip)
	if err != nil {
		return err
	}
	defer c.Quit()

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open %s: %v", localPath, err)
	}
	defer f.Close()
	return c.Stor(remotePath, f)
}

// ListDrives returns Xbox storage drives visible via FTP.
func (m *Manager) ListDrives(ip string) ([]string, error) {
	c, err := m.connect(ip)
	if err != nil {
		return nil, err
	}
	defer c.Quit()

	entries, err := c.List("/")
	if err != nil {
		return nil, fmt.Errorf("LIST /: %v", err)
	}
	var drives []string
	for _, e := range entries {
		if e.Type != goftp.EntryTypeFolder {
			continue
		}
		d := e.Name + ":"
		// Only include known Xbox drive patterns
		lower := strings.ToLower(e.Name)
		if strings.HasPrefix(lower, "hdd") || strings.HasPrefix(lower, "usb") {
			drives = append(drives, d)
		}
	}
	return drives, nil
}

// ── FTP Test (verbose diagnostics) ────────────────────────────────────

// TestResult holds the outcome of a verbose FTP connectivity test.
type TestResult struct {
	OK  bool     `json:"ok"`
	Log []string `json:"log"`
}

// Test performs a verbose FTP connectivity test and returns a log of steps.
func (m *Manager) Test(ip, user, password string) *TestResult {
	r := &TestResult{Log: []string{}}
	logLine := func(s string) { r.Log = append(r.Log, s) }

	if user == "" {
		user = m.App.FTPUsername
	}
	if password == "" {
		password = m.App.FTPPassword
	}

	logLine(fmt.Sprintf("[TEST] Connecting to %s:21 as %s...", ip, user))
	c, err := goftp.Dial(fmt.Sprintf("%s:%d", ip, app.FTPPort),
		goftp.DialWithTimeout(15*time.Second), goftp.DialWithDisabledEPSV(true), goftp.DialWithDisabledUTF8(true))
	if err != nil {
		logLine(fmt.Sprintf("[TEST] FAILED: %v", err))
		return r
	}
	defer c.Quit()

	if err := c.Login(user, password); err != nil {
		logLine(fmt.Sprintf("[TEST] Login FAILED: %v", err))
		return r
	}
	logLine("[TEST] Login successful.")

	logLine("[TEST] Sending PWD...")
	pwd, err := c.CurrentDir()
	if err != nil {
		logLine(fmt.Sprintf("[TEST] PWD failed: %v", err))
	} else {
		logLine(fmt.Sprintf("[TEST] Working directory: %s", pwd))
	}

	logLine("[TEST] Listing root directory...")
	entries, err := c.List("/")
	if err != nil {
		logLine(fmt.Sprintf("[TEST] LIST failed: %v", err))
	} else {
		for _, e := range entries {
			kind := "FILE"
			if e.Type == goftp.EntryTypeFolder {
				kind = "DIR "
			}
			logLine(fmt.Sprintf("  %s %s  (%d bytes)", kind, e.Name, e.Size))
		}
	}
	logLine("[TEST] Connection test PASSED.")
	r.OK = true
	return r
}

// ── Batch operation ───────────────────────────────────────────────────

// BatchOp describes one operation in a batch request.
type BatchOp struct {
	Op         string `json:"op"`                    // "list", "size", "download_base64", "upload_base64", "ensure_dir", "remove", "remove_dir", "cd", "pwd"
	Path       string `json:"path,omitempty"`        // remote path
	LocalPath  string `json:"local_path,omitempty"`  // for download/upload to local filesystem
	Data       string `json:"data,omitempty"`        // base64 payload for upload_base64
	RemotePath string `json:"remote_path,omitempty"` // alias for path in some ops
}

// BatchResult holds the result of one batch operation.
type BatchResult struct {
	OK    bool        `json:"ok"`
	Error string      `json:"error,omitempty"`
	Data  interface{} `json:"data,omitempty"`
}

// Batch executes multiple FTP operations over a single connection.
func (m *Manager) Batch(ip string, ops []BatchOp) []BatchResult {
	results := make([]BatchResult, len(ops))

	c, err := m.connect(ip)
	if err != nil {
		for i := range results {
			results[i] = BatchResult{OK: false, Error: err.Error()}
		}
		return results
	}
	defer c.Quit()

	for i, op := range ops {
		p := op.Path
		if p == "" {
			p = op.RemotePath
		}

		switch op.Op {
		case "list":
			entries, err := c.List(p)
			if err != nil {
				results[i] = BatchResult{OK: false, Error: err.Error()}
			} else {
				de := make([]DirEntry, 0, len(entries))
				for _, e := range entries {
					t := "file"
					if e.Type == goftp.EntryTypeFolder {
						t = "dir"
					}
					de = append(de, DirEntry{Name: e.Name, Type: t, Size: int64(e.Size)})
				}
				results[i] = BatchResult{OK: true, Data: de}
			}

		case "size":
			sz, err := c.FileSize(p)
			if err != nil {
				results[i] = BatchResult{OK: false, Error: err.Error()}
			} else {
				results[i] = BatchResult{OK: true, Data: sz}
			}

		case "download":
			// Download to local filesystem path
			dir := filepath.Dir(op.LocalPath)
			os.MkdirAll(dir, 0755)
			f, err := os.Create(op.LocalPath)
			if err != nil {
				results[i] = BatchResult{OK: false, Error: err.Error()}
				continue
			}
			resp, err := c.Retr(p)
			if err != nil {
				f.Close()
				results[i] = BatchResult{OK: false, Error: err.Error()}
			} else {
				_, cpErr := io.Copy(f, resp)
				resp.Close()
				f.Close()
				if cpErr != nil {
					results[i] = BatchResult{OK: false, Error: cpErr.Error()}
				} else {
					results[i] = BatchResult{OK: true}
				}
			}

		case "download_base64":
			resp, err := c.Retr(p)
			if err != nil {
				results[i] = BatchResult{OK: false, Error: err.Error()}
			} else {
				buf, rdErr := io.ReadAll(resp)
				resp.Close()
				if rdErr != nil {
					results[i] = BatchResult{OK: false, Error: rdErr.Error()}
				} else {
					results[i] = BatchResult{OK: true, Data: base64.StdEncoding.EncodeToString(buf)}
				}
			}

		case "upload":
			f, err := os.Open(op.LocalPath)
			if err != nil {
				results[i] = BatchResult{OK: false, Error: err.Error()}
				continue
			}
			err = c.Stor(p, f)
			f.Close()
			if err != nil {
				results[i] = BatchResult{OK: false, Error: err.Error()}
			} else {
				results[i] = BatchResult{OK: true}
			}

		case "upload_base64":
			data, err := base64.StdEncoding.DecodeString(op.Data)
			if err != nil {
				results[i] = BatchResult{OK: false, Error: "base64 decode: " + err.Error()}
				continue
			}
			r := strings.NewReader(string(data))
			if err := c.Stor(p, r); err != nil {
				results[i] = BatchResult{OK: false, Error: err.Error()}
			} else {
				results[i] = BatchResult{OK: true}
			}

		case "ensure_dir":
			MkdirAll(c, p)
			results[i] = BatchResult{OK: true}

		case "remove":
			if err := c.Delete(p); err != nil {
				results[i] = BatchResult{OK: false, Error: err.Error()}
			} else {
				results[i] = BatchResult{OK: true}
			}

		case "remove_dir":
			if err := m.removeDirRecursive(c, p); err != nil {
				results[i] = BatchResult{OK: false, Error: err.Error()}
			} else {
				results[i] = BatchResult{OK: true}
			}

		case "cd":
			if err := c.ChangeDir(p); err != nil {
				results[i] = BatchResult{OK: false, Error: err.Error()}
			} else {
				results[i] = BatchResult{OK: true}
			}

		case "pwd":
			dir, err := c.CurrentDir()
			if err != nil {
				results[i] = BatchResult{OK: false, Error: err.Error()}
			} else {
				results[i] = BatchResult{OK: true, Data: dir}
			}

		default:
			results[i] = BatchResult{OK: false, Error: "unknown batch op: " + op.Op}
		}
	}
	return results
}

// ── Async jobs ────────────────────────────────────────────────────────

// Upload starts an async job that uploads local files/folders to a remote path.
// Returns the job IDs immediately.
func (m *Manager) Upload(ip string, localPaths []string, remotePath string) []*ManagerJob {
	var jobs []*ManagerJob
	for _, lp := range localPaths {
		id := m.nextJobID()
		name := filepath.Base(lp)
		j := &ManagerJob{
			ID:         id,
			Name:       name,
			RemotePath: remotePath + "/" + name,
			State:      JobQueued,
			CreatedAt:  time.Now(),
		}
		m.storeJob(j)
		jobs = append(jobs, j)

		go m.doUpload(ip, lp, remotePath+"/"+name, j)
	}
	return jobs
}

func (m *Manager) doUpload(ip, localPath, remotePath string, j *ManagerJob) {
	j.State = JobProcessing
	c, err := m.connect(ip)
	if err != nil {
		j.State = JobError
		j.Error = err.Error()
		return
	}
	defer c.Quit()

	info, err := os.Stat(localPath)
	if err != nil {
		j.State = JobError
		j.Error = err.Error()
		return
	}

	if info.IsDir() {
		if err := m.uploadDirRecursive(c, localPath, remotePath, j); err != nil {
			j.State = JobError
			j.Error = err.Error()
			return
		}
	} else {
		// Single file upload with progress
		f, err := os.Open(localPath)
		if err != nil {
			j.State = JobError
			j.Error = err.Error()
			return
		}
		defer f.Close()

		// Ensure parent dir exists
		parent := remotePath[:strings.LastIndex(remotePath, "/")]
		if parent != "" {
			MkdirAll(c, parent)
		}

		pr := &managerProgressReader{
			reader: f,
			total:  info.Size(),
			job:    j,
		}
		if err := c.Stor(remotePath, pr); err != nil {
			j.State = JobError
			j.Error = err.Error()
			return
		}
	}
	j.State = JobReady
	j.Progress = 100
}

// uploadDirRecursive uploads a local directory tree to a remote path.
func (m *Manager) uploadDirRecursive(c *goftp.ServerConn, localDir, remoteDir string, j *ManagerJob) error {
	MkdirAll(c, remoteDir)

	// First pass: count total size
	var totalSize int64
	filepath.Walk(localDir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})

	var uploaded int64
	return filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(localDir, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		remote := remoteDir + "/" + rel
		if info.IsDir() {
			c.MakeDir(remote)
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		pr := &managerProgressReader{
			reader:    f,
			total:     info.Size(),
			job:       j,
			baseBytes: uploaded,
			totalAll:  totalSize,
		}
		if err := c.Stor(remote, pr); err != nil {
			return err
		}
		uploaded += info.Size()
		if totalSize > 0 {
			j.Progress = int(float64(uploaded) / float64(totalSize) * 100)
		}
		return nil
	})
}

// Copy starts an async job that copies a remote file/directory to a new location
// (download to temp + reupload — Xbox FTP has no server-side copy).
func (m *Manager) Copy(ip, src, dst string, isDir bool) *ManagerJob {
	id := m.nextJobID()
	j := &ManagerJob{
		ID:         id,
		Name:       fmt.Sprintf("Copy: %s → %s", filepath.Base(src), dst),
		RemotePath: dst,
		State:      JobQueued,
		CreatedAt:  time.Now(),
	}
	m.storeJob(j)
	go m.doCopy(ip, src, dst, isDir, j)
	return j
}

func (m *Manager) doCopy(ip, src, dst string, isDir bool, j *ManagerJob) {
	j.State = JobProcessing

	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("godsend-ftp-copy-%d", time.Now().UnixNano()))
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	c, err := m.connect(ip)
	if err != nil {
		j.State = JobError
		j.Error = err.Error()
		return
	}
	defer c.Quit()

	j.Progress = 10
	if isDir {
		localDir := filepath.Join(tmpDir, filepath.Base(src))
		if err := m.downloadDirRecursive(c, src, localDir); err != nil {
			j.State = JobError
			j.Error = "download: " + err.Error()
			return
		}
		j.Progress = 50
		if err := m.uploadDirRecursive(c, localDir, dst, j); err != nil {
			j.State = JobError
			j.Error = "upload: " + err.Error()
			return
		}
	} else {
		localFile := filepath.Join(tmpDir, filepath.Base(src))
		if err := m.downloadSingleFile(c, src, localFile); err != nil {
			j.State = JobError
			j.Error = "download: " + err.Error()
			return
		}
		j.Progress = 50
		f, err := os.Open(localFile)
		if err != nil {
			j.State = JobError
			j.Error = err.Error()
			return
		}
		err = c.Stor(dst, f)
		f.Close()
		if err != nil {
			j.State = JobError
			j.Error = "upload: " + err.Error()
			return
		}
	}
	j.State = JobReady
	j.Progress = 100
}

// MoveGame starts an async job that moves a game between Xbox drives.
// Tries FTP RENAME first (same-drive); falls back to download+upload+delete (cross-drive).
func (m *Manager) MoveGame(ip, gameName, srcDrive, directory, targetDrive string) *ManagerJob {
	dirNorm := strings.ReplaceAll(directory, "\\", "/")
	dirNorm = strings.TrimLeft(dirNorm, "/")
	srcPath := "/" + srcDrive + "/" + dirNorm
	dstDriveClean := strings.TrimSuffix(targetDrive, ":")
	dstPath := "/" + dstDriveClean + "/" + dirNorm

	id := m.nextJobID()
	j := &ManagerJob{
		ID:         id,
		Name:       fmt.Sprintf("Move: %s → %s", gameName, dstDriveClean),
		RemotePath: dstPath,
		State:      JobQueued,
		CreatedAt:  time.Now(),
	}
	m.storeJob(j)
	go m.doMoveGame(ip, gameName, srcPath, dstPath, j)
	return j
}

func (m *Manager) doMoveGame(ip, gameName, srcPath, dstPath string, j *ManagerJob) {
	j.State = JobProcessing
	m.App.Logf("FTP MOVE %s: connecting to %s", gameName, ip)

	c, err := m.connect(ip)
	if err != nil {
		j.State = JobError
		j.Error = err.Error()
		m.App.Logf("FTP MOVE %s: connect failed: %v", gameName, err)
		return
	}
	defer c.Quit()

	// Ensure destination parent exists
	dstParent := dstPath[:strings.LastIndex(dstPath, "/")]
	if dstParent != "" {
		MkdirAll(c, dstParent)
	}

	// Try FTP RENAME (works for same-drive moves)
	m.App.Logf("FTP MOVE %s: attempting rename %s → %s", gameName, srcPath, dstPath)
	if err := c.Rename(srcPath, dstPath); err == nil {
		j.State = JobReady
		j.Progress = 100
		m.App.Logf("FTP MOVE %s: rename succeeded — done", gameName)
		return
	}
	m.App.Logf("FTP MOVE %s: rename not supported cross-drive, falling back to download+reupload", gameName)

	// Fallback: download → upload → delete source
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("godsend-move-%d", time.Now().UnixNano()))
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	j.Progress = 10
	localDir := filepath.Join(tmpDir, filepath.Base(srcPath))
	m.App.Logf("FTP MOVE %s: downloading from %s to temp", gameName, srcPath)
	if err := m.downloadDirRecursive(c, srcPath, localDir); err != nil {
		j.State = JobError
		j.Error = "download: " + err.Error()
		m.App.Logf("FTP MOVE %s: download failed: %v", gameName, err)
		return
	}

	j.Progress = 50
	m.App.Logf("FTP MOVE %s: uploading to %s", gameName, dstPath)

	// Re-connect in case the download took a long time
	c.Quit()
	c, err = m.connect(ip)
	if err != nil {
		j.State = JobError
		j.Error = "reconnect for upload: " + err.Error()
		return
	}

	if err := m.uploadDirSimple(c, localDir, dstPath); err != nil {
		j.State = JobError
		j.Error = "upload: " + err.Error()
		m.App.Logf("FTP MOVE %s: upload failed: %v", gameName, err)
		return
	}

	j.Progress = 90
	m.App.Logf("FTP MOVE %s: removing source %s", gameName, srcPath)
	if err := m.removeDirRecursive(c, srcPath); err != nil {
		m.App.Logf("FTP MOVE %s: WARNING: could not remove source: %v", gameName, err)
		// Don't fail the job — the copy succeeded
	}

	j.State = JobReady
	j.Progress = 100
	m.App.Logf("FTP MOVE %s: complete", gameName)
}

// UploadScripts uploads Aurora scripts to the Xbox with state.lua patching.
func (m *Manager) UploadScripts(ip, scriptsDir, remotePath, serverIP, serverPort string) *ManagerJob {
	id := m.nextJobID()
	j := &ManagerJob{
		ID:         id,
		Name:       "Upload Aurora Scripts",
		RemotePath: remotePath,
		State:      JobQueued,
		CreatedAt:  time.Now(),
	}
	m.storeJob(j)
	go m.doUploadScripts(ip, scriptsDir, remotePath, serverIP, serverPort, j)
	return j
}

func (m *Manager) doUploadScripts(ip, scriptsDir, remotePath, serverIP, serverPort string, j *ManagerJob) {
	j.State = JobProcessing
	m.App.Logf("FTP SCRIPTS: uploading from %s to %s:%s", scriptsDir, ip, remotePath)

	c, err := m.connect(ip)
	if err != nil {
		j.State = JobError
		j.Error = err.Error()
		return
	}
	defer c.Quit()

	MkdirAll(c, remotePath)

	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		j.State = JobError
		j.Error = "read scripts dir: " + err.Error()
		return
	}

	total := len(entries)
	for i, entry := range entries {
		localFile := filepath.Join(scriptsDir, entry.Name())

		if entry.IsDir() {
			if err := m.uploadDirSimple(c, localFile, remotePath+"/"+entry.Name()); err != nil {
				j.State = JobError
				j.Error = fmt.Sprintf("upload %s: %v", entry.Name(), err)
				return
			}
		} else {
			// For state.lua, patch BRAIN_IP and PORT before uploading
			if entry.Name() == "state.lua" && serverIP != "" {
				if err := m.uploadPatchedStateLua(c, localFile, remotePath+"/state.lua", serverIP, serverPort); err != nil {
					j.State = JobError
					j.Error = "patch state.lua: " + err.Error()
					return
				}
			} else {
				f, err := os.Open(localFile)
				if err != nil {
					j.State = JobError
					j.Error = fmt.Sprintf("open %s: %v", entry.Name(), err)
					return
				}
				err = c.Stor(remotePath+"/"+entry.Name(), f)
				f.Close()
				if err != nil {
					j.State = JobError
					j.Error = fmt.Sprintf("upload %s: %v", entry.Name(), err)
					return
				}
			}
		}
		j.Progress = int(float64(i+1) / float64(total) * 100)
	}

	j.State = JobReady
	j.Progress = 100
	m.App.Logf("FTP SCRIPTS: upload complete to %s:%s", ip, remotePath)
}

// uploadPatchedStateLua reads state.lua, patches BRAIN_IP and PORT, and uploads.
func (m *Manager) uploadPatchedStateLua(c *goftp.ServerConn, localPath, remotePath, serverIP, serverPort string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	content := string(data)

	// Patch BRAIN_IP = "..."
	if serverIP != "" {
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "BRAIN_IP") && strings.Contains(trimmed, "=") {
				lines[i] = fmt.Sprintf(`BRAIN_IP = "%s"`, serverIP)
			}
			if strings.HasPrefix(trimmed, "PORT") && strings.Contains(trimmed, "=") && !strings.HasPrefix(trimmed, "PORT_") {
				lines[i] = fmt.Sprintf(`PORT = "%s"`, serverPort)
			}
		}
		content = strings.Join(lines, "\n")
	}

	return c.Stor(remotePath, strings.NewReader(content))
}

// ── Internal directory helpers ────────────────────────────────────────

// downloadDirRecursive downloads a remote directory tree to a local path.
func (m *Manager) downloadDirRecursive(c *goftp.ServerConn, remotePath, localDir string) error {
	os.MkdirAll(localDir, 0755)

	entries, err := c.List(remotePath)
	if err != nil {
		return fmt.Errorf("LIST %s: %v", remotePath, err)
	}

	for _, e := range entries {
		remoteChild := remotePath + "/" + e.Name
		localChild := filepath.Join(localDir, e.Name)

		if e.Type == goftp.EntryTypeFolder {
			if err := m.downloadDirRecursive(c, remoteChild, localChild); err != nil {
				return err
			}
		} else {
			if err := m.downloadSingleFile(c, remoteChild, localChild); err != nil {
				return err
			}
		}
	}
	return nil
}

// downloadSingleFile downloads one file from FTP to a local path.
func (m *Manager) downloadSingleFile(c *goftp.ServerConn, remotePath, localPath string) error {
	dir := filepath.Dir(localPath)
	os.MkdirAll(dir, 0755)

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	resp, err := c.Retr(remotePath)
	if err != nil {
		return fmt.Errorf("RETR %s: %v", remotePath, err)
	}
	defer resp.Close()
	_, err = io.Copy(f, resp)
	return err
}

// uploadDirSimple uploads a local directory tree without progress tracking.
func (m *Manager) uploadDirSimple(c *goftp.ServerConn, localDir, remoteDir string) error {
	MkdirAll(c, remoteDir)
	return filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(localDir, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		remote := remoteDir + "/" + rel
		if info.IsDir() {
			c.MakeDir(remote)
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		return c.Stor(remote, f)
	})
}

// ── Progress reader for async uploads ─────────────────────────────────

type managerProgressReader struct {
	reader    io.Reader
	total     int64
	written   int64
	job       *ManagerJob
	lastLog   time.Time
	baseBytes int64 // bytes already counted from prior files in a multi-file upload
	totalAll  int64 // total bytes across all files (0 = single file mode)
}

func (r *managerProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.written += int64(n)

	if time.Since(r.lastLog) > time.Second {
		if r.totalAll > 0 {
			// Multi-file mode: progress across all files
			overall := r.baseBytes + r.written
			r.job.Progress = int(float64(overall) / float64(r.totalAll) * 100)
		} else if r.total > 0 {
			r.job.Progress = int(float64(r.written) / float64(r.total) * 100)
		}
		r.lastLog = time.Now()
	}
	return n, err
}
