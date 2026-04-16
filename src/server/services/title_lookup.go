package services

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Xbox / GOD title display name resolution: XboxUnity → XboxDB (JSON when available) →
// embedded copy of iliazeus/iso2god-rs titles.jsonl (MIT). Used for LIVE CON UTF-16
// title, FTP GOD folder names, and godsend.ini titlename.

// TitleLookupLog is optional; when set, diagnostic lines use the same style as main.logf.
var TitleLookupLog func(format string, args ...interface{})

func logtl(format string, args ...interface{}) {
	if TitleLookupLog != nil {
		TitleLookupLog(format, args...)
	}
}

var (
	iso2godTitlesRaw  []byte
	iso2godTitleMap   map[string]string
	iso2godTitlesOnce sync.Once
)

// RegisterIso2GodTitlesJSONL supplies embedded iso2god-rs titles.jsonl bytes (call from main, typically in init).
func RegisterIso2GodTitlesJSONL(data []byte) {
	iso2godTitlesRaw = data
}

func initIso2GodTitleMap() {
	iso2godTitlesOnce.Do(func() {
		iso2godTitleMap = make(map[string]string, 6200)
		if len(iso2godTitlesRaw) == 0 {
			return
		}
		sc := bufio.NewScanner(bytes.NewReader(iso2godTitlesRaw))
		lineBuf := make([]byte, 0, 64*1024)
		sc.Buffer(lineBuf, 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var row struct {
				TitleID string `json:"TitleID"`
				Name    string `json:"Name"`
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				continue
			}
			id := strings.ToUpper(strings.TrimSpace(row.TitleID))
			name := strings.TrimSpace(row.Name)
			if id != "" && name != "" {
				iso2godTitleMap[id] = name
			}
		}
	})
}

func lookupTitleNameIso2GodEmbedded(titleID string) string {
	initIso2GodTitleMap()
	name := iso2godTitleMap[titleID]
	if name != "" {
		logtl("iso2god-titles: resolved %s → %s", titleID, name)
	}
	return name
}

// lookupTitleNameXboxDB uses https://xboxdb.altervista.org/api/{title_id} when the
// server returns JSON (documented format); otherwise returns "" without error.
func lookupTitleNameXboxDB(titleID string) string {
	apiURL := "https://xboxdb.altervista.org/api/" + strings.ToLower(titleID)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		logtl("XboxDB: request failed for %s: %v", titleID, err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil || len(body) == 0 {
		return ""
	}
	if body[0] != '{' {
		return ""
	}
	var v struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(body, &v) != nil {
		return ""
	}
	name := strings.TrimSpace(v.Name)
	if name == "" {
		return ""
	}
	logtl("XboxDB: resolved %s → %s", titleID, name)
	return name
}

// lookupTitleNameXboxUnity queries XboxUnity TitleList (legacy behaviour).
func lookupTitleNameXboxUnity(titleID string) string {
	apiURL := "http://xboxunity.net/Resources/Lib/TitleList.php?search=" + titleID
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		logtl("XboxUnity: request build error: %v", err)
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		logtl("XboxUnity: request failed for %s: %v", titleID, err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		logtl("XboxUnity: HTTP %d for %s", resp.StatusCode, titleID)
		return ""
	}
	var result struct {
		Items []struct {
			Name string `json:"Name"`
		} `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logtl("XboxUnity: JSON decode error for %s: %v", titleID, err)
		return ""
	}
	if len(result.Items) > 0 && result.Items[0].Name != "" {
		logtl("XboxUnity: resolved %s → %s", titleID, result.Items[0].Name)
		return result.Items[0].Name
	}
	logtl("XboxUnity: no result for %s", titleID)
	return ""
}

// isTruncatedName returns true when a title name looks like it was cut short
// by the data source (e.g. XboxUnity stores "Cloudy with a..." instead of the
// full title).  We treat trailing "..." or unicode ellipsis as truncated.
func isTruncatedName(name string) bool {
	return strings.HasSuffix(name, "...") || strings.HasSuffix(name, "\u2026")
}

// LookupTitleName resolves an 8-hex Title ID for display / folder naming.
// Order: XboxUnity → XboxDB → embedded iso2god-rs title list.
// If a source returns a truncated name (ending with "..."), the next source
// is tried in hopes of getting the full title.
func LookupTitleName(titleID string) string {
	titleID = strings.ToUpper(strings.TrimSpace(titleID))
	if len(titleID) != 8 {
		return ""
	}
	var truncated string
	if s := lookupTitleNameXboxUnity(titleID); s != "" {
		if !isTruncatedName(s) {
			return s
		}
		truncated = s
	}
	if s := lookupTitleNameXboxDB(titleID); s != "" {
		if !isTruncatedName(s) {
			return s
		}
		if truncated == "" || len(s) > len(truncated) {
			truncated = s
		}
	}
	if s := lookupTitleNameIso2GodEmbedded(titleID); s != "" {
		if !isTruncatedName(s) {
			return s
		}
		if truncated == "" || len(s) > len(truncated) {
			truncated = s
		}
	}
	// All sources returned truncated names; return the longest one as best-effort.
	return truncated
}
