// One-off probe: run the app's REAL TorBox CacheTorrent path against a real
// game file in a Minerva collection, to verify the app gets a direct link from
// cached entries (instead of hitting the >200GB rejection and falling back to
// aria2c).
//
// It imports godsend/infrastructure/debrid and calls debrid.Active +
// Provider.CacheTorrent exactly as src/server/services/pipeline/debrid.go does,
// so the result reflects what a real user's download flow would experience.
//
// Usage (from src/server):
//
//	go run ./cmd/torbox-cacheprobe <platform> [fileIndex]
//
// platform in {xbox360, xbox, digital, xbla, dlc, xblig, games}.
// fileIndex picks which file in the collection torrent to request (default 0).
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"godsend/app"
	"godsend/infrastructure/debrid"
)

func main() {
	key := strings.TrimSpace(os.Getenv("GODSEND_TORBOX_KEY"))
	if key == "" {
		fmt.Fprintln(os.Stderr, "GODSEND_TORBOX_KEY not set")
		os.Exit(1)
	}
	platform := "games"
	if len(os.Args) > 1 {
		platform = os.Args[1]
	}
	fileIdx := 0
	if len(os.Args) > 2 {
		fmt.Sscanf(os.Args[2], "%d", &fileIdx)
	}

	torrentURL, ok := app.MinervaTorrentURLs[platform]
	if !ok {
		fail("no torrent URL for platform %q", platform)
	}
	req, _ := http.NewRequest("GET", torrentURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		fail("fetch torrent: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		fail("parse .torrent: %v", err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		fail("unmarshal info: %v", err)
	}
	files := info.UpvertedFiles()
	if fileIdx >= len(files) {
		fail("fileIndex %d out of range (%d files)", fileIdx, len(files))
	}
	selectName := filepath.Base(filepath.Join(files[fileIdx].Path...))
	infoHash := mi.HashInfoBytes().HexString()
	magnet := debrid.Magnet(infoHash, info.Name, trackers(mi))

	logf("platform=%s infohash=%s files=%d", platform, infoHash, len(files))
	logf("requesting file[%d]: %q (%d bytes)", fileIdx, selectName, files[fileIdx].Length)
	logf("calling app debrid.CacheTorrent (TorBox) with 60s wait...")

	prov := debrid.Active("torbox", "", key, func(f string, a ...any) {
		logf("  app-log: "+f, a...)
	})
	if prov == nil {
		fail("debrid.Active returned nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	link, err := prov.CacheTorrent(ctx, magnet, selectName, 60*time.Second)

	fmt.Println()
	switch {
	case err != nil:
		logf("RESULT: ERROR (app would fall back to aria2c) -> %v", err)
	case link == "":
		logf("RESULT: not ready in 60s (app would fall back to aria2c)")
	default:
		logf("RESULT: SUCCESS - app gets a direct HTTP link:")
		logf("  %s", link)
	}
}

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

func logf(format string, args ...any) {
	fmt.Printf("[cacheprobe] "+format+"\n", args...)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[cacheprobe] "+format+"\n", args...)
	os.Exit(1)
}