// middleware.go — HTTP middleware and JSON response helpers.
package http

import (
	"encoding/json"
	stdhttp "net/http"
	"runtime"

	"godsend/app"
	"godsend/infrastructure/ftp"
	"godsend/services/cache"
	"godsend/services/local"
	"godsend/services/pipeline"
)

// Deps holds all dependencies needed by HTTP handlers.
type Deps struct {
	App      *app.App
	IA       *cache.IAService
	Minerva  *cache.MinervaService
	ROM      *cache.ROMService
	Local    *local.Service
	Pipeline *pipeline.Service
	FTP      *ftp.Service
}

// jsonError writes a JSON error response.
func jsonError(w stdhttp.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"state": "Error", "message": message})
}

// jsonSuccess writes a JSON success response.
func jsonSuccess(w stdhttp.ResponseWriter, data map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// RecoverMiddleware wraps a handler with panic recovery.
func RecoverMiddleware(a *app.App, next stdhttp.HandlerFunc) stdhttp.HandlerFunc {
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		defer func() {
			if err := recover(); err != nil {
				a.Logf("PANIC: %s %s: %v", r.Method, r.URL.Path, err)
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				a.Logf("STACK: %s", string(buf[:n]))
				jsonError(w, 500, "Internal server error")
			}
		}()
		next(w, r)
	}
}

// wrap is a shorthand for RecoverMiddleware.
func (d *Deps) wrap(fn stdhttp.HandlerFunc) stdhttp.HandlerFunc {
	return RecoverMiddleware(d.App, fn)
}
