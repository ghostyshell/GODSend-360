package utils

import (
	"encoding/binary"
	"testing"
)

// entry builds one XDVDFS directory record: left/right subtree offsets,
// start sector, size, attributes, name length, name, dword-padded.
func entry(sector, size uint32, attrs byte, name string) []byte {
	b := make([]byte, 14+len(name))
	binary.LittleEndian.PutUint16(b[0:], 0)
	binary.LittleEndian.PutUint16(b[2:], 0)
	binary.LittleEndian.PutUint32(b[4:], sector)
	binary.LittleEndian.PutUint32(b[8:], size)
	b[12] = attrs
	b[13] = byte(len(name))
	copy(b[14:], name)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// A 0-byte file must not terminate the directory table. GTA V Install Disc 1
// stores a 0-byte disc1.rsn as the AVL root, so treating size==0 as the end
// hid default.xex and content/ (issue #4).
func TestParseDirSectorKeepsEntriesAfterZeroSizeFile(t *testing.T) {
	data := make([]byte, 2048)
	for i := range data {
		data[i] = 0xFF // sector tail padding
	}
	var buf []byte
	buf = append(buf, entry(100, 0, 0, "disc1.rsn")...)
	buf = append(buf, entry(200, 4096, xdvdfsAttrDir, "content")...)
	buf = append(buf, entry(300, 26000000, 0, "default.xex")...)
	copy(data, buf)

	got := parseDirSector(data)
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(got), got)
	}
	if got[0].name != "disc1.rsn" || got[0].size != 0 {
		t.Errorf("entry 0 = %+v", got[0])
	}
	if got[1].name != "content" || !got[1].isDir() {
		t.Errorf("entry 1 = %+v", got[1])
	}
	if got[2].name != "default.xex" {
		t.Errorf("entry 2 = %+v", got[2])
	}
}

// Zero-size directories are now visible to the parser, and the "first dir
// found" fallbacks in ProbeContentPackageTitleID / ExtractXDVDFSContentToDir
// must skip them - an empty dir has no table, so picking one would report a
// successful content install that extracted nothing.
func TestParseDirSectorSurfacesEmptyDirForCallerToSkip(t *testing.T) {
	data := make([]byte, 2048)
	for i := range data {
		data[i] = 0xFF
	}
	var buf []byte
	buf = append(buf, entry(400, 0, xdvdfsAttrDir, "empty")...)
	buf = append(buf, entry(500, 2048, xdvdfsAttrDir, "00000002")...)
	copy(data, buf)

	got := parseDirSector(data)
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	var picked *xdvdfsDirEntry
	for i := range got {
		if got[i].isDir() && got[i].size > 0 {
			picked = &got[i]
			break
		}
	}
	if picked == nil || picked.name != "00000002" {
		t.Fatalf("fallback picked %+v, want 00000002", picked)
	}
}

// Zero-filled padding (rather than 0xFF) still terminates the walk.
func TestParseDirSectorStopsOnZeroPadding(t *testing.T) {
	data := make([]byte, 2048)
	copy(data, entry(300, 26000000, 0, "default.xex"))
	got := parseDirSector(data)
	if len(got) != 1 || got[0].name != "default.xex" {
		t.Fatalf("got %+v, want just default.xex", got)
	}
}
