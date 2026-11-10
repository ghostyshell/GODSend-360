// Package dlxb scrapes dlxbgame.com and resolves its download hosts.
//
// The site is WordPress, so the scrape uses its public REST API instead of
// paging through HTML: /wp-json/wp/v2/posts?categories=<id>&per_page=100&page=N
// returns post content (with the raw host links) as JSON. One scrape covers the
// two Xbox 360 categories the app exposes: ISO (disc images) and Jtag/RGH.
package dlxb

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"godsend/app"
	"godsend/infrastructure/proxy"
	"godsend/models"
)

// UserAgent identifies the app to the site; keep it honest and stable.
const UserAgent = "GODsend-360 (+https://github.com/ghostyshell/GODSend-360)"

// Options configures a scrape.
type Options struct {
	Client      *http.Client
	Concurrency int
	PerPage     int
	APIURL      string // overrides app.DlxbAPIURL (tests)
	Proxy       *proxy.Pool
	Logf        func(format string, args ...any)
}

// Scrape downloads every post in the ISO and Jtag/RGH categories and returns a
// cache ready to persist. A nil Options.Client gets a 60s client.
func Scrape(ctx context.Context, opt Options) (*models.DlxbCache, error) {
	if opt.Client == nil {
		opt.Client = &http.Client{Timeout: 120 * time.Second}
		if opt.Proxy != nil {
			opt.Client.Transport = opt.Proxy.Transport(&http.Transport{})
		}
	}
	if opt.Concurrency < 1 {
		opt.Concurrency = 4
	}
	if opt.PerPage < 1 || opt.PerPage > 100 {
		opt.PerPage = 100
	}
	logf := opt.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if opt.APIURL == "" {
		opt.APIURL = app.DlxbAPIURL
	}

	cache := &models.DlxbCache{
		Schema:    app.DlxbCacheSchema,
		Site:      app.DlxbSiteURL,
		BuildTime: time.Now(),
		Games:     map[string][]string{},
		Entries:   map[string]models.DlxbEntry{},
	}

	for _, category := range []string{"iso", "rgh"} {
		if opt.Proxy != nil {
			opt.Proxy.Rotate()
		}
		catID := app.DlxbCategoryIDs[category]
		if catID == 0 {
			continue
		}
		posts, err := fetchCategory(ctx, opt, catID, logf)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", category, err)
		}
		for _, p := range posts {
			entry, ok := parsePost(p, category)
			if !ok {
				continue
			}
			key := EntryKey(category, entry.Title)
			if _, dup := cache.Entries[key]; dup {
				continue
			}
			cache.Entries[key] = entry
			cache.Games[category] = append(cache.Games[category], entry.Title)
		}
		sort.Strings(cache.Games[category])
		logf("DLXB: %s cached %d titles", category, len(cache.Games[category]))
	}
	return cache, nil
}

// EntryKey is the cache key for a title on a category.
func EntryKey(category, title string) string {
	return category + "|" + strings.ToLower(strings.TrimSpace(title))
}

// ── REST fetching ────────────────────────────────────────────────────────────

type wpPost struct {
	Link  string `json:"link"`
	Title struct {
		Rendered string `json:"rendered"`
	} `json:"title"`
	Content struct {
		Rendered string `json:"rendered"`
	} `json:"content"`
}

type wpPage struct {
	posts []wpPost
	total int // total pages, from X-WP-TotalPages
}

func fetchCategory(ctx context.Context, opt Options, catID int, logf func(string, ...any)) ([]wpPost, error) {
	first, err := fetchPage(ctx, opt, catID, 1)
	if err != nil {
		return nil, err
	}
	pages := make([]wpPost, len(first.posts))
	copy(pages, first.posts)
	if first.total <= 1 && first.total != 0 {
		return pages, nil
	}
	if first.total == 0 {
		// The X-WP-TotalPages header is missing: walk pages until one comes
		// back short, which is the REST API's end-of-collection signal.
		for n := 2; ; n++ {
			p, err := fetchPage(ctx, opt, catID, n)
			if err != nil || len(p.posts) == 0 {
				break
			}
			pages = append(pages, p.posts...)
			if len(p.posts) < opt.PerPage {
				break
			}
		}
		return pages, nil
	}
	logf("DLXB: category %d has %d pages", catID, first.total)

	type result struct {
		n     int
		posts []wpPost
		err   error
	}
	jobs := make(chan int)
	results := make(chan result)
	var wg sync.WaitGroup
	workers := opt.Concurrency
	if workers > first.total-1 {
		workers = first.total - 1
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				if opt.Proxy != nil && n%10 == 0 {
					opt.Proxy.Rotate()
				}
				p, err := fetchPage(ctx, opt, catID, n)
				results <- result{n: n, posts: p.posts, err: err}
			}
		}()
	}
	go func() {
		for n := 2; n <= first.total; n++ {
			jobs <- n
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var firstErr error
	done := 0
	for r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("page %d: %w", r.n, r.err)
			}
			continue
		}
		pages = append(pages, r.posts...)
		done++
		if done%10 == 0 {
			logf("DLXB: category %d page %d/%d", catID, done+1, first.total)
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return pages, nil
}

func fetchPage(ctx context.Context, opt Options, catID, page int) (wpPage, error) {
	q := url.Values{}
	q.Set("categories", fmt.Sprint(catID))
	q.Set("per_page", fmt.Sprint(opt.PerPage))
	q.Set("page", fmt.Sprint(page))
	q.Set("orderby", "date")
	q.Set("order", "asc")
	q.Set("_fields", "link,title,content")
	endpoint := opt.APIURL + "?" + q.Encode()

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return wpPage{}, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return wpPage{}, err
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept", "application/json")
		resp, err := opt.Client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		if readErr != nil {
			lastErr = readErr
			continue
		}
		var posts []wpPost
		if err := json.Unmarshal(body, &posts); err != nil {
			lastErr = err
			continue
		}
		total := 0 // 0 = header absent; fetchCategory then pages sequentially
		if v := resp.Header.Get("X-WP-TotalPages"); v != "" {
			if n, err := parseInt(v); err == nil && n > 0 {
				total = n
			}
		}
		return wpPage{posts: posts, total: total}, nil
	}
	return wpPage{}, lastErr
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscan(strings.TrimSpace(s), &n)
	return n, err
}

// ── post parsing ─────────────────────────────────────────────────────────────

var (
	anchorRe = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	tagRe    = regexp.MustCompile(`(?s)<[^>]+>`)
	passRe   = regexp.MustCompile(`(?i)password\s*[:：]\s*([^\s<]{2,64})`)
	regionRe = regexp.MustCompile(`(?i)\[(ntsc-j|ntsc-u|pal|region free)\]`)
	sepRe      = regexp.MustCompile(`[—–-]{3,}`)
	discLabelRe = regexp.MustCompile(`(?i)\b(?:disc|disk|cd|dvd)\s*[0-9]+`)
	passPrefixRe = regexp.MustCompile(`(?i)^[\s:—–-]*password\s*[:：]\s*\S+\s*`)
	fileExts = []string{".iso", ".rar", ".zip", ".7z", ".part1.rar", ".s01", ".bin"}
)

// parsePost turns one WordPress post into a cache entry. It returns false when
// the post has no usable download links.
func parsePost(p wpPost, category string) (models.DlxbEntry, bool) {
	title := strings.TrimSpace(html.UnescapeString(stripTags(p.Title.Rendered)))
	pageURL := strings.TrimSpace(p.Link)
	if title == "" || pageURL == "" {
		return models.DlxbEntry{}, false
	}
	body := p.Content.Rendered
	text := html.UnescapeString(stripTags(body))

	var links []models.DlxbLink
	seen := map[string]bool{}
	prevEnd := 0
	lastDiscLabel := ""
	for _, m := range anchorRe.FindAllStringSubmatchIndex(body, -1) {
		href := strings.TrimSpace(html.UnescapeString(body[m[2]:m[3]]))
		anchorText := strings.TrimSpace(html.UnescapeString(stripTags(body[m[4]:m[5]])))
		// The text before an anchor carries the edition/disc name, e.g.
		// "Assassin's Creed III (USA) (Disc 1) (Rev 1) : <a>Direct</a>".
		// That is what lets the pipeline pick one edition instead of every
		// region the post lists.
		label := editionLabel(body[prevEnd:m[0]], anchorText)
		prevEnd = m[1]
		// Table posts put the disc name once in a section header and then list
		// many links (mirrors/parts) without repeating it. Carry the last disc
		// label forward so every link is grouped under the right disc.
		if discLabelRe.MatchString(label) {
			lastDiscLabel = label
		} else if lastDiscLabel != "" {
			label = lastDiscLabel + " - " + label
		}
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			continue
		}
		host := ClassifyHost(href)
		if host == "" || seen[href] {
			continue
		}
		seen[href] = true
		links = append(links, models.DlxbLink{Host: host, URL: href, Label: label})
	}
	if len(links) == 0 {
		return models.DlxbEntry{}, false
	}

	entry := models.DlxbEntry{
		Title:    title,
		PageURL:  pageURL,
		Category: category,
		Links:    links,
	}
	seenPass := map[string]bool{}
	for _, m := range passRe.FindAllStringSubmatch(text, -1) {
		p := strings.TrimSpace(m[1])
		if p == "" || seenPass[p] {
			continue
		}
		seenPass[p] = true
		entry.Passwords = append(entry.Passwords, p)
		if entry.Password == "" {
			entry.Password = p
		}
		if len(entry.Passwords) >= 6 {
			break
		}
	}
	if m := regionRe.FindStringSubmatch(title); m != nil {
		entry.Region = strings.ToUpper(m[1])
	}
	return entry, true
}

func stripTags(s string) string {
	return tagRe.ReplaceAllString(strings.ReplaceAll(s, "&nbsp;", " "), " ")
}

// editionLabel returns the release/edition text that precedes an anchor. The
// post separates editions with runs of dashes; take the text after the last
// run and use the anchor text when the context is unusable.
func editionLabel(contextHTML, anchorText string) string {
	text := strings.Join(strings.Fields(html.UnescapeString(stripTags(contextHTML))), " ")
	text = passPrefixRe.ReplaceAllString(text, "")
	if locs := sepRe.FindAllStringIndex(text, -1); len(locs) > 0 {
		text = text[locs[len(locs)-1][1]:]
	}
	text = strings.Trim(text, " :-—–")
	if text == "" || len(text) > 140 {
		return anchorText
	}
	return text
}

// ── host classification ──────────────────────────────────────────────────────

// deniedHosts are links that are not downloads: the site itself, mirrors of the
// same publisher, social sharing, and ad networks.
var deniedHosts = []string{
	"dlxbgame.com", "blogger.com", "blogspot.com", "twitter.com", "x.com",
	"facebook.com", "gamepciso.com", "nswgame.com", "dlpsgame.com",
	"downloadgamep3.com", "exorank.com", "adskeeper", "shrinkearn.com",
}

// ClassifyHost returns a canonical label for a link's host, or "" when the link
// is not a download candidate.
func ClassifyHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	h := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
	for _, d := range deniedHosts {
		if h == d || strings.HasSuffix(h, "."+d) {
			return ""
		}
	}
	switch {
	case h == "downloadgameps3.net":
		return "adgate"
	case strings.HasSuffix(h, "1fichier.com"):
		return "1fichier"
	case strings.Contains(h, "filecrypt"):
		return "filecrypt"
	case strings.Contains(h, "mediafire.com"):
		return "mediafire"
	case h == "drive.google.com" || h == "docs.google.com" || h == "drive.usercontent.google.com":
		return "gdrive"
	case strings.Contains(h, "mega.nz") || strings.Contains(h, "mega.co.nz"):
		return "mega"
	case strings.Contains(h, "gofile.io"):
		return "gofile"
	case strings.Contains(h, "anotepad.com"):
		return "anotepad"
	case h == "bit.ly" || h == "goo.gl" || h == "tinyurl.com" || h == "cutt.ly" || h == "shrinkearn.com":
		return "shortener"
	case strings.Contains(h, "ranoz.gg"):
		return "ranoz"
	case strings.Contains(h, "pixeldrain.com"):
		return "pixeldrain"
	case strings.Contains(h, "buzzheavier.com"):
		return "buzzheavier"
	case strings.Contains(h, "datanodes.to"):
		return "datanodes"
	case strings.Contains(h, "send.cm") || strings.Contains(h, "send.now"):
		return "send"
	case strings.Contains(h, "frdl."):
		return "frdl"
	case strings.Contains(h, "qiwi.gg"):
		return "qiwi"
	case strings.Contains(h, "akirabox.com"):
		return "akirabox"
	case strings.Contains(h, "vikingfile.com") || strings.Contains(h, "vikingf1le"):
		return "vikingfile"
	case strings.Contains(h, "rootz.so"):
		return "rootz"
	}
	if hasFileExt(u.Path) {
		return "direct"
	}
	return "web"
}

func hasFileExt(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range fileExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
