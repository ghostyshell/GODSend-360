// router.go — HTTP route registration.
package http

import (
	stdhttp "net/http"
)

// NewRouter builds the HTTP mux with all handlers registered.
func (d *Deps) NewRouter() *stdhttp.ServeMux {
	mux := stdhttp.NewServeMux()

	// RXEA asset endpoints
	mux.HandleFunc("/rxea/decode", d.wrap(d.handleRXEADecode))
	mux.HandleFunc("/rxea/encode", d.wrap(d.handleRXEAEncode))
	mux.HandleFunc("/rxea/encode-multi", d.wrap(d.handleRXEAEncodeMulti))

	// Core game endpoints
	mux.HandleFunc("/browse", d.wrap(d.handleBrowse))
	mux.HandleFunc("/cache-status", d.wrap(d.handleCacheStatus))
	mux.HandleFunc("/cache-refresh", d.wrap(d.handleCacheRefresh))
	mux.HandleFunc("/trigger", d.wrap(d.handleTrigger))
	mux.HandleFunc("/status", d.wrap(d.handleStatus))
	mux.HandleFunc("/queue", d.wrap(d.handleQueue))
	mux.HandleFunc("/queue/remove", d.wrap(d.handleQueueRemove))
	mux.HandleFunc("/debug", d.wrap(d.handleDebug))
	mux.HandleFunc("/register", d.wrap(d.handleRegister))
	mux.HandleFunc("/disc-info", d.wrap(d.handleDiscInfo))
	mux.HandleFunc("/files/", d.wrap(d.handleFileServe))

	// Data management
	mux.HandleFunc("/data/status", d.wrap(d.handleDataStatus))
	mux.HandleFunc("/data/clear", d.wrap(d.handleDataClear))

	// Server config
	mux.HandleFunc("/config", d.wrap(d.handleServerConfig))

	// Toolbox endpoints (ISO conversion)
	mux.HandleFunc("/tools/probe-iso", d.wrap(d.handleToolsProbeISO))
	mux.HandleFunc("/tools/iso2god", d.wrap(d.handleToolsISO2GOD))
	mux.HandleFunc("/tools/iso2xex", d.wrap(d.handleToolsISO2XEX))

	// FTP Manager — synchronous utility operations
	mux.HandleFunc("/ftp/ping", d.wrap(d.handleFTPPing))
	mux.HandleFunc("/ftp/list", d.wrap(d.handleFTPList))
	mux.HandleFunc("/ftp/mkdir", d.wrap(d.handleFTPMkdir))
	mux.HandleFunc("/ftp/delete", d.wrap(d.handleFTPDelete))
	mux.HandleFunc("/ftp/rename", d.wrap(d.handleFTPRename))
	mux.HandleFunc("/ftp/size", d.wrap(d.handleFTPSize))
	mux.HandleFunc("/ftp/download-file", d.wrap(d.handleFTPDownloadFile))
	mux.HandleFunc("/ftp/upload-file", d.wrap(d.handleFTPUploadFile))
	mux.HandleFunc("/ftp/test", d.wrap(d.handleFTPTest))
	mux.HandleFunc("/ftp/drives", d.wrap(d.handleFTPDrives))
	mux.HandleFunc("/ftp/batch", d.wrap(d.handleFTPBatch))

	// FTP Manager — async trackable operations
	mux.HandleFunc("/ftp/upload", d.wrap(d.handleFTPUpload))
	mux.HandleFunc("/ftp/copy", d.wrap(d.handleFTPCopy))
	mux.HandleFunc("/ftp/move-game", d.wrap(d.handleFTPMoveGame))
	mux.HandleFunc("/ftp/upload-scripts", d.wrap(d.handleFTPUploadScripts))

	// FTP Manager — job management
	mux.HandleFunc("/ftp/jobs", d.wrap(d.handleFTPJobs))
	mux.HandleFunc("/ftp/jobs/remove", d.wrap(d.handleFTPJobRemove))

	return mux
}
