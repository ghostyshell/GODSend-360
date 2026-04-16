// handlers_tools.go — ISO conversion and probing tool HTTP handlers.
package http

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"godsend/services"
	"godsend/utils"
)

// toolsCleanISOFileName derives a human-readable title from an ISO filename
// by stripping the extension and removing trailing region/format tags in
// parentheses or brackets, e.g. "Cloudy with a Chance of Meatballs (USA).iso"
// → "Cloudy with a Chance of Meatballs".
var reTrailingTags = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]\s*$`)

func toolsCleanISOFileName(isoPath string) string {
	base := strings.TrimSuffix(filepath.Base(isoPath), filepath.Ext(isoPath))
	for reTrailingTags.MatchString(base) {
		base = reTrailingTags.ReplaceAllString(base, "")
	}
	return strings.TrimSpace(base)
}

// toolsResolveTitleName returns the best available display name for a title ID,
// falling back to a cleaned ISO filename when the online/embedded lookup yields
// a truncated result (e.g. "Cloudy with a..." → prefer filename).
func toolsResolveTitleName(titleID, isoPath string) string {
	looked := services.LookupTitleName(titleID)
	if looked != "" && !services.IsTruncatedName(looked) {
		return looked
	}
	// Lookup was empty or truncated — use the ISO filename as a better source.
	fromFile := toolsCleanISOFileName(isoPath)
	if fromFile != "" {
		return fromFile
	}
	// Last resort: return the (possibly truncated) lookup.
	return looked
}

// POST /tools/probe-iso  { "isoPath": "C:\\...\\game.iso" }
// Returns title info from the ISO without converting.
func (d *Deps) handleToolsProbeISO(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != "POST" {
		jsonError(w, 405, "POST required")
		return
	}
	var req struct {
		ISOPath string `json:"isoPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ISOPath == "" {
		jsonError(w, 400, "Missing isoPath")
		return
	}
	info, err := utils.ProbeISODiscInfo(req.ISOPath)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("Probe failed: %v", err))
		return
	}
	titleIDStr := fmt.Sprintf("%08X", info.TitleID)
	displayName := toolsResolveTitleName(titleIDStr, req.ISOPath)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"titleId":        titleIDStr,
		"mediaId":        fmt.Sprintf("%08X", info.MediaID),
		"discNumber":     info.DiscNumber,
		"discCount":      info.DiscCount,
		"isOriginalXbox": info.IsOriginalXbox,
		"displayName":    displayName,
	})
}

// POST /tools/iso2god  { "isoPath": "...", "outDir": "..." }
// Converts an ISO to GOD format. Output goes into outDir/{DisplayName} - {TitleID}/
func (d *Deps) handleToolsISO2GOD(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != "POST" {
		jsonError(w, 405, "POST required")
		return
	}
	var req struct {
		ISOPath string `json:"isoPath"`
		OutDir  string `json:"outDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ISOPath == "" || req.OutDir == "" {
		jsonError(w, 400, "Missing isoPath or outDir")
		return
	}

	info, err := utils.ProbeISODiscInfo(req.ISOPath)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("Probe failed: %v", err))
		return
	}
	titleIDStr := fmt.Sprintf("%08X", info.TitleID)
	displayName := toolsResolveTitleName(titleIDStr, req.ISOPath)

	safeName := toolsSanitizeFileName(displayName)
	folderName := fmt.Sprintf("%s - %s", safeName, titleIDStr)
	godOutDir := filepath.Join(req.OutDir, folderName)
	if err := os.MkdirAll(godOutDir, 0755); err != nil {
		jsonError(w, 500, fmt.Sprintf("mkdir: %v", err))
		return
	}

	d.App.Logf("[TOOLS] ISO2GOD: %s → %s", filepath.Base(req.ISOPath), godOutDir)
	resolveTitle := func(tid uint32) string {
		return toolsResolveTitleName(fmt.Sprintf("%08X", tid), req.ISOPath)
	}
	if err := utils.RunIso2GodNative(req.ISOPath, godOutDir, resolveTitle); err != nil {
		jsonError(w, 500, fmt.Sprintf("ISO2GOD failed: %v", err))
		return
	}
	d.App.Logf("[TOOLS] ISO2GOD: Conversion complete — %s", folderName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"titleId":     titleIDStr,
		"displayName": displayName,
		"outputDir":   godOutDir,
	})
}

// POST /tools/iso2xex  { "isoPath": "...", "outDir": "..." }
// Extracts XEX folder from ISO. Output goes into outDir/{DisplayName} - {TitleID}/
func (d *Deps) handleToolsISO2XEX(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != "POST" {
		jsonError(w, 405, "POST required")
		return
	}
	var req struct {
		ISOPath string `json:"isoPath"`
		OutDir  string `json:"outDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ISOPath == "" || req.OutDir == "" {
		jsonError(w, 400, "Missing isoPath or outDir")
		return
	}

	info, err := utils.ProbeISODiscInfo(req.ISOPath)
	if err != nil {
		jsonError(w, 500, fmt.Sprintf("Probe failed: %v", err))
		return
	}
	titleIDStr := fmt.Sprintf("%08X", info.TitleID)
	displayName := toolsResolveTitleName(titleIDStr, req.ISOPath)

	safeName := toolsSanitizeFileName(displayName)
	folderName := fmt.Sprintf("%s - %s", safeName, titleIDStr)
	xexOutDir := filepath.Join(req.OutDir, folderName)

	d.App.Logf("[TOOLS] ISO2XEX: %s → %s", filepath.Base(req.ISOPath), xexOutDir)
	if err := utils.ExtractXEXFolderFromISO(req.ISOPath, xexOutDir); err != nil {
		jsonError(w, 500, fmt.Sprintf("ISO2XEX failed: %v", err))
		return
	}
	d.App.Logf("[TOOLS] ISO2XEX: Extraction complete — %s", folderName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"titleId":     titleIDStr,
		"displayName": displayName,
		"outputDir":   xexOutDir,
	})
}

// toolsSanitizeFileName removes characters not allowed in file/folder names.
func toolsSanitizeFileName(name string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	s := replacer.Replace(name)
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return strings.TrimSpace(strings.Trim(s, "_. "))
}
