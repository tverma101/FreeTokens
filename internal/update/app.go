package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

// AppAssetPattern matches the macOS app archive, for example FreeTokens-0.2.0-macOS.zip. The CLI
// archives end in _darwin_<arch>.tar.gz, so neither updater can pick the other's asset.
const AppAssetPattern = `^freetokens-[0-9][^/]*-macos\.zip$`

// AppAssetName is the macOS app archive name for a release version.
func AppAssetName(version string) string { return "FreeTokens-" + Normalize(version) + "-macOS.zip" }

const (
	maxAppArchive = 256 << 20
	maxChecksums  = 1 << 20
)

// applyApp replaces the whole FreeTokens.app around the engine with the release's app archive,
// after checking it against checksums.txt. The caller relaunches the app.
func applyApp(ctx context.Context, info *Info, app string) error {
	rel := info.release
	if rel.ValidationAssetURL == "" {
		return errors.New("the release has no checksums.txt, so the update can't be verified")
	}
	parent := filepath.Dir(app)
	// Work beside the app so the final swap is a rename on one volume.
	work, err := os.MkdirTemp(parent, ".freetokens-update-")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("FreeTokens can't write to %s. Move FreeTokens.app to a folder you own, such as Applications, or download the update from the Releases page", parent)
		}
		return err
	}
	// A failed installation followed by a failed rollback must not delete the only valid app.
	preserveBackup := false
	defer func() {
		if !preserveBackup {
			_ = os.RemoveAll(work)
		}
	}()

	archive, err := fetch(ctx, rel.AssetURL, maxAppArchive)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", rel.AssetName, err)
	}
	sums, err := fetch(ctx, rel.ValidationAssetURL, maxChecksums)
	if err != nil {
		return fmt.Errorf("downloading checksums.txt: %w", err)
	}
	if err := (&selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"}).Validate(rel.AssetName, archive, sums); err != nil {
		return fmt.Errorf("%s failed verification: %w", rel.AssetName, err)
	}
	zipPath := filepath.Join(work, "update.zip")
	if err := os.WriteFile(zipPath, archive, 0o600); err != nil {
		return err
	}
	unpacked := filepath.Join(work, "unpacked")
	if out, err := exec.CommandContext(ctx, "/usr/bin/ditto", "-x", "-k", zipPath, unpacked).CombinedOutput(); err != nil {
		return fmt.Errorf("unpacking %s: %v: %s", rel.AssetName, err, out)
	}
	fresh := filepath.Join(unpacked, filepath.Base(app))
	if _, err := os.Stat(fresh); err != nil {
		fresh = filepath.Join(unpacked, "FreeTokens.app")
	}
	for _, p := range []string{"Contents/Info.plist", "Contents/MacOS/FreeTokens", "Contents/Helpers/freetokens"} {
		if st, err := os.Stat(filepath.Join(fresh, p)); err != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("%s is missing %s", rel.AssetName, p)
		}
	}
	if out, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", fresh).CombinedOutput(); err != nil {
		return fmt.Errorf("the new FreeTokens.app has an invalid signature: %v: %s", err, out)
	}
	old := filepath.Join(work, "previous.app")
	preserveBackup, err = replaceApp(fresh, app, old, os.Rename)
	return err
}

// replaceApp swaps the bundles and restores the old one if installing the replacement fails.
// It returns preserveBackup when rollback also fails; callers must keep the backup directory
// intact so a user can recover the original app rather than deleting it during cleanup.
func replaceApp(fresh, app, old string, rename func(string, string) error) (preserveBackup bool, err error) {
	if err := rename(app, old); err != nil {
		return false, fmt.Errorf("moving the current app aside: %w", err)
	}
	if err := rename(fresh, app); err != nil {
		if rerr := rename(old, app); rerr != nil {
			return true, fmt.Errorf("installing the new app failed (%v), rollback failed (%v); original app preserved at %s", err, rerr, old)
		}
		return false, fmt.Errorf("installing the new app: %w", err)
	}
	return false, nil
}

var downloadClient = &http.Client{Timeout: 10 * time.Minute}

func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("file is larger than expected")
	}
	return data, nil
}
