// rxea_roundtrip: decodes an RXEA from stdin, re-encodes the first non-empty
// slot, decodes the result again, and writes before/after PNGs so the two
// can be compared visually.
package main

import (
	"fmt"
	"image/png"
	"io"
	"os"

	"godsend/utils"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}

	entries, _, err := utils.DecodeRXEA(data)
	if err != nil || len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(1)
	}
	orig := entries[0]
	fmt.Printf("orig slot=%d %dx%d\n", orig.Slot, orig.Width, orig.Height)

	f, _ := os.Create("before.png")
	png.Encode(f, orig.Img)
	f.Close()

	encoded, err := utils.EncodeRXEA(orig.Slot, orig.Img)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
	fmt.Printf("encoded size: %d bytes\n", len(encoded))

	entries2, _, err := utils.DecodeRXEA(encoded)
	if err != nil || len(entries2) == 0 {
		fmt.Fprintln(os.Stderr, "re-decode:", err)
		os.Exit(1)
	}
	after := entries2[0]
	fmt.Printf("after slot=%d %dx%d\n", after.Slot, after.Width, after.Height)

	f, _ = os.Create("after.png")
	png.Encode(f, after.Img)
	f.Close()
}
