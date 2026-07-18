// debrid.go - Debrid (Real-Debrid / TorBox) acceleration for the torrent and IA
// download paths. Both attempt the active provider first and fall back to the
// native source (aria2c / direct IA) on any miss, error, or timeout.
package pipeline

import (
	"context"
	"fmt"
	"time"

	"godsend/app"
	"godsend/infrastructure/debrid"
	"godsend/infrastructure/torrent"
)

// activeDebrid returns the configured Debrid provider, or nil when Debrid is off
// or the selected provider has no key.
func (s *Service) activeDebrid() debrid.Provider {
	return debrid.Active(s.App.DebridProvider, s.App.RealDebridKey, s.App.TorBoxKey, s.App.Logf)
}

// debridCtx builds a context with a little headroom beyond the wait window so a
// slow-but-progressing final API call isn't cut off mid-flight.
func debridCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), debrid.DefaultWait+30*time.Second)
}

// debridTorrentDownloader returns a torrent.DebridDownloader, or nil when no
// provider is configured. The closure caches the magnet on Debrid, waits up to
// debrid.DefaultWait, and on success downloads the direct link over HTTP.
func (s *Service) debridTorrentDownloader(gameName string) torrent.DebridDownloader {
	prov := s.activeDebrid()
	if prov == nil {
		return nil
	}
	return func(infoHashHex, displayName string, trackers []string, selectName, destPath string, collectionSize int64) (bool, error) {
		// TorBox caches the whole torrent with no per-file selection, so a
		// collection over its size cap is rejected server-side (after burning a
		// createtorrent slot). Skip it up front and use P2P. Real-Debrid selects
		// per file, so the cap doesn't apply there.
		if prov.Name() == "TorBox" && collectionSize > debrid.TorBoxMaxTorrentSize {
			s.App.Logf("[INFO] Debrid (TorBox): collection %.1f GB exceeds TorBox's %d GB torrent limit - using P2P",
				float64(collectionSize)/1e9, debrid.TorBoxMaxTorrentSize/1e9)
			return false, nil
		}
		magnet := debrid.Magnet(infoHashHex, displayName, trackers)
		s.App.LogStatus(gameName, "Processing", fmt.Sprintf("Debrid (%s): caching torrent…", prov.Name()))
		ctx, cancel := debridCtx()
		defer cancel()
		direct, err := prov.CacheTorrent(ctx, magnet, selectName, debrid.DefaultWait)
		if err != nil {
			return false, err
		}
		if direct == "" {
			s.App.Logf("[INFO] Debrid (%s): torrent not ready in %s - using P2P", prov.Name(), debrid.DefaultWait)
			return false, nil
		}
		s.App.LogStatus(gameName, "Processing", fmt.Sprintf("Debrid (%s): cached, downloading via HTTP…", prov.Name()))
		if err := s.Download.DownloadWithProgress(direct, destPath, gameName, ""); err != nil {
			return false, fmt.Errorf("debrid HTTP download: %w", err)
		}
		s.App.Logf("[INFO] Debrid (%s): downloaded %s via HTTP", prov.Name(), selectName)
		return true, nil
	}
}

// downloadIAOrDebrid downloads srcURL to dest. When the active provider supports
// web downloads (TorBox), it first tries to cache srcURL on Debrid and download
// the direct link; on any miss it downloads srcURL directly. Real-Debrid returns
// ErrUnsupported and falls straight through to the (already parallel) IA path.
func (s *Service) downloadIAOrDebrid(srcURL, dest, gameName string) error {
	if prov := s.activeDebrid(); prov != nil {
		ctx, cancel := debridCtx()
		defer cancel()
		s.App.LogStatus(gameName, "Processing", fmt.Sprintf("Debrid (%s): caching download…", prov.Name()))
		direct, err := prov.CacheWebURL(ctx, srcURL, debrid.DefaultWait)
		switch {
		case err == debrid.ErrUnsupported:
			// Provider can't cache arbitrary URLs (Real-Debrid) - use direct IA.
		case err != nil:
			s.App.Logf("[WARN] Debrid (%s): IA cache failed (%v) - using direct IA", prov.Name(), err)
		case direct == "":
			s.App.Logf("[INFO] Debrid (%s): IA item not ready in %s - using direct IA", prov.Name(), debrid.DefaultWait)
		default:
			s.App.LogStatus(gameName, "Processing", fmt.Sprintf("Debrid (%s): cached, downloading via HTTP…", prov.Name()))
			if derr := s.Download.DownloadWithProgress(direct, dest, gameName, ""); derr == nil {
				s.App.Logf("[INFO] Debrid (%s): downloaded IA item via HTTP", prov.Name())
				return nil
			} else {
				s.App.Logf("[WARN] Debrid (%s): HTTP download failed (%v) - using direct IA", prov.Name(), derr)
			}
		}
	}
	return s.Download.DownloadWithProgress(srcURL, dest, gameName, app.IADownloadBase)
}
