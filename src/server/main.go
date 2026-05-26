package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"godsend/app"
	"godsend/infrastructure/download"
	"godsend/infrastructure/ftp"
	"godsend/infrastructure/torrent"
	httpintf "godsend/interfaces/http"
	"godsend/models"
	"godsend/services/cache"
	"godsend/services/local"
	"godsend/services/pipeline"
)

func main() {
	a := app.NewApp()
	if err := a.SetupPaths(); err != nil {
		fmt.Printf("[FATAL] Setup failed: %v\n", err)
		os.Exit(1)
	}

	a.CopyBuffer = make([]byte, app.CopyBufferSize)
	a.LoadIAAuthFromEnv()
	a.ServerIP = app.GetOutboundIP()
	if a.ServerIP == "" {
		a.ServerIP = "0.0.0.0"
	}

	// ── Infrastructure services ─────────────────────────────────────
	dlSvc := &download.Service{App: a}
	ftpSvc := &ftp.Service{App: a}
	ftpMgr := ftp.NewManager(a, ftpSvc)
	torrentSvc := &torrent.Service{
		App:                a,
		DarwinCandidatesFn: darwinAria2cExtraCandidates,
	}

	// macOS aria2c bootstrap (no-op on other platforms)
	if err := ensureAria2cDarwinAtStartup(a, torrentSvc); err != nil {
		a.Logf("[WARN] Could not ensure aria2c on macOS: %v — Minerva torrents need aria2 (install Homebrew + brew install aria2, or set GODSEND_SKIP_ARIA2_BOOTSTRAP=1 if you use IA only)", err)
	}

	// ── Service layer ───────────────────────────────────────────────
	iaSvc := &cache.IAService{App: a}
	minervaSvc := &cache.MinervaService{App: a}
	romSvc := &cache.ROMService{App: a, IA: iaSvc}
	localSvc := &local.Service{App: a}
	pipelineSvc := &pipeline.Service{
		App:      a,
		IA:       iaSvc,
		Minerva:  minervaSvc,
		ROM:      romSvc,
		Download: dlSvc,
		FTP:      ftpSvc,
		Torrent:  torrentSvc,
	}

	// ── Banner ──────────────────────────────────────────────────────
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║    GODSend Backend Server v2.12.22        ║")
	fmt.Println("║  ISO + XEX + XBLA + DLC + ROMs (EdgeEmu) ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Printf("[INFO] Copy Buffer: %d MB | Serve Buffer: %d KB | FTP Buffer: %d MB\n",
		app.CopyBufferSize/1024/1024, app.ServeBufferSize/1024, app.FTPBufferSize/1024/1024)
	fmt.Printf("[INFO] Transfer folder (local ISOs): %s\n", a.TransferDir)
	fmt.Printf("[INFO] ROM install path (on Xbox): [Drive]\\%s\\[System]\\\n", a.ROMRootPath)

	// ── Initialise build-state trackers ─────────────────────────────
	a.BuildStatesMu.Lock()
	for p := range app.IACollections {
		a.BuildStates[p] = &models.BuildState{State: "idle"}
	}
	a.BuildStatesMu.Unlock()

	// ── Load persisted caches from disk ─────────────────────────────
	platformOrder := []string{"xbox360", "digital", "xbla", "dlc", "xblig", "games", "xbox"}
	var delay time.Duration
	for _, platform := range platformOrder {
		loaded := iaSvc.LoadCacheFromDisk(platform)
		if loaded {
			a.Logf("CACHE: Loaded %s from disk", platform)
		} else {
			go func(p string, d time.Duration) {
				if d > 0 {
					time.Sleep(d)
				}
				iaSvc.Build(p)
			}(platform, delay)
			delay += 800 * time.Millisecond
		}
	}

	// Minerva caches
	minervaPlatforms := []string{"xbox360", "xbox", "digital", "xbla", "dlc", "xblig", "games"}
	var minervaDelay time.Duration
	for _, mp := range minervaPlatforms {
		if minervaSvc.LoadCacheFromDisk(mp) {
			a.Logf("MINERVA CACHE: Loaded %s from disk", mp)
		} else {
			go func(p string, d time.Duration) {
				if d > 0 {
					time.Sleep(d)
				}
				minervaSvc.Build(p)
			}(mp, minervaDelay)
			minervaDelay += 1200 * time.Millisecond
		}
	}

	// ROM caches (lazy — won't block startup)
	go func() {
		for sysid := range app.ROMSystems {
			if romSvc.LoadFromDisk(sysid) {
				a.Logf("ROM CACHE: Loaded %s from disk", sysid)
			}
		}
	}()

	// ── HTTP routes ─────────────────────────────────────────────────
	deps := &httpintf.Deps{
		App:      a,
		IA:       iaSvc,
		Minerva:  minervaSvc,
		ROM:      romSvc,
		Local:    localSvc,
		Pipeline: pipelineSvc,
		FTP:      ftpSvc,
		FTPMgr:   ftpMgr,
	}
	mux := deps.NewRouter()

	// ── Resume pending FTP jobs from previous sessions ──────────────
	go func() {
		for _, job := range ftpSvc.LoadAllPendingFTPJobs() {
			a.Logf("FTP PENDING: Resuming job for %s (from previous session)", job.GameName)
			a.LogStatus(job.GameName, "Pending FTP", "Resumed from previous session — waiting for Xbox FTP...")
			go ftpSvc.RetryFTPJobForever(job)
		}
	}()

	// ── Listen & serve ──────────────────────────────────────────────
	requestedPort, err := strconv.Atoi(a.ServerPort)
	if err != nil {
		fmt.Printf("[FATAL] invalid server port %q\n", a.ServerPort)
		os.Exit(1)
	}
	listener, chosenPort, err := a.ListenOnAvailablePort(requestedPort)
	if err != nil {
		fmt.Printf("[FATAL] %v\n", err)
		os.Exit(1)
	}
	if chosenPort != requestedPort {
		a.Logf("[INFO] Port %d was in use; listening on %d instead", requestedPort, chosenPort)
	}
	a.ServerPort = strconv.Itoa(chosenPort)
	fmt.Printf("\n[INFO] Server IP: %s:%s\n", a.ServerIP, a.ServerPort)
	a.Logf("[INFO] GODSEND_LISTEN_PORT=%s", a.ServerPort)

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ConnState: func(conn net.Conn, state http.ConnState) {
			if state == http.StateNew {
				if tc, ok := conn.(*net.TCPConn); ok {
					tc.SetNoDelay(true)
					tc.SetKeepAlive(true)
					tc.SetKeepAlivePeriod(app.TCPKeepAlive)
					tc.SetWriteBuffer(app.TCPSendBuffer)
					tc.SetReadBuffer(app.TCPSendBuffer)
				}
			}
		},
	}
	a.Logf("Starting server on port %s... Server started. Please start the script on the xbox", a.ServerPort)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Printf("[FATAL] %v\n", err)
		os.Exit(1)
	}
}
