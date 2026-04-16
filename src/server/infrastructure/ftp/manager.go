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
	Detail     string          `json:"detail,omitempty"`
	Speed      string          `json:"speed,omitempty"`
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

	if remotePath != "" {
		if err := c.ChangeDir(remotePath); err != nil {
			return nil, fmt.Errorf("CWD %s: %v", remotePath, err)
		}
	}
	entries, err := c.List("")
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
	if err := c.ChangeDir(remotePath); err != nil {
		return c.RemoveDirRecur(remotePath)
	}
	entries, err := c.List("")
	if err != nil {
		return c.RemoveDirRecur(remotePath)
	}
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
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
		// Only include drives matching Hdd<N> or Usb<N> (e.g. Hdd1, Usb0).
		lower := strings.ToLower(e.Name)
		if len(lower) >= 4 && lower[len(lower)-1] >= '0' && lower[len(lower)-1] <= '9' &&
			(strings.HasPrefix(lower, "hdd") || strings.HasPrefix(lower, "usb")) {
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
			if p != "" {
				if err := c.ChangeDir(p); err != nil {
					results[i] = BatchResult{OK: false, Error: fmt.Sprintf("CWD %s: %v", p, err)}
					continue
				}
			}
			entries, err := c.List("")
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
	j.Detail = "Connecting…"

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

	if isDir {
		// Measure remote size for accurate progress
		j.Detail = "Scanning size…"
		totalBytes := m.remoteDirSize(c, src)

		// Phase 1: Download (0–50%)
		j.Progress = 0
		j.Detail = "Downloading from Xbox…"
		localDir := filepath.Join(tmpDir, filepath.Base(src))
		var downloaded int64
		if err := m.downloadDirTracked(c, src, localDir, j, 0, 50, &totalBytes, &downloaded); err != nil {
			j.State = JobError
			j.Error = "download: " + err.Error()
			return
		}

		// Phase 2: Upload (50–100%)
		j.Progress = 50
		j.Detail = "Uploading to Xbox…"
		j.Speed = ""
		if err := m.uploadDirTracked(c, localDir, dst, j, 50, 100); err != nil {
			j.State = JobError
			j.Error = "upload: " + err.Error()
			return
		}
	} else {
		// Single file — measure size for progress
		j.Detail = "Scanning size…"
		fileSize := m.remoteFileSize(c, src)
		totalBytes := fileSize

		// Phase 1: Download (0–50%)
		j.Progress = 0
		j.Detail = "Downloading " + filepath.Base(src)
		localFile := filepath.Join(tmpDir, filepath.Base(src))
		var downloaded int64
		if err := m.downloadSingleFileTracked(c, src, localFile, j, 0, 50, &totalBytes, &downloaded); err != nil {
			j.State = JobError
			j.Error = "download: " + err.Error()
			return
		}

		// Phase 2: Upload (50–100%)
		j.Progress = 50
		j.Detail = "Uploading " + filepath.Base(src)
		j.Speed = ""
		f, err := os.Open(localFile)
		if err != nil {
			j.State = JobError
			j.Error = err.Error()
			return
		}
		pr := &managerProgressReader{
			reader:       f,
			total:        fileSize,
			job:          j,
			baseProgress: 50,
			maxProgress:  100,
		}
		err = c.Stor(dst, pr)
		f.Close()
		if err != nil {
			j.State = JobError
			j.Error = "upload: " + err.Error()
			return
		}
	}
	j.State = JobReady
	j.Progress = 100
	j.Detail = "Done"
	j.Speed = ""
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
	j.Detail = "Connecting…"
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
	j.Detail = "Attempting same-drive rename…"
	m.App.Logf("FTP MOVE %s: attempting rename %s → %s", gameName, srcPath, dstPath)
	if err := c.Rename(srcPath, dstPath); err == nil {
		j.State = JobReady
		j.Progress = 100
		j.Detail = "Done"
		j.Speed = ""
		m.App.Logf("FTP MOVE %s: rename succeeded — done", gameName)
		return
	}
	m.App.Logf("FTP MOVE %s: rename not supported cross-drive, falling back to download+reupload", gameName)

	// Measure remote directory size for accurate progress
	j.Detail = "Scanning game size…"
	totalBytes := m.remoteDirSize(c, srcPath)
	m.App.Logf("FTP MOVE %s: total size = %d bytes", gameName, totalBytes)

	// Fallback: download → upload → delete source
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("godsend-move-%d", time.Now().UnixNano()))
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	// Phase 1: Download (progress 0–45%)
	j.Progress = 0
	j.Detail = "Downloading from Xbox…"
	localDir := filepath.Join(tmpDir, filepath.Base(srcPath))
	m.App.Logf("FTP MOVE %s: downloading from %s to temp", gameName, srcPath)
	var downloaded int64
	if err := m.downloadDirTracked(c, srcPath, localDir, j, 0, 45, &totalBytes, &downloaded); err != nil {
		j.State = JobError
		j.Error = "download: " + err.Error()
		m.App.Logf("FTP MOVE %s: download failed: %v", gameName, err)
		return
	}

	j.Progress = 45
	j.Detail = "Reconnecting for upload…"
	j.Speed = ""
	m.App.Logf("FTP MOVE %s: uploading to %s", gameName, dstPath)

	// Re-connect in case the download took a long time
	c.Quit()
	c, err = m.connect(ip)
	if err != nil {
		j.State = JobError
		j.Error = "reconnect for upload: " + err.Error()
		return
	}

	// Phase 2: Upload (progress 45–90%)
	j.Detail = "Uploading to Xbox…"
	if err := m.uploadDirTracked(c, localDir, dstPath, j, 45, 90); err != nil {
		j.State = JobError
		j.Error = "upload: " + err.Error()
		m.App.Logf("FTP MOVE %s: upload failed: %v", gameName, err)
		return
	}

	// Phase 3: Cleanup (progress 90–100%)
	j.Progress = 90
	j.Detail = "Removing source files…"
	j.Speed = ""
	m.App.Logf("FTP MOVE %s: removing source %s", gameName, srcPath)
	if err := m.removeDirRecursive(c, srcPath); err != nil {
		m.App.Logf("FTP MOVE %s: WARNING: could not remove source: %v", gameName, err)
	}

	j.State = JobReady
	j.Progress = 100
	j.Detail = "Done"
	j.Speed = ""
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

// remoteDirSize walks a remote directory tree and returns total file bytes.
func (m *Manager) remoteDirSize(c *goftp.ServerConn, remotePath string) int64 {
	var total int64
	if err := c.ChangeDir(remotePath); err != nil {
		return 0
	}
	entries, err := c.List("")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		if e.Type == goftp.EntryTypeFolder {
			total += m.remoteDirSize(c, remotePath+"/"+e.Name)
		} else {
			total += int64(e.Size)
		}
	}
	return total
}

// remoteFileSize returns the size of a single remote file via FTP SIZE/LIST.
func (m *Manager) remoteFileSize(c *goftp.ServerConn, remotePath string) int64 {
	sz, err := c.FileSize(remotePath)
	if err == nil {
		return sz
	}
	// Fallback: list parent and find by name
	parent := remotePath[:strings.LastIndex(remotePath, "/")]
	name := remotePath[strings.LastIndex(remotePath, "/")+1:]
	if parent == "" {
		parent = "/"
	}
	if err := c.ChangeDir(parent); err != nil {
		return 0
	}
	entries, err := c.List("")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.Name == name {
			return int64(e.Size)
		}
	}
	return 0
}

// downloadDirTracked downloads a remote directory tree with streaming progress.
func (m *Manager) downloadDirTracked(c *goftp.ServerConn, remotePath, localDir string, j *ManagerJob, baseProgress, maxProgress int, totalBytes *int64, downloaded *int64) error {
	os.MkdirAll(localDir, 0755)

	if err := c.ChangeDir(remotePath); err != nil {
		return fmt.Errorf("CWD %s: %v", remotePath, err)
	}
	entries, err := c.List("")
	if err != nil {
		return fmt.Errorf("LIST %s: %v", remotePath, err)
	}

	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		remoteChild := remotePath + "/" + e.Name
		localChild := filepath.Join(localDir, e.Name)

		if e.Type == goftp.EntryTypeFolder {
			if err := m.downloadDirTracked(c, remoteChild, localChild, j, baseProgress, maxProgress, totalBytes, downloaded); err != nil {
				return err
			}
		} else {
			j.Detail = "Downloading " + e.Name
			if err := m.downloadSingleFileTracked(c, remoteChild, localChild, j, baseProgress, maxProgress, totalBytes, downloaded); err != nil {
				return err
			}
		}
	}
	return nil
}

// downloadSingleFileTracked downloads one file with streaming progress updates.
func (m *Manager) downloadSingleFileTracked(c *goftp.ServerConn, remotePath, localPath string, j *ManagerJob, baseProgress, maxProgress int, totalBytes *int64, downloaded *int64) error {
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

	pw := &progressWriter{
		writer:       f,
		job:          j,
		downloaded:   downloaded,
		totalBytes:   *totalBytes,
		baseProgress: baseProgress,
		maxProgress:  maxProgress,
	}
	_, err = io.Copy(pw, resp)
	return err
}

// progressWriter wraps an io.Writer and updates job progress/speed on writes.
type progressWriter struct {
	writer       io.Writer
	job          *ManagerJob
	downloaded   *int64
	totalBytes   int64
	baseProgress int
	maxProgress  int
	lastUpdate   time.Time
	windowStart  time.Time
	windowBytes  int64
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	*pw.downloaded += int64(n)
	pw.windowBytes += int64(n)

	now := time.Now()
	if pw.windowStart.IsZero() {
		pw.windowStart = now
	}

	if now.Sub(pw.lastUpdate) >= 500*time.Millisecond {
		if pw.totalBytes > 0 {
			frac := float64(*pw.downloaded) / float64(pw.totalBytes)
			span := pw.maxProgress - pw.baseProgress
			pw.job.Progress = pw.baseProgress + int(frac*float64(span))
		}
		elapsed := now.Sub(pw.windowStart).Seconds()
		if elapsed > 0.5 {
			bps := float64(pw.windowBytes) / elapsed
			pw.job.Speed = formatSpeed(bps)
			pw.windowBytes = 0
			pw.windowStart = now
		}
		pw.lastUpdate = now
	}
	return n, err
}

// uploadDirTracked uploads a local directory tree with streaming progress.
func (m *Manager) uploadDirTracked(c *goftp.ServerConn, localDir, remoteDir string, j *ManagerJob, baseProgress, maxProgress int) error {
	MkdirAll(c, remoteDir)

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

		j.Detail = "Uploading " + info.Name()

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		pr := &managerProgressReader{
			reader:       f,
			total:        info.Size(),
			job:          j,
			baseBytes:    uploaded,
			totalAll:     totalSize,
			baseProgress: baseProgress,
			maxProgress:  maxProgress,
		}
		if err := c.Stor(remote, pr); err != nil {
			return err
		}
		uploaded += info.Size()
		return nil
	})
}

func formatSpeed(bytesPerSec float64) string {
	switch {
	case bytesPerSec >= 1024*1024:
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/(1024*1024))
	case bytesPerSec >= 1024:
		return fmt.Sprintf("%.0f KB/s", bytesPerSec/1024)
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}

// downloadDirRecursive downloads a remote directory tree to a local path.
func (m *Manager) downloadDirRecursive(c *goftp.ServerConn, remotePath, localDir string) error {
	os.MkdirAll(localDir, 0755)

	if err := c.ChangeDir(remotePath); err != nil {
		return fmt.Errorf("CWD %s: %v", remotePath, err)
	}
	entries, err := c.List("")
	if err != nil {
		return fmt.Errorf("LIST %s: %v", remotePath, err)
	}

	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
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
	reader       io.Reader
	total        int64
	written      int64
	job          *ManagerJob
	lastLog      time.Time
	baseBytes    int64 // bytes already counted from prior files in a multi-file upload
	totalAll     int64 // total bytes across all files (0 = single file mode)
	baseProgress int   // progress range start (default 0)
	maxProgress  int   // progress range end (default 100)
	windowStart  time.Time
	windowBytes  int64
}

func (r *managerProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.written += int64(n)
	r.windowBytes += int64(n)

	now := time.Now()
	if r.windowStart.IsZero() {
		r.windowStart = now
	}

	maxP := r.maxProgress
	if maxP == 0 {
		maxP = 100
	}
	baseP := r.baseProgress

	if now.Sub(r.lastLog) >= 500*time.Millisecond {
		if r.totalAll > 0 {
			overall := r.baseBytes + r.written
			frac := float64(overall) / float64(r.totalAll)
			r.job.Progress = baseP + int(frac*float64(maxP-baseP))
		} else if r.total > 0 {
			frac := float64(r.written) / float64(r.total)
			r.job.Progress = baseP + int(frac*float64(maxP-baseP))
		}
		elapsed := now.Sub(r.windowStart).Seconds()
		if elapsed > 0.5 {
			bps := float64(r.windowBytes) / elapsed
			r.job.Speed = formatSpeed(bps)
			r.windowBytes = 0
			r.windowStart = now
		}
		r.lastLog = now
	}
	return n, err
}
