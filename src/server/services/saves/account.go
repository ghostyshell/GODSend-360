// account.go — Xbox 360 profile gamertag extraction from STFS Account block.
//
// Walks the STFS file table to find the "Account" file, then RC4+HMAC-SHA1
// decrypts it to read the gamertag. This is the authoritative method used by
// Velocity (hetelek/Velocity) and py360 (arkem/py360); see CHANGELOG 2.12.16.
package saves

import (
	"bytes"
	"crypto/hmac"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"unicode/utf16"
)

// retailAccountKey is the public, fixed HMAC key Xbox 360 uses for retail
// Account block encryption. Devkit profiles use a different key (devkitAccountKey).
var retailAccountKey = []byte{
	0xE1, 0xBC, 0x15, 0x9C, 0x73, 0xB1, 0xEA, 0xE9,
	0xAB, 0x31, 0x70, 0xF3, 0xAD, 0x47, 0xEB, 0xF3,
}

var devkitAccountKey = []byte{
	0xDA, 0xB6, 0x9A, 0xD9, 0x8E, 0x28, 0x76, 0x4F,
	0x97, 0x7E, 0xE2, 0x48, 0x7E, 0x4F, 0x3F, 0x68,
}

const (
	stfsBlockSize = 0x1000
	// Volume descriptor fields (all relative to file start).
	vdHeaderSizeOff       = 0x340 // uint32 BE
	vdBlockSeparationOff  = 0x37B // byte; bit0 set ⇒ R/W (hash blocks doubled)
	vdFileTableBlockCount = 0x37C // uint16 LE
	vdFileTableBlockNum   = 0x37E // 3 bytes LE
	vdTotalAllocBlocks    = 0x395 // uint32 BE
)

// ExtractGamertagFromProfilePackage parses an Xbox 360 profile STFS package
// and returns the gamertag stored in its embedded Account file. Returns "" if
// the package cannot be parsed (truncated, unknown format, missing Account).
func ExtractGamertagFromProfilePackage(data []byte) string {
	if len(data) < 0x971 { // header + volume descriptor minimum
		return ""
	}
	magic := string(data[0:4])
	if magic != "CON " && magic != "PIRS" && magic != "LIVE" {
		return ""
	}

	headerSize := binary.BigEndian.Uint32(data[vdHeaderSizeOff : vdHeaderSizeOff+4])
	blockSep := data[vdBlockSeparationOff] & 0x01 // 1 ⇒ hash blocks doubled
	ftBlockCount := binary.LittleEndian.Uint16(data[vdFileTableBlockCount : vdFileTableBlockCount+2])
	ftBlockNum := uint32(data[vdFileTableBlockNum]) |
		uint32(data[vdFileTableBlockNum+1])<<8 |
		uint32(data[vdFileTableBlockNum+2])<<16
	totalBlocks := binary.BigEndian.Uint32(data[vdTotalAllocBlocks : vdTotalAllocBlocks+4])

	// Walk file table; entries are 0x40 bytes each, packed in `ftBlockCount`
	// consecutive data blocks starting at `ftBlockNum`.
	for i := uint16(0); i < ftBlockCount; i++ {
		off := blockToOffset(headerSize, ftBlockNum+uint32(i), blockSep, totalBlocks)
		if off+stfsBlockSize > uint32(len(data)) {
			break
		}
		blk := data[off : off+stfsBlockSize]
		for j := 0; j+0x40 <= len(blk); j += 0x40 {
			entry := blk[j : j+0x40]
			nameLen := int(entry[0x28] & 0x3F)
			if nameLen == 0 || nameLen > 0x28 {
				continue
			}
			name := string(entry[:nameLen])
			if name != "Account" {
				continue
			}
			startBlock := uint32(entry[0x2F]) |
				uint32(entry[0x30])<<8 |
				uint32(entry[0x31])<<16
			fileSize := binary.BigEndian.Uint32(entry[0x34:0x38])
			if fileSize == 0 || fileSize > 0x1000 {
				return ""
			}
			accOff := blockToOffset(headerSize, startBlock, blockSep, totalBlocks)
			if accOff+fileSize > uint32(len(data)) {
				return ""
			}
			return decryptAccountGamertag(data[accOff : accOff+fileSize])
		}
	}
	return ""
}

// blockToOffset converts an STFS data-block index to a byte offset inside the
// package. Data blocks start at the next 0x1000-aligned boundary after the
// header (so a headerSize of 0x971A puts the first hash block at 0xA000).
func blockToOffset(headerSize, blockNum uint32, blockSep byte, totalBlocks uint32) uint32 {
	dataStart := (headerSize + 0xFFF) &^ uint32(0xFFF)

	hashMul := uint32(1)
	if blockSep == 1 {
		hashMul = 2
	}

	// Hash-table blocks before `blockNum`:
	//   - One L0 hash per 0xAA data blocks (the +1 covers the L0 for this block).
	//   - If totalBlocks > 0xAA, one L1 table sits before all L0 tables.
	//   - If totalBlocks > 0x70E4, one L2 table sits before all L1 tables.
	skip := (blockNum/0xAA + 1) * hashMul
	if totalBlocks > 0xAA {
		skip += hashMul // L1
	}
	if totalBlocks > 0x70E4 {
		skip += hashMul // L2
	}
	return dataStart + (blockNum+skip)*stfsBlockSize
}

// decryptAccountGamertag RC4-decrypts an Account file (typically 0x194 bytes)
// and returns the embedded gamertag. The first 0x10 bytes of the file are an
// HMAC-SHA1 hash that doubles as the RC4 key seed.
//
// Decrypted layout (verified empirically against real profiles; matches the
// "+8 confounder" form used by Microsoft's serializer — Velocity's confounder
// constant is the literal "Velocity" written on re-encryption):
//
//	0x00  confounder     (8 bytes — random per-file IV, ignored on read)
//	0x08  reservedFlags  (uint32)
//	0x0C  liveFlags      (uint32)
//	0x10  gamertag       (UTF-16BE, 16 chars max, NUL-terminated/padded → 0x20 bytes)
//	0x30  xuid           (uint64 BE)
func decryptAccountGamertag(file []byte) string {
	if len(file) < 0x10+0x30 {
		return ""
	}
	hash := file[:0x10]
	cipher := file[0x10:]

	// Try retail key first, then devkit. Whichever yields a plausible
	// printable gamertag wins; otherwise return "".
	for _, key := range [][]byte{retailAccountKey, devkitAccountKey} {
		mac := hmac.New(sha1.New, key)
		mac.Write(hash)
		rc4Key := mac.Sum(nil)[:0x10]

		c, err := rc4.NewCipher(rc4Key)
		if err != nil {
			continue
		}
		plain := make([]byte, len(cipher))
		c.XORKeyStream(plain, cipher)

		if len(plain) < 0x30 {
			continue
		}
		gt := readUTF16BE(plain[0x10:0x30])
		if isPlausibleGamertag(gt) {
			return gt
		}
	}
	return ""
}

func readUTF16BE(b []byte) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i < len(b); i += 2 {
		c := uint16(b[i])<<8 | uint16(b[i+1])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

// isPlausibleGamertag rejects decryption failures by checking the result looks
// like a Microsoft gamertag: 1–16 chars, printable, no control bytes.
func isPlausibleGamertag(s string) bool {
	if len(s) < 1 || len(s) > 16 {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7F {
			return false
		}
	}
	// Reject all-NUL or obviously garbage decoded output.
	return !bytes.ContainsRune([]byte(s), 0xFFFD)
}
