package dlxb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"godsend/infrastructure/proxy"
	"godsend/models"
)

// Resolved is a direct HTTP URL aria2c can download.
type Resolved struct {
	URL      string
	Filename string
}

// Resolver turns a stored host link into direct download URLs.
//
// Resolution order: host-specific native flow, then Debrid unrestrict (when a
// provider is configured), then a generic page scrape. Cloudflare challenge
// pages are handed to a FlareSolverr-compatible solver when CFSolverURL is set.
type Resolver struct {
	Client      *http.Client
	UserAgent   string
	CFSolverURL string
	// Debrid returns a direct link for a host URL. nil when Debrid is off.
	Debrid func(ctx context.Context, srcURL string) (directURL string, filename string, err error)
	// Proxy rotates SOCKS5/HTTP proxies and Tor exits between attempts. nil
	// when no proxy environment is configured.
	Proxy *proxy.Pool
	Logf  func(format string, args ...any)
	// AllowPrivate disables the private-address guard (tests only).
	AllowPrivate bool

	// solverClient talks to the local/LAN solver directly, never through the
	// scrape proxy, so a remote proxy never sees or reaches the solver.
	solverClient *http.Client
}

// ErrUnsafeTarget marks a URL that resolves to a private/loopback address or
// uses a non-HTTP scheme. Callers must not fall back to the original URL.
var ErrUnsafeTarget = errors.New("dlxb: unsafe target")

// NewResolver returns a resolver with sane defaults.
func NewResolver() *Resolver {
	r := &Resolver{
		UserAgent:    UserAgent,
		solverClient: &http.Client{Timeout: 120 * time.Second},
	}
	r.Client = r.newClient(nil)
	return r
}

// newClient builds an HTTP client whose redirects are re-checked by
// safeTarget and whose direct dials refuse private addresses.
func (r *Resolver) newClient(t *http.Transport) *http.Client {
	jar, _ := cookiejar.New(nil)
	if t == nil {
		t = &http.Transport{}
	}
	if t.DialContext == nil {
		t.DialContext = r.dialContext
	}
	return &http.Client{
		Timeout:   60 * time.Second,
		Jar:       jar,
		Transport: t,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return fmt.Errorf("dlxb: too many redirects")
			}
			return r.safeTarget(req.URL.String())
		},
	}
}

// SetProxyPool routes the resolver's HTTP client through the pool and rotates
// to a new exit before resolving.
func (r *Resolver) SetProxyPool(p *proxy.Pool) {
	r.Proxy = p
	if p == nil {
		r.Client = r.newClient(nil)
		return
	}
	r.Client = r.newClient(p.Transport(&http.Transport{}))
}

// Resolve resolves one stored link. It may return several files (multi-part
// archives, MediaFire folders, filecrypt containers).
func (r *Resolver) Resolve(ctx context.Context, link models.DlxbLink) ([]Resolved, error) {
	if r.Proxy != nil {
		r.Proxy.Rotate()
	}
	return r.resolve(ctx, link.URL, link.Host, 0)
}

const maxDepth = 3

func (r *Resolver) resolve(ctx context.Context, raw, host string, depth int) ([]Resolved, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("dlxb: link nesting too deep")
	}
	if err := r.safeTarget(raw); err != nil {
		return nil, err
	}
	if host == "" {
		host = ClassifyHost(raw)
	}
	if r.Logf != nil {
		r.Logf("DLXB RESOLVE: %s (%s)", redactURL(raw), host)
	}
	switch host {
	case "direct":
		return []Resolved{{URL: raw, Filename: filenameFromURL(raw)}}, nil
	case "gdrive":
		return r.withDebridFallback(ctx, raw, func() ([]Resolved, error) { return r.gdrive(ctx, raw) })
	case "mediafire":
		return r.withDebridFallback(ctx, raw, func() ([]Resolved, error) { return r.mediafire(ctx, raw) })
	case "gofile":
		return r.withDebridFallback(ctx, raw, func() ([]Resolved, error) { return r.gofile(ctx, raw) })
	case "pixeldrain":
		return r.pixeldrain(raw), nil
	case "1fichier":
		return r.withDebridFallback(ctx, raw, func() ([]Resolved, error) { return r.onefichier(ctx, raw) })
	case "filecrypt":
		// Real-Debrid and TorBox understand filecrypt containers. Their
		// API is the reliable way in; the native page is captcha/session bound.
		if r.Debrid != nil {
			if direct, name, err := r.Debrid(ctx, raw); err == nil && direct != "" {
				return []Resolved{{URL: direct, Filename: name}}, nil
			}
		}
		return r.expand(ctx, raw, depth, r.filecrypt)
	case "anotepad":
		return r.expand(ctx, raw, depth, r.anotepad)
	case "adgate":
		return r.expand(ctx, raw, depth, r.adgate)
	case "shortener":
		return r.shortener(ctx, raw, depth)
	case "mega":
		return r.debridOnly(ctx, raw)
	}
	// Unknown/less common hosts: Debrid knows many of them; otherwise scrape.
	return r.withDebridFallback(ctx, raw, func() ([]Resolved, error) { return r.generic(ctx, raw, depth) })
}

// expand fetches a "container" page (filecrypt, anotepad, the downloadgameps3
// ad gate), extracts the download links it holds, and resolves each.
func (r *Resolver) expand(ctx context.Context, raw string, depth int, fetch func(context.Context, string) ([]models.DlxbLink, error)) ([]Resolved, error) {
	links, err := fetch(ctx, raw)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("dlxb: no download links found on %s", raw)
	}
	var out []Resolved
	var firstErr error
	for _, l := range links {
		files, err := r.resolve(ctx, l.URL, l.Host, depth+1)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, files...)
	}
	if len(out) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("dlxb: no downloadable link on %s", raw)
	}
	return out, nil
}

func (r *Resolver) withDebridFallback(ctx context.Context, raw string, native func() ([]Resolved, error)) ([]Resolved, error) {
	files, err := native()
	if err == nil {
		return files, nil
	}
	if r.Debrid != nil {
		if direct, name, derr := r.Debrid(ctx, raw); derr == nil && direct != "" {
			if r.Logf != nil {
				r.Logf("DLXB RESOLVE: Debrid handled %s", raw)
			}
			return []Resolved{{URL: direct, Filename: name}}, nil
		}
	}
	return nil, err
}

func (r *Resolver) debridOnly(ctx context.Context, raw string) ([]Resolved, error) {
	if r.Debrid == nil {
		return nil, fmt.Errorf("dlxb: %s needs Real-Debrid or TorBox (set GODSEND_DEBRID_PROVIDER + key) or download it in a browser", raw)
	}
	direct, name, err := r.Debrid(ctx, raw)
	if err != nil {
		return nil, err
	}
	return []Resolved{{URL: direct, Filename: name}}, nil
}

// ── HTTP helpers ─────────────────────────────────────────────────────────────

func (r *Resolver) get(ctx context.Context, raw string) (*http.Response, []byte, error) {
	attempts := 1
	if r.Proxy != nil {
		attempts = 3 // a rate-limited exit is retried on a fresh one
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		resp, body, err := r.getOnce(ctx, raw)
		if err == nil {
			if r.Proxy != nil && resp.StatusCode == 403 {
				r.Proxy.Rotate()
				lastErr = fmt.Errorf("HTTP 403 from %s", raw)
				continue
			}
			return resp, body, nil
		}
		lastErr = err
		// 429 (rate limit) is worth retrying from a new exit.
		if r.Proxy != nil && strings.Contains(err.Error(), "HTTP 429") {
			r.Proxy.Rotate()
			continue
		}
		return resp, body, err
	}
	return nil, nil, lastErr
}

func (r *Resolver) getOnce(ctx context.Context, raw string) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", r.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	resp.Body.Close()
	if err != nil {
		return nil, nil, err
	}
	if isCFChallenge(resp.StatusCode, body) {
		solved, final, serr := r.solveCF(ctx, raw)
		if serr != nil {
			return nil, nil, fmt.Errorf("Cloudflare challenge on %s (set GODSEND_CF_SOLVER_URL): %w", raw, serr)
		}
		return &http.Response{StatusCode: 200, Request: &http.Request{URL: mustURL(final)}, Header: http.Header{}}, solved, nil
	}
	if resp.StatusCode >= 400 && resp.StatusCode != 403 {
		return resp, body, fmt.Errorf("HTTP %d from %s", resp.StatusCode, raw)
	}
	return resp, body, nil
}

// safeTarget rejects non-HTTP schemes and private/loopback addresses so a
// tampered cache file cannot make the backend fetch LAN or localhost URLs.
func (r *Resolver) safeTarget(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: bad url %q: %v", ErrUnsafeTarget, raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: unsupported scheme %q", ErrUnsafeTarget, u.Scheme)
	}
	if r.AllowPrivate {
		return nil
	}
	host := strings.ToLower(strings.TrimSuffix(strings.Trim(u.Hostname(), "[]"), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("%w: refusing local address %q", ErrUnsafeTarget, raw)
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("%w: refusing private address %q", ErrUnsafeTarget, raw)
		}
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

func (r *Resolver) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if r.AllowPrivate {
		return (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, network, addr)
	}
	return guardedDialContext(ctx, network, addr)
}

// guardedDialContext resolves a target and refuses to connect when it resolves
// only to loopback/private/link-local addresses (DNS-rebinding guard).
func guardedDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	d := &net.Dialer{Timeout: 30 * time.Second}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return nil, fmt.Errorf("%w: refusing private address %s", ErrUnsafeTarget, addr)
		}
		return d.DialContext(ctx, network, addr)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, a := range ips {
		if isPrivateIP(a.IP) {
			continue
		}
		return d.DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
	}
	return nil, fmt.Errorf("%w: %s resolves only to private addresses", ErrUnsafeTarget, host)
}

// Preflight follows redirects with the guarded client and returns the final
// URL, so a downloader that follows redirects on its own (aria2c) cannot be
// steered at a private address by a redirect the resolver never inspected.
func (r *Resolver) Preflight(ctx context.Context, raw string) (string, error) {
	if err := r.safeTarget(raw); err != nil {
		return "", err
	}
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		req, err := http.NewRequestWithContext(ctx, method, raw, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", r.UserAgent)
		if method == http.MethodGet {
			req.Header.Set("Range", "bytes=0-0")
		}
		resp, err := r.Client.Do(req)
		if err != nil {
			if errors.Is(err, ErrUnsafeTarget) {
				return "", err
			}
			continue
		}
		final := resp.Request.URL.String()
		resp.Body.Close()
		if err := r.safeTarget(final); err != nil {
			return "", err
		}
		if resp.StatusCode < 400 && resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusNotImplemented {
			return final, nil
		}
	}
	return "", fmt.Errorf("dlxb: preflight failed for %s", raw)
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}
	u.User = nil
	s := u.String()
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		return &url.URL{}
	}
	return u
}

func isCFChallenge(status int, body []byte) bool {
	if status != 403 && status != 503 {
		return false
	}
	return bytes.Contains(body, []byte("Just a moment")) ||
		bytes.Contains(body, []byte("cf-challenge")) ||
		bytes.Contains(body, []byte("challenge-platform"))
}

type solverReq struct {
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"maxTimeout"`
}

type solverResp struct {
	Solution struct {
		Response string `json:"response"`
		URL      string `json:"url"`
	} `json:"solution"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// solveCF posts the URL to a FlareSolverr/Byparr-compatible solver and returns
// the rendered HTML and final URL.
func (r *Resolver) solveCF(ctx context.Context, raw string) ([]byte, string, error) {
	if strings.TrimSpace(r.CFSolverURL) == "" {
		return nil, "", fmt.Errorf("no solver configured")
	}
	var lastMsg string
	// A solver can answer "Challenge solved!" with an empty body while it
	// finishes a first-pass challenge; one retry covers that.
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		payload, _ := json.Marshal(solverReq{Cmd: "request.get", URL: raw, MaxTimeout: 90000})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.CFSolverURL, "/")+"/v1", bytes.NewReader(payload))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := r.solverClient.Do(req)
		if err != nil {
			return nil, "", err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, "", fmt.Errorf("solver HTTP %d", resp.StatusCode)
		}
		var s solverResp
		if err := json.Unmarshal(body, &s); err != nil {
			return nil, "", err
		}
		if s.Status == "ok" && s.Solution.Response != "" &&
			!bytes.Contains([]byte(s.Solution.Response), []byte("Just a moment")) &&
			!bytes.Contains([]byte(s.Solution.Response), []byte("_cf_chl_opt")) {
			if s.Solution.URL == "" {
				s.Solution.URL = raw
			}
			return []byte(s.Solution.Response), s.Solution.URL, nil
		}
		lastMsg = s.Message
	}
	return nil, "", fmt.Errorf("solver: %s", lastMsg)
}

func isAttachment(resp *http.Response) bool {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		return true
	}
	if strings.Contains(ct, "application/octet-stream") || strings.HasPrefix(ct, "application/zip") ||
		strings.HasPrefix(ct, "application/x-rar") || strings.HasPrefix(ct, "application/x-7z") {
		return true
	}
	return false
}

// ── host flows ───────────────────────────────────────────────────────────────

var (
	driveConfirmRe = regexp.MustCompile(`confirm=([0-9A-Za-z_-]+)`)
	mfFileRe       = regexp.MustCompile(`https://www\.mediafire\.com/file/[^"'<>\s]+`)
	mfFolderRe     = regexp.MustCompile(`https://www\.mediafire\.com/file/([^/"']+)/([^/"']+)/file`)
	mfDirectRe     = regexp.MustCompile(`https://download[^"'<>\s]*\.mediafire\.com/[^"'<>\s]+`)
	anchorHrefRe   = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	urlRe          = regexp.MustCompile(`https?://[^\s"'<>]+`)
	f1LinkRe       = regexp.MustCompile(`https://a-[0-9]+\.1fichier\.com/[^"'<>\s]+|https://1fichier\.com/d/[^"'<>\s]+`)
)

func driveID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if id := u.Query().Get("id"); id != "" {
		return id
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "d" {
			return parts[i+1]
		}
	}
	return ""
}

func (r *Resolver) gdrive(ctx context.Context, raw string) ([]Resolved, error) {
	if strings.Contains(raw, "/drive/folders/") {
		return nil, fmt.Errorf("dlxb: Google Drive folder links are not resolved automatically; open %s in a browser", raw)
	}
	id := driveID(raw)
	if id == "" {
		return nil, fmt.Errorf("dlxb: no Google Drive file id in %s", raw)
	}
	probe := "https://drive.google.com/uc?export=download&id=" + id
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probe, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", r.UserAgent)
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if isAttachment(resp) {
		return []Resolved{{URL: probe, Filename: filenameFromDisposition(resp)}}, nil
	}
	token := "t"
	if m := driveConfirmRe.FindSubmatch(body); m != nil {
		token = string(m[1])
	}
	direct := fmt.Sprintf("https://drive.usercontent.google.com/download?id=%s&export=download&confirm=%s", id, token)
	return []Resolved{{URL: direct, Filename: ""}}, nil
}

func (r *Resolver) mediafire(ctx context.Context, raw string) ([]Resolved, error) {
	resp, body, err := r.get(ctx, raw)
	if err != nil {
		return nil, err
	}
	if isAttachment(resp) {
		return []Resolved{{URL: resp.Request.URL.String(), Filename: filenameFromDisposition(resp)}}, nil
	}
	text := string(body)
	if strings.Contains(raw, "/folder/") {
		matches := mfFolderRe.FindAllStringSubmatch(text, -1)
		seen := map[string]bool{}
		var out []Resolved
		for _, m := range matches {
			fileURL := "https://www.mediafire.com/file/" + m[1] + "/" + m[2] + "/file"
			if seen[fileURL] {
				continue
			}
			seen[fileURL] = true
			files, err := r.mediafire(ctx, fileURL)
			if err != nil {
				continue
			}
			out = append(out, files...)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("dlxb: no files found in MediaFire folder %s", raw)
		}
		return out, nil
	}
	if m := mfDirectRe.FindString(text); m != "" {
		return []Resolved{{URL: m, Filename: filenameFromURL(m)}}, nil
	}
	if m := mfFileRe.FindString(text); m != "" && m != raw {
		return r.mediafire(ctx, m)
	}
	return nil, fmt.Errorf("dlxb: no MediaFire direct link on %s", raw)
}

func (r *Resolver) gofile(ctx context.Context, raw string) ([]Resolved, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	id := ""
	if len(parts) >= 2 && parts[0] == "d" {
		id = parts[1]
	}
	if id == "" {
		return nil, fmt.Errorf("dlxb: no gofile content id in %s", raw)
	}
	api := "https://api.gofile.io/contents/" + id + "?wt=4fd6sg89d7s6"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", r.UserAgent)
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Children map[string]struct {
				Name string `json:"name"`
				Link string `json:"link"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.Status != "ok" {
		return nil, fmt.Errorf("dlxb: gofile API status %q", payload.Status)
	}
	var out []Resolved
	for _, c := range payload.Data.Children {
		if c.Link != "" {
			out = append(out, Resolved{URL: c.Link, Filename: c.Name})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("dlxb: gofile folder %s is empty", id)
	}
	return out, nil
}

func (r *Resolver) pixeldrain(raw string) []Resolved {
	u, err := url.Parse(raw)
	if err != nil {
		return []Resolved{{URL: raw, Filename: filenameFromURL(raw)}}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "u" {
		return []Resolved{{URL: "https://pixeldrain.com/api/file/" + parts[1] + "?download", Filename: ""}}
	}
	return []Resolved{{URL: raw, Filename: filenameFromURL(raw)}}
}

func (r *Resolver) onefichier(ctx context.Context, raw string) ([]Resolved, error) {
	resp, body, err := r.get(ctx, raw)
	if err != nil {
		return nil, err
	}
	if isAttachment(resp) {
		return []Resolved{{URL: resp.Request.URL.String(), Filename: filenameFromDisposition(resp)}}, nil
	}
	if resp.StatusCode == 404 || bytes.Contains(body, []byte("File not found")) {
		return nil, fmt.Errorf("dlxb: 1fichier file not found (removed or expired)")
	}
	// Free downloads post the form with ad=1, then the page holds the link.
	form := url.Values{"ad": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, raw, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", r.UserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", raw)
	post, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}
	postBody, _ := io.ReadAll(io.LimitReader(post.Body, 8<<20))
	post.Body.Close()
	if isCFChallenge(post.StatusCode, postBody) {
		solved, _, serr := r.solveCF(ctx, raw)
		if serr == nil {
			postBody = solved
		}
	}
	if isAttachment(post) {
		return []Resolved{{URL: raw, Filename: filenameFromDisposition(post)}}, nil
	}
	if m := f1LinkRe.FindString(string(postBody)); m != "" {
		return []Resolved{{URL: m, Filename: filenameFromURL(m)}}, nil
	}
	if m := f1LinkRe.FindString(string(body)); m != "" {
		return []Resolved{{URL: m, Filename: filenameFromURL(m)}}, nil
	}
	return nil, fmt.Errorf("dlxb: 1fichier did not return a free download link (wait timer or premium required)")
}

func (r *Resolver) filecrypt(ctx context.Context, raw string) ([]models.DlxbLink, error) {
	_, body, err := r.get(ctx, raw)
	if err != nil {
		return nil, err
	}
	var links []models.DlxbLink
	seen := map[string]bool{}
	for _, m := range anchorHrefRe.FindAllStringSubmatch(string(body), -1) {
		href := strings.TrimSpace(html.UnescapeString(m[1]))
		host := ClassifyHost(href)
		if host == "" || host == "filecrypt" || seen[href] {
			continue
		}
		seen[href] = true
		links = append(links, models.DlxbLink{Host: host, URL: href, Label: strings.TrimSpace(m[2])})
	}
	return links, nil
}

func (r *Resolver) anotepad(ctx context.Context, raw string) ([]models.DlxbLink, error) {
	_, body, err := r.get(ctx, raw)
	if err != nil {
		return nil, err
	}
	var links []models.DlxbLink
	seen := map[string]bool{}
	for _, m := range urlRe.FindAllString(string(body), -1) {
		href := strings.TrimRight(html.UnescapeString(m), ".,;)")
		host := ClassifyHost(href)
		if host == "" || seen[href] {
			continue
		}
		seen[href] = true
		links = append(links, models.DlxbLink{Host: host, URL: href, Label: host})
	}
	return links, nil
}

func (r *Resolver) adgate(ctx context.Context, raw string) ([]models.DlxbLink, error) {
	_, body, err := r.get(ctx, raw)
	if err != nil {
		return nil, err
	}
	var links []models.DlxbLink
	seen := map[string]bool{}
	for _, m := range anchorHrefRe.FindAllStringSubmatch(string(body), -1) {
		href := strings.TrimSpace(html.UnescapeString(m[1]))
		host := ClassifyHost(href)
		if host == "" || host == "adgate" || seen[href] {
			continue
		}
		seen[href] = true
		links = append(links, models.DlxbLink{Host: host, URL: href, Label: strings.TrimSpace(m[2])})
	}
	return links, nil
}

func (r *Resolver) shortener(ctx context.Context, raw string, depth int) ([]Resolved, error) {
	resp, body, err := r.get(ctx, raw)
	if err != nil {
		return nil, err
	}
	final := resp.Request.URL.String()
	if final != raw {
		host := ClassifyHost(final)
		if host != "" && host != "shortener" {
			return r.resolve(ctx, final, host, depth+1)
		}
	}
	// Ad shorteners often hide the destination behind a JS form; try the raw
	// links in the body as a last resort.
	for _, m := range urlRe.FindAllString(string(body), -1) {
		host := ClassifyHost(m)
		if host != "" && host != "shortener" {
			return r.resolve(ctx, m, host, depth+1)
		}
	}
	return nil, fmt.Errorf("dlxb: shortener %s did not reveal a direct link", raw)
}

func (r *Resolver) generic(ctx context.Context, raw string, depth int) ([]Resolved, error) {
	resp, body, err := r.get(ctx, raw)
	if err != nil {
		return nil, err
	}
	if isAttachment(resp) {
		return []Resolved{{URL: resp.Request.URL.String(), Filename: filenameFromDisposition(resp)}}, nil
	}
	seen := map[string]bool{}
	for _, m := range anchorHrefRe.FindAllStringSubmatch(string(body), -1) {
		href := strings.TrimSpace(html.UnescapeString(m[1]))
		host := ClassifyHost(href)
		if host == "" || host == "web" || seen[href] {
			continue
		}
		seen[href] = true
		if host == "direct" {
			return []Resolved{{URL: href, Filename: filenameFromURL(href)}}, nil
		}
		if depth < maxDepth {
			files, err := r.resolve(ctx, href, host, depth+1)
			if err == nil && len(files) > 0 {
				return files, nil
			}
		}
	}
	// Some hosts put a meta refresh to the file.
	if m := regexp.MustCompile(`(?i)http-equiv=["']refresh["'][^>]*content=["'][^"']*url=([^"']+)`).FindStringSubmatch(string(body)); m != nil {
		if depth < maxDepth {
			return r.resolve(ctx, html.UnescapeString(strings.TrimSpace(m[1])), "", depth+1)
		}
	}
	// Last resort: hand the URL to aria2c; it follows redirects on its own and
	// will fail loudly if the host serves an HTML page instead of a file.
	return []Resolved{{URL: resp.Request.URL.String(), Filename: filenameFromURL(raw)}}, nil
}

// ── small utilities ──────────────────────────────────────────────────────────

func filenameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	name := path.Base(u.Path)
	if name == "/" || name == "." {
		return ""
	}
	return name
}

func filenameFromDisposition(resp *http.Response) string {
	cd := resp.Header.Get("Content-Disposition")
	if cd == "" {
		return filenameFromURL(resp.Request.URL.String())
	}
	if i := strings.Index(strings.ToLower(cd), "filename*="); i >= 0 {
		v := strings.Trim(cd[i+len("filename*="):], `" ;`)
		if j := strings.Index(v, "''"); j >= 0 {
			v = v[j+2:]
		}
		if dec, err := url.PathUnescape(v); err == nil {
			return dec
		}
	}
	if i := strings.Index(strings.ToLower(cd), "filename="); i >= 0 {
		return strings.Trim(cd[i+len("filename="):], `" ;`)
	}
	return ""
}
