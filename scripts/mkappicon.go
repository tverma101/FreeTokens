//go:build ignore

// mkappicon writes the Wails application icon from the same runtime-drawn coin the
// tray uses. Run: go run scripts/mkappicon.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tverma101/FreeTokens/internal/icon"
)

func main() {
	path := filepath.Join("cmd", "freetokens", "build", "appicon.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, icon.PNG(512), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(path)
}
