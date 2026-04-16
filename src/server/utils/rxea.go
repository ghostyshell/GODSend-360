// Package utils — Pure-Go RXEA codec.
//
// Aurora (Xbox 360 dashboard) stores game artwork in a proprietary GPU-texture
// container whose magic bytes spell "RXEA".  Each file holds up to 25 asset
// slots (icon, banner, boxart, background, up to 20 screenshots).  The pixel
// data is compressed with DXT1/DXT3/DXT5, stored in linear row-major order
// with each row padded to a 32-block stride, and byte-swapped 8-in-16 before
// serialization (the GPU_FETCH_CONSTANT header reports endian=1 and tiled=1
// but the on-disk layout is linear, not Xenos-swizzled).
//
// Naming convention used by Aurora:
//   BK{TitleId}.asset — background  (slot 4)
//   GC{TitleId}.asset — game cover  (slot 2)
//   GL{TitleId}.asset — icon+banner (slots 0+1)
//   SS{TitleId}.asset — screenshots (slots 5–24)
//
// This codec implements both directions:
//   Decode: RXEA bytes  →  []image.NRGBA  (to be cached as PNG)
//   Encode: image.Image →  RXEA bytes     (to FTP-upload directly to the console)
//
// References:
//   • 010-Editor binary template by MaesterRowen & Swizzy (AuroraAssetEditor repo)
//   • Xenia GPU emulator  — TextureFormat enum & texture_address.h tiling description
//   • BC/DXT specification (Microsoft D3D10 compressed texture spec)
package utils

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
)

// ──────────────────────────────────────────────────────────────────────────────
// Public types
// ──────────────────────────────────────────────────────────────────────────────

// AssetSlot identifies a logical slot inside an RXEA file.
type AssetSlot int

const (
	SlotIcon       AssetSlot = 0
	SlotBanner     AssetSlot = 1
	SlotBoxart     AssetSlot = 2
	SlotSlot       AssetSlot = 3 // reserved / unused
	SlotBackground AssetSlot = 4
	SlotScreenshot AssetSlot = 5 // screenshots 5–24
	slotMax        AssetSlot = 25
)

// RXEAEntry is one decoded asset from an RXEA file.
type RXEAEntry struct {
	Slot   AssetSlot
	Width  int
	Height int
	Img    *image.NRGBA
}

// ──────────────────────────────────────────────────────────────────────────────
// File-format constants (all numeric values are big-endian on disk)
// ──────────────────────────────────────────────────────────────────────────────

const (
	rxeaMagic    uint32 = 0x52584541 // 'R','X','E','A'
	rxeaVersion  uint32 = 1
	rxeaDataOff         = 2048 // image data starts here
	rxeaNumSlots        = 25   // ASSET_MAX
	rxeaEntryLen        = 64   // ASSET_PACK_ENTRY size in bytes
	// Aurora native header layout (28 bytes):
	//   [0x00..0x03] magic 'RXEA'
	//   [0x04..0x07] version
	//   [0x08..0x0B] unused (we store total tiled size for our own reference)
	//   [0x0C..0x0F] populated-slot bitmask (1<<slot)
	//   [0x10..0x13] screenshot count (only nonzero for SS files)
	//   [0x14..0x1B] zero padding
	// Entries start at 0x1C.
	rxeaTableOff = 28
)

// Xbox 360 GPU texture format codes (Xenia's TextureFormat enum).
const (
	gpuFmt8888 = 6  // k_8_8_8_8       – uncompressed ARGB
	gpuFmtDXT1 = 18 // k_DXT1          – 8 bytes/block
	gpuFmtDXT3 = 19 // k_DXT2_3        – 16 bytes/block
	gpuFmtDXT5 = 20 // k_DXT4_5 (BC3)  – 16 bytes/block
)

// ──────────────────────────────────────────────────────────────────────────────
// DecodeRXEA — RXEA bytes → decoded images
// ──────────────────────────────────────────────────────────────────────────────

// RXEADiag holds per-slot decode diagnostics (non-fatal errors and raw header fields).
type RXEADiag struct {
	Slot      int    `json:"slot"`
	Offset    uint32 `json:"offset"`
	Size      uint32 `json:"size"`
	GpuFmt    int    `json:"gpu_fmt"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Tiled     bool   `json:"tiled"`
	Endian    int    `json:"endian"`
	Error     string `json:"error,omitempty"`
}

// DecodeRXEA parses an RXEA blob and returns a decoded image for every
// non-empty slot it finds, plus diagnostics for every non-empty slot.
// Only a catastrophic file-level failure returns a non-nil error.
func DecodeRXEA(data []byte) ([]*RXEAEntry, []RXEADiag, error) {
	if len(data) < rxeaDataOff {
		return nil, nil, fmt.Errorf("rxea: file too short (%d bytes)", len(data))
	}
	gotMagic := binary.BigEndian.Uint32(data[0:])
	if gotMagic != rxeaMagic {
		hex16 := fmt.Sprintf("%X", data[:min(16, len(data))])
		return nil, nil, fmt.Errorf("rxea: bad magic %08X (first 16 bytes: %s)", gotMagic, hex16)
	}
	// Accept any version — Aurora has shipped files with both version 1 and 2.

	slotMask := binary.BigEndian.Uint32(data[12:])

	var out   []*RXEAEntry
	var diags []RXEADiag
	dataCursor := uint32(rxeaDataOff)
	for i := 0; i < rxeaNumSlots; i++ {
		base := rxeaTableOff + i*rxeaEntryLen
		if base+rxeaEntryLen > len(data) {
			break
		}
		ent := data[base : base+rxeaEntryLen]

		// Aurora native layout has no per-entry offset/size. Populated slots
		// are advertised by the bitmask in header[0x0C]; tiled size is computed
		// from the GPU fetch constants (dw7 pitch × alignedBH × blockSize).
		if slotMask != 0 && (slotMask>>uint(i))&1 == 0 {
			continue
		}
		dw7 := binary.BigEndian.Uint32(ent[32:])
		dw8 := binary.BigEndian.Uint32(ent[36:])
		dw9 := binary.BigEndian.Uint32(ent[40:])
		if slotMask == 0 && dw8 == 0 && dw9 == 0 {
			continue // empty slot (no bitmask, no GPU constants)
		}

		diag := RXEADiag{
			Slot:   i,
			GpuFmt: int(dw8 & 0x3F),
			Width:  int(dw9&0x1FFF) + 1,
			Height: int((dw9>>13)&0x1FFF) + 1,
			Tiled:  (dw7>>1)&1 == 1,
			Endian: int((dw8 >> 6) & 0x3),
		}

		size, err := tiledSizeFromFetch(dw7, dw8, dw9)
		if err != nil {
			diag.Error = err.Error()
			diags = append(diags, diag)
			continue
		}
		diag.Offset = dataCursor - uint32(rxeaDataOff)
		diag.Size = size

		start := dataCursor
		end := start + size
		if int(end) > len(data) {
			diag.Error = fmt.Sprintf("data out of bounds (off=%d size=%d fileLen=%d)", diag.Offset, size, len(data))
			diags = append(diags, diag)
			continue
		}
		raw := data[start:end]
		dataCursor = end

		img, err := decodeSlotFetch(dw7, dw8, dw9, raw)
		if err != nil {
			diag.Error = err.Error()
			diags = append(diags, diag)
			continue
		}
		diags = append(diags, diag)
		out = append(out, &RXEAEntry{
			Slot:   AssetSlot(i),
			Width:  img.Bounds().Dx(),
			Height: img.Bounds().Dy(),
			Img:    img,
		})
	}
	return out, diags, nil
}

// DecodeRXEASlot decodes a single, specific slot from data; returns nil if the
// slot is empty or cannot be decoded.
func DecodeRXEASlot(data []byte, slot AssetSlot) (*image.NRGBA, error) {
	entries, _, err := DecodeRXEA(data)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Slot == slot {
			return e.Img, nil
		}
	}
	return nil, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// EncodeRXEA — image → RXEA bytes
// ──────────────────────────────────────────────────────────────────────────────

// EncodeRXEA encodes a single image into a minimal RXEA file with only the
// specified slot populated.  The image is DXT5-compressed and tiled using the
// Xbox 360 macro-tile layout.
func EncodeRXEA(slot AssetSlot, img image.Image) ([]byte, error) {
	if slot < 0 || slot >= slotMax {
		return nil, fmt.Errorf("rxea: invalid slot %d", slot)
	}
	bounds  := img.Bounds()
	widthPx  := bounds.Dx()
	heightPx := bounds.Dy()
	if widthPx <= 0 || heightPx <= 0 {
		return nil, fmt.Errorf("rxea: empty image")
	}

	// Convert to NRGBA for uniform pixel access.
	nrgba := toNRGBA(img)

	const blockSz = 16 // DXT5 = BC3: 16 bytes per 4×4 block
	bw := (widthPx + 3) / 4
	bh := (heightPx + 3) / 4

	// Compress to linear DXT5 blocks.
	linearDXT := encodeDXT5(nrgba, bw, bh)

	// Apply endian swap (8-in-16, matching real Aurora .asset files).
	for i := 0; i+1 < len(linearDXT); i += 2 {
		linearDXT[i], linearDXT[i+1] = linearDXT[i+1], linearDXT[i]
	}

	// Aurora stores the blocks linearly with rows padded to a 32-block
	// stride (matches the pitch field real .asset files report).
	alignedBW := alignUp(bw, 32)
	alignedBH := alignUp(bh, 32)
	storedDXT := make([]byte, alignedBW*alignedBH*blockSz)
	rowBytes := bw * blockSz
	strideBytes := alignedBW * blockSz
	for by := 0; by < bh; by++ {
		copy(storedDXT[by*strideBytes:], linearDXT[by*rowBytes:(by+1)*rowBytes])
	}

	tiledSize := uint32(len(storedDXT))

	// Assemble the RXEA file in Aurora's native layout.
	buf := make([]byte, rxeaDataOff+int(tiledSize))

	// Header (28 bytes)
	binary.BigEndian.PutUint32(buf[0x00:], rxeaMagic)
	binary.BigEndian.PutUint32(buf[0x04:], rxeaVersion)
	binary.BigEndian.PutUint32(buf[0x08:], tiledSize)       // our own bookkeeping
	binary.BigEndian.PutUint32(buf[0x0C:], uint32(1)<<uint(slot)) // populated-slot bitmask
	// [0x10..0x1B] zero padding (screenshot count only set for SS files).

	// Entry at slot index — matches Aurora's native per-entry layout.
	eBase := rxeaTableOff + int(slot)*rxeaEntryLen
	// [0..3]   zero marker/hash
	binary.BigEndian.PutUint32(buf[eBase+4:],  0x00000003)
	binary.BigEndian.PutUint32(buf[eBase+8:],  0x00000001)
	// [12..23] zero
	binary.BigEndian.PutUint32(buf[eBase+24:], 0xFFFF0000)
	binary.BigEndian.PutUint32(buf[eBase+28:], 0xFFFF0000)

	// GPU fetch constants (dw7/dw8/dw9) at entry offsets 32/36/40.
	pitchField := uint32(alignedBW * blockSz / 128)
	dw7 := uint32(2) | (pitchField << 22) // bit1=tiled, pitch at bits 22–30
	dw8 := uint32(gpuFmtDXT5) | (uint32(1) << 6) // fmt + endian=8-in-16
	dw9 := uint32(widthPx-1) | (uint32(heightPx-1) << 13)
	binary.BigEndian.PutUint32(buf[eBase+32:], dw7)
	binary.BigEndian.PutUint32(buf[eBase+36:], dw8)
	binary.BigEndian.PutUint32(buf[eBase+40:], dw9)

	binary.BigEndian.PutUint32(buf[eBase+44:], 0x00000D10)
	// [48..51] zero
	binary.BigEndian.PutUint32(buf[eBase+52:], 0x00000A00)
	// [56..63] zero tail

	// Image data at rxeaDataOff.
	copy(buf[rxeaDataOff:], storedDXT)

	return buf, nil
}

// EncodePNGToRXEA is a convenience wrapper: PNG bytes → RXEA bytes.
func EncodePNGToRXEA(pngData []byte, slot AssetSlot) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, fmt.Errorf("rxea: png decode: %w", err)
	}
	return EncodeRXEA(slot, img)
}

// EncodeRXEAToPNG encodes a decoded RXEA entry to PNG bytes.
func EncodeRXEAToPNG(entry *RXEAEntry) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, entry.Img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal — slot decoder
// ──────────────────────────────────────────────────────────────────────────────

// tiledSizeFromFetch derives the on-disk tiled data size from the GPU fetch
// constants (dw7 pitch × padded block height × block size).
func tiledSizeFromFetch(dw7, dw8, dw9 uint32) (uint32, error) {
	dataFmt := int(dw8 & 0x3F)
	widthPx := int(dw9&0x1FFF) + 1
	heightPx := int((dw9>>13)&0x1FFF) + 1
	blockSz, err := dxtBlockSize(dataFmt)
	if err != nil {
		return 0, err
	}
	bw := (widthPx + 3) / 4
	bh := (heightPx + 3) / 4
	pitchField := int((dw7 >> 22) & 0x1FF)
	var alignedBW int
	if pitchField > 0 {
		alignedBW = (pitchField * 128) / blockSz
	}
	if alignedBW < bw || alignedBW > 8192 {
		alignedBW = alignUp(bw, 32)
	}
	alignedBH := alignUp(bh, 32)
	return uint32(alignedBW * alignedBH * blockSz), nil
}

// decodeSlotFetch decodes one RXEA slot given its GPU fetch constants and raw
// tiled DXT data.
func decodeSlotFetch(dw7, dw8, dw9 uint32, raw []byte) (*image.NRGBA, error) {
	tiled      := (dw7>>1)&1 == 1
	pitchField := int((dw7 >> 22) & 0x1FF) // row_pitch_bytes / 128
	dataFmt    := int(dw8 & 0x3F)
	endian     := int((dw8 >> 6) & 0x3)    // 0=none, 1=8in16, 2=8in32, 3=16in32
	widthPx    := int(dw9&0x1FFF) + 1
	heightPx   := int((dw9>>13)&0x1FFF) + 1

	blockSz, err := dxtBlockSize(dataFmt)
	if err != nil {
		return nil, err
	}

	bw := (widthPx + 3) / 4
	bh := (heightPx + 3) / 4

	// Pitch field = row_pitch_bytes / 128.  Derive aligned block width from it.
	// Fall back to the standard 8-block alignment rule when pitch is absent/zero.
	var alignedBW int
	if pitchField > 0 {
		pitchBytes := pitchField * 128
		alignedBW  = pitchBytes / blockSz
	}
	if alignedBW < bw || alignedBW > 8192 {
		alignedBW = alignUp(bw, 32)
	}
	alignedBW = alignUp(alignedBW, 32)
	alignedBH := alignUp(bh, 32)

	// Validate buffer size.
	expectedTiled := alignedBW * alignedBH * blockSz
	if len(raw) < expectedTiled {
		// Be lenient: some files are slightly under-sized due to padding truncation.
		// Pad with zeros so we don't panic on out-of-range reads.
		padded := make([]byte, expectedTiled)
		copy(padded, raw)
		raw = padded
	}

	// Aurora RXEA asset files store DXT data in linear row-major order even
	// though the GPU fetch constant has the Tiled bit set. The tiled flag is
	// metadata the GPU uses at sample time, not a description of on-disk
	// layout. Read the actual block stride from the pitch field and copy
	// row-by-row into a packed linear buffer.
	_ = tiled
	linear := make([]byte, bw*bh*blockSz)
	rowBytes := bw * blockSz
	strideBytes := alignedBW * blockSz
	for by := 0; by < bh; by++ {
		src := by * strideBytes
		dst := by * rowBytes
		if src+rowBytes > len(raw) {
			break
		}
		copy(linear[dst:dst+rowBytes], raw[src:src+rowBytes])
	}

	// Undo the Xbox 360 endian swap so the DXT data is in standard LE format.
	applyEndianSwap(linear, endian)

	switch dataFmt {
	case gpuFmtDXT1:
		return decodeDXT1(linear, widthPx, heightPx, bw, bh), nil
	case gpuFmtDXT3:
		return decodeDXT3(linear, widthPx, heightPx, bw, bh), nil
	case gpuFmtDXT5:
		return decodeDXT5(linear, widthPx, heightPx, bw, bh), nil
	case gpuFmt8888:
		return decode8888(raw, widthPx, heightPx), nil
	default:
		return nil, fmt.Errorf("rxea: unsupported GPU format %d", dataFmt)
	}
}

func dxtBlockSize(gpuFmt int) (int, error) {
	switch gpuFmt {
	case gpuFmtDXT1:
		return 8, nil
	case gpuFmtDXT3, gpuFmtDXT5:
		return 16, nil
	case gpuFmt8888:
		return 4, nil
	}
	return 0, fmt.Errorf("rxea: unsupported GPU format %d", gpuFmt)
}

// ──────────────────────────────────────────────────────────────────────────────
// Texture header builder
// ──────────────────────────────────────────────────────────────────────────────

// buildTextureHeader constructs the 52-byte ASSET_PACK_TEXTURE_HEADER for a
// tiled DXT5 texture.  The D3DResource base fields (DWORDs 0–6) are left zero;
// Aurora's asset loader only requires the GPU_FETCH_CONSTANT fields.
func buildTextureHeader(widthPx, heightPx, fmtCode, blockSz, alignedBW int) [52]byte {
	var h [52]byte

	// GPUTEXTURE_FETCH_CONSTANT_0 (DWORD 7, offset 28):
	//   bit  1     : Tiled = 1
	//   bits 22–30 : Pitch = alignedBW * blockSz / 128
	pitchField := uint32(alignedBW * blockSz / 128)
	dw7 := uint32(2) | (pitchField << 22) // bit1=tiled, pitch at bits 22-30
	binary.BigEndian.PutUint32(h[28:], dw7)

	// GPUTEXTURE_FETCH_CONSTANT_1 (DWORD 8, offset 32):
	//   bits  0–5 : DataFormat
	//   bits  6–7 : Endian = 1 (8-in-16, matches real Aurora .asset files)
	dw8 := uint32(fmtCode) | (uint32(1) << 6)
	binary.BigEndian.PutUint32(h[32:], dw8)

	// GPUTEXTURE_FETCH_CONSTANT_2 (DWORD 9, offset 36):
	//   bits  0–12 : Width  – 1
	//   bits 13–25 : Height – 1
	dw9 := uint32(widthPx-1) | (uint32(heightPx-1) << 13)
	binary.BigEndian.PutUint32(h[36:], dw9)

	return h
}

// ──────────────────────────────────────────────────────────────────────────────
// Endian swap helpers
// ──────────────────────────────────────────────────────────────────────────────

// swapEndian32 byte-reverses every 4-byte group (8-in-32 swap) in place.
func swapEndian32(b []byte) {
	for i := 0; i+3 < len(b); i += 4 {
		b[i], b[i+3] = b[i+3], b[i]
		b[i+1], b[i+2] = b[i+2], b[i+1]
	}
}

func applyEndianSwap(b []byte, endian int) {
	switch endian {
	case 1: // 8-in-16
		for i := 0; i+1 < len(b); i += 2 {
			b[i], b[i+1] = b[i+1], b[i]
		}
	case 2: // 8-in-32
		swapEndian32(b)
	case 3: // 16-in-32
		for i := 0; i+3 < len(b); i += 4 {
			b[i], b[i+2] = b[i+2], b[i]
			b[i+1], b[i+3] = b[i+3], b[i+1]
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// DXT1 decompressor
// ──────────────────────────────────────────────────────────────────────────────

func decodeDXT1(linear []byte, widthPx, heightPx, bw, bh int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, widthPx, heightPx))
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			block := linear[(by*bw+bx)*8:]
			decodeDXT1Block(block, out, bx*4, by*4, widthPx, heightPx, false)
		}
	}
	return out
}

func decodeDXT1Block(block []byte, dst *image.NRGBA, x0, y0, imgW, imgH int, forceOpaque bool) {
	c0 := uint16(block[0])<<8 | uint16(block[1]) // big-endian uint16 after swap → LE
	c1 := uint16(block[2])<<8 | uint16(block[3])

	// After the 8-in-32 endian swap the 8-byte block bytes are:
	//   [3][2][1][0] [7][6][5][4]  (original PC: [0][1][2][3][4][5][6][7])
	// So color0 is block[1]|block[0]<<8 in the swapped layout...
	// Actually after swapEndian32 the block has been byte-reversed per 4 bytes:
	//   swapped[0] = orig[3], swapped[1] = orig[2], swapped[2] = orig[1], swapped[3] = orig[0]
	// For DXT1, PC format: orig[0..1]=color0 LE, orig[2..3]=color1 LE, orig[4..7]=indices LE
	// After swap: swapped[0]=orig[3]=color1_hi, swapped[1]=orig[2]=color1_lo,
	//             swapped[2]=orig[1]=color0_hi, swapped[3]=orig[0]=color0_lo
	// So reading swapped[0..1] as LE uint16 gives (swapped[0]|swapped[1]<<8) = color1_hi|color1_lo<<8 = color1 BE?
	// That doesn't match what I expect.
	//
	// Let me reread: after applyEndianSwap(linear, 2) the data is in PC-native LE DXT format.
	// For DXT1, the correct read after swap is:
	c0 = uint16(block[0]) | uint16(block[1])<<8 // LE uint16
	c1 = uint16(block[2]) | uint16(block[3])<<8 // LE uint16

	var palette [4]color.NRGBA
	palette[0] = rgb565(c0)
	palette[1] = rgb565(c1)

	if c0 > c1 || forceOpaque {
		// 4-color mode
		palette[2] = lerpColor(palette[0], palette[1], 1, 3)
		palette[3] = lerpColor(palette[0], palette[1], 2, 3)
	} else {
		// 3-color + transparent
		palette[2] = lerpColor(palette[0], palette[1], 1, 2)
		palette[3] = color.NRGBA{0, 0, 0, 0}
	}

	// 4 rows of 4 indices (2 bits each), packed in 4 bytes.
	for row := 0; row < 4; row++ {
		rowByte := block[4+row]
		for col := 0; col < 4; col++ {
			px := x0 + col
			py := y0 + row
			if px >= imgW || py >= imgH {
				continue
			}
			idx := (rowByte >> uint(col*2)) & 3
			dst.SetNRGBA(px, py, palette[idx])
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// DXT3 decompressor (4-bit explicit alpha + DXT1 color)
// ──────────────────────────────────────────────────────────────────────────────

func decodeDXT3(linear []byte, widthPx, heightPx, bw, bh int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, widthPx, heightPx))
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			block := linear[(by*bw+bx)*16:]
			// First 8 bytes: 4-bit alpha per pixel (2 pixels per byte)
			// Next 8 bytes: DXT1 color block
			colorBlock := block[8:16]
			decodeDXT1Block(colorBlock, out, bx*4, by*4, widthPx, heightPx, true)
			// Overlay alpha
			for row := 0; row < 4; row++ {
				ab := block[row*2 : row*2+2]
				alphaWord := uint16(ab[0]) | uint16(ab[1])<<8
				for col := 0; col < 4; col++ {
					px := bx*4 + col
					py := by*4 + row
					if px >= widthPx || py >= heightPx {
						continue
					}
					a4 := (alphaWord >> uint(col*4)) & 0xF
					a8 := uint8(a4<<4 | a4)
					c := out.NRGBAAt(px, py)
					c.A = a8
					out.SetNRGBA(px, py, c)
				}
			}
		}
	}
	return out
}

// ──────────────────────────────────────────────────────────────────────────────
// DXT5 (BC3) decompressor
// ──────────────────────────────────────────────────────────────────────────────

func decodeDXT5(linear []byte, widthPx, heightPx, bw, bh int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, widthPx, heightPx))
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			off   := (by*bw + bx) * 16
			block := linear[off : off+16]

			// Alpha block (bytes 0–7)
			a0 := block[0]
			a1 := block[1]
			ap := dxt5AlphaPalette(a0, a1)
			// Alpha indices: 48 bits (bytes 2–7), 3 bits per pixel, LE order
			alphaBits := uint64(block[2]) | uint64(block[3])<<8 | uint64(block[4])<<16 |
				uint64(block[5])<<24 | uint64(block[6])<<32 | uint64(block[7])<<40

			// Color block (bytes 8–15) — always 4-color mode in DXT5
			decodeDXT1Block(block[8:], out, bx*4, by*4, widthPx, heightPx, true)

			// Overlay alpha
			for row := 0; row < 4; row++ {
				for col := 0; col < 4; col++ {
					px := bx*4 + col
					py := by*4 + row
					if px >= widthPx || py >= heightPx {
						continue
					}
					pix := row*4 + col
					idx := (alphaBits >> uint(pix*3)) & 7
					c := out.NRGBAAt(px, py)
					c.A = ap[idx]
					out.SetNRGBA(px, py, c)
				}
			}
		}
	}
	return out
}

func dxt5AlphaPalette(a0, a1 byte) [8]uint8 {
	var p [8]uint8
	p[0] = a0
	p[1] = a1
	if a0 > a1 {
		for i := 2; i < 8; i++ {
			num := int(a0)*(8-i) + int(a1)*(i-1)
			p[i] = uint8((num + 3) / 7)
		}
	} else {
		for i := 2; i < 6; i++ {
			num := int(a0)*(6-i) + int(a1)*(i-1)
			p[i] = uint8((num + 2) / 5)
		}
		p[6] = 0
		p[7] = 255
	}
	return p
}

// ──────────────────────────────────────────────────────────────────────────────
// Uncompressed 8888 decoder (ARGB, big-endian)
// ──────────────────────────────────────────────────────────────────────────────

func decode8888(raw []byte, w, h int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			if i+3 >= len(raw) {
				break
			}
			// Xbox 360 stores ARGB big-endian
			out.SetNRGBA(x, y, color.NRGBA{R: raw[i+1], G: raw[i+2], B: raw[i+3], A: raw[i]})
		}
	}
	return out
}

// ──────────────────────────────────────────────────────────────────────────────
// DXT5 (BC3) encoder
// ──────────────────────────────────────────────────────────────────────────────

// encodeDXT5 compresses nrgba into linear DXT5 blocks (bw×bh blocks).
// Each block is 16 bytes.  Quality is adequate for artwork thumbnails; for
// maximum fidelity a dedicated encoder (squish, stb_dxt) may be preferable.
func encodeDXT5(img *image.NRGBA, bw, bh int) []byte {
	out := make([]byte, bw*bh*16)
	bounds := img.Bounds()
	imgW   := bounds.Dx()
	imgH   := bounds.Dy()

	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			// Gather 4×4 pixel block (clamp at edges).
			var px [16]color.NRGBA
			for row := 0; row < 4; row++ {
				for col := 0; col < 4; col++ {
					sx := bx*4 + col
					sy := by*4 + row
					if sx >= imgW {
						sx = imgW - 1
					}
					if sy >= imgH {
						sy = imgH - 1
					}
					px[row*4+col] = img.NRGBAAt(sx+bounds.Min.X, sy+bounds.Min.Y)
				}
			}

			// Encode alpha (DXT5 BC3 alpha block).
			alphaBlock := encodeDXT5AlphaBlock(px[:])

			// Encode color (DXT1-style, always 4-color mode).
			colorBlock := encodeDXT1ColorBlock(px[:])

			off := (by*bw + bx) * 16
			copy(out[off:], alphaBlock[:])
			copy(out[off+8:], colorBlock[:])
		}
	}
	return out
}

func encodeDXT5AlphaBlock(px []color.NRGBA) [8]byte {
	// Find min/max alpha.
	minA, maxA := px[0].A, px[0].A
	for _, p := range px[1:] {
		if p.A < minA {
			minA = p.A
		}
		if p.A > maxA {
			maxA = p.A
		}
	}

	var b [8]byte
	// Use maxA as a0, minA as a1 to ensure 8-value interpolation (a0 > a1).
	a0, a1 := maxA, minA
	if a0 == a1 {
		a1 = 0
		if a0 > 0 {
			a1 = a0 - 1
		}
	}
	b[0] = a0
	b[1] = a1
	ap := dxt5AlphaPalette(a0, a1)

	var bits uint64
	for i, p := range px {
		best, bestD := 0, 999
		for j, av := range ap {
			d := absi(int(p.A) - int(av))
			if d < bestD {
				bestD = d
				best = j
			}
		}
		bits |= uint64(best) << uint(i*3)
	}
	// Pack 48-bit index into bytes 2–7 (little-endian).
	b[2] = byte(bits)
	b[3] = byte(bits >> 8)
	b[4] = byte(bits >> 16)
	b[5] = byte(bits >> 24)
	b[6] = byte(bits >> 32)
	b[7] = byte(bits >> 40)
	return b
}

func encodeDXT1ColorBlock(px []color.NRGBA) [8]byte {
	// Find bounding box in RGB space.
	minR, minG, minB := px[0].R, px[0].G, px[0].B
	maxR, maxG, maxB := minR, minG, minB
	for _, p := range px[1:] {
		if p.R < minR {
			minR = p.R
		}
		if p.G < minG {
			minG = p.G
		}
		if p.B < minB {
			minB = p.B
		}
		if p.R > maxR {
			maxR = p.R
		}
		if p.G > maxG {
			maxG = p.G
		}
		if p.B > maxB {
			maxB = p.B
		}
	}

	c0 := toRGB565(maxR, maxG, maxB)
	c1 := toRGB565(minR, minG, minB)

	// Ensure 4-color mode by making c0 > c1.
	if c0 == c1 {
		if c1 > 0 {
			c1--
		} else {
			c0++
		}
	}
	if c0 < c1 {
		c0, c1 = c1, c0
	}

	palette := [4]color.NRGBA{
		rgb565(c0),
		rgb565(c1),
		lerpColor(rgb565(c0), rgb565(c1), 1, 3),
		lerpColor(rgb565(c0), rgb565(c1), 2, 3),
	}

	var b [8]byte
	// color0 and color1 stored little-endian (PC DXT1 format).
	b[0] = byte(c0)
	b[1] = byte(c0 >> 8)
	b[2] = byte(c1)
	b[3] = byte(c1 >> 8)

	for row := 0; row < 4; row++ {
		var rowByte byte
		for col := 0; col < 4; col++ {
			p := px[row*4+col]
			best, bestD := 0, math.MaxInt32
			for j, pc := range palette {
				d := colorDistSq(p, pc)
				if d < bestD {
					bestD = d
					best = j
				}
			}
			rowByte |= byte(best) << uint(col*2)
		}
		b[4+row] = rowByte
	}
	return b
}

// ──────────────────────────────────────────────────────────────────────────────
// Color helpers
// ──────────────────────────────────────────────────────────────────────────────

func rgb565(c uint16) color.NRGBA {
	r5 := (c >> 11) & 0x1F
	g6 := (c >> 5) & 0x3F
	b5 := c & 0x1F
	return color.NRGBA{
		R: uint8(r5<<3 | r5>>2),
		G: uint8(g6<<2 | g6>>4),
		B: uint8(b5<<3 | b5>>2),
		A: 255,
	}
}

func toRGB565(r, g, b uint8) uint16 {
	return uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
}

func lerpColor(a, b color.NRGBA, num, den int) color.NRGBA {
	lerp := func(x, y uint8) uint8 { return uint8((int(x)*(den-num) + int(y)*num + den/2) / den) }
	return color.NRGBA{lerp(a.R, b.R), lerp(a.G, b.G), lerp(a.B, b.B), 255}
}

func colorDistSq(a, b color.NRGBA) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	return dr*dr + dg*dg + db*db
}

// ──────────────────────────────────────────────────────────────────────────────
// Misc helpers
// ──────────────────────────────────────────────────────────────────────────────

func alignUp(n, align int) int {
	return (n + align - 1) &^ (align - 1)
}

func absi(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func toNRGBA(src image.Image) *image.NRGBA {
	if n, ok := src.(*image.NRGBA); ok {
		return n
	}
	b := src.Bounds()
	n := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			n.Set(x, y, src.At(x, y))
		}
	}
	return n
}
