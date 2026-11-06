//go:build ignore

// mkicons writes the Windows executable icon PNGs into winres/ from the same runtime-drawn glyph
// the tray uses. Run: go run scripts/mkicons.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tverma101/FreeTokens/internal/icon"
)

func main() {
	for _, n := range []int{16, 24, 32, 48, 64, 256} {
		p := filepath.Join("winres", fmt.Sprintf("icon%d.png", n))
		if err := os.WriteFile(p, icon.PNG(n), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
