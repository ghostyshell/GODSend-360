// torrent.go — aria2c probing, Minerva torrent fetching, and torrent-based downloads.
package torrent

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"godsend/app"
	"godsend/infrastructure/helpers"
	"godsend/models"
)

// Service provides torrent-based download capabilities via aria2c.
type Service struct {
	App *app.App

	// DarwinCandidatesFn returns extra macOS Homebrew aria2c paths to probe.
	// Injected from the build-tagged package-main function.
	DarwinCandidatesFn func() []string

	// aria2c resolved path cache (mutex-guarded, local to this service)
	aria2cResolvedMu   sync.Mutex
	aria2cResolvedPath string
}

// FetchMinervaTorrent downloads the collection .torrent file for the given platform from Minerva.
func (s *Service) FetchMinervaTorrent(platform string) ([]byte, error) {
	torrentURL, ok := app.MinervaTorrentURLs[platform]
	if !ok {
		return nil, fmt.Errorf("no torrent URL for platform %q", platform)
	}
	s.App.Logf("TORRENT: Fetching collection torrent for %s...", platform)
	req, err := http.NewRequest("GET", torrentURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("download torrent: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("torrent HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// aria2cWorks runs `<path> --version` with a short timeout and reports whether
// the binary launches cleanly.
func aria2cWorks(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		if snippet == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, snippet)
	}
	return nil
}

// ProbeWorkingAria2c finds a usable aria2c (bundled next to the server binary, PATH,
// then macOS Homebrew locations). Not cached — used at startup and by Aria2cBinary.
func (s *Service) ProbeWorkingAria2c() (string, error) {
	name := "aria2c"
	if runtime.GOOS == "windows" {
		name = "aria2c.exe"
	}
	var lastErr error
	tried := map[string]bool{}

	try := func(p string, label string) (string, bool) {
		if p == "" || tried[p] {
			return "", false
		}
		tried[p] = true
		werr := aria2cWorks(p)
		if werr == nil {
			return p, true
		}
		lastErr = fmt.Errorf("%s (%s) unusable: %v", label, p, werr)
		return "", false
	}

	bundled := filepath.Join(s.App.GodsendExeDir, name)
	if _, err := os.Stat(bundled); err == nil {
		if p, ok := try(bundled, "bundled aria2c"); ok {
			return p, nil
		}
		if lastErr != nil {
			s.App.Logf("[WARN] %v — trying PATH / Homebrew locations", lastErr)
		}
	}

	if lp, err := exec.LookPath("aria2c"); err == nil {
		if p, ok := try(lp, "aria2c on PATH"); ok {
			return p, nil
		}
	}

	var candidates []string
	if s.DarwinCandidatesFn != nil {
		candidates = s.DarwinCandidatesFn()
	}
	for _, cand := range candidates {
		if _, err := os.Stat(cand); err != nil {
			continue
		}
		if p, ok := try(cand, "aria2c"); ok {
			return p, nil
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("aria2c not usable — %v", lastErr)
	}
	return "", fmt.Errorf("aria2c not found — bundled binary missing and not in PATH")
}

// Aria2cBinary returns the path to a working aria2c executable.
// Tries the bundled binary first (next to the server binary), validates it with
// `--version`, then PATH and macOS Homebrew paths. Result is cached.
func (s *Service) Aria2cBinary() (string, error) {
	s.aria2cResolvedMu.Lock()
	defer s.aria2cResolvedMu.Unlock()
	if s.aria2cResolvedPath != "" {
		return s.aria2cResolvedPath, nil
	}
	p, err := s.ProbeWorkingAria2c()
	if err != nil {
		if runtime.GOOS == "darwin" {
			return "", fmt.Errorf("%w. On macOS the backend normally installs Homebrew aria2 at startup; fix the error above or set GODSEND_SKIP_ARIA2_BOOTSTRAP=1 and install aria2 yourself", err)
		}
		return "", fmt.Errorf("%w. Install aria2 and restart the backend", err)
	}
	s.aria2cResolvedPath = p
	bundledName := "aria2c"
	if runtime.GOOS == "windows" {
		bundledName = "aria2c.exe"
	}
	bundled := filepath.Join(s.App.GodsendExeDir, bundledName)
	if p != bundled {
		s.App.Logf("[INFO] Using aria2c: %s", p)
	}
	return p, nil
}

// torrentBasenameMatches reports whether a path inside the .torrent matches the Minerva entry
// filename, including when one side uses HTML entities and the other uses a literal apostrophe.
func torrentBasenameMatches(torrentBase, entryFileName string) bool {
	if strings.EqualFold(torrentBase, entryFileName) {
		return true
	}
	a := helpers.DecodeMinervaName(torrentBase)
	b := helpers.DecodeMinervaName(entryFileName)
	if strings.EqualFold(a, b) {
		return true
	}
	if strings.EqualFold(a, entryFileName) || strings.EqualFold(torrentBase, b) {
		return true
	}
	return false
}

// DownloadViaTorrent uses aria2c to download a single file from the Minerva collection torrent.
// It fetches the .torrent from Minerva's URL, finds the target file's 1-based index, then
// shells out to aria2c with --select-file so only that file is downloaded.
func (s *Service) DownloadViaTorrent(platform, destDir, gameName string, entry models.MinervaEntry) (string, error) {
	aria2c, err := s.Aria2cBinary()
	if err != nil {
		return "", err
	}

	torrentURL, ok := app.MinervaTorrentURLs[platform]
	if !ok {
		return "", fmt.Errorf("no torrent URL for platform %q", platform)
	}

	// Fetch torrent to find the 1-based file index aria2c needs.
	torrentData, err := s.FetchMinervaTorrent(platform)
	if err != nil {
		return "", fmt.Errorf("fetch torrent: %w", err)
	}
	mi, err := metainfo.Load(bytes.NewReader(torrentData))
	if err != nil {
		return "", fmt.Errorf("parse .torrent: %w", err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return "", fmt.Errorf("torrent info: %w", err)
	}

	fileIndex := -1
	var fileSize int64
	for i, f := range info.UpvertedFiles() {
		torrentBase := filepath.Base(filepath.Join(f.Path...))
		if torrentBasenameMatches(torrentBase, entry.FileName) {
			fileIndex = i + 1 // aria2c uses 1-based index
			fileSize = f.Length
			break
		}
	}
	if fileIndex < 0 {
		return "", fmt.Errorf("file %q not found in torrent", entry.FileName)
	}

	s.App.Logf("TORRENT [%s]: aria2c downloading %s (%.0f MB) file-index=%d", gameName, entry.FileName, float64(fileSize)/1048576, fileIndex)
	s.App.LogStatus(gameName, "Processing", fmt.Sprintf("Torrenting (Minerva): starting... (%.0f MB)", float64(fileSize)/1048576))

	// Write torrent to a temp file so aria2c doesn't need to re-fetch it via HTTPS.
	// (aria2c on Windows has SSL issues fetching HTTPS URLs; Go has none.)
	tf, err := os.CreateTemp("", "godsend-*.torrent")
	if err != nil {
		return "", fmt.Errorf("create temp torrent: %w", err)
	}
	torrentFile := tf.Name()
	defer os.Remove(torrentFile)
	if _, err := tf.Write(torrentData); err != nil {
		tf.Close()
		return "", fmt.Errorf("write temp torrent: %w", err)
	}
	tf.Close()

	// aria2c nests output under <torrent-name>/path/… so the full path can exceed
	// Windows MAX_PATH (260 chars) when destDir + torrent subdirs + filename are combined.
	// Use a short-named OS temp dir as the aria2c working directory; move the finished
	// file to destDir afterwards.
	aria2cDir, err := os.MkdirTemp("", "gd-dl-*")
	if err != nil {
		return "", fmt.Errorf("create aria2c temp dir: %w", err)
	}
	defer os.RemoveAll(aria2cDir)

	args := []string{
		"--dir=" + aria2cDir,
		"--select-file=" + strconv.Itoa(fileIndex),
		"--seed-time=0",                    // stop seeding immediately after download
		"--bt-remove-unselected-file=true", // don't keep unselected files
		"--bt-max-peers=100",
		"--follow-torrent=false", // torrent file is our input, don't re-fetch
		"--file-allocation=none", // skip pre-allocation — avoids spurious ENOSPC on large files
		"--console-log-level=warn",
		"--summary-interval=3", // print progress every 3 s
		"--human-readable=true",
		torrentFile,
	}
	if s.App.Aria2ListenPort != "" {
		args = append(args, "--listen-port="+s.App.Aria2ListenPort)
		args = append(args, "--dht-listen-port="+s.App.Aria2ListenPort)
	}
	if s.App.Aria2DhtPort != "" {
		args = append(args, "--dht-listen-port="+s.App.Aria2DhtPort)
	}
	_ = torrentURL // URL was used to fetch; aria2c gets the temp file

	cmd := exec.Command(aria2c, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("aria2c pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // merge stderr into the same pipe

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("aria2c start: %w", err)
	}

	// aria2c summary lines look like:
	//   [#abc123 195MiB/6504MiB(3%) CN:67 DL:9.9MiB ETA:31m]
	summaryRe := regexp.MustCompile(`\[#\S+\s+([\d.]+\S+)/([\d.]+\S+)\((\d+)%\)[^\]]*DL:([\d.]+\S+)[^\]]*ETA:(\S+)\]`)

	// Drain aria2c output in a goroutine so the pipe never fills and deadlocks cmd.Wait().
	const tailMax = 50
	var (
		tailMu  sync.Mutex
		tailBuf []string
	)
	appendTail := func(line string) {
		tailMu.Lock()
		defer tailMu.Unlock()
		if len(tailBuf) >= tailMax {
			tailBuf = tailBuf[1:]
		}
		tailBuf = append(tailBuf, line)
	}

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		sc.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			for i, b := range data {
				if b == '\n' || b == '\r' {
					adv := i + 1
					if b == '\r' && adv < len(data) && data[adv] == '\n' {
						adv++ // consume \r\n as one unit
					}
					return adv, data[:i], nil
				}
			}
			if atEOF && len(data) > 0 {
				return len(data), data, nil
			}
			return 0, nil, nil
		})
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), " \t")
			if line == "" {
				continue
			}
			if m := summaryRe.FindStringSubmatch(line); m != nil {
				pct, dl, eta := m[3], m[4], m[5]
				msg := fmt.Sprintf("Torrenting (Minerva): %s%% @ %s/s ETA %s", pct, dl, eta)
				s.App.Logf("TORRENT [%s]: %s", gameName, msg)
				s.App.LogStatus(gameName, "Processing", msg)
				continue
			}
			// Keep non-progress lines for post-mortem
			appendTail(line)
			s.App.Logf("TORRENT [%s]: aria2c: %s", gameName, line)
		}
	}()

	waitErr := cmd.Wait()
	<-doneCh // ensure pipe is fully drained before proceeding
	if waitErr != nil {
		tailMu.Lock()
		tail := strings.Join(tailBuf, " | ")
		tailMu.Unlock()
		if tail == "" {
			tail = "(no output captured)"
		}
		return "", fmt.Errorf("aria2c: %w — last output: %s", waitErr, tail)
	}

	// Walk the short temp dir to find the downloaded file.
	var foundPath string
	_ = filepath.Walk(aria2cDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Base(path), entry.FileName) {
			foundPath = path
			return filepath.SkipAll
		}
		return nil
	})
	if foundPath == "" {
		return "", fmt.Errorf("aria2c finished but %q not found under %s", entry.FileName, aria2cDir)
	}

	// Move the file to destDir (caller manages destDir lifetime).
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("create dest dir: %w", err)
	}
	destFile := filepath.Join(destDir, filepath.Base(foundPath))
	if err := os.Rename(foundPath, destFile); err != nil {
		return "", fmt.Errorf("move downloaded file to dest: %w", err)
	}

	s.App.Logf("TORRENT [%s]: Download complete (%.0f MB)", gameName, float64(fileSize)/1048576)
	return destFile, nil
}
