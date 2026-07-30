// Package selfupdate wraps github.com/creativeprojects/go-selfupdate to let
// chatxgo update its own binary from GitHub releases.
package selfupdate

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	su "github.com/creativeprojects/go-selfupdate"
	"github.com/sig9org/chatxgo/internal/debugx"
)

// Repository is "owner/repo" on GitHub, where release binaries are
// published (see Taskfile.yml build-all for the naming convention).
const Repository = "sig9org/chatxgo"

// Update checks GitHub for a release newer than currentVersion and, if
// found, replaces the running binary in place. It reports the outcome as a
// human-readable message.
func Update(ctx context.Context, currentVersion string) (string, error) {
	debugx.Printf("checking %s for a release newer than %s", Repository, currentVersion)

	repo := su.ParseSlug(Repository)
	latest, found, err := su.DetectLatest(ctx, repo)
	if err != nil {
		return "", fmt.Errorf("selfupdate: detect latest release: %w", err)
	}
	if !found {
		return "", fmt.Errorf("selfupdate: no release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	debugx.Printf("latest release found: %s (%s)", latest.Version(), latest.AssetName)

	if strings.TrimSpace(currentVersion) != "" && currentVersion != "dev" &&
		latest.LessOrEqual(currentVersion) {
		return fmt.Sprintf("already up to date (current %s, latest %s)", currentVersion, latest.Version()), nil
	}

	exe, err := su.ExecutablePath()
	if err != nil {
		return "", fmt.Errorf("selfupdate: locate running executable: %w", err)
	}
	debugx.Printf("updating executable at %s", exe)

	release, err := su.UpdateCommand(ctx, exe, currentVersion, repo)
	if err != nil {
		return "", fmt.Errorf("selfupdate: update failed: %w", err)
	}
	return fmt.Sprintf("updated to version %s", release.Version()), nil
}
