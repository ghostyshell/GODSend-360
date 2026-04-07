// Package utils — Pure-Go ISO→GOD converter and archive helpers (iso2god.go).
//
// Replaces the bundled iso2god.exe and 7z.exe binaries entirely.
// Xbox 360/Original disc access uses the game-partition offset (XSF/XGD1/XGD2/XGD3)
// and XDVDFS (2048-byte sectors) — the same on-disc layout tools like
// [XboxDev/extract-xiso](https://github.com/XboxDev/extract-xiso) read for extract/create;
// there is no separate “raw ISO” byte read path for console game data.
//
// ISO→GOD pipeline (RunIso2GodNative):
//   1. Detect XGD disc format (XSF / XGD1 / XGD2 / XGD3)
//   2. Parse XDVDFS volume descriptor
//   3. Locate default.xex (Xbox 360) or default.xbe (Xbox Original)
//   4. Extract TitleID, MediaID and execution metadata
//   5. Write GOD data partition files (DataNNNN) with SHA-1 hash tables
//   6. Build inter-partition MHT hash chain
//   7. Write STFS/LIVE CON header file
//
// Output layout under outDir (matches github.com/iliazeus/iso2god-rs file_layout):
//   {TitleID}/00007000/{MediaID}.data/Data0000 … DataNNNN   (Xbox 360 GOD)
//   {TitleID}/00007000/{MediaID}                            ← CON (no extension)
//   Original Xbox: 00005000, folder {TitleID}.data, CON name = TitleID.
//
// Archive helpers replace all exec.Command("7z …") calls:
//   extractArchive   — read ZIP / 7z / RAR archives
//   extractISO       — extract first .iso from an archive
//   createZipFromDir — pack a directory into a store-only ZIP
//   compressROMFile  — pack a single file into a deflated ZIP

package utils

import (
	"archive/zip"
	"crypto/sha1"
	_ "embed"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode"
)

//go:embed data/empty_live.bin
var emptyLIVEBin []byte

func init() {
	if len(emptyLIVEBin) != conHeaderSz {
		panic(fmt.Sprintf("iso2god: embedded data/empty_live.bin: want %d bytes, got %d", conHeaderSz, len(emptyLIVEBin)))
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// Constants
// ══════════════════════════════════════════════════════════════════════════════

const (
	// XDVDFS filesystem
	xdvdfsMagic    = "MICROSOFT*XBOX*MEDIA" // 20-byte volume identifier
	xdvdfsSectorSz = 2048                   // bytes per disc sector
	xdvdfsAttrDir  = 0x10                   // directory attribute flag

	// XEX2 / XBE executable magic strings
	xex2Magic = "XEX2"
	xbeMagic  = "XBEH"

	// XEX2 optional-header key that holds the execution-info block
	xex2ExecInfoKey = uint32(0x00040006)

	// Maximum bytes read from the XEX/XBE header (the full file can be huge)
	xexReadLimit = 65536

	// GOD / STFS constants
	godBlockSz       = 4096                        // bytes per data block
	godBlocksPerPart = 41412                       // 0xA1C4 – data blocks per partition
	godBlocksPerSP   = 204                         // 0xCC   – data blocks per subpart
	godSPsPerPart    = 203                         // 0xCB   – subparts per partition
	godSPSz          = godBlocksPerSP * godBlockSz // subpart data size (835 584 B)
	godHashListSz    = 4096                        // hash-list block is always 4 096 B
	godMaxHashes     = 204                         // max SHA-1 entries per hash list

	// Full blocks per partition = 1 MHT + 203 SHTs + 41 412 data = 41 616
	godBlocksPerPartFull = godBlocksPerPart + godSPsPerPart + 1

	// STFS/LIVE CON header size
	conHeaderSz = 45056 // 0xB000
)

// xgdPartitions maps XGD disc format names to their game-partition byte offsets
// within the ISO image.  Detection order matters: XSF must come first because
// it has offset 0 and would false-positive on all others.
var xgdPartitions = []struct {
	name   string
	offset uint64
}{
	{"XSF", 0},
	{"XGD2", 0x0fd90000},
	{"XGD1", 0x18300000},
	{"XGD3", 0x02080000},
}

// ══════════════════════════════════════════════════════════════════════════════
// Internal types
// ══════════════════════════════════════════════════════════════════════════════

// godHashList is the fixed 4 096-byte block that holds up to 204 × 20-byte
// SHA-1 hashes used in the GOD/STFS hash tree.
type godHashList struct {
	buf [godHashListSz]byte
	n   int // number of hashes written so far
}

func (h *godHashList) addBlockHash(block []byte) {
	d := sha1.Sum(block)
	copy(h.buf[h.n*20:], d[:])
	h.n++
}

func (h *godHashList) addHash(hash [20]byte) {
	copy(h.buf[h.n*20:], hash[:])
	h.n++
}

// digest returns SHA-1 of the entire 4 096-byte buffer (including zero padding).
func (h *godHashList) digest() [20]byte { return sha1.Sum(h.buf[:]) }

// TitleExecInfo contains metadata extracted from a game executable.
type TitleExecInfo struct {
	MediaID        uint32
	Version        uint32
	BaseVersion    uint32
	TitleID        uint32
	Platform       byte
	ExecutableType byte
	DiscNumber     byte
	DiscCount      byte
	IsOriginalXbox bool // true → XBE (Original Xbox), false → XEX2 (Xbox 360)
}

// ══════════════════════════════════════════════════════════════════════════════
// Public entry point: ISO → GOD
// ══════════════════════════════════════════════════════════════════════════════

// RunIso2GodNative converts an Xbox 360 (or Original Xbox) disc ISO into
// Games-on-Demand format entirely in Go; no external binary is required.
//
// Output under outDir (same tree as iliazeus/iso2god-rs):
//
//	{TitleID}/00007000/{MediaID}.data/Data0000 …   (Xbox 360)
//	{TitleID}/00007000/{MediaID}                  ← STFS/LIVE CON header
//	Original Xbox uses 00005000 and {TitleID}.data / CON name = TitleID.
//
// resolveDisplayTitle, if non-nil, is called with the Title ID from the ISO so
// the LIVE header gets UTF-16 display title fields at 0x411 and 0x1691 (same as
// iso2god-rs with_game_title). Pass nil to leave those slots zero.
func RunIso2GodNative(isoPath, outDir string, resolveDisplayTitle func(titleID uint32) string) error {
	f, err := os.Open(isoPath)
	if err != nil {
		return fmt.Errorf("iso2god: open %s: %w", isoPath, err)
	}
	defer f.Close()

	// 1. Detect partition offset.
	partOff, err := detectXGDPartition(f)
	if err != nil {
		return fmt.Errorf("iso2god: %w", err)
	}

	// 2. Read XDVDFS volume descriptor.
	rootSector, rootSize, err := readXDVDFSVolDesc(f, partOff)
	if err != nil {
		return fmt.Errorf("iso2god: volume descriptor: %w", err)
	}

	// 3. Parse game executable for TitleID / MediaID.
	info, err := extractExecInfo(f, partOff, rootSector, rootSize)
	if err != nil {
		return fmt.Errorf("iso2god: exec info: %w", err)
	}

	titleIDStr := fmt.Sprintf("%08X", info.TitleID)
	mediaIDStr := fmt.Sprintf("%08X", info.MediaID)
	if info.IsOriginalXbox {
		mediaIDStr = titleIDStr // XBE has no separate media ID
	}

	// 4. Prepare output directory (iso2god-rs layout: content-type folder + ".data" bundle).
	contentTypeStr := "00007000"
	if info.IsOriginalXbox {
		contentTypeStr = "00005000"
	}
	innerBase := filepath.Join(outDir, titleIDStr, contentTypeStr)
	dataDir := filepath.Join(innerBase, mediaIDStr+".data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("iso2god: mkdir: %w", err)
	}

	// 5. Compute block / partition counts.
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("iso2god: stat ISO: %w", err)
	}
	dataSize := fi.Size() - int64(partOff)
	if dataSize <= 0 {
		return fmt.Errorf("iso2god: no game data after partition offset")
	}
	blockCount := uint64((dataSize + godBlockSz - 1) / godBlockSz)
	partCount := (blockCount + godBlocksPerPart - 1) / godBlocksPerPart

	// 6. Write DataNNNN partition files.
	partSizes := make([]int64, partCount)
	for p := uint64(0); p < partCount; p++ {
		outPath := filepath.Join(dataDir, fmt.Sprintf("Data%04d", p))
		sz, err := writeGODPart(f, partOff, p, blockCount, outPath)
		if err != nil {
			return fmt.Errorf("iso2god: part %d: %w", p, err)
		}
		partSizes[p] = sz
	}

	// 7. Link MHT hash chain across partitions.
	mhtHash, err := buildMHTChain(dataDir, partCount)
	if err != nil {
		return fmt.Errorf("iso2god: MHT chain: %w", err)
	}

	displayTitle := ""
	if resolveDisplayTitle != nil {
		displayTitle = strings.TrimSpace(resolveDisplayTitle(info.TitleID))
	}

	// 8. Write STFS/LIVE CON header (sibling of {MediaID}.data, not inside it).
	conPath := filepath.Join(innerBase, mediaIDStr)
	if err := writeConHeader(conPath, info, blockCount, partCount, partSizes, mhtHash, displayTitle); err != nil {
		return fmt.Errorf("iso2god: CON header: %w", err)
	}

	return nil
}

// ══════════════════════════════════════════════════════════════════════════════
// XDVDFS parsing
// ══════════════════════════════════════════════════════════════════════════════

func detectXGDPartition(f *os.File) (uint64, error) {
	for _, xgd := range xgdPartitions {
		off := int64(xgd.offset) + int64(0x20)*xdvdfsSectorSz
		var buf [20]byte
		if _, err := f.ReadAt(buf[:], off); err != nil {
			continue
		}
		if string(buf[:]) == xdvdfsMagic {
			return xgd.offset, nil
		}
	}
	return 0, fmt.Errorf("XDVDFS magic not found — not a valid Xbox/Xbox 360 ISO")
}

func readXDVDFSVolDesc(f *os.File, partOff uint64) (rootSector, rootSize uint32, err error) {
	// Volume descriptor sits at sector 0x20 from the partition start.
	// The 20-byte magic has already been verified; skip it and read the two u32 fields.
	off := int64(partOff) + int64(0x20)*xdvdfsSectorSz + 20
	var buf [8]byte
	if _, err = f.ReadAt(buf[:], off); err != nil {
		return 0, 0, fmt.Errorf("read volume descriptor: %w", err)
	}
	rootSector = binary.LittleEndian.Uint32(buf[0:4])
	rootSize = binary.LittleEndian.Uint32(buf[4:8])
	return rootSector, rootSize, nil
}

type xdvdfsDirEntry struct {
	sector uint32
	size   uint32
	attrs  byte
	name   string
}

func (e xdvdfsDirEntry) isDir() bool { return e.attrs&xdvdfsAttrDir != 0 }

// readXDVDFSDirTable reads every entry from the directory table stored in the
// given sector range.  Entries are stored sequentially with 4-byte alignment.
func readXDVDFSDirTable(f *os.File, partOff uint64, sector, size uint32) []xdvdfsDirEntry {
	sectorCount := (size + xdvdfsSectorSz - 1) / xdvdfsSectorSz
	var result []xdvdfsDirEntry
	for si := uint32(0); si < sectorCount; si++ {
		off := int64(partOff) + int64(sector+si)*xdvdfsSectorSz
		buf := make([]byte, xdvdfsSectorSz)
		n, err := f.ReadAt(buf, off)
		if n == 0 || (err != nil && err != io.EOF) {
			break
		}
		result = append(result, parseDirSector(buf[:n])...)
	}
	return result
}

// parseDirSector extracts directory entries from a raw 2 048-byte sector buffer.
// Parsing stops when both subtreeLeft and subtreeRight are 0xFFFF (end-of-data
// / padding marker in the XDVDFS format).
func parseDirSector(data []byte) []xdvdfsDirEntry {
	var entries []xdvdfsDirEntry
	pos := 0
	for {
		if pos+14 > len(data) {
			break
		}
		left := binary.LittleEndian.Uint16(data[pos:])
		right := binary.LittleEndian.Uint16(data[pos+2:])
		if left == 0xFFFF || right == 0xFFFF {
			break
		}
		sector := binary.LittleEndian.Uint32(data[pos+4:])
		size := binary.LittleEndian.Uint32(data[pos+8:])
		if size == 0 {
			break
		}
		attrs := data[pos+12]
		nameLen := int(data[pos+13])
		if pos+14+nameLen > len(data) {
			break
		}
		entries = append(entries, xdvdfsDirEntry{
			sector: sector,
			size:   size,
			attrs:  attrs,
			name:   string(data[pos+14 : pos+14+nameLen]),
		})
		after := pos + 14 + nameLen
		pos = (after + 3) &^ 3 // 4-byte align
	}
	return entries
}

// findInDir returns the sector / size of the first entry with the given name
// (case-insensitive) in the directory at (sector, size).
func findInDir(f *os.File, partOff uint64, sector, size uint32, name string) (uint32, uint32, bool) {
	lname := strings.ToLower(name)
	for _, e := range readXDVDFSDirTable(f, partOff, sector, size) {
		if strings.ToLower(e.name) == lname {
			return e.sector, e.size, true
		}
	}
	return 0, 0, false
}

// ══════════════════════════════════════════════════════════════════════════════
// XEX2 / XBE executable parsing
// ══════════════════════════════════════════════════════════════════════════════

func extractExecInfo(f *os.File, partOff uint64, rootSector, rootSize uint32) (*TitleExecInfo, error) {
	// Xbox 360 executable
	if sec, sz, ok := findInDir(f, partOff, rootSector, rootSize, "default.xex"); ok {
		data, err := readISOSlice(f, partOff, sec, sz, xexReadLimit)
		if err != nil {
			return nil, fmt.Errorf("read default.xex: %w", err)
		}
		return parseXEX2(data)
	}
	// Original Xbox executable
	if sec, sz, ok := findInDir(f, partOff, rootSector, rootSize, "default.xbe"); ok {
		data, err := readISOSlice(f, partOff, sec, sz, xexReadLimit)
		if err != nil {
			return nil, fmt.Errorf("read default.xbe: %w", err)
		}
		return parseXBE(data)
	}
	return nil, fmt.Errorf("no game executable (default.xex / default.xbe) found in ISO root")
}

// readISOSlice reads up to limit bytes from a file embedded in the XDVDFS.
func readISOSlice(f *os.File, partOff uint64, sector, size uint32, limit int) ([]byte, error) {
	n := int(size)
	if n > limit {
		n = limit
	}
	buf := make([]byte, n)
	off := int64(partOff) + int64(sector)*xdvdfsSectorSz
	read, err := f.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:read], nil
}

func parseXEX2(data []byte) (*TitleExecInfo, error) {
	if len(data) < 24 {
		return nil, fmt.Errorf("XEX2 data too short (%d B)", len(data))
	}
	if string(data[:4]) != xex2Magic {
		return nil, fmt.Errorf("missing XEX2 magic")
	}
	fieldCount := binary.BigEndian.Uint32(data[20:])
	pos := 24
	for i := uint32(0); i < fieldCount; i++ {
		if pos+8 > len(data) {
			break
		}
		key := binary.BigEndian.Uint32(data[pos:])
		val := binary.BigEndian.Uint32(data[pos+4:])
		pos += 8
		if key != xex2ExecInfoKey {
			continue
		}
		off := int(val)
		if off+20 > len(data) {
			return nil, fmt.Errorf("XEX2 execution-info offset 0x%X out of range (data=%d)", off, len(data))
		}
		d := data[off:]
		return &TitleExecInfo{
			MediaID:        binary.BigEndian.Uint32(d[0:]),
			Version:        binary.BigEndian.Uint32(d[4:]),
			BaseVersion:    binary.BigEndian.Uint32(d[8:]),
			TitleID:        binary.BigEndian.Uint32(d[12:]),
			Platform:       d[16],
			ExecutableType: d[17],
			DiscNumber:     d[18],
			DiscCount:      d[19],
		}, nil
	}
	return nil, fmt.Errorf("XEX2 execution-info optional header (0x40006) not found")
}

func parseXBE(data []byte) (*TitleExecInfo, error) {
	if len(data) < 0x11C+4 {
		return nil, fmt.Errorf("XBE data too short (%d B)", len(data))
	}
	if string(data[:4]) != xbeMagic {
		return nil, fmt.Errorf("missing XBEH magic")
	}
	baseAddr := binary.LittleEndian.Uint32(data[0x104:])
	certAddr := binary.LittleEndian.Uint32(data[0x118:])
	certOff := int(certAddr) - int(baseAddr)
	if certOff < 0 || certOff+12 > len(data) {
		return nil, fmt.Errorf("XBE certificate offset 0x%X out of range", certOff)
	}
	tid := binary.LittleEndian.Uint32(data[certOff+8:])
	return &TitleExecInfo{
		TitleID:        tid,
		DiscNumber:     1,
		DiscCount:      1,
		IsOriginalXbox: true,
	}, nil
}

// ══════════════════════════════════════════════════════════════════════════════
// GOD data partition writing
// ══════════════════════════════════════════════════════════════════════════════

// writeGODPart creates one DataNNNN file.
//
// Partition layout:
//
//	[MHT  4 096 B]
//	[Subpart 0: SHT 4 096 B | 204×4 096 B data blocks]
//	[Subpart 1: SHT 4 096 B | 204×4 096 B data blocks]
//	…
//	[Subpart 202: SHT 4 096 B | ≤204×4 096 B data blocks]
//
// Returns the actual byte size of the written file.
func writeGODPart(iso *os.File, partOff uint64, partIdx, totalBlocks uint64, outPath string) (int64, error) {
	out, err := os.Create(outPath)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	var mht godHashList

	// Write placeholder MHT (overwritten at the end).
	if _, err := out.Write(mht.buf[:]); err != nil {
		return 0, fmt.Errorf("write MHT placeholder: %w", err)
	}

	isoEnd := int64(partOff) + int64(totalBlocks)*godBlockSz
	isoPos := int64(partOff) + int64(partIdx)*int64(godBlocksPerPart)*godBlockSz

	for sp := 0; sp < godSPsPerPart; sp++ {
		if isoPos >= isoEnd {
			break
		}

		// How many bytes to read for this subpart?
		want := int64(godSPSz)
		if isoPos+want > isoEnd {
			want = isoEnd - isoPos
		}

		spData := make([]byte, want)
		n, err := iso.ReadAt(spData, isoPos)
		if err != nil && err != io.EOF {
			return 0, fmt.Errorf("read ISO at 0x%X: %w", isoPos, err)
		}
		spData = spData[:n]
		if len(spData) == 0 {
			break
		}

		// Build Sub Hash Table: one SHA-1 per 4 096-byte block.
		var sht godHashList
		for blk := 0; blk < len(spData); blk += godBlockSz {
			end := blk + godBlockSz
			if end > len(spData) {
				end = len(spData)
			}
			sht.addBlockHash(spData[blk:end])
		}

		// Write SHT, then add its digest to the MHT.
		if _, err := out.Write(sht.buf[:]); err != nil {
			return 0, fmt.Errorf("write SHT: %w", err)
		}
		mht.addBlockHash(sht.buf[:]) // MHT entry = SHA-1(entire SHT block)

		// Write raw data blocks.
		if _, err := out.Write(spData); err != nil {
			return 0, fmt.Errorf("write data: %w", err)
		}

		isoPos += int64(len(spData))
		if int64(len(spData)) < int64(godSPSz) {
			break // last (partial) subpart
		}
	}

	// Rewrite MHT at offset 0 with the accumulated subpart hashes.
	if _, err := out.WriteAt(mht.buf[:], 0); err != nil {
		return 0, fmt.Errorf("write final MHT: %w", err)
	}

	size, err := out.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	return size, nil
}

// buildMHTChain links the MHT of each partition to the one that follows it.
// For N partitions the chain is built right-to-left:
//
//	Part[N-2].MHT  ← append digest(Part[N-1].MHT)
//	Part[N-3].MHT  ← append digest(Part[N-2].MHT updated)
//	…
//	Part[0].MHT    ← append digest(Part[1].MHT updated)
//
// Returns the digest of Part[0]'s final MHT, used as the root hash in the CON header.
func buildMHTChain(dataDir string, partCount uint64) ([20]byte, error) {
	cur, err := readPartMHT(dataDir, partCount-1)
	if err != nil {
		return [20]byte{}, fmt.Errorf("read MHT part %d: %w", partCount-1, err)
	}
	for p := int64(partCount) - 2; p >= 0; p-- {
		prev, err := readPartMHT(dataDir, uint64(p))
		if err != nil {
			return [20]byte{}, fmt.Errorf("read MHT part %d: %w", p, err)
		}
		d := cur.digest()
		prev.addHash(d)
		if err := writePartMHT(dataDir, uint64(p), &prev); err != nil {
			return [20]byte{}, fmt.Errorf("write MHT part %d: %w", p, err)
		}
		cur = prev
	}
	return cur.digest(), nil
}

func readPartMHT(dataDir string, idx uint64) (godHashList, error) {
	path := filepath.Join(dataDir, fmt.Sprintf("Data%04d", idx))
	f, err := os.Open(path)
	if err != nil {
		return godHashList{}, err
	}
	defer f.Close()

	var hl godHashList
	if _, err := io.ReadFull(f, hl.buf[:]); err != nil {
		return godHashList{}, err
	}
	// Count non-zero hash entries.
	var zero [20]byte
	for i := 0; i < godMaxHashes; i++ {
		if [20]byte(hl.buf[i*20:(i+1)*20]) == zero {
			hl.n = i
			return hl, nil
		}
	}
	hl.n = godMaxHashes
	return hl, nil
}

func writePartMHT(dataDir string, idx uint64, mht *godHashList) error {
	path := filepath.Join(dataDir, fmt.Sprintf("Data%04d", idx))
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteAt(mht.buf[:], 0)
	return err
}

// ══════════════════════════════════════════════════════════════════════════════
// STFS / LIVE CON header
// ══════════════════════════════════════════════════════════════════════════════

// writeConHeader creates the 45 056-byte STFS/LIVE metadata header file that
// Aurora/FSD uses to identify and launch a GOD game.
func writeConHeader(path string, info *TitleExecInfo, blockCount, partCount uint64, partSizes []int64, mhtHash [20]byte, displayTitle string) error {
	buf := emptyLIVEHeader()

	var contentType uint32
	if info.IsOriginalXbox {
		contentType = 0x00005000 // Xbox Original
	} else {
		contentType = 0x00007000 // Games on Demand
	}

	// Content type  0x0344 (4 B, BE)
	binary.BigEndian.PutUint32(buf[0x0344:], contentType)

	// Execution info  0x0354–0x0367
	// Note: version (0x0358) and baseVersion (0x035C) are intentionally left as
	// zeroed template values — iso2god-rs does not write these from the XEX2 header.
	binary.BigEndian.PutUint32(buf[0x0354:], info.MediaID)
	binary.BigEndian.PutUint32(buf[0x0360:], info.TitleID)
	buf[0x0364] = info.Platform
	buf[0x0365] = info.ExecutableType
	buf[0x0366] = info.DiscNumber
	buf[0x0367] = info.DiscCount

	// MHT root hash  0x037D (20 B)
	copy(buf[0x037D:], mhtHash[:])

	// Blocks allocated  0x0392 (24-bit BE)
	buf[0x0392] = byte(blockCount >> 16)
	buf[0x0393] = byte(blockCount >> 8)
	buf[0x0394] = byte(blockCount)

	// Blocks not allocated  0x0395 (16-bit BE) — always 0
	buf[0x0395] = 0
	buf[0x0396] = 0

	// Part count  0x03A0 (4 B, LE)
	binary.LittleEndian.PutUint32(buf[0x03A0:], uint32(partCount))

	// Parts total size  0x03A4 (4 B, BE, in 256-byte units)
	// Formula from iso2god-rs:  lastPartSize + (N-1) × fullPartitionBytes
	lastSz := partSizes[len(partSizes)-1]
	fullPartBytes := int64(godBlocksPerPartFull) * godBlockSz
	totalSz := lastSz + int64(partCount-1)*fullPartBytes
	binary.BigEndian.PutUint32(buf[0x03A4:], uint32(totalSz/0x100))

	// Display title (UTF-16 BE + NUL), same offsets as iso2god-rs ConHeaderBuilder::with_game_title.
	if displayTitle != "" {
		writeLIVETitleUTF16BE(buf, displayTitle)
	}

	// Thumbnail: iso2god-rs only touches 0x1712+ when with_game_icon(Some) is used; otherwise
	// empty_live.bin values remain — do not zero here (would diverge from RS).

	// Finalise: match iso2god-rs ConHeaderBuilder::finalize (then SHA-1 over 0x0344..0x0b000).
	buf[0x035B] = 0
	buf[0x035F] = 0
	buf[0x0391] = 0
	digest := sha1.Sum(buf[0x0344 : 0x0b000])
	copy(buf[0x032C:], digest[:])

	return os.WriteFile(path, buf, 0644)
}

const (
	liveTitleSlot1Off   = 0x0411
	liveTitleSlot1Bytes = 0x1691 - liveTitleSlot1Off
	liveTitleSlot2Off   = 0x1691
	liveTitleSlot2Bytes = 0x1712 - liveTitleSlot2Off // stops before PNG size fields
)

// writeLIVETitleUTF16BE writes the same two title regions as iso2god-rs (full string
// in slot 1; same string truncated to slot 2 if needed).
func writeLIVETitleUTF16BE(buf []byte, title string) {
	writeUTF16BENullTerminated(buf, liveTitleSlot1Off, liveTitleSlot1Bytes, title)
	writeUTF16BENullTerminated(buf, liveTitleSlot2Off, liveTitleSlot2Bytes, title)
}

func writeUTF16BENullTerminated(buf []byte, offset, maxBytes int, s string) {
	if maxBytes < 2 || offset < 0 || offset+maxBytes > len(buf) {
		return
	}
	units := utf16.Encode([]rune(s))
	maxUnits := (maxBytes - 2) / 2
	if len(units) > maxUnits {
		units = units[:maxUnits]
	}
	for len(units) > 0 && isUTF16HighSurrogate(units[len(units)-1]) {
		units = units[:len(units)-1]
	}
	pos := 0
	for _, u := range units {
		binary.BigEndian.PutUint16(buf[offset+pos:], u)
		pos += 2
	}
	binary.BigEndian.PutUint16(buf[offset+pos:], 0)
}

func isUTF16HighSurrogate(u uint16) bool { return u >= 0xD800 && u <= 0xDBFF }

// emptyLIVEHeader returns a copy of iliazeus/iso2god-rs `src/god/empty_live.bin` (embedded).
func emptyLIVEHeader() []byte {
	out := make([]byte, conHeaderSz)
	copy(out, emptyLIVEBin)
	return out
}

// ══════════════════════════════════════════════════════════════════════════════
// Archive extraction  (replaces: 7z x …)
// ══════════════════════════════════════════════════════════════════════════════

// extractArchive extracts a .zip, .7z, or .rar archive to destDir using pure-Go
// libraries (archive/zip, bodgit/sevenzip, nwaples/rardecode).
func ExtractArchive(archivePath, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	switch strings.ToLower(filepath.Ext(archivePath)) {
	case ".zip":
		return extractZipAll(archivePath, destDir)
	case ".7z":
		return extract7zAll(archivePath, destDir)
	case ".rar":
		return extractRarAll(archivePath, destDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Ext(archivePath))
	}
}

// ExtractISO extracts the first .iso file found inside a .zip/.7z/.rar archive.
// tempRoot is typically GODSEND_HOME/Temp (caller passes filepath.Join(home, "Temp")).
func ExtractISO(archivePath, safeName, tempRoot string) (string, error) {
	dest := filepath.Join(tempRoot, safeName+"_extracted")
	os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0755); err != nil {
		return "", err
	}
	var err error
	switch strings.ToLower(filepath.Ext(archivePath)) {
	case ".zip":
		err = extractZipFilter(archivePath, dest, ".iso")
	case ".7z":
		err = extract7zFilter(archivePath, dest, ".iso")
	case ".rar":
		err = extractRarFilter(archivePath, dest, ".iso")
	default:
		return "", fmt.Errorf("unsupported archive format: %s", filepath.Ext(archivePath))
	}
	if err != nil {
		return "", err
	}
	iso := findFirstFileByExt(dest, ".iso")
	if iso == "" {
		return "", fmt.Errorf("no .iso found in archive")
	}
	return iso, nil
}

// findFirstFileByExt walks dir and returns the first file with the given extension.
func findFirstFileByExt(dir, ext string) string {
	var found string
	filepath.Walk(dir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(p), ext) {
			found = p
			return io.EOF
		}
		return nil
	})
	return found
}

// ── ZIP ──────────────────────────────────────────────────────────────────────

func extractZipAll(src, destDir string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if err := extractZipEntry(f, destDir); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFilter(src, destDir, wantExt string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if strings.ToLower(filepath.Ext(f.Name)) != wantExt {
			continue
		}
		if err := extractZipEntry(f, destDir); err != nil {
			return err
		}
	}
	return nil
}

func extractZipEntry(f *zip.File, destDir string) error {
	outPath := filepath.Join(destDir, filepath.FromSlash(f.Name))
	if f.FileInfo().IsDir() {
		return os.MkdirAll(outPath, 0755)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// ── 7Z ───────────────────────────────────────────────────────────────────────

func extract7zAll(src, destDir string) error {
	r, err := sevenzip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open 7z: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if err := extract7zEntry(f, destDir); err != nil {
			return err
		}
	}
	return nil
}

func extract7zFilter(src, destDir, wantExt string) error {
	r, err := sevenzip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open 7z: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if strings.ToLower(filepath.Ext(f.Name)) != wantExt {
			continue
		}
		if err := extract7zEntry(f, destDir); err != nil {
			return err
		}
	}
	return nil
}

func extract7zEntry(f *sevenzip.File, destDir string) error {
	outPath := filepath.Join(destDir, filepath.FromSlash(f.Name))
	if f.FileInfo().IsDir() {
		return os.MkdirAll(outPath, 0755)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// ── RAR ──────────────────────────────────────────────────────────────────────

func extractRarAll(src, destDir string) error {
	r, err := rardecode.OpenReader(src, "")
	if err != nil {
		return fmt.Errorf("open rar: %w", err)
	}
	defer r.Close()
	return drainRAR(r, destDir, "")
}

func extractRarFilter(src, destDir, wantExt string) error {
	r, err := rardecode.OpenReader(src, "")
	if err != nil {
		return fmt.Errorf("open rar: %w", err)
	}
	defer r.Close()
	return drainRAR(r, destDir, wantExt)
}

func drainRAR(r *rardecode.ReadCloser, destDir, wantExt string) error {
	for {
		h, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if wantExt != "" && strings.ToLower(filepath.Ext(h.Name)) != wantExt {
			continue
		}
		outPath := filepath.Join(destDir, filepath.FromSlash(h.Name))
		if h.IsDir {
			os.MkdirAll(outPath, 0755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		out, err := os.Create(outPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, r); err != nil {
			out.Close()
			return err
		}
		out.Close()
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// Archive creation  (replaces: 7z a -t7z -mx0 / -mx=1 …)
// ══════════════════════════════════════════════════════════════════════════════

// createZipFromDir archives all files under dir into a ZIP file at outPath.
// Files are stored without compression (zip.Store), matching the old -mx0 flag.
// The archive is readable by Aurora/FSD's ZipFile API on the Xbox 360.
func CreateZipFromDir(dir, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	defer w.Close()

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		fh, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		fh.Name = filepath.ToSlash(rel)
		fh.Method = zip.Store
		fw, err := w.CreateHeader(fh)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(fw, src)
		return err
	})
}

// ══════════════════════════════════════════════════════════════════════════════
// Disc-info probe  (lightweight — no conversion, just reads XEX metadata)
// ══════════════════════════════════════════════════════════════════════════════

// ProbeISODiscInfo opens an ISO, reads the XDVDFS, and returns TitleExecInfo
// from the game executable. No output files are written.
func ProbeISODiscInfo(isoPath string) (*TitleExecInfo, error) {
	f, err := os.Open(isoPath)
	if err != nil {
		return nil, fmt.Errorf("probeDiscInfo: open: %w", err)
	}
	defer f.Close()
	partOff, err := detectXGDPartition(f)
	if err != nil {
		return nil, fmt.Errorf("probeDiscInfo: %w", err)
	}
	rootSector, rootSize, err := readXDVDFSVolDesc(f, partOff)
	if err != nil {
		return nil, fmt.Errorf("probeDiscInfo: volDesc: %w", err)
	}
	return extractExecInfo(f, partOff, rootSector, rootSize)
}

// ══════════════════════════════════════════════════════════════════════════════
// Content-disc extraction  (Disc 2+ DLC/bonus content from XDVDFS)
// ══════════════════════════════════════════════════════════════════════════════

// extractXDVDFSContentToDir extracts the secondary-disc content files from an
// ISO image into destDir (flat — no enclosing subfolder).
//
// It navigates: content/0000000000000000/{TitleID|FFED2000}/{00000002|first-dir}/
// and copies every file recursively into destDir.
func ExtractXDVDFSContentToDir(isoPath, destDir string, info *TitleExecInfo) error {
	f, err := os.Open(isoPath)
	if err != nil {
		return fmt.Errorf("contentExtract: open: %w", err)
	}
	defer f.Close()

	partOff, err := detectXGDPartition(f)
	if err != nil {
		return fmt.Errorf("contentExtract: partition: %w", err)
	}
	rootSector, rootSize, err := readXDVDFSVolDesc(f, partOff)
	if err != nil {
		return fmt.Errorf("contentExtract: volDesc: %w", err)
	}

	// Navigate content/0000000000000000/
	cSec, cSz, ok := findInDir(f, partOff, rootSector, rootSize, "content")
	if !ok {
		return fmt.Errorf("contentExtract: no content/ dir in ISO")
	}
	zSec, zSz, ok := findInDir(f, partOff, cSec, cSz, "0000000000000000")
	if !ok {
		return fmt.Errorf("contentExtract: no content/0000000000000000/ in ISO")
	}

	// Find TitleID subfolder; fall back to FFED2000 or first dir found.
	titleIDStr := fmt.Sprintf("%08X", info.TitleID)
	tSec, tSz, ok := findInDir(f, partOff, zSec, zSz, titleIDStr)
	if !ok {
		if sec, sz, ok2 := findInDir(f, partOff, zSec, zSz, "FFED2000"); ok2 {
			tSec, tSz = sec, sz
		} else {
			for _, e := range readXDVDFSDirTable(f, partOff, zSec, zSz) {
				if e.isDir() {
					tSec, tSz = e.sector, e.size
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("contentExtract: no TitleID subfolder in content/0000000000000000/")
			}
		}
	}

	// Find 00000002 subfolder (standard secondary-disc content code); fall back to first dir.
	dSec, dSz, ok := findInDir(f, partOff, tSec, tSz, "00000002")
	if !ok {
		for _, e := range readXDVDFSDirTable(f, partOff, tSec, tSz) {
			if e.isDir() {
				dSec, dSz = e.sector, e.size
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("contentExtract: no 00000002 subfolder found")
		}
	}

	return xdvdfsExtractRecursive(f, partOff, dSec, dSz, destDir)
}

// ProbeContentPackageTitleID opens an ISO and reads the real Title ID from the
// first STFS/CON content package in the content directory.
//
// Many publishers (e.g. 2K Games) ship Add-On Content Discs whose default.xex
// carries a generic placeholder Title ID such as 0xFFED2000 rather than the
// parent game's real Title ID. The individual content packages (CON/LIVE/PIRS
// STFS files) embedded in content/0000000000000000/…/00000002/ always contain
// the correct Title ID at STFS header offset 0x0360. This function reads that
// value so the server can install to the right folder on the Xbox.
//
// Returns (0, nil) when no valid STFS package with a non-placeholder Title ID
// is found (caller should fall back to game-name heuristics or warn).
func ProbeContentPackageTitleID(isoPath string, info *TitleExecInfo) (uint32, error) {
	f, err := os.Open(isoPath)
	if err != nil {
		return 0, fmt.Errorf("probeContentTitleID: open: %w", err)
	}
	defer f.Close()

	partOff, err := detectXGDPartition(f)
	if err != nil {
		return 0, fmt.Errorf("probeContentTitleID: %w", err)
	}
	rootSector, rootSize, err := readXDVDFSVolDesc(f, partOff)
	if err != nil {
		return 0, fmt.Errorf("probeContentTitleID: volDesc: %w", err)
	}

	// Navigate content/0000000000000000/
	cSec, cSz, ok := findInDir(f, partOff, rootSector, rootSize, "content")
	if !ok {
		return 0, nil
	}
	zSec, zSz, ok := findInDir(f, partOff, cSec, cSz, "0000000000000000")
	if !ok {
		return 0, nil
	}

	// Find TitleID subfolder (same fallback logic as ExtractXDVDFSContentToDir).
	titleIDStr := fmt.Sprintf("%08X", info.TitleID)
	tSec, tSz, ok := findInDir(f, partOff, zSec, zSz, titleIDStr)
	if !ok {
		if sec, sz, ok2 := findInDir(f, partOff, zSec, zSz, "FFED2000"); ok2 {
			tSec, tSz = sec, sz
		} else {
			for _, e := range readXDVDFSDirTable(f, partOff, zSec, zSz) {
				if e.isDir() {
					tSec, tSz = e.sector, e.size
					ok = true
					break
				}
			}
			if !ok {
				return 0, nil
			}
		}
	}

	// Find 00000002 subfolder (standard secondary-disc content type code).
	dSec, dSz, ok := findInDir(f, partOff, tSec, tSz, "00000002")
	if !ok {
		for _, e := range readXDVDFSDirTable(f, partOff, tSec, tSz) {
			if e.isDir() {
				dSec, dSz = e.sector, e.size
				ok = true
				break
			}
		}
		if !ok {
			return 0, nil
		}
	}

	// Read the first 0x0364 bytes of each file in the directory and look for an
	// STFS magic (CON /LIVE/PIRS) followed by a non-placeholder Title ID at 0x0360.
	const stfsHeaderSize = 0x0364
	for _, e := range readXDVDFSDirTable(f, partOff, dSec, dSz) {
		if e.isDir() || e.size < stfsHeaderSize {
			continue
		}
		data, err := readISOSlice(f, partOff, e.sector, e.size, stfsHeaderSize)
		if err != nil || len(data) < stfsHeaderSize {
			continue
		}
		magic := string(data[:4])
		if magic != "CON " && magic != "LIVE" && magic != "PIRS" {
			continue
		}
		tid := binary.BigEndian.Uint32(data[0x0360:])
		if tid != 0 && tid != 0xFFED2000 {
			return tid, nil
		}
	}
	return 0, nil
}

// findXEXRootDirRecursive returns the XDVDFS directory (sector/size) that contains
// default.xex or default.xbe, searching subdirectories depth-first.
func findXEXRootDirRecursive(f *os.File, partOff uint64, dirSec, dirSz uint32) (uint32, uint32, bool) {
	entries := readXDVDFSDirTable(f, partOff, dirSec, dirSz)
	for _, e := range entries {
		if e.isDir() {
			continue
		}
		ln := strings.ToLower(e.name)
		if ln == "default.xex" || ln == "default.xbe" {
			return dirSec, dirSz, true
		}
	}
	for _, e := range entries {
		if !e.isDir() {
			continue
		}
		if ds, dz, ok := findXEXRootDirRecursive(f, partOff, e.sector, e.size); ok {
			return ds, dz, true
		}
	}
	return 0, 0, false
}

// ExtractXEXFolderFromISO extracts the XDVDFS directory tree that contains default.xex
// (Xbox 360) or default.xbe (Original Xbox) into destDir — equivalent to a loose XEX
// folder layout for FTP/HTTP install.
func ExtractXEXFolderFromISO(isoPath, destDir string) error {
	f, err := os.Open(isoPath)
	if err != nil {
		return fmt.Errorf("xexFromISO: open: %w", err)
	}
	defer f.Close()
	partOff, err := detectXGDPartition(f)
	if err != nil {
		return fmt.Errorf("xexFromISO: partition: %w", err)
	}
	rootSector, rootSize, err := readXDVDFSVolDesc(f, partOff)
	if err != nil {
		return fmt.Errorf("xexFromISO: volDesc: %w", err)
	}
	xSec, xSz, ok := findXEXRootDirRecursive(f, partOff, rootSector, rootSize)
	if !ok {
		return fmt.Errorf("xexFromISO: no default.xex or default.xbe in ISO")
	}
	if err := os.RemoveAll(destDir); err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	return xdvdfsExtractRecursive(f, partOff, xSec, xSz, destDir)
}

// xdvdfsExtractRecursive recursively copies an XDVDFS directory tree into destDir.
func xdvdfsExtractRecursive(f *os.File, partOff uint64, sector, size uint32, destDir string) error {
	for _, e := range readXDVDFSDirTable(f, partOff, sector, size) {
		dest := filepath.Join(destDir, e.name)
		if e.isDir() {
			if err := os.MkdirAll(dest, 0755); err != nil {
				return err
			}
			if err := xdvdfsExtractRecursive(f, partOff, e.sector, e.size, dest); err != nil {
				return err
			}
		} else {
			if err := xdvdfsExtractFile(f, partOff, e.sector, e.size, dest); err != nil {
				return err
			}
		}
	}
	return nil
}

// xdvdfsExtractFile copies a single XDVDFS file to a local path.
func xdvdfsExtractFile(f *os.File, partOff uint64, sector, size uint32, destPath string) error {
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()
	off := int64(partOff) + int64(sector)*xdvdfsSectorSz
	remaining := int64(size)
	buf := make([]byte, 256*1024)
	for remaining > 0 {
		toRead := int64(len(buf))
		if toRead > remaining {
			toRead = remaining
		}
		n, readErr := f.ReadAt(buf[:toRead], off)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		if n == 0 {
			break
		}
		off += int64(n)
		remaining -= int64(n)
	}
	return nil
}

// CompressROMFile packages a single ROM file into a deflated ZIP archive.
// Equivalent to the old: 7z a -mx=1 -mmt=on destArchive romFile
func CompressROMFile(romFile, destArchive string) error {
	f, err := os.Create(destArchive)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	defer w.Close()

	info, err := os.Stat(romFile)
	if err != nil {
		return err
	}
	fh, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	fh.Name = filepath.Base(romFile)
	fh.Method = zip.Deflate
	fw, err := w.CreateHeader(fh)
	if err != nil {
		return err
	}
	src, err := os.Open(romFile)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(fw, src)
	return err
}
