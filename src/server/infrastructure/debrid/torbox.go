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

// torBox implements Provider against the TorBox API.
// Docs: https://api.torbox.app/  (base /v1/api, Bearer auth, form-data creates,
// requestdl takes the token as a query param).
type torBox struct {
	key  string
	log  LogFunc
	http *http.Client
	base string // overridable in tests
}

func (t *torBox) Name() string { return "TorBox" }

func (t *torBox) baseURL() string {
	if t.base != "" {
		return t.base
	}
	return "https://api.torbox.app/v1/api"
}

// torBox list responses wrap the payload in {success, data}. Torrents and web
// downloads share the same relevant shape.
type tbFile struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	Size      int64  `json:"size"`
}

type tbItem struct {
	ID               int64    `json:"id"`
	Hash             string   `json:"hash"`
	DownloadState    string   `json:"download_state"`
	DownloadFinished bool     `json:"download_finished"`
	DownloadPresent  bool     `json:"download_present"`
	Cached           bool     `json:"cached"`
	Files            []tbFile `json:"files"`
}

type tbCreateResp struct {
	Success bool   `json:"success"`
	Detail  string `json:"detail"`
	Data    struct {
		TorrentID     int64  `json:"torrent_id"`
		WebdownloadID int64  `json:"webdownload_id"`
		Hash          string `json:"hash"`
	} `json:"data"`
}

type tbListResp struct {
	Success bool     `json:"success"`
	Data    []tbItem `json:"data"`
}

type tbSingleResp struct {
	Success bool   `json:"success"`
	Data    tbItem `json:"data"`
}

type tbDLResp struct {
	Success bool   `json:"success"`
	Data    string `json:"data"`
}

func (t *torBox) CacheTorrent(ctx context.Context, magnet, selectName string, wait time.Duration) (string, error) {
	deadline := time.Now().Add(wait)
	form := url.Values{"magnet": {magnet}}
	var created tbCreateResp
	if err := t.postForm(ctx, "/torrents/createtorrent", form, &created); err != nil {
		return "", fmt.Errorf("createtorrent: %w", err)
	}
	id := created.Data.TorrentID
	if id == 0 {
		return "", fmt.Errorf("createtorrent: no torrent_id (%s)", created.Detail)
	}
	item, err := t.pollItem(ctx, "/torrents/mylist", id, deadline)
	if err != nil || item == nil {
		t.deleteItem("/torrents/deletetorrent", id)
		return "", err
	}
	fileID, found := matchTorBoxFile(item.Files, selectName)
	if !found {
		t.log("[WARN] Debrid: TorBox no file matched %q in %d-file torrent - falling back", selectName, len(item.Files))
		t.deleteItem("/torrents/deletetorrent", id)
		return "", nil
	}
	dl, derr := t.requestDL(ctx, "/torrents/requestdl", "torrent_id", id, fileID)
	if derr != nil {
		t.deleteItem("/torrents/deletetorrent", id)
		return "", derr
	}
	return dl, nil
}

func (t *torBox) CacheWebURL(ctx context.Context, srcURL string, wait time.Duration) (string, error) {
	deadline := time.Now().Add(wait)
	form := url.Values{"link": {srcURL}}
	var created tbCreateResp
	if err := t.postForm(ctx, "/webdl/createwebdownload", form, &created); err != nil {
		return "", fmt.Errorf("createwebdownload: %w", err)
	}
	id := created.Data.WebdownloadID
	if id == 0 {
		return "", fmt.Errorf("createwebdownload: no webdownload_id (%s)", created.Detail)
	}
	item, err := t.pollItem(ctx, "/webdl/mylist", id, deadline)
	if err != nil || item == nil {
		t.deleteItem("/webdl/deletewebdownload", id)
		return "", err
	}
	fileID := int64(0)
	if len(item.Files) > 0 {
		fileID = item.Files[0].ID
	}
	dl, derr := t.requestDL(ctx, "/webdl/requestdl", "web_id", id, fileID)
	if derr != nil {
		t.deleteItem("/webdl/deletewebdownload", id)
		return "", derr
	}
	return dl, nil
}

// deleteItem best-effort removes a queued torrent/webdownload so a cache miss or
// timeout doesn't leave a dead entry on the account.
func (t *torBox) deleteItem(path string, id int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	u := fmt.Sprintf("%s%s?id=%d", t.baseURL(), path, id)
	if err := doJSON(ctx, t.http, "GET", u, t.key, nil, "", nil); err != nil {
		t.log("[WARN] Debrid: TorBox cleanup %s (%d) failed: %v", path, id, err)
	}
}

// pollItem polls a mylist endpoint (filtered by id) until the item reports it is
// finished/present, the deadline passes (nil,nil), or ctx is cancelled.
func (t *torBox) pollItem(ctx context.Context, listPath string, id int64, deadline time.Time) (*tbItem, error) {
	for {
		item, err := t.fetchItem(ctx, listPath, id)
		if err != nil {
			return nil, err
		}
		if item != nil && (item.DownloadFinished || item.DownloadPresent || item.Cached ||
			strings.EqualFold(item.DownloadState, "completed") ||
			strings.EqualFold(item.DownloadState, "cached")) {
			return item, nil
		}
		if time.Now().After(deadline) {
			return nil, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// fetchItem reads a single item by id from a mylist endpoint. TorBox's
// ?id=<n> form returns a single object; without it, an array.
func (t *torBox) fetchItem(ctx context.Context, listPath string, id int64) (*tbItem, error) {
	u := fmt.Sprintf("%s%s?id=%d", t.baseURL(), listPath, id)
	var single tbSingleResp
	if err := doJSON(ctx, t.http, "GET", u, t.key, nil, "", &single); err == nil && single.Data.ID == id {
		out := single.Data
		return &out, nil
	}
	// Fall back to the array form and find our id.
	var list tbListResp
	if err := doJSON(ctx, t.http, "GET", t.baseURL()+listPath, t.key, nil, "", &list); err != nil {
		return nil, err
	}
	for i := range list.Data {
		if list.Data[i].ID == id {
			return &list.Data[i], nil
		}
	}
	return nil, nil
}

func (t *torBox) requestDL(ctx context.Context, dlPath, idParam string, id, fileID int64) (string, error) {
	// requestdl requires the token as a query param (not just the header).
	u := fmt.Sprintf("%s%s?token=%s&%s=%d", t.baseURL(), dlPath, url.QueryEscape(t.key), idParam, id)
	if fileID > 0 {
		u += fmt.Sprintf("&file_id=%d", fileID)
	}
	var out tbDLResp
	if err := doJSON(ctx, t.http, "GET", u, t.key, nil, "", &out); err != nil {
		return "", fmt.Errorf("requestdl: %w", err)
	}
	if out.Data == "" {
		return "", fmt.Errorf("requestdl: empty link")
	}
	return out.Data, nil
}

func (t *torBox) postForm(ctx context.Context, path string, form url.Values, out any) error {
	return doJSON(ctx, t.http, "POST", t.baseURL()+path, t.key,
		strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", out)
}

// matchTorBoxFile returns the file id whose name matches selectName by basename,
// or (0, false) when none match (caller falls back to the native source rather
// than guessing the wrong file).
func matchTorBoxFile(files []tbFile, selectName string) (int64, bool) {
	for _, f := range files {
		name := f.Name
		if name == "" {
			name = f.ShortName
		}
		if strings.EqualFold(path.Base(strings.ReplaceAll(name, "\\", "/")), selectName) {
			return f.ID, true
		}
	}
	return 0, false
}
