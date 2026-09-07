package content

import (
	"testing"

	"godsend/models"
)

// TestExtractTUVersion covers the real-world TU filename shapes the Xbox
// Library scan must version-match against XboxUnity candidates, plus the
// adversarial shapes (hex title IDs, version buried mid-name).
func TestExtractTUVersion(t *testing.T) {
	cases := map[string]int{
		"545408A7_TU27_0C48794E.bin": 27, // GODSend's own XboxUnity upload name
		"tu2_545408A7.bin":           2,
		"tu_5_abc":                   5,
		"Title Update v3":            3,
		"TU_4D5307D8_TU27.bin":       27, // hex title ID must not parse as v4
		"TU_545408A7.bin":            0,  // title ID only, no version
		"Virtua Tennis 4 TU5":        5,  // "tu" inside a word must not win
		"update27.4d41f2.bin":        0,  // no tu<digits> marker
		"no version here":            0,
	}
	for name, want := range cases {
		if got := extractTUVersion(name); got != want {
			t.Errorf("extractTUVersion(%q) = %d, want %d", name, got, want)
		}
	}
}

// TestNormalizeTUVersions guards against the report from commit comment
// 199308639: Active must come only from the FTP scan (.disabled suffix),
// never be flipped onto the highest-version merged row.
func TestNormalizeTUVersions(t *testing.T) {
	tus := []models.ContentItem{
		{DisplayName: "Title Update v9", Installed: false},  // highest version, NOT on console
		{DisplayName: "TU3", Installed: true},               // installed but .disabled
		{DisplayName: "TU5", Installed: true, Active: true}, // the real active one
	}
	(&Service{}).normalizeTUVersions(tus)
	if tus[0].Active {
		t.Errorf("non-installed candidate gained Active")
	}
	if tus[1].Active {
		t.Errorf("installed-but-inactive row gained Active")
	}
	if !tus[2].Active {
		t.Errorf("pre-existing Active on the real active row was cleared")
	}
	if tus[0].Version != 9 || tus[1].Version != 3 || tus[2].Version != 5 {
		t.Errorf("versions: got %d/%d/%d want 9/3/5", tus[0].Version, tus[1].Version, tus[2].Version)
	}
}

func TestNormalizeTUVersionFields(t *testing.T) {
	tus := []models.ContentItem{
		{DisplayName: "545408A7_TU27_0C48794E.bin", Installed: true},
		{DisplayName: "garbage.bin", Installed: true},
	}
	(&Service{}).normalizeTUVersions(tus)
	if tus[0].Version != 27 || tus[0].DisplayName != "Title Update v27" {
		t.Errorf("row 0: version=%d name=%q", tus[0].Version, tus[0].DisplayName)
	}
	if tus[1].Version != 0 || tus[1].DisplayName != "garbage.bin" {
		t.Errorf("row 1: version=%d name=%q", tus[1].Version, tus[1].DisplayName)
	}
}
