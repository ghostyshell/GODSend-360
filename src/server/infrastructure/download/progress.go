// progress.go — ProgressWriter for tracking download progress.
package download

import (
	"fmt"
	"time"

	"godsend/app"
)

// ProgressWriter is an io.Writer that tracks download progress and logs it.
type ProgressWriter struct {
	Total       int64
	Written     int64
	GameName    string
	LastLog     time.Time // logStatus cadence (500 ms — feeds Lua progress)
	LastConsole time.Time // logf cadence (15 s — feeds Electron terminal)
	StartTime   time.Time
	App         *app.App
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n := len(p)
	pw.Written += int64(n)
	now := time.Now()
	if now.Sub(pw.LastLog) > 500*time.Millisecond || pw.Written == pw.Total {
		percent := float64(pw.Written) / float64(pw.Total) * 100
		elapsed := now.Sub(pw.StartTime).Seconds()
		if elapsed < 0.001 {
			elapsed = 0.001
		}
		speedMBs := float64(pw.Written) / elapsed / 1048576
		writtenMB := float64(pw.Written) / 1048576
		totalMB := float64(pw.Total) / 1048576
		elapsedStr := app.FmtDuration(elapsed)
		var etaStr string
		if speedMBs > 0 && percent < 100 {
			etaSecs := float64(pw.Total-pw.Written) / (speedMBs * 1048576)
			etaStr = "~" + app.FmtDuration(etaSecs) + " left"
		} else {
			etaStr = "done"
		}
		pw.App.LogStatus(pw.GameName, "Processing",
			fmt.Sprintf("Downloading: %.0f%% (%.0f/%.0f MB) @ %.1f MB/s | %s | %s",
				percent, writtenMB, totalMB, speedMBs, elapsedStr, etaStr))
		if now.Sub(pw.LastConsole) > 15*time.Second || pw.Written == pw.Total {
			pw.App.Logf("Download [%s]: %.1f%% (%.1f/%.1f MB) @ %.1f MB/s | %s",
				pw.GameName, percent, writtenMB, totalMB, speedMBs, elapsedStr)
			pw.LastConsole = now
		}
		pw.LastLog = now
	}
	return n, nil
}
