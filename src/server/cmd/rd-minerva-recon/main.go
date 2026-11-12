// One-off operator tool: report Real-Debrid's per-file instant-availability for
// every Minerva collection torrent, so we can decide which to actually cache.
//
// Minerva ships one collection .torrent per platform (each game is a file inside
// its platform's collection, sharing one infohash). Real-Debrid caches per-file
// (unlike TorBox) and serves from its network cache; instantAvailability reports
// which files RD already has swarm-wide, by infohash - no addMagnet needed.
//
// Flow: fetch+parse each collection .torrent (infohash + file list in torrent
// order) -> GET /torrents/instantAvailability per hash (RD rejects 4+ comma-
// joined hashes with wrong_parameter, so one call per hash) -> map cached RD
// file IDs (1-based torrent order) to game filenames -> print a per-collection
// coverage report. Read-only: NO addMagnet, NO selectFiles, NO download
// triggered, account left untouched.
//
// Not part of the app build. Run from src/server:
//
//	GODSEND_REALDEBRID_KEY=<key> go run ./cmd/rd-minerva-recon
//
// The key is never logged.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"godsend/app"
)

const apiBase = "https://api.real-debrid.com/rest/1.0"

type file struct {
	Name  string
	Bytes int64
}

type collection struct {
	Platform string
	InfoHash string
	Name     string
	Files    []file
}

func main() {
	key := strings.TrimSpace(os.Getenv("GODSEND_REALDEBRID_KEY"))
	if key == "" {
		fail("GODSEND_REALDEBRID_KEY env var not set")
	}

	// 1. Fetch + parse each platform's collection torrent.
	var cols []collection
	for _, p := range sortedPlatforms() {
		ih, name, files, err := buildCollection(p)
		if err != nil {
			logf("WARN  %s: %v (skipping)", p, err)
			continue
		}
		logf("built %s: infohash=%s files=%d size=%.1fGB", p, ih, len(files), float64(totalBytes(files))/1e9)
		cols = append(cols, collection{Platform: p, InfoHash: ih, Name: name, Files: files})
	}
	if len(cols) == 0 {
		fail("no collection torrents parsed")
	}

	// 2. instantAvailability per hash (RD rejects 4+ comma-joined hashes).
	hashes := make([]string, len(cols))
	for i := range cols {
		hashes[i] = cols[i].InfoHash
	}
	cached, err := instantAvailability(context.Background(), key, hashes)
	if err != nil {
		fail("instantAvailability: %v", err)
	}

	// 3. Per-collection coverage report.
	fmt.Println()
	logf("=== Real-Debrid Minerva recon (read-only; account untouched) ===")
	logf("instantAvailability reflects RD's NETWORK cache (swarm-wide). A file")
	logf("marked cached = selecting it is instant + counts only toward storage;")
	logf("not cached = RD must P2P-download it (slow, may fail on dead torrents).")
	logf("RD caches per-account once selected; file IDs assume 1-based torrent order.")
	fmt.Println()

	var gFiles, gCached int
	var gBytes, gCachedBytes int64
	for _, c := range cols {
		cset := cached[strings.ToLower(c.InfoHash)]
		n := len(c.Files)
		tb := totalBytes(c.Files)
		var cb int64
		var cachedNames []string
		for i, f := range c.Files {
			if cset[strconv.Itoa(i+1)] {
				cb += f.Bytes
				cachedNames = append(cachedNames, f.Name)
			}
		}
		pctF := pct(len(cachedNames), n)
		pctB := pct(int(cb), int(tb))
		gFiles += n
		gCached += len(cachedNames)
		gBytes += tb
		gCachedBytes += cb

		logf("--- %s (%s) ---", c.Platform, c.Name)
		logf("    infohash=%s", c.InfoHash)
		logf("    files: %d total, %d cached (%.1f%%), %d need download", n, len(cachedNames), pctF, n-len(cachedNames))
		logf("    bytes: %.1fGB total, %.1fGB cached (%.1f%%)", float64(tb)/1e9, float64(cb)/1e9, pctB)
		switch {
		case len(cachedNames) == 0:
			logf("    cached games: (none)")
		case len(cachedNames) <= 20:
			logf("    cached games (%d): %s", len(cachedNames), strings.Join(cachedNames, ", "))
		default:
			logf("    cached games (%d): %s, ... (+%d more)", len(cachedNames), strings.Join(cachedNames[:20], ", "), len(cachedNames)-20)
		}
		fmt.Println()
	}
	logf("=== grand total: %d/%d files cached (%.1f%%), %.1f/%.1fGB (%.1f%%) ===",
		gCached, gFiles, pct(gCached, gFiles), float64(gCachedBytes)/1e9, float64(gBytes)/1e9, pct(int(gCachedBytes), int(gBytes)))
}

// buildCollection fetches the platform's collection .torrent and derives
// infohash, display name, magnet, and the file list (name + bytes) in torrent
// order (RD numbers files 1-based in this same order).
func buildCollection(platform string) (infoHash, name string, files []file, err error) {
	torrentURL, ok := app.MinervaTorrentURLs[platform]
	if !ok {
		return "", "", nil, fmt.Errorf("no torrent URL for platform %q", platform)
	}
	req, err := http.NewRequest("GET", torrentURL, nil)
	if err != nil {
		return "", "", nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return "", "", nil, fmt.Errorf("fetch torrent: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", nil, fmt.Errorf("torrent HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, err
	}
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return "", "", nil, fmt.Errorf("parse .torrent: %w", err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return "", "", nil, fmt.Errorf("torrent info: %w", err)
	}
	infoHash = mi.HashInfoBytes().HexString()
	name = info.Name
	for _, fi := range info.UpvertedFiles() {
		path := fi.BestPath()
		base := name
		if len(path) > 0 {
			base = path[len(path)-1]
		}
		files = append(files, file{Name: base, Bytes: fi.Length})
	}
	return infoHash, name, files, nil
}

// instantAvailability asks RD which files of each infohash are already cached on
// its network. One GET per hash (RD rejects 4+ comma-joined hashes with
// wrong_parameter); no account side-effect. A per-hash error is logged and that
// hash is treated as uncached rather than failing the whole recon.
func instantAvailability(ctx context.Context, key string, hashes []string) (map[string]map[string]bool, error) {
	out := make(map[string]map[string]bool)
	for _, h := range hashes {
		u := apiBase + "/torrents/instantAvailability/" + h
		body, err := getJSON(ctx, key, u)
		if err != nil {
			logf("WARN  %s: instantAvailability: %v", h, err)
			continue
		}
		if m := fileSetForHash(body, h); len(m) > 0 {
			out[strings.ToLower(h)] = m
		}
	}
	return out, nil
}

// fileSetForHash extracts the cached RD file IDs for hash from a single-hash
// instantAvailability response. RD keys the response by the requested hash:
// { "<hash>": { "<fileId>": [...], ... } }. A direct { "<fileId>": [...] } form
// (no outer hash key) is also handled in case RD ever returns it.
func fileSetForHash(body []byte, hash string) map[string]bool {
	want := strings.ToLower(hash)
	var outer map[string]json.RawMessage
	if json.Unmarshal(body, &outer) == nil {
		if raw, ok := outer[want]; ok {
			return decodeFileSet(raw)
		}
		for h, raw := range outer {
			if strings.ToLower(h) == want {
				return decodeFileSet(raw)
			}
		}
	}
	return decodeFileSet(body)
}

// decodeFileSet turns one hash's payload into a set of cached file IDs. Response
// shape is inconsistent across torrents: object form { "<fileId>": <non-empty> }
// -> keys are cached file IDs; array form [fileId, ...] -> elements are cached
// file IDs; empty/null/[]/{} -> nothing cached.
func decodeFileSet(raw []byte) map[string]bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" || string(raw) == "{}" {
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		m := make(map[string]bool, len(obj))
		for fid, v := range obj {
			v = bytes.TrimSpace(v)
			if len(v) == 0 || string(v) == "null" || string(v) == "[]" || string(v) == "{}" {
				continue
			}
			m[fid] = true
		}
		return m
	}
	var arr []json.Number
	if json.Unmarshal(raw, &arr) == nil {
		m := make(map[string]bool, len(arr))
		for _, e := range arr {
			m[e.String()] = true
		}
		return m
	}
	return nil
}

// ── HTTP helpers ────────────────────────────────────────────────────────────

// getJSON does a Bearer GET and returns the raw body. Non-2xx is an error
// carrying a short snippet. The key is never included in errors.
func getJSON(ctx context.Context, key, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	return body, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// ── small helpers ───────────────────────────────────────────────────────────

func totalBytes(files []file) int64 {
	var n int64
	for _, f := range files {
		n += f.Bytes
	}
	return n
}

func pct(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return 100 * float64(part) / float64(whole)
}

func sortedPlatforms() []string {
	out := make([]string, 0, len(app.MinervaTorrentURLs))
	for p := range app.MinervaTorrentURLs {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func logf(format string, args ...any) {
	fmt.Printf("[rd-minerva-recon] "+format+"\n", args...)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[rd-minerva-recon] "+format+"\n", args...)
	os.Exit(1)
}
