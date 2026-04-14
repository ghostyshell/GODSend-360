// rxea_decode_test: reads a raw RXEA .asset file from stdin, decodes it,
// and writes each decoded slot as a PNG file to the current directory.
//
// Usage:
//   go run tools/rxea_decode_test/main.go < /tmp/BK555307D4.asset
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
	fmt.Printf("Read %d bytes\n", len(data))

	entries, diags, err := utils.DecodeRXEA(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "DecodeRXEA error:", err)
		os.Exit(1)
	}

	fmt.Printf("Diags (%d non-empty slots):\n", len(diags))
	for _, d := range diags {
		errStr := ""
		if d.Error != "" {
			errStr = " ERROR: " + d.Error
		}
		fmt.Printf("  slot %2d: %dx%d fmt=%d tiled=%v endian=%d off=%d sz=%d%s\n",
			d.Slot, d.Width, d.Height, d.GpuFmt, d.Tiled, d.Endian, d.Offset, d.Size, errStr)
	}

	fmt.Printf("\nDecoded %d image(s):\n", len(entries))
	for _, e := range entries {
		fname := fmt.Sprintf("slot%02d_%dx%d.png", int(e.Slot), e.Width, e.Height)
		f, ferr := os.Create(fname)
		if ferr != nil {
			fmt.Fprintln(os.Stderr, "create:", ferr)
			continue
		}
		if perr := png.Encode(f, e.Img); perr != nil {
			fmt.Fprintln(os.Stderr, "encode:", perr)
		}
		f.Close()
		fmt.Printf("  wrote %s\n", fname)
	}
}
