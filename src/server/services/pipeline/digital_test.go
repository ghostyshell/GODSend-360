package pipeline

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// fakeXboxContentFile writes a minimal header file of the given magic
// ("LIVE", "PIRS", or "CON "): this is the smallest input
// findDigitalContentFiles/ParseXboxHeader will accept as valid.
func fakeXboxContentFile(t *testing.T, path string, magic string, titleID uint32, contentType uint32) {
	t.Helper()
	h := make([]byte, 0x368)
	copy(h[0:4], magic)
	binary.BigEndian.PutUint32(h[0x344:0x348], contentType)
	binary.BigEndian.PutUint32(h[0x360:0x364], titleID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, h, 0644); err != nil {
		t.Fatal(err)
	}
}

// findDigitalContentFiles must return every valid content package in the
// archive, not just the first - regressing this (e.g. stopping the walk
// early) is exactly the "FTP transfers do not complete" bug: only the first
// package of a multi-package XBLA/DLC archive ever got transferred.
func TestFindDigitalContentFiles_MultiplePackages(t *testing.T) {
	dir := t.TempDir()
	// Real IA/Minerva archives extract into a subfolder, not loose at the
	// archive root, and content magics vary (LIVE/PIRS/CON ).
	fakeXboxContentFile(t, filepath.Join(dir, "release", "game.pkg"), "LIVE", 0x41560855, 0x000D0000)
	fakeXboxContentFile(t, filepath.Join(dir, "release", "update.pkg"), "PIRS", 0x41560855, 0x00000002)
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "truncated.pkg"), []byte("LIVEtoo short"), 0644); err != nil {
		t.Fatal(err)
	}

	got := findDigitalContentFiles(dir)
	if len(got) != 2 {
		t.Fatalf("expected 2 content files, got %d: %+v", len(got), got)
	}
	for _, cf := range got {
		if cf.titleID != "41560855" {
			t.Errorf("titleID = %q, want 41560855", cf.titleID)
		}
	}
	types := map[string]bool{got[0].typeDir: true, got[1].typeDir: true}
	if !types["000D0000"] || !types["00000002"] {
		t.Errorf("expected typeDirs 000D0000 and 00000002, got %v", types)
	}
}

// A valid header is still excluded when its extension is on the skip list
// (.txt/.nfo/.jpg release-notes/cover-art files can be large enough to pass
// the size check but must never be mistaken for content).
func TestFindDigitalContentFiles_FilteredExtension(t *testing.T) {
	dir := t.TempDir()
	fakeXboxContentFile(t, filepath.Join(dir, "cover.jpg"), "LIVE", 0x41560855, 0x000D0000)

	if got := findDigitalContentFiles(dir); len(got) != 0 {
		t.Fatalf("expected .jpg to be filtered out even with a valid header, got %d: %+v", len(got), got)
	}
}

func TestFindDigitalContentFiles_None(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("nope"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := findDigitalContentFiles(dir); len(got) != 0 {
		t.Fatalf("expected 0 content files, got %d", len(got))
	}
}

// primaryDigitalContentFile must pick the largest package, not whichever
// sorts first lexically - the non-FTP path installs exactly one file, and a
// title update (e.g. "TU_update.pkg") sorting before the base game must not
// be chosen over it.
func TestPrimaryDigitalContentFile_PicksLargest(t *testing.T) {
	small := digitalContentFile{path: "/x/AAA_update.pkg", size: 1024, titleID: "41560855", typeDir: "00000002"}
	big := digitalContentFile{path: "/x/ZZZ_game.pkg", size: 1024 * 1024 * 500, titleID: "41560855", typeDir: "000D0000"}

	got := primaryDigitalContentFile([]digitalContentFile{small, big})
	if got.path != big.path {
		t.Errorf("got %q, want the larger file %q", got.path, big.path)
	}
}

func TestDistinctTitleIDs(t *testing.T) {
	same := []digitalContentFile{{titleID: "AAAA"}, {titleID: "AAAA"}}
	if ids := distinctTitleIDs(same); len(ids) != 1 {
		t.Errorf("expected 1 distinct titleID, got %v", ids)
	}
	mixed := []digitalContentFile{{titleID: "AAAA"}, {titleID: "BBBB"}}
	if ids := distinctTitleIDs(mixed); len(ids) != 2 {
		t.Errorf("expected 2 distinct titleIDs, got %v", ids)
	}
}
