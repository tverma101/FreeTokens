// Package autostart registers FreeTokens to start at login without cgo: a LaunchAgent on macOS,
// an XDG autostart entry on Linux/BSD, and an HKCU Run value on Windows.
package autostart

import (
	"os"
	"path/filepath"
	"runtime"
)

// ID is the login item identifier.
const ID = "io.github.tverma101.freetokens"

// Entry is a login item.
type Entry struct {
	// Exec is the absolute executable followed by its arguments.
	Exec []string
}

// WindowlessName is the GUI-subsystem build shipped beside freetokens.exe on Windows. It starts the
// tray without a console window, so login items and shortcuts should point at it.
const WindowlessName = "freetokensw.exe"

// TrayEntry is the login item for this installation: the running executable with "tray", or, on
// Windows, the windowless sibling when it is present.
func TrayEntry() (Entry, error) {
	exe, err := os.Executable()
	if err != nil {
		return Entry{}, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if runtime.GOOS == "windows" {
		if w := filepath.Join(filepath.Dir(exe), WindowlessName); fileExists(w) {
			exe = w
		}
	}
	return Entry{Exec: []string{exe, "tray"}}, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
