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
	MaxPartSize     = 1800000000        // 1.8GB Safe limit (FAT32 max is 2GB)
	MaxDLCSizeBytes = 349 * 1024 * 1024 // 349MB Limit

	// Buffer sizes for optimized I/O
	CopyBufferSize  = 4 * 1024 * 1024 // 4MB buffer for file copies
	ServeBufferSize = 256 * 1024      // 256KB buffer for HTTP serving

	// FTP Configuration
	FTPPort       = 21
	FTPTimeout    = 30 * time.Second
	FTPBufferSize = 1 * 1024 * 1024 // 1MB buffer for FTP transfers

	Myrient360Base     = "https://myrient.erista.me/files/Redump/Microsoft%20-%20Xbox%20360/"
	MyrientOrigBase    = "https://myrient.erista.me/files/Redump/Microsoft%20-%20Xbox/"
	MyrientDigitalBase = "https://myrient.erista.me/files/No-Intro/Microsoft%20-%20Xbox%20360%20(Digital)/"
	MyrientDLCBase     = "https://myrient.erista.me/files/No-Intro/Microsoft%20-%20Xbox%20360%20(Digital)/"
)

var (
	toolsDir     string
	sevenZipBin  string
	isoGodBin    string
	jobQueue     sync.Map
	serverIP     string
	gamePartsMap sync.Map
	copyBuffer   []byte

	// Track Xbox IPs for FTP connections
	xboxConnections sync.Map
)

// XboxConnection stores info about a connected Xbox for FTP transfers
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
		if elapsed < 1 {
			elapsed = 1
		}
		speed := float64(pw.Written) / elapsed / 1048576

		fmt.Printf("\r[%s] Download: %.1f%% (%.1f/%.1f MB) @ %.1f MB/s   ",
			time.Now().Format("15:04:05"), percent, float64(pw.Written)/1048576, float64(pw.Total)/1048576, speed)

		logStatus(pw.GameName, "Processing", fmt.Sprintf("Downloading: %.0f%%", percent))
		pw.LastLog = time.Now()
	}
	return n, nil
}

// ==========================================
// MAIN & SETUP
// ==========================================
func main() {
	setupPaths()
	serverIP = getOutboundIP()

	copyBuffer = make([]byte, CopyBufferSize)

	fmt.Println("╔════════════════════════════════════════╗")
	fmt.Println("║      GODSend Backend Server v5.1       ║")
	fmt.Println("║   (HTTP + FTP with DLC/XBLA Support)   ║")
	fmt.Println("╚════════════════════════════════════════╝")
	fmt.Printf("\n[INFO] Server IP: %s:%s\n", serverIP, Port)
	fmt.Printf("[INFO] Copy Buffer: %d MB\n", CopyBufferSize/1024/1024)
	fmt.Printf("[INFO] Serve Buffer: %d KB\n", ServeBufferSize/1024)
	fmt.Printf("[INFO] FTP Transfer Buffer: %d MB\n", FTPBufferSize/1024/1024)

	http.HandleFunc("/browse", handleBrowse)
	http.HandleFunc("/trigger", handleTrigger)
	http.HandleFunc("/status", handleStatus)
	http.HandleFunc("/debug", handleDebug)
	http.HandleFunc("/register", handleRegister)
	http.HandleFunc("/files/", handleFileServe)

	server := &http.Server{
		Addr:              ":" + Port,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	fmt.Printf("[SERVER] Starting on port %s...\n", Port)
	if err := server.ListenAndServe(); err != nil {
		fmt.Printf("ERROR: %v\n", err)
	}
}

func setupPaths() {
	ex, _ := os.Executable()
	toolsDir = filepath.Dir(ex)

	if runtime.GOOS == "windows" {
		sevenZipBin = "7za.exe"
		isoGodBin = "iso2god.exe"
	} else {
		sevenZipBin = "7zz"
		isoGodBin = "iso2god"
	}

	os.MkdirAll(filepath.Join(toolsDir, "Ready"), 0755)
	os.MkdirAll(filepath.Join(toolsDir, "Temp"), 0755)
}

// ==========================================
// HTTP HANDLERS
// ==========================================

func handleBrowse(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	qType := r.URL.Query().Get("type")
	targetURL := Myrient360Base

	if qType == "dlc" {
		targetURL = MyrientDLCBase
		fmt.Println("\n[BROWSE] Browsing DLC/Digital Repository...")
	} else if platform == "xbox" {
		targetURL = MyrientOrigBase
		fmt.Println("\n[BROWSE] Browsing Original Xbox Library...")
	} else if platform == "digital" {
		targetURL = MyrientDigitalBase
		fmt.Println("\n[BROWSE] Browsing Digital Library...")
	} else {
		fmt.Println("\n[BROWSE] Browsing Xbox 360 Library...")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(targetURL)
	if err != nil {
		http.Error(w, "Myrient Unreachable", 500)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var games []string
	lines := strings.Split(string(body), "<a href=\"")
	for _, line := range lines[1:] {
		if end := strings.Index(line, "\""); end != -1 {
			rawName := line[:end]
			if strings.HasSuffix(rawName, ".zip") {
				cleanName, _ := url.QueryUnescape(rawName)
				cleanName = strings.TrimSuffix(cleanName, ".zip")
				games = append(games, cleanName)
			}
		}
	}
	w.Write([]byte(strings.Join(games, "|")))
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	gameName := r.URL.Query().Get("game")
	xboxIP := r.URL.Query().Get("ip")
	drive := r.URL.Query().Get("drive")
	platform := r.URL.Query().Get("platform")
	mode := r.URL.Query().Get("mode")

	if gameName == "" || xboxIP == "" {
		http.Error(w, "Missing game or ip parameter", 400)
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

	conn := XboxConnection{
		IP:        xboxIP,
		Drive:     drive,
		GameName:  gameName,
		Platform:  platform,
		Mode:      mode,
		Timestamp: time.Now(),
	}

	xboxConnections.Store(gameName, conn)

	fmt.Printf("[REGISTER] Xbox %s registered for %s (mode: %s, drive: %s)\n",
		xboxIP, gameName, mode, drive)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "registered",
		"mode":   mode,
		"ip":     xboxIP,
		"drive":  drive,
	})
}

func handleTrigger(w http.ResponseWriter, r *http.Request) {
	gameName := r.URL.Query().Get("game")
	platform := r.URL.Query().Get("platform")

	if gameName == "" {
		http.Error(w, "Missing game parameter", 400)
		return
	}

	if status, exists := jobQueue.Load(gameName); exists {
		if status.(GameStatus).State == "Ready" {
			w.Write([]byte(`{"status":"already_ready"}`))
			return
		}
	}

	if platform == "digital" {
		go processDigital(gameName)
	} else {
		go processGame(gameName, platform)
	}

	w.Write([]byte(`{"status":"triggered"}`))
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	gameName := r.URL.Query().Get("game")
	if gameName == "" {
		http.Error(w, "Missing game parameter", 400)
		return
	}
	status := GameStatus{State: "Missing", Message: "Not Found"}
	if s, exists := jobQueue.Load(gameName); exists {
		status = s.(GameStatus)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func handleDebug(w http.ResponseWriter, r *http.Request) {
	readyDir := filepath.Join(toolsDir, "Ready")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h2>GODSend Debug</h2>")
	fmt.Fprintf(w, "<h3>Ready Games (Subfolders):</h3><ul>")
	files, _ := os.ReadDir(readyDir)
	for _, f := range files {
		if f.IsDir() {
			fmt.Fprintf(w, "<li>%s</li>", f.Name())
		}
	}
	fmt.Fprintf(w, "</ul>")

	fmt.Fprintf(w, "<h3>Registered Xbox Connections:</h3><ul>")
	xboxConnections.Range(func(key, value interface{}) bool {
		conn := value.(XboxConnection)
		fmt.Fprintf(w, "<li>%s: IP=%s, Mode=%s, Drive=%s</li>",
			conn.GameName, conn.IP, conn.Mode, conn.Drive)
		return true
	})
	fmt.Fprintf(w, "</ul>")
}

func handleFileServe(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/files/")
	if relPath == "" {
		http.Error(w, "Not Found", 404)
		return
	}

	decodedPath, err := url.QueryUnescape(relPath)
	if err != nil {
		http.Error(w, "Invalid Path", 400)
		return
	}

	fullPath := filepath.Join(toolsDir, "Ready", decodedPath)

	absReady, _ := filepath.Abs(filepath.Join(toolsDir, "Ready"))
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absReady) {
		http.Error(w, "Forbidden", 403)
		return
	}

	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		http.Error(w, "Not Found", 404)
		return
	}

	if info.IsDir() {
		entries, _ := os.ReadDir(fullPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><body><h2>Index of /%s</h2><ul>", relPath)
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				name += "/"
			}
			fmt.Fprintf(w, "<li><a href=\"%s\">%s</a></li>", url.PathEscape(name), name)
		}
		fmt.Fprintf(w, "</ul></body></html>")
		return
	}

	file, err := os.Open(fullPath)
	if err != nil {
		http.Error(w, "Cannot Open File", 500)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-cache")

	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		var start, end int64
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
			if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-", &start); err == nil {
				end = info.Size() - 1
			} else {
				http.Error(w, "Invalid Range", 416)
				return
			}
		}
		if end == 0 {
			end = info.Size() - 1
		}

		file.Seek(start, 0)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, info.Size()))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		io.CopyN(w, file, end-start+1)
		return
	}

	bufWriter := bufio.NewWriterSize(w, ServeBufferSize)
	io.CopyBuffer(bufWriter, file, make([]byte, ServeBufferSize))
	bufWriter.Flush()
}

// ==========================================
// FTP CONNECTION HELPER
// ==========================================

func connectToXboxFTP(ip string) (*ftp.ServerConn, error) {
	fmt.Printf("[FTP] Connecting to Xbox at %s:%d...\n", ip, FTPPort)

	// Connect with EPSV disabled (Xbox doesn't support it)
	ftpConn, err := ftp.Dial(fmt.Sprintf("%s:%d", ip, FTPPort),
		ftp.DialWithTimeout(FTPTimeout),
		ftp.DialWithDisabledEPSV(true),
		ftp.DialWithDisabledUTF8(true)) // Disable UTF8 - Xbox FTP doesn't support OPTS UTF8
	if err != nil {
		return nil, fmt.Errorf("FTP connection failed: %v", err)
	}

	fmt.Printf("[FTP] Connected! Attempting authentication...\n")

	// Login with Aurora's default credentials
	err = ftpConn.Login("xboxftp", "xboxftp")
	if err != nil {
		fmt.Printf("[FTP] Login failed: %v\n", err)
		ftpConn.Quit()
		return nil, fmt.Errorf("FTP login failed: %v", err)
	}

	fmt.Printf("[FTP] Login successful!\n")
	return ftpConn, nil
}

// ==========================================
// STANDARD GAME PROCESSING
// ==========================================

func processGame(gameName, platform string) {
	fmt.Printf("\n[%s] === Processing Game: %s ===\n", time.Now().Format("15:04:05"), gameName)
	safeName := sanitizeFilename(gameName)

	var xboxConn *XboxConnection
	if conn, exists := xboxConnections.Load(gameName); exists {
		c := conn.(XboxConnection)
		xboxConn = &c
		fmt.Printf("[%s] Transfer mode: %s to %s (drive: %s)\n",
			time.Now().Format("15:04:05"), xboxConn.Mode, xboxConn.IP, xboxConn.Drive)
	}

	gameDir := filepath.Join(toolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	baseURL := Myrient360Base
	if platform == "xbox" {
		baseURL = MyrientOrigBase
	}

	logStatus(gameName, "Processing", "Searching...")
	searchURL := baseURL + "?search=" + url.QueryEscape(gameName)
	zipURL, err := findZip(searchURL, gameName, baseURL)
	if err != nil {
		logStatus(gameName, "Error", err.Error())
		return
	}

	zipPath := filepath.Join(toolsDir, "Temp", safeName+".zip")
	if cached, _ := checkZipCache(zipURL, zipPath, baseURL); cached {
		logStatus(gameName, "Processing", "Using cached download")
	} else {
		logStatus(gameName, "Processing", "Downloading from Myrient...")
		if err := downloadWithProgress(zipURL, zipPath, gameName, baseURL); err != nil {
			logStatus(gameName, "Error", err.Error())
			return
		}
	}

	logStatus(gameName, "Processing", "Extracting ISO...")
	isoPath, err := extractISO(zipPath, safeName)
	if err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("Extract failed: %v", err))
		return
	}

	logStatus(gameName, "Processing", "Converting to GOD...")
	godTempDir := filepath.Join(toolsDir, "Temp", safeName+"_GOD")
	os.MkdirAll(godTempDir, 0755)
	if err := runIso2God(isoPath, godTempDir); err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("GOD Convert failed: %v", err))
		return
	}
	os.Remove(isoPath)

	titleID, mediaID, err := detectGodStructure(godTempDir)
	if err != nil {
		logStatus(gameName, "Error", fmt.Sprintf("GOD structure detection failed: %v", err))
		return
	}

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		logStatus(gameName, "Processing", "FTP Transfer starting...")
		if err := ftpTransferGame(godTempDir, xboxConn, gameName, titleID, mediaID); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("FTP Transfer failed: %v", err))
			os.RemoveAll(godTempDir)
			return
		}

		if platform == "xbox360" {
			logStatus(gameName, "Processing", "Checking for DLC...")
			ftpProcessDLCs(gameName, titleID, xboxConn)
		}

		os.RemoveAll(godTempDir)
		logStatus(gameName, "Ready", "FTP Transfer Complete!")
		fmt.Printf("[%s] === FTP Transfer Complete ===\n\n", time.Now().Format("15:04:05"))
	} else {
		logStatus(gameName, "Processing", "Archiving...")
		titleID, mediaID, err = bucketAndZip(godTempDir, gameDir, gameName, safeName)
		if err != nil {
			logStatus(gameName, "Error", err.Error())
			return
		}
		os.RemoveAll(godTempDir)

		var dlcList []string
		if platform == "xbox360" {
			logStatus(gameName, "Processing", "Checking for DLC...")
			dlcList = processDLCs(gameName, gameDir)
			if len(dlcList) > 0 {
				logStatus(gameName, "Processing", fmt.Sprintf("Paired %d DLCs", len(dlcList)))
			}
		}

		updateGameINI_Parts(gameDir, gameName, titleID, mediaID, dlcList)
		logStatus(gameName, "Ready", "Ready to Install")
		fmt.Printf("[%s] === Complete ===\n\n", time.Now().Format("15:04:05"))
	}
}

// ==========================================
// FTP TRANSFER FUNCTIONS
// ==========================================

func ftpTransferGame(godDir string, conn *XboxConnection, gameName, titleID, mediaID string) error {
	ftpConn, err := connectToXboxFTP(conn.IP)
	if err != nil {
		return err
	}
	defer ftpConn.Quit()

	driveName := strings.TrimSuffix(conn.Drive, ":")
	basePath := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", driveName, titleID, mediaID)

	fmt.Printf("[FTP] Destination: %s\n", basePath)

	if err := ftpMkdirAll(ftpConn, basePath); err != nil {
		return fmt.Errorf("failed to create directory structure: %v", err)
	}

	contentDir := filepath.Join(godDir, titleID, mediaID)

	var totalFiles int
	var totalSize int64
	filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			totalFiles++
			totalSize += info.Size()
		}
		return nil
	})

	fmt.Printf("[FTP] Transferring %d files (%.2f GB)...\n", totalFiles, float64(totalSize)/1073741824)

	var transferred int
	var transferredSize int64

	err = filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(contentDir, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		remotePath := basePath + "/" + rel

		if info.IsDir() {
			ftpConn.MakeDir(remotePath)
			return nil
		}

		transferred++
		percent := float64(transferredSize) / float64(totalSize) * 100

		logStatus(gameName, "Processing",
			fmt.Sprintf("FTP: %d/%d files (%.1f%%)", transferred, totalFiles, percent))

		if err := ftpUploadFile(ftpConn, path, remotePath, gameName, &transferredSize, totalSize); err != nil {
			fmt.Printf("[FTP] Warning: Failed to upload %s: %v\n", rel, err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	fmt.Printf("\n[FTP] Transfer complete: %d files, %.2f GB\n", transferred, float64(transferredSize)/1073741824)
	return nil
}

func ftpMkdirAll(conn *ftp.ServerConn, path string) error {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	currentPath := ""

	for _, part := range parts {
		currentPath += "/" + part
		conn.MakeDir(currentPath)
	}

	return nil
}

func ftpUploadFile(conn *ftp.ServerConn, localPath, remotePath, gameName string, transferred *int64, totalSize int64) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, _ := file.Stat()
	fileSize := info.Size()

	reader := &ftpProgressReader{
		reader:      file,
		total:       fileSize,
		gameName:    gameName,
		fileName:    filepath.Base(localPath),
		transferred: transferred,
		totalSize:   totalSize,
	}

	err = conn.Stor(remotePath, reader)
	if err != nil {
		return err
	}

	*transferred += fileSize
	return nil
}

type ftpProgressReader struct {
	reader      io.Reader
	total       int64
	written     int64
	gameName    string
	fileName    string
	lastLog     time.Time
	transferred *int64
	totalSize   int64
}

func (r *ftpProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.written += int64(n)

	if time.Since(r.lastLog) > 500*time.Millisecond {
		filePercent := float64(r.written) / float64(r.total) * 100
		totalPercent := float64(*r.transferred+r.written) / float64(r.totalSize) * 100

		fmt.Printf("\r[FTP] %s: %.1f%% | Overall: %.1f%%   ",
			r.fileName, filePercent, totalPercent)

		r.lastLog = time.Now()
	}

	return n, err
}

// ==========================================
// FTP DLC PROCESSING
// ==========================================

func ftpProcessDLCs(gameName, titleID string, conn *XboxConnection) {
	searchURL := MyrientDLCBase + "?search=" + url.QueryEscape(gameName)
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	re := regexp.MustCompile(`href="([^"]+\.zip)"`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	gameNameLower := strings.ToLower(gameName)
	dlcCount := 0

	for _, match := range matches {
		link := match[1]
		decoded, _ := url.QueryUnescape(link)
		lower := strings.ToLower(decoded)

		if strings.Contains(lower, gameNameLower) && strings.Contains(lower, "dlc") {
			dlUrl := link
			if !strings.HasPrefix(link, "http") {
				dlUrl = MyrientDLCBase + link
			}

			dlcCount++
			logStatus(gameName, "Processing", fmt.Sprintf("Downloading DLC %d...", dlcCount))

			dlZipPath := filepath.Join(toolsDir, "Temp", "dlc_temp.zip")
			if err := downloadWithProgress(dlUrl, dlZipPath, gameName+" DLC", MyrientDLCBase); err != nil {
				continue
			}

			extDir := filepath.Join(toolsDir, "Temp", "dlc_ext")
			os.RemoveAll(extDir)

			cmd := exec.Command(filepath.Join(toolsDir, sevenZipBin), "x", dlZipPath, "-o"+extDir, "-y")
			cmd.Run()

			filepath.Walk(extDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !info.IsDir() && info.Size() > 1024*1024 {
					ext := strings.ToLower(filepath.Ext(path))
					if ext != ".txt" && ext != ".nfo" && ext != ".jpg" {
						if info.Size() > MaxDLCSizeBytes {
							return nil
						}

						dlcTitleID, contentType := parseXboxHeader(path)
						if dlcTitleID == "" {
							dlcTitleID = titleID
						}
						typeDir := fmt.Sprintf("%08X", contentType)
						if contentType == 0 {
							typeDir = "00000002"
						}

						logStatus(gameName, "Processing", fmt.Sprintf("FTP: Transferring DLC %d...", dlcCount))
						ftpTransferSingleFile(path, conn, dlcTitleID, typeDir, filepath.Base(path))
					}
				}
				return nil
			})

			os.Remove(dlZipPath)
			os.RemoveAll(extDir)
		}
	}

	if dlcCount > 0 {
		fmt.Printf("[FTP] Transferred %d DLC(s)\n", dlcCount)
	}
}

func ftpTransferSingleFile(localPath string, conn *XboxConnection, titleID, typeDir, fileName string) error {
	ftpConn, err := connectToXboxFTP(conn.IP)
	if err != nil {
		return err
	}
	defer ftpConn.Quit()

	driveName := strings.TrimSuffix(conn.Drive, ":")
	basePath := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", driveName, titleID, typeDir)
	remotePath := basePath + "/" + fileName

	fmt.Printf("[FTP] DLC Destination: %s\n", remotePath)

	if err := ftpMkdirAll(ftpConn, basePath); err != nil {
		return fmt.Errorf("failed to create directory structure: %v", err)
	}

	info, _ := os.Stat(localPath)
	var transferred int64

	if err := ftpUploadFile(ftpConn, localPath, remotePath, fileName, &transferred, info.Size()); err != nil {
		return fmt.Errorf("failed to upload file: %v", err)
	}

	fmt.Printf("\n[FTP] DLC transfer complete: %.2f MB\n", float64(info.Size())/1048576)
	return nil
}

// ==========================================
// DIGITAL PROCESSING (XBLA/XBLIG)
// ==========================================

func processDigital(gameName string) {
	fmt.Printf("\n[%s] === Processing Digital: %s ===\n", time.Now().Format("15:04:05"), gameName)
	safeName := sanitizeFilename(gameName)

	var xboxConn *XboxConnection
	if conn, exists := xboxConnections.Load(gameName); exists {
		c := conn.(XboxConnection)
		xboxConn = &c
		fmt.Printf("[%s] Transfer mode: %s to %s (drive: %s)\n",
			time.Now().Format("15:04:05"), xboxConn.Mode, xboxConn.IP, xboxConn.Drive)
	}

	gameDir := filepath.Join(toolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	logStatus(gameName, "Processing", "Searching Digital Repo...")
	searchURL := MyrientDigitalBase + "?search=" + url.QueryEscape(gameName)
	zipURL, err := findZip(searchURL, gameName, MyrientDigitalBase)
	if err != nil {
		logStatus(gameName, "Error", err.Error())
		return
	}

	zipPath := filepath.Join(toolsDir, "Temp", safeName+"_digi.zip")
	if err := downloadWithProgress(zipURL, zipPath, gameName, MyrientDigitalBase); err != nil {
		logStatus(gameName, "Error", err.Error())
		return
	}

	logStatus(gameName, "Processing", "Extracting...")
	extDir := filepath.Join(toolsDir, "Temp", safeName+"_ext")
	os.RemoveAll(extDir)

	cmd := exec.Command(filepath.Join(toolsDir, sevenZipBin), "x", zipPath, "-o"+extDir, "-y")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("[ERROR] 7z: %s\n", string(out))
		logStatus(gameName, "Error", "Extraction Failed")
		return
	}

	var contentFile, titleID, typeDir string
	filepath.Walk(extDir, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && info.Size() > 1024*1024 {
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".txt" && ext != ".nfo" && ext != ".jpg" {
				tid, ctype := parseXboxHeader(path)
				if tid != "" {
					contentFile = path
					titleID = tid
					typeDir = fmt.Sprintf("%08X", ctype)
					return io.EOF
				}
			}
		}
		return nil
	})

	if contentFile == "" {
		logStatus(gameName, "Error", "No valid Xbox content found")
		return
	}

	finalName := filepath.Base(contentFile)

	if xboxConn != nil && xboxConn.Mode == "ftp" {
		logStatus(gameName, "Processing", "FTP Transfer starting...")
		if err := ftpTransferDigital(contentFile, xboxConn, gameName, titleID, typeDir, finalName); err != nil {
			logStatus(gameName, "Error", fmt.Sprintf("FTP Transfer failed: %v", err))
			return
		}
		logStatus(gameName, "Ready", "FTP Transfer Complete!")
	} else {
		destPath := filepath.Join(gameDir, finalName)
		copyFileBuffered(contentFile, destPath)

		fmt.Printf("[%s] Detected: TitleID=%s, Type=%s\n", time.Now().Format("15:04:05"), titleID, typeDir)

		relPath := fmt.Sprintf("Content\\0000000000000000\\%s\\%s\\", titleID, typeDir)
		updateGameINI_Raw(gameDir, gameName, finalName, relPath)
		logStatus(gameName, "Ready", "Ready to Install")
	}

	os.Remove(zipPath)
	os.RemoveAll(extDir)

	fmt.Printf("[%s] === Complete ===\n\n", time.Now().Format("15:04:05"))
}

func ftpTransferDigital(contentFile string, conn *XboxConnection, gameName, titleID, typeDir, fileName string) error {
	ftpConn, err := connectToXboxFTP(conn.IP)
	if err != nil {
		return err
	}
	defer ftpConn.Quit()

	driveName := strings.TrimSuffix(conn.Drive, ":")
	basePath := fmt.Sprintf("/%s/Content/0000000000000000/%s/%s", driveName, titleID, typeDir)
	remotePath := basePath + "/" + fileName

	fmt.Printf("[FTP] Destination: %s\n", remotePath)

	if err := ftpMkdirAll(ftpConn, basePath); err != nil {
		return fmt.Errorf("failed to create directory structure: %v", err)
	}

	info, _ := os.Stat(contentFile)
	var transferred int64

	if err := ftpUploadFile(ftpConn, contentFile, remotePath, gameName, &transferred, info.Size()); err != nil {
		return fmt.Errorf("failed to upload file: %v", err)
	}

	fmt.Printf("\n[FTP] Transfer complete: %.2f MB\n", float64(info.Size())/1048576)
	return nil
}

// ==========================================
// DLC PROCESSING (HTTP MODE)
// ==========================================

func processDLCs(gameName string, gameDir string) []string {
	searchURL := MyrientDLCBase + "?search=" + url.QueryEscape(gameName)
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	re := regexp.MustCompile(`href="([^"]+\.zip)"`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	var processedDLCs []string
	gameNameLower := strings.ToLower(gameName)

	for _, match := range matches {
		link := match[1]
		decoded, _ := url.QueryUnescape(link)
		lower := strings.ToLower(decoded)

		if strings.Contains(lower, gameNameLower) && strings.Contains(lower, "dlc") {
			dlUrl := link
			if !strings.HasPrefix(link, "http") {
				dlUrl = MyrientDLCBase + link
			}

			dlZipPath := filepath.Join(toolsDir, "Temp", "dlc_temp.zip")
			if err := downloadWithProgress(dlUrl, dlZipPath, gameName+" DLC", MyrientDLCBase); err != nil {
				continue
			}

			extDir := filepath.Join(toolsDir, "Temp", "dlc_ext")
			os.RemoveAll(extDir)

			cmd := exec.Command(filepath.Join(toolsDir, sevenZipBin), "x", dlZipPath, "-o"+extDir, "-y")
			cmd.Run()

			var dlcFile string
			filepath.Walk(extDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !info.IsDir() && info.Size() > 1024*1024 {
					ext := strings.ToLower(filepath.Ext(path))
					if ext != ".txt" && ext != ".nfo" && ext != ".jpg" {
						if info.Size() > MaxDLCSizeBytes {
							return nil
						}
						dlcFile = path
					}
				}
				return nil
			})

			if dlcFile != "" {
				finalZipName := filepath.Base(dlcFile) + ".7z"
				destPath := filepath.Join(gameDir, finalZipName)
				stage := filepath.Join(toolsDir, "Temp", "dlc_stage")
				os.MkdirAll(stage, 0755)
				copyFileBuffered(dlcFile, filepath.Join(stage, filepath.Base(dlcFile)))

				if err := createZipFromDir(stage, destPath); err == nil {
					processedDLCs = append(processedDLCs, finalZipName)
				}
				os.RemoveAll(stage)
			}
			os.Remove(dlZipPath)
			os.RemoveAll(extDir)
		}
	}
	return processedDLCs
}

// ==========================================
// INI MANAGEMENT
// ==========================================

func updateGameINI_Parts(gameDir, gameName, titleID, mediaID string, dlcList []string) {
	iniPath := filepath.Join(gameDir, "godsend.ini")
	f, _ := os.Create(iniPath)
	defer f.Close()
	w := bufio.NewWriter(f)

	encode := func(s string) string {
		s = strings.ReplaceAll(s, " ", "%20")
		s = strings.ReplaceAll(s, "(", "%28")
		s = strings.ReplaceAll(s, ")", "%29")
		return s
	}

	partsRaw, _ := gamePartsMap.Load(gameName)
	parts := partsRaw.([]string)

	fmt.Fprintf(w, "[%s]\n", gameName)
	fmt.Fprintf(w, "type=god\n")
	fmt.Fprintf(w, "titleid=%s\n", titleID)
	fmt.Fprintf(w, "mediaid=%s\n", mediaID)
	if len(parts) > 0 {
		fmt.Fprintf(w, "dataurl=%s\n", encode(parts[0]))
	}
	for i := 1; i < len(parts); i++ {
		fmt.Fprintf(w, "dataurlpart%d=%s\n", i+1, encode(parts[i]))
	}
	for i, dlc := range dlcList {
		fmt.Fprintf(w, "dlc_%d=%s\n", i+1, encode(dlc))
	}
	w.Flush()
}

func updateGameINI_Raw(gameDir, gameName, fileName, relPath string) {
	iniPath := filepath.Join(gameDir, "godsend.ini")
	f, _ := os.Create(iniPath)
	defer f.Close()
	w := bufio.NewWriter(f)

	fmt.Fprintf(w, "[%s]\n", gameName)
	fmt.Fprintf(w, "type=raw\n")
	fmt.Fprintf(w, "filename=%s\n", fileName)
	fmt.Fprintf(w, "path=%s\n", relPath)
	w.Flush()
}

// ==========================================
// HELPERS
// ==========================================

func bucketAndZip(src, dest, gameName, safeName string) (string, string, error) {
	titleID, mediaID, err := detectGodStructure(src)
	if err != nil {
		return "", "", err
	}

	stagingBase := filepath.Join(toolsDir, "Temp", safeName+"_staging")
	os.RemoveAll(stagingBase)
	os.MkdirAll(stagingBase, 0755)

	var parts []string
	var currentSize int64
	partNum := 1
	currentPartDir := filepath.Join(stagingBase, fmt.Sprintf("%s_Part%d", safeName, partNum))
	os.MkdirAll(currentPartDir, 0755)

	contentDir := filepath.Join(src, titleID, mediaID)

	filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(contentDir, path)

		if currentSize+info.Size() > MaxPartSize && currentSize > 0 {
			partName := fmt.Sprintf("%s_Part%d.7z", safeName, partNum)
			if err := createZipFromDir(currentPartDir, filepath.Join(dest, partName)); err != nil {
				return err
			}
			parts = append(parts, partName)

			partNum++
			currentSize = 0
			currentPartDir = filepath.Join(stagingBase, fmt.Sprintf("%s_Part%d", safeName, partNum))
			os.MkdirAll(currentPartDir, 0755)
		}

		destPath := filepath.Join(currentPartDir, rel)
		os.MkdirAll(filepath.Dir(destPath), 0755)
		copyFileBuffered(path, destPath)
		currentSize += info.Size()
		return nil
	})

	if currentSize > 0 {
		partName := fmt.Sprintf("%s_Part%d.7z", safeName, partNum)
		createZipFromDir(currentPartDir, filepath.Join(dest, partName))
		parts = append(parts, partName)
	}

	os.RemoveAll(stagingBase)
	gamePartsMap.Store(gameName, parts)
	return titleID, mediaID, nil
}

func detectGodStructure(godDir string) (string, string, error) {
	entries, err := os.ReadDir(godDir)
	if err != nil {
		return "", "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			titleID := entry.Name()
			titlePath := filepath.Join(godDir, titleID)
			mediaEntries, err := os.ReadDir(titlePath)
			if err != nil {
				continue
			}
			for _, mEntry := range mediaEntries {
				if mEntry.IsDir() {
					return titleID, mEntry.Name(), nil
				}
			}
		}
	}
	return "", "", fmt.Errorf("GOD structure not found")
}

func parseXboxHeader(path string) (string, uint32) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()
	header := make([]byte, 1024)
	if _, err := f.Read(header); err != nil {
		return "", 0
	}

	magic := string(header[0:4])
	if magic != "LIVE" && magic != "PIRS" && magic != "CON " {
		return "", 0
	}

	tid := hex.EncodeToString(header[0x360:0x364])
	ctype := binary.BigEndian.Uint32(header[0x344:0x348])
	return strings.ToUpper(tid), ctype
}

func findZip(searchURL, gameName, baseURL string) (string, error) {
	client := &http.Client{}
	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	re := regexp.MustCompile(`href="([^"]+\.zip)"`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	for _, match := range matches {
		link := match[1]
		decoded, _ := url.QueryUnescape(link)
		if strings.Contains(strings.ToLower(decoded), strings.ToLower(gameName)) {
			if strings.HasPrefix(link, "http") {
				return link, nil
			}
			return baseURL + link, nil
		}
	}
	return "", fmt.Errorf("not found")
}

func checkZipCache(urlStr, localPath, referrer string) (bool, error) {
	info, err := os.Stat(localPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("HEAD", urlStr, nil)
	req.Header.Set("Referer", referrer)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return false, err
	}
	if info.Size() == resp.ContentLength && resp.ContentLength > 1000 {
		return true, nil
	}
	return false, nil
}

func downloadWithProgress(urlStr, dest, name, ref string) error {
	client := &http.Client{}
	req, _ := http.NewRequest("GET", urlStr, nil)
	req.Header.Set("Referer", ref)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, _ := os.Create(dest)
	defer out.Close()

	bufOut := bufio.NewWriterSize(out, CopyBufferSize)
	pw := &ProgressWriter{Total: resp.ContentLength, GameName: name, LastLog: time.Now(), StartTime: time.Now()}
	_, err = io.Copy(bufOut, io.TeeReader(resp.Body, pw))
	bufOut.Flush()
	fmt.Println()
	return err
}

func extractISO(zipPath, safeName string) (string, error) {
	dest := filepath.Join(toolsDir, "Temp", safeName+"_extracted")
	os.RemoveAll(dest)
	cmd := exec.Command(filepath.Join(toolsDir, sevenZipBin), "x", zipPath, "-o"+dest, "*.iso", "-r", "-y")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("7z error: %v | %s", err, string(output))
	}

	var iso string
	filepath.Walk(dest, func(p string, i os.FileInfo, e error) error {
		if strings.HasSuffix(p, ".iso") {
			iso = p
		}
		return nil
	})
	if iso == "" {
		return "", fmt.Errorf("no iso")
	}
	return iso, nil
}

func runIso2God(iso, out string) error {
	cmd := exec.Command(filepath.Join(toolsDir, isoGodBin), iso, out)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("iso2god error: %v | %s", err, string(output))
	}
	return nil
}

func createZipFromDir(dir, out string) error {
	cmd := exec.Command(filepath.Join(toolsDir, sevenZipBin), "a", "-t7z", "-mx0", out, "*")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zip error: %v | %s", err, string(output))
	}
	return nil
}

func copyFileBuffered(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	bufIn := bufio.NewReaderSize(in, CopyBufferSize)
	bufOut := bufio.NewWriterSize(out, CopyBufferSize)

	_, err = io.Copy(bufOut, bufIn)
	if err != nil {
		return err
	}

	return bufOut.Flush()
}

func getOutboundIP() string {
	conn, _ := net.Dial("udp", "8.8.8.8:80")
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func sanitizeFilename(n string) string {
	return regexp.MustCompile(`[<>:"/\\|?*]`).ReplaceAllString(n, " -")
}

func logStatus(game, state, msg string) {
	jobQueue.Store(game, GameStatus{State: state, Message: msg})
}
