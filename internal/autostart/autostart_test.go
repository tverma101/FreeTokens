package autostart

import (
	"os"
	"path/filepath"
	"testing"
)

// A login item must outlive the process, and an AppImage mount does not, so the entry names the
// AppImage file instead of the executable inside it.
func TestTrayEntryNamesTheAppImage(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "FreeTokens.AppImage")
	if err := os.WriteFile(img, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPIMAGE", img)

	entry, err := TrayEntry()
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Exec) != 2 || entry.Exec[0] != img || entry.Exec[1] != "gui" {
		t.Fatalf("got %v, want [%s gui]", entry.Exec, img)
	}
}

// A normal install resolves its own executable and still asks for the desktop front end.
func TestTrayEntryNormalInstall(t *testing.T) {
	t.Setenv("APPIMAGE", "")

	entry, err := TrayEntry()
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Exec) != 2 || !filepath.IsAbs(entry.Exec[0]) || entry.Exec[1] != "gui" {
		t.Fatalf("got %v, want an absolute executable and the gui argument", entry.Exec)
	}
}
