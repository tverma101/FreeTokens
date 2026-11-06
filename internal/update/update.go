// Package update checks GitHub Releases for a newer FreeTokens and, only when asked, replaces the
// running executable after verifying the asset against the release's checksums.txt.
package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/creativeprojects/go-selfupdate"
)

// Repo is the GitHub repository that publishes releases.
const Repo = "tverma101/FreeTokens"

// ErrDevBuild means the binary has no release version, so updates can't be compared.
var ErrDevBuild = errors.New("this is a development build; updates are only available for released versions")

// Info describes the newest release for this platform.
type Info struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	URL       string `json:"url,omitempty"`
	AssetName string `json:"asset,omitempty"`
	Notes     string `json:"notes,omitempty"`
	release   *selfupdate.Release
}

func updater() (*selfupdate.Updater, error) {
	src, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, err
	}
	return selfupdate.NewUpdater(selfupdate.Config{
		Source:    src,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
	})
}

// Normalize strips a leading "v"; empty or "dev" means a development build.
func Normalize(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }

// IsRelease reports whether v is a comparable release version.
func IsRelease(v string) bool {
	v = Normalize(v)
	return v != "" && v != "dev" && !strings.Contains(v, "SNAPSHOT") && v[0] >= '0' && v[0] <= '9'
}

// Check finds the latest release for this OS/arch. It sends one anonymous request to the GitHub API
// (GITHUB_TOKEN is used if set, for rate limits).
func Check(ctx context.Context, current string) (*Info, error) {
	up, err := updater()
	if err != nil {
		return nil, err
	}
	rel, found, err := up.DetectLatest(ctx, selfupdate.ParseSlug(Repo))
	if err != nil {
		return nil, fmt.Errorf("checking GitHub releases: %w", err)
	}
	info := &Info{Current: Normalize(current)}
	if !found {
		return info, nil
	}
	info.Latest, info.URL, info.AssetName, info.Notes, info.release = rel.Version(), rel.URL, rel.AssetName, rel.ReleaseNotes, rel
	if IsRelease(current) {
		info.Available = rel.GreaterThan(Normalize(current))
	}
	return info, nil
}

// Apply downloads, verifies and installs info's release over the running executable.
func Apply(ctx context.Context, info *Info) error {
	if info == nil || info.release == nil || !info.Available {
		return errors.New("no update to install")
	}
	if !IsRelease(info.Current) {
		return ErrDevBuild
	}
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("locating the running executable: %w", err)
	}
	up, err := updater()
	if err != nil {
		return err
	}
	if err := up.UpdateTo(ctx, info.release, exe); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("FreeTokens can't replace %s (permission denied). Reinstall it somewhere you can write, or update with your package manager", exe)
		}
		return fmt.Errorf("installing %s: %w", info.Latest, err)
	}
	// Windows archives ship freetokens.exe and the windowless freetokensw.exe; keep them in step.
	if sib := sibling(exe); sib != "" {
		if err := up.UpdateTo(ctx, info.release, sib); err != nil {
			return fmt.Errorf("installing %s for %s: %w", info.Latest, filepath.Base(sib), err)
		}
	}
	return nil
}

// sibling returns the other Windows executable in exe's directory, if it is installed.
func sibling(exe string) string {
	dir, base := filepath.Split(exe)
	var other string
	switch strings.ToLower(base) {
	case "freetokens.exe":
		other = "freetokensw.exe"
	case "freetokensw.exe":
		other = "freetokens.exe"
	default:
		return ""
	}
	p := filepath.Join(dir, other)
	if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return p
}
