// dlxb.go - dlxbgame.com download and processing pipeline.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"godsend/app"
	"godsend/infrastructure/dlxb"
	"godsend/infrastructure/ftp"
	"godsend/infrastructure/helpers"
	"godsend/infrastructure/proxy"
	"godsend/models"
	"godsend/services"
	"godsend/utils"
)

type discDownload struct {
	number int
	path   string
}

// ProcessDlxbGame resolves a dlxbgame.com release, downloads it with aria2c,
// and installs it as GOD (ISO) or XEX (Jtag/RGH).
func (s *Service) ProcessDlxbGame(gameName string, entry models.DlxbEntry, platform string) {
	s.App.Logf("=== dlxbgame %s: %s (%s) ===", platform, gameName, app.DlxbSiteURL)
	s.App.Logf("DLXB: %s", app.DlxbNotice)
	s.App.Logf("DLXB: original page %s", entry.PageURL)
	safeName := helpers.SanitizeFilename(gameName)
	if safeName == "" {
		s.App.LogStatus(gameName, "Error", "Invalid game name")
		return
	}
	var xboxConn *models.XboxConnection
	if c, ok := s.App.XboxConnections.Load(gameName); ok {
		cc := c.(models.XboxConnection)
		xboxConn = &cc
	}
	installType := s.App.LookupInstallType(gameName)
	if platform == "dlxb_rgh" && installType != "xex" {
		installType = "xex"
	}

	links := chooseEditionLinks(entry.Links)
	if len(links) > 0 && len(links) != len(entry.Links) {
		s.App.Logf("DLXB [%s]: %d links on the post, using edition %q (%d links)",
			gameName, len(entry.Links), links[0].Label, len(links))
	}
	discs := groupDiscLinks(links)
	if len(discs) > 1 {
		nums := make([]int, len(discs))
		for i, d := range discs {
			nums[i] = d.number
		}
		s.App.Logf("DLXB [%s]: multi-disc release - discs %v", gameName, nums)
	}

	resolveCtx, cancelResolve := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancelResolve()

	dlDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_dlxb")
	os.RemoveAll(dlDir)
	if err := os.MkdirAll(dlDir, 0755); err != nil {
		s.App.LogStatus(gameName, "Error", "Temp dir: "+err.Error())
		return
	}
	defer os.RemoveAll(dlDir)

	// Downloads get their own long budget: a 16 GB ISO on a slow line dwarfs
	// the resolution window. Multi-disc releases share it across discs.
	dlCtx, cancelDownload := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancelDownload()

	var done []discDownload
	for _, d := range discs {
		s.App.LogStatus(gameName, "Processing", fmt.Sprintf("Resolving Disc %d...", d.number))
		files, via, err := s.resolveDlxb(resolveCtx, gameName, d.links)
		if err != nil {
			s.App.Logf("DLXB [%s]: Disc %d could not be resolved: %v", gameName, d.number, err)
			if d.number == 1 || len(done) == 0 {
				s.App.LogStatus(gameName, "Error", fmt.Sprintf("Disc %d: no working download link: %v", d.number, err))
				return
			}
			continue
		}
		discDir := filepath.Join(dlDir, fmt.Sprintf("disc%d", d.number))
		os.MkdirAll(discDir, 0755)
		var paths []string
		for i, f := range files {
			s.App.LogStatus(gameName, "Processing", fmt.Sprintf("Downloading Disc %d (%d/%d) via %s...", d.number, i+1, len(files), via))
			outName := f.Filename
			if !usableDownloadName(outName) {
				outName = "" // let aria2c use Content-Disposition
			}
			path, err := s.Torrent.DownloadURL(dlCtx, f.URL, discDir, outName, gameName)
			if err != nil {
				s.App.Logf("ERROR [%s]: Disc %d download %s: %v", gameName, d.number, logURL(f.URL), err)
				s.App.LogStatus(gameName, "Error", fmt.Sprintf("Disc %d download failed: %v", d.number, err))
				return
			}
			paths = append(paths, path)
		}
		if len(paths) == 0 {
			continue
		}
		primary := pickPrimaryDownload(paths)
		s.App.Logf("DLXB [%s]: Disc %d downloaded %s (%.0f MB)", gameName, d.number, filepath.Base(primary), fileSizeMB(primary))
		done = append(done, discDownload{number: d.number, path: primary})
	}
	if len(done) == 0 {
		s.App.LogStatus(gameName, "Error", "Download produced no file")
		return
	}

	titleID, mediaID := s.processDownloadedArchive(gameName, safeName, done[0].path, installType, xboxConn, entry.AllPasswords())
	if len(done) > 1 {
		if titleID == "" && installType != "xex" {
			s.App.Logf("DLXB [%s]: primary disc did not install; skipping %d extra disc(s)", gameName, len(done)-1)
		} else {
			s.installExtraDiscs(gameName, safeName, done[1:], titleID, mediaID, installType, xboxConn, entry.AllPasswords())
		}
	}
}

// ── multi-disc ──────────────────────────────────────────────────────────────

type discGroup struct {
	number int
	links  []models.DlxbLink
}

var discNumberRe = regexp.MustCompile(`(?i)\b(?:disc|disk|cd|dvd)\s*([0-9]+)`)

// discNumber reads the disc number from an edition label ("... (Disc 2)"),
// defaulting to Disc 1.
func discNumber(label string) int {
	if m := discNumberRe.FindStringSubmatch(label); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

// groupDiscLinks splits one edition's links into discs, preserving order.
func groupDiscLinks(links []models.DlxbLink) []discGroup {
	byNumber := map[int][]models.DlxbLink{}
	var numbers []int
	for _, l := range links {
		n := discNumber(l.Label)
		if _, ok := byNumber[n]; !ok {
			numbers = append(numbers, n)
		}
		byNumber[n] = append(byNumber[n], l)
	}
	sort.Ints(numbers)
	out := make([]discGroup, 0, len(numbers))
	for _, n := range numbers {
		out = append(out, discGroup{number: n, links: byNumber[n]})
	}
	if len(out) == 0 {
		out = append(out, discGroup{number: 1, links: links})
	}
	return out
}

// installExtraDiscs installs Disc 2+ of a multi-disc release.
//
// FTP mode follows the compatibility table: "content" discs go to
// Content/.../00000002, "god" continuation discs get their own
// "Title (Disc N) - TitleID" GOD folder. HTTP mode packages each disc's
// content as a DLC archive in the Ready folder, which the Aurora install loop
// extracts into Content/.../00000002 after the GOD parts.
func (s *Service) installExtraDiscs(gameName, safeName string, discs []discDownload, titleID, mediaID, installType string, xboxConn *models.XboxConnection, passwords []string) {
	gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
	ftpMode := xboxConn != nil && xboxConn.Mode == "ftp"
	resolvedName := services.LookupTitleName(titleID)
	var dlcList []string

	for _, d := range discs {
		if installType == "xex" {
			if ftpMode {
				s.installXEXDisc(gameName, safeName, d.path, d.number, passwords, xboxConn)
			} else {
				s.App.Logf("DLXB [%s]: Disc %d skipped - HTTP XEX install holds one folder", gameName, d.number)
			}
			continue
		}
		isoPath, cleanup, err := s.discISO(safeName, d.path, d.number, passwords)
		if err != nil {
			s.App.Logf("DLXB [%s]: Disc %d could not be extracted: %v", gameName, d.number, err)
			continue
		}
		if ftpMode {
			rec := models.DiscCompat(parseTitleIDHex(titleID), byte(d.number))
			if installType == "god" && rec.InstallType == "god" {
				s.App.Logf("DLXB [%s]: Disc %d is a continuation disc - installing as GOD (%s)", gameName, d.number, rec.Notes)
				s.installGODDisc(gameName, safeName, isoPath, d.number, resolvedName, xboxConn)
			} else {
				s.App.Logf("DLXB [%s]: Disc %d installing as Content (%s)", gameName, d.number, rec.Notes)
				s.processContentInstallFromISO(gameName, safeName, isoPath, xboxConn)
			}
			cleanup()
			continue
		}

		// HTTP transfer: only a GOD primary can carry extra content archives.
		if installType != "god" {
			s.App.Logf("DLXB [%s]: Disc %d skipped - HTTP %s install cannot merge discs", gameName, d.number, installType)
			cleanup()
			continue
		}
		contentDir, tid, err := s.extractDiscContent(gameName, fmt.Sprintf("%s_disc%d", safeName, d.number), isoPath)
		cleanup()
		if err != nil {
			s.App.Logf("DLXB [%s]: Disc %d content: %v", gameName, d.number, err)
			continue
		}
		partName := fmt.Sprintf("%s_Disc%d.7z", safeName, d.number)
		if err := utils.CreateZipFromDir(contentDir, filepath.Join(gameDir, partName)); err != nil {
			s.App.Logf("DLXB [%s]: Disc %d archive: %v", gameName, d.number, err)
			os.RemoveAll(contentDir)
			continue
		}
		os.RemoveAll(contentDir)
		dlcList = append(dlcList, partName)
		s.App.Logf("DLXB [%s]: Disc %d packaged as content (TitleID %s)", gameName, d.number, tid)
	}

	if len(dlcList) > 0 && ftpMode == false {
		s.updateGameINI_Parts(gameDir, gameName, titleID, mediaID, resolvedName, dlcList)
		s.App.LogStatus(gameName, "Ready", "Ready to Install")
		s.App.Logf("DLXB [%s]: GOD parts + %d content disc(s) ready", gameName, len(dlcList))
	}
}

// discISO returns the ISO inside a downloaded disc file, plus a cleanup func.
func (s *Service) discISO(safeName, path string, number int, passwords []string) (string, func(), error) {
	if strings.EqualFold(filepath.Ext(path), ".iso") {
		return path, func() {}, nil
	}
	suffix := fmt.Sprintf("%s_disc%d", safeName, number)
	iso, err := extractISOAny(path, suffix, filepath.Join(s.App.ToolsDir, "Temp"), passwords)
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() {
		os.RemoveAll(filepath.Join(s.App.ToolsDir, "Temp", suffix+"_extracted"))
	}
	return iso, cleanup, nil
}

// installGODDisc converts an extra disc and transfers it to its own GOD folder.
func (s *Service) installGODDisc(gameName, safeName, isoPath string, number int, resolvedName string, xboxConn *models.XboxConnection) {
	godDir := filepath.Join(s.App.ToolsDir, "Temp", fmt.Sprintf("%s_disc%d_GOD", safeName, number))
	os.RemoveAll(godDir)
	os.MkdirAll(godDir, 0755)
	if err := utils.RunIso2GodNative(isoPath, godDir, Iso2GodResolveDisplayTitle); err != nil {
		s.App.Logf("DLXB [%s]: Disc %d GOD convert: %v", gameName, number, err)
		os.RemoveAll(godDir)
		return
	}
	titleID, mediaID, err := helpers.DetectGodStructure(godDir)
	if err != nil {
		s.App.Logf("DLXB [%s]: Disc %d GOD detect: %v", gameName, number, err)
		os.RemoveAll(godDir)
		return
	}
	if resolvedName == "" {
		resolvedName = "Title"
	}
	name := fmt.Sprintf("%s (Disc %d)", resolvedName, number)
	gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
	s.finalizeGODNamed(gameName, safeName, gameDir, godDir, titleID, mediaID, name, xboxConn)
}

// installXEXDisc installs Disc 2+ of a Jtag/RGH release: a loose XEX folder
// becomes its own "<folder> (Disc N)" folder on the console, and a data disc
// carrying content packages is installed as Content.
func (s *Service) installXEXDisc(gameName, safeName, archivePath string, number int, passwords []string, xboxConn *models.XboxConnection) {
	extDir := filepath.Join(s.App.ToolsDir, "Temp", fmt.Sprintf("%s_disc%d_mext", safeName, number))
	os.RemoveAll(extDir)
	s.App.LogStatus(gameName, "Processing", fmt.Sprintf("Extracting Disc %d for XEX...", number))
	if err := extractArchiveAny(archivePath, extDir, passwords); err != nil {
		s.App.Logf("DLXB [%s]: Disc %d XEX extract: %v", gameName, number, err)
		os.RemoveAll(extDir)
		return
	}
	xexFolder := helpers.FindXEXFolder(extDir)
	if xexFolder != "" {
		name := fmt.Sprintf("%s (Disc %d)", filepath.Base(xexFolder), number)
		gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
		if !s.transferXEXFolderNamed(gameName, safeName, gameDir, xexFolder, name, xboxConn) {
			os.RemoveAll(extDir)
		}
		return
	}
	if files := findDigitalContentFiles(extDir); len(files) > 0 {
		if err := s.transferDigitalContentFilesFTP(files, xboxConn, gameName); err != nil {
			s.App.Logf("DLXB [%s]: Disc %d content FTP: %v", gameName, number, err)
		}
		os.RemoveAll(extDir)
		return
	}
	s.App.Logf("DLXB [%s]: Disc %d has no XEX folder or content package - skipped", gameName, number)
	os.RemoveAll(extDir)
}

// uniquePasswords tries the unencrypted path first, then each distinct password.
func uniquePasswords(passwords []string) []string {
	seen := map[string]bool{"": true}
	out := []string{""}
	for _, p := range passwords {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func extractArchiveAny(path, dest string, passwords []string) error {
	var lastErr error
	for _, p := range uniquePasswords(passwords) {
		os.RemoveAll(dest)
		if err := utils.ExtractArchiveWithPassword(path, dest, p); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no password candidates")
	}
	return lastErr
}

func extractISOAny(path, safeName, tempRoot string, passwords []string) (string, error) {
	var lastErr error
	for _, p := range uniquePasswords(passwords) {
		iso, err := utils.ExtractISOWithPassword(path, safeName, tempRoot, p)
		if err == nil {
			return iso, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no password candidates")
	}
	return "", lastErr
}

func parseTitleIDHex(titleID string) uint32 {
	v, _ := strconv.ParseUint(strings.TrimSpace(titleID), 16, 32)
	return uint32(v)
}

// resolveDlxb tries stored links in preference order until one resolves.
func (s *Service) resolveDlxb(ctx context.Context, gameName string, links []models.DlxbLink) ([]dlxb.Resolved, string, error) {
	if len(links) == 0 {
		return nil, "", fmt.Errorf("post has no download links")
	}
	r := dlxb.NewResolver()
	r.Logf = s.App.Logf
	r.CFSolverURL = strings.TrimSpace(os.Getenv("GODSEND_CF_SOLVER_URL"))
	r.SetProxyPool(proxy.FromEnv(s.App.Logf))
	r.Debrid = s.debridUnrestrict(gameName)

	ordered := make([]models.DlxbLink, 0, len(links))
	for _, l := range links {
		if l.Status == dlxb.StatusDead {
			continue
		}
		ordered = append(ordered, l)
	}
	if len(ordered) == 0 {
		return nil, "", fmt.Errorf("every link for this release is marked dead; refresh the source or open the site")
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return hostPreference(ordered[i].Host) < hostPreference(ordered[j].Host)
	})

	var lastErr error
	for _, link := range ordered {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		default:
		}
		files, err := r.Resolve(ctx, link)
		if err != nil {
			lastErr = err
			s.App.Logf("DLXB [%s]: %s failed: %v", gameName, link.Host, err)
			continue
		}
		if len(files) == 0 {
			continue
		}
		// Follow redirects here with the guarded client so aria2c cannot be
		// steered at a private address by a redirect we never inspected.
		for i := range files {
			final, perr := r.Preflight(ctx, files[i].URL)
			if perr != nil {
				if errors.Is(perr, dlxb.ErrUnsafeTarget) {
					return nil, "", perr
				}
				s.App.Logf("DLXB [%s]: preflight %s: %v (using link as-is)", gameName, logURL(files[i].URL), perr)
				continue
			}
			files[i].URL = final
		}
		return files, link.Host, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all links failed")
	}
	return nil, "", lastErr
}

// debridUnrestrict returns a resolver callback, or nil when Debrid is off.
func (s *Service) debridUnrestrict(gameName string) func(context.Context, string) (string, string, error) {
	prov := s.activeDebrid()
	if prov == nil {
		return nil
	}
	return func(ctx context.Context, srcURL string) (string, string, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		s.App.LogStatus(gameName, "Processing", "Debrid ("+prov.Name()+"): resolving host link...")
		return prov.Unrestrict(ctx, srcURL)
	}
}

// chooseEditionLinks groups a post's links into editions (the text before each
// anchor) and returns the preferred one, so a post listing many regions/downloads
// one release instead of all of them.
func chooseEditionLinks(links []models.DlxbLink) []models.DlxbLink {
	if len(links) <= 1 {
		return links
	}
	groups := map[string][]models.DlxbLink{}
	var order []string
	for _, l := range links {
		key := normalizeEdition(l.Label)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], l)
	}
	best := order[0]
	bestScore := -1
	for _, key := range order {
		score := editionScore(key)
		// On a tie prefer the richer group: posts often have a one-link
		// "container" line alongside the real multi-disc link set.
		if score > bestScore || (score == bestScore && len(groups[key]) > len(groups[best])) {
			best, bestScore = key, score
		}
	}
	return groups[best]
}

var (
	editionNoiseRe = regexp.MustCompile(`(?i)[\(\[]\s*(?:disc|disk|cd|dvd)\s*[0-9]+[^\)\]]*[\)\]]|[\(\[]\s*(?:rev\s*[0-9]+|single-player|multiplayer)[^\)\]]*[\)\]]`)
	bareDiscRe     = regexp.MustCompile(`(?i)\b(?:disc|disk|cd|dvd)\s*[0-9]+\b`)
)

func normalizeEdition(label string) string {
	s := strings.ToLower(label)
	s = editionNoiseRe.ReplaceAllString(s, "")
	s = bareDiscRe.ReplaceAllString(s, "")
	s = strings.NewReplacer(":", " ", "—", " ", "–", " ", "(", " ", ")", " ", "[", " ", "]", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// editionScore prefers a complete, English-friendly release: USA, then region
// free, then Europe/PAL, then NTSC-J.
func editionScore(key string) int {
	score := 0
	for _, kw := range []struct {
		match string
		pts   int
	}{{"usa", 4}, {"region free", 3}, {"europe", 2}, {"pal", 2}, {"japan", 1}, {"korea", 1}, {"ntsc-j", 1}} {
		if strings.Contains(key, kw.match) {
			score += kw.pts
		}
	}
	return score
}

// logURL drops userinfo and truncates for logging.
func logURL(raw string) string {
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

// hostPreference orders links: direct and simple hosters first, ad gates and
// slow/unreliable hosts last.
func hostPreference(host string) int {
	switch host {
	case "direct":
		return 0
	case "gofile":
		return 1
	case "pixeldrain":
		return 2
	case "gdrive":
		return 3
	case "mediafire":
		return 4
	case "buzzheavier", "datanodes", "send", "frdl", "qiwi", "akirabox", "vikingfile", "rootz", "ranoz":
		return 5
	case "filecrypt":
		return 10
	case "adgate":
		return 11
	case "1fichier":
		return 12
	case "shortener":
		return 13
	case "anotepad":
		return 14
	case "mega":
		return 15
	}
	return 20
}

// usableDownloadName reports whether a filename carries an extension the
// pipeline can process directly. Host token names (no extension) are dropped so
// aria2c asks the server for Content-Disposition instead.
func usableDownloadName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".iso", ".rar", ".zip", ".7z", ".bin":
		return true
	}
	return false
}

// pickPrimaryDownload prefers the first archive/ISO; otherwise the largest file.
func pickPrimaryDownload(files []string) string {
	for _, f := range files {
		switch strings.ToLower(filepath.Ext(f)) {
		case ".iso", ".rar", ".7z", ".zip":
			return f
		}
	}
	best := files[0]
	var bestSize int64
	for _, f := range files {
		if i, err := os.Stat(f); err == nil && i.Size() > bestSize {
			bestSize = i.Size()
			best = f
		}
	}
	return best
}

func fileSizeMB(path string) float64 {
	if i, err := os.Stat(path); err == nil {
		return float64(i.Size()) / 1048576
	}
	return 0
}

// ── shared post-download processing ─────────────────────────────────────────

// processDownloadedArchive turns a downloaded archive (or bare ISO) into a
// Ready GOD / content / XEX install. password may be empty.
//
// This is the same flow the Minerva source uses after aria2c finishes; it lives
// here so both sources share one implementation.
// Returns the detected TitleID/MediaID (empty for XEX) so multi-disc callers
// can install the remaining discs against the primary title.
func (s *Service) processDownloadedArchive(gameName, safeName, archivePath, installType string, xboxConn *models.XboxConnection, passwords []string) (string, string) {
	gameDir := filepath.Join(s.App.ToolsDir, "Ready", safeName)
	os.MkdirAll(gameDir, 0755)

	if installType == "xex" {
		extDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_mext")
		os.RemoveAll(extDir)
		s.App.LogStatus(gameName, "Processing", "Extracting archive for XEX...")
		if err := extractArchiveAny(archivePath, extDir, passwords); err != nil {
			os.Remove(archivePath)
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
			return "", ""
		}
		os.Remove(archivePath)
		xexFolder := helpers.FindXEXFolder(extDir)
		if xexFolder == "" {
			os.RemoveAll(extDir)
			s.App.LogStatus(gameName, "Error", "No default.xex found in archive")
			return "", ""
		}
		if !s.transferXEXFolder(gameName, safeName, gameDir, xexFolder, xboxConn) {
			os.RemoveAll(extDir)
		}
		return "", ""
	}

	var isoPath string
	if strings.EqualFold(filepath.Ext(archivePath), ".iso") {
		isoPath = archivePath
	} else {
		s.App.LogStatus(gameName, "Processing", "Extracting ISO...")
		var err error
		isoPath, err = extractISOAny(archivePath, safeName, filepath.Join(s.App.ToolsDir, "Temp"), passwords)
		os.Remove(archivePath)
		if err != nil {
			s.App.LogStatus(gameName, "Error", fmt.Sprintf("Extract: %v", err))
			return "", ""
		}
	}

	if installType == "content" {
		tid := s.processContentInstallFromISO(gameName, safeName, isoPath, xboxConn)
		os.Remove(isoPath)
		return tid, ""
	}

	s.App.LogStatus(gameName, "Processing", "Converting to GOD...")
	godDir := filepath.Join(s.App.ToolsDir, "Temp", safeName+"_MGOD")
	os.MkdirAll(godDir, 0755)
	if err := utils.RunIso2GodNative(isoPath, godDir, Iso2GodResolveDisplayTitle); err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("GOD convert: %v", err))
		os.Remove(isoPath)
		os.RemoveAll(godDir)
		return "", ""
	}
	os.Remove(isoPath)

	titleID, mediaID, err := helpers.DetectGodStructure(godDir)
	if err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("GOD detect: %v", err))
		os.RemoveAll(godDir)
		return "", ""
	}
	s.App.Logf("Downloaded ISO: TitleID=%s MediaID=%s", titleID, mediaID)
	s.finalizeGOD(gameName, safeName, gameDir, godDir, titleID, mediaID, xboxConn)
	return titleID, mediaID
}

// transferXEXFolder copies a loose XEX folder to the console (FTP) or packages
// it for an HTTP install.
// transferXEXFolder returns true when the transfer was scheduled for retry
// and the source folder must be kept on disk.
func (s *Service) transferXEXFolder(gameName, safeName, gameDir, xexFolder string, xboxConn *models.XboxConnection) bool {
	return s.transferXEXFolderNamed(gameName, safeName, gameDir, xexFolder, filepath.Base(xexFolder), xboxConn)
}

// transferXEXFolderNamed is transferXEXFolder with an explicit console folder
// name, used to keep Disc 2's XEX folder from colliding with Disc 1.
func (s *Service) transferXEXFolderNamed(gameName, safeName, gameDir, xexFolder, folderName string, xboxConn *models.XboxConnection) bool {
	if xboxConn != nil && xboxConn.Mode == "ftp" {
		if err := s.FTP.TransferXEX(xexFolder, folderName, xboxConn, gameName); err != nil {
			s.App.Logf("FTP: initial XEX transfer failed for %s: %v - scheduling for retry", gameName, err)
			job := ftp.PendingFTPJob{
				ID:         helpers.SanitizeFilename(gameName) + "_" + strconv.FormatInt(time.Now().UnixNano(), 36),
				GameName:   gameName,
				Type:       "xex",
				SourceDir:  xexFolder,
				GameDir:    gameDir,
				XboxIP:     xboxConn.IP,
				Drive:      xboxConn.Drive,
				FolderName: folderName,
				CreatedAt:  time.Now(),
			}
			s.FTP.SchedulePendingFTP(job)
			return true
		}
		os.RemoveAll(gameDir)
		s.App.LogFTPComplete(gameName, "", xboxConn.IP)
		return false
	}
	partName := fmt.Sprintf("%s_Part1.7z", safeName)
	if err := utils.CreateZipFromDir(xexFolder, filepath.Join(gameDir, partName)); err != nil {
		s.App.LogStatus(gameName, "Error", fmt.Sprintf("Archive XEX: %v", err))
		return false
	}
	s.App.GamePartsMap.Store(gameName, []string{partName})
	s.updateGameINI_XEX(gameDir, gameName, folderName, partName)
	s.App.LogStatus(gameName, "Ready", "Ready to Install")
	return false
}
