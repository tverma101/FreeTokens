package appimage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPath(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "FreeTokens-1.2.3-x86_64.AppImage")
	if err := os.WriteFile(img, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("APPIMAGE", img)
	if got := Path(); got != img {
		t.Fatalf("got %q, want %q", got, img)
	}

	// Unset, relative, missing and not-a-file all mean "not an AppImage run".
	for _, value := range []string{"", "  ", "FreeTokens.AppImage", filepath.Join(dir, "missing.AppImage"), dir} {
		t.Setenv("APPIMAGE", value)
		if got := Path(); got != "" {
			t.Fatalf("%q: got %q, want an empty path", value, got)
		}
	}
}
