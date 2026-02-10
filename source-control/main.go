package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"
)

// ==========================================
// CONFIGURATION
// ==========================================
const (
	Port            = "8080"
	MaxPartSize     = 1800000000
	MaxDLCSizeBytes = 349 * 1024 * 1024
	CopyBufferSize  = 4 * 1024 * 1024
	ServeBufferSize = 128 * 1024  // 128KB - tuned for Xbox 360 TCP window
	FTPPort         = 21
	FTPTimeout      = 30 * time.Second
	FTPBufferSize   = 1 * 1024 * 1024
	FTPMaxRetries   = 3
	FTPRetryDelay   = 2 * time.Second

	// TCP tuning
	TCPSendBuffer = 512 * 1024 // SO_SNDBUF - kernel send buffer per connection
	TCPKeepAlive  = 30 * time.Second

	Myrient360Base     = "https://myrient.erista.me/files/Redump/Microsoft%20-%20Xbox%20360/"
	MyrientOrigBase    = "https://myrient.erista.me/files/Redump/Microsoft%20-%20Xbox/"
	MyrientDigitalBase = "https://myrient.erista.me/files/No-Intro/Microsoft%20-%20Xbox%20360%20(Digital)/"
	MyrientDLCBase     = "https://myrient.erista.me/files/No-Intro/Microsoft%20-%20Xbox%20360%20(Digital)/"
)

var (
	toolsDir        string
	sevenZipBin     string
	isoGodBin       string
	jobQueue        sync.Map
	serverIP        string
	gamePartsMap    sync.Map
	copyBuffer      []byte
	xboxConnections sync.Map
)

type XboxConnection struct {
	IP        string `json:"ip"`
	Drive     string `json:"drive"`
	GameName  string `json:"game"`
	Platform  string `json:"platform"`
	Mode      string `json:"mode"`
	Timestamp time.Time
}
type GameStatus struct {
	State   string `json:"state"`
	Message string `json:"message"`
}
type ProgressWriter struct {
	Total     int64
	Written   int64
	GameName  string
	LastLog   time.Time
	StartTime time.Time
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.Written += int64(n)
	if time.Since(pw.LastLog) > 500*time.Millisecond || pw.Written == pw.Total {
		percent := float64(pw.Written) / float64(pw.Total) * 100
		elapsed := time.Since(pw.StartTime).Seconds()
		if elapsed < 1 { elapsed = 1 }
		speed := float64(pw.Written) / elapsed / 1048576
		fmt.Printf("\r[%s] Download: %.1f%% (%.1f/%.1f MB) @ %.1f MB/s   ",
			time.Now().Format("15:04:05"), percent, float64(pw.Written)/1048576, float64(pw.Total)/1048576, speed)
		logStatus(pw.GameName, "Processing", fmt.Sprintf("Downloading: %.0f%%", percent))
		pw.LastLog = time.Now()
	}
	return n, nil
}

// ==========================================
// ERROR / LOGGING HELPERS
// ==========================================

func jsonError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"state": "Error", "message": message})
}
func jsonSuccess(w http.ResponseWriter, data map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
func recoverMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logf("PANIC: %s %s: %v", r.Method, r.URL.Path, err)
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				logf("STACK: %s", string(buf[:n]))
				jsonError(w, 500, "Internal server error")
			}
		}()
		next(w, r)
	}
}
func logf(format string, args ...interface{}) {
	fmt.Printf("[%s] "+format+"\n", append([]interface{}{time.Now().Format("15:04:05")}, args...)...)
}
func logStatus(game, state, msg string) {
	jobQueue.Store(game, GameStatus{State: state, Message: msg})
}

// ==========================================
// MAIN & SETUP
// ==========================================

func main() {
	if err := setupPaths(); err != nil {
		fmt.Printf("[FATAL] Setup failed: %v\n", err)
		os.Exit(1)
	}
	serverIP = getOutboundIP()
	if serverIP == "" {
		serverIP = "0.0.0.0"
	}
	copyBuffer = make([]byte, CopyBufferSize)

	fmt.Println("╔════════════════════════════════════════╗")
	fmt.Println("║      GODSend Backend Server v5.2       ║")
	fmt.Println("║   (HTTP + FTP with DLC/XBLA Support)   ║")
	fmt.Println("╚════════════════════════════════════════╝")
	fmt.Printf("\n[INFO] Server IP: %s:%s\n", serverIP, Port)
	fmt.Printf("[INFO] Copy Buffer: %d MB | Serve Buffer: %d KB | FTP Buffer: %d MB\n",
		CopyBufferSize/1024/1024, ServeBufferSize/1024, FTPBufferSize/1024/1024)
	fmt.Printf("[INFO] TCP: NODELAY=on SNDBUF=%dKB KeepAlive=%s\n",
		TCPSendBuffer/1024, TCPKeepAlive)
	fmt.Printf("[INFO] File serving: sendfile() zero-copy via http.ServeContent\n")
	verifyTools()

	http.HandleFunc("/browse", recoverMiddleware(handleBrowse))
	http.HandleFunc("/trigger", recoverMiddleware(handleTrigger))
	http.HandleFunc("/status", recoverMiddleware(handleStatus))
	http.HandleFunc("/debug", recoverMiddleware(handleDebug))
	http.HandleFunc("/register", recoverMiddleware(handleRegister))
	http.HandleFunc("/files/", recoverMiddleware(handleFileServe))

	server := &http.Server{
		Addr: ":" + Port, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 0, WriteTimeout: 0, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20,
		ConnState: func(conn net.Conn, state http.ConnState) {
			if state == http.StateNew {
				if tc, ok := conn.(*net.TCPConn); ok {
					tc.SetNoDelay(true)          // TCP_NODELAY - disable Nagle's algorithm
					tc.SetKeepAlive(true)         // Enable keepalive
					tc.SetKeepAlivePeriod(TCPKeepAlive)
					tc.SetWriteBuffer(TCPSendBuffer) // SO_SNDBUF
					tc.SetReadBuffer(TCPSendBuffer)  // SO_RCVBUF
				}
			}
		},
	}
	logf("Starting server on port %s... Server started. Please start the script on the xbox", Port)
	if err := server.ListenAndServe(); err != nil {
		fmt.Printf("[FATAL] %v\n", err)
		os.Exit(1)
	}
}

func setupPaths() error {
	ex, err := os.Executable()
	if err != nil { return fmt.Errorf("executable path: %w", err) }
	toolsDir = filepath.Dir(ex)
	if runtime.GOOS == "windows" {
		sevenZipBin = "7za.exe"; isoGodBin = "iso2god.exe"
	} else {
		sevenZipBin = "7zz"; isoGodBin = "iso2god"
	}
	if err := os.MkdirAll(filepath.Join(toolsDir, "Ready"), 0755); err != nil { return err }
	if err := os.MkdirAll(filepath.Join(toolsDir, "Temp"), 0755); err != nil { return err }
	return nil
}

func verifyTools() {
	for _, t := range []struct{ n, p string }{
		{"7-Zip", filepath.Join(toolsDir, sevenZipBin)},
		{"iso2god", filepath.Join(toolsDir, isoGodBin)},
	} {
		if _, err := os.Stat(t.p); os.IsNotExist(err) {
			logf("WARNING: %s not found at %s", t.n, t.p)
		} else {
			logf("%s found: %s", t.n, t.p)
		}
	}
}

// ==========================================
// HTTP HANDLERS
// ==========================================

func handleBrowse(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	qType := r.URL.Query().Get("type")
	targetURL := Myrient360Base
	if qType == "dlc" { targetURL = MyrientDLCBase
	} else if platform == "xbox" { targetURL = MyrientOrigBase
	} else if platform == "digital" { targetURL = MyrientDigitalBase }
	logf("BROWSE: platform=%s", platform)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(targetURL)
	if err != nil {
		logf("BROWSE ERROR: %v", err)
		jsonError(w, 502, "Myrient unreachable - check server internet")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		jsonError(w, 502, fmt.Sprintf("Myrient returned HTTP %d", resp.StatusCode))
		return
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		jsonError(w, 502, "Failed to read Myrient response")
		return
	}
	var games []string
	for _, line := range strings.Split(string(body), "<a href=\"")[1:] {
		if end := strings.Index(line, "\""); end != -1 {
			raw := line[:end]
			if strings.HasSuffix(raw, ".zip") {
				if clean, err := url.QueryUnescape(raw); err == nil {
					games = append(games, strings.TrimSuffix(clean, ".zip"))
				}
			}
		}
	}
	logf("BROWSE: Found %d games", len(games))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(strings.Join(games, "|")))
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	gameName := r.URL.Query().Get("game")
	xboxIP := r.URL.Query().Get("ip")
	drive := r.URL.Query().Get("drive")
	platform := r.URL.Query().Get("platform")
	mode := r.URL.Query().Get("mode")
	if gameName == "" || xboxIP == "" {
		jsonError(w, 400, "Missing game or ip parameter"); return
	}
	if net.ParseIP(xboxIP) == nil {
		jsonError(w, 400, "Invalid IP address format"); return
	}
	if drive == "" { drive = "Hdd1:" }
	if mode == "" { mode = "http" }
	if platform == "" { platform = "xbox360" }

	xboxConnections.Store(gameName, XboxConnection{
		IP: xboxIP, Drive: drive, GameName: gameName, Platform: platform, Mode: mode, Timestamp: time.Now(),
	})
	logf("REGISTER: Xbox %s for %s (mode=%s drive=%s)", xboxIP, gameName, mode, drive)
	jsonSuccess(w, map[string]string{"status": "registered", "mode": mode, "ip": xboxIP, "drive": drive})
}

func handleTrigger(w http.ResponseWriter, r *http.Request) {
	gameName := r.URL.Query().Get("game")
	platform := r.URL.Query().Get("platform")
	if gameName == "" { jsonError(w, 400, "Missing game parameter"); return }
	if platform == "" { platform = "xbox360" }

	if status, exists := jobQueue.Load(gameName); exists {
		gs := status.(GameStatus)
		if gs.State == "Ready" { jsonSuccess(w, map[string]string{"status": "already_ready"}); return }
		if gs.State == "Processing" { jsonSuccess(w, map[string]string{"status": "already_processing"}); return }
	}

	launcher := func(fn func()) {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logf("PANIC processing %s: %v", gameName, r)
					buf := make([]byte, 4096)
					n := runtime.Stack(buf, false)
					logf("STACK: %s", string(buf[:n]))
					logStatus(gameName, "Error", "Server crashed during processing - check server logs")
				}
			}()
			fn()
		}()
	}

	if platform == "digital" {
		launcher(func() { processDigital(gameName) })
	} else {
		launcher(func() { processGame(gameName, platform) })
	}
	jsonSuccess(w, map[string]string{"status": "triggered"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	gameName := r.URL.Query().Get("game")
	if gameName == "" { jsonError(w, 400, "Missing game parameter"); return }
	status := GameStatus{State: "Missing", Message: "Not Found"}
	if s, exists := jobQueue.Load(gameName); exists { status = s.(GameStatus) }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func handleDebug(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h2>GODSend Debug v5.2</h2><p>Server: %s:%s</p>", serverIP, Port)
	fmt.Fprintf(w, "<h3>Ready Games:</h3><ul>")
	if files, err := os.ReadDir(filepath.Join(toolsDir, "Ready")); err == nil {
		for _, f := range files { if f.IsDir() { fmt.Fprintf(w, "<li>%s</li>", f.Name()) } }
	}
	fmt.Fprintf(w, "</ul><h3>Active Jobs:</h3><ul>")
	jobQueue.Range(func(k, v interface{}) bool {
		gs := v.(GameStatus); fmt.Fprintf(w, "<li>%s: [%s] %s</li>", k, gs.State, gs.Message); return true
	})
	fmt.Fprintf(w, "</ul><h3>Xbox Connections:</h3><ul>")
	xboxConnections.Range(func(k, v interface{}) bool {
		c := v.(XboxConnection)
		fmt.Fprintf(w, "<li>%s: IP=%s Mode=%s Drive=%s (%s ago)</li>",
			c.GameName, c.IP, c.Mode, c.Drive, time.Since(c.Timestamp).Round(time.Second))
		return true
	})
	fmt.Fprintf(w, "</ul>")
}

// ==========================================
// FILE SERVING (FIXED RANGE HANDLING)
// ==========================================

func handleFileServe(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/files/")
	if relPath == "" { jsonError(w, 404, "No file path specified"); return }

	decodedPath, err := url.QueryUnescape(relPath)
	if err != nil {
		logf("FILE ERROR: Bad encoding: %s", relPath)
		jsonError(w, 400, "Invalid file path encoding"); return
	}
	fullPath := filepath.Join(toolsDir, "Ready", decodedPath)

	absReady, _ := filepath.Abs(filepath.Join(toolsDir, "Ready"))
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absReady) {
		logf("FILE BLOCKED: Path traversal: %s", decodedPath)
		jsonError(w, 403, "Access denied"); return
	}

	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		logf("FILE 404: %s", decodedPath)
		jsonError(w, 404, fmt.Sprintf("File not found: %s", filepath.Base(decodedPath))); return
	}
	if err != nil {
		jsonError(w, 500, "Cannot access file"); return
	}

	if info.IsDir() {
		entries, err := os.ReadDir(fullPath)
		if err != nil { jsonError(w, 500, "Cannot list directory"); return }
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><body><h2>Index of /%s</h2><ul>", relPath)
		for _, e := range entries {
			name := e.Name(); if e.IsDir() { name += "/" }
			fmt.Fprintf(w, "<li><a href=\"%s\">%s</a></li>", url.PathEscape(name), name)
		}
		fmt.Fprintf(w, "</ul></body></html>"); return
	}

	file, err := os.Open(fullPath)
	if err != nil { jsonError(w, 500, "Cannot open file"); return }
	defer file.Close()

	fileSize := info.Size()
	fileName := filepath.Base(fullPath)

	// Hint the OS to read sequentially (Linux only, ignored on Windows)
	adviseFadvise(file, fileSize)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Range request handling
	if rh := r.Header.Get("Range"); rh != "" {
		start, end, err := parseRangeHeader(rh, fileSize)
		if err != nil {
			logf("FILE WARN: Bad range '%s' for %s: %v", rh, fileName, err)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable); return
		}
		cl := end - start + 1
		if _, err := file.Seek(start, 0); err != nil {
			logf("FILE ERROR: Seek %s: %v", fileName, err)
			jsonError(w, 500, "File seek error"); return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
		w.Header().Set("Content-Length", strconv.FormatInt(cl, 10))
		w.WriteHeader(http.StatusPartialContent)

		// Buffered range transfer with speed tracking
		startTime := time.Now()
		bw := bufio.NewWriterSize(w, ServeBufferSize)
		written, err := io.CopyN(bw, file, cl)
		if flushErr := bw.Flush(); flushErr != nil && err == nil { err = flushErr }
		elapsed := time.Since(startTime).Seconds()
		if elapsed < 0.001 { elapsed = 0.001 }
		speed := float64(written) / elapsed / 1048576

		if err != nil {
			logf("FILE WARN: Range xfer interrupted %s after %.2f MB (%.1f MB/s): %v", fileName, float64(written)/1048576, speed, err)
		} else {
			logf("FILE: Range %s %.2f MB @ %.1f MB/s", fileName, float64(written)/1048576, speed)
		}
		return
	}

	// Full file transfer - use http.ServeContent for sendfile() zero-copy on Linux
	logf("FILE: Sending %s (%.2f MB)", fileName, float64(fileSize)/1048576)
	startTime := time.Now()

	// ServeContent handles Content-Length, Last-Modified, and uses sendfile() syscall
	// on Linux for zero-copy kernel→network transfer (bypasses userspace entirely)
	http.ServeContent(w, r, fileName, info.ModTime(), file)

	elapsed := time.Since(startTime).Seconds()
	if elapsed < 0.001 { elapsed = 0.001 }
	speed := float64(fileSize) / elapsed / 1048576
	logf("FILE: Done %s (%.2f MB) in %.1fs @ %.1f MB/s", fileName, float64(fileSize)/1048576, elapsed, speed)
}

func parseRangeHeader(header string, fileSize int64) (int64, int64, error) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, fmt.Errorf("not a byte range: %s", header)
	}
	spec := strings.TrimPrefix(header, "bytes=")
	// Suffix: "bytes=-500"
	if strings.HasPrefix(spec, "-") {
		s, err := strconv.ParseInt(spec[1:], 10, 64)
		if err != nil || s <= 0 { return 0, 0, fmt.Errorf("bad suffix: %s", spec) }
		start := fileSize - s
		if start < 0 { start = 0 }
		return start, fileSize - 1, nil
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 { return 0, 0, fmt.Errorf("bad format: %s", spec) }
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil { return 0, 0, fmt.Errorf("bad start: %s", parts[0]) }
	var end int64
	if parts[1] == "" {
		end = fileSize - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil { return 0, 0, fmt.Errorf("bad end: %s", parts[1]) }
	}
	if start < 0 || start >= fileSize { return 0, 0, fmt.Errorf("start %d out of range (size %d)", start, fileSize) }
	if end < start { return 0, 0, fmt.Errorf("end %d < start %d", end, start) }
	if end >= fileSize { end = fileSize - 1 }
	return start, end, nil
}

// adviseFadvise is a placeholder for POSIX_FADV_SEQUENTIAL + POSIX_FADV_WILLNEED
// On Linux, the kernel already does adaptive read-ahead for sequential reads.
// The real perf wins come from http.ServeContent (sendfile syscall) and TCP tuning.
// If you need this on Linux-only builds, use golang.org/x/sys/unix.Fadvise()
func adviseFadvise(f *os.File, size int64) {
	// no-op: avoids syscall import that breaks Windows cross-compilation
}

// ==========================================
// FTP HELPERS
// ==========================================

func connectToXboxFTP(ip string) (*ftp.ServerConn, error) {
	logf("FTP: Connecting to %s:%d...", ip, FTPPort)
	c, err := ftp.Dial(fmt.Sprintf("%s:%d", ip, FTPPort),
		ftp.DialWithTimeout(FTPTimeout), ftp.DialWithDisabledEPSV(true), ftp.DialWithDisabledUTF8(true))
	if err != nil { return nil, fmt.Errorf("FTP connect to %s failed - ensure Aurora FTP enabled: %v", ip, err) }
	if err = c.Login("xboxftp", "xboxftp"); err != nil {
		c.Quit(); return nil, fmt.Errorf("FTP login failed - check credentials: %v", err)
	}
	logf("FTP: Connected to %s", ip)
	return c, nil
}

func connectWithRetry(ip string) (*ftp.ServerConn, error) {
	var last error
	for i := 1; i <= FTPMaxRetries; i++ {
		c, err := connectToXboxFTP(ip)
		if err == nil { return c, nil }
		last = err
		if i < FTPMaxRetries { logf("FTP: Attempt %d/%d failed, retry...", i, FTPMaxRetries); time.Sleep(FTPRetryDelay) }
	}
	return nil, fmt.Errorf("FTP failed after %d attempts: %v", FTPMaxRetries, last)
}

func ftpMkdirAll(conn *ftp.ServerConn, path string) error {
	cur := ""
	for _, p := range strings.Split(strings.Trim(path, "/"), "/") {
		cur += "/" + p; conn.MakeDir(cur)
	}
	return nil
}

func ftpUploadFile(conn *ftp.ServerConn, localPath, remotePath, gameName string, transferred *int64, totalSize int64) error {
	f, err := os.Open(localPath)
	if err != nil { return fmt.Errorf("open %s: %v", filepath.Base(localPath), err) }
	defer f.Close()
	info, err := f.Stat()
	if err != nil { return fmt.Errorf("stat %s: %v", filepath.Base(localPath), err) }
	r := &ftpProgressReader{reader: f, total: info.Size(), gameName: gameName,
		fileName: filepath.Base(localPath), transferred: transferred, totalSize: totalSize}
	if err = conn.Stor(remotePath, r); err != nil {
		return fmt.Errorf("STOR %s: %v", filepath.Base(localPath), err)
	}
	*transferred += info.Size()
	return nil
}

func ftpUploadWithRetry(conn *ftp.ServerConn, xboxIP, localPath, remotePath, gameName string, transferred *int64, totalSize int64) error {
	if err := ftpUploadFile(conn, localPath, remotePath, gameName, transferred, totalSize); err == nil {
		return nil
	}
	logf("FTP: Upload failed, reconnecting for %s", filepath.Base(localPath))
	nc, err := connectToXboxFTP(xboxIP)
	if err != nil { return fmt.Errorf("reconnect failed: %v", err) }
	defer nc.Quit()
	return ftpUploadFile(nc, localPath, remotePath, gameName, transferred, totalSize)
}

type ftpProgressReader struct {
	reader      io.Reader
	total, written int64
	gameName, fileName string
	lastLog     time.Time
	transferred *int64
	totalSize   int64
}

func (r *ftpProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.written += int64(n)
	if time.Since(r.lastLog) > 500*time.Millisecond {
		fp := float64(r.written) / float64(r.total) * 100
		tp := float64(*r.transferred+r.written) / float64(r.totalSize) * 100
		fmt.Printf("\r[FTP] %s: %.1f%% | Overall: %.1f%%   ", r.fileName, fp, tp)
		r.lastLog = time.Now()
	}
	return n, err
}

// ==========================================
// GAME PROCESSING
// ==========================================

func processGame(gameName, platform string) {
	logf("=== Processing: %s (platform=%s) ===", gameName, platform)
	safeName := sanitizeFilename(gameName)
	if safeName == "" { logStatus(gameName, "Error", "Invalid game name"); return }

	var xboxConn *XboxConnection
	if c, ok := xboxConnections.Load(gameName); ok {
		cc := c.(XboxConnection); xboxConn = &cc
		logf("Mode: %s to %s (drive: %s)", xboxConn.Mode, xboxConn.IP, xboxConn.Drive)
	}

	gameDir := filepath.Join(toolsDir, "Ready", safeName)
	if err := os.MkdirAll(gameDir, 0755); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Cannot create directory: %v", err)); return
	}

	baseURL := Myrient360Base
	if platform == "xbox" { baseURL = MyrientOrigBase }

	logStatus(gameName, "Processing", "Searching...")
	zipURL, err := findZip(baseURL+"?search="+url.QueryEscape(gameName), gameName, baseURL)
	if err != nil { logStatus(gameName, "Error", fmt.Sprintf("Not found on Myrient: %v", err)); return }

	zipPath := filepath.Join(toolsDir, "Temp", safeName+".zip")
	if cached, _ := checkZipCache(zipURL, zipPath, baseURL); cached {
		logStatus(gameName, "Processing", "Using cached download")
	} else {
		logStatus(gameName, "Processing", "Downloading from Myrient...")
		if err := downloadWithProgress(zipURL, zipPath, gameName, baseURL); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("Download failed: %v", err)); return
		}
		if info, err := os.Stat(zipPath); err != nil || info.Size() < 1000 {
			logStatus(gameName, "Error", "Download incomplete or corrupt"); os.Remove(zipPath); return
		}
	}

	logStatus(gameName, "Processing", "Extracting ISO...")
	isoPath, err := extractISO(zipPath, safeName)
	if err != nil { logStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err)); return }

	logStatus(gameName, "Processing", "Converting to GOD...")
	godDir := filepath.Join(toolsDir, "Temp", safeName+"_GOD")
	os.MkdirAll(godDir, 0755)
	if err := runIso2God(isoPath, godDir); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("GOD convert: %v", err)); os.Remove(isoPath); os.RemoveAll(godDir); return
	}
	os.Remove(isoPath)

	titleID, mediaID, err := detectGodStructure(godDir)
	if err != nil { logStatus(gameName, "Error", fmt.Sprintf("GOD detect: %v", err)); os.RemoveAll(godDir); return }
	logf("Detected: TitleID=%s MediaID=%s", titleID, mediaID)

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		logStatus(gameName, "Processing", "FTP Transfer starting...")
		if err := ftpTransferGame(godDir, xboxConn, gameName, titleID, mediaID); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("FTP: %v", err)); os.RemoveAll(godDir); return
		}
		if platform == "xbox360" {
			logStatus(gameName, "Processing", "Checking for DLC...")
			ftpProcessDLCs(gameName, titleID, xboxConn)
		}
		os.RemoveAll(godDir)
		logStatus(gameName, "Ready", "FTP Transfer Complete!")
	} else {
		logStatus(gameName, "Processing", "Archiving...")
		titleID, mediaID, err = bucketAndZip(godDir, gameDir, gameName, safeName)
		if err != nil { logStatus(gameName, "Error", fmt.Sprintf("Archive: %v", err)); os.RemoveAll(godDir); return }
		os.RemoveAll(godDir)
		var dlcList []string
		if platform == "xbox360" {
			logStatus(gameName, "Processing", "Checking for DLC...")
			dlcList = processDLCs(gameName, gameDir)
			if len(dlcList) > 0 { logStatus(gameName, "Processing", fmt.Sprintf("Paired %d DLCs", len(dlcList))) }
		}
		updateGameINI_Parts(gameDir, gameName, titleID, mediaID, dlcList)
		logStatus(gameName, "Ready", "Ready to Install")
	}
	logf("=== Complete: %s ===", gameName)
}

// ==========================================
// FTP TRANSFER
// ==========================================

func ftpTransferGame(godDir string, conn *XboxConnection, gameName, titleID, mediaID string) error {
	fc, err := connectWithRetry(conn.IP)
	if err != nil { return err }
	defer fc.Quit()

	drive := strings.TrimSuffix(conn.Drive, ":")
	base := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", drive, titleID, mediaID)
	logf("FTP Dest: %s", base)
	ftpMkdirAll(fc, base)

	contentDir := filepath.Join(godDir, titleID, mediaID)
	if _, err := os.Stat(contentDir); os.IsNotExist(err) {
		return fmt.Errorf("GOD content not found: %s", contentDir)
	}

	var totalFiles int; var totalSize int64
	filepath.Walk(contentDir, func(p string, i os.FileInfo, e error) error {
		if e == nil && !i.IsDir() { totalFiles++; totalSize += i.Size() }; return nil
	})
	if totalFiles == 0 { return fmt.Errorf("no files in GOD content") }
	logf("FTP: %d files (%.2f GB)", totalFiles, float64(totalSize)/1073741824)

	var xferred int; var xferSize int64
	return filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil { return nil }
		rel, _ := filepath.Rel(contentDir, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		remote := base + "/" + rel
		if info.IsDir() { fc.MakeDir(remote); return nil }
		xferred++
		pct := float64(xferSize) / float64(totalSize) * 100
		logStatus(gameName, "Processing", fmt.Sprintf("FTP: %d/%d files (%.1f%%)", xferred, totalFiles, pct))
		if err := ftpUploadWithRetry(fc, conn.IP, path, remote, gameName, &xferSize, totalSize); err != nil {
			logf("FTP WARN: %s: %v", rel, err)
		}
		return nil
	})
}

func ftpTransferSingleFile(localPath string, conn *XboxConnection, titleID, typeDir, fileName string) error {
	fc, err := connectWithRetry(conn.IP)
	if err != nil { return err }
	defer fc.Quit()
	drive := strings.TrimSuffix(conn.Drive, ":")
	base := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", drive, titleID, typeDir)
	ftpMkdirAll(fc, base)
	info, err := os.Stat(localPath)
	if err != nil { return err }
	var xfer int64
	if err := ftpUploadFile(fc, localPath, base+"/"+fileName, fileName, &xfer, info.Size()); err != nil {
		return err
	}
	logf("FTP DLC done: %.2f MB", float64(info.Size())/1048576)
	return nil
}

func ftpProcessDLCs(gameName, titleID string, conn *XboxConnection) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", MyrientDLCBase+"?search="+url.QueryEscape(gameName), nil)
	if err != nil { return }
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil { logf("FTP DLC: search failed: %v", err); return }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	re := regexp.MustCompile(`href="([^"]+\.zip)"`)
	matches := re.FindAllStringSubmatch(string(body), -1)
	gnl := strings.ToLower(gameName)
	count := 0

	for _, m := range matches {
		link := m[1]
		dec, _ := url.QueryUnescape(link)
		low := strings.ToLower(dec)
		if !strings.Contains(low, gnl) || !strings.Contains(low, "dlc") { continue }
		dl := link; if !strings.HasPrefix(link, "http") { dl = MyrientDLCBase + link }
		count++
		logStatus(gameName, "Processing", fmt.Sprintf("Downloading DLC %d...", count))
		zp := filepath.Join(toolsDir, "Temp", "dlc_temp.zip")
		if err := downloadWithProgress(dl, zp, gameName+" DLC", MyrientDLCBase); err != nil {
			logf("FTP DLC: dl failed %d: %v", count, err); continue
		}
		ed := filepath.Join(toolsDir, "Temp", "dlc_ext"); os.RemoveAll(ed)
		cmd := exec.Command(filepath.Join(toolsDir, sevenZipBin), "x", zp, "-o"+ed, "-y")
		if out, err := cmd.CombinedOutput(); err != nil {
			logf("FTP DLC: extract %d: %v | %s", count, err, string(out)); os.Remove(zp); continue
		}
		filepath.Walk(ed, func(p string, i os.FileInfo, e error) error {
			if e != nil || i.IsDir() || i.Size() <= 1024*1024 { return nil }
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".txt" || ext == ".nfo" || ext == ".jpg" { return nil }
			if i.Size() > MaxDLCSizeBytes { logf("FTP DLC: skip oversized %s", filepath.Base(p)); return nil }
			tid, ct := parseXboxHeader(p)
			if tid == "" { tid = titleID }
			td := fmt.Sprintf("%08X", ct); if ct == 0 { td = "00000002" }
			logStatus(gameName, "Processing", fmt.Sprintf("FTP: DLC %d transfer...", count))
			if err := ftpTransferSingleFile(p, conn, tid, td, filepath.Base(p)); err != nil {
				logf("FTP DLC: xfer %s: %v", filepath.Base(p), err)
			}
			return nil
		})
		os.Remove(zp); os.RemoveAll(ed)
	}
	if count > 0 { logf("FTP: %d DLC(s) processed", count) }
}

// ==========================================
// DIGITAL PROCESSING
// ==========================================

func processDigital(gameName string) {
	logf("=== Processing Digital: %s ===", gameName)
	safeName := sanitizeFilename(gameName)
	if safeName == "" { logStatus(gameName, "Error", "Invalid game name"); return }

	var xboxConn *XboxConnection
	if c, ok := xboxConnections.Load(gameName); ok { cc := c.(XboxConnection); xboxConn = &cc }

	gameDir := filepath.Join(toolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	logStatus(gameName, "Processing", "Searching Digital Repo...")
	zipURL, err := findZip(MyrientDigitalBase+"?search="+url.QueryEscape(gameName), gameName, MyrientDigitalBase)
	if err != nil { logStatus(gameName, "Error", fmt.Sprintf("Not found: %v", err)); return }

	zipPath := filepath.Join(toolsDir, "Temp", safeName+"_digi.zip")
	if err := downloadWithProgress(zipURL, zipPath, gameName, MyrientDigitalBase); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Download: %v", err)); return
	}
	if info, err := os.Stat(zipPath); err != nil || info.Size() < 1000 {
		logStatus(gameName, "Error", "Download incomplete"); os.Remove(zipPath); return
	}

	logStatus(gameName, "Processing", "Extracting...")
	extDir := filepath.Join(toolsDir, "Temp", safeName+"_ext"); os.RemoveAll(extDir)
	cmd := exec.Command(filepath.Join(toolsDir, sevenZipBin), "x", zipPath, "-o"+extDir, "-y")
	if out, err := cmd.CombinedOutput(); err != nil {
		logf("ERROR 7z: %s", string(out)); logStatus(gameName, "Error", "Extraction failed"); os.Remove(zipPath); return
	}

	var contentFile, titleID, typeDir string
	filepath.Walk(extDir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() || i.Size() <= 1024*1024 { return nil }
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".txt" || ext == ".nfo" || ext == ".jpg" { return nil }
		tid, ct := parseXboxHeader(p)
		if tid != "" { contentFile = p; titleID = tid; typeDir = fmt.Sprintf("%08X", ct); return io.EOF }
		return nil
	})
	if contentFile == "" { logStatus(gameName, "Error", "No valid Xbox content found"); os.Remove(zipPath); os.RemoveAll(extDir); return }
	logf("Detected: TitleID=%s Type=%s", titleID, typeDir)
	finalName := filepath.Base(contentFile)

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		logStatus(gameName, "Processing", "FTP Transfer...")
		if err := ftpTransferDigital(contentFile, xboxConn, gameName, titleID, typeDir, finalName); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("FTP: %v", err))
		} else {
			logStatus(gameName, "Ready", "FTP Transfer Complete!")
		}
	} else {
		if err := copyFileBuffered(contentFile, filepath.Join(gameDir, finalName)); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("Copy: %v", err))
		} else {
			updateGameINI_Raw(gameDir, gameName, finalName, fmt.Sprintf("Content\\0000000000000000\\%s\\%s\\", titleID, typeDir))
			logStatus(gameName, "Ready", "Ready to Install")
		}
	}
	os.Remove(zipPath); os.RemoveAll(extDir)
	logf("=== Complete: %s ===", gameName)
}

func ftpTransferDigital(contentFile string, conn *XboxConnection, gameName, titleID, typeDir, fileName string) error {
	fc, err := connectWithRetry(conn.IP)
	if err != nil { return err }
	defer fc.Quit()
	drive := strings.TrimSuffix(conn.Drive, ":")
	base := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", drive, titleID, typeDir)
	ftpMkdirAll(fc, base)
	info, err := os.Stat(contentFile)
	if err != nil { return err }
	var xfer int64
	return ftpUploadFile(fc, contentFile, base+"/"+fileName, gameName, &xfer, info.Size())
}

// ==========================================
// DLC PROCESSING (HTTP)
// ==========================================

func processDLCs(gameName, gameDir string) []string {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", MyrientDLCBase+"?search="+url.QueryEscape(gameName), nil)
	if err != nil { return nil }
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil { logf("DLC: search failed: %v", err); return nil }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	re := regexp.MustCompile(`href="([^"]+\.zip)"`)
	matches := re.FindAllStringSubmatch(string(body), -1)
	var result []string
	gnl := strings.ToLower(gameName)

	for _, m := range matches {
		link := m[1]
		dec, _ := url.QueryUnescape(link)
		low := strings.ToLower(dec)
		if !strings.Contains(low, gnl) || !strings.Contains(low, "dlc") { continue }
		dl := link; if !strings.HasPrefix(link, "http") { dl = MyrientDLCBase + link }
		zp := filepath.Join(toolsDir, "Temp", "dlc_temp.zip")
		if err := downloadWithProgress(dl, zp, gameName+" DLC", MyrientDLCBase); err != nil { continue }
		ed := filepath.Join(toolsDir, "Temp", "dlc_ext"); os.RemoveAll(ed)
		cmd := exec.Command(filepath.Join(toolsDir, sevenZipBin), "x", zp, "-o"+ed, "-y")
		if out, err := cmd.CombinedOutput(); err != nil {
			logf("DLC: 7z: %v | %s", err, string(out)); os.Remove(zp); continue
		}
		var dlcFile string
		filepath.Walk(ed, func(p string, i os.FileInfo, e error) error {
			if e != nil || i.IsDir() || i.Size() <= 1024*1024 { return nil }
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".txt" || ext == ".nfo" || ext == ".jpg" || i.Size() > MaxDLCSizeBytes { return nil }
			dlcFile = p; return nil
		})
		if dlcFile != "" {
			zn := filepath.Base(dlcFile) + ".7z"
			dp := filepath.Join(gameDir, zn)
			stage := filepath.Join(toolsDir, "Temp", "dlc_stage"); os.MkdirAll(stage, 0755)
			if err := copyFileBuffered(dlcFile, filepath.Join(stage, filepath.Base(dlcFile))); err == nil {
				if err := createZipFromDir(stage, dp); err == nil { result = append(result, zn) }
			}
			os.RemoveAll(stage)
		}
		os.Remove(zp); os.RemoveAll(ed)
	}
	return result
}

// ==========================================
// INI MANAGEMENT
// ==========================================

func updateGameINI_Parts(gameDir, gameName, titleID, mediaID string, dlcList []string) {
	f, err := os.Create(filepath.Join(gameDir, "godsend.ini"))
	if err != nil { logf("INI ERROR: %v", err); return }
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := func(s string) string {
		s = strings.ReplaceAll(s, " ", "%20"); s = strings.ReplaceAll(s, "(", "%28"); s = strings.ReplaceAll(s, ")", "%29"); return s
	}
	raw, ok := gamePartsMap.Load(gameName)
	if !ok { logf("INI ERROR: no parts for %s", gameName); return }
	parts := raw.([]string)
	fmt.Fprintf(w, "[%s]\ntype=god\ntitleid=%s\nmediaid=%s\n", gameName, titleID, mediaID)
	if len(parts) > 0 { fmt.Fprintf(w, "dataurl=%s\n", enc(parts[0])) }
	for i := 1; i < len(parts); i++ { fmt.Fprintf(w, "dataurlpart%d=%s\n", i+1, enc(parts[i])) }
	for i, d := range dlcList { fmt.Fprintf(w, "dlc_%d=%s\n", i+1, enc(d)) }
	w.Flush()
}

func updateGameINI_Raw(gameDir, gameName, fileName, relPath string) {
	f, err := os.Create(filepath.Join(gameDir, "godsend.ini"))
	if err != nil { logf("INI ERROR: %v", err); return }
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "[%s]\ntype=raw\nfilename=%s\npath=%s\n", gameName, fileName, relPath)
	w.Flush()
}

// ==========================================
// HELPERS
// ==========================================

func bucketAndZip(src, dest, gameName, safeName string) (string, string, error) {
	titleID, mediaID, err := detectGodStructure(src)
	if err != nil { return "", "", err }
	staging := filepath.Join(toolsDir, "Temp", safeName+"_staging")
	os.RemoveAll(staging); os.MkdirAll(staging, 0755)
	var parts []string; var curSize int64; pn := 1
	cpd := filepath.Join(staging, fmt.Sprintf("%s_Part%d", safeName, pn)); os.MkdirAll(cpd, 0755)
	contentDir := filepath.Join(src, titleID, mediaID)
	err = filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() { return nil }
		rel, _ := filepath.Rel(contentDir, path)
		if curSize+info.Size() > MaxPartSize && curSize > 0 {
			pname := fmt.Sprintf("%s_Part%d.7z", safeName, pn)
			if err := createZipFromDir(cpd, filepath.Join(dest, pname)); err != nil { return err }
			parts = append(parts, pname); pn++; curSize = 0
			cpd = filepath.Join(staging, fmt.Sprintf("%s_Part%d", safeName, pn)); os.MkdirAll(cpd, 0755)
		}
		dp := filepath.Join(cpd, rel); os.MkdirAll(filepath.Dir(dp), 0755)
		if err := copyFileBuffered(path, dp); err != nil { return err }
		curSize += info.Size(); return nil
	})
	if err != nil { os.RemoveAll(staging); return "", "", err }
	if curSize > 0 {
		pname := fmt.Sprintf("%s_Part%d.7z", safeName, pn)
		if err := createZipFromDir(cpd, filepath.Join(dest, pname)); err != nil { os.RemoveAll(staging); return "", "", err }
		parts = append(parts, pname)
	}
	os.RemoveAll(staging); gamePartsMap.Store(gameName, parts)
	return titleID, mediaID, nil
}

func detectGodStructure(godDir string) (string, string, error) {
	entries, err := os.ReadDir(godDir)
	if err != nil { return "", "", err }
	for _, e := range entries {
		if !e.IsDir() { continue }
		subs, err := os.ReadDir(filepath.Join(godDir, e.Name()))
		if err != nil { continue }
		for _, s := range subs { if s.IsDir() { return e.Name(), s.Name(), nil } }
	}
	return "", "", fmt.Errorf("GOD structure not found")
}

func parseXboxHeader(path string) (string, uint32) {
	f, err := os.Open(path)
	if err != nil { return "", 0 }
	defer f.Close()
	h := make([]byte, 1024)
	n, err := f.Read(h)
	if err != nil || n < 0x368 { return "", 0 }
	magic := string(h[0:4])
	if magic != "LIVE" && magic != "PIRS" && magic != "CON " { return "", 0 }
	return strings.ToUpper(hex.EncodeToString(h[0x360:0x364])), binary.BigEndian.Uint32(h[0x344:0x348])
}

func findZip(searchURL, gameName, baseURL string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil { return "", err }
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil { return "", fmt.Errorf("search failed: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode != 200 { return "", fmt.Errorf("HTTP %d", resp.StatusCode) }
	body, _ := io.ReadAll(resp.Body)
	for _, m := range regexp.MustCompile(`href="([^"]+\.zip)"`).FindAllStringSubmatch(string(body), -1) {
		dec, _ := url.QueryUnescape(m[1])
		if strings.Contains(strings.ToLower(dec), strings.ToLower(gameName)) {
			if strings.HasPrefix(m[1], "http") { return m[1], nil }
			return baseURL + m[1], nil
		}
	}
	return "", fmt.Errorf("no match for '%s'", gameName)
}

func checkZipCache(urlStr, localPath, referrer string) (bool, error) {
	info, err := os.Stat(localPath)
	if err != nil { return false, nil }
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("HEAD", urlStr, nil)
	req.Header.Set("Referer", referrer)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 { return false, err }
	return info.Size() == resp.ContentLength && resp.ContentLength > 1000, nil
}

func downloadWithProgress(urlStr, dest, name, ref string) error {
	client := &http.Client{Timeout: 0}
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil { return err }
	req.Header.Set("Referer", ref); req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil { return fmt.Errorf("request failed: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode != 200 { return fmt.Errorf("HTTP %d", resp.StatusCode) }
	out, err := os.Create(dest)
	if err != nil { return fmt.Errorf("create file: %w", err) }
	defer out.Close()
	bw := bufio.NewWriterSize(out, CopyBufferSize)
	pw := &ProgressWriter{Total: resp.ContentLength, GameName: name, LastLog: time.Now(), StartTime: time.Now()}
	written, err := io.Copy(bw, io.TeeReader(resp.Body, pw))
	if err != nil { return fmt.Errorf("interrupted after %.2f MB: %w", float64(written)/1048576, err) }
	bw.Flush(); fmt.Println()
	if resp.ContentLength > 0 && written != resp.ContentLength {
		logf("WARN: Size mismatch %s: expected %d got %d", name, resp.ContentLength, written)
	}
	return nil
}

func extractISO(zipPath, safeName string) (string, error) {
	dest := filepath.Join(toolsDir, "Temp", safeName+"_extracted"); os.RemoveAll(dest)
	out, err := exec.Command(filepath.Join(toolsDir, sevenZipBin), "x", zipPath, "-o"+dest, "*.iso", "-r", "-y").CombinedOutput()
	if err != nil { return "", fmt.Errorf("7z: %v | %s", err, string(out)) }
	var iso string
	filepath.Walk(dest, func(p string, i os.FileInfo, e error) error {
		if e == nil && strings.HasSuffix(strings.ToLower(p), ".iso") { iso = p }; return nil
	})
	if iso == "" { return "", fmt.Errorf("no ISO in archive") }
	return iso, nil
}

func runIso2God(iso, out string) error {
	o, err := exec.Command(filepath.Join(toolsDir, isoGodBin), iso, out).CombinedOutput()
	if err != nil { return fmt.Errorf("iso2god: %v | %s", err, string(o)) }
	return nil
}

func createZipFromDir(dir, out string) error {
	cmd := exec.Command(filepath.Join(toolsDir, sevenZipBin), "a", "-t7z", "-mx0", out, "*")
	cmd.Dir = dir
	o, err := cmd.CombinedOutput()
	if err != nil { return fmt.Errorf("7z: %v | %s", err, string(o)) }
	return nil
}

func copyFileBuffered(src, dst string) error {
	in, err := os.Open(src)
	if err != nil { return err }
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil { return err }
	defer out.Close()
	bw := bufio.NewWriterSize(out, CopyBufferSize)
	if _, err = io.Copy(bw, bufio.NewReaderSize(in, CopyBufferSize)); err != nil { return err }
	return bw.Flush()
}

func getOutboundIP() string {
	c, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil { return "" }
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok { return a.IP.String() }
	return ""
}

func sanitizeFilename(n string) string {
	if n == "" { return "" }
	return regexp.MustCompile(`[<>:"/\\|?*]`).ReplaceAllString(n, " -")
}
