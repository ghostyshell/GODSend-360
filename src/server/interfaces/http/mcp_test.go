package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"godsend/app"
)

func mcpPost(t *testing.T, mux *stdhttp.ServeMux, remote, origin, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	r.RemoteAddr = remote
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestMCPOffByDefault(t *testing.T) {
	mux := (&Deps{App: &app.App{}}).NewRouter()
	if w := mcpPost(t, mux, "127.0.0.1:5000", "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); w.Code != 404 {
		t.Fatalf("disabled MCP answered %d, want 404", w.Code)
	}
}

func TestMCPLoopbackOnly(t *testing.T) {
	mux := (&Deps{App: &app.App{MCPEnabled: true}}).NewRouter()
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	if w := mcpPost(t, mux, "192.168.1.50:5000", "", ping); w.Code != 403 {
		t.Fatalf("LAN peer got %d, want 403", w.Code)
	}
	if w := mcpPost(t, mux, "127.0.0.1:5000", "https://evil.example", ping); w.Code != 403 {
		t.Fatalf("cross-origin browser got %d, want 403", w.Code)
	}
	proxied := httptest.NewRequest("POST", "/mcp", strings.NewReader(ping))
	proxied.RemoteAddr = "127.0.0.1:5000"
	proxied.Header.Set("X-Forwarded-For", "203.0.113.9")
	pw := httptest.NewRecorder()
	mux.ServeHTTP(pw, proxied)
	if w := pw; w.Code != 403 {
		t.Fatalf("proxied request got %d, want 403", w.Code)
	}
	if w := mcpPost(t, mux, "[::1]:5000", "http://localhost:3000", ping); w.Code != 200 {
		t.Fatalf("local client got %d, want 200", w.Code)
	}
}

func TestMCPProtocol(t *testing.T) {
	mux := (&Deps{App: &app.App{MCPEnabled: true, DefaultXboxDrive: "Hdd1:"}}).NewRouter()
	local := "127.0.0.1:5000"

	var init struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	w := mcpPost(t, mux, local, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	json.Unmarshal(w.Body.Bytes(), &init)
	if init.Result.ProtocolVersion != "2025-06-18" {
		t.Fatalf("initialize: %s", w.Body.String())
	}
	if w := mcpPost(t, mux, local, "", `{"jsonrpc":"2.0","method":"notifications/initialized"}`); w.Code != 202 {
		t.Fatalf("notification got %d, want 202", w.Code)
	}

	var list struct {
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	json.Unmarshal(mcpPost(t, mux, local, "", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`).Body.Bytes(), &list)
	if len(list.Result.Tools) != len(mcpTools) {
		t.Fatalf("tools/list returned %d tools, want %d", len(list.Result.Tools), len(mcpTools))
	}

	// A real round trip through the API mux.
	w = mcpPost(t, mux, local, "", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"server_config","arguments":{}}}`)
	if !strings.Contains(w.Body.String(), `Hdd1:`) || strings.Contains(w.Body.String(), `"isError":true`) {
		t.Fatalf("server_config call: %s", w.Body.String())
	}
	// Missing required argument is a tool error, not a transport error.
	w = mcpPost(t, mux, local, "", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"ftp_list","arguments":{"ip":"1.2.3.4"}}}`)
	if !strings.Contains(w.Body.String(), `"isError":true`) || !strings.Contains(w.Body.String(), "path") {
		t.Fatalf("missing-arg call: %s", w.Body.String())
	}
}

// Every tool must point at a registered route, and every route must be a tool
// (or deliberately excluded), so MCP cannot drift from the HTTP API.
func TestMCPCoversEveryRoute(t *testing.T) {
	mux := (&Deps{App: &app.App{MCPEnabled: true}}).NewRouter()
	excluded := map[string]bool{"/files/": true, "/debug": true, "/rxea/decode": true, "/rxea/encode": true, "/rxea/encode-multi": true, "/mcp": true}
	tooled := map[string]bool{}
	for _, tool := range mcpTools {
		tooled[tool.Path] = true
		if _, pattern := mux.Handler(httptest.NewRequest(tool.Method, tool.Path, nil)); pattern != tool.Path {
			t.Errorf("tool %s points at unregistered route %s", tool.Name, tool.Path)
		}
	}
	src := routerSource(t)
	for _, line := range strings.Split(src, "\n") {
		i := strings.Index(line, `mux.HandleFunc("`)
		if i < 0 {
			continue
		}
		path := strings.SplitN(line[i+len(`mux.HandleFunc("`):], `"`, 2)[0]
		if !tooled[path] && !excluded[path] {
			t.Errorf("route %s has no MCP tool; add one to mcpTools or exclude it", path)
		}
	}
}

func routerSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// register_console must default to FTP mode, or MCP installs sit Ready forever.
func TestMCPRegisterDefaultsToFTP(t *testing.T) {
	var got string
	api := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { got = r.URL.Query().Get("mode") })
	for _, tool := range mcpTools {
		if tool.Name == "register_console" {
			callTool(context.Background(), api, tool, map[string]any{"game": "Halo 3", "ip": "1.2.3.4"})
		}
	}
	if got != "ftp" {
		t.Fatalf("register_console sent mode=%q, want ftp", got)
	}
}
