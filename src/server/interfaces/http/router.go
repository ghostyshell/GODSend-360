package http

import (
	stdhttp "net/http"
)

// RouterFactory builds the HTTP mux used by the backend. In the first step of
// the refactor, this is a thin wrapper around the existing handlers in main.go;
// as services are extracted, the handler implementations will move here.
func NewRouter() *stdhttp.ServeMux {
	mux := stdhttp.NewServeMux()

	// Handlers will be registered here as they are pulled out of main.go,
	// e.g.:
	//
	// mux.HandleFunc("/browse", browseHandler)
	// mux.HandleFunc("/trigger", triggerHandler)
	// mux.HandleFunc("/status", statusHandler)
	// mux.HandleFunc("/queue", queueHandler)
	// mux.HandleFunc("/files/", filesHandler)
	// mux.HandleFunc("/debug", debugHandler)

	return mux
}

