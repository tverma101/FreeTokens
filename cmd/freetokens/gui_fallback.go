//go:build !gui || !(windows || (linux && cgo))

package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/tverma101/FreeTokens/internal/config"
	"github.com/tverma101/FreeTokens/internal/tracker"
)

// hasPopover reports whether this build embeds the Wails popover instead of only the tray menu.
// `freetokens diagnose` prints it, so a missing window on Linux is one command away from an answer.
const hasPopover = false

// runGUIUI falls back to the classic tray menu on builds without Wails support: the CGO-free
// Linux and BSD binaries, and macOS, where the menu bar app is the SwiftUI one in FreeTokens.app.
// The popover needs cgo, so a plain `go install` or a release archive on Linux gives the tray;
// saying so beats starting a tray with no explanation.
func runGUIUI(t *tracker.Tracker, cfg *config.Store) {
	msg := "This build has no popover, so the tray menu is starting instead."
	switch runtime.GOOS {
	case "darwin":
		msg += " On macOS the menu bar app is FreeTokens.app; open it from Applications."
	case "linux":
		msg += " For the popover on Linux, install libgtk-3-dev and libwebkit2gtk-4.1-dev, " +
			"build the frontend, and compile with -tags \"gui production webkit2_41\" (see the README)."
	}
	fmt.Fprintln(os.Stderr, msg)
	runTrayUI(t, cfg)
}
