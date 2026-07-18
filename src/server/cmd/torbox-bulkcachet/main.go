// One-off operator tool: pre-cache every Minerva collection torrent on TorBox
// so any user's Debrid flow returns an instant cached link for any game.
//
// Minerva ships one collection .torrent per platform (each game is a file inside
// its platform's collection, sharing one infohash). So caching all Minerva items
// = createtorrent on the ~7 collection magnets. TorBox caches the whole
// collection; torrent caches are swarm-shared, so this benefits every user, not
// just this account.
//
// Flow: fetch+parse each collection .torrent -> batch checkcached (skips
// already-cached, costs no creation quota) -> createtorrent the rest (<=7 calls,
// spaced to respect TorBox's 60/hour creation limit) -> poll mylist until each
// reports cached/completed. Resumable via a state file; re-runs skip fetched /
// already-cached / submitted collections and resume polling.
//
// Not part of the app build. Run from src/server:
//
//	GODSEND_TORBOX_KEY=<key> go build -o ../../dist/torbox-bulkcachet ./cmd/torbox-bulkcachet
//	GODSEND_TORBOX_KEY=<key> ../../dist/torbox-bulkcachet
//
// Optional env: TORBOX_BULKCACHE_DIR (state dir, default .torbox-bulkcachet),
// TORBOX_POLL_MAX (per-torrent poll cap, default 24h). The key is never logged.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"godsend/app"
	"godsend/infrastructure/debrid"
)

const apiBase = "https://api.torbox.app/v1/api"

var pollMax = 24 * time.Hour

// collection is one platform's collection torrent + its TorBox submission state.
type collection struct {
	Platform  string `json:"platform"`
	InfoHash  string `json:"info_hash"`
	Name      string `json:"name"`
	Magnet    string `json:"magnet"`
	TorrentID int64  `json:"torrent_id,omitempty"`
	Cached    bool   `json:"cached"`
}

// tbItem mirrors the TorBox mylist item shape (see debrid/torbox.go).
type tbItem struct {
	ID               int64  `json:"id"`
	Hash             string `json:"hash"`
	DownloadState    string `json:"download_state"`
	DownloadFinished bool   `json:"download_finished"`
	DownloadPresent  bool   `json:"download_present"`
	Cached           bool   `json:"cached"`
}

type tbCreateResp struct {
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
	Data    struct {
		TorrentID int64  `json:"torrent_id"`
		Hash      string `json:"hash"`
	} `json:"data"`
}

func main() {
	key := strings.TrimSpace(os.Getenv("GODSEND_TORBOX_KEY"))
	if key == "" {
		fail("GODSEND_TORBOX_KEY env var not set")
	}
	if d := strings.TrimSpace(os.Getenv("TORBOX_POLL_MAX")); d != "" {
		if dur, err := time.ParseDuration(d); err == nil {
			pollMax = dur
		}
	}
	stateDir := strings.TrimSpace(os.Getenv("TORBOX_BULKCACHE_DIR"))
	if stateDir == "" {
		stateDir = ".torbox-bulkcachet"
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		fail("mkdir state dir: %v", err)
	}
	statePath := filepath.Join(stateDir, "state.json")

	cols := loadState(statePath)

	// 1. Fetch + parse each platform's collection torrent (skip if already built).
	for _, p := range sortedPlatforms() {
		if c := findCol(cols, p); c != nil && c.InfoHash != "" {
			continue
		}
		ih, name, magnet, err := buildCollection(p)
		if err != nil {
			logf("WARN  %s: %v (skipping)", p, err)
			continue
		}
		logf("built %s: infohash=%s name=%q", p, ih, name)
		cols = append(cols, collection{Platform: p, InfoHash: ih, Name: name, Magnet: magnet})
		saveState(statePath, cols)
	}

	// 2. Batch checkcached for collections we haven't submitted and don't know cached.
	var toCheck []string
	for i := range cols {
		if cols[i].TorrentID == 0 && !cols[i].Cached {
			toCheck = append(toCheck, cols[i].InfoHash)
		}
	}
	if len(toCheck) > 0 {
		cached, err := checkCached(context.Background(), key, toCheck)
		if err != nil {
			logf("WARN  checkcached: %v", err)
		}
		for i := range cols {
			if cached[cols[i].InfoHash] {
				cols[i].Cached = true
				logf("cached %s: already on TorBox (skipping submit)", cols[i].Platform)
			}
		}
		saveState(statePath, cols)
	}

	// 3. createtorrent the rest, spaced >=61s to stay under TorBox's 60/hour cap.
	// Platforms sharing a collection torrent (same infohash) inherit the first
	// submission's torrent_id instead of re-submitting the same magnet.
	for i := range cols {
		c := &cols[i]
		if c.Cached || c.TorrentID != 0 {
			continue
		}
		if id := siblingTorrentID(cols, c.InfoHash, i); id != 0 {
			c.TorrentID = id
			logf("skip  %s: shares collection with a submitted platform (id=%d)", c.Platform, id)
			saveState(statePath, cols)
			continue
		}
		id, err := createTorrent(context.Background(), key, c.Magnet, c.InfoHash)
		if err != nil {
			logf("WARN  %s: createtorrent failed: %v", c.Platform, err)
			continue
		}
		c.TorrentID = id
		logf("sent  %s: torrent_id=%d", c.Platform, id)
		saveState(statePath, cols)
		time.Sleep(61 * time.Second)
	}

	// 4. Poll mylist to completion for every submitted, not-yet-cached collection.
	pollDeadline := time.Now().Add(pollMax)
	logf("polling to completion (cap %s; re-run anytime to resume)", pollMax)
	pollLoop(statePath, cols, key, pollDeadline)

	// 5. Summary.
	fmt.Println()
	logf("=== summary ===")
	cached, submitted, failed := 0, 0, 0
	for _, c := range cols {
		switch {
		case c.Cached:
			cached++
			logf("  %-8s cached        (id=%d)", c.Platform, c.TorrentID)
		case c.TorrentID != 0:
			submitted++
			logf("  %-8s still caching (id=%d, poll cap hit; re-run to resume)", c.Platform, c.TorrentID)
		default:
			failed++
			logf("  %-8s NOT submitted", c.Platform)
		}
	}
	logf("%d cached, %d still caching, %d failed", cached, submitted, failed)
}

// buildCollection fetches the platform's collection .torrent and derives
// infohash, display name, and a magnet with deduped trackers.
func buildCollection(platform string) (infoHash, name, magnet string, err error) {
	torrentURL, ok := app.MinervaTorrentURLs[platform]
	if !ok {
		return "", "", "", fmt.Errorf("no torrent URL for platform %q", platform)
	}
	req, err := http.NewRequest("GET", torrentURL, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("fetch torrent: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", "", fmt.Errorf("torrent HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", err
	}
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return "", "", "", fmt.Errorf("parse .torrent: %w", err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return "", "", "", fmt.Errorf("torrent info: %w", err)
	}
	infoHash = mi.HashInfoBytes().HexString()
	name = info.Name
	magnet = debrid.Magnet(infoHash, name, trackers(mi))
	return infoHash, name, magnet, nil
}

// trackers flattens Announce + AnnounceList, deduped case-insensitively.
func trackers(mi *metainfo.MetaInfo) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		k := strings.ToLower(s)
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, s)
	}
	add(mi.Announce)
	for _, tier := range mi.AnnounceList {
		for _, u := range tier {
			add(u)
		}
	}
	return out
}

// checkCached asks TorBox which of the given infohashes are already cached
// (network-wide). One batched GET; no creation-quota cost.
func checkCached(ctx context.Context, key string, hashes []string) (map[string]bool, error) {
	cached := make(map[string]bool)
	u := apiBase + "/torrents/checkcached?format=list&hash=" + url.QueryEscape(strings.Join(hashes, ","))
	body, _, err := getJSON(ctx, key, u)
	if err != nil {
		return cached, err
	}
	var resp struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return cached, fmt.Errorf("decode checkcached: %w", err)
	}
	var objs []struct{ Hash string `json:"hash"` }
	if err := json.Unmarshal(resp.Data, &objs); err == nil {
		for _, o := range objs {
			if o.Hash != "" {
				cached[strings.ToLower(o.Hash)] = true
			}
		}
		return cached, nil
	}
	var strs []string
	if err := json.Unmarshal(resp.Data, &strs); err == nil {
		for _, s := range strs {
			if s != "" {
				cached[strings.ToLower(s)] = true
			}
		}
	}
	return cached, nil
}

// createTorrent submits a magnet. On a duplicate (torrent already on this
// account) it recovers the existing id by listing mylist and matching the hash.
func createTorrent(ctx context.Context, key, magnet, infoHash string) (int64, error) {
	form := url.Values{"magnet": {magnet}}
	body, status, err := postForm(ctx, key, apiBase+"/torrents/createtorrent", form)
	if err != nil {
		if status == http.StatusTooManyRequests {
			return 0, fmt.Errorf("rate limited (429); retry later")
		}
		// Duplicate on account -> recover from mylist by hash.
		if id, ok := findTorrentByHash(ctx, key, infoHash); ok {
			return id, nil
		}
		return 0, err
	}
	var r tbCreateResp
	if err := json.Unmarshal(body, &r); err != nil {
		return 0, fmt.Errorf("decode createtorrent: %w", err)
	}
	if r.Data.TorrentID == 0 {
		// createtorrent can return success with no id when the magnet is already
		// on the account; recover from mylist.
		if id, ok := findTorrentByHash(ctx, key, infoHash); ok {
			return id, nil
		}
		return 0, fmt.Errorf("no torrent_id (%s)", r.Detail)
	}
	return r.Data.TorrentID, nil
}

// findTorrentByHash lists the account's torrents and returns the id whose hash
// matches infoHash (recovery for duplicates / re-runs without state).
func findTorrentByHash(ctx context.Context, key, infoHash string) (int64, bool) {
	want := strings.ToLower(infoHash)
	body, _, err := getJSON(ctx, key, apiBase+"/torrents/mylist")
	if err != nil {
		return 0, false
	}
	var resp struct {
		Data []tbItem `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, false
	}
	for _, it := range resp.Data {
		if strings.ToLower(it.Hash) == want {
			return it.ID, true
		}
	}
	return 0, false
}

// itemCached reports whether a mylist item is finished/present/cached.
func itemCached(it *tbItem) bool {
	return it.DownloadFinished || it.DownloadPresent || it.Cached ||
		strings.EqualFold(it.DownloadState, "completed") ||
		strings.EqualFold(it.DownloadState, "cached")
}

// fetchItem reads one torrent from mylist by id (single-object form, with an
// array-form fallback), mirroring debrid/torbox.go fetchItem.
func fetchItem(ctx context.Context, key string, id int64) (*tbItem, error) {
	body, _, err := getJSON(ctx, key, fmt.Sprintf("%s/torrents/mylist?id=%d", apiBase, id))
	if err != nil {
		return nil, err
	}
	var single struct {
		Data tbItem `json:"data"`
	}
	if err := json.Unmarshal(body, &single); err == nil && single.Data.ID == id {
		out := single.Data
		return &out, nil
	}
	var list struct {
		Data []tbItem `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		// Retry the unfiltered array form.
		body2, _, err2 := getJSON(ctx, key, apiBase+"/torrents/mylist")
		if err2 != nil {
			return nil, err2
		}
		if err := json.Unmarshal(body2, &list); err != nil {
			return nil, err
		}
	}
	for i := range list.Data {
		if list.Data[i].ID == id {
			return &list.Data[i], nil
		}
	}
	return nil, nil
}

// pollLoop polls every submitted, not-yet-cached torrent until all are cached or
// deadline elapses. Saves state as each completes. Re-running resumes from state.
func pollLoop(statePath string, cols []collection, key string, deadline time.Time) {
	for {
		if time.Now().After(deadline) {
			logf("poll cap reached; stopping (re-run to resume polling)")
			return
		}
		pending := false
		for i := range cols {
			c := &cols[i]
			if c.Cached || c.TorrentID == 0 {
				continue
			}
			pending = true
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			it, err := fetchItem(ctx, key, c.TorrentID)
			cancel()
			if err != nil {
				logf("WARN  %s: mylist poll: %v", c.Platform, err)
				continue
			}
			if it != nil && itemCached(it) {
				c.Cached = true
				logf("done  %s: cached on TorBox (state=%s)", c.Platform, it.DownloadState)
				saveState(statePath, cols)
			}
		}
		// Anything still pending after this pass?
		anyPending := false
		for i := range cols {
			if !cols[i].Cached && cols[i].TorrentID != 0 {
				anyPending = true
				break
			}
		}
		if !anyPending {
			return
		}
		if !pending {
			// Every pending torrent errored this pass; stop to avoid spinning.
			logf("no progress this pass; stopping poll loop (re-run to resume)")
			return
		}
		logf("still caching; sleeping 60s before next poll...")
		time.Sleep(60 * time.Second)
	}
}

// ── HTTP helpers ────────────────────────────────────────────────────────────

// getJSON does a Bearer GET and returns the raw body. Non-2xx (except 429) is
// an error carrying a short snippet; 429 returns (nil, 429, err) so callers can
// branch. The key is never included in errors.
func getJSON(ctx context.Context, key, u string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, resp.StatusCode, fmt.Errorf("HTTP 429 rate limited")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	return body, resp.StatusCode, nil
}

func postForm(ctx context.Context, key, u string, form url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, resp.StatusCode, fmt.Errorf("HTTP 429 rate limited")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	return body, resp.StatusCode, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// ── state + small helpers ───────────────────────────────────────────────────

func loadState(path string) []collection {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cols []collection
	_ = json.Unmarshal(b, &cols)
	return cols
}

func saveState(path string, cols []collection) {
	b, err := json.MarshalIndent(cols, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		logf("WARN  save state: %v", err)
		return
	}
	_ = os.Rename(tmp, path)
}

func findCol(cols []collection, platform string) *collection {
	for i := range cols {
		if cols[i].Platform == platform {
			return &cols[i]
		}
	}
	return nil
}

// siblingTorrentID returns the torrent_id of an earlier-iterated collection
// sharing infoHash, so platforms that map to the same collection torrent don't
// each burn a createtorrent slot.
func siblingTorrentID(cols []collection, infoHash string, upTo int) int64 {
	for i := 0; i < upTo; i++ {
		if cols[i].InfoHash == infoHash && cols[i].TorrentID != 0 {
			return cols[i].TorrentID
		}
	}
	return 0
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
	fmt.Printf("[torbox-bulkcachet] "+format+"\n", args...)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[torbox-bulkcachet] "+format+"\n", args...)
	os.Exit(1)
}