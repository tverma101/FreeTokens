// Package appimage tells a Linux AppImage run apart from a normal install. The launcher exports
// $APPIMAGE with the file the user started; the process itself runs from a read-only mount under
// /tmp that is gone once the process exits, so anything that must outlive the process (a login
// item, a self-update) has to name the AppImage file instead of the executable inside the mount.
package appimage

import (
	"os"
	"path/filepath"
	"strings"
)

// Path returns the AppImage file this process runs from, or "" for any other install.
func Path() string {
	p := strings.TrimSpace(os.Getenv("APPIMAGE"))
	if p == "" || !filepath.IsAbs(p) {
		return ""
	}
	if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return p
}
