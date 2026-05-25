// client.go — FTP connection, upload, GOD/XEX/Content transfer, and pending FTP job queue.
package ftp

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"godsend/app"
	"godsend/infrastructure/helpers"
	"godsend/models"

	goftp "github.com/jlaffaye/ftp"
)

// FTPUploadStallTimeout aborts an upload whose data channel has produced no
// bytes for this long. Aurora's FTP server occasionally drops the data conn
// silently mid-transfer; without this watchdog, Go's STOR Write blocks
// forever and holds the per-IP semaphore, freezing every other FTP op.
const FTPUploadStallTimeout = 60 * time.Second

// Service provides FTP transfer functionality.
type Service struct {
	App *app.App

	// Per-IP semaphores serializing all FTP sessions to one console at a time.
	// Aurora's FTP server is known to mishandle multiple concurrent data
	// channels (PASV port collisions); without this gate, parallel ops to the
	// same console can stall indefinitely on List/Retr.
	ipSems    sync.Map // map[string]chan struct{} (1-buffered = mutex)
	connOwner sync.Map // map[*goftp.ServerConn]string — IP that owns each issued conn
}

func (s *Service) ipSem(ip string) chan struct{} {
	if v, ok := s.ipSems.Load(ip); ok {
		return v.(chan struct{})
	}
	ch := make(chan struct{}, 1)
	actual, _ := s.ipSems.LoadOrStore(ip, ch)
	return actual.(chan struct{})
}

func (s *Service) acquireIP(ip string) { s.ipSem(ip) <- struct{}{} }
func (s *Service) releaseIP(ip string) {
	select {
	case <-s.ipSem(ip):
	default:
	}
}

// tryAcquireIP attempts to grab the per-IP semaphore with a bounded wait.
// Returns true on success. Background ops (Aurora library refresh, cover
// prefetch) use this so a long-running upload doesn't make them spin
// forever — they just skip the tick and try again later.
func (s *Service) tryAcquireIP(ip string, wait time.Duration) bool {
	sem := s.ipSem(ip)
	if wait <= 0 {
		select {
		case sem <- struct{}{}:
			return true
		default:
			return false
		}
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case sem <- struct{}{}:
		return true
	case <-t.C:
		return false
	}
}

// QuitConn closes an FTP connection issued by ConnectToXboxFTP/ConnectWithRetry
// and releases the per-IP semaphore. Always pair with the connect call:
//
//	c, err := s.FTP.ConnectWithRetry(ip)
//	if err != nil { ... }
//	defer s.FTP.QuitConn(c)
func (s *Service) QuitConn(c *goftp.ServerConn) error {
	if c == nil {
		return nil
	}
	if v, ok := s.connOwner.LoadAndDelete(c); ok {
		s.releaseIP(v.(string))
	}
	return c.Quit()
}

// ── Connection helpers ────────────────────────────────────────────────

// keepAliveDialer dials with TCP keepalive enabled so a silently-dead Xbox
// FTP control connection generates RST/EOF within ~75s rather than hanging
// the goroutine forever.
var keepAliveDialer = net.Dialer{
	Timeout:   app.FTPTimeout,
	KeepAlive: 30 * time.Second,
}

// ConnectToXboxFTP dials and logs into the Xbox Aurora FTP server.
// Acquires a per-IP semaphore; release with QuitConn.
func (s *Service) ConnectToXboxFTP(ip string) (*goftp.ServerConn, error) {
	return s.connectToXboxFTPLocked(ip, true /* blocking acquire */, 0)
}

// TryConnectToXboxFTP is like ConnectToXboxFTP but gives up if the per-IP
// semaphore can't be acquired within `lockWait`. Use for opportunistic
// background ops (Aurora library refresh, periodic prefetch) that should
// skip a tick rather than queue behind a multi-minute upload.
func (s *Service) TryConnectToXboxFTP(ip string, lockWait time.Duration) (*goftp.ServerConn, error) {
	return s.connectToXboxFTPLocked(ip, false /* bounded acquire */, lockWait)
}

func (s *Service) connectToXboxFTPLocked(ip string, blocking bool, lockWait time.Duration) (*goftp.ServerConn, error) {
	if blocking {
		s.acquireIP(ip)
	} else if !s.tryAcquireIP(ip, lockWait) {
		return nil, fmt.Errorf("FTP busy on %s (lock not acquired within %s)", ip, lockWait)
	}
	s.App.Logf("FTP: Connecting to %s:%d...", ip, app.FTPPort)
	c, err := goftp.Dial(fmt.Sprintf("%s:%d", ip, app.FTPPort),
		goftp.DialWithTimeout(app.FTPTimeout),
		goftp.DialWithDialer(keepAliveDialer),
		goftp.DialWithDisabledEPSV(true),
		goftp.DialWithDisabledUTF8(true))
	if err != nil {
		s.releaseIP(ip)
		return nil, fmt.Errorf("FTP connect to %s failed: %v", ip, err)
	}
	if err = c.Login(s.App.FTPUsername, s.App.FTPPassword); err != nil {
		c.Quit()
		s.releaseIP(ip)
		return nil, fmt.Errorf("FTP login failed: %v", err)
	}
	s.connOwner.Store(c, ip)
	s.App.Logf("FTP: Connected to %s", ip)
	return c, nil
}

// ConnectWithRetry tries to connect up to FTPMaxRetries times.
// Acquires a per-IP semaphore; release with QuitConn.
func (s *Service) ConnectWithRetry(ip string) (*goftp.ServerConn, error) {
	var last error
	for i := 1; i <= app.FTPMaxRetries; i++ {
		c, err := s.ConnectToXboxFTP(ip)
		if err == nil {
			return c, nil
		}
		last = err
		if i < app.FTPMaxRetries {
			s.App.Logf("FTP: Attempt %d/%d failed, retry...", i, app.FTPMaxRetries)
			time.Sleep(app.FTPRetryDelay)
		}
	}
	return nil, fmt.Errorf("FTP failed after %d attempts: %v", app.FTPMaxRetries, last)
}

// MkdirAll creates all directories in path on the FTP server.
func MkdirAll(conn *goftp.ServerConn, path string) {
	cur := ""
	for _, p := range strings.Split(strings.Trim(path, "/"), "/") {
		cur += "/" + p
		conn.MakeDir(cur)
	}
}

// ── Upload helpers ────────────────────────────────────────────────────

// UploadFile uploads one file via FTP with progress tracking.
func (s *Service) UploadFile(conn *goftp.ServerConn, localPath, remotePath, gameName string,
	transferred *int64, totalSize int64, fileNum, totalFiles int,
	overallStart time.Time, hwm *float64) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open %s: %v", filepath.Base(localPath), err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %v", filepath.Base(localPath), err)
	}
	now := time.Now()
	fileMB := float64(info.Size()) / 1048576
	s.App.Logf("FTP [%d/%d] Starting: %s (%.1f MB)", fileNum, totalFiles, filepath.Base(localPath), fileMB)
	rdr := &ftpProgressReader{
		reader:       f,
		total:        info.Size(),
		gameName:     gameName,
		fileName:     filepath.Base(localPath),
		lastLog:      now,
		startTime:    now,
		overallStart: overallStart,
		transferred:  transferred,
		totalSize:    totalSize,
		fileNum:      fileNum,
		totalFiles:   totalFiles,
		hwm:          hwm,
		app:          s.App,
	}
	// Stall watchdog: if the data channel stops producing bytes for
	// FTPUploadStallTimeout, abort the upload by closing the FTP control
	// connection. Stor() will then return with an error and the caller
	// (UploadWithRetry / the queue worker) can decide what to do next.
	stop := make(chan struct{})
	go s.watchUploadStall(conn, rdr, filepath.Base(localPath), stop)
	defer close(stop)

	if err = conn.Stor(remotePath, rdr); err != nil {
		return fmt.Errorf("STOR %s: %v", filepath.Base(localPath), err)
	}
	*transferred += info.Size()
	s.App.Logf("FTP [%d/%d] Done:     %s (%.1f MB)", fileNum, totalFiles, filepath.Base(localPath), fileMB)
	return nil
}

// UploadWithRetry uploads a file, reconnecting once on failure.
func (s *Service) UploadWithRetry(conn *goftp.ServerConn, xboxIP, localPath, remotePath, gameName string,
	transferred *int64, totalSize int64, fileNum, totalFiles int, overallStart time.Time) error {
	var hwm float64
	if err := s.UploadFile(conn, localPath, remotePath, gameName, transferred, totalSize, fileNum, totalFiles, overallStart, &hwm); err == nil {
		return nil
	}
	s.App.Logf("FTP [%d/%d] Upload failed — reconnecting and retrying: %s", fileNum, totalFiles, filepath.Base(localPath))
	// Release the broken conn's per-IP lock before reconnecting; otherwise
	// ConnectToXboxFTP would deadlock waiting for the lock we still hold.
	// The caller's outer `defer QuitConn(conn)` becomes a safe no-op.
	s.QuitConn(conn)
	nc, err := s.ConnectToXboxFTP(xboxIP)
	if err != nil {
		return fmt.Errorf("reconnect failed: %v", err)
	}
	defer s.QuitConn(nc)
	return s.UploadFile(nc, localPath, remotePath, gameName, transferred, totalSize, fileNum, totalFiles, overallStart, &hwm)
}

// ── FTP progress reader ───────────────────────────────────────────────

type ftpProgressReader struct {
	reader             io.Reader
	total, written     int64
	gameName, fileName string
	lastLog            time.Time
	startTime          time.Time
	overallStart       time.Time
	transferred        *int64
	totalSize          int64
	fileNum            int
	totalFiles         int
	hwm                *float64
	maxFilePct         float64
	app                *app.App
}

func (r *ftpProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	written := atomic.AddInt64(&r.written, int64(n))

	if time.Since(r.lastLog) > 2*time.Second {
		rawFilePct := float64(written) / float64(r.total) * 100
		if rawFilePct > r.maxFilePct {
			r.maxFilePct = rawFilePct
		}

		overallDone := *r.transferred + written
		rawOverallPct := float64(overallDone) / float64(r.totalSize) * 100
		if rawOverallPct > *r.hwm {
			*r.hwm = rawOverallPct
		}
		overallPct := *r.hwm

		overallMB := float64(overallDone) / 1048576
		totalMB := float64(r.totalSize) / 1048576

		fileElapsed := time.Since(r.startTime).Seconds()
		if fileElapsed < 0.001 {
			fileElapsed = 0.001
		}
		speedMBs := float64(written) / fileElapsed / 1048576

		overallElapsed := time.Since(r.overallStart).Seconds()
		if overallElapsed < 0.001 {
			overallElapsed = 0.001
		}
		elapsedStr := app.FmtDuration(overallElapsed)
		var etaStr string
		if speedMBs > 0 && overallPct < 100 {
			remainingBytes := r.totalSize - overallDone
			if remainingBytes < 0 {
				remainingBytes = 0
			}
			etaSecs := float64(remainingBytes) / (speedMBs * 1048576)
			etaStr = "~" + app.FmtDuration(etaSecs) + " left"
		} else {
			etaStr = "finishing"
		}

		r.app.Logf("FTP [%d/%d] %s  file:%.1f%%  overall:%.1f%% (%.0f/%.0f MB)  @ %.1f MB/s  %s  %s",
			r.fileNum, r.totalFiles, r.fileName,
			r.maxFilePct, overallPct, overallMB, totalMB,
			speedMBs, elapsedStr, etaStr)

		if r.fileNum > 0 {
			r.app.LogStatus(r.gameName, "Processing",
				fmt.Sprintf("FTP: %d/%d (%.1f%%) @ %.1f MB/s | %s | %s",
					r.fileNum, r.totalFiles, overallPct, speedMBs, elapsedStr, etaStr))
		}
		r.lastLog = time.Now()
	}
	return n, err
}

// watchUploadStall is launched in a goroutine for the duration of one upload.
// It polls the progress counter and, if no bytes have moved for
// FTPUploadStallTimeout, force-closes the FTP control connection so the
// blocked Stor call returns with an error.
func (s *Service) watchUploadStall(conn *goftp.ServerConn, rdr *ftpProgressReader, fileName string, stop <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	lastBytes := atomic.LoadInt64(&rdr.written)
	lastChange := time.Now()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			cur := atomic.LoadInt64(&rdr.written)
			if cur != lastBytes {
				lastBytes = cur
				lastChange = time.Now()
				continue
			}
			if time.Since(lastChange) >= FTPUploadStallTimeout {
				s.App.Logf("FTP STALL: no progress on %s for %s — closing conn to abort", fileName, FTPUploadStallTimeout)
				// Quit closes the underlying TCP socket. The blocked Stor
				// goroutine will see EOF / write-on-closed and return an
				// error; the per-IP semaphore is released by QuitConn at
				// the caller's defer.
				_ = conn.Quit()
				return
			}
		}
	}
}

// ── GOD Transfer ──────────────────────────────────────────────────────

// TransferGame uploads a GOD directory to the Xbox via FTP.
func (s *Service) TransferGame(godDir string, conn *models.XboxConnection, gameName, titleID, mediaID, resolvedName string) error {
	fc, err := s.ConnectWithRetry(conn.IP)
	if err != nil {
		return err
	}
	defer s.QuitConn(fc)

	folderID := resolvedName
	if folderID == "" {
		folderID = "Title"
	}
	folderID = helpers.SanitizeFilename(folderID)
	drive := strings.TrimSuffix(conn.Drive, ":")

	// Use custom GOD path if configured, otherwise default to "GOD"
	godSubPath := "GOD"
	if s.App.CustomGodPath != "" {
		godSubPath = strings.ReplaceAll(strings.Trim(s.App.CustomGodPath, "/\\"), "\\", "/")
	}
	base := fmt.Sprintf("/%s/%s/%s - %s", drive, godSubPath, folderID, titleID)
	s.App.Logf("FTP GOD Dest: %s", base)
	MkdirAll(fc, base)

	contentDir := filepath.Join(godDir, titleID)
	if _, err := os.Stat(contentDir); os.IsNotExist(err) {
		return fmt.Errorf("GOD content not found: %s", contentDir)
	}

	var totalFiles int
	var totalSize int64
	filepath.Walk(contentDir, func(p string, i os.FileInfo, e error) error {
		if e == nil && !i.IsDir() {
			totalFiles++
			totalSize += i.Size()
		}
		return nil
	})
	if totalFiles == 0 {
		return fmt.Errorf("no files in GOD content")
	}
	s.App.Logf("FTP GOD: %d files (%.2f GB)", totalFiles, float64(totalSize)/1073741824)

	var xferred int
	var xferSize int64
	xferStart := time.Now()
	return filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(contentDir, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		remote := base + "/" + rel
		if info.IsDir() {
			fc.MakeDir(remote)
			return nil
		}
		xferred++
		return s.UploadWithRetry(fc, conn.IP, path, remote, gameName, &xferSize, totalSize, xferred, totalFiles, xferStart)
	})
}

// TransferContent FTPs extracted content files to
// {Drive}/Content/0000000000000000/{titleID}/00000002/ on the Xbox.
func (s *Service) TransferContent(contentDir string, conn *models.XboxConnection, gameName, titleID string) error {
	fc, err := s.ConnectWithRetry(conn.IP)
	if err != nil {
		return err
	}
	defer s.QuitConn(fc)

	drive := strings.TrimSuffix(conn.Drive, ":")
	base := fmt.Sprintf("/%s/Content/0000000000000000/%s/00000002", drive, titleID)
	s.App.Logf("FTP Content Dest: %s", base)
	MkdirAll(fc, base)

	var totalFiles int
	var totalSize int64
	filepath.Walk(contentDir, func(p string, i os.FileInfo, e error) error {
		if e == nil && !i.IsDir() {
			totalFiles++
			totalSize += i.Size()
		}
		return nil
	})

	var xferred int
	var xferSize int64
	xferStart := time.Now()
	return filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(contentDir, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		remote := base + "/" + rel
		if info.IsDir() {
			fc.MakeDir(remote)
			return nil
		}
		xferred++
		return s.UploadWithRetry(fc, conn.IP, path, remote, gameName, &xferSize, totalSize, xferred, totalFiles, xferStart)
	})
}

// TransferXEX uploads the contents of a XEX folder to /<drive>/<xexPath>/<folderName>/.
func (s *Service) TransferXEX(xexFolder, folderName string, conn *models.XboxConnection, gameName string) error {
	fc, err := s.ConnectWithRetry(conn.IP)
	if err != nil {
		return err
	}
	defer s.QuitConn(fc)

	drive := strings.TrimSuffix(conn.Drive, ":")

	// Use custom XEX path if configured, otherwise default to "XEX"
	xexSubPath := "XEX"
	if s.App.CustomXexPath != "" {
		xexSubPath = strings.ReplaceAll(strings.Trim(s.App.CustomXexPath, "/\\"), "\\", "/")
	}
	base := fmt.Sprintf("/%s/%s/%s", drive, xexSubPath, folderName)
	s.App.Logf("FTP XEX Dest: %s", base)
	MkdirAll(fc, base)

	var totalSize int64
	var totalFiles int
	filepath.Walk(xexFolder, func(p string, i os.FileInfo, e error) error {
		if e == nil && !i.IsDir() {
			totalFiles++
			totalSize += i.Size()
		}
		return nil
	})

	var xferSize int64
	var xferred int
	xferStart := time.Now()
	return filepath.Walk(xexFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(xexFolder, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		remote := base + "/" + rel
		xferred++
		return s.UploadWithRetry(fc, conn.IP, path, remote, gameName, &xferSize, totalSize, xferred, totalFiles, xferStart)
	})
}

// ── Pending FTP Queue ─────────────────────────────────────────────────

// PendingFTPJob describes a game transfer that should be retried indefinitely.
type PendingFTPJob struct {
	ID           string    `json:"id"`
	GameName     string    `json:"game_name"`
	Type         string    `json:"type"`       // "god", "xex", "content"
	SourceDir    string    `json:"source_dir"` // directory with files to upload
	GameDir      string    `json:"game_dir"`   // Ready/ dir to remove on success (may be "")
	XboxIP       string    `json:"xbox_ip"`
	Drive        string    `json:"drive"`
	TitleID      string    `json:"title_id,omitempty"`
	MediaID      string    `json:"media_id,omitempty"`
	ResolvedName string    `json:"resolved_name,omitempty"`
	FolderName   string    `json:"folder_name,omitempty"` // xex only
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Service) pendingFTPJobPath(id string) string {
	return filepath.Join(s.App.PendingFTPDir, id+".json")
}

// SavePendingFTPJob persists a pending job to disk.
func (s *Service) SavePendingFTPJob(job PendingFTPJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return os.WriteFile(s.pendingFTPJobPath(job.ID), data, 0644)
}

// DeletePendingFTPJob removes a persisted pending job.
func (s *Service) DeletePendingFTPJob(id string) {
	os.Remove(s.pendingFTPJobPath(id))
}

// LoadAllPendingFTPJobs reads all pending jobs from disk.
func (s *Service) LoadAllPendingFTPJobs() []PendingFTPJob {
	entries, err := os.ReadDir(s.App.PendingFTPDir)
	if err != nil {
		return nil
	}
	var jobs []PendingFTPJob
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.App.PendingFTPDir, e.Name()))
		if err != nil {
			continue
		}
		var job PendingFTPJob
		if err := json.Unmarshal(data, &job); err != nil {
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs
}

// ExecutePendingFTPJob runs the actual FTP transfer for a pending job.
func (s *Service) ExecutePendingFTPJob(job PendingFTPJob) error {
	conn := &models.XboxConnection{IP: job.XboxIP, Drive: job.Drive}
	switch job.Type {
	case "god":
		if err := s.TransferGame(job.SourceDir, conn, job.GameName, job.TitleID, job.MediaID, job.ResolvedName); err != nil {
			return err
		}
	case "xex":
		if err := s.TransferXEX(job.SourceDir, job.FolderName, conn, job.GameName); err != nil {
			return err
		}
	case "content":
		if err := s.TransferContent(job.SourceDir, conn, job.GameName, job.TitleID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown pending FTP job type: %s", job.Type)
	}
	os.RemoveAll(job.SourceDir)
	if job.GameDir != "" {
		os.RemoveAll(job.GameDir)
	}
	return nil
}

// RetryFTPJobForever retries a pending FTP job indefinitely until it succeeds or is cancelled.
func (s *Service) RetryFTPJobForever(job PendingFTPJob) {
	backoff := 30 * time.Second
	const maxBackoff = 5 * time.Minute
	s.App.Logf("FTP PENDING: %s — will retry every %s", job.GameName, backoff)
	s.App.LogStatus(job.GameName, "Pending FTP", "Xbox unreachable — will retry automatically when FTP comes back online")

	for {
		time.Sleep(backoff)

		if _, suppressed := s.App.SuppressedJobs.Load(job.GameName); suppressed {
			s.App.Logf("FTP PENDING: %s — cancelled, removing", job.GameName)
			s.DeletePendingFTPJob(job.ID)
			os.RemoveAll(job.SourceDir)
			if job.GameDir != "" {
				os.RemoveAll(job.GameDir)
			}
			return
		}

		s.App.Logf("FTP PENDING: Retrying %s...", job.GameName)
		s.App.LogStatus(job.GameName, "Processing", "FTP retry: reconnecting to Xbox...")
		if err := s.ExecutePendingFTPJob(job); err != nil {
			s.App.Logf("FTP PENDING: Retry failed for %s: %v", job.GameName, err)
			s.App.LogStatus(job.GameName, "Pending FTP", fmt.Sprintf("FTP retry failed — will try again: %v", err))
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		s.DeletePendingFTPJob(job.ID)
		s.App.LogFTPComplete(job.GameName, job.TitleID, job.XboxIP)
		s.App.Logf("=== FTP PENDING Complete: %s ===", job.GameName)
		return
	}
}

// SchedulePendingFTP saves the job to disk and starts retrying in the background.
func (s *Service) SchedulePendingFTP(job PendingFTPJob) {
	if err := s.SavePendingFTPJob(job); err != nil {
		s.App.Logf("FTP PENDING: Failed to save job for %s: %v", job.GameName, err)
	}
	go s.RetryFTPJobForever(job)
}
