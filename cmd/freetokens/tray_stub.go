//go:build !((darwin && cgo) || windows || linux || freebsd || openbsd || netbsd)

package main

import (
	"fmt"
	"os"

	"github.com/tverma101/FreeTokens/internal/config"
	"github.com/tverma101/FreeTokens/internal/tracker"
)

func runTrayUI(*tracker.Tracker, *config.Store) {
	fmt.Fprintln(os.Stderr, "This FreeTokens build has no tray support; use \"freetokens status\".")
	os.Exit(1)
}
