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

	return mux
}
