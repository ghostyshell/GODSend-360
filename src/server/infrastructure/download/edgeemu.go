// edgeemu.go — EdgeEmu parallel range downloads for ROM files.
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

// DownloadEdgeEmuWithProgress downloads from edgeemu.net using parallel range
// requests when supported, falling back to single-stream otherwise.
func (s *Service) DownloadEdgeEmuWithProgress(urlStr, dest, name string) error {
	req, err := http.NewRequest("HEAD", urlStr, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0")
		if resp, err := s.App.EdgeEmuHTTPClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				size := resp.ContentLength
				rangeOK := strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes") && size > 0
				if rangeOK && size >= app.IAParallelThreshold && s.App.IADownloadMaxParallel > 1 {
					nSeg := (size + app.IASegmentSize - 1) / app.IASegmentSize
					s.App.Logf("[%s] Chunked ROM download: %.0f MB, %d segments (~%d MiB each), up to %d parallel HTTP",
						name, float64(size)/1048576, nSeg, app.IASegmentSize/(1024*1024), s.App.IADownloadMaxParallel)
					return s.downloadEdgeEmuChunkedParallel(urlStr, dest, name, size)
				}
			}
		}
	}
	return s.downloadEdgeEmuSingle(urlStr, dest, name)
}

// downloadEdgeEmuSingle is a retrying single-stream download for edgeemu.net.
func (s *Service) downloadEdgeEmuSingle(urlStr, dest, name string) error {
	var lastErr error
	for attempt := 0; attempt <= app.IAChunkRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * app.IAChunkRetryBase
			s.App.Logf("RETRY ROM [%s] attempt %d: %v — waiting %s", name, attempt, lastErr, wait)
			time.Sleep(wait)
		}
		req, err := http.NewRequest("GET", urlStr, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := s.App.EdgeEmuHTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request: %w", err)
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		out, err := os.Create(dest)
		if err != nil {
			resp.Body.Close()
			return err
		}
		bw := bufio.NewWriterSize(out, app.CopyBufferSize)
		pw := &ProgressWriter{Total: resp.ContentLength, GameName: name, LastLog: time.Now(), StartTime: time.Now(), App: s.App}
		written, err := io.Copy(bw, io.TeeReader(resp.Body, pw))
		resp.Body.Close()
		bw.Flush()
		out.Close()
		if err != nil {
			os.Remove(dest)
			lastErr = fmt.Errorf("interrupted after %.2f MB: %w", float64(written)/1048576, err)
			continue
		}
		return nil
	}
	return lastErr
}

// downloadEdgeEmuChunkedParallel is the edgeemu.net counterpart to IADownloadChunkedParallel.
func (s *Service) downloadEdgeEmuChunkedParallel(urlStr, dest, name string, totalSize int64) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if err := out.Truncate(totalSize); err != nil {
		out.Close()
		os.Remove(dest)
		return err
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
				etaStr := "..."
				if speedMBs > 0 && pct < 100 {
					etaSecs := float64(totalSize-w) / (speedMBs * 1048576)
					etaStr = "~" + app.FmtDuration(etaSecs) + " left"
				}
				s.App.LogStatus(name, "Processing",
					fmt.Sprintf("Downloading: %.0f%% (%.0f/%.0f MB) @ %.1f MB/s | %s",
						pct, float64(w)/1048576, float64(totalSize)/1048576, speedMBs, etaStr))
				if now.Sub(lastConsole) > 15*time.Second {
					s.App.Logf("ROM Download [%s]: %.1f%% @ %.1f MB/s (chunked HTTP)", name, pct, speedMBs)
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
				if err := s.edgeEmuDownloadRange(ctx, urlStr, out, ss.start, ss.end, &written); err != nil {
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

func (s *Service) edgeEmuDownloadRange(ctx context.Context, urlStr string, out *os.File, start, end int64, writtenAtomic *int64) error {
	expect := end - start + 1
	var lastErr error
	for attempt := 0; attempt <= app.IAChunkRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * app.IAChunkRetryBase
			s.App.Logf("RETRY ROM chunk bytes=%d-%d attempt %d: %v — waiting %s", start, end, attempt, lastErr, wait)
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
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := s.App.EdgeEmuHTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request: %w", err)
			continue
		}
		if resp.StatusCode != 206 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d (expected 206)", resp.StatusCode)
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
					lastErr = fmt.Errorf("write: %w", wErr)
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
			lastErr = fmt.Errorf("read: %w", readErr)
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
