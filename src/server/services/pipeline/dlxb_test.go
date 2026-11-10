package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"godsend/app"
	"godsend/infrastructure/dlxb"
	"godsend/models"
)

func TestChooseEditionLinksPrefersUSAAndGroupsDiscs(t *testing.T) {
	links := []models.DlxbLink{
		{Host: "adgate", URL: "u1", Label: "Game (Europe) (Disc 1) (Rev 1)"},
		{Host: "adgate", URL: "u2", Label: "Game (Europe) (Disc 2) (Rev 1)"},
		{Host: "adgate", URL: "u3", Label: "Game (USA) (Disc 1) (Single-player)"},
		{Host: "adgate", URL: "u4", Label: "Game (USA) (Disc 2) (Multiplayer)"},
		{Host: "1fichier", URL: "u5", Label: "Game (Japan, Korea) (Disc 1)"},
	}
	got := chooseEditionLinks(links)
	if len(got) != 2 {
		t.Fatalf("chose %d links, want the two USA discs: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Label, "USA") || !strings.Contains(got[1].Label, "USA") {
		t.Fatalf("chose %+v, want USA discs", got)
	}
}

func TestChooseEditionLinksFallsBackWithoutLabels(t *testing.T) {
	links := []models.DlxbLink{
		{Host: "1fichier", URL: "a", Label: "1File"},
		{Host: "mediafire", URL: "b", Label: "Mediafire"},
	}
	got := chooseEditionLinks(links)
	if len(got) != 1 || got[0].URL != "a" {
		t.Fatalf("got %+v, want first group only", got)
	}
}

func TestEditionScoreOrdering(t *testing.T) {
	if editionScore(normalizeEdition("Game (USA)")) <= editionScore(normalizeEdition("Game (Europe)")) {
		t.Fatal("USA should outrank Europe")
	}
	if editionScore(normalizeEdition("Game (Region Free)")) <= editionScore(normalizeEdition("Game (Japan)")) {
		t.Fatal("region free should outrank Japan")
	}
}

func TestGroupDiscLinks(t *testing.T) {
	links := []models.DlxbLink{
		{Host: "adgate", URL: "d2a", Label: "Game (USA) (Disc 2) (Rev 1)"},
		{Host: "adgate", URL: "d1a", Label: "Game (USA) (Disc 1) (Rev 1)"},
		{Host: "1fichier", URL: "d1b", Label: "Game (USA) (Disc 1)"},
		{Host: "1fichier", URL: "d2b", Label: "Game (USA) (Disc 2)"},
		{Host: "direct", URL: "single", Label: "1File"},
	}
	groups := groupDiscLinks(links)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2: %+v", len(groups), groups)
	}
	if groups[0].number != 1 || groups[1].number != 2 {
		t.Fatalf("disc order = %d,%d, want 1,2", groups[0].number, groups[1].number)
	}
	// The untagged "1File" link defaults to Disc 1, so Disc 1 has 3 candidates.
	if len(groups[0].links) != 3 || len(groups[1].links) != 2 {
		t.Fatalf("disc link counts = %d,%d, want 3,2", len(groups[0].links), len(groups[1].links))
	}
}

func TestDiscNumber(t *testing.T) {
	cases := map[string]int{
		"Game (USA) (Disc 1)":    1,
		"Game (Europe) (Disc 2)": 2,
		"Game [DVD3]":            3,
		"Game (CD 4)":            4,
		"Game without a tag":     1,
	}
	for in, want := range cases {
		if got := discNumber(in); got != want {
			t.Errorf("discNumber(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestMultiDiscEditionSelectionKeepsAllDiscs(t *testing.T) {
	links := []models.DlxbLink{
		{Host: "adgate", URL: "e1d1", Label: "Game (Europe) (Disc 1)"},
		{Host: "adgate", URL: "e1d2", Label: "Game (Europe) (Disc 2)"},
		{Host: "adgate", URL: "u1d1", Label: "Game (USA) (Disc 1) (Rev 1)"},
		{Host: "adgate", URL: "u1d2", Label: "Game (USA) (Disc 2) (Rev 1)"},
	}
	chosen := chooseEditionLinks(links)
	groups := groupDiscLinks(chosen)
	if len(groups) != 2 {
		t.Fatalf("chose %+v -> %d discs, want 2", chosen, len(groups))
	}
	for _, g := range groups {
		if !strings.Contains(g.links[0].Label, "USA") {
			t.Fatalf("disc %d from the wrong edition: %+v", g.number, g.links)
		}
	}
}

// Real bundled scrape: the USA edition of Assassin's Creed III has Disc 1 and
// Disc 2, and both must survive edition selection + grouping.
func TestRealCacheGroupsAssassinsCreedDiscs(t *testing.T) {
	data, err := os.ReadFile("../../../../cache/dlxb_games.json")
	if err != nil {
		t.Skip("bundled dlxb cache not present")
	}
	var c models.DlxbCache
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	var entry models.DlxbEntry
	for k, e := range c.Entries {
		if strings.HasPrefix(k, "iso|assassins creed 3") && len(e.Links) > 2 {
			entry = e
			break
		}
	}
	if entry.Title == "" {
		t.Skip("Assassin's Creed III not in the cache")
	}
	groups := groupDiscLinks(chooseEditionLinks(entry.Links))
	if len(groups) < 2 {
		t.Fatalf("%q grouped into %d disc(s), want at least Disc 1 + Disc 2", entry.Title, len(groups))
	}
	if groups[0].number != 1 || groups[1].number != 2 {
		t.Fatalf("disc numbers = %d,%d, want 1,2", groups[0].number, groups[1].number)
	}
	for _, g := range groups {
		if !strings.Contains(strings.ToLower(g.links[0].Label), "usa") {
			t.Fatalf("disc %d is not the USA edition: %q", g.number, g.links[0].Label)
		}
	}
}

func TestResolveDlxbRejectsAllDeadLinks(t *testing.T) {
	s := &Service{App: app.NewApp()}
	_, _, err := s.resolveDlxb(context.Background(), "Game [ISO]", []models.DlxbLink{
		{Host: "1fichier", URL: "https://1fichier.com/?x", Status: dlxb.StatusDead},
		{Host: "direct", URL: "https://example.com/x", Status: dlxb.StatusDead},
	})
	if err == nil {
		t.Fatal("all-dead links should be rejected before any network call")
	}
}
