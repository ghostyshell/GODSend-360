// compat.go — multi-disc compatibility table and helpers.
package models

import (
	"regexp"
	"strings"
)

// DiscCompatRec holds the recommended install method for a known multi-disc title.
type DiscCompatRec struct {
	InstallType string // "god" or "content"
	Notes       string
}

// DiscCompatTable maps TitleID → recommendation for Disc 2+ of known titles.
// Sourced from docs/reference/multi-disc-compatibility.md.
var DiscCompatTable = map[uint32]DiscCompatRec{
	0x4D5308AB: {InstallType: "content", Notes: "Disc 2 is bonus content loaded by Disc 1"},
	0x555307DC: {InstallType: "content", Notes: "Disc 2 is bonus content"},
	0x5345082C: {InstallType: "content", Notes: "Disc 2 is bonus content loaded by Disc 1"},
	0x53450833: {InstallType: "content", Notes: "Disc 2 is bonus content loaded by Disc 1"},
	0x545407E7: {InstallType: "content", Notes: "GOTY / multi-disc: Disc 2 is DLC content (Borderlands)"},
	0x5454087C: {InstallType: "content", Notes: "GOTY / multi-disc: Disc 2 is DLC content (Borderlands 2)"},
	0x4541082F: {InstallType: "content", Notes: "Disc 2 is bonus content"},
	0x41560855: {InstallType: "content", Notes: "Disc 2 is multiplayer/zombies content"},
	0x41560817: {InstallType: "content", Notes: "Disc 2 is spec ops content"},
	0x41560882: {InstallType: "content", Notes: "Disc 2 is spec ops content"},
	0x41560812: {InstallType: "content", Notes: "Disc 2 is multiplayer content"},
	0x4541085F: {InstallType: "content", Notes: "Disc 2 is bonus content"},
	0x45410850: {InstallType: "content", Notes: "Disc 2 is bonus content"},
	0x45410889: {InstallType: "content", Notes: "Disc 2 is bonus content"},
	0x524B4005: {InstallType: "content", Notes: "Disc 2/3 are bonus content"},
	0x4541082E: {InstallType: "content", Notes: "Disc 2 is bonus content"},
	0x4541097C: {InstallType: "content", Notes: "Disc 2 is bonus content"},
	0x5254082A: {InstallType: "content", Notes: "Disc 2 is multiplayer content"},
	0x5553083E: {InstallType: "content", Notes: "Disc 2 continues the game as content"},
	0x5454082B: {InstallType: "content", Notes: "Disc 2 (Undead Nightmare) is content"},
	0x5553081A: {InstallType: "content", Notes: "Disc 2 is bonus content"},
	0x4541091B: {InstallType: "content", Notes: "Disc 2 is bonus content"},
	0x5454086B: {InstallType: "content", Notes: "Disc 2 is high-res texture pack"},
	0x5553088F: {InstallType: "content", Notes: "Disc 2 is bonus content"},
	0x4541089C: {InstallType: "content", Notes: "Disc 2 is bonus content"},
	0x0B4607F2: {InstallType: "god", Notes: "Disc 2 is game continuation"},
	0x4D5307E6: {InstallType: "god", Notes: "Disc 2 is game continuation"},
	0x4D5307F1: {InstallType: "god", Notes: "Disc 2 is game continuation"},
	0x4D53082D: {InstallType: "god", Notes: "Disc 2 contains car/track data"},
	0x4D53087F: {InstallType: "god", Notes: "Disc 2 contains car/track data"},
	0x5345200A: {InstallType: "god", Notes: "Disc 2 is game continuation"},
	0x4D530877: {InstallType: "god", Notes: "Disc 2 is multiplayer disc"},
	0x4D530830: {InstallType: "god", Notes: "Multi-disc RPG — all discs are GOD"},
	0x5345082D: {InstallType: "god", Notes: "Disc 2 is game continuation"},
	0x4D530810: {InstallType: "god", Notes: "Disc 2 is game continuation"},
}

// DiscCompat returns the compat recommendation for a given TitleID and disc number.
func DiscCompat(titleID uint32, discNumber byte) DiscCompatRec {
	if discNumber <= 1 {
		return DiscCompatRec{InstallType: "god"}
	}
	if rec, ok := DiscCompatTable[titleID]; ok {
		return rec
	}
	return DiscCompatRec{InstallType: "content", Notes: "Default: Disc 2+ is typically content"}
}

// Redump-style names often use [DVD2] instead of "Disc 2"; Lua menu uses the same idea.
var multiDiscNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bdisc\s*[2-9]\b`),
	regexp.MustCompile(`(?i)\bdisk\s*[2-9]\b`),
	regexp.MustCompile(`(?i)\bcd\s*[2-9]\b`),
	regexp.MustCompile(`(?i)\(disc\s*[2-9]\)`),
	regexp.MustCompile(`(?i)\(disk\s*[2-9]\)`),
	regexp.MustCompile(`(?i)\(cd\s*[2-9]\)`),
	regexp.MustCompile(`(?i)\[dvd\s*[2-9]\]`),
	regexp.MustCompile(`(?i)\[dvd[2-9]\]`),
	regexp.MustCompile(`(?i)\bdvd\s*[2-9]\b`),
	regexp.MustCompile(`(?i)\[cd\s*[2-9]\]`),
}

// IsMultiDiscGameName returns true if the name matches a multi-disc naming pattern.
func IsMultiDiscGameName(name string) bool {
	for _, re := range multiDiscNamePatterns {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// GuessTitleIDFromMultiDiscName maps common IA/Redump strings to Title IDs for /disc-info
// when there is no ISO in Transfer yet (filename-only hint).
func GuessTitleIDFromMultiDiscName(name string) uint32 {
	l := strings.ToLower(name)
	if strings.Contains(l, "borderlands 2") && (strings.Contains(l, "goty") || strings.Contains(l, "game of the year") || strings.Contains(l, "triple pack")) {
		return 0x5454087C
	}
	if strings.Contains(l, "borderlands") && strings.Contains(l, "pre-sequel") {
		return 0
	}
	// "Add-On Content Disc" releases for Borderlands GOTY use placeholder XEX TitleID FFED2000;
	// the content belongs under the main game's TitleID 545407E7.
	if strings.Contains(l, "borderlands") && (strings.Contains(l, "goty") || strings.Contains(l, "game of the year") || strings.Contains(l, "triple pack") || strings.Contains(l, "add-on content")) {
		return 0x545407E7
	}
	return 0
}

// IsContentDiscPlaceholderTitleID returns true when the title ID read from a
// content disc's XEX is a known publisher placeholder rather than the parent
// game's real Title ID.
func IsContentDiscPlaceholderTitleID(tid uint32) bool {
	switch tid {
	case 0xFFED2000: // Borderlands GOTY Add-On Content Disc (2K Games placeholder)
		return true
	}
	return false
}
