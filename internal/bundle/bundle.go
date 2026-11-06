// Package bundle recognizes the macOS app layout, where the FreeTokens engine ships inside
// FreeTokens.app as Contents/Helpers/freetokens and the SwiftUI menu bar app is the main executable.
package bundle

import (
	"os"
	"path/filepath"
	"strings"
)

// HelperDir is the bundle directory that holds the engine binary.
const HelperDir = "Helpers"

// AppFor returns the .app directory that contains exe as its engine
// (<App>.app/Contents/Helpers/<exe>), or "" when exe is a standalone binary.
func AppFor(exe string) string {
	helpers := filepath.Dir(exe)
	if filepath.Base(helpers) != HelperDir {
		return ""
	}
	contents := filepath.Dir(helpers)
	if filepath.Base(contents) != "Contents" {
		return ""
	}
	app := filepath.Dir(contents)
	if !strings.EqualFold(filepath.Ext(app), ".app") {
		return ""
	}
	if st, err := os.Stat(filepath.Join(contents, "Info.plist")); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return app
}

// Current returns the app bundle around the running executable, or "".
func Current() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return AppFor(exe)
}
