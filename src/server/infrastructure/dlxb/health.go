// health.go - link liveness checks for the dlxbgame source.
//
// The checks are deliberately cheap: HEAD or Range: bytes=0-0, never a full
// download. Hosts that require the Cloudflare solver to even answer (adgate,
// filecrypt, MediaFire behind a challenge) are reported as unknown here and
// left for the real resolver, so a health sweep cannot accidentally launch
// thousands of solver sessions.
package dlxb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"godsend/models"
)

const (
	StatusAlive   = "alive"
	StatusDead    = "dead"
	StatusUnknown = "unknown"
)

// Check reports whether a stored link still points at something real.
// It returns StatusAlive, StatusDead, or StatusUnknown plus a short reason.
func (r *Resolver) Check(ctx context.Context, link models.DlxbLink) (string, string) {
	switch link.Host {
	case "gdrive":
		return r.checkGDrive(ctx, link.URL)
	case "1fichier":
		return r.check1Fichier(ctx, link.URL)
	case "pixeldrain":
		return r.checkPixeldrain(ctx, link.URL)
	case "gofile":
		return r.checkGofile(ctx, link.URL)
	case "shortener":
		return r.checkShortener(ctx, link.URL)
	case "mediafire":
		// MediaFire answers unauthenticated clients with a Cloudflare
		// challenge, which is not evidence of a dead file.
		return StatusUnknown, "Cloudflare challenge"
	case "adgate", "filecrypt", "mega":
		return StatusUnknown, "needs the Cloudflare solver / Debrid"
	case "direct", "web", "anotepad", "ranoz", "vikingfile", "akirabox", "rootz",
		"buzzheavier", "datanodes", "send", "frdl", "qiwi":
		return r.checkHTTP(ctx, link.URL)
	}
	return StatusUnknown, "no checker for host"
}

// ── generic HTTP ─────────────────────────────────────────────────────────────

func (r *Resolver) checkHTTP(ctx context.Context, raw string) (string, string) {
	resp, body, err := r.probe(ctx, http.MethodHead, raw, "")
	if err != nil {
		return StatusUnknown, err.Error()
	}
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		resp, body, err = r.probe(ctx, http.MethodGet, raw, "bytes=0-0")
		if err != nil {
			return StatusUnknown, err.Error()
		}
	}
	code := resp.StatusCode
	if code == 404 || code == 410 {
		return StatusDead, fmt.Sprintf("HTTP %d", code)
	}
	if code >= 500 {
		return StatusUnknown, fmt.Sprintf("HTTP %d", code)
	}
	if code == 403 || code == 401 || code == 429 {
		return StatusUnknown, fmt.Sprintf("HTTP %d", code)
	}
	if code >= 200 && code < 400 {
		if isAttachment(resp) {
			return StatusAlive, ""
		}
		// A page that embeds a known dead marker is dead; a plain HTML page is
		// ambiguous (could be an interstitial), so leave it unknown.
		lower := strings.ToLower(string(body))
		if strings.Contains(lower, "file not found") || strings.Contains(lower, "file has been removed") {
			return StatusDead, "page says the file is gone"
		}
		return StatusUnknown, "not a file response"
	}
	return StatusUnknown, fmt.Sprintf("HTTP %d", code)
}

func (r *Resolver) probe(ctx context.Context, method, raw, rangeHeader string) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", r.UserAgent)
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	// Read a small prefix: enough for the dead markers, never a whole file.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	resp.Body.Close()
	return resp, body, nil
}

// ── host-specific ────────────────────────────────────────────────────────────

func (r *Resolver) checkGDrive(ctx context.Context, raw string) (string, string) {
	id := driveID(raw)
	if id == "" {
		return StatusDead, "no file id"
	}
	resp, body, err := r.probe(ctx, http.MethodGet, "https://drive.google.com/uc?export=download&id="+url.QueryEscape(id), "")
	if err != nil {
		return StatusUnknown, err.Error()
	}
	if resp.StatusCode == 404 {
		return StatusDead, "HTTP 404"
	}
	if resp.StatusCode == 403 {
		lower := strings.ToLower(string(body))
		if strings.Contains(lower, "too many") || strings.Contains(lower, "quota") {
			return StatusUnknown, "Drive rate limit"
		}
		return StatusDead, "HTTP 403 (removed or not shared)"
	}
	lower := strings.ToLower(string(body))
	if strings.Contains(lower, "does not exist") || strings.Contains(lower, "can't access") ||
		strings.Contains(lower, "cannot be found") || strings.Contains(lower, "file you have requested") {
		return StatusDead, "Drive says the file is gone"
	}
	if isAttachment(resp) || bytes.Contains(body, []byte("confirm=")) ||
		strings.Contains(lower, "quota") || bytes.Contains(body, []byte("uc-download-link")) {
		return StatusAlive, ""
	}
	return StatusUnknown, "Drive page without a file marker"
}

func (r *Resolver) check1Fichier(ctx context.Context, raw string) (string, string) {
	resp, body, err := r.probe(ctx, http.MethodGet, raw, "")
	if err != nil {
		return StatusUnknown, err.Error()
	}
	if resp.StatusCode == 404 || bytes.Contains(body, []byte("File not found")) {
		return StatusDead, "file not found"
	}
	if resp.StatusCode == 200 && bytes.Contains(body, []byte(`id="f1"`)) {
		return StatusAlive, ""
	}
	if resp.StatusCode == 403 {
		return StatusUnknown, "HTTP 403"
	}
	return StatusUnknown, fmt.Sprintf("HTTP %d", resp.StatusCode)
}

func (r *Resolver) checkPixeldrain(ctx context.Context, raw string) (string, string) {
	u, err := url.Parse(raw)
	if err != nil {
		return StatusDead, "bad url"
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return StatusUnknown, "unrecognised pixeldrain url"
	}
	resp, _, err := r.probe(ctx, http.MethodGet, "https://pixeldrain.com/api/file/"+parts[1]+"/info", "")
	if err != nil {
		return StatusUnknown, err.Error()
	}
	switch resp.StatusCode {
	case 200:
		return StatusAlive, ""
	case 404:
		return StatusDead, "HTTP 404"
	}
	return StatusUnknown, fmt.Sprintf("HTTP %d", resp.StatusCode)
}

func (r *Resolver) checkGofile(ctx context.Context, raw string) (string, string) {
	u, err := url.Parse(raw)
	if err != nil {
		return StatusDead, "bad url"
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "d" {
		return StatusUnknown, "unrecognised gofile url"
	}
	resp, body, err := r.probe(ctx, http.MethodGet, "https://api.gofile.io/contents/"+parts[1]+"?wt=4fd6sg89d7s6", "")
	if err != nil {
		return StatusUnknown, err.Error()
	}
	if resp.StatusCode == 404 || bytes.Contains(body, []byte(`"status":"error-404"`)) {
		return StatusDead, "content not found"
	}
	if bytes.Contains(body, []byte(`"status":"ok"`)) {
		return StatusAlive, ""
	}
	return StatusUnknown, fmt.Sprintf("HTTP %d", resp.StatusCode)
}

func (r *Resolver) checkShortener(ctx context.Context, raw string) (string, string) {
	resp, body, err := r.probe(ctx, http.MethodGet, raw, "")
	if err != nil {
		return StatusUnknown, err.Error()
	}
	final := resp.Request.URL.String()
	if final != raw {
		host := ClassifyHost(final)
		if host != "" && host != "shortener" && host != "adgate" && host != "filecrypt" && host != "mediafire" {
			if status, why := r.Check(ctx, models.DlxbLink{Host: host, URL: final}); status != StatusUnknown {
				return status, why
			}
		}
	}
	if resp.StatusCode == 404 || resp.StatusCode == 410 {
		return StatusDead, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	if strings.Contains(strings.ToLower(string(body)), "link not found") ||
		strings.Contains(strings.ToLower(string(body)), "link expired") {
		return StatusDead, "shortener says the link is gone"
	}
	return StatusUnknown, "shortener destination unclear"
}

// CheckAll runs Check over every link with a small worker pool, mutating the
// cache in place. Only one goroutine writes the cache map: workers return
// results to a collector. logf may be nil.
func (r *Resolver) CheckAll(ctx context.Context, cache *models.DlxbCache, workers int, logf func(string, ...any)) map[string][3]int {
	return r.CheckAllFiltered(ctx, cache, workers, nil, logf)
}

// CheckAllFiltered is CheckAll with an optional host allow-list (nil = all).
func (r *Resolver) CheckAllFiltered(ctx context.Context, cache *models.DlxbCache, workers int, onlyHosts map[string]bool, logf func(string, ...any)) map[string][3]int {
	if workers < 1 {
		workers = 8
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	type job struct {
		entryKey string
		index    int
		link     models.DlxbLink
	}
	type result struct {
		job
		status string
		why    string
	}
	jobs := make(chan job)
	results := make(chan result, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
				status, why := r.Check(cctx, j.link)
				cancel()
				results <- result{job: j, status: status, why: why}
			}
		}()
	}
	// Snapshot the jobs before workers start: the collector writes the entry
	// map, and Go maps cannot be ranged while another goroutine writes them.
	var jobList []job
	for key, entry := range cache.Entries {
		for i, l := range entry.Links {
			if onlyHosts != nil && !onlyHosts[l.Host] {
				continue
			}
			// Only a definitive alive/dead result is cached for a week; an
			// unknown (rate limit, challenge) is retried on the next pass.
			if (l.Status == StatusAlive || l.Status == StatusDead) && time.Since(l.CheckedAt) < 7*24*time.Hour {
				continue
			}
			jobList = append(jobList, job{entryKey: key, index: i, link: l})
		}
	}
	go func() {
		for _, j := range jobList {
			select {
			case jobs <- j:
			case <-ctx.Done():
				close(jobs)
				wg.Wait()
				close(results)
				return
			}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	counts := map[string][3]int{} // host -> alive/dead/unknown
	done := 0
	start := time.Now()
	for res := range results {
		e := cache.Entries[res.entryKey]
		if res.index < len(e.Links) {
			e.Links[res.index].Status = res.status
			e.Links[res.index].CheckedAt = time.Now()
			cache.Entries[res.entryKey] = e
		}
		c := counts[res.link.Host]
		switch res.status {
		case StatusAlive:
			c[0]++
		case StatusDead:
			c[1]++
			if res.why != "" {
				logf("DEAD %s %s: %s", res.link.Host, redactURL(res.link.URL), res.why)
			}
		default:
			c[2]++
		}
		counts[res.link.Host] = c
		done++
		if r.Proxy != nil && done > 0 && done%200 == 0 {
			// Rotate exits periodically so a rate-limited host gets a fresh IP.
			r.Proxy.Rotate()
		}
		if done%250 == 0 {
			logf("DLXB HEALTH: %d links checked (%s)", done, time.Since(start).Round(time.Second))
		}
	}
	logf("DLXB HEALTH: done - %d links in %s", done, time.Since(start).Round(time.Second))
	return counts
}
