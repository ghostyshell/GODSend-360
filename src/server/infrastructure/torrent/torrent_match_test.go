package torrent

import "testing"

// Regression: aria2c writes the on-disk file under its torrent-metadata name (literal
// apostrophe), while entry.FileName carries the scraped HTML entity (&#39;). The two must
// still match, or the post-download verify falsely reports "not found" and the completed
// download gets wiped by the temp-dir cleanup.
func TestTorrentBasenameMatches_HTMLEntityApostrophe(t *testing.T) {
	onDisk := "Assassin's Creed IV - Black Flag (Europe) (Pl,Ru,Cs,Hu) (Disc 1).zip"
	entry := "Assassin&#39;s Creed IV - Black Flag (Europe) (Pl,Ru,Cs,Hu) (Disc 1).zip"
	if !torrentBasenameMatches(onDisk, entry) {
		t.Fatalf("expected match between on-disk %q and entry %q", onDisk, entry)
	}
	// Symmetric, and the plain/identical cases still hold.
	if !torrentBasenameMatches(entry, onDisk) {
		t.Fatal("match should be symmetric")
	}
	if torrentBasenameMatches("Halo 3.zip", onDisk) {
		t.Fatal("unrelated names must not match")
	}
}
