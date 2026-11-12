// One-off operator tool: cache the `games` Minerva collection to Real-Debrid and
// probe whether RD accepts the 3 huge collections (for per-file caching later).
//
// Minerva ships one collection .torrent per platform; each game is a file inside
// its platform's collection (shared infohash). Only `games` (14GB / 35 files) is
// feasible to cache wholesale; the other 3 distinct collections are 1.3 / 17.5 /
// 31.9 TB, so those are per-file-on-demand only. This tool:
//
//	Phase A - cache `games`: addMagnet -> selectFiles all -> poll until
//	  downloaded. ~14GB to the RD account; LEFT on the account (cached = goal).
//	  Once RD has the data, any later per-game selectFiles (incl. the app's
//	  CacheTorrent flow) is instant.
//	Phase B - probe huge: addMagnet each of digital/xbox/xbox360 WITHOUT
//	  selectFiles (no download), poll until magnet_conversion resolves, report
//	  accepted (RD parsed the file list) vs rejected (file-count limits), then
//	  DELETE. Tells us if per-file caching of the huge collections is viable.
//
// Idempotent: lists account torrents first and reuses an existing torrent_id for
// the same hash instead of re-addMagnet, so re-runs don't create duplicates.
//
// Not part of the app build. Run from src/server:
//
//	GODSEND_REALDEBRID_KEY=<key> go run ./cmd/rd-minerva-cache
//
// Optional: RD_CACHE_POLL_MAX (Phase A per-torrent poll cap, default 30m). The
// key is never logged.
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
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"godsend/app"
	"godsend/infrastructure/debrid"
)

const apiBase = "https://api.real-debrid.com/rest/1.0"

var cachePollMax = 30 * time.Minute

const probePollMax = 3 * time.Minute

// rdTorrent mirrors the fields we need from /torrents/info.
type rdTorrent struct {
	ID       string  `json:"id"`
	Hash     string  `json:"hash"`
	Filename string  `json:"filename"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	Files    []struct {
		ID   int64  `json:"id"`
		Path string `json:"path"`
	} `json:"files"`
}

// target is one collection to cache (Role="cache") or probe (Role="probe").
type target struct {
	Role     string
	Platform string
	InfoHash string
	Magnet   string
}

func main() {
	key := strings.TrimSpace(os.Getenv("GODSEND_REALDEBRID_KEY"))
	if key == "" {
		fail("GODSEND_REALDEBRID_KEY env var not set")
	}
	if d := strings.TrimSpace(os.Getenv("RD_CACHE_POLL_MAX")); d != "" {
		if dur, err := time.ParseDuration(d); err == nil {
			cachePollMax = dur
		}
	}

	// 1. Build the 4 distinct collection magnets (games + 3 huge).
	want := []string{"games", "digital", "xbox", "xbox360"}
	var targets []target
	for _, p := range want {
		ih, _, magnet, err := buildCollection(p)
		if err != nil {
			logf("WARN  %s: %v (skipping)", p, err)
			continue
		}
		role := "probe"
		if p == "games" {
			role = "cache"
		}
		targets = append(targets, target{Role: role, Platform: p, InfoHash: ih, Magnet: magnet})
		logf("built %s: infohash=%s role=%s", p, ih, role)
	}
	if len(targets) == 0 {
		fail("no collection torrents parsed")
	}

	// 2. Phase A: cache `games`.
	for _, t := range targets {
		if t.Role != "cache" {
			continue
		}
		cacheGames(context.Background(), key, t)
	}

	// 3. Phase B: probe the huge collections.
	for _, t := range targets {
		if t.Role != "probe" {
			continue
		}
		probeHuge(context.Background(), key, t)
	}

	fmt.Println()
	logf("=== done ===")
}

// cacheGames addMagnets the `games` collection, selects all files, and polls
// until RD reports downloaded (or the poll cap). Leaves it on the account.
// Resume-aware: if the torrent already exists (re-run) and is already selected/
// downloading, it skips selectFiles so an in-progress download isn't disrupted.
func cacheGames(ctx context.Context, key string, t target) {
	id, err := ensureMagnet(ctx, key, t.InfoHash, t.Magnet)
	if err != nil {
		logf("FAIL  %s: addMagnet: %v", t.Platform, err)
		return
	}
	info, err := getTorrent(ctx, key, id)
	if err != nil {
		logf("FAIL  %s: info: %v", t.Platform, err)
		return
	}
	logf("cache %s: id=%s status=%s progress=%.0f%%", t.Platform, id, info.Status, info.Progress)

	switch info.Status {
	case "downloaded":
		logf("OK    %s: already cached on RD (downloaded) - left on account", t.Platform)
		return
	case "error", "magnet_error", "virus", "dead":
		logf("FAIL  %s: RD status=%s (rejected)", t.Platform, info.Status)
		return
	}

	// If still converting, wait for the file list to be known.
	if info.Status == "magnet_conversion" {
		p, err := pollInfo(ctx, key, id, probePollMax, func(t rdTorrent) bool {
			return t.Status != "magnet_conversion" && t.Status != ""
		})
		if err != nil {
			logf("FAIL  %s: poll file list: %v", t.Platform, err)
			return
		}
		if p == nil {
			logf("WAIT  %s: magnet_conversion not resolved in %s (id=%s; re-run to resume)", t.Platform, probePollMax, id)
			return
		}
		if isTerminalError(p.Status) {
			logf("FAIL  %s: RD status=%s (rejected)", t.Platform, p.Status)
			return
		}
		info = p
	}

	// Only select files if RD is still waiting for a selection. If already
	// downloading/queued, the selection was made (possibly by a prior run) -
	// re-selecting could disrupt the in-progress download.
	if info.Status == "waiting_files_selection" {
		logf("cache %s: RD parsed %d files; selecting all", t.Platform, len(info.Files))
		if err := selectFiles(ctx, key, id, "all"); err != nil {
			logf("FAIL  %s: selectFiles all: %v", t.Platform, err)
			return
		}
	}

	// Poll until downloaded (or terminal error / poll cap).
	final, err := pollInfo(ctx, key, id, cachePollMax, func(t rdTorrent) bool {
		switch t.Status {
		case "downloaded", "error", "magnet_error", "virus", "dead":
			return true
		}
		return false
	})
	if err != nil {
		logf("FAIL  %s: poll downloaded: %v", t.Platform, err)
		return
	}
	if final == nil {
		logf("WAIT  %s: not downloaded in %s (id=%s; re-run to resume)", t.Platform, cachePollMax, id)
		return
	}
	if final.Status == "downloaded" {
		logf("OK    %s: cached on RD (progress=%.0f%%, %d files) - left on account",
			t.Platform, final.Progress, len(final.Files))
		return
	}
	logf("FAIL  %s: RD status=%s (not downloaded)", t.Platform, final.Status)
}

// probeHuge addMagnets a huge collection WITHOUT selectFiles (no download), waits
// for magnet_conversion to resolve, reports whether RD accepted it (and how many
// files RD parsed), then deletes it.
func probeHuge(ctx context.Context, key string, t target) {
	id, err := ensureMagnet(ctx, key, t.InfoHash, t.Magnet)
	if err != nil {
		logf("PROBE %s: addMagnet: %v", t.Platform, err)
		return
	}
	info, err := pollInfo(ctx, key, id, probePollMax, func(t rdTorrent) bool {
		return t.Status != "magnet_conversion" && t.Status != ""
	})
	if err != nil {
		logf("PROBE %s: poll: %v (deleting id=%s)", t.Platform, err, id)
		deleteTorrent(ctx, key, id)
		return
	}
	if info == nil {
		logf("PROBE %s: magnet_conversion not resolved in %s (deleting id=%s)", t.Platform, probePollMax, id)
		deleteTorrent(ctx, key, id)
		return
	}
	switch {
	case info.Status == "waiting_files_selection":
		logf("PROBE %s: ACCEPTED - RD parsed %d files (status=%s); per-file caching viable (deleting id=%s, no download)",
			t.Platform, len(info.Files), info.Status, id)
	case isTerminalError(info.Status):
		logf("PROBE %s: REJECTED - RD status=%s (per-file caching NOT viable on RD)", t.Platform, info.Status)
	default:
		logf("PROBE %s: status=%s, %d files (deleting id=%s)", t.Platform, info.Status, len(info.Files), id)
	}
	deleteTorrent(ctx, key, id)
}

// ensureMagnet returns the torrent_id for infoHash, reusing an existing account
// torrent if present (idempotent re-runs), otherwise addMagnets.
func ensureMagnet(ctx context.Context, key, infoHash, magnet string) (string, error) {
	if id, ok := findTorrentByHash(ctx, key, infoHash); ok {
		return id, nil
	}
	var added struct {
		ID string `json:"id"`
	}
	form := url.Values{"magnet": {magnet}}
	if err := postForm(ctx, key, apiBase+"/torrents/addMagnet", form, &added); err != nil {
		return "", err
	}
	if added.ID == "" {
		return "", fmt.Errorf("addMagnet: empty id")
	}
	return added.ID, nil
}

// findTorrentByHash lists the account's torrents and returns the id whose hash
// matches infoHash (idempotency for re-runs / torrents already on the account).
// RD's list-all endpoint is GET /torrents (GET /torrents/info without an id
// 404s with unknown_method).
func findTorrentByHash(ctx context.Context, key, infoHash string) (string, bool) {
	want := strings.ToLower(infoHash)
	body, err := getJSON(ctx, key, apiBase+"/torrents")
	if err != nil {
		return "", false
	}
	var list []rdTorrent
	if err := json.Unmarshal(body, &list); err != nil {
		return "", false
	}
	for _, t := range list {
		if strings.ToLower(t.Hash) == want {
			return t.ID, true
		}
	}
	return "", false
}

// getTorrent fetches one torrent's current state.
func getTorrent(ctx context.Context, key, id string) (*rdTorrent, error) {
	var info rdTorrent
	if err := doJSON(ctx, key, "GET", apiBase+"/torrents/info/"+id, nil, "", &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// selectFiles selects files on a torrent. files="all" selects every file.
func selectFiles(ctx context.Context, key, id, files string) error {
	form := url.Values{"files": {files}}
	return postForm(ctx, key, apiBase+"/torrents/selectFiles/"+id, form, nil)
}

// deleteTorrent removes a torrent from the account (best-effort cleanup).
func deleteTorrent(ctx context.Context, key, id string) {
	if _, err := doReq(ctx, key, "DELETE", apiBase+"/torrents/delete/"+id, nil, ""); err != nil {
		logf("WARN  delete %s: %v", id, err)
	}
}

// pollInfo polls /torrents/info/{id} until pred is satisfied or the deadline
// passes (returns nil,nil). Stops early on terminal-error statuses.
func pollInfo(ctx context.Context, key, id string, cap time.Duration, pred func(rdTorrent) bool) (*rdTorrent, error) {
	deadline := time.Now().Add(cap)
	for {
		if time.Now().After(deadline) {
			return nil, nil
		}
		var info rdTorrent
		if err := doJSON(ctx, key, "GET", apiBase+"/torrents/info/"+id, nil, "", &info); err != nil {
			return nil, err
		}
		if isTerminalError(info.Status) {
			out := info
			return &out, nil
		}
		if pred(info) {
			out := info
			return &out, nil
		}
		time.Sleep(5 * time.Second)
	}
}

func isTerminalError(status string) bool {
	switch status {
	case "error", "magnet_error", "virus", "dead":
		return true
	}
	return false
}

// buildCollection fetches the platform's collection .torrent and derives
// infohash and a magnet with deduped trackers.
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

// ── HTTP helpers ────────────────────────────────────────────────────────────

// doJSON does a Bearer request and decodes JSON into out (when out != nil). The
// key is never included in errors.
func doJSON(ctx context.Context, key, method, u string, body io.Reader, contentType string, out any) error {
	raw, err := doReq(ctx, key, method, u, body, contentType)
	if err != nil {
		return err
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func doReq(ctx context.Context, key, method, u string, body io.Reader, contentType string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	return raw, nil
}

func postForm(ctx context.Context, key, u string, form url.Values, out any) error {
	return doJSON(ctx, key, "POST", u, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", out)
}

func getJSON(ctx context.Context, key, u string) ([]byte, error) {
	return doReq(ctx, key, "GET", u, nil, "")
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// ── small helpers ───────────────────────────────────────────────────────────

func logf(format string, args ...any) {
	fmt.Printf("[rd-minerva-cache] "+format+"\n", args...)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[rd-minerva-cache] "+format+"\n", args...)
	os.Exit(1)
}
