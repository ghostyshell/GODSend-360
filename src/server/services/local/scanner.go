// scanner.go — local Transfer folder scanning, ISO matching, and ready-state checks.
package local

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"godsend/app"
	"godsend/infrastructure/helpers"
)

// Service manages local Transfer folder operations.
type Service struct {
	App *app.App
}

// ==========================================
// LOCAL TRANSFER FOLDER HELPERS
// ==========================================

var (
	// Aurora host buffer reuse can concatenate a browse URL onto a title.
	browseURLLeakPattern = regexp.MustCompile(
		`https?://[\d.]+:\d+/browse\?platform=[a-zA-Z0-9_]+|` +
			`\d{1,3}(?:\.\d{1,3}){3}:\d+/browse\?platform=[a-zA-Z0-9_]+|` +
			`\d+:\d+/browse\?platform=[a-zA-Z0-9_]+`,
	)
	// Aurora letter-jump can leave one ASCII letter after ")"
	trailingParenJumpLetter = regexp.MustCompile(`\)([a-zA-Z])$`)
	// Some Aurora menu buffers can append tiny prompt tails to the game title.
	localQueryTailLeakPattern = regexp.MustCompile(`^[A-Za-z0-9 ]{1,24}$`)
)

// NormalizeClientGameName strips junk Aurora sometimes sends on the `game` query param.
func NormalizeClientGameName(s string) string {
	s = strings.TrimSpace(s)
	if loc := browseURLLeakPattern.FindStringIndex(s); loc != nil {
		s = strings.TrimSpace(s[:loc[0]])
	}
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.ToValidUTF8(s, "")
	s = strings.TrimFunc(s, unicode.IsControl)
	for len(s) > 0 {
		r, sz := utf8.DecodeLastRuneInString(s)
		if r == utf8.RuneError && sz == 1 {
			s = s[:len(s)-1]
			continue
		}
		if unicode.IsControl(r) || r == '\uFFFD' {
			s = s[:len(s)-sz]
			continue
		}
		break
	}
	s = strings.ReplaceAll(s, "\u00A0", " ")
	s = strings.TrimSpace(s)
	for i := 0; i < 8 && trailingParenJumpLetter.MatchString(s); i++ {
		s = strings.TrimSpace(trailingParenJumpLetter.ReplaceAllString(s, ")"))
	}
	return s
}

func (s *Service) ScanTransferFolder() []string {
	entries, err := os.ReadDir(s.App.TransferDir)
	if err != nil {
		return nil
	}
	var games []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(strings.ToLower(n), ".iso") {
			games = append(games, strings.TrimSuffix(n, filepath.Ext(n)))
		}
	}
	sort.Strings(games)
	return games
}

func normalizeLocalBasename(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\uFF0E", "."))
	s = strings.ReplaceAll(s, "\u00A0", " ")
	return s
}

// FindLocalISOExact matches the ISO basename (no extension) case-insensitively.
func (s *Service) FindLocalISOExact(gameName string) string {
	entries, err := os.ReadDir(s.App.TransferDir)
	if err != nil {
		return ""
	}
	want := normalizeLocalBasename(gameName)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
			continue
		}
		base := normalizeLocalBasename(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		if strings.EqualFold(base, want) {
			return filepath.Join(s.App.TransferDir, e.Name())
		}
	}
	return ""
}

func (s *Service) FindLocalISO(gameName string) string {
	gameName = strings.TrimSpace(gameName)
	if gameName == "" {
		return ""
	}
	if p := s.FindLocalISOExact(gameName); p != "" {
		return p
	}
	if strings.Contains(gameName, " ") {
		if p := s.FindLocalISOExact(strings.ReplaceAll(gameName, " ", "+")); p != "" {
			s.App.Logf("LOCAL ISO: matched %q using space→+ fallback (query '+' vs filename)", gameName)
			return p
		}
	}
	// Fallback: tolerate short leaked alpha tails appended after an otherwise exact ISO basename.
	entries, _ := os.ReadDir(s.App.TransferDir)
	var tailMatched string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
			continue
		}
		base := normalizeLocalBasename(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		if len(gameName) < len(base) || !strings.EqualFold(gameName[:len(base)], base) {
			continue
		}
		suffix := strings.TrimSpace(gameName[len(base):])
		if suffix == "" || !localQueryTailLeakPattern.MatchString(suffix) {
			continue
		}
		if tailMatched != "" {
			tailMatched = ""
			break
		}
		tailMatched = filepath.Join(s.App.TransferDir, e.Name())
	}
	if tailMatched != "" {
		s.App.Logf("LOCAL ISO: matched %q by trimming short leaked title suffix", gameName)
		return tailMatched
	}
	// Prefix fallback
	prefixLen := len(gameName) * 60 / 100
	if prefixLen > 4 {
		prefix := strings.ToLower(normalizeLocalBasename(gameName[:prefixLen]))
		var prefixMatch string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
				continue
			}
			base := strings.ToLower(normalizeLocalBasename(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))))
			if strings.HasPrefix(base, prefix) {
				if prefixMatch != "" {
					prefixMatch = ""
					break
				}
				prefixMatch = filepath.Join(s.App.TransferDir, e.Name())
			}
		}
		if prefixMatch != "" {
			s.App.Logf("LOCAL ISO: matched %q using 60%% prefix fallback (%d chars)", gameName, prefixLen)
			return prefixMatch
		}
	}
	var isoNames []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".iso") {
			continue
		}
		isoNames = append(isoNames, e.Name())
		if len(isoNames) >= 24 {
			break
		}
	}
	s.App.Logf("LOCAL ISO miss: query=%q transferDir=%s isoFiles=%v", gameName, s.App.TransferDir, isoNames)
	return ""
}

func (s *Service) IsGameReadyLocally(gameName string) bool {
	_, err := os.Stat(filepath.Join(s.App.ToolsDir, "Ready", helpers.SanitizeFilename(gameName), "godsend.ini"))
	return err == nil
}
