package debrid

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// realDebrid implements Provider against the Real-Debrid REST API.
// Docs: https://api.real-debrid.com/  (base /rest/1.0, Bearer auth).
type realDebrid struct {
	key  string
	log  LogFunc
	http *http.Client
	base string // overridable in tests; defaults to the live API
}

func (r *realDebrid) Name() string { return "Real-Debrid" }

func (r *realDebrid) baseURL() string {
	if r.base != "" {
		return r.base
	}
	return "https://api.real-debrid.com/rest/1.0"
}

type rdFile struct {
	ID       int64  `json:"id"`
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	Selected int    `json:"selected"`
}

type rdTorrentInfo struct {
	ID       string   `json:"id"`
	Filename string   `json:"filename"`
	Status   string   `json:"status"`
	Progress float64  `json:"progress"`
	Files    []rdFile `json:"files"`
	Links    []string `json:"links"`
}

// CacheWebURL is unsupported on Real-Debrid (it only unrestricts known hosters).
func (r *realDebrid) CacheWebURL(context.Context, string, time.Duration) (string, error) {
	return "", ErrUnsupported
}

func (r *realDebrid) CacheTorrent(ctx context.Context, magnet, selectName string, wait time.Duration) (string, error) {
	deadline := time.Now().Add(wait)

	// 1. Add the magnet.
	var added struct {
		ID string `json:"id"`
	}
	form := url.Values{"magnet": {magnet}}
	if err := r.post(ctx, "/torrents/addMagnet", form, &added); err != nil {
		return "", fmt.Errorf("addMagnet: %w", err)
	}
	if added.ID == "" {
		return "", fmt.Errorf("addMagnet: empty id")
	}
	id := added.ID

	// 2. Wait for magnet conversion so the file list is known, then select the
	//    target file. If we can't match selectName, fall back to the native
	//    source rather than selecting all files (which would make unrestricted
	//    Links[0] an arbitrary file, not the target).
	info, err := r.pollUntil(ctx, id, deadline, func(t rdTorrentInfo) bool {
		return t.Status != "magnet_conversion" && len(t.Files) > 0
	})
	if err != nil || info == nil {
		r.deleteTorrent(id)
		return "", err
	}
	fileID, found := matchRDFile(info.Files, selectName)
	if !found {
		r.log("[WARN] Debrid: Real-Debrid no file matched %q in %d-file torrent - falling back", selectName, len(info.Files))
		r.deleteTorrent(id)
		return "", nil
	}
	if err := r.selectFileByID(ctx, id, fileID); err != nil {
		r.log("[WARN] Debrid: Real-Debrid selectFiles failed: %v", err)
		r.deleteTorrent(id)
		return "", nil
	}

	// 3. Wait until fully cached/downloaded on RD's side.
	final, err := r.pollUntil(ctx, id, deadline, func(t rdTorrentInfo) bool {
		switch t.Status {
		case "downloaded":
			return true
		case "error", "magnet_error", "virus", "dead":
			return true // terminal; handled below
		}
		return false
	})
	if err != nil {
		r.deleteTorrent(id)
		return "", err
	}
	if final == nil || final.Status != "downloaded" || len(final.Links) == 0 {
		if final != nil && final.Status != "downloaded" && final.Status != "" {
			r.log("[WARN] Debrid: Real-Debrid torrent status=%s - falling back", final.Status)
		}
		r.deleteTorrent(id)
		return "", nil
	}

	// 4. Unrestrict the single selected file's link into a direct download URL.
	//    With exactly one file selected, final.Links has one entry - the target.
	direct, err := r.unrestrict(ctx, final.Links[0])
	if err != nil {
		r.deleteTorrent(id)
		return "", fmt.Errorf("unrestrict: %w", err)
	}
	return direct, nil
}

// matchRDFile returns the file id whose basename matches selectName, or
// (0, false) when none match.
func matchRDFile(files []rdFile, selectName string) (string, bool) {
	for _, f := range files {
		if strings.EqualFold(path.Base(strings.ReplaceAll(f.Path, "\\", "/")), selectName) {
			return fmt.Sprintf("%d", f.ID), true
		}
	}
	return "", false
}

// selectFileByID selects a single file by its RD file id.
func (r *realDebrid) selectFileByID(ctx context.Context, id, fileID string) error {
	form := url.Values{"files": {fileID}}
	return r.post(ctx, "/torrents/selectFiles/"+id, form, nil)
}

func (r *realDebrid) unrestrict(ctx context.Context, link string) (string, error) {
	var out struct {
		Download string `json:"download"`
	}
	form := url.Values{"link": {link}}
	if err := r.post(ctx, "/unrestrict/link", form, &out); err != nil {
		return "", err
	}
	if out.Download == "" {
		return "", fmt.Errorf("empty download url")
	}
	return out.Download, nil
}

// pollUntil polls /torrents/info/{id} until pred is satisfied, the deadline
// passes (returns nil,nil), or ctx is cancelled.
func (r *realDebrid) pollUntil(ctx context.Context, id string, deadline time.Time, pred func(rdTorrentInfo) bool) (*rdTorrentInfo, error) {
	for {
		// Check the deadline before fetching so we stop the instant wait elapses,
		// rather than issuing one extra API call after the deadline passes during
		// the sleep below.
		if time.Now().After(deadline) {
			return nil, nil
		}
		var info rdTorrentInfo
		if err := doJSON(ctx, r.http, "GET", r.baseURL()+"/torrents/info/"+id, r.key, nil, "", &info); err != nil {
			return nil, err
		}
		if pred(info) {
			out := info
			return &out, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (r *realDebrid) post(ctx context.Context, path string, form url.Values, out any) error {
	return doJSON(ctx, r.http, "POST", r.baseURL()+path, r.key,
		strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", out)
}

func (r *realDebrid) deleteTorrent(id string) {
	// Best-effort cleanup so abandoned attempts don't clutter the account.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := doJSON(ctx, r.http, "DELETE", r.baseURL()+"/torrents/delete/"+id, r.key, nil, "", nil); err != nil {
		r.log("[WARN] Debrid: Real-Debrid cleanup of %s failed: %v", id, err)
	}
}
