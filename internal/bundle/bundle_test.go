package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppFor(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "FreeTokens.app")
	helpers := filepath.Join(app, "Contents", HelperDir)
	if err := os.MkdirAll(helpers, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(helpers, "freetokens")
	if got := AppFor(exe); got != "" {
		t.Fatalf("without Info.plist got %q, want empty", got)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := AppFor(exe); got != app {
		t.Fatalf("AppFor = %q, want %q", got, app)
	}
	for _, p := range []string{
		filepath.Join(root, "freetokens"),
		filepath.Join(app, "Contents", "MacOS", "freetokens"),
		filepath.Join(root, "Other", "Contents", HelperDir, "freetokens"),
	} {
		if got := AppFor(p); got != "" {
			t.Errorf("AppFor(%q) = %q, want empty", p, got)
		}
	}
}
