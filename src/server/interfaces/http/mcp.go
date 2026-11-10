// mcp.go - Model Context Protocol endpoint (Streamable HTTP, JSON responses only).
//
// Every MCP tool is a thin bridge onto an existing HTTP route: the call is
// replayed against the same mux, so MCP can never drift from the real API.
// Off unless GODSEND_MCP_ENABLED is set, and loopback-only even then, because
// the backend listens on the LAN for the Xbox and the API has no auth.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

const mcpProtocolVersion = "2025-06-18"

type mcpParam struct {
	Name, Type, Desc string
	Required         bool
	Items            string // element type when Type is "array"
	Default          string // sent when the caller omits the argument
}

type mcpTool struct {
	Name, Desc, Method, Path string
	Query                    bool // params go in the query string, else a JSON body
	Params                   []mcpParam
}

func p(name, typ, desc string) mcpParam { return mcpParam{Name: name, Type: typ, Desc: desc} }
func req(name, typ, desc string) mcpParam {
	return mcpParam{Name: name, Type: typ, Desc: desc, Required: true}
}

var (
	pIP      = req("ip", "string", "Xbox IP address")
	pDrive   = p("drive", "string", "Xbox drive, e.g. Hdd1:")
	pTitleID = req("title_id", "string", "8-hex-digit Xbox title ID")
)

// mcpTools lists one tool per backend operation. Routes that return binary or
// HTML (/files/, /debug, /rxea/*) are not exposed: MCP tool results are text.
var mcpTools = []mcpTool{
	// Library and install pipeline
	{"browse_games", "List games available for a platform (pipe-separated names).", "GET", "/browse", true, []mcpParam{
		req("platform", "string", "xbox360, xbox, xbla, digital, dlc, xblig, games, local, or rom_<sysid>"),
		p("source", "string", "minerva or ia to limit to one catalog; omit for merged")}},
	{"cache_status", "Per-platform game cache build state and counts.", "GET", "/cache-status", true, nil},
	{"cache_refresh", "Rebuild the game cache for a platform.", "GET", "/cache-refresh", true, []mcpParam{
		req("platform", "string", "all, an IA platform, or rom_<sysid>")}},
	{"register_console", "Register the Xbox IP and destination drive used for FTP install of a game.", "GET", "/register", true, []mcpParam{
		req("game", "string", "game name"), req("ip", "string", "Xbox IP address"), pDrive,
		p("platform", "string", "platform of the game"),
		{Name: "mode", Type: "string", Desc: "ftp (backend pushes the install to the Xbox, default) or http (the Aurora script pulls it)", Default: "ftp"},
		p("install_type", "string", "god, xex or content")}},
	{"install_game", "Start downloading/converting a game and installing it to the registered Xbox.", "GET", "/trigger", true, []mcpParam{
		req("game", "string", "game name as listed by browse_games"), req("platform", "string", "platform"),
		p("source", "string", "minerva or ia; omit for automatic"), p("install_type", "string", "god, xex or content")}},
	{"game_status", "Job state for a game: Idle, Processing, Ready, Error, Missing.", "GET", "/status", true, []mcpParam{
		req("game", "string", "game name")}},
	{"disc_info", "Disc compatibility metadata for a local ISO in the Transfer folder.", "GET", "/disc-info", true, []mcpParam{
		req("game", "string", "game name")}},
	{"list_queue", "List all active and completed install jobs.", "GET", "/queue", true, nil},
	{"remove_from_queue", "Remove one install job, or all jobs when game is omitted.", "POST", "/queue/remove", true, []mcpParam{
		p("game", "string", "game name; omit to clear every job")}},
	{"data_status", "Active jobs, pending FTP jobs and local data size.", "GET", "/data/status", true, nil},
	{"clear_data", "Cancel all jobs and pending FTP transfers and wipe Ready/ and Temp/. Destructive.", "GET", "/data/clear", true, nil},
	{"server_config", "Server-side config readable by the Aurora script (default drive).", "GET", "/config", true, nil},

	// DLC and Title Updates
	{"content_discover", "Installed DLC on the Xbox plus downloadable DLC candidates for a title.", "GET", "/content/discover", true, []mcpParam{
		pTitleID, p("game_name", "string", "game name"), p("xbox_ip", "string", "Xbox IP address"), pDrive}},
	{"content_title_updates", "Title Updates available from XboxUnity for a title.", "GET", "/content/tu", true, []mcpParam{pTitleID}},
	{"content_installed", "DLC and Title Updates already installed on the Xbox for a title.", "GET", "/content/installed", true, []mcpParam{
		pTitleID, p("xbox_ip", "string", "Xbox IP address"), pDrive}},
	{"content_sources", "Download sources for a title's content.", "GET", "/content/sources", true, []mcpParam{
		pTitleID, p("game_name", "string", "game name")}},
	{"content_install", "Queue a DLC or Title Update download and FTP install.", "POST", "/content/queue", false, []mcpParam{
		req("game_name", "string", "game name"), pTitleID, req("content_type", "string", "dlc or tu"),
		req("display_name", "string", "content display name"), p("file_name", "string", "file name"),
		req("source", "string", "minerva, ia or xboxunity"), p("source_url", "string", "direct URL for URL sources"),
		pDrive, p("xbox_ip", "string", "Xbox IP address")}},
	{"content_set_active", "Activate or deactivate an installed Title Update (other TUs are deactivated).", "POST", "/content/set-active", false, []mcpParam{
		pTitleID, pDrive, req("content_type", "string", "tu"), req("file_name", "string", "installed file name"),
		req("xbox_ip", "string", "Xbox IP address"), req("set_active", "boolean", "true to activate")}},

	// Saves
	{"saves_discover", "Xbox profiles with saves (optionally for one title).", "GET", "/saves/discover", true, []mcpParam{
		pIP, pDrive, p("title_id", "string", "title ID filter")}},
	{"saves_list", "Save files for a profile and title.", "GET", "/saves/list", true, []mcpParam{
		pIP, pDrive, pTitleID, req("profile_id", "string", "profile XUID")}},
	{"saves_download", "Back up a profile's saves for a title to the local backup folder.", "POST", "/saves/download", false, []mcpParam{
		pIP, pDrive, pTitleID, req("profile_id", "string", "profile XUID"), p("game_name", "string", "game name")}},
	{"saves_delete", "Delete a save folder on the Xbox. Destructive.", "POST", "/saves/delete", false, []mcpParam{
		pIP, pDrive, pTitleID, req("profile_id", "string", "profile XUID")}},
	{"saves_copy", "Copy saves between profiles.", "POST", "/saves/copy", false, []mcpParam{
		pIP, pDrive, pTitleID, req("src_profile", "string", "source XUID"), req("dst_profile", "string", "destination XUID"),
		p("use_keyvault", "boolean", "re-sign with the console KeyVault")}},
	{"saves_backup_all", "Back up every profile and save on the Xbox.", "POST", "/saves/backup-all", false, []mcpParam{pIP, pDrive}},
	{"saves_keyvault_status", "Whether a usable KeyVault is present for re-signing saves.", "GET", "/saves/keyvault-status", true, []mcpParam{pIP}},

	// ISO tools
	{"iso_probe", "Read title ID, media ID and disc number from a local ISO.", "POST", "/tools/probe-iso", false, []mcpParam{
		req("isoPath", "string", "local ISO path")}},
	{"iso_to_god", "Convert a local ISO to Games on Demand format.", "POST", "/tools/iso2god", false, []mcpParam{
		req("isoPath", "string", "local ISO path"), req("outDir", "string", "local output folder")}},
	{"iso_to_xex", "Extract a local ISO to an XEX folder.", "POST", "/tools/iso2xex", false, []mcpParam{
		req("isoPath", "string", "local ISO path"), req("outDir", "string", "local output folder")}},

	// FTP manager
	{"ftp_ping", "Check FTP connectivity to the Xbox.", "POST", "/ftp/ping", false, []mcpParam{pIP}},
	{"ftp_test", "Verbose FTP connection test.", "POST", "/ftp/test", false, []mcpParam{
		pIP, p("user", "string", "FTP user"), p("password", "string", "FTP password")}},
	{"ftp_drives", "List Xbox drives.", "GET", "/ftp/drives", true, []mcpParam{pIP}},
	{"ftp_list", "List a directory on the Xbox.", "POST", "/ftp/list", false, []mcpParam{pIP, req("path", "string", "remote path")}},
	{"ftp_mkdir", "Create a directory on the Xbox.", "POST", "/ftp/mkdir", false, []mcpParam{pIP, req("path", "string", "remote path")}},
	{"ftp_delete", "Delete a file or directory on the Xbox. Destructive.", "POST", "/ftp/delete", false, []mcpParam{pIP, req("path", "string", "remote path")}},
	{"ftp_rename", "Rename or move a file or directory on the Xbox.", "POST", "/ftp/rename", false, []mcpParam{
		pIP, req("from", "string", "remote source"), req("to", "string", "remote destination")}},
	{"ftp_size", "Size of a file on the Xbox.", "POST", "/ftp/size", false, []mcpParam{pIP, req("path", "string", "remote path")}},
	{"ftp_download_file", "Download one file from the Xbox to this computer.", "POST", "/ftp/download-file", false, []mcpParam{
		pIP, req("remote_path", "string", "remote file"), req("local_path", "string", "local destination")}},
	{"ftp_upload_file", "Upload one local file to the Xbox.", "POST", "/ftp/upload-file", false, []mcpParam{
		pIP, req("local_path", "string", "local file"), req("remote_path", "string", "remote destination")}},
	{"ftp_batch", "Run several FTP ops over one connection (list, size, download, upload, ensure_dir, remove, cd, pwd).", "POST", "/ftp/batch", false, []mcpParam{
		pIP, {Name: "ops", Type: "array", Items: "object", Desc: "operations", Required: true}, p("lock_wait_ms", "integer", "max wait for the FTP lock")}},
	{"ftp_upload", "Queue a tracked upload of local files to the Xbox.", "POST", "/ftp/upload", false, []mcpParam{
		pIP, {Name: "local_paths", Type: "array", Items: "string", Desc: "local files", Required: true}, req("remote_path", "string", "remote folder")}},
	{"ftp_copy", "Queue a tracked copy on the Xbox.", "POST", "/ftp/copy", false, []mcpParam{
		pIP, req("src", "string", "remote source"), req("dst", "string", "remote destination"), p("is_dir", "boolean", "source is a directory")}},
	{"ftp_move_game", "Queue moving an installed game to another drive.", "POST", "/ftp/move-game", false, []mcpParam{
		pIP, req("game_name", "string", "game name"), req("src_drive", "string", "current drive"),
		req("directory", "string", "game folder"), req("target_drive", "string", "destination drive")}},
	{"ftp_upload_scripts", "Queue uploading the Aurora scripts to the Xbox.", "POST", "/ftp/upload-scripts", false, []mcpParam{
		pIP, req("scripts_dir", "string", "local scripts folder"), req("remote_path", "string", "remote Aurora scripts path"),
		p("server_ip", "string", "backend IP for the script"), p("server_port", "string", "backend port for the script")}},
	{"ftp_jobs", "List FTP manager jobs.", "GET", "/ftp/jobs", true, nil},
	{"ftp_job_remove", "Remove an FTP manager job.", "POST", "/ftp/jobs/remove", true, []mcpParam{req("id", "string", "job id")}},
}

func (t mcpTool) schema() map[string]any {
	props := map[string]any{}
	required := []string{}
	for _, prm := range t.Params {
		s := map[string]any{"type": prm.Type, "description": prm.Desc}
		if prm.Items != "" {
			s["items"] = map[string]any{"type": prm.Items}
		}
		props[prm.Name] = s
		if prm.Required {
			required = append(required, prm.Name)
		}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

// mcpLocalOnly rejects anything but a loopback peer, and any browser Origin
// that is not itself local (DNS-rebinding guard from the MCP spec).
func mcpLocalOnly(r *stdhttp.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return false
	}
	// A local reverse proxy or tunnel makes remote callers look like loopback.
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" {
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil {
			return false
		}
		h := u.Hostname()
		return h == "localhost" || (net.ParseIP(h) != nil && net.ParseIP(h).IsLoopback())
	}
	return true
}

type rpcReq struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (d *Deps) handleMCP(api stdhttp.Handler) stdhttp.HandlerFunc {
	byName := map[string]mcpTool{}
	for _, t := range mcpTools {
		byName[t.Name] = t
	}
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if !mcpLocalOnly(r) {
			stdhttp.Error(w, "MCP is only available from this computer", stdhttp.StatusForbidden)
			return
		}
		if r.Method != stdhttp.MethodPost {
			// ponytail: no server-initiated SSE stream; the spec allows 405 here.
			w.Header().Set("Allow", "POST")
			stdhttp.Error(w, "method not allowed", stdhttp.StatusMethodNotAllowed)
			return
		}
		var m rpcReq
		if err := json.NewDecoder(stdhttp.MaxBytesReader(w, r.Body, 1<<20)).Decode(&m); err != nil {
			writeRPC(w, nil, nil, &rpcErr{-32700, "parse error"})
			return
		}
		if len(m.ID) == 0 { // notification
			w.WriteHeader(stdhttp.StatusAccepted)
			return
		}
		switch m.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			json.Unmarshal(m.Params, &p)
			v := mcpProtocolVersion
			if p.ProtocolVersion == "2025-03-26" || p.ProtocolVersion == "2024-11-05" {
				v = p.ProtocolVersion
			}
			writeRPC(w, m.ID, map[string]any{
				"protocolVersion": v,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "godsend-360", "version": "1"},
				"instructions":    "Manage an Xbox 360 running Aurora through the GODsend backend: browse and install games, DLC and Title Updates, back up saves, convert ISOs, and manage files over FTP.",
			}, nil)
		case "ping":
			writeRPC(w, m.ID, map[string]any{}, nil)
		case "tools/list":
			list := make([]map[string]any, 0, len(mcpTools))
			for _, t := range mcpTools {
				list = append(list, map[string]any{"name": t.Name, "description": t.Desc, "inputSchema": t.schema()})
			}
			writeRPC(w, m.ID, map[string]any{"tools": list}, nil)
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(m.Params, &p); err != nil {
				writeRPC(w, m.ID, nil, &rpcErr{-32602, "invalid params"})
				return
			}
			t, ok := byName[p.Name]
			if !ok {
				writeRPC(w, m.ID, nil, &rpcErr{-32602, "unknown tool: " + p.Name})
				return
			}
			text, isErr := callTool(r.Context(), api, t, p.Arguments)
			writeRPC(w, m.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
				"isError": isErr,
			}, nil)
		default:
			writeRPC(w, m.ID, nil, &rpcErr{-32601, "method not found: " + m.Method})
		}
	}
}

// callTool replays the tool as an in-process request against the API mux.
func callTool(ctx context.Context, api stdhttp.Handler, t mcpTool, args map[string]any) (string, bool) {
	if args == nil {
		args = map[string]any{}
	}
	for _, prm := range t.Params {
		if _, ok := args[prm.Name]; !ok {
			if prm.Required {
				return "missing required argument: " + prm.Name, true
			}
			if prm.Default != "" {
				args[prm.Name] = prm.Default
			}
		}
	}
	target := t.Path
	var body []byte
	if t.Query {
		q := url.Values{}
		for k, v := range args {
			q.Set(k, fmt.Sprint(v))
		}
		if len(q) > 0 {
			target += "?" + q.Encode()
		}
	} else {
		body, _ = json.Marshal(args)
	}
	sub := httptest.NewRequest(t.Method, target, bytes.NewReader(body)).WithContext(ctx) // client disconnect cancels the operation
	if body != nil {
		sub.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, sub)
	out := strings.TrimSpace(rec.Body.String())
	if out == "" {
		out = stdhttp.StatusText(rec.Code)
	}
	return out, rec.Code >= 400
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeRPC(w stdhttp.ResponseWriter, id json.RawMessage, result any, e *rpcErr) {
	resp := map[string]any{"jsonrpc": "2.0", "id": id}
	if e != nil {
		resp["error"] = e
	} else {
		resp["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
