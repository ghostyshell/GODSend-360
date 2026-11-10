// url.go - direct HTTP downloads through aria2c (used by the dlxbgame source).
package torrent

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const dlxbUA = "GODsend-360 (+https://github.com/ghostyshell/GODSend-360)"

// DownloadURL downloads rawURL into destDir with aria2c, using multiple
// connections for speed. outName pins the output filename; leave it empty to
// honour the server's Content-Disposition and return the resulting path.
func (s *Service) DownloadURL(ctx context.Context, rawURL, destDir, outName, gameName string) (string, error) {
	aria2c, err := s.Aria2cBinary()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}
	// Filenames come from a remote host; keep only the base name so a crafted
	// Content-Disposition cannot escape destDir via --out.
	outName = filepath.Base(strings.TrimSpace(outName))
	if outName == "." || outName == ".." || outName == string(filepath.Separator) {
		outName = ""
	}

	args := []string{
		"--dir=" + destDir,
		"--continue=true",
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--max-connection-per-server=16",
		"--split=16",
		"--min-split-size=1M",
		"--file-allocation=none",
		"--console-log-level=warn",
		"--summary-interval=5",
		"--human-readable=true",
		"--user-agent=" + dlxbUA,
	}
	if outName != "" {
		args = append(args, "--out="+outName)
	} else {
		args = append(args, "--content-disposition")
	}
	args = append(args, rawURL)

	s.App.Logf("DLXB DOWNLOAD [%s]: aria2c %s", gameName, safeLogURL(rawURL))
	cmd := exec.CommandContext(ctx, aria2c, args...)
	cmd.Env = scrubChildEnv(os.Environ())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("aria2c pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("aria2c start: %w", err)
	}

	summaryRe := regexp.MustCompile(`\[#\S+\s+([\d.]+\S+)/([\d.]+\S+)\((\d+)%\).*?DL:([\d.]+\S+)(?:.*?ETA:(\S+))?\]`)
	var lastLine string
	var mu sync.Mutex
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		mu.Lock()
		lastLine = line
		mu.Unlock()
		if m := summaryRe.FindStringSubmatch(line); m != nil {
			msg := fmt.Sprintf("Downloading (dlxbgame): %s%% (%s/%s) @ %s", m[3], m[1], m[2], m[4])
			if m[5] != "" {
				msg += " ETA " + m[5]
			}
			s.App.LogStatus(gameName, "Processing", msg)
		}
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		mu.Lock()
		tail := lastLine
		mu.Unlock()
		return "", fmt.Errorf("aria2c: %v%s", waitErr, tailSuffix(tail))
	}

	if outName != "" {
		return filepath.Join(destDir, outName), nil
	}
	return newestFile(destDir)
}

func safeLogURL(raw string) string {
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

func tailSuffix(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return " (" + s + ")"
}

// newestFile returns the most recently written file in dir, skipping aria2c
// control files.
func newestFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var best string
	var bestMod int64
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".aria2") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if m := info.ModTime().UnixNano(); m > bestMod {
			bestMod = m
			best = filepath.Join(dir, e.Name())
		}
	}
	if best == "" {
		return "", fmt.Errorf("aria2c reported success but no file appeared in %s", dir)
	}
	return best, nil
}
