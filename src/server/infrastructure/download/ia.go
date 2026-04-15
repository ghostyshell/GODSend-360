// ia.go — Internet Archive HTTP downloads (single-stream and parallel range).
package download

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"godsend/app"
)

// Service provides download functionality.
type Service struct {
	App *app.App
}

// DownloadWithProgress downloads urlStr to dest. For Internet Archive URLs it uses a
// Gopeed-style segment queue (fixed-size ranges, worker pool) when Range is supported.
func (s *Service) DownloadWithProgress(urlStr, dest, name, ref string) error {
	isIA := strings.Contains(strings.ToLower(urlStr), "archive.org")
	if isIA && s.App.IADownloadMaxParallel > 1 {
		size, rangeOK, err := s.IAProbeDownload(urlStr, ref)
		if err != nil {
			s.App.Logf("WARN [%s]: probe failed (%v), using single stream", name, err)
		} else if rangeOK && size >= app.IAParallelThreshold {
			nSeg := (size + app.IASegmentSize - 1) / app.IASegmentSize
			s.App.Logf("[%s] Chunked download: %.0f MB, %d segments (~%d MiB each), up to %d parallel HTTP",
				name, float64(size)/1048576, nSeg, app.IASegmentSize/(1024*1024), s.App.IADownloadMaxParallel)
			return s.IADownloadChunkedParallel(urlStr, dest, name, ref, size)
		}
	}
	return s.IADownloadSingle(urlStr, dest, name, ref)
}

// IAProbeDownload sends a HEAD request and returns (Content-Length, Accept-Ranges, error).
func (s *Service) IAProbeDownload(urlStr, ref string) (size int64, rangeOK bool, err error) {
	req, _ := http.NewRequest("HEAD", urlStr, nil)
	req.Header.Set("Referer", ref)
	s.App.ApplyArchiveOrgHeaders(req)
	resp, err := s.App.IAHTTPClient.Do(req)
	if err != nil {
		return 0, false, err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, false, fmt.Errorf("HEAD HTTP %d", resp.StatusCode)
	}
	size = resp.ContentLength
	rangeOK = strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes") && size > 0
	return size, rangeOK, nil
}

// IADownloadChunkedParallel downloads the file into a single pre-sized destination using a
// queue of fixed-size byte ranges and a bounded worker pool.
func (s *Service) IADownloadChunkedParallel(urlStr, dest, name, ref string, totalSize int64) error {
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	if err := out.Truncate(totalSize); err != nil {
		out.Close()
		os.Remove(dest)
		return fmt.Errorf("truncate: %w", err)
	}

	type seg struct {
		start, end int64
	}
	var segments []seg
	for off := int64(0); off < totalSize; off += app.IASegmentSize {
		end := off + app.IASegmentSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		segments = append(segments, seg{off, end})
	}

	jobs := make(chan seg, len(segments))
	for _, ss := range segments {
		jobs <- ss
	}
	close(jobs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var written int64
	startTime := time.Now()
	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		lastConsole := time.Time{}
		for {
			select {
			case <-progressDone:
				return
			case now := <-ticker.C:
				w := atomic.LoadInt64(&written)
				pct := float64(w) / float64(totalSize) * 100
				elapsed := now.Sub(startTime).Seconds()
				if elapsed < 0.001 {
					elapsed = 0.001
				}
				speedMBs := float64(w) / elapsed / 1048576
				wMB := float64(w) / 1048576
				tMB := float64(totalSize) / 1048576
				etaStr := "..."
				if speedMBs > 0 && pct < 100 {
					etaSecs := float64(totalSize-w) / (speedMBs * 1048576)
					etaStr = "~" + app.FmtDuration(etaSecs) + " left"
				}
				s.App.LogStatus(name, "Processing",
					fmt.Sprintf("Downloading: %.0f%% (%.0f/%.0f MB) @ %.1f MB/s | %s",
						pct, wMB, tMB, speedMBs, etaStr))
				if now.Sub(lastConsole) > 15*time.Second {
					s.App.Logf("Download [%s]: %.1f%% (%.1f/%.1f MB) @ %.1f MB/s (chunked HTTP)",
						name, pct, wMB, tMB, speedMBs)
					lastConsole = now
				}
			}
		}
	}()

	workers := s.App.IADownloadMaxParallel
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ss := range jobs {
				if ctx.Err() != nil {
					return
				}
				if err := s.iaDownloadRange(ctx, urlStr, ref, out, ss.start, ss.end, &written); err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					errMu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	close(progressDone)
	out.Close()

	if firstErr != nil {
		os.Remove(dest)
		return firstErr
	}
	return nil
}

// iaDownloadRange downloads the inclusive byte range [start,end] into out at the same file offsets.
func (s *Service) iaDownloadRange(ctx context.Context, urlStr, ref string, out *os.File, start, end int64, writtenAtomic *int64) error {
	expect := end - start + 1
	var lastErr error
	for attempt := 0; attempt <= app.IAChunkRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * app.IAChunkRetryBase
			s.App.Logf("RETRY chunk bytes=%d-%d (attempt %d/%d): %v — waiting %s",
				start, end, attempt, app.IAChunkRetries, lastErr, wait)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		req.Header.Set("Referer", ref)
		s.App.ApplyArchiveOrgHeaders(req)

		resp, err := s.App.IAHTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request: %w", err)
			continue
		}
		if resp.StatusCode != 206 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d (expected 206 Partial Content)", resp.StatusCode)
			continue
		}

		var chunkWritten int64
		buf := make([]byte, 256*1024)
		var readErr error
		for {
			select {
			case <-ctx.Done():
				resp.Body.Close()
				atomic.AddInt64(writtenAtomic, -chunkWritten)
				return ctx.Err()
			default:
			}
			var n int
			n, readErr = resp.Body.Read(buf)
			if n > 0 {
				off := start + chunkWritten
				if _, wErr := out.WriteAt(buf[:n], off); wErr != nil {
					resp.Body.Close()
					atomic.AddInt64(writtenAtomic, -chunkWritten)
					lastErr = fmt.Errorf("write at +%d: %w", chunkWritten, wErr)
					chunkWritten = 0
					goto nextAttempt
				}
				atomic.AddInt64(writtenAtomic, int64(n))
				chunkWritten += int64(n)
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				break
			}
		}
		resp.Body.Close()
		if readErr != nil && readErr != io.EOF {
			atomic.AddInt64(writtenAtomic, -chunkWritten)
			lastErr = fmt.Errorf("read after %d bytes: %w", chunkWritten, readErr)
			continue
		}
		if chunkWritten != expect {
			atomic.AddInt64(writtenAtomic, -chunkWritten)
			lastErr = fmt.Errorf("range incomplete: got %d want %d bytes", chunkWritten, expect)
			continue
		}
		return nil
	nextAttempt:
	}
	return lastErr
}

// IADownloadSingle is a single-stream download with up to IAChunkRetries retries.
func (s *Service) IADownloadSingle(urlStr, dest, name, ref string) error {
	isIA := strings.Contains(strings.ToLower(urlStr), "archive.org")
	var lastErr error
	for attempt := 0; attempt <= app.IAChunkRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * app.IAChunkRetryBase
			s.App.Logf("RETRY download [%s] (attempt %d/%d): %v — waiting %s",
				name, attempt, app.IAChunkRetries, lastErr, wait)
			time.Sleep(wait)
		}
		lastErr = s.iaDownloadSingleAttempt(urlStr, dest, name, ref, isIA)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func (s *Service) iaDownloadSingleAttempt(urlStr, dest, name, ref string, isIA bool) error {
	client := s.App.IAHTTPClient
	if !isIA {
		client = &http.Client{Timeout: 0}
	}
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", ref)
	if isIA {
		s.App.ApplyArchiveOrgHeaders(req)
	} else {
		req.Header.Set("User-Agent", "Mozilla/5.0")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, urlStr)
	}
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()
	bw := bufio.NewWriterSize(out, app.CopyBufferSize)
	pw := &ProgressWriter{Total: resp.ContentLength, GameName: name, LastLog: time.Now(), StartTime: time.Now(), App: s.App}
	written, err := io.Copy(bw, io.TeeReader(resp.Body, pw))
	if err != nil {
		return fmt.Errorf("interrupted after %.2f MB: %w", float64(written)/1048576, err)
	}
	bw.Flush()
	if resp.ContentLength > 0 && written != resp.ContentLength {
		s.App.Logf("WARN: Size mismatch %s: expected %d got %d", name, resp.ContentLength, written)
	}
	return nil
}
