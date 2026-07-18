// Package debrid caches torrents (and, for TorBox, arbitrary web URLs) on a
// Real-Debrid or TorBox account and returns a direct HTTP link so downloads can
// run over fast HTTP instead of P2P. Callers attempt Debrid first and fall back
// to the native torrent/IA source whenever Debrid is unset or not ready in time.
package debrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultWait is how long to wait for a queued item to become downloadable
// before giving up and falling back to the native source.
const DefaultWait = 60 * time.Second

// TorBoxMaxTorrentSize is TorBox's observed per-torrent size cap. TorBox has no
// per-file selection at createtorrent (it caches the whole torrent or nothing),
// so a collection torrent larger than this is rejected server-side even on
// premium. Used to short-circuit before burning a creation slot on one.
// Tunable if TorBox raises the cap.
const TorBoxMaxTorrentSize = 200 * 1024 * 1024 * 1024 // 200 GiB

// pollInterval is how often provider status is polled while waiting.
// A var (not const) so tests can shorten it.
var pollInterval = 3 * time.Second

// ErrUnsupported is returned by CacheWebURL for providers that cannot cache
// arbitrary URLs (Real-Debrid).
var ErrUnsupported = errors.New("debrid: provider does not support web URL caching")

// LogFunc matches app.App.Logf so the package stays decoupled from app.
type LogFunc func(format string, args ...any)

// Provider is a Debrid backend.
type Provider interface {
	// Name is a short label for logs ("Real-Debrid", "TorBox").
	Name() string
	// CacheTorrent adds a magnet, waits up to `wait` for it to become cached,
	// and returns a direct HTTP link for the file whose basename matches
	// selectName. Returns ("", nil) if it does not become ready in time (a
	// clean signal to fall back). A non-nil error means the attempt itself
	// failed (bad key, network); callers still fall back.
	CacheTorrent(ctx context.Context, magnet, selectName string, wait time.Duration) (string, error)
	// CacheWebURL submits srcURL as a web download and returns a direct link,
	// or ErrUnsupported for providers that can't. Same ("", nil) timeout semantics.
	CacheWebURL(ctx context.Context, srcURL string, wait time.Duration) (string, error)
}

// Magnet builds a magnet URI from a hex infohash plus optional trackers. Both
// providers accept a btih magnet; trackers help them fetch an uncached torrent.
func Magnet(infoHashHex, displayName string, trackers []string) string {
	var b strings.Builder
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(infoHashHex)
	if displayName != "" {
		b.WriteString("&dn=")
		b.WriteString(url.QueryEscape(displayName))
	}
	for _, tr := range trackers {
		if tr = strings.TrimSpace(tr); tr != "" {
			b.WriteString("&tr=")
			b.WriteString(url.QueryEscape(tr))
		}
	}
	return b.String()
}

// Active returns the configured provider, or nil when Debrid is off or the
// selected provider has no key.
func Active(provider, realDebridKey, torBoxKey string, log LogFunc) Provider {
	if log == nil {
		log = func(string, ...any) {}
	}
	client := &http.Client{Timeout: 45 * time.Second}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "realdebrid":
		if strings.TrimSpace(realDebridKey) != "" {
			return &realDebrid{key: strings.TrimSpace(realDebridKey), log: log, http: client}
		}
	case "torbox":
		if strings.TrimSpace(torBoxKey) != "" {
			return &torBox{key: strings.TrimSpace(torBoxKey), log: log, http: client}
		}
	}
	return nil
}

// ── shared HTTP helpers ──────────────────────────────────────────────────────

// doJSON performs an HTTP request with a Bearer token and decodes a JSON body
// into out (when out != nil). Non-2xx responses return an error carrying the
// status and a short body snippet.
func doJSON(ctx context.Context, client *http.Client, method, url, token string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return errors.New(redactToken(err.Error(), token))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		// client.Do returns a *url.Error whose message embeds the request URL.
		// TorBox's requestdl puts the API key in the query string (?token=...),
		// so without redaction the raw key would flow into app logs via the
		// caller's Logf. Strip both the raw and the percent-encoded form.
		return errors.New(redactToken(err.Error(), token))
	}
	defer resp.Body.Close()
	// 4 MB covers large collection torrents (torrents/info can be big); drain the
	// rest so the connection can be reused (keep-alive).
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		// Redact in case a provider ever echoes the request URL (and thus a
		// query-param token) back in an error body.
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, redactToken(snippet, token))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// redactToken strips the API token (raw and percent-encoded forms) from a
// string so it never reaches logs when an error embeds the request URL.
func redactToken(s, token string) string {
	if token == "" {
		return s
	}
	s = strings.ReplaceAll(s, token, "***")
	s = strings.ReplaceAll(s, url.QueryEscape(token), "***")
	return s
}
