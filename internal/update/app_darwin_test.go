//go:build darwin

package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/creativeprojects/go-selfupdate"
)

func TestAppAssetPattern(t *testing.T) {
	re := regexp.MustCompile(AppAssetPattern)
	for name, want := range map[string]bool{
		AppAssetName("v0.2.0"):                 true,
		"FreeTokens-0.2.1-SNAPSHOT-macOS.zip":  true,
		"freetokens_0.2.0_darwin_arm64.tar.gz": false,
		"freetokens_0.2.0_windows_amd64.zip":   false,
		"checksums.txt":                        false,
	} {
		// go-selfupdate matches filters against the lowercased asset name.
		if got := re.MatchString(strings.ToLower(name)); got != want {
			t.Errorf("%s: match=%v, want %v", name, got, want)
		}
	}
}

// fakeApp builds a minimal FreeTokens.app whose Resources/marker holds marker, ad-hoc signed
// unless unsigned is set.
func fakeApp(t *testing.T, dir, marker, bundleID, bundleVersion string, unsigned bool) string {
	t.Helper()
	app := filepath.Join(dir, "FreeTokens.app")
	for _, d := range []string{"MacOS", "Helpers", "Resources"} {
		if err := os.MkdirAll(filepath.Join(app, "Contents", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>FreeTokens</string>
<key>CFBundleIdentifier</key><string>%s</string>
<key>CFBundleShortVersionString</key><string>%s</string>
<key>CFBundlePackageType</key><string>APPL</string>
</dict></plist>`, bundleID, bundleVersion)
	write := func(rel string, data []byte, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(app, "Contents", rel), data, mode); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Skip("no /usr/bin/true to use as a Mach-O stand-in")
	}
	write("Info.plist", []byte(plist), 0o644)
	write("MacOS/FreeTokens", exe, 0o755)
	write("Helpers/freetokens", exe, 0o755)
	write("Resources/marker", []byte(marker), 0o644)
	if !unsigned {
		for _, target := range []string{filepath.Join(app, "Contents/Helpers/freetokens"), app} {
			if out, err := exec.Command("codesign", "--force", "--sign", "-", target).CombinedOutput(); err != nil {
				t.Fatalf("codesign %s: %v: %s", target, err, out)
			}
		}
	}
	return app
}

func zipApp(t *testing.T, app, zip string) []byte {
	t.Helper()
	if out, err := exec.Command("ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", app, zip).CombinedOutput(); err != nil {
		t.Fatalf("ditto: %v: %s", err, out)
	}
	data, err := os.ReadFile(zip)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func marker(t *testing.T, app string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(app, "Contents/Resources/marker"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestApplyApp(t *testing.T) {
	if _, err := exec.LookPath("codesign"); err != nil {
		t.Skip("codesign not available")
	}
	const asset = "FreeTokens-9.9.9-macOS.zip"
	cases := []struct {
		name       string
		unsigned   bool
		badSum     bool
		bundleID   string
		bundleVer  string
		wantErr    string
	}{
		{name: "installs"},
		{name: "checksum mismatch", badSum: true, wantErr: "failed verification"},
		{name: "unsigned", unsigned: true, wantErr: "invalid signature"},
		{name: "wrong bundle identity", bundleID: "org.example.NotFreeTokens", wantErr: "unexpected CFBundleIdentifier"},
		{name: "wrong bundle version", bundleVer: "9.9.8", wantErr: "unexpected CFBundleShortVersionString"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			install := filepath.Join(root, "Applications")
			build := filepath.Join(root, "build")
			for _, d := range []string{install, build} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			const correctID = "io.github.tverma101.FreeTokens"
			app := fakeApp(t, install, "old", correctID, "9.9.8", false)
			bundleID, bundleVer := tc.bundleID, tc.bundleVer
			if bundleID == "" { bundleID = correctID }
			if bundleVer == "" { bundleVer = "9.9.9" }
			archive := zipApp(t, fakeApp(t, build, "new", bundleID, bundleVer, tc.unsigned), filepath.Join(root, asset))
			sum := sha256.Sum256(archive)
			if tc.badSum {
				sum[0] ^= 0xff
			}
			sums := fmt.Sprintf("%x  %s\n%x  freetokens_9.9.9_darwin_arm64.tar.gz\n", sum, asset, sha256.Sum256(nil))

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/" + asset:
					w.Write(archive)
				case "/checksums.txt":
					w.Write([]byte(sums))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			info := &Info{Latest: "9.9.9", release: &selfupdate.Release{
				AssetName:          asset,
				AssetURL:           srv.URL + "/" + asset,
				ValidationAssetURL: srv.URL + "/checksums.txt",
			}}
			err := applyApp(context.Background(), info, app)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("applyApp: %v", err)
				}
				if got := marker(t, app); got != "new" {
					t.Fatalf("installed app marker = %q, want new", got)
				}
				if out, err := exec.Command("codesign", "--verify", "--deep", "--strict", app).CombinedOutput(); err != nil {
					t.Fatalf("installed app signature: %v: %s", err, out)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("applyApp error = %v, want %q", err, tc.wantErr)
				}
				if got := marker(t, app); got != "old" {
					t.Fatalf("app marker after failed update = %q, want old", got)
				}
			}
			// No work directories are left beside the app.
			entries, _ := os.ReadDir(install)
			if len(entries) != 1 {
				var names []string
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Fatalf("install dir has %v, want only FreeTokens.app", names)
			}
		})
	}
}

func TestReplaceAppRollbackPreservesOldApp(t *testing.T) {
	for _, tc := range []struct {
		name         string
		rollbackFail bool
	}{
		{name: "install fails and rollback restores original"},
		{name: "install and rollback both fail; backup survives", rollbackFail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			app := filepath.Join(root, "FreeTokens.app")
			fresh := filepath.Join(root, "new", "FreeTokens.app")
			backup := filepath.Join(root, "work", "previous.app")
			for _, dir := range []string{app, fresh, filepath.Dir(backup)} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(app, "original-marker"), []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			renamed := 0
			preserve, err := replaceApp(fresh, app, backup, func(from, to string) error {
				renamed++
				if renamed == 2 || (renamed == 3 && tc.rollbackFail) {
					return fmt.Errorf("simulated rename failure on call %d", renamed)
				}
				return os.Rename(from, to)
			})
			if err == nil || preserve != tc.rollbackFail {
				t.Fatalf("preserve=%v, error=%v; want preserve=%v and an error", preserve, err, tc.rollbackFail)
			}
			original := filepath.Join(app, "original-marker")
			if tc.rollbackFail {
				original = filepath.Join(backup, "original-marker")
			}
			if _, err := os.Stat(original); err != nil {
				t.Fatalf("original app lost after failed install: %v", err)
			}
		})
	}
}
